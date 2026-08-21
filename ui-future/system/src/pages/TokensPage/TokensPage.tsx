// TokensPage — «Токены и ключи» область system-remote.
//
// TokensRoutes (named) — <Routes>-блок, монтируется SystemPage под `/system/tokens/*`.
// TokensPage (default) — self-contained federated expose (RemoteShell + TokensRoutes).
//
// Страницы (кастомные antd, выпуск через Operation-poll + one-time-secret модалка):
//   Service-account keys — SAKeyService  (/iam/v1/serviceAccounts/{id}/keys)
//   User personal tokens — UserTokenService (/iam/v1/users/{id}/tokens)
// Обе несут required_acr_min="2" (step-up) — friendly notice при отсутствии.

import { Navigate, Route, Routes } from "react-router";
import { TokensLayout } from "@/components/organisms/TokensLayout";
import { TOKENS_LANDING_PATH } from "@/labels";
import ServiceAccountKeysPage from "@shared/pages/system/ServiceAccountKeysPage";
import UserTokensPage from "@shared/pages/system/UserTokensPage";
import { RemoteShell } from "@/pages/RemoteShell";

export function TokensRoutes() {
  return (
    <Routes>
      <Route index element={<Navigate to="service-account-keys" replace />} />
      <Route element={<TokensLayout />}>
        <Route path="service-account-keys" element={<ServiceAccountKeysPage />} />
        <Route path="user-tokens" element={<UserTokensPage />} />
      </Route>
      {/* Адрес назначения АБСОЛЮТНЫЙ — ровно по той же причине, что и у соседнего
          блока маршрутов (`SystemRoutes`). Относительный «service-account-keys»
          внутри splat-маршрута резолвится от УЖЕ СОПОСТАВЛЕННОГО пути
          (`/system/tokens/что-угодно`), давая
          `/system/tokens/что-угодно/service-account-keys`; он снова попадает
          сюда — и перенаправление зацикливается, наращивая адрес до
          бесконечности. Наблюдаемо это как «страница не открывается» без
          единого сообщения; в пробе — как прогон, который не заканчивается
          вовсе: цикл синхронный, поэтому предел времени у пробы не срабатывает.
          Соседний блок это уже чинил, а этот остался с прежней формой. */}
      <Route path="*" element={<Navigate to={TOKENS_LANDING_PATH} replace />} />
    </Routes>
  );
}

export default function TokensPage() {
  return (
    <RemoteShell>
      <TokensRoutes />
    </RemoteShell>
  );
}
