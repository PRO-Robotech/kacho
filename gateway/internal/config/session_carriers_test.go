// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carriers_test.go — ПРОФИЛЬ ОБЯЗАН ВЫРАЖАТЬ ТРИ СОСТОЯНИЯ носителя
// браузерной сессии: только чужой · оба · только наш.
//
// # Почему трёх, а не двух
//
// Посадка личности (`KACHO_API_GATEWAY_IDENTITY_PROVIDER`) выражает ровно два
// взаимоисключающих состояния, и до этой работы читателя носителя выбирала
// она. Следствие наблюдаемое: перевод стенда с людьми со чужой чеканки на нашу
// был АТОМАРЕН — в момент правки профиля каждый, чья чужая сессия жива, терял
// вход, потому что читателя её печенья в новом процессе не оставалось.
//
// У приёма ТОКЕНА этого дефекта нет и никогда не было: издателей край принимает
// МНОЖЕСТВОМ (`KACHO_API_GATEWAY_TOKEN_ISSUERS`), и переходное состояние «наш
// издатель уже принимается, чужой ещё принимается» профиль выражает с первого
// дня. Носитель получает ту же форму — множество, а не переключатель, — потому
// что предмет тот же: переезд между двумя источниками личности.
//
// # Порядок в множестве НЕПРЕДСТАВИМ, и это несущее
//
// Кто выигрывает при двух предъявленных носителях — решение о том, ЧЬЯ ЛИЧНОСТЬ
// ДЕЙСТВУЕТ, и профилю оно не принадлежит. Множество поэтому не перечень: оно
// отвечает на «читаем ли мы эту сторону», и переставить стороны местами в нём
// нечем. Старшинство стоит в коде и держится
// `middleware/session_carrier_precedence_test.go`.
package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// carrierState — как проба называет наблюдаемое состояние, чтобы перепись
// читалась словами, а не парой булевых.
func carrierState(s config.SessionCarrierSet) string {
	switch {
	case s.ReadsOwn() && s.ReadsProvider():
		return "оба"
	case s.ReadsOwn():
		return "только наш"
	case s.ReadsProvider():
		return "только чужой"
	}
	return "ни одного"
}

func resolveCarriers(t *testing.T, posture, carriers string) (config.SessionCarrierSet, error) {
	t.Helper()
	if posture != "" {
		t.Setenv(config.IdentityProviderKnob, posture)
	}
	if carriers != "" {
		t.Setenv(config.SessionCarriersKnob, carriers)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg.ResolvedSessionCarriers()
}

// ─────────────────────────────────────────────────────────────────────────────
// ТРИ СОСТОЯНИЯ, и на каждое — своя строка.

func TestSessionCarriers_ProfileExpressesThreeStates(t *testing.T) {
	cases := []struct {
		name     string
		declared string
		want     string
	}{
		{"только чужой — сегодняшний стенд до переезда", "external", "только чужой"},
		{"оба — переходное состояние: вход не теряет никто", "own,external", "оба"},
		{"только наш — состояние, к которому платформа приходит", "own", "только наш"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCarriers(t, "own", tc.declared)
			if err != nil {
				t.Fatalf("%q обязано разбираться: %v", tc.declared, err)
			}
			if carrierState(got) != tc.want {
				t.Fatalf("%q дало состояние %q, ожидалось %q", tc.declared, carrierState(got), tc.want)
			}
			t.Logf("перепись: объявлено %q → состояние %q (наш %v · чужой %v)",
				tc.declared, carrierState(got), got.ReadsOwn(), got.ReadsProvider())
		})
	}
}

// Переход между состояниями — решение ПРОФИЛЯ: посадка при этом не меняется.
// Три состояния при ОДНОЙ и той же посадке — то, чего не существовало.
func TestSessionCarriers_TheTransitionIsAProfileDecisionNotACodeEdit(t *testing.T) {
	seen := map[string]string{}
	for _, declared := range []string{"external", "own,external", "own"} {
		got, err := resolveCarriers(t, "own", declared)
		if err != nil {
			t.Fatalf("%q: %v", declared, err)
		}
		seen[carrierState(got)] = declared
	}
	if len(seen) != 3 {
		t.Fatalf("при неизменной посадке различимых состояний %d, ожидалось 3: %v", len(seen), seen)
	}
	t.Logf("перепись: посадка одна (own) · различимых состояний носителя %d · %v", len(seen), seen)
}

// ─────────────────────────────────────────────────────────────────────────────
// Старшинство профилю НЕ принадлежит: порядок записи на состояние не влияет.

func TestSessionCarriers_WrittenOrderDoesNotDecideWhoWins(t *testing.T) {
	a, err := resolveCarriers(t, "own", "own,external")
	if err != nil {
		t.Fatalf("own,external: %v", err)
	}
	b, err := resolveCarriers(t, "own", "external,own")
	if err != nil {
		t.Fatalf("external,own: %v", err)
	}
	if a != b {
		t.Fatalf("порядок записи изменил результат: %v против %v — старшинство стало бы величиной "+
			"профиля, то есть решение о действующей личности принимал бы оператор", a, b)
	}
	t.Logf("перепись: записей порядка 2 · различимых результатов 1 · состояние %q", carrierState(a))
}

// ─────────────────────────────────────────────────────────────────────────────
// Необъявленная ручка НЕ ломает ни один существующий профиль: состояние
// выводится из посадки ровно так, как край вёл себя до этой работы.

func TestSessionCarriers_UndeclaredDerivesTodaysBehaviourFromThePosture(t *testing.T) {
	for posture, want := range map[string]string{"own": "только наш", "external": "только чужой"} {
		got, err := resolveCarriers(t, posture, "")
		if err != nil {
			t.Fatalf("посадка %q без объявленной ручки: %v", posture, err)
		}
		if carrierState(got) != want {
			t.Fatalf("посадка %q без ручки дала %q, ожидалось %q — существующий профиль обязан "+
				"вести себя как прежде", posture, carrierState(got), want)
		}
		t.Logf("перепись: посадка %q · ручка не объявлена · состояние %q", posture, carrierState(got))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Отказы. Каждый называет ручку и то, что оператору править.

func TestSessionCarriers_RefusesValuesThatWouldLeaveTheStandWithoutAReader(t *testing.T) {
	cases := []struct {
		name     string
		posture  string
		declared string
		wantText string
	}{
		{
			name:    "объявлено и даёт НОЛЬ элементов — вход пропал бы молча",
			posture: "own", declared: ",", wantText: "elements",
		},
		{
			name:    "имя вне словаря — второго словаря не заводится",
			posture: "own", declared: "own,kratos", wantText: "kratos",
		},
		{
			name:    "одна сторона названа дважды — одна сторона, одна запись",
			posture: "own", declared: "own,own", wantText: "twice",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveCarriers(t, tc.posture, tc.declared)
			if err == nil {
				t.Fatalf("%q принято без отказа", tc.declared)
			}
			if !strings.Contains(err.Error(), config.SessionCarriersKnob) {
				t.Fatalf("отказ не называет ручку %s: %v", config.SessionCarriersKnob, err)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("отказ не называет предмет %q: %v", tc.wantText, err)
			}
			t.Logf("отказ: %v", err)
		})
	}
}

// Посадка не объявлена и ручка не объявлена — выводить не из чего. Отказ
// называет ОБЕ ручки: оператору решать, какую объявить.
func TestSessionCarriers_UndeclaredWithUndeclaredPostureIsRefused(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.ResolvedSessionCarriers(); err == nil {
		t.Fatal("необъявленная ручка при необъявленной посадке принята без отказа — стенд поднялся бы, " +
			"не выбрав ни одного читателя, и был бы неотличим от исправного")
	} else if !strings.Contains(err.Error(), config.SessionCarriersKnob) ||
		!strings.Contains(err.Error(), config.IdentityProviderKnob) {
		t.Fatalf("отказ обязан назвать обе ручки: %v", err)
	}
}
