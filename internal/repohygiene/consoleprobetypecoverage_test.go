// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleprobetypecoverage_test.go — гейт «пробу консоли читает РАЗБОР ТИПОВ, где
// бы она ни лежала».
//
// # Предмет
//
// У сквозных проб консоли ДВА каталога, и это осознанная форма: рядом с
// исполняемым набором стоит каталог проб, ждущих своего условия
// (`specs-awaiting-journal-owner/`, гейт
// TestProbesAwaitingTheirConditionExpireWhenItArrives). Вне `testDir` они стоят
// намеренно — заведомо красная проба не вправе валить каждое слияние. Каталог
// ожидания бывает ПУСТ — это его цель, а не отмена формы, — и охват разбором
// заводится под КАТАЛОГ, а не под сегодняшний список файлов в нём.
//
// Но «не исполняется» и «не читается ничем» — разные вещи, и вторая никем не
// решалась. Пока каталог вне `include` проекта TypeScript, проба там не
// разбирается по типам: неразрешимый импорт, чужое написание помощника,
// параметр без типа — всё это выглядит исправным состоянием ровно до дня, когда
// условие создадут и пробы переедут в исполняемый набор. Тогда цена приходит
// вся сразу и не тогда, когда её удобно платить: пробы становятся обязательным
// контекстом слияния и краснеют по причинам, к их условию отношения не имеющим.
//
// Цена измерена, а не предположена: на день заведения гейта единственная проба
// каталога ожидания несла ДЕВЯТЬ ошибок типов, и корнем был импорт помощника по
// пути, которого не существует (`./fixtures` при `fixtures.ts` в соседнем
// каталоге). Ни одна из девяти не могла быть замечена никаким прогоном.
//
// # Что гейт держит
//
//	ПОКРЫТИЕ  каждый отслеживаемый `.ts` пакета сквозных проб подпадает хотя бы
//	          под один шаблон `include` его `tsconfig.json`.
//	ПЕРЕПИСЬ  сколько файлов осмотрено, сколько покрыто, сколькими шаблонами.
//	          «Ноль находок» обязано быть отличимо от «ноль прочитанного».
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
// Он не запускает `tsc` и о самих типах не судит вовсе: его предмет — ЧИТАЕТ ЛИ
// кто-нибудь эти файлы, а не что он в них найдёт. Разбор типов делает работа
// `typecheck` в `ui.yml`; гейт следит лишь за тем, чтобы её область не сузилась
// молча — обратно к состоянию «файл есть, читателя нет».
//
// Он не требует и не вправе требовать, чтобы каталог ожидания вошёл в `testDir`
// прогонщика: тогда заведомо красная проба валила бы каждое слияние — ровно то,
// чего форма долга и избегает. Исполнение и разбор типов — разные вопросы.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consoleprobetypecoverage_injection_test.go`.
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

// consoleProbePackageDir — пакет сквозных проб консоли: и исполняемый набор, и
// каталог ожидания, и оснастка прогона живут под ним.
const consoleProbePackageDir = "ui-future/e2e/"

// consoleProbeTsconfigPath — проект TypeScript этого пакета: единственное место,
// откуда разбор типов узнаёт, что ему читать.
const consoleProbeTsconfigPath = "ui-future/e2e/tsconfig.json"

// consoleProbeTypeCensus — объём осмотренного. Считается независимо от находок:
// без него молчание гейта не отличить от того, что он ничего не прочитал.
type consoleProbeTypeCensus struct {
	Patterns []string // шаблоны include, как они объявлены
	Files    int      // отслеживаемых .ts в пакете
	Covered  int      // из них покрытых хотя бы одним шаблоном
	Dirs     []string // каталоги пакета, несущие .ts, по возрастанию
}

// consoleProbeTypeFinding — файл, которого не читает разбор типов.
type consoleProbeTypeFinding struct {
	File string
	Why  string
}

// stripJSONCComments снимает комментарии из JSON с комментариями, НЕ трогая
// содержимое строк. Наивная замена по образцу здесь негодна: путь внутри строки
// («http://…», «specs/**/*.ts») комментарием не является, а принятый за него
// съедает объявление и делает перечень шаблонов пустым — то есть гейт объявил бы
// непокрытым ВСЁ, включая исполняемый набор.
func stripJSONCComments(src string) string {
	out := make([]byte, 0, len(src))
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				i++
				out = append(out, src[i])
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// consoleProbeTsconfig — та часть проекта, которая отвечает на вопрос «что
// читается». `exclude` намеренно не разбирается: в этом проекте его нет, а
// поддержка объявления, которого в дереве не существует, была бы кодом без
// предмета.
type consoleProbeTsconfig struct {
	Include []string `json:"include"`
}

// tsIncludeMatcher переводит шаблон `include` в предикат над путём, ОТНОСИТЕЛЬНЫМ
// каталогу проекта. Поддержаны три формы, и все три встречаются в дереве:
//
//	specs/**/*.ts        — `**` любое число сегментов, `*` любые знаки кроме `/`
//	playwright.config.ts — точное имя
//	scripts              — голое имя каталога: TypeScript читает его рекурсивно
//
// Форма, которой распознаватель не знает, объявила бы покрытый файл непокрытым —
// то есть дала бы находку там, где читатель есть. Поэтому неизвестная форма
// здесь не «пропускается молча»: любой шаблон переводится буквально, а его
// негодность видна переписью — она печатает шаблоны, как они объявлены.
func tsIncludeMatcher(pattern string) *regexp.Regexp {
	p := strings.TrimPrefix(strings.TrimSpace(pattern), "./")
	if p == "" {
		return nil
	}
	// Голое имя каталога (нет подстановок и нет расширения) — рекурсивное чтение.
	if !strings.ContainsAny(p, "*?") && filepath.Ext(p) == "" {
		p = strings.TrimSuffix(p, "/") + "/**/*"
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		switch {
		case strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:[^/]+/)*")
			i += 2
		case p[i] == '*':
			b.WriteString("[^/]*")
		case p[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(p[i])))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// auditConsoleProbeTypeCoverage — чистая функция над объявлением проекта и
// составом пакета. Гейт по дереву и инъекция зовут ЕЁ ЖЕ: проба, повторяющая
// логику гейта своей копией, доказывала бы свойство копии.
//
// `rels` — пути .ts ОТНОСИТЕЛЬНО каталога проекта.
func auditConsoleProbeTypeCoverage(tsconfigSrc string, rels []string) (consoleProbeTypeCensus, []consoleProbeTypeFinding, error) {
	var cfg consoleProbeTsconfig
	if err := json.Unmarshal([]byte(stripJSONCComments(tsconfigSrc)), &cfg); err != nil {
		return consoleProbeTypeCensus{}, nil, err
	}

	census := consoleProbeTypeCensus{Patterns: cfg.Include}
	matchers := make([]*regexp.Regexp, 0, len(cfg.Include))
	for _, p := range cfg.Include {
		if m := tsIncludeMatcher(p); m != nil {
			matchers = append(matchers, m)
		}
	}

	dirs := map[string]bool{}
	var findings []consoleProbeTypeFinding
	sorted := append([]string(nil), rels...)
	sort.Strings(sorted)
	for _, rel := range sorted {
		census.Files++
		dir := filepath.Dir(rel)
		if dir == "." {
			dir = "(корень пакета)"
		}
		dirs[dir] = true
		covered := false
		for _, m := range matchers {
			if m.MatchString(rel) {
				covered = true
				break
			}
		}
		if covered {
			census.Covered++
			continue
		}
		findings = append(findings, consoleProbeTypeFinding{
			File: rel,
			Why:  "не подпадает ни под один шаблон include",
		})
	}
	for d := range dirs {
		census.Dirs = append(census.Dirs, d)
	}
	sort.Strings(census.Dirs)
	return census, findings, nil
}

// consoleProbeTypeSources — состав пакета глазами разбора типов. Единица счёта —
// отслеживаемый git-элемент: ровно то множество увидит свежий клон и конвейер, а
// установленные зависимости и отчёты прогонов в индексе не лежат и потому в счёт
// не идут by construction.
func consoleProbeTypeSources(t *testing.T, root string) []string {
	t.Helper()
	var rels []string
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, consoleProbePackageDir) || !strings.HasSuffix(rel, ".ts") {
			continue
		}
		rels = append(rels, strings.TrimPrefix(rel, consoleProbePackageDir))
	}
	return rels
}

// TestConsoleProbesAreReadByTheTypeChecker — гейт: у пробы консоли есть читатель.
func TestConsoleProbesAreReadByTheTypeChecker(t *testing.T) {
	root := repoRoot(t)

	body, err := os.ReadFile(filepath.Join(root, consoleProbeTsconfigPath))
	if err != nil {
		t.Fatalf("проект TypeScript %s не читается (%v) — гейту нечего охранять, "+
			"и его молчание не означало бы, что пробы кто-то читает", consoleProbeTsconfigPath, err)
	}

	rels := consoleProbeTypeSources(t, root)
	if len(rels) == 0 {
		t.Fatalf("в %s не найдено ни одного отслеживаемого .ts — гейт беспредметен. "+
			"«Ноль находок» здесь неотличимо от «ноль прочитанного»", consoleProbePackageDir)
	}

	census, findings, parseErr := auditConsoleProbeTypeCoverage(string(body), rels)
	if parseErr != nil {
		t.Fatalf("%s не разбирается (%v): перечень читаемого неизвестен, "+
			"и всякий вердикт о покрытии был бы утверждением ни о чём", consoleProbeTsconfigPath, parseErr)
	}

	// Предпосылка гейта: объявление, из которого он судит, непусто. Пустой
	// include объявил бы непокрытым ВЕСЬ пакет — то есть находки были бы
	// свойством разбора, а не дерева.
	if len(census.Patterns) == 0 {
		t.Fatalf("%s не объявляет include — разбор сломан либо проект переустроен: "+
			"каждый файл был бы назван непокрытым, и находки ничего не значили бы",
			consoleProbeTsconfigPath)
	}

	for _, f := range findings {
		t.Errorf("%s%s — %s.\n\n"+
			"Файл пробы существует, лежит в индексе и НЕ ЧИТАЕТСЯ разбором типов: "+
			"неразрешимый импорт, чужое написание помощника, параметр без типа "+
			"выглядят исправным состоянием до дня, когда пробу переведут в "+
			"исполняемый набор.\n"+
			"Исходов два: внести каталог в include проекта %s — либо снять файл "+
			"вместе с предметом, ради которого он написан. Третьего («пусть лежит, "+
			"его всё равно никто не гоняет») нет: непрочитанное отличается от "+
			"исправного только тем, что об этом никто не узнает.",
			consoleProbePackageDir, f.File, f.Why, consoleProbeTsconfigPath)
	}

	t.Logf("перепись: шаблонов include %d %v · каталогов с .ts %d %v · файлов %d · "+
		"из них читаются разбором типов %d · находок %d",
		len(census.Patterns), census.Patterns, len(census.Dirs), census.Dirs,
		census.Files, census.Covered, len(findings))
}
