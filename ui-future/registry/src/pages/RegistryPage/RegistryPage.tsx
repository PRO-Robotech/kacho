import { useEffect, useMemo } from "react";
import type { FC, ReactNode } from "react";
import { Navigate, Route, Routes, useParams } from "react-router-dom";
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
import { contextApi } from "@/lib/context-store";
import { REGISTRY } from "@/lib/resource-registry";
import "@/typography.css";
import "@/index.css";

export interface RegistryPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

// Registry-домен: Registry / Repository / Tag через единый REGISTRY.
const REGISTRY_SCOPED = ["registries", "repositories", "tags"].map((id) => REGISTRY[id]).filter(Boolean);

export const RegistryPage: FC<RegistryPageProps> = ({ context }) => {
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
            <RegistryFrame>
              <Routes>
                <Route index element={<ProjectRegistryDefaultRedirect />} />
                {REGISTRY_SCOPED.map((spec) => (
                  <Route key={spec.id}>
                    <Route
                      path={spec.route}
                      element={<ResourceListPage spec={spec} parentField="project_id" parentParam="projectId" />}
                    />
                    <Route
                      path={`${spec.route}/create`}
                      element={<ResourceCreatePage spec={spec} parentField="project_id" parentParam="projectId" />}
                    />
                    <Route path={`${spec.route}/:uid`} element={<ResourceShell spec={spec} />} />
                    <Route path={`${spec.route}/:uid/edit`} element={<ResourceShell spec={spec} mode="edit" />} />
                    <Route
                      path={`${spec.route}/:uid/:childRoute/create`}
                      element={<ResourceShell spec={spec} mode="child-create" />}
                    />
                    <Route path={`${spec.route}/:uid/:tab`} element={<ResourceShell spec={spec} />} />
                  </Route>
                ))}
                {/* Теги репозитория рендерятся встроенной боковой панелью
                    (RepositoryTagsPanel) по клику в списке репозиториев — панель
                    раздвигает таблицу внутри лайаута, без перехода на route. */}
                <Route path="*" element={<ProjectRegistryDefaultRedirect />} />
              </Routes>
            </RegistryFrame>
          </PageHeaderSlotProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
};

function RegistryFrame({ children }: { children: ReactNode }) {
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

function ProjectRegistryDefaultRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/projects/${projectId}/registry/registries`} replace />;
}

export default RegistryPage;
