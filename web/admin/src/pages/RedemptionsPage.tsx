import { FilterX, RotateCcw, Search } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { DataTable } from "../components/DataTable";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { PaginationRow } from "../components/PaginationRow";
import { StatusBadge } from "../components/StatusBadge";
import { getProducts, getRedeemBatches, getRedemptions, retryRedemptionSync } from "../api/client";

function formatBenefit(kind: string, value: number, validityDays: number) {
  if (kind === "subscription") return `${validityDays} 天`;
  return `$${value.toFixed(2)}`;
}

function redemptionTone(status: string) {
  if (status === "synced") return "ok";
  if (status === "gateway_failed") return "danger";
  return "warn";
}

function errorTone(errorClass: string) {
  if (errorClass === "transient" || errorClass === "rate_limited" || errorClass === "partial_success") return "warn";
  if (errorClass === "config_error" || errorClass === "auth_error" || errorClass === "version_error" || errorClass === "data_error") return "danger";
  return "neutral";
}

function errorClassLabel(errorClass: string) {
  const labels: Record<string, string> = {
    transient: "临时错误",
    rate_limited: "限流",
    config_error: "配置错误",
    auth_error: "鉴权错误",
    version_error: "接口版本",
    data_error: "数据错误",
    partial_success: "部分成功",
    unknown_gateway_error: "未知错误"
  };
  return labels[errorClass] ?? errorClass;
}

function operationTone(status: string) {
  if (status === "succeeded") return "ok";
  if (status === "dead_letter") return "danger";
  if (status === "failed") return "warn";
  return "neutral";
}

export function RedemptionsPage() {
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("all");
  const [type, setType] = useState("all");
  const [source, setSource] = useState("");
  const [productId, setProductId] = useState("all");
  const [batchId, setBatchId] = useState("all");
  const [user, setUser] = useState(searchParams.get("user") ?? "");
  const [errorClass, setErrorClass] = useState("all");
  const [retryable, setRetryable] = useState("all");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [pageSize, setPageSize] = useState(50);
  const [offset, setOffset] = useState(0);
  const [retryTarget, setRetryTarget] = useState<{ id: string; label: string } | null>(null);
  const resetOffset = () => setOffset(0);
  const clearFilters = () => {
    setSearch("");
    setStatus("all");
    setType("all");
    setSource("");
    setProductId("all");
    setBatchId("all");
    setUser("");
    setErrorClass("all");
    setRetryable("all");
    setDateFrom("");
    setDateTo("");
    setPageSize(50);
    setOffset(0);
  };
  const products = useQuery({ queryKey: ["admin-products"], queryFn: getProducts });
  const batches = useQuery({
    queryKey: ["admin-redeem-batch-options"],
    queryFn: () => getRedeemBatches({ limit: 300, offset: 0 })
  });
  const redemptions = useQuery({
    queryKey: [
      "admin-redemptions",
      search,
      status,
      type,
      source,
      productId,
      batchId,
      user,
      errorClass,
      retryable,
      dateFrom,
      dateTo,
      pageSize,
      offset
    ],
    queryFn: () =>
      getRedemptions({
        search,
        status,
        type,
        source,
        productId,
        batchId,
        user,
        errorClass,
        retryable,
        dateFrom,
        dateTo,
        limit: pageSize,
        offset
      })
  });
  const retrySync = useMutation({
    mutationFn: ({ id, auditReason }: { id: string; auditReason: string }) => retryRedemptionSync(id, { auditReason }),
    onSettled: () => {
      setRetryTarget(null);
      void queryClient.invalidateQueries({ queryKey: ["admin-redemptions"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-usage-summary"] });
    }
  });

  return (
    <div className="page-stack">
      <PageHeader eyebrow="Ledger" title="兑换记录" description="Brevyn 自有卡密兑换后的到账、网关同步和幂等状态。" />
      <section className="panel filter-panel">
        <div className="search-box full">
          <Search size={16} />
          <input
            onChange={(event) => {
              setSearch(event.target.value);
              resetOffset();
            }}
            placeholder="搜索记录、卡密、用户邮箱、商品、批次、订单号、错误"
            value={search}
          />
        </div>
        <div className="filter-grid">
          <label>
            <span>同步状态</span>
            <select
              onChange={(event) => {
                setStatus(event.target.value);
                resetOffset();
              }}
              value={status}
            >
              <option value="all">全部状态</option>
              <option value="synced">Synced</option>
              <option value="pending_gateway">Pending</option>
              <option value="gateway_failed">Failed</option>
            </select>
          </label>
          <label>
            <span>类型</span>
            <select
              onChange={(event) => {
                setType(event.target.value);
                resetOffset();
              }}
              value={type}
            >
              <option value="all">全部类型</option>
              <option value="balance">Balance</option>
              <option value="subscription">Subscription</option>
            </select>
          </label>
          <label>
            <span>来源</span>
            <input
              onChange={(event) => {
                setSource(event.target.value);
                resetOffset();
              }}
              placeholder="ldxp / manual"
              value={source}
            />
          </label>
          <label>
            <span>商品</span>
            <select
              onChange={(event) => {
                setProductId(event.target.value);
                resetOffset();
              }}
              value={productId}
            >
              <option value="all">全部商品</option>
              {(products.data?.items ?? []).map((product) => (
                <option key={product.id} value={product.id}>
                  {product.name} · {product.sku}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>批次</span>
            <select
              onChange={(event) => {
                setBatchId(event.target.value);
                resetOffset();
              }}
              value={batchId}
            >
              <option value="all">全部批次</option>
              {(batches.data?.items ?? []).map((batch) => (
                <option key={batch.id} value={batch.id}>
                  {batch.name} · {batch.productName || "未关联"}
                </option>
              ))}
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
      {redemptions.isLoading ? <div className="panel inline-state">正在加载兑换记录...</div> : null}
      {redemptions.isError ? <div className="panel inline-state danger-text">兑换记录加载失败。</div> : null}
      {retrySync.isError ? <div className="panel inline-state danger-text">重试失败：{retrySync.error.message}</div> : null}
      <DataTable
        rows={redemptions.data?.items ?? []}
        getRowKey={(row) => row.id}
        columns={[
          { key: "id", header: "记录", render: (row) => row.id },
          { key: "code", header: "卡密", render: (row) => row.redeemCodeId },
          { key: "user", header: "用户", render: (row) => row.userId || row.userEmail },
          { key: "product", header: "商品", render: (row) => row.productName || "-" },
          { key: "batch", header: "批次", render: (row) => row.batchName || "-" },
          { key: "orderRef", header: "订单号", render: (row) => row.orderRef || "-" },
          { key: "value", header: "到账", render: (row) => formatBenefit(row.kind, row.value, row.validityDays), align: "right" },
          { key: "gateway", header: "网关", render: (row) => row.gatewayOperation || "pending" },
          {
            key: "operation",
            header: "队列",
            render: (row) =>
              row.operationStatus ? (
                <div className="queue-cell">
                  <StatusBadge tone={operationTone(row.operationStatus)}>{row.operationStatus}</StatusBadge>
                  <span>
                    {row.operationAttempts}/{row.operationMaxAttempts || 8}
                  </span>
                </div>
              ) : (
                "-"
              )
          },
          {
            key: "status",
            header: "状态",
            render: (row) => <StatusBadge tone={redemptionTone(row.status)}>{row.status}</StatusBadge>
          },
          {
            key: "error",
            header: "错误",
            render: (row) =>
              row.errorMessage ? (
                <div className="error-cell" title={row.errorDetail || row.errorMessage}>
                  {row.errorClass ? (
                    <StatusBadge tone={errorTone(row.errorClass)}>{errorClassLabel(row.errorClass)}</StatusBadge>
                  ) : null}
                  <span className="clipped-cell">{row.errorMessage}</span>
                  <code>{row.errorStage || row.errorCode || "gateway"}</code>
                  {row.errorRetryable ? <span className="retryable-text">可重试</span> : null}
                </div>
              ) : (
                "-"
              )
          },
          {
            key: "actions",
            header: "操作",
            align: "right",
            render: (row) =>
              row.status === "gateway_failed" || row.status === "pending_gateway" ? (
                <button
                  className="secondary-action"
                  disabled={retrySync.isPending}
                  type="button"
                  onClick={() => setRetryTarget({ id: row.id, label: row.productName || row.id })}
                >
                  <RotateCcw size={15} />
                  <span>{retrySync.isPending ? "同步中" : "重试同步"}</span>
                </button>
              ) : (
                <span className="clipped-cell">已处理</span>
              )
          },
          { key: "time", header: "时间", render: (row) => new Date(row.createdAt).toLocaleString(), align: "right" }
        ]}
      />
      <PaginationRow
        total={redemptions.data?.total ?? 0}
        limit={pageSize}
        offset={offset}
        isFetching={redemptions.isFetching}
        onOffsetChange={setOffset}
      />
      <DangerConfirmModal
        open={Boolean(retryTarget)}
        title="重试兑换同步"
        description={`将重新同步 ${retryTarget?.label ?? ""} 到 Sub2API。若上次是部分成功，系统会按幂等逻辑继续补齐。`}
        confirmLabel="确认重试"
        pending={retrySync.isPending}
        onCancel={() => setRetryTarget(null)}
        onConfirm={(auditReason) => {
          if (retryTarget) retrySync.mutate({ id: retryTarget.id, auditReason });
        }}
      />
    </div>
  );
}
