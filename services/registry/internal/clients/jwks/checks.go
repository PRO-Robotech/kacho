// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// checks.go — ОБЪЯВЛЕНИЕ состава проверок, которые исполняет этот проверяющий.
//
// Перечень обязательных проверок объявлен один раз — в `pkg/tokenpolicy`. Пока
// каждая поверхность держит состав у себя, различие между поверхностями НЕ
// ВЫРАЖЕНО и потому не может покраснеть: одна перестанет требовать срок, другая
// тип, и об этом не узнает никто. Объявление ниже делает состав предметом, на
// который смотрит и проба, и гейт по дереву.
package jwks

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

// implementedChecks — проверки, которые эта реализация исполняет НА КАЖДОМ
// предъявлении, независимо от настройки. Порядок соответствует порядку в
// `Verify`: он и есть содержание объявления.
//
// Отзыв сюда НЕ входит намеренно: он исполняется по записи источника, которая
// его объявила, и приписывать его проверяющему, у которого такой записи нет,
// значило бы объявить контроль, не отказавший ни разу за свою жизнь.
var implementedChecks = []tokenpolicy.Check{
	tokenpolicy.CheckAlgorithmAllowed,
	tokenpolicy.CheckKeyID,
	tokenpolicy.CheckIssuer,
	tokenpolicy.CheckTokenType,
	tokenpolicy.CheckKeyBoundAlgorithm,
	tokenpolicy.CheckSignature,
	tokenpolicy.CheckExpiry,
	tokenpolicy.CheckNotBefore,
	tokenpolicy.CheckCriticalHeaders,
	tokenpolicy.CheckAudience,
}

// DeclaredChecks возвращает состав проверок ЭТОГО проверяющего.
//
// Объявление ПРАВДИВО по построению: отзыв попадает в него тогда и только тогда,
// когда хотя бы одна запись источника объявила чтение отзыва (а `New` не
// позволяет объявить его без читателя). Поэтому
// `tokenpolicy.MissingChecks(v.DeclaredChecks())` пусто ровно у того
// проверяющего, который действительно исполняет весь обязательный перечень, — и
// непусто у того, у кого отзыв не провязан.
func (v *Verifier) DeclaredChecks() []tokenpolicy.Check {
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
