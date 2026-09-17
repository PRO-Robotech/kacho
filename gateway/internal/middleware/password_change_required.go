// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// password_change_required.go — требование сменить пароль ДОНОСИТСЯ от полосы
// сессии до решения по каталогу прав (приёмка Ф3, Р8).
//
// # Носитель — контекст запроса, а не заголовок
//
// Уровень уверенности полоса доносит заголовком, потому что у него есть
// читатель ЗА краем (второй замок в службе). У требования сменить пароль
// читатель ровно один и он на крае — решение по каталогу (`AuthzMiddleware.HTTP`).
// Заголовок завёл бы величину на проводе без читателя за краем, которую
// пришлось бы снимать перед пересылкой и защищать от подделки клиентом;
// контекст запроса живёт в процессе и наружу не выходит by construction.
//
// # Где отказ, и где его НЕТ
//
// Отказ стоит в решении по каталогу: на всяком пути С ЗАПИСЬЮ каталога, ДО
// вопроса к модели прав, `PERMISSION_DENIED` / 403 с `reason =
// PASSWORD_CHANGE_REQUIRED`. Множество проходящих — ровно перечень путей без
// записи каталога (`isPublicHTTPPath`: пробы живости, выход полосы токенов,
// «кто я», глаголы формы): консоль узнаёт следующий шаг из «кто я»,
// признак формы вида `password` выдаётся, смена пароля проходит и снимает
// требование, выход проходит, вход судится как всякий вход (Р8, Д1).
package middleware

import "context"

type passwordChangeRequiredKey struct{}

// WithPasswordChangeRequired помечает контекст запроса требованием сменить
// пароль. Зовёт полоса сессии, прочитавшая поле у службы.
func WithPasswordChangeRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, passwordChangeRequiredKey{}, true)
}

// PasswordChangeRequiredFromContext — стоит ли на запросе требование сменить
// пароль. Отсутствие метки означает «требования нет», а не «не спрашивали»:
// метку ставит единственный производитель — полоса, прочитавшая сессию.
func PasswordChangeRequiredFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(passwordChangeRequiredKey{}).(bool)
	return v
}

// Текст и причина отказа по требованию — контракт (Ф3-23, Ф5-23): называют
// следующий шаг, а не состояние чужого ресурса.
const (
	passwordChangeRequiredReason  = "PASSWORD_CHANGE_REQUIRED"
	passwordChangeRequiredMessage = "password change required before any other action"
)
