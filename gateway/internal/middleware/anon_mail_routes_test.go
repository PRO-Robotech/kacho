// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"sort"
	"testing"
)

// TestLoginLanePaths_NTF2_63_AnonMailMarksExactlyTheTwoMailPlacingVerbs —
// ограничитель края действует только на анонимные почтовые глаголы (приёмка
// NTF-2, Р5, NTF2-63): признак `anonMail` стоит у записей `recovery` и
// `register` и только у них. Пути предъявления кода — `recovery/complete` и
// `register/confirm` — письма не ставят, и признака у них нет; перебор кода на
// них держат оси службы (Р6).
//
// Перечень путей звена не пишется вторым местом: читатель признака — монтаж
// корня, и проба судит объявление, а не копию.
func TestLoginLanePaths_NTF2_63_AnonMailMarksExactlyTheTwoMailPlacingVerbs(t *testing.T) {
	want := map[string]string{
		"recovery": LoginLanePathRecovery,
		"register": LoginLanePathRegister,
	}
	var marked []string
	for _, rt := range LoginLaneRoutes() {
		if !rt.AnonMail() {
			continue
		}
		marked = append(marked, rt.Verb)
		if want[rt.Verb] != rt.Path {
			t.Errorf("запись %q (%s) несёт признак anonMail, по приёмке он только у recovery и register", rt.Verb, rt.Path)
		}
		if rt.Target != RelayTargetForm {
			t.Errorf("запись %q с признаком anonMail ретранслируется на %q, а не на слушатель формы", rt.Verb, rt.Target)
		}
	}
	sort.Strings(marked)
	if len(marked) != len(want) {
		t.Fatalf("признак anonMail у %d записей %v, по приёмке — у 2 (recovery, register)", len(marked), marked)
	}
	// Близнец: оба пути предъявления кода — без признака.
	for _, p := range []string{LoginLanePathRecoveryComplete, LoginLanePathRegisterConfirm} {
		rt, ok := LoginLaneRouteFor(p)
		if !ok {
			t.Fatalf("путь предъявления кода %q не объявлен", p)
		}
		if rt.AnonMail() {
			t.Errorf("путь предъявления кода %q несёт признак anonMail — ограничитель края его не трогает (NTF2-63)", p)
		}
	}
	t.Logf("перепись: записей объявления %d · с признаком anonMail %d %v", len(LoginLaneRoutes()), len(marked), marked)
}

// TestLoginLanePaths_NTF2_RegisterConfirmIsAnAnonymousFormVerb — путь
// предъявления кода регистрации ретранслируется на слушатель формы, носителя не
// читает (ретранслируется и при неответе службы, как предъявление кода
// восстановления) и до подтверждения адреса закрыт умолчанием: решение F6b о
// девяти открытых записях не расширяется.
func TestLoginLanePaths_NTF2_RegisterConfirmIsAnAnonymousFormVerb(t *testing.T) {
	rt, ok := LoginLaneRouteFor("/iam/v1/auth/register/confirm")
	if !ok {
		t.Fatal("путь /iam/v1/auth/register/confirm не объявлен — край ответил бы на него 404")
	}
	if rt.Verb != "register-confirm" || rt.Target != RelayTargetForm {
		t.Errorf("запись %+v: ожидались глагол register-confirm и слушатель формы", rt)
	}
	if !loginLaneRelaysWhenUnanswered(rt.Path) {
		t.Error("предъявление кода регистрации носитель не читает и обязано ретранслироваться при неответе службы")
	}
	if loginLaneOpenBeforeAddressConfirmation(rt.Path) {
		t.Error("предъявление кода регистрации открыто до подтверждения адреса — решение F6b (девять записей) расширено без основания")
	}
	if !isPublicHTTPPath(rt.Path) {
		t.Error("путь предъявления кода регистрации не освобождён от записи каталога — каталог отверг бы его до службы")
	}
}
