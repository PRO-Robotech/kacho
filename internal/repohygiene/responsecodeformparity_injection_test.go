// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что проверка согласия двух разборов СПОСОБНА упасть — и
// что падает она на существе, а не на форме.
//
// Инъекция идёт в обе стороны по КАЖДОЙ оси, потому что одного «краснеет» мало
// (проверка, краснеющая на всём, ничего не измеряет), и одного «молчит» тоже мало
// (молчание бывает от того, что читать не стали):
//
//	граница = строка вместо `;`          → КРАСНЕЕТ на форме с переносом;
//	граница = `;`                        → молчит (законный близнец);
//	только `eql`, без `equal`            → КРАСНЕЕТ на втором написании;
//	оба написания                        → молчит;
//	привязки к `pm.response.code` нет    → КРАСНЕЕТ на соседнем стейтменте о gRPC;
//	привязка есть                        → молчит;
//	отрицание принято за принятие        → КРАСНЕЕТ на `to.not.equal(401)`;
//	отрицание исключено                  → молчит.
//
// Проба гоняет ТО ЖЕ ядро (`acceptedResponseCodes`) и ТОТ ЖЕ корпус, что и
// проверка по дереву: проба, повторяющая логику своей копией, доказывала бы
// свойство копии.
package repohygiene_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// parseWith — разбор ТОЙ ЖЕ формы, что в гейте, но с подменяемыми регулярками:
// так инъекция возвращает НАСТОЯЩИЙ дефект (прежнюю редакцию предиката), а не
// изображает его.
func parseWith(body string, eq, oneOf *regexp.Regexp, perLine bool) []int {
	codes := map[int]bool{}
	chunks := []string{body}
	if perLine {
		chunks = strings.Split(body, "\n")
	}
	for _, ch := range chunks {
		// Прежняя редакция брала коды ТОЛЬКО со строк, упоминающих `response.code`.
		// Без этого условия инъекция изображала бы дефект, а не возвращала его:
		// продолжение выражения (`  .to.eql(400));`) само по себе читается, и
		// «построчный» разбор выглядел бы исправным.
		if perLine && !strings.Contains(ch, "response.code") {
			continue
		}
		for _, m := range eq.FindAllStringSubmatch(ch, -1) {
			codes[atoiSafe(m[1])] = true
		}
		for _, m := range oneOf.FindAllStringSubmatch(ch, -1) {
			for _, c := range respCodeAnyRe.FindAllString(m[1], -1) {
				codes[atoiSafe(c)] = true
			}
		}
	}
	out := make([]int, 0, len(codes))
	for c := range codes {
		if c >= 100 && c <= 599 {
			out = append(out, c)
		}
	}
	sort.Ints(out)
	return out
}

// formLines — форма корпуса по имени. Инъекция обязана бить по ТОЙ ЖЕ форме,
// которой судится дерево, иначе она доказывает свойство выдумки.
func formLines(t *testing.T, id string) (string, []int) {
	t.Helper()
	corpus := loadResponseCodeCorpus(t, repoRootFor(t))
	for _, f := range corpus.Forms {
		if f.ID == id {
			want := append([]int(nil), f.Accepted...)
			sort.Ints(want)
			return strings.Join(f.Lines, "\n"), want
		}
	}
	t.Fatalf("формы %q в корпусе нет — инъекция била бы по выдумке", id)
	return "", nil
}

// ДЕФЕКТ 1 — построчная граница (прежняя редакция гейта). На форме с переносом
// внутри выражения она не читает НИ ОДНОГО кода, поэтому шаг, утверждающий отказ,
// становится «успешным созданием».
func TestFormParityRedsWhenTheBoundaryIsTheLineNotTheStatement(t *testing.T) {
	t.Parallel()

	body, want := formLines(t, "eql-split-across-lines")
	require.Equal(t, []int{400}, want, "корпус обязан объявлять по этой форме отказ")

	broken := parseWith(body,
		regexp.MustCompile(`to\.(?:be\.)?(?:eql|equal)\(\s*(\d{3})`),
		regexp.MustCompile(`oneOf\(\[([0-9,\s]+)]`), true)
	require.NotEqual(t, fmt.Sprint(want), fmt.Sprint(broken),
		"построчный разбор обязан РАЗОЙТИСЬ с корпусом — иначе проверка не измеряет "+
			"границу вовсе")
	require.Empty(t, broken, "он не читает ни одного кода — отсюда и ложная находка об утечке")

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же форма, действующая граница — молчит.
	require.Equal(t, fmt.Sprint(want),
		fmt.Sprint(sortedCodes(acceptedResponseCodes(body))),
		"действующий разбор обязан сойтись с корпусом")
}

// ДЕФЕКТ 2 — читается только `eql`. Ровно этим болел генератор: 724 вхождения
// `to.equal(` были ему невидимы, и шаги, УТВЕРЖДАЮЩИЕ 403, оборачивались ретраем
// по 403.
func TestFormParityRedsWhenOnlyOneSpellingOfEqualityIsRead(t *testing.T) {
	t.Parallel()

	body, want := formLines(t, "equal-spelling")
	require.Equal(t, []int{403}, want)

	broken := parseWith(body,
		regexp.MustCompile(`pm\.response\.code[^;]*?\.to\.eql\((\d{3})\)`),
		respCodeOneOf, false)
	require.Empty(t, broken,
		"разбор одного написания обязан ПРОМОЛЧАТЬ о 403 — это и есть дефект: шаг, "+
			"утверждающий отказ, выглядит ничего не утверждающим")

	require.Equal(t, fmt.Sprint(want),
		fmt.Sprint(sortedCodes(acceptedResponseCodes(body))))
}

// ДЕФЕКТ 3 — набор кодов не привязан к `pm.response.code`. Тогда соседний
// стейтмент о gRPC-коде тела подмешивается в набор HTTP-исходов.
func TestFormParityRedsWhenTheCodeSetIsNotAnchoredToTheResponseCode(t *testing.T) {
	t.Parallel()

	body, want := formLines(t, "neighbouring-statement-not-bled-in")
	require.Equal(t, []int{400}, want)

	broken := parseWith(body,
		regexp.MustCompile(`to\.(?:be\.)?(?:eql|equal)\(\s*(\d{3})`),
		regexp.MustCompile(`oneOf\(\[([0-9,\s]+)]`), false)
	require.Contains(t, broken, 200,
		"непривязанный разбор обязан втянуть gRPC-код 200 — из-за этого отвергаемое "+
			"создание читалось бы успешным")

	require.Equal(t, fmt.Sprint(want),
		fmt.Sprint(sortedCodes(acceptedResponseCodes(body))))
}

// ДЕФЕКТ 4 — отрицание принято за принятие. `to.not.equal(401)` означает «что
// угодно, кроме 401»; прочитать это как «401 приемлем» значит перевернуть смысл.
func TestFormParityRedsWhenNegationIsReadAsAcceptance(t *testing.T) {
	t.Parallel()

	body, want := formLines(t, "negated-equality-is-not-acceptance")
	require.Empty(t, want, "корпус обязан объявлять по этой форме ПУСТОЙ набор")

	broken := parseWith(body,
		regexp.MustCompile(`pm\.response\.code[^;]*?(?:eql|equal)\((\d{3})\)`),
		respCodeOneOf, false)
	require.Contains(t, broken, 401,
		"разбор, не различающий отрицания, обязан втянуть 401 — иначе проверка не "+
			"измеряет эту ось")

	require.Empty(t, sortedCodes(acceptedResponseCodes(body)),
		"действующий разбор обязан промолчать: отрицание принятием не является")
}

// ЗАКОННЫЙ БЛИЗНЕЦ ОБЩИЙ — на ВСЁМ корпусе действующее ядро сходится с
// объявленным. Без этой половины четыре отрицания выше зеленели бы и на разборе,
// который не читает ничего.
func TestFormParityCoreAgreesWithTheWholeCorpus(t *testing.T) {
	t.Parallel()

	corpus := loadResponseCodeCorpus(t, repoRootFor(t))
	nonEmpty := 0
	for _, f := range corpus.Forms {
		want := append([]int(nil), f.Accepted...)
		sort.Ints(want)
		if len(want) > 0 {
			nonEmpty++
		}
		got := sortedCodes(acceptedResponseCodes(strings.Join(f.Lines, "\n")))
		require.Equalf(t, fmt.Sprint(want), fmt.Sprint(got),
			"форма %q: %s", f.ID, f.Why)
	}
	require.Positive(t, nonEmpty,
		"корпус, состоящий из одних пустых наборов, зеленел бы на разборе, который "+
			"не читает ничего")
	t.Logf("перепись: форм %d, из них с непустым набором %d", len(corpus.Forms), nonEmpty)
}
