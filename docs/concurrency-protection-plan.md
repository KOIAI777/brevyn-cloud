# Brevyn Cloud 并发保护方案

状态：草案

最后更新：2026-06-01

## 目标

这份文档从一个用户的完整生命周期出发，列清楚 Brevyn Cloud 哪些地方需要并发保护、现在已经有什么保护、还缺什么、以及后面应该怎么做模拟测试。

核心边界：

```text
Sub2API 负责模型请求并发。
Brevyn Cloud 负责产品控制面并发。
```

Sub2API 的模型请求链路已经有 Redis 级别的用户并发、账号并发和等待队列保护。它用 Redis Sorted Set + Lua 脚本做槽位获取、释放、过期清理和跨实例并发控制。所以 Brevyn Cloud 不应该为了“控制并发”去代理正常模型请求。

模型流量应该这样走：

```text
客户端 -> api.brevyn.org -> Sub2API -> 上游模型账号
```

Brevyn Cloud 重点保护这些操作：

```text
用户注册
用户登录 / token 刷新
Sub2API 影子账号创建
Sub2API API Key 创建
兑换码使用
余额 / 套餐同步到 Sub2API
管理员赠送余额
管理员轮换 / 禁用 key
管理员批量生成卡密
管理员同步分组 / 模型目录
worker 重试网关操作
```

## 保护手段总表

| 手段 | 用在哪里 | 作用 |
| --- | --- | --- |
| Redis 限速 | 注册、登录、兑换、获取 key、同步按钮 | 请求进数据库 / Sub2API 前先挡掉异常流量 |
| PostgreSQL 唯一约束 | 邮箱、兑换码 hash、兑换记录、每组一个 key | 最后的数据一致性兜底 |
| PostgreSQL 事务 + `FOR UPDATE` | 兑换码、刷新 token、钱包流水 | 防止双花、重复兑换、token 复用 |
| 幂等键 | 兑换同步、管理员赠送、worker 操作 | 失败重试不重复加钱 |
| 用户级锁 | 创建影子账号、创建 key、轮换 key | 防止同一个用户并发创建多份远程资源 |
| 持久化队列 | Sub2API 写操作失败重试 | Sub2API 慢或挂掉时，用户流程不中断 |
| worker 批量限制 | gateway_operations 消费 | backlog 恢复时不打爆 Sub2API |
| 短超时 | 注册、兑换、获取 provider 配置 | 避免用户请求一直卡在 Sub2API 上 |
| 本地读模型 | 用户信息、分组、模型、余额展示 | 用户打开页面不频繁请求 Sub2API |

## 当前已有保护

| 区域 | 当前保护 |
| --- | --- |
| 兑换尝试 | Redis 限速：IP 60/10min，用户 20/10min，卡密 5/10min |
| 兑换码只能用一次 | `redeem_codes` 使用 `FOR UPDATE` 锁行，`redeem_redemptions` 有 `UNIQUE(redeem_code_id)` |
| Refresh Token 轮换 | refresh token 行使用 `FOR UPDATE`；旧 RT 复用会吊销整个 family |
| 网关同步队列 | `gateway_operations` 有幂等键、重试退避、`FOR UPDATE SKIP LOCKED`、batch size 10 |
| 注册入口 | Redis 限速：IP 10/10min，邮箱 5/30min，IP+邮箱 5/10min |
| 注册后的网关创建 | Sub2API provisioning 最多同时 10 个同步尝试、最多等待 3 秒；忙/失败后写入 `provision_gateway_credential` 队列 |
| 用户登录 | Redis 请求限速：IP 120/10min、IP+邮箱 20/10min；失败限速：IP+邮箱 5/15min、邮箱 12/15min、IP 60/15min |
| 用户网关创建 | `GatewaySyncService` 有进程内用户锁 + Redis 用户级分布式锁 |
| 管理员登录 | 内存失败限制：失败 5 次，封 15 分钟 |
| 模型请求并发 | Sub2API 自己用 Redis 用户/账号并发槽位和等待队列 |

## 生产前要补的缺口

| 缺口 | 风险 | 建议 |
| --- | --- | --- |
| 用户注册没有 Redis 限速 | 注册攻击会消耗 bcrypt CPU，刷大量本地用户 | 已补：IP + 邮箱 hash 限速，放在 bcrypt 前 |
| 用户登录没有 Redis 限速 | 密码爆破会反复打 bcrypt | 已补：请求限速 + 失败计数，成功后清理 IP+邮箱失败计数 |
| 网关创建只有进程内锁 | 多 API 实例时锁不共享 | 已补：Redis 用户级分布式锁，Redis 不注入时保留进程内锁兜底 |
| 注册时会同步尝试 Sub2API | 同时大量注册会冲 Sub2API 管理接口 | 已补：立即 provisioning 并发 10、短超时、失败入队 |
| `/provider/conversation` 和 `/provider/official` 可重复点击 | 本地 key 缺失时可能重复打 Sub2API | key 缺失时加用户级 cooldown 和锁 |
| 管理员批量操作缺预算 | 一次批量同步/生成太大可能拖垮接口 | 限最大数量，大任务转后台 |
| 分组/模型同步可重复点 | 反复读取 Sub2API 管理接口 | Redis job lock，前端显示运行中 |
| worker 参数固定 | backlog 恢复时可能太猛或太慢 | batch、interval、并发数做成配置 |

## 用户生命周期模拟

### 1. 新用户打开 App

流程：

```text
打开 Electron App
未登录时不请求用户模型目录
登录后再请求 /api/v1/me、/api/v1/me/groups、/api/v1/provider/official?externalGroupId=xxx
```

| 风险 | 保护 |
| --- | --- |
| 很多人同时打开 App | 静态资源走 Nginx/CDN 缓存，不打 Sub2API |
| 匿名爬模型目录 | 用户模型目录必须登录后才能看 |

期望：

```text
打开 App 很快。
用户登录前不请求 Sub2API。
```

### 2. 用户注册

流程：

```text
POST /api/v1/auth/register
  -> 创建 Brevyn 本地用户
  -> 签发 AT / RT
  -> 短时间尝试创建 Sub2API 影子账号和默认 key
  -> 如果 Sub2API 慢或失败，返回 202 + provisioning 状态
  -> worker 后台继续重试
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 同一用户双击注册 | 两个标签页同时提交同一邮箱 | 邮箱唯一约束 + 事务 | 一个成功，一个返回已注册 |
| 单 IP 大量注册 | 1 分钟 100 次 | Redis IP 限速 | 超限返回 429 |
| 同邮箱重复注册 | 攻击或误点 | Redis 邮箱 hash 限速 | 超限返回 429 或已注册 |
| 大量真实用户同时注册 | 1 分钟 500 个不同用户 | Sub2API provisioning 并发预算 + 队列 | 本地注册成功，部分用户显示配置中 |
| Sub2API 挂了 | 管理 API 超时 | gateway_operations 队列 | 注册仍可完成，后续自动重试 |

建议初始限制：

```text
注册 IP 限制：10 / 10min
注册邮箱 hash 限制：5 / 30min
注册请求体限制：只允许小 JSON
立即同步 Sub2API 超时：2-3 秒
立即 provisioning 全局并发：10
同一用户 provisioning 并发：1
超过预算或失败：写入 gateway_operations
```

### 3. 用户登录

流程：

```text
POST /api/v1/auth/login
  -> 校验邮箱密码
  -> 签发 AT / RT
  -> 登录热路径不请求 Sub2API
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 错误密码爆破 | 一个邮箱被多次试密码 | 邮箱 hash 失败限速 | 返回 429 或延迟 |
| 一个 IP 扫很多邮箱 | 攻击者扫库 | IP 限速 | 返回 429 |
| 很多正常用户同时登录 | 高峰期 | 本地 DB 索引，不调用 Sub2API | 登录保持快 |
| 用户被禁用后登录 | 管理员刚禁用 | 检查本地用户状态 | 拒绝登录 |

建议初始限制：

```text
登录请求 IP 限制：120 / 10min
登录请求 IP+邮箱限制：20 / 10min
登录失败 IP+邮箱限制：5 / 15min
登录失败邮箱限制：12 / 15min
登录失败 IP 限制：60 / 15min
登录成功后清理该 IP+邮箱的失败计数
```

### 4. Access Token 刷新

流程：

```text
POST /api/v1/auth/refresh
  -> 校验 RT
  -> 锁 refresh_tokens 行
  -> 轮换 RT
  -> 如果发现旧 RT 复用，吊销整个 token family
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| App 同时发两个 refresh | 网络重试或多窗口 | `FOR UPDATE` 锁 token 行 | 一个成功，一个安全失败 |
| 旧 RT 被重放 | refresh token 泄露 | reuse detection | 吊销 family，要求重新登录 |
| refresh 死循环 | 客户端 bug | Redis session/user 限速 | 返回 429 |

建议初始限制：

```text
同一 session family：30 / 5min
同一 IP：120 / 10min
```

当前实现：

```text
Refresh 入口先做 Redis 限速，再进入 refresh_tokens FOR UPDATE 事务。
RT 可解析时同时计入 session family；RT 不可解析时仍计入 IP，避免无效 token 攻击打数据库。
超限返回 429 refresh_rate_limited，并带 Retry-After。
```

### 5. App 首页展示

流程：

```text
GET /api/v1/me
GET /api/v1/me/groups
GET /api/v1/me/wallet
GET /api/v1/provider/official?externalGroupId=xxx
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| App 频繁刷新页面 | 用户切换页面 | 只读 Brevyn 本地表 | 不给 Sub2API 压力 |
| 很多用户同时打开 App | 高峰期 | 本地读模型 + 索引 | 响应稳定 |
| 模型目录有点旧 | 管理员还没同步 | 显示本地最新快照 | 页面仍可用 |

原则：

```text
用户读取类接口不要实时请求 Sub2API。
模型、分组、渠道信息由管理员或后台同步到 Brevyn Cloud 本地表。
```

### 6. App 获取官方 Provider 配置

流程：

```text
GET /api/v1/provider/official?externalGroupId=xxx
  -> 校验用户是否拥有该分组；默认组允许注册兜底
  -> provider config 读取限速
  -> 如果本地已有 encrypted key，直接返回
  -> 如果没有，先做 provisioning 限速
  -> 获取 user+group Redis 锁
  -> 短超时创建 Sub2API 账号/key
  -> 锁被占用或上游失败时入队 gateway_operations
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 用户连续点刷新 | 10 次请求 provider config | 用户级 Redis 锁 + 本地缓存 | 不重复创建 key |
| 注册时还没创建成功 | 之前 Sub2API 失败 | 短同步或排队 | 返回配置中或创建一次 |
| 多设备登录 | 桌面和笔记本同时登录 | 设备策略 + key 规则 | 创建可控，不混乱 |
| Sub2API admin 登录失效 | 不能创建 key | 入队重试，返回网关暂不可用 | 用户看到明确状态 |

建议初始限制：

```text
provider config：60 / 10min / user
provider config：180 / 10min / IP
缺 key 时 provisioning：3 / 10min / user+group
Redis 锁：brevyn:lock:provider:provision:user:{userID}:group:{externalGroupID}
锁 TTL：30s
```

当前实现：

```text
正式客户端只调用 /api/v1/provider/conversation 和 /api/v1/provider/official。
旧 /api/v1/api-keys/system 与 /api/v1/models/catalog 用户侧接口已删除。
模型目录只作为内部/admin 数据源，客户端从 provider payload 读取 models。
缺 key 时有 user+group provisioning 限速和 Redis 锁。
```

### 7. 用户兑换卡密

流程：

```text
POST /api/v1/redeem
  -> Redis 兑换限速
  -> 锁 redeem_codes 行
  -> 标记卡密已使用
  -> 创建 redemption
  -> 余额商品写 wallet transaction
  -> 创建 gateway operation
  -> 尝试立即同步 Sub2API
  -> 同步失败则返回已记录，后台重试
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 同一卡密双击兑换 | 两个标签页同时提交 | `FOR UPDATE` + `UNIQUE(redeem_code_id)` | 只有一个成功 |
| 猜卡密 | 大量随机码 | IP / 用户 / 卡密 hash 限速 | 超限 429 |
| 同用户快速兑两个有效码 | 两个不同卡密 | 钱包流水事务 | 余额正确 |
| Sub2API 本地兑换后挂了 | 本地已成功，远程未同步 | gateway_operations | 后台重试，后台可见 |
| worker 重试同一兑换 | 网络超时不知道是否成功 | 幂等键 / 远程引用 | 不重复加钱 |

当前已经做得好的地方：

```text
兑换码行使用 FOR UPDATE。
redemptions 有 UNIQUE(redeem_code_id)。
gateway_operations 有 idempotency_key。
```

生产级要补：

```text
最好给 Sub2API 补 admin grant 的 reference_id/idempotency 支持。
否则远程超时后无法 100% 判断是否已经加过余额，只能靠保守重试和人工核对。
```

### 8. 用户实际使用模型

流程：

```text
客户端 -> https://api.brevyn.org -> Sub2API -> 上游账号
```

| 场景 | 例子 | 并发保护归属 | 期望结果 |
| --- | --- | --- | --- |
| 同用户开很多聊天 | 同一个 key 并发请求 | Sub2API 用户并发 | 排队或 429 |
| 一个上游账号忙 | Kiro/DeepSeek 账号满并发 | Sub2API 账号并发 | 切账号、等待或容量错误 |
| 多用户打同一分组 | 共享号池 | Sub2API 分组/账号池 | 负载感知选择 |
| 流式请求等待槽位 | 用户槽位暂满 | Sub2API 等待 + ping | 保活或 429 |

结论：

```text
Brevyn Cloud 不进入模型请求热路径。
这里不用 Cloud 再做一层并发队列。
```

### 9. 用户切换分组 / 套餐

推荐产品模型：

```text
一个分组 = 一个 gateway API key。
用户切换分组时，客户端选择不同 key。
不要每次切换都去修改 Sub2API 默认分组。
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 用户买了周卡 | 原来有余额组 key，需要套餐组 key | 用户 + 分组级 key 锁 | 只创建一个新 key |
| 用户快速切换分组 | UI 来回点 | 本地状态切换 | 不写 Sub2API |
| 用户请求无权限分组 | 手动改请求参数 | 权限检查 | 返回 403 |
| 套餐过期但 App 没刷新 | key 仍存在 | Sub2API 计费资格检查 | 模型请求失败，Cloud 下次同步显示过期 |

后续接口建议：

```text
GET /api/v1/provider/official?externalGroupId=2
GET /api/v1/api-keys/group?externalGroupId=2
```

### 10. 管理员创建商品和卡密

流程：

```text
POST /api/v1/admin/products
POST /api/v1/admin/redeem-codes/generate
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 生成 1 张卡 | 普通订单 | 事务 | 明文只返回一次 |
| 生成 500 张卡 | 批量上架 | 最大 batch 限制 + 前端 pending 禁用 | 不超时 |
| 两个管理员创建同 SKU | 商品重复 | SKU 唯一或提示 | 商品列表不乱 |
| 联动小铺同订单重复提交 | order_ref 重复 | 可选唯一 order_ref | 防止重复交付 |

建议：

```text
UI 单批最多 500。
超过 500 转后台任务。
卡密明文只在生成后展示/导出一次。
数据库只存 code_hash。
```

### 11. 管理员手动赠送余额 / 套餐

流程：

```text
POST /api/v1/admin/users/:id/grant-balance
POST /api/v1/admin/subscriptions/assign
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 管理员双击赠送 | 同一请求提交两次 | Idempotency-Key | 不重复赠送 |
| Sub2API 挂了 | 远程赠送失败 | gateway_operations | 本地审计可见，后台重试 |
| 用户刚被禁用 | 管理员操作竞态 | 事务里重新检查用户状态 | 拒绝或按策略入队 |

建议：

```text
手动赠送必须填写原因。
写审计日志。
管理员前端发送 Idempotency-Key。
```

### 12. 管理员轮换 / 禁用 API Key

流程：

```text
POST /api/v1/admin/users/:id/api-keys/rotate
POST /api/v1/admin/api-keys/:id/disable
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 连续点击轮换 | 创建多个新 key | 用户/分组锁 + 本地事务 | 最终只有最新 key active |
| 本地禁用成功，远程失败 | Sub2API 不可用 | `disable_api_key_remote` 队列 | 后台可见，worker 重试 |
| 禁用用户 | 需要级联禁用 key | 每个 key 一个幂等操作 | 不遗漏远程 active key |

原则：

```text
本地状态立即生效。
远程状态先同步，失败就进入队列。
```

### 13. 管理员同步 Sub2API 分组和模型

流程：

```text
POST /api/v1/admin/sub2api/sync-groups
POST /api/v1/admin/sub2api/sync-models
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 管理员重复点同步 | 多个同步任务同时跑 | Redis job lock | 第二次返回 already_running |
| 同步时用户读取模型 | 用户打开 App | 本地快照/事务 upsert | 用户看到旧快照或新快照，不半截 |
| Sub2API 渠道很多 | 模型目录大 | 超时、分块、错误可见 | 失败不污染数据 |

建议：

```text
sync-groups 锁 TTL：60s
sync-models 锁 TTL：180s
前端按钮显示“同步中”
```

### 14. Worker 消费 gateway_operations

流程：

```text
worker 每 10s 扫描
  -> 用 FOR UPDATE SKIP LOCKED 抢任务
  -> 调 Sub2API 写操作
  -> 成功标记 succeeded
  -> 失败按错误类型 retry 或 dead_letter
```

| 场景 | 例子 | 必要保护 | 期望结果 |
| --- | --- | --- | --- |
| 多个 worker 同时跑 | 横向扩容 | `FOR UPDATE SKIP LOCKED` | 一个任务只被一个 worker 拿到 |
| Sub2API 挂了产生 backlog | 1000 个失败任务 | 退避 + batch size + provider 并发上限 | 恢复后慢慢消化 |
| 单个任务卡死 | 网络不返回 | 90s 执行超时 | 标记失败可重试 |
| worker 崩溃留下 running | 进程退出 | stale running requeue | 超时后重新入队 |

生产建议：

```text
SUB2API_OPERATION_CONCURRENCY=5
SUB2API_OPERATION_BATCH_SIZE=10
SUB2API_OPERATION_INTERVAL=10s
SUB2API_OPERATION_STALE_TIMEOUT=5m
每轮 worker 抢任务前会回收 locked_at 超过 stale timeout 的 running 操作。
```

## 优先级清单

### P0：真实用户前必须补

| 项目 | 状态 |
| --- | --- |
| 用户注册 Redis 限速 | Done |
| 用户登录 Redis 限速 | Done |
| Sub2API provisioning Redis 用户锁 | Done |
| 注册时 Sub2API 短超时 + 入队状态在 UI 明确展示 | Backend done，UI 状态展示待细化 |
| 注册立即 provisioning 全局并发 10 | Done |
| 缺 key 时获取 provider/key 的 cooldown | Done |
| 分组/模型同步 Redis job lock | Done |
| stale running gateway operation 重新入队 | Done |

### P1：小规模上线后补

| 项目 | 状态 |
| --- | --- |
| worker 并发、batch、interval 配置化 | Done：并发 5、batch 10、interval 10s |
| 管理员赠送支持 Idempotency-Key | Done |
| 大批量卡密生成转后台任务 | Partially done：单批先限制 500，后台任务 Later |
| Sub2API Admin API 增加余额/套餐 grant 幂等引用 | Cloud done：调用侧已传 Idempotency-Key，Sub2API 服务端保证后续再核对 |
| 每分组官方 provider/key 接口 | Done |
| 设备级 key 策略 | Todo |

### P2：后续增强

| 项目 | 状态 |
| --- | --- |
| 如果 gateway_operations 量大，换 asynq 等专门队列 | Later |
| 公开状态页和事故横幅 | Later |
| 滥用分析后台 | Later |
| 按风险分动态限速 | Later |

## 模拟测试用例

仓库内提供了一个轻量 smoke 脚本：

```bash
BASE_URL=http://127.0.0.1:4000 TOTAL=20 CONCURRENCY=10 ./scripts/concurrency_smoke.sh register
ADMIN_EMAIL=admin@example.com TOTAL=12 CONCURRENCY=12 ./scripts/concurrency_smoke.sh admin-login-fail
ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD='...' TOTAL=2 CONCURRENCY=2 ./scripts/concurrency_smoke.sh admin-sync-lock
```

### 场景 A：重复注册

```text
Given test@example.com 还没有注册
When 两个注册请求同时提交
Then 一个返回 201 或 202
And 另一个返回 409
And 本地只存在一个用户
And 最多只创建一个 Sub2API 影子账号
```

### 场景 B：注册洪峰

```text
Given 500 个不同用户在 1 分钟内注册
When Sub2API 正常
Then Brevyn 本地注册成功率保持高
And 立即 provisioning 被限制在预算内
And 超出的用户显示配置中
And worker 逐步完成后台同步
```

### 场景 C：登录攻击

```text
Given 一个 IP 连续提交错误密码
When 超过失败阈值
Then 后续请求返回 429 + Retry-After
And bcrypt CPU 被保护
And 其他正常 IP 不受全局影响
```

### 场景 D：同一卡密双兑换

```text
Given 有一张未使用卡密
When 同一用户从两个标签页同时提交
Then 只有一个请求成功
And 只有一条 redemption
And 余额商品只有一条 wallet transaction
And 只有一个 gateway operation
```

### 场景 E：Sub2API 在兑换时挂掉

```text
Given Sub2API Admin API 不可用
When 用户兑换有效卡密
Then 本地卡密变成 used
And redemption 状态变成 gateway_failed 或 pending_gateway
And gateway operation 可重试
And 用户看到“已记录，正在同步”
And 管理员后台能看到失败并重试
```

### 场景 F：Provider 配置被狂点

```text
Given 用户本地还没有 gateway key
When App 很快请求 provider config 20 次
Then 只有一个 provisioning 过程运行
And 其他请求返回已有 key、配置中状态或 429 cooldown
And Sub2API 不会收到 20 次 create-key
```

### 场景 G：Worker backlog 恢复

```text
Given Sub2API 挂掉期间堆了 1000 个 gateway operation
When Sub2API 恢复
Then worker 按并发上限处理
And 不打爆 Sub2API 管理接口
And 失败任务按退避重试
```

### 场景 H：模型请求高峰

```text
Given 100 个用户同时使用模型
When 他们请求 api.brevyn.org
Then Brevyn Cloud 不接收模型流量
And Sub2API 执行用户/账号并发限制
And 用户看到的是 gateway 的 429 或容量错误，不是 Cloud 崩溃
```

## 产品决策

第一阶段保持这个边界：

```text
Brevyn Cloud 负责账号、商品、兑换、余额、key、同步正确性。
Sub2API 负责模型路由、用户并发、账号并发、token 计费。
```

不要让 App 每个动作都实时请求 Sub2API。用户读数据尽量走 Brevyn Cloud 本地缓存和本地表。只有 provisioning、兑换、赠送、轮换、禁用、管理员同步这些写操作才需要碰 Sub2API。
