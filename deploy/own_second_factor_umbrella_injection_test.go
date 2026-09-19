// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_second_factor_umbrella_injection_test.go — доказательство того, что
// проверка двух величин Ф12 СПОСОБНА упасть и способна смолчать.
//
// Вход СИНТЕТИЧЕСКИЙ, а не подделка дерева: подделка трогает общий клон, а
// вердикт обязан доказываться на входе, построенном здесь и целиком видном
// читателю. Тот же порядок, что у соседнего kaname_listener_knobs_injection_test.go.
//
// Каждый случай меняет РОВНО ОДИН факт против законного близнеца: иначе
// неизвестно, который из двух дал вердикт.
package deploy_test

import (
	"strings"
	"testing"
)

// legalOwnStack — законный близнец: стенд на `own`, объявивший обе величины.
func legalOwnStack() secondFactorStackFacts {
	return secondFactorStackFacts{Stack: "стенд-own", Posture: "own", Freshness: true, SecretRef: true}
}

func TestSecondFactorJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		facts   []secondFactorStackFacts
		want    int    // ожидаемое число находок
		mustSay string // улика в тексте находки; пусто — находок не ждём
	}{
		{
			name:  "законный близнец: own объявил обе — молчит",
			facts: []secondFactorStackFacts{legalOwnStack()},
			want:  0,
		},
		{
			name: "own без окна свежести — находка",
			facts: []secondFactorStackFacts{func() secondFactorStackFacts {
				f := legalOwnStack()
				f.Freshness = false
				return f
			}()},
			want:    1,
			mustSay: selfServiceFreshnessKey,
		},
		{
			name: "own без координат секрета — находка",
			facts: []secondFactorStackFacts{func() secondFactorStackFacts {
				f := legalOwnStack()
				f.SecretRef = false
				return f
			}()},
			want:    1,
			mustSay: secondFactorEnv,
		},
		{
			name: "own без обеих — ДВЕ находки, а не одна: оператор обязан узнать перечень за один перезапуск",
			facts: []secondFactorStackFacts{{
				Stack: "стенд-own", Posture: "own", Freshness: false, SecretRef: false,
			}},
			want:    2,
			mustSay: selfServiceFreshnessKey,
		},
		{
			// ЗАКОННЫЙ БЛИЗНЕЦ, ради которого проверка ветвится по посадке: под
			// `external` страж службы обе ручки не читает вовсе, и требовать их
			// значило бы краснеть на исправном дереве.
			name: "external без обеих — НЕ находка",
			facts: []secondFactorStackFacts{{
				Stack: "стенд-external", Posture: "external", Freshness: false, SecretRef: false,
			}},
			want: 0,
		},
		{
			name: "посадка не объявлена вовсе — НЕ находка: стенд наследует базовое значение подчарта",
			facts: []secondFactorStackFacts{{
				Stack: "стенд-молчун", Posture: "", Freshness: false, SecretRef: false,
			}},
			want: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings, census := judgeSecondFactorDeclarations(c.facts)
			if len(findings) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(findings), c.want, findings)
			}
			if c.mustSay != "" {
				var said bool
				for _, f := range findings {
					if strings.Contains(f, c.mustSay) {
						said = true
					}
				}
				if !said {
					t.Errorf("ни одна находка не называет %q — оператор не узнает, какой ручки "+
						"не хватает: %v", c.mustSay, findings)
				}
			}
			if census.Stacks != len(c.facts) {
				t.Errorf("перепись осмотренного %d, подано %d — «находок ноль» стало бы "+
					"неотличимо от «прочитано ноль»", census.Stacks, len(c.facts))
			}
		})
	}
}

// TestSecondFactorJudgement_CountsOwnStacksSeparately — перепись различает
// «стендов осмотрено» и «из них на own». Без второго числа вердикт «находок нет»
// неотличим от «ни один стенд на own не заведён», а это ровно то состояние
// дерева, в котором проверка беспредметна.
func TestSecondFactorJudgement_CountsOwnStacksSeparately(t *testing.T) {
	facts := []secondFactorStackFacts{
		legalOwnStack(),
		{Stack: "стенд-external", Posture: "external"},
		{Stack: "стенд-молчун", Posture: ""},
	}
	_, census := judgeSecondFactorDeclarations(facts)
	if census.Stacks != 3 {
		t.Errorf("осмотрено %d, подано 3", census.Stacks)
	}
	if census.Own != 1 {
		t.Errorf("на посадке own насчитано %d, подан 1", census.Own)
	}
}
