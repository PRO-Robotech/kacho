// Tokens API — SAKeyService + UserTokenService bindings (Stage 4).
//
// REST endpoints (verbatim из kacho-proto google.api.http annotations):
//   SA keys (kaname.cloud.iam.v1.SAKeyService):
//     POST   /iam/v1/serviceAccounts/{service_account_id}/keys           → Issue  → Operation → IssueSAKeyResponse
//     GET    /iam/v1/serviceAccounts/{service_account_id}/keys           → List   → {keys: ServiceAccountOAuthClient[]}
//     DELETE /iam/v1/serviceAccounts/{service_account_id}/keys/{key_id}  → Revoke → Operation
//   User tokens (kaname.cloud.iam.v1.UserTokenService):
//     POST   /iam/v1/users/{user_id}/tokens             → Issue  → Operation → IssueUserTokenResponse
//     GET    /iam/v1/users/{user_id}/tokens             → List   → {tokens: UserOAuthClient[]}
//     DELETE /iam/v1/users/{user_id}/tokens/{token_id}  → Revoke → Operation
//
// Мутации async → Operation envelope: клиент поллит GET /operations/{id}
// (см. lib/use-operation.ts) до done=true, затем читает Operation.response
// (IssueSAKeyResponse / IssueUserTokenResponse) — там one-time private_key_pem.
//
// Все три RPC несут required_acr_min="2" (step-up MFA). Без свежего AAL2 api-gateway
// вернёт 403/FailedPrecondition — вызывающий показывает friendly step-up сообщение.
//
// Wire-format: api/client.ts конвертирует camelCase ↔ snake_case на границе, поэтому
// здесь поля snake_case (как в proto). Auth — ambient httpOnly Kratos session cookie
// (same-origin fetch; api/client.ts не выставляет credentials, дефолт same-origin
// уже шлёт cookie).

import { api } from "./client";
import type { CredentialKind } from "@shared/lib/tokens-util";
import type { Operation } from "./types";

/** ServiceAccountOAuthClient — публичная запись static SA-ключа (без секрета). */
export interface SAKey {
  /** `soc_<...>` */
  id: string;
  /** ServiceAccount.id владельца. */
  sva_id: string;
  hydra_client_id: string;
  description?: string;
  expires_at?: string;
  last_used_at?: string;
  created_by_user_id?: string;
  created_at?: string;
  /** Вид удостоверения (`CredentialKind`) — чем оно себя предъявляет. */
  credential_kind?: CredentialKind;
}

/** UserOAuthClient — публичная запись персонального токена пользователя. */
export interface UserToken {
  /** `uoc_<...>` */
  id: string;
  user_id: string;
  hydra_client_id: string;
  description?: string;
  expires_at?: string;
  last_used_at?: string;
  created_by_user_id?: string;
  created_at?: string;
  public_key_pem?: string;
  key_algorithm?: string;
  /** Вид удостоверения (`CredentialKind`) — чем оно себя предъявляет. */
  credential_kind?: CredentialKind;
}

export interface ListSAKeysResponse {
  keys?: SAKey[];
  next_page_token?: string;
}

export interface ListUserTokensResponse {
  tokens?: UserToken[];
  next_page_token?: string;
}

/**
 * IssueSAKeyResponse — Operation.response после done=true. Секрет
 * (`private_key_pem`) присутствует РОВНО ОДИН РАЗ и невосстановим.
 */
export interface IssueSAKeyResponse {
  key?: SAKey;
  client_id?: string;
  /** ПОКАЗЫВАЕТСЯ ОДИН РАЗ. PEM PKCS#8 ECDSA P-256. */
  private_key_pem?: string;
  public_key_pem?: string;
  /** JOSE alg — всегда "ES256". */
  algorithm?: string;
  /** JWK kid зарегистрированного публичного ключа. */
  key_id?: string;
  audiences?: string[];
  /** ПОКАЗЫВАЕТСЯ ОДИН РАЗ. Заполнен ТОЛЬКО у вида SECRET. */
  secret?: string;
}

/** IssueUserTokenResponse — Operation.response после done=true. */
export interface IssueUserTokenResponse {
  token?: UserToken;
  client_id?: string;
  private_key_pem?: string;
  public_key_pem?: string;
  algorithm?: string;
  key_id?: string;
  /** ПОКАЗЫВАЕТСЯ ОДИН РАЗ. Заполнен ТОЛЬКО у вида SECRET. */
  secret?: string;
}

/**
 * Выпущенное одноразовое удостоверение — РАЗМЕЧЕННОЕ ВИДОМ, а не «набор полей,
 * часть которых пуста».
 *
 * Контракт заполняет РОВНО ОДНО из двух — `private_key_pem` у ключевой пары либо
 * `secret` у секрета, — и вид это и есть дискриминатор. Общая структура с двумя
 * необязательными полями заставила бы КАЖДОГО читателя решать заново, что перед
 * ним, и первый же забывший ветку показал бы пустое поле там, где лежит
 * единственный экземпляр невосстановимого значения.
 */
export type IssuedCredential =
  | {
      kind: "keypair";
      client_id: string;
      key_id: string;
      algorithm: string;
      private_key_pem: string;
      public_key_pem?: string;
    }
  | {
      kind: "secret";
      client_id: string;
      key_id: string;
      secret: string;
    };

/**
 * Достаёт one-time credential из тела завершённой операции.
 *
 * ЧИТАТЬ НАДО ТЕЛО ОТВЕТА ВЫДАЧИ, А НЕ ОПРОШЕННУЮ СТРОКУ ОПЕРАЦИИ. У вида SECRET
 * выдача завершается на пути запроса, и секрет подменяется в теле ответа ПОСЛЕ
 * записи: строка операции его не несёт ни в какой момент. Поэтому опрос
 * `GET /operations/{id}` вернёт тело БЕЗ секрета, и вызывающий обязан сперва
 * спросить немедленный ответ POST. У ключевой пары обе формы работают (ключ
 * лежит в строке до отложенного стирания).
 *
 * Отсутствие обеих форм — это НИЧЕГО, а не пустое удостоверение: показать
 * фантом с пустым полем значило бы объявить выдачу состоявшейся.
 */
export function issuedCredentialFromOperation(op: Operation | undefined | null): IssuedCredential | null {
  const resp = op?.response as (IssueSAKeyResponse & IssueUserTokenResponse) | undefined;
  if (!resp) return null;
  const clientId = resp.client_id ?? "";
  const keyId = resp.key_id ?? "";
  const secret = resp.secret ?? "";
  if (secret) {
    return { kind: "secret", client_id: clientId, key_id: keyId, secret };
  }
  const pem = resp.private_key_pem ?? "";
  if (!pem) return null;
  return {
    kind: "keypair",
    client_id: clientId,
    key_id: keyId,
    algorithm: resp.algorithm ?? "ES256",
    private_key_pem: pem,
    public_key_pem: resp.public_key_pem,
  };
}

export interface IssueTokenBody {
  description?: string;
  /**
   * Срок в секундах. Смысл нуля ЗАВИСИТ ОТ ВИДА: у ключевой пары это
   * «бессрочно», у секрета — «срок не назван», и его разрешает умолчание
   * политики (бессрочного секрета не бывает ни в каком написании).
   */
  ttl_seconds?: number;
  /** Требуется backend'ом (proto `created_by_user_id` (required)). */
  created_by_user_id: string;
  /**
   * Вид выдаваемого удостоверения. Не назван — сервер сохраняет ПРЕЖНЕЕ
   * поведение и выпускает ключевую пару; консоль называет вид ЯВНО, чтобы
   * умолчание сервера не решало за арендатора.
   */
  credential_kind?: CredentialKind;
}

const SA_KEYS = (saId: string) => `/iam/v1/serviceAccounts/${encodeURIComponent(saId)}/keys`;
const USER_TOKENS = (userId: string) => `/iam/v1/users/${encodeURIComponent(userId)}/tokens`;

export const saKeysApi = {
  /** GET keys для ServiceAccount. */
  list: (saId: string): Promise<SAKey[]> =>
    api.get<ListSAKeysResponse>(SA_KEYS(saId)).then((r) => r.keys ?? []),

  /** POST issue key → Operation. */
  issue: (saId: string, body: IssueTokenBody): Promise<{ operation: Operation }> =>
    api.create(SA_KEYS(saId), { service_account_id: saId, ...body }),

  /** DELETE revoke key → Operation. */
  revoke: (saId: string, keyId: string): Promise<{ operation: Operation }> =>
    api.delete(`${SA_KEYS(saId)}/${encodeURIComponent(keyId)}`),
};

export const userTokensApi = {
  /** GET tokens для User. */
  list: (userId: string): Promise<UserToken[]> =>
    api.get<ListUserTokensResponse>(USER_TOKENS(userId)).then((r) => r.tokens ?? []),

  /** POST issue token → Operation. */
  issue: (userId: string, body: IssueTokenBody): Promise<{ operation: Operation }> =>
    api.create(USER_TOKENS(userId), { user_id: userId, ...body }),

  /** DELETE revoke token → Operation. */
  revoke: (userId: string, tokenId: string): Promise<{ operation: Operation }> =>
    api.delete(`${USER_TOKENS(userId)}/${encodeURIComponent(tokenId)}`),
};
