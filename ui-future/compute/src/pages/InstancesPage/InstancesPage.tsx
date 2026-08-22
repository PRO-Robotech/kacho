import { useEffect, useMemo } from "react";
import type { FC, ReactNode } from "react";
import { Navigate, Route, Routes, useParams } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { ThemeProvider } from "@/lib/theme-context";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@/components/molecules/PageHeaderSlot";
import { GlobalResourceFormModal } from "@/components/organisms/GlobalResourceFormModal";
import { OperationBanner } from "@/components/molecules/OperationBanner";
import { Toaster } from "@/components/molecules/Toaster";
import { ResourceCreatePage } from "@/components/organisms/ResourceCreatePage";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { ResourceShell } from "@/components/organisms/ResourceShell";
// Доменные расширения карточки машины (строки Обзора, действия шапки, вкладки
// «Диски» / «Сетевые интерфейсы») регистрируются в общем реестре side-effect
// импортом — до рендера страниц. Общая оболочка карточки остаётся
// app-agnostic: доменное содержимое инжектится модулем, а не хардкодится в ней.
import "@/registerExtensions";
import { contextApi } from "@/lib/context-store";
import { REGISTRY } from "@/lib/resource-registry";
import "@/typography.css";
import "@shared/index.css";

export interface InstancesPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

// Compute-домен: Instance (виртуальная машина / контейнер-джоба) + MachineType
// (read-only cluster-scoped каталог sizing) через единый REGISTRY. Detail инстанса
// (start/stop/restart + attach-disk/attach-nic) подаётся доменными расширениями
// DETAIL_EXTENSIONS поверх generic ResourceShell.
const INSTANCES = REGISTRY["compute-instances"];
const MACHINE_TYPES = REGISTRY["machine-types"];
// Ключ входа в гостя — полноценный ресурс проекта (список / карточка / создание),
// а не поле формы машины: полем его нельзя ни отозвать, ни заменить.
const GUEST_ACCESS_KEYS = REGISTRY["guest-access-keys"];
const PLACEMENT_GROUPS = REGISTRY["placement-groups"];

/**
 * Таблица маршрутов раздела — ОТДЕЛЬНАЯ функция, а не вложенный узел страницы.
 *
 * Раздел, которого нет в таблице, не отличим от раздела, которого не задумывали:
 * сайдбар предлагает переход, роутер отвечает ловушкой «всё остальное», и
 * оператор возвращается на список машин без единого признака, что раздел
 * существует. Вынесенная функция даёт пробе НАСТОЯЩИЙ сопоставитель роутера
 * вместо поиска строки в исходнике.
 */
export function ComputeRoutes() {
  return (
    <Routes>
      <Route index element={<ProjectComputeDefaultRedirect />} />
      <Route
        path={INSTANCES.route}
        element={<ResourceListPage spec={INSTANCES} parentField="project_id" parentParam="projectId" panelForms />}
      />
      <Route
        path={`${INSTANCES.route}/create`}
        element={<ResourceCreatePage spec={INSTANCES} parentField="project_id" parentParam="projectId" />}
      />
      <Route path={`${INSTANCES.route}/:uid`} element={<ResourceShell spec={INSTANCES} />} />
      <Route path={`${INSTANCES.route}/:uid/edit`} element={<ResourceShell spec={INSTANCES} mode="edit" />} />
      <Route path={`${INSTANCES.route}/:uid/:tab`} element={<ResourceShell spec={INSTANCES} />} />
      {/* MachineType — read-only cluster-scoped каталог sizing (без create/edit). */}
      <Route path={MACHINE_TYPES.route} element={<ResourceListPage spec={MACHINE_TYPES} panelForms />} />
      <Route path={`${MACHINE_TYPES.route}/:uid`} element={<ResourceShell spec={MACHINE_TYPES} />} />
      <Route path={`${MACHINE_TYPES.route}/:uid/:tab`} element={<ResourceShell spec={MACHINE_TYPES} />} />
      {/* GuestAccessKey — ключ входа в гостя, полноценный ресурс проекта. */}
      <Route
        path={GUEST_ACCESS_KEYS.route}
        element={
          <ResourceListPage spec={GUEST_ACCESS_KEYS} parentField="project_id" parentParam="projectId" panelForms />
        }
      />
      <Route
        path={`${GUEST_ACCESS_KEYS.route}/create`}
        element={<ResourceCreatePage spec={GUEST_ACCESS_KEYS} parentField="project_id" parentParam="projectId" />}
      />
      <Route path={`${GUEST_ACCESS_KEYS.route}/:uid`} element={<ResourceShell spec={GUEST_ACCESS_KEYS} />} />
      <Route
        path={`${GUEST_ACCESS_KEYS.route}/:uid/edit`}
        element={<ResourceShell spec={GUEST_ACCESS_KEYS} mode="edit" />}
      />
      <Route path={`${GUEST_ACCESS_KEYS.route}/:uid/:tab`} element={<ResourceShell spec={GUEST_ACCESS_KEYS} />} />
      {/* PlacementGroup — правило взаимного размещения машин (CRUD + операции). */}
      <Route
        path={PLACEMENT_GROUPS.route}
        element={
          <ResourceListPage spec={PLACEMENT_GROUPS} parentField="project_id" parentParam="projectId" panelForms />
        }
      />
      <Route
        path={`${PLACEMENT_GROUPS.route}/create`}
        element={<ResourceCreatePage spec={PLACEMENT_GROUPS} parentField="project_id" parentParam="projectId" />}
      />
      <Route path={`${PLACEMENT_GROUPS.route}/:uid`} element={<ResourceShell spec={PLACEMENT_GROUPS} />} />
      <Route
        path={`${PLACEMENT_GROUPS.route}/:uid/edit`}
        element={<ResourceShell spec={PLACEMENT_GROUPS} mode="edit" />}
      />
      <Route path={`${PLACEMENT_GROUPS.route}/:uid/:tab`} element={<ResourceShell spec={PLACEMENT_GROUPS} />} />
      <Route path="*" element={<ProjectComputeDefaultRedirect />} />
    </Routes>
  );
}

export const InstancesPage: FC<InstancesPageProps> = ({ context }) => {
  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: 1,
            staleTime: 5_000,
            refetchOnWindowFocus: false,
          },
        },
      }),
    [],
  );

  useEffect(() => {
    if (context?.account) {
      contextApi.hydrate({ account: context.account });
    }
    if (context?.project) {
      contextApi.hydrate({ project: context.project });
    }
  }, [context]);

  return (
    <ThemeProvider>
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <PageHeaderSlotProvider>
            <ComputeFrame>
              <ComputeRoutes />
            </ComputeFrame>
          </PageHeaderSlotProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
};

function ComputeFrame({ children }: { children: ReactNode }) {
  return (
    <section className="vpc-remote-frame">
      <div className="vpc-host-header-slots">
        <div className="vpc-host-header-actions">
          <HeaderRightSlot />
        </div>
      </div>

      <OperationBanner />
      <div className="vpc-remote-content">{children}</div>
      <GlobalResourceFormModal />
      <Toaster />
    </section>
  );
}

function ProjectComputeDefaultRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/projects/${projectId}/compute/instances`} replace />;
}

export default InstancesPage;
