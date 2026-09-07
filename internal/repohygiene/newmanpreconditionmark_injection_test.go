// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanpreconditionmark_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) сними метку у стража — гейт краснеет и НАЗЫВАЕТ набор, число и координату;
// (б) оставь метку — гейт молчит, в том числе на наборе, у которого стражей нет
//
//	вовсе (предмета нет — значит и находки нет).
//
// Отдельно доказывается ось СОГЛАСИЯ: два разных текста метки в дереве — находка,
// называющая оба текста и их владельцев. Без этой стороны набор мог бы нести
// метку, которую вердикт не читает, и выглядеть исправным.
//
// Отдельно — АНТИМАСКА адъюдикации: набор с непомеченными стражами не перестаёт
// быть находкой оттого, что рядом нашлось расхождение текстов. Категория,
// поглощающая соседнюю находку, была бы маской.
//
// И отдельно — что предикаты чтения находят свой предмет в НАСТОЯЩЕЙ форме,
// которую пишет производитель, и НЕ находят его в объясняющем комментарии
// рядом: гейт по подстроке остался бы зелёным на снятой метке, покраснев на
// собственном объяснении.
package repohygiene

import (
	"strings"
	"testing"
)

const injMark = "[УСЛОВИЕ НЕ СОЗДАНО]"

// injCoords — n координат непомеченных стражей. Число берётся из длины
// перечня, а не из отдельного счётчика: у гейта его тоже нет.
func injCoords(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "cluster.json :: list-clusters")
	}
	return out
}

func injHealthyPrecondRoots() []newmanPrecondRoot {
	return []newmanPrecondRoot{
		{Root: "gateway/tests/newman", Declarations: []string{injMark}, Guards: 38},
		{Root: "services/iam/tests/newman", Declarations: []string{injMark}, Guards: 2264},
		{Root: "services/vpc/tests/newman", Declarations: []string{injMark}, Guards: 378},
	}
}

func TestNewmanPrecondMark_ProvenByInjection(t *testing.T) {
	t.Run("законный близнец: все объявили и все помечены — гейт молчит", func(t *testing.T) {
		if found := adjudicateNewmanPrecondMark(injHealthyPrecondRoots()); len(found) != 0 {
			t.Fatalf("ложное срабатывание на исправном дереве: %v", found)
		}
	})

	t.Run("законный близнец: набор БЕЗ стражей и без объявления — предмета нет", func(t *testing.T) {
		roots := append(injHealthyPrecondRoots(),
			newmanPrecondRoot{Root: "services/quiet/tests/newman"})
		if found := adjudicateNewmanPrecondMark(roots); len(found) != 0 {
			t.Fatalf("гейт предписывает работу набору, у которого предмета нет: %v", found)
		}
	})

	t.Run("набор не объявляет метку при живых стражах — краснеет и называет ЕГО", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[2].Declarations = nil
		roots[2].Unmarked = injCoords(378)
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		if !strings.HasPrefix(found[0], "services/vpc/tests/newman:") {
			t.Fatalf("обвиняемым назван не тот набор:\n%s", found[0])
		}
		if !strings.Contains(found[0], "378") {
			t.Fatalf("находка не называет ЧИСЛО стражей, оставшихся без категории:\n%s", found[0])
		}
		if !strings.Contains(found[0], "PRECONDITION_MARK") {
			t.Fatalf("находка не называет предмет правки — производителя метки:\n%s", found[0])
		}
	})

	t.Run("объявил, но часть стражей не помечена — число и координаты", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[0].Unmarked = []string{"cluster.json :: list-clusters", "cluster.json :: get-cluster"}
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		if !strings.HasPrefix(found[0], "gateway/tests/newman:") ||
			!strings.Contains(found[0], "2 из 38") {
			t.Fatalf("находка не называет набор и обе величины:\n%s", found[0])
		}
		if !strings.Contains(found[0], "cluster.json :: get-cluster") {
			t.Fatalf("находка не называет координату — читатель пойдёт искать не там:\n%s", found[0])
		}
	})

	t.Run("перечень координат усекается, а ЧИСЛО — нет", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[2].Unmarked = injCoords(378)
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 1 || !strings.Contains(found[0], "378 из 378") {
			t.Fatalf("полное число не названо: %v", found)
		}
		if !strings.Contains(found[0], "и ещё 373") {
			t.Fatalf("усечение перечня не объявлено — читатель примет часть за всё:\n%s", found[0])
		}
	})

	t.Run("СОГЛАСИЕ: два текста метки — находка с обоими текстами и владельцами", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[2].Declarations = []string{"[PRECONDITION NOT MET]"}
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка о согласии, получено %d: %v", len(found), found)
		}
		if !strings.Contains(found[0], "PRECONDITION NOT MET") ||
			!strings.Contains(found[0], "УСЛОВИЕ НЕ СОЗДАНО") {
			t.Fatalf("находка не называет ОБА текста:\n%s", found[0])
		}
		if !strings.Contains(found[0], "services/vpc/tests/newman") {
			t.Fatalf("находка не называет владельца расходящегося текста:\n%s", found[0])
		}
	})

	t.Run("два объявления в одном наборе — находка", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[1].Declarations = []string{injMark, injMark}
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 1 || !strings.Contains(found[0], "производителей метки третьего исхода 2") {
			t.Fatalf("второе объявление в наборе не опознано: %v", found)
		}
	})

	// АНТИМАСКА: одна категория находок не поглощает другую.
	t.Run("АНТИМАСКА: расхождение текстов не гасит непомеченных стражей", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[2].Declarations = []string{"[PRECONDITION NOT MET]"}
		roots[0].Unmarked = injCoords(8)
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 2 {
			t.Fatalf("ожидались ОБЕ находки, получено %d: %v", len(found), found)
		}
		joined := strings.Join(found, "\n")
		if !strings.Contains(joined, "текстов метки третьего исхода в дереве 2") {
			t.Fatalf("находка о согласии поглощена:\n%s", joined)
		}
		if !strings.Contains(joined, "8 из 38") {
			t.Fatalf("находка о непомеченных стражах поглощена:\n%s", joined)
		}
	})

	t.Run("кейс ВЫПИСАЛ текст метки литералом — находка с его координатой", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[2].CasesWriting = []string{"services/vpc/tests/newman/cases/subnet.py"}
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		if !strings.Contains(found[0], "cases/subnet.py") ||
			!strings.Contains(found[0], "ВЫПИСЫВАЕТ") {
			t.Fatalf("находка не называет файл кейса и предмет:\n%s", found[0])
		}
		if !strings.Contains(found[0], "PRECONDITION_MARK") {
			t.Fatalf("находка не называет санкционированный путь — куда идти автору:\n%s", found[0])
		}
	})

	// АНТИМАСКА второй оси: кейс-нарушитель не гасит непомеченных стражей рядом.
	t.Run("АНТИМАСКА: литерал в кейсе не гасит непомеченных стражей", func(t *testing.T) {
		roots := injHealthyPrecondRoots()
		roots[2].CasesWriting = []string{"services/vpc/tests/newman/cases/subnet.py"}
		roots[0].Unmarked = injCoords(4)
		found := adjudicateNewmanPrecondMark(roots)
		if len(found) != 2 {
			t.Fatalf("ожидались ОБЕ находки, получено %d: %v", len(found), found)
		}
		joined := strings.Join(found, "\n")
		if !strings.Contains(joined, "ВЫПИСЫВАЕТ") || !strings.Contains(joined, "4 из 38") {
			t.Fatalf("одна из находок поглощена:\n%s", joined)
		}
	})

	// ПРЕДИКАТ ОБЪЯВЛЕНИЯ находит присвоение и НЕ находит прозу о нём.
	t.Run("предикат объявления: присвоение — да, упоминание — нет", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want string
			ok   bool
		}{
			{"двойные кавычки", "PRECONDITION_MARK = \"[УСЛОВИЕ НЕ СОЗДАНО]\"\n", "[УСЛОВИЕ НЕ СОЗДАНО]", true},
			{"одинарные кавычки", "PRECONDITION_MARK = '[X]'\n", "[X]", true},
			{"без пробелов", "PRECONDITION_MARK=\"[X]\"\n", "[X]", true},
			{"проза об имени константы", "# метку ставит PRECONDITION_MARK выше\n", "", false},
			{"чтение, а не присвоение", "    return f\"{PRECONDITION_MARK} {title}\"\n", "", false},
			{"присвоение с отступом — не объявление модуля", "    PRECONDITION_MARK = \"[X]\"\n", "", false},
			{"присвоение выражением, а не литералом", "PRECONDITION_MARK = mark()\n", "", false},
		}
		for _, c := range cases {
			m := reNewmanPrecondDecl.FindStringSubmatch(c.body)
			if (m != nil) != c.ok {
				t.Errorf("%s: предикат вернул %v, ждали %v на\n\t%s", c.name, m != nil, c.ok, c.body)
				continue
			}
			if !c.ok {
				continue
			}
			got := m[1]
			if got == "" {
				got = m[2]
			}
			if got != c.want {
				t.Errorf("%s: прочитан текст %q, ждали %q", c.name, got, c.want)
			}
		}
	})

	// ПРЕДИКАТ СТРАЖА находит настоящую форму и НЕ находит комментарий.
	t.Run("предикат стража: исполняемое — да, комментарий — нет", func(t *testing.T) {
		cases := []struct {
			name   string
			line   string
			isTest bool
		}{
			{"настоящая форма из порождённой коллекции",
				`  pm.test('harness config: jwtNoBindings is set (subject under test)', () => {`, true},
			{"с меткой третьего исхода",
				`  pm.test('[УСЛОВИЕ НЕ СОЗДАНО] harness config: internalBaseUrl is set', () => {`, true},
			{"ОБЪЯСНЕНИЕ рядом — не страж",
				`// HARNESS-CONFIG GUARD — pm.test с фразой harness config: помечается меткой`, false},
			{"утверждение без фразы — не страж настройки харнесса",
				`  pm.test('operation reached done', () => {`, false},
			{"фраза без утверждения — не страж",
				`  console.log('harness config: ' + v);`, false},
		}
		for _, c := range cases {
			trimmed := strings.TrimSpace(c.line)
			got := !strings.HasPrefix(trimmed, "//") &&
				reNewmanPmTest.MatchString(c.line) &&
				strings.Contains(c.line, newmanHarnessGuardDecl)
			if got != c.isTest {
				t.Errorf("%s: предикат вернул %v, ждали %v на\n\t%s", c.name, got, c.isTest, c.line)
			}
		}
	})
}
