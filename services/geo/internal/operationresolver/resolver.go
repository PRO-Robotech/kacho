// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package operationresolver — доменный resolver осиротевших LRO для kacho-geo.
//
// Движок reconciler'а живёт в kacho-corelib/operations (сканирует таблицу
// operations по grace-окну, клеймит orphan'ы под FOR UPDATE SKIP LOCKED). Сам
// resolver — доменная часть в сервисе: он знает типы метаданных операций geo
// (*geov1.<Verb><Resource>Metadata) и сверяет осиротевшую операцию с
// committed-реальностью ресурса через read-порт repo.Get.
//
// Контракт диспетчеризации (writer-TX атомарна, частичных состояний нет):
//   - Create-метаданные: ресурс присутствует → Done(current как Response);
//     отсутствует → Interrupted.
//   - Update-метаданные (существование ресурса не меняют): присутствует →
//     Done(current); отсутствует → Interrupted.
//   - Delete-метаданные: отсутствует → Done(Empty); присутствует → Interrupted.
//   - неузнанный / nil тип метаданных → Skip (строка остаётся done=false, sweep
//     повторится);
//   - transient-ошибка чтения ресурса → (ResolverResult{}, err): движок
//     инкрементит reconcile_errors и пропускает orphan до следующего sweep'а.
//
// Resolver не делает re-drive (повторный запуск worker-fn) — он приводит статус
// операции в соответствие тому, что реально закоммичено.
package operationresolver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
)

// RegionReader / ZoneReader — узкие read-порты двух мутируемых ресурсов geo. Get
// возвращает текущий proto-ресурс, либо geoerrors.ErrNotFound (absent), либо
// transient-ошибку. Реализуются adapter'ами в composition root поверх pg-репо.
type RegionReader interface {
	Get(ctx context.Context, id string) (*geov1.Region, error)
}

type ZoneReader interface {
	Get(ctx context.Context, id string) (*geov1.Zone, error)
}

// Readers — набор read-портов, инжектируемый composition root'ом. Незаполненный
// (nil) порт → соответствующие orphan'ы пропускаются (Skip).
type Readers struct {
	Region RegionReader
	Zone   ZoneReader
}

// kind — категория операции, выводимая из типа метаданных. Три значения намеренно
// зеркалят таксономию метаданных corelib-Resolver (Create/Update/Delete), а не
// схлопнуты в bool: kindCreate и kindUpdate дают идентичный исход (present →
// Done(current)) не по копипасте, а потому что таков ПЛАТФОРМЕННЫЙ контракт
// reconcile (kacho-corelib/operations: «Create/Update-метаданные: present →
// Done(current)» — reconcile к committed-реальности, НЕ re-apply). Отдельные
// самодокументирующие метки на switch-диспетче (case *Update*Metadata →
// kindUpdate) сохранены как type-level seam: если платформенный контракт когда-либо
// разведёт update-семантику (напр. orphan-Update → Interrupted вместо Done, см.
// docs/architecture/known-divergences.md §7), точка расхождения уже названа.
type kind int

const (
	kindCreate kind = iota // present → Done(current); absent → Interrupted
	kindUpdate             // ТОЖДЕСТВЕНЕН kindCreate по контракту reconcile (см. выше), метка намеренная
	kindDelete             // absent → Done(Empty); present → Interrupted
)

// Resolver — доменный resolver geo поверх узких read-портов репозиториев.
type Resolver struct {
	r   Readers
	log *slog.Logger
}

// New конструирует Resolver поверх набора read-портов. logger — структурированный
// логгер диагностики resolve; nil → slog.Default().
func New(r Readers, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{r: r, log: logger}
}

// Resolve реализует operations.Resolver: по метаданным осиротевшей операции
// определяет терминальный исход, сверяясь с committed-реальностью ресурса.
func (rs *Resolver) Resolve(ctx context.Context, op operations.Operation) (operations.ResolverResult, error) {
	if op.Metadata == nil {
		return skip(), nil
	}
	msg, err := op.Metadata.UnmarshalNew()
	if err != nil {
		// Неизвестный / неразбираемый тип метаданных — не наша операция в этом
		// прогоне. Skip, а не ошибка: строка остаётся done=false.
		rs.log.Warn("operation resolver: undecodable metadata, skipping orphan",
			"op", op.ID, "type_url", op.Metadata.TypeUrl, "err", err)
		return skip(), nil
	}

	switch m := msg.(type) {
	// ---- Region: Create / Update / Delete ----
	case *geov1.CreateRegionMetadata:
		return resolveExistence(ctx, kindCreate, m.GetRegionId(), rs.r.Region)
	case *geov1.UpdateRegionMetadata:
		return resolveExistence(ctx, kindUpdate, m.GetRegionId(), rs.r.Region)
	case *geov1.DeleteRegionMetadata:
		return resolveExistence(ctx, kindDelete, m.GetRegionId(), rs.r.Region)

	// ---- Zone: Create / Update / Delete ----
	case *geov1.CreateZoneMetadata:
		return resolveExistence(ctx, kindCreate, m.GetZoneId(), rs.r.Zone)
	case *geov1.UpdateZoneMetadata:
		return resolveExistence(ctx, kindUpdate, m.GetZoneId(), rs.r.Zone)
	case *geov1.DeleteZoneMetadata:
		return resolveExistence(ctx, kindDelete, m.GetZoneId(), rs.r.Zone)

	default:
		// Прочие (не-операционные / unwired) типы метаданных — не наши.
		return skip(), nil
	}
}

// resolveExistence — общая логика «существование ресурса → терминальный исход».
// reader.Get читает proto-ресурс (geoerrors.ErrNotFound → отсутствует). Если
// reader не сконфигурирован (nil — dev/неполный wiring), orphan пропускается (Skip).
func resolveExistence[T proto.Message](
	ctx context.Context,
	k kind,
	id string,
	reader interface {
		Get(context.Context, string) (T, error)
	},
) (operations.ResolverResult, error) {
	// Порт не сконфигурирован (Readers.Region/Zone не задан) → Skip.
	if reader == nil {
		return skip(), nil
	}
	rec, err := reader.Get(ctx, id)
	present := false
	switch {
	case err == nil:
		present = true
	case errors.Is(err, geoerrors.ErrNotFound):
		present = false
	default:
		// transient read-ошибка → движок инкрементит reconcile_errors, пропускает.
		return operations.ResolverResult{}, fmt.Errorf("operationresolver: get %q: %w", id, err)
	}

	if k == kindDelete {
		if present {
			return interrupted(), nil
		}
		return done(nil), nil // Empty-семантика
	}
	// Create / Update.
	if !present {
		return interrupted(), nil
	}
	resp, err := anypb.New(rec)
	if err != nil {
		return operations.ResolverResult{}, fmt.Errorf("operationresolver: marshal %q: %w", id, err)
	}
	return done(resp), nil
}

func skip() operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeSkip}
}

func interrupted() operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeInterrupted}
}

func done(resp *anypb.Any) operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeDone, Response: resp}
}
