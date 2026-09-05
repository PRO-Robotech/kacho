// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package snapshot — use-case ресурса Snapshot.
//
// Use-case слой: domain + порт Repo + peer IAMClient + corelib operations. Get/List
// — sync; Create/Update/Delete — async Operation (ban #9). source_volume_id —
// within-service ссылка на volumes (same-DB FK SET NULL); existence + READY-check
// делает repo атомарным INSERT…SELECT (не TOCTOU). project_id — cross-service →
// kaname (peer-validate на request-path, fail-closed). immutable source_volume_id.
package snapshot

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

// Pagination — вход для List: cursor-пагинация + project-scope + filter=name
// (listauthz энфорсится authz-слоем; repo скоупит по project_id).
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

// SnapshotUpdate — резолвнутый набор mutable-изменений для Repo.Update. nil-поле →
// без изменения (COALESCE); LabelsSet различает «labels в маске» от «не трогать».
type SnapshotUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
}

// Repo — порт хранилища snapshots (Reader+Writer). CQRS-split — при необходимости
// read-replica.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.Snapshot, error)
	List(ctx context.Context, p Pagination) ([]*domain.Snapshot, string, error)
	Insert(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, []ownerregister.Registration, error)
	Update(ctx context.Context, id string, u SnapshotUpdate) (*domain.Snapshot, []ownerregister.Registration, error)
	Delete(ctx context.Context, id string) error
	// Copy заводит КОПИЮ снимка в другой зоне. Копия — новый ресурс, а не правка
	// существующего: зона снимка неизменяема, и перенос выражается копией.
	Copy(ctx context.Context, s *domain.Snapshot, sourceID, targetZone string) (*domain.Snapshot, error)
}

// GeoClient — peer-валидация зоны через kacho-geo (fail-closed).
//
// Нужен ровно копированию: целевую зону называет вызывающий, и несуществующая зона
// без этой проверки дала бы отказ «нет привязки» — вызывающего отправило бы чинить
// каталог вместо опечатки в имени зоны.
type GeoClient interface {
	EnsureZoneExists(ctx context.Context, zoneID string) error
}

// IAMClient — peer-валидация project_id через kaname (fail-closed).
type IAMClient interface {
	EnsureProjectExists(ctx context.Context, projectID string) error
}

// ErrToStatus — инжектированный sentinel→gRPC-status mapper.
type ErrToStatus func(error) error

// knownUpdateFields — mutable-поля Snapshot.Update. Immutable (source_volume_id/
// project_id/size_bytes) НЕ входят — immutable-switch отвергает их раньше конвенц-
// сообщением, а не generic «unknown field».
var knownUpdateFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"labels":      {},
}

// UseCase — бизнес-логика Snapshot поверх Repo, peer IAMClient, LRO-стека operations
// и инжектированного transport-mapper'а errStatus.
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
	repo      Repo
	iam       IAMClient
	ops       operations.Repo
	errStatus ErrToStatus
	// registrar — синхронная регистрация owner-tuple после commit (immediate
	// анти-BOLA; nil → только async register-drainer). Инжектится WithRegistrar.
	registrar fgaregister.Registrar
	// listFilter — per-object фильтр видимости страницы List (kaname
	// AuthorizeService.BatchCheck). nil → passthrough (dev / list-filter disabled;
	// production boot-guard такую посадку запрещает). Инжектится WithListFilter.
	listFilter *listnarrow.Narrower
	// installPrefix — префикс имени объектов этого развёртывания у бэкенда.
	// Инжектится WithInstallPrefix; без него имя не выводится, и создание снимка
	// отвергается синхронно — молча снять снимок без префикса нельзя, иначе
	// соседнее облако на том же кластере усыновило бы его объект.
	installPrefix string
	// dataPlane — объявлена ли плоскость данных. Тот же признак, что читает
	// проводка сверщика: два решения об одном предмете не должны разъезжаться.
	dataPlane bool
	// geo — владелец географии. Провязывается WithGeo; без него копирование
	// отвергается закрыто, а не пропускает непроверенную зону.
	geo GeoClient
}

// New собирает UseCase для Snapshot.
func New(repo Repo, iam IAMClient, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{repo: repo, iam: iam, ops: ops, errStatus: errStatus}
}

// WithRegistrar подключает синхронный owner-tuple registrar (парити vpc / Volume):
// после Create-commit owner-grant регистрируется сразу для immediate анти-BOLA-резолва
// на свежий снапшот. Best-effort (durable outbox-intent + drainer — at-least-once
// backstop); nil → sync-путь пропускается (dev/no-iam).
func (u *UseCase) WithRegistrar(r fgaregister.Registrar) *UseCase {
	u.registrar = r
	return u
}

// WithListFilter подключает per-object фильтр видимости публичного List.
// WithQuota провязывает совещательную полосу учёта числа ресурсов.
func (u *UseCase) WithQuota(q QuotaGuard) *UseCase { u.quota = q; return u }

func (u *UseCase) WithListFilter(f *listnarrow.Narrower) *UseCase {
	u.listFilter = f
	return u
}

// WithInstallPrefix задаёт префикс установки, из которого выводится имя объекта у
// бэкенда.
//
// Он приходит из конфигурации процесса, а не из ресурса: это свойство РАЗВЁРТЫВАНИЯ,
// отличающее наши объекты от объектов соседнего облака в общем кластере хранилища.
// Пустой префикс боевой страж старта не пропускает — см. config.Validate.
// WithGeo подключает владельца географии.
func (u *UseCase) WithGeo(g GeoClient) *UseCase {
	u.geo = g
	return u
}

// WithDataPlane объявляет наличие плоскости данных. Тот же признак, из которого
// композиционный корень поднимает сверщик, — чтобы решения не разъезжались.
func (u *UseCase) WithDataPlane(v bool) *UseCase { u.dataPlane = v; return u }

func (u *UseCase) WithInstallPrefix(p string) *UseCase {
	u.installPrefix = p
	return u
}

// registerOwnerTuple — best-effort синхронная регистрация owner-tuple после commit
// (ошибка не пробрасывается: register-drainer применит durable intent at-least-once).
func (u *UseCase) registerOwnerTuple(ctx context.Context, regs []ownerregister.Registration) {
	if u.registrar == nil || len(regs) == 0 {
		return
	}
	if err := u.registrar.Register(ctx, regs); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async drainer will apply",
			"object", regs[0].Tuple.Object, "err", err)
	}
}

// idInvalid — malformed snp-id первым стейтментом: sync InvalidArgument
// "invalid snapshot id '<X>'". well-formed-но-нет → NotFound (repo.Get).
func idInvalid(id string) error {
	if !ids.IsValid(id, domain.PrefixSnapshot) {
		return fmt.Errorf("%w: invalid snapshot id '%s'", storageerr.ErrInvalidArg, id)
	}
	return nil
}

// Get возвращает Snapshot по id (malformed → sync InvalidArgument первым стейтментом).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Snapshot, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	s, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return s, nil
}

// List возвращает снимки (cursor-пагинация; garbage page_size → InvalidArgument;
// filter=name whitelisted через corelib filter).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Snapshot, string, error) {
	// projectId — обязательный scope публичного List (in-service backstop к gateway
	// scope_extractor {project,project_id}): пустой projectId вернул бы строки ВСЕХ
	// проектов (repo сужает лишь при ProjectID!=""), поэтому отвергаем СИНХРОННО
	// первым стейтментом — кросс-проектной утечки нет by construction (INV-10;
	// docs/architecture/overview.md; acceptance CS1-S3-07/GAP-C).
	if p.ProjectID == "" {
		return nil, "", u.errStatus(fmt.Errorf("%w: projectId is required", storageerr.ErrInvalidArg))
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	// Repo получает РАЗОБРАННЫЙ УЗЕЛ, а не выдернутое из него значение: оператор —
	// часть выражения, и потерять его здесь значит ответить равенством на запрос
	// подстроки (#460).
	if p.Filter != "" {
		ast, ferr := filter.Parse(p.Filter, []string{"name"})
		if ferr != nil {
			return nil, "", u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, ferr.Error()))
		}
		p.FilterAST = ast
	}
	snaps, next, err := u.repo.List(ctx, p)
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
		authzfilter.ResourceTypeSnapshot, authzfilter.ActionSnapshotList, snaps,
		func(s *domain.Snapshot) string { return s.ID })
	if ferr != nil {
		// Fail-closed: ошибка iam НИКОГДА не отдаёт нефильтрованную страницу.
		return nil, "", u.errStatus(ferr)
	}
	return visible, next, nil
}

// Create создаёт Snapshot тома (async Operation). Sync-фаза: domain-validate
// (source_volume_id обязателен, name-длина) → project_id peer-validate (kaname,
// fail-closed Unavailable). Async-worker: repo.Insert — атомарный INSERT…SELECT
// (source volume существует В ТОМ ЖЕ проекте И state=READY; size_bytes=
// volumes.size_bytes; state→READY сразу). Не-READY/отсутствующий источник → Operation
// error FAILED_PRECONDITION; том ЧУЖОГО проекта неотличим от несуществующего
// ("Volume <id> not found") — анти-BOLA hide-existence, чужой том не снапшотится.
func (u *UseCase) Create(ctx context.Context, s *domain.Snapshot) (*operations.Operation, error) {
	if err := s.Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error()))
	}
	// Способность СЕРВИСА исполнить запрос проверяется ДО обращения к соседу:
	// посадка без префикса установки не снимет снимка ни при каком ответе владельца
	// проекта, и тратить на это чужой бюджет незачем.
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
	// Sync BVA at the request edge, matching Volume and Image. The domain validator
	// does not look at description or labels, so without these two an over-limit
	// value travelled all the way to the INSERT, was caught by a database
	// constraint, and came back ASYNCHRONOUSLY inside the operation error under a
	// generic text. For the caller that is the difference between "your description
	// is too long" and "the operation failed for some reason".
	//
	// The error goes to the mapper AS IS. pkg/validate already answers in the
	// contract's own shape — INVALID_ARGUMENT, generic "invalid argument" message,
	// offending field in the google.rpc.BadRequest detail — and rebuilding it from
	// err.Error() took the text and dropped the detail, so the caller learned the
	// code and never learned WHICH field was rejected.
	if err := validate.Description("description", s.Description); err != nil {
		return nil, u.errStatus(err)
	}
	if err := validate.Labels("labels", s.Labels); err != nil {
		return nil, u.errStatus(err)
	}
	if err := u.iam.EnsureProjectExists(ctx, s.ProjectID); err != nil {
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
		if err := u.quota.Admit(ctx, s.ProjectID, quota.KindSnapshots); err != nil {
			return nil, u.errStatus(err)
		}
	}
	s.ID = ids.NewID(domain.PrefixSnapshot)
	// Пустое имя не доживает до записи (#715). Подстановка стоит ЗДЕСЬ, после
	// чеканки идентификатора: умолчание выводится из него, и раньше выводить было
	// не из чего. Два безымянных снимка в одном проекте не спорят за
	// UNIQUE(project,name) — идентификатор уникален глобально by construction.
	s.Name = validate.NameOrDefault(s.Name, s.ID)
	// Имя объекта у бэкенда выводится из СОБСТВЕННОГО идентификатора снимка, а не из
	// тома: том удаляется раньше снимка (ссылка на источник обнуляется), и имя,
	// производное от тома, пережило бы то, что им названо. Вывод детерминирован —
	// отсюда идемпотентность повтора by construction.
	s.Backend.BackendObject = blockbackend.SnapshotObjectName(u.installPrefix, s.ID)
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Create snapshot %s", s.ID),
		&storagev1.CreateSnapshotMetadata{SnapshotId: s.ID, SourceVolumeId: s.SourceVolumeID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = s.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	created := *s
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, regs, derr := u.repo.Insert(ctx, &created)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
		// регистрируем для immediate анти-BOLA-резолва (best-effort, backstop — drainer).
		u.registerOwnerTuple(ctx, regs)
		return marshalSnapshot(res)
	})
	return &op, nil
}

// Update меняет mutable-поля Snapshot (async Operation). Sync-фаза: malformed-id
// первым стейтментом → immutable-switch (ДО UpdateMask, api-conventions gotcha) →
// UpdateMask known-set. Пустой mask → full-object PATCH (immutable из тела нет —
// UpdateSnapshotRequest их не несёт). Async: repo.Update (0-row → NotFound).
func (u *UseCase) Update(ctx context.Context, id string, mask []string, name, description string, labels map[string]string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	for _, p := range mask {
		switch p {
		case "source_volume_id", "project_id", "size_bytes":
			return nil, u.errStatus(fmt.Errorf("%w: %s is immutable after Snapshot.Create", storageerr.ErrInvalidArg, p))
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
		fmt.Sprintf("Update snapshot %s", id),
		&storagev1.UpdateSnapshotMetadata{SnapshotId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, regs, derr := u.repo.Update(ctx, id, upd)
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
		return marshalSnapshot(res)
	})
	return &op, nil
}

// Delete удаляет Snapshot (async Operation). Malformed-id → sync InvalidArgument.
// Ссылки со стороны volumes (source_snapshot_id) НЕ блокируют — FK SET NULL (§1.2,
// S1-09). Успех → response=Empty.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Delete snapshot %s", id),
		&storagev1.DeleteSnapshotMetadata{SnapshotId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		if derr := u.repo.Delete(ctx, id); derr != nil {
			return nil, u.errStatus(derr)
		}
		return anypb.New(&emptypb.Empty{})
	})
	return &op, nil
}

// ListOperations возвращает операции по конкретному Snapshot (corelib-standard:
// resource_id-фильтр общей operations-таблицы). Malformed snp-id → sync
// InvalidArgument (парити с Get): без этой проверки мусорная строка уезжает в общий
// журнал и возвращает пустую страницу — то есть ответ «операций нет» на вопрос,
// который вообще не про снимок.
//
// Журнал есть у тома и образа; у снимка его не было, хотя операции у него те же
// три. Отсутствие журнала означает, что об исходе создания вызывающий узнаёт только
// из ответа на сам запрос: потерял идентификатор операции — потерял причину отказа.
func (u *UseCase) ListOperations(ctx context.Context, snapshotID string, p Pagination) ([]operations.Operation, string, error) {
	if err := idInvalid(snapshotID); err != nil {
		return nil, "", u.errStatus(err)
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	return operations.ListForCaller(ctx, u.ops,
		operations.ListFilter{ResourceID: snapshotID, PageSize: size, PageToken: p.PageToken})
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
// До этого не проверялось НИ ОДНО из трёх (снимок был шире тома и образа — те хотя
// бы прогоняли имя): переразмерное описание, переполненные метки и незаконное имя
// доезжали до UPDATE, ловились snapshots_description_check / snapshots_labels_valid /
// snapshots_name_check и возвращались АСИНХРОННО в ошибке операции обобщённым
// «Illegal argument» — поздно и без имени поля. Ошибка pkg/validate уходит наверх
// КАК ЕСТЬ: имя поля она кладёт в google.rpc.BadRequest-детали, а пересборка через
// err.Error() их теряет. Имя — исключение: его контрактный текст сам называет поле
// («Illegal argument name»), поэтому оно идёт привычной sentinel-обёрткой.
func resolveUpdate(mask []string, name, description string, labels map[string]string) (SnapshotUpdate, error) {
	var u SnapshotUpdate
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
		return SnapshotUpdate{}, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, domain.ErrIllegalName)
	}
	if applyName {
		n := name
		u.Name = &n
	}
	if apply("description") {
		if err := validate.Description("description", description); err != nil {
			return SnapshotUpdate{}, err
		}
		d := description
		u.Description = &d
	}
	if apply("labels") {
		if err := validate.Labels("labels", labels); err != nil {
			return SnapshotUpdate{}, err
		}
		u.Labels = labels
		u.LabelsSet = true
	}
	return u, nil
}

// marshalSnapshot упаковывает domain.Snapshot в Operation.response через единый
// protoconv.Snapshot (та же проекция, что handler — без дрейфа полей).
func marshalSnapshot(s *domain.Snapshot) (*anypb.Any, error) {
	return anypb.New(protoconv.Snapshot(s))
}

// CopyInput — вход копирования снимка в другую зону.
type CopyInput struct {
	// ProjectID — проект, в котором создаётся копия, и объект вопроса о правах.
	// Обязан совпадать с проектом источника (см. Copy).
	ProjectID    string
	SnapshotID   string
	TargetZoneID string
	Name         string
	Description  string
	Labels       map[string]string
}

// Copy копирует снимок в другую зону.
//
// Это ЕДИНСТВЕННЫЙ законный путь переноса данных между зонами: зона тома неизменяема,
// и без копии её неизменяемость была бы не решением, а тупиком. Создаётся НОВЫЙ
// снимок; исходный не меняется ни одним полем.
//
// Целевая зона проверяется у владельца географии — не потому, что мы ей не доверяем,
// а потому, что несуществующая зона иначе дала бы отказ «нет привязки», отправив
// вызывающего чинить каталог вместо опечатки в имени зоны.
//
// # Почему проект назван вызывающим и сверяется здесь
//
// Право создавать спрашивают у родителя (`editor@project`), и край задаёт этот
// вопрос про `project_id` из запроса. Значит проект обязан быть НАЗВАН — иначе
// у края нет объекта вопроса. Отсюда обязанность этой функции: убедиться, что
// названный проект и есть проект источника. Без сверки вызывающий предъявлял бы
// край проект, где он редактор, а копировал снимок из чужого — то есть выбирал
// себе область прав сам (BOLA).
//
// Расхождение отвечает ТЕМ ЖЕ текстом, что и отсутствующий снимок. Отличимый
// ответ («снимок есть, но не ваш») сообщал бы о существовании чужой строки —
// ровно то, что скрытие и закрывает.
func (u *UseCase) Copy(ctx context.Context, in CopyInput) (*operations.Operation, error) {
	if err := idInvalid(in.SnapshotID); err != nil {
		return nil, u.errStatus(err)
	}
	if in.ProjectID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: project_id: required", storageerr.ErrInvalidArg))
	}
	// Формат чужого id здесь НЕ проверяется (B4: формат — только у своих), и
	// существование проекта не спрашивается у iam: проект источника авторитетен,
	// а расхождение всё равно отвечает промахом ниже. Лишний вызов к соседу дал
	// бы ещё одну причину отказать на пути, где ответ уже известен.
	if in.TargetZoneID == "" {
		return nil, u.errStatus(fmt.Errorf("%w: target_zone_id: required", storageerr.ErrInvalidArg))
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
	if u.geo == nil {
		// Непроверенная зона не пропускается: отсутствие владельца географии —
		// неполная посадка, а не разрешение поверить вызывающему на слово.
		return nil, status.Error(codes.Unavailable, "zone owner is not configured")
	}
	if err := u.geo.EnsureZoneExists(ctx, in.TargetZoneID); err != nil {
		return nil, u.errStatus(err)
	}

	src, err := u.repo.Get(ctx, in.SnapshotID)
	if err != nil {
		return nil, u.errStatus(err)
	}
	if src.ProjectID != in.ProjectID {
		// Байт-в-байт тон промаха репозитория: чужая строка неотличима от
		// отсутствующей.
		return nil, u.errStatus(fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrNotFound, in.SnapshotID))
	}

	copyItem := &domain.Snapshot{
		ID:        ids.NewID(domain.PrefixSnapshot),
		ProjectID: src.ProjectID,
		// Происхождение копии — НЕПОСРЕДСТВЕННЫЙ РОДИТЕЛЬ, а не источник источника.
		//
		// Здесь стояло наследование тома от исходного снимка. Оно проводило копию
		// через проверку домена, но утверждало о ней неправду: копия снята не с
		// тома, а с другого снимка, и вставка копии всё это время записывала
		// именно родителя в `source_snapshot_id`. То есть строка в базе и объект
		// в памяти расходились, а наружу не выходило ни то, ни другое.
		//
		// Настоящая причина, по которой глагол не работал ни разу, — не отсутствие
		// источника, а то, что второй вид происхождения не был признан ни доменом,
		// ни контрактом. Теперь он признан обоими.
		SourceSnapshotID: src.ID,
		Name:             in.Name,
		Description:      in.Description,
		Labels:           in.Labels,
	}
	copyItem.Backend.BackendObject = blockbackend.SnapshotObjectName(u.installPrefix, copyItem.ID)
	if verr := copyItem.Validate(); verr != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, verr.Error()))
	}
	// Копия — ОТДЕЛЬНЫЙ путь появления строки со своим идентификатором, поэтому
	// подстановка нужна и здесь: правило «пустое имя не доживает до записи»
	// принадлежит записи, а не глаголу (#715).
	copyItem.Name = validate.NameOrDefault(copyItem.Name, copyItem.ID)

	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Copy snapshot %s to zone %s", in.SnapshotID, in.TargetZoneID),
		&storagev1.CopySnapshotMetadata{SnapshotId: copyItem.ID, SourceSnapshotId: in.SnapshotID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = copyItem.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	target := in.TargetZoneID
	source := in.SnapshotID
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, derr := u.repo.Copy(ctx, copyItem, source, target)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalSnapshot(res)
	})
	return &op, nil
}
