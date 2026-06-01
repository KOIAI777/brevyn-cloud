import { Archive, Ban, Copy, Download, FilterX, Pencil, Plus, Save, Search, X } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { DataTable } from "../components/DataTable";
import { DangerConfirmModal } from "../components/DangerConfirmModal";
import { PageHeader } from "../components/PageHeader";
import { PaginationRow } from "../components/PaginationRow";
import { StatusBadge } from "../components/StatusBadge";
import {
  createProduct,
  deleteProduct,
  disableRedeemBatch,
  disableRedeemCode,
  generateRedeemCodes,
  getGatewayGroups,
  getProducts,
  getRedeemBatches,
  getRedeemCodes,
  updateProduct,
  type GenerateRedeemCodesResult,
  type GatewayGroup,
  type Product,
  type ProductInput,
  type RedeemCode,
  type RedeemCodeBatch
} from "../api/client";

function formatBenefit(kind: string, value: number, validityDays: number) {
  if (kind === "subscription") return `${validityDays} 天`;
  return `$${value.toFixed(2)}`;
}

function formatTime(value: string | null) {
  if (!value) return "-";
  return new Date(value).toLocaleString();
}

function formatProductStatus(product: Product) {
  if (product.status === "archived") return "已归档";
  if (product.status === "disabled") return "已下架";
  if (!product.forSale) return "未上架";
  return "上架中";
}

const sourcePresets = [
  { value: "ldxp", label: "联动小铺" },
  { value: "manual", label: "手动" },
  { value: "promo", label: "活动赠送" }
];

function SourcePresetButtons({ onSelect }: { onSelect: (value: string) => void }) {
  return (
    <div className="source-preset-row">
      {sourcePresets.map((item) => (
        <button className="text-chip-button" key={item.value} onClick={() => onSelect(item.value)} type="button">
          {item.label}
        </button>
      ))}
    </div>
  );
}

function formatSourceLabel(value: string) {
  const preset = sourcePresets.find((item) => item.value === value);
  if (preset) return preset.label;
  return value || "-";
}

function formatNotes(value: string) {
  const trimmed = value.trim();
  return trimmed || "-";
}

function productStatusTone(product: Product) {
  if (product.status === "active" && product.forSale) return "ok";
  if (product.status === "disabled") return "warn";
  return "neutral";
}

type ProductGroupGuard = {
  blocking: string[];
  warnings: string[];
};

function findProductGroup(product: Product | undefined, groups: GatewayGroup[]) {
  if (!product) return undefined;
  return groups.find((group) => group.id === product.gatewayGroupId) ?? groups.find((group) => group.externalGroupId === product.externalGroupId);
}

function findFormGroup(input: ProductInput, groups: GatewayGroup[]) {
  return groups.find((group) => group.id === input.gatewayGroupId) ?? groups.find((group) => group.externalGroupId === input.externalGroupId);
}

function groupTypeLabel(group: GatewayGroup | undefined) {
  if (!group) return "未绑定";
  if (group.subscriptionType === "subscription") return "subscription · 订阅限额组";
  return "standard · 余额扣费组";
}

function groupLimitLabel(group: GatewayGroup | undefined) {
  if (!group) return "无分组限额";
  const parts = [
    group.dailyLimitUsd ? `日 $${group.dailyLimitUsd}` : "",
    group.weeklyLimitUsd ? `周 $${group.weeklyLimitUsd}` : "",
    group.monthlyLimitUsd ? `月 $${group.monthlyLimitUsd}` : ""
  ].filter(Boolean);
  if (group.subscriptionType === "subscription") {
    return parts.length ? parts.join(" / ") : "订阅未配置限额";
  }
  return parts.length ? parts.join(" / ") : "余额按钱包扣费";
}

function groupOptionLabel(group: GatewayGroup) {
  return `${group.name} · #${group.externalGroupId} · ${groupTypeLabel(group)} · ${groupLimitLabel(group)}`;
}

function buildProductGroupGuard(benefitType: string, group: GatewayGroup | undefined, hasGroupBinding: boolean): ProductGroupGuard {
  const blocking: string[] = [];
  const warnings: string[] = [];
  if (benefitType === "subscription" && !hasGroupBinding) {
    blocking.push("订阅商品必须绑定 Sub2API subscription 分组");
  }
  if (hasGroupBinding && !group) {
    blocking.push("绑定的分组未同步或已不存在");
  }
  if (group?.status && group.status !== "active") {
    blocking.push(`分组状态为 ${group.status}，不能用于新卡密`);
  }
  if (benefitType === "balance" && group?.subscriptionType === "subscription") {
    blocking.push("余额商品不能绑定 subscription 分组，请选择 standard 分组或保持无分组");
  }
  if (benefitType === "subscription" && group && group.subscriptionType !== "subscription") {
    blocking.push(`当前分组类型是 ${group.subscriptionType}，订阅商品需要 subscription`);
  }
  if (
    benefitType === "subscription" &&
    group?.subscriptionType === "subscription" &&
    !group.dailyLimitUsd &&
    !group.weeklyLimitUsd &&
    !group.monthlyLimitUsd
  ) {
    warnings.push("subscription 分组没有日/周/月限额，套餐会接近不限额");
  }
  if (group && group.upstreamAccountCount === 0) {
    warnings.push("分组还没有绑定账号");
  } else if (group && group.activeSchedulableAccountCount === 0) {
    warnings.push("分组暂无可调度账号");
  }
  if (group && group.models.length === 0) {
    warnings.push("分组暂无可用模型");
  }
  if (group && group.unpricedModelCount > 0) {
    warnings.push(`${group.unpricedModelCount} 个模型未定价`);
  }
  return { blocking, warnings };
}

function guardTone(guard: ProductGroupGuard): "ok" | "warn" | "danger" | "neutral" {
  if (guard.blocking.length > 0) return "danger";
  if (guard.warnings.length > 0) return "warn";
  return "ok";
}

function guardLabel(guard: ProductGroupGuard) {
  if (guard.blocking.length > 0) return "blocked";
  if (guard.warnings.length > 0) return "warning";
  return "ready";
}

function groupTitle(group: GatewayGroup | undefined) {
  if (!group) return "未绑定分组";
  return `${group.name} · #${group.externalGroupId}`;
}

function GroupGuardCard({
  benefitType,
  className = "",
  emptyText,
  group,
  guard,
  title,
  validityDays
}: {
  benefitType?: Product["benefitType"];
  className?: string;
  emptyText: string;
  group?: GatewayGroup;
  guard: ProductGroupGuard;
  title: string;
  validityDays?: number;
}) {
  const issues = [...guard.blocking, ...guard.warnings];
  return (
    <div className={`product-guard-card ${guardTone(guard)} ${className}`}>
      <div className="product-guard-head">
        <span>{title}</span>
        <StatusBadge tone={guardTone(guard)}>{guardLabel(guard)}</StatusBadge>
      </div>
      <strong>{groupTitle(group)}</strong>
      {group ? (
        <div className="product-guard-meta">
          <span>{groupTypeLabel(group)}</span>
          <span>倍率 {group.rateMultiplier}</span>
          <span>{groupLimitLabel(group)}</span>
          {group.subscriptionType === "subscription" ? <span>分组默认 {group.defaultValidityDays} 天</span> : null}
          {benefitType === "subscription" && validityDays ? <span>商品有效 {validityDays} 天</span> : null}
          <span>{group.activeSchedulableAccountCount}/{group.upstreamAccountCount} 可调度</span>
          <span>{group.models.length} models</span>
          <span>{group.unpricedModelCount} 未定价</span>
        </div>
      ) : (
        <p>{emptyText}</p>
      )}
      {issues.length > 0 ? (
        <ul className="product-guard-list">
          {issues.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      ) : (
        <p>当前配置可以用于生成新卡密。</p>
      )}
    </div>
  );
}

function ProductGroupCell({ groups, product }: { groups: GatewayGroup[]; product: Product }) {
  const group = findProductGroup(product, groups);
  if (!product.gatewayGroupId && !product.externalGroupId) return <span className="muted-cell">无分组</span>;
  return (
    <div className="product-table-group">
      <strong>{group?.name ?? "未同步分组"}</strong>
      <span>#{product.externalGroupId || group?.externalGroupId || "-"}</span>
      {group ? <StatusBadge tone={group.status === "active" ? "ok" : "warn"}>{groupTypeLabel(group)}</StatusBadge> : null}
    </div>
  );
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? `${fallback}: ${error.message}` : fallback;
}

const emptyProductForm: ProductInput = {
  sku: "",
  name: "",
  description: "",
  benefitType: "balance",
  priceCny: 0,
  originalPriceCny: null,
  value: 20,
  validityDays: 0,
  gatewayGroupId: "",
  externalGroupId: 0,
  source: "ldxp",
  features: "",
  forSale: true,
  sortOrder: 100,
  status: "active"
};

const productPresets = [
  {
    label: "体验余额",
    form: { name: "体验余额 $20", benefitType: "balance", priceCny: 5, value: 20, validityDays: 0, sortOrder: 10 }
  },
  {
    label: "学生余额",
    form: { name: "学生余额 $100", benefitType: "balance", priceCny: 25, value: 100, validityDays: 0, sortOrder: 20 }
  },
  {
    label: "学生周卡",
    form: { name: "学生周卡 7 天", benefitType: "subscription", priceCny: 19, value: 0, validityDays: 7, sortOrder: 30 }
  },
  {
    label: "轻量月卡",
    form: { name: "轻量月卡 30 天", benefitType: "subscription", priceCny: 49, value: 0, validityDays: 30, sortOrder: 40 }
  }
] satisfies Array<{ label: string; form: Partial<ProductInput> }>;

const pageSizeOptions = [25, 50, 100, 200];

function escapeCsv(value: string | number | null | undefined) {
  return `"${String(value ?? "").replace(/"/g, '""')}"`;
}

function downloadTextFile(filename: string, content: string, type = "text/plain;charset=utf-8") {
  const blob = new Blob([content], { type });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}

function productToForm(product: Product): ProductInput {
  return {
    sku: product.sku,
    name: product.name,
    description: product.description,
    benefitType: product.benefitType === "subscription" ? "subscription" : "balance",
    priceCny: product.priceCny,
    originalPriceCny: product.originalPriceCny,
    value: product.value,
    validityDays: product.validityDays,
    gatewayGroupId: product.gatewayGroupId,
    externalGroupId: product.externalGroupId,
    source: product.source,
    features: product.features,
    forSale: product.forSale,
    sortOrder: product.sortOrder,
    status: product.status
  };
}

function normalizeProductInput(input: ProductInput): ProductInput {
  return {
    ...input,
    sku: input.sku.trim(),
    name: input.name.trim(),
    description: input.description.trim(),
    features: input.features.trim(),
    source: input.source.trim() || "ldxp",
    value: Number(input.value) || 0,
    validityDays: Number(input.validityDays) || 0,
    priceCny: Number(input.priceCny) || 0,
    originalPriceCny: input.originalPriceCny ? Number(input.originalPriceCny) : null,
    sortOrder: Number(input.sortOrder) || 0,
    externalGroupId: Number(input.externalGroupId) || 0
  };
}

export function RedeemCodesPage() {
  const queryClient = useQueryClient();
  const [productId, setProductId] = useState("");
  const [editingProductId, setEditingProductId] = useState<string | null>(null);
  const [productForm, setProductForm] = useState<ProductInput>(emptyProductForm);
  const [count, setCount] = useState(10);
  const [batchName, setBatchName] = useState("");
  const [source, setSource] = useState("ldxp");
  const [orderRef, setOrderRef] = useState("");
  const [batchNotes, setBatchNotes] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("");
  const [batchSearch, setBatchSearch] = useState("");
  const [batchStatus, setBatchStatus] = useState("all");
  const [batchSource, setBatchSource] = useState("");
  const [batchProductId, setBatchProductId] = useState("all");
  const [batchDateFrom, setBatchDateFrom] = useState("");
  const [batchDateTo, setBatchDateTo] = useState("");
  const [batchPageSize, setBatchPageSize] = useState(25);
  const [batchOffset, setBatchOffset] = useState(0);
  const [codeSearch, setCodeSearch] = useState("");
  const [codeStatus, setCodeStatus] = useState("all");
  const [codeType, setCodeType] = useState("all");
  const [codeSource, setCodeSource] = useState("");
  const [codeProductId, setCodeProductId] = useState("all");
  const [codeBatchId, setCodeBatchId] = useState("all");
  const [codeUsedBy, setCodeUsedBy] = useState("");
  const [codeDateFrom, setCodeDateFrom] = useState("");
  const [codeDateTo, setCodeDateTo] = useState("");
  const [codePageSize, setCodePageSize] = useState(100);
  const [codeOffset, setCodeOffset] = useState(0);
  const [generated, setGenerated] = useState<GenerateRedeemCodesResult | null>(null);
  const [productNotice, setProductNotice] = useState("");
  const [copyNotice, setCopyNotice] = useState("");
  const [archiveProduct, setArchiveProduct] = useState<Product | null>(null);
  const [disableBatchTarget, setDisableBatchTarget] = useState<RedeemCodeBatch | null>(null);
  const [disableCodeTarget, setDisableCodeTarget] = useState<RedeemCode | null>(null);

  const products = useQuery({ queryKey: ["admin-products"], queryFn: getProducts });
  const gatewayGroups = useQuery({ queryKey: ["admin-gateway-groups"], queryFn: getGatewayGroups });
  const batches = useQuery({
    queryKey: [
      "admin-redeem-batches",
      batchSearch,
      batchStatus,
      batchSource,
      batchProductId,
      batchDateFrom,
      batchDateTo,
      batchPageSize,
      batchOffset
    ],
    queryFn: () =>
      getRedeemBatches({
        search: batchSearch,
        status: batchStatus,
        source: batchSource,
        productId: batchProductId,
        dateFrom: batchDateFrom,
        dateTo: batchDateTo,
        limit: batchPageSize,
        offset: batchOffset
      })
  });
  const codes = useQuery({
    queryKey: [
      "admin-redeem-codes",
      codeSearch,
      codeStatus,
      codeType,
      codeSource,
      codeProductId,
      codeBatchId,
      codeUsedBy,
      codeDateFrom,
      codeDateTo,
      codePageSize,
      codeOffset
    ],
    queryFn: () =>
      getRedeemCodes({
        search: codeSearch,
        status: codeStatus,
        type: codeType,
        source: codeSource,
        productId: codeProductId,
        batchId: codeBatchId,
        usedBy: codeUsedBy,
        dateFrom: codeDateFrom,
        dateTo: codeDateTo,
        limit: codePageSize,
        offset: codeOffset
      })
  });
  const batchOptions = useQuery({
    queryKey: ["admin-redeem-batch-options"],
    queryFn: () => getRedeemBatches({ limit: 300, offset: 0 })
  });

  const productRows = useMemo(() => products.data?.items ?? [], [products.data?.items]);
  const groupRows = useMemo(() => gatewayGroups.data?.items ?? [], [gatewayGroups.data?.items]);
  const batchOptionRows = useMemo(() => batchOptions.data?.items ?? [], [batchOptions.data?.items]);
  const activeProductRows = useMemo(
    () => productRows.filter((product) => product.status === "active" && product.forSale),
    [productRows]
  );
  const selectedProduct = useMemo(
    () => productRows.find((product) => product.id === productId),
    [productId, productRows]
  );
  const selectedProductGroup = useMemo(() => findProductGroup(selectedProduct, groupRows), [selectedProduct, groupRows]);
  const selectedProductGuard = useMemo(
    () =>
      selectedProduct
        ? buildProductGroupGuard(
            selectedProduct.benefitType,
            selectedProductGroup,
            Boolean(selectedProduct.gatewayGroupId || selectedProduct.externalGroupId)
          )
        : { blocking: [], warnings: [] },
    [selectedProduct, selectedProductGroup]
  );
  const productFormGroup = useMemo(() => findFormGroup(productForm, groupRows), [productForm, groupRows]);
  const productFormGuard = useMemo(
    () =>
      buildProductGroupGuard(
        productForm.benefitType,
        productFormGroup,
        Boolean(productForm.gatewayGroupId || productForm.externalGroupId)
      ),
    [productForm.benefitType, productForm.externalGroupId, productForm.gatewayGroupId, productFormGroup]
  );
  const resetBatchOffset = () => setBatchOffset(0);
  const resetCodeOffset = () => setCodeOffset(0);
  const clearBatchFilters = () => {
    setBatchSearch("");
    setBatchStatus("all");
    setBatchSource("");
    setBatchProductId("all");
    setBatchDateFrom("");
    setBatchDateTo("");
    setBatchPageSize(25);
    setBatchOffset(0);
  };
  const clearCodeFilters = () => {
    setCodeSearch("");
    setCodeStatus("all");
    setCodeType("all");
    setCodeSource("");
    setCodeProductId("all");
    setCodeBatchId("all");
    setCodeUsedBy("");
    setCodeDateFrom("");
    setCodeDateTo("");
    setCodePageSize(100);
    setCodeOffset(0);
  };

  const generate = useMutation({
    mutationFn: () =>
      generateRedeemCodes({
        productId,
        count,
        batchName,
        source: source.trim() || "ldxp",
        orderRef: orderRef.trim(),
        notes: batchNotes,
        expiresInDays: expiresInDays ? Number(expiresInDays) : undefined
      }),
    onSuccess: async (result) => {
      setGenerated(result);
      setCopyNotice("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-codes"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batches"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batch-options"] })
      ]);
    }
  });

  const createProductMutation = useMutation({
    mutationFn: () => createProduct(normalizeProductInput(productForm)),
    onSuccess: async (result) => {
      setEditingProductId(null);
      setProductForm(emptyProductForm);
      setProductId(result.product.id);
      setProductNotice(`商品已创建，SKU：${result.product.sku}`);
      await queryClient.invalidateQueries({ queryKey: ["admin-products"] });
    }
  });
  const updateProductMutation = useMutation({
    mutationFn: () => updateProduct(editingProductId!, normalizeProductInput(productForm)),
    onSuccess: async (result) => {
      setEditingProductId(null);
      setProductForm(emptyProductForm);
      setProductId(result.product.id);
      setProductNotice("商品已保存");
      await queryClient.invalidateQueries({ queryKey: ["admin-products"] });
    }
  });
  const deleteProductMutation = useMutation({
    mutationFn: ({ id, auditReason }: { id: string; auditReason: string }) => deleteProduct(id, { auditReason }),
    onSuccess: async (result) => {
      setArchiveProduct(null);
      setEditingProductId(null);
      setProductForm(emptyProductForm);
      setProductNotice(result.mode === "archived" ? "商品已归档，不再用于新批次" : "商品已删除");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-products"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-codes"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batches"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batch-options"] })
      ]);
    }
  });
  const disableBatchMutation = useMutation({
    mutationFn: ({ id, auditReason }: { id: string; auditReason: string }) => disableRedeemBatch(id, { auditReason }),
    onSuccess: async (result) => {
      setDisableBatchTarget(null);
      setProductNotice(`批次已作废 ${result.disabledCodes} 张未使用卡密`);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-codes"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batches"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batch-options"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-overview"] })
      ]);
    }
  });
  const disableCodeMutation = useMutation({
    mutationFn: ({ id, auditReason }: { id: string; auditReason: string }) => disableRedeemCode(id, { auditReason }),
    onSuccess: async () => {
      setDisableCodeTarget(null);
      setProductNotice("卡密已作废，无法再兑换");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-codes"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batches"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-redeem-batch-options"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-overview"] })
      ]);
    }
  });

  const canGenerate =
    Boolean(productId) && count > 0 && count <= 500 && selectedProductGuard.blocking.length === 0 && !generate.isPending;
  const canSaveProduct =
    (!editingProductId || productForm.sku.trim() !== "") &&
    productForm.name.trim() !== "" &&
    (productForm.benefitType === "balance" ? Number(productForm.value) !== 0 : Number(productForm.validityDays) > 0) &&
    (productForm.benefitType !== "subscription" || Boolean(productForm.gatewayGroupId)) &&
    productFormGuard.blocking.length === 0 &&
    !createProductMutation.isPending &&
    !updateProductMutation.isPending;
  const generatedText = generated?.codes.map((item) => item.code).join("\n") ?? "";
  const generatedCsv =
    generated
      ? [
          ["code", "batch", "product", "sku", "benefit_type", "value", "validity_days", "source", "order_ref", "notes"]
            .map(escapeCsv)
            .join(","),
          ...generated.codes.map((item) =>
            [
              item.code,
              generated.batch.name,
              generated.product.name,
              generated.product.sku,
              generated.product.benefitType,
              generated.product.value,
              generated.product.validityDays,
              generated.batch.source,
              generated.batch.orderRef,
              generated.batch.notes
            ]
              .map(escapeCsv)
              .join(",")
          )
        ].join("\n")
      : "";
  const generatedFilename = generated
    ? `${generated.batch.name || generated.product.sku}-${generated.batch.id}`.replace(/[^a-zA-Z0-9._-]+/g, "-")
    : "brevyn-codes";
  const copyText = async (text: string, label: string) => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopyNotice(`${label}已复制`);
    } catch {
      setCopyNotice(`${label}复制失败，请手动选择文本`);
    }
  };
  const exportGenerated = (format: "txt" | "csv") => {
    if (!generated) return;
    if (format === "csv") {
      downloadTextFile(`${generatedFilename}.csv`, generatedCsv, "text/csv;charset=utf-8");
      return;
    }
    downloadTextFile(`${generatedFilename}.txt`, generatedText);
  };
  const applyProductPreset = (preset: (typeof productPresets)[number]) => {
    const firstSubscriptionGroup = groupRows.find((group) => group.status === "active" && group.subscriptionType === "subscription");
    const isSubscription = preset.form.benefitType === "subscription";
    setEditingProductId(null);
    setProductForm({
      ...emptyProductForm,
      ...preset.form,
      benefitType: (preset.form.benefitType ?? "balance") as ProductInput["benefitType"],
      gatewayGroupId: isSubscription ? firstSubscriptionGroup?.id ?? "" : "",
      externalGroupId: isSubscription ? firstSubscriptionGroup?.externalGroupId ?? 0 : 0,
      sku: ""
    });
  };
  const saveProduct = () => {
    if (!canSaveProduct) return;
    if (editingProductId) {
      updateProductMutation.mutate();
      return;
    }
    createProductMutation.mutate();
  };
  const resetProductForm = () => {
    setEditingProductId(null);
    setProductForm(emptyProductForm);
  };
  const startEditProduct = (product: Product) => {
    setEditingProductId(product.id);
    setProductForm(productToForm(product));
  };

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Codes"
        title="兑换码"
        description="Brevyn Cloud 自有卡密；商品语义对齐 Sub2API 的 balance / subscription，兑换时再同步到网关。"
        actions={
          <button className="primary-action" disabled={!canGenerate} onClick={() => generate.mutate()} type="button">
            <Plus size={16} />
            <span>{generate.isPending ? "生成中" : "生成批次"}</span>
          </button>
        }
      />

      <section className="panel">
        <div className="panel-heading">
          <h3>生成批次</h3>
          <StatusBadge tone={selectedProduct ? "ok" : "warn"}>{selectedProduct ? selectedProduct.benefitType : "select product"}</StatusBadge>
        </div>
        <div className="form-grid">
          <label>
            商品
            <select value={productId} onChange={(event) => setProductId(event.target.value)}>
              <option value="">选择商品</option>
              {activeProductRows.map((product) => (
                <option key={product.id} value={product.id}>
                  {product.name} · {product.sku}
                </option>
              ))}
            </select>
          </label>
          <label>
            数量
            <input
              min={1}
              max={500}
              onChange={(event) => setCount(Number(event.target.value))}
              type="number"
              value={count}
            />
          </label>
          <label>
            批次名
            <input
              onChange={(event) => setBatchName(event.target.value)}
              placeholder="ldxp-0528-student"
              value={batchName}
            />
          </label>
          <label>
            来源
            <input
              onChange={(event) => setSource(event.target.value)}
              placeholder="联动小铺 / campus-a / agent-01"
              value={source}
            />
            <SourcePresetButtons onSelect={setSource} />
            <span className="field-hint">可输入自定义来源；常用值会保留为建议。</span>
          </label>
          <label>
            卡密过期天数
            <input
              min={1}
              onChange={(event) => setExpiresInDays(event.target.value)}
              placeholder="不填则不过期"
              type="number"
              value={expiresInDays}
            />
          </label>
          <label>
            联动小铺订单号
            <input
              onChange={(event) => setOrderRef(event.target.value)}
              placeholder="例如：LDXP-20260601-001"
              value={orderRef}
            />
          </label>
          <label className="wide-field">
            内部备注
            <textarea
              onChange={(event) => setBatchNotes(event.target.value)}
              placeholder="买家备注、发货批次说明，仅供运营追踪"
              value={batchNotes}
            />
          </label>
          <div className="field-summary">
            <span>权益</span>
            <strong>
              {selectedProduct
                ? formatBenefit(selectedProduct.benefitType, selectedProduct.value, selectedProduct.validityDays)
                : "-"}
            </strong>
          </div>
          {selectedProduct ? (
            <GroupGuardCard
              benefitType={selectedProduct.benefitType}
              className="wide-field"
              emptyText="余额商品可以不绑定分组或绑定 standard 分组；订阅商品必须绑定 Sub2API subscription 分组。"
              group={selectedProductGroup}
              guard={selectedProductGuard}
              title="发货校验"
              validityDays={selectedProduct.validityDays}
            />
          ) : (
            <div className="product-guard-card neutral wide-field">
              <div className="product-guard-head">
                <span>发货校验</span>
                <StatusBadge tone="neutral">waiting</StatusBadge>
              </div>
              <strong>先选择商品</strong>
              <p>选择商品后会显示绑定分组、可调度账号、模型和定价状态。</p>
            </div>
          )}
        </div>
        {generate.isError ? <div className="form-error">{errorMessage(generate.error, "生成失败，请检查商品分组和有效期")}</div> : null}
      </section>

      {generated ? (
        <section className="panel">
          <div className="panel-heading">
            <div>
              <h3>本次生成明文</h3>
              <p className="panel-subtitle">
                {generated.product.name} · {generated.batch.quantity} 张 · {formatSourceLabel(generated.batch.source)}
              </p>
            </div>
            <div className="button-row">
              <button className="secondary-action" onClick={() => copyText(generatedText, "卡密")} type="button">
                <Copy size={16} />
                <span>复制卡密</span>
              </button>
              <button className="secondary-action" onClick={() => copyText(generatedCsv, "CSV")} type="button">
                <Copy size={16} />
                <span>复制 CSV</span>
              </button>
              <button className="secondary-action" onClick={() => exportGenerated("csv")} type="button">
                <Download size={16} />
                <span>导出 CSV</span>
              </button>
              <button className="secondary-action" onClick={() => exportGenerated("txt")} type="button">
                <Download size={16} />
                <span>导出 TXT</span>
              </button>
            </div>
          </div>
          {generated.batch.orderRef ? <div className="batch-note-preview">联动小铺订单号：{generated.batch.orderRef}</div> : null}
          {generated.batch.notes ? <div className="batch-note-preview">{generated.batch.notes}</div> : null}
          {copyNotice ? <div className="form-success">{copyNotice}</div> : null}
          <textarea readOnly className="code-output" value={generatedText} />
        </section>
      ) : null}

      <section className="split-grid">
        <article className="panel product-admin-panel">
          <div className="panel-heading">
            <h3>{editingProductId ? "编辑商品" : "商品"}</h3>
            <button className="secondary-action" onClick={resetProductForm} type="button">
              <Plus size={16} />
              <span>新增商品</span>
            </button>
          </div>
          <div className="preset-row">
            {productPresets.map((preset) => (
              <button className="secondary-action" key={preset.label} onClick={() => applyProductPreset(preset)} type="button">
                {preset.label}
              </button>
            ))}
          </div>
          <div className="form-grid product-form">
            <label>
              SKU
              <input
                onChange={(event) => setProductForm((current) => ({ ...current, sku: event.target.value }))}
                placeholder={editingProductId ? "必填" : "留空自动生成"}
                value={productForm.sku}
              />
            </label>
            <label>
              商品名
              <input
                onChange={(event) => setProductForm((current) => ({ ...current, name: event.target.value }))}
                placeholder="余额 $20"
                value={productForm.name}
              />
            </label>
            <label>
              类型
              <select
                onChange={(event) => {
                  const benefitType = event.target.value as ProductInput["benefitType"];
                  const firstSubscriptionGroup = groupRows.find((group) => group.status === "active" && group.subscriptionType === "subscription");
                  setProductForm((current) => {
                    const currentGroup = groupRows.find((group) => group.id === current.gatewayGroupId);
                    const subscriptionGroup = currentGroup?.subscriptionType === "subscription" ? currentGroup : firstSubscriptionGroup;
                    return {
                      ...current,
                      benefitType,
                      value: benefitType === "subscription" ? 0 : current.value || 20,
                      validityDays: benefitType === "subscription" ? current.validityDays || 7 : 0,
                      gatewayGroupId: benefitType === "subscription" ? subscriptionGroup?.id ?? "" : "",
                      externalGroupId: benefitType === "subscription" ? subscriptionGroup?.externalGroupId ?? 0 : 0
                    };
                  });
                }}
                value={productForm.benefitType}
              >
                <option value="balance">Balance</option>
                <option value="subscription">Subscription</option>
              </select>
            </label>
            <label>
              售价 RMB
              <input
                min={0}
                onChange={(event) => setProductForm((current) => ({ ...current, priceCny: Number(event.target.value) }))}
                type="number"
                value={productForm.priceCny}
              />
            </label>
            <label>
              原价 RMB
              <input
                min={0}
                onChange={(event) =>
                  setProductForm((current) => ({
                    ...current,
                    originalPriceCny: event.target.value === "" ? null : Number(event.target.value)
                  }))
                }
                placeholder="可不填"
                type="number"
                value={productForm.originalPriceCny ?? ""}
              />
            </label>
            <label>
              余额额度 USD
              <input
                disabled={productForm.benefitType === "subscription"}
                onChange={(event) => setProductForm((current) => ({ ...current, value: Number(event.target.value) }))}
                type="number"
                value={productForm.value}
              />
            </label>
            <label>
              有效天数
              <input
                disabled={productForm.benefitType === "balance"}
                min={0}
                onChange={(event) => setProductForm((current) => ({ ...current, validityDays: Number(event.target.value) }))}
                type="number"
                value={productForm.validityDays}
              />
            </label>
            <label>
              分组
              <select
                onChange={(event) => {
                  const group = gatewayGroups.data?.items.find((item) => item.id === event.target.value);
                  setProductForm((current) => ({
                    ...current,
                    gatewayGroupId: event.target.value,
                    externalGroupId: group?.externalGroupId ?? 0
                  }));
                }}
                value={productForm.gatewayGroupId ?? ""}
              >
                <option value="">无分组</option>
                {groupRows.map((group) => (
                  <option key={group.id} value={group.id}>
                    {groupOptionLabel(group)}
                  </option>
                ))}
              </select>
            </label>
            <GroupGuardCard
              benefitType={productForm.benefitType}
              className="wide-field"
              emptyText="余额商品可以不绑定分组或绑定 active standard 分组；订阅商品必须选择 active subscription 分组。"
              group={productFormGroup}
              guard={productFormGuard}
              title="商品分组校验"
              validityDays={productForm.validityDays}
            />
            <label>
              状态
              <select
                onChange={(event) => setProductForm((current) => ({ ...current, status: event.target.value }))}
                value={productForm.status}
              >
                <option value="active">上架 / Active</option>
                <option value="disabled">下架 / Disabled</option>
                <option value="archived">归档 / Archived</option>
              </select>
            </label>
            <label>
              默认来源
              <input
                onChange={(event) => setProductForm((current) => ({ ...current, source: event.target.value }))}
                placeholder="ldxp / campus-a / agent-01"
                value={productForm.source}
              />
              <SourcePresetButtons
                onSelect={(value) => setProductForm((current) => ({ ...current, source: value }))}
              />
            </label>
            <label>
              排序
              <input
                onChange={(event) => setProductForm((current) => ({ ...current, sortOrder: Number(event.target.value) }))}
                type="number"
                value={productForm.sortOrder}
              />
            </label>
            <label className="wide-field">
              描述
              <textarea
                onChange={(event) => setProductForm((current) => ({ ...current, description: event.target.value }))}
                placeholder="给运营看的商品说明"
                value={productForm.description}
              />
            </label>
            <label className="wide-field">
              商品详情
              <textarea
                onChange={(event) => setProductForm((current) => ({ ...current, features: event.target.value }))}
                placeholder="每行一个卖点或说明"
                value={productForm.features}
              />
            </label>
            <label className="inline-checkbox">
              <input
                checked={productForm.forSale}
                onChange={(event) => setProductForm((current) => ({ ...current, forSale: event.target.checked }))}
                type="checkbox"
              />
              <span>可售</span>
            </label>
          </div>
          <div className="button-row product-actions">
            <button className="primary-action" disabled={!canSaveProduct} onClick={saveProduct} type="button">
              <Save size={16} />
              <span>{editingProductId ? "保存修改" : "添加商品"}</span>
            </button>
            {editingProductId ? (
              <button className="secondary-action" onClick={resetProductForm} type="button">
                <X size={16} />
                <span>取消</span>
              </button>
            ) : null}
          </div>
          {createProductMutation.isError || updateProductMutation.isError ? (
            <div className="form-error">商品保存失败，请检查 SKU 是否重复，订阅商品是否绑定了有效分组。</div>
          ) : null}
          {productNotice ? <div className="form-success">{productNotice}</div> : null}
          <DataTable
            rows={productRows}
            getRowKey={(row) => row.id}
            columns={[
              { key: "name", header: "商品", render: (row) => row.name },
              { key: "sku", header: "SKU", render: (row) => <code>{row.sku}</code> },
              { key: "benefit", header: "权益", render: (row) => formatBenefit(row.benefitType, row.value, row.validityDays) },
              { key: "price", header: "价格", render: (row) => `¥${row.priceCny.toFixed(2)}`, align: "right" },
              {
                key: "sale",
                header: "状态",
                render: (row) => (
                  <StatusBadge tone={productStatusTone(row)}>{formatProductStatus(row)}</StatusBadge>
                )
              },
              { key: "group", header: "分组", render: (row) => <ProductGroupCell groups={groupRows} product={row} /> },
              { key: "source", header: "来源", render: (row) => formatSourceLabel(row.source) },
              {
                key: "actions",
                header: "操作",
                render: (row) => (
                  <div className="compact-actions">
                    <button className="secondary-action" onClick={() => startEditProduct(row)} type="button">
                      <Pencil size={14} />
                      <span>编辑</span>
                    </button>
                    <button
                      className="danger-action"
                      disabled={deleteProductMutation.isPending || row.status === "archived"}
                      onClick={() => setArchiveProduct(row)}
                      type="button"
                    >
                      <Archive size={14} />
                      <span>{row.status === "archived" ? "已归档" : "归档"}</span>
                    </button>
                  </div>
                )
              }
            ]}
          />
        </article>
        <article className="panel batch-admin-panel">
          <div className="panel-heading">
            <h3>批次</h3>
            <StatusBadge tone="neutral">{String(batches.data?.total ?? 0)}</StatusBadge>
          </div>
          <div className="filter-panel nested-filter-panel">
            <div className="search-box full">
              <Search size={16} />
              <input
                onChange={(event) => {
                  setBatchSearch(event.target.value);
                  resetBatchOffset();
                }}
                placeholder="搜索批次、商品、SKU、来源、订单号、备注"
                value={batchSearch}
              />
            </div>
            <div className="filter-grid">
              <label>
                <span>状态</span>
                <select
                  onChange={(event) => {
                    setBatchStatus(event.target.value);
                    resetBatchOffset();
                  }}
                  value={batchStatus}
                >
                  <option value="all">全部状态</option>
                  <option value="active">Active</option>
                  <option value="disabled">Disabled</option>
                  <option value="archived">Archived</option>
                </select>
              </label>
              <label>
                <span>商品</span>
                <select
                  onChange={(event) => {
                    setBatchProductId(event.target.value);
                    resetBatchOffset();
                  }}
                  value={batchProductId}
                >
                  <option value="all">全部商品</option>
                  {productRows.map((product) => (
                    <option key={product.id} value={product.id}>
                      {product.name} · {product.sku}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>来源</span>
                <input
                  onChange={(event) => {
                    setBatchSource(event.target.value);
                    resetBatchOffset();
                  }}
                  placeholder="ldxp / manual"
                  value={batchSource}
                />
              </label>
              <label>
                <span>开始日期</span>
                <input
                  onChange={(event) => {
                    setBatchDateFrom(event.target.value);
                    resetBatchOffset();
                  }}
                  type="date"
                  value={batchDateFrom}
                />
              </label>
              <label>
                <span>结束日期</span>
                <input
                  onChange={(event) => {
                    setBatchDateTo(event.target.value);
                    resetBatchOffset();
                  }}
                  type="date"
                  value={batchDateTo}
                />
              </label>
              <label>
                <span>每页</span>
                <select
                  onChange={(event) => {
                    setBatchPageSize(Number(event.target.value));
                    setBatchOffset(0);
                  }}
                  value={batchPageSize}
                >
                  {pageSizeOptions.map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              </label>
              <button className="secondary-action" onClick={clearBatchFilters} type="button">
                <FilterX size={15} />
                <span>清空筛选</span>
              </button>
            </div>
          </div>
          {batches.isLoading ? <div className="inline-state">正在加载批次...</div> : null}
          {batches.isError ? <div className="inline-state danger-text">批次加载失败。</div> : null}
          {disableBatchMutation.isError ? <div className="inline-state danger-text">批次作废失败：{disableBatchMutation.error.message}</div> : null}
          <DataTable
            rows={batches.data?.items ?? []}
            getRowKey={(row) => row.id}
            columns={[
              { key: "name", header: "批次", render: (row) => row.name },
              { key: "product", header: "商品", render: (row) => row.productName || "-" },
              { key: "source", header: "来源", render: (row) => formatSourceLabel(row.source) },
              { key: "orderRef", header: "订单号", render: (row) => row.orderRef || "-" },
              { key: "quantity", header: "数量", render: (row) => row.quantity, align: "right" },
              { key: "unused", header: "未用", render: (row) => row.unusedCount, align: "right" },
              { key: "used", header: "已用", render: (row) => row.usedCount, align: "right" },
              { key: "status", header: "状态", render: (row) => <StatusBadge tone={row.status === "active" ? "ok" : "neutral"}>{row.status}</StatusBadge> },
              {
                key: "notes",
                header: "备注",
                render: (row) => <span className="clipped-cell">{formatNotes(row.notes)}</span>
              },
              { key: "created", header: "创建", render: (row) => formatTime(row.createdAt), align: "right" },
              {
                key: "actions",
                header: "操作",
                align: "right",
                render: (row) =>
                  row.status === "active" && row.unusedCount > 0 ? (
                    <button
                      className="danger-action"
                      disabled={disableBatchMutation.isPending}
                      onClick={() => setDisableBatchTarget(row)}
                      type="button"
                    >
                      <Ban size={14} />
                      <span>作废未用</span>
                    </button>
                  ) : (
                    <span className="muted-cell">-</span>
                  )
              }
            ]}
          />
          <PaginationRow
            total={batches.data?.total ?? 0}
            limit={batchPageSize}
            offset={batchOffset}
            isFetching={batches.isFetching}
            onOffsetChange={setBatchOffset}
          />
        </article>
      </section>

      <section className="panel filter-panel">
        <div className="panel-heading">
          <h3>卡密</h3>
          <StatusBadge tone="neutral">{String(codes.data?.total ?? 0)}</StatusBadge>
        </div>
        <div className="search-box full">
          <Search size={16} />
          <input
            onChange={(event) => {
              setCodeSearch(event.target.value);
              resetCodeOffset();
            }}
            placeholder="搜索卡密前缀、SKU、批次、兑换用户、订单号、备注"
            value={codeSearch}
          />
        </div>
        <div className="filter-grid">
          <label>
            <span>状态</span>
            <select
              onChange={(event) => {
                setCodeStatus(event.target.value);
                resetCodeOffset();
              }}
              value={codeStatus}
            >
              <option value="all">全部状态</option>
              <option value="unused">Unused</option>
              <option value="used">Used</option>
              <option value="expired">Expired</option>
              <option value="disabled">Disabled</option>
            </select>
          </label>
          <label>
            <span>类型</span>
            <select
              onChange={(event) => {
                setCodeType(event.target.value);
                resetCodeOffset();
              }}
              value={codeType}
            >
              <option value="all">全部类型</option>
              <option value="balance">Balance</option>
              <option value="subscription">Subscription</option>
            </select>
          </label>
          <label>
            <span>商品</span>
            <select
              onChange={(event) => {
                setCodeProductId(event.target.value);
                resetCodeOffset();
              }}
              value={codeProductId}
            >
              <option value="all">全部商品</option>
              {productRows.map((product) => (
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
                setCodeBatchId(event.target.value);
                resetCodeOffset();
              }}
              value={codeBatchId}
            >
              <option value="all">全部批次</option>
              {batchOptionRows.map((batch) => (
                <option key={batch.id} value={batch.id}>
                  {batch.name} · {batch.productName || "未关联"}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>来源</span>
            <input
              onChange={(event) => {
                setCodeSource(event.target.value);
                resetCodeOffset();
              }}
              placeholder="ldxp / manual"
              value={codeSource}
            />
          </label>
          <label>
            <span>兑换用户</span>
            <input
              onChange={(event) => {
                setCodeUsedBy(event.target.value);
                resetCodeOffset();
              }}
              placeholder="邮箱 / 用户 ID"
              value={codeUsedBy}
            />
          </label>
          <label>
            <span>开始日期</span>
            <input
              onChange={(event) => {
                setCodeDateFrom(event.target.value);
                resetCodeOffset();
              }}
              type="date"
              value={codeDateFrom}
            />
          </label>
          <label>
            <span>结束日期</span>
            <input
              onChange={(event) => {
                setCodeDateTo(event.target.value);
                resetCodeOffset();
              }}
              type="date"
              value={codeDateTo}
            />
          </label>
          <label>
            <span>每页</span>
            <select
              onChange={(event) => {
                setCodePageSize(Number(event.target.value));
                setCodeOffset(0);
              }}
              value={codePageSize}
            >
              {pageSizeOptions.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
          <button className="secondary-action" onClick={clearCodeFilters} type="button">
            <FilterX size={15} />
            <span>清空筛选</span>
          </button>
        </div>
      </section>

      {codes.isLoading ? <div className="panel inline-state">正在加载兑换码...</div> : null}
      {codes.isError ? <div className="panel inline-state danger-text">兑换码加载失败。</div> : null}
      {disableCodeMutation.isError ? <div className="panel inline-state danger-text">卡密作废失败：{disableCodeMutation.error.message}</div> : null}
      <DataTable
        rows={codes.data?.items ?? []}
        getRowKey={(row) => row.id}
        columns={[
          { key: "code", header: "卡密", render: (row) => <code>{row.maskedCode}</code> },
          { key: "product", header: "商品", render: (row) => row.productName || "-" },
          { key: "kind", header: "类型", render: (row) => row.kind },
          { key: "value", header: "权益", render: (row) => formatBenefit(row.kind, row.value, row.validityDays), align: "right" },
          {
            key: "status",
            header: "状态",
            render: (row) => <StatusBadge tone={row.status === "unused" ? "ok" : "neutral"}>{row.status}</StatusBadge>
          },
          { key: "source", header: "来源", render: (row) => formatSourceLabel(row.source) },
          { key: "batch", header: "批次", render: (row) => row.batchName || "-" },
          { key: "orderRef", header: "订单号", render: (row) => row.orderRef || "-" },
          {
            key: "notes",
            header: "备注",
            render: (row) => <span className="clipped-cell">{formatNotes(row.notes)}</span>
          },
          { key: "usedBy", header: "兑换用户", render: (row) => row.usedByEmail || row.usedByUserId || "-" },
          { key: "time", header: "创建", render: (row) => formatTime(row.createdAt), align: "right" },
          {
            key: "actions",
            header: "操作",
            align: "right",
            render: (row) =>
              row.status === "unused" ? (
                <button
                  className="danger-action"
                  disabled={disableCodeMutation.isPending}
                  onClick={() => setDisableCodeTarget(row)}
                  type="button"
                >
                  <Ban size={14} />
                  <span>作废</span>
                </button>
              ) : (
                <span className="muted-cell">-</span>
              )
          }
        ]}
      />
      <PaginationRow
        total={codes.data?.total ?? 0}
        limit={codePageSize}
        offset={codeOffset}
        isFetching={codes.isFetching}
        onOffsetChange={setCodeOffset}
      />
      <DangerConfirmModal
        open={Boolean(archiveProduct)}
        title="归档商品"
        description={`将归档商品 ${archiveProduct?.name ?? ""}，归档后不能再用于新批次，历史兑换记录仍会保留。`}
        confirmLabel="确认归档"
        pending={deleteProductMutation.isPending}
        onCancel={() => setArchiveProduct(null)}
        onConfirm={(auditReason) => {
          if (archiveProduct) deleteProductMutation.mutate({ id: archiveProduct.id, auditReason });
        }}
      />
      <DangerConfirmModal
        open={Boolean(disableBatchTarget)}
        title="作废批次未用卡密"
        description={`将作废批次 ${disableBatchTarget?.name ?? ""} 中 ${disableBatchTarget?.unusedCount ?? 0} 张未使用卡密；已兑换卡密不会回滚。`}
        confirmLabel="确认作废"
        pending={disableBatchMutation.isPending}
        onCancel={() => setDisableBatchTarget(null)}
        onConfirm={(auditReason) => {
          if (disableBatchTarget) disableBatchMutation.mutate({ id: disableBatchTarget.id, auditReason });
        }}
      />
      <DangerConfirmModal
        open={Boolean(disableCodeTarget)}
        title="作废卡密"
        description={`将作废 ${disableCodeTarget?.maskedCode ?? ""}，只允许未使用卡密作废；已兑换权益不会回滚。`}
        confirmLabel="确认作废"
        pending={disableCodeMutation.isPending}
        onCancel={() => setDisableCodeTarget(null)}
        onConfirm={(auditReason) => {
          if (disableCodeTarget) disableCodeMutation.mutate({ id: disableCodeTarget.id, auditReason });
        }}
      />
    </div>
  );
}
