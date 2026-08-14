// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"log/slog"
	"sync/atomic"
)

// Observer — наблюдаемость шва. Считает то, чего иначе не увидит никто.
//
// # Почему счётчики, а не только записи в журнал
//
// Предел незавершённых сообщений, срабатывающий молча, неотличим от предела,
// который не срабатывал ни разу, — а «ни разу за всю жизнь» есть отдельное
// утверждение, и оно обязано быть проверяемым. Поэтому Observer держит
// НАКОПИТЕЛЬНЫЕ величины и печатает их при закрытии КАЖДОГО потока, включая
// нули: «переполнений 0» напечатанное и «переполнений не считали» иначе
// выглядят одинаково.
//
// # Почему конкретный тип, а не порт
//
// Порт понадобился бы, чтобы подменить наблюдателя в пробе. Подменять здесь
// нечего: наблюдатель не принимает решений и не ходит наружу — он пишет в
// журнал и складывает числа, а проба читает те же числа методами ниже. Порт
// добавил бы второй набор реализаций, расходящийся с этой молча.
type Observer struct {
	log *slog.Logger

	streamsStarted  atomic.Int64
	streamsFinished atomic.Int64
	intentsSent     atomic.Int64
	overflows       atomic.Int64
	resyncs         atomic.Int64
	staleReports    atomic.Int64
	missingBodies   atomic.Int64
	streamsRefused  atomic.Int64
}

// NewObserver собирает наблюдателя. logger обязателен: наблюдатель без места,
// куда писать, — форма без содержания.
func NewObserver(logger *slog.Logger) *Observer {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Observer{log: logger}
}

// StreamStarted — подписка принята.
func (o *Observer) StreamStarted(known int64) {
	o.streamsStarted.Add(1)
	o.log.Info("dataplane intent stream started",
		"known_revision", known,
		"streams_started_total", o.streamsStarted.Load())
}

// StreamRefused — подписка отклонена до подключения к бэкенду.
func (o *Observer) StreamRefused(why string) {
	o.streamsRefused.Add(1)
	o.log.Warn("dataplane intent stream refused",
		"why", why,
		"streams_refused_total", o.streamsRefused.Load())
}

// StreamFinished — подписка закрыта. Печатает ВСЕ накопительные величины,
// включая нулевые: ради этого места они и накапливаются.
func (o *Observer) StreamFinished(sent int64, err error) {
	o.streamsFinished.Add(1)
	o.log.Info("dataplane intent stream finished",
		"intents_sent", sent,
		"err", err,
		"streams_started_total", o.streamsStarted.Load(),
		"streams_finished_total", o.streamsFinished.Load(),
		"intents_sent_total", o.intentsSent.Load(),
		"overflow_total", o.overflows.Load(),
		"resync_total", o.resyncs.Load(),
		"stale_reports_total", o.staleReports.Load(),
		"missing_bodies_total", o.missingBodies.Load())
}

// IntentSent — отдано одно намерение.
func (o *Observer) IntentSent() { o.intentsSent.Add(1) }

// Overflowed — предел незавершённых сообщений исчерпан.
//
// Уровень ERROR, а не WARN: это не «замедлились», а «сообщение не доставлено
// и поток отправлен на полную пересинхронизацию». Тихая запись здесь означала
// бы мёртвый предел, о срабатывании которого узнают по вторичным признакам.
func (o *Observer) Overflowed(limit int, cursor int64) {
	o.overflows.Add(1)
	o.log.Error("dataplane intent stream overflowed its pending limit; forcing a full resync",
		"pending_limit", limit,
		"cursor_revision", cursor,
		"overflow_total", o.overflows.Load())
}

// ResyncSignalled — исполнителю сказано начинать с полной выдачи.
func (o *Observer) ResyncSignalled(cause string, known int64, b Bounds) {
	o.resyncs.Add(1)
	o.log.Warn("dataplane intent stream signalled a full resync",
		"cause", cause,
		"known_revision", known,
		"horizon_revision", b.Horizon,
		"head_revision", b.Head,
		"resync_total", o.resyncs.Load())
}

// ReportStale — подтверждение старее уже записанного.
func (o *Observer) ReportStale(resourceID string, reported, current int64) {
	o.staleReports.Add(1)
	o.log.Info("dataplane apply report is stale and was not counted as fresh",
		"resource_id", resourceID,
		"reported_revision", reported,
		"current_revision", current,
		"stale_reports_total", o.staleReports.Load())
}

// BodyMissing — у живого намерения не нашлось строки ресурса.
//
// Означает рассогласование проекции с таблицей ресурса — состояние, которого не
// бывает, пока намерение штампуется триггером в той же транзакции, что и сама
// мутация. Уровень ERROR: пропуск строки виден только здесь.
func (o *Observer) BodyMissing(kind Kind, resourceID string, revision int64) {
	o.missingBodies.Add(1)
	o.log.Error("dataplane intent row has no resource body; the projection disagrees with its table",
		"kind", string(kind),
		"resource_id", resourceID,
		"revision", revision,
		"missing_bodies_total", o.missingBodies.Load())
}

// Totals — накопленные величины. Существует ради проб: утверждать «предел
// сработал» по тексту журнала значило бы разбирать прозу.
type Totals struct {
	StreamsStarted  int64
	StreamsFinished int64
	StreamsRefused  int64
	IntentsSent     int64
	Overflows       int64
	Resyncs         int64
	StaleReports    int64
	MissingBodies   int64
}

// Totals снимает накопленные величины.
func (o *Observer) Totals() Totals {
	return Totals{
		StreamsStarted:  o.streamsStarted.Load(),
		StreamsFinished: o.streamsFinished.Load(),
		StreamsRefused:  o.streamsRefused.Load(),
		IntentsSent:     o.intentsSent.Load(),
		Overflows:       o.overflows.Load(),
		Resyncs:         o.resyncs.Load(),
		StaleReports:    o.staleReports.Load(),
		MissingBodies:   o.missingBodies.Load(),
	}
}
