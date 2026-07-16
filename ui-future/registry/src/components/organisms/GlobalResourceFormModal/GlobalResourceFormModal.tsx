// GlobalResourceFormModal — глобальный mount-point для Create/Edit модалок.
// Mountится один раз в Layout, читает URL (любого вида:
// /projects/.../vpc/..., /projects/.../compute/..., /iam/..., /system/...)
// и определяет «активный контейнер» (project / iam / system) — пробрасывает
// его как `containerId` в ResourceFormModal.
//
// Это позволяет любой странице ставить `?modal=<spec.id>-create` и не
// заботиться о mount'е — модалка работает автоматически.

import { useLocation } from "react-router-dom";
import { ResourceFormModal } from "@/components/organisms/ResourceFormModal";

export function GlobalResourceFormModal() {
  const location = useLocation();

  // Парсим из pathname id активного контейнера.
  // /projects/<id>/...   → projectId
  // /iam/...             → "iam"     (global IAM resources, не требуют id).
  // /system/...          → "system"  (admin cluster-scoped ресурсы).
  const containerId = (() => {
    const project = location.pathname.match(/^\/projects\/([^/]+)/);
    if (project) return project[1];
    if (location.pathname.startsWith("/iam")) return "iam";
    if (location.pathname.startsWith("/system/")) return "system";
    return null;
  })();

  if (!containerId) return null;
  return <ResourceFormModal projectId={containerId} />;
}
