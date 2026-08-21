// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_grpc_listener.go — dedicated internal-only gRPC listener for
// InternalAuthzCacheService (port 9091 by default). Internal/admin-only RPCs
// MUST NOT be exposed on the external TLS endpoint.
//
// The kacho-iam push-drainer dials KACHO_IAM_GATEWAY_INTERNAL_ADDR (e.g.
// "kacho-api-gateway-internal:9091") and invokes
// apigateway.v1.InternalAuthzCacheService.InvalidateSubject on the listener
// built here, so a revoke lands as push-invalidation within <1s. A background
// subject-change poll-loop converges sibling replicas as a fallback.
package main

import (
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// startInternalGRPCListener builds the internal-only gRPC server, listens on
// addr (host:port; ":0" for ephemeral in tests), registers
// InternalAuthzCacheService on it (and NOT on externalSrv — internal-only
// invariant), and returns the wired server + listener + this listener's rate
// ceiling, so the caller drives Serve() and GracefulStop() under its existing
// signal-shutdown flow and counts admissions in the same place it counts the
// external listener's.
//
// SECURITY (security.md invariant #1/#4): the internal perimeter is NOT trusted.
// When sec.mtlsEnabled the listener mounts mTLS transport credentials
// (RequireAndVerifyClientCert against the internal CA) PLUS a per-RPC SPIFFE
// allow-list interceptor chain (authN+authZ), so only the allow-listed iam
// push-drainer identity can flush the authz cache. When mTLS is disabled it is
// the dev/local insecure listener (opt-in) — the production guard in main.go
// refuses that posture in a production-class env. Server-reflection rides that
// same condition (see below): this is the only listener that serves it, and only
// where callers are authenticated and authorised.
//
// addr=":0" → kernel picks port; the caller can read it via lis.Addr() (used
// by the unit test for ephemeral-port lifecycle).
//
// limits — the per-caller rate/concurrency ceiling for THIS listener. Passed in
// rather than resolved here so both listeners answer one resolution of the
// posture: two resolutions of the same knobs are two places about one subject,
// and they drift silently — one listener would end up on the platform floor and
// the other on the operator's numbers with nothing red anywhere.
func startInternalGRPCListener(
	addr string, inv handler.Invalidator,
	externalSrv *grpc.Server, sec internalListenerSecurity,
	limits grpcsrv.AdmissionLimits, logger *slog.Logger,
) (*grpc.Server, net.Listener, *grpcsrv.Admission, error) {
	if addr == "" {
		return nil, nil, nil, fmt.Errorf("internal grpc listener: addr required")
	}
	if externalSrv == nil {
		// Defensive: RegisterInternalAuthzCacheService panics on nil
		// externalSrv to enforce the internal-only invariant. Surface the
		// same error at construction time so wiring bugs are caught before
		// Serve().
		return nil, nil, nil, fmt.Errorf("internal grpc listener: externalSrv required (pass both servers to make the internal-only invariant explicit)")
	}
	if sec.mtlsEnabled && sec.serverCreds == nil {
		// Defensive: buildInternalListenerSecurity never returns this shape, but a
		// hand-rolled posture with mtlsEnabled and no creds would silently downgrade
		// to plaintext — fail loudly instead.
		return nil, nil, nil, fmt.Errorf("internal grpc listener: mTLS enabled but server credentials are nil")
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("listen internal grpc %s: %w", addr, err)
	}

	opts := []grpc.ServerOption{
		// Match external-server keepalives so long-lived drainer streams stay
		// healthy across NAT / kube-proxy idle timeouts.
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 0, // never close idle conns (drainer is long-lived)
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			// Permit the long-lived push-drainer to send frequent keepalive
			// pings: an explicit small MinTime (well under any drainer ping
			// interval — its client keepalive Time is 10s) keeps the server
			// from counting the pings as too-frequent and issuing a GOAWAY,
			// which the gRPC server default (5m MinTime) would do. Without an
			// explicit KeepaliveEnforcementPolicy the field would NOT be the
			// permissive posture — it is precisely this value that permits it.
			MinTime:             time.Second,
			PermitWithoutStream: true,
		}),
	}
	// Звено восстановления паники — БЕЗУСЛОВНО, до и независимо от посадки mTLS.
	// Оно намеренно НЕ живёт в sec.serverOptions: та возвращает пустой набор на
	// insecure-посадке, и звено уехало бы вместе с ней — то есть пропадало бы
	// ровно там, где на листенере не остаётся вообще ни одного интерсептора.
	// Доступность от посадки транспорта не зависит: паника обработчика ЭТОГО
	// листенера завершает процесс ВСЕГО края (вместе с внешним :443 и
	// REST-мультиплексором), а не только internal-порт.
	opts = append(opts,
		grpc.ChainUnaryInterceptor(grpcsrv.UnaryPanicRecovery(logger)),
		grpc.ChainStreamInterceptor(grpcsrv.StreamPanicRecovery(logger)),
	)
	// mTLS transport creds + SPIFFE allow-list authN/authZ interceptors (empty
	// when the listener is the dev/local insecure opt-in). ChainUnaryInterceptor
	// накапливает, а не замещает, поэтому звено выше остаётся outermost и
	// охватывает SPIFFE-фильтр — фильтр паникует так же, как обработчик.
	opts = append(opts, sec.serverOptions(logger)...)

	srv := grpc.NewServer(opts...)

	// ПОТОЛОК ТЕМПА И ОДНОВРЕМЕННОСТИ на вызывающего — и на этом слушателе тоже.
	//
	// «Внутренний — значит доверенный» здесь запрещено ровно так же, как в
	// вопросе о правах: сюда ходит толкатель iam, а инвалидация кэша решений о
	// доступе — самый дешёвый для вызывающего и самый дорогой для края вызов,
	// какой у этого порта есть. Модуль, ушедший в петлю повторов, обнулял бы кэш
	// края непрерывно, и каждый следующий запрос арендатора снова шёл бы в iam за
	// решением — то есть отказ одного соседа превращался бы в нагрузку на всех.
	//
	// Ключ ведра — личность СЕРТИФИКАТА, а не конечного пользователя: запрос
	// модуля несёт личности разных арендаторов, и ключ по арендатору дробил бы
	// бюджет соседа на тысячу вёдер, задушив его на ровном месте.
	//
	// Ошибка сборки — ОТКАЗ, а не «поднимемся без потолка»: объект, который
	// выглядит ограничителем и не ограничивает, есть ровно тот класс, который мы
	// ловим в чужом коде.
	adm, admErr := grpcsrv.NewAdmission("internal", limits, grpcsrv.CertIdentitySubject)
	if admErr != nil {
		_ = lis.Close()
		return nil, nil, nil, fmt.Errorf("internal grpc listener: request admission: %w", admErr)
	}

	// Регистрация идёт ЧЕРЕЗ обёртку: она получает дескриптор службы целиком и
	// подставляет допуск МЕЖДУ цепочкой звеньев и обработчиком, то есть ПОСЛЕ
	// того, как личность сертификата установлена. Забыть метод здесь не на чем —
	// перечня методов у вызывающего нет.
	handler.RegisterInternalAuthzCacheService(adm.Registrar(srv), externalSrv, inv, logger)

	// gRPC reflection — schema discovery for `grpcurl` and friends during
	// incident response. This is the ONLY listener that serves it: the
	// externally-reachable server does not register it at all
	// (external_grpc_services.go), because there it would answer callers that
	// nothing authenticated.
	//
	// Registered only under the mTLS posture, and that condition is the whole
	// safety argument. With mTLS on, every RPC here — reflection included, via
	// the STREAM interceptor — must present a verified client cert whose SPIFFE
	// SAN is on the caller allow-list. With mTLS off (the dev/local opt-in) the
	// listener mounts NO authN/authZ interceptor (the panic-recovery link above
	// is unconditional, but it decides nothing about access), so registering
	// reflection there would put schema enumeration in front of an
	// unauthenticated port; not registering it is the only refusal available in
	// that posture.
	if sec.mtlsEnabled {
		reflection.Register(srv)
	}

	if logger != nil {
		logger.Info("api-gateway internal gRPC listener ready",
			slog.String("addr", lis.Addr().String()),
			slog.String("services", "kacho.cloud.apigateway.v1.InternalAuthzCacheService"),
			slog.Bool("mtls", sec.mtlsEnabled),
			slog.Bool("reflection", sec.mtlsEnabled),
			slog.String("invariant", "internal-only — never on external TLS"))
	}
	return srv, lis, adm, nil
}
