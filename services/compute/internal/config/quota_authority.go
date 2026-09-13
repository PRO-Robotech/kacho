// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority.go — объявление домена величин и страж его посадки.
//
// # У ручки осталось ОДНО действующее значение
//
// Авторитет величин снят со службы доступа целиком (задача продукта 2117), и
// своего домена платформа не завела: производителя у контракта нет ни в одном
// дереве. Значит адрес обращаться некуда, и объявивший его получает ОТКАЗ
// СТАРТА, а не молчаливое «как будто отсутствует» — названное значение обязано
// значить названное.
//
// # Здесь больше нет ПАРЫ «адрес и удостоверение»
//
// Пара стерегла живое ребро: удостоверение к нему несло имя, по которому
// сверяется рукопожатие, и объявленное при отсутствующем собеседнике называло
// пира, к которому ребро не идёт. Ребра нет — нет и предмета у пары. Требование
// проверяемого транспорта снято тем же изменением: страж, требующий mTLS на
// проводе, которого композиционный корень не собирает, есть фантомное
// требование, пережившее то, что охраняло (поймано переписью рёбер
// композиционного корня, а не чтением).
//
// # Один предикат на стража и на проводку
//
// Метод зовут оба — страж старта ради вердикта и композиционный корень ради
// объявления. Второго предиката здесь нет намеренно: разошедшиеся страж и
// проводка расходятся именно там, где расхождение опасно.

import (
	corequota "github.com/PRO-Robotech/corelib/quota"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaedge"
)

const quotaAuthorityKnob = "KACHO_COMPUTE_QUOTA_AUTHORITY"

// QuotaAuthorityDeclaration разрешает объявление домена величин.
func (c Config) QuotaAuthorityDeclaration() (corequota.Authority, error) {
	a, err := corequota.ResolveAuthority(corequota.Declaration{
		Knob:  quotaAuthorityKnob,
		Value: c.QuotaAuthority,
	})
	if err != nil {
		return corequota.Authority{}, err
	}
	// Адрес отвергается стартом: собеседника нет. Предикат стоит ПОСЛЕ разбора,
	// а не до него: «оператор не выбрал» судит разборщик и называет это своими
	// словами, и два текста об одном входе давали бы тот отказ, который успел
	// сработать первым.
	if err := quotaedge.ValidateAuthorityHasAProducer(quotaAuthorityKnob, c.QuotaAuthority); err != nil {
		return corequota.Authority{}, err
	}
	return a, nil
}

// ValidateQuotaAuthority — тот же предикат, вызванный ради вердикта.
func (c Config) ValidateQuotaAuthority() error {
	_, err := c.QuotaAuthorityDeclaration()
	return err
}
