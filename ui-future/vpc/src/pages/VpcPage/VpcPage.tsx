import { useEffect, useMemo } from "react";
import type { FC, ReactNode } from "react";
import { Navigate, Route, Routes, useParams } from "react-router-dom";
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
import { SubnetCreatePage } from "@/pages/SubnetCreatePage";
import { contextApi } from "@shared/lib/context-store";
import { REGISTRY } from "@shared/lib/resource-registry";
import "@shared/typography.css";
import "@shared/index.css";

export interface VpcPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

const VPC_SCOPED = [
  "networks",
  "subnets",
  "addresses",
  "route-tables",
  "security-groups",
  "network-interfaces",
  "gateways",
]
  .map((id) => REGISTRY[id])
  .filter(Boolean);

export const VpcPage: FC<VpcPageProps> = ({ context }) => {
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
              <Routes>
                <Route index element={<ProjectVpcDefaultRedirect />} />
                {VPC_SCOPED.map((spec) => (
                  <Route key={spec.id}>
                    <Route
                      path={spec.route}
                      element={<ResourceListPage spec={spec} parentField="project_id" parentParam="projectId" />}
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
