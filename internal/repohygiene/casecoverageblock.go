// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// casecoverageblock.go — разбор блока состава в шапке модуля кейсов newman.
//
// # Предмет
//
// Шапка модуля кейсов вправе нести блок `Coverage:` — перечень кейсов с
// описанием каждого. Перечень этот ВЫПИСАН и потому не имеет владельца: правит
// его тот, кто наткнётся, а модуль растёт коммитами, которые до шапки не
// доходят. Число стареет молча и утаскивает за собой выводы, сделанные из него:
// читатель, планирующий работу по составу набора, недосчитается позиций.
//
// Замер, из которого разбор заведён (#2207): блок несут ДВА модуля из ста, и
// РАСХОДЯТСЯ ОБА — 9 из 14 у одного, 15 из 16 у другого.
//
// # Почему блок не снимается, раз рядом есть машинный указатель
//
// `docs/CASES-INDEX.md` перечисляет те же идентификаторы, выводится из дерева и
// держится своим сверщиком — но описаний он не несёт BY CONSTRUCTION, и его
// собственная шапка это заявляет: «описания у каждого кейса нет — оно живёт в
// шапке своего модуля». Снять блок значило бы потерять то, чего больше нигде
// нет. Вывести его нельзя — это проза. Остаётся согласие: описание живёт в
// одном месте, а расхождение ПЕРЕЧНЯ краснеет.
//
// # Что здесь считается ЗАКОННЫМИ ФОРМАМИ, и откуда они взяты
//
// Формы выведены из дерева, а не из памяти; форма, о которой разбор не знает,
// уходит из-под наблюдения молча — это худший исход, потому что он не даёт ни
// красного, ни зелёного.
//
//	объявление кейса   `id="…"` в строке кода — с отдельной строки ЛИБО внутри
//	                   однострочного вызова
//	позиция перечня    строка блока, НАЧИНАЮЩАЯСЯ идентификатором
//	продолжение        строка блока, идентификатором НЕ начинающаяся
//
// Форма объявления одна: `id = "` с пробелами в дереве встречается дважды и ОБА
// раза в комментарии (`iam-rbac-subjects.py`), поэтому комментарий отбрасывается
// до разбора — иначе проза о чужом идентификаторе стала бы объявлением.
// Эквивалентность этого предиката разбору синтаксического дерева Python
// проверена вторым выражением: 1462 объявления по обоим, расхождений ноль.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. идентификатор, СОБРАННЫЙ из частей (`id=PREFIX + "-OK"`): объявление
//     судится по литералу, а не по потоку значений;
//  2. блок под другим заголовком: заголовки перечислены явно (`coverageHeads`),
//     и незнакомый заголовок означает «блока нет», а не «блок пуст»;
//  3. ПРАВДИВОСТЬ описания против идентификатора — машинно не решается, и это
//     сказано вслух, чтобы «блок зелёный» не читалось шире сделанного.
package repohygiene

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// coverageHeads — заголовки, которыми блок состава открывается в этом дереве.
// Перечень закрыт: незнакомый заголовок означает отсутствие блока, а не пустой
// блок, — иначе гейт молчал бы там, где перечень просто назвали иначе.
var coverageHeads = []string{"Coverage:", "Покрытие:", "Состав:"}

var (
	// caseIDDecl — объявление кейса: `id="…"` в строке кода.
	//
	// Форм записи ДВЕ, и обе законны: аргумент с отдельной строки (так в дереве
	// написаны все 1462 объявления) и аргумент внутри однострочного вызова
	// (`case(id="…")`). Второй в дереве сегодня НЕТ — расширение здесь
	// превентивное, и перепись оно не меняет; сказано это затем, чтобы его не
	// приняли за находку. Разбор, знающий одну форму, увёл бы объявления второй
	// из-под наблюдения МОЛЧА — ни красного, ни зелёного.
	//
	// Граница слова слева отсекает соседей вида `valid=`/`uuid=`; ложных
	// срабатываний на дереве ноль (сверено разбором синтаксического дерева
	// Python: 1462 объявления по обоим предикатам).
	caseIDDecl = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])id\s*=\s*"([^"]+)"`)
	// caseIDLead — позиция перечня: строка блока, начинающаяся идентификатором.
	// Идентификатор кейса — ЗАГЛАВНЫЕ сегменты через дефис, не менее трёх.
	caseIDLead = regexp.MustCompile(
		"^\\s*(?:[-*]\\s*)?`?([A-Z][A-Z0-9]*(?:-[A-Z0-9]+){2,})`?\\b")
)

// CaseCoverage — состав одного модуля кейсов.
type CaseCoverage struct {
	// HasBlock — модуль несёт блок состава. Ложь означает «блока нет», и это
	// законно: большинство модулей описывают состав прозой.
	HasBlock bool
	// Listed — идентификаторы, названные блоком, в порядке появления.
	Listed []string
	// Declared — идентификаторы, ОБЪЯВЛЕННЫЕ модулем.
	Declared []string
}

// Missing — объявлено, но блоком не названо.
func (c CaseCoverage) Missing() []string { return diffSorted(c.Declared, c.Listed) }

// Stray — названо блоком, но модулем не объявлено.
func (c CaseCoverage) Stray() []string { return diffSorted(c.Listed, c.Declared) }

func diffSorted(a, b []string) []string {
	have := make(map[string]bool, len(b))
	for _, x := range b {
		have[x] = true
	}
	var out []string
	for _, x := range a {
		if !have[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// ParseCaseCoverage — разобрать модуль кейсов.
//
// Модульный docstring выделяется ПЕРВЫМ тройным литералом файла: `id=` в прозе
// шапки объявлением не является, и отделить одно от другого можно только зная,
// где кончается литерал.
func ParseCaseCoverage(src string) CaseCoverage {
	lines := strings.Split(src, "\n")

	// ── границы модульного docstring
	docFrom, docTo, quote := -1, -1, ""
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		for _, q := range []string{`"""`, "'''"} {
			if strings.HasPrefix(t, q) || strings.HasPrefix(t, "r"+q) || strings.HasPrefix(t, "u"+q) {
				docFrom, quote = i, q
			}
		}
		break // первая значащая строка решает: либо docstring, либо его нет
	}
	if docFrom >= 0 {
		body := lines[docFrom][strings.Index(lines[docFrom], quote)+len(quote):]
		if strings.Contains(body, quote) { // однострочный docstring
			docTo = docFrom
		} else {
			for i := docFrom + 1; i < len(lines); i++ {
				if strings.Contains(lines[i], quote) {
					docTo = i
					break
				}
			}
		}
	}

	var out CaseCoverage

	// ── перечень блока: только внутри docstring
	if docFrom >= 0 && docTo >= docFrom {
		doc := lines[docFrom : docTo+1]
		head := -1
		for i, ln := range doc {
			t := strings.TrimSpace(ln)
			for _, h := range coverageHeads {
				if t == h {
					head = i
				}
			}
			if head >= 0 {
				break
			}
		}
		if head >= 0 {
			out.HasBlock = true
			seen := map[string]bool{}
			for _, ln := range doc[head+1:] {
				// Блок кончается первой НЕПУСТОЙ строкой без отступа: так
				// устроены соседние разделы шапки.
				if strings.TrimSpace(ln) != "" && !strings.HasPrefix(ln, " ") &&
					!strings.HasPrefix(ln, "\t") {
					break
				}
				if m := caseIDLead.FindStringSubmatch(ln); m != nil && !seen[m[1]] {
					seen[m[1]] = true
					out.Listed = append(out.Listed, m[1])
				}
			}
		}
	}

	// ── объявления: строки КОДА вне docstring и вне комментария
	seen := map[string]bool{}
	for i, ln := range lines {
		if docFrom >= 0 && i >= docFrom && i <= docTo {
			continue
		}
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") {
			continue
		}
		for _, m := range caseIDDecl.FindAllStringSubmatch(t, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out.Declared = append(out.Declared, m[1])
			}
		}
	}
	return out
}

// CaseCoverageCensus — объём осмотренного одним обходом.
type CaseCoverageCensus struct {
	Modules, Blocks, Listed, Declared int
}

// ScanCaseCoverage — находки и перепись по названным модулям.
//
// Принимает УЖЕ ПРОЧИТАННЫЕ модули, а не корень дерева: тогда доказательство
// способности упасть подаёт синтетические миры той же функции, которую зовёт
// гейт, — а не повторяет её логику своей копией. Копия осталась бы зелёной
// ровно тогда, когда гейт перестал бы работать.
func ScanCaseCoverage(mods map[string]string) ([]string, CaseCoverageCensus) {
	var findings []string
	c := CaseCoverageCensus{Modules: len(mods)}

	rels := make([]string, 0, len(mods))
	for rel := range mods {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		cov := ParseCaseCoverage(mods[rel])
		c.Declared += len(cov.Declared)
		if !cov.HasBlock {
			// Блока нет — законно: большинство модулей описывают состав прозой.
			continue
		}
		c.Blocks++
		c.Listed += len(cov.Listed)
		if m := cov.Missing(); len(m) != 0 {
			findings = append(findings, rel+": блок состава называет "+
				strconv.Itoa(len(cov.Listed))+" кейсов, модуль объявляет "+strconv.Itoa(len(cov.Declared))+
				".\nНЕ НАЗВАНЫ ("+strconv.Itoa(len(m))+"): "+strings.Join(m, ", ")+
				"\nПеречень выписан рядом с растущим составом: правит его тот, кто "+
				"наткнётся, и читатель модуля недосчитается позиций")
		}
		if st := cov.Stray(); len(st) != 0 {
			findings = append(findings, rel+": блок состава называет кейсы, которых "+
				"модуль НЕ объявляет ("+strconv.Itoa(len(st))+"): "+strings.Join(st, ", ")+
				"\nЗапись пережила свой предмет — кейс снят, строка осталась")
		}
	}
	// ПРЕДПОСЫЛКА обхода: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного», поэтому пустой обход — отказ, а не молчание.
	if c.Modules == 0 {
		findings = append(findings, "модулей кейсов не прочитано ни одного — обход "+
			"беспредметен, и «ноль находок» было бы его свойством, а не свойством дерева")
	} else if c.Declared == 0 {
		findings = append(findings, "не прочитано ни одного объявления кейса — предикат "+
			"объявлений ослеп, и молчание было бы его свойством")
	}
	return findings, c
}
