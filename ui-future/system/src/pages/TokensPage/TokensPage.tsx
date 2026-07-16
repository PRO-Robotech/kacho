// TokensPage — «Токены и ключи» область system-remote.
//
// TokensRoutes (named) — <Routes>-блок, монтируется SystemPage под `/system/tokens/*`.
// TokensPage (default) — self-contained federated expose (RemoteShell + TokensRoutes).
//
// Страницы (кастомные antd, выпуск через Operation-poll + one-time-secret модалка):
//   Service-account keys — SAKeyService  (/iam/v1/serviceAccounts/{id}/keys)
//   User personal tokens — UserTokenService (/iam/v1/users/{id}/tokens)
// Обе несут required_acr_min="2" (step-up) — friendly notice при отсутствии.

import { Navigate, Route, Routes } from "react-router-dom";
import { TokensLayout } from "@/components/organisms/TokensLayout";
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
      <Route path="*" element={<Navigate to="service-account-keys" replace />} />
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
