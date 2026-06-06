import { LockKeyhole, Plus, QrCode, RefreshCw, Save, Server, ShieldCheck, Trash2, Wifi } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import {
  disableAdminTOTP,
  enableAdminTOTP,
  getGatewayGroups,
  getAdminTOTPStatus,
  getOfficialCapabilities,
  getSub2APISettings,
  setupAdminTOTP,
  syncSub2APIGroups,
  syncSub2APIModels,
  testSub2APIConnection,
  updateOfficialCapabilities,
  updateSub2APISettings,
  type GatewayGroup,
  type OfficialCapabilityDefinition,
  type Sub2APISettings,
  type Sub2APISettingsInput
} from "../api/client";

type SettingsForm = {
  baseUrl: string;
  adminEmail: string;
  adminPassword: string;
  defaultGroupId: string;
};

function authModeLabel(mode: string) {
  if (mode === "admin_api_key") return "API Key";
  if (mode === "admin_credentials") return "管理员账号";
  return "未配置";
}

function defaultGroupOptionLabel(group: GatewayGroup) {
  return `${group.name} · #${group.externalGroupId} · standard · 余额扣费组`;
}

export function SettingsPage() {
  const settings = useQuery({ queryKey: ["admin-sub2api-settings"], queryFn: getSub2APISettings });

  if (!settings.data?.settings) {
    return (
      <div className="page-stack">
        <PageHeader eyebrow="System" title="设置" description="Sub2API 连接、管理员同步账号、Provider endpoint 和安全开关。" />
        <section className="panel">
          <div className="panel-heading">
            <h3>Sub2API 连接</h3>
            <StatusBadge tone={settings.isError ? "danger" : "neutral"}>{settings.isError ? "failed" : "loading"}</StatusBadge>
          </div>
        </section>
      </div>
    );
  }

  return <SettingsContent initialSettings={settings.data.settings} />;
}

function SettingsContent({ initialSettings }: { initialSettings: Sub2APISettings }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<SettingsForm>({
    baseUrl: initialSettings.baseUrl,
    adminEmail: initialSettings.adminEmail,
    adminPassword: "",
    defaultGroupId: String(initialSettings.defaultGroupId || 0)
  });
  const [notice, setNotice] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [disablePassword, setDisablePassword] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [syncGroupsOpen, setSyncGroupsOpen] = useState(false);
  const [syncModelsOpen, setSyncModelsOpen] = useState(false);
  const gatewayGroups = useQuery({ queryKey: ["admin-gateway-groups"], queryFn: getGatewayGroups });
  const totpStatus = useQuery({ queryKey: ["admin-totp-status"], queryFn: getAdminTOTPStatus });
  const activeStandardGroups = (gatewayGroups.data?.items ?? []).filter((group) => group.status === "active" && group.subscriptionType === "standard");
  const selectedDefaultIsAvailable =
    form.defaultGroupId === "0" || activeStandardGroups.some((group) => String(group.externalGroupId) === form.defaultGroupId);

  const saveSettings = useMutation({
    mutationFn: () => {
      const input: Sub2APISettingsInput = {
        baseUrl: form.baseUrl.trim(),
        adminEmail: form.adminEmail.trim(),
        defaultGroupId: Number(form.defaultGroupId) || 0
      };
      if (form.adminPassword.trim() !== "") {
        input.adminPassword = form.adminPassword.trim();
      }
      return updateSub2APISettings(input);
    },
    onSuccess: async () => {
      setForm((current) => ({ ...current, adminPassword: "" }));
      setNotice("设置已保存");
      await queryClient.invalidateQueries({ queryKey: ["admin-sub2api-settings"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-gateway-groups"] });
    }
  });

  const testConnection = useMutation({
    mutationFn: testSub2APIConnection,
    onSuccess: (result) => {
      setNotice(result.ok ? `连接正常，读取到 ${result.groupCount} 个分组` : `检测失败：${result.error}`);
    }
  });

  const syncGroups = useMutation({
    mutationFn: ({ auditReason }: { auditReason: string }) => syncSub2APIGroups({ auditReason }),
    onSuccess: async (result) => {
      setSyncGroupsOpen(false);
      setNotice(`已同步 ${result.synced} 个分组`);
      await queryClient.invalidateQueries({ queryKey: ["admin-gateway-groups"] });
    }
  });

  const syncModels = useMutation({
    mutationFn: ({ auditReason }: { auditReason: string }) => syncSub2APIModels({ auditReason }),
    onSuccess: async (result) => {
      setSyncModelsOpen(false);
      setNotice(`已同步 ${result.syncedModels} 个分组模型绑定，来自 ${result.syncedAccounts} 个账号 / ${result.syncedChannels} 个渠道`);
      await queryClient.invalidateQueries({ queryKey: ["admin-model-catalog"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-gateway-groups"] });
    }
  });

  const setupTotp = useMutation({
    mutationFn: setupAdminTOTP,
    onSuccess: () => {
      enableTotp.reset();
      setTotpCode("");
      setNotice("已生成二步验证二维码，请扫码后输入 6 位验证码启用。");
    }
  });

  const enableTotp = useMutation({
    mutationFn: () => enableAdminTOTP(totpCode),
    onSuccess: () => {
      setTotpCode("");
      setupTotp.reset();
      setNotice("管理员二步验证已启用");
      queryClient.setQueryData(["admin-totp-status"], { enabled: true });
      void queryClient.invalidateQueries({ queryKey: ["admin-totp-status"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-diagnostics"] });
    }
  });

  const disableTotp = useMutation({
    mutationFn: () => disableAdminTOTP({ password: disablePassword, code: disableCode }),
    onSuccess: () => {
      setDisablePassword("");
      setDisableCode("");
      setNotice("管理员二步验证已关闭");
      queryClient.setQueryData(["admin-totp-status"], { enabled: false });
      void queryClient.invalidateQueries({ queryKey: ["admin-totp-status"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-diagnostics"] });
    }
  });

  const current = initialSettings;
  const testResult = testConnection.data;
  const syncResult = syncGroups.data;
  const isBusy = saveSettings.isPending || testConnection.isPending || syncGroups.isPending || syncModels.isPending;
  const securityBusy = setupTotp.isPending || enableTotp.isPending || disableTotp.isPending;
  const securityError = setupTotp.error || enableTotp.error || disableTotp.error;

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="System"
        title="设置"
        description="Sub2API 连接、管理员同步账号、Provider endpoint 和安全开关。"
        actions={
          <div className="page-actions">
            <button className="secondary-action" disabled={isBusy} onClick={() => testConnection.mutate()} type="button">
              <Wifi size={16} />
              <span>{testConnection.isPending ? "检测中" : "检测连接"}</span>
            </button>
            <button className="primary-action" disabled={isBusy} onClick={() => saveSettings.mutate()} type="button">
              <Save size={16} />
              <span>{saveSettings.isPending ? "保存中" : "保存设置"}</span>
            </button>
          </div>
        }
      />

      <section className="panel">
        <div className="panel-heading">
          <div>
            <h3>管理员二步验证</h3>
            <p className="panel-subtitle">登录后台时要求 Authenticator 6 位动态验证码。</p>
          </div>
          <StatusBadge tone={totpStatus.data?.enabled ? "ok" : "warn"}>
            {totpStatus.data?.enabled ? "enabled" : "not enabled"}
          </StatusBadge>
        </div>
        {totpStatus.data?.enabled ? (
          <div className="form-grid settings-form-grid">
            <div className="field-summary wide-field">
              <span>当前状态</span>
              <strong>已启用。之后管理员登录会先验证密码，再验证 6 位动态码。</strong>
            </div>
            <label>
              管理员密码
              <input
                autoComplete="current-password"
                onChange={(event) => setDisablePassword(event.target.value)}
                placeholder="关闭时需要确认密码"
                type="password"
                value={disablePassword}
              />
            </label>
            <label>
              二步验证码
              <input
                autoComplete="one-time-code"
                inputMode="numeric"
                maxLength={6}
                onChange={(event) => setDisableCode(event.target.value)}
                placeholder="6 位验证码"
                value={disableCode}
              />
            </label>
            <button
              className="danger-action"
              disabled={securityBusy || !disablePassword || disableCode.length < 6}
              onClick={() => disableTotp.mutate()}
              type="button"
            >
              <LockKeyhole size={16} />
              <span>{disableTotp.isPending ? "关闭中" : "关闭二步验证"}</span>
            </button>
          </div>
        ) : (
          <div className="totp-setup-grid">
            <div>
              <button className="primary-action" disabled={securityBusy} onClick={() => setupTotp.mutate()} type="button">
                <QrCode size={16} />
                <span>{setupTotp.isPending ? "生成中" : setupTotp.data ? "重新生成二维码" : "绑定 Authenticator"}</span>
              </button>
              <p className="field-hint">推荐使用 1Password、Google Authenticator、Microsoft Authenticator 或 iCloud Passwords。</p>
            </div>
            {setupTotp.data ? (
              <>
                <div className="totp-qr">
                  <img alt="TOTP QR code" src={setupTotp.data.qrPngDataUrl} />
                </div>
                <div className="form-stack">
                  <div className="secret-box">
                    <span>手动密钥</span>
                    <code>{setupTotp.data.secret}</code>
                  </div>
                  <label>
                    验证码
                    <input
                      autoComplete="one-time-code"
                      inputMode="numeric"
                      maxLength={6}
                      onChange={(event) => setTotpCode(event.target.value)}
                      placeholder="扫码后输入 6 位验证码"
                      value={totpCode}
                    />
                  </label>
                  <button
                    className="primary-action"
                    disabled={securityBusy || totpCode.length < 6}
                    onClick={() => enableTotp.mutate()}
                    type="button"
                  >
                    <ShieldCheck size={16} />
                    <span>{enableTotp.isPending ? "启用中" : "验证并启用"}</span>
                  </button>
                </div>
              </>
            ) : null}
          </div>
        )}
        {securityError ? <p className="inline-error">{securityError.message}</p> : null}
      </section>

      <section className="panel">
        <div className="panel-heading">
          <h3>Sub2API 连接</h3>
          <StatusBadge tone={current?.authMode === "not_configured" ? "warn" : "ok"}>
            {authModeLabel(current?.authMode ?? "not_configured")}
          </StatusBadge>
        </div>
        <div className="form-grid settings-form-grid">
          <label className="wide-field">
            Base URL
            <input
              onChange={(event) => setForm((currentForm) => ({ ...currentForm, baseUrl: event.target.value }))}
              placeholder="http://host.docker.internal:8080"
              value={form.baseUrl}
            />
          </label>
          <label>
            管理员邮箱
            <input
              onChange={(event) => setForm((currentForm) => ({ ...currentForm, adminEmail: event.target.value }))}
              placeholder="admin@brevyn.local"
              value={form.adminEmail}
            />
          </label>
          <label>
            管理员密码
            <input
              onChange={(event) => setForm((currentForm) => ({ ...currentForm, adminPassword: event.target.value }))}
              placeholder={current?.hasAdminPassword ? "已配置，留空不修改" : "未配置"}
              type="password"
              value={form.adminPassword}
            />
          </label>
          <label>
            默认分组
            <select
              onChange={(event) => setForm((currentForm) => ({ ...currentForm, defaultGroupId: event.target.value }))}
              value={form.defaultGroupId}
            >
              <option value="0">自动选择 active standard 分组</option>
              {!selectedDefaultIsAvailable ? (
                <option disabled value={form.defaultGroupId}>
                  当前默认分组不是 active standard，请重新选择
                </option>
              ) : null}
              {activeStandardGroups.map((group) => (
                <option key={group.id} value={group.externalGroupId}>
                  {defaultGroupOptionLabel(group)}
                </option>
              ))}
            </select>
            <span className="field-hint">默认分组只用于余额用户的 API Key，不能选择 subscription 分组。</span>
          </label>
        </div>
      </section>

      <OfficialCapabilitiesPanel />

      <section className="split-grid">
        <div className="panel">
          <div className="panel-heading">
            <h3>当前状态</h3>
            <Server size={18} />
          </div>
          <dl className="kv-list">
            <dt>Base URL</dt>
            <dd>{current?.baseUrl || "-"}</dd>
            <dt>认证</dt>
            <dd>{authModeLabel(current?.authMode ?? "not_configured")}</dd>
            <dt>密码</dt>
            <dd>{current?.hasAdminPassword ? "已配置" : "未配置"}</dd>
            <dt>默认分组</dt>
            <dd>{current?.defaultGroupId ? `#${current.defaultGroupId}` : "自动选择"}</dd>
          </dl>
        </div>

        <div className="panel">
          <div className="panel-heading">
            <h3>同步 Sub2API</h3>
            <ShieldCheck size={18} />
          </div>
          <div className="button-row">
            <button className="secondary-action" disabled={isBusy} onClick={() => setSyncGroupsOpen(true)} type="button">
              <RefreshCw size={16} />
              <span>{syncGroups.isPending ? "同步中" : "同步分组"}</span>
            </button>
            <button className="primary-action" disabled={isBusy} onClick={() => setSyncModelsOpen(true)} type="button">
              <RefreshCw size={16} />
              <span>{syncModels.isPending ? "同步中" : "同步模型"}</span>
            </button>
          </div>
          {notice ? <p className="inline-notice">{notice}</p> : null}
          {saveSettings.error || syncGroups.error || syncModels.error || testConnection.error ? (
            <p className="inline-error">{saveSettings.error?.message || syncGroups.error?.message || syncModels.error?.message || testConnection.error?.message}</p>
          ) : null}
        </div>
      </section>

      {testResult ? (
        <section className="panel">
          <div className="panel-heading">
            <h3>检测结果</h3>
            <StatusBadge tone={testResult.ok ? "ok" : "danger"}>{testResult.status}</StatusBadge>
          </div>
          <dl className="kv-list">
            <dt>Health</dt>
            <dd>{testResult.healthOk ? "ok" : "failed"}</dd>
            <dt>Auth</dt>
            <dd>{testResult.authOk ? "ok" : "failed"}</dd>
            <dt>Groups</dt>
            <dd>{testResult.groupCount}</dd>
            <dt>Latency</dt>
            <dd>{testResult.latencyMs}ms</dd>
          </dl>
        </section>
      ) : null}

      {syncResult?.groups?.length ? (
        <section className="panel">
          <div className="panel-heading">
            <h3>最近同步</h3>
            <StatusBadge tone="ok">{String(syncResult.synced)}</StatusBadge>
          </div>
          <div className="sync-preview-list">
            {syncResult.groups.map((group) => (
              <div className="sync-preview-row" key={group.externalGroupId}>
                <strong>{group.name}</strong>
                <span>#{group.externalGroupId}</span>
                <span>{group.platform}</span>
                <span>{group.subscriptionType}</span>
              </div>
            ))}
          </div>
        </section>
      ) : null}
      <DangerConfirmModal
        open={syncGroupsOpen}
        title="同步 Sub2API 分组"
        description="将读取 Sub2API 当前分组并更新 Brevyn Cloud 的分组映射。已有商品绑定关系会继续按分组 ID 匹配。"
        confirmLabel="确认同步"
        pending={syncGroups.isPending}
        onCancel={() => setSyncGroupsOpen(false)}
        onConfirm={(auditReason) => syncGroups.mutate({ auditReason })}
      />
      <DangerConfirmModal
        open={syncModelsOpen}
        title="同步 Sub2API 模型"
        description="将优先读取 Sub2API 分组绑定的可调度账号和账号模型映射，并用渠道定价补充价格信息。"
        confirmLabel="确认同步"
        pending={syncModels.isPending}
        onCancel={() => setSyncModelsOpen(false)}
        onConfirm={(auditReason) => syncModels.mutate({ auditReason })}
      />
    </div>
  );
}

function OfficialCapabilitiesPanel() {
  const queryClient = useQueryClient();
  const capabilities = useQuery({ queryKey: ["admin-official-capabilities"], queryFn: getOfficialCapabilities });
  const [drafts, setDrafts] = useState<OfficialCapabilityDefinition[]>([]);
  const [auditReason, setAuditReason] = useState("");
  const [notice, setNotice] = useState("");
  const sourceItems = useMemo(() => capabilities.data?.items ?? [], [capabilities.data?.items]);

  useEffect(() => {
    setDrafts(sourceItems.map(cloneCapabilityDefinition));
    setAuditReason("");
  }, [sourceItems]);

  const saveCapabilities = useMutation({
    mutationFn: () => updateOfficialCapabilities({ items: drafts.map(normalizeCapabilityDraft), auditReason }),
    onSuccess: async (result) => {
      setNotice(`已保存 ${result.total} 个官方能力`);
      setAuditReason("");
      await queryClient.invalidateQueries({ queryKey: ["admin-official-capabilities"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-gateway-groups"] });
    }
  });

  const dirty = JSON.stringify(drafts.map(normalizeCapabilityDraft)) !== JSON.stringify(sourceItems.map(normalizeCapabilityDraft));
  const canSave = dirty && auditReason.trim().length > 0 && !saveCapabilities.isPending;

  const updateDraft = (index: number, patch: Partial<OfficialCapabilityDefinition>) => {
    setDrafts((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item));
  };

  const addDraft = () => {
    setDrafts((current) => [
      ...current,
      {
        id: "",
        key: "",
        name: "",
        description: "",
        providerKind: "custom-openai",
        adapterKind: "openai_chat_completions",
        protocol: "openai_compatible",
        modelHintCapabilities: [],
        minClientVersion: "",
        enabled: true,
        sortOrder: (current.length + 1) * 10
      }
    ]);
  };

  const removeDraft = (index: number) => {
    setDrafts((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, enabled: false } : item));
  };

  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h3>官方能力注册</h3>
          <p className="panel-subtitle">定义 Cloud 可以给客户端发放的能力；Gateway 分组页再为每个能力选择模型。</p>
        </div>
        <StatusBadge tone={capabilities.isError ? "danger" : "ok"}>{capabilities.isLoading ? "loading" : `${drafts.filter((item) => item.enabled).length} active`}</StatusBadge>
      </div>
      <div className="form-stack">
        {drafts.map((item, index) => (
          <div className="panel soft-panel" key={`${item.id || "new"}-${index}`}>
            <div className="panel-heading compact">
              <div>
                <h4>{item.name || item.key || "新能力"}</h4>
                <p className="panel-subtitle">{item.enabled ? "启用中" : "已停用；保存后客户端不会再收到这个能力。"}</p>
              </div>
              <label className="toggle-row">
                <input checked={item.enabled} onChange={(event) => updateDraft(index, { enabled: event.target.checked })} type="checkbox" />
                <span>{item.enabled ? "启用" : "停用"}</span>
              </label>
            </div>
            <div className="form-grid settings-form-grid">
              <label>
                Key
                <input
                  onChange={(event) => updateDraft(index, { key: event.target.value })}
                  placeholder="ocr"
                  value={item.key}
                />
              </label>
              <label>
                名称
                <input
                  onChange={(event) => updateDraft(index, { name: event.target.value })}
                  placeholder="文档 OCR"
                  value={item.name}
                />
              </label>
              <label>
                Provider kind
                <input
                  onChange={(event) => updateDraft(index, { providerKind: event.target.value })}
                  placeholder="ocr-custom-openai"
                  value={item.providerKind}
                />
              </label>
              <label>
                Adapter kind
                <input
                  onChange={(event) => updateDraft(index, { adapterKind: event.target.value })}
                  placeholder="openai_chat_completions"
                  value={item.adapterKind}
                />
              </label>
              <label>
                Protocol
                <input
                  onChange={(event) => updateDraft(index, { protocol: event.target.value })}
                  placeholder="openai_compatible"
                  value={item.protocol}
                />
              </label>
              <label>
                最低客户端版本
                <input
                  onChange={(event) => updateDraft(index, { minClientVersion: event.target.value })}
                  placeholder="0.2.8"
                  value={item.minClientVersion}
                />
              </label>
              <label className="wide-field">
                模型提示标签
                <input
                  onChange={(event) => updateDraft(index, { modelHintCapabilities: splitCapabilityHints(event.target.value) })}
                  placeholder="vision_input, ocr"
                  value={item.modelHintCapabilities.join(", ")}
                />
              </label>
              <label className="wide-field">
                说明
                <input
                  onChange={(event) => updateDraft(index, { description: event.target.value })}
                  placeholder="这个能力会在哪里使用"
                  value={item.description}
                />
              </label>
            </div>
            <div className="button-row">
              <button className="secondary-action" onClick={() => removeDraft(index)} type="button">
                <Trash2 size={15} />
                <span>停用</span>
              </button>
            </div>
          </div>
        ))}
        {drafts.length === 0 ? <p className="inline-state">暂无能力定义，先添加 embedding / vision / ocr。</p> : null}
      </div>
      <div className="gateway-official-actions">
        <button className="secondary-action" disabled={saveCapabilities.isPending} onClick={addDraft} type="button">
          <Plus size={15} />
          <span>新增能力</span>
        </button>
        <input
          aria-label="官方能力变更原因"
          onChange={(event) => setAuditReason(event.target.value)}
          placeholder="变更原因"
          value={auditReason}
        />
        <button className="secondary-action" disabled={!dirty || saveCapabilities.isPending} onClick={() => setDrafts(sourceItems.map(cloneCapabilityDefinition))} type="button">
          重置
        </button>
        <button className="primary-action" disabled={!canSave} onClick={() => saveCapabilities.mutate()} type="button">
          <Save size={15} />
          <span>{saveCapabilities.isPending ? "保存中" : "保存能力"}</span>
        </button>
      </div>
      {notice ? <p className="inline-notice">{notice}</p> : null}
      {capabilities.isError || saveCapabilities.isError ? <p className="inline-error">{capabilities.error?.message || saveCapabilities.error?.message}</p> : null}
    </section>
  );
}

function cloneCapabilityDefinition(item: OfficialCapabilityDefinition): OfficialCapabilityDefinition {
  return { ...item, modelHintCapabilities: [...(item.modelHintCapabilities ?? [])] };
}

function normalizeCapabilityDraft(item: OfficialCapabilityDefinition): OfficialCapabilityDefinition {
  return {
    ...item,
    key: item.key.trim().toLowerCase(),
    name: item.name.trim(),
    description: item.description.trim(),
    providerKind: item.providerKind.trim(),
    adapterKind: item.adapterKind.trim(),
    protocol: item.protocol.trim(),
    modelHintCapabilities: splitCapabilityHints(item.modelHintCapabilities.join(",")),
    minClientVersion: item.minClientVersion.trim(),
    sortOrder: Number(item.sortOrder) || 0
  };
}

function splitCapabilityHints(value: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of value.split(",")) {
    const item = raw.trim();
    const key = item.toLowerCase();
    if (!item || seen.has(key)) continue;
    seen.add(key);
    out.push(item);
  }
  return out;
}
