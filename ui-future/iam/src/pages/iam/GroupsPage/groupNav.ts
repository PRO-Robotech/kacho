// groupNav — чистый (без UI-графа) helper навигации после создания группы.
// Вынесен из GroupsPage.tsx, чтобы логику выбора detail-target можно было
// юнит-тестировать без импорта тяжёлого antd/react-query-графа.

import type { Operation } from "@shared/api/types";

// CreateGroupMetadata.GroupId приходит в Operation.metadata после camelToSnake
// как `group_id` (см. inviteUser → metadata.user_id). Если id есть — ведём на
// detail новой группы, иначе fallback на список.
export function groupDetailPathFromOp(op: Operation | undefined): string {
  const newId = (op?.metadata as { group_id?: string } | undefined)?.group_id;
  return newId ? `/iam/groups/${newId}` : "/iam/groups";
}
