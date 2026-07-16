// Settings — manage passkeys + TOTP (KAC-127 Phase 2, acceptance §6.4).
//
// Что покрывает:
//   - List existing passkeys (из flow.ui webauthn_remove nodes).
//   - "Add Passkey" → navigator.credentials.create через webauthn_register_trigger.
//   - "Remove Passkey" → POST с конкретным removed credential_id.
//   - TOTP enroll: показывает QR (image node), вводит код.
//   - TOTP revoke: чек-бокс + submit.
//   - Step-up confirm: settings-flow требует AAL2 — если не свежий, Kratos
//     вернёт 403 + state=show_form → UI redirect на login?refresh=true&aal=aal2.

import { useEffect, useMemo, useState } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import { Button, Card, Form, Input, Typography, Alert, Space, Spin, Divider, List, Modal, Image } from "antd";
import { KeyOutlined, SafetyOutlined, DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { kratos, type SelfServiceFlow, csrfToken, findNode, flowMessages } from "@shared/lib/kratos";
import { AuthLayout, bufferToBase64Url } from "./Login";
import { config } from "@shared/lib/config";
import { formatDateTime } from "@shared/lib/datetime";

const { Title, Text, Paragraph } = Typography;

interface PasskeyEntry {
  id: string;
  display_name?: string;
  added_at?: string;
}

export function SettingsPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [flow, setFlow] = useState<SelfServiceFlow | null>(null);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [totpForm] = Form.useForm<{ totp_code?: string }>();

  const flowId = searchParams.get("flow");

  useEffect(() => {
    if (!flowId) {
      window.location.assign(kratos.settingsUrl(window.location.pathname));
      return;
    }
    setLoading(true);
    kratos
      .getFlow<SelfServiceFlow>("settings", flowId)
      .then((f) => {
        setFlow(f);
        // Если flow требует privileged session (последняя AAL2 устарела) —
        // Kratos выдаёт state=show_form но обычно требует re-login.
        // Простой signal — flow.refresh=true.
        if (f.refresh === true) {
          setInfo("Для изменений безопасности нужна повторная проверка. Подтвердите passkey.");
        }
      })
      .catch((e: Error & { status?: number; redirect_browser_to?: string }) => {
        if (e.redirect_browser_to) {
          // Kratos вернул редирект (на login с aal=aal2).
          window.location.assign(e.redirect_browser_to);
          return;
        }
        if (e.status === 410 || e.status === 404) {
          window.location.assign(kratos.settingsUrl());
          return;
        }
        setError(e.message || "Не удалось загрузить settings-flow");
      })
      .finally(() => setLoading(false));
  }, [flowId]);

  const messages = useMemo(() => (flow ? flowMessages(flow.ui) : []), [flow]);

  /** Извлекаем список зарегистрированных passkeys из UI-nodes. */
  const passkeys = useMemo<PasskeyEntry[]>(() => {
    if (!flow) return [];
    return flow.ui.nodes
      .filter((n) => n.group === "webauthn" && n.attributes?.name === "webauthn_remove")
      .map((n) => ({
        id: String(n.attributes?.value ?? ""),
        display_name: (n.meta?.label?.text as string | undefined) ?? "Passkey",
        added_at: (n.attributes?.added_at as string | undefined) ?? undefined,
      }));
  }, [flow]);

  /** TOTP отображение QR (если flow в стадии enroll). */
  const totpImage = useMemo(() => {
    if (!flow) return null;
    const n = flow.ui.nodes.find((x) => x.type === "img" && x.attributes?.name === "totp_qr");
    return n?.attributes?.src ? String(n.attributes.src) : null;
  }, [flow]);

  const totpEnrolled = useMemo(() => {
    if (!flow) return false;
    // Если есть node totp_unlink — TOTP уже подключён.
    return !!findNode(flow.ui, "totp_unlink");
  }, [flow]);

  const addPasskey = async () => {
    if (!flow) return;
    setSubmitting(true);
    setError(null);
    try {
      const triggerNode = findNode(flow.ui, "webauthn_register_trigger");
      if (!triggerNode?.attributes?.value) {
        throw new Error("Add Passkey недоступен");
      }
      const opts = JSON.parse(triggerNode.attributes.value as string) as {
        publicKey: PublicKeyCredentialCreationOptions;
      };
      const cred = (await navigator.credentials.create({
        publicKey: opts.publicKey,
      })) as PublicKeyCredential | null;
      if (!cred) throw new Error("Ceremony отменена");
      const response = cred.response as AuthenticatorAttestationResponse;
      const body = {
        csrf_token: csrfToken(flow.ui),
        method: "webauthn",
        webauthn_register: JSON.stringify({
          id: cred.id,
          rawId: bufferToBase64Url(cred.rawId),
          type: cred.type,
          response: {
            attestationObject: bufferToBase64Url(response.attestationObject),
            clientDataJSON: bufferToBase64Url(response.clientDataJSON),
          },
        }),
        webauthn_register_displayname: `Passkey ${new Date().toLocaleDateString()}`,
      };
      const updated = await kratos.submitFlow<SelfServiceFlow>("settings", flow.id, body);
      setFlow(updated);
      setInfo("Passkey добавлен");
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      setError(err.ui?.messages?.[0]?.text || err.message || "Ошибка добавления");
    } finally {
      setSubmitting(false);
    }
  };

  const removePasskey = (entry: PasskeyEntry) => {
    if (!flow) return;
    Modal.confirm({
      title: "Удалить passkey?",
      content: `Passkey "${entry.display_name}" будет удалён. Если это последний — вход возможен только через recovery.`,
      okText: "Удалить",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: async () => {
        setSubmitting(true);
        setError(null);
        try {
          const body = {
            csrf_token: csrfToken(flow.ui),
            method: "webauthn",
            webauthn_remove: entry.id,
          };
          const updated = await kratos.submitFlow<SelfServiceFlow>("settings", flow.id, body);
          setFlow(updated);
          setInfo("Passkey удалён");
        } catch (e: unknown) {
          const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
          setError(err.ui?.messages?.[0]?.text || err.message || "Ошибка удаления");
        } finally {
          setSubmitting(false);
        }
      },
    });
  };

  const submitTotp = async (values: { totp_code?: string }) => {
    if (!flow || !values.totp_code) return;
    setSubmitting(true);
    setError(null);
    try {
      const body = {
        csrf_token: csrfToken(flow.ui),
        method: "totp",
        totp_code: values.totp_code,
      };
      const updated = await kratos.submitFlow<SelfServiceFlow>("settings", flow.id, body);
      setFlow(updated);
      setInfo("TOTP подключён");
      totpForm.resetFields();
    } catch (e: unknown) {
      const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
      setError(err.ui?.messages?.[0]?.text || err.message || "Ошибка TOTP");
    } finally {
      setSubmitting(false);
    }
  };

  const unlinkTotp = () => {
    if (!flow) return;
    Modal.confirm({
      title: "Отключить TOTP?",
      content: "Без TOTP останется только passkey/recovery как 2-й фактор.",
      okText: "Отключить",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: async () => {
        setSubmitting(true);
        setError(null);
        try {
          const body = {
            csrf_token: csrfToken(flow.ui),
            method: "totp",
            totp_unlink: true,
          };
          const updated = await kratos.submitFlow<SelfServiceFlow>("settings", flow.id, body);
          setFlow(updated);
          setInfo("TOTP отключён");
        } catch (e: unknown) {
          const err = e as Error & { ui?: { messages?: Array<{ text: string }> } };
          setError(err.ui?.messages?.[0]?.text || err.message || "Ошибка");
        } finally {
          setSubmitting(false);
        }
      },
    });
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
        Настройки безопасности
      </Title>
      <Text type="secondary">Управление passkey и TOTP в {config.webauthnRpName}</Text>

      {info && (
        <Alert
          type="info"
          showIcon
          closable
          message={info}
          style={{ marginTop: 16 }}
          onClose={() => setInfo(null)}
          data-testid="settings-info"
        />
      )}
      {messages.map((m, i) => (
        <Alert
          key={i}
          type={m.type === "error" ? "error" : "info"}
          showIcon
          message={m.text}
          style={{ marginTop: 16 }}
        />
      ))}
      {error && <Alert type="error" showIcon message={error} style={{ marginTop: 16 }} data-testid="settings-error" />}

      {/* Passkeys section */}
      <Card
        title={
          <Space>
            <KeyOutlined />
            Passkey
          </Space>
        }
        style={{ marginTop: 24 }}
        extra={
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
            loading={submitting}
            onClick={addPasskey}
            data-testid="settings-add-passkey"
          >
            Добавить
          </Button>
        }
      >
        {passkeys.length === 0 ? (
          <Paragraph type="secondary" style={{ margin: 0 }}>
            Passkey не зарегистрированы.
          </Paragraph>
        ) : (
          <List
            dataSource={passkeys}
            renderItem={(entry) => (
              <List.Item
                actions={[
                  <Button
                    key="del"
                    type="text"
                    danger
                    icon={<DeleteOutlined />}
                    onClick={() => removePasskey(entry)}
                    data-testid={`settings-remove-passkey-${entry.id}`}
                  >
                    Удалить
                  </Button>,
                ]}
              >
                <List.Item.Meta
                  avatar={<KeyOutlined />}
                  title={entry.display_name}
                  description={entry.added_at ? `Добавлен ${formatDateTime(entry.added_at)}` : undefined}
                />
              </List.Item>
            )}
          />
        )}
      </Card>

      {/* TOTP section */}
      <Card
        title={
          <Space>
            <SafetyOutlined />
            TOTP (authenticator-приложение)
          </Space>
        }
        style={{ marginTop: 16 }}
      >
        {totpEnrolled ? (
          <Space orientation="vertical" style={{ width: "100%" }}>
            <Paragraph type="secondary" style={{ margin: 0 }}>
              TOTP подключён.
            </Paragraph>
            <Button
              danger
              icon={<DeleteOutlined />}
              loading={submitting}
              onClick={unlinkTotp}
              data-testid="settings-totp-unlink"
            >
              Отключить TOTP
            </Button>
          </Space>
        ) : (
          <>
            {totpImage && (
              <>
                <Paragraph type="secondary">Отсканируйте QR в Google Authenticator / Authy / 1Password:</Paragraph>
                <div style={{ textAlign: "center", padding: 12 }}>
                  <Image src={totpImage} width={180} preview={false} />
                </div>
                <Divider />
              </>
            )}
            <Form form={totpForm} layout="inline" requiredMark={false} onFinish={submitTotp}>
              <Form.Item name="totp_code" rules={[{ required: true, message: "6-значный код" }]}>
                <Input
                  prefix={<SafetyOutlined />}
                  maxLength={6}
                  placeholder="123456"
                  data-testid="settings-totp-code"
                />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" loading={submitting} data-testid="settings-totp-submit">
                  Подтвердить
                </Button>
              </Form.Item>
            </Form>
          </>
        )}
      </Card>

      <div style={{ marginTop: 24, textAlign: "center" }}>
        <Button type="link" onClick={() => navigate("/")} data-testid="settings-back">
          Вернуться в консоль
        </Button>
      </div>
    </AuthLayout>
  );
}
