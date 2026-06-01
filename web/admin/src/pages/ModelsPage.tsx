import { useQuery } from "@tanstack/react-query";
import { DataTable } from "../components/DataTable";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import { getModelCatalog } from "../api/client";

export function ModelsPage() {
  const models = useQuery({ queryKey: ["admin-model-catalog"], queryFn: getModelCatalog });
  const rows = models.data?.items ?? [];

  return (
    <div className="page-stack">
      <PageHeader eyebrow="Catalog" title="模型目录" description="控制 App 可见模型、能力标签和默认选择。" />
      {models.isLoading ? <div className="panel inline-state">正在加载模型目录...</div> : null}
      {models.isError ? <div className="panel inline-state danger-text">模型目录加载失败。</div> : null}
      <DataTable
        rows={rows}
        getRowKey={(row) => row.id}
        columns={[
          { key: "id", header: "模型", render: (row) => <code>{row.id}</code> },
          { key: "name", header: "显示名", render: (row) => row.displayName },
          { key: "family", header: "协议族", render: (row) => row.providerFamily },
          { key: "caps", header: "能力", render: (row) => row.capabilities.join(", ") || "-" },
          {
            key: "status",
            header: "状态",
            render: (row) => <StatusBadge tone={row.status === "active" ? "ok" : "neutral"}>{row.status}</StatusBadge>
          },
          {
            key: "visible",
            header: "可见性",
            render: (row) => (
              <StatusBadge tone={row.publicVisible ? "ok" : "neutral"}>{row.publicVisible ? "公开" : "隐藏"}</StatusBadge>
            ),
            align: "right"
          }
        ]}
      />
      {!models.isLoading && rows.length === 0 ? <div className="panel inline-state">暂无模型目录数据。</div> : null}
    </div>
  );
}
