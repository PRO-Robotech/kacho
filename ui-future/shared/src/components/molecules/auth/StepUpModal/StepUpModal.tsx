// Повторное подтверждение личности — ОДНА реализация на продукт.
//
// Копий было две, в iam и в system, и различались они ровно одной строкой —
// алиасом импорта страницы входа, — причём обе вели к одному и тому же файлу в
// shared. То есть дублировались 368 строк ради разницы, которой по существу не
// было. Сама та страница с тех пор снята (#1225): продукт её не монтировал ни
// одним маршрутом, а адрес входа принадлежит поставщику личности. Кодировщик
// двоичных полей, единственное, что окно оттуда брало, живёт своим модулем.
//
// Дом здесь потому, что подтверждение личности принадлежит не модулю, а
// продукту: его просит любое действие, меняющее посадку безопасности, и просить
// его двумя разными окнами значит показывать человеку два разных продукта в
// момент, когда он и так насторожён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ОКНО ДЕЛАЕТ (#1213)
//
// Край объявляет части глаголов пол уровня уверенности «2» и спрашивает его на
// браузерной полосе. Окно — единственное место консоли, где арендатор этот
// уровень поднимает. Значит оно обязано вести ТОТ способ, который служба
// личности действительно предлагает, а не тот, который однажды выбрал автор.
//
// Прежняя редакция вела РОВНО ОДИН способ — ключ доступа, — и объявляла его
// единственным в тексте кнопки. Настройки же объявляют ключ доступа
// БЕСПАРОЛЬНЫМ, то есть ПЕРВЫМ фактором: в потоке `aal=aal2` служба его не
// предлагает вовсе. Две стороны об одном предмете, и неверна была их РАЗНИЦА:
// 32 глагола каталога оказывались недостижимы из браузера ДЛЯ ВСЕХ.
//
// Поэтому способ теперь ВЫВОДИТСЯ ИЗ ПОТОКА, а не выбирается здесь: окно
// спрашивает службу личности, что она предлагает, и ведёт первый способ, который
// умеет (перечень — `@shared/lib/step-up-methods`, он же сторона консоли для
// гейта дерева). Согласие двух сторон становится свойством построения, а не
// совпадением двух объявлений.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОТСУТСТВИЕ ВТОРОГО ФАКТОРА — НАЗВАННОЕ СОСТОЯНИЕ, А НЕ ОШИБКА
//
// У арендатора, заведённого паролем, второго фактора нет НИ ОДНОГО, пока он его
// не настроит. Это не сбой и не отказ в правах: это отсутствующее предусловие,
// и единственный полезный ответ — сказать, чего не хватает, и отвести туда, где
// это заводят. Молчаливый отказ здесь читался бы как «действие вам запрещено»,
// а его на самом деле просто нечем подтвердить.
//
// Обещание запроса при этом НЕ разрешается: fail-closed. Разрешить его значило
// бы пропустить действие, за которое никто не поручился.

import { useEffect, useState } from "react";
import { Modal, Button, Alert, Space, Typography, Input } from "antd";
import { SafetyOutlined, KeyOutlined, NumberOutlined, SettingOutlined } from "@ant-design/icons";
import { useAuth } from "@shared/contexts/AuthContext";
import { kratos, findNode, csrfToken, type SelfServiceFlow } from "@shared/lib/kratos";
import { bufferToBase64Url } from "@shared/lib/webauthn";
import { STEP_UP_METHODS, STEP_UP_METHOD_NODES, type StepUpMethod } from "@shared/lib/step-up-methods";

const { Paragraph, Text } = Typography;

interface PendingRequest {
  acr?: string;
  resolve: () => void;
  reject: (e: Error) => void;
}

/** Способ, отправляемый КОДОМ, а не церемонией ключа доступа. */
type CodeMethod = Exclude<StepUpMethod, "webauthn">;

/**
 * Способы, отправляемые кодом.
 *
 * Тип ключа — исчерпывающий, а не строка: способ, добавленный в перечень и
 * забытый здесь, роняет СБОРКУ, а не даёт пустое окно у арендатора. Гейт
 * дерева про это ничего не знает — он читает перечень, а не таблицу.
 */
const CODE_METHODS: Record<CodeMethod, { field: string; label: string; hint: string; placeholder: string }> = {
  totp: {
    field: "totp_code",
    label: "Код из приложения-аутентификатора",
    hint: "Введите шестизначный код, который показывает ваше приложение-аутентификатор.",
    placeholder: "123456",
  },
  lookup_secret: {
    field: "lookup_secret",
    label: "Запасной код",
    hint: "Введите один из запасных кодов, выданных при настройке второго фактора. Код одноразовый.",
    placeholder: "xxxxxxxx",
  },
};

/**
 * Адрес, по которому арендатор заводит второй фактор.
 *
 * Спрашивается у СЛУЖБЫ ЛИЧНОСТИ, а не выписывается здесь: путь раздела
 * параметров безопасности задаётся развёртыванием (`selfservice.flows.settings.
 * ui_url`), и константа в консоли разошлась бы с ним молча.
 *
 * Адрес возврата — АБСОЛЮТНЫЙ (`location.href`), а не путь: перечень
 * разрешённых возвратов службы объявлен полными адресами консоли, и
 * относительный путь она разрешала бы относительно СВОЕГО адреса, то есть
 * увела бы человека не туда либо отвергла возврат вовсе.
 */
export function stepUpEnrollUrl(): string {
  return kratos.settingsUrl(window.location.href);
}

/** Первый способ, который служба личности предложила И окно умеет вести. */
export function offeredStepUpMethod(flow: SelfServiceFlow): StepUpMethod | null {
  for (const m of STEP_UP_METHODS) {
    if (findNode(flow.ui, STEP_UP_METHOD_NODES[m])) return m;
  }
  return null;
}

export function StepUpModal() {
  const { setStepUpHandler, markMfaFresh, refresh } = useAuth();
  const [pending, setPending] = useState<PendingRequest | null>(null);
  const [flow, setFlow] = useState<SelfServiceFlow | null>(null);
  const [method, setMethod] = useState<StepUpMethod | null>(null);
  const [loading, setLoading] = useState(false);
  const [code, setCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Регистрируем обработчик. Состояние «спрашиваем способ» выставляется ЗДЕСЬ,
  // а не в эффекте ниже: эффект вправе трогать состояние только после ответа
  // службы личности, иначе открытие окна давало бы каскад перерисовок.
  useEffect(() => {
    const handler = (acr?: string) =>
      new Promise<void>((resolve, reject) => {
        setFlow(null);
        setMethod(null);
        setError(null);
        setCode("");
        setLoading(true);
        setPending({ acr, resolve, reject });
      });
    setStepUpHandler(handler);
    return () => setStepUpHandler(null);
  }, [setStepUpHandler]);

  // Поток поднимается СРАЗУ при открытии окна, а не по нажатию: способ выбирает
  // служба личности, и до её ответа окно не знает, что предлагать человеку.
  useEffect(() => {
    if (!pending) return;
    let cancelled = false;
    void (async () => {
      try {
        const f = await kratos.initFlow<SelfServiceFlow>("login", { refresh: "true", aal: "aal2" });
        if (cancelled) return;
        setFlow(f);
        setMethod(offeredStepUpMethod(f));
      } catch (e) {
        if (cancelled) return;
        setFlow(null);
        setMethod(null);
        setError((e as Error).message || "Не удалось начать повторное подтверждение");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [pending]);

  const cancel = () => {
    if (pending) {
      pending.reject(new Error("Step-up cancelled by user"));
    }
    setPending(null);
    setFlow(null);
    setMethod(null);
    setError(null);
    setCode("");
  };

  const enroll = () => {
    // Обещание запроса отвергается ДО ухода со страницы: действие не выполнено
    // и выполнено не будет. Оставить его висеть значило бы держать вызывающего
    // в ожидании исхода, которого не наступит.
    pending?.reject(new Error("Second factor is not enrolled"));
    window.location.assign(stepUpEnrollUrl());
  };

  const done = async () => {
    markMfaFresh();
    await refresh();
    pending?.resolve();
    setPending(null);
    setFlow(null);
    setMethod(null);
    setCode("");
  };

  const confirm = async () => {
    if (!pending || !flow || !method) return;
    setSubmitting(true);
    setError(null);
    try {
      if (method === "webauthn") {
        const raw = findNode(flow.ui, STEP_UP_METHOD_NODES.webauthn)?.attributes?.value;
        // Величина узла типизирована как неизвестная: приводить её к строке
        // безусловно значит однажды отправить в разбор `[object Object]`.
        if (typeof raw !== "string" || raw === "") {
          throw new Error("служба личности не отдала вызов ключа доступа");
        }
        const opts = JSON.parse(raw) as { publicKey: PublicKeyCredentialRequestOptions };
        const cred = (await navigator.credentials.get({
          publicKey: { ...opts.publicKey, userVerification: "required" },
        })) as PublicKeyCredential | null;
        if (!cred) throw new Error("Ceremony отменена");
        const response = cred.response as AuthenticatorAssertionResponse;
        await kratos.submitFlow<SelfServiceFlow>("login", flow.id, {
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
        });
      } else {
        const spec = CODE_METHODS[method];
        const value = code.trim();
        if (!value) throw new Error(`${spec.label} — обязательное поле`);
        await kratos.submitFlow<SelfServiceFlow>("login", flow.id, {
          csrf_token: csrfToken(flow.ui),
          method,
          [spec.field]: value,
        });
      }
      await done();
    } catch (e: unknown) {
      const err = e as Error;
      setError(err.message || "Step-up failed");
    } finally {
      setSubmitting(false);
    }
  };

  const acr = pending?.acr ?? "2";
  const codeSpec = method && method !== "webauthn" ? CODE_METHODS[method] : null;
  // Второго фактора нет: поток поднялся, но ни один способ, который окно умеет
  // вести, служба личности не предложила. Отличать это от «поток не поднялся»
  // обязательно — исправления у них разные.
  const noSecondFactor = !loading && !error && flow !== null && method === null;

  const footer: React.ReactNode[] = [
    <Button key="cancel" onClick={cancel} disabled={submitting}>
      Отменить
    </Button>,
  ];
  if (noSecondFactor) {
    footer.push(
      <Button key="enroll" type="primary" icon={<SettingOutlined />} onClick={enroll} data-testid="stepup-enroll">
        Настроить второй фактор
      </Button>,
    );
  } else if (method !== null) {
    footer.push(
      <Button
        key="ok"
        type="primary"
        icon={method === "webauthn" ? <KeyOutlined /> : <NumberOutlined />}
        loading={submitting}
        onClick={confirm}
        data-testid="stepup-confirm"
      >
        {method === "webauthn" ? "Подтвердить ключом доступа" : "Подтвердить"}
      </Button>,
    );
  }

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
      footer={footer}
      data-testid="stepup-modal"
    >
      <Paragraph>Эта операция требует дополнительной проверки безопасности (ACR={acr}).</Paragraph>

      {loading && (
        <Paragraph data-testid="stepup-loading">
          <Text type="secondary">Спрашиваем, каким способом можно подтвердить…</Text>
        </Paragraph>
      )}

      {method === "webauthn" && (
        <Paragraph>
          <Text type="secondary">
            Подтвердите запрос вашим ключом доступа с биометрией (Touch&nbsp;ID / Windows&nbsp;Hello / аппаратный
            ключ).
          </Text>
        </Paragraph>
      )}

      {codeSpec && (
        <>
          <Paragraph>
            <Text type="secondary">{codeSpec.hint}</Text>
          </Paragraph>
          <Input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder={codeSpec.placeholder}
            aria-label={codeSpec.label}
            autoComplete="one-time-code"
            data-testid="stepup-code"
          />
        </>
      )}

      {noSecondFactor && (
        <Alert
          type="warning"
          showIcon
          data-testid="stepup-no-second-factor"
          message="Второй фактор не настроен"
          description={
            "Это действие подтверждается вторым фактором, а у вашей учётной записи его пока нет: " +
            "вход выполнен только паролем. Настройте одноразовые коды в параметрах безопасности " +
            "и повторите действие."
          }
          style={{ marginTop: 12 }}
        />
      )}

      {error && <Alert type="error" showIcon message={error} data-testid="stepup-error" style={{ marginTop: 12 }} />}
    </Modal>
  );
}
