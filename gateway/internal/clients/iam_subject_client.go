// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — gRPC-direct клиенты для cluster-internal сервисов
// (НЕ через restmux api-gateway — loop-prevention).
//
// iam_subject_client.go: вызов `InternalIAMService.LookupSubject` на
// kaname:9091.
package clients

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/cache"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// subjectLookupStub — узкое подмножество iamv1.InternalIAMServiceClient,
// которое адаптер субъекта действительно зовёт. Зависимость от порта, а не от
// всего порождённого клиента, даёт пробам детерминированного дублёра без сети
// (порт принадлежит потребителю). Настоящий iamv1.NewInternalIAMServiceClient
// ему удовлетворяет.
type subjectLookupStub interface {
	LookupSubject(ctx context.Context, in *iamv1.LookupSubjectRequest, opts ...grpc.CallOption) (*iamv1.LookupSubjectResponse, error)
	Check(ctx context.Context, in *iamv1.CheckRequest, opts ...grpc.CallOption) (*iamv1.CheckResponse, error)
}

type IAMSubjectClient struct {
	conn   *grpc.ClientConn
	stub   subjectLookupStub
	cache  *cache.SubjectCache
	logger *slog.Logger

	// callTimeout — единый срок вызова, от которого считают все соседние
	// методы клиента (LookupSubject и Check). Один источник держит поведение
	// одинаковым при задержке службы доступа — architecture.md: "все sibling-методы клиента обязаны применять один и тот
	// же configured-timeout (не «часть — да, часть — нет»)".
	callTimeout time.Duration
}

// NewIAMSubjectClient — клиент `InternalIAMService` (kaname:9091) над
// соединением, которое передаёт корень: соединение `iamInternal` карты
// бэкендов, то самое, что опрашивает `/readyz` края.
//
// Своего соединения клиент НЕ заводит (kacho#3284). Прежде он набирал службу
// доступа сам, сверх соединения карты; после переката службы оба
// восстанавливались по своим таймерам, и «готов» говорил о соединении карты, а
// вход по токену шёл своим — ещё в отказе, с ответом сразу «connection
// refused». Удостоверение ребра, удержание простоя и учёт причины отказа соседу
// (kacho#3221) несёт соединение карты: их ставит корень (`dialBackends`).
//
// Соединение принадлежит корню и закрывается им; у клиента закрывать нечего.
func NewIAMSubjectClient(conn *grpc.ClientConn, logger *slog.Logger) (*IAMSubjectClient, error) {
	if conn == nil || conn.Target() == "" {
		return nil, fmt.Errorf("iam internal connection absent")
	}
	return &IAMSubjectClient{
		conn:        conn,
		stub:        iamv1.NewInternalIAMServiceClient(conn),
		cache:       cache.NewSubjectCache(10_000, 30*time.Second, nil),
		logger:      logger,
		callTimeout: 5 * time.Second,
	}, nil
}

func (c *IAMSubjectClient) LookupByExternalID(ctx context.Context, externalID string) (middleware.Subject, error) {
	if externalID == "" {
		return middleware.Subject{}, stderrors.New("external_id empty")
	}

	if cached, ok := c.cache.Get(externalID); ok {
		return cached, nil
	}

	timeout, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()

	resp, err := c.stub.LookupSubject(timeout, &iamv1.LookupSubjectRequest{
		Key: &iamv1.LookupSubjectRequest_ExternalId{ExternalId: externalID},
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.NotFound {
			// Метка оборачивается (%w), чтобы errors.Is(err, errSubjectNotFound)
			// узнавал её независимо от текста: вызывающий, отличающий «субъекта
			// нет» от «служба доступа не ответила», не должен зависеть от
			// формулировки.
			return middleware.Subject{}, fmt.Errorf("%w: %s", errSubjectNotFound, externalID)
		}
		c.logger.Warn("iam.LookupSubject failed",
			"external_id", externalID, "code", st.Code(), "msg", st.Message())
		return middleware.Subject{}, fmt.Errorf("iam lookup failed: %w", err)
	}

	var subj middleware.Subject
	switch s := resp.Subject.(type) {
	case *iamv1.LookupSubjectResponse_User:
		subj = middleware.Subject{
			Type:        "user",
			ID:          s.User.GetId(),
			DisplayName: pickDisplayName(s.User.GetDisplayName(), s.User.GetEmail()),
		}
	case *iamv1.LookupSubjectResponse_ServiceAccount:
		subj = middleware.Subject{
			Type:        "service_account",
			ID:          s.ServiceAccount.GetId(),
			DisplayName: s.ServiceAccount.GetName(),
		}
	default:
		return middleware.Subject{}, fmt.Errorf("unexpected subject oneof from iam")
	}
	c.cache.Set(externalID, subj)
	return subj, nil
}

// errSubjectNotFound — sentinel, отличает «не найден» от других ошибок
// (network, panic, и т.п.). LookupByExternalID оборачивает его через %w,
// поэтому классификация — чистый errors.Is (без text-matching).
var errSubjectNotFound = stderrors.New("subject not found")

// IsSystemAdmin — проверка system-admin tuple через
// InternalIAMService.Check(kacho_system:root#admin). Subject = "user:<id>" |
// "service_account:<id>". Возвращает (allowed, error).
func (c *IAMSubjectClient) IsSystemAdmin(ctx context.Context, subject string) (bool, error) {
	if subject == "" {
		return false, nil
	}
	timeout, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()
	resp, err := c.stub.Check(timeout, &iamv1.CheckRequest{
		SubjectId: subject,
		Relation:  "admin",
		Object:    "kacho_system:root",
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.Unimplemented || st.Code() == codes.PermissionDenied {
			return false, nil
		}
		return false, err
	}
	return resp.GetAllowed(), nil
}

func (c *IAMSubjectClient) InvalidateAll() { c.cache.InvalidateAll() }

// BasicCredentialStub — тот же клиент внутреннего слушателя, что уже держит эта
// связь, поданный как узкий порт авторитета о предъявленном базовом секрете
// (#1142). Второй связи под это ребро НЕ заводится: адрес, якорь доверия и
// удостоверение края у него ТЕ ЖЕ — «отзыв действует не позже N» обязано
// означать одно и то же на всех полосах, а вторая связь со своими величинами
// это разошла бы молча.
func (c *IAMSubjectClient) BasicCredentialStub() iamv1.InternalIAMServiceClient {
	return iamv1.NewInternalIAMServiceClient(c.conn)
}

func pickDisplayName(displayName, email string) string {
	if displayName != "" {
		return displayName
	}
	return email
}
