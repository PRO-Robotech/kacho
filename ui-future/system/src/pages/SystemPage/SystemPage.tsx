// SystemPage — «System / Administration» + «Токены и ключи» области system-remote.
//
// SystemRoutes (named) — <Routes>-блок для admin-ресурсов (regions/zones/
// address-pools/cluster-admins). SystemPage (default) — self-contained federated
// expose (RemoteShell-обвязка + System- и Tokens-роуты), host монтирует его под
// /system/* (внутренние роуты /system/* и /system/tokens/*).
//
// Ресурсы (registry-driven, generic ResourceListPage/CreatePage/DetailPage/
// EditPage — location-relative, панель/страница-формы):
//   Regions       — /geo/v1/regions             (чтение RegionService; мутации InternalRegionService — только admin-плоскость)
//   Zones         — /geo/v1/zones               (чтение ZoneService; мутации InternalZoneService — только admin-плоскость)
//   AddressPools  — /vpc/v1/addressPools        (AddressPoolService — публичная с ADM-1 S1; CIDR через :addCidrBlocks/:removeCidrBlocks)
//   Cluster admins— /iam/v1/internal/cluster    (кастомная ClusterAdminsPage; InternalClusterService — только admin-плоскость)
// Все мутации async → Operation (poll /operations/{id}).
//
// Плюс /system/search — общая admin-страница поиска по id/имени (живёт в shared).
// Адрес держит рейл хоста («Поиск»), а хост маршрутизирует весь /system/* сюда;
// страница строит ссылки на /system/regions|zones|address-pools, то есть на
// маршруты ЭТОГО модуля. Достижимость её доменов через dev-прокси держит не
// память, а SystemPage.proxy-coverage.test.ts: он берёт таблицу целей у самой
// страницы поиска и правила — у загруженного vite.config.ts.

import { lazy, Suspense, useMemo, type ReactNode } from "react";
import { Navigate, Route, Routes } from "react-router";
import { Spin } from "antd";
import { REGISTRY, type ResourceSpec } from "@shared/lib/resource-registry";
import { adminPlaneMutable, useAdminPlanePosture } from "@shared/lib/admin-plane-posture";
import { AdminPlaneUnavailable } from "@shared/components/molecules/AdminPlaneUnavailable";
import { AdminLayout } from "@/components/organisms/AdminLayout";
import { ResourceListPage } from "@shared/components/organisms/ResourceListPage";
import { ResourceCreatePage } from "@shared/components/organisms/ResourceCreatePage";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceEditPage } from "@shared/components/organisms/ResourceEditPage";
import { AddressPoolDetailPage } from "@shared/pages/AddressPoolDetailPage";
import { SystemSearchPage } from "@shared/pages/SystemSearchPage";
import { RemoteShell } from "@/pages/RemoteShell";
import { TokensRoutes } from "@/pages/TokensPage";

const ClusterAdminsPage = lazy(() => import("@shared/pages/system/ClusterAdminsPage"));

const spin = (
  <div style={{ padding: 48, textAlign: "center" }}>
    <Spin size="large" />
  </div>
);

/**
 * Спеки, адреса которых этот модуль маршрутизирует.
 *
 * Один источник для маршрутов НИЖЕ и для проверки достижимости их доменов через
 * dev-прокси: выписанный рядом с проверкой перечень разошёлся бы с маршрутами
 * молча, и «все домены покрыты» стало бы утверждением о другом наборе.
 */
const regionsSpec = REGISTRY.regions;
const zonesSpec = REGISTRY.zones;
const addressPoolsSpec = REGISTRY["address-pools"];
export const ROUTED_SPECS = [regionsSpec, zonesSpec, addressPoolsSpec];

/**
 * Спека без мутаций: чтение остаётся, кнопок создания/правки/удаления нет.
 *
 * Снимаются они у регионов и зон — их мутации обслуживает только admin-плоскость
 * (`InternalRegionService`/`InternalZoneService`), — когда `adminPlaneMutable`
 * говорит «нет». Пулы адресов не снимаются никогда: с ADM-1 S1 их мутации
 * обслуживает публичная `AddressPoolService` на обоих слушателях края, и снятые
 * на внешней посадке кнопки отняли бы у администратора облака работающее
 * действие (#3091).
 */
function withoutMutations(spec: ResourceSpec): ResourceSpec {
  return { ...spec, ops: { create: false, update: false, delete: false } };
}

/**
 * Экран, чьи мутации живут только на admin-плоскости: на посадке без неё над ним
 * — слова о том, что раздел здесь недоступен. Над пулами адресов этих слов нет:
 * там они были бы ложью.
 */
function OnAdminPlane({ absent, children }: { absent: boolean; children: ReactNode }) {
  return (
    <>
      {absent && <AdminPlaneUnavailable />}
      {children}
    </>
  );
}

export function SystemRoutes() {
  const posture = useAdminPlanePosture();
  const mutable = adminPlaneMutable(posture);
  const absent = posture === "absent";
  const [regions, zones] = useMemo(
    () => (mutable ? [regionsSpec, zonesSpec] : [regionsSpec, zonesSpec].map(withoutMutations)),
    [mutable],
  );
  const plane = (node: ReactNode) => <OnAdminPlane absent={absent}>{node}</OnAdminPlane>;
  return (
    <Routes>
      <Route index element={<Navigate to="regions" replace />} />

      {/* List/cluster страницы — в общей оболочке раздела (вертикальный рейл
          пунктов, тот же, что на карточке ресурса). */}
      <Route element={<AdminLayout />}>
        <Route path="regions" element={plane(<ResourceListPage spec={regions} panelForms />)} />
        <Route path="zones" element={plane(<ResourceListPage spec={zones} panelForms />)} />
        <Route path="address-pools" element={<ResourceListPage spec={addressPoolsSpec} panelForms />} />
        <Route
          path="cluster/admins"
          element={plane(
            <Suspense fallback={spin}>
              <ClusterAdminsPage />
            </Suspense>,
          )}
        />
        {/* ЗДЕСЬ БЫЛ раздел администратора «Пределы» — назначение величин.
            Служба, которой он правил величины, выпилена из службы доступа
            целиком; производителя у этой поверхности не осталось ни одного,
            и страница отвечала бы отказом при любом входе. Чтение учёта
            арендатором — другой предмет, живёт у владельцев типов и
            остаётся (см. QuotasPage). */}
      </Route>

      {/* Create/Detail/Edit — страница-формы (без рейла раздела). */}
      <Route path="regions/create" element={plane(<ResourceCreatePage spec={regions} />)} />
      <Route path="regions/:uid" element={plane(<ResourceDetailPage spec={regions} />)} />
      <Route path="regions/:uid/edit" element={plane(<ResourceEditPage spec={regions} />)} />

      <Route path="zones/create" element={plane(<ResourceCreatePage spec={zones} />)} />
      <Route path="zones/:uid" element={plane(<ResourceDetailPage spec={zones} />)} />
      <Route path="zones/:uid/edit" element={plane(<ResourceEditPage spec={zones} />)} />

      {/* Поиск — адрес, который рекламирует рейл хоста; без этого маршрута
          «Поиск» молча уводил на список регионов через catch-all ниже. */}
      <Route path="search" element={<SystemSearchPage />} />

      <Route path="address-pools/create" element={<ResourceCreatePage spec={addressPoolsSpec} />} />
      <Route path="address-pools/:uid" element={<AddressPoolDetailPage />} />
      <Route path="address-pools/:uid/edit" element={<ResourceEditPage spec={addressPoolsSpec} />} />

      {/* Адрес назначения АБСОЛЮТНЫЙ. Относительный «regions» внутри splat-маршрута
          резолвится от УЖЕ СОПОСТАВЛЕННОГО пути (`/system/что-угодно`), давая
          `/system/что-угодно/regions`; он снова попадает сюда — и перенаправление
          зацикливается, наращивая адрес до бесконечности. Наблюдаемо это как
          «страница не открывается» без единого сообщения. */}
      <Route path="*" element={<Navigate to="/system/regions" replace />} />
    </Routes>
  );
}

export default function SystemPage() {
  return (
    <RemoteShell>
      <Routes>
        {/* Токены и ключи — под /system/tokens/*. */}
        <Route path="tokens/*" element={<TokensRoutes />} />
        {/* Всё остальное под /system/* — admin-ресурсы. */}
        <Route path="*" element={<SystemRoutes />} />
      </Routes>
    </RemoteShell>
  );
}
