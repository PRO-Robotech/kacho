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

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

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
