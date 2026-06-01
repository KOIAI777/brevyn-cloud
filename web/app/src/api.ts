export type User = {
  id: string;
  email: string;
  displayName: string;
  status: string;
};

export type TokenPair = {
  accessToken: string;
  refreshToken: string;
  tokenType: string;
  expiresIn: number;
};

export type Wallet = {
  balance: number;
};

export type GatewayAccount = {
  provider: string;
  externalUserId: number;
  externalEmail: string;
  defaultGroupId: number;
  concurrency: number;
  status: string;
  lastSyncedAt: string | null;
};

export type GatewayGroup = {
  externalGroupId: number;
  name: string;
  description: string;
  platform: string;
  subscriptionType: string;
  rateMultiplier: number;
  dailyLimitUsd?: number;
  weeklyLimitUsd?: number;
  monthlyLimitUsd?: number;
  defaultValidityDays: number;
  rpmLimit: number;
  status: string;
  modelCount: number;
  source?: string;
  isCurrent: boolean;
};

export type APIKey = {
  id: string;
  provider: string;
  externalKeyId: number;
  externalGroupId: number;
  groupName?: string;
  groupType?: string;
  platform?: string;
  maskedApiKey: string;
  status: string;
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

export type ModelItem = {
  id: string;
  name: string;
  displayName: string;
  providerFamily: string;
  platform?: string;
  externalGroupId?: number;
  groupName?: string;
  billingMode?: string;
  capabilities: string[];
  supportsVision: boolean;
  supportsStreaming: boolean;
  enabled: boolean;
};

export type ProviderConfig = {
  purpose: string;
  providerKind: string;
  adapterKind: string;
  protocol: string;
  name: string;
  baseUrl: string;
  authMode: string;
  apiKey: string;
  selectedModel: string;
  enabled: boolean;
  models: ModelItem[];
};

export type Redemption = {
  id: string;
  codeId: string;
  productName: string;
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
  createdAt: string;
};

export type RedeemResult = {
  redemption: Redemption;
  wallet: Wallet;
  gateway: GatewayAccount;
  apiKey?: APIKey;
  plainApiKey?: string;
};

export type AuthResult = {
  user: User;
  tokens: TokenPair;
};

export type MeResult = {
  user: User;
  wallet: Wallet;
  gateway: GatewayAccount | null;
  currentGroup: GatewayGroup | null;
};

export type ModelsResult = {
  items: ModelItem[];
  total: number;
  externalGroupId: number;
};

export type OfficialProviderResult = {
  provider?: ProviderConfig;
  gateway?: GatewayAccount;
  apiKey?: APIKey | null;
  status?: string;
  error?: string;
  detail?: string;
  retryAfterSeconds?: number;
};

export type SystemAPIKeyResult = {
  key: string;
  baseUrl: string;
  apiKey: APIKey | null;
};

const ACCESS_KEY = "brevyn.app.access";
const REFRESH_KEY = "brevyn.app.refresh";
let refreshPromise: Promise<string> | null = null;

export class APIError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export function getAccessToken() {
  return localStorage.getItem(ACCESS_KEY) || "";
}

export function getRefreshToken() {
  return localStorage.getItem(REFRESH_KEY) || "";
}

export function saveTokens(tokens: TokenPair) {
  localStorage.setItem(ACCESS_KEY, tokens.accessToken);
  localStorage.setItem(REFRESH_KEY, tokens.refreshToken);
}

export function clearTokens() {
  localStorage.removeItem(ACCESS_KEY);
  localStorage.removeItem(REFRESH_KEY);
}

async function parseError(response: Response) {
  let code = response.statusText || "request_failed";
  let message = `${response.status} ${response.statusText}`;
  try {
    const payload = (await response.json()) as {
      error?: string | { code?: string; message?: string };
      detail?: string;
    };
    if (typeof payload.error === "string") {
      code = payload.error;
      message = payload.detail ? `${payload.error}: ${payload.detail}` : payload.error;
    } else if (payload.error?.code || payload.error?.message) {
      code = payload.error.code || code;
      message = payload.error.message || payload.error.code || message;
      if (payload.detail) message = `${message}: ${payload.detail}`;
    } else if (payload.detail) {
      message = payload.detail;
    }
  } catch {
    // Keep the HTTP status when the server returns no JSON body.
  }
  return new APIError(response.status, code, message);
}

async function refreshTokens() {
  const refreshToken = getRefreshToken();
  if (!refreshToken) throw new APIError(401, "unauthorized", "请先登录");
  const response = await fetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refreshToken })
  });
  if (!response.ok) {
    clearTokens();
    throw await parseError(response);
  }
  const payload = (await response.json()) as AuthResult;
  saveTokens(payload.tokens);
  return payload.tokens.accessToken;
}

function refreshTokensOnce() {
  if (!refreshPromise) {
    refreshPromise = refreshTokens().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

export async function request<T>(path: string, init: RequestInit = {}, auth = true): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (auth) {
    const token = getAccessToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
  }

  const response = await fetch(path, { ...init, headers });
  if (response.status === 401 && auth && getRefreshToken()) {
    const token = await refreshTokensOnce();
    headers.set("Authorization", `Bearer ${token}`);
    const retry = await fetch(path, { ...init, headers });
    if (!retry.ok) throw await parseError(retry);
    return retry.json() as Promise<T>;
  }
  if (!response.ok) throw await parseError(response);
  return response.json() as Promise<T>;
}

export async function login(email: string, password: string) {
  const result = await request<AuthResult>(
    "/api/v1/auth/login",
    { method: "POST", body: JSON.stringify({ email, password }) },
    false
  );
  saveTokens(result.tokens);
  return result;
}

export async function register(email: string, password: string, displayName: string) {
  const result = await request<AuthResult>(
    "/api/v1/auth/register",
    { method: "POST", body: JSON.stringify({ email, password, displayName }) },
    false
  );
  saveTokens(result.tokens);
  return result;
}

export async function logout() {
  const refreshToken = getRefreshToken();
  try {
    await request<{ status: string }>("/api/v1/auth/logout", {
      method: "POST",
      body: JSON.stringify({ refreshToken })
    });
  } finally {
    clearTokens();
  }
}

export function getMe() {
  return request<MeResult>("/api/v1/me");
}

export function getWallet() {
  return request<{ wallet: Wallet; transactions: WalletTransaction[] }>("/api/v1/me/wallet");
}

export function getAPIKeys() {
  return request<{ items: APIKey[]; total: number }>("/api/v1/me/api-keys");
}

export function getGroups() {
  return request<{ items: GatewayGroup[]; total: number }>("/api/v1/me/groups");
}

export function getModels(externalGroupId?: number | null) {
  const params = new URLSearchParams();
  if (externalGroupId) params.set("externalGroupId", String(externalGroupId));
  const query = params.toString();
  return request<ModelsResult>(`/api/v1/models/catalog${query ? `?${query}` : ""}`);
}

export function redeemCode(code: string) {
  return request<{ status: string; result: RedeemResult }>("/api/v1/redeem", {
    method: "POST",
    body: JSON.stringify({ code })
  });
}

export function getOfficialProvider(externalGroupId?: number | null) {
  const params = new URLSearchParams();
  if (externalGroupId) params.set("externalGroupId", String(externalGroupId));
  const query = params.toString();
  return request<OfficialProviderResult>(`/api/v1/provider/official${query ? `?${query}` : ""}`);
}

export function getSystemAPIKey() {
  return request<SystemAPIKeyResult>("/api/v1/api-keys/system");
}
