// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"
	"fmt"
	"log/slog"
)

// Подъём синхронизатора величин — одна сборка на всех владельцев.
//
// # Что здесь появляется и почему это отдельный процесс, а не путь запроса
//
// Строка снимка заводится материализацией ОДИН раз — на первой мутации в
// проекте. Дальше промаха не случается, поэтому материализация не вызывается, а
// её запись идёт `ON CONFLICT DO NOTHING` и существующую строку не трогает. Без
// отдельного тянущего изменение величины не доезжает до живого проекта НИ ПРИ
// КАКИХ УСЛОВИЯХ — ни поднятие, ни понижение.
//
// Тянет ВЛАДЕЛЕЦ, а не толкает сосед: владелец величин — лист, и толчок к нам
// был бы ребром обратно, то есть циклом.
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

// StartLimitSyncer поднимает тянущего в фоне и возвращает его останов.
//
// Отказывает ПРИ СБОРКЕ — и это намеренно: синхронизатор, собранный без
// источника или без проекции, исполнялся бы по расписанию и не делал ничего,
// оставаясь на вид работающим. Ровно тот класс, который этот механизм и чинит.
//
// Первый проход — синхронный и до фонового цикла: он же и проверка, что путь
// доставки существует. Отказ первого прохода НЕ фатален (сосед мог ещё не
// подняться), но назван, поэтому «величины не догоняют» не приходится выяснять
// по симптомам.
func StartLimitSyncer(
	ctx context.Context,
	db Execer,
	src Source,
	schema string,
	cfg Config,
	logger *slog.Logger,
) (func(), error) {
	if logger == nil {
		logger = slog.Default()
	}

	proj, err := NewPgProjection(db, schema)
	if err != nil {
		return nil, fmt.Errorf("quota limit projection: %w", err)
	}

	syncer, err := NewSyncer(src, proj, cfg,
		logger.With(slog.String("component", "quota-limit-syncer")))
	if err != nil {
		return nil, fmt.Errorf("quota limit syncer: %w", err)
	}

	switch rows, ran, ferr := syncer.RunOnce(ctx); {
	case ferr != nil:
		logger.Warn("resource-count quota: first limit sync failed, retrying in background",
			slog.String("error", ferr.Error()), slog.String("schema", schema))
	case !ran:
		// Проход держит другая реплика. Первый проход остаётся проверкой того,
		// что путь доставки существует, — но исполняет его одна реплика, и
		// «нас развели» обязано быть отличимо от «догнали».
		logger.Info("resource-count quota: first limit sync skipped, another replica holds the pass",
			slog.String("schema", schema))
	default:
		logger.Info("resource-count quota: limit snapshot caught up",
			slog.Int64("rows", rows), slog.String("schema", schema))
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
