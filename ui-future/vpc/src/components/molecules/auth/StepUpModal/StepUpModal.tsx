// StepUpModal — intercept 403 step_up_required (KAC-127 Phase 2).
//
// Подписывается на onStepUpRequired в AuthContext (через
// useAuth().setStepUpHandler). При вызове:
//   1. Открывает modal с описанием действия.
//   2. По кнопке "Подтвердить" триггерит Passkey ceremony с
//      `userVerification: "required"` (для ACR=3).
//   3. На success → markMfaFresh() + resolve handler-promise → apiClient
//      replays original request.
//   4. На cancel → reject (handler error) → apiClient throws StepUpRequiredError.

import { useEffect, useRef, useState } from "react";
import { Modal, Button, Alert, Space, Typography } from "antd";
import { SafetyOutlined, KeyOutlined } from "@ant-design/icons";
import { useAuth } from "@shared/contexts/AuthContext";
import { kratos, findNode, csrfToken, type SelfServiceFlow } from "@shared/lib/kratos";
import { bufferToBase64Url } from "@/pages/auth/Login";
import { config } from "@shared/lib/config";

const { Paragraph, Text } = Typography;

interface PendingRequest {
  acr?: string;
  resolve: () => void;
  reject: (e: Error) => void;
}

export function StepUpModal() {
  const { setStepUpHandler, markMfaFresh, refresh } = useAuth();
  const [pending, setPending] = useState<PendingRequest | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pendingRef = useRef<PendingRequest | null>(null);
  pendingRef.current = pending;

  // Регистрируем обработчик.
  useEffect(() => {
    const handler = (acr?: string) =>
      new Promise<void>((resolve, reject) => {
        setPending({ acr, resolve, reject });
      });
    setStepUpHandler(handler);
    return () => setStepUpHandler(null);
  }, [setStepUpHandler]);

  const cancel = () => {
    if (pending) {
      pending.reject(new Error("Step-up cancelled by user"));
    }
    setPending(null);
    setError(null);
  };

  const confirm = async () => {
    if (!pending) return;
    setSubmitting(true);
    setError(null);
    try {
      // Открываем refresh-login flow (Kratos AAL2 с UV).
      // Получаем flow ID — для embedded ceremony Kratos требует init-redirect,
      // но в SPA-context можно получить flow через API-call.
      const initRes = await fetch(`${config.kratosUrl}/self-service/login/browser?refresh=true&aal=aal2`, {
        credentials: "include",
        redirect: "manual",
      });
      // Browser-flow возвращает 303 с Location: /auth/login?flow=<id>.
      let flowId: string | null = null;
      const loc = initRes.headers.get("Location");
      if (loc) {
        const u = new URL(loc, window.location.origin);
        flowId = u.searchParams.get("flow");
      }
      if (!flowId) {
        // Fallback: full-page redirect — оставит UI без replay, не идеал, но
        // не оставляет user без option (e.g. embedded API authn недоступен).
        window.location.assign(kratos.loginUrl(window.location.pathname + window.location.search));
        return;
      }
      const flow = await kratos.getFlow<SelfServiceFlow>("login", flowId);
      const triggerNode = findNode(flow.ui, "webauthn_login_trigger");
      if (!triggerNode?.attributes?.value) {
        throw new Error("Step-up через passkey недоступен");
      }
      const opts = JSON.parse(triggerNode.attributes.value as string) as {
        publicKey: PublicKeyCredentialRequestOptions;
      };
      const cred = (await navigator.credentials.get({
        publicKey: { ...opts.publicKey, userVerification: "required" },
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
      await kratos.submitFlow<SelfServiceFlow>("login", flow.id, body);
      // Mark MFA fresh + refresh AuthContext.
      markMfaFresh();
      await refresh();
      pending.resolve();
      setPending(null);
    } catch (e: unknown) {
      const err = e as Error;
      setError(err.message || "Step-up failed");
    } finally {
      setSubmitting(false);
    }
  };

  const acr = pending?.acr ?? "2";

  return (
    <Modal
      open={pending !== null}
      title={
        <Space>
          <SafetyOutlined />
          Подтверждение действия
        </Space>
      }
      onCancel={cancel}
      mask={{ closable: false }}
      footer={[
        <Button key="cancel" onClick={cancel} disabled={submitting}>
          Отменить
        </Button>,
        <Button
          key="ok"
          type="primary"
          icon={<KeyOutlined />}
          loading={submitting}
          onClick={confirm}
          data-testid="stepup-confirm"
        >
          Подтвердить через Passkey
        </Button>,
      ]}
      data-testid="stepup-modal"
    >
      <Paragraph>Эта операция требует дополнительной проверки безопасности (ACR={acr}).</Paragraph>
      <Paragraph>
        <Text type="secondary">
          Подтвердите запрос вашим passkey с биометрией (Touch&nbsp;ID / Windows&nbsp;Hello / security key).
        </Text>
      </Paragraph>
      {error && <Alert type="error" showIcon message={error} data-testid="stepup-error" style={{ marginTop: 12 }} />}
    </Modal>
  );
}
