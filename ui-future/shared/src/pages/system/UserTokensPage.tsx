// UserTokensPage — Stage 4. Выпуск/список/отзыв персональных access-токенов
// пользователя (kacho.cloud.iam.v1.UserTokenService, private_key_jwt).
//
// Users — глобальный список (GET /iam/v1/users). Приватный ключ выдаётся один раз.

import { iamApi } from "@shared/api/iam";
import { userTokensApi } from "@shared/api/tokens";
import { TokenIssuancePage, type SubjectOption, type CredentialRow, type TokenKindConfig } from "./TokenIssuancePage";

const USER_TOKENS_CONFIG: TokenKindConfig = {
  kind: "user",
  pageTitle: "Персональные токены пользователей",
  pageSubtitle:
    "Персональные access-токены (private_key_jwt). Приватный ключ выдаётся один раз; пользователь подписывает им client_assertion и обменивает в Hydra на kacho-JWT с принципалом user:<id>.",
  subjectSingular: "пользователь",
  subjectLabel: "Пользователь",
  credentialSingular: "токен",
  credentialPlural: "Токены",
  issuedTitle: "Персональный токен выпущен",
  listSubjects: async (): Promise<SubjectOption[]> => {
    const resp = await iamApi.listUsers({ pageSize: "1000" });
    return (resp.users ?? []).map((u) => ({
      value: u.id,
      label: `${u.display_name || u.email || u.id} · ${u.id}`,
    }));
  },
  listCredentials: (userId): Promise<CredentialRow[]> => userTokensApi.list(userId),
  issue: (userId, body) => userTokensApi.issue(userId, body),
  revoke: (userId, tokenId) => userTokensApi.revoke(userId, tokenId),
};

export default function UserTokensPage() {
  return <TokenIssuancePage config={USER_TOKENS_CONFIG} />;
}
