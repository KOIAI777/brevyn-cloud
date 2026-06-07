import { Link, useParams } from "react-router-dom";
import { Ban, Copy, Gift, RotateCcw, ShieldOff, Shuffle, SlidersHorizontal } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { DataTable } from "../components/DataTable";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import {
  changeUserGatewayGroup,
  disableGatewayAPIKey,
  getAdminUser,
  getGatewayGroups,
  getRedemptions,
  getUserAPIKeys,
  getUserDevices,
  getUserGatewayAccounts,
  getUserSubscriptions,
  getUserWalletTransactions,
  grantUserBalance,
  rotateUserAPIKey,
  updateUserConcurrency,
  updateAdminUser
} from "../api/client";

function keyTone(status: string) {
  if (status === "active") return "ok";
  if (status === "disabled") return "danger";
  return "warn";
}

function syncTone(status: string) {
  if (status === "active" || status === "synced" || status === "succeeded") return "ok";
  if (status === "disabled" || status === "gateway_failed" || status === "dead_letter") return "danger";
  if (status === "pending_gateway" || status === "failed" || status === "running") return "warn";
  return "neutral";
}

function formatUSD(value: number) {
  const sign = value > 0 ? "+" : "";
  return `${sign}$${value.toFixed(2)}`;
}

function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

type UserDetailConfirmAction =
  | { type: "grant" }
  | { type: "rotate"; externalGroupId?: number; label: string }
  | { type: "changeGroup"; externalGroupId: number; label: string }
  | { type: "concurrency"; concurrency: number }
  | { type: "disableUser"; email: string }
  | { type: "disableKey"; keyId: string; label: string };

export function UserDetailPage() {
  const { id } = useParams();
  const queryClient = useQueryClient();
  const [grantOpen, setGrantOpen] = useState(false);
  const [grantAmount, setGrantAmount] = useState("");
  const [grantNotes, setGrantNotes] = useState("");
  const [syncGrant, setSyncGrant] = useState(true);
  const [notice, setNotice] = useState("");
  const [newPlainKey, setNewPlainKey] = useState("");
  const [groupInput, setGroupInput] = useState("");
  const [concurrencyInput, setConcurrencyInput] = useState("");
  const [confirmAction, setConfirmAction] = useState<UserDetailConfirmAction | null>(null);
  const user = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user", id],
    queryFn: () => getAdminUser(id!)
  });
  const apiKeys = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user-api-keys", id],
    queryFn: () => getUserAPIKeys(id!)
  });
  const walletTransactions = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user-wallet-transactions", id],
    queryFn: () => getUserWalletTransactions(id!, 50)
  });
  const devices = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user-devices", id],
    queryFn: () => getUserDevices(id!)
  });
  const gatewayAccounts = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user-gateway-accounts", id],
    queryFn: () => getUserGatewayAccounts(id!)
  });
  const gatewayGroups = useQuery({
    queryKey: ["admin-gateway-groups"],
    queryFn: getGatewayGroups
  });
  const subscriptions = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user-subscriptions", id],
    queryFn: () => getUserSubscriptions(id!)
  });
  const redemptions = useQuery({
    enabled: Boolean(id),
    queryKey: ["admin-user-redemptions", id],
    queryFn: () => getRedemptions({ user: id!, limit: 10, offset: 0 })
  });
  const disableKey = useMutation({
    mutationFn: ({ keyId, auditReason }: { keyId: string; auditReason: string }) => disableGatewayAPIKey(keyId, { auditReason }),
    onSuccess: () => {
      setConfirmAction(null);
      void queryClient.invalidateQueries({ queryKey: ["admin-user-api-keys", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    }
  });
  const grantBalance = useMutation({
    mutationFn: (auditReason: string) =>
      grantUserBalance(id!, {
        amount: Number(grantAmount),
        notes: grantNotes,
        syncSub2api: syncGrant,
        auditReason
      }),
    onSuccess: (result) => {
      setConfirmAction(null);
      setNotice(result.syncWarning ? `本地已赠送，Sub2API 同步警告：${result.syncWarning}` : `已赠送，当前余额 $${result.balance.toFixed(2)}`);
      setGrantAmount("");
      setGrantNotes("");
      setGrantOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin-user", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-wallet-transactions", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-redemptions", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-users"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-usage-summary"] });
    }
  });
  const rotateKey = useMutation({
    mutationFn: ({ externalGroupId, auditReason }: { externalGroupId?: number; auditReason: string }) =>
      rotateUserAPIKey(id!, { ...(externalGroupId ? { externalGroupId } : {}), auditReason }),
    onSuccess: (result) => {
      setConfirmAction(null);
      setNewPlainKey(result.plainApiKey);
      setNotice(result.syncWarnings.length ? `新 Key 已生成，旧 Key 同步警告：${result.syncWarnings.join("; ")}` : "新 Key 已生成，旧 Key 已禁用。");
      void queryClient.invalidateQueries({ queryKey: ["admin-user-api-keys", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-gateway-accounts", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    }
  });
  const changeGroup = useMutation({
    mutationFn: ({ externalGroupId, auditReason }: { externalGroupId: number; auditReason: string }) =>
      changeUserGatewayGroup(id!, { externalGroupId, auditReason }),
    onSuccess: (result) => {
      setConfirmAction(null);
      setNewPlainKey(result.plainApiKey);
      setNotice(
        result.syncWarnings.length
          ? `默认分组已变更，新 Key 已生成，旧 Key 同步警告：${result.syncWarnings.join("; ")}`
          : "默认分组已变更，新 Key 已生成，旧 Key 已禁用。"
      );
      void queryClient.invalidateQueries({ queryKey: ["admin-user", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-api-keys", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-gateway-accounts", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-users"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    }
  });
  const updateConcurrency = useMutation({
    mutationFn: ({ concurrency, auditReason }: { concurrency: number; auditReason: string }) =>
      updateUserConcurrency(id!, { concurrency, auditReason }),
    onSuccess: (result) => {
      setConfirmAction(null);
      setNotice(
        result.syncOperation
          ? `并发数已进入同步队列：${result.syncOperation}`
          : `并发数已更新为 ${result.concurrency}`
      );
      void queryClient.invalidateQueries({ queryKey: ["admin-user-gateway-accounts", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-users"] });
    }
  });
  const disableUser = useMutation({
    mutationFn: (auditReason: string) => updateAdminUser(id!, { status: "disabled", cascadeDisableKeys: true, auditReason }),
    onSuccess: (result) => {
      setConfirmAction(null);
      setNotice(`用户已禁用：${result.syncWarning}`);
      void queryClient.invalidateQueries({ queryKey: ["admin-user", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-api-keys", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-user-gateway-accounts", id] });
      void queryClient.invalidateQueries({ queryKey: ["admin-users"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    }
  });
  const data = user.data?.user;
  const primaryGatewayAccount = gatewayAccounts.data?.items.find((item) => item.provider === "sub2api") ?? gatewayAccounts.data?.items[0];
  const currentGroupId = primaryGatewayAccount?.defaultGroupId || data?.defaultGroupId || 0;
  const currentConcurrency = primaryGatewayAccount?.concurrency || 5;
  const standardGroups = useMemo(
    () => (gatewayGroups.data?.items ?? []).filter((group) => group.status === "active" && group.subscriptionType === "standard"),
    [gatewayGroups.data?.items]
  );
  const selectedGroup = standardGroups.find((group) => String(group.externalGroupId) === groupInput);
  const parsedConcurrency = Number(concurrencyInput);
  const canChangeGroup = Boolean(selectedGroup) && Number(groupInput) > 0 && Number(groupInput) !== currentGroupId;
  const canUpdateConcurrency = Number.isInteger(parsedConcurrency) && parsedConcurrency >= 1 && parsedConcurrency <= 500 && parsedConcurrency !== currentConcurrency;
  const isBusy =
    disableKey.isPending ||
    grantBalance.isPending ||
    rotateKey.isPending ||
    changeGroup.isPending ||
    updateConcurrency.isPending ||
    disableUser.isPending;
  useEffect(() => {
    setGroupInput(currentGroupId ? String(currentGroupId) : "");
  }, [currentGroupId]);
  useEffect(() => {
    setConcurrencyInput(String(currentConcurrency || 5));
  }, [currentConcurrency]);
  const confirmCopy =
    confirmAction?.type === "grant"
      ? {
          title: "赠送余额",
          description: `将给 ${data?.email ?? id} 赠送 $${Number(grantAmount || 0).toFixed(2)}，${syncGrant ? "并同步到 Sub2API 余额。" : "仅写入 Brevyn 本地账本。"}`,
          label: "确认赠送"
        }
      : confirmAction?.type === "rotate"
        ? {
            title: "轮换 API Key",
            description: `将为 ${data?.email ?? id} 生成新的 ${confirmAction.label} Key，并禁用旧 active Key。`,
            label: "确认轮换"
          }
        : confirmAction?.type === "changeGroup"
          ? {
              title: "变更默认分组",
              description: `将把 ${data?.email ?? id} 的 Sub2API allowed_groups 改为 ${confirmAction.label}，并重新生成该分组的新 Key。`,
              label: "确认变更"
            }
          : confirmAction?.type === "concurrency"
            ? {
                title: "更新并发数",
                description: `将把 ${data?.email ?? id} 的 Sub2API 用户并发数改为 ${confirmAction.concurrency}。`,
                label: "确认更新"
              }
            : confirmAction?.type === "disableUser"
              ? {
                  title: "禁用用户",
                  description: `将禁用 ${confirmAction.email}，并级联禁用该用户当前 active API Key。`,
                  label: "确认禁用"
                }
              : confirmAction?.type === "disableKey"
                ? {
                    title: "禁用 API Key",
                    description: `将禁用 ${confirmAction.label}，用户需要重新获取新的官方配置。`,
                    label: "确认禁用"
                  }
                : null;

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="User Detail"
        title={id ?? "用户详情"}
        description="用户资料、Sub2API 影子账号、设备 Key、余额和操作记录。"
        actions={
          <div className="button-row">
            <button className="secondary-action" onClick={() => setGrantOpen((value) => !value)} type="button">
              <Gift size={16} />
              <span>赠送余额</span>
            </button>
            <button
              className="secondary-action"
              disabled={isBusy}
              onClick={() => setConfirmAction({ type: "rotate", label: "默认分组" })}
              type="button"
            >
              <RotateCcw size={16} />
              <span>{rotateKey.isPending ? "轮换中" : "轮换 Key"}</span>
            </button>
            <button
              className="danger-action"
              disabled={isBusy || data?.status === "disabled"}
              onClick={() => setConfirmAction({ type: "disableUser", email: data?.email ?? id ?? "该用户" })}
              type="button"
            >
              <Ban size={16} />
              <span>{disableUser.isPending ? "禁用中" : "禁用"}</span>
            </button>
          </div>
        }
      />
      {user.isLoading ? <div className="panel inline-state">正在加载用户详情...</div> : null}
      {user.isError ? <div className="panel inline-state danger-text">用户详情加载失败。</div> : null}
      {notice ? <div className="panel inline-state">{notice}</div> : null}
      {grantBalance.isError || rotateKey.isError || changeGroup.isError || updateConcurrency.isError || disableUser.isError ? (
        <div className="panel inline-state danger-text">
          {grantBalance.error?.message || rotateKey.error?.message || changeGroup.error?.message || updateConcurrency.error?.message || disableUser.error?.message}
        </div>
      ) : null}
      {newPlainKey ? (
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h3>新 API Key</h3>
              <p className="panel-subtitle">仅本次展示，刷新页面后只保留脱敏记录。</p>
            </div>
            <button className="secondary-action" onClick={() => void navigator.clipboard.writeText(newPlainKey)} type="button">
              <Copy size={15} />
              <span>复制</span>
            </button>
          </div>
          <textarea className="code-output" readOnly value={newPlainKey} />
        </section>
      ) : null}
      {grantOpen ? (
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h3>赠送余额</h3>
              <p className="panel-subtitle">按美元额度入账，默认同步给 Sub2API 用户余额。</p>
            </div>
          </div>
          <div className="form-grid settings-form-grid">
            <label>
              <span>赠送额度</span>
              <input inputMode="decimal" onChange={(event) => setGrantAmount(event.target.value)} placeholder="例如 10" step="0.01" type="number" value={grantAmount} />
            </label>
            <label>
              <span>备注</span>
              <input onChange={(event) => setGrantNotes(event.target.value)} placeholder="客服补偿 / 测试额度" value={grantNotes} />
            </label>
            <label className="inline-checkbox">
              <input checked={syncGrant} onChange={(event) => setSyncGrant(event.target.checked)} type="checkbox" />
              <span>同步到 Sub2API 余额</span>
            </label>
            <button className="primary-action" disabled={isBusy || Number(grantAmount) <= 0} onClick={() => setConfirmAction({ type: "grant" })} type="button">
              <Gift size={15} />
              <span>{grantBalance.isPending ? "赠送中" : "确认赠送"}</span>
            </button>
          </div>
        </section>
      ) : null}
      <section className="detail-grid">
        <article className="panel">
          <div className="panel-heading">
            <h3>Brevyn 账号</h3>
            <StatusBadge tone={data?.status === "active" ? "ok" : "danger"}>{data?.status ?? "unknown"}</StatusBadge>
          </div>
          <dl className="kv-list">
            <dt>邮箱</dt>
            <dd>{data?.email ?? "-"}</dd>
            <dt>设备数量</dt>
            <dd>{data?.deviceCount ?? 0} / 3</dd>
            <dt>余额</dt>
            <dd>${(data?.balance ?? 0).toFixed(2)}</dd>
          </dl>
        </article>
        <article className="panel">
          <div className="panel-heading">
            <h3>Sub2API 影子账号</h3>
            <StatusBadge tone={data?.gatewayStatus === "active" ? "ok" : "neutral"}>
              {data?.gatewayStatus || "unmapped"}
            </StatusBadge>
          </div>
          <dl className="kv-list">
            <dt>shadow email</dt>
            <dd>{data?.gatewayEmail || "-"}</dd>
            <dt>default_group_id</dt>
            <dd>{data?.defaultGroupId || "未分组"}</dd>
            <dt>创建时间</dt>
            <dd>{data?.createdAt ? new Date(data.createdAt).toLocaleString() : "-"}</dd>
          </dl>
        </article>
      </section>
      <section className="split-grid">
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h3>Sub2API 账号记录</h3>
              <p className="panel-subtitle">影子账号、远端用户 ID、默认分组和最近同步时间。</p>
            </div>
            <StatusBadge tone={gatewayAccounts.data?.total ? "ok" : "warn"}>{`${gatewayAccounts.data?.total ?? 0} accounts`}</StatusBadge>
          </div>
          {gatewayAccounts.isLoading ? <div className="inline-state">正在加载网关账号...</div> : null}
          {gatewayAccounts.isError ? <div className="inline-state danger-text">网关账号加载失败。</div> : null}
          <DataTable
            rows={gatewayAccounts.data?.items ?? []}
            getRowKey={(row) => String(row.id)}
            columns={[
              { key: "provider", header: "网关", render: (row) => row.provider },
              { key: "external", header: "远端用户", render: (row) => row.externalUserId || "-", align: "right" },
              { key: "email", header: "邮箱", render: (row) => row.externalEmail || "-" },
              { key: "group", header: "分组", render: (row) => row.defaultGroupId || "-", align: "right" },
              { key: "concurrency", header: "并发", render: (row) => row.concurrency || 5, align: "right" },
              { key: "synced", header: "同步", render: (row) => formatDate(row.lastSyncedAt), align: "right" },
              { key: "status", header: "状态", render: (row) => <StatusBadge tone={syncTone(row.status)}>{row.status}</StatusBadge> }
            ]}
          />
        </section>
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h3>账号策略</h3>
              <p className="panel-subtitle">对齐 Sub2API 用户的 allowed_groups 和 concurrency。</p>
            </div>
            <StatusBadge tone={primaryGatewayAccount ? "ok" : "warn"}>{primaryGatewayAccount ? "mapped" : "unmapped"}</StatusBadge>
          </div>
          <div className="form-grid settings-form-grid">
            <label>
              <span>默认余额分组</span>
              <select
                disabled={isBusy || standardGroups.length === 0}
                onChange={(event) => setGroupInput(event.target.value)}
                value={groupInput}
              >
                <option value="">选择 active standard 分组</option>
                {standardGroups.map((group) => (
                  <option key={group.id} value={group.externalGroupId}>
                    {group.name} · #{group.externalGroupId} · rate {group.rateMultiplier}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>并发数</span>
              <input
                inputMode="numeric"
                min={1}
                max={500}
                onChange={(event) => setConcurrencyInput(event.target.value)}
                type="number"
                value={concurrencyInput}
              />
            </label>
            <button
              className="secondary-action"
              disabled={isBusy || !canChangeGroup}
              onClick={() =>
                setConfirmAction({
                  type: "changeGroup",
                  externalGroupId: Number(groupInput),
                  label: `${selectedGroup?.name ?? "分组"} #${groupInput}`
                })
              }
              type="button"
            >
              <Shuffle size={15} />
              <span>{changeGroup.isPending ? "变更中" : "变更分组"}</span>
            </button>
            <button
              className="secondary-action"
              disabled={isBusy || !canUpdateConcurrency}
              onClick={() => setConfirmAction({ type: "concurrency", concurrency: parsedConcurrency })}
              type="button"
            >
              <SlidersHorizontal size={15} />
              <span>{updateConcurrency.isPending ? "更新中" : "更新并发"}</span>
            </button>
          </div>
          {standardGroups.length === 0 ? <p className="inline-error">还没有同步到 active standard 分组。</p> : null}
        </section>
      </section>
      <section className="split-grid">
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h3>设备</h3>
              <p className="panel-subtitle">设备指纹不会明文展示，这里只看活跃状态和最后使用时间。</p>
            </div>
            <StatusBadge tone={devices.data?.total ? "ok" : "neutral"}>{`${devices.data?.total ?? 0} devices`}</StatusBadge>
          </div>
          {devices.isLoading ? <div className="inline-state">正在加载设备...</div> : null}
          {devices.isError ? <div className="inline-state danger-text">设备加载失败。</div> : null}
          <DataTable
            rows={devices.data?.items ?? []}
            getRowKey={(row) => row.id}
            columns={[
              { key: "name", header: "设备", render: (row) => row.name || row.id },
              { key: "platform", header: "平台", render: (row) => row.platform || "-" },
              { key: "lastSeen", header: "最近活跃", render: (row) => formatDate(row.lastSeenAt), align: "right" },
              { key: "status", header: "状态", render: (row) => <StatusBadge tone={syncTone(row.status)}>{row.status}</StatusBadge> }
            ]}
          />
        </section>
      </section>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h3>Sub2API 订阅实例</h3>
            <p className="panel-subtitle">直接读取 Sub2API 当前订阅；复杂调整仍在订阅页统一操作。</p>
          </div>
          <StatusBadge tone={subscriptions.data?.total ? "ok" : "neutral"}>{`${subscriptions.data?.total ?? 0} subscriptions`}</StatusBadge>
        </div>
        {subscriptions.isLoading ? <div className="inline-state">正在加载订阅实例...</div> : null}
        {subscriptions.isError ? <div className="inline-state danger-text">订阅实例加载失败，请检查 Sub2API 连接。</div> : null}
        <DataTable
          rows={subscriptions.data?.items ?? []}
          getRowKey={(row) => String(row.id)}
          columns={[
            { key: "id", header: "订阅", render: (row) => `#${row.id}` },
            { key: "group", header: "分组", render: (row) => row.group ? `${row.group.name} · #${row.groupId}` : `#${row.groupId}` },
            { key: "validity", header: "有效期", render: (row) => `${formatDate(row.startsAt)} - ${formatDate(row.expiresAt)}` },
            {
              key: "usage",
              header: "日 / 周 / 月用量",
              render: (row) => `$${row.dailyUsageUsd.toFixed(2)} / $${row.weeklyUsageUsd.toFixed(2)} / $${row.monthlyUsageUsd.toFixed(2)}`,
              align: "right"
            },
            { key: "status", header: "状态", render: (row) => <StatusBadge tone={syncTone(row.status)}>{row.status}</StatusBadge> }
          ]}
        />
      </section>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h3>最近兑换</h3>
            <p className="panel-subtitle">这个用户最近 10 条卡密兑换和网关同步结果。</p>
          </div>
          <Link className="secondary-action" to={`/admin/redemptions?user=${encodeURIComponent(id ?? "")}`}>
            查看全部
          </Link>
        </div>
        {redemptions.isLoading ? <div className="inline-state">正在加载兑换记录...</div> : null}
        {redemptions.isError ? <div className="inline-state danger-text">兑换记录加载失败。</div> : null}
        <DataTable
          rows={redemptions.data?.items ?? []}
          getRowKey={(row) => row.id}
          columns={[
            { key: "id", header: "记录", render: (row) => row.id },
            { key: "product", header: "商品", render: (row) => row.productName || "-" },
            {
              key: "value",
              header: "到账",
              render: (row) => (row.kind === "subscription" ? `${row.validityDays} 天` : `$${row.value.toFixed(2)}`),
              align: "right"
            },
            { key: "group", header: "分组", render: (row) => row.externalGroupId || "-", align: "right" },
            {
              key: "status",
              header: "状态",
              render: (row) => <StatusBadge tone={syncTone(row.status)}>{row.status}</StatusBadge>
            },
            {
              key: "operation",
              header: "队列",
              render: (row) =>
                row.operationStatus ? `${row.operationStatus} ${row.operationAttempts}/${row.operationMaxAttempts || 8}` : "-"
            },
            { key: "time", header: "时间", render: (row) => formatDate(row.createdAt), align: "right" }
          ]}
        />
      </section>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h3>余额流水</h3>
            <p className="panel-subtitle">Brevyn Cloud 自有钱包账本，包含兑换、管理员赠送和 Sub2API 同步修正。</p>
          </div>
          <StatusBadge tone={walletTransactions.data?.total ? "ok" : "neutral"}>{`${walletTransactions.data?.total ?? 0} rows`}</StatusBadge>
        </div>
        {walletTransactions.isLoading ? <div className="inline-state">正在加载余额流水...</div> : null}
        {walletTransactions.isError ? <div className="inline-state danger-text">余额流水加载失败。</div> : null}
        <DataTable
          rows={walletTransactions.data?.items ?? []}
          getRowKey={(row) => row.id}
          columns={[
            { key: "id", header: "流水", render: (row) => row.id },
            { key: "kind", header: "类型", render: (row) => row.kind },
            { key: "amount", header: "变动", render: (row) => formatUSD(row.amount), align: "right" },
            { key: "balance", header: "变动后", render: (row) => `$${row.balanceAfter.toFixed(2)}`, align: "right" },
            { key: "source", header: "来源", render: (row) => row.source },
            {
              key: "notes",
              header: "备注",
              render: (row) => (
                <span className="clipped-cell" title={row.notes || row.referenceId}>
                  {row.notes || row.referenceId || "-"}
                </span>
              )
            },
            { key: "time", header: "时间", render: (row) => formatDate(row.createdAt), align: "right" }
          ]}
        />
      </section>
      <section className="panel">
        <div className="panel-heading">
          <div>
            <h3>API Keys</h3>
            <p className="panel-subtitle">只展示脱敏 Key；泄露或异常时可直接禁用本地记录并同步到 Sub2API。</p>
          </div>
          <StatusBadge tone={apiKeys.data?.total ? "ok" : "neutral"}>{`${apiKeys.data?.total ?? 0} keys`}</StatusBadge>
        </div>
        {apiKeys.isLoading ? <div className="inline-state">正在加载 API Keys...</div> : null}
        {apiKeys.isError ? <div className="inline-state danger-text">API Keys 加载失败。</div> : null}
        {disableKey.isError ? <div className="inline-state danger-text">{disableKey.error.message}</div> : null}
        {disableKey.data?.syncWarning ? (
          <div className="inline-state danger-text">本地已禁用，但同步 Sub2API 失败：{disableKey.data.syncWarning}</div>
        ) : null}
        <DataTable
          rows={apiKeys.data?.items ?? []}
          getRowKey={(row) => row.id}
          columns={[
            { key: "key", header: "Key", render: (row) => <code>{row.maskedApiKey || row.id}</code> },
            { key: "provider", header: "网关", render: (row) => row.provider },
            { key: "group", header: "分组", render: (row) => row.externalGroupId || "-", align: "right" },
            { key: "external", header: "远端 ID", render: (row) => row.externalKeyId || "-", align: "right" },
            { key: "lastUsed", header: "最近使用", render: (row) => formatDate(row.lastUsedAt), align: "right" },
            {
              key: "status",
              header: "状态",
              render: (row) => <StatusBadge tone={keyTone(row.status)}>{row.status}</StatusBadge>
            },
            {
              key: "actions",
              header: "操作",
              align: "right",
              render: (row) =>
                row.status === "active" ? (
                  <button
                    className="danger-action"
                    disabled={disableKey.isPending}
                    type="button"
                    onClick={() => setConfirmAction({ type: "disableKey", keyId: row.id, label: row.maskedApiKey || row.id })}
                  >
                    <ShieldOff size={15} />
                    <span>{disableKey.isPending ? "禁用中" : "禁用"}</span>
                  </button>
                ) : (
                  <span className="clipped-cell">无需操作</span>
                ),
            },
            {
              key: "rotate",
              header: "轮换",
              align: "right",
              render: (row) =>
                row.status === "active" ? (
                  <button
                    className="secondary-action"
                    disabled={isBusy}
                    type="button"
                    onClick={() => setConfirmAction({ type: "rotate", externalGroupId: row.externalGroupId, label: `分组 ${row.externalGroupId}` })}
                  >
                    <RotateCcw size={15} />
                    <span>轮换</span>
                  </button>
                ) : (
                  <span className="clipped-cell">-</span>
                )
            }
          ]}
        />
      </section>
      {confirmCopy ? (
        <DangerConfirmModal
          open={Boolean(confirmAction)}
          title={confirmCopy.title}
          description={confirmCopy.description}
          confirmLabel={confirmCopy.label}
          pending={isBusy}
          onCancel={() => setConfirmAction(null)}
          onConfirm={(auditReason) => {
            if (!confirmAction) return;
            if (confirmAction.type === "grant") grantBalance.mutate(auditReason);
            if (confirmAction.type === "rotate") rotateKey.mutate({ externalGroupId: confirmAction.externalGroupId, auditReason });
            if (confirmAction.type === "changeGroup") changeGroup.mutate({ externalGroupId: confirmAction.externalGroupId, auditReason });
            if (confirmAction.type === "concurrency") updateConcurrency.mutate({ concurrency: confirmAction.concurrency, auditReason });
            if (confirmAction.type === "disableUser") disableUser.mutate(auditReason);
            if (confirmAction.type === "disableKey") disableKey.mutate({ keyId: confirmAction.keyId, auditReason });
          }}
        />
      ) : null}
    </div>
  );
}
