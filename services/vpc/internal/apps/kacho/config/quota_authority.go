// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority.go — объявление домена величин и страж его посадки.
//
// # Один предикат на стража и на проводку
//
// [Config.QuotaAuthority] зовут ОБА: страж старта (ради вердикта) и
// композиционный корень (ради адреса). Второго предиката здесь нет намеренно —
// разошедшиеся страж и проводка расходятся именно там, где расхождение опасно:
// страж доволен, а дозвон уходит не туда (`security.md` §AuthN+AuthZ п.5, о том
// же классе на круге отправителей).
//
// # Почему страж безусловен, а не только в боевом режиме
//
// Незаданное объявление означает, что оператор не выбрал между «потолки
// действуют» и «потолков нет». Это не свойство посадки, а отсутствие решения, и
// подставить за него разумное умолчание нельзя ни в каком режиме.

import (
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

const (
	// quotaAuthorityKnob / quotaAuthorityTransportKnob — имена ручек, какими их
	// видит оператор. Попадают в текст отказа старта: без имени ручки стенд не
	// поднять, и это одно из трёх мест, прямо выведенных из-под запрета
	// `security.md` §«Публичные артефакты».
	quotaAuthorityKnob          = "quota.authority (KACHO_VPC_QUOTA__AUTHORITY)"
	quotaAuthorityTransportKnob = "KACHO_VPC_QUOTA_AUTHORITY_MTLS_ENABLE"
)

// QuotaAuthority разрешает объявление домена величин вместе с удостоверением к
// нему.
//
// Удостоверением считается client-cert ребра либо server-TLS — ровно те две
// формы, что признаёт транспортный страж остальных рёбер этой службы. Третьей
// формы не заводится: она означала бы, что у этого ребра свои правила
// транспорта, и различие пришлось бы помнить.
func (c Config) QuotaAuthority(m MTLSConfig) (corequota.Authority, error) {
	return corequota.ResolveAuthority(corequota.Declaration{
		Knob:              quotaAuthorityKnob,
		Value:             c.Quota.Authority,
		TransportKnob:     quotaAuthorityTransportKnob,
		TransportRequired: c.AuthN.Mode.IsProduction(),
		TransportDeclared: m.QuotaAuthorityMTLS.Enable,
	})
}

// ValidateQuotaAuthority — тот же предикат, вызванный ради вердикта.
func (c Config) ValidateQuotaAuthority(m MTLSConfig) error {
	_, err := c.QuotaAuthority(m)
	return err
}

// QuotaAuthorityEdgeLive — дилится ли ребро величин по этому объявлению.
//
// Читает ТО ЖЕ объявление, но БЕЗ половины про удостоверение, и это не второй
// предикат об одном предмете: вопрос здесь другой. «Полна ли пара» — вопрос
// стража посадки; «дилится ли ребро» — вопрос того, кто требует от живых рёбер
// проверяемого транспорта. Спроси он полную пару — молчал бы ровно на той
// посадке, ради которой заведён: адрес объявлен, удостоверения нет, разбор
// отказывает, и страж пропускает открытый канал.
func (c Config) QuotaAuthorityEdgeLive() bool {
	a, err := corequota.ResolveAuthority(corequota.Declaration{
		Knob:  quotaAuthorityKnob,
		Value: c.Quota.Authority,
	})
	return err == nil && a.Deployed()
}
