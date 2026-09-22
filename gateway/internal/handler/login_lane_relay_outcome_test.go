// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay_outcome_test.go — КЛАССИФИКАЦИЯ ИСХОДА ВЫХОДА ПОЛНА.
//
// Проба внутри пакета и зовёт классификатор напрямую, и это решение, а не
// удобство: её предмет — ПОЛНОТА РАЗБОРА, а не путь запроса. Часть кодов
// (1xx, а также значения вне диапазона) окончательным ответом на ретрансляции
// не бывает — клиент Go их потребляет либо не производит вовсе, — и проверять
// их через HTTP значило бы подавать вход, которого ни один производитель не
// может выдать. Через путь запроса те же три ветви судит соседняя проба.
package handler

import (
	"net/http"
	"testing"
)

// Корзины «прочее» нет: КАЖДЫЙ код попадает в одну из трёх ветвей, и
// нераспознанный ведёт к ГАШЕНИЮ — в сторону состояния, из которого человек
// может войти заново.
func TestLogoutOutcomeOf_EveryCodeFallsIntoANamedBranch(t *testing.T) {
	cases := []struct {
		code int
		want logoutOutcome
		why  string
	}{
		{http.StatusOK, logoutPerformed, "обычный успех"},
		{http.StatusNoContent, logoutPerformed, "успех без тела"},
		{299, logoutPerformed, "верхняя граница успеха"},
		{http.StatusMovedPermanently, logoutPerformed, "перенаправление — форма успеха браузерного выхода"},
		{http.StatusFound, logoutPerformed, "то же"},
		{http.StatusSeeOther, logoutPerformed, "то же, после POST"},
		{399, logoutPerformed, "верхняя граница перенаправления"},
		{http.StatusBadRequest, logoutRefused, "служба отвергла"},
		{http.StatusUnauthorized, logoutRefused, "служба отвергла"},
		{http.StatusServiceUnavailable, logoutRefused, "выход не выполнен"},
		{599, logoutRefused, "верхняя граница отказа"},
		{http.StatusContinue, logoutUnrecognised, "исход выхода не назван"},
		{199, logoutUnrecognised, "информационный, исход не назван"},
		{0, logoutUnrecognised, "кода нет вовсе"},
		{600, logoutUnrecognised, "вне диапазона HTTP"},
	}
	byBranch := map[logoutOutcome]int{}
	for _, tc := range cases {
		got := logoutOutcomeOf(tc.code)
		if got != tc.want {
			t.Errorf("код %d отнесён к ветви %d, ожидалась %d — %s", tc.code, got, tc.want, tc.why)
			continue
		}
		byBranch[got]++
	}
	// Все три ветви обязаны быть НЕПУСТЫМИ: ветвь без представителя означает,
	// что о ней ничего не утверждено.
	for branch, name := range map[logoutOutcome]string{
		logoutPerformed:    "выполнен",
		logoutRefused:      "отвергнут",
		logoutUnrecognised: "не распознан",
	} {
		if byBranch[branch] == 0 {
			t.Errorf("ветвь «%s» без представителя — о ней ничего не утверждено", name)
		}
	}
	t.Logf("перепись: кодов проверено %d · ветвей 3 · выполнен %d · отвергнут %d · не распознан %d",
		len(cases), byBranch[logoutPerformed], byBranch[logoutRefused], byBranch[logoutUnrecognised])
}
