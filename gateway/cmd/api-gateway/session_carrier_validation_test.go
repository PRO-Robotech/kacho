// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_validation_test.go — страж старта: объявленное множество
// читателей носителя обязано БЫТЬ ИСПОЛНИМЫМ на этой посадке.
//
// Множество разведено с посадкой намеренно, но разведены они не до
// независимости: не всякая пара (посадка, множество) осмысленна, и
// бессмысленная обязана отказывать при СТАРТЕ. Отказ при старте виден
// оператору; читатель, провязанный впустую, виден только по жалобе человека,
// который не смог войти.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

func carrierSet(t *testing.T, declared string) config.SessionCarrierSet {
	t.Helper()
	t.Setenv(config.IdentityProviderKnob, "own")
	t.Setenv(config.SessionCarriersKnob, declared)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set, err := cfg.ResolvedSessionCarriers()
	if err != nil {
		t.Fatalf("ResolvedSessionCarriers(%q): %v", declared, err)
	}
	return set
}

// Три законных состояния проходят стража — положительный контроль, без
// которого «отказ всегда» выглядел бы строгостью.
func TestSessionCarrierGuard_TheThreeLawfulStatesPass(t *testing.T) {
	cases := []struct {
		declared string
		posture  identityposture.Provider
	}{
		{"external", identityposture.External},
		{"own,external", identityposture.Own},
		{"own", identityposture.Own},
	}
	for _, tc := range cases {
		set := carrierSet(t, tc.declared)
		err := validateSessionCarrierConfig(SessionCarrierConfig{
			Posture: tc.posture, Carriers: set, ProviderURL: "http://kratos:80",
		})
		if err != nil {
			t.Fatalf("законное состояние %q на посадке %s отвергнуто: %v", tc.declared, tc.posture, err)
		}
	}
	t.Logf("перепись: законных состояний проверено %d · отвергнуто 0", len(cases))
}

// НАШ носитель под ЧУЖОЙ посадкой — читатель без производителя: печенье нашей
// сессии чеканит наша полоса входа, а её под `external` край не поднимает.
func TestSessionCarrierGuard_OurReaderUnderTheForeignPostureIsRefused(t *testing.T) {
	err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture:     identityposture.External,
		Carriers:    carrierSet(t, "own,external"),
		ProviderURL: "http://kratos:80",
	})
	if err == nil {
		t.Fatal("наш читатель под посадкой external принят: край читал бы печенье, которое на этом " +
			"стенде некому выдать — контроль, выглядящий включённым и не имеющий входа")
	}
	for _, want := range []string{config.SessionCarriersKnob, config.IdentityProviderKnob} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет ручку %s: %v", want, err)
		}
	}
	t.Logf("отказ: %v", err)
}

// ЧУЖОЙ носитель ОБЪЯВЛЕН, а адреса чужой стороны нет — объявление принято и не
// действует. Отказ, а не молчание.
func TestSessionCarrierGuard_ADeclaredProviderReaderWithoutItsAddressIsRefused(t *testing.T) {
	for _, url := range []string{"disabled", "", "   "} {
		err := validateSessionCarrierConfig(SessionCarrierConfig{
			Posture:     identityposture.Own,
			Carriers:    carrierSet(t, "own,external"),
			ProviderURL: url,
		})
		if err == nil {
			t.Fatalf("адрес %q принят при объявленном чужом читателе: профиль назвал переходное "+
				"состояние, и оно молча не наступило — ровно тот вход, ради которого оно заводится", url)
		}
		if !strings.Contains(err.Error(), config.SessionCarriersKnob) {
			t.Fatalf("отказ не называет ручку: %v", err)
		}
	}
	t.Logf("перепись: вырожденных адресов проверено 3 · принято 0")
}

// ВЫВЕДЕННОЕ множество ведёт себя как прежде: посадка `external` с выключенным
// адресом не отказывает, потому что это СЕГОДНЯШНЕЕ поведение края, а не чьё-то
// объявление. Различие «не задано» / «задано» — то же, что у приёма токена.
func TestSessionCarrierGuard_ADerivedSetKeepsTodaysBehaviourOnADisabledAddress(t *testing.T) {
	t.Setenv(config.IdentityProviderKnob, "external")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set, err := cfg.ResolvedSessionCarriers()
	if err != nil {
		t.Fatalf("ResolvedSessionCarriers: %v", err)
	}
	if set.Declared() {
		t.Fatal("необъявленное множество считает себя объявленным — различие, на котором стоит " +
			"весь разбор, стёрто")
	}
	if err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture: identityposture.External, Carriers: set, ProviderURL: "disabled",
	}); err != nil {
		t.Fatalf("выведенное множество с выключенным адресом отвергнуто: %v — существующий профиль "+
			"перестал бы подниматься от одного появления ручки", err)
	}
}
