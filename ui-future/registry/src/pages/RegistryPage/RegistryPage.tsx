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
// Доменные расширения карточки — side-effect-импорт входной точки модуля: он
// подключает их до первого рендера `ResourceShell`, которая читает их по
// идентификатору спеки. Оболочка при этом остаётся app-agnostic.
import "@/registerExtensions";
// Типографика — ОДИН лист на всё дерево (`@shared/typography.css`). Здесь
// лежала его байт-в-байт копия: у форка листа стилей нет ни гейта, ни пробы —
// перепись форков читает только `.ts`/`.tsx`, — поэтому разойтись он мог молча
// и незаметно. Так же берут его vpc, iam и раздел администрирования.
import "@shared/typography.css";
import "@shared/index.css";
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
                ))}
                {/* Карточка репозитория живёт ПОД СВОИМ РЕЕСТРОМ (#627).

                    У репозитория нет собственного идентификатора: его натуральный
                    ключ — пара «реестр + имя», и читается он адресом
                    `/registry/v1/registries/{registryId}/repositories/{name}`.
                    Плоского маршрута ему поэтому не хватает: он не называет
                    реестр, а без реестра адрес не собрать. Его дочерняя вкладка
                    «Теги» требует ОБА сегмента, поэтому не оживала ни при каком
                    входе — вкладка была объявлена, покрыта типами и неисполнима.

                    Имя параметра совпадает с именем подстановки в адресе
                    (`:registryId` → `{registryId}`) — это и есть всё правило
                    связи; таблицы соответствий нет намеренно, она была бы вторым
                    местом об одном предмете.

                    Плоский `repositories/:uid` выше остаётся: он адресует список
                    репозиториев проекта. Карточку он открыть не может и раньше
                    не мог — здесь она получает свой адрес, а не второй. */}
                <Route
                  path="registries/:registryId/repositories/:uid"
                  element={<ResourceShell spec={REGISTRY["repositories"]} />}
                />
                <Route
                  path="registries/:registryId/repositories/:uid/:tab"
                  element={<ResourceShell spec={REGISTRY["repositories"]} />}
                />
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
