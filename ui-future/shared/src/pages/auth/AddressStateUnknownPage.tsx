// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Button, Space, Spin, Typography } from "antd";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { CeremonyScreen } from "./CeremonyScreen";
import { useLogout } from "./use-logout";

// Страница «не удалось узнать» стража подтверждённости адреса (приёмка F6b, Р7):
// ответ о сессии не назвал подтверждённость адреса либо не пришёл по существу.
//
// Каркаса она не открывает — рубеж закрывающий, — но и «вы вышли» не говорит и
// на вход не уводит: «спросить не удалось» не есть «сессии нет». Действий два:
// спросить снова и выйти. Текст — что именно не удалось узнать; причины консоль
// не выдумывает.
//
// СЛЕДУЮЩИЙ ШАГ И АДРЕСАТ НАЗВАНЫ (#2955). Страница стоит над всей консолью, и
// человеку на ней нужен путь дальше, названный его словами: проверить снова,
// войти заново, а если не помогает — к кому идти. Внутренних слов (как устроена
// доставка ответа о сессии) на ней нет: они человеку ничего не объясняют.

/** Текст страницы «не названо» (Р7): что именно не удалось узнать. */
export const ADDRESS_STATE_NOT_NAMED_TEXT =
  "Не удалось узнать, подтверждён ли адрес почты: служба доступа не сообщила этого в ответе о сессии.";

/** Следующий шаг и адресат обращения — один на оба положения страницы. */
export const ADDRESS_STATE_NEXT_STEP =
  "Нажмите «Проверить снова». Если страница появляется опять, нажмите «Выйти» и войдите заново; если и это не " +
  "помогает, сообщите администратору облака, что консоль не может проверить подтверждение адреса почты.";

export function AddressStateUnknownPage({
  text,
  onRetry,
  leave,
}: {
  text: string;
  onRetry: () => void;
  leave?: (to: string) => void;
}) {
  const { logout, busy, refusal } = useLogout(leave);
  return (
    <CeremonyScreen title="Не удалось проверить сессию">
      <Typography.Paragraph>{text}</Typography.Paragraph>
      <Typography.Paragraph>{ADDRESS_STATE_NEXT_STEP}</Typography.Paragraph>
      {refusal && (
        <div style={{ marginBottom: 16 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      <Space>
        <Button type="primary" onClick={onRetry}>
          Проверить снова
        </Button>
        <Button loading={busy} onClick={() => void logout()}>
          Выйти
        </Button>
      </Space>
    </CeremonyScreen>
  );
}

/** Ответа о сессии ещё нет: ни каркаса, ни экрана — только ожидание. */
export function SessionPending() {
  return (
    <main style={{ height: "100vh", display: "grid", placeItems: "center", background: "var(--kc-page)" }}>
      <Spin />
    </main>
  );
}
