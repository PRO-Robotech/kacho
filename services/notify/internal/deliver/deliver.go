// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package deliver — исполнители строк пачки `Claim` (замысел З21, З22,
// SDR-Н1): исход строки выбирает одна функция (decide.go) с объявленным
// порядком клеток, срок обработки строки считается от отправки `Claim` по
// монотонным часам notify, `Ack` идёт в собственном контексте до конца аренды.
//
// Что здесь решено и почему:
//
//   - Порядок клеток — один список [cells] (CX1-47 (а)); `template_skew`
//     первым (CX1-47 (в)), клетки 2–6 — до права (З22).
//   - Колонку `class` строки читает одно место — сравнение клетки 2 (CX1-52);
//     дальше строка описана [Resolved], в котором колонки нет. Держит это гейт
//     пакета (rowclass_gate_test.go).
//   - Крайний момент строки — `min(t_send + lease_remaining − AckMargin,
//     t_start + resolveSendTimeout + smtpSessionTimeout)`; под ним идут
//     `ResolveSend`, резерв сетки и SMTP до ответа на `DATA` (З21, CX1-27).
//     Моменты часов источника (`enqueued_at`) в вычисление не входят (УК71).
//   - Строку, которой не хватает аренды на сумму сроков либо чей срок прошёл,
//     notify не начинает и `Ack` не шлёт: исхода Р11 для «не начал» нет,
//     строку выдаст следующий `Claim` (клетка 8, З21).
//   - `Ack` — под `WithDeadline(WithoutCancel(<строка>), t_send +
//     lease_remaining)` и повторяется на `UNAVAILABLE` и `DEADLINE_EXCEEDED`
//     вызова до этого срока (SDR-Н1): повтор идемпотентен по З9.
//   - Письмо уходит только подписанным DKIM: подпись — последний шаг над
//     окончательными байтами сборщика до SMTP-сессии; пара подписи читается
//     у источника на каждом письме. Без подписчика [New] не собирается, отказ
//     подписи — строка отложена, неподписанного письма нет.
//   - Резерв сетки освобождается однажды на любом исходе, кроме `SENT`
//     (З24); ошибка базы резерва — не сторож: строка без `Ack` (CX1-68 (б)).
package deliver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/source"
)

// WorkersMax — верхняя граница числа исполнителей: размер `Claim` не больше
// числа свободных исполнителей и меньше предела ленты 500 (З21).
const WorkersMax = config.WorkersMax

// Build — сборка шаблонов notify: проверенный шаблон по пространству и имени.
type Build interface {
	Template(namespace, name string) (*spec.Template, bool)
}

// Grants — путь `ResolveSend` процесса (клетка 7).
type Grants interface {
	For(module string) (*grant.Gate, error)
}

// Limiter — сетка на адресата и потолок потока (клетка 9).
type Limiter interface {
	Reserve(ctx context.Context, row limits.Row) (*limits.Reservation, error)
	Release(ctx context.Context, res *limits.Reservation) error
}

// Renderer — сборщик письма по описанию строки (З25, полоса N5).
type Renderer interface {
	Render(Resolved) ([]byte, error)
}

// Sender — одна SMTP-сессия на письмо (З26).
type Sender interface {
	Send(ctx context.Context, env smtp.Envelope, msg []byte) smtp.Attempt
}

// Signer — подпись DKIM окончательного письма (`*dkim.Signer`): заголовок
// DKIM-Signature в начало, байты письма не меняются.
type Signer interface {
	Sign(msg []byte) ([]byte, error)
}

// Config — то, из чего собираются исполнители строк.
type Config struct {
	// Build — сборка шаблонов.
	Build Build
	// Sources — перечень источников (разобранный стражем конфигурации).
	Sources []config.Source
	// Grants — путь ResolveSend; у каждого источника перечня свой Gate.
	Grants Grants
	// Limiter — сетка на адресата.
	Limiter Limiter
	// Render — сборщик письма.
	Render Renderer
	// Sender — отправитель на ретранслятор.
	Sender Sender
	// Signer — подпись DKIM письма перед отправкой.
	Signer Signer
	// From — адрес отправителя установки в конверте (`notify.smtp.fromAddress`).
	From string
	// Workers — число исполнителей (`notify.workers`), [1..WorkersMax].
	Workers int
	// ResolveSendTimeout, SMTPSessionTimeout — сроки ручек; их сумма с
	// [config.AckMargin] — сумма сроков обработки строки (З21).
	ResolveSendTimeout time.Duration
	SMTPSessionTimeout time.Duration
	// DeferFor — отсрочка DEFER (`notify.deferFor`), [feed.MinDefer..feed.MaxDefer].
	DeferFor time.Duration
	// Clock — часы сроков строки.
	Clock Clock
	// Metrics — куда регистрируются счётчики исполнителей.
	Metrics prometheus.Registerer
	// Log — журнал исполнителей.
	Log *slog.Logger
}

// route — всё, что исполнителю нужно о строке своего источника.
type route struct {
	src  config.Source
	gate *grant.Gate
	// feed — путь `Ack` пачки ([source.Batch.Feed]); ставится на пачку.
	feed source.Feed
}

// Worker — исполнители строк: получатель пачек цикла источника
// ([source.Deliverer]).
type Worker struct {
	build    Build
	limiter  Limiter
	render   Renderer
	sender   Sender
	signer   Signer
	from     string
	routes   map[string]route
	resolve  time.Duration
	session  time.Duration
	deferFor time.Duration
	clock    Clock
	log      *slog.Logger
	m        *metrics

	slots chan struct{}
	wg    sync.WaitGroup
}

var _ source.Deliverer = (*Worker)(nil)

// New проверяет конфигурацию и собирает исполнителей. Отказ — ошибка
// программы с именем предмета; ни один счётчик при отказе не зарегистрирован
// частично для источника вне перечня.
func New(cfg Config) (*Worker, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("deliver: %w", err)
	}
	routes := make(map[string]route, len(cfg.Sources))
	modules := make([]string, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		if _, dup := routes[s.Module]; dup {
			return nil, fmt.Errorf("deliver: модуль %q назван в перечне дважды", s.Module)
		}
		g, err := cfg.Grants.For(s.Module)
		if err != nil {
			return nil, fmt.Errorf("deliver: %w", err)
		}
		routes[s.Module] = route{src: s, gate: g}
		modules = append(modules, s.Module)
	}
	m, err := newMetrics(cfg.Metrics, modules)
	if err != nil {
		return nil, fmt.Errorf("deliver: %w", err)
	}
	return &Worker{
		build:    cfg.Build,
		limiter:  cfg.Limiter,
		render:   cfg.Render,
		sender:   cfg.Sender,
		signer:   cfg.Signer,
		from:     cfg.From,
		routes:   routes,
		resolve:  cfg.ResolveSendTimeout,
		session:  cfg.SMTPSessionTimeout,
		deferFor: cfg.DeferFor,
		clock:    cfg.Clock,
		log:      cfg.Log,
		m:        m,
		slots:    make(chan struct{}, cfg.Workers),
	}, nil
}

func (c Config) validate() error {
	var errs []error
	if c.Build == nil {
		errs = append(errs, errors.New("сборка шаблонов не задана"))
	}
	if len(c.Sources) == 0 {
		errs = append(errs, errors.New("перечень источников пуст"))
	}
	if c.Grants == nil {
		errs = append(errs, errors.New("путь ResolveSend не задан"))
	}
	if c.Limiter == nil {
		errs = append(errs, errors.New("сетка на адресата не задана"))
	}
	if c.Render == nil {
		errs = append(errs, errors.New("сборщик письма не задан"))
	}
	if c.Sender == nil {
		errs = append(errs, errors.New("отправитель не задан"))
	}
	if c.Signer == nil {
		errs = append(errs, errors.New("подписчик DKIM не задан"))
	}
	if c.From == "" {
		errs = append(errs, errors.New("адрес отправителя установки пуст"))
	}
	if c.Workers < 1 || c.Workers > WorkersMax {
		errs = append(errs, fmt.Errorf("исполнителей %d вне [1..%d]", c.Workers, WorkersMax))
	}
	if c.ResolveSendTimeout < config.ResolveSendTimeoutMin || c.ResolveSendTimeout > config.ResolveSendTimeoutMax {
		errs = append(errs, fmt.Errorf("срок ResolveSend %v вне [%v..%v]",
			c.ResolveSendTimeout, config.ResolveSendTimeoutMin, config.ResolveSendTimeoutMax))
	}
	if c.SMTPSessionTimeout < config.SMTPSessionTimeoutMin || c.SMTPSessionTimeout > config.SMTPSessionTimeoutMax {
		errs = append(errs, fmt.Errorf("срок SMTP-сессии %v вне [%v..%v]",
			c.SMTPSessionTimeout, config.SMTPSessionTimeoutMin, config.SMTPSessionTimeoutMax))
	}
	if c.DeferFor < feed.MinDefer || c.DeferFor > feed.MaxDefer {
		errs = append(errs, fmt.Errorf("отсрочка %v вне [%v..%v]", c.DeferFor, feed.MinDefer, feed.MaxDefer))
	}
	if c.Clock == nil {
		errs = append(errs, errors.New("часы не заданы"))
	}
	if c.Metrics == nil {
		errs = append(errs, errors.New("реестр метрик не задан"))
	}
	if c.Log == nil {
		errs = append(errs, errors.New("журнал не задан"))
	}
	return errors.Join(errs...)
}

// Free — сколько исполнителей свободно.
func (w *Worker) Free() int { return cap(w.slots) - len(w.slots) }

// Deliver раздаёт строки пачки исполнителям и возвращается, не дожидаясь их.
// Строка пачки, которой не хватило свободного исполнителя, ждёт его здесь:
// цикл источника последователен и размер `Claim` берёт из [Worker.Free].
//
// Строка доводится и после отмены ctx (остановка процесса): её путь
// ограничен крайним моментом строки, а `Ack` — концом аренды (З21, З24 (в)).
func (w *Worker) Deliver(ctx context.Context, b source.Batch) {
	rt, ok := w.routes[b.Source.Module]
	if !ok {
		// Цикл источника вне перечня исполнителей — ошибка сборки процесса.
		// Строки без Ack: их выдаст следующий Claim после конца аренды.
		w.log.Error("пачка источника вне перечня исполнителей: строки не обработаны",
			"source", b.Source.Module, "rows", len(b.Rows))
		return
	}
	if b.Feed == nil {
		// Пачка без пути Ack — ошибка сборки цикла источника: исход строки
		// записать некуда, и строку не начинают вовсе (ни права, ни SMTP).
		// Строки без Ack: их выдаст следующий Claim после конца аренды.
		w.log.Error("пачка без пути Ack: строки не обработаны",
			"source", b.Source.Module, "rows", len(b.Rows))
		return
	}
	rt.feed = b.Feed
	base := context.WithoutCancel(ctx)
	for _, row := range b.Rows {
		w.slots <- struct{}{}
		w.wg.Add(1)
		go func() {
			defer func() {
				<-w.slots
				w.wg.Done()
			}()
			w.handle(base, rt, b.SentAt, row)
		}()
	}
}

// Wait дожидается строк в полёте.
func (w *Worker) Wait() { w.wg.Wait() }
