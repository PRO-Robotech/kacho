// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/tls"
	"net"
	"net/http"

	"github.com/soheilhy/cmux"

	"github.com/PRO-Robotech/kacho/gateway/internal/cmuxh2"
)

// edgeTLSConfig — TLS внешнего слушателя края: ALPN предлагает h2 и http/1.1.
// h2 нужен нативному gRPC, http/1.1 — REST-клиентам, не умеющим h2.
func edgeTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}
}

// splitEdgeCmux делит мультиплексор края на gRPC (HTTP/2 с content-type
// application/grpc) и всё остальное (REST). ЕДИНСТВЕННОЕ место, где у края
// объявляются матчеры: оба мультиплексора — открытый и TLS — делятся им.
//
// REST получает и HTTP/2 (kacho#3125): клиент, выбравший h2 по ALPN поверх TLS
// либо заранее без TLS, с не-gRPC запросом попадает сюда, уже получив от
// матчера SETTINGS. Под-слушатель REST поэтому несёт cmuxh2.Listener — фильтр
// снимает подтверждения тех SETTINGS, иначе сервер HTTP/2 рвёт соединение на
// подтверждении, которого не ждал. Пара к нему — edgeHTTPProtocols: без неё
// HTTP/2-соединение за мультиплексором сервер читал бы как HTTP/1.1 и отвечал
// строкой статуса, которую клиент разбирает как кадр (FRAME_SIZE_ERROR).
// Обе половины держит edge_h2_rest_test.go.
func splitEdgeCmux(m cmux.CMux) (grpcL, restL net.Listener) {
	grpcL = m.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	restL = cmuxh2.Listener(m.Match(cmux.Any()))
	return grpcL, restL
}

// edgeHTTPProtocols — протоколы REST-сервера края: HTTP/1.1 и HTTP/2.
//
// За мультиплексором соединение не *tls.Conn, поэтому согласованный по ALPN h2
// стандартный сервер сам не узнаёт: HTTP/2 он обслуживает только как «HTTP/2
// без шифрования» — по преамбуле в начале потока. TLS при этом снят
// слушателем раньше, так что для REST поверх TLS это тот же HTTP/2 по ALPN.
// Тот же сервер обслуживает и внутренний REST-слушатель: там HTTP/2 по
// преамбуле тоже принимается, проверки пути и прав от протокола не зависят.
func edgeHTTPProtocols() *http.Protocols {
	var p http.Protocols
	p.SetHTTP1(true)
	p.SetHTTP2(true)
	p.SetUnencryptedHTTP2(true)
	return &p
}
