// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestScriptPopulationIsSelectedByTheDeclaredRootsNotALiteral — отбор популяции
// в оболочке и питоне берётся у объявленного перечня корней, а не у литерала.
//
// Близнец TestPopulationIsSelectedByTheDeclaredRootsNotALiteral для языков,
// которых тот не читает: он разбирает Go-AST, поэтому оболочка и питон в его
// популяцию не входят ВОВСЕ. Класс дал в оболочке два живых дефекта, и оба были
// молчаливыми (kacho#2339).
func TestScriptPopulationIsSelectedByTheDeclaredRootsNotALiteral(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	findings, census, err := AuditContractRootScripts(root)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}

	files, lexemes, bearing, _ := census.Totals()
	if files == 0 {
		t.Fatalf("обход пуст: файлов прочитано 0 — вердикт беспредметен (%s)", census)
	}
	if lexemes == 0 {
		t.Fatalf("лексем не выделено НИ ОДНОЙ при %d файлах: разбор перестал их "+
			"видеть, и молчание гейта ничего не означает (%s)", files, census)
	}

	// Знаменатель переписи: корень контрактов обязан ВСТРЕЧАТЬСЯ. Ноль здесь
	// означал бы, что судить нечего — «находок ноль» стало бы неотличимо от
	// «этот язык дерева контрактов не касается».
	if bearing == 0 {
		t.Fatalf("лексем, НЕСУЩИХ корень контрактов, ноль при %d осмотренных: "+
			"популяция пуста, и зелёное здесь было бы верным и ПУСТЫМ (%s)",
			lexemes, census)
	}

	// Перепись обязана называть КАЖДЫЙ язык порознь: одно суммарное число
	// скрывает ровно тот случай, ради которого гейт заведён — язык, чьи файлы
	// перестали попадать в обход, при сложении неотличим от языка без находок.
	for _, lang := range []string{langPython, langShell} {
		lc := census.ByLang[lang]
		if lc == nil || lc.Files == 0 {
			t.Errorf("язык %s не осмотрен ВОВСЕ (файлов 0): его слепота — ровно то, "+
				"ради чего гейт заведён (%s)", lang, census)
		}
	}

	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("перепись: %s", census)
}

// TestScriptGateCoversEveryExecutableScriptTouchingTheContractTree — популяция
// ВЫВОДИТСЯ из дерева, и выводится по МЕСТУ ЖИЗНИ КЛАССА.
//
// Единица счёта — ИСПОЛНЯЕМЫЙ СКРИПТ (файл с шебангом), ТРОГАЮЩИЙ дерево
// контрактов. Ни расширение, ни имя каталога здесь не выписываются: перечень
// расширений разошёлся бы с деревом на первом же новом языке — молча, потому
// что проба продолжала бы быть зелёной.
//
// Почему шебанг, а не расширение: отбор популяции — свойство ИСПОЛНЯЕМОГО кода.
// Проза, контракты, порождённые каталоги и разметка развёртывания корень
// НАЗЫВАЮТ, но им не отбирают; требовать охвата от них значило бы завести
// ветвь, которая зеленеет всегда и не утверждает ничего.
//
// Фикстуры (`testdata/`) исключены НАМЕРЕННО и это не послабление: они —
// замороженный ВХОД чужих гейтов, а не код дерева. Судить их значило бы
// требовать исправности от намеренно испорченного.
func TestScriptGateCoversEveryExecutableScriptTouchingTheContractTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	all, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева НЕ ИЗМЕРЕН: %v", err)
	}

	judged, byInterp := 0, map[string][]string{}
	scripts, bearing := 0, 0
	for _, p := range all {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "/testdata/") || strings.HasPrefix(rel, "testdata/") {
			continue
		}
		src, rerr := readFileString(p)
		if rerr != nil || !strings.HasPrefix(src, "#!") {
			continue
		}
		scripts++
		if !bearsContractRoot(src) {
			continue
		}
		bearing++
		if scriptLangOf(rel) != "" {
			judged++
			continue
		}
		interp := shebangInterpreter(src)
		byInterp[interp] = append(byInterp[interp], rel)
	}

	if scripts == 0 {
		t.Fatalf("исполняемых скриптов в дереве не найдено ВОВСЕ при %d отслеживаемых "+
			"файлах: обход беспредметен, и зелёное было бы пустым", len(all))
	}
	if bearing == 0 {
		t.Fatalf("скриптов, ТРОГАЮЩИХ дерево контрактов, ноль при %d осмотренных: "+
			"предмета у пробы нет (%d файлов дерева)", scripts, len(all))
	}
	if judged == 0 {
		t.Fatalf("из %d скриптов, трогающих дерево контрактов, гейт не судит НИ ОДНОГО: "+
			"популяция пуста, и молчание гейта ничего не означает", bearing)
	}

	var unnamed []string
	interps := make([]string, 0, len(byInterp))
	for i := range byInterp {
		interps = append(interps, i)
	}
	sort.Strings(interps)
	for _, i := range interps {
		if contractRootScriptUnjudgedInterpreters[i] == "" {
			unnamed = append(unnamed, i+" ("+strconv.Itoa(len(byInterp[i]))+
				" файлов, напр. "+byInterp[i][0]+")")
		}
	}
	if len(unnamed) > 0 {
		t.Errorf("исполняемый скрипт ТРОГАЕТ дерево контрактов, а гейт его языка НЕ "+
			"СУДИТ: %s — класс «популяция отбирается литералом корня» живёт там вне "+
			"наблюдения МОЛЧА. Либо завести ветвь разбора, либо назвать язык в "+
			"contractRootScriptUnjudgedInterpreters С ПРИЧИНОЙ, измеренной по дереву",
			strings.Join(unnamed, ", "))
	}

	// Запись, которой больше нечего исключать, — находка: граница, пережившая
	// свой предмет, читается как действующая и прикрывает следующую слепоту.
	for interp, why := range contractRootScriptUnjudgedInterpreters {
		if len(byInterp[interp]) == 0 {
			t.Errorf("граница инструмента для %q потеряла предмет (скриптов, трогающих "+
				"дерево контрактов, ноль): снимите запись — причина была %q", interp, why)
		}
	}

	outside := 0
	for _, v := range byInterp {
		outside += len(v)
	}
	t.Logf("перепись: исполняемых скриптов %d, из них ТРОГАЮТ дерево контрактов %d "+
		"(судятся %d, вне разбора %d по интерпретаторам %v)",
		scripts, bearing, judged, outside, interps)
}

// contractRootScriptUnjudgedInterpreters — интерпретаторы, чьи скрипты трогают
// дерево контрактов, но лексического разбора у этого гейта не имеют.
//
// Это НЕ ведомость прощений: запись не прощает нарушение, она называет ГРАНИЦУ
// ИНСТРУМЕНТА, и потому обязана нести ИЗМЕРЕННУЮ причину, а не намерение.
// Запись, у которой предмет исчез, роняет пробу выше — граница, пережившая свой
// предмет, прикрывает следующую слепоту молча.
var contractRootScriptUnjudgedInterpreters = map[string]string{
	"node": "замер по дереву 2026-09-08: файлов 2, и НИ ОДИН не отбирает популяцию — " +
		"`selftest-assertions.js` называет домен ОШИБКИ (`nlb.kacho.cloud`, это " +
		"ErrorInfo.domain, а не дерево контрактов), `check-ui-language.mjs` называет " +
		"пакет в КОММЕНТАРИИ. Предмета нет; появится отбор — заводить ветвь разбора",
}

// shebangInterpreter — имя интерпретатора из шебанга, огрублённое до языка.
func shebangInterpreter(src string) string {
	line := src
	if i := strings.IndexByte(src, '\n'); i >= 0 {
		line = src[:i]
	}
	switch {
	case strings.Contains(line, "python"):
		return "python"
	case strings.Contains(line, "node"):
		return "node"
	case strings.Contains(line, "bash"), strings.Contains(line, "/sh"),
		strings.HasSuffix(strings.TrimSpace(line), "sh"):
		return "shell"
	}
	f := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(f) == 0 {
		return "(пустой шебанг)"
	}
	return filepath.Base(f[len(f)-1])
}
