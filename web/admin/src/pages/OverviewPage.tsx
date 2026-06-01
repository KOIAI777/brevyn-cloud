import { Activity, CircleDollarSign, KeyRound, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "../components/PageHeader";
import { StatGrid } from "../components/StatGrid";
import { DataTable } from "../components/DataTable";
import { StatusBadge } from "../components/StatusBadge";
import { useServiceHealth } from "../state/useServiceHealth";
import { getAdminOverview } from "../api/client";

function formatUSD(value: number) {
  return `$${value.toFixed(2)}`;
}

function formatBenefit(kind: string, value: number, validityDays: number) {
  if (kind === "subscription") return `${validityDays} 天`;
  return formatUSD(value);
}

export function OverviewPage() {
  const health = useServiceHealth();
  const overview = useQuery({ queryKey: ["admin-overview"], queryFn: getAdminOverview });
  const ready = health.ready.data?.status === "ready";
  const summary = overview.data?.summary;
  const recentRedemptions = overview.data?.recentRedemptions ?? [];

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Operations"
        title="总览"
        description="用户、余额、兑换、网关状态的日常巡检入口。"
      />
      <StatGrid
        stats={[
          {
            label: "活跃用户",
            value: String(summary?.activeUsers ?? 0),
            delta: `${summary?.usersToday ?? 0} 今日新增 / ${summary?.totalUsers ?? 0} 总用户`,
            tone: "green",
            icon: Users
          },
          {
            label: "余额池",
            value: formatUSD(summary?.walletBalanceUsd ?? 0),
            delta: "Brevyn 钱包账本",
            tone: "cyan",
            icon: CircleDollarSign
          },
          {
            label: "设备 Key",
            value: String(summary?.activeKeys ?? 0),
            delta: `${summary?.reviewKeys ?? 0} 个待复核`,
            tone: (summary?.reviewKeys ?? 0) > 0 ? "amber" : "green",
            icon: KeyRound
          },
          {
            label: "今日请求",
            value: String(summary?.requestCountToday ?? 0),
            delta: `${formatUSD(summary?.costTodayUsd ?? 0)} 今日上游成本 / 兑换 ${summary?.redemptionsToday ?? 0} 次`,
            tone: summary?.usageStatus === "synced" ? "cyan" : "amber",
            icon: Activity
          }
        ]}
      />
      {overview.isLoading ? <div className="panel inline-state">正在加载总览数据...</div> : null}
      {overview.isError ? <div className="panel inline-state danger-text">总览数据加载失败。</div> : null}

      <section className="split-grid">
        <article className="panel">
          <div className="panel-heading">
            <h3>服务状态</h3>
            <StatusBadge tone={ready ? "ok" : "warn"}>{ready ? "Ready" : "Pending"}</StatusBadge>
          </div>
          <div className="health-list">
            <div>
              <span>API</span>
              <strong>{health.live.data?.status ?? "checking"}</strong>
            </div>
            <div>
              <span>Postgres / Redis</span>
              <strong>{health.ready.data?.status ?? "checking"}</strong>
            </div>
            <div>
              <span>Admin Surface</span>
              <strong>{health.admin.data?.surface ?? "checking"}</strong>
            </div>
          </div>
        </article>

        <article className="panel">
          <div className="panel-heading">
            <h3>最新兑换</h3>
            <StatusBadge tone="neutral">{String(recentRedemptions.length)}</StatusBadge>
          </div>
          <DataTable
            rows={recentRedemptions}
            getRowKey={(row) => row.id}
            columns={[
              { key: "user", header: "用户", render: (row) => row.userEmail || row.userId },
              { key: "product", header: "商品", render: (row) => row.productName || "-" },
              { key: "value", header: "到账", render: (row) => formatBenefit(row.kind, row.value, row.validityDays) },
              { key: "time", header: "时间", render: (row) => new Date(row.createdAt).toLocaleString(), align: "right" }
            ]}
          />
          {!overview.isLoading && recentRedemptions.length === 0 ? (
            <p className="inline-state">暂无真实兑换记录。</p>
          ) : null}
        </article>
      </section>
    </div>
  );
}
