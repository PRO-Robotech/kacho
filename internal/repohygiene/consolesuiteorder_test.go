// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consolesuiteorder_test.go — порядок суит консоли задаётся деревом, а не машиной.
//
// # Предмет
//
// Порядок по умолчанию у jest вычисляет `@jest/test-sequencer` (30.4.1,
// `build/index.js`, `sort()`) по четырём величинам, и ни одна из них не является
// деревом: упала ли суита в ПРЕДЫДУЩЕМ прогоне · есть ли о ней запись в кэше
// длительностей `perf-cache-<id>` · сама длительность · РАЗМЕР ФАЙЛА, когда записи
// нет. Значит порядок меняется от прогона к прогону на одном и том же дереве:
// после красного он не тот, что после зелёного, а правка комментария меняет размер
// файла и вместе с ним очередь.
//
// # Почему это чинится, а не терпится
//
// Пока порядок плавает, суита, зависящая от порядка, даёт РАЗНОЕ число падений на
// одном дереве — так и было записано в PRO-Robotech/kacho#461: «4–6 упавших, число
// меняется между прогонами». Такую красноту нельзя ни воспроизвести, ни отличить от
// настоящей находки, и она обесценивает весь прогон: следующий читатель приучается
// его игнорировать.
//
// Закрепление порядка зависимость от порядка НЕ прячет — оно делает её
// воспроизводимой. Обратный порядок остаётся достижим одной ручкой того же
// секвенсора (`KACHO_JEST_SUITE_ORDER=reverse`), поэтому «а проверить другой
// порядок» не требует второго механизма.
//
// # Что именно требуется
//
//	каждый пакет консоли, гоняющий jest, объявляет `testSequencer`,
//	и объявленный файл существует.
//
// # Разбор идёт по КОДУ, а не по тексту — и это здесь не педантизм
//
// Провязка в конфигах сопровождается комментарием, который слово `testSequencer`
// содержит. Гейт, ищущий подстроку в сыром файле, нашёл бы её в объяснении и
// остался бы зелёным при снятом ключе (`testing.md` §«Гейт на класс», п. 4).
// Поэтому конфиг прогоняется через `tsScan`: комментарии удалены, тела литералов
// вычищены, а сами литералы отданы отдельным списком — по ним и резолвится путь.
//
// # Своя предпосылка
//
// Обход обязан видеть КАЖДОГО, кто гоняет jest. Пакет, объявивший прогон без
// конфига, который гейт умеет прочесть, — слепая зона: гейт молчал бы о нём,
// не сказав об этом. Поэтому перечень пакетов с jest в `scripts.test` сверяется
// с перечнем найденных конфигов, и расхождение — находка.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consolesuiteorder_injection_test.go`.
package repohygiene

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Предикат класса.
// ─────────────────────────────────────────────────────────────────────────────

type suiteOrderFinding struct {
	Config string // путь конфига относительно корня репозитория
	Why    string
}

// declaredSequencerRef выдаёт ссылку на секвенсор, объявленную в ИСПОЛНЯЕМОЙ части
// конфига, и признак того, что ключ вообще объявлен.
//
// Литерал берётся по позиции: `tsScan` заменяет каждое тело строки на `""` и
// складывает тела в отдельный список в том же порядке, поэтому номер нужного
// литерала — это число `""`, встреченных до него.
func declaredSequencerRef(src string) (ref string, declared bool) {
	code, literals := tsScan(src)

	at := strings.Index(code, "testSequencer")
	if at < 0 {
		return "", false
	}
	// Поиск литерала ограничен ОДНОЙ строкой объявления. Без границы «следующий
	// литерал в файле» мог бы прийти из соседнего ключа (`moduleNameMapper` их несёт
	// десятками), и гейт резолвил бы чужой путь как секвенсор — то есть молчал бы,
	// прочитав не то.
	line := code[at:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	rel := strings.Index(line, `""`)
	if rel < 0 {
		// Ключ объявлен, но значение — не строковый литерал (переменная, вызов без
		// аргумента-строки). Проверить существование файла нечем; это не «чисто».
		return "", true
	}
	n := strings.Count(code[:at+rel], `""`)
	if n >= len(literals) {
		return "", true
	}
	return literals[n], true
}

// auditConsoleSuiteOrder — предикат класса. Вход — соответствие «путь конфига → его
// исходник»; `exists` отвечает, разрешается ли ссылка относительно каталога пакета.
// Инъекция гоняет ЭТУ ЖЕ функцию, а не свою копию логики.
//
// Возвращает находки и число конфигов, объявивших порядок годно (законные близнецы):
// молчание без непустого второго числа означало бы «ничего не прочитал».
func auditConsoleSuiteOrder(
	configs map[string]string,
	exists func(configRel, ref string) bool,
) (findings []suiteOrderFinding, good int) {
	for rel, src := range configs {
		ref, declared := declaredSequencerRef(src)
		switch {
		case !declared:
			findings = append(findings, suiteOrderFinding{
				Config: rel,
				Why:    "порядок суит не объявлен: ключа `testSequencer` в исполняемой части конфига нет",
			})
		case ref == "":
			findings = append(findings, suiteOrderFinding{
				Config: rel,
				Why:    "`testSequencer` объявлен, но значение — не путь: проверить, что секвенсор существует, нечем",
			})
		case !exists(rel, ref):
			findings = append(findings, suiteOrderFinding{
				Config: rel,
				Why:    "`testSequencer` указывает на " + ref + " — такого файла нет",
			})
		default:
			good++
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Config < findings[j].Config })
	return findings, good
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт по дереву.
// ─────────────────────────────────────────────────────────────────────────────

// jestTestScript — `scripts.test`, зовущий jest. Именно по нему определяется, кто
// обязан объявить порядок: конфиг без прогона никого не касается, прогон без
// конфига — слепая зона.
var jestTestScript = regexp.MustCompile(`"test"\s*:\s*"[^"]*jest[^"]*"`)

// consoleJestConfigs собирает конфиги консоли и перечень пакетов, которые гоняют
// jest, — второе нужно, чтобы обход мог доказать свою полноту.
func consoleJestConfigs(t *testing.T, root string) (configs map[string]string, jestPackages []string) {
	t.Helper()
	configs = map[string]string{}
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, "ui-future/") {
			continue
		}
		switch {
		case strings.HasSuffix(rel, "/jest.config.cjs"),
			strings.HasSuffix(rel, "/jest.config.js"),
			strings.HasSuffix(rel, "/jest.config.mjs"),
			strings.HasSuffix(rel, "/jest.config.ts"):
			b, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatalf("%s: %v — состав конфигов консоли неизвестен, значит вердикт был бы утверждением ни о чём", rel, err)
			}
			configs[rel] = string(b)
		case strings.HasSuffix(rel, "/package.json") && strings.Count(rel, "/") == 2:
			b, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			if jestTestScript.Match(b) {
				jestPackages = append(jestPackages, path.Dir(rel))
			}
		}
	}
	sort.Strings(jestPackages)
	return configs, jestPackages
}

// TestConsoleSuiteOrderIsAPropertyOfTheTree — порядок суит консоли задан деревом.
func TestConsoleSuiteOrderIsAPropertyOfTheTree(t *testing.T) {
	root := repoRoot(t)
	configs, jestPackages := consoleJestConfigs(t, root)

	if len(configs) == 0 {
		t.Fatal("обход не нашёл ни одного jest-конфига консоли — гейт беспредметен. " +
			"Либо каталог переехал, либо имя конфига изменилось; в обоих случаях " +
			"зелёный вердикт ниже был бы получен даром.")
	}

	// Своя предпосылка: обход видит каждого, кто гоняет jest. Пакет с прогоном, но
	// без читаемого конфига, — слепая зона, а не чистота.
	withConfig := map[string]bool{}
	for rel := range configs {
		withConfig[path.Dir(rel)] = true
	}
	for _, pkg := range jestPackages {
		if !withConfig[pkg] {
			t.Errorf("%s гоняет jest, но конфига, который гейт умеет прочесть, у него нет — "+
				"порядок его суит не проверяется ничем, и молчание гейта об этом пакете "+
				"означает «не прочитал», а не «чисто». Исход: вынести конфигурацию в "+
				"jest.config.cjs рядом с package.json либо расширить обход.", pkg)
		}
	}
	if len(jestPackages) == 0 {
		t.Error("ни один пакет консоли не объявляет прогон jest — распознавание прогона сломано; " +
			"полноту обхода доказать нечем")
	}

	exists := func(configRel, ref string) bool {
		dir := filepath.Join(root, filepath.FromSlash(path.Dir(configRel)))
		p := strings.ReplaceAll(ref, "<rootDir>", dir)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, filepath.FromSlash(p))
		}
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}

	findings, good := auditConsoleSuiteOrder(configs, exists)

	for _, f := range findings {
		t.Errorf("%s: %s\n\n"+
			"Без явного порядка его задаёт `@jest/test-sequencer` — по тому, что упало в "+
			"ПРОШЛЫЙ раз, по кэшу длительностей этой машины и по размеру файла. Тогда суита, "+
			"зависящая от порядка, даёт разное число падений на одном дереве, и краснота "+
			"перестаёт что-либо значить (#461).\n"+
			"Исход: `testSequencer: require.resolve(\"../shared/jest-sequencer-by-path.cjs\")` "+
			"в конфиге пакета. Обратный порядок для проверки — "+
			"`KACHO_JEST_SUITE_ORDER=reverse npm test`, тем же секвенсором.",
			f.Config, f.Why)
	}

	t.Logf("перепись: jest-конфигов консоли осмотрено %d, пакетов с прогоном jest %d, "+
		"порядок объявлен годно у %d, находок %d", len(configs), len(jestPackages), good, len(findings))
}
