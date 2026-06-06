import {
  Archive,
  CalendarClock,
  ChevronDown,
  DatabaseBackup,
  Download,
  LockKeyhole,
  Plus,
  QrCode,
  RefreshCw,
  RotateCcw,
  Save,
  Server,
  ShieldCheck,
  Trash2,
  Wifi,
  X
} from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import {
  disableAdminTOTP,
  createBackup,
  deleteBackup,
  downloadBackup,
  enableAdminTOTP,
  getBackupConfig,
  getBackupS3Config,
  getBackupSchedule,
  getGatewayGroups,
  getAdminTOTPStatus,
  getOfficialCapabilities,
  getSub2APISettings,
  listBackups,
  restoreBackup,
  setupAdminTOTP,
  syncSub2APIGroups,
  syncSub2APIModels,
  testBackupS3Config,
  testSub2APIConnection,
  updateOfficialCapabilities,
  updateBackupS3Config,
  updateBackupSchedule,
  updateSub2APISettings,
  type BackupRecord,
  type BackupS3Config,
  type BackupScheduleConfig,
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

type SettingsSection = "connection" | "capabilities" | "security" | "backup" | "sync";

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
  const [activeSection, setActiveSection] = useState<SettingsSection>("connection");
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

      <div className="settings-layout">
        <aside className="settings-sidebar" aria-label="设置导航">
          <button className={activeSection === "connection" ? "active" : ""} onClick={() => setActiveSection("connection")} type="button">
            <Wifi size={16} />
            <span>连接</span>
          </button>
          <button className={activeSection === "capabilities" ? "active" : ""} onClick={() => setActiveSection("capabilities")} type="button">
            <Server size={16} />
            <span>官方能力</span>
          </button>
          <button className={activeSection === "security" ? "active" : ""} onClick={() => setActiveSection("security")} type="button">
            <ShieldCheck size={16} />
            <span>安全</span>
          </button>
          <button className={activeSection === "backup" ? "active" : ""} onClick={() => setActiveSection("backup")} type="button">
            <DatabaseBackup size={16} />
            <span>备份</span>
          </button>
          <button className={activeSection === "sync" ? "active" : ""} onClick={() => setActiveSection("sync")} type="button">
            <RefreshCw size={16} />
            <span>同步状态</span>
          </button>
        </aside>

        <main className="settings-content-pane">
          {notice ? <p className="inline-notice">{notice}</p> : null}
          {saveSettings.error || syncGroups.error || syncModels.error || testConnection.error ? (
            <p className="inline-error">{saveSettings.error?.message || syncGroups.error?.message || syncModels.error?.message || testConnection.error?.message}</p>
          ) : null}

          {activeSection === "connection" ? (
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
          ) : null}

          {activeSection === "capabilities" ? <OfficialCapabilitiesPanel /> : null}

          {activeSection === "security" ? (
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
          ) : null}

          {activeSection === "backup" ? <BackupCenterPanel /> : null}

          {activeSection === "sync" ? (
            <div className="form-stack">
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
            </div>
          ) : null}
        </main>
      </div>
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
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);
  const [auditReason, setAuditReason] = useState("");
  const [notice, setNotice] = useState("");
  const sourceItems = useMemo(() => capabilities.data?.items ?? [], [capabilities.data?.items]);
  const sourceActiveItems = useMemo(() => sourceItems.filter((item) => item.enabled).map(cloneCapabilityDefinition), [sourceItems]);

  useEffect(() => {
    setDrafts(sourceActiveItems.map(cloneCapabilityDefinition));
    setExpandedIndex(null);
    setAuditReason("");
  }, [sourceActiveItems]);

  const saveCapabilities = useMutation({
    mutationFn: () => updateOfficialCapabilities({ items: drafts.map(normalizeCapabilityDraft), auditReason }),
    onSuccess: async (result) => {
      setNotice(`已保存 ${result.total} 个官方能力`);
      setAuditReason("");
      await queryClient.invalidateQueries({ queryKey: ["admin-official-capabilities"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-gateway-groups"] });
    }
  });

  const dirty = JSON.stringify(drafts.map(normalizeCapabilityDraft)) !== JSON.stringify(sourceActiveItems.map(normalizeCapabilityDraft));
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
    setExpandedIndex(drafts.length);
  };

  const removeDraft = (index: number) => {
    setDrafts((current) => current.filter((_item, itemIndex) => itemIndex !== index));
    setExpandedIndex((current) => {
      if (current === null) return null;
      if (current === index) return null;
      return current > index ? current - 1 : current;
    });
  };

  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h3>官方能力注册</h3>
          <p className="panel-subtitle">定义 Cloud 可以给客户端发放的能力；Gateway 分组页再为每个能力选择模型。</p>
        </div>
        <StatusBadge tone={capabilities.isError ? "danger" : "ok"}>{capabilities.isLoading ? "loading" : `${drafts.length} active`}</StatusBadge>
      </div>
      <div className="official-capability-list">
        {drafts.map((item, index) => {
          const expanded = expandedIndex === index;
          return (
            <article className={`official-capability-card${expanded ? " expanded" : ""}`} key={`${item.id || "new"}-${index}`}>
              <button className="official-capability-card-head" onClick={() => setExpandedIndex(expanded ? null : index)} type="button">
                <span className="official-capability-icon">{(item.key || item.name || "?").slice(0, 2).toUpperCase()}</span>
                <span className="official-capability-title">
                  <strong>{item.name || item.key || "新能力"}</strong>
                  <small>{item.key || "未设置 key"} · {item.protocol || "protocol"} · {item.adapterKind || "adapter"}</small>
                </span>
                <span className="official-capability-pills">
                  {item.modelHintCapabilities.slice(0, 3).map((hint) => <code key={hint}>{hint}</code>)}
                  {item.minClientVersion ? <code>{item.minClientVersion}+</code> : null}
                </span>
                <ChevronDown className="official-capability-chevron" size={16} />
              </button>
              <div className="official-capability-actions">
                <button className="secondary-action compact" onClick={() => removeDraft(index)} type="button">
                  <Trash2 size={14} />
                  <span>删除</span>
                </button>
              </div>
              {expanded ? (
                <div className="official-capability-editor">
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
                        placeholder="ocr-mineru-gitee"
                        value={item.providerKind}
                      />
                    </label>
                    <label>
                      Adapter kind
                      <input
                        onChange={(event) => updateDraft(index, { adapterKind: event.target.value })}
                        placeholder="mineru_document_parse"
                        value={item.adapterKind}
                      />
                    </label>
                    <label>
                      Protocol
                      <input
                        onChange={(event) => updateDraft(index, { protocol: event.target.value })}
                        placeholder="mineru_async_parse"
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
                        placeholder="document_parse, ocr, table, formula"
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
                </div>
              ) : null}
            </article>
          );
        })}
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
        <button
          className="secondary-action"
          disabled={!dirty || saveCapabilities.isPending}
          onClick={() => {
            setDrafts(sourceActiveItems.map(cloneCapabilityDefinition));
            setExpandedIndex(null);
          }}
          type="button"
        >
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

function BackupCenterPanel() {
  const queryClient = useQueryClient();
  const runtime = useQuery({ queryKey: ["admin-backup-config"], queryFn: getBackupConfig });
  const s3Config = useQuery({ queryKey: ["admin-backup-s3-config"], queryFn: getBackupS3Config });
  const schedule = useQuery({ queryKey: ["admin-backup-schedule"], queryFn: getBackupSchedule });
  const backups = useQuery({
    queryKey: ["admin-backups"],
    queryFn: () => listBackups(100),
    refetchInterval: (query) => {
      const items = query.state.data?.items ?? [];
      return items.some((item) => item.status === "running" || item.restoreStatus === "running") ? 3000 : false;
    }
  });

  const [s3Draft, setS3Draft] = useState<BackupS3Config>({
    endpoint: "",
    region: "auto",
    bucket: "",
    accessKeyId: "",
    secretAccessKey: "",
    prefix: "cloud-backups",
    forcePathStyle: false,
    secretConfigured: false,
    storageConfigured: false
  });
  const [scheduleDraft, setScheduleDraft] = useState<BackupScheduleConfig>({
    enabled: false,
    cronExpr: "0 3 * * *",
    retainDays: 14,
    retainCount: 0
  });
  const [s3Reason, setS3Reason] = useState("");
  const [scheduleReason, setScheduleReason] = useState("");
  const [notice, setNotice] = useState("");
  const [restoreTarget, setRestoreTarget] = useState<BackupRecord | null>(null);
  const [restorePassword, setRestorePassword] = useState("");
  const [restoreConfirm, setRestoreConfirm] = useState("");
  const [restoreReason, setRestoreReason] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<BackupRecord | null>(null);

  useEffect(() => {
    if (!s3Config.data?.config) return;
    setS3Draft({ ...s3Config.data.config, secretAccessKey: "" });
  }, [s3Config.data?.config]);

  useEffect(() => {
    if (!schedule.data?.schedule) return;
    setScheduleDraft(schedule.data.schedule);
  }, [schedule.data?.schedule]);

  const invalidateBackups = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["admin-backup-config"] }),
      queryClient.invalidateQueries({ queryKey: ["admin-backup-s3-config"] }),
      queryClient.invalidateQueries({ queryKey: ["admin-backup-schedule"] }),
      queryClient.invalidateQueries({ queryKey: ["admin-backups"] })
    ]);
  };

  const saveS3 = useMutation({
    mutationFn: () => updateBackupS3Config({ ...s3Draft, auditReason: s3Reason }),
    onSuccess: async () => {
      setNotice("对象存储配置已保存");
      setS3Reason("");
      await invalidateBackups();
    }
  });

  const testS3 = useMutation({
    mutationFn: () => testBackupS3Config(s3Draft),
    onSuccess: (result) => {
      setNotice(result.ok ? "对象存储连接正常" : `对象存储检测失败：${translateBackupError(result.message)}`);
    }
  });

  const saveSchedule = useMutation({
    mutationFn: () => updateBackupSchedule({ ...scheduleDraft, auditReason: scheduleReason }),
    onSuccess: async () => {
      setNotice(scheduleDraft.enabled ? "定时备份已启用" : "定时备份已关闭");
      setScheduleReason("");
      await invalidateBackups();
    }
  });

  const createManualBackup = useMutation({
    mutationFn: () => createBackup({ expireDays: scheduleDraft.retainDays || runtime.data?.config.retentionDays || 14 }),
    onSuccess: async () => {
      setNotice("备份任务已开始");
      await queryClient.invalidateQueries({ queryKey: ["admin-backups"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-backup-config"] });
    }
  });

  const download = useMutation({
    mutationFn: (record: BackupRecord) => downloadBackup(record.id, record.fileName),
    onError: (error) => setNotice(`下载失败：${error.message}`)
  });

  const remove = useMutation({
    mutationFn: ({ record, auditReason }: { record: BackupRecord; auditReason: string }) => deleteBackup(record.id, { auditReason }),
    onSuccess: async () => {
      setDeleteTarget(null);
      setNotice("备份记录已删除");
      await queryClient.invalidateQueries({ queryKey: ["admin-backups"] });
    }
  });

  const restore = useMutation({
    mutationFn: () =>
      restoreBackup(restoreTarget?.id ?? "", {
        password: restorePassword,
        confirmation: restoreConfirm,
        auditReason: restoreReason
      }),
    onSuccess: async () => {
      setRestoreTarget(null);
      setRestorePassword("");
      setRestoreConfirm("");
      setRestoreReason("");
      setNotice("恢复任务已开始，系统会先自动创建一份恢复前备份。");
      await queryClient.invalidateQueries({ queryKey: ["admin-backups"] });
    }
  });

  const busy =
    saveS3.isPending ||
    testS3.isPending ||
    saveSchedule.isPending ||
    createManualBackup.isPending ||
    download.isPending ||
    remove.isPending ||
    restore.isPending;
  const backupItems = backups.data?.items ?? [];
  const config = runtime.data?.config;
  const activeJob = backupItems.some((item) => item.status === "running" || item.restoreStatus === "running");

  return (
    <section className="panel backup-center">
      <div className="panel-heading">
        <div>
          <h3>Cloud 备份中心</h3>
          <p className="panel-subtitle">备份 Cloud 自己的 PostgreSQL 数据；Sub2API 仍使用它自己的数据管理。</p>
        </div>
        <StatusBadge tone={activeJob ? "warn" : "ok"}>{activeJob ? "running" : "ready"}</StatusBadge>
      </div>

      <div className="backup-stepper">
        <div>
          <span>1</span>
          <strong>配置存储</strong>
        </div>
        <div>
          <span>2</span>
          <strong>定时策略</strong>
        </div>
        <div>
          <span>3</span>
          <strong>备份与恢复</strong>
        </div>
      </div>

      <div className="backup-config-strip">
        <div>
          <span>本地目录</span>
          <strong>{config?.backupDir || "-"}</strong>
        </div>
        <div>
          <span>对象存储</span>
          <strong>{config?.s3Configured ? "已配置" : "未配置，仅本地备份"}</strong>
        </div>
        <div>
          <span>定时备份</span>
          <strong>{config?.scheduleEnabled ? config.scheduleCronExpr || "已启用" : "未启用"}</strong>
        </div>
        <div>
          <span>后台恢复</span>
          <strong>{config?.restoreEnabled ? "已开启" : "默认关闭"}</strong>
        </div>
      </div>

      <div className="backup-section">
        <div className="backup-section-heading">
          <div className="backup-section-title">
            <span>1</span>
            <div>
              <h4>对象存储配置</h4>
              <p>不配置也能用本地备份；配置 R2/S3 后会额外上传一份并支持外部下载。</p>
            </div>
          </div>
          <StatusBadge tone={s3Draft.storageConfigured ? "ok" : "neutral"}>{s3Draft.storageConfigured ? "s3 ready" : "local only"}</StatusBadge>
        </div>
        <div className="form-grid backup-form-grid">
          <label>
            Endpoint
            <input
              onChange={(event) => setS3Draft((current) => ({ ...current, endpoint: event.target.value }))}
              placeholder="https://<account_id>.r2.cloudflarestorage.com"
              value={s3Draft.endpoint}
            />
          </label>
          <label>
            Region
            <input onChange={(event) => setS3Draft((current) => ({ ...current, region: event.target.value }))} placeholder="auto" value={s3Draft.region} />
          </label>
          <label>
            Bucket
            <input onChange={(event) => setS3Draft((current) => ({ ...current, bucket: event.target.value }))} value={s3Draft.bucket} />
          </label>
          <label>
            Prefix
            <input onChange={(event) => setS3Draft((current) => ({ ...current, prefix: event.target.value }))} placeholder="cloud-backups" value={s3Draft.prefix} />
          </label>
          <label>
            Access Key ID
            <input onChange={(event) => setS3Draft((current) => ({ ...current, accessKeyId: event.target.value }))} value={s3Draft.accessKeyId} />
          </label>
          <label>
            Secret Access Key
            <input
              onChange={(event) => setS3Draft((current) => ({ ...current, secretAccessKey: event.target.value }))}
              placeholder={s3Draft.secretConfigured ? "已配置，留空不修改" : ""}
              type="password"
              value={s3Draft.secretAccessKey ?? ""}
            />
          </label>
          <label className="inline-checkbox">
            <input checked={s3Draft.forcePathStyle} onChange={(event) => setS3Draft((current) => ({ ...current, forcePathStyle: event.target.checked }))} type="checkbox" />
            <span>Force path style</span>
          </label>
          <label>
            变更原因
            <input onChange={(event) => setS3Reason(event.target.value)} placeholder="例如：配置 Cloudflare R2 生产备份桶" value={s3Reason} />
          </label>
        </div>
        <div className="button-row">
          <button className="secondary-action" disabled={busy} onClick={() => testS3.mutate()} type="button">
            <Wifi size={16} />
            <span>{testS3.isPending ? "检测中" : "检测 S3"}</span>
          </button>
          <button className="primary-action" disabled={busy || !s3Reason.trim()} onClick={() => saveS3.mutate()} type="button">
            <Save size={16} />
            <span>{saveS3.isPending ? "保存中" : "保存存储配置"}</span>
          </button>
        </div>
      </div>

      <div className="backup-section">
        <div className="backup-section-heading">
          <div className="backup-section-title">
            <span>2</span>
            <div>
              <h4>定时备份策略</h4>
              <p>按 cron 表达式定时创建备份；保留天数和保留份数用于自动清理旧记录。</p>
            </div>
          </div>
          <CalendarClock size={18} />
        </div>
        <div className="form-grid backup-schedule-grid">
          <label className="inline-checkbox">
            <input checked={scheduleDraft.enabled} onChange={(event) => setScheduleDraft((current) => ({ ...current, enabled: event.target.checked }))} type="checkbox" />
            <span>启用定时备份</span>
          </label>
          <label>
            Cron
            <input onChange={(event) => setScheduleDraft((current) => ({ ...current, cronExpr: event.target.value }))} placeholder="0 3 * * *" value={scheduleDraft.cronExpr} />
          </label>
          <label>
            保留天数
            <input
              min={0}
              onChange={(event) => setScheduleDraft((current) => ({ ...current, retainDays: Number(event.target.value) || 0 }))}
              type="number"
              value={scheduleDraft.retainDays}
            />
          </label>
          <label>
            保留份数
            <input
              min={0}
              onChange={(event) => setScheduleDraft((current) => ({ ...current, retainCount: Number(event.target.value) || 0 }))}
              type="number"
              value={scheduleDraft.retainCount}
            />
          </label>
          <label>
            变更原因
            <input onChange={(event) => setScheduleReason(event.target.value)} placeholder="例如：每天凌晨自动备份" value={scheduleReason} />
          </label>
        </div>
        <button className="primary-action" disabled={busy || !scheduleReason.trim()} onClick={() => saveSchedule.mutate()} type="button">
          <Save size={16} />
          <span>{saveSchedule.isPending ? "保存中" : "保存定时策略"}</span>
        </button>
      </div>

      <div className="backup-section">
        <div className="backup-section-heading">
          <div className="backup-section-title">
            <span>3</span>
            <div>
              <h4>备份记录</h4>
              <p>恢复是破坏性操作，执行前会自动创建一份恢复前备份。</p>
            </div>
          </div>
          <div className="button-row">
            <button className="secondary-action" disabled={backups.isFetching || busy} onClick={() => backups.refetch()} type="button">
              <RefreshCw size={16} />
              <span>{backups.isFetching ? "刷新中" : "刷新"}</span>
            </button>
            <button className="primary-action" disabled={busy} onClick={() => createManualBackup.mutate()} type="button">
              <DatabaseBackup size={16} />
              <span>{createManualBackup.isPending ? "备份中" : "立即备份"}</span>
            </button>
          </div>
        </div>

        <div className="table-wrap backup-table">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>状态</th>
                <th>文件</th>
                <th>大小</th>
                <th>存储</th>
                <th>过期</th>
                <th>来源</th>
                <th>开始时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {backupItems.map((record) => (
                <tr key={record.id}>
                  <td>
                    <code>{record.id}</code>
                  </td>
                  <td>
                    <div className="backup-status-cell">
                      <StatusBadge tone={backupStatusTone(record)}>{backupStatusLabel(record)}</StatusBadge>
                      {record.errorMessage ? <span>{record.errorMessage}</span> : null}
                      {record.restoreError ? <span>{record.restoreError}</span> : null}
                    </div>
                  </td>
                  <td>
                    <div className="backup-file-cell">
                      <strong>{record.fileName || "-"}</strong>
                      <span>{record.sha256 ? record.sha256.slice(0, 16) : "-"}</span>
                    </div>
                  </td>
                  <td>{formatBytes(record.sizeBytes)}</td>
                  <td>{storageKindLabel(record.storageKind)}</td>
                  <td>{record.expiresAt ? formatDate(record.expiresAt) : "不过期"}</td>
                  <td>{triggerLabel(record.triggeredBy)}</td>
                  <td>{formatDate(record.startedAt)}</td>
                  <td>
                    <div className="compact-actions">
                      <button className="secondary-action compact" disabled={record.status !== "completed" || busy} onClick={() => download.mutate(record)} type="button">
                        <Download size={14} />
                        <span>下载</span>
                      </button>
                      <button
                        className="secondary-action compact"
                        disabled={record.status !== "completed" || busy || !config?.restoreEnabled}
                        onClick={() => setRestoreTarget(record)}
                        type="button"
                      >
                        <RotateCcw size={14} />
                        <span>恢复</span>
                      </button>
                      <button className="secondary-action compact" disabled={busy || record.status === "running"} onClick={() => setDeleteTarget(record)} type="button">
                        <Trash2 size={14} />
                        <span>删除</span>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {backupItems.length === 0 ? (
                <tr>
                  <td className="align-center" colSpan={9}>
                    暂无备份记录
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        {notice ? <p className="inline-notice">{notice}</p> : null}
        {saveS3.error || testS3.error || saveSchedule.error || createManualBackup.error || remove.error || restore.error ? (
          <p className="inline-error">
            {saveS3.error?.message ||
              testS3.error?.message ||
              saveSchedule.error?.message ||
              createManualBackup.error?.message ||
              remove.error?.message ||
              restore.error?.message}
          </p>
        ) : null}
      </div>

      {restoreTarget ? (
        <div className="modal-backdrop" role="presentation">
          <section aria-modal="true" className="danger-confirm-modal backup-restore-modal" role="dialog">
            <div className="modal-heading">
              <div className="modal-icon danger">
                <Archive size={20} />
              </div>
              <div>
                <h3>恢复 Cloud 数据库</h3>
                <p>将恢复到备份 {restoreTarget.id}。执行前会自动创建恢复前备份，恢复完成后建议重启 API 和 worker。</p>
              </div>
              <button aria-label="关闭" className="icon-button" disabled={restore.isPending} onClick={() => setRestoreTarget(null)} type="button">
                <X size={18} />
              </button>
            </div>
            <div className="form-grid backup-restore-grid">
              <label>
                管理员密码
                <input autoComplete="current-password" onChange={(event) => setRestorePassword(event.target.value)} type="password" value={restorePassword} />
              </label>
              <label>
                确认词
                <input onChange={(event) => setRestoreConfirm(event.target.value)} placeholder="输入 RESTORE" value={restoreConfirm} />
              </label>
              <label className="wide-field">
                操作原因
                <textarea onChange={(event) => setRestoreReason(event.target.value)} placeholder="例如：生产数据误删，需要恢复到指定备份" value={restoreReason} />
              </label>
            </div>
            <div className="modal-footer">
              <span>{restoreReason.trim().length}/500</span>
              <div className="button-row">
                <button className="secondary-action" disabled={restore.isPending} onClick={() => setRestoreTarget(null)} type="button">
                  取消
                </button>
                <button
                  className="danger-action"
                  disabled={restore.isPending || !restorePassword || restoreConfirm !== "RESTORE" || !restoreReason.trim()}
                  onClick={() => restore.mutate()}
                  type="button"
                >
                  {restore.isPending ? "恢复中" : "确认恢复"}
                </button>
              </div>
            </div>
          </section>
        </div>
      ) : null}

      {deleteTarget ? (
        <DangerConfirmModal
          open
          title="删除备份记录"
          description={`将删除 ${deleteTarget.fileName || deleteTarget.id} 以及本地文件；如果对象存储中存在同名对象，也会尝试删除。`}
          confirmLabel="确认删除"
          pending={remove.isPending}
          onCancel={() => setDeleteTarget(null)}
          onConfirm={(auditReason) => remove.mutate({ record: deleteTarget, auditReason })}
        />
      ) : null}
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

function formatDate(value: string | null | undefined) {
  return value ? new Date(value).toLocaleString() : "-";
}

function formatBytes(value: number) {
  if (!value) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function backupStatusTone(record: BackupRecord): "ok" | "warn" | "danger" | "neutral" {
  if (record.restoreStatus === "failed" || record.status === "failed") return "danger";
  if (record.restoreStatus === "running" || record.status === "running" || record.status === "pending") return "warn";
  if (record.restoreStatus === "completed" || record.status === "completed") return "ok";
  return "neutral";
}

function backupStatusLabel(record: BackupRecord) {
  if (record.restoreStatus === "running") return "恢复中";
  if (record.restoreStatus === "completed") return "已恢复";
  if (record.restoreStatus === "failed") return "恢复失败";
  if (record.status === "running") return record.progress === "uploading" ? "上传中" : "备份中";
  if (record.status === "pending") return "等待中";
  if (record.status === "completed") return "已完成";
  if (record.status === "failed") return "失败";
  return record.status || "-";
}

function storageKindLabel(value: string) {
  if (value === "local+s3") return "本地 + S3";
  if (value === "s3") return "S3";
  return "本地";
}

function triggerLabel(value: string) {
  if (value === "scheduled") return "定时";
  if (value.startsWith("pre_restore:")) return "恢复前备份";
  return "手动";
}

function translateBackupError(value: string) {
  const code = value.trim();
  const map: Record<string, string> = {
    backup_s3_not_configured: "对象存储未配置完整",
    backup_in_progress: "已有备份正在执行",
    restore_in_progress: "已有恢复正在执行",
    backup_not_completed: "备份尚未完成",
    restore_disabled: "后台恢复未开启",
    pg_dump_not_found: "API 容器缺少 pg_dump",
    pg_restore_not_found: "API 容器缺少 pg_restore"
  };
  return map[code] ?? code;
}
