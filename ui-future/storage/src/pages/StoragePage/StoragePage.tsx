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
import { REGISTRY, type ResourceSpec } from "@/lib/resource-registry";
import "@/typography.css";
import "@/index.css";

export interface StoragePageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

// Storage-домен: Volume / Snapshot (project-scoped CRUD) + DiskType (read-only
// cluster-scoped справочник) через единый REGISTRY.
const CRUD_SPECS: ResourceSpec[] = ["volumes", "snapshots"].map((id) => REGISTRY[id]).filter(Boolean);
const DISK_TYPES = REGISTRY["disk-types"];

export const StoragePage: FC<StoragePageProps> = ({ context }) => {
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
            <StorageFrame>
              <Routes>
                <Route index element={<ProjectStorageDefaultRedirect />} />
                {CRUD_SPECS.map((spec) => (
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
                    <Route path={`${spec.route}/:uid/:tab`} element={<ResourceShell spec={spec} />} />
                  </Route>
                ))}
                {/* DiskType — read-only cluster-scoped справочник (без create/edit). */}
                <Route path={DISK_TYPES.route} element={<ResourceListPage spec={DISK_TYPES} />} />
                <Route path={`${DISK_TYPES.route}/:uid`} element={<ResourceShell spec={DISK_TYPES} />} />
                <Route path={`${DISK_TYPES.route}/:uid/:tab`} element={<ResourceShell spec={DISK_TYPES} />} />
                <Route path="*" element={<ProjectStorageDefaultRedirect />} />
              </Routes>
            </StorageFrame>
          </PageHeaderSlotProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
};

function StorageFrame({ children }: { children: ReactNode }) {
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

function ProjectStorageDefaultRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/projects/${projectId}/storage/volumes`} replace />;
}

export default StoragePage;
