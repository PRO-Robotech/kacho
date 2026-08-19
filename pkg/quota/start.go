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
	rec Recorder,
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

	// Сток — ОБЯЗАТЕЛЬНЫЙ параметр, а не вариативная настройка. Причина не в
	// стиле: пять владельцев уже подняли тянущего, и опция дала бы всем пятерым
	// молчаливое умолчание «стока нет». Обязательный параметр ломает сборку у
	// каждого, то есть заставляет ПРИНЯТЬ РЕШЕНИЕ, а шестому владельцу не даёт
	// не заметить, что решение вообще есть.
	//
	// Нулевой сток остаётся законным — наблюдение не вправе ронять доставку
	// величин, — но молчать о себе он не будет: слепой тянущий снаружи
	// неотличим от живого (на неизменной конфигурации живой правит ноль строк),
	// поэтому единственное место, где эта разница ещё видна, — журнал подъёма.
	// Что «ни один вызов не поднимает тянущего вслепую», держит гейт дерева
	// `TestEveryLimitSyncerCarriesItsObservabilitySink`: nil компилируется.
	if rec == nil {
		logger.Warn("resource-count quota: limit syncer started WITHOUT an observability sink — "+
			"pulls, failures, applied rows and the moment of the last success are not published, "+
			"so a dead syncer is indistinguishable from a live one on an unchanged configuration",
			slog.String("schema", schema))
	} else {
		syncer.WithObserver(schema, rec)
	}

	// Догоняющий проход — ОДИН, синхронный и ПОД СВОИМ сроком. Цикл ниже ждёт
	// тик перед каждым своим проходом, поэтому второго на подъёме не будет:
	// прежде их было два, с интервалом в десятки миллисекунд, и второй повторял
	// тот же вопрос тому же соседу заведомо в том же состоянии сети.
	if rows, ferr := syncer.RunOnceWithin(ctx); ferr != nil {
		logger.Warn("resource-count quota: first limit sync failed, retrying in background",
			slog.String("error", ferr.Error()), slog.String("schema", schema))
	} else {
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
