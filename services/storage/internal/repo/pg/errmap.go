// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// SQLSTATE'ы, которыми триггер учёта числа ресурсов сообщает СВОЙ исход
// (миграция 0023_project_resource_quotas).
//
// Классы, начинающиеся с буквы за пределами зарезервированных Postgres'ом,
// свободны для приложения — поэтому эти три не могут совпасть ни с одним кодом
// сервера и ни с одним кодом расширения. Они объявлены здесь и в самой миграции;
// оба места называют один предмет, и второе — то, где они производятся.
const (
	// sqlstateQuotaExceeded — место кончилось: строка учёта есть, used >= limit.
	sqlstateQuotaExceeded = "KQ001"
	// sqlstateQuotaNotProvisioned — потолок не назван ни на одной области.
	sqlstateQuotaNotProvisioned = "KQ002"
	// sqlstateQuotaNoProjectID — строка ресурса не несёт проекта. Дефект схемы,
	// а не арендатора: наружу уходит фиксированным внутренним отказом.
	sqlstateQuotaNoProjectID = "KQ003"
)

// mapQuotaErr отличает исход учёта от всего остального; nil означает «это не
// отказ учёта» и передаёт разбор той классификации, которая его позвала.
//
// Текст производителя сохраняется ДОСЛОВНО для двух первых исходов: он и есть
// контракт («project <P> has reached its limit of <N> <kind>»), а не диагностика
// хранилища, поэтому пересказывать его здесь значило бы завести второе место об
// одном предмете — ровно то, от чего обе полосы и защищены единственным
// производителем. Для третьего исхода текст НЕ сохраняется: он про нашу схему, и
// арендатору о ней знать нечего.
func mapQuotaErr(err error) error {
	if err == nil {
		return nil
	}
	// Уже-замапленный отказ учёта пробрасывается как есть: иначе повторный
	// проход через классификацию вызывающего схлопнул бы его в ErrInternal,
	// потеряв и код, и контрактный текст.
	switch {
	case errors.Is(err, storageerr.ErrQuotaExceeded), errors.Is(err, storageerr.ErrQuotaNotProvisioned):
		return err
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	// Величины производителя приклеиваются ЗДЕСЬ — там, где `*pgconn.PgError` ещё
	// не потерян. Дальше по пути его нет, и прочитать `DETAIL` больше негде:
	// текст переживает переход, величины — нет (задача продукта #1605). Разбор
	// общий (`pkg/quota/quotadetail`) по тому же доводу, по которому производитель
	// один: шесть копий разошлись бы молча.
	switch pgErr.Code {
	case sqlstateQuotaExceeded:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", storageerr.ErrQuotaExceeded, pgErr.Message), pgErr.Detail)
	case sqlstateQuotaNotProvisioned:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", storageerr.ErrQuotaNotProvisioned, pgErr.Message), pgErr.Detail)
	case sqlstateQuotaNoProjectID:
		slog.Error("quota accounting: resource row carries no project_id",
			"sqlstate", pgErr.Code, "detail", pgErr.Message)
		return storageerr.ErrInternal
	}
	return nil
}

// mapQuotaRepoErr — классификация для операций НАД САМИМИ строками учёта
// (совещательный вопрос и материализация).
//
// Отличается от `mapQuotaErr` ровно тем, ради чего заведена: та отвечает «это не
// отказ учёта» значением nil, потому что её зовут ПЕРВОЙ из чужих
// классификаторов и разбор передают дальше. Здесь передавать некому — это
// последний классификатор на пути, и nil здесь означал бы «ошибки не было».
//
// Ошибка стоила пробы, а не рассуждения: `MaterializeQuotas` возвращала
// `mapQuotaErr(err)` напрямую, и вставка строки без зеркала аккаунта —
// отвергнутая схемой — доезжала до вызывающего КАК УСПЕХ. То есть ограничение,
// заведённое ровно затем, чтобы состояние «невидимая дельте строка» было
// невыразимо, срабатывало, а его срабатывание терялось по дороге. Поймала это
// `TestQuotaMaterialise_RejectsARowWithoutTheAccountMirror`; без неё дефект был
// бы виден только тем, что аккаунтная дельта однажды не нашла бы строк.
func mapQuotaRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if qerr := mapQuotaErr(err); qerr != nil {
		return qerr
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Нарушение ограничения строки учёта — дефект НАШЕЙ подготовки строки
		// (пустое зеркало, неизвестная область), а не ввода арендатора: сюда
		// приходят значения, которые собрали мы сами из ответа соседа. Арендатору
		// о нашей схеме знать нечего, поэтому наружу — фиксированный внутренний
		// отказ, а SQLSTATE остаётся оператору в журнале.
		slog.Error("quota row rejected by schema",
			"sqlstate", pgErr.Code, "constraint", pgErr.ConstraintName, "detail", pgErr.Message)
		return storageerr.ErrInternal
	}
	slog.Error("quota accounting: uncategorized db error", "err", err.Error())
	return storageerr.ErrInternal
}

// volErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок Volume-репо.
// Точные тексты ошибок (§1.7) зависят от того, КАКОЙ constraint нарушен, поэтому
// mapVolumeErr переключается по PgError.ConstraintName, а id/name/diskType/snapshot
// подставляются в контрактное сообщение.
type volErrCtx struct {
	volumeID   string
	volumeName string
	diskTypeID string
	snapshotID string
	imageID    string // source_image_id FK → images текст ("Image <id> not found")
	deviceName string // attach-путь: device_name для UNIQUE(instance_id,device_name) текста
	instanceID string // attach-путь: instance_id для device/boot-конфликт текста
}

// Имена DB-constraint'ов (миграция 0003_storage_domain). Inline-FK без CONSTRAINT
// именуются Postgres'ом как <table>_<column>_fkey; именованные — как в ALTER/CREATE.
const (
	cnVolumeNameUniq     = "volumes_name_uniq"                 // partial UNIQUE(project_id,name) WHERE name<>''
	cnVolumeDiskTypeFK   = "volumes_disk_type_id_fkey"         // volumes.disk_type_id → disk_types RESTRICT
	cnVolumeSnapshotFK   = "volumes_source_snapshot_fk"        // volumes.source_snapshot_id → snapshots SET NULL
	cnVolumeImageFK      = "volumes_source_image_fk"           // volumes.source_image_id → images SET NULL (0007)
	cnAttachmentVolumeFK = "volume_attachments_volume_id_fkey" // volume_attachments.volume_id → volumes RESTRICT
	cnAttachDeviceUniq   = "volume_attachments_instance_device_uniq"
	cnAttachOneBoot      = "volume_attachments_one_boot"
)

// mapVolumeErr транслирует pgx/pgconn-ошибку в чистый sentinel с контрактным
// текстом Kachō (§1.7). Сырой pgx/SQL наружу не течёт: некатегоризированный
// SQLSTATE → storageerr.ErrInternal (serviceerr → фиксированный "internal error"), но
// сам SQLSTATE логируется на repo-границе (operator-trail, CWE-390).
func mapVolumeErr(err error, c volErrCtx) error {
	if err == nil {
		return nil
	}
	// Отказ учёта числа ресурсов классифицируется ПЕРВЫМ, до общих классов.
	//
	// Порядок здесь несущий, а не косметический: собственные SQLSTATE триггера
	// учёта не входят ни в один из классов ниже, поэтому без этой ветки они
	// доехали бы до `ErrInternal` — то есть арендатор, упёршийся в предел, видел
	// бы «что-то сломалось» вместо «место кончилось», и ровно тот отказ, ради
	// которого механизм существует, стал бы неотличим от сбоя хранилища.
	if qerr := mapQuotaErr(err); qerr != nil {
		return qerr
	}
	// Идемпотентность: уже-замапленный sentinel (напр. hand-crafted
	// "Volume size can only be increased" / NotFound из disambiguation Update)
	// пробрасывается как есть — иначе default-ветка ниже коллапсировала бы его в
	// ErrInternal (теряя контрактный текст).
	switch {
	case errors.Is(err, storageerr.ErrNotFound), errors.Is(err, storageerr.ErrAlreadyExists),
		errors.Is(err, storageerr.ErrFailedPrecondition), errors.Is(err, storageerr.ErrInvalidArg),
		errors.Is(err, storageerr.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: Volume %s not found", storageerr.ErrNotFound, c.volumeID)
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique:
			switch f.Constraint {
			case cnVolumeNameUniq:
				return fmt.Errorf("%w: volume with name %s already exists in project", storageerr.ErrAlreadyExists, c.volumeName)
			case cnAttachDeviceUniq:
				return fmt.Errorf("%w: device %s is already in use on Instance %s", storageerr.ErrFailedPrecondition, c.deviceName, c.instanceID)
			}
			return fmt.Errorf("%w: volume already exists", storageerr.ErrAlreadyExists)
		case pgfault.ForeignKey:
			switch f.Constraint {
			case cnVolumeDiskTypeFK:
				return fmt.Errorf("%w: DiskType %s not found", storageerr.ErrFailedPrecondition, c.diskTypeID)
			case cnVolumeSnapshotFK:
				return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrFailedPrecondition, c.snapshotID)
			case cnVolumeImageFK:
				return fmt.Errorf("%w: Image %s not found", storageerr.ErrFailedPrecondition, c.imageID)
			case cnAttachmentVolumeFK:
				return fmt.Errorf("%w: Volume %s is in use", storageerr.ErrFailedPrecondition, c.volumeID)
			}
			return fmt.Errorf("%w: volume violates a reference constraint", storageerr.ErrFailedPrecondition)
		case pgfault.Check: // size_bytes>0 / block_size>0 / name / labels
			return checkViolation(f, "volume", c.volumeID)
		case pgfault.Exclusion: // EXCLUDE … WHERE is_boot
			if f.Constraint == cnAttachOneBoot {
				return fmt.Errorf("%w: Instance %s already has a boot volume", storageerr.ErrFailedPrecondition, c.instanceID)
			}
			return fmt.Errorf("%w: volume exclusion constraint", storageerr.ErrFailedPrecondition)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			append([]any{"volume_id", c.volumeID}, f.LogAttrs()...)...)
		return storageerr.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "volume_id", c.volumeID)
	return storageerr.ErrInternal
}

// cnSnapshotNameUniq — partial UNIQUE(project_id,name) WHERE name<>” снапшотов.
const cnSnapshotNameUniq = "snapshots_name_uniq"

// snapErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок Snapshot-репо.
type snapErrCtx struct {
	snapshotID     string
	snapshotName   string
	sourceVolumeID string
}

// mapSnapshotErr транслирует pgx/pgconn-ошибку Snapshot-репо в чистый sentinel
// с контрактным текстом Kachō. Сырой pgx/SQL наружу не течёт (uncategorized →
// storageerr.ErrInternal, SQLSTATE логируется на границе). Уже-замапленный sentinel
// (from-READY disambiguation / NotFound) пробрасывается как есть.
func mapSnapshotErr(err error, c snapErrCtx) error {
	if err == nil {
		return nil
	}
	// Отказ учёта числа ресурсов классифицируется ПЕРВЫМ, до общих классов.
	//
	// Порядок здесь несущий, а не косметический: собственные SQLSTATE триггера
	// учёта не входят ни в один из классов ниже, поэтому без этой ветки они
	// доехали бы до `ErrInternal` — то есть арендатор, упёршийся в предел, видел
	// бы «что-то сломалось» вместо «место кончилось», и ровно тот отказ, ради
	// которого механизм существует, стал бы неотличим от сбоя хранилища.
	if qerr := mapQuotaErr(err); qerr != nil {
		return qerr
	}
	switch {
	case errors.Is(err, storageerr.ErrNotFound), errors.Is(err, storageerr.ErrAlreadyExists),
		errors.Is(err, storageerr.ErrFailedPrecondition), errors.Is(err, storageerr.ErrInvalidArg),
		errors.Is(err, storageerr.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrNotFound, c.snapshotID)
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique:
			if f.Constraint == cnSnapshotNameUniq {
				return fmt.Errorf("%w: snapshot with name %s already exists in project", storageerr.ErrAlreadyExists, c.snapshotName)
			}
			return fmt.Errorf("%w: snapshot already exists", storageerr.ErrAlreadyExists)
		case pgfault.ForeignKey: // source_volume_id → volumes
			return fmt.Errorf("%w: Volume %s not found", storageerr.ErrFailedPrecondition, c.sourceVolumeID)
		case pgfault.Check: // name / description / size / labels
			return checkViolation(f, "snapshot", c.snapshotID)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			append([]any{"snapshot_id", c.snapshotID}, f.LogAttrs()...)...)
		return storageerr.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "snapshot_id", c.snapshotID)
	return storageerr.ErrInternal
}

// Имена DB-constraint'ов образа (миграция 0007_image_and_volume_source_image).
const (
	cnImageNameUniq   = "images_name_uniq"               // partial UNIQUE(project_id,name) WHERE name<>''
	cnImageSnapshotFK = "images_source_snapshot_id_fkey" // images.source_snapshot_id → snapshots SET NULL (inline-FK name)
	cnImageVolumeFK   = "images_source_volume_id_fkey"   // images.source_volume_id → volumes SET NULL (inline-FK name)
)

// imgErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок Image-репо.
type imgErrCtx struct {
	imageID    string
	imageName  string
	snapshotID string
	volumeID   string
}

// mapImageErr транслирует pgx/pgconn-ошибку Image-репо в чистый sentinel с
// контрактным текстом Kachō. Сырой pgx/SQL наружу не течёт (uncategorized →
// storageerr.ErrInternal, SQLSTATE логируется на границе). Уже-замапленный sentinel
// пробрасывается как есть.
func mapImageErr(err error, c imgErrCtx) error {
	if err == nil {
		return nil
	}
	// Отказ учёта числа ресурсов классифицируется ПЕРВЫМ, до общих классов.
	//
	// Порядок здесь несущий, а не косметический: собственные SQLSTATE триггера
	// учёта не входят ни в один из классов ниже, поэтому без этой ветки они
	// доехали бы до `ErrInternal` — то есть арендатор, упёршийся в предел, видел
	// бы «что-то сломалось» вместо «место кончилось», и ровно тот отказ, ради
	// которого механизм существует, стал бы неотличим от сбоя хранилища.
	if qerr := mapQuotaErr(err); qerr != nil {
		return qerr
	}
	switch {
	case errors.Is(err, storageerr.ErrNotFound), errors.Is(err, storageerr.ErrAlreadyExists),
		errors.Is(err, storageerr.ErrFailedPrecondition), errors.Is(err, storageerr.ErrInvalidArg),
		errors.Is(err, storageerr.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, c.imageID)
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique:
			if f.Constraint == cnImageNameUniq {
				return fmt.Errorf("%w: image with name %s already exists in project", storageerr.ErrAlreadyExists, c.imageName)
			}
			return fmt.Errorf("%w: image already exists", storageerr.ErrAlreadyExists)
		case pgfault.ForeignKey: // source_snapshot_id / source_volume_id
			switch f.Constraint {
			case cnImageSnapshotFK:
				return fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrFailedPrecondition, c.snapshotID)
			case cnImageVolumeFK:
				return fmt.Errorf("%w: Volume %s not found", storageerr.ErrFailedPrecondition, c.volumeID)
			}
			return fmt.Errorf("%w: image violates a reference constraint", storageerr.ErrFailedPrecondition)
		case pgfault.Check: // source at-most-one / name / description / format / size / labels
			return checkViolation(f, "image", c.imageID)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			append([]any{"image_id", c.imageID}, f.LogAttrs()...)...)
		return storageerr.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "image_id", c.imageID)
	return storageerr.ErrInternal
}

// dtErrCtx — контекстный hint (id) для constraint-aware маппинга ошибок DiskType-репо.
type dtErrCtx struct {
	diskTypeID string
}

// mapDiskTypeErr транслирует pgx/pgconn-ошибку DiskType-репо в чистый sentinel
// с контрактным текстом Kachō (Q4). FK RESTRICT со стороны volumes → "DiskType <id>
// is in use". Сырой pgx/SQL наружу не течёт (uncategorized → storageerr.ErrInternal).
func mapDiskTypeErr(err error, c dtErrCtx) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, storageerr.ErrNotFound), errors.Is(err, storageerr.ErrAlreadyExists),
		errors.Is(err, storageerr.ErrFailedPrecondition), errors.Is(err, storageerr.ErrInvalidArg),
		errors.Is(err, storageerr.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: DiskType %s not found", storageerr.ErrNotFound, c.diskTypeID)
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique: // дубликат PK-слага
			return fmt.Errorf("%w: DiskType %s already exists", storageerr.ErrAlreadyExists, c.diskTypeID)
		case pgfault.ForeignKey: // volumes.disk_type_id RESTRICT (delete in-use, Q4)
			if f.Constraint == cnVolumeDiskTypeFK {
				return fmt.Errorf("%w: DiskType %s is in use", storageerr.ErrFailedPrecondition, c.diskTypeID)
			}
			return fmt.Errorf("%w: disk type violates a reference constraint", storageerr.ErrFailedPrecondition)
		case pgfault.Check: // description length / zone_ids array
			return checkViolation(f, "disk type", c.diskTypeID)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			append([]any{"disk_type_id", c.diskTypeID}, f.LogAttrs()...)...)
		return storageerr.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "disk_type_id", c.diskTypeID)
	return storageerr.ErrInternal
}

// checkViolation разбирает 23514 на две полосы по вопросу «чьё это значение»
// (задача #718; тот же разбор, что в vpc и nlb — класс, а не экземпляр).
//
// Форму имени storage проверяет сам — `validate.Name` / `validate.NameOrDefault`
// на всех трёх ресурсах (том, снимок, образ), на обоих путях записи. Значит
// ограничение таблицы, поставленное миграцией 715001, есть защита последнего
// рубежа, и его срабатывание означает, что негодное значение прошло МИМО
// проверки: дефект сервиса, а не ввода. `INVALID_ARGUMENT` здесь обвинял бы
// вызывающего в нашей ошибке и не давал бы ему ничего, что можно исправить.
//
// Прочие ограничения остаются отказом по вводу с прежним контрактным тоном
// («Illegal argument»). Имя ограничения наружу не идёт ни в одной из полос — оно
// идёт в журнал: ERROR для нашего дефекта, WARN для ввода. Ограничение, которое
// ловит ввод регулярно, — кандидат в синхронную проверку, и его частота обязана
// быть счётной.
func checkViolation(f pgfault.Fault, kind, id string) error {
	if pgfault.CheckLaneOf(f) == pgfault.LaneServiceDefect {
		slog.Error("name form backstop fired: service admitted a name it validates itself",
			append([]any{"kind", kind, "id", id}, f.LogAttrs()...)...)
		return storageerr.ErrInternal
	}
	slog.Warn("check constraint rejected caller input",
		append([]any{"kind", kind, "id", id}, f.LogAttrs()...)...)
	return fmt.Errorf("%w: Illegal argument", storageerr.ErrInvalidArg)
}
