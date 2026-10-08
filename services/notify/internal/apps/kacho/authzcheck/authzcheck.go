// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzcheck — право вызывающего на область запроса в use-case
// методов, освобождённых у края (приёмка NTF-5 Р16, Р21; замысел issue-2924
// З16 п.2): одна функция [RequireScope] на все восемь методов, второй ручной
// проверки права в notify нет.
//
// Порядок, который держит вызывающий: обязательность и форма области — до
// права (неверная форма не становится объектом вопроса о правах), право — до
// выборки. Отказ права — PERMISSION_DENIED; служба доступа не ответила
// вердиктом — UNAVAILABLE с причиной PEER_UNAVAILABLE: вердикт из ошибки не
// выводится, и пустой страницы вместо отказа нет.
package authzcheck

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/authz"
	coreerrors "github.com/PRO-Robotech/corelib/errors"
)

// service — имя службы в домене деталей отказа (`notify.kacho.cloud`).
const service = "notify"

// Scope — область запроса: объект вопроса о праве (`<Type>:<ID>`).
type Scope struct {
	// Type — тип объекта модели прав: "project" либо "account".
	Type string
	ID   string
}

// Object — написание объекта вопроса.
func (s Scope) Object() string { return s.Type + ":" + s.ID }

// Subject — субъект вопроса о праве: пользователь либо учётка, переслана
// trust-aware парой носителя. Безымянный вызывающий — UNAUTHENTICATED;
// служба под своим именем на публичное чтение права не имеет — PERMISSION_DENIED.
func Subject(ctx context.Context) (string, error) {
	c, ok := authz.CallerSubject(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "caller principal required")
	}
	p, forwarded := c.Forwarded()
	if !forwarded {
		return "", status.Error(codes.PermissionDenied, "permission denied")
	}
	subject, named := authz.TenantSubject(p.Type, p.ID)
	if !named {
		return "", status.Error(codes.PermissionDenied, "permission denied")
	}
	return subject, nil
}

// RequireScope — вопрос `Check(<субъект>, relation, <область>)` владельцу
// модели прав. Ровно один вопрос на вызов.
func RequireScope(ctx context.Context, checker authz.CheckClient, relation string, scope Scope) error {
	subject, err := Subject(ctx)
	if err != nil {
		return err
	}
	allowed, err := checker.Check(ctx, subject, relation, scope.Object())
	if err != nil {
		return PeerUnavailable()
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

// PeerUnavailable — служба доступа не ответила вердиктом (недоступна, срок,
// отказ вызова). Один конструктор на вопрос о праве и на сужение ссылок.
func PeerUnavailable() error {
	return coreerrors.ReasonPeerUnavailable.Errf(coreerrors.PeerRef{Service: service},
		"access service did not answer")
}

// WithBudget — порт проверки со сроком одного вопроса (`arch-per-call-deadline`):
// вызов владельца модели без срока держал бы обработчик столько, сколько
// молчит сосед.
func WithBudget(c authz.CheckClient, budget time.Duration) authz.CheckClient {
	return authz.CheckClientFunc(func(ctx context.Context, subject, relation, object string) (bool, error) {
		ctx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		return c.Check(ctx, subject, relation, object)
	})
}
