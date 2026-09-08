// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_listener_knobs_injection_test.go — доказательство того, что проверка
// ручек транспорта СПОСОБНА упасть и способна смолчать.
//
// Вход подаётся СИНТЕТИЧЕСКИЙ, а не подделкой дерева: подделка дерева трогает
// общий клон, а вердикт должен доказываться на входе, который построен здесь и
// целиком виден читателю. Тот же порядок, что у соседнего
// identity_callback_transport_test.go.
//
// Каждый случай меняет РОВНО ОДИН факт против законного близнеца: иначе
// неизвестно, который из двух дал вердикт.
package deploy_test

import (
	"strings"
	"testing"
)

// legalKnobs — законная раскладка: одна ручка на одного слушателя.
func legalKnobs() []knobFacts {
	return []knobFacts{
		{knob: "hooks", fallback: "httpListeners", surfaces: []string{"HOOKS"}, blocks: 3},
		{knob: "metrics", fallback: "httpListeners", surfaces: []string{"METRICS"}, blocks: 1},
	}
}

// legalStack — боевой стенд, объявивший обе ручки: одна под транспортом, вторая
// открыта по ОБЪЯВЛЕННОМУ исключению.
func legalStack() stackListenerFacts {
	return stackListenerFacts{
		stack:      "стенд",
		production: true,
		declaredKnob: map[string]any{
			"hooks":   false,
			"metrics": true,
		},
		declaredException: map[string]any{"hooks": true},
	}
}

func TestListenerKnobsJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name   string
		knobs  []knobFacts
		stacks []stackListenerFacts
		want   []string // подстроки, которые находка обязана назвать
		silent bool
		why    string
	}{
		{
			name:  "законный близнец: одна ручка — один слушатель, всё объявлено",
			knobs: legalKnobs(), stacks: []stackListenerFacts{legalStack()},
			silent: true,
			why:    "верная раскладка обязана молчать, иначе первый ложный срабат снимет проверку",
		},
		{
			name: "ОДНА РУЧКА НА ДВА СЛУШАТЕЛЯ — находка с координатой",
			knobs: func() []knobFacts {
				k := legalKnobs()
				k[0].surfaces = []string{"HOOKS", "METRICS"} // единственное отличие
				return k
			}(),
			stacks: []stackListenerFacts{legalStack()},
			want:   []string{kindKnobCoversManySurfaces, "hooks", "HOOKS", "METRICS"},
			why:    "именно она делает боевой профиль невыразимым: требования у слушателей разные",
		},
		{
			name: "ОДИН СЛУШАТЕЛЬ ПОД ДВУМЯ РУЧКАМИ — зеркало того же класса",
			knobs: func() []knobFacts {
				k := legalKnobs()
				k[1].surfaces = []string{"HOOKS"} // тот же слушатель, вторая ручка
				return k
			}(),
			stacks: []stackListenerFacts{{
				stack: "стенд", production: true,
				declaredKnob:      map[string]any{"hooks": true, "metrics": true},
				declaredException: map[string]any{},
			}},
			want: []string{kindSurfaceUnderManyKnobs, "HOOKS"},
			why:  "две ручки об одном предмете расходятся молча — это тише, чем первый случай",
		},
		{
			name:  "РУЧКА НЕ ОБЪЯВЛЕНА боевым профилем",
			knobs: legalKnobs(),
			stacks: []stackListenerFacts{func() stackListenerFacts {
				st := legalStack()
				delete(st.declaredKnob, "metrics") // единственное отличие
				return st
			}()},
			want: []string{kindKnobNotDeclared, "metrics", "httpListeners"},
			why:  "величина, которую построение подставляет само, предметом стража быть не может",
		},
		{
			name:  "ОТКРЫТЫЙ ТЕКСТ БЕЗ ОБЪЯВЛЕННОГО ИСКЛЮЧЕНИЯ",
			knobs: legalKnobs(),
			stacks: []stackListenerFacts{func() stackListenerFacts {
				st := legalStack()
				st.declaredException = map[string]any{} // единственное отличие
				return st
			}()},
			want: []string{kindPlaintextNotDeclared, "hooks"},
			why:  "страж старта откажется пускать процесс — узнать это на объявлениях дешевле, чем на стенде",
		},
		{
			name:  "ИСКЛЮЧЕНИЕ ПРОТИВОРЕЧИТ ОБЪЯВЛЕННОМУ ТРАНСПОРТУ",
			knobs: legalKnobs(),
			stacks: []stackListenerFacts{func() stackListenerFacts {
				st := legalStack()
				st.declaredKnob["hooks"] = true // единственное отличие
				return st
			}()},
			want: []string{kindExceptionContradicts, "hooks"},
			why:  "два правила об одном предмете; тот же вердикт даёт и страж старта процесса",
		},
		{
			name:  "СТЕНД РАЗРАБОТКИ не судится: страж транспорта там no-op",
			knobs: legalKnobs(),
			stacks: []stackListenerFacts{func() stackListenerFacts {
				st := legalStack()
				st.production = false // единственное отличие
				st.declaredKnob = map[string]any{}
				st.declaredException = map[string]any{}
				return st
			}()},
			silent: true,
			why:    "требовать объявлений там, где их никто не проверит, значит краснеть на исправном",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := judgeListenerKnobs(tc.knobs, tc.stacks)
			if tc.silent {
				if len(findings) != 0 {
					t.Fatalf("законный близнец обязан молчать (%s), а сказано: %v", tc.why, findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("проверка обязана найти (%s), а она смолчала", tc.why)
			}
			var said strings.Builder
			for _, f := range findings {
				said.WriteString(f.stack + " " + f.knob + " " + f.kind + " " + f.detail + "\n")
			}
			for _, w := range tc.want {
				if !strings.Contains(said.String(), w) {
					t.Errorf("находка обязана назвать %q (%s); сказано:\n%s", w, tc.why, said.String())
				}
			}
		})
	}
}

// TestListenerKnobScan_ReadsTheTemplateNotTheProse — распознаватель судит
// ДЕЙСТВИЕ шаблона, а не текст рядом с ним.
//
// Без этого проверка краснела бы на собственном объяснении: имена ручек и
// переменных стоят в комментариях того же файла, и предикат по подстроке
// засчитал бы прозу за условие.
func TestListenerKnobScan_ReadsTheTemplateNotTheProse(t *testing.T) {
	tpl := strings.Join([]string{
		`            # ручка hooks накрывает KANAME_HOOKS_SERVER_MTLS_ENABLE — это ПРОЗА`,
		`            {{- if (dig "hooks" .Values.mtls.httpListeners .Values.mtls) }}`,
		`            - name: KANAME_HOOKS_SERVER_MTLS_ENABLE`,
		`              value: "true"`,
		`            {{- if .Values.somethingElse }}`,
		`            - name: KANAME_METRICS_SERVER_MTLS_ENABLE`,
		`              value: "true"`,
		`            {{- end }}`,
		`            {{- end }}`,
		`            - name: KANAME_REST_SERVER_MTLS_ENABLE`,
		`              value: "true"`,
		`            - name: KANAME_REST_UPSTREAM_MTLS_ENABLE`,
		`              value: "true"`,
	}, "\n")

	got := scanListenerKnobs(tpl)
	if len(got) != 1 {
		t.Fatalf("ручек распознано %d, ожидалась одна: %+v", len(got), got)
	}
	// METRICS лежит ВНУТРИ вложенного условия того же блока — он под этой ручкой.
	// REST лежит СНАРУЖИ блока и под неё не подпадает; UPSTREAM не слушатель вовсе.
	want := "HOOKS METRICS"
	if strings.Join(got[0].surfaces, " ") != want {
		t.Fatalf("слушатели под ручкой: %q, ожидалось %q — распознаватель либо не считает "+
			"глубину вложенности, либо принимает за слушателя исходящее удостоверение",
			strings.Join(got[0].surfaces, " "), want)
	}
	t.Logf("осмотрено: строк шаблона %d · ручек 1 · слушателей под ней %d",
		strings.Count(tpl, "\n")+1, len(got[0].surfaces))
}
