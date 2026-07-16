// RequireAuth — route guard, требует залогиненного user (KAC-127 Phase 2).
//
// Использование:
//   <Route element={<RequireAuth/>}>
//     <Route path="/dashboard" .../>
//   </Route>
//
// Если loading → Spin; если user=null → redirect на /auth/login?return_to=...
// (preserve original path для post-login redirect).

import { Navigate, Outlet, useLocation } from "react-router-dom";
import { Spin } from "antd";
import { useAuth } from "@shared/contexts/AuthContext";

interface RequireAuthProps {
  redirectTo?: string;
  /** Custom render для loading state. */
  fallback?: React.ReactNode;
  children?: React.ReactNode;
}

export function RequireAuth({ redirectTo = "/auth/login", fallback, children }: RequireAuthProps) {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      fallback ?? (
        <div
          style={{ minHeight: "60vh", display: "flex", alignItems: "center", justifyContent: "center" }}
          data-testid="require-auth-loading"
        >
          <Spin size="large" />
        </div>
      )
    );
  }

  if (!user) {
    const returnTo = location.pathname + location.search;
    const url = `${redirectTo}?return_to=${encodeURIComponent(returnTo)}`;
    return <Navigate to={url} replace data-testid="require-auth-redirect" />;
  }

  return <>{children ?? <Outlet />}</>;
}
