// UserTokensPanel — вкладка «Токены» пользователя: список личных OAuth-токенов
// (UserTokenService.List) + выпуск токена с TTL + одноразовый показ секрета +
// отзыв.
//
// Вид и поведение — общие с ключами сервисного аккаунта (`TokensPanel`): это
// один экран, и рисовать его дважды значило бы завести расхождение заранее.
// Своё у пользователя — только путь коллекции, ключ кэша и разворот полезной
// нагрузки списка; это объявляет ВЛАДЕЛЕЦ, здесь, рядом со своим путём.

import { iamApi, userTokensPath } from "@shared/api/iam";
import { TokensPanel, type TokenRow } from "@/components/organisms/TokensPanel";

export function UserTokensPanel({ userId }: { userId: string }) {
  return (
    <TokensPanel
      subjectId={userId}
      collectionPath={userTokensPath}
      queryKind="user-tokens"
      list={async (id) => ((await iamApi.listUserTokens(id, { page_size: "1000" })).tokens ?? []) as TokenRow[]}
      fallbackFileName="user-token"
      descriptionExample="токен для CI"
      tableTestId="user-tokens-table"
    />
  );
}
