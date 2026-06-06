import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Boxes, ChevronDown, ChevronRight, Database, Eye, KeyRound, Network, Route, Save, Server, ShieldCheck } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PageHeader } from "../components/PageHeader";
import { StatGrid } from "../components/StatGrid";
import { StatusBadge } from "../components/StatusBadge";
import {
  getGatewayGroups,
  getOfficialCapabilities,
  getProducts,
  getSub2APISettings,
  updateGatewayGroupOfficialModels,
  type GatewayChannel,
  type GatewayGroup,
  type GatewayGroupModel,
  type GatewayGroupOfficialModelConfig,
  type GatewayGroupOfficialPurposeConfig,
  type OfficialCapabilityDefinition
} from "../api/client";

function authModeLabel(mode: string) {
  if (mode === "admin_api_key") return "API Key";
  if (mode === "admin_credentials") return "管理员账号";
  return "未配置";
}

function formatPrice(value: number) {
  return (value * 1_000_000).toFixed(2);
}

function numericPrice(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function formatMTokPrice(value: unknown) {
  const numeric = numericPrice(value);
  return numeric === undefined ? "-" : `$${formatPrice(numeric)}`;
}

function formatRequestPrice(value: unknown) {
  const numeric = numericPrice(value);
  return numeric === undefined ? "-" : `$${numeric.toFixed(4)}`;
}

function intervalLabel(interval: NonNullable<GatewayGroupModel["pricing"]["intervals"]>[number]) {
  if (interval.tier_label) return interval.tier_label;
  const min = interval.min_tokens ?? 0;
  const max = interval.max_tokens;
  return max == null ? `${min}+ tokens` : `${min}-${max} tokens`;
}

function intervalHasPrice(interval: NonNullable<GatewayGroupModel["pricing"]["intervals"]>[number]) {
  return numericPrice(interval.input_price) !== undefined ||
    numericPrice(interval.output_price) !== undefined ||
    numericPrice(interval.cache_write_price) !== undefined ||
    numericPrice(interval.cache_read_price) !== undefined ||
    numericPrice(interval.per_request_price) !== undefined;
}

function pricingLabel(model: GatewayGroupModel) {
  const pricing = model.pricing ?? {};
  const intervals = Array.isArray(pricing.intervals) ? pricing.intervals.filter(intervalHasPrice) : [];
  const billingMode = model.billingMode || pricing.billing_mode || "token";
  if (intervals.length > 0) {
    const first = intervals[0];
    const suffix = intervals.length > 1 ? ` 等 ${intervals.length} 档` : "";
    if (billingMode === "per_request" || billingMode === "request" || billingMode === "image") {
      return `${intervalLabel(first)} ${formatRequestPrice(first.per_request_price ?? pricing.per_request_price)} / 次${suffix}`;
    }
    return `${intervalLabel(first)} 输入 ${formatMTokPrice(first.input_price ?? pricing.input_price)} / 输出 ${formatMTokPrice(first.output_price ?? pricing.output_price)} / MTok${suffix}`;
  }
  if ((billingMode === "per_request" || billingMode === "request" || billingMode === "image") && typeof pricing.per_request_price === "number") {
    return `$${pricing.per_request_price.toFixed(4)} / 次`;
  }
  if (typeof pricing.input_price === "number" || typeof pricing.output_price === "number") {
    return `输入 $${formatPrice(pricing.input_price ?? 0)} / 输出 $${formatPrice(pricing.output_price ?? 0)} / MTok`;
  }
  if (typeof pricing.image_output_price === "number") {
    return `图片输出 $${formatPrice(pricing.image_output_price)} / MTok`;
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

function groupTypeShortLabel(group: GatewayGroup) {
  if (group.subscriptionType === "subscription") return "订阅限额";
  return "余额扣费";
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

function groupHasIssue(group: GatewayGroup) {
  return group.status !== "active" || group.upstreamAccountCount === 0 || group.activeSchedulableAccountCount === 0 || group.models.length === 0 || group.unpricedModelCount > 0;
}

function groupIssueLabel(group: GatewayGroup) {
  if (group.status !== "active") return group.status;
  if (group.upstreamAccountCount === 0) return "缺账号";
  if (group.activeSchedulableAccountCount === 0) return "不可调度";
  if (group.models.length === 0) return "缺模型";
  if (group.unpricedModelCount > 0) return `${group.unpricedModelCount} 未定价`;
  return "正常";
}

function modelSourceLabel(model: GatewayGroupModel) {
  if (model.sourceType === "channel_pricing") return model.channelName ? `渠道定价: ${model.channelName}` : `渠道定价 #${model.externalChannelId}`;
  if (model.sourceType === "channel_mapping") return model.channelName ? `渠道映射: ${model.channelName}` : `渠道映射 #${model.externalChannelId}`;
  return "账号映射";
}

function officialPurposeConfiguredLabel(config: GatewayGroupOfficialPurposeConfig) {
  if (config.modelIds.length === 0) return "未配置";
  return `${config.modelIds.length} 个 · 默认 ${config.defaultModelId || config.modelIds[0]}`;
}

function normalizedOfficialConfig(config: GatewayGroupOfficialModelConfig | undefined, definitions: OfficialCapabilityDefinition[]): GatewayGroupOfficialModelConfig {
  const out: GatewayGroupOfficialModelConfig = {};
  const sourceDefinitions = definitions.length > 0 ? definitions : fallbackOfficialCapabilityDefinitions;
  for (const definition of sourceDefinitions) {
    if (!definition.enabled) continue;
    out[definition.key] = normalizedPurposeConfig(config?.[definition.key]);
  }
  return out;
}

function normalizedPurposeConfig(config?: GatewayGroupOfficialPurposeConfig): GatewayGroupOfficialPurposeConfig {
  const seen = new Set<string>();
  const modelIds: string[] = [];
  for (const raw of config?.modelIds ?? []) {
    const modelId = raw.trim();
    if (!modelId || seen.has(modelId.toLowerCase())) continue;
    seen.add(modelId.toLowerCase());
    modelIds.push(modelId);
  }
  const defaultModelId = modelIds.find((modelId) => modelId.toLowerCase() === (config?.defaultModelId ?? "").toLowerCase()) ?? modelIds[0] ?? "";
  return { modelIds, defaultModelId };
}

function officialConfigKey(config: GatewayGroupOfficialModelConfig) {
  return JSON.stringify(Object.fromEntries(Object.entries(config).sort(([left], [right]) => left.localeCompare(right))));
}

function hasCapability(model: GatewayGroupModel, capability: string) {
  return (model.capabilities ?? []).some((item) => item.toLowerCase() === capability.toLowerCase());
}

function modelHintTone(model: GatewayGroupModel, definition: OfficialCapabilityDefinition): "ok" | "warn" | "neutral" {
  if ((definition.modelHintCapabilities ?? []).some((capability) => hasCapability(model, capability))) return "ok";
  return "neutral";
}

function modelHintLabel(model: GatewayGroupModel, definition: OfficialCapabilityDefinition) {
  if ((definition.modelHintCapabilities ?? []).some((capability) => hasCapability(model, capability))) return "建议";
  return "手动判断";
}

const fallbackOfficialCapabilityDefinitions: OfficialCapabilityDefinition[] = [
  {
    id: "fallback-embedding",
    key: "embedding",
    name: "向量检索",
    description: "",
    providerKind: "custom-openai",
    adapterKind: "openai_embedding",
    protocol: "openai_compatible",
    modelHintCapabilities: ["embedding"],
    minClientVersion: "",
    enabled: true,
    sortOrder: 10
  },
  {
    id: "fallback-vision",
    key: "vision",
    name: "视觉识别",
    description: "",
    providerKind: "vision-custom-openai",
    adapterKind: "openai_chat_completions",
    protocol: "openai_compatible",
    modelHintCapabilities: ["vision_input"],
    minClientVersion: "",
    enabled: true,
    sortOrder: 20
  }
];

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
  const groupIdsKey = groupRows.map((group) => group.id).join("|");
  const defaultOpenGroupIds = useMemo(() => {
    const issueGroups = groupRows.filter(groupHasIssue).slice(0, 4).map((group) => group.id);
    if (issueGroups.length > 0) return new Set(issueGroups);
    const firstActive = groupRows.find((group) => group.status === "active") ?? groupRows[0];
    return firstActive ? new Set([firstActive.id]) : new Set<string>();
  }, [groupRows]);
  const initializedGroupKey = useRef("");
  const [openGroupIds, setOpenGroupIds] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!groupIdsKey || initializedGroupKey.current === groupIdsKey) return;
    initializedGroupKey.current = groupIdsKey;
    setOpenGroupIds(defaultOpenGroupIds);
  }, [defaultOpenGroupIds, groupIdsKey]);
  const toggleGroup = (groupId: string) => {
    setOpenGroupIds((current) => {
      const next = new Set(current);
      if (next.has(groupId)) {
        next.delete(groupId);
      } else {
        next.add(groupId);
      }
      return next;
    });
  };
  const expandAllGroups = () => setOpenGroupIds(new Set(groupRows.map((group) => group.id)));
  const collapseAllGroups = () => setOpenGroupIds(new Set());

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
          <div>
            <h3>分组</h3>
            <p className="panel-subtitle">折叠查看每个 Sub2API 分组；展开后再处理官方能力模型、账号、渠道和定价。</p>
          </div>
          <div className="gateway-panel-actions">
            <StatusBadge tone="neutral">{String(groups.data?.total ?? 0)}</StatusBadge>
            <button className="secondary-action compact" type="button" onClick={expandAllGroups} disabled={groupRows.length === 0}>
              全部展开
            </button>
            <button className="secondary-action compact" type="button" onClick={collapseAllGroups} disabled={openGroupIds.size === 0}>
              全部收起
            </button>
          </div>
        </div>
        <div className="gateway-group-list">
          <div className="gateway-model-section-heading">
            <div>
              <h4>分组总览</h4>
              <p>标题行只放关键状态，问题组会优先默认展开。</p>
            </div>
            <Boxes size={18} aria-hidden="true" />
          </div>
          {groupRows.map((group) => (
            <GatewayGroupCard
              group={group}
              isOpen={openGroupIds.has(group.id)}
              key={group.id}
              onToggle={() => toggleGroup(group.id)}
            />
          ))}
          {!groups.isLoading && groupRows.length === 0 ? <p className="gateway-model-empty">暂无网关分组。</p> : null}
        </div>
      </section>
    </div>
  );
}

function GatewayGroupCard({ group, isOpen, onToggle }: { group: GatewayGroup; isOpen: boolean; onToggle: () => void }) {
  const hasIssue = groupHasIssue(group);
  return (
    <section className={`gateway-group-card ${isOpen ? "open" : ""}`} data-tone={hasIssue ? "attention" : "ready"}>
      <button className="gateway-group-toggle" type="button" aria-expanded={isOpen} onClick={onToggle}>
        <span className="gateway-group-title-cell">
          <span className="gateway-chevron" aria-hidden="true">
            {isOpen ? <ChevronDown size={18} /> : <ChevronRight size={18} />}
          </span>
          <span className="gateway-group-title">
            <strong>{group.name}</strong>
            <span>sub2api group_id #{group.externalGroupId} · {group.platform} · sort {group.sortOrder}</span>
          </span>
        </span>

        <span className="gateway-group-compact-metrics">
          <span>
            <strong>{groupTypeShortLabel(group)}</strong>
            <small>倍率 {group.rateMultiplier} · {groupLimitLabel(group)}</small>
          </span>
          <span>
            <strong>{accountHealthLabel(group)}</strong>
            <small>{group.requireOauthOnly ? "OAuth only" : "不限登录"} · privacy {yesNo(group.requirePrivacySet)}</small>
          </span>
          <span>
            <strong>{group.channelCount} 渠道</strong>
            <small>{group.modelRoutingEnabled ? "模型路由开启" : "模型路由关闭"}</small>
          </span>
          <span>
            <strong>{group.pricedModelCount}/{group.models.length} 定价</strong>
            <small>{group.unpricedModelCount > 0 ? `${group.unpricedModelCount} 个缺定价` : "定价完整"}</small>
          </span>
        </span>

        <span className="gateway-group-state-cell">
          <StatusBadge tone={statusTone(group.status)}>{group.status}</StatusBadge>
          <StatusBadge tone={hasIssue ? "warn" : "ok"}>{groupIssueLabel(group)}</StatusBadge>
        </span>
      </button>

      {isOpen ? <GatewayGroupDetails group={group} /> : null}
    </section>
  );
}

function GatewayGroupDetails({ group }: { group: GatewayGroup }) {
  return (
    <div className="gateway-group-body">
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
            <dt>默认有效期</dt>
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

      <OfficialModelConfigPanel group={group} />

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
    </div>
  );
}

function OfficialModelConfigPanel({ group }: { group: GatewayGroup }) {
  const queryClient = useQueryClient();
  const capabilityDefinitions = useQuery({ queryKey: ["admin-official-capabilities"], queryFn: getOfficialCapabilities });
  const activeDefinitions = useMemo(
    () => (capabilityDefinitions.data?.items ?? fallbackOfficialCapabilityDefinitions).filter((item) => item.enabled),
    [capabilityDefinitions.data?.items]
  );
  const activeDefinitionKey = activeDefinitions.map((item) => item.key).join("|");
  const initialConfig = useMemo(() => normalizedOfficialConfig(group.officialModelConfig, activeDefinitions), [activeDefinitionKey, group.officialModelConfig]);
  const [draft, setDraft] = useState<GatewayGroupOfficialModelConfig>(initialConfig);
  const [auditReason, setAuditReason] = useState("");
  useEffect(() => {
    setDraft(initialConfig);
    setAuditReason("");
  }, [initialConfig]);
  const saveConfig = useMutation({
    mutationFn: () => updateGatewayGroupOfficialModels(group.externalGroupId, { capabilities: draft, auditReason }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["admin-gateway-groups"] });
      setAuditReason("");
    }
  });
  const dirty = officialConfigKey(draft) !== officialConfigKey(initialConfig);
  const canSave = dirty && auditReason.trim().length > 0 && !saveConfig.isPending;

  return (
    <div className="gateway-official-section">
      <div className="gateway-model-section-heading compact">
        <div>
          <h4>官方能力模型</h4>
          <p>Cloud 默认和允许范围；客户端仍可在允许模型内选择自己的默认模型。</p>
        </div>
        <ShieldCheck size={17} aria-hidden="true" />
      </div>
      <div className="gateway-official-summary">
        {activeDefinitions.map((definition) => (
          <div key={definition.key}>
            <span>{definition.name}</span>
            <strong>{officialPurposeConfiguredLabel(draft[definition.key] ?? normalizedPurposeConfig())}</strong>
          </div>
        ))}
      </div>
      <div className="gateway-official-grid">
        {activeDefinitions.map((definition) => (
          <OfficialPurposeEditor
            key={definition.key}
            definition={definition}
            icon={definition.key === "embedding" ? <Database size={15} aria-hidden="true" /> : definition.key === "vision" ? <Eye size={15} aria-hidden="true" /> : <ShieldCheck size={15} aria-hidden="true" />}
            models={group.models}
            config={draft[definition.key] ?? normalizedPurposeConfig()}
            onChange={(config) => setDraft((current) => ({ ...current, [definition.key]: config }))}
          />
        ))}
      </div>
      <div className="gateway-official-actions">
        <input
          value={auditReason}
          onChange={(event) => setAuditReason(event.target.value)}
          placeholder="变更原因"
          aria-label="官方能力模型变更原因"
        />
        <button className="secondary-action" type="button" disabled={!dirty || saveConfig.isPending} onClick={() => setDraft(initialConfig)}>
          重置
        </button>
        <button className="primary-action" type="button" disabled={!canSave} onClick={() => saveConfig.mutate()}>
          <Save size={15} aria-hidden="true" />
          {saveConfig.isPending ? "保存中" : "保存配置"}
        </button>
      </div>
      {saveConfig.isError ? <p className="inline-error">{saveConfig.error.message}</p> : null}
      {saveConfig.isSuccess && !dirty ? <p className="form-success">官方能力模型配置已保存。</p> : null}
    </div>
  );
}

function OfficialPurposeEditor({
  definition,
  icon,
  models,
  config,
  onChange
}: {
  definition: OfficialCapabilityDefinition;
  icon: ReactNode;
  models: GatewayGroupModel[];
  config: GatewayGroupOfficialPurposeConfig;
  onChange: (config: GatewayGroupOfficialPurposeConfig) => void;
}) {
  const selected = new Set(config.modelIds.map((modelId) => modelId.toLowerCase()));
  const updateSelection = (modelId: string, enabled: boolean) => {
    const nextIds = enabled
      ? [...config.modelIds, modelId]
      : config.modelIds.filter((item) => item.toLowerCase() !== modelId.toLowerCase());
    const next = normalizedPurposeConfig({
      modelIds: nextIds,
      defaultModelId: enabled ? config.defaultModelId || modelId : config.defaultModelId
    });
    onChange(next);
  };
  const setDefault = (modelId: string) => {
    if (!selected.has(modelId.toLowerCase())) {
      onChange(normalizedPurposeConfig({ modelIds: [...config.modelIds, modelId], defaultModelId: modelId }));
      return;
    }
    onChange(normalizedPurposeConfig({ ...config, defaultModelId: modelId }));
  };

  return (
    <section className="gateway-official-purpose">
      <div className="gateway-official-purpose-heading">
        <div>
          {icon}
          <strong>{definition.name}</strong>
        </div>
        <StatusBadge tone={config.modelIds.length > 0 ? "ok" : "neutral"}>{`${config.modelIds.length} models`}</StatusBadge>
      </div>
      <label className="gateway-official-default">
        <span>默认模型</span>
        <select value={config.defaultModelId} onChange={(event) => setDefault(event.target.value)} disabled={config.modelIds.length === 0}>
          {config.modelIds.length === 0 ? <option value="">未选择模型</option> : null}
          {config.modelIds.map((modelId) => (
            <option key={modelId} value={modelId}>
              {modelId}
            </option>
          ))}
        </select>
      </label>
      <div className="gateway-official-models">
        {models.map((model) => {
          const checked = selected.has(model.modelId.toLowerCase());
          return (
            <label className="gateway-official-model-row" key={`${definition.key}-${model.id}`}>
              <input
                type="checkbox"
                checked={checked}
                onChange={(event) => updateSelection(model.modelId, event.target.checked)}
              />
              <span>
                <strong>{model.displayName}</strong>
                <code>{model.modelId}</code>
              </span>
              <StatusBadge tone={modelHintTone(model, definition)}>
                {modelHintLabel(model, definition)}
              </StatusBadge>
              <button
                type="button"
                className="text-chip-button"
                disabled={!checked || config.defaultModelId.toLowerCase() === model.modelId.toLowerCase()}
                onClick={(event) => {
                  event.preventDefault();
                  setDefault(model.modelId);
                }}
              >
                默认
              </button>
            </label>
          );
        })}
        {models.length === 0 ? <p className="gateway-model-empty">该分组暂无模型，先同步 Sub2API 模型后再配置。</p> : null}
      </div>
    </section>
  );
}
