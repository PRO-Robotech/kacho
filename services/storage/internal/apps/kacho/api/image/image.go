// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package image — use-case (бизнес-логика) ресурса Image (VM boot-образ, REGIONAL).
//
// Use-case слой чистой архитектуры: импортирует domain + порты (Reader/Writer,
// Geo/IAM peer-клиенты) + corelib operations; НЕ тянет pgx/grpc-transport.
// Публичные Get/List — read-only (sync); мутации Create/Update/Delete возвращают
// operation.Operation (async LRO): sync-фаза валидирует и пишет LRO-строку
// (done=false), фоновый corelib-worker выполняет доменную запись и финализирует
// (done=true, response=Image/Empty либо error). Клиент поллит OperationService.Get(id)
// до done. Create → state=CREATING: предмет операции — НАМЕРЕНИЕ (строка закоммичена,
// durable Operation.done, ban #9 — не гейтит downstream), а объект у бэкенда создаёт
// сверщик, он же объявляет образ READY. Исключение одно —
// InternalImageService.Register: объект внесён в хранилище вне облака и уже
// существует, поэтому зарегистрированный образ рождается готовым.
// InternalImageService.GetInternal (infra-проекция) — анкер data-plane.
package image

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
)

// Pagination — вход для List: cursor-пагинация (page_size + opaque page_token) +
// project-scope и filter=name.
type Pagination struct {
	PageSize  int64
	PageToken string
	ProjectID string
	// Filter — СЫРОЕ filter-выражение запроса (`name="x"` либо `name CONTAINS "x"`),
	// как его прислал вызывающий. Разбирает его use-case; репозиторий на это поле
	// НЕ смотрит.
	Filter string
	// FilterAST — результат разбора Filter, и ровно он предназначен репозиторию.
	//
	// Здесь прежде лежало голое ЗНАЧЕНИЕ (`p.Filter = ast.Value`), а оператор
	// оставался в узле и до SQL не доезжал — репозиторий, которому нечем было
	// отличить одно от другого, строил равенство для обоих, и `name CONTAINS "x"`
	// получал ответ про точное совпадение под 200 (#460). Узел несёт оператор с
	// собой, поэтому предикат эмитит он сам (ToSQLOn на колонке владельца), а не
	// вызывающий по памяти.
	FilterAST *filter.FilterAST
}

// ImageUpdate — резолвнутый набор mutable-изменений для Writer.Update. nil-поле →
// без изменения (COALESCE); LabelsSet различает «labels в маске» от «не трогать».
type ImageUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
}

// RegisterInput — то, что называет регистрирующий образ, УЖЕ внесённый в хранилище
// командой провайдера (InternalImageService.Register, system_admin @ cluster).
//
// Отдельный тип, а не domain.Image, — и это несущее решение: у зарегистрированного
// образа источника ВНУТРИ облака нет by construction. Приняв domain.Image, порт
// принимал бы поля источника, которые запись всё равно выбрасывает, то есть
// принято-и-проигнорировано на внутренней границе. Здесь их просто негде назвать.
//
// Размеры называет регистрирующий по той же причине: выводить их не из чего —
// источника, с которого их снимают на публичном Create, не существует.
type RegisterInput struct {
	ProjectID   string
	RegionID    string
	Name        string
	Description string
	Labels      map[string]string
	// BackendObject — имя объекта у бэкенда, ЕДИНСТВЕННОЕ место контракта, где оно
	// приходит извне. На всех прочих путях имя выводится (префикс установки +
	// неизменяемый идентификатор) и из запроса не принимается — принимая, сервис
	// позволил бы вызывающему адресовать чужой объект. Здесь выводить не из чего:
	// объект внесён ДО того, как у облака появилась строка, и его имя — факт
	// провайдера, а не наше решение.
	BackendObject string
	SizeBytes     int64
	MinDiskBytes  int64
}

// Reader — read-порт образов (Get/List + internal-проекция). CQRS-разделён с Writer.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Image, error)
	List(ctx context.Context, p Pagination) ([]*domain.Image, string, error)
	// GetInternal — full (infra) проекция Image, internal-only (:9091) — data-plane.
	GetInternal(ctx context.Context, id string) (*domain.Image, error)
}

// Writer — write-порт мутаций образов (Insert/Update/Delete).
type Writer interface {
	// Insert вставляет образ, сверяя источник с regionZones — зонами, которые
	// СОСТАВЛЯЮТ регион образа. Сверка идёт ВНУТРИ вставки-CAS (ban #10), см.
	// repo/pg.imageInsertCoherentSQL; несовпадение → 0 строк → hide-existence
	// "<Resource> <id> not found", байт-в-байт как настоящий miss.
	Insert(ctx context.Context, i *domain.Image, regionZones []string) (*domain.Image, []ownerregister.Registration, error)
	Update(ctx context.Context, id string, u ImageUpdate) (*domain.Image, []ownerregister.Registration, error)
	Delete(ctx context.Context, id string) error
	// Copy заводит КОПИЮ образа в другом регионе. targetZones — зоны целевого
	// региона по данным владельца географии: образ региональный, привязка
	// зональная, и адресатом годится любая действующая ревизия нужного класса
	// внутри региона.
	Copy(ctx context.Context, i *domain.Image, sourceID string, targetZones []string) (*domain.Image, error)
	// Register вносит строку об образе, объект которого у бэкенда УЖЕ существует:
	// состояние READY и наблюдение READY выставляет сама запись (см. её godoc),
	// потому что «создавать существующее» сверщику поручить нельзя.
	Register(ctx context.Context, i *domain.Image) (*domain.Image, []ownerregister.Registration, error)
}

// GeoClient — порт peer-валидации размещения через kacho-geo (fail-closed). Ребро
// storage→geo (one-way). Image — REGIONAL, поэтому валидируется регион (не зона,
// как Volume).
type GeoClient interface {
	EnsureRegionExists(ctx context.Context, regionID string) error

	// ZonesOfRegion возвращает зоны региона по данным ВЛАДЕЛЬЦА Geography.
	// Нужен, потому что источник образа (Volume) — ЗОНАЛЬНЫЙ, а образ —
	// РЕГИОНАЛЬНЫЙ: когерентность означает «зона источника ∈ регион образа», и
	// решает это сам insert-CAS, сверяя живую строку источника с этим набором.
	// Регион зоны НИКОГДА не выводится разбором имени (data-integrity.md).
	// Пир недоступен → Unavailable (fail-closed: непроверяемое предусловие не
	// считается выполненным).
	ZonesOfRegion(ctx context.Context, regionID string) ([]string, error)
}

// IAMClient — порт peer-валидации project_id через kaname (ProjectService.Get,
// fail-closed). Ребро storage→iam (one-way).
type IAMClient interface {
	EnsureProjectExists(ctx context.Context, projectID string) error
}

// ErrToStatus маппит доменную/repo sentinel-ошибку в transport-status, сохраняемый
// async-worker'ом в Operation.error. Инжектится composition root'ом. nil → identity.
type ErrToStatus func(error) error

// knownUpdateFields — mutable-поля Image.Update (update_mask discipline). Immutable
// (region_id/source_*/format) НЕ входят в known-set — immutable-switch отвергает их
// раньше конвенц-сообщением, а не generic «unknown field».
var knownUpdateFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"labels":      {},
}

// UseCase — бизнес-логика Image поверх CQRS-портов Reader/Writer, peer-клиентов,
// LRO-стека operations и инжектированного transport-mapper'а errStatus.
// QuotaGuard — совещательная полоса учёта числа ресурсов арендатора.
//
// Порт объявлен здесь, у того, кто им пользуется: реализация живёт в
// `apps/kacho/shared/quota` и заводит строки учёта на промахе. Полоса ничего НЕ
// решает — решение принимает атомарное списание триггера в той же транзакции,
// что вставка строки ресурса (ban #10). Она существует ради РАННЕГО отказа:
// без неё исчерпание предела наблюдается арендатором как «200 и операция,
// упавшая через секунду».
//
// nil-реализация означает «раннего отказа нет», а НЕ «предела нет»: место
// по-прежнему занимает триггер, и исчерпание приезжает отказом операции.
type QuotaGuard interface {
	Admit(ctx context.Context, projectID, kind string) error
}

type UseCase struct {
	// quota — совещательная полоса учёта числа ресурсов; nil → раннего отказа
	// нет, место по-прежнему занимает триггер. Инжектится WithQuota.
	quota     QuotaGuard
	reader    Reader
	writer    Writer
	geo       GeoClient
	iam       IAMClient
	ops       operations.Repo
	errStatus ErrToStatus
	registrar fgaregister.Registrar
	// installPrefix — префикс имени объектов этого развёртывания у бэкенда.
	// Инжектится WithInstallPrefix; без него имя не выводится, и создание образа
	// отвергается синхронно — молча создать объект без префикса нельзя, иначе
	// соседнее облако на том же кластере усыновило бы его.
	installPrefix string
	// dataPlane — объявлена ли плоскость данных. Тот же признак, что читает
	// проводка сверщика: два решения об одном предмете не должны разъезжаться.
	dataPlane bool
	// listFilter — per-object фильтр видимости страницы List (kaname
	// AuthorizeService.BatchCheck). nil → passthrough (dev / list-filter disabled;
	// production boot-guard такую посадку запрещает). Инжектится WithListFilter.
	listFilter *listnarrow.Narrower
}

// New собирает UseCase для Image. reader/writer — CQRS-разделённые порты; geo/iam —
// peer-клиенты cross-domain валидации; ops — corelib LRO-репозиторий; errStatus —
// инжектированный маппер sentinel→gRPC-status.
func New(reader Reader, writer Writer, geo GeoClient, iam IAMClient, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, geo: geo, iam: iam, ops: ops, errStatus: errStatus}
}

// WithRegistrar подключает синхронный owner-tuple registrar (парити vpc/Volume):
// после успешного Create-commit owner-grant регистрируется сразу (immediate
// анти-BOLA-резолв), без гонки с async register-drainer'ом. Best-effort: durable
// outbox-intent + register-drainer — at-least-once backstop, sync-ошибка НЕ валит
// Create. nil registrar → sync-путь пропускается (dev/no-iam).
func (u *UseCase) WithRegistrar(r fgaregister.Registrar) *UseCase {
	u.registrar = r
	return u
}

// WithInstallPrefix задаёт префикс установки, из которого выводится имя объекта у
// бэкенда.
//
// Он приходит из конфигурации процесса, а не из ресурса: это свойство РАЗВЁРТЫВАНИЯ,
// отличающее наши объекты от объектов соседнего облака в общем кластере хранилища.
// Пустой префикс боевой страж старта не пропускает — см. config.Validate.
// WithDataPlane объявляет наличие плоскости данных. Тот же признак, из которого
// композиционный корень поднимает сверщик, — чтобы решения не разъезжались.
func (u *UseCase) WithDataPlane(v bool) *UseCase { u.dataPlane = v; return u }

func (u *UseCase) WithInstallPrefix(p string) *UseCase {
	u.installPrefix = p
	return u
}

// WithListFilter подключает per-object фильтр видимости публичного List.
// WithQuota провязывает совещательную полосу учёта числа ресурсов.
func (u *UseCase) WithQuota(q QuotaGuard) *UseCase { u.quota = q; return u }

func (u *UseCase) WithListFilter(f *listnarrow.Narrower) *UseCase {
	u.listFilter = f
	return u
}

// registerOwnerTuple — best-effort синхронная регистрация owner-tuple после commit.
// Ошибка НЕ пробрасывается: durable outbox-intent уже в writer-TX, register-drainer
// применит его at-least-once. Логируем WARN.
func (u *UseCase) registerOwnerTuple(ctx context.Context, regs []ownerregister.Registration) {
	if u.registrar == nil || len(regs) == 0 {
		return
	}
	if err := u.registrar.Register(ctx, regs); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async drainer will apply",
			"object", regs[0].Tuple.Object, "err", err)
	}
}

// idInvalid — malformed img-id первым стейтментом (api-conventions.md): sync
// InvalidArgument "invalid image id '<X>'". well-formed-но-нет → NotFound (repo.Get).
func idInvalid(id string) error {
	if !ids.IsValid(id, domain.PrefixImage) {
		return fmt.Errorf("%w: invalid image id '%s'", storageerr.ErrInvalidArg, id)
	}
	return nil
}

// Get возвращает Image по id (malformed → sync InvalidArgument первым стейтментом).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Image, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	i, err := u.reader.Get(ctx, id)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return i, nil
}

// List возвращает образы проекта (cursor-пагинация). Порядок: format-validate
// (projectId-required → page_size → filter) выполняется ДО repo — детерминированно,
// независимо от grant-state (INV-7, api-conventions Gotcha): caller без грантов не
// получает 200 на garbage-token/page_size>1000, а именно InvalidArgument. listauthz
// row-filter (анти-BOLA) энфорсится gateway scope_extractor'ом {project,project_id}
// + project-scope repo-запросом (парити Volume; make audit-list-filter).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Image, string, error) {
	// projectId — обязательный scope публичного List (in-service backstop к gateway
	// scope_extractor {project,project_id}): пустой projectId вернул бы строки ВСЕХ
	// проектов, поэтому отвергаем СИНХРОННО первым стейтментом (INV-10).
	if p.ProjectID == "" {
		return nil, "", u.errStatus(fmt.Errorf("%w: projectId is required", storageerr.ErrInvalidArg))
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	// filter=name — whitelist через corelib filter; невалидное поле/форма →
	// InvalidArgument. Repo получает РАЗОБРАННЫЙ УЗЕЛ, а не выдернутое из него
	// значение: оператор — часть выражения, и потерять его здесь значит ответить
	// равенством на запрос подстроки (#460).
	if p.Filter != "" {
		ast, ferr := filter.Parse(p.Filter, []string{"name"})
		if ferr != nil {
			return nil, "", u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, ferr.Error()))
		}
		p.FilterAST = ast
	}
	imgs, next, err := u.reader.List(ctx, p)
	if err != nil {
		return nil, "", u.errStatus(err)
	}
	// Per-object видимость страницы (batched Check по её id на `viewer` — то же
	// отношение, что энфорсит Get) —
	// см. authzfilter package-doc: вопрос задаётся про ПРОЧИТАННУЮ страницу, а не
	// «перечисли всё разрешённое» (тот приём усекается пределом ListObjects).
	// Вызывается ПОСЛЕ валидации page_size (выше) и page_token (repo), поэтому
	// мусорный маркер даёт InvalidArgument независимо от grant-state.
	visible, ferr := listnarrow.Page(ctx, u.listFilter,
		authzfilter.ResourceTypeImage, authzfilter.ActionImageList, imgs,
		func(i *domain.Image) string { return i.ID })
	if ferr != nil {
		// Fail-closed: ошибка iam НИКОГДА не отдаёт нефильтрованную страницу.
		return nil, "", u.errStatus(ferr)
	}
	return visible, next, nil
}

// Create создаёт Image (async Operation). Малформ/невалидный вход отвергается
// СИНХРОННО (InvalidArgument: name / source exactly-one), посадка без префикса
// установки — UNAVAILABLE ДО обращений к соседям, cross-domain ссылки
// (region→geo, project→iam) валидируются на request-path fail-closed (peer
// Unavailable → UNAVAILABLE). Валидный вход → LRO-строка + worker (writer.Insert;
// state→CREATING, готовность объявит сверщик; source / partial UNIQUE(name) →
// Operation error). Источник (source_snapshot_id / source_volume_id) резолвится repo
// СТРОГО в проекте образа, в его размещении и ГОТОВЫМ (CAS): чужой приватный
// снапшот/том неотличим от несуществующего — Operation error FAILED_PRECONDITION
// "<Resource> <id> not found" (анти-BOLA hide-existence; иначе содержимое чужого тома
// утекало бы в образ caller'а), свой неготовый называется вслух ("… is not ready").
func (u *UseCase) Create(ctx context.Context, i *domain.Image) (*operations.Operation, error) {
	i.Placement = domain.ImagePlacementRegional
	if err := i.Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error()))
	}
	// Способность СЕРВИСА исполнить запрос проверяется ДО обращений к соседям:
	// посадка без префикса установки не создаст образ ни при каком вводе, и тратить
	// на это вызовы владельцам региона и проекта незачем.
	//
	// Код именно UNAVAILABLE: арендатор не сделал ничего неверного — сервис в этой
	// посадке неспособен. FAILED_PRECONDITION или INVALID_ARGUMENT отправили бы его
	// чинить собственный ввод, которого чинить нечего. Боевой страж старта такую
	// посадку не пропускает, поэтому ветка достижима лишь в неполной локальной
	// сборке — и молчать о ней нельзя.
	// Префикс требуется ТОЛЬКО когда объявлена плоскость данных: из него
	// выводится имя объекта у бэкенда. Её нет — выводить не для чего, объекта не
	// будет, и готовность наступает на фиксации записи. Требование префикса в
	// такой посадке беспредметно, а отказ Unavailable означал бы «сервис
	// недоступен» там, где он исправен и делает ровно то, что должен.
	if u.dataPlane && u.installPrefix == "" {
		return nil, status.Error(codes.Unavailable, "storage backend is not configured")
	}
	// Sync BVA at the request edge (parity with Volume, #61): reject over-limit
	// description (>256) / labels (>64) BEFORE any peer/DB call, so an over-limit
	// input returns INVALID_ARGUMENT instead of a 200 Operation.
	//
	// The error goes to the mapper AS IS. pkg/validate already answers in the
	// contract's own shape — INVALID_ARGUMENT, generic "invalid argument" message,
	// offending field in the google.rpc.BadRequest detail — and rebuilding it from
	// err.Error() took the text and dropped the detail, so the caller learned the
	// code and never learned WHICH field was rejected.
	if err := validate.Description("description", i.Description); err != nil {
		return nil, u.errStatus(err)
	}
	if err := validate.Labels("labels", i.Labels); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.geo.EnsureRegionExists(ctx, i.RegionID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.iam.EnsureProjectExists(ctx, i.ProjectID); err != nil {
		return nil, u.errStatus(err)
	}
	// Совещательная полоса учёта: ранний отказ по числу образов проекта.
	//
	// Провязана на ВСЕ ТРИ пути появления образа — заведение, регистрацию уже
	// лежащего у бэкенда объекта и копию в другой регион, — потому что все три
	// вставляют строку и все три списывают место триггером. Полоса на одном из
	// них давала бы ранний отказ выборочно: арендатор получал бы 429 при
	// заведении и «операция упала» при копии — на один и тот же предел.
	//
	// Проверка на nil стоит здесь, хотя реализация переживает nil-приёмник сама:
	// не провязанный порт — nil-ИНТЕРФЕЙС, и вызов метода на нём паникует.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, i.ProjectID, quota.KindImages); err != nil {
			return nil, u.errStatus(err)
		}
	}
	// Зоны региона образа — у владельца Geography. Их сверяет с живой строкой
	// источника САМ insert-CAS (placement-coherence, ban #10): образ REGIONAL,
	// его источник ZONAL, и они когерентны только если зона источника лежит в
	// этом регионе. Резолв идёт до Operation, поэтому недоступность geo видна
	// вызывающему синхронно (fail-closed), а не прячется в асинхронный отказ.
	//
	// Спрашиваем ТОЛЬКО когда источник вообще задан: без источника сверять нечего
	// (оба предиката CAS тривиально истинны на NULL), и платить за вызов пира —
	// и делать образ без источника заложником доступности geo — незачем. Решение
	// принимается по полю ЗАПРОСА, а не по состоянию БД, поэтому это не
	// check-then-act.
	var regionZones []string
	if i.SourceSnapshot != "" || i.SourceVolume != "" {
		zones, zerr := u.geo.ZonesOfRegion(ctx, i.RegionID)
		if zerr != nil {
			return nil, u.errStatus(zerr)
		}
		regionZones = zones
	}
	i.ID = ids.NewID(domain.PrefixImage)
	// Пустое имя не доживает до записи (#715). Подстановка стоит ЗДЕСЬ, после
	// чеканки идентификатора: умолчание выводится из него. Два безымянных образа
	// в одном проекте не спорят за UNIQUE(project,name) — идентификатор
	// уникален глобально by construction.
	i.Name = validate.NameOrDefault(i.Name, i.ID)
	// Имя объекта у бэкенда ВЫВОДИТСЯ здесь и нигде больше не принимается: оно
	// вычислимо арендатором (идентификатор он видит), поэтому авторизация на его
	// неугадываемость нигде не опирается, а идемпотентность повтора держится тем,
	// что повтор попадает в тот же объект.
	i.Backend.BackendObject = blockbackend.ObjectName(u.installPrefix, i.ID)
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Create image %s", i.ID),
		&storagev1.CreateImageMetadata{ImageId: i.ID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = i.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	created := *i
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, regs, derr := u.writer.Insert(ctx, &created, regionZones)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
		// регистрируем для immediate анти-BOLA-резолва (best-effort, post-commit;
		// backstop — async register-drainer at-least-once).
		u.registerOwnerTuple(ctx, regs)
		return marshalImage(res)
	})
	return &op, nil
}

// Register регистрирует образ, ВНЕСЁННЫЙ в хранилище командой провайдера вне облака
// (internal :9091, system_admin @ cluster).
//
// Метод обязан существовать, и причина не в удобстве: единственный источник ОС для
// машины — storage-образ, а публичный Create делает образ ТОЛЬКО из тома или снимка.
// На чистой установке нет ни того, ни другого, поэтому без регистрации первая машина
// не запускается by construction.
//
// Синхронный и отдаёт ресурс, а не Operation: за регистрацией нет длящейся работы —
// это запись строки о том, что у бэкенда уже есть. Обернуть её в операцию значило бы
// заставить администратора поллить готовое.
//
// Префикс установки здесь НЕ требуется — в отличие от Create. Имя объекта выбрал
// провайдер, и выводить нам нечего; требовать префикс значило бы гейтить регистрацию
// настройкой, которая на этом пути не участвует.
//
// Cross-domain ссылки (region→geo, project→iam) валидируются на request-path
// fail-closed: непроверенное предусловие не считается выполненным, поэтому строка о
// чужом объекте не заводится, пока владелец региона или проекта не ответил.
func (u *UseCase) Register(ctx context.Context, in RegisterInput) (*domain.Image, error) {
	if in.ProjectID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: image project_id is required", storageerr.ErrInvalidArg))
	}
	if in.RegionID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: image region_id is required", storageerr.ErrInvalidArg))
	}
	// Имя проверяется тем же валидатором, что и на публичном Create: у
	// зарегистрированного и у созданного образа ОДИН контракт имени, иначе арендатор
	// увидел бы два разных правила на одном поле.
	if err := domain.ImageName(in.Name).Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error()))
	}
	// Ошибка pkg/validate уходит наверх КАК ЕСТЬ: имя отвергнутого поля она кладёт в
	// google.rpc.BadRequest-детали, а пересборка через err.Error() их теряет.
	if err := validate.Description("description", in.Description); err != nil {
		return nil, u.errStatus(err)
	}
	if err := validate.Labels("labels", in.Labels); err != nil {
		return nil, u.errStatus(err)
	}
	// Обязательная по форме запроса ссылка несёт СВОЙ required-check: пустое имя
	// объекта уехало бы в запись и легло строкой, которая ни на что не указывает.
	if in.BackendObject == "" {
		return nil, u.errStatus(fmt.Errorf("%w: backend_object: required", storageerr.ErrInvalidArg))
	}
	if in.SizeBytes <= 0 {
		return nil, u.errStatus(fmt.Errorf("%w: Illegal argument size_bytes", storageerr.ErrInvalidArg))
	}
	if in.MinDiskBytes <= 0 {
		return nil, u.errStatus(fmt.Errorf("%w: Illegal argument min_disk_bytes", storageerr.ErrInvalidArg))
	}
	if err := u.geo.EnsureRegionExists(ctx, in.RegionID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.iam.EnsureProjectExists(ctx, in.ProjectID); err != nil {
		return nil, u.errStatus(err)
	}
	// Совещательная полоса учёта: ранний отказ по числу образов проекта.
	//
	// Провязана на ВСЕ ТРИ пути появления образа — заведение, регистрацию уже
	// лежащего у бэкенда объекта и копию в другой регион, — потому что все три
	// вставляют строку и все три списывают место триггером. Полоса на одном из
	// них давала бы ранний отказ выборочно: арендатор получал бы 429 при
	// заведении и «операция упала» при копии — на один и тот же предел.
	//
	// Проверка на nil стоит здесь, хотя реализация переживает nil-приёмник сама:
	// не провязанный порт — nil-ИНТЕРФЕЙС, и вызов метода на нём паникует.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, in.ProjectID, quota.KindImages); err != nil {
			return nil, u.errStatus(err)
		}
	}
	// Идентификатор чеканится ОТДЕЛЬНОЙ строкой, а не внутри литерала: из него
	// выводится имя по умолчанию, и внутри литерала на него сослаться нечем.
	imageID := ids.NewID(domain.PrefixImage)
	i := &domain.Image{
		ID:        imageID,
		ProjectID: in.ProjectID,
		// Пустое имя не доживает до записи и на ЭТОМ пути тоже (#715): правило
		// принадлежит записи, а не глаголу, а путей появления образа три.
		Name:         validate.NameOrDefault(in.Name, imageID),
		Description:  in.Description,
		Labels:       in.Labels,
		RegionID:     in.RegionID,
		Placement:    domain.ImagePlacementRegional,
		SizeBytes:    in.SizeBytes,
		MinDiskBytes: in.MinDiskBytes,
		Backend:      domain.Placement{BackendObject: in.BackendObject},
	}
	res, regs, err := u.writer.Register(ctx, i)
	if err != nil {
		return nil, u.errStatus(err)
	}
	// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
	// регистрируем для immediate анти-BOLA-резолва (best-effort, post-commit;
	// backstop — async register-drainer at-least-once).
	u.registerOwnerTuple(ctx, regs)
	return res, nil
}

// Update меняет mutable-поля Image (async Operation). Sync-фаза: malformed-id первым
// стейтментом → immutable-switch (ДО UpdateMask, api-conventions gotcha) → UpdateMask
// known-set → name-format. Пустой mask → full-object PATCH.
func (u *UseCase) Update(ctx context.Context, id string, mask []string, name, description string, labels map[string]string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	// immutable-switch ДО UpdateMask: known-set НЕ содержит immutable-полей, иначе
	// UpdateMask отверг бы их generic'ом «unknown field» вместо конвенц-сообщения.
	for _, p := range mask {
		switch p {
		case "region_id", "source_snapshot_id", "source_volume_id", "format", "placement_type", "size_bytes", "min_disk_bytes":
			return nil, u.errStatus(fmt.Errorf("%w: %s is immutable after Image.Create", storageerr.ErrInvalidArg, p))
		}
	}
	if err := validate.UpdateMask("update_mask", mask, knownUpdateFields); err != nil {
		return nil, err
	}
	upd, err := resolveUpdate(mask, name, description, labels)
	if err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Update image %s", id),
		&storagev1.UpdateImageMetadata{ImageId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, regs, derr := u.writer.Update(ctx, id, upd)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// Смена меток меняет ПРОЕКЦИЮ, которую читает селектор владельца прав,
		// поэтому обновлённое зеркало доставляется на пути запроса — ровно как
		// регистрация на Create. Durable-intent (та же writer-TX) остаётся
		// at-least-once backstop'ом, но ждать только его значит отдать ОТЗЫВ по
		// снятию метки глубине очереди (замер соседнего сервиса 2026-08-05:
		// 188–365 с при клиентском бюджете чтения-своих-записей 15 с).
		// Update без меток в маске проекции не меняет — регистрации нет.
		if upd.LabelsSet {
			u.registerOwnerTuple(ctx, regs)
		}
		return marshalImage(res)
	})
	return &op, nil
}

// Delete удаляет Image (async Operation). Malformed-id → sync InvalidArgument.
// Удаление образа, засевшего в томе, ПРОХОДИТ — volumes.source_image_id FK ON DELETE
// SET NULL (provenance, STOR-1-28: том цел, lineage очищается). Успех → response=Empty.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Delete image %s", id),
		&storagev1.DeleteImageMetadata{ImageId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		if derr := u.writer.Delete(ctx, id); derr != nil {
			return nil, u.errStatus(derr)
		}
		return anypb.New(&emptypb.Empty{})
	})
	return &op, nil
}

// ListOperations возвращает операции по конкретному Image (corelib-standard:
// resource_id-фильтр общей operations-таблицы). Malformed img-id → sync
// InvalidArgument (парити с Get).
func (u *UseCase) ListOperations(ctx context.Context, imageID string, p Pagination) ([]operations.Operation, string, error) {
	if err := idInvalid(imageID); err != nil {
		return nil, "", u.errStatus(err)
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.ops,
		operations.ListFilter{ResourceID: imageID, PageSize: size, PageToken: p.PageToken})
}

// GetInternal — full (infra) проекция Image (internal :9091) — data-plane.
func (u *UseCase) GetInternal(ctx context.Context, id string) (*domain.Image, error) {
	return u.reader.GetInternal(ctx, id)
}

// resolveUpdate резолвит mutable-изменения из mask + тела. Пустой mask → full-object
// PATCH (все mutable из тела). Непустой mask → только перечисленные поля.
//
// Каждое ПРИМЕНЯЕМОЕ поле валидируется по тем же правилам, что Create. Проверка
// стоит внутри ветки apply(...), а не перед ней, и это несущее решение: поле, не
// попавшее в маску, сервис игнорирует — отвергать запрос за значение, которое всё
// равно не будет записано, значит ввести новый дефект вместо исправленного. При
// пустой маске применяется всё тело, поэтому проверяется тоже всё.
//
// До этого description и labels не проверялись вовсе: переразмерное значение
// доезжало до UPDATE, ловилось images_description_check / images_labels_valid и
// возвращалось АСИНХРОННО в ошибке операции обобщённым «Illegal argument» — поздно
// и без имени поля. Ошибка pkg/validate уходит наверх КАК ЕСТЬ: имя поля она
// кладёт в google.rpc.BadRequest-детали, а пересборка через err.Error() их теряет.
func resolveUpdate(mask []string, name, description string, labels map[string]string) (ImageUpdate, error) {
	var u ImageUpdate
	apply := func(field string) bool {
		if len(mask) == 0 {
			return true // full-object PATCH
		}
		for _, m := range mask {
			if m == field {
				return true
			}
		}
		return false
	}
	// Решение «что делать с именем на правке» принимает ЕДИНСТВЕННАЯ функция
	// дерева (validate.NameOnUpdate): пять исходов маски и значения — правило
	// горизонтальное, оно про форму запроса, а не про предмет сервиса.
	//
	// Наружу отказ уходит контрактным ТОНОМ storage, а не ошибкой канона:
	// «Illegal argument name» — часть контракта (§1.7), он сам называет поле, и
	// его пинят кейсы чёрного ящика на пути правки. Канон отвечает generic'ом с
	// именем поля в деталях; отдать его как есть значило бы сменить наблюдаемое
	// у landed-кейсов. Пересборка через err.Error() запрещена отдельно — она
	// приклеила бы к сообщению обёртку gRPC.
	applyName, nerr := validate.NameOnUpdate("name", mask, name)
	if nerr != nil {
		return ImageUpdate{}, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, domain.ErrIllegalName)
	}
	if applyName {
		n := name
		u.Name = &n
	}
	if apply("description") {
		if err := validate.Description("description", description); err != nil {
			return ImageUpdate{}, err
		}
		d := description
		u.Description = &d
	}
	if apply("labels") {
		if err := validate.Labels("labels", labels); err != nil {
			return ImageUpdate{}, err
		}
		u.Labels = labels
		u.LabelsSet = true
	}
	return u, nil
}

// marshalImage упаковывает domain.Image в Operation.response через единый
// protoconv.Image (та же проекция, что handler и LRO-recovery — без дрейфа полей).
func marshalImage(i *domain.Image) (*anypb.Any, error) {
	return anypb.New(protoconv.Image(i))
}

// CopyInput — вход копирования образа в другой регион.
type CopyInput struct {
	// ProjectID — проект, в котором создаётся копия, и объект вопроса о правах.
	// Обязан совпадать с проектом источника (разбор — у snapshot.Copy).
	ProjectID      string
	ImageID        string
	TargetRegionID string
	Name           string
	Description    string
	Labels         map[string]string
}

// Copy копирует образ в другой регион.
//
// Регион образа неизменяем — как и зона тома, — поэтому распространение образа по
// регионам выражается копией, а не правкой. Создаётся НОВЫЙ образ; исходный не
// меняется ни одним полем.
//
// Зоны целевого региона резолвятся у ВЛАДЕЛЬЦА географии и никогда не выводятся
// разбором имени: имена региона и зоны — произвольные строки, а деривация разбором
// молча даёт пустой набор и превращает поиск адресата в отказ без причины.
func (u *UseCase) Copy(ctx context.Context, in CopyInput) (*operations.Operation, error) {
	if err := idInvalid(in.ImageID); err != nil {
		return nil, u.errStatus(err)
	}
	if in.ProjectID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: project_id: required", storageerr.ErrInvalidArg))
	}
	// Формат чужого id здесь НЕ проверяется (B4: формат — только у своих), и
	// существование проекта не спрашивается у iam: проект источника авторитетен,
	// а расхождение всё равно отвечает промахом ниже. Лишний вызов к соседу дал
	// бы ещё одну причину отказать на пути, где ответ уже известен.
	if in.TargetRegionID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: target_region_id: required", storageerr.ErrInvalidArg))
	}
	// Префикс требуется ТОЛЬКО когда объявлена плоскость данных: из него
	// выводится имя объекта у бэкенда. Её нет — выводить не для чего, объекта не
	// будет, и готовность наступает на фиксации записи. Требование префикса в
	// такой посадке беспредметно, а отказ Unavailable означал бы «сервис
	// недоступен» там, где он исправен и делает ровно то, что должен.
	if u.dataPlane && u.installPrefix == "" {
		return nil, status.Error(codes.Unavailable, "storage backend is not configured")
	}
	if err := validate.Description("description", in.Description); err != nil {
		return nil, u.errStatus(err)
	}
	if err := validate.Labels("labels", in.Labels); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.geo.EnsureRegionExists(ctx, in.TargetRegionID); err != nil {
		return nil, u.errStatus(err)
	}
	zones, zerr := u.geo.ZonesOfRegion(ctx, in.TargetRegionID)
	if zerr != nil {
		return nil, u.errStatus(zerr)
	}
	// Совещательная полоса учёта: ранний отказ по числу образов проекта.
	//
	// Провязана на ВСЕ ТРИ пути появления образа — заведение, регистрацию уже
	// лежащего у бэкенда объекта и копию в другой регион, — потому что все три
	// вставляют строку и все три списывают место триггером. Полоса на одном из
	// них давала бы ранний отказ выборочно: арендатор получал бы 429 при
	// заведении и «операция упала» при копии — на один и тот же предел.
	//
	// Проверка на nil стоит здесь, хотя реализация переживает nil-приёмник сама:
	// не провязанный порт — nil-ИНТЕРФЕЙС, и вызов метода на нём паникует.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, in.ProjectID, quota.KindImages); err != nil {
			return nil, u.errStatus(err)
		}
	}

	src, gerr := u.reader.Get(ctx, in.ImageID)
	if gerr != nil {
		return nil, u.errStatus(gerr)
	}
	if src.ProjectID != in.ProjectID {
		// Байт-в-байт тон промаха: чужая строка неотличима от отсутствующей.
		return nil, u.errStatus(fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, in.ImageID))
	}

	copyItem := &domain.Image{
		ID:          ids.NewID(domain.PrefixImage),
		ProjectID:   src.ProjectID,
		RegionID:    in.TargetRegionID,
		Placement:   domain.ImagePlacementRegional,
		Name:        in.Name,
		Description: in.Description,
		Labels:      in.Labels,
		Format:      src.Format,
		// Происхождение копии — НЕПОСРЕДСТВЕННЫЙ РОДИТЕЛЬ (см. тот же разбор в
		// копии снимка): вставка копии с самого начала писала `source_image_id`,
		// а наследование источника источника расходилось с этой строкой и
		// утверждало о копии неправду.
		SourceImageID: src.ID,
	}
	copyItem.Backend.BackendObject = blockbackend.ObjectName(u.installPrefix, copyItem.ID)
	// Копия НЕ проверяла имя вовсе: малформ доезжал до images_name_check и
	// возвращался АСИНХРОННО в ошибке операции, тогда как копия снимка отвергает
	// его синхронно. Один контракт, два исполнения — и то из них, что молчит,
	// заставляло вызывающего искать причину в чужом слое.
	if verr := copyItem.Validate(); verr != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, verr.Error()))
	}
	// Подстановка — ПОСЛЕ проверки формы и до записи: копия минтит свой
	// идентификатор, значит и своё умолчание (#715).
	copyItem.Name = validate.NameOrDefault(copyItem.Name, copyItem.ID)

	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Copy image %s to region %s", in.ImageID, in.TargetRegionID),
		&storagev1.CopyImageMetadata{ImageId: copyItem.ID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = copyItem.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	source := in.ImageID
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, derr := u.writer.Copy(ctx, copyItem, source, zones)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalImage(res)
	})
	return &op, nil
}
