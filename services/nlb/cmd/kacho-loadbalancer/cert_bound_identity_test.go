// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// cert_bound_identity_test.go — anti-spoof guard для principal-identity на ОБОИХ
// gRPC-листенерах (9090 public и :9091 internal).
//
// SECURITY: principal — единственный subject per-RPC FGA Check. Прежняя связка
// grpcsrv.UnaryPrincipalExtract / StreamPrincipalExtract БЕЗУСЛОВНО доверяла
// x-kacho-principal-* metadata любого peer'а: peer без верифицированного
// mTLS-client-cert'а мог подделать identity (usr-victim) и получить его права. Fix
// переводит оба листенера на trust-aware связку grpcsrv.UnaryCertIdentityExtract +
// grpcsrv.UnaryTrustedPrincipalExtract (+ опциональный SAN-allowlist форвардеров для
// api-gateway): forwarded principal доходит до use-case'ов/Check только когда
// CertIdentityExtract доказал, что peer mTLS-verified (и, если задан allowlist, что
// его SAN — доверенный форвардер).
//
// Два комплементарных стража:
//  1. wiring guard (source-level): оба листенера используют Trusted-варианты, НЕ
//     legacy; порядок CertIdentityExtract → TrustedPrincipalExtract сохранён.
//  2. behavioral guard: собирает точную цепочку principal-extract и доказывает, что
//     forged principal недоверенного peer'а снимается (carrier остаётся
//     SystemPrincipal, trusted=false), а verified peer — honored.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

const gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// # Куда делся страж проводки текстом исходника
//
// Здесь стоял `TestListeners_TakeThePairFromTheSharedConstructor`: он читал
// `wiring.go` и требовал, чтобы каждая из четырёх цепочек брала пару извлечения
// личности у общего конструктора. Его предмет ИСЧЕЗ вместе с цепочками — собрать
// свою пару композиционный корень больше не может, потому что поля
// интерсепторного типа в дескрипторе нет, а цепочку ставит носитель контура.
//
// Свойство не осталось без проверки, и вот чем оно держится теперь — тремя
// артефактами вместо одного грепа:
//
//   - `pkg/servicehost` — `TestForwardedIdentityIsHonouredOnlyFromTheCircle` и
//     `TestUnnarrowedCircleWouldHonourAnyVerifiedPeer`: пара стоит в цепочке
//     носителя, в правильном порядке, и решает на исполняемом коде;
//   - `internal/repohygiene` — `TestCompositionRootsCarryNoServerConstructionOfTheirOwn`:
//     ни один корень не держит значений интерсепторного типа, и это свойство
//     ДЕРЕВА, а не одного файла;
//   - `describe_test.go` — `TestDeclaredCircleIsTheOneTheProcessCarries`: какой
//     круг приносит ИМЕННО ЭТОТ сервис и что пустой он принести не вправе.
//
// Поведенческие стражи ниже остались как были: они собирают пару у того же общего
// конструктора и ничего не знали о проводке сервиса.

// --- 2. behavioral guards ---

// TestPrincipalChain_DropsForgedPrincipal_HonorsVerified — точная цепочка
// principal-extract (CertIdentityExtract → TrustedPrincipalExtract), как на обоих
// листенерах. Без SAN-allowlist (пусто → доверяем любому verified peer'у —
// insecure dev/back-compat).
//
//   - unverified TLS peer с forged x-kacho-principal-* → principal снимается,
//     carrier остаётся SystemPrincipal (RED с legacy extractor'ом — он бы
//     проштамповал usr-mallory как subject Check'а).
//   - mTLS-verified peer → principal honored (без регресса для verified-вызовов).
func TestPrincipalChain_DropsForgedPrincipal_HonorsVerified(t *testing.T) {
	chain := principalChainUnderTest()

	t.Run("unverified_tls_peer_forged_principal_dropped", func(t *testing.T) {
		ctx := withForgedPrincipal(unverifiedTLSPeerCtx(), "usr-mallory")

		carrierID, trusted, present := runChain(t, chain, ctx)
		if trusted {
			t.Errorf("principal недоверенного TLS-peer'а НЕ должен быть trusted")
		}
		if present {
			t.Errorf("носитель личности пережил недоверенного отправителя: got %q — он обязан быть "+
				"вычищен, иначе предикат владения признаёт этот запрос владельцем системно "+
				"записанных операций", carrierID)
		}
		if carrierID == "usr-mallory" {
			t.Errorf("spoof: forged principal id 'usr-mallory' дошёл до subject'а FGA Check")
		}
	})

	t.Run("verified_mtls_peer_principal_honored", func(t *testing.T) {
		ctx := withForgedPrincipal(verifiedPeerCtx(t, gatewaySAN), "usr-alice")

		carrierID, trusted, present := runChain(t, chain, ctx)
		if !trusted {
			t.Errorf("principal verified mTLS-peer'а обязан быть trusted (без регресса)")
		}
		if !present {
			t.Errorf("носитель личности обязан быть заполнен для доверенного отправителя")
		}
		if carrierID != "usr-alice" {
			t.Errorf("verified principal не honored: got %q, want %q", carrierID, "usr-alice")
		}
	})
}

// TestPrincipalChain_ForwarderAllowlist_DropsNonGateway — с заданным SAN-allowlist
// (api-gateway SA) end-user principal форвардится ТОЛЬКО когда cert-identity peer'а
// ∈ allowlist. Verified-но-не-форвардер (внутренний сервис со своим валидным
// cert'ом) подделать пользователя НЕ может (anti-confused-deputy).
func TestPrincipalChain_ForwarderAllowlist_DropsNonGateway(t *testing.T) {
	chain := principalChainUnderTest(gatewaySAN)

	t.Run("verified_non_gateway_peer_principal_dropped", func(t *testing.T) {
		other := "spiffe://kacho.cloud/ns/kacho/sa/kacho-vpc"
		ctx := withForgedPrincipal(verifiedPeerCtx(t, other), "usr-victim")

		carrierID, trusted, present := runChain(t, chain, ctx)
		if trusted {
			t.Errorf("verified-но-не-форвардер peer (%s) НЕ должен форвардить end-user principal'а", other)
		}
		if carrierID == "usr-victim" {
			t.Errorf("confused-deputy: internal-сервис проштамповал чужого principal'а 'usr-victim'")
		}
		if present {
			t.Errorf("носитель личности пережил недоверенного отправителя: got %q — он обязан быть "+
				"вычищен, иначе предикат владения признаёт этот запрос владельцем системно "+
				"записанных операций", carrierID)
		}
	})

	t.Run("gateway_peer_principal_honored", func(t *testing.T) {
		ctx := withForgedPrincipal(verifiedPeerCtx(t, gatewaySAN), "usr-alice")

		carrierID, trusted, present := runChain(t, chain, ctx)
		if !trusted {
			t.Errorf("principal от доверенного форвардера (api-gateway SAN) обязан быть honored")
		}
		if !present {
			t.Errorf("носитель личности обязан быть заполнен для доверенного форвардера")
		}
		if carrierID != "usr-alice" {
			t.Errorf("gateway-forwarded principal не honored: got %q, want %q", carrierID, "usr-alice")
		}
	})
}

// --- helpers ---

// principalChainUnderTest прогоняет запрос через ТУ ЖЕ пару извлечения, которую
// композиционный корень навешивает на оба листенера, — она берётся у общего
// конструктора, а не пересобирается здесь. Реконструкция была бы фикстурой,
// способной разойтись с продуктом молча: пара, собранная в тесте, осталась бы
// верной и после того, как продукт перестал бы её собирать.
// forwarderSANs — круг доверенных отправителей (пусто → круг не сужен).
func principalChainUnderTest(forwarderSANs ...string) grpc.UnaryServerInterceptor {
	return chainUnaryServer(grpcsrv.PrincipalExtractUnary(grpcsrv.NewTrustDomain("kacho.cloud"), grpcsrv.NewTrustedForwarders(forwarderSANs...))...)
}

// runChain прогоняет ctx через цепочку и возвращает то, что увидел бы handler:
// личность, признак доверия к пересылающему и НАЛИЧИЕ носителя личности.
//
// present читается через operations.PrincipalFromContextOK — единственный
// аксессор, который отличает «носитель вычищен» от «в носителе лежит системная
// личность». PrincipalFromContext такого различения не даёт по своему контракту
// (оба состояния → SystemPrincipal), поэтому утверждать им вычищенность нельзя:
// проверка останется зелёной и тогда, когда недоверенному пиру выдали системную
// личность, а она и есть владелец каждой системно записанной операции.
func runChain(t *testing.T, chain grpc.UnaryServerInterceptor, ctx context.Context) (carrierID string, trusted, present bool) {
	t.Helper()
	final := func(c context.Context, _ any) (any, error) {
		p, ok := operations.PrincipalFromContextOK(c)
		carrierID, present = p.ID, ok
		_, trusted = grpcsrv.TrustedPrincipalFromContext(c)
		return nil, nil
	}
	if _, err := chain(ctx, nil, nil, final); err != nil {
		t.Fatalf("chain returned error: %v", err)
	}
	return carrierID, trusted, present
}

func withForgedPrincipal(ctx context.Context, id string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		grpcsrv.MDKeyPrincipalType, "user",
		grpcsrv.MDKeyPrincipalID, id,
		grpcsrv.MDKeyPrincipalDisplay, id+"@example.com",
	))
}

// unverifiedTLSPeerCtx — TLS present, но НЕТ verified client-cert (пустой
// VerifiedChains) — ровно то, как выглядит cert-less/unverified peer.
func unverifiedTLSPeerCtx() context.Context {
	tlsPeer := &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{}}}
	return peer.NewContext(context.Background(), tlsPeer)
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
