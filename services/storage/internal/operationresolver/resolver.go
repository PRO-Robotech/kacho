// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package operationresolver — доменный разрешитель осиротевших операций kacho-storage.
//
// Движок живёт в corelib (pkg/operations.Reconciler): он сканирует таблицу
// операций по grace-окну и клеймит кандидатов под FOR UPDATE SKIP LOCKED.
// Доменная часть — здесь: она знает типы метаданных storage и сверяет
// осиротевшую строку с тем, что РЕАЛЬНО закоммичено, через repo.Get.
//
// Зачем это вообще нужно. Строка операции коммитится ДО запуска работы, а живой
// исполнитель добирает только то, что диспетчеризовал сам этот процесс. Значит
// перекат пода, OOM, исчерпание бюджета терминальной записи и переполнение
// очереди исполнителя оставляют строку done=false НАВСЕГДА: клиент поллит её до
// конца своего терпения и не узнаёт исхода ни разу. Схема storage этого
// разрешителя ЗАЯВЛЯЛА (миграция 0002 несёт частичный индекс, построенный ровно
// под его запрос: `ON kacho_storage.operations (modified_at) WHERE NOT done`), а
// проводка его не содержала.
//
// Контракт диспетчеризации (writer-TX атомарна, частичных состояний нет):
//   - Create-метаданные: ресурс есть → Done(текущий ресурс в Response); нет → Interrupted;
//   - Update-метаданные (существование не меняют): есть → Done(текущий); нет → Interrupted;
//   - Delete-метаданные: нет → Done(Empty); есть → Interrupted;
//   - неузнанный / nil тип метаданных → Skip (строка остаётся, следующий проход повторит);
//   - transient-ошибка чтения → ошибка наверх: движок считает её и пропускает кандидата.
//
// Покрытие — три мутируемых ресурса storage (Volume / Snapshot / Image), то есть
// ВСЕ типы метаданных, которые этот сервис эмитит: каждая точка operations.Run
// в services/storage несёт один из девяти разобранных ниже типов. Ветка default
// существует для чужих/будущих типов, а не как признанная слепая зона.
//
// Разрешитель НЕ переигрывает работу. Он перестаёт врать о её состоянии.
package operationresolver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/anypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// Узкие read-порты трёх мутируемых ресурсов storage. Удовлетворяются
// соответствующими репозиториями pg.
type (
	// VolumeReader читает том по id (storageerr.ErrNotFound → тома нет).
	VolumeReader interface {
		Get(ctx context.Context, id string) (*domain.Volume, error)
	}
	// SnapshotReader читает снимок по id.
	SnapshotReader interface {
		Get(ctx context.Context, id string) (*domain.Snapshot, error)
	}
	// ImageReader читает образ по id.
	ImageReader interface {
		Get(ctx context.Context, id string) (*domain.Image, error)
	}
)

// Readers — набор read-портов, инжектируемый композиционным корнем.
type Readers struct {
	Volume   VolumeReader
	Snapshot SnapshotReader
	Image    ImageReader
}

// kind — категория операции, выводимая из типа метаданных.
type kind int

const (
	kindCreate kind = iota // есть → Done(текущий); нет → Interrupted
	kindUpdate             // то же (сверка с закоммиченным, не повтор работы)
	kindDelete             // нет → Done(Empty); есть → Interrupted
)

// Resolver — доменный разрешитель storage поверх узких read-портов.
type Resolver struct {
	r   Readers
	log *slog.Logger
}

// Option — функциональная опция Resolver.
type Option func(*Resolver)

// WithLogger подключает логгер (диагностика разбора).
func WithLogger(l *slog.Logger) Option {
	return func(r *Resolver) {
		if l != nil {
			r.log = l
		}
	}
}

// New конструирует Resolver поверх набора read-портов.
func New(r Readers, opts ...Option) *Resolver {
	rs := &Resolver{r: r, log: slog.Default()}
	for _, o := range opts {
		o(rs)
	}
	return rs
}

// Resolve реализует operations.Resolver.
func (rs *Resolver) Resolve(ctx context.Context, op operations.Operation) (operations.ResolverResult, error) {
	if op.Metadata == nil {
		return skip(), nil
	}
	msg, err := op.Metadata.UnmarshalNew()
	if err != nil {
		// Неразбираемый тип метаданных — не наша операция в этом проходе. Skip, а не
		// ошибка: строка остаётся, и её разберёт тот, чей это тип.
		rs.log.Warn("operation resolver: undecodable metadata, skipping orphan",
			"op", op.ID, "type_url", op.Metadata.TypeUrl, "err", err)
		return skip(), nil
	}

	switch m := msg.(type) {
	case *storagev1.CreateVolumeMetadata:
		return resolveExistence(ctx, kindCreate, m.GetVolumeId(), rs.r.Volume, marshalVolume)
	case *storagev1.UpdateVolumeMetadata:
		return resolveExistence(ctx, kindUpdate, m.GetVolumeId(), rs.r.Volume, marshalVolume)
	case *storagev1.DeleteVolumeMetadata:
		return resolveExistence(ctx, kindDelete, m.GetVolumeId(), rs.r.Volume, marshalVolume)

	case *storagev1.CreateSnapshotMetadata:
		return resolveExistence(ctx, kindCreate, m.GetSnapshotId(), rs.r.Snapshot, marshalSnapshot)
	case *storagev1.UpdateSnapshotMetadata:
		return resolveExistence(ctx, kindUpdate, m.GetSnapshotId(), rs.r.Snapshot, marshalSnapshot)
	case *storagev1.DeleteSnapshotMetadata:
		return resolveExistence(ctx, kindDelete, m.GetSnapshotId(), rs.r.Snapshot, marshalSnapshot)

	case *storagev1.CreateImageMetadata:
		return resolveExistence(ctx, kindCreate, m.GetImageId(), rs.r.Image, marshalImage)
	case *storagev1.UpdateImageMetadata:
		return resolveExistence(ctx, kindUpdate, m.GetImageId(), rs.r.Image, marshalImage)
	case *storagev1.DeleteImageMetadata:
		return resolveExistence(ctx, kindDelete, m.GetImageId(), rs.r.Image, marshalImage)

	default:
		return skip(), nil
	}
}

// resolveExistence — общая логика «существует ли ресурс → терминальный исход».
// Незаданный читатель (неполная проводка) → Skip: выдумывать исход по ресурсу,
// которого не читали, нельзя.
func resolveExistence[T any](
	ctx context.Context,
	k kind,
	id string,
	reader interface {
		Get(context.Context, string) (*T, error)
	},
	toAny func(*T) (*anypb.Any, error),
) (operations.ResolverResult, error) {
	if reader == nil {
		return skip(), nil
	}
	rec, err := reader.Get(ctx, id)
	switch {
	case err == nil:
		// есть
	case errors.Is(err, storageerr.ErrNotFound):
		rec = nil // нет
	default:
		return operations.ResolverResult{}, fmt.Errorf("operationresolver: get %q: %w", id, err)
	}

	present := rec != nil
	if k == kindDelete {
		if present {
			return interrupted(), nil
		}
		return done(nil), nil // Empty-семантика
	}
	if !present {
		return interrupted(), nil
	}
	resp, err := toAny(rec)
	if err != nil {
		return operations.ResolverResult{}, fmt.Errorf("operationresolver: marshal %q: %w", id, err)
	}
	return done(resp), nil
}

func marshalVolume(v *domain.Volume) (*anypb.Any, error)     { return anypb.New(protoconv.Volume(v)) }
func marshalSnapshot(s *domain.Snapshot) (*anypb.Any, error) { return anypb.New(protoconv.Snapshot(s)) }
func marshalImage(i *domain.Image) (*anypb.Any, error)       { return anypb.New(protoconv.Image(i)) }

func skip() operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeSkip}
}

func interrupted() operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeInterrupted}
}

func done(resp *anypb.Any) operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeDone, Response: resp}
}
