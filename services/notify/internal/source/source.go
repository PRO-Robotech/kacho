// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package source — сторона notify у лент источников (замысел З21): перечень
// источников, клиент с точным SAN сервера ленты, подписка на ленту и цикл
// `Claim`. На каждый источник — один цикл; зависший источник держит только
// свой цикл и не дольше [sourceCallTimeout] на вызов.
//
// Что делать со строкой пачки — решение получателя ([Deliverer]): разрешение
// `ResolveSend` или исключение `certificate` (NTF1-G22), рендер, SMTP и `Ack`.
// Цикл передаёт получателю запись перечня вместе с пачкой и моментом отправки
// запроса `Claim` по монотонным часам notify.
package source

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// sourceCallTimeout — срок вызова `Claim` и установления потока `Subscribe`
// до `SubscriptionOpened` (§8 замысла, SDR-Н2, CX1-71, CX1-72). Константа
// сборки, а не ручка `notify.claimInterval`: у ручки нижняя граница 1 с, и
// срок, равный ей, отменял бы вызов живого, но медленного источника. Довод
// числа — §8: ответ к концу срока оставляет строке не меньше
// `feed.LeaseTTL − sourceCallTimeout`, что выше суммы сроков обработки строки.
const sourceCallTimeout = 10 * time.Second

// kanameModule — единственная запись перечня, которой допустимо исключение
// `authorization: certificate` (приёмка Р3, NTF1-G22).
const kanameModule = "kaname"

// Batch — пачка одного вызова `Claim` одного источника.
type Batch struct {
	// Source — запись перечня источника: получатель по ней выбирает способ
	// подтверждения права (`resolveSend` либо исключение `certificate`).
	Source config.Source
	// Rows — строки, чья аренда выдана этим вызовом.
	Rows []*notifyv1.ClaimedNotification
	// SentAt — показание монотонных часов notify в момент ОТПРАВКИ запроса
	// `Claim` (`t_send`, З21): конец аренды для notify — `SentAt +
	// lease_remaining`, конец срока строки — `SentAt + expires_in`. Значение
	// не проходит через `.UTC()`, `.Round()` и сериализацию — монотонное
	// показание снимается ими (УК80).
	SentAt time.Time
	// Feed — путь `Ack` строк пачки: клиент ТОГО ЖЕ соединения, по которому
	// строки взяты в аренду (то же удостоверение notify и точный SAN сервера
	// ленты). Второго соединения к источнику у исполнителей нет.
	Feed Feed
}

// Feed — лента источника в части `Ack` (клиент gRPC ленты).
type Feed interface {
	Ack(ctx context.Context, req *notifyv1.AckRequest, opts ...grpc.CallOption) (*notifyv1.AckResponse, error)
}

// Deliverer — получатель пачек: исполнители строк.
type Deliverer interface {
	// Free — сколько исполнителей свободно сейчас. Размер `Claim` не больше
	// этого числа; при нуле `Claim` не зовётся (З21 «Размер `Claim`»).
	Free() int
	// Deliver принимает пачку. Зовётся на цикле источника последовательно;
	// строки пачки получатель обрабатывает параллельно, а не здесь по одной.
	Deliver(ctx context.Context, b Batch)
	// Wait дожидается строк в полёте: их `Ack` идёт по соединению пачки, и
	// [Loops.Wait] закрывает соединения только после него.
	Wait()
}

// Config — то, из чего поднимаются циклы.
type Config struct {
	// Sources — перечень источников, разобранный стражем конфигурации.
	Sources []config.Source
	// Peer — удостоверение notify для mTLS к источникам: клиентский лист и
	// УЦ серверов. Точный SAN сервера цикл сверяет сам по записи перечня;
	// конфигурация вызывающего не меняется.
	Peer *tls.Config
	// ClaimInterval — такт `Claim` по таймеру (`notify.claimInterval`).
	ClaimInterval time.Duration
	// Deliverer — получатель пачек.
	Deliverer Deliverer
	// Metrics — куда регистрируются счётчики циклов.
	Metrics prometheus.Registerer
	// Log — журнал циклов.
	Log *slog.Logger
}

// Loops — поднятые циклы источников.
type Loops struct {
	wg        sync.WaitGroup
	conns     []io.Closer
	deliverer Deliverer
}

// Wait ждёт, пока все циклы остановятся после отмены контекста [Start], затем
// — строк в полёте у получателя ([Deliverer.Wait]: их `Ack` идёт по
// соединениям циклов), и только после этого закрывает соединения с
// источниками. Обратный порядок роняет `Ack` строки, чьё письмо уже ушло, и
// следующий `Claim` выдаёт её снова — дубль письма.
func (l *Loops) Wait() {
	l.wg.Wait()
	l.deliverer.Wait()
	for _, c := range l.conns {
		_ = c.Close()
	}
}

// Start проверяет перечень и поднимает по циклу на источник. Отказ — до
// подъёма чего бы то ни было: ни соединения, ни регистрации счётчиков.
// Циклы живут до отмены ctx; [Loops.Wait] дожидается их остановки.
func Start(ctx context.Context, cfg Config) (*Loops, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	m, err := newMetrics(cfg.Metrics)
	if err != nil {
		return nil, err
	}
	loops := &Loops{deliverer: cfg.Deliverer}
	built := make([]*loop, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		lp, err := newLoop(cfg, src, m)
		if err != nil {
			for _, b := range built {
				_ = b.conn.Close()
			}
			return nil, err
		}
		built = append(built, lp)
	}
	for _, lp := range built {
		loops.conns = append(loops.conns, lp.conn)
		loops.wg.Add(1)
		go func() {
			defer loops.wg.Done()
			lp.run(ctx)
		}()
	}
	return loops, nil
}

// validate — страж перечня: исключение `certificate` допустимо только
// записи `kaname` (NTF1-G22); прочее — обязательные части конфигурации.
func (c Config) validate() error {
	var errs []error
	if len(c.Sources) == 0 {
		errs = append(errs, errors.New("перечень источников пуст"))
	}
	for i, s := range c.Sources {
		if s.Authorization == config.AuthorizationCertificate && s.Module != kanameModule {
			errs = append(errs, fmt.Errorf("запись #%d (модуль %q): authorization: %s допустим только "+
				"записи %q — исключение Р3 (NTF1-G22)", i+1, s.Module, config.AuthorizationCertificate, kanameModule))
		}
		if len(s.Classes) == 0 {
			errs = append(errs, fmt.Errorf("запись #%d (модуль %q): перечень классов пуст", i+1, s.Module))
		}
		for _, cl := range s.Classes {
			if _, ok := wireClass(cl); !ok {
				errs = append(errs, fmt.Errorf("запись #%d (модуль %q): класс %q вне перечня %v",
					i+1, s.Module, cl, feed.Classes()))
			}
		}
	}
	if c.Peer == nil {
		errs = append(errs, errors.New("нет удостоверения notify для mTLS к источникам"))
	}
	if c.ClaimInterval <= 0 {
		errs = append(errs, fmt.Errorf("такт Claim %s не положителен", c.ClaimInterval))
	}
	if c.Deliverer == nil {
		errs = append(errs, errors.New("нет получателя пачек"))
	}
	if c.Metrics == nil {
		errs = append(errs, errors.New("нет реестра метрик"))
	}
	if c.Log == nil {
		errs = append(errs, errors.New("нет журнала"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("перечень источников notify отвергнут: %w", err)
	}
	return nil
}

// wireClass — класс перечня в значение провода. Перечень закрыт: класс вне
// него — отказ старта, а не «неуказанный» на проводе.
func wireClass(c feed.Class) (notifyv1.NotificationClass, bool) {
	switch c {
	case feed.ClassSecurity:
		return notifyv1.NotificationClass_SECURITY, true
	case feed.ClassNotice:
		return notifyv1.NotificationClass_NOTICE, true
	}
	return notifyv1.NotificationClass_NOTIFICATION_CLASS_UNSPECIFIED, false
}

// metrics — счётчики циклов (З27).
type metrics struct {
	streamOpens       *prometheus.CounterVec
	claimCallTimeouts *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		streamOpens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notify_source_stream_opens_total",
			Help: "Открытия потока подписки на ленту источника (CX1-71): у исправного потока — одно.",
		}, []string{"source"}),
		claimCallTimeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notify_claim_call_timeouts_total",
			Help: "Вызовы Claim, отменённые по сроку sourceCallTimeout (CX1-72): аренда строк " +
				"могла быть закоммичена, строки ждут её конца.",
		}, []string{"source"}),
	}
	for _, c := range []prometheus.Collector{m.streamOpens, m.claimCallTimeouts} {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("регистрация счётчиков циклов источников: %w", err)
		}
	}
	return m, nil
}
