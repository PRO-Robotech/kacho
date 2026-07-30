// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

func TestIsKnownProtocolName(t *testing.T) {
	known := []string{
		domain.AnyProtocolName, "any", "AnY",
		"tcp", "TCP", "udp", "icmp", "ipv6-icmp", "esp", "ah", "gre", "sctp",
		"igmp", "ipip", "vrrp", "l2tp", "udplite", "ethernet",
	}
	for _, name := range known {
		if !domain.IsKnownProtocolName(name) {
			t.Errorf("%q must be a known protocol name", name)
		}
	}

	// Пустая строка — «протокол не задан», а не имя: обязательность решает
	// вызывающая проверка, поэтому предикат про неё говорит «не знаю».
	unknown := []string{"", "klingon", "tcp/udp", "TCP ", " tcp", "42", "http", "tls"}
	for _, name := range unknown {
		if domain.IsKnownProtocolName(name) {
			t.Errorf("%q must not be a known protocol name", name)
		}
	}
}

// Правила, которые продукт создаёт сам, обязаны лежать внутри контракта, иначе
// default-SG сети нельзя было бы отредактировать через публичный API.
func TestDefaultSecurityGroupRulesAreWithinContract(t *testing.T) {
	for i, r := range domain.NewDefaultSecurityGroupRules() {
		if !domain.IsKnownProtocolName(r.ProtocolName) {
			t.Errorf("default rule %d carries unknown protocol %q", i, r.ProtocolName)
		}
		if r.ProtocolNumber != domain.AnyProtocolNumber &&
			(r.ProtocolNumber < domain.MinProtocolNumber || r.ProtocolNumber > domain.MaxProtocolNumber) {
			t.Errorf("default rule %d carries protocol number %d outside the contract", i, r.ProtocolNumber)
		}
		anyFrom := r.FromPort == domain.AnyPort
		anyTo := r.ToPort == domain.AnyPort
		if anyFrom != anyTo {
			t.Errorf("default rule %d carries a half-any port range %d-%d", i, r.FromPort, r.ToPort)
		}
		if !anyFrom && (r.FromPort < domain.MinPort || r.ToPort > domain.MaxPort || r.FromPort > r.ToPort) {
			t.Errorf("default rule %d carries port range %d-%d outside the contract", i, r.FromPort, r.ToPort)
		}
	}
}
