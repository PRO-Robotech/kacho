// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// address_verification_knobs_umbrella_test.go — ПЯТЬ РУЧЕК ПОДТВЕРЖДЕНИЯ АДРЕСА
// ДОЕЗЖАЮТ ДО СЛУЖБЫ ДОСТУПА НА КАЖДОМ СТЕКЕ ПОСАДКИ `own` (kacho#2901).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Решение владельца 2026-09-27: дальше экранов регистрации и входа человек
// проходит только с подтверждённым адресом почты. Служба доступа вводит это
// вместе с пятью ручками полосы входа (приёмка kaname#456, Р7, Р9; сценарий
// EV-90): `authn.login.verification-code-ttl`, `verification-code-attempts`,
// `verification-resend-interval`, `verification-resend-limit`,
// `verification-resend-window`. Умолчания нет ни у одной: незаданная роняет
// старт службы с именем ключа и переменной. Величины профиля продукта —
// 30m · 5 · 60s · 5 · 24h.
//
// Здесь судится половина зонта: каждый стек `deploy/stacks.txt` на посадке
// `own` доставляет все пять ключом настроек с величиной профиля, а профиль,
// снявший любую, доставляет её НЕЗАДАННОЙ — ключа в настройках нет, и страж
// службы получает отсутствие, а не подставленное построением значение.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Отказ старта с именем ключа — исход стража службы, и держит его проба службы
// (EV-90). Требует ли их пин службы в go.mod, решает блок обязательных величин
// пиненного модуля: называет их — их судит и проба класса
// (pinned_required_settings_reach_the_service_test.go); перепись ниже печатает,
// сколько из пяти пин требует.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// addressVerificationKnobs — пять ручек: ключ настроек, имя величины в
// значениях чарта и величина профиля продукта (Р9 приёмки службы).
var addressVerificationKnobs = []struct{ configKey, valueKey, want string }{
	{"verification-code-ttl", "verificationCodeTtl", "30m"},
	{"verification-code-attempts", "verificationCodeAttempts", "5"},
	{"verification-resend-interval", "verificationResendInterval", "60s"},
	{"verification-resend-limit", "verificationResendLimit", "5"},
	{"verification-resend-window", "verificationResendWindow", "24h"},
}

// renderStackSubchart — рендер подчарта службы доступа значениями цепочки стека
// (секция `kaname` и `global` умбреллы, слитые так, как их сливает helm) плюс
// точечные установки поверх.
func renderStackSubchart(t *testing.T, name string, values map[string]any, sets ...string) (string, error) {
	t.Helper()
	body, err := yaml.Marshal(values)
	if err != nil {
		t.Fatalf("стек %s: значения подчарта не сериализуются: %v", name, err)
	}
	file := filepath.Join(t.TempDir(), name+".yaml")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := filepath.Abs(umbrellaDir)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(base, file)
	if err != nil {
		t.Fatal(err)
	}
	return renderIdentitySubchart(t, []string{rel}, sets...)
}

// addressVerificationDelivered — величины пяти ручек в отрендеренных настройках
// службы; отсутствующий ключ в карту не попадает.
func addressVerificationDelivered(t *testing.T, rendered string) map[string]string {
	t.Helper()
	got := map[string]string{}
	login, ok := configSection(kanameServiceConfig(t, rendered), "authn", "login")
	if !ok {
		return got
	}
	for _, k := range addressVerificationKnobs {
		if v, present := login[k.configKey]; present && v != nil {
			got[k.configKey] = fmt.Sprint(v)
		}
	}
	return got
}

// pinRequiresAddressVerification — называет ли блок обязательных величин
// пиненного модуля хоть одну из пяти ручек.
func pinRequiresAddressVerification(t *testing.T) int {
	t.Helper()
	n := 0
	for _, r := range pinnedRequiredRows(t) {
		for _, k := range addressVerificationKnobs {
			if r.key == "authn.login."+k.configKey {
				n++
			}
		}
	}
	return n
}

// TestAddressVerificationKnobs_EveryOwnStackDeliversAllFive — каждый стек на
// посадке `own` доставляет все пять ручек ключом настроек с величиной профиля.
func TestAddressVerificationKnobs_EveryOwnStackDeliversAllFive(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	own, delivered := 0, 0
	for _, name := range names {
		values := stackIdentityValues(t, stacks[name])
		if idp, _ := lookup(values, "config", "authn", "identityProvider"); idp != "own" {
			continue
		}
		own++
		out, err := renderStackSubchart(t, name, values)
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы не удался: %v\n%s", name, err, out)
		}
		got := addressVerificationDelivered(t, out)
		for _, k := range addressVerificationKnobs {
			v, present := got[k.configKey]
			switch {
			case !present:
				t.Errorf("стек %s: `authn.login.%s` рендер не доставляет — профиль не объявил "+
					"`kaname.config.authn.login.%s`, и служба откажет в старте с именем ключа",
					name, k.configKey, k.valueKey)
			case v != k.want:
				t.Errorf("стек %s: `authn.login.%s` = %s, а величина профиля продукта — %s "+
					"(Р9 приёмки kaname#456)", name, k.configKey, v, k.want)
			default:
				delivered++
			}
		}
	}
	if own == 0 {
		t.Fatal("ни одного стека на посадке own — вердикта нет")
	}
	t.Logf("перепись: стеков на own %d · ручек в ожидании %d · доставлено %d из %d · "+
		"пин службы требует из пяти %d", own, len(addressVerificationKnobs), delivered,
		own*len(addressVerificationKnobs), pinRequiresAddressVerification(t))
}

// TestAddressVerificationKnobs_RemovedKnobReachesTheGuardUnset — снятая в
// профиле ручка доезжает до стража службы НЕЗАДАННОЙ: ключа в настройках нет.
// Инъекция по каждой из пяти на корне каждой цепочки; законный близнец — та же
// цепочка без снятия доставляет все пять.
func TestAddressVerificationKnobs_RemovedKnobReachesTheGuardUnset(t *testing.T) {
	stacks := deployStacks(t)
	var found, silent int
	for _, name := range []string{"dev", "prod"} {
		chain, ok := stacks[name]
		if !ok {
			t.Fatalf("стека %q в таблице нет — инъекция потеряла вход", name)
		}
		values := stackIdentityValues(t, chain)

		twin, err := renderStackSubchart(t, name, values)
		if err != nil {
			t.Fatalf("стек %s: рендер близнеца не удался: %v\n%s", name, err, twin)
		}
		if got := addressVerificationDelivered(t, twin); len(got) != len(addressVerificationKnobs) {
			t.Fatalf("стек %s, близнец: доставлено %d из %d — инъекции судить не с чем",
				name, len(got), len(addressVerificationKnobs))
		}
		silent++

		for _, k := range addressVerificationKnobs {
			out, err := renderStackSubchart(t, name, values, "config.authn.login."+k.valueKey+"=null")
			if err != nil {
				t.Fatalf("стек %s без `%s`: рендер не удался: %v\n%s", name, k.valueKey, err, out)
			}
			got := addressVerificationDelivered(t, out)
			if v, present := got[k.configKey]; present {
				t.Errorf("стек %s: снятая `%s` доехала значением %q — построение подставило "+
					"величину, которой профиль не объявлял, и страж службы её не увидит",
					name, k.valueKey, v)
				continue
			}
			if len(got) != len(addressVerificationKnobs)-1 {
				t.Errorf("стек %s: снятие `%s` задело соседей — доставлено %d вместо %d",
					name, k.valueKey, len(got), len(addressVerificationKnobs)-1)
				continue
			}
			found++
		}
	}
	t.Logf("перепись инъекции: снятий доехало незаданными %d из %d · законных близнецов %d",
		found, 2*len(addressVerificationKnobs), silent)
	if found != 2*len(addressVerificationKnobs) || silent != 2 {
		t.Fatalf("инъекция неполна: %s", strings.TrimSpace(fmt.Sprintf("найдено %d, близнецов %d", found, silent)))
	}
}
