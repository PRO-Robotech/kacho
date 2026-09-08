// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_rest_client_auth_parity_injection_test.go — доказательство того, что
// сверка «профиль объявляет режим ↔ страж требует» СПОСОБНА упасть и способна
// смолчать.
//
// Вход СИНТЕТИЧЕСКИЙ, а не подделка дерева: подделка трогает общий клон, а
// вердикт обязан доказываться на входе, построенном здесь и целиком видном
// читателю. Тот же порядок, что у соседних kaname_listener_knobs_injection_test.go
// и client_identity_leaf_injection_test.go.
//
// КАЖДЫЙ СЛУЧАЙ МЕНЯЕТ РОВНО ОДИН ФАКТ против законного близнеца. Инъекция,
// меняющая два, доказывает лишь то, что покраснело что-то: который из двух дал
// вердикт — неизвестно (testing.md §«Гейт на класс», п. 2 и change-graph.md §6).
//
// Доказываются ТРИ вещи, и третья — не украшение:
//
//  1. снятое объявление режима у боевого стенда — НАХОДКА, и находка называет
//     ИМЯ СТЕНДА (находка, не называющая где, посылает читателя искать по всему
//     дереву);
//  2. законные близнецы — МОЛЧАНИЕ, и их три, по одному на каждое основание не
//     судить: режим объявлен верно · посадка не боевая · ребро не поднято;
//  3. распознаватель шаблона знает форму записи предмета и ОТКАЗЫВАЕТСЯ, когда
//     не знает, — а не молчит. Форма, о которой распознаватель не знает, не даёт
//     ни красного, ни зелёного: всё записанное в ней оказывается вне наблюдения
//     (testing.md §«Гейт на класс», п. 7). Именно этим был слеп соседний гейт, и
//     именно поэтому этот заведён.
package deploy_test

import (
	"strings"
	"testing"
)

// injectedEdge — ребро под стражем взаимного режима.
//
// Значения синтетические и с деревом НЕ совпадают намеренно: совпадение сделало
// бы доказательство чувствительным к правке профилей, к которой оно отношения не
// имеет. При этом ФОРМА имён взята настоящая (латиница, разделитель `_` у
// переменной процесса, camelCase у ключа профиля) — распознаватель судит форму,
// и фикстура, написанная в форме, которой в дереве не бывает, доказывала бы
// работу распознавателя на входе, которого он никогда не увидит.
func injectedEdge() guardedEdge {
	return guardedEdge{
		edge:      "SynthEdge",
		guardFile: "synth/guard.go",
		required:  "synth-mutual",
		envSuffix: "SYNTH_SERVER_MTLS_CLIENTAUTHMODE",
		envName:   "SYNTHPRODUCT_SYNTH_SERVER_MTLS_CLIENTAUTHMODE",
		surface:   "SYNTH",
		knob:      "synthEdge",
		fallback:  "synthAllHTTP",
		valuesKey: "synthEdgeClientAuthMode",
	}
}

// legalParityStack — боевой стенд, поднявший ребро и объявивший ТРЕБУЕМЫЙ режим.
// Положительный контроль: без него отрицание зеленело бы на всём сломанном.
func legalParityStack() stackModeFacts {
	return stackModeFacts{
		stack: "боевой", production: true, transport: true,
		declared: true, mode: "synth-mutual",
	}
}

func TestClientAuthParityJudgement_CanFailAndStaysSilent(t *testing.T) {
	edge := injectedEdge()

	cases := []struct {
		name        string
		mutate      func(s *stackModeFacts)
		wantFinding bool
		wantKind    string
		wantWords   []string
	}{
		{
			name:   "законный близнец: режим объявлен требуемым — молчание",
			mutate: func(*stackModeFacts) {},
		},
		{
			name:        "профиль ПЕРЕСТАЛ объявлять режим — находка",
			mutate:      func(s *stackModeFacts) { s.declared = false; s.mode = "" },
			wantFinding: true,
			wantKind:    "режим не объявлен",
			wantWords:   []string{"боевой", "synthEdgeClientAuthMode", "synth-mutual"},
		},
		{
			name:        "режим объявлен ЗАПРАШИВАЮЩИМ вместо взаимного — находка",
			mutate:      func(s *stackModeFacts) { s.mode = "synth-optional" },
			wantFinding: true,
			wantKind:    "режим не взаимный",
			wantWords:   []string{"боевой", "synth-optional", "synth-mutual"},
		},
		{
			name:        "режим объявлен с опечаткой — находка (значение сверяется, а не наличие)",
			mutate:      func(s *stackModeFacts) { s.mode = "synth-mutuall" },
			wantFinding: true,
			wantKind:    "режим не взаимный",
			wantWords:   []string{"боевой", "synth-mutuall"},
		},
		{
			name:   "законный близнец: посадка НЕ боевая — молчание (страж там no-op)",
			mutate: func(s *stackModeFacts) { s.production = false; s.declared = false; s.mode = "" },
		},
		{
			name:   "законный близнец: ребро НЕ поднято — молчание (переменной режима не бывает)",
			mutate: func(s *stackModeFacts) { s.transport = false; s.declared = false; s.mode = "" },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := legalParityStack()
			c.mutate(&s)
			got := judgeClientAuthParity(edge, []stackModeFacts{s})

			if !c.wantFinding {
				if len(got) != 0 {
					t.Fatalf("законный близнец дал находку %q — %s", got[0].kind, got[0].detail)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("ждали ровно одну находку, получили %d", len(got))
			}
			f := got[0]
			if f.kind != c.wantKind {
				t.Errorf("род находки %q, ждали %q", f.kind, c.wantKind)
			}
			if f.stack != s.stack {
				t.Errorf("находка не называет стенд: %q, ждали %q", f.stack, s.stack)
			}
			if f.edge != edge.edge {
				t.Errorf("находка не называет ребро: %q, ждали %q", f.edge, edge.edge)
			}
			for _, w := range c.wantWords {
				if !strings.Contains(f.stack+" "+f.detail, w) {
					t.Errorf("находка не называет %q — читателю негде искать; текст: %s", w, f.detail)
				}
			}
		})
	}
}

// TestClientAuthParityJudgement_OneFactSeparatesTheVerdicts — различие между
// «находка» и «молчание» держится РОВНО ОДНИМ фактом, и это проверяется, а не
// объявляется: три пары ниже отличаются одним полем каждая.
func TestClientAuthParityJudgement_OneFactSeparatesTheVerdicts(t *testing.T) {
	edge := injectedEdge()
	broken := legalParityStack()
	broken.declared, broken.mode = false, ""

	pairs := []struct {
		fact  string
		quiet stackModeFacts
	}{
		{"посадка", func() stackModeFacts { s := broken; s.production = false; return s }()},
		{"транспорт", func() stackModeFacts { s := broken; s.transport = false; return s }()},
		{"объявление", legalParityStack()},
	}
	if n := len(judgeClientAuthParity(edge, []stackModeFacts{broken})); n != 1 {
		t.Fatalf("сломанный вход не дал находки (%d) — доказательство беспредметно", n)
	}
	for _, p := range pairs {
		if n := len(judgeClientAuthParity(edge, []stackModeFacts{p.quiet})); n != 0 {
			t.Errorf("смена одного факта %q не вернула молчания (находок %d)", p.fact, n)
		}
	}
}

// TestClientAuthParityBinding_KnowsTheFormAndRefusesWhenItDoesNot — связывание
// стороны стража со стороной шаблона обязано ОТКАЗЫВАТЬ на входе, формы которого
// оно не знает, а не молчать.
//
// Это несущая половина доказательства. Гейт, чей распознаватель форму не узнал,
// не краснеет — он ПРОХОДИТ, потому что судить ему становится нечего; так
// выглядит проверка, снятая молча.
func TestClientAuthParityBinding_KnowsTheFormAndRefusesWhenItDoesNot(t *testing.T) {
	knobs := []knobFacts{{knob: "synthEdge", fallback: "synthAllHTTP", surfaces: []string{"SYNTH"}}}

	const legal = `
            {{- if (dig "synthEdge" .Values.mtls.synthAllHTTP .Values.mtls) }}
            - name: SYNTHPRODUCT_SYNTH_SERVER_MTLS_ENABLE
              value: "true"
            - name: SYNTHPRODUCT_SYNTH_SERVER_MTLS_CLIENTAUTHMODE
              value: {{ .Values.mtls.synthEdgeClientAuthMode | default "synth-other" | quote }}
            {{- end }}
`

	base := func() guardedEdge {
		return guardedEdge{edge: "SynthEdge", required: "synth-mutual",
			envSuffix: "SYNTH_SERVER_MTLS_CLIENTAUTHMODE"}
	}

	t.Run("законный вход — связывается, и все три координаты выведены", func(t *testing.T) {
		e := base()
		if err := bindEdgeToTemplate(&e, knobs, legal, "синт.yaml"); err != nil {
			t.Fatalf("законный вход не связался: %v", err)
		}
		if e.knob != "synthEdge" || e.valuesKey != "synthEdgeClientAuthMode" ||
			e.envName != "SYNTHPRODUCT_SYNTH_SERVER_MTLS_CLIENTAUTHMODE" {
			t.Fatalf("координаты выведены неверно: ручка %q · ключ %q · переменная %q",
				e.knob, e.valuesKey, e.envName)
		}
	})

	refusals := []struct {
		name     string
		template string
		knobs    []knobFacts
		edge     func() guardedEdge
		words    string
	}{
		{
			name:     "переменной с этим хвостом в шаблоне НЕТ — отказ, а не молчание",
			template: strings.Replace(legal, "SYNTH_SERVER_MTLS_CLIENTAUTHMODE", "SYNTH_SERVER_MTLS_OTHERTAIL", 1),
			knobs:    knobs, edge: base, words: "не связывается",
		},
		{
			name:     "переменная есть, но заполняется не из профиля — отказ",
			template: strings.Replace(legal, `{{ .Values.mtls.synthEdgeClientAuthMode | default "synth-other" | quote }}`, `"synth-mutual"`, 1),
			knobs:    knobs, edge: base, words: "ключ профиля не выводится",
		},
		{
			name:     "поверхность не накрыта НИ ОДНОЙ ручкой транспорта — отказ",
			template: legal, knobs: []knobFacts{{knob: "other", fallback: "synthAllHTTP", surfaces: []string{"FOREIGN"}}},
			edge: base, words: "не накрыта",
		},
		{
			name:     "хвост носят ДВЕ переменные — отказ, а не выбор наугад",
			template: legal + "\n            - name: SECOND_SYNTH_SERVER_MTLS_CLIENTAUTHMODE\n",
			knobs:    knobs, edge: base, words: "ДВЕ переменные",
		},
		{
			name:     "тег объявления не разбирается на поверхность и хвост — отказ",
			template: legal, knobs: knobs,
			edge:  func() guardedEdge { e := base(); e.envSuffix = "NOSEPARATOR"; return e },
			words: "не разбирается",
		},
	}
	for _, r := range refusals {
		t.Run(r.name, func(t *testing.T) {
			e := r.edge()
			err := bindEdgeToTemplate(&e, r.knobs, r.template, "синт.yaml")
			if err == nil {
				t.Fatalf("вход, формы которого распознаватель не знает, связался МОЛЧА — "+
					"выведено: ручка %q · ключ %q · переменная %q", e.knob, e.valuesKey, e.envName)
			}
			if !strings.Contains(err.Error(), r.words) {
				t.Errorf("отказ не называет предмет %q; текст: %v", r.words, err)
			}
		})
	}
}
