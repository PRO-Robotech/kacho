// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

// Синхронизатор величин: то, чем снимок предела догоняет авторитет.
//
// # Что здесь появляется и почему это отдельный процесс, а не путь запроса
//
// Строка снимка заводится материализацией ОДИН раз — на первой мутации в
// проекте. Дальше промаха не случается, поэтому материализация не вызывается, а
// её запись идёт `ON CONFLICT DO NOTHING` и существующую строку не трогает. Без
// отдельного тянущего изменение величины не доезжает до живого проекта НИ ПРИ
// КАКИХ УСЛОВИЯХ — ни поднятие, ни понижение.
//
// Тянет ВЛАДЕЛЕЦ, а не толкает сосед: kacho-iam — лист, и толчок к нам был бы
// ребром обратно, то есть циклом.
//
// # Почему отказ здесь не роняет сервис
//
// Величина — не путь запроса. Недоступность соседа означает, что снимок какое-то
// время постоит со старой величиной; предел при этом продолжает действовать —
// место занимает триггер в writer-транзакции. Ронять сервис из-за отставшего
// снимка значило бы менять ограниченное отставание на полный отказ.
//
// Но и «молча продолжаем» не годится: каждый отказ называется в логе, а
// накопительные счётчики в таблице курсора делают заметным состояние «ни одна
// строка не синхронизирована за всё время» — иначе мёртвый тянущий неотличим от
// живого на неизменной конфигурации.

// limitDeltaSource — то единственное, что синхронизатору нужно от соседа.
//
// Объявлен здесь, у потребителя: composition root не обязан знать, чем именно
// клиент отличается от порта, а порт, объявленный у поставщика, связывал бы
// владельца типа с формой чужого клиента.
type limitDeltaSource interface {
	ListChangedSince(ctx context.Context, cursor string, pageSize int32) ([]corequota.Change, string, error)
}

// startQuotaLimitSyncer поднимает тянущего в фоне и возвращает его останов.
//
// Отказывает при сборке — и это намеренно: синхронизатор, собранный без
// источника или без проекции, исполнялся бы по расписанию и не делал ничего,
// оставаясь на вид работающим. Ровно тот класс, который эта задача и чинит.
func startQuotaLimitSyncer(
	ctx context.Context,
	pool *pgxpool.Pool,
	src limitDeltaSource,
	schema string,
	logger *slog.Logger,
) (func(), error) {
	proj, err := corequota.NewPgProjection(pool, schema)
	if err != nil {
		return nil, fmt.Errorf("quota limit projection: %w", err)
	}

	syncer, err := corequota.NewSyncer(src, proj, corequota.Config{},
		logger.With(slog.String("component", "quota-limit-syncer")))
	if err != nil {
		return nil, fmt.Errorf("quota limit syncer: %w", err)
	}

	// Первый проход — синхронный и до фонового цикла: он же и проверка, что
	// путь доставки существует. Отказ первого прохода НЕ фатален (сосед мог ещё
	// не подняться), но назван, поэтому «величины не догоняют» не приходится
	// выяснять по симптомам.
	if rows, ferr := syncer.RunOnce(ctx); ferr != nil {
		logger.Warn("resource-count quota: first limit sync failed, retrying in background",
			"error", ferr.Error())
	} else {
		logger.Info("resource-count quota: limit snapshot caught up", "rows", rows)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		syncer.Run(runCtx)
	}()

	return func() {
		cancel()
		<-done
	}, nil
}
