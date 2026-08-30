// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanselftestreach_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) сними вызов — гейт краснеет и НАЗЫВАЕТ владельца суиты и его прогонщик;
// (б) оставь вызов — гейт молчит, в том числе на суите, чей вызов записан не
//
//	тем же начертанием, что у образца.
//
// Отдельно доказывается самоистечение ведомости: запись, которой больше нечего
// прощать, обязана краснеть. Без этой стороны послабление пережило бы свой
// предмет — тот самый класс, ради которого гейт и заведён.
//
// И отдельно — что предикат чтения находит свой предмет в настоящей форме
// вызова и НЕ находит его в объясняющем комментарии рядом: гейт по подстроке
// остался бы зелёным на снятом вызове, покраснев на собственном объяснении.
package repohygiene

import (
	"strings"
	"testing"
)

func TestNewmanSelftestReach_ProvenByInjection(t *testing.T) {
	healthy := []newmanSelftestSuite{
		{Owner: "services/geo", RunnerPath: "services/geo/tests/newman/scripts/run.sh", Calls: 1},
		{Owner: "services/nlb", RunnerPath: "services/nlb/tests/newman/scripts/run.sh", Calls: 1},
	}
	noExempt := map[string]string{}

	t.Run("законный близнец: все зовут — гейт молчит", func(t *testing.T) {
		if found := adjudicateNewmanSelftestReach(healthy, noExempt); len(found) != 0 {
			t.Fatalf("ложное срабатывание на исправном дереве: %v", found)
		}
	})

	t.Run("снят вызов у одной суиты — краснеет и называет ЕЁ, а не соседа", func(t *testing.T) {
		broken := append([]newmanSelftestSuite(nil), healthy...)
		broken[1].Calls = 0
		found := adjudicateNewmanSelftestReach(broken, noExempt)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		if !strings.Contains(found[0], "services/nlb") {
			t.Fatalf("находка не называет владельца суиты:\n%s", found[0])
		}
		if !strings.Contains(found[0], "run.sh") {
			t.Fatalf("находка не называет прогонщик, в котором вызова нет:\n%s", found[0])
		}
		// Обвиняемого называет НАЧАЛО находки; geo упоминается ниже как образец
		// формы вызова, и это не обвинение. Проба спрашивает именно про
		// обвиняемого — иначе она запретила бы находке приводить пример.
		if !strings.HasPrefix(found[0], "services/nlb:") {
			t.Fatalf("обвиняемым назван не тот, у кого снят вызов:\n%s", found[0])
		}
	})

	t.Run("самопроверка есть, прогонщика нет — отдельная находка", func(t *testing.T) {
		orphan := []newmanSelftestSuite{{Owner: "services/x"}}
		found := adjudicateNewmanSelftestReach(orphan, noExempt)
		if len(found) != 1 || !strings.Contains(found[0], "прогонщика") {
			t.Fatalf("суита без прогонщика не опознана: %v", found)
		}
	})

	t.Run("ведомость гасит находку, пока у неё есть предмет", func(t *testing.T) {
		broken := append([]newmanSelftestSuite(nil), healthy...)
		broken[1].Calls = 0
		exempt := map[string]string{"services/nlb": "причина"}
		if found := adjudicateNewmanSelftestReach(broken, exempt); len(found) != 0 {
			t.Fatalf("запись ведомости не гасит свою находку: %v", found)
		}
	})

	t.Run("ПОСЛАБЛЕНИЕ ИСТЕКАЕТ: вызов появился — запись краснеет", func(t *testing.T) {
		exempt := map[string]string{"services/nlb": "причина"}
		found := adjudicateNewmanSelftestReach(healthy, exempt)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка о потерявшей предмет записи, получено %d: %v",
				len(found), found)
		}
		if !strings.Contains(found[0], "потеряла предмет") ||
			!strings.Contains(found[0], "services/nlb") {
			t.Fatalf("находка не называет истёкшую запись:\n%s", found[0])
		}
	})

	t.Run("ПОСЛАБЛЕНИЕ ИСТЕКАЕТ: суита исчезла — запись краснеет", func(t *testing.T) {
		exempt := map[string]string{"services/ghost": "причина"}
		found := adjudicateNewmanSelftestReach(healthy, exempt)
		if len(found) != 1 || !strings.Contains(found[0], "services/ghost") {
			t.Fatalf("запись о несуществующей суите не опознана: %v", found)
		}
	})

	// ПРЕДИКАТ ЧТЕНИЯ НАХОДИТ СВОЙ ПРЕДМЕТ — и только его. Разойдись он с формой
	// вызова, гейт объявил бы недостижимой исправную суиту и был бы снят как
	// шумный; прими он комментарий за вызов — молчал бы на снятом вызове.
	t.Run("предикат вызова: находит исполняемое, не находит прозу", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want bool
		}{
			{"вызов как у geo", `  if ! node "$NEWMAN_DIR/scripts/selftest-assertions.js"; then`, true},
			{"вызов из каталога суиты", "if ! node scripts/selftest-assertions.js; then", true},
			{"вызов целью make", "\t@cd tests/newman && node scripts/selftest-assertions.js", true},
			{"ОБЪЯСНЕНИЕ рядом с вызовом — не вызов",
				"# selftest-assertions.js судит сгенерированные коллекции и стенда не требует", false},
			{"закомментированный вызов — не вызов",
				"# node scripts/selftest-assertions.js", false},
			{"имя файла в прозе без node — не вызов",
				"echo 'см. scripts/selftest-assertions.js' >&2", false},
		}
		for _, c := range cases {
			if got := reNewmanSelftestCall.MatchString(c.body); got != c.want {
				t.Errorf("%s: предикат вернул %v, ждали %v на\n\t%s", c.name, got, c.want, c.body)
			}
		}
	})
}
