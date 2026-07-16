// UserMenu — dropdown в header справа когда залогинены.
// Аватарка (initials или generic icon) + display_name + dropdown {Профиль, Выйти}.

import { Avatar, Dropdown, Space, Typography, theme } from "antd";
import { LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@shared/contexts/AuthContext";

function initials(name?: string): string {
  if (!name) return "";
  const parts = name.trim().split(/\s+/);
  if (parts.length === 0) return "";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export function UserMenu() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const { token } = theme.useToken();

  if (!user) return null;

  const display = user.display_name || user.email || user.id;
  const ini = initials(user.display_name || user.email);

  const items = [
    {
      key: "profile",
      icon: <UserOutlined />,
      label: "Профиль",
      onClick: () => navigate("/iam/users"),
    },
    { type: "divider" as const },
    {
      key: "logout",
      icon: <LogoutOutlined />,
      label: "Выйти",
      onClick: async () => {
        logout();
        navigate("/");
      },
    },
  ];

  return (
    <Dropdown menu={{ items }} placement="bottomRight" trigger={["click"]}>
      <Space
        size={6}
        style={{
          cursor: "pointer",
          padding: "0 6px",
          borderRadius: 6,
          height: 28,
          display: "inline-flex",
          alignItems: "center",
        }}
      >
        <Avatar
          size={22}
          style={{
            background: token.colorPrimary,
            color: "#fff",
            fontSize: 11,
            fontWeight: 600,
          }}
        >
          {ini || <UserOutlined />}
        </Avatar>
        <Typography.Text
          style={{
            fontSize: 13,
            maxWidth: 160,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            color: token.colorText,
          }}
        >
          {display}
        </Typography.Text>
      </Space>
    </Dropdown>
  );
}
