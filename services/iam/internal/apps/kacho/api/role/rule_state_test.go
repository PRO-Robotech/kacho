// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package role

// rule_state_test.go — постатейное состояние правила заполняется ОДИНАКОВО в
// Get и в List (задача продукта #1962).
//
// Приёмка `services/iam/docs/engineering/acceptance/rule-state-names-withdrawn-apart-from-unresolved.md`,
// сценарии MOD-RS-02, MOD-RS-03, MOD-RS-06, MOD-RS-12.
//
// Здесь утверждается ПРОВЯЗКА: что состояние, вычисляемое доменом, доезжает до
// обоих чтений и что они не расходятся. Сама функция утверждается пробами домена
// — разные предметы, и первый второго не покрывает.

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// stateOfRule — состояние названного правила и его три величины.
func stateOfRule(t *testing.T, r domain.Role, idx int) domain.RuleState {
	t.Helper()
	require.Len(t, r.RuleStates, len(r.Rules),
		"записей состояния обязано быть по числу правил: иначе ключ RuleIndex ложен")
	for _, st := range r.RuleStates {
		if st.RuleIndex == idx {
			return st
		}
	}
	t.Fatalf("правило %d не получило состояния", idx)
	return domain.RuleState{}
}

// MOD-RS-02 / MOD-RS-03 — ДВЕ причины потери дают РАЗНОЕ слово при ОДИНАКОВЫХ
// счётчиках роли. Это и есть предмет задачи: без слова они неотличимы.
//
// Проба парная намеренно. Утверждение об одном состоянии зеленело бы на
// реализации, называющей всякую потерю отозванной, — то есть ровно на том
// дефекте, ради которого предмет и заведён.
func TestRuleState_MODRS0203_TwoCausesReadDifferently(t *testing.T) {
	h := newIntegrityHarness(t)

	// Потеря ОБЪЯСНЕНА ведомостью: платформа сняла строку каталога.
	taken := h.addCustomRole("rol0000000000000take",
		ruleOn("vpc", "network", "get"), ruleOn("vpc", "gateway", "delete"))
	h.project(taken, "vpc.network", "get")
	h.withdraw(taken, "vpc.gateway", "delete", "gateway снят из манифеста vpc")

	// Потеря НЕ объяснена: правило называет то, чего модель прав не знает.
	// Форма инцидента 513001, у которой переселения не было вовсе.
	never := h.addCustomRole("rol0000000000000nvr1",
		ruleOn("vpc", "network", "get"), ruleOn("probe", "thing", "delete"))
	h.project(never, "vpc.network", "get")

	for _, tc := range []struct {
		id    string
		want  domain.RuleLifecycle
		wdraw int
		unres int
	}{
		{taken, domain.RuleLifecycleWithdrawn, 1, 0},
		{never, domain.RuleLifecycleUnresolved, 0, 1},
	} {
		got, err := h.get(tc.id)
		require.NoError(t, err, "Get %s", tc.id)

		// Счётчики РОЛИ у обеих одинаковы — вот почему нужно слово.
		require.Equal(t, domain.RoleHealthDegraded, got.Integrity.Health, "%s: целость роли", tc.id)
		require.Equal(t, 2, got.Integrity.Declared, "%s: объявлено", tc.id)
		require.Equal(t, 1, got.Integrity.Unresolved, "%s: неразрешённых", tc.id)

		st := stateOfRule(t, got, 1)
		require.Equal(t, tc.want, st.State, "%s: состояние правила 1", tc.id)
		require.Equal(t, tc.wdraw, st.Withdrawn, "%s: объяснённых потерь", tc.id)
		require.Equal(t, tc.unres, st.Unresolved, "%s: необъяснённых потерь", tc.id)

		// Соседнее правило не задето — иначе утверждение было бы односторонним.
		require.Equal(t, domain.RuleLifecycleActive, stateOfRule(t, got, 0).State,
			"%s: правило 0 обязано остаться действующим", tc.id)
	}
}

// MOD-RS-06 — Get и List говорят одно построчно, по СОСТАВУ.
func TestRuleState_MODRS06_GetAndListAgreePerRule(t *testing.T) {
	h := newIntegrityHarness(t)
	id := h.addCustomRole("rol0000000000000agr1",
		ruleOn("vpc", "network", "get"), ruleOn("vpc", "gateway", "delete"))
	h.project(id, "vpc.network", "get")
	h.withdraw(id, "vpc.gateway", "delete", "снят")

	got, err := h.get(id)
	require.NoError(t, err)
	lst := h.listRole(t, id)
	require.Equal(t, got.RuleStates, lst.RuleStates,
		"List разошёлся с Get по состоянию правил — производитель обязан быть один")
	require.NotEmpty(t, got.RuleStates, "контроль: пустое совпало бы с пустым и не утверждало бы ничего")
}

// MOD-RS-12 — отказ ведомости роняет чтение: «не смог прочитать» не есть
// «отобранного нет». Без этого состояние молча стало бы UNRESOLVED на всякой
// недоступности, то есть fail-open ровно на объяснении.
func TestRuleState_MODRS12_LedgerFailureFailsTheRead(t *testing.T) {
	h := newIntegrityHarness(t)
	id := h.addCustomRole("rol0000000000000led1", ruleOn("vpc", "gateway", "delete"))
	h.wdFail = stderrors.New("ведомость недоступна")

	_, err := h.get(id)
	require.Error(t, err, "Get обязан отказать при недоступной ведомости")
	_, _, lerr := h.list()
	require.Error(t, lerr, "List обязан отказать по той же причине")
}

// MOD-RS-05 — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: роль без адресуемых сегментов получает
// записи состояния и они ДЕЙСТВУЮЩИЕ. Без него утверждения выше проходили бы на
// реализации, не заполняющей состояние вовсе.
func TestRuleState_MODRS05_WildcardRuleIsActiveAndStillGetsARecord(t *testing.T) {
	h := newIntegrityHarness(t)
	id := h.addSystemRole("rol0000000000000wld2",
		domain.Rule{Module: "*", Resources: []string{"*"}, Verbs: []string{"*"}})

	got, err := h.get(id)
	require.NoError(t, err)
	st := stateOfRule(t, got, 0)
	require.Equal(t, domain.RuleLifecycleActive, st.State,
		"правилу `*.*` терять нечего — тревога на нём была бы ложной")
	require.Equal(t, 0, st.Declared)
}
