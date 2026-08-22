// SaKeysPanel — вкладка «Токены» сервисного аккаунта: список OAuth-ключей
// (SAKeyService.List) + выпуск токена с TTL + одноразовый показ секрета + отзыв.
//
// Вид и поведение — общие с личными токенами пользователя (`TokensPanel`): это
// один экран, и рисовать его дважды значило бы завести расхождение заранее.
// Своё у сервисного аккаунта — только путь коллекции, ключ кэша и разворот
// полезной нагрузки списка; это объявляет ВЛАДЕЛЕЦ, здесь, рядом со своим путём.

import { iamApi, saKeysPath } from "@shared/api/iam";
import { TokensPanel, type TokenRow } from "@/components/organisms/TokensPanel";

export function SaKeysPanel({ serviceAccountId }: { serviceAccountId: string }) {
  return (
    <TokensPanel
      subjectId={serviceAccountId}
      collectionPath={saKeysPath}
      queryKind="sa-keys"
      list={async (id) => ((await iamApi.listSaKeys(id, { page_size: "1000" })).keys ?? []) as TokenRow[]}
      fallbackFileName="sa-key"
      descriptionExample="ключ для CI"
      tableTestId="sa-keys-table"
    />
  );
}
