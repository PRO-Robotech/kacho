// Recovery — magic-link recovery (KAC-127 Phase 2, acceptance §6.5).
//
// 3 steps:
//   1. Email input → POST `/self-service/recovery?flow=<id>` с {email, method=link}
//      → state=sent_email; UI показывает "Check email" + TTL.
//   2. ?code=...&flow=... в URL → автоматически submit `{code, method=link}`,
//      Kratos verify-ит и переводит flow в state=passed_challenge.
//   3. Force re-enrollment Passkey (settings flow с UV=required).
//      После success → redirect /dashboard.

import { useEffect, useMemo, useState } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import { Button, Form, Input, Typography, Alert, Spin, Result } from "antd";
import { MailOutlined, KeyOutlined } from "@ant-design/icons";
import { kratos, type SelfServiceFlow, csrfToken, flowMessages } from "@shared/lib/kratos";
import { AuthLayout } from "./Login";
import { config } from "@shared/lib/config";

const { Title, Text, Paragraph } = Typography;

type Step = "email" | "sent" | "passkey-enroll" | "done";

interface RecoveryFormValues {
  email?: string;
}

export function RecoveryPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [flow, setFlow] = useState<SelfServiceFlow | null>(null);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [step, setStep] = useState<Step>("email");
  const [form] = Form.useForm<RecoveryFormValues>();

  const flowId = searchParams.get("flow");
  const code = searchParams.get("code");

  useEffect(() => {
    if (!flowId) {
      window.location.assign(kratos.recoveryUrl());
      return;
    }
    setLoading(true);
    kratos
      .getFlow<SelfServiceFlow>("recovery", flowId)
      .then((f) => {
        setFlow(f);
        // Восстанавливаем step по flow.state.
        if (f.state === "sent_email") setStep("sent");
        else if (f.state === "passed_challenge") setStep("passkey-enroll");
        else setStep("email");
      })
      .catch((e: Error & { status?: number }) => {
        if (e.status === 410 || e.status === 404) {
          window.location.assign(kratos.recoveryUrl());
          return;
        }
        setError(e.message || "Не удалось загрузить recovery-flow");
      })
      .finally(() => setLoading(false));
  }, [flowId]);

  // Авто-submit code если приехали по magic-link.
  useEffect(() => {
    if (!flow || !code || step !== "email") return;
    void submitCode(code);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flow, code]);

  const messages = useMemo(() => (flow ? flowMessages(flow.ui) : []), [flow]);

  const submitEmail = async (values: RecoveryFormValues) => {
    if (!flow) return;
    setSubmitting(true);
    setError(null);
    try {
      const body = {
        csrf_token: csrfToken(flow.ui),
        method: "code",
        email: values.email,
      };
      const updated = await kratos.submitFlow<SelfServiceFlow>("recovery", flow.id, body);
      setFlow(updated);
      setStep("sent");
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      const uiMsg = err.ui?.messages?.[0]?.text;
      setError(uiMsg || err.message || "Не удалось отправить ссылку");
    } finally {
      setSubmitting(false);
    }
  };

  const submitCode = async (codeValue: string) => {
    if (!flow) return;
    setSubmitting(true);
    setError(null);
    try {
      const body = {
        csrf_token: csrfToken(flow.ui),
        method: "code",
        code: codeValue,
      };
      const updated = await kratos.submitFlow<SelfServiceFlow>("recovery", flow.id, body);
      setFlow(updated);
      if (updated.state === "passed_challenge") {
        setStep("passkey-enroll");
      }
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      const uiMsg = err.ui?.messages?.[0]?.text;
      setError(uiMsg || err.message || "Неверный или истекший код");
    } finally {
      setSubmitting(false);
    }
  };

  const triggerPasskeyEnroll = () => {
    // После passed_challenge Kratos выдаёт privileged session → settings-flow.
    window.location.assign(kratos.settingsUrl("/dashboard"));
  };

  if (loading || !flow) {
    return (
      <AuthLayout>
        <div style={{ textAlign: "center", padding: 48 }}>
          <Spin size="large" />
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <Title level={3} style={{ margin: "0 0 12px" }}>
        Восстановление доступа
      </Title>

      {messages.map((m, i) => (
        <Alert
          key={i}
          type={m.type === "error" ? "error" : "info"}
          showIcon
          message={m.text}
          style={{ marginTop: 16 }}
        />
      ))}
      {error && <Alert type="error" showIcon message={error} style={{ marginTop: 16 }} data-testid="recovery-error" />}

      {step === "email" && (
        <>
          <Text type="secondary">
            Введите email — мы пришлём ссылку для восстановления доступа. Ссылка действительна{" "}
            {config.recoveryLinkTtlMin} минут.
          </Text>
          <Form form={form} layout="vertical" requiredMark={false} onFinish={submitEmail} style={{ marginTop: 24 }}>
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
                data-testid="recovery-email"
              />
            </Form.Item>
            <Button
              type="primary"
              size="large"
              htmlType="submit"
              block
              loading={submitting}
              icon={<MailOutlined />}
              data-testid="recovery-submit"
            >
              Отправить ссылку
            </Button>
            <Button type="link" block style={{ marginTop: 12 }} onClick={() => navigate("/auth/login")}>
              Вернуться ко входу
            </Button>
          </Form>
        </>
      )}

      {step === "sent" && (
        <Result
          status="success"
          icon={<MailOutlined />}
          title="Проверьте email"
          subTitle={
            <Paragraph type="secondary">
              Мы отправили ссылку для восстановления. Откройте email и кликните по ссылке (действительна{" "}
              {config.recoveryLinkTtlMin} минут).
            </Paragraph>
          }
          extra={
            <Button type="link" onClick={() => navigate("/auth/login")} data-testid="recovery-back-to-login">
              Вернуться ко входу
            </Button>
          }
        />
      )}

      {step === "passkey-enroll" && (
        <Result
          status="info"
          icon={<KeyOutlined />}
          title="Восстановите passkey"
          subTitle="Старые passkey удалены. Зарегистрируйте новый ключ для дальнейшего доступа."
          extra={
            <Button
              type="primary"
              icon={<KeyOutlined />}
              onClick={triggerPasskeyEnroll}
              data-testid="recovery-passkey-enroll"
            >
              Зарегистрировать passkey
            </Button>
          }
        />
      )}
    </AuthLayout>
  );
}
