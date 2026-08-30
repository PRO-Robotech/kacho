// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pbconv — общие proto-конвертеры, переиспользуемые use-case-хендлерами
// kacho-vpc (маппинг operation→proto, извлечение FGA-subject).
package pbconv

import (
	"context"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
)

// OperationToProto — прослойка к общему слою: перевод строки операции в контракт
// объявлен в дереве ОДИН раз (`pkg/operations/operationspb`, задача #1369).
//
// До сведения объявлений было двенадцать, а смысловых версий — пять; расходились
// они именем помощника усечения времени и охраной пустого значения, то есть там,
// где расхождение не ломает сборку и видно только тому, кто сравнит копии.
func OperationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(op)
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
