// AdminLayout — обёртка над admin-страницами /system/{regions,zones,address-pools}.
// Рендерит горизонтальные табы навигации между admin-ресурсами в одном месте,
// чтобы пользователь видел все доступные admin-сущности и мог создавать любую.
//
// Применяется только для list-страниц через App.tsx. Detail/Create/Edit
// используют ResourceDetailPage/CreatePage/EditPage как обычно.
//
// KAC item #2b: /iam/v1/internal/cluster/* теперь требует system_admin
// (раньше был exempt — некоторые ручки cluster_viewer). UI скрывает таб
// "Cluster admins" от не-админов; AddressPool admin тоже admin-only через FGA.

import { useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { Space, Tabs, Typography } from "antd";
import { GlobalResourceFormModal } from "@shared/components/organisms/GlobalResourceFormModal";
import { usePermissions } from "@shared/lib/permissions";

interface AdminTab {
  key: string;
  label: string;
  /** Predicate над PermissionSnapshot — true → таб виден; default — всегда. */
  visible?: (p: ReturnType<typeof usePermissions>) => boolean;
}

const TABS: AdminTab[] = [
  { key: "/system/regions", label: "Регионы" },
  { key: "/system/zones", label: "Зоны" },
  {
    key: "/system/address-pools",
    label: "Пулы адресов",
    // AddressPool — admin-only (FGA admin@cluster:cluster_kacho_root).
    // Скрываем для не-админов (KAC item #2b).
    visible: (p) => p.isSystemAdmin,
  },
  {
    key: "/system/cluster/admins",
    label: "Cluster admins",
    // KAC-196 + KAC item #2b: /iam/v1/internal/cluster/* теперь требует
    // system_admin (раньше был exempt).
    visible: (p) => p.isSystemAdmin,
  },
];

export function AdminLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const perms = usePermissions();

  const visibleTabs = useMemo(() => TABS.filter((t) => !t.visible || t.visible(perms)), [perms]);

  const active =
    visibleTabs.find((t) => location.pathname.startsWith(t.key))?.key ?? visibleTabs[0]?.key ?? TABS[0].key;

  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <div>
        <Typography.Title level={3} className="t-page-title" style={{ margin: 0 }}>
          Администрирование
        </Typography.Title>
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          Глобальные ресурсы инфраструктуры. Доступны только администраторам.
        </Typography.Text>
      </div>

      <Tabs
        activeKey={active}
        onChange={(k) => navigate(k)}
        items={visibleTabs.map((t) => ({ key: t.key, label: t.label }))}
        size="middle"
        style={{ marginBottom: 0 }}
        data-testid="admin-tabs"
      />

      <Outlet />
      {/* Глобальный mount модалок Create/Edit для admin-страниц (regions /
          zones / address-pools). Не project/account-scoped — используем
          "system" как containerId-placeholder; ResourceFormModal не требует
          конкретного projectId для cluster-scoped ресурсов. */}
      <GlobalResourceFormModal />
    </Space>
  );
}
