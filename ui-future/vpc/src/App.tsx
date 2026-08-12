import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Route, Routes, useParams } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfigProvider, App as AntdApp } from "antd";
import ruRU from "antd/locale/ru_RU";
import { ThemeProvider, useThemeMode } from "@shared/lib/theme-context";
import { buildTheme } from "@shared/lib/theme";
import { Layout } from "@/components/organisms/Layout";
import { RequireAuth } from "@/components/molecules/auth/RequireAuth";
import { AdminLayout } from "@/components/organisms/AdminLayout";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { ResourceCreatePage } from "@/components/organisms/ResourceCreatePage";
import { ResourceEditPage } from "@/components/organisms/ResourceEditPage";
import { Toaster } from "@/components/molecules/Toaster";
import { REGISTRY } from "@shared/lib/resource-registry";
import { COMPUTE_SCOPED_IDS, NLB_SCOPED_IDS, VPC_SCOPED_IDS } from "@/lib/scoped-resources";
import { AddressPoolDetailPage } from "@/pages/AddressPoolDetailPage";
import { ResourceShell } from "@shared/components/organisms/ResourceShell";
// KAC-231: SubnetDetailPage/SecurityGroupDetailPage/RouteTableDetailPage/
// AddressDetailPage/NetworkInterfaceDetailPage/VpcDetailShell заменены единым
// ResourceShell + DETAIL_EXTENSIONS. SubnetCreatePage сохранён (create со списка).
import { SubnetCreatePage } from "@/pages/SubnetCreatePage";
import { NetworkInterfaceCreatePage } from "@/pages/NetworkInterfaceCreatePage";
import { VpcListShell } from "@/components/organisms/VpcShell";
import { InstanceDetailPage } from "@/pages/InstanceDetailPage";
import { TargetGroupDetailPage } from "@/pages/TargetGroupDetailPage";
import { OperationsPage } from "@/pages/OperationsPage";
import { SystemSearchPage } from "@/pages/SystemSearchPage";
import { DashboardPage } from "@/pages/DashboardPage";
import { AuthCallback } from "@/pages/auth/AuthCallback";
import { SignupPage } from "@/pages/auth/SignupPage";
import { LogoutPage } from "@/pages/auth/Logout";
import { LoginPage } from "@/pages/auth/Login";
import { RegisterPage } from "@/pages/auth/Register";
import { RecoveryPage } from "@/pages/auth/Recovery";
import { SettingsPage } from "@/pages/auth/Settings";
import { StepUpModal } from "@/components/molecules/auth/StepUpModal";
import { AuthProvider } from "@shared/contexts/AuthContext";

// KAC-196: cluster admins UI — лениво подгружаемая страница.
const ClusterAdminsPage = lazy(() => import("@/pages/system/ClusterAdminsPage"));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 5_000,
      refetchOnWindowFocus: false,
    },
  },
});

// Разделы приложения — по списку id из scoped-resources.ts (там же сказано,
// почему список вынесен отдельным модулем и чем он проверяется).
const PROJECT_SCOPED = VPC_SCOPED_IDS.map((id) => REGISTRY[id]).filter(Boolean);
const COMPUTE_SCOPED = COMPUTE_SCOPED_IDS.map((id) => REGISTRY[id]).filter(Boolean);
const NLB_SCOPED = NLB_SCOPED_IDS.map((id) => REGISTRY[id]).filter(Boolean);

export default function App() {
  return (
    <ThemeProvider>
      <ThemedApp />
    </ThemeProvider>
  );
}

/**
 * ThemedApp — ConfigProvider, чья тема реактивно зависит от useThemeMode().
 * Вынесено из App, чтобы хук useThemeMode читался уже внутри <ThemeProvider>.
 */
function ThemedApp() {
  const { mode } = useThemeMode();
  return (
    <ConfigProvider
      locale={ruRU}
      form={{
        // Звёздочка required справа от label (по умолчанию AntD ставит слева).
        // По указанию user'а: все звёздочки должны быть справа.
        requiredMark: (label, info) => (
          <>
            {label}
            {info.required && (
              <span style={{ color: "#ff4d4f", marginLeft: 4 }} aria-hidden>
                *
              </span>
            )}
          </>
        ),
      }}
      theme={buildTheme(mode)}
    >
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <AuthProvider>
              <StepUpModal />
              <AppRoutes />
              <Toaster />
            </AuthProvider>
          </BrowserRouter>
        </QueryClientProvider>
      </AntdApp>
    </ConfigProvider>
  );
}

/**
 * AppRoutes — экспорт-only routes-tree (без BrowserRouter/AuthProvider/
 * QueryClient/ConfigProvider обёрток). Нужен для integration-тестов через
 * `<MemoryRouter><AppRoutes/></MemoryRouter>` (см. App.routes.test.tsx,
 * KAC-199). В runtime — рендерится App'ом выше.
 */
export function AppRoutes() {
  return (
    <Routes>
      {/* === Public routes (без RequireAuth) ===
              signup / login / registration / recovery / settings — Kratos
              self-service страницы. /auth/callback и /logout — пост-OIDC
              landing'и; user в этот момент ещё мог не дозагрузиться через
              /me — если поставить под RequireAuth, словим бесконечный
              redirect-loop callback↔login. */}
      <Route path="/signup" element={<SignupPage />} />
      <Route path="/auth/login" element={<LoginPage />} />
      <Route path="/auth/registration" element={<RegisterPage />} />
      <Route path="/auth/recovery" element={<RecoveryPage />} />
      <Route path="/auth/settings" element={<SettingsPage />} />
      <Route path="/auth/callback" element={<AuthCallback />} />
      <Route path="/logout" element={<LogoutPage />} />

      {/* === Protected routes (KAC-199 — требуют залогиненного user'а) ===
              RequireAuth: loading → Spin; user=null → redirect на
              /auth/login?return_to=<original>. Без неё anonymous user
              мог гулять по dashboard / IAM / VPC / NLB / Compute и видеть
              пустые таблицы (API возвращает 401). */}
      <Route element={<RequireAuth />}>
        <Route element={<Layout />}>
          {/* Root → dashboard. */}
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          {/* Dashboard with project context in URL. */}
          <Route path="/projects/:projectId/dashboard" element={<DashboardPage />} />

          {/* === Project-scoped VPC ресурсы === */}
          {/* /projects/:projectId/vpc/{networks|subnets|addresses|route-tables|security-groups} */}
          {PROJECT_SCOPED.map((spec) => (
            <Route key={spec.id}>
              <Route
                path={`/projects/:projectId/vpc/${spec.route}`}
                element={
                  // VpcListShell = ResourceListPage + ResourceFormModal mount
                  // (модалка открывается по ?modal=<spec>-create или
                  // ?modal=<spec>-edit&id=<uid>).
                  // VPC-раздел регистрирует `${route}/create` и панель правки (ниже).
                  <VpcListShell spec={spec} parentField="project_id" parentParam="projectId" panelForms />
                }
              />
              <Route
                path={`/projects/:projectId/vpc/${spec.route}/create`}
                element={
                  // Subnet — отдельная standalone-страница SubnetCreatePage
                  // (resource-specific layout как у SubnetDetailPage в edit-mode).
                  // Использует ?networkId=<n> для пред-фиксации сети;
                  // без параметра — показывает RefSelect "Сеть" вверху.
                  // Generic ResourceCreatePage оставлен для остальных VPC-
                  // ресурсов (Network, Address, RT, SG, Gateway, PE).
                  spec.id === "subnets" ? (
                    <SubnetCreatePage />
                  ) : spec.id === "network-interfaces" ? (
                    <NetworkInterfaceCreatePage />
                  ) : (
                    <ResourceCreatePage spec={spec} parentField="project_id" parentParam="projectId" />
                  )
                }
              />
              {/* KAC-231: единый ResourceShell для ВСЕХ VPC-ресурсов.
                    Доменный контент (SG-правила, RT-маршруты, Subnet-CIDR, ...)
                    подключён через DETAIL_EXTENSIONS (resource-detail-extensions). */}
              <Route path={`/projects/:projectId/vpc/${spec.route}/:uid`} element={<ResourceShell spec={spec} />} />
              <Route
                path={`/projects/:projectId/vpc/${spec.route}/:uid/edit`}
                element={<ResourceShell spec={spec} mode="edit" />}
              />
              {/* child-create: форма-панель в зоне 3 shell родителя (URI уникален). */}
              <Route
                path={`/projects/:projectId/vpc/${spec.route}/:uid/:childRoute/create`}
                element={<ResourceShell spec={spec} mode="child-create" />}
              />
              {/* path-based табы (related / extra / operations / json). */}
              <Route
                path={`/projects/:projectId/vpc/${spec.route}/:uid/:tab`}
                element={<ResourceShell spec={spec} />}
              />
            </Route>
          ))}

          {/* === Project-scoped Compute ресурсы === */}
          {/* /projects/:projectId/compute/instances */}
          {COMPUTE_SCOPED.map((spec) => (
            <Route key={spec.id}>
              <Route
                path={`/projects/:projectId/compute/${spec.route}`}
                // Правка compute-ресурса в vpc-разделе — модалка: панели правки
                // (ResourceShell) этот раздел для них не регистрирует.
                element={
                  <ResourceListPage spec={spec} parentField="project_id" parentParam="projectId" panelForms={false} />
                }
              />
              <Route
                path={`/projects/:projectId/compute/${spec.route}/create`}
                element={<ResourceCreatePage spec={spec} parentField="project_id" parentParam="projectId" />}
              />
              <Route
                path={`/projects/:projectId/compute/${spec.route}/:uid`}
                element={spec.id === "compute-instances" ? <InstanceDetailPage /> : <ResourceDetailPage spec={spec} />}
              />
              <Route
                path={`/projects/:projectId/compute/${spec.route}/:uid/edit`}
                element={spec.id === "compute-instances" ? <InstanceDetailPage /> : <ResourceDetailPage spec={spec} />}
              />
            </Route>
          ))}

          {/* === Project-scoped NLB ресурсы (KAC-141 / KAC-171) === */}
          {/* /projects/:projectId/nlb/{load-balancers|listeners|target-groups} */}
          {NLB_SCOPED.map((spec) => (
            <Route key={spec.id}>
              <Route
                path={`/projects/:projectId/nlb/${spec.route}`}
                // Как и compute выше: в vpc-разделе у NLB-ресурсов панели правки нет.
                element={
                  <ResourceListPage spec={spec} parentField="project_id" parentParam="projectId" panelForms={false} />
                }
              />
              <Route
                path={`/projects/:projectId/nlb/${spec.route}/create`}
                element={<ResourceCreatePage spec={spec} parentField="project_id" parentParam="projectId" />}
              />
              <Route
                path={`/projects/:projectId/nlb/${spec.route}/:uid`}
                element={spec.id === "target-groups" ? <TargetGroupDetailPage /> : <ResourceDetailPage spec={spec} />}
              />
              <Route
                path={`/projects/:projectId/nlb/${spec.route}/:uid/edit`}
                element={spec.id === "target-groups" ? <TargetGroupDetailPage /> : <ResourceDetailPage spec={spec} />}
              />
            </Route>
          ))}

          {/* KAC-231: child-create и path-based табы для ВСЕХ VPC-ресурсов
                генерируются в VPC_SCOPED.map (см. выше) — отдельные
                networks-/addresses-роуты больше не нужны. */}

          {/* === Global VPC Operations (project-scoped) === */}
          <Route path="/projects/:projectId/vpc/operations" element={<OperationsPage />} />

          {/* KAC-231: вложенные network-/subnet-scoped detail-страницы
                (SubnetDetailPage/RouteTableDetailPage/SecurityGroupDetailPage/
                AddressDetailPage) заменены единым flat ResourceShell (см.
                VPC_SCOPED.map). Навигация в дочерний ресурс — на его flat-URL,
                родитель показывается в хлебных крошках. */}

          {/* /projects/:projectId — редирект на dashboard */}
          <Route path="/projects/:projectId" element={<ProjectDefaultRedirect />} />
          {/* Edit project — full-page форма (KAC-124). */}
          <Route
            path="/projects/:projectId/edit"
            element={<ResourceEditPage spec={REGISTRY.projects} paramKey="projectId" />}
          />

          {/* === System (admin-only, kacho-only) === */}
          {/* Region/Zone/AddressPool — глобальные ресурсы. Не публикуются на
                external TLS endpoint, см. CLAUDE.md kacho-vpc §16.
                List-страницы обёрнуты в AdminLayout с горизонтальными табами
                навигации между admin-сущностями + кнопкой "Создать <singular>"
                в правом header-slot. */}
          <Route element={<AdminLayout />}>
            <Route path="/system/regions" element={<ResourceListPage spec={REGISTRY.regions} panelForms />} />
            <Route path="/system/zones" element={<ResourceListPage spec={REGISTRY.zones} panelForms />} />
            <Route
              path="/system/address-pools"
              element={<ResourceListPage spec={REGISTRY["address-pools"]} panelForms />}
            />
            {/* KAC-196: Cluster admins (Grant/Revoke) — единственная admin-страница,
                  не следующая ResourceListPage pattern: кастомная таблица с denorm
                  ClusterAdminEntry + GrantAdminModal вместо ResourceFormModal. */}
            <Route
              path="/system/cluster/admins"
              element={
                <Suspense fallback={null}>
                  <ClusterAdminsPage />
                </Suspense>
              }
            />
          </Route>
          <Route path="/system/regions/create" element={<ResourceCreatePage spec={REGISTRY.regions} />} />
          {/* Каталог размещения — тот же ResourceShell, что у VPC-ресурсов:
              иконка типа во вкладке, обзор без второго заголовка «Общее»,
              действия в шапке в общем стиле, а правка разворачивается ПАНЕЛЬЮ в
              правой части, а не уводит в модалку. Прежде эти карточки жили на
              старой странице, и все четыре отличия были её следствием. */}
          <Route path="/system/regions/:uid" element={<ResourceShell spec={REGISTRY.regions} />} />
          <Route path="/system/regions/:uid/edit" element={<ResourceShell spec={REGISTRY.regions} mode="edit" />} />
          <Route path="/system/zones/create" element={<ResourceCreatePage spec={REGISTRY.zones} />} />
          <Route path="/system/zones/:uid" element={<ResourceShell spec={REGISTRY.zones} />} />
          <Route path="/system/zones/:uid/edit" element={<ResourceShell spec={REGISTRY.zones} mode="edit" />} />
          <Route
            path="/system/address-pools/create"
            element={<ResourceCreatePage spec={REGISTRY["address-pools"]} />}
          />
          <Route path="/system/address-pools/:uid" element={<AddressPoolDetailPage />} />
          <Route
            path="/system/address-pools/:uid/edit"
            element={<ResourceEditPage spec={REGISTRY["address-pools"]} />}
          />
          <Route path="/system/search" element={<SystemSearchPage />} />

          {/* Управления доступом здесь НЕТ: `/iam/*` обслуживает ремоут iam
                (хост федерирует его туда), а ремоут vpc наружу отдаёт только
                `./VpcPage`. Вторая поверхность IAM жила в этом приложении, была
                достижима лишь в standalone-сборке и успела разойтись с
                контрактом до неработоспособности. */}

          {/* KAC-199: /auth/callback и /logout вынесены наверх под public-
                routes (Kratos session ещё не доступна в момент рендера
                callback'а; RequireAuth-guard вокруг них создал бы
                redirect-loop). */}

          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  );
}

// ProjectDefaultRedirect: /projects/:projectId → /projects/:projectId/dashboard
function ProjectDefaultRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/projects/${projectId}/dashboard`} replace />;
}
