import { Activity, CheckCircle2, Clock, Database, RefreshCw, RotateCcw, Server, ShieldCheck, Wifi, Zap } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { StatGrid } from "../components/StatGrid";
import { StatusBadge } from "../components/StatusBadge";
import { getDiagnostics, retryFailedGatewayOperations, type DiagnosticCheck, type WorkerCheck } from "../api/client";

function badgeTone(status: string): "ok" | "warn" | "danger" | "neutral" {
  if (status === "ok" || status === "ready") return "ok";
  if (status === "error" || status === "health_failed" || status === "auth_failed" || status === "settings_error") {
    return "danger";
  }
  return "warn";
}

function metricTone(count: number, warn = 1): "green" | "amber" {
  return count >= warn ? "amber" : "green";
}

function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

function CheckRow({ label, check }: { label: string; check: DiagnosticCheck }) {
  return (
    <div className="diagnostic-row">
      <span>{label}</span>
      <strong>{check.detail || check.status}</strong>
      <StatusBadge tone={badgeTone(check.status)}>{check.status}</StatusBadge>
      <small>{check.latencyMs}ms</small>
    </div>
  );
}

function WorkerRow({ check }: { check: WorkerCheck }) {
  return (
    <div className="diagnostic-row">
      <span>Worker</span>
      <strong>{check.detail || check.status}</strong>
      <StatusBadge tone={badgeTone(check.status)}>{check.status}</StatusBadge>
      <small>{check.lastSeenAt ? `${check.ageSeconds}s ago` : "-"}</small>
    </div>
  );
}

export function DiagnosticsPage() {
  const queryClient = useQueryClient();
  const [retryFailedOpen, setRetryFailedOpen] = useState(false);
  const diagnostics = useQuery({ queryKey: ["admin-diagnostics"], queryFn: getDiagnostics });
  const retryFailed = useMutation({
    mutationFn: ({ auditReason }: { auditReason: string }) => retryFailedGatewayOperations({ auditReason }),
    onSuccess: () => {
      setRetryFailedOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["admin-diagnostics"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-gateway-operations"] });
      void queryClient.invalidateQueries({ queryKey: ["admin-audit-logs"] });
    }
  });
  const data = diagnostics.data?.diagnostics;
  const queue = data?.queue;
  const retryableFailed = queue?.retryableFailed ?? 0;
  const blocked = (queue?.deadLetter ?? 0) + (queue?.staleRunning ?? 0);
  const sub2apiStatus = data?.sub2api.ok ? "ok" : (data?.sub2api.status ?? "checking");
  const servicesLive = data?.services.postgres.status === "ok" && data?.services.redis.status === "ok";
  const readiness = data?.productionReadiness ?? [];
  const readinessWarnings = readiness.filter((item) => item.status !== "ok").length;

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Ops"
        title="运维诊断"
        description="检查 API、Worker、数据库、Redis、Sub2API 和同步队列状态。"
        actions={
          <>
            <button
              className="secondary-action"
              disabled={diagnostics.isFetching}
              onClick={() => void diagnostics.refetch()}
              type="button"
            >
              <RefreshCw size={16} />
              <span>{diagnostics.isFetching ? "检测中" : "重新检测"}</span>
            </button>
            <button
              className="secondary-action"
              disabled={retryFailed.isPending || retryableFailed === 0}
              onClick={() => setRetryFailedOpen(true)}
              type="button"
            >
              <RotateCcw size={16} />
              <span>{retryFailed.isPending ? "入队中" : "重试失败任务"}</span>
            </button>
          </>
        }
      />

      <StatGrid
        stats={[
          {
            label: "Worker",
            value: data?.services.worker.status ?? "checking",
            delta: data?.services.worker.workerId || "Redis heartbeat",
            tone: data?.services.worker.status === "ok" ? "green" : "amber",
            icon: Server
          },
          {
            label: "Ready Now",
            value: String(queue?.readyNow ?? 0),
            delta: `${queue?.pending ?? 0} pending / ${queue?.running ?? 0} running`,
            tone: metricTone(queue?.readyNow ?? 0),
            icon: Zap
          },
          {
            label: "失败可重试",
            value: String(retryableFailed),
            delta: `${queue?.failed ?? 0} failed / ${queue?.deadLetter ?? 0} dead`,
            tone: metricTone(retryableFailed),
            icon: RotateCcw
          },
          {
            label: "Sub2API",
            value: sub2apiStatus,
            delta: `${data?.sub2api.groupCount ?? 0} groups / ${data?.sub2api.latencyMs ?? 0}ms`,
            tone: data?.sub2api.ok ? "green" : "red",
            icon: Wifi
          },
          {
            label: "上线检查",
            value: readinessWarnings === 0 && readiness.length > 0 ? "clear" : `${readinessWarnings} warning`,
            delta: `${readiness.length} checks`,
            tone: readinessWarnings === 0 && readiness.length > 0 ? "green" : "amber",
            icon: ShieldCheck
          }
        ]}
      />

      {diagnostics.isLoading ? <div className="panel inline-state">正在生成诊断快照...</div> : null}
      {diagnostics.isError ? <div className="panel inline-state danger-text">诊断快照加载失败：{diagnostics.error.message}</div> : null}
      {retryFailed.isSuccess ? (
        <div className="panel inline-state">已重新入队 {retryFailed.data.retried} 个可重试失败任务。</div>
      ) : null}
      {retryFailed.isError ? <div className="panel inline-state danger-text">批量重试失败：{retryFailed.error.message}</div> : null}

      <section className="split-grid">
        <article className="panel">
          <div className="panel-heading">
            <h3>服务连通性</h3>
            <StatusBadge tone={servicesLive ? "ok" : "warn"}>
              {data ? "live" : "checking"}
            </StatusBadge>
          </div>
          <div className="diagnostic-list">
            {data ? (
              <>
                <CheckRow label="API" check={data.services.api} />
                <CheckRow label="Postgres" check={data.services.postgres} />
                <CheckRow label="Redis" check={data.services.redis} />
                <WorkerRow check={data.services.worker} />
              </>
            ) : null}
          </div>
        </article>

        <article className="panel">
          <div className="panel-heading">
            <h3>Sub2API</h3>
            <StatusBadge tone={badgeTone(sub2apiStatus)}>{sub2apiStatus}</StatusBadge>
          </div>
          <dl className="kv-list">
            <dt>Base URL</dt>
            <dd>{data?.sub2api.baseUrl || "-"}</dd>
            <dt>Auth mode</dt>
            <dd>{data?.sub2api.authMode || "-"}</dd>
            <dt>Health / Auth</dt>
            <dd>
              {data?.sub2api.healthOk ? "health ok" : "health pending"} / {data?.sub2api.authOk ? "auth ok" : "auth pending"}
            </dd>
            <dt>Last check</dt>
            <dd>{formatDate(data?.sub2api.checkedAt ?? null)}</dd>
            <dt>Error</dt>
            <dd className={data?.sub2api.error ? "danger-text" : undefined}>{data?.sub2api.error || "-"}</dd>
          </dl>
        </article>
      </section>

      <section className="split-grid">
        <article className="panel">
          <div className="panel-heading">
            <h3>队列统计</h3>
            <StatusBadge tone={blocked > 0 ? "warn" : "ok"}>{blocked > 0 ? "needs review" : "clear"}</StatusBadge>
          </div>
          <div className="queue-diagnostics-grid">
            <div>
              <span>Total</span>
              <strong>{queue?.total ?? 0}</strong>
            </div>
            <div>
              <span>Succeeded</span>
              <strong>{queue?.succeeded ?? 0}</strong>
            </div>
            <div>
              <span>Due soon</span>
              <strong>{queue?.dueSoon ?? 0}</strong>
            </div>
            <div>
              <span>Stale running</span>
              <strong>{queue?.staleRunning ?? 0}</strong>
            </div>
          </div>
        </article>

        <article className="panel">
          <div className="panel-heading">
            <h3>最近状态</h3>
            <StatusBadge tone="neutral">{data ? new Date(data.generatedAt).toLocaleTimeString() : "checking"}</StatusBadge>
          </div>
          <div className="timeline-list">
            <div>
              <Clock size={16} />
              <span>最后成功</span>
              <strong>{formatDate(queue?.lastSucceededAt ?? null)}</strong>
            </div>
            <div>
              <Activity size={16} />
              <span>最后失败</span>
              <strong>{formatDate(queue?.lastFailedAt ?? null)}</strong>
            </div>
            <div>
              <Database size={16} />
              <span>最后任务变更</span>
              <strong>{formatDate(queue?.lastOperationAt ?? null)}</strong>
            </div>
          </div>
        </article>
      </section>

      <section className="panel">
        <div className="panel-heading">
          <h3>上线检查清单</h3>
          <StatusBadge tone={readinessWarnings === 0 && readiness.length > 0 ? "ok" : "warn"}>
            {readinessWarnings === 0 && readiness.length > 0 ? "ready" : `${readinessWarnings} warnings`}
          </StatusBadge>
        </div>
        <div className="readiness-grid">
          {readiness.map((item) => (
            <div className="readiness-row" key={item.key}>
              <CheckCircle2 size={17} />
              <div>
                <strong>{item.label}</strong>
                <span>{item.detail}</span>
              </div>
              <StatusBadge tone={badgeTone(item.status)}>{item.status}</StatusBadge>
              <small>{item.status === "ok" ? "无需处理" : item.action}</small>
            </div>
          ))}
        </div>
        {data && readiness.length === 0 ? <div className="inline-state">暂无上线检查项。</div> : null}
      </section>
      <DangerConfirmModal
        open={retryFailedOpen}
        title="批量重试失败任务"
        description={`将重新入队 ${retryableFailed} 个可重试失败任务。请确认已经处理配置、鉴权或限流问题。`}
        confirmLabel="确认批量重试"
        pending={retryFailed.isPending}
        onCancel={() => setRetryFailedOpen(false)}
        onConfirm={(auditReason) => retryFailed.mutate({ auditReason })}
      />
    </div>
  );
}
