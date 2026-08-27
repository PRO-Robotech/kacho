# Subject-privileges visibility — account-admin sees member privileges (by-design)

By-design note for `AccessBindingService.ListSubjectPrivileges`.

## Decision (conscious, accepted)

An account-admin / owner who views the privileges of a member of **their own
Account** sees that member's `role_name`, `scope`, `granted_by_user_id` and
`created_at`. This is intentional **audit visibility** — "who has which roles
inside my Account" — not an information leak.

Rationale: the Account is the IAM tenancy boundary. An admin who is authorized to
**grant / revoke** roles on the account tree (the `requireGrantAuthority` set) is
equally authorized to **see** the roles already held by members. "Who may grant"
== "who may view", keeping the authz model minimal and consistent.

## Authorization gate

`ListSubjectPrivileges` allows the caller iff EITHER:

- `IsSelf(subject_id)` — the caller views their own privileges; OR
- the caller administers the **subject's home Account**
  (`user.account_id` / `serviceAccount.account_id`): owner of that Account
  (`accounts.owner_user_id`) OR FGA `admin` on `account:<homeAccountId>`.

Cross-account reads (caller has neither self nor admin on the subject's home
account) are rejected with `PERMISSION_DENIED` — no privilege data is returned
(cross-account isolation). The subject's home account is resolved via a
within-`kacho_iam` `Users().Get` / `ServiceAccounts().Get` (same-schema read,
**not** a new cross-domain edge).

## Допуск и сужение — разные вещи (пересмотрено, задача #1354)

> [!warning] Здесь стояло «gate-authorized for the entire set, no per-row filtering»
> Утверждение опиралось на посылку «There is no heterogeneous per-row ownership
> to filter», и посылка **неверна**: строка ответа несёт `resource_type` /
> `resource_id`, то есть называет ОБЛАСТЬ выдачи, а области одного человека
> бывают в разных аккаунтах. Пройдя допуск по аккаунту `A`, распорядитель `A`
> получал строки про аккаунты `B` и `C` — то есть узнавал о существовании
> арендаторов, к которым отношения не имеет.
>
> Это тот же класс, что закрыт решением по #1085 (перечень аккаунтов человека,
> отданный распорядителю одного из них), в другой форме: не членства, а области
> выдач. Наблюдаемое следствие одно — картирование состава арендаторов.
>
> Абзац оставлен, а не удалён: он **отговаривал** от сужения, и следующий
> читатель, найдя фильтр в коде и это утверждение в документе, снял бы фильтр как
> лишний.

Действующая норма: страница читается курсором из своей базы и **сужается
построчно** — строка остаётся тогда, когда вызывающий вправе прочитать её выдачу
по идентификатору. Предикат берётся не отсюда, а из `internal/authzfilter`, где
он привязан к отношению, которым каталог прав гейтит одиночное чтение этого типа;
страница поэтому не может быть шире чтения.

**Законный обзор при этом не сужается, и это свойство модели, а не оговорка.**
`v_get` на выдаче выводится через `super_admin`, а тот — через
`admin from account`: распорядитель аккаунта `A` держит его на каждой выдаче, чей
родитель — `A`, без единого прямого кортежа; выдача в аккаунте `B` не выводится
ему ниоткуда. Сужение снимает ровно чужое.

### Две полосы проходят несужёнными

| полоса | почему |
|---|---|
| собственное чтение (вызывающий и есть субъект) | та же граница, по которой безопасен `ListBySubject`: ответ не шире того, что вызывающему принадлежит. Сузить её значило бы опустошить главное употребление чтения — и опустошить тихо: выдачей распоряжается администратор области, а не тот, кому она выдана, поэтому прямого кортежа у субъекта на свою же выдачу обычно нет |
| администратор облака | верхний ярус супер-доступа, паритет с `Get` / `ListByScope` / `ListByAccount` / `ListByRole` / `List`. Вопрос задаётся один раз на запрос |

### Fail-closed

Порт модели прав не провязан либо не ответил — **отказ** (`UNAVAILABLE`), а не
несужённая и не молча пустая страница: за этим чтением полоса края —
`scope_filtered`, пообъектной проверки нет, откатиться не на что. По той же
причине вызывающий, которого нечем назвать модели (принципал вида, для которого
субъект не строится), отсекается безусловно: пустой субъект `VisibleSet` не
отвергает — он возвращает пустой набор, и страница схлопнулась бы в `200`, не
отличимый для вызывающего от отзыва прав.

### Известный размен, названный, а не скрытый

Сужение идёт ПОСЛЕ чтения страницы — та же форма, что у `ListByAccount` и
`ListByScope`. Следствия два, оба документированы: страница бывает короче
запрошенной, а `next_page_token` может кодировать строку, недоступную вызывающему
(идентификатор и отметка времени; содержимое закрыто —
`security.md` §«Фильтрация — страница → проверка страницы»). Обход при этом ничего
не теряет: курсор идёт по собственным строкам субъекта.

### Чем держится

`services/iam/internal/apps/kacho/api/access_binding/list_subject_privileges_narrowing_test.go`
— семь проб, у отрицания в каждой стоит положительная половина на том же ответе.
Объявление поверхности — `rowFilter` в `services/iam/tools/auditlistfilter/profile.go`,
и оно проверяемо: `make -C services/iam audit-list-filter` доходит до
пообъектного вопроса внутри use-case'а, а не верит объявлению на слово.

## Distinction from `ListBySubject`

`ListBySubject` keeps its **strict self-list** contract (caller may only read
their own bindings; group → membership) and returns raw `AccessBinding` rows. It
is unchanged. `ListSubjectPrivileges` is a new, additive RPC with a broader
authz tier and an enriched response — the existing self-list contract is not
altered (no silent semantic break for current consumers).
