import { Activity, CircleDollarSign, Layers3, TrendingUp, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { DataTable } from "../components/DataTable";
import { PageHeader } from "../components/PageHeader";
import { StatGrid } from "../components/StatGrid";
import { StatusBadge } from "../components/StatusBadge";
import { getUsageSummary } from "../api/client";

function formatUSD(value: number) {
  return `$${value.toFixed(2)}`;
}

function formatCNY(value: number) {
  return `¥${value.toFixed(2)}`;
}

function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

function usageStatusLabel(status?: string) {
  if (status === "synced") return "已接入";
  if (status === "sync_error") return "同步失败";
  if (status === "not_synced") return "未接入";
  return "检查中";
}

function usageStatusTone(status?: string) {
  if (status === "synced") return "ok";
  if (status === "sync_error") return "danger";
  return "warn";
}

export function UsagePage() {
  const usage = useQuery({ queryKey: ["admin-usage-summary"], queryFn: getUsageSummary });
  const data = usage.data;
  const status = data?.usage.status;
  const grossMarginEstimate = (data?.ledger.balanceRedeemedTodayUsd ?? 0) - (data?.usage.actualCostUsd ?? data?.usage.costTodayUsd ?? 0);

  return (
    <div className="page-stack">
      <PageHeader eyebrow="Usage" title="用量和成本" description="从 Sub2API 同步请求、成本、余额扣减和异常峰值。" />
      <StatGrid
        stats={[
          {
            label: "今日请求",
            value: String(data?.usage.requestCountToday ?? 0),
            delta: status === "synced" ? "来自 Sub2API 实时统计" : status === "sync_error" ? "Sub2API 同步失败" : "Sub2API 用量同步未接入",
            tone: status === "synced" ? "green" : "amber",
            icon: Activity
          },
          {
            label: "今日成本",
            value: formatUSD(data?.usage.costTodayUsd ?? 0),
            delta: `${(data?.usage.inputTokensToday ?? 0).toLocaleString()} input tokens`,
            tone: "amber",
            icon: CircleDollarSign
          },
          {
            label: "今日到账",
            value: formatUSD(data?.ledger.balanceRedeemedTodayUsd ?? 0),
            delta: `${data?.ledger.redemptionCountToday ?? 0} 次兑换`,
            tone: "cyan",
            icon: TrendingUp
          },
          {
            label: "毛利估算",
            value: formatUSD(grossMarginEstimate),
            delta: "余额到账 - 今日上游成本",
            tone: grossMarginEstimate >= 0 ? "green" : "red",
            icon: Layers3
          }
        ]}
      />
      {usage.isLoading ? <div className="panel inline-state">正在加载用量账本...</div> : null}
      {usage.isError ? <div className="panel inline-state danger-text">用量账本加载失败。</div> : null}
      <section className="panel empty-state">
        <div className="panel-heading">
          <h3>真实数据状态</h3>
          <StatusBadge tone={usageStatusTone(status)}>{usageStatusLabel(status)}</StatusBadge>
        </div>
        <dl className="kv-list">
          <dt>钱包余额</dt>
          <dd>{formatUSD(data?.ledger.walletBalanceUsd ?? 0)}</dd>
          <dt>今日钱包入账</dt>
          <dd>{formatUSD(data?.ledger.walletCreditsTodayUsd ?? 0)}</dd>
          <dt>累计余额兑换</dt>
          <dd>{formatUSD(data?.ledger.balanceRedeemedTotalUsd ?? 0)}</dd>
          <dt>今日订阅兑换</dt>
          <dd>{data?.ledger.subscriptionCountToday ?? 0} 次</dd>
          <dt>今日网关失败</dt>
          <dd>{data?.ledger.gatewayFailedToday ?? 0} 次</dd>
        </dl>
        <p>
          请求量、token 和上游成本来自 Sub2API admin usage/stats；钱包、兑换和商品归因来自 Brevyn Cloud 自有账本。当前 Sub2API
          只返回总用量，所以利润先做总估算，不伪装成每用户精确成本。
        </p>
      </section>

      <section className="split-grid">
        <article className="panel">
          <div className="panel-heading">
            <h3>今日商品归因</h3>
            <StatusBadge tone="neutral">{`${data?.attribution.products.length ?? 0} products`}</StatusBadge>
          </div>
          <DataTable
            rows={data?.attribution.products ?? []}
            getRowKey={(row) => row.productId || `${row.name}-${row.benefitType}`}
            columns={[
              {
                key: "product",
                header: "商品",
                render: (row) => (
                  <div className="audit-event">
                    <strong>{row.name}</strong>
                    <code>{row.sku || row.benefitType}</code>
                  </div>
                )
              },
              { key: "count", header: "兑换", render: (row) => row.redeemedCount, align: "right" },
              { key: "value", header: "额度", render: (row) => formatUSD(row.balanceValueUsd), align: "right" },
              { key: "subs", header: "套餐", render: (row) => `${row.subscriptionCount} 次`, align: "right" },
              { key: "revenue", header: "销售额", render: (row) => formatCNY(row.revenueCny), align: "right" }
            ]}
          />
          {!usage.isLoading && (data?.attribution.products.length ?? 0) === 0 ? <div className="inline-state">今日暂无商品兑换。</div> : null}
        </article>

        <article className="panel">
          <div className="panel-heading">
            <h3>今日分组归因</h3>
            <StatusBadge tone="neutral">{`${data?.attribution.groups.length ?? 0} groups`}</StatusBadge>
          </div>
          <DataTable
            rows={data?.attribution.groups ?? []}
            getRowKey={(row) => String(row.externalGroupId)}
            columns={[
              {
                key: "group",
                header: "分组",
                render: (row) => (
                  <div className="audit-event">
                    <strong>{row.name}</strong>
                    <code>{row.externalGroupId || "-"} · {row.subscriptionType}</code>
                  </div>
                )
              },
              { key: "count", header: "兑换", render: (row) => row.redeemedCount, align: "right" },
              { key: "value", header: "额度", render: (row) => formatUSD(row.balanceValueUsd), align: "right" },
              { key: "subs", header: "套餐", render: (row) => row.subscriptionCount, align: "right" },
              { key: "keys", header: "Active Key", render: (row) => row.activeKeyCount, align: "right" }
            ]}
          />
          {!usage.isLoading && (data?.attribution.groups.length ?? 0) === 0 ? <div className="inline-state">今日暂无分组兑换。</div> : null}
        </article>
      </section>

      <section className="panel">
        <div className="panel-heading">
          <h3>今日用户归因</h3>
          <StatusBadge tone="neutral">{`${data?.attribution.users.length ?? 0} users`}</StatusBadge>
        </div>
        <DataTable
          rows={data?.attribution.users ?? []}
          getRowKey={(row) => row.userId}
          columns={[
            {
              key: "user",
              header: "用户",
              render: (row) => (
                <div className="audit-event">
                  <strong>{row.email}</strong>
                  <code>{row.userId}</code>
                </div>
              )
            },
            { key: "wallet", header: "余额", render: (row) => formatUSD(row.walletBalanceUsd), align: "right" },
            { key: "count", header: "兑换", render: (row) => row.redeemedCount, align: "right" },
            { key: "value", header: "额度", render: (row) => formatUSD(row.balanceValueUsd), align: "right" },
            { key: "subs", header: "套餐", render: (row) => row.subscriptionCount, align: "right" },
            { key: "failed", header: "失败", render: (row) => row.gatewayFailedCount, align: "right" },
            { key: "last", header: "最后兑换", render: (row) => formatDate(row.lastRedeemedAt), align: "right" }
          ]}
        />
        {!usage.isLoading && (data?.attribution.users.length ?? 0) === 0 ? <div className="inline-state">今日暂无用户兑换。</div> : null}
      </section>
    </div>
  );
}
