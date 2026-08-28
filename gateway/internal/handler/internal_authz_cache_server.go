// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_authz_cache_server.go — gRPC handler that drops api-gateway's
// per-subject authz decision-cache entries on revoke events pushed from
// kacho-iam's subject_change_outbox drainer.
//
// Registered ONLY on api-gateway's internal mTLS listener (port 9091) — see
// RegisterInternalAuthzCacheService below. NEVER exposed on the external
// TLS REST mux: Internal.* methods do not appear on the external endpoint.
package handler

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	apigatewayv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"
)

// Invalidator — minimal port the InternalAuthzCacheServer depends on.
// Implemented by AuthzMiddleware.AsInvalidator() (see
// internal/middleware/authz.go). Lives here so the handler is unit-testable
// without importing middleware (also avoids circular dependency that would
// arise if Invalidator lived in middleware/ and handler/ imported middleware/).
type Invalidator interface {
	// InvalidateSubject drops cache entries scoped to the given subject
	// (FGA-prefixed, e.g. "user:usr_abc"). Returns count of entries dropped.
	InvalidateSubject(subject string) int

	// Invalidate flushes the whole decision cache (safety-net fallback).
	// Not invoked by InternalAuthzCacheService.InvalidateSubject path but
	// part of the contract for the broader caller (the cache watcher etc.).
	Invalidate()
}

// SubjectStreamCloser — закрывает ОТКРЫТЫЕ потоки субъекта.
//
// Второй порт, а не расширение [Invalidator], и это не вкус. У них разные
// предметы и разная полярность отказа: кэш решений хранит ответ, который просто
// перестаёт быть верным, а поток — открытое соединение, по которому арендатор
// продолжает получать данные. Слей их — и провязка одного молча включала бы
// другой либо, что хуже, отсутствие одного выключало бы второй.
//
// Реализуется проекцией потока (`gateway/internal/subscriptionstream`.Handler).
// Порт объявлен ЗДЕСЬ, у вызывающего, чтобы обработчик оставался проверяемым без
// импорта проекции.
type SubjectStreamCloser interface {
	// CloseSubject закрывает потоки названного субъекта и возвращает их число.
	CloseSubject(subject string) int
}

// InternalAuthzCacheServer implements
// apigatewayv1.InternalAuthzCacheServiceServer.
type InternalAuthzCacheServer struct {
	apigatewayv1.UnimplementedInternalAuthzCacheServiceServer
	inv     Invalidator
	streams SubjectStreamCloser
	logger  *slog.Logger
}

// NewInternalAuthzCacheServer constructs the handler. logger may be nil
// (silent operation — used in unit tests).
func NewInternalAuthzCacheServer(inv Invalidator, logger *slog.Logger) *InternalAuthzCacheServer {
	return &InternalAuthzCacheServer{inv: inv, logger: logger}
}

// WithSubjectStreamCloser провязывает закрытие открытых потоков субъекта
// (kacho#1022) и возвращает тот же обработчик.
//
// # Почему отзыв обязан доезжать сюда, а не только до кэша
//
// Кэш решений покрывает ЗАПРОСЫ: следующий запрос отозванного получит отказ.
// Длинное соединение следующего запроса не делает — проверка на нём случилась
// один раз, при открытии. Это класс «контроль, действующий на выдаче, но не на
// предъявлении»: сброс кэша выглядит отзывом и потока не касается вовсе, а само
// состояние не сходится — сходиться нечему.
//
// # Почему закрывается на ЛЮБОМ изменении субъекта, а не только на снятии права
//
// Вид события (`event_type`) контракт объявляет диагностическим и прямо говорит,
// что на поведение сброса он не влияет. Строить на нём решение о доступе значило
// бы завести второе, противоречащее первому чтение одного поля. Цена ошибки
// несимметрична: закрытый на выдаче права поток стоит одного переподключения,
// переживший снятие права — того, ради чего задача заведена.
//
// Ноль означает «не провязан»: обработчик тогда делает ровно то, что делал
// прежде. Молчаливого включения не бывает — провязку видно в самоотчёте старта.
func (s *InternalAuthzCacheServer) WithSubjectStreamCloser(c SubjectStreamCloser) *InternalAuthzCacheServer {
	s.streams = c
	return s
}

// InvalidateSubject — see apigatewayv1.InternalAuthzCacheServiceServer.
//
// Contract:
//   - empty Subject                        → codes.InvalidArgument
//   - 0 entries dropped AND 0 streams closed → codes.NotFound (idempotent;
//     drainer maps to drainer.ErrAlreadyApplied and marks sent_at)
//   - anything actually done                 → OK + Empty{}
//
// Закрытые потоки входят в «сделано» наравне со сброшенными записями: субъект
// без записей кэша, но с открытым потоком получил бы `NotFound` — ответ
// «делать было нечего» на вызов, который закрыл соединение.
//
// ResourceType / ResourceID are ignored — per-subject invalidate is the
// safe upper bound. EventType — diagnostic only (logged).
func (s *InternalAuthzCacheServer) InvalidateSubject(
	_ context.Context, req *apigatewayv1.InvalidateSubjectRequest,
) (*emptypb.Empty, error) {
	if req.GetSubject() == "" {
		return nil, status.Error(codes.InvalidArgument, "subject required")
	}
	dropped := s.inv.InvalidateSubject(req.GetSubject())
	closed := 0
	if s.streams != nil {
		closed = s.streams.CloseSubject(req.GetSubject())
	}
	if s.logger != nil {
		s.logger.Info("authz cache invalidate (per-subject)",
			slog.String("subject", req.GetSubject()),
			slog.String("event_type", req.GetEventType()),
			slog.String("resource_type", req.GetResourceType()),
			slog.String("resource_id", req.GetResourceId()),
			slog.Int("dropped", dropped),
			// Закрытые потоки считаются ОТДЕЛЬНО от сброшенных записей: слей их
			// — и «отзыв доехал до потоков» стало бы неотличимо от «сбросили
			// кэш», то есть от состояния до этой задачи.
			slog.Int("streams_closed", closed),
		)
	}
	if dropped+closed == 0 {
		// Idempotent miss — gateway has no cache entries for this subject.
		// Drainer (kacho-iam side) maps NotFound → drainer.ErrAlreadyApplied
		// and marks sent_at; row is not retried.
		return nil, status.Error(codes.NotFound, "no cache entries for subject")
	}
	return &emptypb.Empty{}, nil
}

// RegisterInternalAuthzCacheService registers the service ONLY on the
// internal mTLS gRPC server (port 9091) — NEVER on the external TLS-facing
// server (Internal.* methods do not appear on the external endpoint).
//
// Both internalSrv and externalSrv arguments are required so the test suite
// can assert that the FQN appears on internal and is absent from external in
// one call. The externalSrv is intentionally not used to register — that is
// the invariant. Both args are panic-guarded against nil to catch arg-swap
// programmer bugs (the most likely way to accidentally expose this on the
// external endpoint).
//
// internalSrv is a grpc.ServiceRegistrar rather than a *grpc.Server so the
// composition root can hand in the server WRAPPED by the per-caller rate ceiling
// (grpcsrv.Admission.Registrar). The wrapper rewrites every method of the
// descriptor, which is why the ceiling has to be applied where registration
// happens and not afterwards. externalSrv stays a *grpc.Server: its role here is
// not to receive a registration but to make the internal-only invariant a
// checkable argument.
func RegisterInternalAuthzCacheService(
	internalSrv grpc.ServiceRegistrar, externalSrv *grpc.Server, inv Invalidator,
	streams SubjectStreamCloser, logger *slog.Logger,
) {
	if internalSrv == nil {
		panic("RegisterInternalAuthzCacheService: internalSrv is nil (programmer error)")
	}
	if externalSrv == nil {
		panic("RegisterInternalAuthzCacheService: externalSrv is nil (programmer error — pass both servers to make the internal-only invariant explicit)")
	}
	srv := NewInternalAuthzCacheServer(inv, logger).WithSubjectStreamCloser(streams)
	apigatewayv1.RegisterInternalAuthzCacheServiceServer(internalSrv, srv)
	// externalSrv intentionally NOT registered — see comment above.
}
