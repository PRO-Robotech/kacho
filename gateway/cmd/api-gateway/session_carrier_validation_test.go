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

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛНОЕ ПРОИЗВЕДЕНИЕ «посадка × множество», а не перечень удобных пар.
//
// Прежняя редакция называла ТРИ пары и объявляла их законными. Пар при двух
// посадках и трёх состояниях носителя ШЕСТЬ, и та, что осталась вне перечня,
// осталась и вне суда: посадка `own` с множеством БЕЗ нашего читателя. На ней
// край поднимает нашу полосу формы входа, наша служба чеканит `kaname_session`,
// а читателя этого печенья в процессе нет — человек проходит форму и остаётся
// анонимом. Перечень удобных пар не способен такое увидеть: он утверждает о
// том, что в нём есть.
//
// Произведение поэтому ВЫВОДИТСЯ: посадки берутся из словаря
// (`identityposture.Values`), состояния носителя — из трёх объявлений профиля.
// Третье значение посадки, если оно когда-нибудь появится, сделает пробу
// красной, а не молчаливой.

// carrierStateDeclarations — три состояния, которые профиль умеет объявить.
var carrierStateDeclarations = []string{"external", "own,external", "own"}

// lawfulCarrierPairs — пары, на которых провязка состоится и будет полной.
// Ключ — «<посадка>/<объявление>».
var lawfulCarrierPairs = map[string]bool{
	"own/own":           true,
	"own/own,external":  true,
	"external/external": true,
}

func TestSessionCarrierGuard_EveryPostureCarrierPairIsJudged(t *testing.T) {
	postures := identityposture.Values()
	if len(postures) == 0 {
		t.Fatal("словарь посадок пуст — произведение строить не из чего")
	}
	pairs, lawful, refused := 0, 0, 0
	for _, posture := range postures {
		for _, declared := range carrierStateDeclarations {
			pairs++
			key := posture.String() + "/" + declared
			err := validateSessionCarrierConfig(SessionCarrierConfig{
				Posture: posture, Carriers: carrierSet(t, declared), ProviderURL: "http://kratos:80",
			})
			switch {
			case lawfulCarrierPairs[key] && err != nil:
				t.Errorf("законная пара %s отвергнута: %v", key, err)
			case !lawfulCarrierPairs[key] && err == nil:
				t.Errorf("пара %s принята, а провязка на ней НЕПОЛНА: край заводит не всех "+
					"читателей, чей носитель на этом стенде кто-то чеканит, и человек, прошедший "+
					"вход, остаётся анонимом", key)
			}
			if lawfulCarrierPairs[key] {
				lawful++
			} else {
				refused++
			}
		}
	}
	t.Logf("перепись: посадок в словаре %d · состояний носителя %d · пар осмотрено %d · "+
		"законных %d · обязанных отказать %d", len(postures), len(carrierStateDeclarations),
		pairs, lawful, refused)
}

// ЧЕТВЁРТАЯ ПАРА, ради которой заведено произведение: посадка `own` без нашего
// читателя. Названа отдельно, потому что это ВХОД БЕЗ ЧИТАТЕЛЯ — зеркало
// правила «читатель без производителя», и отказ обязан называть обе ручки.
func TestSessionCarrierGuard_OurMintWithoutOurReaderIsRefused(t *testing.T) {
	err := validateSessionCarrierConfig(SessionCarrierConfig{
		Posture:     identityposture.Own,
		Carriers:    carrierSet(t, "external"),
		ProviderURL: "http://kratos:80",
	})
	if err == nil {
		t.Fatal("посадка own без нашего читателя принята: край поднимает нашу полосу формы входа, " +
			"служба чеканит kaname_session, и читателя этого печенья в процессе нет — человек " +
			"проходит вход и остаётся анонимом")
	}
	for _, want := range []string{config.SessionCarriersKnob, config.IdentityProviderKnob} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет ручку %s: %v", want, err)
		}
	}
	t.Logf("отказ: %v", err)
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
