// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package principalmeta is the single source of truth for the principal-identity
// header/metadata key contract that authorization decisions,
// operation-ownership enforcement, and principal forwarding all depend on.
//
// Producers (auth / DPoP middleware) set these on the request; consumers
// (authz, idempotency, restmux WithMetadata, opsproxy) read them. Previously
// each side re-typed the bare string literal independently, so a rename on one
// side that missed the other would compile cleanly and silently drop the
// principal at runtime (anonymous → PermissionDenied), a security-relevant break
// with no compile-time or test signal. Referencing these constants makes any
// divergence a compile error.
//
// Three surface forms exist for the SAME logical key:
//
//   - Header*      — canonical HTTP header (http.Header.Set/Get canonicalises to
//     this casing). Used by HTTP-side producers/consumers and downstream audit.
//   - HeaderGRPCMeta* — the "Grpc-Metadata-"-prefixed HTTP header. grpc-gateway
//     forwards an HTTP header `Grpc-Metadata-Foo` to the backend as gRPC
//     metadata `foo`, so producers set BOTH the bare and the prefixed header.
//   - Meta*        — the lowercase gRPC metadata key (metadata.MD is lowercased)
//     read by a backend / opsproxy via metadata.FromIncomingContext.
package principalmeta

import "strings"

// Canonical HTTP header names.
const (
	HeaderPrincipalType    = "X-Kacho-Principal-Type"
	HeaderPrincipalID      = "X-Kacho-Principal-Id"
	HeaderPrincipalDisplay = "X-Kacho-Principal-Display-Name"
	HeaderTokenACR         = "X-Kacho-Token-Acr"   // #nosec G101 -- HTTP header name (token ACR claim), not a credential
	HeaderTokenJti         = "X-Kacho-Token-Jti"   // #nosec G101 -- HTTP header name (token jti claim), not a credential
	HeaderTokenScope       = "X-Kacho-Token-Scope" // #nosec G101 -- HTTP header name (token scope claim), not a credential
	HeaderTokenExp         = "X-Kacho-Token-Exp"   // #nosec G101 -- HTTP header name (token exp claim), not a credential
)

// Grpc-Metadata-prefixed HTTP header names (grpc-gateway → gRPC metadata bridge).
const (
	HeaderGRPCMetaPrincipalType    = "Grpc-Metadata-" + HeaderPrincipalType
	HeaderGRPCMetaPrincipalID      = "Grpc-Metadata-" + HeaderPrincipalID
	HeaderGRPCMetaPrincipalDisplay = "Grpc-Metadata-" + HeaderPrincipalDisplay
	HeaderGRPCMetaTokenACR         = "Grpc-Metadata-" + HeaderTokenACR
	HeaderGRPCMetaTokenJti         = "Grpc-Metadata-" + HeaderTokenJti
	HeaderGRPCMetaTokenScope       = "Grpc-Metadata-" + HeaderTokenScope
)

// Lowercase gRPC metadata keys (metadata.MD.Get/Append lowercases its argument;
// backends read exactly these).
const (
	MetaPrincipalType    = "x-kacho-principal-type"
	MetaPrincipalID      = "x-kacho-principal-id"
	MetaPrincipalDisplay = "x-kacho-principal-display-name"
	MetaTokenACR         = "x-kacho-token-acr"   // #nosec G101 -- gRPC metadata key name (token ACR claim), not a credential
	MetaTokenJti         = "x-kacho-token-jti"   // #nosec G101 -- gRPC metadata key name (token jti claim), not a credential
	MetaTokenScope       = "x-kacho-token-scope" // #nosec G101 -- gRPC metadata key name (token scope claim), not a credential
)

// Lowercase prefixes used to strip forgeable client-supplied identity
// headers/metadata before the gateway sets its own trusted values.
const (
	MetaPrincipalPrefix = "x-kacho-principal-"
	MetaTokenPrefix     = "x-kacho-token-" // #nosec G101 -- metadata key prefix, not a credential
)

// Namespace is the reserved prefix of EVERY Kachō identity/context key on the
// wire. Anything under it is gateway-owned by definition: it is derived from a
// validated credential and a client may never supply it.
//
// BridgePrefix is grpc-gateway's REST→gRPC bridge prefix: an inbound HTTP header
// `Grpc-Metadata-Foo` is forwarded to the backend as gRPC metadata `foo`
// (runtime.DefaultHeaderMatcher). Both surface forms of the same logical key must
// therefore be treated identically by any policy over the namespace.
const (
	Namespace    = "x-kacho-"
	BridgePrefix = "grpc-metadata-"
)

// gatewayProducedPrefixes — the CLOSED set of `x-kacho-` sub-families the
// gateway itself produces after a validated credential and is therefore allowed
// to forward to a backend:
//
//   - `x-kacho-principal-` (type / id / display-name) — set by the Bearer, Kratos,
//     DPoP and mTLS auth paths (setPrincipalHeaders / injectVerifiedTokenHeaders).
//   - `x-kacho-token-` (acr / jti / scope / exp) — validated JWT claims, consumed
//     by the step-up gate and the iam acr-floor on the internal re-dial.
//
// Everything else in the namespace — `x-kacho-admin`, `x-kacho-project-id`,
// `x-kacho-actor`, and any key added tomorrow — is client-forgeable input and is
// dropped at the edge.
//
// # Why a closed set and not a list of banned names
//
// The previous form enumerated the FORBIDDEN keys (principal + token). It was
// correct on the day it was written and silently wrong the day a backend started
// reading a third key: `x-kacho-admin` and `x-kacho-project-id` were never added
// to the list, so a client could set `Grpc-Metadata-X-Kacho-Admin: true`, the
// bridge forwarded it verbatim, and kacho-compute raised its cluster-admin flag
// on the PUBLIC listener — lifting the operation-ownership predicate (an
// Operation response carries the created resource in full). A deny-list has to
// be extended for every new key or it leaks; an allow-list has to be extended for
// every new key or the key simply does not work. Only the second failure mode is
// safe, and only the second one is noticed immediately.
var gatewayProducedPrefixes = []string{MetaPrincipalPrefix, MetaTokenPrefix}

// KachoNamespaceKey normalises an inbound HTTP header name or gRPC metadata key
// to its bare lower-cased form and reports whether it belongs to the reserved
// `x-kacho-` namespace. The grpc-gateway bridge prefix is stripped first, so
// `Grpc-Metadata-X-Kacho-Admin` and `x-kacho-admin` both yield
// ("x-kacho-admin", true).
func KachoNamespaceKey(key string) (string, bool) {
	lower := strings.ToLower(key)
	lower = strings.TrimPrefix(lower, BridgePrefix)
	if !strings.HasPrefix(lower, Namespace) {
		return "", false
	}
	return lower, true
}

// IsGatewayProducedKey reports whether a bare lower-cased `x-kacho-` key (as
// returned by KachoNamespaceKey) is one the gateway itself produces — i.e. one
// that may legitimately cross the REST→gRPC bridge to a backend.
func IsGatewayProducedKey(name string) bool {
	for _, p := range gatewayProducedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// IsClientForgeableKey reports whether an inbound header name / metadata key
// must be stripped before any auth path runs. True for the ENTIRE `x-kacho-`
// namespace in both surface forms: every key under it is gateway-derived, so a
// client-supplied one is by definition a forgery — including the keys the
// gateway produces itself (they are re-set from the validated credential right
// after the strip).
//
// No client-supplied `X-Kacho-…` header is legitimate at the gateway edge: the
// UI and the SDKs authenticate with `Authorization`/`Cookie`/`DPoP`, and the one
// shared-secret Kachō header that does exist (`X-Kacho-Hook-Token`, the
// Hydra→iam webhook) is served by iam's own HTTP listener, not by this gateway.
// If that ever changes, add the key to an explicit allow-list here rather than
// narrowing the namespace sweep.
func IsClientForgeableKey(key string) bool {
	_, ok := KachoNamespaceKey(key)
	return ok
}
