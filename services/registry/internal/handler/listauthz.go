// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listauthz.go — handler-level authz для ScopeFiltered registry-RPC
// (ListRepositories/ListTags/DeleteTag). Interceptor эти RPC пропускает
// (ScopeFiltered), потому что per-RPC single-object Check не покрывает две
// потребности: (1) per-repo row-filter каталога (namespace-viewer НЕ видит все repos
// автоматически) и (2) existence-hiding deny→NOT_FOUND (interceptor вернул бы
// PermissionDenied, раскрыв факт существования чужого repo). Обе — здесь.
//
// Authz читает caller-principal из ctx (populated principal-extract интерсептором на
// public-листенере) и Check'ает через InternalIAMService.Check по mTLS. Fail-closed:
// iam недоступен → Unavailable (не нефильтрованный список, не «deny»). breakglass
// (nil Authorizer) → bypass ПРОВЕРКИ ПРАВ, но НЕ требования личности.
//
// Личность обязательна БЕЗУСЛОВНО (callerSubject, первым делом в каждом гейте). За этими
// RPC нет per-RPC Check интерсептора, на который можно откатиться: он отдаёт passthrough
// ДО извлечения субъекта (ScopeFiltered). Значит запрос, не назвавший никого, обязан
// отсекаться здесь — и до того, как ветка breakglass успеет что-либо пропустить.
package handler

import (
	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// repoAuthzConcurrency — верхняя граница параллельных per-repo authz-Check при
// row-фильтрации ОДНОЙ страницы каталога. Паритет с data-plane serveCatalog
// (catalogAuthzConcurrency): fan-out в iam ограничен, а не «по одному синхронно».
// Само число Check ограничивается окном page_size ДО фильтра (см. ListRepositories),
// поэтому здесь только concurrency-bound.
const repoAuthzConcurrency = 8

// Authorizer — узкий порт per-repo authz-Check (InternalIAMService.Check →
// OpenFGA/ReBAC). subject — FGA subject-строка ("user:usr_…" / "service_account:…"),
// relation — verb-relation (v_list/v_delete), object — FGA object-строка. Реализуется
// check.IAMCheckClient. nil → breakglass (authz bypass).
type Authorizer interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

// verb-relations per-repo authz — локальные alias'ы единого источника internal/domain
// (для ScopeFiltered registry-RPC authz энфорсится ЗДЕСЬ, не в interceptor'е —
// repo-verb НЕ наследуется от namespace-tier). check.PermissionMap /
// dataplane/authz ссылаются на те же domain-константы: drift между planes исключён.
const (
	relationVList   = domain.FGARelationVList
	relationVGet    = domain.FGARelationVGet
	relationVCreate = domain.FGARelationVCreate
	relationVUpdate = domain.FGARelationVUpdate
	relationVDelete = domain.FGARelationVDelete
	relationAdmin   = domain.FGARelationAdmin
)

// registryObjectRef — FGA object namespace-реестра "registry_registry:<id>".
func registryObjectRef(registryID string) string {
	return domain.FGAObjectRef(domain.FGAObjectTypeRegistry, registryID)
}

// repositoryObjectRef — FGA object репозитория "registry_repository:<id>/<repo>".
func repositoryObjectRef(registryID, repository string) string {
	return domain.FGAObjectRef(domain.FGAObjectTypeRepository, registryID+"/"+repository)
}

// repoAuthz — handler-level per-repo authz. Пустой az (breakglass) → bypass.
//
// log — sink для строк, которые фильтр НЕ СУМЕЛ классифицировать и потому отбросил.
// Выпадение обязано быть названо: молча исчезнувшая строка неотличима от строки,
// которой не было, и разбираться в этом придётся уже по жалобе владельца ресурса.
// Значение берётся из slog.Default(), который композиционный корень выставляет на
// старте (serve.go).
type repoAuthz struct {
	az  Authorizer
	log *slog.Logger
}

func newRepoAuthz(az Authorizer) repoAuthz { return repoAuthz{az: az, log: slog.Default()} }

// callerSubject — FGA subject-строка вызывающего. Ошибка, если запрос не назвал НИКОГО:
// субъект не подставляется и не выводится, он либо есть, либо запроса нет.
//
// Три случая «никого», и все три обязаны отвергаться одинаково:
//   - ctx не нёс principal'а вовсе (нет credential'а / нет форвардера). Брать здесь
//     PrincipalFromContext нельзя: он отдаёт fallback {system, bootstrap}, который
//     форматируется в субъект уровня начальной загрузки — то есть безымянный запрос
//     спрашивал бы права от имени учётки, которой на кластере разрешено всё;
//   - principal несёт зарезервированное слово анонимности (edge пометил запрос без
//     credential'а) — именованная анонимность личностью не является;
//   - тип principal'а не называет тенантного субъекта (user / service_account) либо id
//     содержит FGA-разделители: FormatSubject в обоих случаях СОБЕРЁТ какую-то строку
//     (system→user:<id>, битый id→user:unknown), и вызывающий получил бы субъекта,
//     которым не является.
func callerSubject(ctx context.Context) (string, error) {
	p, ok := operations.PrincipalFromContextOK(ctx)
	if !ok || p.IsAnonymous() {
		return "", errUnnamedCaller()
	}
	if p.Type != subjectTypeUser && p.Type != subjectTypeServiceAccount {
		return "", errUnnamedCaller()
	}
	subject := authz.FormatSubject(p.Type, p.ID)
	if subject != p.Type+":"+p.ID {
		// FormatSubject подменил id (FGA-разделители) — субъект не назван, а сконструирован.
		return "", errUnnamedCaller()
	}
	return subject, nil
}

// Типы principal'ов, которые вправе быть субъектом tenant-facing RPC реестра.
const (
	subjectTypeUser           = "user"
	subjectTypeServiceAccount = "service_account"
)

// checkAs — единичный Check заданного субъекта против объекта. breakglass (nil az) →
// allow (субъект к этому моменту уже установлен вызывающим гейтом). Ошибка az —
// проброс наружу (caller маппит в Unavailable, fail-closed).
func (a repoAuthz) checkAs(ctx context.Context, subject, relation, object string) (bool, error) {
	if a.az == nil {
		return true, nil
	}
	return a.az.Check(ctx, subject, relation, object)
}

// gate — общая форма всех call-gate'ов: личность → Check → решение. onDeny задаёт, как
// выглядит отказ этого гейта (existence-hiding NOT_FOUND либо честный PERMISSION_DENIED).
// Отсутствие личности — отдельный, ПЕРВЫЙ исход: не «deny прав» и не «iam недоступен».
func (a repoAuthz) gate(ctx context.Context, relation, object string, onDeny error) error {
	subject, serr := callerSubject(ctx)
	if serr != nil {
		return serr
	}
	allowed, err := a.checkAs(ctx, subject, relation, object)
	if err != nil {
		return errAuthzUnavailable()
	}
	if !allowed {
		return onDeny
	}
	return nil
}

// namespaceGate — call-gate: subject обязан иметь v_list на registry_registry:<reg>.
// deny → NOT_FOUND (existence-hiding); az-error → UNAVAILABLE (fail-closed).
func (a repoAuthz) namespaceGate(ctx context.Context, registryID string) error {
	return a.gate(ctx, relationVList, registryObjectRef(registryID), errHideExistence())
}

// checkRepo — per-repo verb-Check (ListTags v_list / DeleteTag v_delete). deny →
// NOT_FOUND (existence-hiding — не раскрывать существование чужого repo); az-error →
// UNAVAILABLE.
func (a repoAuthz) checkRepo(ctx context.Context, registryID, repository, relation string) error {
	return a.gate(ctx, relation, repositoryObjectRef(registryID, repository), errHideExistence())
}

// checkRepository — per-repo verb-Check config-overlay Repository RPC (GetRepository
// v_get / UpdateRepository v_update / DeleteRepository v_delete / RenameRepository
// v_update / ListReferrers v_get). deny|az-absent → NOT_FOUND "repository not found"
// (existence-hiding, БАЙТ-В-БАЙТ с use-case failNotFound — unauthorized неотличимо от
// absent, A08/C02/A15); az-error → UNAVAILABLE (fail-closed).
func (a repoAuthz) checkRepository(ctx context.Context, registryID, repository, relation string) error {
	return a.gate(ctx, relation, repositoryObjectRef(registryID, repository), errRepoHideExistence())
}

// registryGate — namespace call-gate по заданному verb-relation на registry_registry:
// <reg> (CreateRepository v_create — невидимый реестр existence-hidden, X04). deny →
// NOT_FOUND (existence-hiding); az-error → UNAVAILABLE (fail-closed).
func (a repoAuthz) registryGate(ctx context.Context, registryID, relation string) error {
	return a.gate(ctx, relation, registryObjectRef(registryID), errHideExistence())
}

// requireRegistryAdmin — admin-gate any-path-to-PUBLIC (D-6): subject обязан держать
// admin на registry_registry:<reg>. НЕ existence-hiding: caller уже доказал доступ к
// видимому ресурсу (v_create/v_update прошёл ДО этого гейта), поэтому deny честен коду —
// PERMISSION_DENIED с contract-текстом msg (B02/B08/B10); az-error → UNAVAILABLE.
// breakglass (nil az) → allow.
func (a repoAuthz) requireRegistryAdmin(ctx context.Context, registryID, msg string) error {
	return a.gate(ctx, relationAdmin, registryObjectRef(registryID), status.Error(codes.PermissionDenied, msg))
}

// filterRegistries — row-filter коллекции реестров: оставляет только реестры, на
// registry_registry:<id> которых subject имеет v_list. non-member → пустой список
// (200+empty, НЕ 403 — List не гейтится per-object Check). breakglass → все;
// az-error → UNAVAILABLE (не отдаём нефильтрованный список — no-leak).
//
// Fan-out ограничен: сами Check выполняются bounded-concurrency (repoAuthzConcurrency)
// — паритет с filterRepos/filterOperations (List latency не масштабируется линейно по
// числу реестров страницы). Результат детерминирован (indexed slice сохраняет входной
// порядок).
func (a repoAuthz) filterRegistries(ctx context.Context, regs []*domain.Registry) ([]*domain.Registry, error) {
	subject, serr := callerSubject(ctx)
	if serr != nil {
		return nil, serr
	}
	if a.az == nil {
		return regs, nil
	}
	allowed := make([]bool, len(regs))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(repoAuthzConcurrency)
	for i, r := range regs {
		g.Go(func() error {
			ok, err := a.az.Check(gctx, subject, relationVList, registryObjectRef(r.ID))
			if err != nil {
				return err
			}
			allowed[i] = ok
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, errAuthzUnavailable()
	}
	out := make([]*domain.Registry, 0, len(regs))
	for i, r := range regs {
		if allowed[i] {
			out = append(out, r)
		}
	}
	return out, nil
}

// filterRepos — per-repo row-filter каталога: оставляет только repos, на
// registry_repository:<reg>/<repo> которых subject имеет v_list (REG-22/23). breakglass
// → все; az-error → UNAVAILABLE (не отдаём нефильтрованный список — no-leak).
//
// Fan-out ограничен: (1) вызывающий (ListRepositories) передаёт УЖЕ окно page_size
// (bounded число Check per RPC — anti-DoS, CWE-770), (2) сами Check выполняются
// bounded-concurrency (repoAuthzConcurrency) — паритет с data-plane serveCatalog.
// Результат детерминирован (indexed slice сохраняет входной порядок имён ASC).
func (a repoAuthz) filterRepos(ctx context.Context, registryID string, repos []*domain.Repository) ([]*domain.Repository, error) {
	subject, serr := callerSubject(ctx)
	if serr != nil {
		return nil, serr
	}
	if a.az == nil {
		return repos, nil
	}
	allowed := make([]bool, len(repos))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(repoAuthzConcurrency)
	for i, r := range repos {
		g.Go(func() error {
			ok, err := a.az.Check(gctx, subject, relationVList, repositoryObjectRef(registryID, r.Name))
			if err != nil {
				return err
			}
			allowed[i] = ok
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, errAuthzUnavailable()
	}
	out := make([]*domain.Repository, 0, len(repos))
	for i, r := range repos {
		if allowed[i] {
			out = append(out, r)
		}
	}
	return out, nil
}

// filterOperations — per-repo row-filter истории операций реестра (ListOperations).
// Interceptor гейтит RPC ТОЛЬКО namespace v_list на registry_registry:<reg>; но
// операция, scoped к конкретному sub-repo (DeleteTag → metadata.repository +
// Description "Delete tag … of <reg>/<repo>"), раскрыла бы namespace-viewer'у, НЕ
// имеющему v_list на этот repo, само его существование (имя/тег) — existence-oracle,
// обходящий per-repo isolation, которую держат ListTags/checkRepo. Поэтому repo-scoped
// операция видна ТОЛЬКО при per-repo v_list на registry_repository:<reg>/<repo> (тот
// же Check, что checkRepo/ListTags); иначе тихо выпадает (existence-hiding: без ошибки).
// Registry-level операции (metadata разобрана, непустого поля "repository" в ней нет)
// остаются видны — namespace v_list (interceptor) достаточно.
//
// Строка, принадлежность которой УСТАНОВИТЬ НЕ УДАЛОСЬ (opScopeUnknown), отбрасывается.
// Это не осторожность про запас: принадлежность добывается рефлексией, а рефлексия —
// шаг, который может не дать ответа (тип метаданных не резолвится в этом бинаре, байты
// не разбираются). «Не разобрал» и «разобрал, репозитория нет» — разные исходы, и
// выдавать второй за первый значит отдавать строку вообще без вопроса о правах: у
// такой строки остаётся описание вида "Delete tag <tag> of <reg>/<repo>", то есть ровно
// то имя, ради сокрытия которого фильтр и существует. Полярность та же, что у сужения
// потока изменений в compute (InternalWatchHandler.narrowToSubject): строка, чей вид не
// имеет объекта в модели прав, выпадает, а не проезжает.
//
// Выпадение НАЗЫВАЕТСЯ (одна запись на страницу, с числом): молча исчезнувшая строка
// неотличима от строки, которой не было, и различать их пришлось бы уже по жалобе
// владельца ресурса. Запись пишется только когда есть что называть — на полностью
// классифицированной странице её нет.
//
// registryID берётся из ЗАПРОСА (не из metadata) — операции уже отфильтрованы
// use-case'ом по resource_id=registryID, а доверять registry_id из metadata для
// построения authz-объекта незачем. breakglass → все; az-error → UNAVAILABLE
// (fail-closed, не отдаём частичный список — паритет с filterRepos/filterRegistries).
// Fan-out bounded-concurrency (repoAuthzConcurrency), детерминированный порядок.
func (a repoAuthz) filterOperations(ctx context.Context, registryID string, ops []operations.Operation) ([]operations.Operation, error) {
	subject, serr := callerSubject(ctx)
	if serr != nil {
		return nil, serr
	}
	if a.az == nil {
		return ops, nil
	}
	keep := make([]bool, len(ops))
	unclassified := 0
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(repoAuthzConcurrency)
	for i := range ops {
		repository, scope := repositoryOfOperation(&ops[i])
		switch scope {
		case opScopeRegistry:
			keep[i] = true // registry-level op — namespace v_list (interceptor) достаточно
			continue
		case opScopeUnknown:
			unclassified++ // принадлежность не установлена → строка не отдаётся
			continue
		case opScopeRepository:
		}

		g.Go(func() error {
			ok, err := a.az.Check(gctx, subject, relationVList, repositoryObjectRef(registryID, repository))
			if err != nil {
				return err
			}
			keep[i] = ok
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, errAuthzUnavailable()
	}
	if unclassified > 0 {
		a.logger().Warn(
			"list authz: operations dropped — their repository scope could not be determined from metadata",
			"dropped", unclassified, "page", len(ops), "registry_id", registryID)
	}
	out := make([]operations.Operation, 0, len(ops))
	for i := range ops {
		if keep[i] {
			out = append(out, ops[i])
		}
	}
	return out, nil
}

// logger — sink выпадений. Нулевой repoAuthz (собранный не через newRepoAuthz —
// например литералом в тесте) не должен ронять запрос разыменованием nil.
func (a repoAuthz) logger() *slog.Logger {
	if a.log == nil {
		return slog.Default()
	}
	return a.log
}

// opScope — насколько установлена принадлежность операции к sub-репозиторию.
// Три состояния, а не два: «не удалось установить» — самостоятельный исход, и
// схлопывать его в «registry-level» нельзя (см. filterOperations).
type opScope uint8

const (
	// opScopeUnknown — принадлежность НЕ установлена (метаданных нет, тип не
	// резолвится, байты не разбираются). Строка не отдаётся.
	opScopeUnknown opScope = iota
	// opScopeRegistry — установлено: операция уровня реестра, sub-репозитория у неё нет.
	opScopeRegistry
	// opScopeRepository — установлено: операция принадлежит названному sub-репозиторию.
	opScopeRepository
)

// repositoryOfOperation — sub-repository, к которому scoped операция (из её metadata),
// и НАСКОЛЬКО эта принадлежность установлена.
//
// Metadata Any интроспектится generic'ом (protoreflect-поле "repository") — любой новый
// repo-scoped op-тип покрывается автоматически, без перечисления конкретных
// *Metadata-типов. Именно поэтому шаг может не дать ответа, и ответ «не знаю» обязан
// быть отличим от ответа «репозитория нет»:
//
//   - метаданных нет вовсе → opScopeUnknown. Ни один из путей создания операции в
//     registry такую строку не производит (все зовут anypb.New), поэтому строка без
//     метаданных — не «уровень реестра», а строка неизвестного происхождения;
//   - тип метаданных не резолвится в этом бинаре либо байты не разбираются →
//     opScopeUnknown. Достижимо при версионном перекосе: строку записал бинарь,
//     знающий тип, читает — не знающий (например во время выкатки);
//   - метаданные разобраны, поля "repository" нет либо оно пусто → opScopeRegistry.
//     Это и есть настоящая операция уровня реестра (Create/Update/Delete/GC).
func repositoryOfOperation(op *operations.Operation) (string, opScope) {
	if op == nil || op.Metadata == nil {
		return "", opScopeUnknown
	}
	msg, err := op.Metadata.UnmarshalNew()
	if err != nil {
		return "", opScopeUnknown
	}
	fd := msg.ProtoReflect().Descriptor().Fields().ByName("repository")
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return "", opScopeRegistry
	}
	repository := msg.ProtoReflect().Get(fd).String()
	if repository == "" {
		return "", opScopeRegistry
	}
	return repository, opScopeRepository
}

// errHideExistence — deny на объект, который caller не вправе видеть: NOT_FOUND
// (existence-hiding — «есть-но-не-твой» неотличимо от «нет»).
func errHideExistence() error { return status.Error(codes.NotFound, "not found") }

// errRepoHideExistence — existence-hiding config-overlay Repository RPC: NOT_FOUND
// "repository not found" — БАЙТ-В-БАЙТ с use-case failNotFound (unauthorized неотличимо
// от absent; A08/C02/A15). Отдельный текст от errHideExistence (namespace-level "not
// found"), т.к. Repository-контракт несёт "repository not found".
func errRepoHideExistence() error { return status.Error(codes.NotFound, "repository not found") }

// errAuthzUnavailable — iam.Check недоступен: fail-closed UNAVAILABLE.
func errAuthzUnavailable() error {
	return status.Error(codes.Unavailable, "authorization service unavailable")
}

// errUnnamedCaller — запрос не назвал никого. Код и текст — те же, что даёт
// per-RPC-гейт интерсептора безымянному запросу на любом ОСТАЛЬНОМ RPC сервиса
// (DecisionDenied → PERMISSION_DENIED "permission denied"). Паритет намеренный: иначе
// сам код ответа сообщал бы неаутентифицированному пробующему, какие RPC авторизуются
// на уровне данных, а какие — интерсептором.
func errUnnamedCaller() error {
	return status.Error(codes.PermissionDenied, "permission denied")
}
