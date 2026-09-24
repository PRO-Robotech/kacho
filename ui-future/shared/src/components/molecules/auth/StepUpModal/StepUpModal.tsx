// Повторное подтверждение личности — ОДНА реализация на продукт.
//
// Дом здесь потому, что подтверждение личности принадлежит не модулю, а
// продукту: его просит любое действие, меняющее посадку безопасности, и просить
// его двумя разными окнами значит показывать человеку два разных продукта в
// момент, когда он и так насторожён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ОКНО ДЕЛАЕТ (#1213, приёмка F8 S2)
//
// Окно — единственное место консоли, где человек поднимает уровень
// уверенности или свежесть предъявления. Ведёт оно это НАШИМ глаголом —
// `POST /iam/v1/auth/step-up` с признаком формы вида `step-up`, — а не уводит на
// чужое приложение: церемония проходит, не покидая консоли, и отвергнутое
// действие после неё повторяется (его повторяет клиент API, `requestStepUp`).
//
// Способ выбирает ЧЕЛОВЕК, и окно предлагает только те, что отвечают просьбе
// (`StepUpRequest`, @shared/api/step-up):
//
//   • вызов края RFC 9470 — уровень (`acr_values`) либо свежесть второго фактора
//     (один `max_age`) закрывает только второй фактор, поэтому пароля в выборе
//     нет — и тогда, когда уровень в вызове не назван;
//   • служба требует свежести (`SESSION_NOT_FRESH`) — годится и пароль.
//
// ПОЧЕМУ «ВТОРОЙ ФАКТОР НЕ НАСТРОЕН» — НАЗВАННОЕ СОСТОЯНИЕ, А НЕ ПУСТОЕ ОКНО
//
// Окно не спрашивает заранее, заведён ли фактор: служба отвечает на
// предъявление, и отказ `SECOND_FACTOR_NOT_ENROLLED` называет это сама. Окно
// показывает её текст и путь туда, где фактор заводят, и НЕ закрывается молча
// (F8-36). Обещание запроса при этом не разрешается: fail-closed — разрешить его
// значило бы пропустить действие, за которое никто не поручился.

import { useEffect, useId, useMemo, useState } from "react";
import { Button, Form, Input, Modal, Radio, Typography } from "antd";
import {
  FormTokenHolder,
  LANE_REASON,
  LaneRefusal,
  laneRefusalOf,
  loginLane,
  type SecondFactorPresentation,
} from "@shared/api/login-lane";
import { setStepUpRequester, type StepUpRequest } from "@shared/api/step-up";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { EMPTY_PRESENTATION, SecondFactorCodeField } from "@shared/components/molecules/auth/SecondFactorCodeField";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useOptionalAuth } from "@shared/contexts/AuthContext";
import { ACCOUNT_SETTINGS_ADDRESS } from "@shared/pages/auth/ceremony-addresses";

const { Paragraph } = Typography;

/** Экран, где заводят второй фактор, — параметры учётной записи консоли. */
export const SECOND_FACTOR_ENROLLMENT_ADDRESS = ACCOUNT_SETTINGS_ADDRESS;

interface PendingRequest {
  request: StepUpRequest;
  resolve: () => void;
  reject: (e: Error) => void;
}

type Branch = "пароль" | "второй фактор";

export function StepUpModal() {
  const id = useId();
  const auth = useOptionalAuth();
  // Признак добывается при отправке, а не при монтировании: окно смонтировано
  // на каждой странице модуля, а открывается редко.
  const holder = useMemo(() => new FormTokenHolder("step-up"), []);
  const [pending, setPending] = useState<PendingRequest | null>(null);
  const [branch, setBranch] = useState<Branch>("второй фактор");
  const [password, setPassword] = useState("");
  const [factor, setFactor] = useState<SecondFactorPresentation>(EMPTY_PRESENTATION);
  const [submitting, setSubmitting] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  // Обработчик ОБЪЯВЛЯЕТСЯ клиенту API — он и есть его читатель (#1213).
  useEffect(() => {
    const handler = (request: StepUpRequest) =>
      new Promise<void>((resolve, reject) => {
        setBranch(request.cause === "freshness" ? "пароль" : "второй фактор");
        setPassword("");
        setFactor(EMPTY_PRESENTATION);
        setRefusal(null);
        setPending({ request, resolve, reject });
      });
    setStepUpRequester(handler);
    return () => setStepUpRequester(null);
  }, []);

  // Пароль предлагается ТОЛЬКО на просьбу свежести от службы: вызов края
  // закрывает один второй фактор, назван в нём уровень или нет.
  const passwordAllowed = pending?.request.cause === "freshness";

  const close = () => {
    setPending(null);
    setPassword("");
    setFactor(EMPTY_PRESENTATION);
    setRefusal(null);
  };

  const cancel = () => {
    pending?.reject(new Error("повышение отменено человеком"));
    close();
  };

  const confirm = async () => {
    if (!pending || submitting) return;
    setSubmitting(true);
    setRefusal(null);
    try {
      await loginLane.stepUp(
        holder,
        branch === "пароль" && passwordAllowed ? { method: "password", password } : factor,
      );
      // Уровень после церемонии знает край по нашей сессии; консоли достаточно
      // перечитать личность и отпустить отвергнутый запрос на повтор.
      await auth?.refresh();
      pending.resolve();
      close();
    } catch (err) {
      setRefusal(laneRefusalOf(err));
    } finally {
      setSubmitting(false);
    }
  };

  const notEnrolled = refusal?.reason === LANE_REASON.secondFactorNotEnrolled;
  const passwordId = `${id}-password`;

  return (
    <Modal
      open={pending !== null}
      title="Подтверждение действия"
      onCancel={cancel}
      mask={{ closable: false }}
      footer={[
        <Button key="cancel" onClick={cancel} disabled={submitting}>
          Отменить
        </Button>,
        <Button key="ok" type="primary" loading={submitting} onClick={() => void confirm()}>
          Подтвердить
        </Button>,
      ]}
    >
      <FormGrid label="Подтверждение действия" onSubmit={() => void confirm()}>
        <Paragraph>
          {passwordAllowed
            ? "Это действие требует подтвердить личность ещё раз."
            : "Это действие подтверждается вторым фактором."}
        </Paragraph>
        {passwordAllowed && (
          <Form.Item label="Способ">
            <Radio.Group
              aria-label="Способ"
              value={branch}
              onChange={(ev) => setBranch(ev.target.value as Branch)}
              options={[
                { value: "пароль", label: "Паролем" },
                { value: "второй фактор", label: "Вторым фактором" },
              ]}
            />
          </Form.Item>
        )}
        {branch === "пароль" && passwordAllowed ? (
          <Form.Item label="Пароль" htmlFor={passwordId}>
            <Input
              id={passwordId}
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(ev) => setPassword(ev.target.value)}
            />
          </Form.Item>
        ) : (
          <SecondFactorCodeField
            value={factor}
            onChange={setFactor}
            codeError={refusal?.field === "code" ? refusal.message : null}
            methodError={refusal?.field === "method" ? refusal.message : null}
          />
        )}
        {refusal && refusal.field !== "code" && refusal.field !== "method" && (
          <div style={{ marginBottom: 12 }}>
            <LaneRefusalAlert refusal={refusal} />
            {notEnrolled && (
              <Paragraph style={{ marginTop: 8 }}>
                <a
                  href={SECOND_FACTOR_ENROLLMENT_ADDRESS}
                  onClick={() => pending?.reject(new Error("второй фактор не настроен"))}
                >
                  Настроить второй фактор
                </a>
              </Paragraph>
            )}
          </div>
        )}
        {/* Отправка клавишей ввода из поля — та же, что кнопкой. */}
        <button type="submit" hidden aria-hidden tabIndex={-1} />
      </FormGrid>
    </Modal>
  );
}
