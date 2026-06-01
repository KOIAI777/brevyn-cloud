import { AlertTriangle, FilterX, RefreshCw, RotateCcw, Search } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { DataTable } from "../components/DataTable";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { PaginationRow } from "../components/PaginationRow";
import { StatusBadge } from "../components/StatusBadge";
import { getGatewayOperations, retryGatewayOperation } from "../api/client";

function operationTone(status: string) {
  if (status === "succeeded") return "ok";
  if (status === "dead_letter") return "danger";
  if (status === "failed" || status === "running") return "warn";
  return "neutral";
}

function errorTone(errorClass: string) {
  if (errorClass === "transient" || errorClass === "rate_limited" || errorClass === "partial_success") return "warn";
  if (errorClass) return "danger";
  return "neutral";
}

function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

function canRetry(status: string) {
  return status === "failed" || status === "dead_letter" || status === "pending";
}

export function GatewayOperationsPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("all");
  const [operation, setOperation] = useState("all");
  const [errorClass, setErrorClass] = useState("all");
  const [retryable, setRetryable] = useState("all");
  const [user, setUser] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [pageSize, setPageSize] = useState(50);
  const [offset, setOffset] = useState(0);
  const [notice, setNotice] = useState("");
  const [retryTarget, setRetryTarget] = useState<{ id: string; label: string } | null>(null);
  const resetOffset = () => setOffset(0);
  const clearFilters = () => {
    setSearch("");
    setStatus("all");
    setOperation("all");
    setErrorClass("all");
    setRetryable("all");
    setUser("");
    setDateFrom("");
    setDateTo("");
    setPageSize(50);
    setOffset(0);
  };
  const operations = useQuery({
    queryKey: ["admin-gateway-operations", search, status, operation, errorClass, retryable, user, dateFrom, dateTo, pageSize, offset],
    queryFn: () =>
      getGatewayOperations({
        search,
        status,
        operation,
        errorClass,
        retryable,
        user,
        dateFrom,
        dateTo,
        limit: pageSize,
        offset
      })
  });
  const retry = useMutation({
    mutationFn: ({ id, auditReason }: { id: string; auditReason: string }) => retryGatewayOperation(id, { auditReason }),
    onSuccess: (result) => {
      setRetryTarget(null);
      setNotice(`已重新入队：${result.operation.id}，worker 会按 next_run_at 继续处理。`);
      void queryClient.invalidateQueries({ queryKey: ["admin-gateway-operations"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-redemptions"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-audit-logs"] });
    }
  });
  const rows = operations.data?.items ?? [];

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Gateway Queue"
        title="同步队列"
        description="查看 Brevyn Cloud 写入 Sub2API 的后台任务、失败原因、重试次数和死信状态。"
        actions={
          <button className="secondary-action" disabled={operations.isFetching} onClick={() => void operations.refetch()} type="button">
            <RefreshCw size={16} />
            <span>{operations.isFetching ? "刷新中" : "刷新"}</span>
          </button>
        }
      />
      <section className="panel warning-panel">
        <AlertTriangle size={18} />
        <div>
          <strong>这里是同步任务，不是模型请求日志。</strong>
          <p>兑换、赠送、Key 创建等写入网关的动作会进入队列；失败后可以在这里重新入队，worker 会自动继续处理。</p>
        </div>
      </section>
      <section className="panel filter-panel">
        <div className="search-box full">
          <Search size={16} />
          <input
            onChange={(event) => {
              setSearch(event.target.value);
              resetOffset();
            }}
            placeholder="搜索任务 ID、兑换记录、用户、错误、幂等键"
            value={search}
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
              <option value="pending">Pending</option>
              <option value="running">Running</option>
              <option value="failed">Failed</option>
              <option value="dead_letter">Dead letter</option>
              <option value="succeeded">Succeeded</option>
            </select>
          </label>
          <label>
            <span>任务</span>
            <select
              onChange={(event) => {
                setOperation(event.target.value);
                resetOffset();
              }}
              value={operation}
            >
              <option value="all">全部任务</option>
              <option value="sync_redemption">sync_redemption</option>
              <option value="sync_admin_balance_grant">sync_admin_balance_grant</option>
              <option value="disable_api_key_remote">disable_api_key_remote</option>
            </select>
          </label>
          <label>
            <span>错误类型</span>
            <select
              onChange={(event) => {
                setErrorClass(event.target.value);
                resetOffset();
              }}
              value={errorClass}
            >
              <option value="all">全部错误</option>
              <option value="transient">临时错误</option>
              <option value="rate_limited">限流</option>
              <option value="config_error">配置错误</option>
              <option value="auth_error">鉴权错误</option>
              <option value="version_error">接口版本</option>
              <option value="data_error">数据错误</option>
              <option value="partial_success">部分成功</option>
              <option value="unknown_gateway_error">未知错误</option>
            </select>
          </label>
          <label>
            <span>可重试</span>
            <select
              onChange={(event) => {
                setRetryable(event.target.value);
                resetOffset();
              }}
              value={retryable}
            >
              <option value="all">全部</option>
              <option value="true">可重试</option>
              <option value="false">不可重试</option>
            </select>
          </label>
          <label>
            <span>用户</span>
            <input
              onChange={(event) => {
                setUser(event.target.value);
                resetOffset();
              }}
              placeholder="邮箱 / 用户 ID"
              value={user}
            />
          </label>
          <label>
            <span>开始日期</span>
            <input
              onChange={(event) => {
                setDateFrom(event.target.value);
                resetOffset();
              }}
              type="date"
              value={dateFrom}
            />
          </label>
          <label>
            <span>结束日期</span>
            <input
              onChange={(event) => {
                setDateTo(event.target.value);
                resetOffset();
              }}
              type="date"
              value={dateTo}
            />
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
      {notice ? <div className="panel inline-state">{notice}</div> : null}
      {operations.isLoading ? <div className="panel inline-state">正在加载同步队列...</div> : null}
      {operations.isError ? <div className="panel inline-state danger-text">同步队列加载失败。</div> : null}
      {retry.isError ? <div className="panel inline-state danger-text">重新入队失败：{retry.error.message}</div> : null}
      <div className="wide-table operations-table">
        <DataTable
          rows={rows}
          getRowKey={(row) => row.id}
          columns={[
            {
              key: "operation",
              header: "任务",
              render: (row) => (
                <div className="audit-event">
                  <strong>{row.operation}</strong>
                  <code>{row.id}</code>
                </div>
              )
            },
            {
              key: "status",
              header: "状态",
              render: (row) => <StatusBadge tone={operationTone(row.status)}>{row.status}</StatusBadge>
            },
            { key: "target", header: "对象", render: (row) => `${row.targetType}:${row.targetId || row.redemptionId || "-"}` },
            { key: "user", header: "用户", render: (row) => row.userEmail || row.userId || "-" },
            {
              key: "attempts",
              header: "次数",
              render: (row) => `${row.attempts}/${row.maxAttempts}`,
              align: "right"
            },
            { key: "next", header: "下次执行", render: (row) => formatDate(row.nextRunAt), align: "right" },
            {
              key: "error",
              header: "错误",
              render: (row) =>
                row.lastErrorMessage ? (
                  <div className="error-cell" title={row.lastErrorDetail || row.lastErrorMessage}>
                    {row.lastErrorClass ? <StatusBadge tone={errorTone(row.lastErrorClass)}>{row.lastErrorClass}</StatusBadge> : null}
                    <span className="clipped-cell">{row.lastErrorMessage}</span>
                    <code>{row.lastErrorStage || row.lastErrorCode || "gateway"}</code>
                    {row.lastErrorRetryable ? <span className="retryable-text">可重试</span> : null}
                  </div>
                ) : (
                  "-"
                )
            },
            {
              key: "lock",
              header: "锁",
              render: (row) => (row.lockedAt ? `${row.lockedBy || "worker"} · ${formatDate(row.lockedAt)}` : "-")
            },
            {
              key: "actions",
              header: "操作",
              align: "right",
              render: (row) =>
                canRetry(row.status) ? (
                  <button
                    className="secondary-action"
                    disabled={retry.isPending}
                    onClick={() => setRetryTarget({ id: row.id, label: `${row.operation} · ${row.id}` })}
                    type="button"
                  >
                    <RotateCcw size={15} />
                    <span>{retry.isPending ? "入队中" : "重新入队"}</span>
                  </button>
                ) : (
                  <span className="clipped-cell">无需操作</span>
                )
            },
            { key: "created", header: "创建时间", render: (row) => formatDate(row.createdAt), align: "right" }
          ]}
        />
      </div>
      <PaginationRow
        total={operations.data?.total ?? 0}
        limit={pageSize}
        offset={offset}
        isFetching={operations.isFetching}
        onOffsetChange={setOffset}
      />
      {!operations.isLoading && rows.length === 0 ? <div className="panel inline-state">暂无同步任务。</div> : null}
      <DangerConfirmModal
        open={Boolean(retryTarget)}
        title="重新入队同步任务"
        description={`将 ${retryTarget?.label ?? ""} 重新放入 worker 队列，之后会继续调用 Sub2API。`}
        confirmLabel="确认入队"
        pending={retry.isPending}
        onCancel={() => setRetryTarget(null)}
        onConfirm={(auditReason) => {
          if (retryTarget) retry.mutate({ id: retryTarget.id, auditReason });
        }}
      />
    </div>
  );
}
