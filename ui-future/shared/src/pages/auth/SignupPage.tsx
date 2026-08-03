// SignupPage — welcome / registration entry-point (KAC-109 DoD #1).
//
// Поведение: обе кнопки ведут в self-service поток развёрнутого провайдера
// личности — `authApi.register()` и `authApi.login()`, ровно те же вызовы, что
// делает AuthContext. Своей церемонии у края нет.
//
// Прежде страница уходила на маршрут края, который проводил церемонию у ДРУГОГО
// провайдера. Тот маршрут снят вместе с ним (его не включал ни один профиль, и
// отработать он не мог ни разу), поэтому ссылка отсюда стала бы потребителем без
// производителя — тем же дефектом, только со стороны консоли.

import { Button, Card, Space, Typography, Alert } from "antd";
import { LoginOutlined, UserAddOutlined } from "@ant-design/icons";
import { Link } from "react-router";
import { authApi } from "@shared/api/auth";

const { Title, Paragraph, Text } = Typography;

export function SignupPage() {
  const onSignup = () => {
    authApi.register();
  };
  const onLogin = () => {
    authApi.login();
  };
  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "#1c1d22",
        padding: 24,
      }}
    >
      <Card
        style={{
          width: 480,
          background: "#26272d",
          border: "1px solid #383941",
        }}
      >
        <Space direction="vertical" size="large" style={{ width: "100%" }}>
          <div style={{ textAlign: "center" }}>
            <Title level={2} style={{ marginBottom: 4 }}>
              Kachō Console
            </Title>
            <Text type="secondary">облачная платформа управления</Text>
          </div>

          <Alert
            type="info"
            showIcon
            message="Регистрация"
            description={
              <>
                При первом входе автоматически создаётся <b>Account</b> (ваш tenant) и стартовый <b>Project</b>. Вы
                становитесь <code>owner</code> и можете приглашать пользователей / создавать service-accounts / выдавать
                роли.
              </>
            }
          />

          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Button type="primary" size="large" block icon={<UserAddOutlined />} onClick={onSignup}>
              Зарегистрироваться
            </Button>
            <Button size="large" block icon={<LoginOutlined />} onClick={onLogin}>
              Войти
            </Button>
          </Space>

          <Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }}>
            После регистрации вы попадёте в Console.{" "}
            <Link to="/">Уже работает в анонимном режиме (E0)? Открыть консоль →</Link>
          </Paragraph>
        </Space>
      </Card>
    </div>
  );
}
