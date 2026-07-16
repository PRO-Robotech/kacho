// HeaderAuth — switch: LoginButton, если не залогинен, иначе UserMenu.
// Loading state — skeleton-spacer (фиксированная ширина чтобы header не дёргался).

import { Spin } from "antd";
import { useAuth } from "@shared/contexts/AuthContext";
import { LoginButton } from "../LoginButton";
import { UserMenu } from "../UserMenu";

export function HeaderAuth() {
  const { user, loading } = useAuth();

  if (loading) {
    return (
      <div
        style={{
          width: 80,
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Spin size="small" />
      </div>
    );
  }

  return user ? <UserMenu /> : <LoginButton />;
}
