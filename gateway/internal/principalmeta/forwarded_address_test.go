// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// forwarded_address_test.go — АДРЕС ИСТОЧНИКА ЗА КРАЙ НЕ УЕЗЖАЕТ (kacho#3028,
// круг 5, канал «выход края к службам»).
//
// Край выводит адрес клиента ОДНИМ оператором и пользуется им сам (условие
// модели прав, ретрансляция полосы входа). За краем адрес из метаданных не
// читает ни одна служба — а уезжал он туда словами клиента: нативный переход
// копирует входящие метаданные целиком (`x-forwarded-for`, `forwarded`,
// `grpcgateway-*` клиента), мост REST дописывает заголовок клиента к адресу
// пира. Служба, которая начала бы читать этот ключ, читала бы подделку.
// Поэтому исходящий вызов края к службе адреса источника не несёт вовсе —
// перехватчиком соединения, а не правкой каждой сборки метаданных.
package principalmeta_test

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// forwardedKeys — каждая форма адреса источника, которую пишет клиент, звено
// или мост: голая и под приставкой моста.
var forwardedKeys = []string{
	"x-forwarded-for", "x-real-ip", "forwarded", "x-original-forwarded-for",
	"grpcgateway-x-forwarded-for", "grpcgateway-x-real-ip", "grpcgateway-forwarded",
	"grpcgateway-x-original-forwarded-for",
}

func outgoingWith(pairs ...string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs(pairs...))
}

// Унарный вызов: адрес источника снят во всех формах, прочие ключи — на
// месте (близнец в том же вызове: личность и номер запроса).
func TestForwardedAddressStrip_UnaryDropsEveryFormAndKeepsTheRest(t *testing.T) {
	pairs := []string{"x-kacho-principal-id", "usr-1", "x-request-id", "rq-1"}
	for _, k := range forwardedKeys {
		pairs = append(pairs, k, "198.51.100.66")
	}
	var seen metadata.MD
	invoker := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		seen, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}
	if err := principalmeta.ForwardedAddressStripUnary()(outgoingWith(pairs...), "/x.Svc/M", nil, nil, nil, invoker); err != nil {
		t.Fatal(err)
	}
	for _, k := range forwardedKeys {
		if v := seen.Get(k); len(v) != 0 {
			t.Errorf("ключ %s уехал к службе: %v", k, v)
		}
	}
	if seen.Get("x-kacho-principal-id")[0] != "usr-1" || seen.Get("x-request-id")[0] != "rq-1" {
		t.Errorf("близнец: прочие ключи потеряны: %v", seen)
	}
}

// Поток: то же снятие.
func TestForwardedAddressStrip_StreamDropsTheAddress(t *testing.T) {
	var seen metadata.MD
	streamer := func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		seen, _ = metadata.FromOutgoingContext(ctx)
		return nil, nil
	}
	_, _ = principalmeta.ForwardedAddressStripStream()(outgoingWith("x-forwarded-for", "198.51.100.66", "x-request-id", "rq"),
		&grpc.StreamDesc{}, nil, "/x.Svc/S", streamer)
	if len(seen.Get("x-forwarded-for")) != 0 || len(seen.Get("x-request-id")) != 1 {
		t.Fatalf("поток: %v", seen)
	}
}

// Входящие метаданные исходящего вызова не меняются: снятие — копия, а не
// правка общего отображения (его читает оператор адреса края).
func TestForwardedAddressStrip_DoesNotMutateTheCallersMetadata(t *testing.T) {
	md := metadata.Pairs("x-forwarded-for", "198.51.100.66")
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error { return nil }
	_ = principalmeta.ForwardedAddressStripUnary()(ctx, "/x.Svc/M", nil, nil, nil, invoker)
	if len(md.Get("x-forwarded-for")) != 1 {
		t.Fatal("снятие изменило отображение вызывающего")
	}
}
