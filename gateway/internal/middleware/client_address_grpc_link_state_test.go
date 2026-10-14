// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// client_address_grpc_link_state_test.go — НА НАТИВНОМ gRPC ОПЕРАТОР АДРЕСА
// ЧИТАЕТ СОСТОЯНИЕ TLS ЗВЕНА ИЗ linktls.AuthInfo (kacho#3028, строки 12 и 33).
//
// Внешний сервер края кладёт состояние TLS своим типом linktls.AuthInfo, а не
// credentials.TLSInfo: TLSInfo читает полоса личности по сертификату, и выдай
// его сервер, лист установки стал бы служебной учёткой снаружи (строка 33).
// Значит, модель прав обязана брать состояние звена там, куда его кладёт
// сервер, — через linktls.PeerState. Прочти она TLSInfo — звено на gRPC не
// узналось бы никогда, и адрес клиента за звеном стал бы адресом звена.
//
// Близнецы: та же цепочка в credentials.TLSInfo (форма, которой сервер края
// не производит) и пир без состояния — источник остаётся пиром.

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestAuthz_GRPC_LinkStateIsReadFromTheEdgeServerCredentials(t *testing.T) {
	linkCA := newTestAuthority(t, "front-link-ca")
	extractor := middleware.NewContextExtractor(time.Now, true,
		middleware.WithTrustedProxyHops(1),
		middleware.WithTrustedProxies(netip.MustParsePrefix("10.244.0.0/16")),
		middleware.WithTrustedPeers(links{frontPod}),
		middleware.WithTrustedLinkSANs(frontSAN),
		middleware.WithTrustedLinkAnchor(linktls.NewAnchor(linkCA.cert)))
	state := linkCA.state(t, frontSAN)

	for _, c := range []struct {
		name string
		info credentials.AuthInfo
		want string
	}{
		{"состояние сервера края (linktls.AuthInfo) — звено", linktls.AuthInfo{State: *state}, clientA},
		{"близнец: та же цепочка в credentials.TLSInfo — не читается", credentials.TLSInfo{State: *state}, frontPod},
		{"близнец: состояния нет", nil, frontPod},
	} {
		t.Run(c.name, func(t *testing.T) {
			checker := &fakeChecker{allowed: true}
			mw := buildAuthzMiddleware(t, buildCatalog(t, getEntry), checker, func(cfg *middleware.AuthzMiddlewareConfig) {
				cfg.Context = extractor
			})
			md := metadata.New(map[string]string{
				"x-kacho-principal-id":   "usr_x",
				"x-kacho-principal-type": "user",
				"x-kacho-token-acr":      "2",
				"x-forwarded-for":        clientA,
			})
			ctx := peer.NewContext(metadata.NewIncomingContext(context.Background(), md),
				&peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(frontPod), Port: 40000}, AuthInfo: c.info})
			if _, err := mw.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/Get"},
				func(context.Context, any) (any, error) { return "ok", nil }); err != nil {
				t.Fatalf("вызов отвергнут: %v", err)
			}
			in := checker.lastInput.Load()
			if in == nil {
				t.Fatal("проверка прав не позвана — проба не видит условия client_ip")
			}
			if got := in.Context["client_ip"]; got != c.want {
				t.Errorf("client_ip = %v, ожидался %s", got, c.want)
			}
		})
	}
}
