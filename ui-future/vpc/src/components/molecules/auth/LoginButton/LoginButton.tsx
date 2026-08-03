// LoginButton — кнопка для старта входа.
// Рендерится в header справа когда `user === null`. Клик → `useAuth().login()`,
// то есть full-page redirect в self-service поток развёрнутого провайдера
// личности. Через край вход НЕ идёт: своей церемонии у него нет.
//
// Прежний комментарий описывал маршрут края и другого провайдера — ни того, ни
// другого этот код никогда не звал, и оба сняты.

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
