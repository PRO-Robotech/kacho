// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — authn_interceptor.go: gRPC unary/stream интерсептор, который
// требует наличия аутентифицированного принципала и больше ничего не решает.
//
// Принципал кладёт в ctx `grpcsrv.Unary/StreamPrincipalExtract` выше по цепочке
// (`x-kacho-principal-*`, форвардит api-gateway / вызывающий сервис через
// `pkg/auth.PropagateOutgoing`). Отсюда его читают потребители, которым нужна
// личность: `pbconv.SubjectFromContext` для per-object видимости и per-RPC
// authz-интерсептор для Check'а.
//
// **Авторизация здесь не живёт — ни в каком виде.** Решение о доступе принимает
// модель прав (`internal/check.PermissionMap` → `InternalIAMService.Check`),
// одинаково на обоих листенерах, fail-closed. Этот интерсептор отвечает ровно на один
// вопрос: «предъявлен ли принципал» — и в production-mode отвергает вызов, если нет.
//
// # Что отсюда снято и почему
//
// Раньше здесь жили две самодельные конструкции, обе — решения о доступе в Go:
//
//  1. **admin-гейт internal-листенера.** Он поднимал «cluster-admin» из клиентского
//     plaintext-заголовка `x-kacho-admin` и по нему пропускал cluster-scoped
//     admin-RPC. Заголовок предъявлял о себе сам звонящий, поэтому это была не
//     проверка, а опция, которую атакующий включает себе сам: любой peer,
//     дотянувшийся до :9091, объявлял себя администратором строкой в metadata.
//     Заодно гейт РЕШАЛ ЗА МОДЕЛЬ в обратную сторону — вызывающего, которому модель
//     говорит «да», он отвергал, если тот прислал `x-kacho-project-id` без
//     admin-заголовка. Привилегия выдаётся моделью и отзывается моделью; объявить её
//     о себе нельзя.
//
//  2. **tenant-заголовки как личность** (`x-kacho-project-id`, `x-kacho-admin`).
//     Их не форвардит ни один компонент платформы — cross-service вызовы несут
//     только `x-kacho-principal-*`. Читать их значило принимать за identity то, что
//     звонящий о себе написал: неаутентифицированный вызывающий проходил
//     production-mode AuthN-guard, просто добавив `x-kacho-project-id: что угодно`.
//     Ни один потребитель эти поля не читал — авторизация и так шла через модель.
//
// Снятие обоих не расширило доступ: единственная форма вызова, исход которой зависел
// от гейта, — это как раз вызывающий с сочинёнными tenant-заголовками, и вместе с
// чтением заголовков она перестала существовать. Матрица форм × RPC зафиксирована в
// internal_listener_authorization_test.go: единственный источник «да» — модель.
package handler

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// errAuthNRequired — production-mode fail-closed: вызов без предъявленного
// принципала не обслуживается (защита от деплоя без identity-форварда).
const errAuthNRequired = "AuthN required (production mode): forward an authenticated principal"

// principalForwarded сообщает, несёт ли ctx реальный forwarded end-user principal
// (`x-kacho-principal-*`, положенный в ctx `grpcsrv.UnaryPrincipalExtract` выше по
// цепочке). Зеркалит kaname `authzguard.IsAnonymous`.
//
// Анонимность не является личностью: `{system, anonymous}` (его подставляет
// api-gateway) и `SystemPrincipal()`-fallback `{system, bootstrap}` (principal-headers
// не форвардились вовсе) — это «личность не предъявлена», а не «предъявлена
// привилегированная».
//
// Признак — ТИП, а не перечень идентификаторов. Раньше отсекались два известных
// значения id, поэтому любое третье при том же типе проходило как «личность
// предъявлена» — а тип целиком назначает себе сам вызывающий. Тип `system` на
// платформе и есть ярлык анонимности, поэтому им не может называться никто: ни
// один законный путь его не шлёт (служебные вызовы несут
// `user:system.<сервис>-<роль>`, см. pkg/auth.SystemPrincipalFor, межсервисный
// вызов передаёт личность инициатора). Тот же предикат применяет
// shared/pbconv.SubjectFromContext — иначе страж и извлечение субъекта отвечали
// бы на один вопрос по-разному.
func principalForwarded(ctx context.Context) bool {
	p := operations.PrincipalFromContext(ctx)
	if p.ID == "" || p.Type == "" {
		return false
	}
	if p.Type == "anonymous" || p.ID == "anonymous" {
		return false
	}
	if p.Type == "system" {
		return false
	}
	return true
}

// AuthNUnaryInterceptor — gRPC unary интерсептор. productionMode=true → вызов без
// предъявленного принципала отвергается (`KACHO_VPC_AUTH_MODE=production`).
// Авторизацию принимает модель прав ниже по цепочке, одинаково на обоих листенерах.
func AuthNUnaryInterceptor(productionMode bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if productionMode && !principalForwarded(ctx) {
			return nil, status.Error(codes.PermissionDenied, errAuthNRequired)
		}
		return handler(ctx, req)
	}
}

// AuthNStreamInterceptor — то же для server-stream RPC.
func AuthNStreamInterceptor(productionMode bool) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if productionMode && !principalForwarded(ss.Context()) {
			return status.Error(codes.PermissionDenied, errAuthNRequired)
		}
		return handler(srv, ss)
	}
}
