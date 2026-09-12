// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority.go — объявление домена величин и страж его посадки.
//
// Один предикат на стража и на проводку: [Config.QuotaAuthorityDeclaration]
// зовут оба — страж старта ради вердикта и композиционный корень ради адреса.

import (
	corequota "github.com/PRO-Robotech/corelib/quota"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaedge"
)

const (
	quotaAuthorityKnob           = "quota.authority (KACHO_NLB_QUOTA__AUTHORITY)"
	quotaAuthorityTransportKnob  = "mtls.quota-authority.enable (KACHO_NLB_MTLS__QUOTA_AUTHORITY__ENABLE)"
	quotaAuthorityServerNameKnob = "mtls.quota-authority.servername (KACHO_NLB_MTLS__QUOTA_AUTHORITY__SERVERNAME)"
)

// QuotaAuthorityDeclaration разрешает объявление домена величин вместе с
// удостоверением к нему.
func (c Config) QuotaAuthorityDeclaration() (corequota.Authority, error) {
	a, err := corequota.ResolveAuthority(corequota.Declaration{
		Knob:              quotaAuthorityKnob,
		Value:             c.Quota.Authority,
		TransportKnob:     quotaAuthorityTransportKnob,
		TransportRequired: c.Mode().IsProduction(),
		TransportDeclared: c.MTLS.QuotaAuthority.Enable,
	})
	if err != nil {
		return corequota.Authority{}, err
	}
	// Вторая половина пары: адрес объявил ОТСУТСТВИЕ домена, а удостоверение к
	// нему объявлено — имя для сверки называет пира, к которому ребро не идёт.
	if err := quotaedge.ValidateAbsentAuthorityCarriesNoTransport(quotaedge.Pair{
		AuthorityKnob:  quotaAuthorityKnob,
		Authority:      c.Quota.Authority,
		TransportKnob:  quotaAuthorityTransportKnob,
		ServerNameKnob: quotaAuthorityServerNameKnob,
		Transport:      c.MTLS.QuotaAuthority,
	}); err != nil {
		return corequota.Authority{}, err
	}
	return a, nil
}

// ValidateQuotaAuthority — тот же предикат, вызванный ради вердикта.
func (c Config) ValidateQuotaAuthority() error {
	_, err := c.QuotaAuthorityDeclaration()
	return err
}
