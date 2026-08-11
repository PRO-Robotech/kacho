// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// CheckClient — порт решателя доступа для handler-side пообъектных проверок.
// Структурный (не импорт клиентского пакета): use-case-слой не обязан знать, чем
// решение доставляется. Совпадает по методу с `internal/clients/iam`.CheckClient.
type CheckClient interface {
	Check(ctx context.Context, subjectID, relation, object string) (bool, error)
}

// authzCheckUnavailableMsg — единственный текст, которым эта полоса сообщает
// «решение не получено». Один и тот же и когда владелец модели не ответил, и
// когда спрашивать было некому: снаружи это одно и то же состояние, а
// различимый текст здесь был бы отчётом о нашей посадке.
const authzCheckUnavailableMsg = "authorization check unavailable"

// AuthorizeObject — ЕДИНСТВЕННОЕ правило пообъектного решения о доступе в
// kacho-nlb: решение принимает модель прав, а всё, что помешало её спросить, —
// отказ.
//
// Полосы (все fail-closed, четвёртой нет):
//
//   - решателя нет (`c == nil`) → `Unavailable`. Отсутствие звена решения — не
//     разрешение. Это состояние настройки, а не сбой: в production-режиме оно
//     отвергается ещё при старте (`internal/apps/kacho/config`.Validate требует
//     адрес внутреннего листенера iam, из которого строится этот клиент), поэтому
//     на посадке, где сервис вообще поднялся, ветка недостижима — и остаётся
//     здесь как отказ, а не как пропуск, чтобы её недостижимость держалась
//     стражей старта, а не чьей-то памятью.
//   - вызывающего нельзя назвать субъектом (`domain.FGASubjectFromPrincipal`
//     вернул пусто: пустой тип/идентификатор, тип `system`, зарезервированное
//     слово анонимности) → `PermissionDenied`, БЕЗ обращения к модели. Спросить
//     про выдуманный субъект значило бы решить доступ по чужим правам; та же
//     посадка, что у iam (`authzguard.PrincipalSubject` → `("", false)`).
//   - модель ответила отказом либо не смогла ответить → код по её сигналу.
//
// `deniedMsg` — готовый текст отказа вызывающему; он ОДИН на все отрицательные
// исходы полосы, поэтому по нему нельзя отличить «нет права» от «мы не смогли
// тебя назвать» (иначе это оракул о нашей посадке).
//
// Прежняя редакция этих полос отвечала «разрешено» на первые две: обе ветки
// возвращали nil. Обоснование, стоявшее в комментариях всех четырёх мест —
// «звено решения обходит system-субъект по той же причине», — деревом не
// подтверждается: опция сквозного пропуска system-субъекта
// (`authz.InterceptorOptions.AllowSystemPrincipal`) не выставлена ни в одном
// сервисе, то есть обходить было нечего.
func AuthorizeObject(ctx context.Context, c CheckClient, relation, object, deniedMsg string) error {
	if c == nil {
		return status.Error(codes.Unavailable, authzCheckUnavailableMsg)
	}
	p := operations.PrincipalFromContext(ctx)
	subject := domain.FGASubjectFromPrincipal(p.Type, p.ID)
	if subject == "" {
		return status.Error(codes.PermissionDenied, deniedMsg)
	}
	allowed, err := c.Check(ctx, subject, relation, object)
	if err != nil {
		return authzCheckErr(err, deniedMsg)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, deniedMsg)
	}
	return nil
}

// authzCheckErr — сигнал решателя → gRPC-status (fail-closed): нет пути → отказ;
// владелец модели недоступен → Unavailable; плохие аргументы → InvalidArgument;
// прочее → Internal фиксированным текстом (без leak'а).
func authzCheckErr(err error, deniedMsg string) error {
	switch {
	case errors.Is(err, authz.ErrNoPath):
		return status.Error(codes.PermissionDenied, deniedMsg)
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, authzCheckUnavailableMsg)
	case errors.Is(err, domain.ErrInvalidArg):
		return status.Errorf(codes.InvalidArgument, "authorization check: %v", err)
	}
	return status.Error(codes.Internal, "authorization check failed")
}
