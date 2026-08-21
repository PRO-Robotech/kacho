import { useEffect, useState } from "react";
import type { Dispatch, FC, SetStateAction } from "react";
import { ConfigProvider } from "antd";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router";
import { ModuleErrorBoundary } from "@shared/components/organisms/ModuleErrorBoundary";
import { buildTheme } from "@shared/lib/theme";
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

/*
 * Умолчание — ТЁМНАЯ тема. Консоль операторская: в неё смотрят подолгу, часто
 * рядом с другими тёмными инструментами, и палитра продукта построена вокруг
 * тёмного фона — светлая тема выведена из неё, а не наоборот.
 *
 * Умолчание срабатывает ТОЛЬКО когда выбора нет. Сохранённый выбор сильнее его
 * в обе стороны: "light" остаётся светлой, "dark" — тёмной. Поэтому сравнение
 * идёт со светлым значением, а не с тёмным: любое иное содержимое ключа (пустая
 * строка, мусор от прежней версии) — это «выбора нет», а не «выбор светлой».
 */
const readStoredTheme = (): boolean => {
  try {
    return window.localStorage.getItem(THEME_STORAGE_KEY) !== "light";
  } catch {
    // localStorage недоступен (private mode) — выбора нет, значит умолчание.
    return true;
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
    /* Тема каркаса собирается ТЕМ ЖЕ buildTheme, что и у модулей. Прежде здесь
       стоял свой короткий набор токенов (свой primary, свой радиус), и каркас
       красился мимо общей палитры: правка цвета в shared доезжала до модулей и
       не доезжала до рейла с шапкой. Одно место — один источник. */
    <ConfigProvider theme={buildTheme(dark ? "dark" : "light")}>
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
