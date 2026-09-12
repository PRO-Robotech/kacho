// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleselftestreach_test.go — САМОПРОВЕРКА КОНСОЛИ, КОТОРУЮ НИКТО НЕ ЗОВЁТ,
// НЕОТЛИЧИМА ОТ ОТСУТСТВУЮЩЕЙ.
//
// # Предмет
//
// В `ui-future/e2e/scripts/` живут самопроверки решений, которые пробы браузером
// не проверяют by construction: решение о браузере, отображение имени стенда,
// разбор отказа пробы потока, решение о посадке домена величин. Каждая гоняется
// голым `node` за секунды и стоит ДО стенда — её отказ означает «прогон не
// вынесет вердикта о продукте».
//
// Гейт заведён ЗАМЕРОМ, а не по симметрии. На момент заведения самопроверок в
// дереве было четыре, а звал конвейер три: `quota-posture-selftest.ts` приехала
// вместе со своими пробами и не звалась НИ ОТКУДА. Форма беды тиха настолько,
// насколько это возможно: файл в дереве ЕСТЬ, он проходит, его правят, его
// читают на ревью — и всё это не значит ничего, потому что желания запустить
// его никто не изъявляет. Зелёный прогон проб при этом читается как вердикт о
// свойстве, которого никто не спрашивал.
//
// # Почему шагом конвейера, а не предполётно из прогонщика проб
//
// Решение отличается от newman-суиты (`newmanselftestreach_test.go`) осознанно,
// и различие названо здесь, чтобы следующий не выбирал заново. У суиты есть
// один прогонщик, через который проходит ВСЯКИЙ; у проб консоли такого места
// нет — их запускает playwright, которому до самопроверок решений дела нет, и
// вешать на него предполёт значило бы платить им за каждый локальный отбор
// одной пробы. Конвейер же обязан их прогнать все и до стенда.
//
// # Чего гейт НЕ делает
//
// Он не судит СОДЕРЖИМОЕ самопроверок: их правят свои линии. Здесь только
// достижимость. И он не требует, чтобы самопроверка была у каждого решения:
// заводить её решает владелец решения, а гейт связывает лишь того, кто уже
// завёл, — иначе он предписывал бы работу, а не стерёг свойство.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Раскладка и суффикс — соглашение, а не перечень: новая самопроверка попадает
// под гейт сама, без правки списка.
const (
	consoleSelftestDir    = "ui-future/e2e/scripts"
	consoleSelftestSuffix = "-selftest.ts"
	consoleWorkflowDir    = ".github/workflows"
)

// Вызов самопроверки из описания процесса. Признак — ИСПОЛНЯЕМЫЙ вызов node по
// этому файлу, а не упоминание имени: имя стоит и в объясняющем комментарии
// рядом с шагом, и гейт по подстроке остался бы зелёным на снятом вызове,
// покраснев на собственном объяснении (`testing.md` §«Гейт на класс», п. 4).
// `[^#\n]*` от начала строки закрывает именно это: в YAML комментарий начинается
// с `#`, и пересечь его выражение не может.
func consoleSelftestCallRe(base string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[^#\n]*\bnode\b[^#\n]*` + regexp.QuoteMeta(base))
}

// consoleSelftest — одна самопроверка: где лежит и откуда зовётся.
type consoleSelftest struct {
	Path     string   // путь от корня репозитория
	Base     string   // имя файла
	CalledBy []string // описания процессов, которые её зовут
}

// consoleSelftestExempt — ведомость: самопроверка, которую намеренно НЕ зовёт
// конвейер, с причиной. Ключ — путь от корня.
//
// Ведомость ИСТЕКАЕТ САМА: запись, которой больше нечего прощать (файл исчез
// либо вызов появился), — находка, а не безобидный остаток. Слепую зону
// наследует следующий, кто её не заводил.
//
// ВЕДОМОСТЬ ПУСТА, И ЭТО ЦЕЛЬ, А НЕ ПОЛОМКА. Гейт на пустой ведомости ПРОХОДИТ:
// проба, падающая на достижении своей цели, толкает держать запись ради
// зелёного. Способность гейта упасть доказывает не запись, а adjudicate-проба
// на синтетике.
var consoleSelftestExempt = map[string]string{}

// adjudicateConsoleSelftestReach — суждение, отделённое от чтения дерева: иначе
// способность гейта упасть доказывалась бы только порчей рабочей копии.
//
// ПОРЯДОК ВЕТВЕЙ НЕСУЩИЙ. Пустой обход отвергается ПЕРВЫМ — раньше ведомости и
// раньше разбора вызовов. Послабление, стоящее выше отказа на пустоте, съело бы
// её: обход, разошедшийся с раскладкой дерева, объявил бы свойство выполненным,
// не осмотрев ничего, и «ноль находок» стало бы неотличимо от «ноль
// прочитанного».
func adjudicateConsoleSelftestReach(selftests []consoleSelftest, exempt map[string]string) []string {
	if len(selftests) == 0 {
		return []string{"обход пуст: в индексе нет ни одного файла " + consoleSelftestDir +
			"/*" + consoleSelftestSuffix + ".\n" +
			"    Вердикта о ничём не бывает: либо предикат разошёлся с раскладкой дерева, " +
			"либо самопроверки сняты все разом, и то и другое требует решения, а не " +
			"молчаливого зелёного."}
	}

	var out []string
	seen := make(map[string]bool, len(selftests))
	for _, s := range selftests {
		seen[s.Path] = true

		if reason, ok := exempt[s.Path]; ok {
			// Послабление законно ровно пока у него есть предмет.
			if len(s.CalledBy) > 0 {
				out = append(out, s.Path+": запись ведомости потеряла предмет — "+
					"самопроверку зовёт "+strings.Join(s.CalledBy, ", ")+
					", а ведомость прощает ей отсутствие вызова ("+reason+").\n"+
					"    Снимите запись: послабление без предмета — это слепая зона, "+
					"выданная вперёд, и следующая настоящая находка уедет под неё.")
			}
			continue
		}

		if len(s.CalledBy) == 0 {
			out = append(out, s.Path+": НИ ОДНО описание процесса в "+consoleWorkflowDir+
				" не зовёт `node` по этому файлу.\n"+
				"    Самопроверка находит классы, которых пробы браузером не находят by "+
				"construction, и стоит секунды ДО стенда. Пока её не зовут, эти классы "+
				"въезжают в ствол молча, а зелёный прогон проб читается как вердикт о "+
				"свойстве, которого никто не спрашивал.\n"+
				"    Форма вызова — шаг с `id` и `working-directory: ui-future/e2e` в "+
				"console-e2e.yml, ДО подъёма стенда. Образец — шаг "+
				"`selftest-stream-verdict`.\n"+
				"    Намеренное отступление заводится записью в consoleSelftestExempt "+
				"с причиной — но молчаливого отступления не бывает.")
		}
	}

	// Зеркало ведомости: запись о файле, которого в дереве нет вовсе.
	var stale []string
	for path := range exempt {
		if !seen[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		out = append(out, path+": запись ведомости потеряла предмет — такого файла в "+
			"индексе нет вовсе. Снимите запись.")
	}
	return out
}

// collectConsoleSelftests — чтение дерева: индекс git, а не диск, потому что
// именно индекс увидит конвейер на свежем checkout'е.
func collectConsoleSelftests(t *testing.T, root string) []consoleSelftest {
	t.Helper()

	workflows := readConsoleWorkflowBodies(t, root)

	var out []consoleSelftest
	for _, line := range gitLsFiles(t, root) {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		rel := line[tab+1:]
		if filepath.Dir(rel) != consoleSelftestDir {
			continue
		}
		base := filepath.Base(rel)
		if !strings.HasSuffix(base, consoleSelftestSuffix) {
			continue
		}
		if isProbeFixture(rel) {
			continue
		}

		s := consoleSelftest{Path: rel, Base: base}
		re := consoleSelftestCallRe(base)
		for _, w := range workflows {
			if re.MatchString(w.body) {
				s.CalledBy = append(s.CalledBy, w.name)
			}
		}
		sort.Strings(s.CalledBy)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

type consoleWorkflowBody struct {
	name string
	body string
}

func readConsoleWorkflowBodies(t *testing.T, root string) []consoleWorkflowBody {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, consoleWorkflowDir))
	if err != nil {
		t.Fatalf("каталог описаний процессов %s не прочитан: %v — обход был бы пуст, а "+
			"вердикт о ничём", consoleWorkflowDir, err)
	}
	var out []consoleWorkflowBody
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, consoleWorkflowDir, name))
		if err != nil {
			t.Fatalf("описание процесса %s не прочитано: %v", name, err)
		}
		out = append(out, consoleWorkflowBody{name: name, body: string(body)})
	}
	// Пустой набор описаний — тоже «ноль прочитанного», и он обязан отказать
	// ЗДЕСЬ: иначе каждая самопроверка ниже станет находкой по ложной причине.
	if len(out) == 0 {
		t.Fatalf("в %s нет ни одного описания процесса — предикат вызова разошёлся с "+
			"деревом, и все находки ниже были бы о его слепоте, а не о дереве",
			consoleWorkflowDir)
	}
	return out
}

func TestConsoleSelftestIsReachableFromTheWorkflow(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	selftests := collectConsoleSelftests(t, root)

	var names []string
	called := 0
	for _, s := range selftests {
		mark := "—"
		if len(s.CalledBy) > 0 {
			mark = strings.Join(s.CalledBy, "+")
			called++
		}
		names = append(names, s.Base+" ("+mark+")")
	}
	var suppressed []string
	for path := range consoleSelftestExempt {
		suppressed = append(suppressed, path)
	}
	sort.Strings(suppressed)
	suppressedText := "нет"
	if len(suppressed) > 0 {
		suppressedText = strings.Join(suppressed, ", ")
	}
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: самопроверок консоли в индексе %s, из них зовётся "+
		"описанием процесса %s; подавлено ведомостью %d (%s). Перечень: %s",
		strconv.Itoa(len(selftests)), strconv.Itoa(called),
		len(consoleSelftestExempt), suppressedText, strings.Join(names, ", "))

	for _, finding := range adjudicateConsoleSelftestReach(selftests, consoleSelftestExempt) {
		t.Error(finding)
	}
}
