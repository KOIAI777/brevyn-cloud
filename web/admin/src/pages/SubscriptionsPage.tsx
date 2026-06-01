import { CalendarClock, FilterX, Plus, RefreshCw, RotateCcw, Search, TimerReset, X } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { DataTable } from "../components/DataTable";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { PaginationRow } from "../components/PaginationRow";
import { StatusBadge } from "../components/StatusBadge";
import {
  assignSubscription,
  extendSubscription,
  getGatewayGroups,
  getSubscriptions,
  resetSubscriptionQuota,
  revokeSubscription,
  type AdminSubscription,
  type GatewayGroup
} from "../api/client";

type ModalState =
  | { type: "assign" }
  | { type: "extend"; subscription: AdminSubscription }
  | { type: "reset"; subscription: AdminSubscription }
  | null;

function statusTone(status: string) {
  if (status === "active") return "ok";
  if (status === "expired") return "warn";
  if (status === "revoked" || status === "suspended" || status === "deleted") return "danger";
  return "neutral";
}

function formatDate(value: string | null | undefined) {
  return value ? new Date(value).toLocaleString() : "-";
}

function formatUsd(value: number | null | undefined) {
  if (value === null || value === undefined) return "不限";
  return `$${value.toFixed(2)}`;
}

function remainingDays(expiresAt: string) {
  const ms = new Date(expiresAt).getTime() - Date.now();
  return Math.ceil(ms / 86_400_000);
}

function usagePercent(used: number, limit: number | null | undefined) {
  if (!limit || limit <= 0) return 0;
  return Math.min(100, Math.round((used / limit) * 100));
}

function groupLabel(group: GatewayGroup) {
  const typeLabel = group.subscriptionType === "subscription" ? "订阅限额组" : "余额扣费组";
  return `#${group.externalGroupId} · ${group.name} · ${typeLabel}`;
}

function UsageMeter({ label, used, limit }: { label: string; used: number; limit: number | null | undefined }) {
  const percent = usagePercent(used, limit);
  return (
    <div className="subscription-usage-meter">
      <div>
        <span>{label}</span>
        <strong>
          ${used.toFixed(2)} / {formatUsd(limit)}
        </strong>
      </div>
      <div className="usage-track" aria-hidden="true">
        <span style={{ width: `${percent}%` }} />
      </div>
    </div>
  );
}

function SubscriptionActionModal({
  state,
  groups,
  pending,
  error,
  onCancel,
  onAssign,
  onExtend,
  onReset
}: {
  state: ModalState;
  groups: GatewayGroup[];
  pending: boolean;
  error: Error | null;
  onCancel: () => void;
  onAssign: (input: { externalUserId: number; externalGroupId: number; validityDays: number; notes: string; auditReason: string }) => void;
  onExtend: (input: { id: number; days: number; auditReason: string }) => void;
  onReset: (input: { id: number; daily: boolean; weekly: boolean; monthly: boolean; auditReason: string }) => void;
}) {
  const [externalUserId, setExternalUserId] = useState("");
  const [externalGroupId, setExternalGroupId] = useState("");
  const [validityDays, setValidityDays] = useState("7");
  const [notes, setNotes] = useState("");
  const [days, setDays] = useState("7");
  const [daily, setDaily] = useState(true);
  const [weekly, setWeekly] = useState(false);
  const [monthly, setMonthly] = useState(false);
  const [auditReason, setAuditReason] = useState("");

  useEffect(() => {
    if (!state) return;
    setAuditReason("");
    if (state.type === "assign") {
      setExternalUserId("");
      setExternalGroupId("");
      setValidityDays("7");
      setNotes("");
    }
    if (state.type === "extend") {
      setDays("7");
    }
    if (state.type === "reset") {
      setDaily(true);
      setWeekly(false);
      setMonthly(false);
    }
  }, [state]);

  if (!state) return null;

  const isAssign = state.type === "assign";
  const isExtend = state.type === "extend";
  const title = isAssign ? "手动分配订阅" : isExtend ? "调整订阅天数" : "重置订阅额度";
  const description = isAssign
    ? "直接调用 Sub2API admin assign 接口，字段对应 user_id、group_id、validity_days、notes。"
    : isExtend
      ? `调整订阅 #${state.subscription.id} 的 expires_at，正数延长，负数缩短。`
      : `重置订阅 #${state.subscription.id} 的 daily / weekly / monthly usage 窗口用量。`;
  const trimmedReason = auditReason.trim();
  const selectedGroup = groups.find((group) => String(group.externalGroupId) === externalGroupId);
  const normalizedValidityDays = Number(validityDays);
  const normalizedDays = Number(days);
  const canSubmit =
    trimmedReason.length > 0 &&
    !pending &&
    ((isAssign && Number(externalUserId) > 0 && Number(externalGroupId) > 0 && normalizedValidityDays > 0) ||
      (isExtend && normalizedDays !== 0) ||
      (state.type === "reset" && (daily || weekly || monthly)));

  return (
    <div className="modal-backdrop" role="presentation">
      <section aria-modal="true" className="danger-confirm-modal subscription-action-modal" role="dialog">
        <div className="modal-heading">
          <div className="modal-icon">
            {isAssign ? <Plus size={20} /> : isExtend ? <CalendarClock size={20} /> : <TimerReset size={20} />}
          </div>
          <div>
            <h3>{title}</h3>
            <p>{description}</p>
          </div>
          <button aria-label="关闭" className="icon-button" disabled={pending} onClick={onCancel} type="button">
            <X size={18} />
          </button>
        </div>

        {isAssign ? (
          <div className="form-grid settings-form-grid">
            <label>
              <span>Sub2API user_id</span>
              <input autoFocus min={1} onChange={(event) => setExternalUserId(event.target.value)} type="number" value={externalUserId} />
            </label>
            <label>
              <span>订阅分组 group_id</span>
              <select
                onChange={(event) => {
                  const value = event.target.value;
                  setExternalGroupId(value);
                  const group = groups.find((item) => String(item.externalGroupId) === value);
                  if (group?.defaultValidityDays) setValidityDays(String(group.defaultValidityDays));
                }}
                value={externalGroupId}
              >
                <option value="">选择订阅分组</option>
                {groups.map((group) => (
                  <option key={group.id} value={group.externalGroupId}>
                    {groupLabel(group)}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>有效天数 validity_days</span>
              <input min={1} onChange={(event) => setValidityDays(event.target.value)} type="number" value={validityDays} />
            </label>
            <div className="field-summary">
              <span>分组限额</span>
              <strong>
                日 {formatUsd(selectedGroup?.dailyLimitUsd)} / 周 {formatUsd(selectedGroup?.weeklyLimitUsd)} / 月{" "}
                {formatUsd(selectedGroup?.monthlyLimitUsd)}
              </strong>
            </div>
            <label className="wide-field">
              <span>Sub2API notes</span>
              <input onChange={(event) => setNotes(event.target.value)} placeholder="例如：联动小铺补发 / 客服手动开通" value={notes} />
            </label>
          </div>
        ) : null}

        {isExtend ? (
          <div className="form-grid settings-form-grid">
            <label>
              <span>调整天数 days</span>
              <input autoFocus onChange={(event) => setDays(event.target.value)} type="number" value={days} />
            </label>
            <div className="field-summary">
              <span>当前到期</span>
              <strong>{formatDate(state.subscription.expiresAt)}</strong>
            </div>
          </div>
        ) : null}

        {state.type === "reset" ? (
          <div className="subscription-reset-grid">
            <label className="inline-checkbox">
              <input checked={daily} onChange={(event) => setDaily(event.target.checked)} type="checkbox" />
              <span>重置 daily_usage_usd</span>
            </label>
            <label className="inline-checkbox">
              <input checked={weekly} onChange={(event) => setWeekly(event.target.checked)} type="checkbox" />
              <span>重置 weekly_usage_usd</span>
            </label>
            <label className="inline-checkbox">
              <input checked={monthly} onChange={(event) => setMonthly(event.target.checked)} type="checkbox" />
              <span>重置 monthly_usage_usd</span>
            </label>
          </div>
        ) : null}

        <label className="modal-reason-field">
          <span>操作原因</span>
          <textarea
            maxLength={500}
            onChange={(event) => setAuditReason(event.target.value)}
            placeholder="例如：用户补偿 / 客服确认 / 修复订阅窗口"
            value={auditReason}
          />
        </label>
        {error ? <div className="form-error">{error.message}</div> : null}
        <div className="modal-footer">
          <span>{trimmedReason.length}/500</span>
          <div className="button-row">
            <button className="secondary-action" disabled={pending} onClick={onCancel} type="button">
              取消
            </button>
            <button
              className={state.type === "reset" ? "danger-action" : "primary-action"}
              disabled={!canSubmit}
              onClick={() => {
                if (isAssign) {
                  onAssign({
                    externalUserId: Number(externalUserId),
                    externalGroupId: Number(externalGroupId),
                    validityDays: normalizedValidityDays,
                    notes,
                    auditReason: trimmedReason
                  });
                  return;
                }
                if (isExtend) {
                  onExtend({ id: state.subscription.id, days: normalizedDays, auditReason: trimmedReason });
                  return;
                }
                onReset({ id: state.subscription.id, daily, weekly, monthly, auditReason: trimmedReason });
              }}
              type="button"
            >
              {pending ? "处理中" : "确认执行"}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}

export function SubscriptionsPage() {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState("all");
  const [externalUserId, setExternalUserId] = useState("");
  const [externalGroupId, setExternalGroupId] = useState("all");
  const [platform, setPlatform] = useState("");
  const [sortBy, setSortBy] = useState("created_at");
  const [sortOrder, setSortOrder] = useState("desc");
  const [pageSize, setPageSize] = useState(50);
  const [offset, setOffset] = useState(0);
  const [modal, setModal] = useState<ModalState>(null);
  const [revokeTarget, setRevokeTarget] = useState<AdminSubscription | null>(null);
  const resetOffset = () => setOffset(0);

  const groups = useQuery({ queryKey: ["admin-gateway-groups"], queryFn: getGatewayGroups });
  const subscriptionGroups = useMemo(
    () =>
      (groups.data?.items ?? []).filter(
        (group) => group.status === "active" && group.subscriptionType === "subscription" && group.externalGroupId > 0
      ),
    [groups.data?.items]
  );
  const subscriptions = useQuery({
    queryKey: ["admin-subscriptions", status, externalUserId, externalGroupId, platform, sortBy, sortOrder, pageSize, offset],
    queryFn: () =>
      getSubscriptions({
        status,
        externalUserId,
        externalGroupId,
        platform,
        sortBy,
        sortOrder,
        limit: pageSize,
        offset
      })
  });

  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["admin-subscriptions"] }),
      queryClient.invalidateQueries({ queryKey: ["admin-audit-logs"] })
    ]);
  };

  const assign = useMutation({
    mutationFn: assignSubscription,
    onSuccess: async () => {
      setModal(null);
      await invalidate();
    }
  });
  const extend = useMutation({
    mutationFn: ({ id, days, auditReason }: { id: number; days: number; auditReason: string }) => extendSubscription(id, { days, auditReason }),
    onSuccess: async () => {
      setModal(null);
      await invalidate();
    }
  });
  const resetQuota = useMutation({
    mutationFn: ({ id, daily, weekly, monthly, auditReason }: { id: number; daily: boolean; weekly: boolean; monthly: boolean; auditReason: string }) =>
      resetSubscriptionQuota(id, { daily, weekly, monthly, auditReason }),
    onSuccess: async () => {
      setModal(null);
      await invalidate();
    }
  });
  const revoke = useMutation({
    mutationFn: ({ id, auditReason }: { id: number; auditReason: string }) => revokeSubscription(id, { auditReason }),
    onSuccess: async () => {
      setRevokeTarget(null);
      await invalidate();
    }
  });

  const clearFilters = () => {
    setStatus("all");
    setExternalUserId("");
    setExternalGroupId("all");
    setPlatform("");
    setSortBy("created_at");
    setSortOrder("desc");
    setPageSize(50);
    setOffset(0);
  };
  const actionError = assign.error ?? extend.error ?? resetQuota.error;
  const rows = subscriptions.data?.items ?? [];

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Sub2API Subscriptions"
        title="订阅实例"
        description="按 Sub2API user_subscriptions 语义管理用户订阅，查看有效期、日周月窗口用量、分配备注和手动调整记录。"
        actions={
          <div className="compact-actions">
            <button className="secondary-action" disabled={subscriptions.isFetching} onClick={() => void subscriptions.refetch()} type="button">
              <RefreshCw size={16} />
              <span>{subscriptions.isFetching ? "刷新中" : "刷新"}</span>
            </button>
            <button className="primary-action" onClick={() => setModal({ type: "assign" })} type="button">
              <Plus size={16} />
              <span>手动分配</span>
            </button>
          </div>
        }
      />

      <section className="panel filter-panel">
        <div className="search-box full">
          <Search size={16} />
          <input
            onChange={(event) => {
              setExternalUserId(event.target.value);
              resetOffset();
            }}
            placeholder="按 Sub2API user_id 过滤"
            value={externalUserId}
          />
        </div>
        <div className="filter-grid">
          <label>
            <span>状态</span>
            <select
              onChange={(event) => {
                setStatus(event.target.value);
                resetOffset();
              }}
              value={status}
            >
              <option value="all">全部状态</option>
              <option value="active">Active</option>
              <option value="expired">Expired</option>
              <option value="suspended">Suspended</option>
            </select>
          </label>
          <label>
            <span>订阅分组</span>
            <select
              onChange={(event) => {
                setExternalGroupId(event.target.value);
                resetOffset();
              }}
              value={externalGroupId}
            >
              <option value="all">全部订阅组</option>
              {subscriptionGroups.map((group) => (
                <option key={group.id} value={group.externalGroupId}>
                  {groupLabel(group)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Platform</span>
            <input
              onChange={(event) => {
                setPlatform(event.target.value);
                resetOffset();
              }}
              placeholder="anthropic / openai"
              value={platform}
            />
          </label>
          <label>
            <span>排序字段</span>
            <select
              onChange={(event) => {
                setSortBy(event.target.value);
                resetOffset();
              }}
              value={sortBy}
            >
              <option value="created_at">created_at</option>
              <option value="expires_at">expires_at</option>
              <option value="starts_at">starts_at</option>
              <option value="user_id">user_id</option>
              <option value="group_id">group_id</option>
              <option value="assigned_at">assigned_at</option>
            </select>
          </label>
          <label>
            <span>排序方向</span>
            <select
              onChange={(event) => {
                setSortOrder(event.target.value);
                resetOffset();
              }}
              value={sortOrder}
            >
              <option value="desc">desc</option>
              <option value="asc">asc</option>
            </select>
          </label>
          <label>
            <span>每页</span>
            <select
              onChange={(event) => {
                setPageSize(Number(event.target.value));
                setOffset(0);
              }}
              value={pageSize}
            >
              <option value={25}>25</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={200}>200</option>
            </select>
          </label>
          <button className="secondary-action" onClick={clearFilters} type="button">
            <FilterX size={15} />
            <span>清空筛选</span>
          </button>
        </div>
      </section>

      {subscriptions.isLoading ? <div className="panel inline-state">正在加载订阅实例...</div> : null}
      {subscriptions.isError ? <div className="panel inline-state danger-text">订阅实例加载失败：{subscriptions.error.message}</div> : null}
      {revoke.isError ? <div className="panel inline-state danger-text">撤销失败：{revoke.error.message}</div> : null}

      <div className="wide-table subscriptions-table">
        <DataTable
          rows={rows}
          getRowKey={(row) => String(row.id)}
          columns={[
            {
              key: "subscription",
              header: "订阅",
              render: (row) => (
                <div className="audit-event">
                  <strong>#{row.id}</strong>
                  <StatusBadge tone={statusTone(row.status)}>{row.status}</StatusBadge>
                </div>
              )
            },
            {
              key: "user",
              header: "用户",
              render: (row) => (
                <div className="audit-event">
                  <strong>{row.user?.email || `Sub2API #${row.userId}`}</strong>
                  <code>user_id {row.userId}</code>
                </div>
              )
            },
            {
              key: "group",
              header: "分组",
              render: (row) => (
                <div className="product-table-group">
                  <strong>{row.group?.name || `Sub2API group #${row.groupId}`}</strong>
                  <span>
                    #{row.groupId} · {row.group?.platform || "-"} · {row.group?.subscriptionType || "-"}
                  </span>
                </div>
              )
            },
            {
              key: "period",
              header: "有效期",
              render: (row) => {
                const left = remainingDays(row.expiresAt);
                return (
                  <div className="subscription-period">
                    <strong className={left < 0 ? "danger-text" : left <= 3 ? "retryable-text" : ""}>
                      {left < 0 ? `已过期 ${Math.abs(left)} 天` : `剩 ${left} 天`}
                    </strong>
                    <span>{formatDate(row.startsAt)}</span>
                    <span>{formatDate(row.expiresAt)}</span>
                  </div>
                );
              }
            },
            {
              key: "quota",
              header: "日/周/月用量",
              render: (row) => (
                <div className="subscription-usage-stack">
                  <UsageMeter label="day" used={row.dailyUsageUsd} limit={row.group?.dailyLimitUsd} />
                  <UsageMeter label="week" used={row.weeklyUsageUsd} limit={row.group?.weeklyLimitUsd} />
                  <UsageMeter label="month" used={row.monthlyUsageUsd} limit={row.group?.monthlyLimitUsd} />
                </div>
              )
            },
            {
              key: "windows",
              header: "窗口",
              render: (row) => (
                <div className="subscription-window-list">
                  <span>D {formatDate(row.dailyWindowStart)}</span>
                  <span>W {formatDate(row.weeklyWindowStart)}</span>
                  <span>M {formatDate(row.monthlyWindowStart)}</span>
                </div>
              )
            },
            {
              key: "assigned",
              header: "分配",
              render: (row) => (
                <div className="product-table-group">
                  <strong>{formatDate(row.assignedAt)}</strong>
                  <span>by {row.assignedByUser?.email || row.assignedBy || "-"}</span>
                  {row.notes ? <span title={row.notes}>{row.notes}</span> : null}
                </div>
              )
            },
            {
              key: "actions",
              header: "操作",
              align: "right",
              render: (row) => (
                <div className="compact-actions">
                  <button className="secondary-action" onClick={() => setModal({ type: "extend", subscription: row })} type="button">
                    <CalendarClock size={15} />
                    <span>天数</span>
                  </button>
                  <button className="secondary-action" onClick={() => setModal({ type: "reset", subscription: row })} type="button">
                    <RotateCcw size={15} />
                    <span>重置</span>
                  </button>
                  <button className="danger-action" onClick={() => setRevokeTarget(row)} type="button">
                    撤销
                  </button>
                </div>
              )
            }
          ]}
        />
      </div>
      {!subscriptions.isLoading && rows.length === 0 ? <div className="panel inline-state">暂无订阅实例，或当前筛选没有结果。</div> : null}
      <PaginationRow
        total={subscriptions.data?.total ?? 0}
        limit={pageSize}
        offset={offset}
        isFetching={subscriptions.isFetching}
        onOffsetChange={setOffset}
      />

      <SubscriptionActionModal
        error={actionError instanceof Error ? actionError : null}
        groups={subscriptionGroups}
        pending={assign.isPending || extend.isPending || resetQuota.isPending}
        state={modal}
        onAssign={(input) => assign.mutate(input)}
        onCancel={() => setModal(null)}
        onExtend={(input) => extend.mutate(input)}
        onReset={(input) => resetQuota.mutate(input)}
      />
      <DangerConfirmModal
        open={Boolean(revokeTarget)}
        title="撤销订阅"
        description={`将撤销 Sub2API 订阅 #${revokeTarget?.id ?? ""}。撤销后用户不再享受该订阅分组限额。`}
        confirmLabel="确认撤销"
        pending={revoke.isPending}
        onCancel={() => setRevokeTarget(null)}
        onConfirm={(auditReason) => {
          if (revokeTarget) revoke.mutate({ id: revokeTarget.id, auditReason });
        }}
      />
    </div>
  );
}
