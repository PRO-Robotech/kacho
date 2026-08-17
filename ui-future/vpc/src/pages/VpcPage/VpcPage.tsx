import { useEffect, useMemo } from "react";
import type { FC, ReactNode } from "react";
import { Navigate, Route, Routes, useParams } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { ThemeProvider } from "@shared/lib/theme-context";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { GlobalResourceFormModal } from "@shared/components/organisms/GlobalResourceFormModal";
import { OperationBanner } from "@shared/components/molecules/OperationBanner";
import { Toaster } from "@/components/molecules/Toaster";
import { ResourceCreatePage } from "@/components/organisms/ResourceCreatePage";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { ResourceShell } from "@shared/components/organisms/ResourceShell";
import { NetworkInterfaceCreatePage } from "@/pages/NetworkInterfaceCreatePage";
import { OperationsPage } from "@/pages/OperationsPage";
import { QuotasPage } from "@/pages/QuotasPage";
import { SubnetCreatePage } from "@/pages/SubnetCreatePage";
import { contextApi } from "@shared/lib/context-store";
import { REGISTRY } from "@shared/lib/resource-registry";
import { VPC_SCOPED_IDS } from "@/lib/scoped-resources";
import "@shared/typography.css";
import "@shared/index.css";

export interface VpcPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
  /**
   * Поверхность, названная оболочкой. Пусто — обычный раздел vpc со своим
   * деревом маршрутов; `"quotas"` — витрина пределов проекта.
   *
   * Витрина стоит НЕ под сегментом сервиса (`/projects/:id/quotas`), поэтому
   * маршрутам этого модуля она не видна: они потомки точки монтирования vpc и
   * получают лишь остаток пути после неё, а у этого адреса остаток пуст —
   * совпал бы корневой маршрут и увёл на список сетей. Разбирать полный адрес
   * здесь вторым правилом нельзя: оболочка уже знает свой маршрут, и второе
   * место об одном предмете разошлось бы с первым молча.
   */
  surface?: string;
}

// Перечень берётся из `lib/scoped-resources`, а не выписывается здесь: id,
// исчезнувший из общего реестра, `filter(Boolean)` выбрасывает МОЛЧА — раздел
// просто не появляется. Список, лежащий отдельным модулем, становится предметом
// проверки (`lib/scoped-resources.test.ts`), и исчезновение спеки перестаёт
// быть беззвучным.
const VPC_SCOPED = VPC_SCOPED_IDS.map((id) => REGISTRY[id]).filter(Boolean);

export const VpcPage: FC<VpcPageProps> = ({ context, surface }) => {
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
            <VpcFrame>
              {surface === "quotas" ? (
                <QuotasPage />
              ) : (
              <Routes>
                <Route index element={<ProjectVpcDefaultRedirect />} />
                {VPC_SCOPED.map((spec) => (
                  <Route key={spec.id}>
                    <Route
                      path={spec.route}
                      // VPC-раздел регистрирует `${route}/create` (ниже) и панель правки.
                      element={
                        <ResourceListPage spec={spec} parentField="project_id" parentParam="projectId" panelForms />
                      }
                    />
                    <Route path={`${spec.route}/create`} element={createElementFor(spec)} />
                    <Route path={`${spec.route}/:uid`} element={<ResourceShell spec={spec} />} />
                    <Route path={`${spec.route}/:uid/edit`} element={<ResourceShell spec={spec} mode="edit" />} />
                    <Route
                      path={`${spec.route}/:uid/:childRoute/create`}
                      element={<ResourceShell spec={spec} mode="child-create" />}
                    />
                    <Route path={`${spec.route}/:uid/:tab`} element={<ResourceShell spec={spec} />} />
                  </Route>
                ))}
                <Route path="operations" element={<OperationsPage />} />
                <Route path="*" element={<ProjectVpcDefaultRedirect />} />
              </Routes>
              )}
            </VpcFrame>
          </PageHeaderSlotProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
};

function VpcFrame({ children }: { children: ReactNode }) {
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

function createElementFor(spec: (typeof VPC_SCOPED)[number]): ReactNode {
  if (spec.id === "subnets") {
    return <SubnetCreatePage />;
  }
  if (spec.id === "network-interfaces") {
    return <NetworkInterfaceCreatePage />;
  }
  return <ResourceCreatePage spec={spec} parentField="project_id" parentParam="projectId" />;
}

function ProjectVpcDefaultRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/projects/${projectId}/vpc/networks`} replace />;
}

export default VpcPage;
