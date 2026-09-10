// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_front_leaf_pair_declared_injection_test.go — доказательство того, что
// сверка «ребро требует лист ↔ стенд объявляет его производителя» СПОСОБНА
// упасть и способна смолчать.
//
// Вход СИНТЕТИЧЕСКИЙ, а не подделка дерева: подделка трогает общий клон, а
// вердикт обязан доказываться на входе, построенном здесь и целиком видном
// читателю. Тот же порядок, что у соседних
// internal_rest_client_auth_parity_injection_test.go и
// client_identity_leaf_injection_test.go.
//
// КАЖДЫЙ СЛУЧАЙ МЕНЯЕТ РОВНО ОДИН ФАКТ против законного близнеца. Инъекция,
// меняющая два, доказывает лишь то, что покраснело что-то: который из двух дал
// вердикт — неизвестно (testing.md §«Гейт на класс», п. 2 и change-graph.md §6).
//
// Доказываются ТРИ вещи, и третья — не украшение:
//
//  1. снятый производитель у стенда, требующего лист, — НАХОДКА, и находка
//     называет ИМЯ СТЕНДА;
//  2. законные близнецы — МОЛЧАНИЕ, и их три, по одному на каждое основание не
//     судить: пара объявлена целиком · лист не требуется, материала нет ·
//     лист не требуется, имя чужое;
//  3. распознаватель знает форму записи предмета и НЕ отвечает на форму, которой
//     прогонщик не понимает. Продолженное имя флага (`--ssl-client-cert-xx`)
//     листа не предъявляет; приняв его за предъявление, проверка зеленела бы на
//     сломанном прогонщике (testing.md §«Гейт на класс», п. 7).
package deploy_test

import (
	"strings"
	"testing"
)

// legalLeafStack — стенд, требующий лист и объявивший его производителя.
// Положительный контроль: без него отрицание зеленело бы на всём сломанном.
//
// Имя секрета синтетическое и с деревом НЕ совпадает намеренно: совпадение
// сделало бы доказательство чувствительным к правке профилей, к которой оно
// отношения не имеет. ФОРМА имени взята настоящая — распознаватель судит форму.
func legalLeafStack() leafStackFacts {
	return leafStackFacts{
		stack:        "synthetic-prod",
		leafRequired: true,
		credEnabled:  true,
		credName:     "synthetic-client-tls",
	}
}

const wantLeafSecret = "synthetic-client-tls"

func TestOwnFrontLeafPairJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		facts   leafStackFacts
		finding bool
		kind    string
	}{
		{
			name:    "производителя нет: ребро взаимное, выпуск листа снят",
			facts:   func() leafStackFacts { f := legalLeafStack(); f.credEnabled = false; return f }(),
			finding: true,
			kind:    "удостоверения нет вовсе",
		},
		{
			name:    "имя расходится: выпуск включён, секрет не тот",
			facts:   func() leafStackFacts { f := legalLeafStack(); f.credName = "other-client-tls"; return f }(),
			finding: true,
			kind:    "имя расходится",
		},
		{
			name:    "законный близнец: пара объявлена целиком",
			facts:   legalLeafStack(),
			finding: false,
		},
		{
			name:    "законный близнец: лист не требуется — материала и не должно быть",
			facts:   func() leafStackFacts { f := legalLeafStack(); f.leafRequired, f.credEnabled = false, false; return f }(),
			finding: false,
		},
		{
			name: "законный близнец: лист не требуется — чужое имя ничего не решает",
			facts: func() leafStackFacts {
				f := legalLeafStack()
				f.leafRequired, f.credName = false, "other-client-tls"
				return f
			}(),
			finding: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := judgeOwnFrontLeafPair(wantLeafSecret, []leafStackFacts{c.facts})
			if !c.finding {
				if len(got) != 0 {
					t.Fatalf("законный близнец дал находку %+v — проверка судит не тот факт", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("ожидалась ровно одна находка, получено %d (%+v)", len(got), got)
			}
			if got[0].kind != c.kind {
				t.Fatalf("вид находки %q, ожидался %q", got[0].kind, c.kind)
			}
			// Находка, не называющая ГДЕ она, посылает читателя искать по всему дереву.
			if got[0].stack != c.facts.stack {
				t.Fatalf("находка не называет стенд: %q вместо %q", got[0].stack, c.facts.stack)
			}
			if !strings.Contains(got[0].detail, wantLeafSecret) {
				t.Fatalf("находка не называет секрет, который спросит прогонщик: %q", got[0].detail)
			}
		})
	}
}

// TestOwnFrontLeafPairJudgement_OneFactSeparatesTheVerdicts — различие между
// «находка» и «молчание» держится РОВНО ОДНИМ фактом, и это проверяется, а не
// объявляется.
func TestOwnFrontLeafPairJudgement_OneFactSeparatesTheVerdicts(t *testing.T) {
	pairs := []struct {
		name  string
		red   leafStackFacts
		green leafStackFacts
	}{
		{
			name:  "выпуск листа",
			red:   func() leafStackFacts { f := legalLeafStack(); f.credEnabled = false; return f }(),
			green: legalLeafStack(),
		},
		{
			name:  "имя секрета",
			red:   func() leafStackFacts { f := legalLeafStack(); f.credName = "other-client-tls"; return f }(),
			green: legalLeafStack(),
		},
		{
			name:  "требование листа",
			red:   func() leafStackFacts { f := legalLeafStack(); f.credEnabled = false; return f }(),
			green: func() leafStackFacts { f := legalLeafStack(); f.credEnabled, f.leafRequired = false, false; return f }(),
		},
	}

	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			if d := leafFactsDelta(p.red, p.green); d != 1 {
				t.Fatalf("пара отличается %d фактами, а не одним — вердикт нельзя приписать "+
					"проверяемому факту", d)
			}
			if len(judgeOwnFrontLeafPair(wantLeafSecret, []leafStackFacts{p.red})) == 0 {
				t.Fatalf("красная сторона пары молчит")
			}
			if got := judgeOwnFrontLeafPair(wantLeafSecret, []leafStackFacts{p.green}); len(got) != 0 {
				t.Fatalf("зелёная сторона пары дала находку %+v", got)
			}
		})
	}
}

// leafFactsDelta — число РАЗЛИЧАЮЩИХСЯ фактов. Дельта вычисляется, а не
// объявляется: объявленная разошлась бы с фикстурой молча.
func leafFactsDelta(a, b leafStackFacts) int {
	d := 0
	if a.leafRequired != b.leafRequired {
		d++
	}
	if a.credEnabled != b.credEnabled {
		d++
	}
	if a.credName != b.credName {
		d++
	}
	return d
}

// TestOwnFrontLeafRecognisers_KnowTheFormAndRefuseTheRest — распознаватели
// судят ИСПОЛНЯЕМУЮ форму и не отвечают ни прозе, ни флагу, которого прогонщик
// не понимает.
func TestOwnFrontLeafRecognisers_KnowTheFormAndRefuseTheRest(t *testing.T) {
	t.Run("предъявление: настоящий флаг узнан", func(t *testing.T) {
		if !presentsClientLeaf.MatchString(`  ARGS=(--ssl-client-cert "$d/client.crt")`) {
			t.Fatal("настоящая форма предъявления не узнана — всё записанное в ней вне осмотра")
		}
	})

	// ЗАКОННЫЙ БЛИЗНЕЦ ГРАНИЦЫ ИМЕНИ. Продолженное имя — не флаг newman: лист по
	// нему не уедет. Приняв его за предъявление, проверка объявила бы сломанный
	// прогонщик исправным. Через `\b` этот случай проходил бы как настоящий.
	t.Run("предъявление: продолженное имя флага НЕ узнано", func(t *testing.T) {
		if presentsClientLeaf.MatchString(`  ARGS=(--ssl-client-cert-xx "$d/client.crt")`) {
			t.Fatal("продолженное имя принято за предъявление — переименование флага " +
				"осталось бы незамеченным")
		}
	})

	t.Run("секрет: имя берётся у обращения, а не у прозы", func(t *testing.T) {
		body := `  echo "лист взят из secret/prose-only-tls"` + "\n" +
			`  kubectl -n "$NS" get secret real-client-tls -o jsonpath='{.data.tls\.crt}'`
		hits := fetchesSecret.FindAllStringSubmatch(body, -1)
		if len(hits) != 1 {
			t.Fatalf("обращений за секретом найдено %d, ожидалось одно: %v", len(hits), hits)
		}
		if hits[0][1] != "real-client-tls" {
			t.Fatalf("снято имя %q — распознаватель прочитал прозу вместо обращения", hits[0][1])
		}
	})

	t.Run("функции: тело берётся до закрытия в начале строки", func(t *testing.T) {
		src := "before() {\n  noise\n}\n" +
			"leaf() {\n  kubectl get secret real-client-tls\n  ARGS=(--ssl-client-cert x)\n}\n" +
			"after() {\n  kubectl get secret foreign-tls\n}\n"
		fns := shellFunctions(src)
		if len(fns) != 3 {
			t.Fatalf("функций разобрано %d, ожидалось три: %v", len(fns), keysOfLeafFns(fns))
		}
		// Область — ФУНКЦИЯ, а не файл: соседняя берёт ЧУЖОЙ секрет, и файловая
		// область смешала бы два предмета.
		if strings.Contains(fns["leaf"], "foreign-tls") {
			t.Fatal("в тело функции затёк чужой секрет соседней — область разбора не функция")
		}
		if !strings.Contains(fns["leaf"], "real-client-tls") {
			t.Fatal("тело функции, предъявляющей лист, разобрано не полностью")
		}
	})
}

func keysOfLeafFns(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
