import { Activity, CircleDollarSign, TrendingUp } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "../components/PageHeader";
import { StatGrid } from "../components/StatGrid";
import { StatusBadge } from "../components/StatusBadge";
import { getUsageSummary } from "../api/client";

function formatUSD(value: number) {
  return `$${value.toFixed(2)}`;
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
          请求量、token 和上游成本来自 Sub2API admin usage/stats；钱包和兑换数据来自 Brevyn Cloud 自有账本。
        </p>
      </section>
    </div>
  );
}
