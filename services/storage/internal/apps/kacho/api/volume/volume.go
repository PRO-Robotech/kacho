// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package volume — use-case (бизнес-логика) ресурса Volume.
//
// Use-case слой чистой архитектуры: импортирует domain + порты (Reader/Writer,
// Geo/IAM peer-клиенты) + corelib operations; НЕ тянет pgx/grpc-transport.
// Публичные Get/List — read-only (sync); мутации Create/Update/Delete возвращают
// operation.Operation (async LRO): sync-фаза валидирует и пишет LRO-строку
// (done=false), фоновый corelib-worker выполняет доменную запись и финализирует
// (done=true, response=Volume/Empty либо error). Клиент поллит
// OperationService.Get(id) до done. Internal Attach/Detach/ListAttachments (:9091,
// sync CAS) реализованы (S2); GetInternal (infra-проекция) — анкер data-plane (§0.3).
package volume

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
// project-scope и filter=name (listauthz-фильтрация энфорсится authz-слоем).
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

// VolumeUpdate — резолвнутый набор mutable-изменений для Writer.Update. nil-поле →
// без изменения (COALESCE); LabelsSet различает «labels в маске» от «не трогать».
type VolumeUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
	SizeBytes   *int64
}

// Reader — read-порт томов (Get/List + internal-проекции). CQRS-разделён с Writer.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Volume, error)
	List(ctx context.Context, p Pagination) ([]*domain.Volume, string, error)
	// GetInternal — full (infra) проекция Volume, internal-only (:9091) — S2.
	GetInternal(ctx context.Context, id string) (*domain.Volume, error)
	// ListAttachments — батч-чтение attachments по instance_id (compute-mirror) — S2.
	ListAttachments(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error)
}

// Writer — write-порт мутаций томов (Insert/Update/Delete + attach/detach CAS).
// Update — атомарный размер-CAS increase-only + mutable COALESCE (data-integrity.md),
// НЕ software TOCTOU.
type Writer interface {
	// Insert — zoneRegionID: регион ЗОНЫ тома, разрешённый владельцем Geography.
	// Участвует в атомарной image-полосе CAS (ZONAL-том обязан лежать в регионе
	// REGIONAL-образа). Пусто → образ-полоса не матчится (fail-closed).
	Insert(ctx context.Context, v *domain.Volume, zoneRegionID string) (*domain.Volume, []ownerregister.Registration, error)
	Update(ctx context.Context, id string, u VolumeUpdate) (*domain.Volume, []ownerregister.Registration, error)
	// ChangeDiskType назначает ЖЕЛАЕМУЮ ревизию привязки целевого класса. Данные
	// переносит сверщик — этот вызов лишь фиксирует намерение.
	ChangeDiskType(ctx context.Context, id, diskTypeID string) (*domain.Volume, error)
	Delete(ctx context.Context, id string) error
	Attach(ctx context.Context, a *domain.VolumeAttachment) error
	Detach(ctx context.Context, volumeID, instanceID string) error
}

// GeoClient — порт peer-валидации zone_id через kacho-geo (ZoneService.Get,
// fail-closed). Ребро storage→geo (one-way).
type GeoClient interface {
	EnsureZoneExists(ctx context.Context, zoneID string) error
	// RegionOfZone возвращает region_id зоны (geo.v1.ZoneService.Get →
	// `Zone.region_id`). Регион НИКОГДА не выводится из имени зоны — имена
	// региона и зоны произвольны, единственный авторитет — владелец Geography.
	// Нужен для placement-coherence ZONAL-тома с REGIONAL (anycast) образом.
	RegionOfZone(ctx context.Context, zoneID string) (string, error)
}

// IAMClient — порт peer-валидации project_id через kaname (ProjectService.Get,
// fail-closed). Ребро storage→iam (one-way).
type IAMClient interface {
	EnsureProjectExists(ctx context.Context, projectID string) error
}

// ErrToStatus маппит доменную/repo sentinel-ошибку в transport-status, сохраняемый
// async-worker'ом в Operation.error. Инжектится composition root'ом
// (serviceerr.ToStatus). Пустой (nil) → identity.
type ErrToStatus func(error) error

// knownUpdateFields — mutable-поля Volume.Update (update_mask discipline).
// Immutable-поля НЕ входят в known-set (immutable-switch отвергает их раньше
// конвенц-сообщением, а не generic «unknown field»).
var knownUpdateFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"labels":      {},
	"size_bytes":  {},
}

// UseCase — бизнес-логика Volume поверх CQRS-портов Reader/Writer, peer-клиентов,
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
	// registrar — синхронная регистрация owner-tuple в kaname после commit
	// (immediate анти-BOLA-резолв; nil → sync-путь пропускается, остаётся async
	// register-drainer как at-least-once backstop). Инжектится WithRegistrar.
	registrar fgaregister.Registrar
	// installPrefix — префикс имени объектов этого развёртывания у бэкенда.
	// Инжектится WithInstallPrefix; без него имя не выводится, и создание тома
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
	// instanceGate — вопрос модели прав про ВТОРОЙ объект запросов Attach/Detach:
	// инстанс, в чей набор привязок пишется (или из чьего удаляется) строка. nil →
	// спрашивать негде, и это ОТКАЗ, не passthrough (см. requireInstanceControl).
	// Инжектится WithInstanceGate.
	instanceGate *listnarrow.Narrower
}

// New собирает UseCase для Volume. reader/writer — CQRS-разделённые порты;
// geo/iam — peer-клиенты cross-domain валидации; ops — corelib LRO-репозиторий;
// errStatus — инжектированный маппер sentinel→gRPC-status.
func New(reader Reader, writer Writer, geo GeoClient, iam IAMClient, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, geo: geo, iam: iam, ops: ops, errStatus: errStatus}
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2, парити vpc):
// после успешного Create-commit owner-grant регистрируется сразу, чтобы public
// Get/Update/Delete и internal Attach/Detach на свежий том разрешались без гонки с
// async drainer'ом. Best-effort: durable outbox-intent + register-drainer —
// at-least-once backstop, поэтому sync-ошибка НЕ валит Create (мутатор ban #9 async).
// nil registrar → sync-путь пропускается (dev/no-iam).
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

// WithInstanceGate подключает вопрос модели прав про ИНСТАНС — второй объект
// запросов Attach/Detach. См. requireInstanceControl.
func (u *UseCase) WithInstanceGate(g *listnarrow.Narrower) *UseCase {
	u.instanceGate = g
	return u
}

// registerOwnerTuple — best-effort синхронная регистрация owner-tuple после commit.
// Ошибка НЕ пробрасывается: durable outbox-intent уже записан в writer-TX, а
// register-drainer применит его at-least-once (idempotent). Логируем WARN, чтобы
// потерянная sync-регистрация была видна (async backstop подхватит).
func (u *UseCase) registerOwnerTuple(ctx context.Context, regs []ownerregister.Registration) {
	if u.registrar == nil || len(regs) == 0 {
		return
	}
	if err := u.registrar.Register(ctx, regs); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async drainer will apply",
			"object", regs[0].Tuple.Object, "err", err)
	}
}

// idInvalid — malformed vol-id первым стейтментом (api-conventions.md): sync
// InvalidArgument "invalid volume id '<X>'". well-formed-но-нет → NotFound (repo.Get).
func idInvalid(id string) error {
	if !ids.IsValid(id, domain.PrefixVolume) {
		return fmt.Errorf("%w: invalid volume id '%s'", storageerr.ErrInvalidArg, id)
	}
	return nil
}

// Get возвращает Volume по id (malformed → sync InvalidArgument первым стейтментом).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Volume, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	v, err := u.reader.Get(ctx, id)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return v, nil
}

// List возвращает тома (cursor-пагинация; garbage page_size → InvalidArgument;
// filter=name whitelisted через corelib filter).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Volume, string, error) {
	// projectId — обязательный scope публичного List (in-service backstop к gateway
	// scope_extractor {project,project_id}): пустой projectId вернул бы строки ВСЕХ
	// проектов (repo сужает лишь при ProjectID!=""), поэтому отвергаем СИНХРОННО
	// первым стейтментом — кросс-проектной утечки нет by construction (INV-10;
	// docs/architecture/overview.md; acceptance CS1-S1-13/GAP-C).
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
	vols, next, err := u.reader.List(ctx, p)
	if err != nil {
		return nil, "", u.errStatus(err)
	}
	// Per-object видимость: страница уже прочитана курсором — спрашиваем kaname
	// БАТЧЕМ про её id (`viewer` — то же отношение, что энфорсит Get; read==enforce). Обратный порядок
	// («перечисли все разрешённые id → сузь ими SQL») запрещён: перечисление
	// разрешённого усекалось без продолжения, и собственный ресурс тенанта молча
	// выпадал за префикс. Движок, у которого этот предел был серверным, снят, но
	// запрет остаётся: вопрос «перечисли вселенную» неограничен по построению, а
	// его ответ страницей не является. Побочный эффект: страница может вернуться
	// НЕПОЛНОЙ — это нормально для cursor-пагинации, next_page_token берётся от
	// последней ПРОСМОТРЕННОЙ строки, поэтому обход без пропусков не ломается.
	//
	// Вызывается ПОСЛЕ валидации page_size (выше) и page_token (repo) — мусорный
	// маркер страницы даёт InvalidArgument независимо от grant-state, а не пустую
	// страницу (api-conventions.md, security.md §7).
	visible, ferr := listnarrow.Page(ctx, u.listFilter,
		authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList, vols,
		func(v *domain.Volume) string { return v.ID })
	if ferr != nil {
		// Fail-closed: ошибка iam НИКОГДА не отдаёт нефильтрованную страницу.
		return nil, "", u.errStatus(ferr)
	}
	return visible, next, nil
}

// Create создаёт Volume (async Operation). Малформ/невалидный вход отвергается
// СИНХРОННО (InvalidArgument: size/name), cross-domain ссылки (zone→geo,
// project→iam) валидируются на request-path fail-closed (peer Unavailable →
// UNAVAILABLE). Для boot-тома дополнительно резолвится РЕГИОН зоны (geo) —
// placement-coherence с REGIONAL (anycast) образом энфорсится атомарно внутри
// insert-CAS (зона тома ∈ регион образа; migration 0007). Валидный вход → LRO-строка + worker (writer.Insert; state→READY
// сразу; disk_type FK → Operation error). Источник тома (source_snapshot_id /
// source_image_id) резолвится repo СТРОГО в проекте тома (project-coherent CAS):
// чужой приватный снапшот/образ неотличим от несуществующего — Operation error
// FAILED_PRECONDITION "<Resource> <id> not found" (анти-BOLA hide-existence).
func (u *UseCase) Create(ctx context.Context, v *domain.Volume) (*operations.Operation, error) {
	if err := v.Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error()))
	}
	// Способность СЕРВИСА исполнить запрос проверяется ДО обращений к соседям:
	// посадка без префикса установки не создаст тома ни при каком вводе, и тратить
	// на это вызовы к владельцам зоны и проекта незачем.
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
	// Sync BVA at the request edge (parity with Image, #61): reject over-limit
	// description (>256) / labels (>64) BEFORE any peer/DB call.
	//
	// The error goes to the mapper AS IS. pkg/validate already answers in the
	// contract's own shape — INVALID_ARGUMENT, generic "invalid argument" message,
	// offending field in the google.rpc.BadRequest detail — and rebuilding it from
	// err.Error() took the text and dropped the detail, so the caller learned the
	// code and never learned WHICH field was rejected. (It also pasted gRPC's wire
	// framing into the message: "rpc error: code = ... desc = ...".)
	if err := validate.Description("description", v.Description); err != nil {
		return nil, u.errStatus(err)
	}
	if err := validate.Labels("labels", v.Labels); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.geo.EnsureZoneExists(ctx, v.ZoneID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.iam.EnsureProjectExists(ctx, v.ProjectID); err != nil {
		return nil, u.errStatus(err)
	}
	// Совещательная полоса учёта: ранний отказ по числу ресурсов вида ДО
	// создания операции. Стоит ПОСЛЕ проверки существования проекта — заводить
	// строки учёта проекту, которого нет, значило бы материализовать предел на
	// имя, а не на арендатора; и ПЕРЕД операцией — иначе исчерпание предела
	// наблюдалось бы как успешный вызов с упавшей операцией.
	//
	// Проверка на nil стоит ЗДЕСЬ, хотя реализация переживает nil-приёмник
	// сама. Это разные пустоты: не провязанный порт — nil-ИНТЕРФЕЙС, и вызов
	// метода на нём паникует, тогда как `*Guard`, положенный в интерфейс,
	// интерфейсу не равен nil и до тела доходит. Закрыты обе, потому что
	// первая означала бы «сервис не работает», а не «нет раннего отказа».
	if u.quota != nil {
		if err := u.quota.Admit(ctx, v.ProjectID, quota.KindVolumes); err != nil {
			return nil, u.errStatus(err)
		}
	}
	// Регион ЗОНЫ тома — у владельца Geography (из имени зоны он не выводится).
	// Нужен, только когда том засевается образом: Image REGIONAL (anycast),
	// Volume ZONAL, и зона обязана лежать в регионе образа. Резолвится на
	// request-path, fail-closed; энфорсится атомарно внутри insert-CAS.
	zoneRegionID := ""
	if v.SourceImage != "" {
		region, rerr := u.geo.RegionOfZone(ctx, v.ZoneID)
		if rerr != nil {
			return nil, u.errStatus(rerr)
		}
		zoneRegionID = region
	}
	v.ID = ids.NewID(domain.PrefixVolume)
	// Пустое имя не доживает до записи (#715): ресурса без имени не бывает.
	// Подстановка стоит ЗДЕСЬ, а не в домене, потому что умолчание выводится из
	// идентификатора, а идентификатор чеканится строкой выше — до неё выводить
	// было не из чего. Отсюда же и то, что два безымянных тома в одном проекте
	// не спорят за UNIQUE(project,name): идентификатор глобально уникален by
	// construction, значит и производное имя тоже.
	v.Name = validate.NameOrDefault(v.Name, v.ID)
	v.Backend.BackendObject = blockbackend.ObjectName(u.installPrefix, v.ID)
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Create volume %s", v.ID),
		&storagev1.CreateVolumeMetadata{VolumeId: v.ID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = v.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	created := *v
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, regs, derr := u.writer.Insert(ctx, &created, zoneRegionID)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
		// регистрируем для immediate анти-BOLA-резолва (best-effort, post-commit;
		// backstop — async register-drainer at-least-once).
		u.registerOwnerTuple(ctx, regs)
		return marshalVolume(res)
	})
	return &op, nil
}

// Update меняет mutable-поля Volume (async Operation). Sync-фаза: malformed-id
// первым стейтментом → immutable-switch (ДО UpdateMask, api-conventions gotcha) →
// UpdateMask known-set → name-format. Пустой mask → full-object PATCH (immutable из
// тела silently игнорируются). Async: writer.Update (size-CAS increase-only →
// Operation error "Volume size can only be increased").
func (u *UseCase) Update(ctx context.Context, id string, mask []string, name, description string, labels map[string]string, sizeBytes int64) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	// immutable-switch ДО UpdateMask: known-set НЕ содержит immutable-полей, иначе
	// UpdateMask отверг бы их generic'ом «unknown field» вместо конвенц-сообщения.
	for _, p := range mask {
		switch p {
		case "zone_id", "disk_type_id", "block_size", "source_snapshot_id", "source_image_id", "used_by", "attachments":
			return nil, u.errStatus(fmt.Errorf("%w: %s is immutable after Volume.Create", storageerr.ErrInvalidArg, p))
		}
	}
	if err := validate.UpdateMask("update_mask", mask, knownUpdateFields); err != nil {
		return nil, err
	}
	upd, err := resolveUpdate(mask, name, description, labels, sizeBytes)
	if err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Update volume %s", id),
		&storagev1.UpdateVolumeMetadata{VolumeId: id})
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
		return marshalVolume(res)
	})
	return &op, nil
}

// Delete удаляет Volume (async Operation). Malformed-id → sync InvalidArgument.
// Привязанный том → FK RESTRICT → Operation error FailedPrecondition
// "Volume <id> is in use" (§3.6). Успех → response=Empty.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Delete volume %s", id),
		&storagev1.DeleteVolumeMetadata{VolumeId: id})
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

// ListOperations возвращает операции по конкретному Volume (corelib-standard:
// resource_id-фильтр общей operations-таблицы). Malformed vol-id → sync
// InvalidArgument (парити с Get).
func (u *UseCase) ListOperations(ctx context.Context, volumeID string, p Pagination) ([]operations.Operation, string, error) {
	if err := idInvalid(volumeID); err != nil {
		return nil, "", u.errStatus(err)
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.ops,
		operations.ListFilter{ResourceID: volumeID, PageSize: size, PageToken: p.PageToken})
}

// requireInstanceControl — вопрос модели прав про ВТОРОЙ объект запроса: инстанс.
//
// Attach/Detach называют ДВА ресурса с РАЗНЫМИ владельцами — том и машину, — а
// per-RPC Check интерсептора задаётся ровно об одном (`editor` на
// `storage_volume:<volume_id>`; та же запись в каталоге шлюза). Про машину не
// спрашивал никто: она приходит из самоописывающегося payload'а, и CAS сверяет
// зону/проект СВОЕЙ строки томов, а не права на названную машину. Правдивость
// payload'а держалась лишь на том, что его выводит compute из своей строки, уже
// проверив `v_update` на инстансе, — то есть составной путь был защищён, а прямой
// вызов внутреннего листенера нет.
//
// Спрашивается ровно то, чем compute гейтит свои AttachDisk/DetachDisk, — модель
// прав общая для всех доменов, поэтому вызова в compute не происходит и
// ацикличность держится (см. authzfilter.ResourceTypeComputeInstance).
//
// Три ветки различаются намеренно:
//   - "" субъект (identity не извлечена, в т.ч. system-принципал) → «не знаю, кто
//     ты», а не «доверенный»: отказ БЕЗУСЛОВНЫЙ, модель не спрашивается;
//   - гейт не подключён → «спросить негде» тоже не «да»: отказ. Привяжи мы проверку
//     к наличию гейта — осталась бы посадка, где мутация в чужую машину проходит
//     молча, то есть ровно исходная дыра (production boot-guard такую посадку
//     запрещает, но гейт защиты не должен зависеть от того, что кто-то помнит про
//     boot-guard);
//   - ошибка модели → fail-closed отказ как есть: недоступный ответ не есть «да».
//
// Порядок несущий: отказ приходит ДО writer'а. Отказ ПОСЛЕ мутации оставил бы строку
// в наборе привязок чужой машины — вместе с занятым загрузочным слотом.
func (u *UseCase) requireInstanceControl(ctx context.Context, instanceID, action string, relations []string) error {
	// Обязательная по форме запроса ссылка несёт СВОЙ required-check: пустая строка
	// иначе уехала бы в вопрос про объект `compute_instance:` и вернулась бы отказом
	// прав на ресурс, которого вызывающий не называл.
	if instanceID == "" {
		return u.errStatus(fmt.Errorf("%w: instance_id: required", storageerr.ErrInvalidArg))
	}
	// Личность вызывающего и провязанность модели решает та же функция, что и на
	// списках: два ответа на один вопрос разъезжаются молча.
	allowed, err := listnarrow.AllowedOnObject(ctx, u.instanceGate,
		authzfilter.ResourceTypeComputeInstance, action, relations, instanceID)
	if err != nil {
		if _, isStatus := status.FromError(err); isStatus {
			return err
		}
		return u.errStatus(err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

// attachRelations / detachRelations — принимаемые отношения на инстансе.
// Привязка меняет машину (`v_update`); отвязка принимает ДОПОЛНИТЕЛЬНО право сноса
// (`v_delete`), потому что шаг освобождения томов идёт внутри удаления машины под
// личностью инициатора — см. authzfilter.RelationInstanceDelete.
var (
	attachRelations = []string{authzfilter.RelationInstanceUpdate}
	detachRelations = []string{authzfilter.RelationInstanceUpdate, authzfilter.RelationInstanceDelete}
)

// Attach — атомарный CAS-insert строки volume_attachments (internal :9091, §3.2).
// Malformed vol-id → sync InvalidArgument первым стейтментом (парити с Get); затем
// вопрос прав про ИНСТАНС (второй объект запроса, см. requireInstanceControl) — ДО
// записи. Успех → обновлённый Volume (derived IN_USE) для AttachVolumeResponse.
// Sync (CAS мгновенный); tenant-мутация остаётся async через compute-AttachDisk
// (ban #9 не нарушен).
func (u *UseCase) Attach(ctx context.Context, a *domain.VolumeAttachment) (*domain.Volume, error) {
	if err := idInvalid(a.VolumeID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.requireInstanceControl(ctx, a.InstanceID, authzfilter.ActionVolumeAttach, attachRelations); err != nil {
		return nil, err
	}
	if err := u.writer.Attach(ctx, a); err != nil {
		return nil, u.errStatus(err)
	}
	v, err := u.reader.Get(ctx, a.VolumeID)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return v, nil
}

// Detach — идемпотентное удаление строки volume_attachments (internal :9091, §3.3).
// Malformed vol-id → sync InvalidArgument; затем вопрос прав про ИНСТАНС, с чьего
// набора снимается привязка (ДО удаления строки). Успех → обновлённый Volume
// (derived AVAILABLE) для DetachVolumeResponse.
func (u *UseCase) Detach(ctx context.Context, volumeID, instanceID string) (*domain.Volume, error) {
	if err := idInvalid(volumeID); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.requireInstanceControl(ctx, instanceID, authzfilter.ActionVolumeDetach, detachRelations); err != nil {
		return nil, err
	}
	if err := u.writer.Detach(ctx, volumeID, instanceID); err != nil {
		return nil, u.errStatus(err)
	}
	v, err := u.reader.Get(ctx, volumeID)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return v, nil
}

// ListAttachments — батч-чтение привязок том↔инстанс по instance_id (internal :9091,
// зеркало compute для Instance.Get/List; не N+1) — S2.
//
// Авторизуется ЗДЕСЬ, на уровне данных: инстансы называет ВЫЗЫВАЮЩИЙ, а ответ
// касается томов, у каждого из которых свой владелец, — единичного объекта, про
// который можно спросить заранее, у этого RPC нет. Прежний per-RPC вопрос («viewer
// на синглтоне cluster») относился к глобальному справочнику и пропускал КАЖДОГО
// аутентифицированного субъекта, отдавая привязки любых названных инстансов из чужих
// проектов и аккаунтов (см. полосу `scope_filtered` в каталоге прав).
//
// Три случая субъекта различаются намеренно:
//   - реальный `user:`/`service_account:` → спрашиваем модель про ИНСТАНСЫ, которые
//     назвал вызывающий (`viewer` на `compute_instance:<id>`), ДО чтения строк. Сужение
//     по видимости ТОМА было первой попыткой и ОТМЕНЕНО: привязку, которую сервис
//     отказывался вернуть, цикл отцепления пропускал, а строку инстанса удалял
//     безусловно — том оставался занят навсегда. Ответ поэтому «всё или ничего» на
//     инстанс (см. код ниже, он говорил ровно это, пока эта строка утверждала обратное);
//   - "" (identity не извлечена, в т.ч. system-принципал) → это «не знаю, кто ты», а
//     НЕ «доверенный»: fail-closed, пустой результат, строки даже не читаются;
//   - ошибка фильтра → fail-closed отказ: недоступный ответ модели не есть ответ «да».
//
// Пустой субъект отсекается БЕЗУСЛОВНО — в отличие от публичных List, где тот же
// guard живёт внутри FilterVisiblePage и потому не срабатывает при неподключённом
// фильтре. Разница не косметическая: за публичными List остаётся per-RPC Check,
// который сам отвергает вызывающего без принципала, а этот RPC помечен ScopeFiltered,
// то есть Check за него не задаётся вовсе. Привяжи мы fail-closed к наличию фильтра —
// посадка без фильтра отдавала бы привязки всего кластера кому угодно, что ровно та
// дыра, ради которой сюда пришли.
func (u *UseCase) ListAttachments(ctx context.Context, instanceIDs []string) ([]*domain.VolumeAttachment, error) {
	// Пустой ответ здесь означал бы «привязок нет», а единственные потребители этого
	// RPC действуют по ответу РАЗРУШИТЕЛЬНО (см. godoc выше). Поэтому обе линии —
	// безымянный вызывающий и непровязанная модель — отвечают ОТКАЗОМ, и решает их
	// одна функция общего фундамента.
	if err := listnarrow.Precheck(ctx, u.listFilter); err != nil {
		return nil, err
	}
	// Спрашиваем про ИНСТАНСЫ, которые назвал вызывающий, а не про тома. Ответ
	// становится «всё или ничего» на инстанс, и это несущее свойство: снос видит
	// ВСЕ привязки инстанса, который вправе снести, а на чужой инстанс не получает
	// ни строки.
	visibleInstances, ferr := listnarrow.Page(ctx, u.listFilter,
		authzfilter.ResourceTypeComputeInstance, authzfilter.ActionAttachmentsList,
		instanceIDs, func(id string) string { return id })
	if ferr != nil {
		// Fail-closed: ошибка iam НИКОГДА не отдаёт нефильтрованную страницу.
		return nil, u.errStatus(ferr)
	}
	if len(visibleInstances) == 0 {
		return nil, nil
	}
	att, err := u.reader.ListAttachments(ctx, visibleInstances)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return att, nil
}

// GetInternal — full (infra) проекция Volume (internal :9091) — S2/data-plane.
func (u *UseCase) GetInternal(ctx context.Context, id string) (*domain.Volume, error) {
	return u.reader.GetInternal(ctx, id)
}

// resolveUpdate резолвит mutable-изменения из mask + тела. Пустой mask →
// full-object PATCH (все mutable из тела; size применяется лишь если >0 — 0 не
// «уменьшение до нуля», а «не задано»). Непустой mask → только перечисленные поля.
//
// Каждое ПРИМЕНЯЕМОЕ поле валидируется по тем же правилам, что Create. Проверка
// стоит внутри ветки apply(...), а не перед ней, и это несущее решение: поле, не
// попавшее в маску, сервис игнорирует — отвергать запрос за значение, которое всё
// равно не будет записано, значит ввести новый дефект вместо исправленного. При
// пустой маске применяется всё тело, поэтому проверяется тоже всё.
//
// До этого description и labels не проверялись вовсе: переразмерное значение
// доезжало до UPDATE, ловилось volumes_description_check / volumes_labels_valid и
// возвращалось АСИНХРОННО в ошибке операции обобщённым «Illegal argument» — поздно
// и без имени поля. Ошибка pkg/validate уходит наверх КАК ЕСТЬ: имя поля она
// кладёт в google.rpc.BadRequest-детали, а пересборка через err.Error() их теряет.
func resolveUpdate(mask []string, name, description string, labels map[string]string, sizeBytes int64) (VolumeUpdate, error) {
	var u VolumeUpdate
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
		return VolumeUpdate{}, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, domain.ErrIllegalName)
	}
	if applyName {
		n := name
		u.Name = &n
	}
	if apply("description") {
		if err := validate.Description("description", description); err != nil {
			return VolumeUpdate{}, err
		}
		d := description
		u.Description = &d
	}
	if apply("labels") {
		if err := validate.Labels("labels", labels); err != nil {
			return VolumeUpdate{}, err
		}
		u.Labels = labels
		u.LabelsSet = true
	}
	if apply("size_bytes") {
		if len(mask) == 0 && sizeBytes <= 0 {
			// full-patch без явного размера — не трогаем (0 не значит «shrink to 0»).
		} else {
			s := sizeBytes
			u.SizeBytes = &s
		}
	}
	return u, nil
}

// marshalVolume упаковывает domain.Volume в Operation.response через единый
// protoconv.Volume (та же проекция, что handler и LRO-recovery — без дрейфа полей).
func marshalVolume(v *domain.Volume) (*anypb.Any, error) {
	return anypb.New(protoconv.Volume(v))
}

// ChangeDiskTypeInput — вход смены класса тома.
type ChangeDiskTypeInput struct {
	VolumeID   string
	DiskTypeID string
}

// ChangeDiskType переводит том в другой класс диска.
//
// # Почему это ОТДЕЛЬНЫЙ глагол, а не поле правки
//
// Это перемещение данных, а не изменение поля: оно длится, может отказать на
// половине и меняет физическое расположение. Пропусти его через общую правку — и
// запрос, менявший метку, мог бы задеть размещение терабайтов; а маска правки,
// которая обязана быть предсказуемой, стала бы содержать поле с несопоставимой ценой.
//
// # Что делает этот вызов и чего он НЕ делает
//
// Он назначает ЖЕЛАЕМУЮ ревизию привязки и возвращается. Данные переносит сверщик,
// и статус тома отражает переезд, пока действующая ревизия не сравняется с желаемой.
// Предмет операции здесь тот же, что у создания: намерение зафиксировано.
func (u *UseCase) ChangeDiskType(ctx context.Context, in ChangeDiskTypeInput) (*operations.Operation, error) {
	if err := idInvalid(in.VolumeID); err != nil {
		return nil, u.errStatus(err)
	}
	if in.DiskTypeID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: disk_type_id: required", storageerr.ErrInvalidArg))
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Change disk type of volume %s", in.VolumeID),
		&storagev1.ChangeDiskTypeMetadata{VolumeId: in.VolumeID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = in.VolumeID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		v, derr := u.writer.ChangeDiskType(ctx, in.VolumeID, in.DiskTypeID)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalVolume(v)
	})
	return &op, nil
}
