// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// public_principal_test.go — anti-spoof guard для ПУБЛИЧНОГО листенера (:9090),
// где живут read-only RegionService / ZoneService.Get/List.
//
// SECURITY: до фикса публичная цепочка использовала bare
// grpcsrv.UnaryPrincipalExtract(), читавшую forwarded x-kacho-principal-* metadata
// БЕЗУСЛОВНО — любой peer с валидным platform-issued client-cert'ом (mTLS —
// RequireAndVerifyClientCert) мог выставить произвольный principal-header и быть
// авторизован как этот principal (CWE-290). Internal-листенер уже trust-gated;
// публичный — нет. Fix навешивает ту же trust-aware связку
// grpcsrv.UnaryCertIdentityExtract + UnaryTrustedPrincipalExtract(
// WithTrustedForwarders(...)) и на публичную цепочку: verified-но-не-форвардер
// peer не может выдать себя за viewer-principal'а, honored только api-gateway.

import (
	"testing"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Source-level wiring guard для public/internal листенеров теперь — общий
// TestServe_BothListeners_UseSharedPrincipalBuilder (cert_bound_identity_test.go):
// оба листенера строятся из единого newPrincipalInterceptors(forwarders), поэтому
// отдельный per-listener brace-block-скрейпинг (хрупкий, мог пройти вакуумно) убран.
// Ниже — поведенческий страж публичной цепочки поверх РЕАЛЬНОГО builder'а.

// --- behavioral guard ---

// TestPublicPrincipalChain_ForwarderAllowlist_DropsNonGateway — точная публичная
// цепочка (CertIdentityExtract → TrustedPrincipalExtract) с allow-list api-gateway
// SA: verified-но-не-форвардер peer (внутренний сервис vpc/compute со своим
// валидным cert'ом) форвардить viewer-principal НЕ может; api-gateway — может.
func TestPublicPrincipalChain_ForwarderAllowlist_DropsNonGateway(t *testing.T) {
	chain := publicPrincipalChain(gatewaySAN)

	t.Run("verified_non_gateway_forged_viewer_dropped", func(t *testing.T) {
		other := "spiffe://kacho.cloud/ns/kacho/sa/kacho-compute"
		ctx := withForgedPrincipal(verifiedPeerCtx(t, other), "usr-viewer-victim")

		carrierID, trusted := runChain(t, chain, ctx)
		if trusted {
			t.Errorf("verified-но-не-форвардер peer (%s) НЕ должен форвардить end-user principal'а", other)
		}
		if carrierID == "usr-viewer-victim" {
			t.Errorf("principal-spoofing: peer проштамповал 'usr-viewer-victim' как subject FGA Check на публичном endpoint")
		}
		if carrierID != operations.SystemPrincipal().ID {
			t.Errorf("forged principal протёк в carrier: got %q, want system fallback %q", carrierID, operations.SystemPrincipal().ID)
		}
	})

	t.Run("gateway_peer_principal_honored", func(t *testing.T) {
		ctx := withForgedPrincipal(verifiedPeerCtx(t, gatewaySAN), "usr-viewer")

		carrierID, trusted := runChain(t, chain, ctx)
		if !trusted {
			t.Errorf("principal от доверенного форвардера (api-gateway SAN) обязан быть honored")
		}
		if carrierID != "usr-viewer" {
			t.Errorf("gateway-forwarded principal не honored: got %q, want %q", carrierID, "usr-viewer")
		}
	})
}

// publicPrincipalChain собирает unary-цепочку principal-extract из РЕАЛЬНОГО
// serve.go-builder'а newPrincipalInterceptors (public и internal используют один и
// тот же builder), а не её локальную реконструкцию. forwarderSANs — allow-list
// доверенных форвардеров.
func publicPrincipalChain(forwarderSANs ...string) grpc.UnaryServerInterceptor {
	unary, _ := newPrincipalInterceptors(forwarderSANs)
	return chainUnaryServer(unary...)
}
