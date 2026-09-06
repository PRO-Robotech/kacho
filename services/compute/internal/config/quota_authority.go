// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority.go — объявление домена величин и страж его посадки.
//
// Один предикат на стража и на проводку: [Config.QuotaAuthorityDeclaration]
// зовут оба — страж старта ради вердикта и композиционный корень ради адреса.
// Второго предиката здесь нет намеренно: разошедшиеся страж и проводка
// расходятся именно там, где расхождение опасно.

import (
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

const (
	quotaAuthorityKnob          = "KACHO_COMPUTE_QUOTA_AUTHORITY"
	quotaAuthorityTransportKnob = "KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_ENABLE"
)

// QuotaAuthorityDeclaration разрешает объявление домена величин вместе с
// удостоверением к нему.
func (c Config) QuotaAuthorityDeclaration() (corequota.Authority, error) {
	return corequota.ResolveAuthority(corequota.Declaration{
		Knob:              quotaAuthorityKnob,
		Value:             c.QuotaAuthority,
		TransportKnob:     quotaAuthorityTransportKnob,
		TransportRequired: c.Posture().IsProduction(),
		TransportDeclared: c.QuotaAuthorityMTLS.Enable,
	})
}

// ValidateQuotaAuthority — тот же предикат, вызванный ради вердикта.
func (c Config) ValidateQuotaAuthority() error {
	_, err := c.QuotaAuthorityDeclaration()
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
		Value: c.QuotaAuthority,
	})
	return err == nil && a.Deployed()
}
