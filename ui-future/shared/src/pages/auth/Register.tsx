// Register — Kratos registration + Passkey enroll (KAC-127 Phase 2).
//
// Flow:
//   1. Init flow через `/self-service/registration/browser` (если flow ID нет).
//   2. UI рендерит form: email + displayName, primary CTA "Sign up with Passkey"
//      → `navigator.credentials.create({...})` после ввода email.
//   3. Fallback CTA "Sign up with password" — password input с HIBP
//      k-anonymity check (debounced 500ms), submit с method=password +
//      TOTP chained-prompt.
//
// Acceptance: §6.1.1 (Passkey enrollment happy path), §6.1.3 (UV initialization),
// §6.3.1 (password+TOTP fallback), §6.3.3 (HIBP rejection).

import { useEffect, useMemo, useState } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import { Button, Form, Input, Typography, Alert, Space, Spin, Divider } from "antd";
import { KeyOutlined, LockOutlined, MailOutlined, UserOutlined } from "@ant-design/icons";
import { kratos, type SelfServiceFlow, csrfToken, findNode, flowMessages } from "@shared/lib/kratos";
import { AuthLayout, bufferToBase64Url } from "./Login";
import { config } from "@shared/lib/config";
import { resolvePostAuthTarget } from "@shared/lib/redirect";

const { Title, Text, Paragraph } = Typography;

interface RegisterFormValues {
  email?: string;
  display_name?: string;
  password?: string;
}

export function RegisterPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [flow, setFlow] = useState<SelfServiceFlow | null>(null);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hibpWarn, setHibpWarn] = useState<string | null>(null);
  const [form] = Form.useForm<RegisterFormValues>();

  const flowId = searchParams.get("flow");

  useEffect(() => {
    if (!flowId) {
      window.location.assign(kratos.registrationUrl());
      return;
    }
    setLoading(true);
    kratos
      .getFlow<SelfServiceFlow>("registration", flowId)
      .then((f) => setFlow(f))
      .catch((e: Error & { status?: number }) => {
        if (e.status === 410 || e.status === 404) {
          window.location.assign(kratos.registrationUrl());
          return;
        }
        setError(e.message || "Не удалось загрузить registration-flow");
      })
      .finally(() => setLoading(false));
  }, [flowId]);

  const messages = useMemo(() => (flow ? flowMessages(flow.ui) : []), [flow]);
  const hasWebauthn = useMemo(() => (flow ? !!findNode(flow.ui, "webauthn_register_trigger") : false), [flow]);
  const hasPassword = useMemo(() => (flow ? !!findNode(flow.ui, "password") : false), [flow]);

  /** HIBP k-anonymity check — отправляет первые 5 SHA-1 hex char'ов. */
  const checkHibp = async (password: string): Promise<boolean> => {
    if (!password || password.length < 8) return false;
    try {
      const enc = new TextEncoder().encode(password);
      const digest = await crypto.subtle.digest("SHA-1", enc);
      const bytes = new Uint8Array(digest);
      const hex = Array.from(bytes)
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("")
        .toUpperCase();
      const prefix = hex.slice(0, 5);
      const suffix = hex.slice(5);
      const res = await fetch(`https://api.pwnedpasswords.com/range/${prefix}`, {
        method: "GET",
      });
      if (!res.ok) return false;
      const text = await res.text();
      const found = text.split(/\r?\n/).some((line) => line.split(":")[0].toUpperCase() === suffix);
      return found;
    } catch {
      // Network error — fail-open (нет confirm-blocking).
      return false;
    }
  };

  // Debounced HIBP check на change password input.
  useEffect(() => {
    const t = setTimeout(async () => {
      const password = form.getFieldValue("password");
      if (!password) {
        setHibpWarn(null);
        return;
      }
      const pwned = await checkHibp(password);
      setHibpWarn(pwned ? "Этот пароль уже встречался в утечках. Выберите другой." : null);
    }, 500);
    return () => clearTimeout(t);
  }, [form]);

  const submitPasskey = async () => {
    if (!flow) return;
    setSubmitting(true);
    setError(null);
    try {
      const values = await form.validateFields(["email", "display_name"]);
      const triggerNode = findNode(flow.ui, "webauthn_register_trigger");
      if (!triggerNode?.attributes?.value) {
        throw new Error("WebAuthn registration недоступен для этого flow");
      }
      const opts = JSON.parse(triggerNode.attributes.value as string) as {
        publicKey: PublicKeyCredentialCreationOptions;
      };
      const cred = (await navigator.credentials.create({
        publicKey: {
          ...opts.publicKey,
          // Kratos обычно передаёт user.{id,name,displayName} из traits;
          // но для свежего flow добавим display_name если не задан.
          user: {
            ...opts.publicKey.user,
            displayName: values.display_name ?? opts.publicKey.user.displayName,
            name: values.email ?? opts.publicKey.user.name,
          },
        },
      })) as PublicKeyCredential | null;
      if (!cred) throw new Error("Ceremony отменена");
      const response = cred.response as AuthenticatorAttestationResponse;
      const body: Record<string, unknown> = {
        csrf_token: csrfToken(flow.ui),
        method: "webauthn",
        traits: {
          email: values.email,
          display_name: values.display_name,
        },
        webauthn_register: JSON.stringify({
          id: cred.id,
          rawId: bufferToBase64Url(cred.rawId),
          type: cred.type,
          response: {
            attestationObject: bufferToBase64Url(response.attestationObject),
            clientDataJSON: bufferToBase64Url(response.clientDataJSON),
          },
        }),
        webauthn_register_displayname: values.display_name ?? "Passkey",
      };
      const result = await kratos.submitFlow<SelfServiceFlow>("registration", flow.id, body);
      handleSubmitSuccess(result);
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      const uiMsg = err.ui?.messages?.[0]?.text;
      setError(uiMsg || err.message || "Не удалось зарегистрироваться");
    } finally {
      setSubmitting(false);
    }
  };

  const submitPassword = async (values: RegisterFormValues) => {
    if (!flow) return;
    if (hibpWarn) {
      setError(hibpWarn);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const body = {
        csrf_token: csrfToken(flow.ui),
        method: "password",
        password: values.password,
        traits: {
          email: values.email,
          display_name: values.display_name,
        },
      };
      const result = await kratos.submitFlow<SelfServiceFlow>("registration", flow.id, body);
      handleSubmitSuccess(result);
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      const uiMsg = err.ui?.messages?.[0]?.text;
      setError(uiMsg || err.message || "Не удалось зарегистрироваться");
    } finally {
      setSubmitting(false);
    }
  };

  function handleSubmitSuccess(result: SelfServiceFlow): void {
    // return_to is flow-supplied — constrain it to a same-origin in-app path
    // (CWE-601), mirroring Login. Falls back to the post-registration dashboard.
    const target = resolvePostAuthTarget(result.return_to, null, "/dashboard");
    navigate(target, { replace: true });
  }

  if (loading || !flow) {
    return (
      <AuthLayout>
        <div style={{ textAlign: "center", padding: 48 }}>
          <Spin size="large" />
          <Paragraph type="secondary" style={{ marginTop: 16 }}>
            Загрузка registration-flow…
          </Paragraph>
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <Title level={3} style={{ margin: "0 0 12px" }}>
        Регистрация в {config.webauthnRpName}
      </Title>
      <Text type="secondary">Создайте аккаунт с passkey или паролем</Text>

      {messages.map((m, i) => (
        <Alert
          key={i}
          type={m.type === "error" ? "error" : "info"}
          showIcon
          message={m.text}
          style={{ marginTop: 16 }}
        />
      ))}
      {error && <Alert type="error" showIcon message={error} style={{ marginTop: 16 }} data-testid="register-error" />}

      <Form form={form} layout="vertical" requiredMark={false} onFinish={submitPassword} style={{ marginTop: 24 }}>
        <Form.Item
          name="email"
          label="Email"
          rules={[
            { required: true, message: "Введите email" },
            { type: "email", message: "Неверный формат email" },
          ]}
        >
          <Input
            size="large"
            prefix={<MailOutlined />}
            placeholder="user@example.com"
            autoComplete="email"
            data-testid="register-email"
          />
        </Form.Item>
        <Form.Item name="display_name" label="Имя" rules={[{ required: true, message: "Введите имя" }]}>
          <Input
            size="large"
            prefix={<UserOutlined />}
            placeholder="Alice"
            autoComplete="name"
            data-testid="register-name"
          />
        </Form.Item>

        {hasWebauthn && (
          <Button
            type="primary"
            size="large"
            block
            icon={<KeyOutlined />}
            loading={submitting}
            onClick={submitPasskey}
            data-testid="register-passkey-btn"
          >
            Создать аккаунт с Passkey
          </Button>
        )}

        {hasWebauthn && hasPassword && <Divider plain>или</Divider>}

        {hasPassword && (
          <>
            <Form.Item
              name="password"
              label="Пароль"
              rules={[
                { required: true, message: "Введите пароль" },
                { min: 8, message: "Минимум 8 символов" },
              ]}
              extra={hibpWarn || undefined}
              validateStatus={hibpWarn ? "warning" : undefined}
            >
              <Input.Password
                size="large"
                prefix={<LockOutlined />}
                placeholder="••••••••"
                autoComplete="new-password"
                data-testid="register-password"
              />
            </Form.Item>
            <Button
              type={hasWebauthn ? "default" : "primary"}
              size="large"
              htmlType="submit"
              block
              loading={submitting}
              icon={<LockOutlined />}
              disabled={!!hibpWarn}
              data-testid="register-password-btn"
            >
              Создать аккаунт с паролем
            </Button>
          </>
        )}

        <Space orientation="vertical" style={{ marginTop: 16, width: "100%" }}>
          <Button type="link" block onClick={() => navigate("/auth/login")} data-testid="register-to-login">
            Уже есть аккаунт? Войти
          </Button>
        </Space>
      </Form>
    </AuthLayout>
  );
}
