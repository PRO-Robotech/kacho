// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/cmuxh2"
	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// Сборка HTTP-поверхности края. Сервер (newEdgeHTTPServer), разделение порта
// (splitEdgeCmux) и подъём на слушателях (serveEdgeMux, serveInternalREST)
// живут здесь и только здесь: композиционный корень их зовёт, а пробы
// edge_h2_rest_test.go зовут ТЕ ЖЕ функции, а не свою копию сборки. Перепись
// TestEdgeH2REST_EdgeAssemblyHasASingleHome держит, что в пакете корня нет
// второго места, где собирается сервер, делится порт или вызывается Serve.

// edgeTLSConfig — TLS внешнего слушателя края: ALPN предлагает h2 и http/1.1.
// h2 нужен нативному gRPC, http/1.1 — REST-клиентам, не умеющим h2.
func edgeTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}
}

// newEdgeHTTPServer — ЕДИНСТВЕННЫЙ HTTP-сервер края: он обслуживает все три
// HTTP-слушателя — открытый за мультиплексором (его целит ingress), внешний
// TLS за мультиплексором и выделенный внутренний REST-слушатель.
func newEdgeHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler: handler,
		// ReadHeaderTimeout bounds the slow-header (Slowloris) attack surface
		// independently of the body-read budget: a client trickling request
		// headers cannot pin a connection/goroutine indefinitely (CWE-400/770).
		// It applies only from the moment this server owns the connection — the
		// window BEFORE that (protocol sniffing by the multiplexer) is bounded
		// separately by edgeFirstByteBudget; see cmux_firstbyte.go.
		//
		// Граница для HTTP/2: ReadHeaderTimeout его соединение не ограничивает.
		// Молчащее h2-соединение держат edgeFirstByteBudget и проверка живости
		// edgeHTTP2Config (SendPingTimeout + PingTimeout: тот, кто не отвечает
		// на PING, закрывается); отвечающее на PING, но без потоков — IdleTimeout.
		//
		// WriteTimeout is intentionally left unset — the same server multiplexes
		// grpc-gateway responses (incl. long-lived streaming/long-poll REST) and a
		// blanket write deadline would truncate them; slow-read draining is bounded
		// instead by IdleTimeout + the reverse-proxy/L7 in front of the edge
		// (and, for HTTP/2, by edgeHTTP2Config.WriteByteTimeout).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// SECURITY (fail-closed): the SAME server serves every HTTP listener.
		// ConnContext tags the internal admin listener's connections (wrapped
		// with listenerorigin.InternalListener in serveInternalREST) internal and
		// the two external HTTP listeners' connections (wrapped with
		// listenerorigin.ExternalListener in serveEdgeMux) external. Each reader
		// refuses by default: the REST dispatcher / authz middleware 404
		// Internal* paths on every connection without the internal mark, and the
		// ceremony records (handler.MountLoginLaneRoutes) relay only on
		// connections with the external mark. A listener that lost its wrapper
		// serves neither. listener_origin_wiring_test.go holds the wrappers.
		//
		// Вторым ConnContext кладёт состояние TLS соединения (linktls): за
		// мультиплексором r.TLS пуст всегда, а звено фронта узнаётся по
		// имени в своём сертификате (kacho#3028, C4).
		ConnContext: linktls.WithConnState(listenerorigin.ConnContext),
		Protocols:   edgeHTTPProtocols(),
		HTTP2:       edgeHTTP2Config(),
	}
}

// edgeHTTPProtocols — протоколы REST-сервера края: HTTP/1.1 и HTTP/2.
//
// За мультиплексором соединение не *tls.Conn, поэтому согласованный по ALPN h2
// стандартный сервер сам не узнаёт: HTTP/2 он обслуживает только как «HTTP/2
// без шифрования» — по преамбуле в начале потока. TLS при этом снят
// слушателем раньше, так что для REST поверх TLS это тот же HTTP/2 по ALPN.
// Без этого HTTP/2-соединение за мультиплексором сервер читал бы как HTTP/1.1
// и отвечал строкой статуса, которую клиент разбирает как кадр
// (FRAME_SIZE_ERROR, kacho#3125).
//
// Тот же сервер обслуживает и внутренний REST-слушатель: там HTTP/2 по
// преамбуле тоже принимается, проверки пути и прав от протокола не зависят.
// Обновление «Upgrade: h2c» сервер не исполняет — только преамбулу.
func edgeHTTPProtocols() *http.Protocols {
	var p http.Protocols
	p.SetHTTP1(true)
	p.SetHTTP2(true)
	p.SetUnencryptedHTTP2(true)
	return &p
}

// edgeHTTP2Config — параметры HTTP/2 REST-сервера края, каждый явным значением.
//
// Соединение HTTP/2 на крае открывает любой, кто умеет TCP: аутентификация
// наступает только с первым запросом. Поэтому всё, что сервер выделяет до
// первого HEADERS, обязано быть ограничено сопоставимо с HTTP/1.1, где до
// строки запроса соединение стоило единиц КиБ.
//
//   - MaxReadFrameSize — 16 КиБ, наименьшее допустимое (RFC 9113 §4.2).
//     Умолчание стандартной библиотеки — 1 МиБ, и буфер кадра такого размера
//     сервер держал бы на каждом анонимном соединении, приславшем заголовок
//     длинного кадра. Кадр длиннее сервер отвергает FRAME_SIZE_ERROR и
//     закрывает соединение.
//   - MaxReceiveBufferPerConnection / PerStream — 64 КиБ, наименьшее
//     допустимое для соединения: столько данных тела сервер буферизует, пока
//     обработчик их не читает (анонимный запрос отвергается, тело не читая).
//     Потолок тела запроса края — 1 МиБ (middleware.EdgeMaxRequestBodyBytes),
//     окно его не ограничивает, только темп.
//   - MaxConcurrentStreams — 100: столько запросов одно соединение держит
//     одновременно.
//   - MaxDecoderHeaderTableSize / MaxEncoderHeaderTableSize — 4 КиБ,
//     начальный размер таблицы HPACK (RFC 7541 §4.2).
//   - SendPingTimeout / PingTimeout — 30 с / 15 с: соединение, не приславшее
//     ни одного кадра 30 с, получает PING, и не ответившее за 15 с закрывается.
//   - WriteByteTimeout — 30 с: соединение, не забирающее ответ, закрывается.
func edgeHTTP2Config() *http.HTTP2Config {
	return &http.HTTP2Config{
		MaxConcurrentStreams:          100,
		MaxReadFrameSize:              16 << 10,
		MaxReceiveBufferPerConnection: 64 << 10,
		MaxReceiveBufferPerStream:     64 << 10,
		MaxDecoderHeaderTableSize:     4 << 10,
		MaxEncoderHeaderTableSize:     4 << 10,
		SendPingTimeout:               30 * time.Second,
		PingTimeout:                   15 * time.Second,
		WriteByteTimeout:              30 * time.Second,
	}
}

// splitEdgeCmux делит мультиплексор края на gRPC (HTTP/2 с content-type
// application/grpc) и всё остальное (REST). ЕДИНСТВЕННОЕ место, где у края
// объявляются матчеры: оба мультиплексора — открытый и TLS — делятся им.
//
// Матчер gRPC — cmuxh2.MatchHeaderFieldSendSettings, а не одноимённый у cmux:
// тот выделяет буфер кадра по длине из его заголовка (до 16 МиБ) и читает блок
// заголовков без предела — на соединении, которое ещё не аутентифицировано.
//
// REST получает и HTTP/2 (kacho#3125): клиент, выбравший h2 по ALPN поверх TLS
// либо заранее без TLS, с не-gRPC запросом попадает сюда, уже получив от
// матчера SETTINGS. Под-слушатель REST поэтому несёт cmuxh2.Listener — фильтр
// снимает подтверждения тех SETTINGS, иначе сервер HTTP/2 рвёт соединение на
// подтверждении, которого не ждал. Пара к нему — edgeHTTPProtocols.
// Обе половины держит edge_h2_rest_test.go.
func splitEdgeCmux(m cmux.CMux) (grpcL, restL net.Listener) {
	grpcL = m.MatchWithWriters(
		cmuxh2.MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	restL = cmuxh2.Listener(m.Match(cmux.Any()))
	return grpcL, restL
}

// serveEdgeMux обслуживает мультиплексор внешнего слушателя края: gRPC и REST —
// в своих горутинах, сам мультиплексор — в вызывающей; возвращается, когда
// мультиплексор остановлен. ЕДИНСТВЕННОЕ место, где REST края встаёт на
// внешний слушатель, — поэтому оба внешних слушателя корня (открытый и TLS)
// получают одно и то же: разделение splitEdgeCmux и метку «внешний».
//
// died получает отказ сервера, вышедшего не по остановке: what — "grpc" либо
// "http".
func serveEdgeMux(m cmux.CMux, grpcSrv *grpc.Server, httpSrv *http.Server, died func(what string, err error)) error {
	grpcL, restL := splitEdgeCmux(m)
	go func() {
		if err := grpcSrv.Serve(grpcL); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			died("grpc", err)
		}
	}()
	go func() {
		if err := httpSrv.Serve(listenerorigin.ExternalListener(restL)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			died("http", err)
		}
	}()
	return m.Serve()
}

// serveInternalREST обслуживает выделенный внутренний REST-слушатель: ЕДИНСТВЕННОЕ
// место кода края вне пакета listenerorigin, где соединения получают метку
// «внутренний» (обёрткой слушателя; прямую метку на контексте здесь не ставят —
// контекст общего сервера пометил бы и внешние слушатели). Возвращает nil на
// остановке сервера.
func serveInternalREST(httpSrv *http.Server, l net.Listener) error {
	if err := httpSrv.Serve(listenerorigin.InternalListener(l)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
