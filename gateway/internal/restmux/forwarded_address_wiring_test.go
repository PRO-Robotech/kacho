// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// forwarded_address_wiring_test.go — МОСТ REST НЕ НЕСЁТ АДРЕС ИСТОЧНИКА К
// СЛУЖБЕ (kacho#3028, круг 5). Библиотека моста кладёт в метаданные
// `x-forwarded-for` = заголовок клиента + адрес пира; служба, которая начала бы
// его читать, читала бы подделку. Проба — настоящий запрос через мост к
// настоящему серверу gRPC, который записывает пришедшие метаданные.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// recordingBackend — сервер gRPC, отвечающий на любой метод отказом и
// записывающий метаданные вызова.
func recordingBackend(t *testing.T) (string, <-chan metadata.MD) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan metadata.MD, 4)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
		md, _ := metadata.FromIncomingContext(ss.Context())
		got <- md
		return status.Error(codes.Unavailable, "recording backend")
	}))
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return l.Addr().String(), got
}

func TestRESTBridge_BackendSeesNoSourceAddress(t *testing.T) {
	addr, got := recordingBackend(t)
	addrs := geoMuxAddrs()
	addrs["geo"] = addr
	h, err := NewMux(context.Background(), addrs, nil, nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/geo/v1/regions", nil)
	req.RemoteAddr = "10.244.1.17:40000"
	req.Header.Set("X-Forwarded-For", "198.51.100.66")
	req.Header.Set("X-Real-IP", "198.51.100.66")
	req.Header.Set("Forwarded", "for=198.51.100.66")
	req.Header.Set("X-Request-ID", "rq-forwarded-probe")
	h.ServeHTTP(httptest.NewRecorder(), req)

	select {
	case md := <-got:
		for _, k := range []string{"x-forwarded-for", "x-real-ip", "forwarded", "grpcgateway-x-forwarded-for",
			"grpcgateway-x-real-ip", "grpcgateway-forwarded"} {
			if v := md.Get(k); len(v) != 0 {
				t.Errorf("служба получила %s=%v от моста REST", k, v)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("вызов до службы не дошёл — проба не исполнилась")
	}
}
