// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleformatterversion_test.go — формат консоли судит ОДНА версия форматтера,
// и названа она объявлением, а не замком.
//
// # Предмет
//
// Форматтер — не библиотека, а СУДЬЯ: его версия решает, как выглядит законный
// файл. Девять пакетов консоли объявляют один и тот же скрипт `format:check`
// (`prettier . --check`) при побайтово одинаковом `.prettierrc` — и закрепляли
// ТРИ РАЗНЫЕ версии: 3.8.3 (host, dashboard), 3.9.4 (compute, storage, nlb,
// registry), 3.9.6 (vpc, iam, system — через замок корневого workspace).
//
// Расхождение не гипотетическое: объединение типов, помещающееся в предел строки,
// одна версия требует разбить, другая — собрать. Файл, приведённый в порядок одной
// версией, другая отвергает.
//
// # Почему объявление, а не только замок
//
// Диапазон у всех девяти был ОДИН и тот же — `^3.8.3`, — и именно поэтому дерево
// разъехалось: каретка разрешает 3.8.3, 3.9.4 и 3.9.6 одновременно, а какая из них
// достанется пакету, решает МОМЕНТ, в который его замок последний раз
// пересобирали. Свести замки и оставить каретку значит закрыть экземпляр: следующий
// `npm install` в одном пакете разведёт судей заново, и никто этого не заметит.
//
// Поэтому гейт судит ОБЕ стороны:
//
//	объявление ТОЧНОЕ (без диапазона) и ОДНО на все пакеты   — чтобы дрейф был невозможен впредь;
//	замок разрешает ровно объявленное                        — чтобы он не разошёлся с объявлением сегодня.
//
// Одной первой мало: объявление без замка ничего не исполняет. Одной второй мало:
// сведённые замки при каретке — состояние, а не свойство.
//
// # Чего гейт НЕ проверяет — названо, чтобы его не читали шире
//
// Он судит СОГЛАСИЕ судей, а не НАЛИЧИЕ производителя у проверки формата: то, что
// `format:check` вообще кто-то зовёт, держит соседний
// consoleformatproducer_test.go. Два гейта об одном скрипте и разных его свойствах;
// пересечения между ними нет by construction — один читает перечень вызовов,
// другой версии.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает, сколько
// манифестов осмотрел, сколько объявляют форматтер, сколько замков прочитал и
// сколько РАЗЛИЧНЫХ версий нашёл. Пустой обход — провал.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// consoleFormatterDep — имя пакета-судьи в манифестах консоли.
const consoleFormatterDep = "prettier"

// consoleExactVersionRe — точная версия semver без диапазона. Каретка, тильда,
// `*`, `x`, `>=` и объединение через `||` этой формой не описываются намеренно:
// каждая из них означает «пусть решит момент установки», то есть отказ назвать
// судью.
var consoleExactVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)

// TestConsoleFormatIsJudgedByOneVersion — вердикт на настоящем дереве.
func TestConsoleFormatIsJudgedByOneVersion(t *testing.T) {
	root := repoRoot(t)
	uiRoot := filepath.Join(root, "ui-future")

	facts := scanConsolePackages(t, uiRoot)
	if len(facts) == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}

	declared := map[string]string{}
	locked := map[string][]string{}
	locksRead := 0
	for _, f := range facts {
		if f.DeclaredPrettier != "" {
			declared[f.Name] = f.DeclaredPrettier
		}
		if f.HasLock {
			locksRead++
			if len(f.LockedPrettier) > 0 {
				locked[f.Name] = f.LockedPrettier
			}
		}
	}
	// Замок корневого workspace — судья для его членов (shared, vpc, iam, system):
	// своего замка у них нет by construction, поэтому без этой строки четыре
	// пакета из девяти остались бы вне наблюдения.
	if rootLocked, ok := consoleLockedFormatter(t, uiRoot); ok {
		locksRead++
		if len(rootLocked) > 0 {
			locked["."] = rootLocked
		}
	}

	if len(declared) == 0 {
		t.Fatal("ни один пакет консоли не объявляет " + consoleFormatterDep +
			" — либо обход сломан, либо форматтер сняли, не сняв этот гейт")
	}
	if len(locked) == 0 {
		t.Fatal("ни один замок консоли не закрепляет " + consoleFormatterDep +
			" — гейт судил бы объявление, которое ничего не исполняет")
	}

	for _, f := range judgeConsoleFormatterVersions(declared, locked) {
		t.Errorf("%s", f)
	}

	t.Logf("перепись: манифестов осмотрено — %d, объявляют %s — %d, замков прочитано — %d, "+
		"из них закрепляют %s — %d, различных версий — %d",
		len(facts), consoleFormatterDep, len(declared), locksRead,
		consoleFormatterDep, len(locked), len(consoleFormatterVersionSet(declared, locked)))
}

// judgeConsoleFormatterVersions — решающая часть, вынесенная из вердикта, чтобы её
// можно было проверить подставными входами, а не только состоянием дерева.
func judgeConsoleFormatterVersions(declared map[string]string, locked map[string][]string) []string {
	var findings []string

	exact := map[string]bool{}
	for name, spec := range declared {
		if !consoleExactVersionRe.MatchString(spec) {
			findings = append(findings,
				"пакет ui-future/"+name+" объявляет "+consoleFormatterDep+" как "+spec+
					" — это диапазон, а не судья: какую версию получит пакет, решит момент "+
					"установки, и следующий npm install разведёт судей заново")
			continue
		}
		exact[spec] = true
	}
	if len(exact) > 1 {
		findings = append(findings,
			"пакеты консоли объявляют "+consoleFormatterDep+" разными точными версиями ("+
				strings.Join(consoleSortedSet(exact), ", ")+") — судей столько же, сколько версий, "+
				"и файл, законный у одного, другой отвергает")
	}

	// Сверка замка с объявлением осмысленна только когда объявление ОДНО и точное:
	// иначе сравнивать не с чем, и о расхождении уже сказано выше.
	if len(exact) != 1 {
		sort.Strings(findings)
		return findings
	}
	want := consoleSortedSet(exact)[0]
	for _, owner := range sortedKeys(locked) {
		for _, got := range locked[owner] {
			if got != want {
				findings = append(findings,
					"замок "+consoleLockOwnerPath(owner)+" закрепляет "+consoleFormatterDep+
						" "+got+", а объявлен "+want+" — судит замок, поэтому объявление здесь "+
						"описывает не то, что исполняется")
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// consoleFormatterVersionSet — все версии судьи, встреченные в дереве: и
// объявленные, и закреплённые. Число этого множества — то, ради чего гейт стоит,
// поэтому оно печатается переписью, а не выводится читателем из перечня находок.
func consoleFormatterVersionSet(declared map[string]string, locked map[string][]string) []string {
	set := map[string]bool{}
	for _, spec := range declared {
		set[spec] = true
	}
	for _, versions := range locked {
		for _, v := range versions {
			set[v] = true
		}
	}
	return consoleSortedSet(set)
}

// consoleLockOwnerPath — как назвать замок в находке, чтобы за ним можно было
// пойти. Корневой обозначается точкой, потому что он судит членов workspace, а не
// себя.
func consoleLockOwnerPath(owner string) string {
	if owner == "." {
		return "ui-future/package-lock.json"
	}
	return "ui-future/" + owner + "/package-lock.json"
}

// consoleLockedFormatter — версии судьи, закреплённые замком каталога. Второй
// результат отличает «замок прочитан, судьи в нём нет» от «замка нет вовсе»:
// у членов workspace своего замка не бывает by construction, и молчание по такому
// каталогу находкой не является.
func consoleLockedFormatter(t *testing.T, dir string) ([]string, bool) {
	t.Helper()
	// #nosec G304 -- читается замок пакета этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return nil, false
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if json.Unmarshal(raw, &lock) != nil {
		t.Fatalf("не разобран замок %s", dir)
	}
	set := map[string]bool{}
	for path, entry := range lock.Packages {
		if path == "node_modules/"+consoleFormatterDep ||
			strings.HasSuffix(path, "/node_modules/"+consoleFormatterDep) {
			set[entry.Version] = true
		}
	}
	return consoleSortedSet(set), true
}

// consoleSortedSet — устойчивый порядок элементов множества: находки сравниваются
// построчно, и порядок обхода карты сделал бы их недетерминированными. Соседний
// sortedKeys принимает другую карту и потому не подходит.
func consoleSortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// consolePackageFacts — то, что гейты консоли читают об одном пакете. Обход один
// на все три оси (скрипты · конфигурация типов · закреплённый форматтер): три
// обходчика об одном дереве разошлись бы молча.
type consolePackageFacts struct {
	Name             string
	Scripts          map[string]string
	HasTSConfig      bool
	DeclaredPrettier string
	LockedPrettier   []string
	HasLock          bool
}

// scanConsolePackages — обход каталога консоли. Возвращает факты по КАЖДОМУ
// каталогу, несущему package.json, чтобы «ноль объявивших» было отличимо от
// «ноль прочитанных».
func scanConsolePackages(t *testing.T, uiRoot string) []consolePackageFacts {
	t.Helper()
	entries, err := os.ReadDir(uiRoot)
	if err != nil {
		t.Fatalf("не прочитан каталог консоли: %v", err)
	}
	var facts []consolePackageFacts
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "node_modules" {
			continue
		}
		dir := filepath.Join(uiRoot, e.Name())
		// #nosec G304 -- читается манифест пакета этого же репозитория.
		raw, readErr := os.ReadFile(filepath.Join(dir, "package.json"))
		if readErr != nil {
			continue
		}
		var pkg struct {
			Scripts         map[string]string `json:"scripts"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(raw, &pkg) != nil {
			t.Fatalf("не разобран package.json пакета %s", e.Name())
		}
		f := consolePackageFacts{Name: e.Name(), Scripts: pkg.Scripts}
		if _, statErr := os.Stat(filepath.Join(dir, "tsconfig.json")); statErr == nil {
			f.HasTSConfig = true
		}
		if v, ok := pkg.DevDependencies[consoleFormatterDep]; ok {
			f.DeclaredPrettier = v
		} else if v, ok := pkg.Dependencies[consoleFormatterDep]; ok {
			f.DeclaredPrettier = v
		}
		f.LockedPrettier, f.HasLock = consoleLockedFormatter(t, dir)
		facts = append(facts, f)
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].Name < facts[j].Name })
	return facts
}
