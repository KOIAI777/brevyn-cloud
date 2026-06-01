import { Link } from "react-router-dom";
import { AlertTriangle, Ban, CheckCircle2, ChevronLeft, ChevronRight, Copy, FilterX, Plus, RefreshCw, Search, Trash2, X } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { DataTable } from "../components/DataTable";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import {
  createAdminUser,
  deleteAdminUser,
  getAdminUsers,
  getGatewayGroups,
  getSub2APISettings,
  importSub2APIUser,
  syncSub2APIUsers,
  updateAdminUser
} from "../api/client";

function formatBalance(value: number) {
  return `$${value.toFixed(2)}`;
}

function formatLastSeen(value: string | null) {
  if (!value) return "未活跃";
  const diffMs = Date.now() - new Date(value).getTime();
  const minutes = Math.max(0, Math.round(diffMs / 60_000));
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  return `${Math.round(hours / 24)} 天前`;
}

type UserConfirmAction =
  | { type: "sync" }
  | { type: "status"; id: string; nextStatus: "active" | "disabled"; email: string }
  | { type: "delete"; id: string; email: string };

export function UsersPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("all");
  const [syncState, setSyncState] = useState("all");
  const [groupId, setGroupId] = useState("");
  const [minBalance, setMinBalance] = useState("");
  const [maxBalance, setMaxBalance] = useState("");
  const [pageSize, setPageSize] = useState(50);
  const [page, setPage] = useState(0);
  const [notice, setNotice] = useState("");
  const [provisionOpen, setProvisionOpen] = useState(false);
  const [provisionMode, setProvisionMode] = useState<"create" | "import">("create");
  const [provisionEmail, setProvisionEmail] = useState("");
  const [provisionDisplayName, setProvisionDisplayName] = useState("");
  const [provisionPassword, setProvisionPassword] = useState("");
  const [provisionGroupId, setProvisionGroupId] = useState("");
  const [provisionSync, setProvisionSync] = useState(true);
  const [generatedPassword, setGeneratedPassword] = useState("");
  const [generatedAPIKey, setGeneratedAPIKey] = useState("");
  const [confirmAction, setConfirmAction] = useState<UserConfirmAction | null>(null);
  const gatewayGroups = useQuery({ queryKey: ["admin-gateway-groups"], queryFn: getGatewayGroups });
  const sub2APISettings = useQuery({ queryKey: ["admin-sub2api-settings"], queryFn: getSub2APISettings });
  const users = useQuery({
    queryKey: ["admin-users", search, status, syncState, groupId, minBalance, maxBalance, pageSize, page],
    queryFn: () =>
      getAdminUsers({
        search,
        status,
        sync: syncState,
        groupId,
        minBalance,
        maxBalance,
        limit: pageSize,
        offset: page * pageSize
      })
  });
  const syncUsers = useMutation({
    mutationFn: ({ auditReason }: { auditReason: string }) => syncSub2APIUsers({ auditReason }),
    onSuccess: async (result) => {
      setConfirmAction(null);
      setNotice(`同步完成：匹配 ${result.synced} 个，跳过 ${result.skipped} 个，余额修正 ${result.balanceAdjusted} 个。`);
      await queryClient.invalidateQueries({ queryKey: ["admin-users"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-usage-summary"] });
    }
  });
  const updateUser = useMutation({
    mutationFn: ({ id, nextStatus, auditReason }: { id: string; nextStatus: "active" | "disabled"; auditReason: string }) =>
      updateAdminUser(id, { status: nextStatus, cascadeDisableKeys: nextStatus === "disabled", auditReason }),
    onSuccess: async (result) => {
      setConfirmAction(null);
      setNotice(`用户状态已更新：${result.syncWarning}`);
      await queryClient.invalidateQueries({ queryKey: ["admin-users"] });
    }
  });
  const deleteUser = useMutation({
    mutationFn: ({ id, auditReason }: { id: string; auditReason: string }) => deleteAdminUser(id, { auditReason }),
    onSuccess: async () => {
      setConfirmAction(null);
      setNotice("本地用户已删除；如存在 Sub2API 账号，需要去 Sub2API 后台单独处理。");
      await queryClient.invalidateQueries({ queryKey: ["admin-users"] });
    }
  });
  const provisionUser = useMutation({
    mutationFn: () => {
      if (provisionMode === "import") {
        return importSub2APIUser({
          email: provisionEmail,
          displayName: provisionDisplayName,
          password: provisionPassword
        });
      }
      return createAdminUser({
        email: provisionEmail,
        displayName: provisionDisplayName,
        password: provisionPassword,
        syncSub2api: provisionSync,
        externalGroupId: provisionSync ? Number(selectedProvisionGroupId) || 0 : 0
      });
    },
    onSuccess: async (result) => {
      setGeneratedPassword(result.generatedPassword);
      setGeneratedAPIKey(result.plainApiKey ?? "");
      setNotice(
        result.managementWarning ||
          result.gatewayWarning ||
          `${provisionMode === "import" ? "已导入并绑定" : "用户已创建"}：${result.user.email}`
      );
      await queryClient.invalidateQueries({ queryKey: ["admin-users"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    }
  });
  const rows = users.data?.items ?? [];
  const total = users.data?.total ?? 0;
  const start = total === 0 ? 0 : page * pageSize + 1;
  const end = Math.min(total, page * pageSize + rows.length);
  const activeStandardGroups = (gatewayGroups.data?.items ?? []).filter(
    (group) => group.status === "active" && group.subscriptionType === "standard" && group.externalGroupId > 0
  );
  const configuredDefaultGroupId = sub2APISettings.data?.settings.defaultGroupId ?? 0;
  const fallbackProvisionGroupId =
    activeStandardGroups.find((group) => group.externalGroupId === configuredDefaultGroupId)?.externalGroupId ??
    activeStandardGroups[0]?.externalGroupId ??
    0;
  const selectedProvisionGroupId = provisionGroupId || String(fallbackProvisionGroupId || "");
  const canPrev = page > 0;
  const canNext = end < total;
  const isBusy = syncUsers.isPending || updateUser.isPending || deleteUser.isPending || provisionUser.isPending;
  const confirmPending = syncUsers.isPending || updateUser.isPending || deleteUser.isPending;
  const confirmCopy =
    confirmAction?.type === "sync"
      ? {
          title: "同步 Sub2API 用户",
          description: "会扫描 Sub2API 用户并更新 Brevyn 本地映射、余额修正等信息。请填写本次同步原因。",
          label: "确认同步"
        }
      : confirmAction?.type === "status"
        ? {
            title: confirmAction.nextStatus === "disabled" ? "禁用用户" : "启用用户",
            description:
              confirmAction.nextStatus === "disabled"
                ? `将禁用 ${confirmAction.email}，并级联禁用该用户 active API Key。`
                : `将启用 ${confirmAction.email}，已禁用的 API Key 不会自动恢复。`,
            label: confirmAction.nextStatus === "disabled" ? "确认禁用" : "确认启用"
          }
        : confirmAction?.type === "delete"
          ? {
              title: "删除本地用户",
              description: `只删除 Brevyn 本地用户 ${confirmAction.email}，不会删除 Sub2API 账号和 Key。`,
              label: "确认删除"
            }
          : null;
  const resetPage = () => setPage(0);
  const clearFilters = () => {
    setSearch("");
    setStatus("all");
    setSyncState("all");
    setGroupId("");
    setMinBalance("");
    setMaxBalance("");
    setPageSize(50);
    setPage(0);
  };
  const openProvision = () => {
    setProvisionOpen(true);
    setGeneratedPassword("");
    setProvisionEmail("");
    setProvisionDisplayName("");
    setProvisionPassword("");
    setProvisionGroupId("");
    setProvisionSync(true);
    setGeneratedAPIKey("");
  };
  const closeProvision = () => {
    setProvisionOpen(false);
    setGeneratedPassword("");
    setGeneratedAPIKey("");
  };

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Accounts"
        title="用户"
        description="查询 Brevyn 用户、影子网关账号、设备和余额。"
        actions={
          <div className="button-row">
            <button className="secondary-action" onClick={openProvision} type="button">
              <Plus size={16} />
              <span>新建 / 导入</span>
            </button>
            <button className="primary-action" disabled={syncUsers.isPending} onClick={() => setConfirmAction({ type: "sync" })} type="button">
              <RefreshCw size={16} />
              <span>{syncUsers.isPending ? "同步中" : "同步 Sub2API 用户"}</span>
            </button>
          </div>
        }
      />
      <section className="panel warning-panel">
        <AlertTriangle size={18} />
        <div>
          <strong>用户以 Brevyn Cloud 为主。</strong>
          <p>禁用用户会同步禁用该用户当前 active API Key；删除仍只删除 Brevyn 本地用户，网关侧账号请谨慎单独处理。</p>
        </div>
      </section>
      <section className="panel filter-panel">
        <div className="search-box full">
          <Search size={16} />
          <input
            onChange={(event) => {
              setSearch(event.target.value);
              resetPage();
            }}
            placeholder="搜索用户号、邮箱、Sub2API 邮箱、远端用户 ID、分组 ID"
            value={search}
          />
        </div>
        <div className="filter-grid">
          <label>
            <span>账号状态</span>
            <select
              onChange={(event) => {
                setStatus(event.target.value);
                resetPage();
              }}
              value={status}
            >
              <option value="all">全部状态</option>
              <option value="active">Active</option>
              <option value="disabled">Disabled</option>
            </select>
          </label>
          <label>
            <span>网关同步</span>
            <select
              onChange={(event) => {
                setSyncState(event.target.value);
                resetPage();
              }}
              value={syncState}
            >
              <option value="all">全部同步状态</option>
              <option value="synced">已同步</option>
              <option value="local_only">仅本地</option>
              <option value="gateway_disabled">网关非 active</option>
            </select>
          </label>
          <label>
            <span>分组 ID</span>
            <input
              inputMode="numeric"
              onChange={(event) => {
                setGroupId(event.target.value);
                resetPage();
              }}
              placeholder="例如 8"
              value={groupId}
            />
          </label>
          <label>
            <span>余额区间</span>
            <div className="range-inputs">
              <input
                inputMode="decimal"
                onChange={(event) => {
                  setMinBalance(event.target.value);
                  resetPage();
                }}
                placeholder="最低"
                value={minBalance}
              />
              <input
                inputMode="decimal"
                onChange={(event) => {
                  setMaxBalance(event.target.value);
                  resetPage();
                }}
                placeholder="最高"
                value={maxBalance}
              />
            </div>
          </label>
          <label>
            <span>每页</span>
            <select
              onChange={(event) => {
                setPageSize(Number(event.target.value));
                setPage(0);
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
      {notice ? <div className="panel inline-state">{notice}</div> : null}
      {users.isLoading ? <div className="panel inline-state">正在加载用户...</div> : null}
      {users.isError ? <div className="panel inline-state danger-text">用户加载失败。</div> : null}
      {syncUsers.isError || updateUser.isError || deleteUser.isError ? (
        <div className="panel inline-state danger-text">用户操作失败，请检查 Sub2API 设置或刷新后重试。</div>
      ) : null}
      <DataTable
        rows={rows}
        getRowKey={(row) => row.id}
        columns={[
          { key: "id", header: "用户号", render: (row) => <Link to={`/admin/users/${row.id}`}>{row.id}</Link> },
          { key: "email", header: "邮箱", render: (row) => row.email },
          {
            key: "status",
            header: "状态",
            render: (row) => <StatusBadge tone={row.status === "active" ? "ok" : "danger"}>{row.status}</StatusBadge>
          },
          { key: "balance", header: "余额", render: (row) => formatBalance(row.balance), align: "right" },
          { key: "group", header: "分组", render: (row) => row.defaultGroupId || "未分组" },
          {
            key: "sync",
            header: "同步",
            render: (row) => (
              <StatusBadge tone={row.gatewayEmail ? "ok" : "warn"}>{row.gatewayEmail ? "synced" : "local-only"}</StatusBadge>
            )
          },
          { key: "devices", header: "设备", render: (row) => row.deviceCount, align: "center" },
          { key: "lastSeen", header: "活跃", render: (row) => formatLastSeen(row.lastSeenAt), align: "right" },
          {
            key: "actions",
            header: "本地操作",
            render: (row) => (
              <div className="compact-actions">
                <button
                  className="secondary-action"
                  disabled={isBusy}
                  onClick={() => {
                    const nextStatus = row.status === "active" ? "disabled" : "active";
                    setConfirmAction({ type: "status", id: row.id, nextStatus, email: row.email });
                  }}
                  type="button"
                >
                  {row.status === "active" ? <Ban size={14} /> : <CheckCircle2 size={14} />}
                  <span>{row.status === "active" ? "禁用" : "启用"}</span>
                </button>
                <button
                  className="danger-action"
                  disabled={isBusy}
                  onClick={() => setConfirmAction({ type: "delete", id: row.id, email: row.email })}
                  type="button"
                >
                  <Trash2 size={14} />
                  <span>删除</span>
                </button>
              </div>
            )
          }
        ]}
      />
      <section className="pagination-row">
        <span>
          {start}-{end} / {total}
        </span>
        <div className="compact-actions">
          <button className="secondary-action" disabled={!canPrev || users.isFetching} onClick={() => setPage((value) => Math.max(0, value - 1))} type="button">
            <ChevronLeft size={15} />
            <span>上一页</span>
          </button>
          <button className="secondary-action" disabled={!canNext || users.isFetching} onClick={() => setPage((value) => value + 1)} type="button">
            <span>下一页</span>
            <ChevronRight size={15} />
          </button>
        </div>
      </section>
      {!users.isLoading && rows.length === 0 ? <div className="panel inline-state">暂无 Brevyn 用户。</div> : null}
      {provisionOpen ? (
        <div className="modal-backdrop" role="presentation">
          <section aria-modal="true" className="danger-confirm-modal user-provision-modal" role="dialog">
            <div className="modal-heading">
              <div className="modal-icon">
                <Plus size={18} />
              </div>
              <div>
                <h3>新建或导入用户</h3>
                <p>新建用户由 Brevyn 管理；导入模式用于绑定已经存在的 Sub2API 用户。</p>
              </div>
              <button aria-label="关闭" className="icon-button" onClick={closeProvision} type="button">
                <X size={16} />
              </button>
            </div>
            <div className="form-grid settings-form-grid">
              <label>
                <span>处理方式</span>
                <select onChange={(event) => setProvisionMode(event.target.value as "create" | "import")} value={provisionMode}>
                  <option value="create">新建 Brevyn 用户</option>
                  <option value="import">导入已有 Sub2API 用户</option>
                </select>
              </label>
              <label>
                <span>邮箱</span>
                <input onChange={(event) => setProvisionEmail(event.target.value)} placeholder="student@example.com" value={provisionEmail} />
              </label>
              <label>
                <span>显示名称</span>
                <input onChange={(event) => setProvisionDisplayName(event.target.value)} placeholder="可不填" value={provisionDisplayName} />
              </label>
              <label>
                <span>初始密码</span>
                <input onChange={(event) => setProvisionPassword(event.target.value)} placeholder="留空自动生成" type="password" value={provisionPassword} />
              </label>
              {provisionMode === "create" ? (
                <>
                  <label>
                    <span>指定余额分组</span>
                    <select
                      disabled={!provisionSync || gatewayGroups.isLoading}
                      onChange={(event) => setProvisionGroupId(event.target.value)}
                      value={selectedProvisionGroupId}
                    >
                      {activeStandardGroups.length === 0 ? <option value="">暂无 active standard 分组</option> : null}
                      {activeStandardGroups.map((group) => (
                        <option key={group.id} value={group.externalGroupId}>
                          {group.name} · #{group.externalGroupId}
                        </option>
                      ))}
                    </select>
                    <span className="field-hint">新账号只允许使用这一组，并自动创建对应 API Key。</span>
                  </label>
                  <label className="inline-checkbox">
                    <input checked={provisionSync} onChange={(event) => setProvisionSync(event.target.checked)} type="checkbox" />
                    <span>同时创建 Sub2API 影子账号</span>
                  </label>
                  <label>
                    <span>Sub2API 并发</span>
                    <input disabled value="5" />
                    <span className="field-hint">Brevyn 管理账号统一默认并发 5。</span>
                  </label>
                </>
              ) : null}
            </div>
            {generatedPassword ? (
              <div className="generated-secret">
                <span>初始密码仅本次展示</span>
                <code>{generatedPassword}</code>
                <button className="secondary-action" onClick={() => void navigator.clipboard.writeText(generatedPassword)} type="button">
                  <Copy size={15} />
                  <span>复制</span>
                </button>
              </div>
            ) : null}
            {generatedAPIKey ? (
              <div className="generated-secret">
                <span>Sub2API API Key 仅本次展示</span>
                <code>{generatedAPIKey}</code>
                <button className="secondary-action" onClick={() => void navigator.clipboard.writeText(generatedAPIKey)} type="button">
                  <Copy size={15} />
                  <span>复制</span>
                </button>
              </div>
            ) : null}
            {provisionUser.isError ? <div className="form-error">处理失败：{provisionUser.error.message}</div> : null}
            <div className="modal-footer">
              <span>{provisionMode === "import" ? "导入后请检查远端密码管理模式" : "邮箱将作为 Brevyn 登录账号"}</span>
              <div className="button-row">
                <button className="secondary-action" onClick={closeProvision} type="button">关闭</button>
                <button
                  className="primary-action"
                  disabled={
                    !provisionEmail.trim() ||
                    provisionUser.isPending ||
                    (provisionMode === "create" && provisionSync && Number(selectedProvisionGroupId) <= 0)
                  }
                  onClick={() => provisionUser.mutate()}
                  type="button"
                >
                  <span>{provisionUser.isPending ? "处理中" : provisionMode === "import" ? "导入并绑定" : "创建用户"}</span>
                </button>
              </div>
            </div>
          </section>
        </div>
      ) : null}
      {confirmCopy ? (
        <DangerConfirmModal
          open={Boolean(confirmAction)}
          title={confirmCopy.title}
          description={confirmCopy.description}
          confirmLabel={confirmCopy.label}
          pending={confirmPending}
          onCancel={() => setConfirmAction(null)}
          onConfirm={(auditReason) => {
            if (!confirmAction) return;
            if (confirmAction.type === "sync") syncUsers.mutate({ auditReason });
            if (confirmAction.type === "status") updateUser.mutate({ id: confirmAction.id, nextStatus: confirmAction.nextStatus, auditReason });
            if (confirmAction.type === "delete") deleteUser.mutate({ id: confirmAction.id, auditReason });
          }}
        />
      ) : null}
    </div>
  );
}
