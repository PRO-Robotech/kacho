// TokensLayout — обёртка над страницами выпуска credential'ов
// /system/tokens/{service-account-keys,user-tokens}. Горизонтальные табы
// переключения между SA-ключами и персональными токенами пользователей.

import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { Space, Tabs, Typography } from "antd";

const TABS = [
  { key: "/system/tokens/service-account-keys", label: "Ключи сервисных аккаунтов" },
  { key: "/system/tokens/user-tokens", label: "Токены пользователей" },
];

export function TokensLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const active = TABS.find((t) => location.pathname.startsWith(t.key))?.key ?? TABS[0].key;

  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <div>
        <Typography.Title level={3} className="t-page-title" style={{ margin: 0 }}>
          Токены и ключи
        </Typography.Title>
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          Выпуск и отзыв OAuth-креденшалов. Приватный ключ показывается один раз при выпуске.
        </Typography.Text>
      </div>

      <Tabs
        activeKey={active}
        onChange={(k) => navigate(k)}
        items={TABS.map((t) => ({ key: t.key, label: t.label }))}
        size="middle"
        style={{ marginBottom: 0 }}
        data-testid="tokens-tabs"
      />

      <Outlet />
    </Space>
  );
}

export default TokensLayout;
