// RequireMFAFresh — route-guard для admin / sensitive операций (KAC-127 Phase 2).
//
// Использование:
//   <Route element={<RequireAuth/>}>
//     <Route element={<RequireMFAFresh/>}>
//       <Route path="/iam/access-bindings" .../>
//     </Route>
//   </Route>
//
// Если mfaFreshUntil > now → render children.
// Иначе → trigger setStepUpHandler / redirect на login?refresh=true&aal=aal2.

import { useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { Result, Button, Spin } from "antd";
import { KeyOutlined } from "@ant-design/icons";
import { useAuth, isMfaFresh } from "@shared/contexts/AuthContext";
import { kratos } from "@shared/lib/kratos";

interface RequireMFAFreshProps {
  children?: React.ReactNode;
  /** Авто-trigger step-up flow при mount. По умолчанию true. */
  autoTrigger?: boolean;
}

export function RequireMFAFresh({ children, autoTrigger = true }: RequireMFAFreshProps) {
  const auth = useAuth();
  const location = useLocation();

  const fresh = isMfaFresh(auth);
  const returnTo = location.pathname + location.search;

  useEffect(() => {
    if (fresh || !autoTrigger || auth.loading) return;
    // Если есть mounted StepUpModal — используем его. Иначе — full-page redirect.
    // Trigger через apiClient onStepUpRequired (тот же путь, что 403).
    // Простейшая интеграция: redirect на refresh login.
    const url = `${kratos.loginUrl(returnTo)}&refresh=true&aal=aal2`;
    window.location.assign(url);
  }, [fresh, autoTrigger, auth.loading, returnTo]);

  if (auth.loading) {
    return (
      <div
        style={{
          minHeight: "60vh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Spin size="large" />
      </div>
    );
  }

  if (!fresh) {
    return (
      <Result
        status="warning"
        icon={<KeyOutlined />}
        title="Подтвердите свежесть MFA"
        subTitle="Для этой операции требуется недавняя 2FA-аутентификация."
        extra={
          <Button
            type="primary"
            icon={<KeyOutlined />}
            onClick={() => {
              const url = `${kratos.loginUrl(returnTo)}&refresh=true&aal=aal2`;
              window.location.assign(url);
            }}
            data-testid="require-mfa-fresh-trigger"
          >
            Подтвердить через passkey
          </Button>
        }
        data-testid="require-mfa-fresh"
      />
    );
  }

  return <>{children ?? <Outlet />}</>;
}
