// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Link, useLocation } from "react-router";
import { Typography } from "antd";
import { CeremonyScreen } from "./CeremonyScreen";

// Адрес церемонии, которого консоль на этой стадии не ведёт (приёмка F8, Р3).
//
// Отвечает НАЗВАННОЙ страницей, а не переводом на панель: замыкающее правило
// маршрутизатора делает отказ похожим на успех — «не открывается» и всё. Путь
// наружу один — ко входу: восстановления доступа и подтверждения адреса на
// посадке нет, и страница их не обещает.

export function CeremonyAddressNotServedPage() {
  const { pathname } = useLocation();
  return (
    <CeremonyScreen title="Такого адреса здесь нет" footer={<Link to="/login">Перейти ко входу</Link>}>
      <Typography.Paragraph>
        Консоль не ведёт адрес <Typography.Text code>{pathname}</Typography.Text>.
      </Typography.Paragraph>
    </CeremonyScreen>
  );
}

export default CeremonyAddressNotServedPage;
