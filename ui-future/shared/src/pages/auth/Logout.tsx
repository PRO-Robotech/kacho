// Logout — Kratos token-flow + Hydra BCL (KAC-127 Phase 2).
//
// Flow:
//   1. AuthContext.logout() — init Kratos logout (получает logout_token),
//      submit'ит его (Kratos очищает session cookie). Чистит DPoP-key из IDB.
//   2. На Hydra-сессию шлёт BCL request: GET `/oauth2/sessions/logout?id_token_hint=...`
//      (если есть id-token). Hydra back-channel logout распостранит signal
//      на все RP'ы (E3-functionality; на E2 — simple front-channel ok).
//   3. Redirect на `/` (или `?post_logout_redirect_uri=...`).

import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Result, Spin, Alert, Button } from "antd";
import { LogoutOutlined } from "@ant-design/icons";
import { useAuth } from "@shared/contexts/AuthContext";
import { config } from "@shared/lib/config";
import { safeInternalPath } from "@shared/lib/redirect";
import { AuthLayout } from "./Login";

export function LogoutPage() {
  const { logout, user } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [status, setStatus] = useState<"running" | "ok" | "error">("running");
  const [errMsg, setErrMsg] = useState<string>("");

  // post_logout_redirect_uri is caller-supplied — constrain it to a same-origin
  // in-app path so a freshly-logged-out user cannot be bounced to an attacker
  // page for re-phishing (CWE-601).
  const postLogoutRedirect = safeInternalPath(params.get("post_logout_redirect_uri"));

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await logout();
        // BCL для Hydra session (если есть id-token-hint). Fire-and-forget.
        try {
          await fetch(`${config.hydraUrl}/sessions/logout`, {
            method: "GET",
            credentials: "include",
            mode: "no-cors",
          });
        } catch {
          // не критично — Kratos logout уже снял session
        }
        if (cancelled) return;
        setStatus("ok");
        setTimeout(() => navigate(postLogoutRedirect, { replace: true }), 800);
      } catch (e: unknown) {
        if (cancelled) return;
        setStatus("error");
        setErrMsg((e as Error).message || "Не удалось завершить сессию");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [logout, navigate, postLogoutRedirect]);

  if (status === "running") {
    return (
      <AuthLayout>
        <div style={{ textAlign: "center", padding: 32 }}>
          <Spin size="large" tip={user ? `Выходим из аккаунта ${user.email ?? user.id}…` : "Выходим…"} />
        </div>
      </AuthLayout>
    );
  }

  if (status === "ok") {
    return (
      <AuthLayout>
        <Result
          status="success"
          icon={<LogoutOutlined />}
          title="Сессия завершена"
          subTitle="Перенаправляем…"
          data-testid="logout-success"
        />
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <Alert
        type="error"
        showIcon
        message="Не удалось завершить сессию"
        description={errMsg}
        style={{ marginBottom: 16 }}
        data-testid="logout-error"
      />
      <Button type="primary" onClick={() => navigate("/", { replace: true })}>
        На главную
      </Button>
    </AuthLayout>
  );
}
