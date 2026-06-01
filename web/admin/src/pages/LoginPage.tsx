import { LockKeyhole, ShieldCheck } from "lucide-react";
import { FormEvent, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { adminLogin } from "../api/client";

export function LoginPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("owner@brevyn.local");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [totpRequired, setTotpRequired] = useState(false);
  const login = useMutation({
    mutationFn: adminLogin,
    onSuccess: async (result) => {
      if (result.totpRequired) {
        setTotpRequired(true);
        setTotpCode("");
        return;
      }
      await queryClient.invalidateQueries({ queryKey: ["admin-me"] });
      navigate("/admin", { replace: true });
    }
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    login.mutate({ email, password, totpCode: totpRequired ? totpCode : undefined });
  }

  return (
    <main className="login-screen">
      <section className="login-panel">
        <div className="brand-block">
          <div className="brand-mark">B</div>
          <div>
            <div className="brand-name">Brevyn</div>
            <div className="brand-subtitle">Cloud Admin</div>
          </div>
        </div>

        <div className="login-title">
          <ShieldCheck size={22} />
          <h1>管理员登录</h1>
        </div>

        <form className="form-stack" onSubmit={handleSubmit}>
          <label>
            邮箱
            <input
              autoComplete="username"
              onChange={(event) => setEmail(event.target.value)}
              placeholder="owner@brevyn.local"
              type="email"
              value={email}
            />
          </label>
          <label>
            密码
            <input
              autoComplete="current-password"
              disabled={totpRequired}
              onChange={(event) => setPassword(event.target.value)}
              placeholder="输入管理员密码"
              type="password"
              value={password}
            />
          </label>
          {totpRequired ? (
            <label>
              二步验证码
              <input
                autoComplete="one-time-code"
                inputMode="numeric"
                maxLength={6}
                onChange={(event) => setTotpCode(event.target.value)}
                placeholder="输入 6 位验证码"
                value={totpCode}
              />
            </label>
          ) : null}
          {totpRequired ? <div className="form-success">密码已验证，请输入 Authenticator 里的 6 位验证码。</div> : null}
          {login.isError ? <div className="form-error">{totpRequired ? "验证码错误或已过期。" : "登录失败，请检查管理员邮箱和密码。"}</div> : null}
          <button className="primary-action full" disabled={login.isPending} type="submit">
            <LockKeyhole size={16} />
            <span>{login.isPending ? "登录中" : totpRequired ? "验证并登录" : "登录"}</span>
          </button>
          {totpRequired ? (
            <button
              className="secondary-action full"
              disabled={login.isPending}
              onClick={() => {
                setTotpRequired(false);
                setTotpCode("");
              }}
              type="button"
            >
              返回密码登录
            </button>
          ) : null}
        </form>
      </section>
      <aside className="login-aside">
        <div className="signal-line" />
        <p>网关、用户额度、兑换记录和审计日志集中管理。</p>
      </aside>
    </main>
  );
}
