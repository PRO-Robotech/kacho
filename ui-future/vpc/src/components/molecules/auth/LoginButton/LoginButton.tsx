// LoginButton — кнопка для старта OIDC-flow.
// Рендерится в header справа когда `user === null`. Клик → full-page redirect
// на `/iam/v1/auth/login` (api-gateway генерирует state/PKCE и редиректит на
// Zitadel /oauth/v2/authorize).

import { Button } from "antd";
import { LoginOutlined } from "@ant-design/icons";
import { useAuth } from "@shared/contexts/AuthContext";

export function LoginButton() {
  const { login, loading } = useAuth();
  return (
    <Button type="primary" size="small" icon={<LoginOutlined />} onClick={() => login()} loading={loading}>
      Войти
    </Button>
  );
}
