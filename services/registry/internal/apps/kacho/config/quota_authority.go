// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority.go — объявление домена величин и страж его посадки.
//
// Один предикат на стража и на проводку: [Config.QuotaAuthorityDeclaration]
// зовут оба — страж старта ради вердикта и композиционный корень ради адреса.

import (
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

const (
	quotaAuthorityKnob          = "KACHO_REGISTRY_QUOTA_AUTHORITY"
	quotaAuthorityTransportKnob = "KACHO_REGISTRY_QUOTA_AUTHORITY_MTLS_ENABLE"
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
