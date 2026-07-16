# kacho-ui-future — iam remote → паритет с kacho-ui (backlog)

Источник: параллельный аудит паритета iam-UI (`kacho-ui-future/iam` ↔ эталон `kacho-ui`),
6 из 8 областей (accounts + access-bindings упали по schema-таймауту, доснять отдельно).
Ни одна область не at-parity. 17 high-gaps, 20 med-gaps.

## Сделано (визуальная полировка — этой сессией)
- **IamListShell + useTableScrollY** (`iam/src/components/organisms/iam/IamListShell/`): kc-surface
  на всю высоту + PanelHeader (иконка/«Список»/заголовок/счётчик) + фикс-thead с h/v-скроллом тела
  (scroll.x=max-content снимает посимвольный перенос колонок).
- Применено к RolesPage, GroupsPage, AccessBindingsPage, AccessPage, UsersPage (кастомные страницы
  висели на голом тёмном фоне, таблицы обрезались у футера, «Имя» переносилась посимвольно).

## HIGH (по приоритету)

1. **iam-operations — страница отсутствует целиком.** Нет `IamOperationsPage` и роута
   `/iam/operations`. Эталон: single account-scoped RPC + cursor-пагинация («Показать ещё» +
   nextPageToken) + accumulation (reset по смене account) + account-gate. Создать
   `pages/iam/IamOperationsPage/` + Route в `IamPage.tsx` + тест (single-RPC/account-gate/accumulation),
   `showResourceKind={false}` для OperationsTable.
2. **roles — RBAC rules-model 2026 отсутствует.** Только legacy `permissions[]`; нет `rules[]`,
   `RulesEditor`, ARM_ANCHOR/ARM_NAMES/ARM_LABELS (match_labels/resource_names). Добавить `rules[]` в
   API-слой, портировать `components/iam/RulesEditor.tsx`, заменить `PermissionsEditor` в
   `RolesPage.tsx`. Плюс: Create/Edit → modal-flow (`?modal=roles-create|edit`) вместо full-page роутов;
   список permissions `slice(0,3)+N` monospace; permission-каталог `usePermissionCatalog()`;
   выбор аккаунта в create; selective `update_mask`.
3. **groups — не registry-driven.** Нет spec `groups` в `resource-registry.tsx` (всё bespoke).
   Дубль `GroupMembersPanel` (экспортируемый неиспользуемый :39 + используемый :305). Свести к generic
   ListShell + `GlobalResourceFormModal`, оставив reusable `GroupMembersPanel(groupId, accountId)`.
4. **access — route-based grant вместо modal.** `AccessGrantPage` (`/iam/access/grant`) → `InviteModal`
   overlay (`setInviteOpen(true)`). Устаревший API `listAccessBindingsByResource` →
   `listAccessBindingsByScope` (F-50 rename). После success `navigate('/iam/access')` → `onClose()`.
5. **projects — «Аккаунт» = raw uid.** Заменить `format:"uid-short"` (registry :234) на
   `<IamRefLink specId="accounts" refId={row.account_id}/>`. **Портировать `IamRefLink`** из
   `kacho-ui/src/components/iam/IamRefLink.tsx` (адаптировать импорты: `@/components/organisms/form/
   ResourceIcon`, `getByPath` из `@/lib/path`, инлайн-иконка вместо gradient-tile). Плюс: `plural`
   «Проекты» (не «Projects»), `docs`/`emptyState`, убрать лишний `childRoute`.
6. **users-invite.** Колонка `account_id` с IamRefLink в списке Users; InviteUserPage — account
   auto-select (1 аккаунт → авто, >1 → Empty; иначе риск authz `account:*`); обёртка `FORM_WIDTH` (820);
   `data-testid` (invite-magic-link/invite-user-form); Space-группировка кнопок magic-link.

## MED
Roles: generic ResourceListPage + FormSection; groups: account_id-колонка + members в detail-табе
(не expandable) + полный набор form-полей; access: PanelHeader + account auto-select; projects: docs/
emptyState; users: registry-driven список + testid; iam-operations: account-gate + «Показать ещё» + тест.

## Доснять
accounts + access-bindings области (schema-таймаут в аудите) — прогнать точечно и добавить в backlog.

Общий приём резолва raw-id (usr…/rol…/prj…) в именах — общий `IamRefLink` (см. #5) во всех колонках
subject/role/resource/account (access-bindings, projects, groups, users).
