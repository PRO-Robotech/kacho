// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { Link, useLocation } from "react-router";
import { Typography } from "antd";
import { CeremonyScreen } from "./CeremonyScreen";
import { loginAddress } from "./ceremony-addresses";

// Адрес церемонии, которого консоль на этой стадии не ведёт (приёмка F8, Р3).
//
// Отвечает НАЗВАННОЙ страницей, а не переводом на панель: замыкающее правило
// маршрутизатора делает отказ похожим на успех — «не открывается» и всё. Путь
// наружу один — ко входу: восстановления доступа на посадке нет, и страница его
// не обещает. Страница стоит за стражем подтверждённости адреса (приёмка F6b,
// Р7): неподтверждённую сессию она не видит — та уходит на экран подтверждения.

export function CeremonyAddressNotServedPage() {
  const { pathname } = useLocation();
  return (
    <CeremonyScreen title="Такого адреса здесь нет" footer={<Link to={loginAddress()}>Перейти ко входу</Link>}>
      <Typography.Paragraph>
        Консоль не ведёт адрес <Typography.Text code>{pathname}</Typography.Text>.
      </Typography.Paragraph>
    </CeremonyScreen>
  );
}

export default CeremonyAddressNotServedPage;
