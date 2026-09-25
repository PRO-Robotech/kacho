// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_transport.go — транспорт ретрансляции к целям края (слушатель
// формы — приёмка Ф3 Р2, Р16; слушатель выдачи — замысел LINE-A-1 §5.1б п. 2)
// и оператор клиентского адреса, общий с решением о доступе.
package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// newClientAddressOperator — ОДИН оператор чтения цепочки пересылки на два
// читателя: условие `client_ip` модели прав и `X-Forwarded-For` ретрансляции
// полосы формы. Обе ручки те же: доверять ли заголовкам пересылки и сколько
// доверенных прыжков стоит перед краем (адрес берётся СПРАВА). Второй
// экземпляр с теми же ручками разошёлся бы с первым при следующей правке одной
// из них.
func newClientAddressOperator(cfg config.Config) *middleware.ContextExtractor {
	return middleware.NewContextExtractor(time.Now, cfg.AuthZTrustedXForwardedFor,
		middleware.WithTrustedProxyHops(cfg.AuthZTrustedProxyCount))
}

// newLoginLaneTransport — транспорт к слушателю цели ретрансляции: якорь
// внутреннего CA, имя сервера для SNI (ручка KACHO_API_GATEWAY_MTLS_IAM_SERVER_NAME,
// как у gRPC-ребра к службе; пустая → хост адреса) и — ТОЛЬКО если режим
// предъявления цели его спрашивает — клиентская пара края. Тот же клиент, что у
// хопов к нашему авторитету отзыва, — одна реализация на все хопы; здесь
// добавлено только имя сервера, которого у тех хопов нет: их адрес и есть имя.
//
// Удостоверение выводится из режима цели тем же предикатом, что набор осей
// стража (`presentsClientPair`): страж, объявивший ось пары неприменимой, и
// транспорт, всё-таки предъявляющий пару, говорили бы о разном.
//
// Страж старта уже отверг пустой адрес, незашифрованную схему и неполную
// пару; здесь пара ЧИТАЕТСЯ, и нечитаемая — тоже отказ старта: продолжить без
// сертификата значило бы объявить личность на хопе и не предъявлять её.
func newLoginLaneTransport(cfg config.Config, target relayTargetDecl, rawURL string) (http.RoundTripper, error) {
	presents, err := target.Mode.presentsClientPair()
	if err != nil {
		return nil, fmt.Errorf("%s: %w — отказ в старте", target.URLKnob, err)
	}
	var client *http.Client
	if presents {
		pair, perr := tls.LoadX509KeyPair(strings.TrimSpace(cfg.MTLSClientCertFile), strings.TrimSpace(cfg.MTLSClientKeyFile))
		if perr != nil {
			return nil, fmt.Errorf("%s / %s не читаются как пара (%v) — отказ в старте: слушатель "+
				"за %s допускает ровно край по клиентскому сертификату, и хоп без пары отвергался "+
				"бы на каждом рукопожатии", mtlsClientCertKnob, mtlsClientKeyKnob, perr, target.URLKnob)
		}
		client, err = newPinnedHopClientWithIdentity(mtlsCAKnob, cfg.MTLSCAFile, &pair, handler.LoginLaneRelayTimeout)
	} else {
		client, err = newPinnedHopClient(mtlsCAKnob, cfg.MTLSCAFile, handler.LoginLaneRelayTimeout)
	}
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
func prepareRelayTarget(provider identityposture.Provider, cfg config.Config, serves middleware.RelayTarget, rawURL string) (http.RoundTripper, relayTargetDecl, error) {
	target, ok := relayTargetDeclFor(serves)
	if !ok {
		return nil, relayTargetDecl{}, fmt.Errorf("relay target %q has no guard declaration (refuse to start)", serves)
	}
	if err := validateLoginLaneConfig(provider, LoginLaneConfig{
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

// relayGuardSummary — оси стража цели одной строкой для самоотчёта старта:
// неприменимая названа с причиной, а не опущена.
func relayGuardSummary(target relayTargetDecl) string {
	parts := make([]string, 0, 4)
	for _, a := range relayGuardAxes(target) {
		if a.Applies {
			parts = append(parts, a.Name)
			continue
		}
		parts = append(parts, a.Name+" — "+a.Reason)
	}
	return strings.Join(parts, "; ")
}

// logRelayWired — самоотчёт провязки ретранслятора: цель, ручка адреса, режим
// предъявления, оси стража (неприменимая — с причиной), предел и число записей.
func logRelayWired(logger *slog.Logger, relay *handler.LoginLaneRelay, target relayTargetDecl, rawURL string) {
	records := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Target == relay.Serves() {
			records++
		}
	}
	logger.Info("relay to the identity service wired",
		"target", string(relay.Serves()), "knob", target.URLKnob, "url", rawURL,
		"client_auth", string(target.Mode), "guard", relayGuardSummary(target),
		"limit", relay.Limit().String(), "records", records,
		"strips", "authorization + x-kacho-* (both forms)")
}
