// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Link } from "react-router";
import { Button, Typography } from "antd";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { CeremonyScreen } from "./CeremonyScreen";
import { useLogout } from "./use-logout";

// Экран `/logout`: выход ПО НАЖАТИЮ, а не по открытию адреса.
//
// Выход по открытию сделал бы адрес ловушкой: ссылка на него с чужой страницы
// выводила бы человека из консоли без его ведома. Поэтому экран спрашивает, и
// только нажатие зовёт глагол выхода с признаком своего вида.

export function LogoutPage({ leave }: { leave?: (to: string) => void }) {
  const { logout, busy, refusal } = useLogout(leave);
  return (
    <CeremonyScreen title="Выход из консоли" footer={<Link to="/">Остаться в консоли</Link>}>
      <Typography.Paragraph>Сессия этого браузера будет завершена службой.</Typography.Paragraph>
      {refusal && (
        <div style={{ marginBottom: 16 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      <Button type="primary" block loading={busy} onClick={() => void logout()}>
        Выйти
      </Button>
    </CeremonyScreen>
  );
}

export default LogoutPage;
