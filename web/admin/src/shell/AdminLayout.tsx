import { Navigate, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  BadgeDollarSign,
  CalendarClock,
  ClipboardList,
  Gauge,
  KeyRound,
  LogOut,
  RefreshCw,
  ScrollText,
  Settings,
  ShieldCheck,
  Stethoscope,
  Ticket,
  Users
} from "lucide-react";
import { clsx } from "clsx";
import { useServiceHealth } from "../state/useServiceHealth";
import { adminLogout, ApiError, getAdminMe } from "../api/client";

const navItems = [
  { to: "/admin", label: "总览", icon: Gauge, end: true },
  { to: "/admin/users", label: "用户", icon: Users },
  { to: "/admin/redeem-codes", label: "兑换码", icon: Ticket },
  { to: "/admin/redemptions", label: "兑换记录", icon: ClipboardList },
  { to: "/admin/subscriptions", label: "订阅", icon: CalendarClock },
  { to: "/admin/usage", label: "用量成本", icon: Activity },
  { to: "/admin/gateway", label: "网关", icon: KeyRound },
  { to: "/admin/operations", label: "队列", icon: RefreshCw },
  { to: "/admin/diagnostics", label: "诊断", icon: Stethoscope },
  { to: "/admin/audit-logs", label: "审计", icon: ScrollText },
  { to: "/admin/settings", label: "设置", icon: Settings }
];

export function AdminLayout() {
  const health = useServiceHealth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const me = useQuery({ queryKey: ["admin-me"], queryFn: getAdminMe, retry: false });
  const logout = useMutation({
    mutationFn: adminLogout,
    onSettled: async () => {
      await queryClient.removeQueries({ queryKey: ["admin-me"] });
      navigate("/admin/login", { replace: true });
    }
  });
  const ready = health.ready.data?.status === "ready";

  if (me.isLoading) {
    return (
      <main className="center-screen">
        <div className="loading-panel">
          <div className="brand-mark">B</div>
          <strong>正在检查管理员会话</strong>
        </div>
      </main>
    );
  }

  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/admin/login" replace />;
  }

  return (
    <div className="admin-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="brand-mark">B</div>
          <div>
            <div className="brand-name">Brevyn</div>
            <div className="brand-subtitle">Cloud Admin</div>
          </div>
        </div>

        <nav className="nav-list" aria-label="主导航">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              end={item.end}
              to={item.to}
              className={({ isActive }) => clsx("nav-item", isActive && "active")}
            >
              <item.icon size={18} strokeWidth={1.9} />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>

        <div className="sidebar-footer">
          <div className="operator-pill">
            <ShieldCheck size={16} />
            <span>{me.data?.admin.role ?? "Owner"}</span>
          </div>
          <button className="icon-text-button" type="button" onClick={() => logout.mutate()}>
            <LogOut size={16} />
            <span>退出</span>
          </button>
        </div>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <div>
            <div className="eyebrow">Production Readiness</div>
            <h1>Brevyn Cloud 控制台</h1>
          </div>
          <div className="topbar-actions">
            <div className={clsx("status-chip", ready ? "ok" : "warn")}>
              <span className="pulse-dot" />
              {ready ? "API Ready" : "API Pending"}
            </div>
            <button className="primary-action" onClick={() => navigate("/admin/users")} type="button">
              <BadgeDollarSign size={16} />
              <span>手动赠送</span>
            </button>
          </div>
        </header>

        <main className="content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
