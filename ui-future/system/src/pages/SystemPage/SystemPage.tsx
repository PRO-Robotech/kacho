// SystemPage — «System / Administration» + «Токены и ключи» области system-remote.
//
// SystemRoutes (named) — <Routes>-блок для admin-ресурсов (regions/zones/
// address-pools/cluster-admins). SystemPage (default) — self-contained federated
// expose (RemoteShell-обвязка + System- и Tokens-роуты), host монтирует его под
// /system/* (внутренние роуты /system/* и /system/tokens/*).
//
// Ресурсы (registry-driven, generic ResourceListPage/CreatePage/DetailPage/
// EditPage — location-relative, панель/страница-формы):
//   Regions       — /geo/v1/regions             (geo.v1.RegionService + InternalRegionService)
//   Zones         — /geo/v1/zones               (geo.v1.ZoneService + InternalZoneService)
//   AddressPools  — /vpc/v1/addressPools        (kacho-vpc admin; CIDR через :addCidrBlocks/:removeCidrBlocks)
//   Cluster admins— /iam/v1/internal/cluster    (кастомная ClusterAdminsPage)
// Все мутации async → Operation (poll /operations/{id}).
//
// Плюс /system/search — общая admin-страница поиска по id/имени (живёт в shared).
// Адрес держит рейл хоста («Поиск»), а хост маршрутизирует весь /system/* сюда;
// страница строит ссылки на /system/regions|zones|address-pools, то есть на
// маршруты ЭТОГО модуля. Достижимость её доменов через dev-прокси держит не
// память, а SystemPage.proxy-coverage.test.ts: он берёт таблицу целей у самой
// страницы поиска и правила — у загруженного vite.config.ts.

import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router";
import { Spin } from "antd";
import { REGISTRY } from "@shared/lib/resource-registry";
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
const LimitsPage = lazy(() => import("@shared/pages/system/LimitsPage"));

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

export function SystemRoutes() {
  return (
    <Routes>
      <Route index element={<Navigate to="regions" replace />} />

      {/* List/cluster страницы — в AdminLayout (горизонтальные табы). */}
      <Route element={<AdminLayout />}>
        <Route path="regions" element={<ResourceListPage spec={regionsSpec} panelForms />} />
        <Route path="zones" element={<ResourceListPage spec={zonesSpec} panelForms />} />
        <Route path="address-pools" element={<ResourceListPage spec={addressPoolsSpec} panelForms />} />
        <Route
          path="cluster/admins"
          element={
            <Suspense fallback={spin}>
              <ClusterAdminsPage />
            </Suspense>
          }
        />
        <Route
          path="limits"
          element={
            <Suspense fallback={spin}>
              <LimitsPage />
            </Suspense>
          }
        />
      </Route>

      {/* Create/Detail/Edit — страница-формы (без AdminLayout-табов). */}
      <Route path="regions/create" element={<ResourceCreatePage spec={regionsSpec} />} />
      <Route path="regions/:uid" element={<ResourceDetailPage spec={regionsSpec} />} />
      <Route path="regions/:uid/edit" element={<ResourceEditPage spec={regionsSpec} />} />

      <Route path="zones/create" element={<ResourceCreatePage spec={zonesSpec} />} />
      <Route path="zones/:uid" element={<ResourceDetailPage spec={zonesSpec} />} />
      <Route path="zones/:uid/edit" element={<ResourceEditPage spec={zonesSpec} />} />

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
