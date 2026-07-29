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
// до done. Create → state=READY сразу (control-plane; durable Operation.done, ban #9 —
// не гейтит downstream owner-tuple видимость). InternalImageService.GetInternal
// (infra-проекция) — анкер data-plane.
package image

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// Pagination — вход для List: cursor-пагинация (page_size + opaque page_token) +
// project-scope и filter=name.
type Pagination struct {
	PageSize  int64
	PageToken string
	ProjectID string
	Filter    string // name=<v> уже распарсен use-case-слоем в чистое значение
}

// ImageUpdate — резолвнутый набор mutable-изменений для Writer.Update. nil-поле →
// без изменения (COALESCE); LabelsSet различает «labels в маске» от «не трогать».
type ImageUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
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
	Insert(ctx context.Context, i *domain.Image, regionZones []string) (*domain.Image, error)
	Update(ctx context.Context, id string, u ImageUpdate) (*domain.Image, error)
	Delete(ctx context.Context, id string) error
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

// IAMClient — порт peer-валидации project_id через kacho-iam (ProjectService.Get,
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
type UseCase struct {
	reader    Reader
	writer    Writer
	geo       GeoClient
	iam       IAMClient
	ops       operations.Repo
	errStatus ErrToStatus
	registrar fgaregister.Registrar
	// listFilter — per-object фильтр видимости страницы List (kacho-iam
	// AuthorizeService.BatchCheck). nil → passthrough (dev / list-filter disabled;
	// production boot-guard такую посадку запрещает). Инжектится WithListFilter.
	listFilter authzfilter.Filter
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

// WithListFilter подключает per-object фильтр видимости публичного List.
func (u *UseCase) WithListFilter(f authzfilter.Filter) *UseCase {
	u.listFilter = f
	return u
}

// registerOwnerTuple — best-effort синхронная регистрация owner-tuple после commit.
// Ошибка НЕ пробрасывается: durable outbox-intent уже в writer-TX, register-drainer
// применит его at-least-once. Логируем WARN.
func (u *UseCase) registerOwnerTuple(ctx context.Context, item fgaregister.Item) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, []fgaregister.Item{item}); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async drainer will apply",
			"object", item.Tuple.Object, "err", err)
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
	// filter=name — whitelist через corelib filter; невалидное поле/форма → InvalidArgument.
	if p.Filter != "" {
		ast, ferr := filter.Parse(p.Filter, []string{"name"})
		if ferr != nil {
			return nil, "", u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, ferr.Error()))
		}
		p.Filter = ast.Value
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
	visible, ferr := authzfilter.FilterVisiblePage(ctx, u.listFilter,
		authzfilter.ResourceTypeImage, authzfilter.ActionImageList, imgs,
		func(i *domain.Image) string { return i.ID })
	if ferr != nil {
		// Fail-closed: ошибка iam НИКОГДА не отдаёт нефильтрованную страницу.
		return nil, "", u.errStatus(ferr)
	}
	return visible, next, nil
}

// Create создаёт Image (async Operation). Малформ/невалидный вход отвергается
// СИНХРОННО (InvalidArgument: name / source exactly-one), cross-domain ссылки
// (region→geo, project→iam) валидируются на request-path fail-closed (peer
// Unavailable → UNAVAILABLE). Валидный вход → LRO-строка + worker (writer.Insert;
// state→READY сразу; source / partial UNIQUE(name) → Operation error). Источник
// (source_snapshot_id / source_volume_id) резолвится repo СТРОГО в проекте образа
// (project-coherent CAS): чужой приватный снапшот/том неотличим от несуществующего —
// Operation error FAILED_PRECONDITION "<Resource> <id> not found" (анти-BOLA
// hide-existence; иначе содержимое чужого тома утекало бы в образ caller'а).
func (u *UseCase) Create(ctx context.Context, i *domain.Image) (*operations.Operation, error) {
	i.Placement = domain.ImagePlacementRegional
	if err := i.Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error()))
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
		res, derr := u.writer.Insert(ctx, &created, regionZones)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
		// регистрируем для immediate анти-BOLA-резолва (best-effort, post-commit;
		// backstop — async register-drainer at-least-once).
		u.registerOwnerTuple(ctx, fgaregister.ImageItem(res.ProjectID, res.ID, res.Labels))
		return marshalImage(res)
	})
	return &op, nil
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
		res, derr := u.writer.Update(ctx, id, upd)
		if derr != nil {
			return nil, u.errStatus(derr)
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
	if apply("name") {
		if err := domain.ImageName(name).Validate(); err != nil {
			return ImageUpdate{}, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
		}
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
