export type ServiceHealth = {
  status: string;
  surface?: string;
};

export type AdminAccount = {
  id: string;
  email: string;
  role: string;
};

export type AdminLoginResult = {
  admin?: AdminAccount;
  totpRequired?: boolean;
};

export type AdminTOTPStatus = {
  enabled: boolean;
};

export type AdminTOTPSetup = {
  secret: string;
  otpauthUrl: string;
  qrPngDataUrl: string;
  expiresInSeconds: number;
};

export type AdminUser = {
  id: string;
  email: string;
  status: string;
  balance: number;
  defaultGroupId: number;
  gatewayEmail: string;
  gatewayStatus: string;
  deviceCount: number;
  lastSeenAt: string | null;
  createdAt: string;
};

export type AdminUserUpdateInput = {
  status: "active" | "disabled";
  cascadeDisableKeys?: boolean;
  auditReason?: string;
};

export type AdminUserProvisionResult = {
  user: AdminUser;
  generatedPassword: string;
  gatewayAccount?: GatewayAccount | null;
  apiKey?: GatewayAPIKey | null;
  plainApiKey?: string;
  gatewayWarning?: string;
  managementMode: string;
  managementWarning?: string;
  created?: boolean;
  balanceAdjusted?: boolean;
};

export type AdminUserFilters = {
  search?: string;
  status?: string;
  sync?: string;
  groupId?: string;
  minBalance?: string;
  maxBalance?: string;
  limit?: number;
  offset?: number;
};

export type Sub2APIUserSyncResult = {
  status: string;
  seen: number;
  synced: number;
  skipped: number;
  balanceAdjusted: number;
  note: string;
};

export type GatewayGroup = {
  id: string;
  provider: string;
  externalGroupId: number;
  name: string;
  description: string;
  platform: string;
  subscriptionType: string;
  rateMultiplier: number;
  isExclusive: boolean;
  dailyLimitUsd: number | null;
  weeklyLimitUsd: number | null;
  monthlyLimitUsd: number | null;
  defaultValidityDays: number;
  rpmLimit: number;
  sortOrder: number;
  allowImageGeneration: boolean;
  imageRateIndependent: boolean;
  imageRateMultiplier: number;
  imagePrice1k: number | null;
  imagePrice2k: number | null;
  imagePrice4k: number | null;
  claudeCodeOnly: boolean;
  fallbackGroupId: number | null;
  fallbackGroupIdOnInvalidRequest: number | null;
  modelRouting: Record<string, number[]>;
  modelRoutingEnabled: boolean;
  mcpXmlInject: boolean;
  supportedModelScopes: string[];
  allowMessagesDispatch: boolean;
  requireOauthOnly: boolean;
  requirePrivacySet: boolean;
  defaultMappedModel: string;
  messagesDispatchModelConfig: Record<string, unknown>;
  status: string;
  createdAt: string;
  updatedAt: string;
  models: GatewayGroupModel[];
  accounts: GatewayUpstreamAccount[];
  channels: GatewayChannel[];
  officialModelConfig: GatewayGroupOfficialModelConfig;
  upstreamAccountCount: number;
  activeSchedulableAccountCount: number;
  channelCount: number;
  pricedModelCount: number;
  unpricedModelCount: number;
};

export type GatewayGroupOfficialPurposeConfig = {
  modelIds: string[];
  defaultModelId: string;
};

export type GatewayGroupOfficialModelConfig = {
  embedding: GatewayGroupOfficialPurposeConfig;
  vision: GatewayGroupOfficialPurposeConfig;
};

export type GatewayGroupModel = {
  id: string;
  externalChannelId: number;
  platform: string;
  modelId: string;
  displayName: string;
  providerFamily: string;
  capabilities: string[];
  pricing: {
    models?: string[];
    billing_mode?: string;
    input_price?: number | null;
    output_price?: number | null;
    cache_write_price?: number | null;
    cache_read_price?: number | null;
    image_output_price?: number | null;
    per_request_price?: number | null;
    intervals?: Array<{
      id?: number;
      min_tokens?: number;
      max_tokens?: number | null;
      tier_label?: string;
      input_price?: number | null;
      output_price?: number | null;
      cache_write_price?: number | null;
      cache_read_price?: number | null;
      per_request_price?: number | null;
      sort_order?: number;
    }>;
  };
  billingMode: string;
  status: string;
  lastSyncedAt: string | null;
  sourceType: "account_mapping" | "channel_mapping" | "channel_pricing" | string;
  pricingStatus: "configured" | "missing" | string;
  channelName: string;
};

export type GatewayChannel = {
  id: string;
  externalChannelId: number;
  name: string;
  description: string;
  status: string;
  billingModelSource: string;
  restrictModels: boolean;
  groupIds: number[];
  modelMapping: Record<string, Record<string, string>>;
  modelPricing: unknown[];
  pricingCount: number;
  lastSyncedAt: string | null;
};

export type GatewayUpstreamAccount = {
  id: string;
  externalAccountId: number;
  name: string;
  platform: string;
  accountType: string;
  status: string;
  schedulable: boolean;
  concurrency: number;
  currentConcurrency: number;
  priority: number;
  rateMultiplier: number;
  errorMessage: string;
  groupIds: number[];
  mappedModels: string[];
  mappedModelCount: number;
  lastUsedAt: string | null;
  expiresAt: string | null;
  rateLimitedAt: string | null;
  rateLimitResetAt: string | null;
  overloadUntil: string | null;
  tempUnschedulableUntil: string | null;
  lastSyncedAt: string | null;
};

export type Sub2APISettings = {
  baseUrl: string;
  adminEmail: string;
  hasAdminPassword: boolean;
  adminApiKeyConfigured: boolean;
  authMode: "admin_api_key" | "admin_credentials" | "not_configured" | string;
  defaultGroupId: number;
};

export type Sub2APISettingsInput = {
  baseUrl: string;
  adminEmail: string;
  adminPassword?: string;
  defaultGroupId?: number;
};

export type Sub2APIGroupPreview = {
  externalGroupId: number;
  name: string;
  platform: string;
  subscriptionType: string;
  rateMultiplier: number;
  rpmLimit: number;
  sortOrder: number;
  status: string;
};

export type Sub2APITestResult = {
  ok: boolean;
  status: string;
  baseUrl: string;
  authMode: string;
  healthOk: boolean;
  authOk: boolean;
  groupCount: number;
  latencyMs: number;
  error: string;
  groupsPreview: Sub2APIGroupPreview[];
};

export type Sub2APISyncGroupsResult = {
  status: string;
  synced: number;
  total: number;
  groups: Sub2APIGroupPreview[];
};

export type Sub2APISyncModelsResult = {
  status: string;
  syncedGroups: number;
  syncedAccounts: number;
  syncedChannels: number;
  syncedModels: number;
  totalGroups: number;
  totalAccounts: number;
  totalChannels: number;
  groups: Sub2APIGroupPreview[];
};

export type Product = {
  id: string;
  sku: string;
  name: string;
  description: string;
  benefitType: "balance" | "subscription" | string;
  priceCny: number;
  originalPriceCny: number | null;
  value: number;
  validityDays: number;
  gatewayGroupId: string;
  gatewayGroupName: string;
  externalGroupId: number;
  source: string;
  features: string;
  forSale: boolean;
  sortOrder: number;
  status: string;
  createdAt: string;
  updatedAt: string;
};

export type ProductInput = {
  sku: string;
  name: string;
  description: string;
  benefitType: "balance" | "subscription";
  priceCny: number;
  originalPriceCny?: number | null;
  value: number;
  validityDays: number;
  gatewayGroupId?: string;
  externalGroupId?: number;
  source: string;
  features: string;
  forSale: boolean;
  sortOrder: number;
  status: string;
};

export type RedeemCodeBatch = {
  id: string;
  name: string;
  source: string;
  orderRef: string;
  quantity: number;
  status: string;
  notes: string;
  productId: string;
  productName: string;
  unusedCount: number;
  usedCount: number;
  createdAt: string;
};

export type ListMeta = {
  total: number;
  limit?: number;
  offset?: number;
};

export type PagedItems<T> = ListMeta & {
  items: T[];
};

export type AdminListFilters = {
  search?: string;
  status?: string;
  type?: string;
  source?: string;
  productId?: string;
  batchId?: string;
  user?: string;
  usedBy?: string;
  action?: string;
  actorType?: string;
  operation?: string;
  provider?: string;
  platform?: string;
  externalUserId?: string;
  externalGroupId?: string;
  errorClass?: string;
  retryable?: string;
  sortBy?: string;
  sortOrder?: string;
  dateFrom?: string;
  dateTo?: string;
  limit?: number;
  offset?: number;
};

export type RedeemCode = {
  id: string;
  maskedCode: string;
  codePrefix: string;
  kind: string;
  value: number;
  validityDays: number;
  status: string;
  orderRef: string;
  notes: string;
  productId: string;
  productName: string;
  productSku: string;
  batchId: string;
  batchName: string;
  externalGroupId: number;
  source: string;
  usedByUserId: string;
  usedByEmail: string;
  usedAt: string | null;
  expiresAt: string | null;
  createdAt: string;
};

export type Redemption = {
  id: string;
  redeemCodeId: string;
  userId: string;
  userEmail: string;
  productName: string;
  batchName: string;
  orderRef: string;
  kind: string;
  value: number;
  validityDays: number;
  externalUserId: number;
  externalGroupId: number;
  gatewayOperation: string;
  status: string;
  errorMessage: string;
  errorCode: string;
  errorClass: string;
  errorStage: string;
  errorRetryable: boolean;
  errorDetail: string;
  operationId: string;
  operationStatus: string;
  operationAttempts: number;
  operationMaxAttempts: number;
  operationNextRunAt: string | null;
  createdAt: string;
};

export type GatewayAPIKey = {
  id: string;
  provider: string;
  externalKeyId: number;
  externalGroupId: number;
  maskedApiKey: string;
  status: string;
  userId: string;
  userEmail: string;
  remoteSync: string;
  syncWarning?: string;
  lastUsedAt: string | null;
  createdAt: string;
};

export type WalletTransaction = {
  id: string;
  kind: string;
  amount: number;
  balanceAfter: number;
  source: string;
  referenceId: string;
  notes: string;
  createdAt: string;
};

export type UserDevice = {
  id: string;
  name: string;
  platform: string;
  status: string;
  lastSeenAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type GatewayAccount = {
  id: number;
  provider: string;
  externalUserId: number;
  externalEmail: string;
  defaultGroupId: number;
  concurrency: number;
  status: string;
  lastSyncedAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type GatewayOperation = {
  id: string;
  provider: string;
  operation: string;
  status: string;
  targetType: string;
  targetId: string;
  redemptionId: string;
  userId: string;
  userEmail: string;
  idempotencyKey: string;
  attempts: number;
  maxAttempts: number;
  nextRunAt: string | null;
  lockedAt: string | null;
  lockedBy: string;
  startedAt: string | null;
  completedAt: string | null;
  lastErrorMessage: string;
  lastErrorCode: string;
  lastErrorClass: string;
  lastErrorStage: string;
  lastErrorRetryable: boolean;
  lastErrorDetail: string;
  payload: string;
  result: string;
  createdAt: string;
  updatedAt: string;
};

export type AdminSubscriptionUser = {
  id: number;
  email: string;
  username: string;
  role: string;
  balance: number;
  concurrency: number;
  rpmLimit: number;
  status: string;
  allowedGroups: number[];
  lastActiveAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type AdminSubscriptionGroup = {
  id: number;
  name: string;
  description: string;
  platform: string;
  subscriptionType: string;
  dailyLimitUsd: number | null;
  weeklyLimitUsd: number | null;
  monthlyLimitUsd: number | null;
  rpmLimit: number;
  rateMultiplier: number;
  isExclusive: boolean;
  status: string;
  allowImageGeneration: boolean;
  claudeCodeOnly: boolean;
};

export type AdminSubscription = {
  id: number;
  userId: number;
  groupId: number;
  startsAt: string;
  expiresAt: string;
  status: string;
  dailyWindowStart: string | null;
  weeklyWindowStart: string | null;
  monthlyWindowStart: string | null;
  dailyUsageUsd: number;
  weeklyUsageUsd: number;
  monthlyUsageUsd: number;
  createdAt: string;
  updatedAt: string;
  user?: AdminSubscriptionUser;
  group?: AdminSubscriptionGroup;
  assignedBy: number | null;
  assignedAt: string;
  notes: string;
  assignedByUser?: AdminSubscriptionUser;
};

export type AssignSubscriptionInput = {
  externalUserId: number;
  externalGroupId: number;
  validityDays: number;
  notes?: string;
  idempotencyKey?: string;
  auditReason?: string;
};

export type ExtendSubscriptionInput = {
  days: number;
  auditReason?: string;
};

export type ResetSubscriptionQuotaInput = {
  daily: boolean;
  weekly: boolean;
  monthly: boolean;
  auditReason?: string;
};

export type DiagnosticCheck = {
  status: string;
  detail: string;
  latencyMs: number;
  checkedAt: string;
};

export type WorkerCheck = {
  status: string;
  detail: string;
  workerId: string;
  lastSeenAt: string | null;
  ageSeconds: number;
  checkedAt: string;
};

export type DiagnosticsSnapshot = {
  generatedAt: string;
  services: {
    api: DiagnosticCheck;
    postgres: DiagnosticCheck;
    redis: DiagnosticCheck;
    worker: WorkerCheck;
  };
  queue: {
    total: number;
    pending: number;
    running: number;
    succeeded: number;
    failed: number;
    deadLetter: number;
    retryableFailed: number;
    readyNow: number;
    dueSoon: number;
    staleRunning: number;
    lastSucceededAt: string | null;
    lastFailedAt: string | null;
    lastOperationAt: string | null;
  };
  sub2api: {
    status: string;
    ok: boolean;
    baseUrl: string;
    authMode: string;
    healthOk: boolean;
    authOk: boolean;
    groupCount: number;
    latencyMs: number;
    error: string;
    checkedAt: string;
  };
  productionReadiness: Array<{
    key: string;
    label: string;
    status: string;
    detail: string;
    action: string;
    section: string;
  }>;
};

export type AdminOverview = {
  summary: {
    activeUsers: number;
    usersToday: number;
    totalUsers: number;
    walletBalanceUsd: number;
    activeKeys: number;
    reviewKeys: number;
    redemptionsToday: number;
    gatewayFailedToday: number;
    requestCountToday: number;
    costTodayUsd: number;
    usageStatus: string;
  };
  recentRedemptions: Redemption[];
  generatedAt: string;
};

export type UsageSummary = {
  usage: {
    status: string;
    source: string;
    requestCountToday: number;
    inputTokensToday: number;
    outputTokensToday: number;
    costTodayUsd: number;
    actualCostUsd: number;
  };
  ledger: {
    walletBalanceUsd: number;
    walletCreditsTodayUsd: number;
    walletCreditsTotalUsd: number;
    balanceRedeemedTodayUsd: number;
    balanceRedeemedTotalUsd: number;
    redemptionCountToday: number;
    subscriptionCountToday: number;
    gatewayFailedToday: number;
  };
  attribution: {
    products: Array<{
      productId: string;
      sku: string;
      name: string;
      benefitType: string;
      redeemedCount: number;
      balanceValueUsd: number;
      subscriptionCount: number;
      revenueCny: number;
      lastRedeemedAt: string | null;
    }>;
    groups: Array<{
      externalGroupId: number;
      name: string;
      subscriptionType: string;
      redeemedCount: number;
      balanceValueUsd: number;
      subscriptionCount: number;
      activeKeyCount: number;
    }>;
    users: Array<{
      userId: string;
      email: string;
      walletBalanceUsd: number;
      redeemedCount: number;
      balanceValueUsd: number;
      subscriptionCount: number;
      gatewayFailedCount: number;
      lastRedeemedAt: string | null;
    }>;
  };
};

export type ModelCatalogItem = {
  id: string;
  displayName: string;
  providerFamily: string;
  capabilities: string[];
  publicVisible: boolean;
  supportsStreaming: boolean;
  status: string;
  updatedAt: string;
};

export type AuditLog = {
  id: string;
  actorType: string;
  actorId: number;
  actorLabel: string;
  action: string;
  actionLabel: string;
  targetType: string;
  targetId: string;
  ip: string;
  userAgent: string;
  metadata: string;
  summary: string;
  resultTone: "ok" | "warn" | "danger" | "neutral" | string;
  createdAt: string;
};

export type GenerateRedeemCodesInput = {
  productId: string;
  count: number;
  batchName: string;
  source: string;
  orderRef?: string;
  notes?: string;
  expiresInDays?: number;
};

export type AuditReasonInput = {
  auditReason?: string;
};

export type GenerateRedeemCodesResult = {
  batch: {
    id: string;
    name: string;
    quantity: number;
    source: string;
    orderRef: string;
    notes: string;
  };
  product: {
    id: string;
    sku: string;
    name: string;
    benefitType: string;
    value: number;
    validityDays: number;
    externalGroupId: number;
  };
  codes: Array<{
    code: string;
    maskedCode: string;
    codePrefix: string;
  }>;
};

export type GrantBalanceInput = {
  amount: number;
  notes?: string;
  syncSub2api?: boolean;
  idempotencyKey?: string;
  auditReason?: string;
};

export type GrantBalanceResult = {
  status: string;
  transactionId: string;
  balance: number;
  syncWarning: string;
  syncOperation: string;
};

export type RotateAPIKeyInput = {
  externalGroupId?: number;
  auditReason?: string;
};

export type RotateAPIKeyResult = {
  apiKey: GatewayAPIKey;
  plainApiKey: string;
  syncWarnings: string[];
};

export type ChangeGatewayGroupInput = {
  externalGroupId: number;
  auditReason?: string;
};

export type ChangeGatewayGroupResult = RotateAPIKeyResult;

export type UpdateUserConcurrencyInput = {
  concurrency: number;
  auditReason?: string;
};

export type UpdateUserConcurrencyResult = {
  status: string;
  externalGroupId: number;
  concurrency: number;
  syncOperation?: string;
};

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function listQuery(params?: AdminListFilters) {
  const query = new URLSearchParams();
  const add = (key: keyof AdminListFilters) => {
    const value = params?.[key];
    if (value === undefined || value === null || value === "" || value === "all") return;
    query.set(key, String(value));
  };
  add("search");
  add("status");
  add("type");
  add("source");
  add("productId");
  add("batchId");
  add("user");
  add("usedBy");
  add("action");
  add("actorType");
  add("operation");
  add("provider");
  add("platform");
  add("externalUserId");
  add("externalGroupId");
  add("errorClass");
  add("retryable");
  add("sortBy");
  add("sortOrder");
  add("dateFrom");
  add("dateTo");
  add("limit");
  add("offset");
  return query.toString() ? `?${query.toString()}` : "";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    },
    ...init
  });

  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const payload = (await response.json()) as {
        error?: string | { code?: string; message?: string };
        detail?: string;
      };
      if (typeof payload.error === "string") {
        message = payload.detail ? `${payload.error}: ${payload.detail}` : payload.error;
      } else if (payload.error?.message) {
        message = payload.error.message;
      } else if (payload.error?.code) {
        message = payload.detail ? `${payload.error.code}: ${payload.detail}` : payload.error.code;
      } else if (payload.detail) {
        message = payload.detail;
      }
    } catch {
      // Keep the status text when the server did not return JSON.
    }
    throw new ApiError(response.status, message);
  }

  return response.json() as Promise<T>;
}

function createIdempotencyKey(scope: string) {
  if (globalThis.crypto?.randomUUID) {
    return `${scope}-${globalThis.crypto.randomUUID()}`;
  }
  return `${scope}-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export function getHealth() {
  return request<ServiceHealth>("/healthz");
}

export function getReady() {
  return request<ServiceHealth>("/readyz");
}

export function getAdminHealth() {
  return request<ServiceHealth>("/api/v1/admin/health");
}

export function adminLogin(input: { email: string; password: string; totpCode?: string }) {
  return request<AdminLoginResult>("/api/v1/admin/auth/login", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function adminLogout() {
  return request<{ status: string }>("/api/v1/admin/auth/logout", {
    method: "POST",
    body: JSON.stringify({})
  });
}

export function getAdminMe() {
  return request<{ admin: AdminAccount }>("/api/v1/admin/me");
}

export function getAdminTOTPStatus() {
  return request<AdminTOTPStatus>("/api/v1/admin/security/totp");
}

export function setupAdminTOTP() {
  return request<AdminTOTPSetup>("/api/v1/admin/security/totp/setup", {
    method: "POST",
    body: JSON.stringify({})
  });
}

export function enableAdminTOTP(code: string) {
  return request<AdminTOTPStatus>("/api/v1/admin/security/totp/enable", {
    method: "POST",
    body: JSON.stringify({ code })
  });
}

export function disableAdminTOTP(input: { password: string; code: string }) {
  return request<AdminTOTPStatus>("/api/v1/admin/security/totp/disable", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function getAdminOverview() {
  return request<AdminOverview>("/api/v1/admin/overview");
}

export function getDiagnostics() {
  return request<{ diagnostics: DiagnosticsSnapshot }>("/api/v1/admin/diagnostics");
}

export function getUsageSummary() {
  return request<UsageSummary>("/api/v1/admin/usage-summary");
}

export function getAdminUsers(params?: AdminUserFilters) {
  const query = new URLSearchParams();
  if (params?.search) query.set("search", params.search);
  if (params?.status && params.status !== "all") query.set("status", params.status);
  if (params?.sync && params.sync !== "all") query.set("sync", params.sync);
  if (params?.groupId) query.set("groupId", params.groupId);
  if (params?.minBalance) query.set("minBalance", params.minBalance);
  if (params?.maxBalance) query.set("maxBalance", params.maxBalance);
  if (typeof params?.limit === "number") query.set("limit", String(params.limit));
  if (typeof params?.offset === "number") query.set("offset", String(params.offset));
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return request<{ items: AdminUser[]; total: number }>(`/api/v1/admin/users${suffix}`);
}

export function getAdminUser(id: string) {
  return request<{ user: AdminUser }>(`/api/v1/admin/users/${encodeURIComponent(id)}`);
}

export function getUserAPIKeys(userId: string) {
  return request<{ items: GatewayAPIKey[]; total: number }>(
    `/api/v1/admin/users/${encodeURIComponent(userId)}/api-keys`
  );
}

export function getUserWalletTransactions(userId: string, limit = 50) {
  return request<{ items: WalletTransaction[]; total: number }>(
    `/api/v1/admin/users/${encodeURIComponent(userId)}/wallet-transactions?limit=${limit}`
  );
}

export function getUserDevices(userId: string) {
  return request<{ items: UserDevice[]; total: number }>(`/api/v1/admin/users/${encodeURIComponent(userId)}/devices`);
}

export function getUserGatewayAccounts(userId: string) {
  return request<{ items: GatewayAccount[]; total: number }>(
    `/api/v1/admin/users/${encodeURIComponent(userId)}/gateway-accounts`
  );
}

export function getUserSubscriptions(userId: string) {
  return request<{ items: AdminSubscription[]; total: number }>(
    `/api/v1/admin/users/${encodeURIComponent(userId)}/subscriptions`
  );
}

export function createAdminUser(input: {
  email: string;
  displayName?: string;
  password?: string;
  syncSub2api?: boolean;
  externalGroupId?: number;
}) {
  return request<AdminUserProvisionResult>("/api/v1/admin/users", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function importSub2APIUser(input: { email: string; displayName?: string; password?: string }) {
  return request<AdminUserProvisionResult>("/api/v1/admin/users/import-sub2api", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function updateAdminUser(id: string, input: AdminUserUpdateInput) {
  return request<{ user: AdminUser; syncWarning: string }>(`/api/v1/admin/users/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(input)
  });
}

export function grantUserBalance(id: string, input: GrantBalanceInput) {
  const idempotencyKey = input.idempotencyKey || createIdempotencyKey("admin-grant-balance");
  return request<GrantBalanceResult>(`/api/v1/admin/users/${encodeURIComponent(id)}/grant-balance`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ ...input, idempotencyKey })
  });
}

export function rotateUserAPIKey(id: string, input: RotateAPIKeyInput = {}) {
  return request<RotateAPIKeyResult>(`/api/v1/admin/users/${encodeURIComponent(id)}/api-keys/rotate`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function changeUserGatewayGroup(id: string, input: ChangeGatewayGroupInput) {
  return request<ChangeGatewayGroupResult>(`/api/v1/admin/users/${encodeURIComponent(id)}/gateway-group`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function updateUserConcurrency(id: string, input: UpdateUserConcurrencyInput) {
  return request<UpdateUserConcurrencyResult>(`/api/v1/admin/users/${encodeURIComponent(id)}/concurrency`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function deleteAdminUser(id: string, input: AuditReasonInput = {}) {
  return request<{ status: string; syncWarning: string }>(`/api/v1/admin/users/${encodeURIComponent(id)}`, {
    method: "DELETE",
    body: JSON.stringify(input)
  });
}

export function syncSub2APIUsers(input: AuditReasonInput = {}) {
  return request<Sub2APIUserSyncResult>("/api/v1/admin/users/sync-sub2api", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function getGatewayGroups() {
  return request<{ items: GatewayGroup[]; total: number }>("/api/v1/admin/gateway-groups");
}

export function updateGatewayGroupOfficialModels(
  externalGroupId: number,
  input: GatewayGroupOfficialModelConfig & AuditReasonInput
) {
  return request<{ officialModelConfig: GatewayGroupOfficialModelConfig }>(
    `/api/v1/admin/gateway-groups/${encodeURIComponent(String(externalGroupId))}/official-models`,
    {
      method: "PUT",
      body: JSON.stringify(input)
    }
  );
}

export function getSub2APISettings() {
  return request<{ settings: Sub2APISettings }>("/api/v1/admin/sub2api/settings");
}

export function updateSub2APISettings(input: Sub2APISettingsInput) {
  return request<{ settings: Sub2APISettings }>("/api/v1/admin/sub2api/settings", {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export function testSub2APIConnection() {
  return request<Sub2APITestResult>("/api/v1/admin/sub2api/test", {
    method: "POST",
    body: JSON.stringify({})
  });
}

export function syncSub2APIGroups(input: AuditReasonInput = {}) {
  return request<Sub2APISyncGroupsResult>("/api/v1/admin/sub2api/sync-groups", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function syncSub2APIModels(input: AuditReasonInput = {}) {
  return request<Sub2APISyncModelsResult>("/api/v1/admin/sub2api/sync-models", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function getProducts() {
  return request<{ items: Product[]; total: number }>("/api/v1/admin/products");
}

export function createProduct(input: ProductInput) {
  return request<{ product: Product }>("/api/v1/admin/products", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function updateProduct(id: string, input: ProductInput) {
  return request<{ product: Product }>(`/api/v1/admin/products/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export function deleteProduct(id: string, input: AuditReasonInput = {}) {
  return request<{ status: string; mode: "deleted" | "archived"; product?: Product }>(
    `/api/v1/admin/products/${encodeURIComponent(id)}`,
    { method: "DELETE", body: JSON.stringify(input) }
  );
}

export function getRedeemBatches(params?: AdminListFilters) {
  return request<PagedItems<RedeemCodeBatch>>(`/api/v1/admin/redeem-code-batches${listQuery(params)}`);
}

export function disableRedeemBatch(id: string, input: AuditReasonInput = {}) {
  return request<{ status: string; batch: RedeemCodeBatch; disabledCodes: number }>(
    `/api/v1/admin/redeem-code-batches/${encodeURIComponent(id)}/disable`,
    {
      method: "POST",
      body: JSON.stringify(input)
    }
  );
}

export function getRedeemCodes(params?: AdminListFilters) {
  return request<PagedItems<RedeemCode>>(`/api/v1/admin/redeem-codes${listQuery(params)}`);
}

export function disableRedeemCode(id: string, input: AuditReasonInput = {}) {
  return request<{ status: string; redeemCode: RedeemCode }>(`/api/v1/admin/redeem-codes/${encodeURIComponent(id)}/disable`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function generateRedeemCodes(input: GenerateRedeemCodesInput) {
  return request<GenerateRedeemCodesResult>("/api/v1/admin/redeem-codes/generate", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function getRedemptions(params?: AdminListFilters) {
  return request<PagedItems<Redemption>>(`/api/v1/admin/redemptions${listQuery(params)}`);
}

export function getSubscriptions(params?: AdminListFilters) {
  return request<PagedItems<AdminSubscription>>(`/api/v1/admin/subscriptions${listQuery(params)}`);
}

export function assignSubscription(input: AssignSubscriptionInput) {
  const idempotencyKey = input.idempotencyKey || createIdempotencyKey("admin-assign-subscription");
  return request<{ subscription: AdminSubscription }>("/api/v1/admin/subscriptions/assign", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ ...input, idempotencyKey })
  });
}

export function extendSubscription(id: number, input: ExtendSubscriptionInput) {
  return request<{ subscription: AdminSubscription }>(`/api/v1/admin/subscriptions/${encodeURIComponent(String(id))}/extend`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function resetSubscriptionQuota(id: number, input: ResetSubscriptionQuotaInput) {
  return request<{ subscription: AdminSubscription }>(`/api/v1/admin/subscriptions/${encodeURIComponent(String(id))}/reset-quota`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function revokeSubscription(id: number, input: AuditReasonInput = {}) {
  return request<{ status: string }>(`/api/v1/admin/subscriptions/${encodeURIComponent(String(id))}`, {
    method: "DELETE",
    body: JSON.stringify(input)
  });
}

export function retryRedemptionSync(id: string, input: AuditReasonInput = {}) {
  return request<{ status: string; redemption: Redemption }>(
    `/api/v1/admin/redemptions/${encodeURIComponent(id)}/retry-sync`,
    {
      method: "POST",
      body: JSON.stringify(input)
    }
  );
}

export function getGatewayOperations(params?: AdminListFilters) {
  return request<PagedItems<GatewayOperation>>(`/api/v1/admin/gateway-operations${listQuery(params)}`);
}

export function retryGatewayOperation(id: string, input: AuditReasonInput = {}) {
  return request<{ status: string; operation: GatewayOperation }>(
    `/api/v1/admin/gateway-operations/${encodeURIComponent(id)}/retry`,
    {
      method: "POST",
      body: JSON.stringify(input)
    }
  );
}

export function retryFailedGatewayOperations(input: AuditReasonInput = {}) {
  return request<{ status: string; retried: number }>("/api/v1/admin/gateway-operations/retry-failed", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function disableGatewayAPIKey(id: string, input: AuditReasonInput = {}) {
  return request<{ apiKey: GatewayAPIKey; syncWarning: string }>(
    `/api/v1/admin/api-keys/${encodeURIComponent(id)}/disable`,
    {
      method: "POST",
      body: JSON.stringify(input)
    }
  );
}

export function getModelCatalog() {
  return request<{ items: ModelCatalogItem[]; total: number }>("/api/v1/admin/models");
}

export function getAuditLogs(params?: AdminListFilters) {
  return request<PagedItems<AuditLog>>(`/api/v1/admin/audit-logs${listQuery(params)}`);
}
