import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle2,
  Copy,
  KeyRound,
  LogOut,
  Sparkles,
  Ticket,
  WalletCards
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  APIError,
  clearTokens,
  getAPIKeys,
  getAccessToken,
  getGroups,
  getMe,
  getOfficialProvider,
  getWallet,
  login,
  logout,
  redeemCode,
  register,
  type APIKey,
  type GatewayGroup,
  type ModelItem
} from "./api";

function formatMoney(value: number) {
  return `$${value.toFixed(2)}`;
}

function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

function errorText(error: unknown, fallback = "操作失败") {
  if (error instanceof APIError) return error.message;
  if (error instanceof Error) return error.message;
  return fallback;
}

function copyText(text: string) {
  return navigator.clipboard.writeText(text);
}

function modelTags(model: ModelItem) {
  const tags = [];
  if (model.supportsStreaming) tags.push("stream");
  if (model.supportsVision) tags.push("vision");
  if (model.billingMode) tags.push(model.billingMode);
  return tags;
}

function groupTypeText(group?: GatewayGroup | null) {
  if (!group) return "未分组";
  if (group.subscriptionType === "subscription") return "订阅组";
  if (group.subscriptionType === "standard") return "余额组";
  return group.subscriptionType || "分组";
}

function groupSourceText(source?: string) {
  if (!source) return "cloud";
  return source
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => (item === "api_key" ? "API Key" : item === "default" ? "默认组" : item))
    .join(" / ");
}

function groupLimitText(group?: GatewayGroup | null) {
  if (!group) return "-";
  const limits = [
    group.dailyLimitUsd ? `日 ${formatMoney(group.dailyLimitUsd)}` : "",
    group.weeklyLimitUsd ? `周 ${formatMoney(group.weeklyLimitUsd)}` : "",
    group.monthlyLimitUsd ? `月 ${formatMoney(group.monthlyLimitUsd)}` : "",
    group.rpmLimit ? `${group.rpmLimit} RPM` : ""
  ].filter(Boolean);
  return limits.join(" · ") || "未限额";
}

function APIKeyRow({ item, onCopy }: { item: APIKey; onCopy: (value: string) => void }) {
  return (
    <div className="key-row">
      <div>
        <strong>{item.groupName || `group #${item.externalGroupId}`}</strong>
        <span>
          #{item.externalKeyId || "-"} · {item.status} · {item.platform || item.provider}
        </span>
      </div>
      <code>{item.maskedApiKey}</code>
      <button
        className="icon-action"
        onClick={() => {
          void copyText(item.maskedApiKey);
          onCopy("已复制脱敏 Key");
        }}
        type="button"
      >
        <Copy size={15} />
      </button>
    </div>
  );
}

export function App() {
  const [booted, setBooted] = useState(() => Boolean(getAccessToken()));
  return booted ? <Dashboard onLogout={() => setBooted(false)} /> : <AuthScreen onAuthed={() => setBooted(true)} />;
}

function AuthScreen({ onAuthed }: { onAuthed: () => void }) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const auth = useMutation({
    mutationFn: () => (mode === "login" ? login(email, password) : register(email, password, displayName)),
    onSuccess: onAuthed
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    auth.mutate();
  };

  return (
    <main className="auth-shell">
      <section className="brand-panel">
        <div className="brand-mark">B</div>
        <div>
          <p className="eyebrow">Brevyn App</p>
          <h1>Claude access, without the console maze.</h1>
          <p className="hero-copy">登录后自动完成官方配置，兑换卡密即可查看余额和可用模型。</p>
        </div>
        <div className="signal-strip">
          <span>Claude</span>
          <span>Kiro</span>
          <span>Sub2API</span>
        </div>
      </section>
      <section className="auth-card">
        <div className="segmented">
          <button className={mode === "login" ? "active" : ""} onClick={() => setMode("login")} type="button">
            登录
          </button>
          <button className={mode === "register" ? "active" : ""} onClick={() => setMode("register")} type="button">
            注册
          </button>
        </div>
        <form onSubmit={submit}>
          <label>
            邮箱
            <input autoComplete="email" onChange={(event) => setEmail(event.target.value)} placeholder="you@example.com" value={email} />
          </label>
          {mode === "register" ? (
            <label>
              昵称
              <input onChange={(event) => setDisplayName(event.target.value)} placeholder="Koi" value={displayName} />
            </label>
          ) : null}
          <label>
            密码
            <input
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              minLength={8}
              onChange={(event) => setPassword(event.target.value)}
              placeholder="至少 8 位"
              type="password"
              value={password}
            />
          </label>
          {auth.isError ? <p className="error-line">{errorText(auth.error, "登录失败")}</p> : null}
          <button className="primary-action" disabled={auth.isPending || !email || password.length < 8} type="submit">
            <span>{auth.isPending ? "处理中" : mode === "login" ? "进入控制台" : "创建账号"}</span>
            <ArrowRight size={16} />
          </button>
        </form>
      </section>
    </main>
  );
}

function Dashboard({ onLogout }: { onLogout: () => void }) {
  const queryClient = useQueryClient();
  const [redeemInput, setRedeemInput] = useState("");
  const [copied, setCopied] = useState("");
  const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null);

  const me = useQuery({ queryKey: ["me"], queryFn: getMe });
  const groups = useQuery({ queryKey: ["groups"], queryFn: getGroups });
  const wallet = useQuery({ queryKey: ["wallet"], queryFn: getWallet });
  const keys = useQuery({ queryKey: ["api-keys"], queryFn: getAPIKeys });
  const providerConfig = useQuery({
    queryKey: ["official-provider", selectedGroupId],
    queryFn: () => getOfficialProvider(selectedGroupId),
    enabled: selectedGroupId !== null,
    refetchOnWindowFocus: false,
    retry: false,
    staleTime: 5 * 60 * 1000
  });
  const redeem = useMutation({
    mutationFn: () => redeemCode(redeemInput),
    onSuccess: async (result) => {
      setRedeemInput("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["me"] }),
        queryClient.invalidateQueries({ queryKey: ["groups"] }),
        queryClient.invalidateQueries({ queryKey: ["wallet"] }),
        queryClient.invalidateQueries({ queryKey: ["api-keys"] }),
        queryClient.invalidateQueries({ queryKey: ["official-provider"] })
      ]);
    }
  });

  const signOut = useMutation({
    mutationFn: logout,
    onSuccess: () => {
      queryClient.clear();
      onLogout();
    },
    onError: () => {
      clearTokens();
      queryClient.clear();
      onLogout();
    }
  });

  const groupRows = groups.data?.items ?? [];
  const providerGroupId = providerConfig.data?.apiKey?.externalGroupId || providerConfig.data?.gateway?.defaultGroupId || null;
  const currentGatewayGroupId =
    me.data?.currentGroup?.externalGroupId || me.data?.gateway?.defaultGroupId || providerGroupId || null;
  const selectedGroup = useMemo(() => {
    return groupRows.find((group) => group.externalGroupId === selectedGroupId) ?? me.data?.currentGroup ?? null;
  }, [groupRows, me.data?.currentGroup, selectedGroupId]);
  const activeKeys = useMemo(() => (keys.data?.items ?? []).filter((item) => item.status === "active"), [keys.data?.items]);
  const officialProvider = providerConfig.data?.provider;
  const groupedModels = useMemo(() => {
    const rows = officialProvider?.models ?? [];
    return rows.slice(0, 8);
  }, [officialProvider?.models]);

  useEffect(() => {
    if (groupRows.length === 0) return;
    const current = currentGatewayGroupId && groupRows.some((group) => group.externalGroupId === currentGatewayGroupId) ? currentGatewayGroupId : null;
    setSelectedGroupId((previous) => {
      if (previous && groupRows.some((group) => group.externalGroupId === previous)) return previous;
      return current ?? groupRows[0].externalGroupId;
    });
  }, [currentGatewayGroupId, groupRows]);

  useEffect(() => {
    if (!providerConfig.data) return;
    void queryClient.invalidateQueries({ queryKey: ["me"] });
    void queryClient.invalidateQueries({ queryKey: ["groups"] });
    void queryClient.invalidateQueries({ queryKey: ["api-keys"] });
  }, [providerConfig.data, queryClient]);

  return (
    <main className="app-shell">
      <header className="topbar">
        <div className="brand-inline">
          <div className="brand-mark small">B</div>
          <div>
            <strong>Brevyn</strong>
            <span>学生自助台</span>
          </div>
        </div>
        <button className="ghost-action" onClick={() => signOut.mutate()} type="button">
          <LogOut size={16} />
          <span>退出</span>
        </button>
      </header>

      {me.isError ? (
        <section className="panel danger-panel">
          <p>{errorText(me.error, "会话已失效")}</p>
          <button className="secondary-action" onClick={() => signOut.mutate()} type="button">
            重新登录
          </button>
        </section>
      ) : null}

      <section className="hero-grid">
        <article className="panel account-panel">
          <p className="eyebrow">Account</p>
          <h2>{me.data?.user.displayName || me.data?.user.email || "正在加载"}</h2>
          <p>{me.data?.user.email ?? "-"}</p>
          <div className="status-row">
            <span>{me.data?.user.status ?? "loading"}</span>
            <span>{selectedGroup?.name || `group #${currentGatewayGroupId || "-"}`}</span>
            <span>{me.data?.gateway?.concurrency || 5} concurrency</span>
          </div>
          <div className="account-detail-grid">
            <div>
              <span>Cloud ID</span>
              <code>{me.data?.user.id ?? "-"}</code>
            </div>
            <div>
              <span>Sub2API UID</span>
              <code>{me.data?.gateway?.externalUserId ?? "-"}</code>
            </div>
            <div>
              <span>当前分组</span>
              <code>{selectedGroup ? `${selectedGroup.name} (#${selectedGroup.externalGroupId})` : "-"}</code>
            </div>
            <div>
              <span>同步时间</span>
              <code>{formatDate(me.data?.gateway?.lastSyncedAt ?? null)}</code>
            </div>
          </div>
        </article>
        <article className="metric-panel balance">
          <WalletCards size={24} />
          <span>钱包余额</span>
          <strong>{formatMoney(wallet.data?.wallet.balance ?? me.data?.wallet.balance ?? 0)}</strong>
        </article>
        <article className="metric-panel key">
          <KeyRound size={24} />
          <span>Active Key</span>
          <strong>{activeKeys.length}</strong>
          <small>{groupRows.length || 0} 个可用分组</small>
        </article>
      </section>

      <section className="panel group-panel">
        <div className="panel-head">
          <div>
            <p className="eyebrow">Gateway Groups</p>
            <h3>当前账号分组</h3>
          </div>
          <span className="count-pill">{groups.data?.total ?? 0}</span>
        </div>
        <div className="group-switcher">
          {groupRows.map((group) => (
            <button
              className={group.externalGroupId === (selectedGroupId ?? currentGatewayGroupId) ? "active" : ""}
              key={group.externalGroupId}
              onClick={() => setSelectedGroupId(group.externalGroupId)}
              type="button"
            >
              <strong>{group.name}</strong>
              <span>#{group.externalGroupId}</span>
            </button>
          ))}
          {!groups.isLoading && groupRows.length === 0 ? <p className="empty-line">账号配置中；系统会自动创建默认分组和 Key。</p> : null}
        </div>
        <div className="group-detail-grid">
          <div>
            <span>类型</span>
            <strong>{groupTypeText(selectedGroup)}</strong>
          </div>
          <div>
            <span>模型数</span>
            <strong>{selectedGroup?.modelCount ?? officialProvider?.models.length ?? 0}</strong>
          </div>
          <div>
            <span>倍率</span>
            <strong>{selectedGroup ? `${selectedGroup.rateMultiplier}x` : "-"}</strong>
          </div>
          <div>
            <span>限额</span>
            <strong>{groupLimitText(selectedGroup)}</strong>
          </div>
          <div>
            <span>平台</span>
            <strong>{selectedGroup?.platform || "anthropic"}</strong>
          </div>
          <div>
            <span>来源</span>
            <strong>{groupSourceText(selectedGroup?.source)}</strong>
          </div>
        </div>
      </section>

      <section className="work-grid">
        <article className="panel redeem-panel">
          <div className="panel-head">
            <div>
              <p className="eyebrow">Redeem</p>
              <h3>兑换卡密</h3>
            </div>
            <Ticket size={20} />
          </div>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              redeem.mutate();
            }}
          >
            <input
              onChange={(event) => setRedeemInput(event.target.value)}
              placeholder="输入联动小铺发货卡密"
              value={redeemInput}
            />
            <button className="primary-action" disabled={redeem.isPending || redeemInput.trim().length === 0} type="submit">
              {redeem.isPending ? "兑换中" : "立即兑换"}
            </button>
          </form>
          {redeem.isError ? <p className="error-line">{errorText(redeem.error, "兑换失败")}</p> : null}
          {redeem.isSuccess ? (
            <p className="success-line">
              <CheckCircle2 size={16} />
              {redeem.data.result.redemption.kind === "subscription"
                ? `订阅已兑换 ${redeem.data.result.redemption.validityDays} 天`
                : `余额已到账 ${formatMoney(redeem.data.result.redemption.value)}`}
            </p>
          ) : null}
        </article>

        <article className="panel provider-panel">
          <div className="panel-head">
            <div>
              <p className="eyebrow">Provider</p>
              <h3>官方配置</h3>
            </div>
            <Sparkles size={20} />
          </div>
          {selectedGroupId === null && groups.isLoading ? <p className="empty-line">正在读取账号分组...</p> : null}
          {providerConfig.isLoading ? <p className="empty-line">正在自动配置账号...</p> : null}
          {providerConfig.isError ? <p className="error-line">{errorText(providerConfig.error, "官方配置暂时不可用")}</p> : null}
          {providerConfig.data && !providerConfig.data.provider ? (
            <p className="empty-line">
              {providerConfig.data.status === "provisioning" ? "账号配置中，系统会自动重试。" : providerConfig.data.detail || "官方配置暂时不可用"}
            </p>
          ) : null}
          {officialProvider ? (
            <div className="config-box">
              <div>
                <span>状态</span>
                <strong>{officialProvider.enabled ? "已自动配置" : "未启用"}</strong>
              </div>
              <div>
                <span>Base URL</span>
                <code>{officialProvider.baseUrl}</code>
                <button
                  className="icon-action"
                  onClick={async () => {
                    await copyText(officialProvider.baseUrl);
                    setCopied("Base URL 已复制");
                  }}
                  type="button"
                >
                  <Copy size={15} />
                </button>
              </div>
              <div>
                <span>Model</span>
                <code>{officialProvider.selectedModel}</code>
              </div>
              <div>
                <span>Models</span>
                <code>{officialProvider.models.length}</code>
              </div>
            </div>
          ) : null}
          {copied ? <p className="success-line">{copied}</p> : null}
        </article>
      </section>

      <section className="content-grid">
        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="eyebrow">Models</p>
              <h3>{selectedGroup?.name || "可用模型"}</h3>
            </div>
            <span className="count-pill">{officialProvider?.models.length ?? selectedGroup?.modelCount ?? 0}</span>
          </div>
          {providerConfig.isError ? <p className="error-line">{errorText(providerConfig.error, "模型加载失败")}</p> : null}
          <div className="model-list">
            {groupedModels.map((model) => (
              <div className="model-row" key={`${model.id}-${model.externalGroupId ?? 0}`}>
                <div>
                  <strong>{model.displayName || model.id}</strong>
                  <span>{model.groupName || model.providerFamily || "Brevyn"}</span>
                </div>
                <div className="tag-row">
                  {modelTags(model).map((tag) => (
                    <span key={tag}>{tag}</span>
                  ))}
                </div>
              </div>
            ))}
            {!providerConfig.isLoading && selectedGroupId !== null && groupedModels.length === 0 ? (
              <p className="empty-line">暂无模型，先在管理端同步 Sub2API 模型。</p>
            ) : null}
          </div>
        </article>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="eyebrow">Keys</p>
              <h3>API Key 状态</h3>
            </div>
            <span className="count-pill">{keys.data?.total ?? 0}</span>
          </div>
          <div className="key-list">
            {(keys.data?.items ?? []).slice(0, 5).map((item) => (
              <APIKeyRow item={item} key={item.id} onCopy={setCopied} />
            ))}
            {!keys.isLoading && (keys.data?.items.length ?? 0) === 0 ? <p className="empty-line">暂无 API Key；系统正在后台自动配置。</p> : null}
          </div>
        </article>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="eyebrow">Ledger</p>
              <h3>余额流水</h3>
            </div>
            <span className="count-pill">{wallet.data?.transactions.length ?? 0}</span>
          </div>
          <div className="ledger-list">
            {(wallet.data?.transactions ?? []).slice(0, 6).map((item) => (
              <div className="ledger-row" key={item.id}>
                <div>
                  <strong>{item.kind}</strong>
                  <span>{formatDate(item.createdAt)}</span>
                </div>
                <b>{formatMoney(item.amount)}</b>
              </div>
            ))}
            {!wallet.isLoading && (wallet.data?.transactions.length ?? 0) === 0 ? <p className="empty-line">还没有余额流水。</p> : null}
          </div>
        </article>
      </section>
    </main>
  );
}
