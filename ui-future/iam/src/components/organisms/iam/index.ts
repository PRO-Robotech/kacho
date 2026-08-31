export {
  CopyableMonoId,
  fmtTs,
  groupedRoleOptions,
  SystemTag,
  useIamMutation,
} from "@shared/components/organisms/iam/IamCommon";
export { IamScopedListShell } from "./IamScopedListShell";
export { InlineRoleCreateForm } from "./InlineRoleCreateForm";
export { InlineRoleEditForm } from "./InlineRoleEditForm";
// Только компонент — helper'ы (emptyRule/ruleInvalid/rulesInvalid)
// импортируются напрямую из ./RulesEditor, чтобы не конфликтовать по имени
// `emptyRule` с form-barrel (SgRulesEditor.emptyRule). WILDCARD в этом
// перечислении стоял, пока RulesEditor его ре-экспортировал; дом символа —
// `@shared/api/usePermissionCatalog`, и все потребители берут его оттуда (#1783).
export { RulesEditor } from "./RulesEditor";
