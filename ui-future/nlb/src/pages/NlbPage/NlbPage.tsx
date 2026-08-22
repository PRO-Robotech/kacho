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
import { contextApi } from "@/lib/context-store";
import { REGISTRY } from "@/lib/resource-registry";
// Доменные расширения карточек раздела кладутся в ОБЩИЙ реестр расширений при
// загрузке этого модуля: общая оболочка карточки спрашивает их по `spec.id` и
// о разделе ничего не знает. Импорт ради побочного действия — символов отсюда
// точке входа не нужно, а без него карточки NLB остались бы без доменных строк
// «Обзора» и без вкладки «Целевые группы».
import "@/lib/nlb-detail-extensions";
import "@/typography.css";
import "@shared/index.css";

export interface NlbPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

// NLB-домен: LoadBalancer / Listener / TargetGroup через единый REGISTRY.
const NLB_SCOPED = ["load-balancers", "listeners", "target-groups"].map((id) => REGISTRY[id]).filter(Boolean);

export const NlbPage: FC<NlbPageProps> = ({ context }) => {
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
            <NlbFrame>
              <Routes>
                <Route index element={<ProjectNlbDefaultRedirect />} />
                {NLB_SCOPED.map((spec) => {
                  // Карточка — ОДНА оболочка на все ресурсы раздела. Прежде у
                  // балансировщика стояла своя страница-обёртка ради одной
                  // вкладки; вкладку теперь подаёт расширение по `spec.id`, и
                  // разветвления по имени ресурса здесь не осталось.
                  return (
                    <Route key={spec.id}>
                      <Route
                        path={spec.route}
                        element={
                          <ResourceListPage spec={spec} parentField="project_id" parentParam="projectId" panelForms />
                        }
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
                  );
                })}
                <Route path="*" element={<ProjectNlbDefaultRedirect />} />
              </Routes>
            </NlbFrame>
          </PageHeaderSlotProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
};

function NlbFrame({ children }: { children: ReactNode }) {
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

function ProjectNlbDefaultRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/projects/${projectId}/nlb/load-balancers`} replace />;
}

export default NlbPage;
