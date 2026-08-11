// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pbconv — общие proto-конвертеры, переиспользуемые use-case-хендлерами
// kacho-vpc (маппинг operation→proto, извлечение FGA-subject).
package pbconv

import (
	"context"
	"time"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OperationToProto — конвертирует corelib operations.Operation в его proto-форму.
func OperationToProto(op *operations.Operation) *operationpb.Operation {
	if op == nil {
		return nil
	}
	p := &operationpb.Operation{
		Id:          op.ID,
		Description: op.Description,
		// Усечение до секунды — конвенция продукта для КАЖДОГО proto-ответа:
		// БД хранит микросекунды, клиент их не видит. Operation — ответ каждой
		// мутации, то есть самая частая поверхность утечки долей секунды.
		CreatedAt:            timestamppb.New(op.CreatedAt.Truncate(time.Second)),
		CreatedBy:            op.CreatedBy,
		ModifiedAt:           timestamppb.New(op.ModifiedAt.Truncate(time.Second)),
		Done:                 op.Done,
		Metadata:             op.Metadata,
		PrincipalType:        op.Principal.Type,
		PrincipalId:          op.Principal.ID,
		PrincipalDisplayName: op.Principal.DisplayName,
	}
	if op.Error != nil {
		p.Result = &operationpb.Operation_Error{Error: op.Error}
	} else if op.Response != nil {
		p.Result = &operationpb.Operation_Response{Response: op.Response}
	}
	return p
}

// SubjectFromContext — извлекает FGA-subject ("user:usr_x"/"service_account:sva_x")
// из Principal запроса. Различает ДВА случая:
//   - реальный user/service_account → "<type>:<id>" (идёт в per-page BatchCheck-фильтр);
//   - никого не названо → "" — это «не знаю, кто ты», и потребитель обязан
//     fail-closed (пустой результат), НИКОГДА не passthrough.
//
// «Никого не названо» покрывает и отсутствие принципала в ctx, и ЯРЛЫК АНОНИМНОСТИ
// `system`: этот тип край подставляет запросу БЕЗ удостоверения (`{system,
// anonymous}`), а fallback без auth-интерсептора даёт `{system, bootstrap}`. Тип
// приезжает в заголовке, то есть назначается самим вызывающим, поэтому «тип равен
// system» не может означать «доверенный вызов»: такое прочтение выдавало полное
// доверие и подделавшему заголовок, и вообще неаутентифицированному вызывающему.
// Ни один законный путь платформы этим типом не пользуется — служебные вызовы
// несут `user:system.<сервис>-<роль>` (pkg/auth.SystemPrincipalFor), а межсервисный
// вызов передаёт личность инициатора. Тот же предикат применяют compute и storage
// (internal/authzfilter/subject.go) — vpc был единственным, кто читал его иначе.
func SubjectFromContext(ctx context.Context) string {
	p := operations.PrincipalFromContext(ctx)
	// Зарезервированное слово анонимности отсекается ОТДЕЛЬНО от типа:
	// `{user, anonymous}` — та же «неизвестно кто», но тип объявляет отправитель
	// заголовков, поэтому проверять надо само слово. Предикат общий
	// (operations.Principal.IsAnonymous), чтобы производитель метки и её
	// распознаватели не разъехались; `system` остаётся отдельным, более
	// строгим отказом.
	if p.IsAnonymous() || p.Type == "system" {
		return ""
	}
	return p.Type + ":" + p.ID
}
