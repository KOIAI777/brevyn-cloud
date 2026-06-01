import { useQuery } from "@tanstack/react-query";
import { FilterX, RefreshCw, Search } from "lucide-react";
import { useState } from "react";
import { DataTable } from "../components/DataTable";
import { PageHeader } from "../components/PageHeader";
import { PaginationRow } from "../components/PaginationRow";
import { StatusBadge } from "../components/StatusBadge";
import { getAuditLogs } from "../api/client";

function resultLabel(tone: string) {
  if (tone === "danger") return "failed";
  if (tone === "warn") return "warning";
  return "recorded";
}

export function AuditLogsPage() {
  const [search, setSearch] = useState("");
  const [action, setAction] = useState("all");
  const [actorType, setActorType] = useState("all");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [pageSize, setPageSize] = useState(100);
  const [offset, setOffset] = useState(0);
  const resetOffset = () => setOffset(0);
  const clearFilters = () => {
    setSearch("");
    setAction("all");
    setActorType("all");
    setDateFrom("");
    setDateTo("");
    setPageSize(100);
    setOffset(0);
  };
  const auditLogs = useQuery({
    queryKey: ["admin-audit-logs", search, action, actorType, dateFrom, dateTo, pageSize, offset],
    queryFn: () =>
      getAuditLogs({
        search,
        action,
        actorType,
        dateFrom,
        dateTo,
        limit: pageSize,
        offset
      })
  });
  const rows = auditLogs.data?.items ?? [];

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Audit"
        title="审计日志"
        description="管理员、系统任务、余额和 Key 操作记录。"
        actions={
          <button className="secondary-action" disabled={auditLogs.isFetching} onClick={() => void auditLogs.refetch()} type="button">
            <RefreshCw size={16} />
            <span>{auditLogs.isFetching ? "刷新中" : "刷新"}</span>
          </button>
        }
      />
      <section className="panel filter-panel">
        <div className="search-box full">
          <Search size={16} />
          <input
            onChange={(event) => {
              setSearch(event.target.value);
              resetOffset();
            }}
            placeholder="搜索操作者、事件、对象、IP、元数据"
            value={search}
          />
        </div>
        <div className="filter-grid">
          <label>
            <span>事件</span>
            <select
              onChange={(event) => {
                setAction(event.target.value);
                resetOffset();
              }}
              value={action}
            >
              <option value="all">全部事件</option>
              <option value="admin.login">管理员登录</option>
              <option value="admin.totp.enable">启用二步验证</option>
              <option value="admin.totp.disable">关闭二步验证</option>
              <option value="user.create">创建用户</option>
              <option value="user.import_sub2api">导入 Sub2API 用户</option>
              <option value="user.balance_grant">赠送余额</option>
              <option value="user.local_update">更新用户</option>
              <option value="user.local_delete">删除用户</option>
              <option value="gateway_api_key.rotate">轮换 Key</option>
              <option value="gateway_api_key.disable">禁用 Key</option>
              <option value="redemption.retry_sync">重试同步</option>
              <option value="subscription.assign">分配订阅</option>
              <option value="subscription.extend">调整订阅天数</option>
              <option value="subscription.reset_quota">重置订阅额度</option>
              <option value="subscription.revoke">撤销订阅</option>
              <option value="gateway_operation.retry">任务重新入队</option>
              <option value="gateway_operation.retry_failed">批量重试失败任务</option>
              <option value="redeem_codes.generate">生成卡密</option>
              <option value="product.create">创建商品</option>
              <option value="product.update">更新商品</option>
              <option value="product.archive">归档商品</option>
              <option value="sub2api.users.sync">同步用户</option>
              <option value="sub2api.groups.sync">同步分组</option>
              <option value="sub2api.models.sync">同步模型</option>
              <option value="sub2api.settings.update">更新设置</option>
            </select>
          </label>
          <label>
            <span>操作者</span>
            <select
              onChange={(event) => {
                setActorType(event.target.value);
                resetOffset();
              }}
              value={actorType}
            >
              <option value="all">全部操作者</option>
              <option value="admin">Admin</option>
              <option value="user">User</option>
              <option value="system">System</option>
            </select>
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
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={200}>200</option>
              <option value={300}>300</option>
            </select>
          </label>
          <button className="secondary-action" onClick={clearFilters} type="button">
            <FilterX size={15} />
            <span>清空筛选</span>
          </button>
        </div>
      </section>
      {auditLogs.isLoading ? <div className="panel inline-state">正在加载审计日志...</div> : null}
      {auditLogs.isError ? <div className="panel inline-state danger-text">审计日志加载失败。</div> : null}
      <div className="wide-table audit-log-table">
        <DataTable
          rows={rows}
          getRowKey={(row) => row.id}
          columns={[
            {
              key: "action",
              header: "事件",
              render: (row) => (
                <div className="audit-event">
                  <strong>{row.actionLabel || row.action}</strong>
                  <code>{row.action}</code>
                </div>
              )
            },
            {
              key: "summary",
              header: "摘要",
              render: (row) => (
                <span className="audit-summary" title={row.summary || row.metadata}>
                  {row.summary || row.metadata || "-"}
                </span>
              )
            },
            { key: "actor", header: "操作者", render: (row) => row.actorLabel },
            { key: "target", header: "对象", render: (row) => `${row.targetType}:${row.targetId}` },
            { key: "ip", header: "IP", render: (row) => row.ip || "-" },
            { key: "time", header: "时间", render: (row) => new Date(row.createdAt).toLocaleString() },
            {
              key: "result",
              header: "结果",
              render: (row) => <StatusBadge tone={row.resultTone === "danger" || row.resultTone === "warn" ? row.resultTone : "ok"}>{resultLabel(row.resultTone)}</StatusBadge>,
              align: "right"
            }
          ]}
        />
      </div>
      <PaginationRow
        total={auditLogs.data?.total ?? 0}
        limit={pageSize}
        offset={offset}
        isFetching={auditLogs.isFetching}
        onOffsetChange={setOffset}
      />
      {!auditLogs.isLoading && rows.length === 0 ? <div className="panel inline-state">暂无真实审计日志。</div> : null}
    </div>
  );
}
