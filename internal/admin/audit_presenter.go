package admin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func decorateAuditLog(item AuditLogItem) AuditLogItem {
	meta := parseAuditMetadata(item.Metadata)
	item.ActionLabel = auditActionLabel(item.Action)
	item.ResultTone = "ok"
	item.Summary = auditSummary(item, meta) + auditReasonSuffix(meta)

	if hasAuditWarning(item, meta) {
		item.ResultTone = "warn"
	}
	if hasAuditFailure(item, meta) {
		item.ResultTone = "danger"
	}
	return item
}

func auditActionLabel(action string) string {
	switch action {
	case "admin.login":
		return "管理员登录"
	case "admin.totp.enable":
		return "启用二步验证"
	case "admin.totp.disable":
		return "关闭二步验证"
	case "user.balance_grant":
		return "赠送余额"
	case "user.create":
		return "创建用户"
	case "user.import_sub2api":
		return "导入 Sub2API 用户"
	case "user.local_update":
		return "更新用户状态"
	case "user.local_delete":
		return "删除用户"
	case "user.gateway_group_change":
		return "变更用户分组"
	case "user.concurrency_update":
		return "更新用户并发"
	case "gateway_api_key.rotate":
		return "轮换 API Key"
	case "gateway_api_key.disable":
		return "禁用 API Key"
	case "redemption.retry_sync":
		return "重试兑换同步"
	case "subscription.assign":
		return "分配订阅"
	case "subscription.extend":
		return "调整订阅天数"
	case "subscription.reset_quota":
		return "重置订阅额度"
	case "subscription.revoke":
		return "撤销订阅"
	case "gateway_operation.retry":
		return "重新入队任务"
	case "gateway_operation.retry_failed":
		return "批量重试失败任务"
	case "redeem_codes.generate":
		return "生成卡密"
	case "redeem_batch.disable":
		return "作废批次"
	case "redeem_code.disable":
		return "作废卡密"
	case "product.create":
		return "创建商品"
	case "product.update":
		return "更新商品"
	case "product.archive":
		return "归档商品"
	case "sub2api.users.sync":
		return "同步 Sub2API 用户"
	case "sub2api.groups.sync":
		return "同步 Sub2API 分组"
	case "sub2api.models.sync":
		return "同步 Sub2API 模型"
	case "sub2api.settings.update":
		return "更新 Sub2API 设置"
	default:
		return action
	}
}

func auditSummary(item AuditLogItem, meta map[string]any) string {
	target := auditTarget(item)
	switch item.Action {
	case "admin.login":
		return fmt.Sprintf("%s 登录管理后台", item.ActorLabel)
	case "admin.totp.enable":
		return fmt.Sprintf("%s 启用管理员二步验证", item.ActorLabel)
	case "admin.totp.disable":
		return fmt.Sprintf("%s 关闭管理员二步验证", item.ActorLabel)
	case "user.balance_grant":
		return fmt.Sprintf("给 %s 赠送 %s，赠送后余额 %s%s",
			target,
			auditUSD(auditFloat(meta, "amount")),
			auditUSD(auditFloat(meta, "balance_after")),
			auditWarningSuffix(meta),
		)
	case "user.create":
		return fmt.Sprintf("创建用户 %s，邮箱 %s，同步 Sub2API：%s%s",
			target,
			auditStringAny(meta, "email"),
			auditStringAny(meta, "sync_sub2api"),
			auditWarningSuffix(meta),
		)
	case "user.import_sub2api":
		return fmt.Sprintf("导入 Sub2API 用户 %s，远端用户 #%s，本地新建：%s，余额修正：%s",
			target,
			auditStringAny(meta, "external_user_id"),
			auditStringAny(meta, "created"),
			auditStringAny(meta, "balance_adjusted"),
		)
	case "user.local_update":
		status := auditString(meta, "status")
		if status == "" {
			status = "updated"
		}
		cascade := "否"
		if auditBool(meta, "cascade_disable_keys") {
			cascade = "是"
		}
		return fmt.Sprintf("将 %s 状态改为 %s，级联禁用 Key：%s%s", target, status, cascade, auditWarningSuffix(meta))
	case "user.local_delete":
		email := auditString(meta, "email")
		if email == "" {
			email = target
		}
		return fmt.Sprintf("删除本地用户 %s，Sub2API 账号不会自动删除", email)
	case "user.gateway_group_change":
		return fmt.Sprintf("将 %s 默认分组改为 #%s，禁用旧 Key %s 个%s",
			target,
			auditStringAny(meta, "external_group_id"),
			auditStringAny(meta, "disabled_old_keys"),
			auditWarningsSuffix(meta),
		)
	case "user.concurrency_update":
		return fmt.Sprintf("将 %s 并发数改为 %s，分组 #%s%s",
			target,
			auditStringAny(meta, "concurrency"),
			auditStringAny(meta, "external_group_id"),
			auditWarningSuffix(meta),
		)
	case "gateway_api_key.rotate":
		return fmt.Sprintf("为 %s 轮换分组 %s 的 API Key，已禁用旧 Key %s 个%s",
			target,
			auditStringAny(meta, "external_group_id"),
			auditStringAny(meta, "disabled_old_keys"),
			auditWarningsSuffix(meta),
		)
	case "gateway_api_key.disable":
		remoteSync := auditString(meta, "remote_sync")
		if remoteSync == "" {
			remoteSync = "skipped"
		}
		return fmt.Sprintf("禁用 %s，远端同步：%s%s", target, remoteSync, auditWarningSuffix(meta))
	case "redemption.retry_sync":
		return fmt.Sprintf("重试同步 %s，类型 %s，Sub2API 用户 #%s，分组 %s",
			target,
			auditString(meta, "kind"),
			auditStringAny(meta, "external_user_id"),
			auditStringAny(meta, "external_group_id"),
		)
	case "subscription.assign":
		return fmt.Sprintf("给 Sub2API 用户 #%s 分配分组 #%s 订阅 %s 天",
			auditStringAny(meta, "external_user_id"),
			auditStringAny(meta, "external_group_id"),
			auditStringAny(meta, "validity_days"),
		)
	case "subscription.extend":
		return fmt.Sprintf("调整 %s 天数 %+g 天，Sub2API 用户 #%s，分组 #%s，到期 %s",
			target,
			auditFloat(meta, "days"),
			auditStringAny(meta, "external_user_id"),
			auditStringAny(meta, "external_group_id"),
			auditString(meta, "expires_at"),
		)
	case "subscription.reset_quota":
		return fmt.Sprintf("重置 %s 的%s额度，Sub2API 用户 #%s，分组 #%s",
			target,
			auditResetQuotaScope(meta),
			auditStringAny(meta, "external_user_id"),
			auditStringAny(meta, "external_group_id"),
		)
	case "subscription.revoke":
		return fmt.Sprintf("撤销 %s", target)
	case "gateway_operation.retry":
		return fmt.Sprintf("将 %s 重新入队，任务 %s，当前尝试 %s/%s",
			target,
			auditString(meta, "operation"),
			auditStringAny(meta, "attempts"),
			auditStringAny(meta, "max_attempts"),
		)
	case "gateway_operation.retry_failed":
		return fmt.Sprintf("批量重新入队 %s 个可重试失败任务", auditStringAny(meta, "count"))
	case "redeem_codes.generate":
		return fmt.Sprintf("为 %s 生成 %s 张兑换码", target, auditStringAny(meta, "count"))
	case "redeem_batch.disable":
		return fmt.Sprintf("作废批次 %s 的 %s 张未使用卡密，来源 %s，订单 %s",
			auditStringAny(meta, "batch_name"),
			auditStringAny(meta, "disabled_codes"),
			auditStringAny(meta, "source"),
			auditStringAny(meta, "order_ref"),
		)
	case "redeem_code.disable":
		return fmt.Sprintf("作废卡密前缀 %s，商品 %s，批次 %s",
			auditStringAny(meta, "code_prefix"),
			auditStringAny(meta, "product_name"),
			auditStringAny(meta, "batch_name"),
		)
	case "sub2api.users.sync":
		return fmt.Sprintf("扫描 %s 个 Sub2API 用户，匹配 %s 个，跳过 %s 个，修正余额 %s 个",
			auditStringAny(meta, "seen"),
			auditStringAny(meta, "synced"),
			auditStringAny(meta, "skipped"),
			auditStringAny(meta, "balance_adjusted"),
		)
	case "sub2api.groups.sync":
		return fmt.Sprintf("同步 %s / %s 个 Sub2API 分组", auditStringAny(meta, "synced"), auditStringAny(meta, "total"))
	case "sub2api.models.sync":
		return fmt.Sprintf("同步 %s 个模型，来源 %s 个渠道 / %s 个分组",
			auditStringAny(meta, "synced_models"),
			auditStringAny(meta, "synced_channels"),
			auditStringAny(meta, "synced_groups"),
		)
	case "sub2api.settings.update":
		return "更新 Sub2API 连接地址或管理员凭据"
	case "product.create":
		return fmt.Sprintf("创建商品 %s", target)
	case "product.update":
		return fmt.Sprintf("更新商品 %s", target)
	case "product.archive":
		return fmt.Sprintf("归档商品 %s", target)
	default:
		if item.Metadata != "" && item.Metadata != "{}" {
			return item.Metadata
		}
		return fmt.Sprintf("%s %s", item.Action, target)
	}
}

func auditTarget(item AuditLogItem) string {
	if item.TargetID == "" {
		return item.TargetType
	}
	if item.TargetType == "" {
		return item.TargetID
	}
	return item.TargetType + ":" + item.TargetID
}

func hasAuditWarning(item AuditLogItem, meta map[string]any) bool {
	warning := strings.TrimSpace(auditString(meta, "sync_warning"))
	warnings := strings.TrimSpace(auditString(meta, "warnings"))
	if warning != "" || warnings != "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.Summary), "warning") || strings.Contains(strings.ToLower(item.Summary), "failed")
}

func hasAuditFailure(item AuditLogItem, meta map[string]any) bool {
	return auditString(meta, "remote_sync") == "failed"
}

func auditWarningSuffix(meta map[string]any) string {
	warning := strings.TrimSpace(auditString(meta, "sync_warning"))
	if warning == "" {
		return ""
	}
	return "；警告：" + warning
}

func auditWarningsSuffix(meta map[string]any) string {
	warning := strings.TrimSpace(auditString(meta, "warnings"))
	if warning == "" {
		return ""
	}
	return "；警告：" + warning
}

func auditReasonSuffix(meta map[string]any) string {
	reason := strings.TrimSpace(auditString(meta, "reason"))
	if reason == "" {
		return ""
	}
	return "；原因：" + reason
}

func parseAuditMetadata(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func auditString(meta map[string]any, key string) string {
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

func auditStringAny(meta map[string]any, key string) string {
	value := strings.TrimSpace(auditString(meta, key))
	if value == "" {
		return "-"
	}
	return value
}

func auditFloat(meta map[string]any, key string) float64 {
	value, ok := meta[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	default:
		return 0
	}
}

func auditBool(meta map[string]any, key string) bool {
	value, ok := meta[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return false
	}
}

func auditResetQuotaScope(meta map[string]any) string {
	scopes := []string{}
	if auditBool(meta, "daily") {
		scopes = append(scopes, "日")
	}
	if auditBool(meta, "weekly") {
		scopes = append(scopes, "周")
	}
	if auditBool(meta, "monthly") {
		scopes = append(scopes, "月")
	}
	if len(scopes) == 0 {
		return "选定"
	}
	return strings.Join(scopes, "/")
}

func auditUSD(value float64) string {
	return "$" + strconv.FormatFloat(value, 'f', 2, 64)
}
