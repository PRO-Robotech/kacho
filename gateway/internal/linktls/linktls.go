// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package linktls — состояние TLS соединения, принятого слушателем края за
// мультиплексором протоколов, для тех, кто узнаёт звено фронта по имени в его
// клиентском сертификате (kacho#3028, C4).
//
// # Зачем пакет
//
// Внешний TLS-слушатель края завершает рукопожатие сам (`tls.Listen`), а
// разводит HTTP и gRPC мультиплексор протоколов. Читатели за ним видят обёртку,
// а не *tls.Conn:
//
//   - сервер HTTP заполняет r.TLS только для голого *tls.Conn — за
//     мультиплексором r.TLS пуст всегда;
//   - сервер gRPC без своих учётных данных о TLS не знает вовсе — peer.AuthInfo
//     пуст.
//
// Проверенная якорем цепочка клиентского сертификата есть, но не видна ни
// одному читателю. Пакет достаёт её из-под обёрток и отдаёт обоим: HTTP — через
// контекст соединения (ConnContext / FromRequest), gRPC — через учётные данные
// сервера (ServerCredentials), которые кладут её в peer.AuthInfo как
// credentials.TLSInfo.
//
// # Чего пакет не делает
//
// Он не проверяет сертификат: проверка — дело рукопожатия по политике
// слушателя (необязательный сертификат, якорь установки). Пакет лишь отдаёт
// состояние ЗАВЕРШЁННОГО рукопожатия; незавершённое — «состояния нет», чтобы
// никакой читатель не принял непроверенную цепочку за проверенную.
package linktls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/soheilhy/cmux"
	"google.golang.org/grpc/credentials"
)

// unwrapDepth — предел разворачивания обёрток: обёрток у слушателя края
// две-три, и цикл без предела на обёртке, возвращающей саму себя, повесил бы
// приём соединения.
const unwrapDepth = 16

// ConnState — состояние завершённого TLS-рукопожатия соединения под обёртками
// края (мультиплексор протоколов, метка происхождения слушателя). nil — не TLS
// либо рукопожатие не завершено.
func ConnState(c net.Conn) *tls.ConnectionState {
	tc := findTLS(c)
	if tc == nil {
		return nil
	}
	st := tc.ConnectionState()
	if !st.HandshakeComplete {
		return nil
	}
	return &st
}

// findTLS — *tls.Conn под обёртками края либо nil.
func findTLS(c net.Conn) *tls.Conn {
	for i := 0; c != nil && i < unwrapDepth; i++ {
		switch w := c.(type) {
		case *tls.Conn:
			return w
		case *cmux.MuxConn:
			c = w.Conn
		case interface{ NetConn() net.Conn }:
			c = w.NetConn()
		default:
			return nil
		}
	}
	return nil
}

type connKey struct{}

// ConnContext кладёт в контекст запросов соединения само соединение
// (http.Server.ConnContext); состояние TLS читается из него на запросе
// (FromRequest). Не на приёме: ConnContext сервер зовёт в цикле приёма, когда
// рукопожатие ещё может быть не начато (мультиплексор, отдавший соединение без
// чтения), а ждать рукопожатия там значит отдать цикл приёма медленному
// клиенту. К первому запросу рукопожатие завершено — запрос прочитан через него.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if findTLS(c) == nil {
		return ctx
	}
	return context.WithValue(ctx, connKey{}, c)
}

// WithConnState — ConnContext, который после next кладёт соединение для
// FromRequest (корень края: next — метка происхождения слушателя).
func WithConnState(next func(context.Context, net.Conn) context.Context) func(context.Context, net.Conn) context.Context {
	return func(ctx context.Context, c net.Conn) context.Context {
		if next != nil {
			ctx = next(ctx, c)
		}
		return ConnContext(ctx, c)
	}
}

// FromRequest — состояние TLS соединения запроса: r.TLS, если сервер его
// заполнил (голый TLS-слушатель), иначе — соединения, положенного ConnContext.
// nil — не TLS либо рукопожатие не завершено.
func FromRequest(r *http.Request) *tls.ConnectionState {
	if r == nil {
		return nil
	}
	if r.TLS != nil {
		return r.TLS
	}
	c, _ := r.Context().Value(connKey{}).(net.Conn)
	return ConnState(c)
}

// ServerCredentials — учётные данные сервера gRPC, принимающего соединения, на
// которых TLS уже завершён слушателем (либо которых TLS нет вовсе). Рукопожатия
// они не ведут: соединение с завершённым TLS получает credentials.TLSInfo с его
// состоянием, прочее — сведения без защиты, как у сервера без учётных данных.
func ServerCredentials() credentials.TransportCredentials { return terminated{} }

type terminated struct{}

// plain — сведения о соединении без TLS.
type plain struct{ credentials.CommonAuthInfo }

func (plain) AuthType() string { return "insecure" }

// ServerHandshake: сервер gRPC зовёт её в своей горутине соединения под
// дедлайном рукопожатия, поэтому незавершённое TLS-рукопожатие здесь
// доводится до конца (мультиплексор, распознавший gRPC, обычно уже провёл
// его чтением). Отказ рукопожатия — отказ соединения.
func (terminated) ServerHandshake(c net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if tc := findTLS(c); tc != nil && !tc.ConnectionState().HandshakeComplete {
		if err := tc.Handshake(); err != nil {
			return nil, nil, fmt.Errorf("linktls: рукопожатие TLS: %w", err)
		}
	}
	if st := ConnState(c); st != nil {
		return c, credentials.TLSInfo{
			State:          *st,
			CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity},
		}, nil
	}
	return c, plain{credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}}, nil
}

func (terminated) ClientHandshake(context.Context, string, net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("linktls: учётные данные серверные — исходящих соединений они не ведут")
}

func (terminated) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls"}
}

func (t terminated) Clone() credentials.TransportCredentials { return t }

// OverrideServerName — устаревший метод интерфейса; серверу имя не нужно.
func (terminated) OverrideServerName(string) error { return nil }
