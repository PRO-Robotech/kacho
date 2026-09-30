// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Button, Space, Spin, Typography } from "antd";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { CeremonyScreen } from "./CeremonyScreen";
import { useLogout } from "./use-logout";

// Страница «не удалось узнать» стража подтверждённости адреса (приёмка F6b, Р7):
// край не назвал подтверждённость либо не ответил о сессии по существу.
//
// Каркаса она не открывает — рубеж закрывающий, — но и «вы вышли» не говорит и
// на вход не уводит: «спросить не удалось» не есть «сессии нет». Действий два:
// спросить край снова и выйти. Текст — что именно не удалось узнать; причины
// консоль не выдумывает.

/** Текст страницы «не названо» (Р7): что именно не удалось узнать и почему. */
export const ADDRESS_STATE_NOT_NAMED_TEXT =
  "Не удалось узнать, подтверждён ли адрес: край не назвал это в ответе о сессии";

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

/** Край ещё не ответил о сессии: ни каркаса, ни экрана — только ожидание. */
export function SessionPending() {
  return (
    <main style={{ height: "100vh", display: "grid", placeItems: "center", background: "var(--kc-page)" }}>
      <Spin />
    </main>
  );
}
