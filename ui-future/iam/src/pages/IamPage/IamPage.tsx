import { useEffect, useMemo, type FC, type ReactNode } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp } from "antd";
import { ThemeProvider } from "@shared/lib/theme-context";
import { AuthProvider } from "@shared/contexts/AuthContext";
import { StepUpModal } from "@/components/molecules/auth/StepUpModal";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { OperationBanner } from "@shared/components/molecules/OperationBanner";
import { Toaster } from "@/components/molecules/Toaster";
import { GlobalResourceFormModal } from "@shared/components/organisms/GlobalResourceFormModal";
import { ResourceCreatePage } from "@/components/organisms/ResourceCreatePage";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { ResourceShell } from "@shared/components/organisms/ResourceShell";
import { IamScopedListShell } from "@/components/organisms/iam/IamScopedListShell";
import { RolesListShell } from "@/components/organisms/iam/RolesListShell";
import { IamUsersListShell } from "@/components/organisms/iam/IamUsersListShell";
import { AccessBindingCreatePage, AccessBindingsPage } from "@/pages/iam/AccessBindingsPage";
import { AccessGrantPage, AccessPage } from "@/pages/iam/AccessPage";
import { GroupCreatePage } from "@/pages/iam/GroupsPage";
import { RoleCreatePage } from "@/pages/iam/RolesPage";
import { IamOperationsPage } from "@/pages/iam/IamOperationsPage";
import { InviteUserPage } from "@/pages/iam/UsersPage";
import { contextApi, useContext as useIamContext } from "@shared/lib/context-store";
import { REGISTRY } from "@shared/lib/resource-registry";
import "@shared/typography.css";
import "@shared/index.css";
// Регистрирует доменные IAM-расширения detail-страниц и inline-форм (side-effect,
// до рендера страниц) в app-agnostic shared-реестрах.
import "@/registerExtensions";

export interface IamPageProps {
  context?: {
    account: { id: string; name: string } | null;
    project: { id: string; name: string; accountId: string } | null;
  };
  navigate?: (path: string) => void | Promise<void>;
}

export const IamPage: FC<IamPageProps> = ({ context }) => {
  const isTest = process.env.NODE_ENV === "test";
  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: isTest ? false : 1,
            gcTime: isTest ? Infinity : 5 * 60 * 1000,
            staleTime: 5_000,
            refetchOnWindowFocus: false,
          },
        },
      }),
    [isTest],
  );

  useEffect(() => {
    if (context?.account) {
      contextApi.hydrate({ account: context.account });
    }
    if (context?.project) {
      contextApi.hydrate({ project: context.project });
    }
  }, [context]);

  useEffect(() => {
    return () => {
      queryClient.clear();
    };
  }, [queryClient]);

  return (
    <ThemeProvider>
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <StepUpModal />
            <PageHeaderSlotProvider>
              <IamFrame>
                <Routes>
                  <Route index element={<Navigate to="/iam/accounts" replace />} />
                  <Route path="accounts" element={<ResourceListPage spec={REGISTRY.accounts} />} />
                  <Route path="accounts/create" element={<ResourceCreatePage spec={REGISTRY.accounts} />} />
                  <Route path="accounts/:uid" element={<ResourceShell spec={REGISTRY.accounts} />} />
                  <Route path="accounts/:uid/edit" element={<ResourceShell spec={REGISTRY.accounts} mode="edit" />} />
                  <Route
                    path="accounts/:uid/:childRoute/create"
                    element={<ResourceShell spec={REGISTRY.accounts} mode="child-create" />}
                  />
                  <Route path="accounts/:uid/:tab" element={<ResourceShell spec={REGISTRY.accounts} />} />
                  <Route path="projects" element={<IamScopedListShell spec={REGISTRY.projects} disableChildRoute />} />
                  <Route
                    path="projects/create"
                    element={<IamScopedCreatePage spec={REGISTRY.projects} parentField="account_id" />}
                  />
                  <Route path="projects/:uid" element={<ResourceShell spec={REGISTRY.projects} />} />
                  <Route path="projects/:uid/edit" element={<ResourceShell spec={REGISTRY.projects} mode="edit" />} />
                  <Route path="projects/:uid/:tab" element={<ResourceShell spec={REGISTRY.projects} />} />
                  <Route path="service-accounts" element={<IamScopedListShell spec={REGISTRY["service-accounts"]} />} />
                  <Route
                    path="service-accounts/create"
                    element={<IamScopedCreatePage spec={REGISTRY["service-accounts"]} parentField="account_id" />}
                  />
                  <Route path="service-accounts/:uid" element={<ResourceShell spec={REGISTRY["service-accounts"]} />} />
                  <Route
                    path="service-accounts/:uid/edit"
                    element={<ResourceShell spec={REGISTRY["service-accounts"]} mode="edit" />}
                  />
                  <Route
                    path="service-accounts/:uid/:childRoute/create"
                    element={<ResourceShell spec={REGISTRY["service-accounts"]} mode="child-create" />}
                  />
                  <Route
                    path="service-accounts/:uid/:tab"
                    element={<ResourceShell spec={REGISTRY["service-accounts"]} />}
                  />
                  <Route path="users" element={<IamUsersListShell />} />
                  <Route path="users/invite" element={<InviteUserPage />} />
                  <Route path="users/:uid" element={<ResourceShell spec={REGISTRY.users} />} />
                  <Route
                    path="users/:uid/:childRoute/create"
                    element={<ResourceShell spec={REGISTRY.users} mode="child-create" />}
                  />
                  <Route path="users/:uid/:tab" element={<ResourceShell spec={REGISTRY.users} />} />
                  <Route path="groups" element={<IamScopedListShell spec={REGISTRY.groups} />} />
                  <Route path="groups/create" element={<GroupCreatePage />} />
                  <Route path="groups/:uid/edit" element={<ResourceShell spec={REGISTRY.groups} mode="edit" />} />
                  <Route path="groups/:uid" element={<ResourceShell spec={REGISTRY.groups} />} />
                  <Route
                    path="groups/:uid/:childRoute/create"
                    element={<ResourceShell spec={REGISTRY.groups} mode="child-create" />}
                  />
                  <Route path="groups/:uid/:tab" element={<ResourceShell spec={REGISTRY.groups} />} />
                  <Route path="roles" element={<RolesListShell />} />
                  <Route path="roles/create" element={<RoleCreatePage />} />
                  <Route path="roles/:uid/edit" element={<ResourceShell spec={REGISTRY.roles} mode="edit" />} />
                  <Route path="roles/:uid" element={<ResourceShell spec={REGISTRY.roles} />} />
                  <Route path="roles/:uid/:tab" element={<ResourceShell spec={REGISTRY.roles} />} />
                  <Route path="access-bindings" element={<AccessBindingsPage />} />
                  <Route path="access-bindings/create" element={<AccessBindingCreatePage />} />
                  <Route path="access-bindings/:uid" element={<ResourceShell spec={REGISTRY["access-bindings"]} />} />
                  <Route path="access-bindings/:uid/:tab" element={<ResourceShell spec={REGISTRY["access-bindings"]} />} />
                  <Route path="operations" element={<IamOperationsPage />} />
                  <Route path="access" element={<AccessPage />} />
                  <Route path="access/grant" element={<AccessGrantPage />} />
                  <Route path="*" element={<Navigate to="/iam/accounts" replace />} />
                </Routes>
              </IamFrame>
            </PageHeaderSlotProvider>
          </AuthProvider>
        </QueryClientProvider>
      </AntdApp>
    </ThemeProvider>
  );
};

function IamFrame({ children }: { children: ReactNode }) {
  return (
    <section className="vpc-remote-frame iam-remote-frame">
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

export default IamPage;

function IamScopedCreatePage({ spec, parentField }: { spec: (typeof REGISTRY)[string]; parentField: "account_id" }) {
  const account = useIamContext((s) => s.account);

  if (!account) {
    return <IamScopedListShell spec={spec} />;
  }

  return <ResourceCreatePage spec={spec} parentField={parentField} parentValue={account.id} />;
}
