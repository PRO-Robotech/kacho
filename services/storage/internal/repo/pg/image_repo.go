// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// ImageRepo — реализация image.Reader/Writer поверх pgxpool (handwritten pgx, БЕЗ
// ORM). Within-service инварианты — на DB (source at-most-one mutual-exclusion CHECK,
// source FK SET NULL — provenance, partial UNIQUE(name)); мутации пишут
// fga_register_outbox в той же writer-TX (атомарно, один commit).
type ImageRepo struct {
	pool *pgxpool.Pool
	// readyOnCommit — см. VolumeRepo: без плоскости данных фиксация записи
	// сама есть готовность.
	readyOnCommit bool
}

// NewImageRepo создаёт ImageRepo поверх pgxpool.
func NewImageRepo(pool *pgxpool.Pool) *ImageRepo { return &ImageRepo{pool: pool} }

// WithReadyOnCommit — см. VolumeRepo.WithReadyOnCommit.
func (r *ImageRepo) WithReadyOnCommit(v bool) *ImageRepo { r.readyOnCommit = v; return r }

// imageSelectCols — проекционный список для Get/List. Image — всегда REGIONAL
// (placement const), поэтому колонки placement нет; source_* nullable → COALESCE ”.
//
// Тома, засеянные этим образом, приходят ТЕМ ЖЕ запросом подзапросом-массивом, а не
// вторым обращением на строку: список нужен на каждом чтении (арендатор обязан
// видеть детей ДО удаления образа), и отдельный запрос на образ превратил бы
// страницу списка в N+1. Подзапрос опирается на индекс volumes(source_image_id).
const imageSelectCols = `
	i.id, i.project_id, i.created_at, i.updated_at, i.name, i.description, i.labels,
	i.region_id, COALESCE(i.source_snapshot_id, ''), COALESCE(i.source_volume_id, ''),
	i.size_bytes, i.min_disk_bytes, i.format, i.state, i.status_reason,
	ARRAY(SELECT sv.id FROM volumes sv WHERE sv.source_image_id = i.id
	       ORDER BY sv.created_at ASC, sv.id ASC)`

// imageInfraCols — инфра-проекция образа (:9091). НИ ОДНА из этих колонок не
// выходит на публичную поверхность: по имени объекта и ревизии привязки картируется
// раскладка хранилища. Читаются они отдельным запросом GetInternal, а не общей
// проекцией, — чтобы на публичном пути их не было вовсе.
const imageInfraCols = `
	COALESCE(i.binding_id, ''), COALESCE(i.backend_object, ''), i.observed_state, i.observed_at`

// scanImage читает одну строку проекции imageSelectCols в domain.Image, деривя
// Status/Format/Placement (Image всегда REGIONAL; format single-tier STANDARD).
//
// extra — приёмники ДОПОЛНИТЕЛЬНЫХ колонок, которые вызывающий дописал к проекции
// (см. GetInternal). Список приёмников один на обе проекции намеренно: разъехавшись,
// они дали бы два разных прочтения одной строки.
func scanImage(row pgx.Row, extra ...any) (*domain.Image, error) {
	var (
		i          domain.Image
		labelsJSON []byte
		format     string
		state      string
		reason     string
		seeded     []string
	)
	dest := []any{
		&i.ID, &i.ProjectID, &i.CreatedAt, &i.UpdatedAt, &i.Name, &i.Description, &labelsJSON,
		&i.RegionID, &i.SourceSnapshot, &i.SourceVolume, &i.SizeBytes, &i.MinDiskBytes, &format, &state,
		&reason, &seeded,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	if len(labelsJSON) > 0 {
		if err := json.Unmarshal(labelsJSON, &i.Labels); err != nil {
			return nil, err
		}
	}
	i.Placement = domain.ImagePlacementRegional
	i.Format = imageFormatFromDB(format)
	i.Status = domain.ImageStatusFromState(state)
	i.StatusReason = domain.StatusReason(reason)
	i.SeededVolumeIDs = seeded
	return &i, nil
}

// Get реализует image.Reader: образ по id.
func (r *ImageRepo) Get(ctx context.Context, id string) (*domain.Image, error) {
	q := `SELECT ` + imageSelectCols + ` FROM images i WHERE i.id = $1`
	i, err := scanImage(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, mapImageErr(err, imgErrCtx{imageID: id})
	}
	return i, nil
}

// List реализует image.Reader: cursor-пагинация (created_at,id) ASC, project-scope
// (WHERE i.project_id = $ — listauthz posture, make audit-list-filter) и filter=name.
func (r *ImageRepo) List(ctx context.Context, p image.Pagination) ([]*domain.Image, string, error) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if p.ProjectID != "" {
		add("i.project_id = $%d", p.ProjectID)
	}
	if p.Filter != "" {
		add("i.name = $%d", p.Filter)
	}
	if p.PageToken != "" {
		cur, derr := decodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", derr
		}
		args = append(args, cur.createdAt, cur.id)
		conds = append(conds, fmt.Sprintf("(i.created_at, i.id) > ($%d, $%d)", len(args)-1, len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, p.PageSize+1)
	q := fmt.Sprintf(`SELECT %s FROM images i %s
		ORDER BY i.created_at ASC, i.id ASC
		LIMIT $%d`, imageSelectCols, where, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", mapImageErr(err, imgErrCtx{})
	}
	defer rows.Close()
	var out []*domain.Image
	for rows.Next() {
		i, serr := scanImage(rows)
		if serr != nil {
			return nil, "", mapImageErr(serr, imgErrCtx{})
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, "", mapImageErr(err, imgErrCtx{})
	}
	var next string
	if int64(len(out)) > p.PageSize {
		last := out[p.PageSize-1]
		next = encodePageToken(cursor{createdAt: last.CreatedAt, id: last.ID})
		out = out[:p.PageSize]
	}
	return out, next, nil
}

// imageInsertCoherentSQL — атомарная вставка-если-можно (INSERT…SELECT, ban #10 — НЕ
// Get→check→INSERT): источник (snapshot ЛИБО volume) обязан быть когерентен
// создаваемому Image по ДВУМ осям сразу, и обе проверяются в ТОМ ЖЕ стейтменте, что
// вставка (row-lock FK-проверки), поэтому TOCTOU-окна нет ни у одной.
//
// Ось 1 — ПРОЕКТ. Голого FK недостаточно: он проверяет лишь существование строки,
// поэтому caller мог засеять свой образ ЧУЖИМ приватным снапшотом/томом и вычитать
// содержимое чужого тома (cross-project disclosure/BOLA).
//
// Ось 2 — РАЗМЕЩЕНИЕ. Image — REGIONAL, Volume — ZONAL, и они когерентны только
// когда зона тома принадлежит региону образа (migration 0007 и image.proto заявляют
// именно это). Обратное направление — Volume ИЗ Image — это уже энфорсило; захват
// Image ИЗ Volume не энфорсил ничего, кроме проекта, поэтому region-1-образ можно
// было собрать из region-2-тома: и расхождение с заявленным контрактом, и тихий
// перенос данных через границу размещения.
//
// $10 — зоны, СОСТАВЛЯЮЩИЕ регион образа, полученные у ВЛАДЕЛЬЦА Geography (kacho-geo).
// Регион зоны НИКОГДА не выводится разбором имени (data-integrity.md прямо это
// запрещает: строковая деривация молча даёт пустую строку и превращает проверку в
// no-op). Решение принимает САМ стейтмент, сверяя живую строку источника с этим
// набором, — не Go-код до него.
//
// Снапшот СВОЕЙ зоны не несёт (колонки нет): его единственное свидетельство
// размещения — том, с которого он снят (`snapshots.source_volume_id`). Поэтому для
// snapshot-источника сверяется зона ЭТОГО тома — иначе проверка обходилась бы одним
// лишним шагом (сначала снять снапшот, потом собрать образ). Лineage может быть уже
// NULL (FK на volumes — ON DELETE SET NULL): тогда у снапшота размещения нет вовсе,
// сравнивать не с чем, и придумывать его нельзя — строка проходит (см.
// image_source_region_integration_test.go, где эта граница зафиксирована явно).
//
// size_bytes/min_disk_bytes снимаются с той же уже РАЗРЕШЁННОЙ строки источника.
// Источник не задан (”→NULL, пост-SET-NULL форма) → оба предиката тривиально истинны,
// size 0 — прежнее поведение сохранено.
// ТРЕТЬЯ ось — ГОТОВНОСТЬ источника. Образ снимается с БАЙТОВ, а не со строки: пока
// объект источника у бэкенда не материализован, снимать нечего, и захват отвергается.
// До этой полосы предикат сверял существование и размещение, поэтому образ спокойно
// собирался с тома, которого у бэкенда ещё нет, — и получал размер строки при пустом
// объекте.
//
// Полосы дают РАЗНЫЕ исходы, и их обязательно различать. Источник ЧУЖОГО проекта
// (или чужого региона) не резолвится вовсе → байт-в-байт "<Resource> <id> not found":
// отдельный текст про готовность был бы оракулом состояния чужого ресурса. Источник
// СВОЙ и вызывающему виден → причина называется вслух ("<Resource> <id> is not
// ready"), потому что скрывать нечего, а промах отправил бы его искать существующий
// ресурс. Поэтому стейтмент ВСЕГДА возвращает ровно одну строку-дискриминатор:
// успешную вставку ЛИБО (NULL, NULL, NULL, NULL, состояние своего снимка, состояние
// своего тома).
//
// ЧЕТВЁРТАЯ ось — ПОЛИТИКА. Ревизия привязки НАСЛЕДУЕТСЯ от источника: образ лежит
// там же, где его байты, и выбирать ему ревизию заново значило бы завести второй
// источник истины о том, где эти байты находятся. У источника без ревизии наследовать
// нечего — колонка остаётся пустой, и придумывать её нельзя.
//
// Состояние рождения — CREATING, а не READY: строка закоммичена, объекта у бэкенда
// ещё нет. Готовым образ объявит сверщик, увидев объект. Одна величина на «намерение»
// и «факт» сделала бы это расхождение ненаходимым.
const imageInsertCoherentSQL = `
	WITH snap_ok AS (
		SELECT s.state, s.size_bytes, s.binding_id
		  FROM snapshots s
		 WHERE s.id = $7::text AND s.project_id = $2::text
		   AND (s.source_volume_id IS NULL OR EXISTS (
		           SELECT 1 FROM volumes lv
		            WHERE lv.id = s.source_volume_id
		              AND lv.zone_id = ANY($10::text[])))
	), vol_ok AS (
		SELECT v.state, v.size_bytes, v.binding_id
		  FROM volumes v
		 WHERE v.id = $8::text AND v.project_id = $2::text
		   AND v.zone_id = ANY($10::text[])
	), src AS (
		SELECT size_bytes, binding_id FROM snap_ok WHERE state = 'READY'
		UNION ALL
		SELECT size_bytes, binding_id FROM vol_ok  WHERE state = 'READY'
	), ins AS (
		INSERT INTO images
			(id, project_id, name, description, labels, region_id,
			 source_snapshot_id, source_volume_id, size_bytes, min_disk_bytes, format, state,
			 binding_id, backend_object, status_reason)
		SELECT $1::text,$2::text,$3::text,$4::text,$5::jsonb,$6::text,$7::text,$8::text,
		       COALESCE((SELECT size_bytes FROM src), 0),
		       COALESCE((SELECT size_bytes FROM src), 0),
		       $9::text,'%s',
		       (SELECT binding_id FROM src), $11::text, $12::text
		 WHERE ($7::text IS NULL OR EXISTS (SELECT 1 FROM snap_ok WHERE state = 'READY'))
		   AND ($8::text IS NULL OR EXISTS (SELECT 1 FROM vol_ok  WHERE state = 'READY'))
		RETURNING created_at, updated_at, size_bytes, min_disk_bytes
	)
	SELECT created_at, updated_at, size_bytes, min_disk_bytes, NULL::text, NULL::text FROM ins
	UNION ALL
	SELECT NULL::timestamptz, NULL::timestamptz, NULL::bigint, NULL::bigint,
	       (SELECT state FROM snap_ok), (SELECT state FROM vol_ok)
	 WHERE NOT EXISTS (SELECT 1 FROM ins)`

// Insert реализует image.Writer: state=CREATING (объекта у бэкенда ещё нет — готовым
// образ объявит сверщик); size_bytes/min_disk_bytes derived из размера источника
// (snapshot ЛИБО volume) на INSERT; source_* ”→NULL. Источник обязан лежать в ТОМ ЖЕ
// проекте, в том же размещении и быть ГОТОВЫМ (imageInsertCoherentSQL) — чужой
// источник отвечает hide-existence "<Resource> <id> not found" (byte-identical
// настоящему miss'у, security.md §6), свой неготовый — "<Resource> <id> is not ready".
// Ревизия привязки наследуется от источника; имя объекта у бэкенда выводит use-case и
// передаёт сюда готовым. source FK (23503) / source at-most-one mutual-exclusion
// CHECK (23514) / partial UNIQUE(name) (23505) → контрактные sentinel'ы. exactly-one
// на Create — domain.Validate() (sync). В той же writer-TX (один commit) пишется
// fga_register_outbox (owner-tuple storage_image). Прежняя редакция называла рядом с ним
// доменный outbox — таблица дропнута миграцией 0011, и вставки в неё в этой транзакции
// нет; упоминание описывало запись, которой не происходит.
func (r *ImageRepo) Insert(ctx context.Context, i *domain.Image, regionZones []string) (*domain.Image, []ownerregister.Registration, error) {
	var regs []ownerregister.Registration
	labels, err := json.Marshal(nonNilLabels(i.Labels))
	if err != nil {
		return nil, nil, storageerr.ErrInternal
	}
	var srcSnap, srcVol *string
	if i.SourceSnapshot != "" {
		srcSnap = &i.SourceSnapshot
	}
	if i.SourceVolume != "" {
		srcVol = &i.SourceVolume
	}
	// nil и пустой срез в `= ANY($10)` ведут себя одинаково (ни одна зона не
	// совпадает), но передаём всегда непустой типизированный срез, чтобы драйвер
	// не отправил NULL: `zone_id = ANY(NULL)` даёт NULL, а не false, и предикат
	// стал бы неопределённым вместо отказа.
	if regionZones == nil {
		regionZones = []string{}
	}
	created := *i
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Строка-дискриминатор: вставка ЛИБО (NULL, NULL, NULL, NULL, состояние
		// СВОЕГО когерентного снимка, состояние СВОЕГО когерентного тома).
		var createdAt, updatedAt *time.Time
		var sizeBytes, minDiskBytes *int64
		var snapState, volState *string
		serr := tx.QueryRow(ctx, fmt.Sprintf(imageInsertCoherentSQL, bornState(r.readyOnCommit)),
			i.ID, i.ProjectID, i.Name, i.Description, labels, i.RegionID,
			srcSnap, srcVol, domain.FormatStandard, regionZones,
			backendObjectArg(i.Backend.BackendObject), string(i.StatusReason)).
			Scan(&createdAt, &updatedAt, &sizeBytes, &minDiskBytes, &snapState, &volState)
		if serr != nil {
			if errors.Is(serr, pgx.ErrNoRows) {
				// Стейтмент обязан вернуть строку всегда; пусто = неучтённый исход.
				return storageerr.ErrInternal
			}
			return serr
		}
		if createdAt == nil {
			// Источник СВОЙ и когерентный, но не готовый: причина называется вслух —
			// вызывающий этот ресурс видит, скрывать нечего. Чужой источник сюда не
			// доходит (полосы проекта и размещения его уже сняли) и остаётся
			// неотличим от промаха.
			if snapState != nil && *snapState != "READY" {
				return fmt.Errorf("%w: Snapshot %s is not ready", storageerr.ErrFailedPrecondition, i.SourceSnapshot)
			}
			if volState != nil && *volState != "READY" {
				return fmt.Errorf("%w: Volume %s is not ready", storageerr.ErrFailedPrecondition, i.SourceVolume)
			}
			return imageSourceUnavailable(i.SourceSnapshot, i.SourceVolume)
		}
		created.CreatedAt, created.UpdatedAt = *createdAt, *updatedAt
		created.SizeBytes, created.MinDiskBytes = *sizeBytes, *minDiskBytes
		// owner-tuple register-intent в той же writer-TX (F13/STOR-1-27): анти-BOLA.
		reg, eerr := emitFGARegister(ctx, tx, fgaregister.EventRegister,
			fgaregister.ImageItem(i.ProjectID, i.ID, i.Labels))
		if eerr != nil {
			return eerr
		}
		regs = []ownerregister.Registration{reg}
		return nil
	})
	if txErr != nil {
		return nil, nil, mapImageErr(txErr, imgErrCtx{
			imageID: i.ID, imageName: i.Name, snapshotID: i.SourceSnapshot, volumeID: i.SourceVolume,
		})
	}
	created.Format = domain.ImageFormatStandard
	created.Placement = domain.ImagePlacementRegional
	created.Status = domain.ImageStatusCreating
	created.Observation.State = domain.ObservedAbsent
	return &created, regs, nil
}

// backendObjectArg отдаёт имя объекта у бэкенда в форме, пригодной для колонки:
// пустое имя — NULL, а не пустая строка.
//
// Разница несущая: частичная уникальность имени объекта объявлена `WHERE
// backend_object IS NOT NULL`, поэтому две строки с пустой строкой считались бы
// дубликатами одного «объекта», которого нет ни у одной. NULL означает ровно то, чем
// является: имя ещё не названо.
func backendObjectArg(name string) *string {
	if name == "" {
		return nil
	}
	return &name
}

// imageRegisterSQL — регистрация образа, объект которого у бэкенда УЖЕ существует.
//
// state и observed_state здесь READY ОБА, и это не дубль: строка закоммичена и объект
// наблюдён. Поручить сверщику «создать» существующий объект было бы поручением
// создать то, что есть, — поэтому регистрация рождает готовый образ, а не CREATING.
//
// observed_at заполняется тем же стейтментом: READY без времени наблюдения
// неотличим от «не смотрели ни разу», а это разные состояния, требующие разных
// действий оператора. Наблюдение здесь — заявление регистрирующего; сверщик
// перепроверит его в свой черёд и переведёт строку в ERROR, если объекта нет.
//
// Колонок источника нет вовсе: у зарегистрированного образа источника ВНУТРИ облака
// не существует. Размеры называет регистрирующий — снимать их не с чего.
const imageRegisterSQL = `
	INSERT INTO images
		(id, project_id, name, description, labels, region_id,
		 size_bytes, min_disk_bytes, format, state,
		 backend_object, observed_state, observed_at, status_reason)
	VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,'READY',$10,'READY',now(),'')
	RETURNING created_at, updated_at, observed_at`

// cnImageBackendObjectUniq — частичная уникальность имени объекта у бэкенда
// (миграция 0017). Один объект хранилища — один образ.
const cnImageBackendObjectUniq = "images_backend_object_uniq"

// Register реализует image.Writer: вносит строку об образе, УЖЕ лежащем в хранилище
// (единственный путь появления образа на чистой установке — блоб-конвейера у нас нет).
//
// Уникальность имени объекта держит частичный уникальный индекс, а не проверка в
// коде: две конкурентные регистрации одного имени иначе обе прошли бы чтение и обе
// записали, и один объект получил бы два образа — с двумя независимыми удалениями.
func (r *ImageRepo) Register(ctx context.Context, i *domain.Image) (*domain.Image, []ownerregister.Registration, error) {
	var regs []ownerregister.Registration
	labels, err := json.Marshal(nonNilLabels(i.Labels))
	if err != nil {
		return nil, nil, storageerr.ErrInternal
	}
	registered := *i
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		serr := tx.QueryRow(ctx, imageRegisterSQL,
			i.ID, i.ProjectID, i.Name, i.Description, labels, i.RegionID,
			i.SizeBytes, i.MinDiskBytes, domain.FormatStandard, i.Backend.BackendObject).
			Scan(&registered.CreatedAt, &registered.UpdatedAt, &registered.Observation.At)
		if serr != nil {
			return imageBackendObjectTaken(serr, i.Backend.BackendObject)
		}
		reg, eerr := emitFGARegister(ctx, tx, fgaregister.EventRegister,
			fgaregister.ImageItem(i.ProjectID, i.ID, i.Labels))
		if eerr != nil {
			return eerr
		}
		regs = []ownerregister.Registration{reg}
		return nil
	})
	if txErr != nil {
		return nil, nil, mapImageErr(txErr, imgErrCtx{imageID: i.ID, imageName: i.Name})
	}
	registered.Format = domain.ImageFormatStandard
	registered.Placement = domain.ImagePlacementRegional
	registered.Status = domain.ImageStatusReady
	registered.Observation.State = domain.ObservedReady
	return &registered, regs, nil
}

// imageBackendObjectTaken различает столкновение по ИМЕНИ ОБЪЕКТА и всё остальное.
//
// Общая ветка 23505 отвечает «image already exists» — про образ, тогда как занят
// объект ХРАНИЛИЩА, и администратор пошёл бы искать образ с тем же именем, которого
// нет. Разбор идёт здесь, а не в общем мэппере, потому что предмет знает только этот
// путь: на прочих путях имя объекта в запросе не участвует.
func imageBackendObjectTaken(err error, backendObject string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == cnImageBackendObjectUniq {
		return fmt.Errorf("%w: image with backend object %s already exists",
			storageerr.ErrAlreadyExists, backendObject)
	}
	return err
}

// imageSourceUnavailable разбирает 0-row исход project-coherent CAS: заданный источник
// не резолвится В ПРОЕКТЕ образа — либо его нет вовсе, либо он принадлежит ЧУЖОМУ
// проекту. Оба исхода отдают ОДИН И ТОТ ЖЕ контрактный текст "<Resource> <id> not
// found" (FailedPrecondition) — byte-identical с настоящим FK-miss'ом (security.md §6):
// различимый ответ был бы existence-oracle («чужой ресурс существует»). 0 rows без
// заданного источника невозможно (предикат тривиально истинен) → opaque INTERNAL.
func imageSourceUnavailable(snapshotID, volumeID string) error {
	switch {
	case snapshotID != "":
		return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrFailedPrecondition, snapshotID)
	case volumeID != "":
		return fmt.Errorf("%w: Volume %s not found", storageerr.ErrFailedPrecondition, volumeID)
	default:
		return storageerr.ErrInternal
	}
}

// Update реализует image.Writer: mutable name/description/labels (COALESCE, nil →
// без изменения). Один UPDATE, БЕЗ Get (нет TOCTOU). 0 rows → NotFound.
func (r *ImageRepo) Update(ctx context.Context, id string, u image.ImageUpdate) (*domain.Image, []ownerregister.Registration, error) {
	var regs []ownerregister.Registration
	var labelsArg any
	if u.LabelsSet {
		b, err := json.Marshal(nonNilLabels(u.Labels))
		if err != nil {
			return nil, nil, storageerr.ErrInternal
		}
		labelsArg = b
	}
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var (
			rowID       string
			projectID   string
			labelsAfter []byte
		)
		serr := tx.QueryRow(ctx, `UPDATE images SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				labels      = COALESCE($4::jsonb, labels),
				updated_at  = now()
			WHERE id = $1
			RETURNING id, project_id, labels`,
			id, u.Name, u.Description, labelsArg).Scan(&rowID, &projectID, &labelsAfter)
		if serr == nil {
			reg, eerr := reEmitLabelMirror(ctx, tx, u.LabelsSet, labelsAfter,
				func(labels map[string]string) fgaregister.Item {
					return fgaregister.ImageItem(projectID, rowID, labels)
				})
			if eerr != nil {
				return eerr
			}
			if !reg.SourceVersion.IsZero() {
				regs = []ownerregister.Registration{reg}
			}
			return nil
		}
		if errors.Is(serr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, id)
		}
		return serr
	})
	if txErr != nil {
		return nil, nil, mapImageErr(txErr, imgErrCtx{imageID: id, imageName: derefStr(u.Name)})
	}
	updated, gerr := r.Get(ctx, id)
	return updated, regs, gerr
}

// Delete реализует image.Writer: DELETE строки образа + fga unregister-intent
// в той же tx. Образ, засевший в томе, удаляется — volumes.
// source_image_id FK ON DELETE SET NULL очищает lineage (STOR-1-28), не RESTRICT.
// 0 rows → NotFound.
func (r *ImageRepo) Delete(ctx context.Context, id string) error {
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var projectID string
		err := tx.QueryRow(ctx, `DELETE FROM images WHERE id = $1 RETURNING project_id`, id).Scan(&projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, id)
			}
			return err
		}
		_, uerr := emitFGARegister(ctx, tx, fgaregister.EventUnregister,
			fgaregister.Item{Tuple: fgaregister.StorageImage(projectID, id)})
		return uerr
	})
	if txErr != nil {
		return mapImageErr(txErr, imgErrCtx{imageID: id})
	}
	return nil
}

// GetInternal реализует image.Reader — ПОЛНАЯ (инфра) проекция образа, :9091.
//
// Отдельный запрос, а не тот же, что у публичного Get: ревизия привязки, имя объекта
// у бэкенда и наблюдение — координаты, по которым картируется раскладка хранилища, и
// на публичном пути их не должно быть даже в памяти. Одна общая проекция сделала бы
// разницу между поверхностями вопросом дисциплины конвертера, а не устройства чтения.
func (r *ImageRepo) GetInternal(ctx context.Context, id string) (*domain.Image, error) {
	var (
		bindingID     string
		backendObject string
		observedState string
		observedAt    *time.Time
	)
	q := `SELECT ` + imageSelectCols + `, ` + imageInfraCols + ` FROM images i WHERE i.id = $1`
	i, err := scanImage(r.pool.QueryRow(ctx, q, id),
		&bindingID, &backendObject, &observedState, &observedAt)
	if err != nil {
		return nil, mapImageErr(err, imgErrCtx{imageID: id})
	}
	i.Backend.BindingID = bindingID
	i.Backend.BackendObject = backendObject
	i.Observation.State = domain.ObservedState(observedState)
	if observedAt != nil {
		i.Observation.At = *observedAt
	}
	return i, nil
}

// imageFormatFromDB маппит text-колонку format → domain.ImageFormat (single-tier).
func imageFormatFromDB(s string) domain.ImageFormat {
	if s == domain.FormatStandard {
		return domain.ImageFormatStandard
	}
	return domain.ImageFormatUnspecified
}

var (
	_ image.Reader = (*ImageRepo)(nil)
	_ image.Writer = (*ImageRepo)(nil)
)

// copyImageSQL — копия образа в ДРУГОЙ регион одним стейтментом.
//
// Целевая привязка ищется среди зон целевого региона ($7) по классу ИСТОЧНИКА: образ
// региональный, привязка зональная, и любая действующая ревизия нужного класса внутри
// региона — законный адресат. Класс берётся у источника, а не у вызывающего: копия,
// молча легшая на другой класс, сменила бы арендатору гарантии без единого слова в
// ответе.
//
// Источник обязан лежать в проекте вызывающего и быть ГОТОВЫМ. Чужой проект не
// матчится и остаётся неотличим от промаха — подтверждать существование чужого образа
// отдельным текстом значило бы отвечать на незаданный вопрос.
const copyImageSQL = `
	WITH src AS (
		SELECT i.id, i.size_bytes, i.min_disk_bytes, i.state, i.format, b.disk_type_id
		  FROM images i
		  LEFT JOIN disk_type_bindings b ON b.id = i.binding_id
		 WHERE i.id = $6 AND i.project_id = $2
	), target AS (
		SELECT tb.id,
		       CASE WHEN tb.namespace_template = '' THEN $2::text
		            ELSE replace(tb.namespace_template, '{projectId}', $2::text) END AS ns
		  FROM disk_type_bindings tb, src
		 WHERE tb.disk_type_id = src.disk_type_id AND tb.zone_id = ANY($7::text[])
		   AND tb.status = 'ACTIVE'
		 LIMIT 1
	)
	INSERT INTO images
		(id, project_id, name, description, labels, region_id, source_image_id,
		 size_bytes, min_disk_bytes, format, state, binding_id, backend_object, backend_namespace)
	SELECT $1, $2, $3, $4, $5::jsonb, $8, src.id,
	       src.size_bytes, src.min_disk_bytes, src.format, '%s',
	       target.id, $9, target.ns
	  FROM src, target
	 WHERE src.state = 'READY'
	RETURNING created_at, updated_at, size_bytes, min_disk_bytes`

// Copy реализует image.Writer: копия образа в другой регион. Копия рождается
// СОЗДАВАЕМОЙ — материализует её сверщик.
func (r *ImageRepo) Copy(ctx context.Context, i *domain.Image, sourceID string, targetZones []string) (*domain.Image, error) {
	labels, err := json.Marshal(nonNilLabels(i.Labels))
	if err != nil {
		return nil, storageerr.ErrInternal
	}
	created := *i
	err = r.pool.QueryRow(ctx, fmt.Sprintf(copyImageSQL, bornState(r.readyOnCommit)),
		i.ID, i.ProjectID, i.Name, i.Description, labels, sourceID, targetZones,
		i.RegionID, i.Backend.BackendObject).
		Scan(&created.CreatedAt, &created.UpdatedAt, &created.SizeBytes, &created.MinDiskBytes)
	if err == nil {
		created.SourceImageID = sourceID
		created.Status = domain.ImageStatusCreating
		return &created, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, mapImageErr(err, imgErrCtx{imageID: i.ID, imageName: i.Name})
	}
	return nil, r.copyUnavailable(ctx, sourceID, i.RegionID)
}

// copyUnavailable разбирает нулевую выборку копии образа: каждая причина — своим
// текстом, чтобы вызывающий чинил то, что сломано, а не гадал.
func (r *ImageRepo) copyUnavailable(ctx context.Context, sourceID, targetRegion string) error {
	var state, diskType string
	err := r.pool.QueryRow(ctx, `
		SELECT i.state, COALESCE(b.disk_type_id, '')
		  FROM images i LEFT JOIN disk_type_bindings b ON b.id = i.binding_id
		 WHERE i.id = $1`, sourceID).Scan(&state, &diskType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: Image %s not found", storageerr.ErrFailedPrecondition, sourceID)
		}
		return mapImageErr(err, imgErrCtx{imageID: sourceID})
	}
	if state != "READY" {
		return fmt.Errorf("%w: Image %s is not ready", storageerr.ErrFailedPrecondition, sourceID)
	}
	if diskType == "" {
		return fmt.Errorf("%w: Image %s has no placement and cannot be copied",
			storageerr.ErrFailedPrecondition, sourceID)
	}
	return fmt.Errorf("%w: DiskType %s has no active binding in region %s",
		storageerr.ErrFailedPrecondition, diskType, targetRegion)
}
