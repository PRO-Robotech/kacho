// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Durable LRO recovery wiring: доменный resolver geo + corelib-reconciler.
//
// При крахе процесса (OOM / kill -9) live-worker'ы умирают, их in-flight
// операции остаются done=false навсегда (worker добирает только операции,
// диспетчеризованные в ЭТОМ процессе — клиентский poll OperationService.Get
// никогда не done). Reconciler при старте (RecoverAll — ДО приёма трафика) и
// периодическим sweep'ом (Run — backstop под супервизором) разрешает осиротевшие
// операции в терминал, сверяясь с committed-реальностью ресурса через доменный
// resolver. Покрывает backlog-overflow, исчерпание terminal-write retry, shutdown
// и crash mid-op — тот самый backstop, который обещает комментарий serve.go про
// «reconciler добьёт durable-строку done=false».

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/geo/internal/operationresolver"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

const (
	// geoReconcileOrphanGrace — orphan-кандидат должен быть старше этого окна,
	// чтобы reconciler не разрешил преждевременно ещё-живого worker'а. Должен
	// превышать максимальную ожидаемую длительность операции.
	geoReconcileOrphanGrace = 5 * time.Minute
	// geoReconcileInterval — каденция периодического backstop-sweep'а.
	geoReconcileInterval = 30 * time.Second
	// geoReconcileBatchSize — размер пачки claim'а за один sweep.
	geoReconcileBatchSize = 100
	// geoOperationsSchema — schema-квалификатор таблицы operations geo (совпадает
	// с operations.NewRepo(pool, "kacho_geo")).
	geoOperationsSchema = "kacho_geo"
)

// startLRORecovery конструирует доменный resolver (поверх pg-read-портов) +
// corelib-reconciler поверх schema kacho_geo, прогоняет startup-recovery
// (RecoverAll, ДО Serve) и возвращает reconciler — его периодический Run(ctx)
// вешается на супервизор в runServe. Ошибка startup-recovery — не фатальна
// (best-effort backstop; периодический Run добьёт позже): boot не валится из-за
// transient DB-сбоя reconciler'а.
func startLRORecovery(ctx context.Context, pool *pgxpool.Pool, regionRepo *pg.RegionRepo, zoneRepo *pg.ZoneRepo, rec operations.Recorder, logger *slog.Logger) *operations.Reconciler {
	readers := operationresolver.Readers{
		Region: regionReader{repo: regionRepo},
		Zone:   zoneReader{repo: zoneRepo},
	}
	resolver := operationresolver.New(readers, logger)
	reconciler := operations.NewReconciler(pool, resolver, operations.ReconcilerConfig{
		Schema:      geoOperationsSchema,
		OrphanGrace: geoReconcileOrphanGrace,
		BatchSize:   geoReconcileBatchSize,
		Interval:    geoReconcileInterval,
	},
		operations.WithReconcilerRecorder(rec),
		operations.WithReconcilerLogger(logger.With(slog.String("component", "lro-reconciler"))),
	)

	if err := reconciler.RecoverAll(ctx); err != nil {
		logger.Error("LRO startup-recovery failed; periodic sweep will retry", "err", err)
	} else {
		logger.Info("LRO startup-recovery complete (orphaned operations resolved)")
	}
	return reconciler
}

// regionReader / zoneReader — adapter'ы pg-репо → read-порты resolver'а. Каждый
// читает запись по id и конвертит domain→proto через ЕДИНЫЙ protoconv (та же
// проекция, что use-case-marshaller и handler — reconciler разрешает осиротевшую
// операцию в тот же response, что вернул бы обычный worker, без риска дрейфа полей).
// pg-репо возвращает geoerrors.ErrNotFound для отсутствующего ресурса — resolver
// трактует это как absent.

type regionReader struct{ repo *pg.RegionRepo }

func (r regionReader) Get(ctx context.Context, id string) (*geov1.Region, error) {
	rg, err := r.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return protoconv.Region(rg), nil
}

type zoneReader struct{ repo *pg.ZoneRepo }

func (r zoneReader) Get(ctx context.Context, id string) (*geov1.Zone, error) {
	z, err := r.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return protoconv.Zone(z), nil
}
