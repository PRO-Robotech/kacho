// AdminLayout — оболочка раздела «Администрирование» (`/system/{regions,zones,
// address-pools,cluster/admins}`).
//
// Вид берётся ОБЩИЙ — `DetailShell`, тот же, что на карточке ресурса: вертикальный
// рейл слева, содержимое справа. Прежде здесь стоял свой горизонтальный ряд
// вкладок, и раздел выглядел другим местом продукта (правило 8 `ui.md`, решение
// владельца #447).
//
// Применяется только для списочных страниц раздела. Карточка / создание / правка
// ресурса идут своими страницами, как и у остальных ресурсов.
//
// Форма-модалка здесь НЕ монтируется: её монтирует фрейм раздела (`SystemPage`),
// а регионы/зоны/пулы используют панельные формы.

import { useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "react-router";
import { DetailShell, type DetailTab } from "@shared/components/organisms/DetailShell";
import { usePermissions } from "@shared/lib/permissions";

interface AdminSection {
  /** Идентификатор вкладки и он же — адрес её страницы. */
  path: string;
  label: string;
  /** Предикат над снимком прав — true → пункт виден; без него виден всегда. */
  visible?: (p: ReturnType<typeof usePermissions>) => boolean;
}

const SECTIONS: AdminSection[] = [
  { path: "/system/regions", label: "Регионы" },
  { path: "/system/zones", label: "Зоны" },
  {
    path: "/system/address-pools",
    label: "Пулы адресов",
    // Пул адресов — administrative-only ресурс.
    visible: (p) => p.isSystemAdmin,
  },
  {
    path: "/system/cluster/admins",
    label: "Администраторы кластера",
    visible: (p) => p.isSystemAdmin,
  },
];

export function AdminLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const perms = usePermissions();

  const visible = useMemo(() => SECTIONS.filter((s) => !s.visible || s.visible(perms)), [perms]);

  const active = visible.find((s) => location.pathname.startsWith(s.path))?.path ?? visible[0]?.path ?? SECTIONS[0].path;

  // Содержимое вкладки рисует маршрут, а не сама вкладка: страницы раздела
  // остаются самостоятельными адресами (ссылку на пункт можно дать и открыть).
  const tabs: DetailTab[] = useMemo(
    () => visible.map((s) => ({ id: s.path, label: s.label, render: () => <Outlet />, fill: true })),
    [visible],
  );

  return (
    <DetailShell
      resourceLabel="Администрирование"
      resourceName="Администрирование"
      tabs={tabs}
      activeTabId={active}
      onTabSelect={(id) => void navigate(id)}
    />
  );
}

export default AdminLayout;
