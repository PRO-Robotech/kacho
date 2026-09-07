// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package registry — use-case (бизнес-логика) реестра образов kacho-registry.
//
// Use-case слой чистой архитектуры: импортирует domain + порты + corelib
// operations; НЕ тянет pgx/grpc/transport. Здесь объявлены port-интерфейсы
// (RegistryReader/RegistryWriter — CQRS, ZotClient, IAMClient) и общая часть
// UseCase; тела мутаций — в create.go / update.go / delete.go.
//
// Формы RPC (api-conventions.md): Get/List/ListRepositories/ListTags — sync;
// Create/Update/Delete/DeleteTag — async через operation.Operation. Read-часть
// (Get/List) — sync pass-through к репозиторию; мутации sync-валидируют вход и
// project-existence, затем LRO-worker (с проброшенным principal) финализирует.
package registry

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// ListQuery — вход для List реестров project'а (cursor-пагинация).
type ListQuery struct {
	ProjectID string
	PageSize  int64
	PageToken string
	Filter    string
}

// RepoListQuery — вход для ListRepositories namespace (cursor-пагинация).
type RepoListQuery struct {
	RegistryID string
	PageSize   int64
	PageToken  string
}

// TagListQuery — вход для ListTags конкретного repo (cursor-пагинация).
type TagListQuery struct {
	RegistryID string
	Repository string
	PageSize   int64
	PageToken  string
}

// CreateSpec — вход на создание Registry (тело CreateRegistryRequest, распарсенное
// тонким handler'ом в нейтральную форму).
type CreateSpec struct {
	ProjectID   string
	Name        string
	Description string
	Labels      map[string]string
	// RegionID — REGIONAL placement-якорь (REG-1 F4). Обязателен; peer-validate geo.
	RegionID string
}

// UpdateSpec — вход Update. project immutable (в spec не входит); name — mutable.
// Handler подаёт сырые Name/Description/Labels + Mask; use-case после mask-discipline
// выставляет ApplyName/ApplyDescription/ApplyLabels — по ним репозиторий строит
// частичный UPDATE (пустая карта Labels при ApplyLabels=true реально очищает метки).
type UpdateSpec struct {
	RegistryID       string
	Name             string
	Description      string
	Labels           map[string]string
	Mask             []string
	ApplyName        bool
	ApplyDescription bool
	ApplyLabels      bool
	// DefaultVisibility — сид visibility для новых repo (RG-1, B10/B11). Mutable через
	// UpdateRegistry; переход →PUBLIC admin-gated (handler). ApplyDefaultVisibility —
	// выставляется mask-discipline при "default_visibility" в update_mask.
	DefaultVisibility      domain.Visibility
	ApplyDefaultVisibility bool
}

// ---- Порты (АНКЕРЫ для rpc-implementer; CQRS-разделение read/write) ----------

// RegistryReader — read-порт таблицы registries. Реализуется
// internal/repo/kacho/pg.RegistryRepo; в unit-тестах подменяется mock.
type RegistryReader interface {
	// Get возвращает реестр по id (well-formed-но-нет → ErrNotFound).
	Get(ctx context.Context, id string) (*domain.Registry, error)
	// List возвращает реестры project'а (cursor-пагинация; listauthz-фильтр — в handler).
	List(ctx context.Context, q ListQuery) ([]*domain.Registry, string, error)
}

// RegistryWriter — write-порт таблицы registries. Мутации атомарны на DB-уровне
// (INSERT/UPDATE ... RETURNING, CAS-переход ACTIVE→DELETING) и пишут owner-tuple
// intent в registry_outbox в ТОЙ ЖЕ writer-tx. Никакого software check-then-act.
type RegistryWriter interface {
	// Insert создаёт реестр + register-intent в registry_outbox одной tx. partial
	// UNIQUE(project_id,name) WHERE status<>'DELETING' → 23505 → ErrAlreadyExists.
	Insert(ctx context.Context, r *domain.Registry, intent domain.RegisterIntent) (*domain.Registry, domain.RegisterIntent, error)
	// Update применяет mutable-поля (по Apply*-флагам) одним UPDATE ... RETURNING;
	// mirror register-intent строится callback'ом ИЗ обновлённой строки (нужны
	// её project_id + новые labels) и эмитится в ТОЙ ЖЕ tx (без Get/TOCTOU).
	Update(ctx context.Context, spec UpdateSpec, mirror func(*domain.Registry) domain.RegisterIntent) (*domain.Registry, error)
	// MarkDeleting — атомарный forward-only CAS в DELETING (UPDATE ... WHERE
	// status IN ('ACTIVE','DELETING') RETURNING): ACTIVE→DELETING либо идемпотентно
	// DELETING→DELETING (уже DELETING → строка возвращается как success, чтобы
	// retry/крэш-рекавери довели удаление до конца). 0 rows только когда строки
	// нет (уже удалена) → ErrNotFound. DELETING терминальный: revert в ACTIVE невозможен.
	MarkDeleting(ctx context.Context, id string) (*domain.Registry, error)
	// Delete удаляет строку реестра + unregister-intent в registry_outbox одной tx.
	Delete(ctx context.Context, id string, intent domain.RegisterIntent) error
}

// RegistryRepo — композитный CQRS-порт (read+write) для composition root.
type RegistryRepo interface {
	RegistryReader
	RegistryWriter
}

// ZotClient — порт к data/registry-API zot (source of truth образов). Проекции
// Repository/Tag читаются на request-path; удаление тегов/GC — через этот же порт.
type ZotClient interface {
	// ListRepositories возвращает окно проекции namespace (из zot). Repo без единого
	// тега приходит с tag_count=0 и адаптером НЕ скрывается: видим ли он тенанту,
	// зависит от наличия строки наложения, а её адаптер не знает — решает объединение
	// overlay ⊔ projection (mergeRepository).
	ListRepositories(ctx context.Context, q RepoListQuery) ([]*domain.Repository, string, error)
	// ListRepositoryNames возвращает ИМЕНА repos namespace, реально присутствующих в
	// движке (без namespace-префикса, ASC) — одним дешёвым запросом, БЕЗ per-repo
	// fan-out. Нужен объединению overlay ⊔ projection, чтобы отличить строку наложения,
	// имени которой в движке НЕТ вовсе (её проекцию спрашивать не у чего), от строки,
	// лежащей в другой странице проекции — разностью множеств, а не поштучным опросом
	// проекции. Имя, которое движок помнит, попадает сюда независимо от числа тегов —
	// его строка приходит окном проекции. zot недоступен → ErrUnavailable (fail-closed).
	ListRepositoryNames(ctx context.Context, registryID string) ([]string, error)
	// ListTags возвращает теги repo (проекция из zot).
	ListTags(ctx context.Context, q TagListQuery) ([]*domain.Tag, string, error)
	// DeleteTag удаляет тег/манифест repo в zot.
	DeleteTag(ctx context.Context, registryID, repository, tag string) error
	// NamespaceEmpty сообщает, пуст ли namespace (Delete непустого → FailedPrecondition).
	NamespaceEmpty(ctx context.Context, registryID string) (bool, error)
	// RemoveNamespace снимает storage-namespace реестра в zot (шаг async-Delete).
	RemoveNamespace(ctx context.Context, registryID string) error
	// TriggerGC запускает garbage collection namespace в zot.
	TriggerGC(ctx context.Context, registryID string) error
	// Stats возвращает инфра-статистику namespace (только для Internal-API).
	Stats(ctx context.Context, registryID string) (*domain.RegistryStats, error)

	// RepositoryProjection возвращает projection-слой одного repo (tag_count/size/
	// artifact-типы/timestamps) для overlay ⟂ projection LEFT JOIN GetRepository.
	// Нет проекции (repo без единого тега / ещё не пушился) → (nil, nil) — durable
	// overlay пережил пустоту (tagCount=0), ephemeral без проекции невидим (handler
	// existence-hiding). zot недоступен → ErrUnavailable (fail-closed).
	RepositoryProjection(ctx context.Context, registryID, repository string) (*domain.Repository, error)
	// RepositoryEmpty сообщает, есть ли у repo ≥1 тег (DeleteRepository reject-if-tags,
	// D-4: source of truth emptiness = engine). zot недоступен → ErrUnavailable
	// (fail-closed: overlay не сносим, пока не подтвердили пустоту, A14).
	RepositoryEmpty(ctx context.Context, registryID, repository string) (bool, error)
	// CopyRepositoryTags копирует ВСЕ теги/манифесты repo old→new в движке —
	// НЕРАЗРУШАЮЩАЯ фаза переноса (многошаговая НЕ-атомарная OCI-операция, D-5).
	// Сбой движка → ErrUnavailable (fail-closed): старое имя цело и резолвится, под
	// новым может остаться ЧАСТИЧНАЯ копия. Идемпотентна: повторная публикация того
	// же манифеста под тем же тегом — это тот же контент по тому же digest'у.
	CopyRepositoryTags(ctx context.Context, registryID, oldName, newName string) error
	// PurgeRepositoryTags снимает ВСЕ теги repo в движке — РАЗРУШАЮЩАЯ фаза переноса,
	// исполняемая ТОЛЬКО ПОСЛЕ того, как новое имя закреплено в наложении и правах.
	// Обратный порядок (снять, потом закрепить) на сбое посередине уносит теги из
	// имени, которое знает тенант и на которое выданы права, — см. rename_repository.go.
	// Идемпотентна: отсутствующий тег — не ошибка.
	PurgeRepositoryTags(ctx context.Context, registryID, repository string) error
	// ListReferrers возвращает referrer-проекцию subject_digest (bounded full-set, D-8),
	// опционально отфильтрованную server-side по artifactType facet. Пусто → []
	// (не ошибка, C03). zot недоступен → ErrUnavailable.
	ListReferrers(ctx context.Context, registryID, repository, subjectDigest, artifactType string) ([]*domain.Referrer, error)
}

// IAMClient — порт к kaname: cross-domain валидация project (ProjectService.Get)
// на Create. Owner-tuple lifecycle идёт НЕ отсюда, а через registry_outbox +
// register-drainer (fga-proxy) — чтобы атомарно с DML и at-least-once.
type IAMClient interface {
	// ProjectExists валидирует project-владельца на Create (не найдено →
	// ErrInvalidArg; iam недоступен → ErrUnavailable, мутация fail-closed).
	ProjectExists(ctx context.Context, projectID string) error
}

// GeoClient — порт к kacho-geo: cross-domain валидация region (RegionService.Get) на
// Create (REG-1 F4, новое ребро registry→geo). По by-lane (peer-validate lane):
// region отсутствует → ErrFailedPrecondition (REG-1-12); geo недоступен →
// ErrUnavailable (мутация fail-closed, REG-1-13). Per-call deadline — в adapter'е.
type GeoClient interface {
	// RegionExists валидирует region-якорь Registry на Create.
	RegionExists(ctx context.Context, regionID string) error
}

// RepoRegistrar — порт эмита owner/parent-tuple intent'ов репозитория в
// registry_outbox (тот же transactional-outbox, что CRUD реестра). Repo как
// authz-объект появляется на первом push (register) и снимается на удалении
// последнего тега (unregister) — оба через durable-intent, применяемый
// register-drainer'ом через fga-proxy идемпотентно. Реализуется pg.RegistryRepo.
type RepoRegistrar interface {
	// RegisterRepository эмитит register-intent (parent+owner tuple) нового repo.
	RegisterRepository(ctx context.Context, intent domain.RegisterIntent) error
	// UnregisterRepository эмитит unregister-intent repo (снятие parent-tuple).
	UnregisterRepository(ctx context.Context, intent domain.RegisterIntent) error
}

// SyncRegistrar — порт СИНХРОННОЙ регистрации owner/parent/public-grant tuple'ов в
// kaname СРАЗУ после durable-commit ресурса (immediate materialization). Реализуется
// clients/iam.SyncRegistrar поверх InternalIAMService.RegisterResource (idempotent).
//
// Отличие от RepoRegistrar (durable outbox-emit в writer-tx): SyncRegistrar применяет тот
// же register-tuple-набор СИНХРОННО через iam post-commit — чтобы owner-grant / pull-grant
// был виден без гонки с async register-drainer'ом. Без него под burst создания repo/registry
// drainer сериализуется, owner-tuple поздних ресурсов лагает, и repo GET 404-ит в окне
// материализации (read-your-writes EC). register-ONLY: снятие tuple (unregister) идёт
// исключительно async-drainer'ом (не read-your-writes-критично).
//
// Best-effort у вызывающего: ошибка НЕ валит Create — durable outbox-intent + register-drainer
// остаются at-least-once backstop'ом (та же идемпотентная регистрация повторно безопасна).
// nil-port (dev/no-iam) → sync-путь пропускается, остаётся ТОЛЬКО async-drainer.
type SyncRegistrar interface {
	// Register синхронно применяет каждый tuple каждого intent через iam RegisterResource
	// (per-call deadline — в adapter'е). Возвращает первую ошибку; вызывающий логирует WARN.
	Register(ctx context.Context, intents []domain.RegisterIntent) error
}

// UseCase — бизнес-логика Registry поверх портов (CQRS repo + config-overlay repo +
// zot + iam + repo-registrar) и LRO-стека operations.
type UseCase struct {
	reader       RegistryReader
	writer       RegistryWriter
	cfg          RepositoryConfigRepo
	zot          ZotClient
	iam          IAMClient
	geo          GeoClient
	repoReg      RepoRegistrar
	ops          operations.Repo
	endpointBase string
	// syncReg — синхронная регистрация owner-tuple в kaname после durable-commit
	// (immediate materialization; nil → sync-путь пропускается, остаётся async
	// register-drainer как at-least-once backstop). Инжектится WithSyncRegistrar.
	syncReg SyncRegistrar
	// quota — совещательная полоса учёта числа ресурсов.
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции),
	// поэтому провязка обязательна на любом поднятом стенде; отсутствие
	// допустимо только там, где нет и соседа, у которого спрашивать величины.
	quota QuotaGuard
}

// QuotaGuard — совещательная полоса учёта числа ресурсов.
//
// Порт объявлен здесь, у вызывающего, а реализация живёт в
// `apps/kacho/quota`: use-case не импортирует адаптер, и подставить полосу в
// пробе можно, не поднимая ни базы, ни соседа.
//
// Полоса НЕ является решением: между её ответом и вставкой помещается чужая
// запись, и решает атомарное списание триггера (ban #10, §7.4 приёмки).
type QuotaGuard interface {
	// Admit — есть ли место у ПРОЕКТА под ещё одну строку этого вида.
	Admit(ctx context.Context, projectID, kind string) error
	// AdmitCarrier — тот же вопрос про носителя-РОДИТЕЛЯ.
	AdmitCarrier(ctx context.Context, carrierType, carrierID, kind string) error
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *UseCase) WithQuotaGuard(g QuotaGuard) *UseCase {
	u.quota = g
	return u
}

// New собирает UseCase. reader/writer — одна pg-реализация (CQRS-разделение на
// уровне портов); cfg — config-overlay Repository (RG-1, repository_configs);
// repoReg эмитит repo-tuple intent'ы (register-on-first-push / unregister-on-last-tag,
// adopt-owner, public-grant governance); ops — corelib LRO-репозиторий; endpointBase —
// tenant-facing база для output-only Registry.endpoint ("<base>/<id>").
func New(reader RegistryReader, writer RegistryWriter, cfg RepositoryConfigRepo, zot ZotClient, iam IAMClient, geo GeoClient, repoReg RepoRegistrar, ops operations.Repo, endpointBase string) *UseCase {
	if endpointBase == "" {
		endpointBase = "registry.kacho.local"
	}
	return &UseCase{reader: reader, writer: writer, cfg: cfg, zot: zot, iam: iam, geo: geo, repoReg: repoReg, ops: ops, endpointBase: endpointBase}
}

// WithSyncRegistrar подключает синхронный owner-tuple registrar (парити storage/vpc/nlb):
// после успешного Create-commit register-type tuple'ы регистрируются сразу, чтобы
// repo/registry GET и authz-filtered List на свежий ресурс разрешались без гонки с async
// register-drainer'ом. Best-effort: durable outbox-intent + register-drainer — at-least-once
// backstop, поэтому sync-ошибка НЕ валит Create (ban #9 async-мутация). nil → sync-путь
// пропускается (dev/no-iam). Возвращает тот же UseCase (chainable).
func (u *UseCase) WithSyncRegistrar(r SyncRegistrar) *UseCase {
	u.syncReg = r
	return u
}

// syncRegisterOwnerTuples — best-effort синхронная регистрация register-type owner-tuple'ов
// после durable-commit. Ошибка НЕ пробрасывается: durable outbox-intent уже записан в
// writer-tx, register-drainer применит его at-least-once (idempotent). Логируем WARN, чтобы
// потерянная sync-регистрация была видна (async backstop подхватит). nil registrar / пустой
// набор → no-op.
func (u *UseCase) syncRegisterOwnerTuples(ctx context.Context, intents ...domain.RegisterIntent) {
	if u.syncReg == nil || len(intents) == 0 {
		return
	}
	if err := u.syncReg.Register(ctx, intents); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async register-drainer will apply", "err", err)
	}
}

// registerIntents извлекает register-type intents (Event==FGAEventRegister) из смешанного
// OutboxIntent-набора — sync-registrar регистрирует ТОЛЬКО register-tuple'ы (owner/parent/
// public-grant); unregister идёт исключительно async-drainer'ом (снятие tuple не
// read-your-writes-критично).
func registerIntents(intents []OutboxIntent) []domain.RegisterIntent {
	out := make([]domain.RegisterIntent, 0, len(intents))
	for _, it := range intents {
		if it.Event == domain.FGAEventRegister {
			out = append(out, it.Intent)
		}
	}
	return out
}

// EndpointFor возвращает tenant-facing OCI-endpoint реестра ("<base>/<id>").
// Output-only проекция; используется handler'ом (Get/List) и worker'ом (Create).
func (u *UseCase) EndpointFor(id string) string {
	if id == "" {
		return ""
	}
	return u.endpointBase + "/" + id
}

// assertWired — defensive-гейт: composition root обязан подать все коллабораторы.
// Незаполненная зависимость → Unavailable (не паника в prod-path).
func (u *UseCase) assertWired() error {
	if u.reader == nil || u.writer == nil || u.zot == nil || u.iam == nil || u.geo == nil || u.ops == nil {
		return regerrors.ErrUnavailable
	}
	return nil
}

// assertRepoWired — defensive-гейт config-overlay Repository RPC: cfg-порт обязателен.
// Отдельно от assertWired, т.к. overlay-порт добавлен позже (RG-1) и старые CRUD-пути
// реестра его не требуют — незаполненный cfg → Unavailable (не паника в prod-path).
func (u *UseCase) assertRepoWired() error {
	if err := u.assertWired(); err != nil {
		return err
	}
	if u.cfg == nil || u.repoReg == nil {
		return regerrors.ErrUnavailable
	}
	return nil
}

// Get возвращает Registry по id. Тонкий pass-through к read-порту.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Registry, error) {
	if err := u.assertWired(); err != nil {
		return nil, err
	}
	if err := ValidateRegistryID(id); err != nil {
		return nil, err
	}
	r, err := u.reader.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return r, nil
}

// List возвращает реестры project'а (listauthz-фильтр выполняет handler).
// Sync: валидирует page_size (0→default 50, max 1000; вне диапазона →
// InvalidArgument), затем cursor-запрос; garbage page_token → InvalidArgument.
func (u *UseCase) List(ctx context.Context, q ListQuery) ([]*domain.Registry, string, error) {
	if err := u.assertWired(); err != nil {
		return nil, "", err
	}
	size, err := validatePageSize(q.PageSize)
	if err != nil {
		return nil, "", err
	}
	q.PageSize = size
	items, next, err := u.reader.List(ctx, q)
	if err != nil {
		return nil, "", mapRepoErr(err)
	}
	return items, next, nil
}

// ListRepositories возвращает объединение overlay ⊔ projection repos namespace (A20,
// D-1) СТРАНИЦАМИ. Ephemeral (проекция без overlay) несёт visibility=PRIVATE и
// lifecycle=EPHEMERAL; durable (проекция + overlay-строка) обогащается overlay-полями
// (description/labels/visibility/created_at/lifecycle); durable-empty (overlay без
// единого тега) переживает пустоту и присутствует в выдаче. Per-repo v_list row-filter
// (existence-hiding) — В ХЕНДЛЕРЕ ПОСЛЕ union.
//
// Объединение — ДВЕ НЕПЕРЕСЕКАЮЩИЕСЯ полосы, проходимые последовательно одним опаковым
// курсором: (1) строки наложения, чьего имени НЕТ в движке вовсе (тегов нет by
// construction, проекцию по ним запрашивать не у чего); (2) окно проекции движка —
// ВСЕ имена, которые движок помнит, включая те, у которых не осталось ни одного тега.
// Полосы не пересекаются (признак полосы — знает ли движок имя), поэтому ни одна
// строка не приходит дважды и ни одна не теряется, а размер страницы соблюдается в обеих.
//
// Пустой repo существует в двух РАЗНЫХ качествах, и различить их можно только здесь:
// со строкой наложения он живой долговременный ресурс (Get его отдаёт, удалить может
// только DeleteRepository) — обязан быть в выдаче; без строки это просто имя, которое
// движок ещё помнит после удаления тегов, — тенанту невидимо. Поэтому адаптер отдаёт
// такие имена как есть (tag_count=0), а скрытие решает mergeRepository. Пока решение
// принимал адаптер (у которого наложения нет), живой долговременный repo с нулём тегов
// не попадал НИ в одну полосу и пропадал из перечисления, оставаясь читаемым поштучно.
//
// Число обращений к движку на страницу — КОНСТАНТА (перечень имён namespace + окно
// проекции), а не функция размера реестра: durable-only строки определяются разностью
// множеств, а не поштучным опросом проекции. Прежняя реализация дописывала К ОКНУ все
// строки наложения на первой странице, каждую отдельным обращением к движку — страница
// раздувалась до размера реестра, обращений выходило N−page_size+1, а строка, лежащая в
// другой странице проекции, приходила ДВАЖДЫ.
//
// Курсор опаковый и имён не несёт (existence-oracle): полоса + позиция внутри неё.
// Позиции — offset'ы, поэтому push/удаление между страницами может сдвинуть границу —
// то же свойство, что у offset-курсора проекции.
func (u *UseCase) ListRepositories(ctx context.Context, q RepoListQuery) ([]*domain.Repository, string, error) {
	if err := u.assertRepoWired(); err != nil {
		return nil, "", err
	}
	size, err := validatePageSize(q.PageSize)
	if err != nil {
		return nil, "", err
	}
	lane, cursor, err := decodeRepoCursor(q.PageToken)
	if err != nil {
		return nil, "", err
	}

	var out []*domain.Repository
	if lane == repoLaneDurable {
		page, next, derr := u.durableEmptyPage(ctx, q.RegistryID, size, cursor)
		if derr != nil {
			return nil, "", derr
		}
		out = page
		if next != "" {
			return out, encodeRepoCursor(repoLaneDurable, next), nil
		}
		cursor = "" // полоса наложения исчерпана — дальше только проекция, с её начала
	}

	remaining := size - int64(len(out))
	if remaining <= 0 {
		// Страница заполнена ровно полосой наложения: следующую начинаем с проекции.
		return out, encodeRepoCursor(repoLaneProjection, cursor), nil
	}

	window, next, zerr := u.zot.ListRepositories(ctx, RepoListQuery{
		RegistryID: q.RegistryID,
		PageSize:   remaining,
		PageToken:  cursor,
	})
	if zerr != nil {
		return nil, "", zerr
	}
	// Строки наложения читаются ТОЛЬКО для имён окна. Прежде здесь стоял полный скан
	// каталога наложения — на КАЖДОЙ странице, до выбора полосы, вместе с метками:
	// обход реестра стоил O(N²/page_size) прочитанных строк, а при page_size=1
	// страница читала весь реестр. Имена окна известны — больше ничего и не нужно.
	byName, berr := u.configsForWindow(ctx, q.RegistryID, window)
	if berr != nil {
		return nil, "", berr
	}
	for _, r := range window {
		// Тот же merge, что у поштучного чтения (GetRepository): у одного объекта одна
		// публичная проекция, как бы его ни спросили. Он же решает судьбу repo БЕЗ
		// тегов — а такое решение возможно только здесь, где видны обе стороны:
		// строка наложения есть ⇒ ресурс жив и пустоту переживает (durable), строки
		// нет ⇒ имя без содержимого ресурсом не является и тенанту невидимо (nil).
		merged := mergeRepository(q.RegistryID, r.Name, byName[r.Name], r)
		if merged == nil {
			continue
		}
		out = append(out, merged)
	}
	if next == "" {
		return out, "", nil
	}
	return out, encodeRepoCursor(repoLaneProjection, next), nil
}

// configsForWindow — строки наложения ТОЛЬКО для имён окна проекции.
func (u *UseCase) configsForWindow(ctx context.Context, registryID string, window []*domain.Repository) (map[string]*domain.RepositoryConfig, error) {
	if len(window) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(window))
	for _, r := range window {
		names = append(names, r.Name)
	}
	configs, err := u.cfg.ConfigsByNames(ctx, registryID, names)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	byName := make(map[string]*domain.RepositoryConfig, len(configs))
	for _, c := range configs {
		byName[c.Name] = c
	}
	return byName, nil
}

// durableEmptyPage — страница полосы (1): строки наложения, которых НЕТ в движке.
// «Нет в движке» ⇒ тегов нет, поэтому projection-часть таких строк — нули, и поштучно
// опрашивать движок незачем (ровно это и делало прежнее объединение). Возвращает
// страницу + позицию следующей ("" — полоса исчерпана).
func (u *UseCase) durableEmptyPage(
	ctx context.Context, registryID string, size int64, cursor string,
) ([]*domain.Repository, string, error) {
	off, err := decodeRepoOffset(cursor)
	if err != nil {
		return nil, "", err
	}
	names, nerr := u.zot.ListRepositoryNames(ctx, registryID)
	if nerr != nil {
		return nil, "", nerr
	}
	// Отбор «строки наложения, которых нет в движке» ведёт БД: ей передаётся набор
	// имён движка, и она возвращает РОВНО окно. Прежняя реализация материализовала
	// ВЕСЬ каталог наложения (с метками) и нарезала окно из него в памяти — на каждой
	// странице, поэтому стоимость страницы не зависела от page_size и росла с размером
	// реестра. Курсор остаётся опаковым offset'ом по ОТФИЛЬТРОВАННОМУ набору и имён не
	// несёт (иначе он раскрывал бы имя скрытого репозитория, existence-oracle).
	page, perr := u.cfg.ListConfigsExcludingNames(ctx, registryID, names, off, int(size)+1)
	if perr != nil {
		return nil, "", mapRepoErr(perr)
	}
	hasMore := int64(len(page)) > size
	if hasMore {
		page = page[:size]
	}
	out := make([]*domain.Repository, 0, len(page))
	for _, c := range page {
		out = append(out, mergeRepository(registryID, c.Name, c, nil))
	}
	if hasMore {
		return out, strconv.Itoa(off + len(page)), nil
	}
	return out, "", nil
}

// Полосы объединения overlay ⊔ projection (порядок обхода — durable-empty, затем
// проекция движка).
const (
	repoLaneDurable    = "d"
	repoLaneProjection = "p"
)

// encodeRepoCursor кодирует (полоса, позиция) в опаковый токен. Имён не несёт: per-repo
// фильтр применяется ПОСЛЕ страницы, поэтому name-курсор раскрыл бы имя скрытого репо.
func encodeRepoCursor(lane, cursor string) string {
	return base64.StdEncoding.EncodeToString([]byte(lane + ":" + cursor))
}

// decodeRepoCursor разбирает токен в (полоса, позиция). Пустой токен — начало обхода
// (полоса наложения). Битый → ErrInvalidArg (маппинг в gRPC — на границе mapErr).
func decodeRepoCursor(token string) (string, string, error) {
	if token == "" {
		return repoLaneDurable, "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", "", fmt.Errorf("%w: invalid page_token", regerrors.ErrInvalidArg)
	}
	lane, cursor, found := strings.Cut(string(raw), ":")
	if !found || (lane != repoLaneDurable && lane != repoLaneProjection) {
		return "", "", fmt.Errorf("%w: invalid page_token", regerrors.ErrInvalidArg)
	}
	return lane, cursor, nil
}

// decodeRepoOffset разбирает позицию внутри полосы наложения (пусто → 0).
func decodeRepoOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	off, err := strconv.Atoi(cursor)
	if err != nil || off < 0 {
		return 0, fmt.Errorf("%w: invalid page_token", regerrors.ErrInvalidArg)
	}
	return off, nil
}

// ListTags возвращает проекцию тегов repo из zot.
func (u *UseCase) ListTags(ctx context.Context, q TagListQuery) ([]*domain.Tag, string, error) {
	if err := u.assertWired(); err != nil {
		return nil, "", err
	}
	return u.zot.ListTags(ctx, q)
}

// Stats возвращает инфра-статистику namespace (Internal-API).
func (u *UseCase) Stats(ctx context.Context, registryID string) (*domain.RegistryStats, error) {
	if err := u.assertWired(); err != nil {
		return nil, err
	}
	// malformed id → sync InvalidArgument первым стейтментом (parity с TriggerGC/Get);
	// без этого malformed id доходил бы до zot-бэкенда вместо fail-fast reject.
	if err := ValidateRegistryID(registryID); err != nil {
		return nil, err
	}
	return u.zot.Stats(ctx, registryID)
}
