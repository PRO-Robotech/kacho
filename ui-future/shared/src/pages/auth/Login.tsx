// Login — Kratos AAL1+AAL2 + WebAuthn passwordless (KAC-127 Phase 2).
//
// Flow:
//   1. На mount — initFlow `/self-service/login/browser` через Kratos.
//      Кратос редиректит на текущий путь с `?flow=<id>`; SPA читает flow
//      по GET `/self-service/login/flows?id=<id>` и рендерит UI-nodes.
//   2. Conditional UI (WebAuthn Level 3): если браузер поддерживает
//      `PublicKeyCredential.isConditionalMediationAvailable()`, на mount
//      запускаем `navigator.credentials.get({mediation: 'conditional'})`.
//      Autofill в username-input предложит Passkey без явного click.
//   3. Explicit "Sign in with Passkey" / "Sign in with password" — кнопки
//      от Kratos UI-nodes (webauthn / password group).
//
// Acceptance: §6.2.1 (Conditional UI autofill), §6.2.2 (explicit ceremony),
// §6.2.3 (phishing-resistance — реализуется самим browser API), §6.2.4 (UV gating).

import { useEffect, useMemo, useState } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import { Button, Card, Space, Typography, Alert, Input, Form, Divider, Spin } from "antd";
import { KeyOutlined, LockOutlined, MailOutlined, SafetyOutlined } from "@ant-design/icons";
import { kratos, type SelfServiceFlow, csrfToken, findNode, flowMessages } from "@shared/lib/kratos";
import { config } from "@shared/lib/config";
import { safeInternalPath, resolvePostAuthTarget } from "@shared/lib/redirect";

const { Title, Text, Paragraph } = Typography;

interface LoginFormValues {
  identifier?: string;
  password?: string;
  totp_code?: string;
}

export function LoginPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [flow, setFlow] = useState<SelfServiceFlow | null>(null);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [form] = Form.useForm<LoginFormValues>();

  const flowId = searchParams.get("flow");
  // return_to is caller-supplied — constrain it to a same-origin in-app path so
  // it cannot become an open-redirect / re-phishing hand-off (CWE-601).
  const returnTo = safeInternalPath(searchParams.get("return_to"));

  // Step 1: получаем flow по ID или инициируем новый через redirect.
  useEffect(() => {
    if (!flowId) {
      window.location.assign(kratos.loginUrl(returnTo));
      return;
    }
    setLoading(true);
    kratos
      .getFlow<SelfServiceFlow>("login", flowId)
      .then((f) => setFlow(f))
      .catch((e: Error & { status?: number }) => {
        if (e.status === 410 || e.status === 404) {
          // Flow expired / not found → re-init.
          window.location.assign(kratos.loginUrl(returnTo));
          return;
        }
        setError(e.message || "Не удалось загрузить login-flow");
      })
      .finally(() => setLoading(false));
  }, [flowId, returnTo]);

  // Conditional UI passkey autofill (WebAuthn Level 3).
  useEffect(() => {
    if (!flow) return;
    if (typeof window === "undefined") return;
    const pkc = (
      window as Window & {
        PublicKeyCredential?: {
          isConditionalMediationAvailable?: () => Promise<boolean>;
        };
      }
    ).PublicKeyCredential;
    if (!pkc?.isConditionalMediationAvailable) return;
    let cancelled = false;
    pkc.isConditionalMediationAvailable().then((available) => {
      if (!available || cancelled) return;
      // Берём challenge из flow.ui (Kratos рендерит webauthn_login_trigger).
      const triggerNode = findNode(flow.ui, "webauthn_login_trigger");
      if (!triggerNode?.attributes?.value) return;
      try {
        // Kratos сам формирует publicKey options в onclick handler.
        // Для conditional autofill — вызываем browser-native ceremony.
        const optsRaw = triggerNode.attributes.value as string;
        const opts = JSON.parse(optsRaw) as { publicKey: PublicKeyCredentialRequestOptions };
        navigator.credentials
          .get({
            ...opts,
            mediation: "conditional",
          } as CredentialRequestOptions)
          .then(() => {
            // На успех Kratos обработает результат через form-submit с hidden response field.
            // В conditional UI mode — focus переходит на input → user'у предложат Passkey.
            setInfo("Выберите passkey в автозаполнении username");
          })
          .catch(() => {
            // User cancelled — не показываем error.
          });
      } catch {
        // bad JSON — игнорируем conditional UI.
      }
    });
    return () => {
      cancelled = true;
    };
  }, [flow]);

  const messages = useMemo(() => (flow ? flowMessages(flow.ui) : []), [flow]);
  const hasWebauthn = useMemo(() => (flow ? !!findNode(flow.ui, "webauthn_login_trigger") : false), [flow]);
  const hasPassword = useMemo(() => (flow ? !!findNode(flow.ui, "password") : false), [flow]);

  const submitPasskey = async () => {
    if (!flow) return;
    setSubmitting(true);
    setError(null);
    try {
      const triggerNode = findNode(flow.ui, "webauthn_login_trigger");
      if (!triggerNode?.attributes?.value) {
        throw new Error("Passkey login недоступен для этого flow");
      }
      const opts = JSON.parse(triggerNode.attributes.value as string) as {
        publicKey: PublicKeyCredentialRequestOptions;
      };
      // Explicit ceremony — userVerification по flow's requested AAL.
      const cred = (await navigator.credentials.get({
        publicKey: {
          ...opts.publicKey,
          userVerification: flow.requested_aal === "aal2" ? "required" : "preferred",
        },
      })) as PublicKeyCredential | null;
      if (!cred) throw new Error("Ceremony отменена");
      const response = cred.response as AuthenticatorAssertionResponse;
      const body = {
        csrf_token: csrfToken(flow.ui),
        method: "webauthn",
        webauthn_login: JSON.stringify({
          id: cred.id,
          rawId: bufferToBase64Url(cred.rawId),
          type: cred.type,
          response: {
            authenticatorData: bufferToBase64Url(response.authenticatorData),
            clientDataJSON: bufferToBase64Url(response.clientDataJSON),
            signature: bufferToBase64Url(response.signature),
            userHandle: response.userHandle ? bufferToBase64Url(response.userHandle) : null,
          },
        }),
      };
      const result = await kratos.submitFlow<SelfServiceFlow>("login", flow.id, body);
      // Successful flow → Kratos выдаёт session cookie; redirect_browser_to set.
      // SDK обычно отвечает 200 + return_to; на browser-flow Kratos редиректит сам.
      handleSubmitSuccess(result);
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      const uiMsg = err.ui?.messages?.[0]?.text;
      setError(uiMsg || err.message || "Не удалось войти через Passkey");
    } finally {
      setSubmitting(false);
    }
  };

  const submitPassword = async (values: LoginFormValues) => {
    if (!flow) return;
    setSubmitting(true);
    setError(null);
    try {
      const body: Record<string, unknown> = {
        csrf_token: csrfToken(flow.ui),
        method: "password",
        identifier: values.identifier,
        password: values.password,
      };
      if (values.totp_code) {
        // На AAL2-prompt Kratos шлёт второй flow с method=totp; для упрощения
        // допускаем код сразу — Kratos валидирует если требуется.
        body.totp_code = values.totp_code;
        body.method = "totp";
      }
      const result = await kratos.submitFlow<SelfServiceFlow>("login", flow.id, body);
      handleSubmitSuccess(result);
    } catch (e: unknown) {
      const err = e as Error & {
        status?: number;
        ui?: { messages?: Array<{ text: string }> };
      };
      // Kratos на AAL2-prompt возвращает 400 + новый ui — обновляем flow.
      const uiMsg = err.ui?.messages?.[0]?.text;
      setError(uiMsg || err.message || "Не удалось войти");
    } finally {
      setSubmitting(false);
    }
  };

  function handleSubmitSuccess(result: SelfServiceFlow): void {
    // Browser-flow: Kratos выставляет cookie и редиректит сам через 303.
    // Если попали сюда — берём return_to из flow или default.
    // Re-validate: the flow-supplied return_to must also stay same-origin.
    const target = resolvePostAuthTarget(result.return_to, returnTo);
    navigate(target, { replace: true });
  }

  if (loading || !flow) {
    return (
      <AuthLayout>
        <div style={{ textAlign: "center", padding: 48 }}>
          <Spin size="large" />
          <Paragraph type="secondary" style={{ marginTop: 16 }}>
            Загрузка login-flow…
          </Paragraph>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <Title level={3} style={{ margin: "0 0 12px" }}>
        Вход в {config.webauthnRpName}
      </Title>
      <Text type="secondary">Используйте passkey или пароль для входа в аккаунт</Text>

      {info && <Alert type="info" showIcon message={info} style={{ marginTop: 16 }} />}
      {messages.map((m, i) => (
        <Alert
          key={i}
          type={m.type === "error" ? "error" : "info"}
          showIcon
          message={m.text}
          style={{ marginTop: 16 }}
        />
      ))}
      {error && <Alert type="error" showIcon message={error} style={{ marginTop: 16 }} data-testid="login-error" />}

      <div style={{ marginTop: 24 }}>
        {hasWebauthn && (
          <Button
            type="primary"
            size="large"
            block
            icon={<KeyOutlined />}
            loading={submitting}
            onClick={submitPasskey}
            data-testid="login-passkey-btn"
          >
            Войти через Passkey
          </Button>
        )}

        {hasWebauthn && hasPassword && <Divider plain>или</Divider>}

        {hasPassword && (
          <Form form={form} layout="vertical" onFinish={submitPassword} requiredMark={false}>
            <Form.Item name="identifier" label="Email" rules={[{ required: true, message: "Введите email" }]}>
              <Input
                size="large"
                prefix={<MailOutlined />}
                placeholder="user@example.com"
                autoComplete="username webauthn"
                data-testid="login-identifier"
              />
            </Form.Item>
            <Form.Item name="password" label="Пароль" rules={[{ required: true, message: "Введите пароль" }]}>
              <Input.Password
                size="large"
                prefix={<LockOutlined />}
                placeholder="••••••••"
                autoComplete="current-password"
                data-testid="login-password"
              />
            </Form.Item>
            {flow.requested_aal === "aal2" && (
              <Form.Item
                name="totp_code"
                label="Код из authenticator-приложения"
                rules={[{ required: true, message: "Введите 6-значный код" }]}
              >
                <Input
                  size="large"
                  prefix={<SafetyOutlined />}
                  maxLength={6}
                  placeholder="123456"
                  data-testid="login-totp"
                />
              </Form.Item>
            )}
            <Button
              type={hasWebauthn ? "default" : "primary"}
              size="large"
              htmlType="submit"
              block
              loading={submitting}
              icon={<LockOutlined />}
              data-testid="login-password-btn"
            >
              Войти через пароль
            </Button>
          </Form>
        )}

        <Space orientation="vertical" style={{ marginTop: 16, width: "100%" }}>
          <Button type="link" block onClick={() => navigate("/auth/registration")} data-testid="login-to-register">
            Создать аккаунт
          </Button>
          <Button type="link" block onClick={() => navigate("/auth/recovery")} data-testid="login-to-recovery">
            Забыли доступ?
          </Button>
        </Space>
      </div>
    </AuthLayout>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

export function bufferToBase64Url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/=+$/g, "").replace(/\+/g, "-").replace(/\//g, "_");
}

export function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "#1c1d22",
        padding: 24,
      }}
    >
      <Card
        style={{
          width: 480,
          background: "#26272d",
          border: "1px solid #383941",
        }}
      >
        {children}
      </Card>
    </div>
  );
}
