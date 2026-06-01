import { Boxes, KeyRound, Network, Route, Server, ShieldCheck } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "../components/PageHeader";
import { StatGrid } from "../components/StatGrid";
import { StatusBadge } from "../components/StatusBadge";
import { getGatewayGroups, getProducts, getSub2APISettings, type GatewayChannel, type GatewayGroup, type GatewayGroupModel } from "../api/client";

function authModeLabel(mode: string) {
  if (mode === "admin_api_key") return "API Key";
  if (mode === "admin_credentials") return "管理员账号";
  return "未配置";
}

function formatPrice(value: number) {
  return (value * 1_000_000).toFixed(2);
}

function pricingLabel(model: GatewayGroupModel) {
  const pricing = model.pricing ?? {};
  if (model.billingMode === "request" && typeof pricing.per_request_price === "number") {
    return `$${pricing.per_request_price.toFixed(4)} / req`;
  }
  if (typeof pricing.input_price === "number" || typeof pricing.output_price === "number") {
    return `输入 $${formatPrice(pricing.input_price ?? 0)} / 输出 $${formatPrice(pricing.output_price ?? 0)} / MTok`;
  }
  return "未配置渠道定价";
}

function statusTone(status: string): "ok" | "warn" | "danger" | "neutral" {
  if (status === "active") return "ok";
  if (status === "disabled" || status === "error") return "danger";
  if (status === "inactive") return "neutral";
  return "warn";
}

function yesNo(value: boolean) {
  return value ? "是" : "否";
}

function groupTypeLabel(group: GatewayGroup) {
  if (group.subscriptionType === "subscription") return "subscription · 订阅限额组";
  return "standard · 余额扣费组";
}

function groupLimitLabel(group: GatewayGroup) {
  const parts = [
    group.dailyLimitUsd ? `日 $${group.dailyLimitUsd}` : "",
    group.weeklyLimitUsd ? `周 $${group.weeklyLimitUsd}` : "",
    group.monthlyLimitUsd ? `月 $${group.monthlyLimitUsd}` : ""
  ].filter(Boolean);
  if (group.subscriptionType === "subscription") {
    return parts.length ? parts.join(" / ") : "订阅未配置限额";
  }
  return parts.length ? parts.join(" / ") : "余额按钱包扣费";
}

function fallbackLabel(value: number | null) {
  return value && value > 0 ? `#${value}` : "无";
}

function accountHealthLabel(group: GatewayGroup) {
  if (group.upstreamAccountCount === 0) return "无账号";
  return `${group.activeSchedulableAccountCount}/${group.upstreamAccountCount} 可调度`;
}

function modelSourceLabel(model: GatewayGroupModel) {
  if (model.sourceType === "channel_pricing") return model.channelName ? `渠道定价: ${model.channelName}` : `渠道定价 #${model.externalChannelId}`;
  if (model.sourceType === "channel_mapping") return model.channelName ? `渠道映射: ${model.channelName}` : `渠道映射 #${model.externalChannelId}`;
  return "账号映射";
}

function mappingCount(channel: GatewayChannel) {
  return Object.values(channel.modelMapping ?? {}).reduce((total, mapping) => total + Object.keys(mapping ?? {}).length, 0);
}

export function GatewayPage() {
  const groups = useQuery({ queryKey: ["admin-gateway-groups"], queryFn: getGatewayGroups });
  const products = useQuery({ queryKey: ["admin-products"], queryFn: getProducts });
  const sub2apiSettings = useQuery({ queryKey: ["admin-sub2api-settings"], queryFn: getSub2APISettings });
  const groupRows = groups.data?.items ?? [];
  const productRows = products.data?.items ?? [];
  const settings = sub2apiSettings.data?.settings;
  const activeGroups = groupRows.filter((group) => group.status === "active");

  return (
    <div className="page-stack">
      <PageHeader eyebrow="Gateway" title="网关" description="Sub2API 语义映射：分组、商品权益、影子账号和设备 Key。" />
      <StatGrid
        stats={[
          { label: "Gateway Groups", value: String(activeGroups.length), delta: "已同步 active groups", tone: "green", icon: Server },
          { label: "Products", value: String(productRows.length), delta: "balance / subscription", tone: "amber", icon: KeyRound },
          { label: "Auth Mode", value: authModeLabel(settings?.authMode ?? "not_configured"), delta: "Sub2API admin", tone: "cyan", icon: Network }
        ]}
      />

      <section className="panel">
        <div className="panel-heading">
          <h3>Sub2API</h3>
          <StatusBadge tone={settings?.authMode && settings.authMode !== "not_configured" ? "ok" : "warn"}>
            {settings?.authMode && settings.authMode !== "not_configured" ? "configured" : "not configured"}
          </StatusBadge>
        </div>
        <dl className="kv-list">
          <dt>Sub2API base URL</dt>
          <dd>{settings?.baseUrl ?? "未配置"}</dd>
          <dt>Admin auth</dt>
          <dd>{authModeLabel(settings?.authMode ?? "not_configured")}</dd>
          <dt>Default group</dt>
          <dd>{settings?.defaultGroupId ? `#${settings.defaultGroupId}` : "自动选择 active 分组"}</dd>
          <dt>Synced groups</dt>
          <dd>{groups.isLoading ? "加载中" : `${activeGroups.length} active / ${groupRows.length} total`}</dd>
          <dt>Group semantics</dt>
          <dd>standard = API Key 默认分组，按用户余额扣费；subscription = 用户订阅分组，按日/周/月 USD 限额控制</dd>
          <dt>Redeem semantics</dt>
          <dd>balance 增加钱包余额；subscription 分配或续期指定 subscription 分组</dd>
        </dl>
      </section>

      <section className="panel">
        <div className="panel-heading">
          <h3>分组</h3>
          <StatusBadge tone="neutral">{String(groups.data?.total ?? 0)}</StatusBadge>
        </div>
        <div className="gateway-group-list">
          <div className="gateway-model-section-heading">
            <div>
              <h4>分组块</h4>
              <p>每个分组独立展示规则、账号、渠道和模型定价状态。</p>
            </div>
            <Boxes size={18} aria-hidden="true" />
          </div>
          {groupRows.map((group) => (
            <section className="gateway-group-card" key={group.id}>
              <div className="gateway-model-group-heading">
                <div>
                  <strong>{group.name}</strong>
                  <span>sub2api group_id #{group.externalGroupId} · sort {group.sortOrder}</span>
                </div>
                <div className="gateway-group-badges">
                  <StatusBadge tone={statusTone(group.status)}>{group.status}</StatusBadge>
                  <StatusBadge tone={group.activeSchedulableAccountCount > 0 ? "ok" : "warn"}>{accountHealthLabel(group)}</StatusBadge>
                  <StatusBadge tone={group.models.length > 0 ? "ok" : "neutral"}>{`${group.models.length} models`}</StatusBadge>
                  {group.unpricedModelCount > 0 ? <StatusBadge tone="warn">{`${group.unpricedModelCount} 未定价`}</StatusBadge> : null}
                </div>
              </div>

              <div className="gateway-summary-grid">
                <div className="gateway-summary-item">
                  <span>权益类型</span>
                  <strong>{groupTypeLabel(group)}</strong>
                  <small>倍率 {group.rateMultiplier} · {groupLimitLabel(group)}</small>
                </div>
                <div className="gateway-summary-item">
                  <span>账号调度</span>
                  <strong>{accountHealthLabel(group)}</strong>
                  <small>{group.requireOauthOnly ? "OAuth only" : "不限登录方式"} · privacy {yesNo(group.requirePrivacySet)}</small>
                </div>
                <div className="gateway-summary-item">
                  <span>渠道绑定</span>
                  <strong>{group.channelCount} 个渠道</strong>
                  <small>{group.modelRoutingEnabled ? "已启用模型路由" : "未启用模型路由"}</small>
                </div>
                <div className="gateway-summary-item">
                  <span>模型定价</span>
                  <strong>{group.pricedModelCount}/{group.models.length} 已定价</strong>
                  <small>{group.unpricedModelCount > 0 ? `${group.unpricedModelCount} 个模型需要补定价` : "定价完整"}</small>
                </div>
              </div>

              <div className="gateway-group-info-grid">
                <section className="gateway-info-section">
                  <h4>基础</h4>
                  <dl className="gateway-mini-kv">
                    <dt>平台</dt>
                    <dd>{group.platform}</dd>
                    <dt>类型</dt>
                    <dd>{groupTypeLabel(group)}</dd>
                    <dt>状态</dt>
                    <dd>{group.status}</dd>
                    <dt>排序</dt>
                    <dd>{group.sortOrder}</dd>
                  </dl>
                </section>
                <section className="gateway-info-section">
                  <h4>计费额度</h4>
                  <dl className="gateway-mini-kv">
                    <dt>倍率</dt>
                    <dd>{group.rateMultiplier}</dd>
                    <dt>订阅限额</dt>
                    <dd>{groupLimitLabel(group)}</dd>
                    <dt>分组默认有效期</dt>
                    <dd>{group.defaultValidityDays} 天</dd>
                    <dt>RPM</dt>
                    <dd>{group.rpmLimit > 0 ? group.rpmLimit : "不限"}</dd>
                  </dl>
                </section>
                <section className="gateway-info-section">
                  <h4>路由限制</h4>
                  <dl className="gateway-mini-kv">
                    <dt>独占</dt>
                    <dd>{yesNo(group.isExclusive)}</dd>
                    <dt>Claude Code</dt>
                    <dd>{yesNo(group.claudeCodeOnly)}</dd>
                    <dt>fallback</dt>
                    <dd>{fallbackLabel(group.fallbackGroupId)}</dd>
                    <dt>invalid fallback</dt>
                    <dd>{fallbackLabel(group.fallbackGroupIdOnInvalidRequest)}</dd>
                  </dl>
                </section>
                <section className="gateway-info-section">
                  <h4>调度约束</h4>
                  <dl className="gateway-mini-kv">
                    <dt>messages</dt>
                    <dd>{yesNo(group.allowMessagesDispatch)}</dd>
                    <dt>OAuth only</dt>
                    <dd>{yesNo(group.requireOauthOnly)}</dd>
                    <dt>privacy</dt>
                    <dd>{yesNo(group.requirePrivacySet)}</dd>
                    <dt>渠道</dt>
                    <dd>{group.channelCount}</dd>
                  </dl>
                </section>
              </div>

              <div className="gateway-account-section">
                <div className="gateway-model-section-heading compact">
                  <div>
                    <h4>分组账号</h4>
                    <p>从 Sub2API 账号绑定读取，用来判断该组是否真的可调度。</p>
                  </div>
                  <ShieldCheck size={17} aria-hidden="true" />
                </div>
                {group.accounts.length > 0 ? (
                  <div className="gateway-account-list">
                    {group.accounts.map((account) => (
                      <div className="gateway-account-row" key={`${group.id}-${account.id}`}>
                        <div className="gateway-account-main">
                          <strong>{account.name}</strong>
                          <code>#{account.externalAccountId}</code>
                        </div>
                        <div className="gateway-model-meta">
                          <span>{account.platform}</span>
                          <span>{account.accountType || "unknown"}</span>
                          <span>并发 {account.currentConcurrency}/{account.concurrency}</span>
                          <span>优先级 {account.priority}</span>
                          <span>倍率 {account.rateMultiplier}</span>
                          <span>{account.mappedModelCount} models</span>
                        </div>
                        <StatusBadge tone={account.status === "active" && account.schedulable ? "ok" : statusTone(account.status)}>
                          {account.status === "active" && account.schedulable ? "schedulable" : account.status}
                        </StatusBadge>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="gateway-model-empty">未同步到账号。请在 Sub2API 分组中绑定账号，然后在设置页同步模型。</p>
                )}
              </div>

              <div className="gateway-channel-section">
                <div className="gateway-model-section-heading compact">
                  <div>
                    <h4>渠道与定价</h4>
                    <p>来自 Sub2API 渠道绑定，用来解释价格来源和模型映射。</p>
                  </div>
                  <Route size={17} aria-hidden="true" />
                </div>
                {group.channels.length > 0 ? (
                  <div className="gateway-channel-list">
                    {group.channels.map((channel) => (
                      <div className="gateway-channel-row" key={`${group.id}-${channel.id}`}>
                        <div className="gateway-account-main">
                          <strong>{channel.name}</strong>
                          <code>#{channel.externalChannelId}</code>
                        </div>
                        <div className="gateway-model-meta">
                          <span>{channel.billingModelSource}</span>
                          <span>{channel.restrictModels ? "限制模型" : "不限制模型"}</span>
                          <span>{mappingCount(channel)} mappings</span>
                          <span>{channel.pricingCount} pricing</span>
                        </div>
                        <StatusBadge tone={channel.pricingCount > 0 ? "ok" : "warn"}>{channel.pricingCount > 0 ? "priced" : "no pricing"}</StatusBadge>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="gateway-model-empty">该分组暂无渠道绑定。模型可以由账号映射提供，但价格可能缺失。</p>
                )}
              </div>

              {group.models.length > 0 ? (
                <div className="gateway-model-section">
                  <div className="gateway-model-section-heading compact">
                    <div>
                      <h4>可用模型</h4>
                      <p>{group.pricedModelCount} 个已定价，{group.unpricedModelCount} 个未定价。</p>
                    </div>
                    <Boxes size={17} aria-hidden="true" />
                  </div>
                  <div className="gateway-model-list">
                    {group.models.map((model) => (
                      <div className="gateway-model-row" key={model.id}>
                        <div className="gateway-model-name">
                          <strong>{model.displayName}</strong>
                          <code>{model.modelId}</code>
                        </div>
                        <div className="gateway-model-meta">
                          <span>{model.platform}</span>
                          <span>{model.billingMode}</span>
                          <span>{modelSourceLabel(model)}</span>
                        </div>
                        <div className="gateway-model-price-stack">
                          <StatusBadge tone={model.pricingStatus === "configured" ? "ok" : "warn"}>
                            {model.pricingStatus === "configured" ? "已定价" : "未定价"}
                          </StatusBadge>
                          <span className="gateway-model-price">{pricingLabel(model)}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <p className="gateway-model-empty">未同步到模型。请确认该分组绑定了可调度账号，且账号里配置了 model_mapping，然后在设置页同步模型。</p>
              )}
            </section>
          ))}
          {!groups.isLoading && groupRows.length === 0 ? <p className="gateway-model-empty">暂无网关分组。</p> : null}
        </div>
      </section>
    </div>
  );
}
