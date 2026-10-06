// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// forwarded_address_dial_test.go — НАТИВНЫЙ ПЕРЕХОД К СЛУЖБЕ НЕ НЕСЁТ АДРЕС
// ИСТОЧНИКА (kacho#3028, круг 5). Нативный прокси копирует входящие метаданные
// целиком (principalmeta.OutgoingFromIncoming): `x-forwarded-for`, `forwarded`,
// `grpcgateway-*` клиента уезжали к службе дословно. Снятие стоит на
// соединениях к службам (dialBackends), а не в каждой сборке метаданных, —
// проба зовёт службу через настоящее соединение dialBackends.

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

func TestDialBackends_NativeHopCarriesNoSourceAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan metadata.MD, 1)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
		md, _ := metadata.FromIncomingContext(ss.Context())
		got <- md
		return status.Error(codes.Unavailable, "recording backend")
	}))
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()

	backends, closeAll, err := dialBackends(config.Config{GeoAddr: l.Addr().String(), ListenAddr: ":0"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"x-forwarded-for", "198.51.100.66", "grpcgateway-x-forwarded-for", "198.51.100.66",
		"forwarded", "for=198.51.100.66", "x-request-id", "rq-native-probe"))
	in, out := []byte{}, []byte{}
	_ = backends["geo"].Invoke(ctx, "/kacho.cloud.geo.v1.RegionService/Get", &in, &out, grpc.ForceCodec(rawCodec{}))

	select {
	case md := <-got:
		for _, k := range []string{"x-forwarded-for", "grpcgateway-x-forwarded-for", "forwarded"} {
			if v := md.Get(k); len(v) != 0 {
				t.Errorf("служба получила %s=%v по нативному переходу", k, v)
			}
		}
		if v := md.Get("x-request-id"); len(v) != 1 {
			t.Errorf("близнец: номер запроса потерян: %v", md)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("вызов до службы не дошёл — проба не исполнилась")
	}
}
