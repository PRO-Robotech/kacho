// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_transport.go — транспорт ретрансляции полосы формы и оператор
// клиентского адреса, общий с решением о доступе (приёмка Ф3 Р2, Р16).
package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
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

// newLoginLaneTransport — транспорт к слушателю полосы формы: клиентская пара
// края, якорь внутреннего CA и имя сервера для SNI (ручка
// KACHO_API_GATEWAY_MTLS_IAM_SERVER_NAME, как у gRPC-ребра к службе; пустая →
// хост адреса). Тот же клиент, что у хопов к нашему авторитету отзыва, — одна
// реализация на все хопы с предъявлением пары; здесь добавлено только имя
// сервера, которого у тех хопов нет: их адрес и есть имя.
//
// Страж старта уже отверг пустой адрес, незашифрованную схему и неполную
// пару; здесь пара ЧИТАЕТСЯ, и нечитаемая — тоже отказ старта: продолжить без
// сертификата значило бы объявить личность на хопе и не предъявлять её.
func newLoginLaneTransport(cfg config.Config) (http.RoundTripper, error) {
	pair, err := tls.LoadX509KeyPair(strings.TrimSpace(cfg.MTLSClientCertFile), strings.TrimSpace(cfg.MTLSClientKeyFile))
	if err != nil {
		return nil, fmt.Errorf("%s / %s не читаются как пара (%v) — отказ в старте: слушатель "+
			"полосы формы допускает ровно край по клиентскому сертификату, и хоп без пары отвергался "+
			"бы на каждом рукопожатии", mtlsClientCertKnob, mtlsClientKeyKnob, err)
	}
	client, err := newPinnedHopClientWithIdentity(mtlsCAKnob, cfg.MTLSCAFile, &pair, 10*time.Second)
	if err != nil {
		return nil, err
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		return nil, fmt.Errorf("%s: транспорт полосы формы собран без TLS — отказ в старте", config.LoginLaneURLKnob)
	}
	serverName := strings.TrimSpace(cfg.MTLSIAMServerName)
	if serverName == "" {
		u, perr := url.Parse(strings.TrimSpace(cfg.LoginLaneURL))
		if perr != nil {
			return nil, fmt.Errorf("%s: %w", config.LoginLaneURLKnob, perr)
		}
		serverName = u.Hostname()
	}
	tr.TLSClientConfig.ServerName = serverName
	return tr, nil
}
