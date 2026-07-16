// AuthCallback — legacy-страница `/auth/callback` от OAuth2-flow (Zitadel-era).
//
// Ory stack (KAC-115): Kratos self-service flow завершается прямым redirect'ом
// на `default_browser_return_url` (`/dashboard` или `/`). Этот landing больше
// не нужен для primary userflow, но оставлен как fallback / для будущего
// Hydra OAuth2 (CLI-shim) consumer.
//
// Поведение сейчас: показывает Spin → navigate('/') через 1.5s. Если придёт
// `?error=...` (например, от Hydra или OIDC-провайдера) — render error message.

import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Alert, Button, Result, Spin } from "antd";

export function AuthCallback() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [status, setStatus] = useState<"loading" | "ok" | "error">("loading");
  const [errMsg, setErrMsg] = useState<string>("");
  const calledRef = useRef(false);

  useEffect(() => {
    if (calledRef.current) return;
    calledRef.current = true;

    const oidcErr = params.get("error");
    const oidcErrDesc = params.get("error_description");
    if (oidcErr) {
      setStatus("error");
      setErrMsg(oidcErrDesc ? `${oidcErr}: ${oidcErrDesc}` : oidcErr);
      return;
    }
    setStatus("ok");
    setTimeout(() => navigate("/", { replace: true }), 1000);
  }, [params, navigate]);

  if (status === "loading") {
    return (
      <div style={{ padding: 48, textAlign: "center" }}>
        <Spin size="large" tip="Завершаем вход…" />
      </div>
    );
  }
  if (status === "ok") {
    return <Result status="success" title="Вход выполнен" subTitle="Перенаправляем на главную…" />;
  }
  return (
    <div style={{ padding: 24, maxWidth: 560, margin: "0 auto" }}>
      <Alert
        type="error"
        showIcon
        message="Не удалось завершить вход"
        description={errMsg}
        style={{ marginBottom: 16 }}
      />
      <Button type="primary" onClick={() => navigate("/")}>
        На главную
      </Button>
    </div>
  );
}
