// IamUsersListShell — тонкая обёртка над generic ResourceListPage для User.
//
// User создаётся не Create-формой, а приглашением: вместо CTA «Создать» — кнопка
// «Пригласить пользователя» (full-page InviteUserPage, POST /iam/v1/users:invite).
// Список глобальный (ListUsers без account_id). Invite-action ставится через
// useHeaderRight поверх generic-страницы: parent-эффект выполняется после
// child'ового (у users ops.create=false → generic не ставит свой CTA).
//
// РАСХОЖДЕНИЕ С КАНОНОМ, КОТОРОЕ ЗДЕСЬ НЕ ЗАКРЫВАЕТСЯ. По канону действие стоит
// ПОСЛЕДНИМ в ряду ручек списка, а не в слоте шапки приложения: там оно попадает
// в один ряд с элементами каркаса (выбор области, профиль) и читается как
// принадлежащее им. Слот — единственный канал, который generic-страница
// оставляет вызывающему: своего входа для добавочного действия у неё нет.
// Закрывается это в общем коде (проп добавочных действий у `ResourceListPage`),
// а не заведением здесь второй реализации списка.

import { useMemo } from "react";
import { useNavigate } from "react-router";
import { Button } from "antd";
import { UserAddOutlined } from "@ant-design/icons";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";

export function IamUsersListShell() {
  const navigate = useNavigate();
  const inviteAction = useMemo(
    () => (
      // Кнопка называет ДЕЙСТВИЕ: предмет назван заголовком страницы
      // («Пользователи»), и повторять его подписью незачем.
      <Button type="primary" icon={<UserAddOutlined />} onClick={() => navigate("/iam/users/invite")}>
        Пригласить
      </Button>
    ),
    [navigate],
  );
  useHeaderRight(inviteAction);
  return <ResourceListPage spec={REGISTRY.users} panelForms />;
}
