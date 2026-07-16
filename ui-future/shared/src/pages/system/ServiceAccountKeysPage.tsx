// ServiceAccountKeysPage — Stage 4. Выпуск/список/отзыв static OAuth-ключей
// сервисных аккаунтов (kacho.cloud.iam.v1.SAKeyService).
//
// SA-list требует account_id (ListServiceAccountsRequest.account_id), поэтому
// список best-effort: если 403/empty — пользователь вводит ID SA вручную.

import { iamApi } from "@shared/api/iam";
import { saKeysApi } from "@shared/api/tokens";
import { TokenIssuancePage, type SubjectOption, type CredentialRow, type TokenKindConfig } from "./TokenIssuancePage";

const SA_KEYS_CONFIG: TokenKindConfig = {
  kind: "sa",
  pageTitle: "Ключи сервисных аккаунтов",
  pageSubtitle:
    "Static OAuth-ключи (Class A workload identity). Приватный ключ выдаётся один раз и подписывает client_assertion для получения kacho-JWT через Hydra.",
  subjectSingular: "сервисный аккаунт",
  subjectLabel: "Сервисный аккаунт",
  credentialSingular: "ключ",
  credentialPlural: "Ключи",
  issuedTitle: "Ключ сервисного аккаунта выпущен",
  listSubjects: async (): Promise<SubjectOption[]> => {
    const resp = await iamApi.listServiceAccounts({ pageSize: "1000" });
    return (resp.service_accounts ?? []).map((sa) => ({
      value: sa.id,
      label: `${sa.name || sa.id} · ${sa.id}`,
    }));
  },
  listCredentials: (saId): Promise<CredentialRow[]> => saKeysApi.list(saId),
  issue: (saId, body) => saKeysApi.issue(saId, body),
  revoke: (saId, keyId) => saKeysApi.revoke(saId, keyId),
};

export default function ServiceAccountKeysPage() {
  return <TokenIssuancePage config={SA_KEYS_CONFIG} />;
}
