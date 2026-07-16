// IamUsersListShell — тонкая обёртка над generic ResourceListPage для User.
//
// User создаётся не Create-формой, а приглашением: вместо CTA «Создать» — кнопка
// «Пригласить пользователя» (full-page InviteUserPage, POST /iam/v1/users:invite).
// Список глобальный (ListUsers без account_id). Invite-action ставится через
// useHeaderRight поверх generic-страницы: parent-эффект выполняется после
// child'ового (у users ops.create=false → generic не ставит свой CTA).

import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "antd";
import { UserAddOutlined } from "@ant-design/icons";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";

export function IamUsersListShell() {
  const navigate = useNavigate();
  const inviteAction = useMemo(
    () => (
      <Button type="primary" icon={<UserAddOutlined />} onClick={() => navigate("/iam/users/invite")}>
        Пригласить пользователя
      </Button>
    ),
    [navigate],
  );
  useHeaderRight(inviteAction);
  return <ResourceListPage spec={REGISTRY.users} />;
}
