// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// anonymous_admission.go — путь вызывающего без личности до решения о доступе
// (приёмка kaname docs/engineering/acceptance/anonymous-and-authenticated-are-subject-classes.md,
// Р3, Р6, Р7; зеркало края — kacho#3238).
//
// КТО ЧТО РЕШАЕТ. Звеньев два, и оба читают ОДИН предикат строки каталога —
// CatalogEntry.AdmitsAnonymous:
//
//  1. Слой аутентификации пропускает запрос без удостоверения только на метод,
//     строка которого допускает анонима, и только когда ступень пола провязана
//     (её поиск по каталогу и маршрутизатор REST — те же, что у пола). Не
//     провязана — прежний отказ: пропуск без удостоверения не зависит от того,
//     смонтирован ли следующий слой. Именованной личности запрос не получает —
//     метаданных принципала у него нет (SC-30): личность `{system, anonymous}`
//     уже была дефектом, при котором каждый анонимный запрос владел операциями
//     всех прочих.
//  2. Посредник прав видит отметку пропуска в контексте запроса, сверяет строку
//     своего каталога тем же предикатом и спрашивает модель от имени класса
//     `system_class:anonymous`. Отметку ставит только слой аутентификации:
//     ключ контекста — неэкспортируемый тип, из заголовков и метаданных клиента
//     её не произвести.
//
// ОТКАЗ АНОНИМУ — ПРЕЖНИЙ (Р7). Любой не-разрешающий исход решения о классе
// (отказ модели, скрытие существования, неправильный id, неразрешимая область
// потоковой полосы) отдаётся тем же `401` / `UNAUTHENTICATED` и теми же байтами,
// что отказ слоя аутентификации без удостоверения: существующий закрытый объект,
// несуществующий и метод без допуска неотличимы (SC-32). Несостоявшееся решение
// (источник вердикта не ответил) остаётся `UNAVAILABLE`: оно об объекте не
// говорит ничего и повторяемо.

import (
	"context"
	"net/http"
	"strings"
)

// anonymousClassSubject — субъект вопроса к модели о вызывающем без личности.
// Класс `authenticated` и подстановка `user:*` говорящими не бывают (Р3).
const anonymousClassSubject = subjectPrefixSystemClass + ":" + anonymousClassName

const (
	// subjectPrefixSystemClass — вид субъекта «системный класс» в модели прав.
	subjectPrefixSystemClass = "system_class"
	// anonymousClassName — имя класса вызывающего без личности.
	anonymousClassName = "anonymous"
)

// anonymousSubject — разрешённый субъект класса `anonymous`.
func anonymousSubject() ResolvedSubject {
	return ResolvedSubject{
		FGA:    anonymousClassSubject,
		Kind:   SubjectKindSystemClass,
		ID:     anonymousClassName,
		Source: "edge: no credential on a row admitting anonymous",
	}
}

// anonymousAdmissionKey — ключ отметки «слой аутентификации пропустил запрос
// без удостоверения по строке, допускающей анонима».
type anonymousAdmissionKey struct{}

// withAnonymousAdmission ставит отметку пропуска.
func withAnonymousAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, anonymousAdmissionKey{}, true)
}

// anonymousAdmitted — стоит ли отметка пропуска в контексте запроса.
func anonymousAdmitted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(anonymousAdmissionKey{}).(bool)
	return v
}

// admitsAnonymousGRPC — допускает ли строка нативного метода анонима. Ступень
// пола не провязана — нет (fail-closed).
func (a *AuthInterceptor) admitsAnonymousGRPC(fullMethod string) bool {
	if !a.StepUpMounted() {
		return false
	}
	return a.stepUpLookup.Lookup(strings.TrimPrefix(fullMethod, "/")).AdmitsAnonymous
}

// admitsAnonymousHTTP — то же для REST: метод выводится из (глагол, путь) тем
// же маршрутизатором, что у пола. Неразрешимый маршрут анонима не допускает.
func (a *AuthInterceptor) admitsAnonymousHTTP(r *http.Request) bool {
	if !a.StepUpMounted() {
		return false
	}
	fqn, ok := a.stepUpRoutes.Resolve(r.Method, r.URL.Path)
	if !ok || fqn == "" {
		return false
	}
	return a.stepUpLookup.Lookup(fqn).AdmitsAnonymous
}

// anonymousRefusal — исход решения о классе `anonymous`, отдаваемый наружу:
// всякий не-разрешающий исход, кроме несостоявшегося решения, — прежний отказ
// без удостоверения (Р7).
func anonymousRefusal(dec decision) decision {
	switch dec.outcome {
	case outcomeAllow, outcomeError:
		return dec
	default:
		return decision{
			outcome:    outcomeUnauthenticated,
			reasons:    []string{"subject: unauthenticated request"},
			descriptor: dec.descriptor,
			entry:      dec.entry,
		}
	}
}
