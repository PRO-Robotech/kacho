// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanpreconditionmark_test.go — ТРЕТИЙ ИСХОД ЛИБО ЕСТЬ ВО ВСЕХ НАБОРАХ, ЛИБО
// ЕГО НЕТ НИ В ОДНОМ НАБОРЕ.
//
// # Предмет
//
// Страж настройки харнесса — утверждение, чьё имя несёт фразу `harness config:`,
// — падает тогда, когда предмет шага обязан был создать НЕ продукт: адрес
// поверхности от прогонщика, субъект от посева фикстур. Его отказ и отказ
// продукта приходят вердикту одним слотом (`assertions.failed`), поэтому
// различает их МЕТКА в имени утверждения.
//
// Метка не делает исход зелёным. Прогон остаётся ненулевым, утверждение —
// красным и названным; меняется КАТЕГОРИЯ и КОД ВОЗВРАТА, то есть место, куда
// идёт разбирающий: харнесс и посев вместо продукта.
//
// # Почему свойство ДЕРЕВА, а не набора
//
// Вердикт по каждой из восьми суит выносит ОДИН скрипт
// (`services/iam/tests/newman/scripts/assert-suites-green.sh`, запускается с cwd
// = каталог проверяемой суиты — см. `.github/workflows/e2e-newman.yml`, «ОДИН
// скрипт (живёт в iam) применяется к КАЖДОЙ суите»). Метку он читает у
// ЕДИНСТВЕННОГО производителя — `services/iam/tests/newman/scripts/gen.py`, —
// потому что берёт её по `dirname "${BASH_SOURCE[0]}"`, а не по cwd.
//
// Отсюда несущее следствие: набор, чей производитель объявил ДРУГОЙ текст
// метки, молча выпадает из третьей категории. Его стражи снова читаются
// находками о продукте, при том что метка у них есть и на глаз всё исправно.
// Согласие объявлений здесь — не аккуратность, а условие работоспособности
// механизма, и держать его обязан гейт дерева: набор о соседях не знает.
//
// # Чего гейт НЕ делает
//
//   - не судит, читает ли ВЕРДИКТНЫЙ ГЕЙТ метку у производителя: скрипт вердикта
//     в дереве один, и эту ось держит
//     `services/iam/tests/newman/scripts/precondition_mark_test.py`;
//   - не требует стражей от набора, у которого их нет: предмет заводит владелец
//     суиты, а гейт связывает лишь того, кто уже завёл;
//   - не судит стражей ДРУГОГО рода — предмет шага, не созданный предыдущим
//     шагом. Такой отказ есть находка о продукте или о кейсе, и метки он нести
//     НЕ ДОЛЖЕН: увести его в третью категорию значило бы завести маску.
//
// # Кто вправе ставить метку
//
// Только производитель формы. Файл кейса, ВЫПИСАВШИЙ текст метки литералом,
// выводит своё падение из находок — вручную и без чьего-либо решения; это ровно
// то вычитание, которое снято из вердикта целиком. Кейсу, которому третий исход
// нужен по существу (рукописный страж настройки харнесса), текст приходит от
// производителя своего набора — константа впрыснута в пространство имён кейсов,
// и подстановка вторым местом об одном предмете не является.
//
// Ось внутри набора iam держит его собственная проба; здесь она распространена
// на дерево — набор о соседях не знает, а тексты обязаны совпасть у всех.
//
// # Как страж опознаётся
//
// По фразе в ИМЕНИ утверждения, а не по слову в файле. Имя производит тот же
// блок, что и само утверждение, — то есть «это настройка харнесса» здесь
// сказано его собственным автором. Комментарий за стража не считается: гейт по
// сырому тексту краснел бы на прозе, объясняющей эту же защиту
// (`testing.md` §«Гейт на класс», п. 4).
//
// Судятся ПОРОЖДЁННЫЕ коллекции, а не исходники генератора: стражи бывают двух
// родов — общие (помощник в `gen.py`) и рукописные, вписанные прямо в кейс, — и
// проверка по исходникам видела бы только первый.
//
// Самопроверка способности упасть — `newmanpreconditionmark_injection_test.go`.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// Фраза, которой страж объявляет себя сам, — в ИМЕНИ утверждения.
	newmanHarnessGuardDecl = "harness config:"
	newmanGenBase          = "gen.py"
	// Потолок ПЕЧАТИ координат в одной находке. Число не усекается никогда.
	newmanPrecondCoordCap = 5
)

// Присвоение константы — то, что ПРОИЗВОДИТ метку. Упоминание имени в прозе
// производителем не является, поэтому образец требует присвоения строкового
// литерала на своей строке. RE2 обратных ссылок не имеет, поэтому кавычки
// перечислены ветвями, а не связаны `\1`.
var reNewmanPrecondDecl = regexp.MustCompile(
	`(?m)^PRECONDITION_MARK\s*=\s*(?:"([^"]*)"|'([^']*)')\s*$`)

var reNewmanPmTest = regexp.MustCompile(`pm\.test\s*\(`)

// newmanPrecondRoot — один набор сквозных проб.
//
// Помеченных ЗДЕСЬ НЕТ ПОЛЕМ: величина выводится как `Guards - len(Unmarked)`.
// Два счётчика об одном предмете разошлись бы молча, и разошлись бы там, где
// расхождение не видно, — перепись печатала бы одно, а находка называла другое.
type newmanPrecondRoot struct {
	Root         string   // `services/vpc/tests/newman`
	Declarations []string // тексты метки, объявленные в scripts/gen.py
	Guards       int      // стражей настройки харнесса в порождённых коллекциях
	Unmarked     []string // координаты непомеченных, `<коллекция> :: <шаг>`
	Cases        int      // файлов кейсов осмотрено
	CasesWriting []string // файлы кейсов, ВЫПИСАВШИЕ текст метки литералом
}

// adjudicateNewmanPrecondMark — суждение, отделённое от чтения дерева: иначе
// способность гейта упасть доказывалась бы только порчей рабочей копии.
func adjudicateNewmanPrecondMark(roots []newmanPrecondRoot) []string {
	var out []string

	// Согласие объявлений — сперва, потому что расхождение текста обесценивает
	// пометку целого набора, а не одного стража.
	byText := map[string][]string{}
	for _, r := range roots {
		for _, d := range r.Declarations {
			byText[d] = append(byText[d], r.Root)
		}
	}
	if len(byText) > 1 {
		var texts []string
		for text := range byText {
			texts = append(texts, text)
		}
		sort.Strings(texts)
		var parts []string
		for _, text := range texts {
			owners := byText[text]
			sort.Strings(owners)
			parts = append(parts, strconv.Quote(text)+" — "+strings.Join(owners, ", "))
		}
		out = append(out, "текстов метки третьего исхода в дереве "+
			strconv.Itoa(len(byText))+", а должен быть один: "+strings.Join(parts, "; ")+".\n"+
			"    Вердикт по КАЖДОЙ суите выносит один скрипт и читает метку у ОДНОГО\n"+
			"    производителя (services/iam/tests/newman/scripts/gen.py). Набор,\n"+
			"    объявивший другой текст, молча выпадает из третьей категории: его\n"+
			"    стражи снова читаются находками о продукте, хотя метка у них есть.")
	}

	for _, r := range roots {
		switch {
		case len(r.Declarations) > 1:
			out = append(out, r.Root+": производителей метки третьего исхода "+
				strconv.Itoa(len(r.Declarations))+", а должен быть ровно один — "+
				"второе объявление разойдётся с первым молча.")
		case len(r.Declarations) == 0 && r.Guards > 0:
			out = append(out, r.Root+": стражей настройки харнесса "+
				strconv.Itoa(r.Guards)+", а метки третьего исхода набор не объявляет вовсе "+
				"(scripts/"+newmanGenBase+"::PRECONDITION_MARK).\n"+
				"    Значит все "+strconv.Itoa(r.Guards)+" приходят вердикту находками о продукте,\n"+
				"    и разбирающий идёт чинить дерево там, где не отработал прогонщик или посев.\n"+
				"    Метку ставит ПРОИЗВОДИТЕЛЬ ФОРМЫ (scripts/"+newmanGenBase+"), а не файл кейса.")
			continue
		}
		for _, rel := range r.CasesWriting {
			out = append(out, r.Root+": "+rel+" ВЫПИСЫВАЕТ текст метки третьего исхода "+
				"литералом.\n"+
				"    Падение кейса ушло бы из находок без чьего-либо решения — то самое\n"+
				"    вычитание, которое снято из вердикта целиком. Кейсу, которому третий\n"+
				"    исход нужен по существу, текст приходит ОТ ПРОИЗВОДИТЕЛЯ набора\n"+
				"    (scripts/"+newmanGenBase+"::PRECONDITION_MARK впрыснут в пространство имён кейсов).")
		}
		if len(r.Unmarked) == 0 {
			continue
		}
		coords := r.Unmarked
		tail := ""
		if len(coords) > newmanPrecondCoordCap {
			tail = "\n    … и ещё " + strconv.Itoa(len(coords)-newmanPrecondCoordCap) +
				" того же вида (перечень усечён; число выше — полное)"
			coords = coords[:newmanPrecondCoordCap]
		}
		out = append(out, r.Root+": стражей настройки харнесса без метки третьего исхода — "+
			strconv.Itoa(len(r.Unmarked))+" из "+strconv.Itoa(r.Guards)+".\n"+
			"    Их отказ приходит вердикту находкой о продукте, хотя условие не создал\n"+
			"    харнесс. Пометьте их в производителе формы (scripts/"+newmanGenBase+") и\n"+
			"    перегенерируйте коллекции.\n    "+strings.Join(coords, "\n    ")+tail)
	}
	return out
}

// newmanCollectGuards — обход одной порождённой коллекции.
func newmanCollectGuards(doc any, trail string, mark string, r *newmanPrecondRoot, coll string) {
	switch node := doc.(type) {
	case []any:
		for _, child := range node {
			newmanCollectGuards(child, trail, mark, r, coll)
		}
	case map[string]any:
		name := trail
		if s, ok := node["name"].(string); ok && s != "" {
			name = s
		}
		if events, ok := node["event"].([]any); ok {
			for _, ev := range events {
				evm, ok := ev.(map[string]any)
				if !ok {
					continue
				}
				script, ok := evm["script"].(map[string]any)
				if !ok {
					continue
				}
				var lines []string
				switch src := script["exec"].(type) {
				case []any:
					for _, l := range src {
						if s, ok := l.(string); ok {
							lines = append(lines, s)
						}
					}
				case string:
					lines = append(lines, src)
				}
				for _, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "//") {
						continue
					}
					if !reNewmanPmTest.MatchString(line) ||
						!strings.Contains(line, newmanHarnessGuardDecl) {
						continue
					}
					r.Guards++
					// Пустая метка означает «набор её не объявляет»: считать
					// такого стража помеченным по вхождению пустой строки
					// значило бы объявить свойство выполненным у всех.
					if mark != "" && strings.Contains(line, mark) {
						continue
					}
					r.Unmarked = append(r.Unmarked, coll+" :: "+name)
				}
			}
		}
		newmanCollectGuards(node["item"], name, mark, r, coll)
	}
}

// collectNewmanPrecondRoots — чтение дерева: индекс git, а не диск, потому что
// именно индекс увидит конвейер на свежем checkout'е.
func collectNewmanPrecondRoots(t *testing.T, root string) []newmanPrecondRoot {
	t.Helper()

	var genFiles []string
	collections := map[string][]string{}
	caseFiles := map[string][]string{}
	for _, line := range gitLsFiles(t, root) {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		rel := line[tab+1:]
		if isProbeFixture(rel) {
			continue
		}
		dir := filepath.Dir(rel)
		switch {
		case filepath.Base(rel) == newmanGenBase && filepath.Base(dir) == "scripts" &&
			strings.HasSuffix(filepath.Dir(dir), "/tests/newman"):
			genFiles = append(genFiles, rel)
		case filepath.Base(dir) == "collections" && strings.HasSuffix(rel, ".json") &&
			strings.HasSuffix(filepath.Dir(dir), "/tests/newman"):
			owner := filepath.Dir(dir)
			collections[owner] = append(collections[owner], rel)
		case filepath.Base(dir) == "cases" && strings.HasSuffix(rel, ".py") &&
			strings.HasSuffix(filepath.Dir(dir), "/tests/newman"):
			owner := filepath.Dir(dir)
			caseFiles[owner] = append(caseFiles[owner], rel)
		}
	}

	var roots []newmanPrecondRoot
	for _, gen := range genFiles {
		newmanRoot := filepath.Dir(filepath.Dir(gen))
		r := newmanPrecondRoot{Root: newmanRoot}

		body, err := os.ReadFile(filepath.Join(root, gen))
		if err != nil {
			t.Fatalf("чтение %s: %v", gen, err)
		}
		for _, m := range reNewmanPrecondDecl.FindAllStringSubmatch(string(body), -1) {
			text := m[1]
			if text == "" {
				text = m[2]
			}
			r.Declarations = append(r.Declarations, text)
		}
		mark := ""
		if len(r.Declarations) == 1 {
			mark = r.Declarations[0]
		}

		cases := caseFiles[newmanRoot]
		sort.Strings(cases)
		for _, rel := range cases {
			raw, rerr := os.ReadFile(filepath.Join(root, rel))
			if rerr != nil {
				t.Fatalf("чтение %s: %v", rel, rerr)
			}
			r.Cases++
			if mark != "" && strings.Contains(string(raw), mark) {
				r.CasesWriting = append(r.CasesWriting, rel)
			}
		}

		files := collections[newmanRoot]
		sort.Strings(files)
		for _, rel := range files {
			raw, rerr := os.ReadFile(filepath.Join(root, rel))
			if rerr != nil {
				t.Fatalf("чтение %s: %v", rel, rerr)
			}
			var doc any
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			base := strings.TrimSuffix(filepath.Base(rel), ".json")
			if m, ok := doc.(map[string]any); ok {
				newmanCollectGuards(m["item"], base, mark, &r, filepath.Base(rel))
			}
		}
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Root < roots[j].Root })
	return roots
}

func TestNewmanHarnessGuardsCarryTheThirdOutcomeMark(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	roots := collectNewmanPrecondRoots(t, root)

	var lines []string
	totGuards, totMarked, totDecl, totCases := 0, 0, 0, 0
	for _, r := range roots {
		marked := r.Guards - len(r.Unmarked)
		totGuards += r.Guards
		totMarked += marked
		totDecl += len(r.Declarations)
		totCases += r.Cases
		lines = append(lines, r.Root+" (объявлений "+strconv.Itoa(len(r.Declarations))+
			" · стражей "+strconv.Itoa(r.Guards)+
			" · помечено "+strconv.Itoa(marked)+
			" · НЕ помечено "+strconv.Itoa(len(r.Unmarked))+")")
	}
	// ПЕРЕПИСЬ ПЕЧАТАЕТ ОБЕ ВЕЛИЧИНЫ. Одно число («стражей N») скрыло бы ровно
	// тот случай, ради которого гейт заведён.
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: наборов %d · объявлений метки %d · файлов кейсов %d · "+
		"стражей %d · помечено %d · НЕ помечено %d\n  %s",
		len(roots), totDecl, totCases, totGuards, totMarked, totGuards-totMarked,
		strings.Join(lines, "\n  "))

	// ПУСТОЙ ОБХОД — ОТКАЗ, А НЕ УСПЕХ. «Ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if len(roots) == 0 {
		t.Fatal("в индексе не найдено ни одного набора сквозных проб " +
			"(*/tests/newman/scripts/" + newmanGenBase + ") — обход пуст, и вердикт был бы " +
			"о ничём: предикат разошёлся с раскладкой дерева")
	}
	if totCases == 0 {
		t.Fatal("ни одного файла кейса не прочитано во всех " + strconv.Itoa(len(roots)) +
			" наборах — ось «метку ставит производитель, а не кейс» осталась бы " +
			"беспредметной, объявив себя выполненной")
	}
	if totGuards == 0 {
		t.Fatal("ни одного стража настройки харнесса не прочитано во всех " +
			strconv.Itoa(len(roots)) + " наборах — либо коллекции не порождены, либо " +
			"предикат опознания стража разошёлся с формой, которую пишет производитель")
	}

	for _, finding := range adjudicateNewmanPrecondMark(roots) {
		t.Error(finding)
	}
}
