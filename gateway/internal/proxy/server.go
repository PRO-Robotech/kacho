// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy

import (
	"strings"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// Backends — карта «доменное имя → долгоживущий *grpc.ClientConn».
// Ключи: "iam", "vpc", "compute", "storage", "geo", "loadbalancer", "registry".
type Backends map[string]*grpc.ClientConn

// MethodResolver — re-export of local proxy MethodResolver type signature.
type MethodResolver = methodResolverInternal

// RoutableDomain сообщает ключ Backends, без которого fullMethod не
// маршрутизируется вовсе: сегмент `<domain>` облачной формы
// "/<корень>.cloud.<domain>.<...>/<Method>" (корни — ContractRoots, выведенные
// из словаря `corelib/contractroot`) либо второй сегмент формы фундамента
// "/corelib.<пакет>.<...>/<Method>"
// (FoundationRoots). ok=false — путь не той формы, какую край маршрутизирует,
// что бы ни лежало в карте бэкендов.
//
// Exported because being IN the allowlist and being ROUTABLE are two different
// facts, and until this existed only the first one was checkable from outside.
// The allowlist is computed against the proto descriptors (allowlist/parity_test.go),
// but the map of open backend connections is built by the composition root from a
// hand-written key set, and nothing joined the two: a domain whose key is absent
// resolves to nothing, and the caller is answered by exactly the error that hides
// the admin surface. The wiring gate calls THIS function rather than re-deriving
// the domain, so the gate and the resolver cannot come to disagree about what a
// method needs.
func RoutableDomain(fullMethod string) (string, bool) {
	if !hasContractRoot(fullMethod) {
		return "", false
	}
	// fullMethod = "/<корень>.cloud.<domain>.<...>/<Method>" либо "/corelib.<пакет>.<...>/<Method>"
	parts := strings.SplitN(fullMethod, "/", 3)
	if len(parts) < 3 || parts[1] == "" {
		return "", false
	}
	pkgParts := strings.Split(parts[1], ".")
	// Пакет фундамента — `<корень>.<пакет>[.<версия>].<Служба>`: ключ — ВТОРОЙ
	// сегмент (см. FoundationRoots). Облачный пакет — `<корень>.cloud.<домен>.…`:
	// ключ — третий.
	if _, ok := foundationRootOf(parts[1]); ok {
		if len(pkgParts) < 3 || pkgParts[1] == "" {
			return "", false
		}
		return pkgParts[1], true
	}
	if len(pkgParts) < 4 {
		return "", false
	}
	return pkgParts[2], true
}

// Resolver builds a MethodResolver for the unknown-service handler installed
// on the gRPC server. Behaviour:
//
//  1. метод с корнем контракта (`kacho.cloud.`, `kaname.cloud.`, `corelib.`) —
//     проходит allowlist и поиск бэкенда.
//  2. anything else (blocked / Internal* / unknown method) — returns ok=false,
//     which Handler (shimproxy.go) maps to codes.NotFound, NOT Unimplemented.
//     NotFound is load-bearing: it makes Internal* and unknown methods
//     indistinguishable from "method does not exist", so the external listener
//     is not an existence-oracle for admin endpoints ("exists but unimplemented"
//     would leak reconnaissance). Keep parity with the shimproxy.go contract.
//
// THE REFUSAL HERE IS NOT THE WHOLE STORY, and for a while this comment claimed
// it was. This resolver runs at the END of the interceptor chain, so anything
// that answers earlier answers INSTEAD. Authorization did: an Internal* method
// whose permission the caller lacked came back PermissionDenied naming the
// permission, while its neighbours came back "unknown method" — the very
// distinction the paragraph above says does not exist. Nine of the platform's
// seventy-nine Internal* methods behaved that way, all of them ones whose
// authorization target is resolved from the request body.
//
// The uniform answer is therefore produced by route_refusal.go, an interceptor
// mounted AHEAD of authorization and returning byte-for-byte the error Handler
// returns here. This resolver remains the backstop: it is what makes the
// property true for traffic that reaches it, and what the refusal is copied
// from. Changing the message in one place without the other reintroduces the
// difference.
//
// Маршрутизируются только службы с объявленным корнем контракта (`kacho.cloud.*`,
// `kaname.cloud.*`, `corelib.*` — contract_roots.go); посторонних служб бэкенды
// не выставляют.
func Resolver(backends Backends) MethodResolver {
	return func(fullMethod string) (string, grpc.ClientConnInterface, bool) {
		method := fullMethod
		if !hasContractRoot(method) {
			return "", nil, false
		}
		if allowlist.HasInternalSuffix(method) || !allowlist.IsAllowed(method) {
			return "", nil, false
		}
		domain, ok := RoutableDomain(method)
		if !ok {
			return "", nil, false
		}
		conn, ok := backends[domain]
		if !ok {
			return "", nil, false
		}
		return method, conn, true
	}
}

// NewServer создаёт gRPC-сервер, чей UnknownServiceHandler направляет трафик
// служб с объявленным корнем контракта (`kacho.cloud.*`, `kaname.cloud.*`,
// `corelib.*`) в бэкенд их домена.
//
// Native services registered on this server (Health, OperationService) take
// precedence over the unknown-service handler, as per gRPC dispatch
// semantics.
func NewServer(resolve MethodResolver, opts ...grpc.ServerOption) *grpc.Server {
	base := []grpc.ServerOption{
		grpc.UnknownServiceHandler(Handler(resolve)),
	}
	return grpc.NewServer(append(base, opts...)...)
}
