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

export interface InstancesPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

// Compute-домен: Instance (виртуальная машина) через единый REGISTRY. Detail
// инстанса (start/stop/restart + attach-disk/attach-nic) подаётся доменными
// расширениями DETAIL_EXTENSIONS поверх generic ResourceShell.
const INSTANCES = REGISTRY["compute-instances"];

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
              <Routes>
                <Route index element={<ProjectComputeDefaultRedirect />} />
                <Route
                  path={INSTANCES.route}
                  element={<ResourceListPage spec={INSTANCES} parentField="project_id" parentParam="projectId" />}
                />
                <Route
                  path={`${INSTANCES.route}/create`}
                  element={<ResourceCreatePage spec={INSTANCES} parentField="project_id" parentParam="projectId" />}
                />
                <Route path={`${INSTANCES.route}/:uid`} element={<ResourceShell spec={INSTANCES} />} />
                <Route path={`${INSTANCES.route}/:uid/edit`} element={<ResourceShell spec={INSTANCES} mode="edit" />} />
                <Route path={`${INSTANCES.route}/:uid/:tab`} element={<ResourceShell spec={INSTANCES} />} />
                <Route path="*" element={<ProjectComputeDefaultRedirect />} />
              </Routes>
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
