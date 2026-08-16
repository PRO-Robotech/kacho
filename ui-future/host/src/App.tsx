import { useEffect, useState } from "react";
import type { Dispatch, FC, SetStateAction } from "react";
import { ConfigProvider, theme } from "antd";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router";
import { ModuleErrorBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { HostShell } from "./components";
import { ModulePlaceholderPage, ReachabilityPage } from "./pages";
import {
  ComputeRemote,
  DashboardRemote,
  IamRemote,
  NlbRemote,
  RegistryRemote,
  StorageRemote,
  SystemRemote,
  VpcRemote,
} from "./remotes";

const THEME_STORAGE_KEY = "kacho-theme";

const readStoredTheme = (): boolean => {
  try {
    return window.localStorage.getItem(THEME_STORAGE_KEY) === "dark";
  } catch {
    return false;
  }
};

const App: FC = () => {
  const [dark, setDark] = useState(readStoredTheme);

  useEffect(() => {
    const mode = dark ? "dark" : "light";
    document.documentElement.dataset.theme = mode;
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, mode);
    } catch {
      // localStorage may be unavailable in restricted browser modes.
    }
  }, [dark]);

  return (
    <ConfigProvider
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: "#3d8df5",
          borderRadius: 6,
          // Базовый размер как в kacho-ui (эталон 13, не дефолтный AntD 14) —
          // покрывает host-хром + dashboard; remotes задают полную тему сами.
          fontSize: 13,
          fontFamily: "Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
        },
      }}
    >
      {/* Корневая граница отказа (#371): последний рубеж для того, что не поймала
          граница модуля — отказ самого каркаса host'а. Без неё непойманная ошибка
          доходит до корня, и React снимает с экрана ВСЁ дерево (белый экран). */}
      <ModuleErrorBoundary moduleLabel="Консоль Kachō">
        <BrowserRouter>
          <AppRoutes dark={dark} setDark={setDark} />
        </BrowserRouter>
      </ModuleErrorBoundary>
    </ConfigProvider>
  );
};

const AppRoutes: FC<{
  dark: boolean;
  setDark: Dispatch<SetStateAction<boolean>>;
}> = ({ dark, setDark }) => {
  const location = useLocation();
  const showReachability = (import.meta.env?.DEV ?? false) && location.pathname === "/dev/reachability";

  return (
    <HostShell dark={dark} setDark={setDark} showReachability={showReachability}>
      {(context) => (
        <Routes>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardRemote context={context} />} />
          <Route path="/projects/:projectId/dashboard" element={<DashboardRemote context={context} />} />
          <Route path="/projects/:projectId/vpc/*" element={<VpcRemote context={context} />} />
          <Route path="/projects/:projectId/compute/*" element={<ComputeRemote context={context} />} />
          <Route path="/projects/:projectId/storage/*" element={<StorageRemote context={context} />} />
          <Route path="/projects/:projectId/nlb/*" element={<NlbRemote context={context} />} />
          <Route path="/projects/:projectId/registry/*" element={<RegistryRemote context={context} />} />
          {/* Квоты — свойство ПРОЕКТА, а не сервиса, поэтому раздела в адресе у
              них нет. Маршрут обязан стоять ВЫШЕ ловушки «всё остальное» ниже:
              она принимает любой первый сегмент, поэтому `quotas` попадал в неё
              и раздел отвечал заглушкой «страница будет позже» — при живой
              странице, объявленной внутри vpc. Поверхность названа явно: адрес
              не лежит под точкой монтирования vpc, и остаток пути у модуля пуст
              (см. `RemotePageProps.surface`). */}
          <Route path="/projects/:projectId/quotas" element={<VpcRemote context={context} surface="quotas" />} />
          <Route path="/projects/:projectId/:moduleKey/*" element={<ModulePlaceholderPage />} />
          <Route path="/iam/*" element={<IamRemote context={context} />} />
          <Route path="/system/*" element={<SystemRemote context={context} />} />
          <Route path="/dev/reachability" element={<ReachabilityPage />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      )}
    </HostShell>
  );
};

export default App;
