// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// cert_bound_identity_test.go — anti-confused-deputy guard для internal-листенера
// (:9091), где живут admin-CRUD InternalRegionService / InternalZoneService.
//
// SECURITY: principal — единственный subject per-RPC FGA Check. Trust-aware
// связка grpcsrv.UnaryCertIdentityExtract + UnaryTrustedPrincipalExtract снимает
// forwarded x-kacho-principal-* metadata на peer'е без verified mTLS-cert'а. Но
// БЕЗ allow-list форвардеров (WithTrustedForwarders) ЛЮБОЙ verified mTLS-peer
// (напр. другой внутренний сервис со своим валидным client-cert'ом) мог форвардить
// произвольного principal'а (в т.ч. администратора) → эскалация до admin-CRUD
// Region/Zone (confused-deputy). Единственный легитимный источник forwarded
// end-user principal'а на :9091 — api-gateway; consumer'ы vpc/compute/nlb ходят в
// публичный :9090 (read-only Zone/Region.Get), а не в internal. Fix пробрасывает
// allow-list SAN'ов доверенных форвардеров (WithTrustedForwarders, config-driven)
// в оба internal-интерсептора: у verified-но-не-форвардер peer'а principal
// снимается → authz видит no-principal → fail-closed deny.
//
// Два комплементарных стража:
//   1. wiring guard (source-level): internalUnary/internalStream навешивают
//      TrustedPrincipalExtract С WithTrustedForwarders(...) — не bare-вариант.
//   2. behavioral guard: точная цепочка internal-листенера с allow-list доказывает,
//      что forged principal verified-но-не-форвардер peer'а снимается (carrier —
//      SystemPrincipal, trusted=false), а api-gateway — honored.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

const gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// --- 1. wiring guard (robust source assertion, non-vacuous) ---

// TestServe_BothListeners_UseSharedPrincipalBuilder — оба листенера (public :9090 и
// internal :9091) строятся из ЕДИНОГО newPrincipalInterceptors(forwarders), а не
// расходящимися inline-цепочками. Робастная замена прежнему brace-block-скрейпингу
// (тот мог пройти вакуумно на пустом/несовпавшем блоке — см. 2-й аудит finding
// «brittle source-string matching»). Проверяет:
//  1. ровно ДВА вызова newPrincipalInterceptors(forwarders) — public + internal;
//  2. forwarders привязан к cfg.AuthZTrustedForwarderSANs (allow-list доезжает);
//  3. сам builder навешивает TrustedPrincipalExtract(WithTrustedForwarders(...));
//  4. нет bare PrincipalExtract (доверяющего любому peer'у).
//
// Поведенческая сторона (что цепочка реально снимает forged principal) покрыта
// тестами ниже, которые исполняют РЕАЛЬНЫЙ newPrincipalInterceptors, а не локальную
// реконструкцию — source-assertion лишь фиксирует, что serve.go его вызывает дважды.
func TestServe_BothListeners_UseSharedPrincipalBuilder(t *testing.T) {
	src := readServeSrc(t)

	if n := strings.Count(src, "newPrincipalInterceptors(forwarders)"); n != 2 {
		t.Fatalf("serve.go: newPrincipalInterceptors(forwarders) вызван %d раз, ожидалось 2 (public+internal) — листенеры могут разъехаться по anti-spoof", n)
	}
	if !strings.Contains(src, "forwarders := cfg.AuthZTrustedForwarderSANs") {
		t.Errorf("serve.go: forwarders не привязан к cfg.AuthZTrustedForwarderSANs — allow-list форвардеров может не доехать до цепочки")
	}
	if !strings.Contains(src, "grpcsrv.UnaryTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(") ||
		!strings.Contains(src, "grpcsrv.StreamTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(") {
		t.Errorf("serve.go: newPrincipalInterceptors без TrustedPrincipalExtract(WithTrustedForwarders(...)) — principal НЕ trust-gated (confused-deputy)")
	}
	if strings.Contains(src, "grpcsrv.UnaryPrincipalExtract(") || strings.Contains(src, "grpcsrv.StreamPrincipalExtract(") {
		t.Errorf("serve.go: bare PrincipalExtract присутствует — forwarded principal доверяется безусловно (principal-spoofing)")
	}
}

// --- 2. behavioral guard ---

// TestInternalPrincipalChain_ForwarderAllowlist_DropsNonGateway — точная
// internal-цепочка (CertIdentityExtract → TrustedPrincipalExtract) с allow-list
// api-gateway SA. Verified-но-не-форвардер peer (внутренний сервис со своим
// валидным cert'ом) форвардить пользователя НЕ может; api-gateway — может.
func TestInternalPrincipalChain_ForwarderAllowlist_DropsNonGateway(t *testing.T) {
	chain := internalPrincipalChain(gatewaySAN)

	t.Run("verified_non_gateway_forged_admin_dropped", func(t *testing.T) {
		other := "spiffe://kacho.cloud/ns/kacho/sa/kacho-vpc"
		ctx := withForgedPrincipal(verifiedPeerCtx(t, other), "usr-admin-victim")

		carrierID, trusted := runChain(t, chain, ctx)
		if trusted {
			t.Errorf("verified-но-не-форвардер peer (%s) НЕ должен форвардить end-user principal'а", other)
		}
		if carrierID == "usr-admin-victim" {
			t.Errorf("confused-deputy: internal-сервис проштамповал admin-principal'а 'usr-admin-victim' как subject FGA Check")
		}
		if carrierID != operations.SystemPrincipal().ID {
			t.Errorf("forged principal протёк в carrier: got %q, want system fallback %q", carrierID, operations.SystemPrincipal().ID)
		}
	})

	t.Run("gateway_peer_principal_honored", func(t *testing.T) {
		ctx := withForgedPrincipal(verifiedPeerCtx(t, gatewaySAN), "usr-admin")

		carrierID, trusted := runChain(t, chain, ctx)
		if !trusted {
			t.Errorf("principal от доверенного форвардера (api-gateway SAN) обязан быть honored")
		}
		if carrierID != "usr-admin" {
			t.Errorf("gateway-forwarded principal не honored: got %q, want %q", carrierID, "usr-admin")
		}
	})
}

// --- helpers ---

// internalPrincipalChain собирает unary-цепочку principal-extract из РЕАЛЬНОГО
// serve.go-builder'а newPrincipalInterceptors — тест исполняет ту же wiring-логику,
// что и продовый листенер, а не её локальную реконструкцию (устранена зависимость
// от совпадения строк serve.go). forwarderSANs — allow-list доверенных форвардеров.
func internalPrincipalChain(forwarderSANs ...string) grpc.UnaryServerInterceptor {
	unary, _ := newPrincipalInterceptors(forwarderSANs)
	return chainUnaryServer(unary...)
}

func runChain(t *testing.T, chain grpc.UnaryServerInterceptor, ctx context.Context) (carrierID string, trusted bool) {
	t.Helper()
	final := func(c context.Context, _ any) (any, error) {
		carrierID = operations.PrincipalFromContext(c).ID
		_, trusted = grpcsrv.TrustedPrincipalFromContext(c)
		return nil, nil
	}
	if _, err := chain(ctx, nil, nil, final); err != nil {
		t.Fatalf("chain returned error: %v", err)
	}
	return carrierID, trusted
}

func withForgedPrincipal(ctx context.Context, id string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		grpcsrv.MDKeyPrincipalType, "user",
		grpcsrv.MDKeyPrincipalID, id,
		grpcsrv.MDKeyPrincipalDisplay, id+"@example.com",
	))
}

// verifiedPeerCtx — mTLS-verified peer: непустая verified-chain с leaf-cert'ом,
// несущим переданный SPIFFE-SAN.
func verifiedPeerCtx(t *testing.T, san string) context.Context {
	t.Helper()
	leaf := &x509.Certificate{URIs: mustParseURIs(t, san)}
	tlsPeer := &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{leaf}},
	}}}
	return peer.NewContext(context.Background(), tlsPeer)
}

// chainUnaryServer композирует unary server-интерсепторы слева-направо вокруг
// финального handler'а (семантика grpc.ChainUnaryInterceptor).
func chainUnaryServer(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		chained := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			ic := interceptors[i]
			next := chained
			chained = func(c context.Context, r any) (any, error) { return ic(c, r, info, next) }
		}
		return chained(ctx, req)
	}
}

func readServeSrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	return string(b)
}

func mustParseURIs(t *testing.T, raw ...string) []*url.URL {
	t.Helper()
	out := make([]*url.URL, 0, len(raw))
	for _, r := range raw {
		u, err := url.Parse(r)
		if err != nil {
			t.Fatalf("parse uri %q: %v", r, err)
		}
		out = append(out, u)
	}
	return out
}
