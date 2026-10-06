// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/notify/address"

	"github.com/PRO-Robotech/kacho/services/notify/bundle"
	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/deliver"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dkim"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
	"github.com/PRO-Robotech/kacho/services/notify/internal/kanameclient"
	"github.com/PRO-Robotech/kacho/services/notify/internal/peeranswer"
	"github.com/PRO-Robotech/kacho/services/notify/internal/peertls"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render"
	"github.com/PRO-Robotech/kacho/services/notify/internal/source"
)

// letterLocale — локаль письма NTF-1: приёмка Р7 «локали — {ru}». Строка
// ленты локали не несёт (контракт `corelib.notify`); локаль адресата приходит
// с адресацией субъекта NTF-3, и тогда эта константа уходит вместе с ней.
const letterLocale = "ru"

// deliveryDeps — то, что цикл доставки берёт у подъёма процесса.
type deliveryDeps struct {
	// Pairs — источник пары DKIM: страж DNS установки (*dnscheck.Guard).
	Pairs dkim.PairSource
	// Limiter — сетка на адресата (*limits.Limiter после записи ограды).
	Limiter deliver.Limiter
	// Registry — реестр метрик процесса.
	Registry prometheus.Registerer
	// Log — журнал процесса.
	Log *slog.Logger
}

// delivery — поднятый цикл доставки.
type delivery struct {
	loops  *source.Loops
	kaname io.Closer
}

// Wait — после отмены контекста [startDelivery]: циклы источников, строки в
// полёте (их Ack идёт по соединениям циклов), соединения с источниками и,
// последним, соединение с kaname — строк, которым оно нужно, уже нет.
func (d *delivery) Wait() {
	d.loops.Wait()
	_ = d.kaname.Close()
}

// startDelivery собирает цикл доставки из загруженной и проверенной
// конфигурации (З21–З26) и поднимает циклы источников. Порядок сборки —
// от листьев к циклу: отказ любого шага — отказ старта с именем предмета, и
// ни один цикл ещё не поднят.
//
//   - сборка шаблонов — встроенный каталог (bundle.New, Р7);
//   - отправитель — узел `notify.smtp.connectionURI`, доверенный набор —
//     якорь `notify.smtp.trustAnchorFile` либо корневое хранилище образа;
//   - рендер — origin и отправитель установки;
//   - подпись — домен From установки над парой стража DNS (Р19);
//   - право — kaname `ResolveSend` по mTLS с точным SAN `notify.kaname.san`;
//   - исполнители строк и циклы источников; путь Ack — путь пачки.
func startDelivery(ctx context.Context, cfg config.Config, d deliveryDeps) (*delivery, error) {
	build, err := bundle.New()
	if err != nil {
		return nil, fmt.Errorf("сборка шаблонов: %w", err)
	}
	roots, err := relayRoots(cfg)
	if err != nil {
		return nil, err
	}
	sender, err := newRelaySender(cfg, roots)
	if err != nil {
		return nil, fmt.Errorf("отправитель: %w", err)
	}
	from, err := address.Normalize(cfg.SMTPFromAddress)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", config.KnobOfField("SMTPFromAddress"), err)
	}
	rnd, err := render.New(render.Config{
		Origin: cfg.Origin,
		From:   render.Sender{Name: cfg.FromName(), Address: from},
	})
	if err != nil {
		return nil, fmt.Errorf("рендер письма: %w", err)
	}
	signer, err := dkim.NewSigner(cfg.FromDomain(), d.Pairs)
	if err != nil {
		return nil, err
	}
	peer, err := peerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	signals, err := peeranswer.NewSignals(d.Registry)
	if err != nil {
		return nil, err
	}
	roster := cfg.SourceRoster()

	kanameLog := d.Log.With(slog.String("peer", "kaname"))
	kanameConn, err := grpcclient.DialPeer(grpcclient.PeerDialOptions{
		Endpoint: cfg.KanameAddr,
		Creds: peertls.ExactSAN(peer, cfg.KanameSAN, func(got []string) {
			kanameLog.Error("сервер kaname предъявил чужое удостоверение — подключение отвергнуто",
				slog.String("alarm", "kaname_identity_mismatch"),
				slog.String("want_san", cfg.KanameSAN), slog.Any("got_san", got))
		}),
		DialTimeout:   cfg.ResolveSendTimeout,
		KeepAliveTime: grpcclient.DefaultKeepaliveTime,
		UserAgent:     "kacho-notify",
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", config.KnobOfField("KanameAddr"), err)
	}
	fail := func(err error) (*delivery, error) {
		_ = kanameConn.Close()
		return nil, err
	}

	grants, err := grant.New(kanameclient.New(kanameConn), roster, grant.Policy{
		CallTimeout: cfg.ResolveSendTimeout,
		DeferFor:    cfg.DeferFor,
	}, d.Registry, signals)
	if err != nil {
		return fail(err)
	}
	worker, err := deliver.New(deliver.Config{
		Build:              build,
		Sources:            roster,
		Grants:             grants,
		Limiter:            d.Limiter,
		Render:             letterRenderer{r: rnd},
		Sender:             sender,
		Signer:             signer,
		From:               cfg.SMTPFromAddress,
		Workers:            cfg.Workers,
		ResolveSendTimeout: cfg.ResolveSendTimeout,
		SMTPSessionTimeout: cfg.SMTPSessionTimeout,
		DeferFor:           cfg.DeferFor,
		Clock:              deliver.SystemClock{},
		Metrics:            d.Registry,
		Log:                d.Log,
	})
	if err != nil {
		return fail(err)
	}
	loops, err := source.Start(ctx, source.Config{
		Sources:       roster,
		Peer:          peer,
		ClaimInterval: cfg.ClaimInterval,
		Deliverer:     worker,
		Metrics:       d.Registry,
		Log:           d.Log,
	})
	if err != nil {
		return fail(err)
	}
	return &delivery{loops: loops, kaname: kanameConn}, nil
}

// relayRoots — доверенный набор проверки листа ретранслятора: якорь
// `notify.smtp.trustAnchorFile`, если чарт его смонтировал, иначе корневое
// хранилище образа. Якорь, заданный и не дающий ни одного сертификата, —
// отказ старта с именем ручки, а не тихий переход на системный набор.
func relayRoots(cfg config.Config) (*x509.CertPool, error) {
	path, ok := cfg.TrustAnchorFile()
	if !ok {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("корневое хранилище образа для проверки ретранслятора: %w", err)
		}
		return roots, nil
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: якорь ретранслятора не читается: %w", config.TrustAnchorKnob(), err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("%s: в файле якоря нет ни одного сертификата PEM", config.TrustAnchorKnob())
	}
	return roots, nil
}

// letterRenderer — сборщик письма (render.Renderer) в порту исполнителей:
// описание строки после клеток 1–6 → письмо локали [letterLocale] с моментом
// сборки в заголовке Date.
type letterRenderer struct {
	r *render.Renderer
}

func (l letterRenderer) Render(res deliver.Resolved) ([]byte, error) {
	if res.Template == nil {
		return nil, fmt.Errorf("render: строка %s без шаблона сборки", res.ID)
	}
	return l.r.Render(render.Letter{
		Namespace: res.Namespace,
		RowID:     res.ID,
		Template:  *res.Template,
		Locale:    letterLocale,
		Attrs:     res.Attrs,
		To:        res.Recipient,
		Date:      time.Now(),
	})
}
