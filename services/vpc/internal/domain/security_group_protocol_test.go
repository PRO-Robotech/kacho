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
		// Псевдонимы: написания, под которыми те же протоколы знают операторы
		// и /etc/protocols. Отвечать на них отказом значило бы наказывать за
		// орфографию реестра.
		"ospf", "OSPF", "icmpv6", "mobile", "ipencap", "all",
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

// Имя протокола в правилах, которые продукт создаёт сам, обязано быть внутри
// набора — иначе default-SG сети нельзя было бы отредактировать через публичный
// API.
//
// Границы портов здесь НЕ переповторяются: их проверяет
// `securitygroup.TestValidateSGRule_DefaultSecurityGroupRulesPass`, прогоняя те
// же правила через НАСТОЯЩИЙ валидатор. Копия его логики в этом файле умела бы
// разойтись с оригиналом молча.
func TestDefaultSecurityGroupRuleProtocolIsKnown(t *testing.T) {
	for i, r := range domain.NewDefaultSecurityGroupRules() {
		if !domain.IsKnownProtocolName(r.ProtocolName) {
			t.Errorf("default rule %d carries unknown protocol %q", i, r.ProtocolName)
		}
	}
}
