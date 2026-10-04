// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// subscribeKind — вид ленты на проводе подписки: тип объекта модели прав, у
// которого один производитель — сервер ленты corelib.
var subscribeKind = string(feed.FeedObjectType)

// errSANMismatch — сервер ленты предъявил удостоверение, отличное от
// записи перечня: подключение отвергнуто (NTF1-G22).
var errSANMismatch = errors.New("SAN сервера ленты не совпадает с записью перечня")

// loop — цикл одного источника.
type loop struct {
	src       config.Source
	classes   []notifyv1.NotificationClass
	conn      *grpc.ClientConn
	feed      notifyv1.InternalNotificationFeedServiceClient
	sub       subscriptionv1.InternalSubscriptionServiceClient
	interval  time.Duration
	deliverer Deliverer
	log       *slog.Logger

	streamOpens       prometheus.Counter
	claimCallTimeouts prometheus.Counter

	// wake — сигнал «позвать `Claim`»: событие ленты либо (пере)открытие
	// потока. Ёмкость 1: сигналы, пришедшие во время вызова, сливаются в
	// один — событие только сигнал (`sub-refetch-not-apply`).
	wake chan struct{}
}

func newLoop(cfg Config, src config.Source, m *metrics) (*loop, error) {
	classes := make([]notifyv1.NotificationClass, 0, len(src.Classes))
	for _, c := range src.Classes {
		w, _ := wireClass(c) // перечень проверен стражем [Config.validate]
		classes = append(classes, w)
	}
	log := cfg.Log.With(slog.String("source", src.Module))
	conn, err := grpcclient.DialPeer(grpcclient.PeerDialOptions{
		Endpoint: src.FeedAddr,
		Creds:    exactSANCreds(cfg.Peer, src, log),
		// Пол установления соединения — десятая доля срока вызова (1 с): при
		// меньшем полу рукопожатие mTLS под нагрузкой не успевает, и вызов
		// падает UNAVAILABLE раньше своего срока.
		DialTimeout:   sourceCallTimeout,
		KeepAliveTime: grpcclient.DefaultKeepaliveTime,
		UserAgent:     "kacho-notify",
	})
	if err != nil {
		return nil, fmt.Errorf("источник %q: %w", src.Module, err)
	}
	opens := m.streamOpens.WithLabelValues(src.Module)
	timeouts := m.claimCallTimeouts.WithLabelValues(src.Module)
	return &loop{
		src:               src,
		classes:           classes,
		conn:              conn,
		feed:              notifyv1.NewInternalNotificationFeedServiceClient(conn),
		sub:               subscriptionv1.NewInternalSubscriptionServiceClient(conn),
		interval:          cfg.ClaimInterval,
		deliverer:         cfg.Deliverer,
		log:               log,
		streamOpens:       opens,
		claimCallTimeouts: timeouts,
		wake:              make(chan struct{}, 1),
	}, nil
}

// exactSANCreds — транспорт к серверу ленты: цепочка и имя узла
// проверяются штатно, и сверх того лист сервера обязан нести ровно один
// URI-SAN, равный записи перечня (SPIFFE ID — один на лист). Иначе — отказ
// рукопожатия: ни `Claim`, ни `Subscribe` до сервера не доходят.
func exactSANCreds(peer *tls.Config, src config.Source, log *slog.Logger) credentials.TransportCredentials {
	cfg := peer.Clone()
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	want := src.SAN
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("%w: сервер не предъявил сертификат", errSANMismatch)
		}
		uris := cs.PeerCertificates[0].URIs
		if len(uris) == 1 && uris[0].String() == want {
			return nil
		}
		got := make([]string, 0, len(uris))
		for _, u := range uris {
			got = append(got, u.String())
		}
		log.Error("сервер ленты источника предъявил чужое удостоверение — подключение отвергнуто",
			slog.String("alarm", "source_identity_mismatch"),
			slog.String("want_san", want), slog.Any("got_san", got))
		return fmt.Errorf("%w: ожидался %s, предъявлено %v", errSANMismatch, want, got)
	}
	return credentials.NewTLS(cfg)
}

// run — цикл источника до отмены ctx: поток подписки живёт отдельно и только
// будит `Claim`; сам `Claim` последователен — такты во время вызова не
// копятся.
func (l *loop) run(ctx context.Context) {
	subDone := make(chan struct{})
	go func() {
		defer close(subDone)
		l.subscribe(ctx)
	}()
	defer func() { <-subDone }()

	tick := time.NewTicker(l.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.wake:
		case <-tick.C:
		}
		// Пачка заполнена до размера запроса — строк может быть больше:
		// следующий `Claim` сразу, пока есть свободные исполнители.
		for full := true; full; {
			full = l.claim(ctx)
		}
	}
}

func (l *loop) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// claim — один вызов `Claim` под сроком [sourceCallTimeout]. Возвращает,
// заполнена ли пачка до размера запроса.
func (l *loop) claim(ctx context.Context) bool {
	free := l.deliverer.Free()
	if free <= 0 || ctx.Err() != nil {
		return false
	}
	size := min(free, feed.MaxClaim)
	callCtx, cancel := context.WithTimeout(ctx, sourceCallTimeout)
	defer cancel()
	sentAt := time.Now()
	resp, err := l.feed.Claim(callCtx, &notifyv1.ClaimRequest{
		Max:     uint32(size), // #nosec G115 -- size в [1..feed.MaxClaim]
		Classes: l.classes,
	})
	if err != nil {
		switch {
		case ctx.Err() != nil:
			// Цикл останавливается — не отказ источника.
		case errors.Is(callCtx.Err(), context.DeadlineExceeded):
			// Аренда строк могла быть закоммичена: строки ждут её конца и
			// выдаются следующим `Claim` (CX1-72).
			l.claimCallTimeouts.Inc()
			l.log.Warn("Claim не вернулся за срок — вызов отменён",
				slog.Duration("timeout", sourceCallTimeout))
		default:
			l.log.Warn("Claim отвергнут", slog.Any("error", err))
		}
		return false
	}
	rows := resp.GetNotifications()
	if len(rows) == 0 {
		return false
	}
	l.deliverer.Deliver(ctx, Batch{Source: l.src, Rows: rows, SentAt: sentAt})
	return len(rows) >= size
}

// subscribe держит поток подписки на ленту: открывает, будит `Claim` при
// открытии и на каждое событие, после обрыва открывает заново на следующем
// такте.
func (l *loop) subscribe(ctx context.Context) {
	pause := time.NewTimer(l.interval)
	defer pause.Stop()
	for {
		l.stream(ctx)
		pause.Reset(l.interval)
		select {
		case <-ctx.Done():
			return
		case <-pause.C:
		}
	}
}

// stream — одно открытие потока. У контекста потока своего срока нет
// (CX1-71): срок [sourceCallTimeout] действует только на установление — до
// `SubscriptionOpened`; пришло — срок снят, поток живёт под контекстом цикла.
func (l *loop) stream(ctx context.Context) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	establish := time.AfterFunc(sourceCallTimeout, cancel)
	defer establish.Stop()

	l.streamOpens.Inc()
	// Установление ждёт готовности соединения в пределах своего срока, а не
	// падает на первом неготовом соединении: иначе открытие потока
	// откладывалось бы на такт (при такте 5 мин — на 5 мин).
	st, err := l.sub.Subscribe(streamCtx, &subscriptionv1.SubscriptionRequest{Kinds: []string{subscribeKind}},
		grpc.WaitForReady(true))
	if err != nil {
		l.streamDown(ctx, err)
		return
	}
	first, err := st.Recv()
	if err != nil {
		l.streamDown(ctx, err)
		return
	}
	if first.GetOpened() == nil {
		l.log.Warn("первое сообщение потока ленты — не SubscriptionOpened; поток отменён")
		return
	}
	if !establish.Stop() {
		// Срок установления истёк одновременно с приходом открытия: поток
		// уже отменён.
		return
	}
	l.signal()
	for {
		msg, err := st.Recv()
		if err != nil {
			l.streamDown(ctx, err)
			return
		}
		if ev := msg.GetEvent(); ev != nil && ev.GetKind() == subscribeKind {
			l.signal()
		}
	}
}

func (l *loop) streamDown(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	l.log.Warn("поток подписки на ленту источника недоступен — Claim по таймеру", slog.Any("error", err))
}
