// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// checks.go — ОБЪЯВЛЕНИЕ состава проверок, которые исполняет проверяющий края.
//
// Перечень обязательных проверок объявлен один раз — в `pkg/tokenpolicy`. Пока
// каждая поверхность держит состав у себя, различие между поверхностями НЕ
// ВЫРАЖЕНО и потому не может покраснеть ни у одной: одна перестанет требовать
// срок, другая тип, и об этом не узнает никто, потому что спрашивать не у чего.
//
// Объявление ниже делает состав предметом, на который смотрит и проба пакета, и
// гейт по дереву (`internal/repohygiene`). Реализация при этом остаётся своя:
// поверхности разные по существу — одна разбирает токен библиотекой, другая
// своим кодом над crypto. Общей делается ПОЛИТИКА, а не функция.
package middleware

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

// implementedChecks — проверки, которые эта реализация исполняет НА КАЖДОМ
// предъявлении, независимо от настройки. Порядок соответствует порядку в
// `JWTVerifier.Verify`: он и есть содержание объявления.
var implementedChecks = []tokenpolicy.Check{
	tokenpolicy.CheckAlgorithmAllowed,
	tokenpolicy.CheckKeyID,
	tokenpolicy.CheckIssuer,
	tokenpolicy.CheckTokenType,
	tokenpolicy.CheckKeyBoundAlgorithm,
	tokenpolicy.CheckSignature,
	tokenpolicy.CheckExpiry,
	tokenpolicy.CheckNotBefore,
	tokenpolicy.CheckAudience,
}

// DeclaredChecks возвращает состав проверок ЭТОГО проверяющего.
//
// Объявление ПРАВДИВО по построению, а не по добросовестности: отзыв попадает в
// него тогда и только тогда, когда хотя бы одна объявленная запись издателя
// объявила его чтение. Приписать проверяющему контроль, которого ни одна его
// запись не исполняет, отсюда нельзя — значит `tokenpolicy.MissingChecks` пусто
// ровно у того проверяющего, который действительно исполняет весь обязательный
// перечень.
//
// Исполнение отзыва при этом лежит на СЛОЕ authN, а не внутри `Verify`: он
// один на обе поверхности края и умеет различать «отозван» и «авторитет не
// ответил» кодами, которые видит клиент. Объявление и исполнение разведены по
// слоям, но связаны одним признаком записи — `ReadRevocation`, — поэтому
// разойтись молча им нечем.
func (v *JWTVerifier) DeclaredChecks() []tokenpolicy.Check {
	out := make([]tokenpolicy.Check, 0, len(implementedChecks)+1)
	out = append(out, implementedChecks...)
	for _, rec := range v.records {
		if rec.readRevocation {
			out = append(out, tokenpolicy.CheckRevocation)
			break
		}
	}
	return out
}
