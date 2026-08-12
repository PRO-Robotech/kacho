// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package snapshot — use-case ресурса Snapshot.
//
// Use-case слой: domain + порт Repo + peer IAMClient + corelib operations. Get/List
// — sync; Create/Update/Delete — async Operation (ban #9). source_volume_id —
// within-service ссылка на volumes (same-DB FK SET NULL); existence + READY-check
// делает repo атомарным INSERT…SELECT (не TOCTOU). project_id — cross-service →
// kacho-iam (peer-validate на request-path, fail-closed). immutable source_volume_id.
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
	Filter    string // name=<v> уже распарсен use-case-слоем в чистое значение
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
}

// IAMClient — peer-валидация project_id через kacho-iam (fail-closed).
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
type UseCase struct {
	repo      Repo
	iam       IAMClient
	ops       operations.Repo
	errStatus ErrToStatus
	// registrar — синхронная регистрация owner-tuple после commit (immediate
	// анти-BOLA; nil → только async register-drainer). Инжектится WithRegistrar.
	registrar fgaregister.Registrar
	// listFilter — per-object фильтр видимости страницы List (kacho-iam
	// AuthorizeService.BatchCheck). nil → passthrough (dev / list-filter disabled;
	// production boot-guard такую посадку запрещает). Инжектится WithListFilter.
	listFilter *listnarrow.Narrower
	// installPrefix — префикс имени объектов этого развёртывания у бэкенда.
	// Инжектится WithInstallPrefix; без него имя не выводится, и создание снимка
	// отвергается синхронно — молча снять снимок без префикса нельзя, иначе
	// соседнее облако на том же кластере усыновило бы его объект.
	installPrefix string
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
	if p.Filter != "" {
		ast, ferr := filter.Parse(p.Filter, []string{"name"})
		if ferr != nil {
			return nil, "", u.errStatus(fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, ferr.Error()))
		}
		p.Filter = ast.Value
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
// (source_volume_id обязателен, name-длина) → project_id peer-validate (kacho-iam,
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
	if u.installPrefix == "" {
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
	s.ID = ids.NewID(domain.PrefixSnapshot)
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
	if apply("name") {
		if err := domain.SnapshotName(name).Validate(); err != nil {
			return SnapshotUpdate{}, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
		}
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
