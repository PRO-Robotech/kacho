// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_transport.go — транспорт ретрансляции к целям края (слушатель
// формы — приёмка Ф3 Р2, Р16; слушатель выдачи — замысел LINE-A-1 §5.1б п. 2)
// и оператор клиентского адреса, общий с решением о доступе.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/frontpeers"
	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// startClientAddressOperator — ЕДИНСТВЕННАЯ сборка оператора адреса клиента
// (kacho#3028, C6): её зовут корень и проба старта
// (client_address_operator_start_test.go), и второй сборки нет.
//
// Один оператор на три читателя: условие `client_ip` модели прав (HTTP и
// нативный gRPC) и `X-Forwarded-For` ретрансляции полосы формы, по которому
// служба доступа ведёт ограничение частоты «на источник». Ручки: доверять ли
// заголовкам пересылки, сколько доверенных прыжков стоит перед краем (адрес
// берётся СПРАВА), круг сетей, звенья фронта поимённо (адреса подов безголовых
// служб фронта) и имена звеньев в сертификате (C4). Разбор и отказ старта по
// каждой — config.TrustedProxyCircle, TrustedProxyPeers, TrustedProxyLinkSANs.
//
// БОЕВОЙ ПРОФИЛЬ НЕ ВПРАВЕ НЕ ДОВЕРЯТЬ НИКОМУ: край за звеном фронта, не
// принимающий заголовок ни от кого, видит всех клиентов одним адресом звена, и
// ограничение частоты «на источник» становится общим на всех. Это отказ в
// старте, а не предупреждение: предупреждение такого края не останавливает.
//
// Обновление перечня звеньев запускается здесь же, на контексте процесса
// (frontpeers.Set.Run): сборка, вернувшая перечень, который никто не
// обновляет, через три периода перестала бы признавать звенья молча.
func startClientAddressOperator(ctx context.Context, cfg config.Config, resolve frontpeers.Resolver, logger *slog.Logger) (
	*middleware.ContextExtractor, *frontpeers.Set, error) {
	circle, err := cfg.TrustedProxyCircle()
	if err != nil {
		return nil, nil, err
	}
	names, err := cfg.TrustedProxyPeers()
	if err != nil {
		return nil, nil, err
	}
	sans, err := cfg.TrustedProxyLinkSANs()
	if err != nil {
		return nil, nil, err
	}
	opts := []middleware.ExtractorOption{
		middleware.WithTrustedProxyHops(cfg.AuthZTrustedProxyCount),
		middleware.WithTrustedProxies(circle...),
		middleware.WithTrustedLinkSANs(sans...),
	}
	var links *frontpeers.Set
	if len(names) > 0 {
		links, err = frontpeers.New(frontpeers.Options{
			Names: names, Refresh: cfg.AuthZTrustedProxyPeersRefresh, Resolve: resolve, Logger: logger,
		})
		if err != nil {
			return nil, nil, err
		}
		opts = append(opts, middleware.WithTrustedPeers(links))
	}
	op := middleware.NewContextExtractor(time.Now, cfg.AuthZTrustedXForwardedFor, opts...)
	if cfg.ProductionPosture() && op.TrustsNobody() {
		return nil, nil, fmt.Errorf("боевой профиль (KACHO_APP_ENV=%q): заголовок адреса клиента не принимается ни от "+
			"одного звена — за звеном фронта все клиенты были бы одним адресом и делили бы одно ограничение частоты "+
			"входа; объявите круг %s, звенья поимённо %s и имена звеньев в сертификате %s "+
			"(доверие пересылке KACHO_API_GATEWAY_AUTHZ_TRUSTED_XFF=%v, прыжков %d)", cfg.AppEnv,
			config.TrustedProxyCIDRsKnob, config.TrustedProxyPeersKnob, config.TrustedProxySANsKnob,
			cfg.AuthZTrustedXForwardedFor, cfg.AuthZTrustedProxyCount)
	}
	if links != nil {
		go links.Run(ctx)
	}
	return op, links, nil
}

// lookupFrontLinks — разрешение имени службы звена в адреса её подов
// разрешателем процесса (поиск по пространству имён пода — из его resolv.conf).
func lookupFrontLinks(ctx context.Context, name string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", name)
}

// newLoginLaneTransport — транспорт к слушателю цели ретрансляции: якорь
// внутреннего CA, имя сервера для SNI (ручка KACHO_API_GATEWAY_MTLS_IAM_SERVER_NAME,
// как у gRPC-ребра к службе; пустая → хост адреса) и клиентская пара края —
// у КАЖДОЙ цели: оба слушателя службы узнают край только по ней (страж судит ту
// же ось, `relayGuardAxes`). Тот же клиент, что у хопов к нашему авторитету
// отзыва, — одна реализация на все хопы; здесь добавлено только имя сервера,
// которого у тех хопов нет: их адрес и есть имя.
//
// Страж старта уже отверг пустой адрес, незашифрованную схему и неполную
// пару; здесь пара ЧИТАЕТСЯ, и нечитаемая — тоже отказ старта: продолжить без
// сертификата значило бы объявить личность на хопе и не предъявлять её.
func newLoginLaneTransport(cfg config.Config, target relayTargetDecl, rawURL string) (http.RoundTripper, error) {
	cost, err := target.Mode.withoutPair()
	if err != nil {
		return nil, fmt.Errorf("%s: %w — отказ в старте", target.URLKnob, err)
	}
	pair, err := tls.LoadX509KeyPair(strings.TrimSpace(cfg.MTLSClientCertFile), strings.TrimSpace(cfg.MTLSClientKeyFile))
	if err != nil {
		return nil, fmt.Errorf("%s / %s не читаются как пара (%v) — отказ в старте: слушатель за %s "+
			"(режим %s) %s", mtlsClientCertKnob, mtlsClientKeyKnob, err, target.URLKnob, target.Mode, cost)
	}
	client, err := newPinnedHopClientWithIdentity(mtlsCAKnob, cfg.MTLSCAFile, &pair, handler.LoginLaneRelayTimeout)
	if err != nil {
		return nil, err
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		return nil, fmt.Errorf("%s: транспорт ретрансляции собран без TLS — отказ в старте", target.URLKnob)
	}
	serverName := strings.TrimSpace(cfg.MTLSIAMServerName)
	if serverName == "" {
		u, perr := url.Parse(strings.TrimSpace(rawURL))
		if perr != nil {
			return nil, fmt.Errorf("%s: %w", target.URLKnob, perr)
		}
		serverName = u.Hostname()
	}
	tr.TLSClientConfig.ServerName = serverName
	return tr, nil
}

// prepareRelayTarget — страж и транспорт одной цели ретрансляции: одно место
// для обеих провязок композиционного корня, чтобы вторая цель не получила
// половину пары молча. Возвращает транспорт и запись цели для самоотчёта.
//
// Посадку не читает ни он, ни страж внутри (#2873): оба судят цель всегда, а
// отличие `own` от прочего даёт место вызова — ветка посадки `own` корня; его
// держит own_lane_readers_wiring_test.go.
func prepareRelayTarget(cfg config.Config, serves middleware.RelayTarget, rawURL string) (http.RoundTripper, relayTargetDecl, error) {
	target, ok := relayTargetDeclFor(serves)
	if !ok {
		return nil, relayTargetDecl{}, fmt.Errorf("relay target %q has no guard declaration (refuse to start)", serves)
	}
	if err := validateLoginLaneConfig(LoginLaneConfig{
		Target:         target,
		URL:            rawURL,
		ClientCertFile: cfg.MTLSClientCertFile,
		ClientKeyFile:  cfg.MTLSClientKeyFile,
		CAFile:         cfg.MTLSCAFile,
	}); err != nil {
		return nil, target, err
	}
	tr, err := newLoginLaneTransport(cfg, target, rawURL)
	if err != nil {
		return nil, target, err
	}
	return tr, target, nil
}

// logRelayWired — самоотчёт провязки ретранслятора: цель, ручка адреса, режим
// слушателя цели, оси стража, предел и число записей.
func logRelayWired(logger *slog.Logger, relay *handler.LoginLaneRelay, target relayTargetDecl, rawURL string) {
	records := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Target == relay.Serves() {
			records++
		}
	}
	logger.Info("relay to the identity service wired",
		"target", string(relay.Serves()), "knob", target.URLKnob, "url", rawURL,
		"client_auth", string(target.Mode), "guard", strings.Join(relayGuardAxes(), "; "),
		"limit", relay.Limit().String(), "records", records,
		"strips", "authorization + x-kacho-* (both forms)")
}
