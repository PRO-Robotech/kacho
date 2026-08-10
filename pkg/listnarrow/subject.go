// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listnarrow

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Типы принципалов, которые вправе быть субъектом тенантного списочного RPC.
const (
	subjectTypeUser           = "user"
	subjectTypeServiceAccount = "service_account"
)

// ErrUnnamedCaller — запрос не назвал никого. Отдельный, ПЕРВЫЙ исход: это не «отказ
// прав» и не «сосед недоступен», а отсутствие вызывающего.
//
// Код `Unauthenticated`: ответ обязан говорить о ЛИЧНОСТИ, а не о том, что оператор
// прописал в конфигурации, — иначе один и тот же запрос получал бы разный ответ в
// зависимости от посадки.
func ErrUnnamedCaller() error {
	return status.Error(codes.Unauthenticated, "list filter: subject required")
}

// SubjectFromContext — субъект модели прав («user:usr_x» / «service_account:sva_x»)
// вызывающего. Субъект не подставляется и не выводится: он либо назван, либо запроса
// нет.
//
// Пять случаев «никого», и все обязаны отвергаться одинаково:
//
//   - контекст не нёс принципала вовсе (нет удостоверения, нет доверенного
//     отправителя). Брать здесь безусловный извлекатель нельзя: он отдаёт запасное
//     значение уровня начальной загрузки, которому на кластере разрешено всё, — то
//     есть безымянный запрос спрашивал бы права от имени этой учётки;
//   - принципал несёт зарезервированное слово анонимности (край пометил им запрос
//     без удостоверения). Именованная анонимность личностью не является, и
//     проверять надо само слово: тип объявляет отправитель заголовков;
//   - тип принципала не называет тенантного субъекта (служебный, неизвестный);
//   - идентификатор пуст;
//   - идентификатор содержит разделители модели прав. Форматирование в обоих
//     последних случаях СОБЕРЁТ какую-то строку, и вызывающий получил бы субъекта,
//     которым не является: `usr_a#member` стал бы ссылкой на набор, `usr_a:usr_b`
//     сдвинул бы границу «тип:идентификатор».
func SubjectFromContext(ctx context.Context) (string, error) {
	p, ok := operations.PrincipalFromContextOK(ctx)
	if !ok || p.IsAnonymous() {
		return "", ErrUnnamedCaller()
	}
	if p.Type != subjectTypeUser && p.Type != subjectTypeServiceAccount {
		return "", ErrUnnamedCaller()
	}
	subject := authz.FormatSubject(p.Type, p.ID)
	if subject != p.Type+":"+p.ID {
		// Форматирование подменило идентификатор — субъект не назван, а сконструирован.
		return "", ErrUnnamedCaller()
	}
	return subject, nil
}
