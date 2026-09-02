// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
)

// FGA-register-intent — чистые domain-value-типы transactional-outbox owner-tuple
// реле через IAM. Вместо прямой записи прав на стороне владельца после commit'а
// (dual-write) writer-tx Create/Delete/Update пишет RegisterIntent строкой в
// registry_outbox В ТОЙ ЖЕ tx (один commit). Отдельный register-drainer применяет
// каждый intent через kacho-iam InternalIAMService.RegisterResource /
// UnregisterResource по mTLS — идемпотентно, at-least-once, owner-tuple не теряется.
//
// Файл — leaf (только stdlib): импортируется и writer-side (emit), и client-side
// (decode) без import-цикла и без pgx/grpc в контракте.

// FGAObjectTypeRegistry — FGA object-type namespace-реестра. object-prefix
// `registry_` РАВЕН имени сервиса kacho-registry, и словарь имён модулей
// (pkg/platformmodules) объявляет у него обе колонки одинаковыми — в отличие от
// балансировщика, у которого различны три написания.
const FGAObjectTypeRegistry = "registry_registry"

// FGAObjectTypeRepository — FGA object-type конкретного репозитория (parent =
// registry_registry). object-id — "<registryID>/<repo>". Per-repo verb-relations
// развязаны от namespace-tier: доступ к repo требует отдельного
// verb-tuple, namespace-viewer НЕ видит все repos автоматически.
const FGAObjectTypeRepository = "registry_repository"

// FGAObjectTypeProject — FGA object-type parent-проекта (владелец в модели kacho-iam
// — тип `project`). Единый источник для обоих planes: project-hierarchy tuple
// (FGAProjectTuple) и create-child interceptor-Check (check.PermissionMap). Раньше
// check держал независимый литерал `iam_project` — тип, которого НЕТ в FGA-модели →
// Create.Check всегда denied. Константа исключает этот drift.
const FGAObjectTypeProject = "project"

// FGA relation-строки owner-hierarchy tuple'ов registry_registry. `project`
// линкует ресурс к project-у; `owner` — creator-tuple (обязателен: модель несёт
// relation owner, иначе creator-intent застрял бы unsent в outbox); `parent` —
// hierarchy-relation репозитория к его namespace-реестру
// (registry_repository #parent @registry_registry).
//
// Все три НАЗЫВАЮТСЯ из объявления приёмной стороны (pkg/authz/proxytuple), а не
// пишутся здесь литералом: закрытый набор принадлежит kacho-iam, и второе написание
// чужого набора — копия, которая разойдётся молча. Цена расхождения не «красный
// тест»: отвергнутое отношение отвергается на КАЖДОЙ доставке, а очередь считает
// отказ временным и заклинивает голову партиции на всё окно повторов.
const (
	FGARelationProject = string(proxytuple.RelationProject)
	FGARelationOwner   = string(proxytuple.RelationOwner)
	FGARelationParent  = string(proxytuple.RelationParent)
)

// FGA register-intent event-types (parity с CHECK-constraint registry_outbox и с
// kacho-iam RegisterResource/UnregisterResource).
const (
	FGAEventRegister   = "fga.register"
	FGAEventUnregister = "fga.unregister"
)

// Вид объекта, к которому относится намерение (колонка `resource_kind` очереди).
//
// Названы константами, а не повторены литералом семь раз, потому что по этому
// значению теперь ОТБИРАЮТ: подметальщик осиротевших регистраций сужает журнал до
// вида «репозиторий» — под тем же идентификатором объекта едут ещё и намерения о
// публичном доступе, и, смешай их, последним событием объекта оказалось бы снятие
// публичности, а сам объект остался бы неназванным. Опечатка в такой выборке не
// падает, а МОЛЧА сужает её до пустоты.
const (
	RegisterIntentKindRegistry              = "Registry"
	RegisterIntentKindRepository            = "Repository"
	RegisterIntentKindRepositoryPublicGrant = "RepositoryPublicGrant"
)

// FGA verb-relation-строки (verb-bearing модель Kachō: repo-verb НЕ
// наследуется от namespace-tier). ЕДИНЫЙ источник истины для обоих planes —
// check.PermissionMap (control-plane interceptor-gate), handler/listauthz
// (ScopeFiltered row-filter) и dataplane/authz (OCI push/pull) ссылаются сюда,
// не переобъявляя литералы (иначе rename одной строки рассинхронит планы).
const (
	FGARelationVGet    = "v_get"
	FGARelationVList   = "v_list"
	FGARelationVCreate = "v_create"
	FGARelationVUpdate = "v_update"
	FGARelationVDelete = "v_delete"
)

// FGARelationAdmin — admin-tier relation на registry_registry. Любой путь, где
// принципал САМ приводит ресурс к PUBLIC (per-repo flip B02, create-with-PUBLIC B08,
// default_visibility→PUBLIC B10) требует этого relation (D-6 any-path-to-PUBLIC gate).
const FGARelationAdmin = "admin"

// FGARelationEditor — editor-tier relation на PARENT-объекте (project). Create-child
// (RegistryService.Create) гейтится editor@project (не v_create@project): «create a
// registry IN the project» — editor-tier способность на РОДИТЕЛЕ, тогда как
// `project#v_create` — это account-level «создать сам project» (iam-реконсайлер НЕ
// материализует v_create/v_delete на project-scope для edit-роли). Совпадает с proto
// required_relation="editor" + api-gateway permission-catalog (defense-in-depth: оба
// плана резолвят ОДНО решение). Зеркалит compute/vpc/storage create-child = editor@project.
const FGARelationEditor = "editor"

// FGASubjectPublicWildcard — FGA subject-строка анонимного/публичного принципала
// ("user:*"). visibility=PUBLIC ⟺ существует tuple "user:* v_get registry_repository:
// <reg>/<repo>" (D-7): анонимный pull резолвится в этот wildcard subject. Governance —
// register/unregister по ИТОГОВОМУ visibility overlay-строки (B01/B06/B12).
const FGASubjectPublicWildcard = FGASubjectTypeUser + ":*"

// FGA subject-type namespaces аутентифицированного principal (parity с kacho-iam
// FGA-моделью). Разделяемы обоими encoders (FGASubjectFromPrincipal — control-plane
// по Principal.Type; FGASubjectFromID — data-plane по id-prefix), чтобы тип-строка
// жила в одном месте.
const (
	FGASubjectTypeUser           = "user"
	FGASubjectTypeServiceAccount = "service_account"
)

// Kachō principal id-prefix'ы (parity с kacho-iam domain PrefixUser/PrefixServiceAccount).
// Data-plane имеет только id из верифицированного JWT (без Principal.Type), поэтому
// выводит subject-тип по этим префиксам — единственный доступный ему дискриминатор.
const (
	principalIDPrefixUser           = "usr"
	principalIDPrefixServiceAccount = "sva"
)

// FGATuple — один owner-hierarchy tuple intent "<subject> #<relation> @<object>".
// Имена полей совпадают с kacho-proto RegisterResourceRequest (subject_id /
// relation / object) — applier мапит 1:1 без трансляции.
type FGATuple struct {
	SubjectID string `json:"subject_id"`
	Relation  string `json:"relation"`
	Object    string `json:"object"`
}

// Valid — все три компонента непусты. Неполный tuple — caller-side баг (drainer
// трактует декодированный неполный tuple как poison, не transient-retry).
func (t FGATuple) Valid() bool {
	return t.SubjectID != "" && t.Relation != "" && t.Object != ""
}

// SourceVersion — монотонный per-object маркер регистрации, общий для ОБОИХ путей её
// доставки в kacho-iam: BEFORE-INSERT-триггер registry_outbox штампует его
// clock_timestamp()'ом внутри writer-tx (миграция 0011), а синхронный registrar
// штампует wall-clock уже после commit'а — то есть строго не раньше. iam применяет
// зеркало last-source-state-wins (`source_version < EXCLUDED.source_version`), поэтому
// вторая доставка одной и той же регистрации не меняет ни одной строки, и iam по этому
// признаку опознаёт редоставку и пропускает повторную материализацию. Повторная
// регистрация (в т.ч. «выдать → отозвать → выдать») этим схлопнуться не может: каждая
// несёт строго более новую версию, а снятие регистрации удаляет строку зеркала целиком.
//
// Собственный UnmarshalJSON нужен ради строк, поставленных в очередь ДО миграции 0011:
// триггер штамповал туда BIGSERIAL id строки, то есть JSON-ЧИСЛО. Ошибка декода
// классифицировалась бы drainer'ом как ErrPermanent и ОТРАВИЛА бы durable-строку,
// потеряв owner-tuple. Число читается как «версии нет» (zero) → на проводе nil → iam
// '-infinity' → безусловное применение, ровно прежнее поведение.
type SourceVersion struct {
	time.Time
}

// UnmarshalJSON принимает RFC3339-строку (текущий формат) и молча вырождает в zero
// любое НЕ-строковое значение (legacy BIGSERIAL до миграции 0011) — см. тип.
func (v *SourceVersion) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		return nil
	}
	if s[0] != '"' {
		v.Time = time.Time{}
		return nil
	}
	return v.Time.UnmarshalJSON(b)
}

// RegisterIntent — полный набор owner-hierarchy tuple'ов одного реестра
// (project-hierarchy + creator). Весь набор — одна outbox-строка = одна логическая
// единица apply. Несёт labels + parent-project для output-only resource_mirror в
// iam (питает label-selectable authz-scope; source of truth = kacho-registry).
type RegisterIntent struct {
	// Kind — вид ресурса для observability ("Registry"). Не участвует в apply.
	Kind string `json:"kind"`
	// ResourceID — id ресурса для observability/tracing. Не участвует в apply.
	ResourceID string `json:"resource_id"`
	// Tuples — набор tuple-намерений (project-tuple ПЕРВЫМ — grabli listener-
	// visibility: project-tuple применяется раньше creator-tuple).
	Tuples []FGATuple `json:"tuples"`
	// Labels — копия labels реестра (label-selectable authz-scope в iam-mirror).
	// На Update несёт НОВЫЕ labels → снятая метка реально отзывает label-scope.
	Labels map[string]string `json:"labels,omitempty"`
	// ParentProjectID — owning-project (containment scope в iam-mirror).
	ParentProjectID string `json:"parent_project_id,omitempty"`
	// ParentChain — цепь предков от БЛИЖАЙШЕГО к дальнему, `"<type>:<id>"`.
	//
	// Заполняется там, где иерархия ГЛУБЖЕ проекта. У реестра это репозиторий:
	// он лежит под реестром, а реестр — под проектом. Без цепи репозиторий
	// регистрировался с одним лишь `ParentProjectID`, то есть как будто он
	// подчинён проекту напрямую, и промежуточное звено — сам реестр — не
	// существовало для области выдачи вовсе.
	ParentChain []string `json:"parent_chain,omitempty"`
	// SourceVersion — монотонный per-object маркер. На write-пути значение,
	// сериализованное здесь, ПЕРЕЗАПИСЫВАЕТСЯ BEFORE-INSERT-триггером
	// registry_outbox (clock_timestamp() внутри writer-tx, миграция 0011) — Go его не
	// проставляет. Читается на read-пути: applier прокидывает его в
	// RegisterResource/UnregisterResource. Синхронный registrar штампует свою версию
	// сам (после commit'а), в это поле не заглядывая.
	SourceVersion SourceVersion `json:"source_version,omitempty"`
}

// Marshal сериализует intent в JSONB-payload registry_outbox.payload.
func (i RegisterIntent) Marshal() ([]byte, error) {
	b, err := json.Marshal(i)
	if err != nil {
		return nil, fmt.Errorf("marshal fga register intent: %w", err)
	}
	return b, nil
}

// UnmarshalRegisterIntent разбирает JSONB-payload registry_outbox обратно в intent.
func UnmarshalRegisterIntent(payload []byte) (RegisterIntent, error) {
	var i RegisterIntent
	if err := json.Unmarshal(payload, &i); err != nil {
		return RegisterIntent{}, fmt.Errorf("unmarshal fga register intent: %w", err)
	}
	return i, nil
}

// FGAObjectRef строит "<objectType>:<objectID>" FGA object-строку.
func FGAObjectRef(objectType, objectID string) string {
	return objectType + ":" + objectID
}

// FGAProjectTuple — project-hierarchy tuple
// "project:<projectID> #project @registry_registry:<registryID>".
func FGAProjectTuple(registryID, projectID string) FGATuple {
	return FGATuple{
		SubjectID: FGAObjectRef(FGAObjectTypeProject, projectID),
		Relation:  FGARelationProject,
		Object:    FGAObjectRef(FGAObjectTypeRegistry, registryID),
	}
}

// FGAOwnerTuple — creator owner-tuple
// "<subject> #owner @registry_registry:<registryID>". subject — FGA subject-строка
// (напр. "user:usr…") аутентифицированного principal. Пустой subject → неполный
// tuple; caller пропускает его (system-инициированный ресурс без human-owner).
func FGAOwnerTuple(subject, registryID string) FGATuple {
	return FGATuple{
		SubjectID: subject,
		Relation:  FGARelationOwner,
		Object:    FGAObjectRef(FGAObjectTypeRegistry, registryID),
	}
}

// FGASubjectFromPrincipal — FGA subject-строка "<type>:<id>" аутентифицированного
// principal, либо "" для system/unauthenticated (creator-tuple тогда пропускается).
// "system" трактуется как unauthenticated.
func FGASubjectFromPrincipal(principalType, principalID string) string {
	if principalType == "" || principalID == "" || principalType == "system" {
		return ""
	}
	return principalType + ":" + principalID
}

// FGASubjectFromID — FGA subject-строка из одного Kachō principal id (data-plane
// имеет только `sub` из верифицированного JWT, без Principal.Type). Тип выводится по
// id-prefix — единственному дискриминатору, доступному без обращения к iam: usr_ →
// user, sva_ → service_account. В схеме ids Kachō id-prefix и Principal.Type
// согласованы (usr_ всегда type=user), поэтому результат совпадает с
// control-plane FGASubjectFromPrincipal для тех же principal'ов. Пустой id → ""
// (невалидный caller — AuthN не должен был его пропустить). Неизвестный prefix
// трактуется как service_account (docker login сейчас — SA-key only; сохраняет
// прежнее поведение data-plane heuristic'а).
func FGASubjectFromID(principalID string) string {
	switch {
	case principalID == "":
		return ""
	case strings.HasPrefix(principalID, principalIDPrefixUser):
		return FGAObjectRef(FGASubjectTypeUser, principalID)
	case strings.HasPrefix(principalID, principalIDPrefixServiceAccount):
		return FGAObjectRef(FGASubjectTypeServiceAccount, principalID)
	default:
		// docker login сейчас — SA-key only (identity-only токен); неизвестный prefix
		// → service_account (сохраняет прежнее поведение data-plane heuristic'а).
		return FGAObjectRef(FGASubjectTypeServiceAccount, principalID)
	}
}

// FGASubjectForPrincipalID — resolve a verified data-plane principal id to its FGA
// subject, honoring the configured anonymous principal id (RG-1 D-7). When
// anonPrincipalID is non-empty AND principalID equals it, the caller is the public
// anonymous principal and resolves to FGASubjectPublicWildcard ("user:*"): a valid
// anon Bearer thus reads only PUBLIC repos (via the repo's `user:* v_get` tuple) and
// can NEVER write (no wildcard write relation exists) — B03 / B14. Every other
// principalID resolves by id-prefix via FGASubjectFromID.
//
// anonPrincipalID=="" → anonymous pull DISABLED (secure-by-default): a token whose sub
// happens to equal the anon id resolves as an ordinary principal, never silently
// gaining the wildcard. The anon principal id is deployment-configured (the iam anon
// Hydra client id); deploy MUST keep it reserved (no real principal shares it), since
// a token proving sub==anonPrincipalID can only be minted by holding the anon client's
// key (the anon flow).
func FGASubjectForPrincipalID(principalID, anonPrincipalID string) string {
	if anonPrincipalID != "" && principalID == anonPrincipalID {
		return FGASubjectPublicWildcard
	}
	return FGASubjectFromID(principalID)
}

// RegisterIntentForCreate — intent на Create реестра: project-tuple ПЕРВЫМ, затем
// (при аутентифицированном principal) owner-tuple. Несёт labels + parent-project
// для label-selectable authz-scope в iam-mirror.
func RegisterIntentForCreate(r *Registry, principalType, principalID string) RegisterIntent {
	tuples := []FGATuple{FGAProjectTuple(r.ID, r.ProjectID)}
	if subject := FGASubjectFromPrincipal(principalType, principalID); subject != "" {
		tuples = append(tuples, FGAOwnerTuple(subject, r.ID))
	}
	return RegisterIntent{
		Kind:            RegisterIntentKindRegistry,
		ResourceID:      r.ID,
		Tuples:          tuples,
		Labels:          copyLabels(r.Labels),
		ParentProjectID: r.ProjectID,
	}
}

// RegisterIntentForUpdate — mirror-feed re-register на Update: project-tuple с
// ОБНОВЛЁННЫМИ labels (без creator-tuple). Снятая из labels метка реально отзывает
// label-scoped доступ в iam-mirror (security-инвариант против label-clear no-op).
func RegisterIntentForUpdate(r *Registry) RegisterIntent {
	return RegisterIntent{
		Kind:            RegisterIntentKindRegistry,
		ResourceID:      r.ID,
		Tuples:          []FGATuple{FGAProjectTuple(r.ID, r.ProjectID)},
		Labels:          copyLabels(r.Labels),
		ParentProjectID: r.ProjectID,
	}
}

// UnregisterIntentForDelete — unregister-intent на Delete: называет project-tuple.
//
// Остальное с объекта снимает ПРИНИМАЮЩАЯ сторона. Здесь называется только иерархический
// указатель, потому что `owner` пишется от личности вызывающего, а её не хранит ни строка
// реестра, ни зеркало iam — назвать этот tuple на удалении отсюда нечем. Снятие
// иерархического указателя iam трактует как снос объекта и убирает всё, что этот же proxy
// мог на объекте написать (services/iam/.../internal_iam: residualTuples).
//
// Прежняя редакция этой строки утверждала то же самое про «iam-side GC», но механизма не
// существовало: замер 2026-08-04 показал, что `owner` пережил доставленное снятие в 180
// случаях из 180 у реестров и в 60 из 60 у репозиториев, а модель на удалённом объекте
// продолжала отвечать allowed. Комментарий описывал намерение, а не код.
func UnregisterIntentForDelete(registryID, projectID string) RegisterIntent {
	return RegisterIntent{
		Kind:       RegisterIntentKindRegistry,
		ResourceID: registryID,
		Tuples:     []FGATuple{FGAProjectTuple(registryID, projectID)},
	}
}

// repoObjectID — FGA object-id репозитория "<registryID>/<repo>".
func repoObjectID(registryID, repo string) string { return registryID + "/" + repo }

// FGARepoParentTuple — parent-hierarchy tuple репозитория
// "registry_registry:<reg> #parent @registry_repository:<reg>/<repo>". Линкует repo
// к namespace-реестру (наследование tier'ов project→registry→repository).
func FGARepoParentTuple(registryID, repo string) FGATuple {
	return FGATuple{
		SubjectID: FGAObjectRef(FGAObjectTypeRegistry, registryID),
		Relation:  FGARelationParent,
		Object:    FGAObjectRef(FGAObjectTypeRepository, repoObjectID(registryID, repo)),
	}
}

// FGARepoOwnerTuple — creator owner-tuple репозитория
// "<subject> #owner @registry_repository:<reg>/<repo>". subject — FGA subject-строка
// толкающего principal ("service_account:sva…"); пустой subject → tuple пропускается.
func FGARepoOwnerTuple(registryID, repo, subject string) FGATuple {
	return FGATuple{
		SubjectID: subject,
		Relation:  FGARelationOwner,
		Object:    FGAObjectRef(FGAObjectTypeRepository, repoObjectID(registryID, repo)),
	}
}

// RegisterIntentForRepoPush — intent на первый push нового repo: parent-tuple ПЕРВЫМ
// (repo линкуется к реестру раньше creator-tuple — тот же урок порядка, что для
// project→owner), затем (при аутентифицированном pushing-principal) owner-tuple.
// projectID — owning-project реестра-владельца; несётся как ParentProjectID, чтобы
// resource_mirror строка репо в iam получила containment scope и reconciler
// материализовал per-object v_* (без него репо невидим/непуллим даже владельцу).
// Labels репо не несёт (у type нет own-table labels — label-scope неприменим).
// subject — FGA subject толкающего.
func RegisterIntentForRepoPush(registryID, repo, projectID, subject string) RegisterIntent {
	tuples := []FGATuple{FGARepoParentTuple(registryID, repo)}
	if subject != "" {
		tuples = append(tuples, FGARepoOwnerTuple(registryID, repo, subject))
	}
	return RegisterIntent{
		Kind:       RegisterIntentKindRepository,
		ResourceID: repoObjectID(registryID, repo),
		Tuples:     tuples,
		// Цепь называет ПРОМЕЖУТОЧНОЕ звено, которого два прежних поля выразить
		// не могли: репозиторий подчинён реестру, а реестр — проекту. Порядок от
		// ближайшего предка к дальнему.
		ParentChain:     []string{"registry_registry:" + registryID, "project:" + projectID},
		ParentProjectID: projectID,
	}
}

// UnregisterIntentForRepo — unregister-intent на удаление последнего тега repo: называет
// parent-tuple registry_repository:<reg>/<repo>.
//
// Как и у UnregisterIntentForDelete, остальное с объекта снимает принимающая сторона по
// факту снятия иерархического указателя. Для репозитория это важнее, чем для реестра:
// его id — путь `<registryID>/<repo>`, то есть ИМЯ, а не случайный идентификатор. Уцелевший
// `owner` на удалённом репозитории поэтому не просто висит — он ВОЗВРАЩАЕТСЯ в силу, как
// только кто-то создаст репозиторий с тем же именем в том же реестре, и прежний владелец
// получает полный доступ к чужому содержимому.
func UnregisterIntentForRepo(registryID, repo string) RegisterIntent {
	return RegisterIntent{
		Kind:       RegisterIntentKindRepository,
		ResourceID: repoObjectID(registryID, repo),
		Tuples:     []FGATuple{FGARepoParentTuple(registryID, repo)},
	}
}

// FGARepoPublicGetTuple — public-read wildcard tuple репозитория
// "user:* #v_get @registry_repository:<reg>/<repo>". Существование этого tuple ⟺
// visibility=PUBLIC (D-7): анонимный (`user:*`) data-plane read проходит Check.
func FGARepoPublicGetTuple(registryID, repo string) FGATuple {
	return FGATuple{
		SubjectID: FGASubjectPublicWildcard,
		Relation:  FGARelationVGet,
		Object:    FGAObjectRef(FGAObjectTypeRepository, repoObjectID(registryID, repo)),
	}
}

// RegisterIntentForRepoPublicGrant — register-intent public-read wildcard tuple:
// материализует "user:* v_get" на repo (visibility стал PUBLIC — per-repo flip B01,
// create-with-PUBLIC B08, inherited-default B12). Идемпотентно at-least-once через
// outbox: повторный register того же wildcard дедуплицируется iam-side.
func RegisterIntentForRepoPublicGrant(registryID, repo string) RegisterIntent {
	return RegisterIntent{
		Kind:       RegisterIntentKindRepositoryPublicGrant,
		ResourceID: repoObjectID(registryID, repo),
		Tuples:     []FGATuple{FGARepoPublicGetTuple(registryID, repo)},
	}
}

// UnregisterIntentForRepoPublicGrant — unregister-intent public-read wildcard tuple:
// снимает "user:* v_get" (visibility стал PRIVATE — flip B06 — либо repo удалён/
// переименован). anon pull снова fail-closed 404. Per-subject grants не трогаются.
func UnregisterIntentForRepoPublicGrant(registryID, repo string) RegisterIntent {
	return RegisterIntent{
		Kind:       RegisterIntentKindRepositoryPublicGrant,
		ResourceID: repoObjectID(registryID, repo),
		Tuples:     []FGATuple{FGARepoPublicGetTuple(registryID, repo)},
	}
}

// copyLabels — defensive-копия карты labels (mirror не должен ссылаться на
// внутреннюю карту domain-объекта). Пустая карта → nil (omitempty в payload).
func copyLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
