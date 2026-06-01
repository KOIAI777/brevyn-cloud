import { createBrowserRouter, Navigate } from "react-router-dom";
import { AdminLayout } from "./shell/AdminLayout";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { UsersPage } from "./pages/UsersPage";
import { UserDetailPage } from "./pages/UserDetailPage";
import { RedeemCodesPage } from "./pages/RedeemCodesPage";
import { RedemptionsPage } from "./pages/RedemptionsPage";
import { SubscriptionsPage } from "./pages/SubscriptionsPage";
import { UsagePage } from "./pages/UsagePage";
import { GatewayPage } from "./pages/GatewayPage";
import { GatewayOperationsPage } from "./pages/GatewayOperationsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { AuditLogsPage } from "./pages/AuditLogsPage";
import { SettingsPage } from "./pages/SettingsPage";

export const router = createBrowserRouter([
  { path: "/", element: <Navigate to="/admin" replace /> },
  { path: "/admin/login", element: <LoginPage /> },
  {
    path: "/admin",
    element: <AdminLayout />,
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "users", element: <UsersPage /> },
      { path: "users/:id", element: <UserDetailPage /> },
      { path: "redeem-codes", element: <RedeemCodesPage /> },
      { path: "redemptions", element: <RedemptionsPage /> },
      { path: "subscriptions", element: <SubscriptionsPage /> },
      { path: "usage", element: <UsagePage /> },
      { path: "models", element: <Navigate to="/admin/gateway" replace /> },
      { path: "gateway", element: <GatewayPage /> },
      { path: "operations", element: <GatewayOperationsPage /> },
      { path: "diagnostics", element: <DiagnosticsPage /> },
      { path: "audit-logs", element: <AuditLogsPage /> },
      { path: "settings", element: <SettingsPage /> }
    ]
  }
]);
