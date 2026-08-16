// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleprobeissuelink_test.go — гейт: ссылка `verifies #<N>` в сквозной пробе
// консоли называет номер задачи и стоит ВНУТРИ пробы.
//
// # Предмет
//
// Решение владельца 2026-08-15: на каждую находку по консоли пишется проба
// playwright. Норма — `.claude/rules/ui.md` §«Правило 12»; здесь механизм, без
// которого норма остаётся пожеланием (`multi-agent-flow.md` §11).
//
// Ссылка связывает пробу с предметом, ради которого она написана. Без связи
// проба через полгода неотличима от декоративной: её нельзя ни снять вместе с
// предметом, ни объяснить, ни защитить при чистке набора. Форма взята у
// регрессионных кейсов чёрного ящика (`testing.md` §«Test-only PR»: `# verifies
// <issue-url>`) и сведена к номеру — репозиторий у обоих один.
//
// # Что гейт держит
//
//	ФОРМА     ссылка называет номер: `#<N>`, положительное десятичное без
//	          ведущего нуля. `verifies #`, `verifies issue 423`, `verifies #0` —
//	          находки: номера, на который можно перейти, в них нет.
//	МЕСТО     ссылка стоит внутри вызова `test(…)`. На верхнем уровне файла или
//	          в теле `test.describe(…)` она ПЕРЕЖИВЁТ пробу, к которой относилась:
//	          пробу снимут, ссылка останется и будет утверждать про набор то,
//	          чего в нём уже нет (`doc-truthfulness`: утверждение, пережившее
//	          свой предмет).
//	ПЕРЕПИСЬ  сколько файлов прочитано, сколько проб распознано, сколько ссылок
//	          найдено. «Ноль находок» обязано быть отличимо от «ноль
//	          прочитанного»; ноль распознанных проб при непустом корпусе —
//	          поломка разбора, а не чистота.
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
// Он не может знать, что проба написана ПО НАХОДКЕ: у пробы без ссылки предмета
// в дереве не остаётся вовсе, и отличить её от обычной невозможно ничем
// механическим. Эта половина нормы держится вниманием и обзором PR — так и
// записано в правиле. Гейт держит вторую половину: ссылка, которая есть, обязана
// быть годной и стоять там, где умрёт вместе со своей пробой.
//
// Существование номера в трекере — измерение СЕТЕВОЕ, и по умолчанию гейт его не
// делает: вердикт проверки не вправе быть функцией доступности GitHub, а
// `go test ./...` не вправе ходить в сеть. Измерение включается ручкой
// `KACHO_ISSUE_TRACKER_CHECK=1` (в конвейере `gh` уже аутентифицирован), и
// перепись НАЗЫВАЕТ его состояние на каждом прогоне — «сверено 0 из N» никогда
// не выглядит как «сверено».
//
// # Пустой перечень — цель, а не поломка
//
// Ноль ссылок при непустом корпусе проб гейт проходит: перечень послаблений и
// перечень связей — разные вещи, но требование одно (`testing.md` §«Проба не
// имеет права падать на достижении своей цели»). Падение здесь толкало бы
// заводить ссылку ради зелёного.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consoleprobeissuelink_injection_test.go`:
// обе формы дефекта краснеют с координатой `файл:строка`, четыре законных
// близнеца молчат, и объём осмотренного при этом растёт.
package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// consoleProbeSpecDir — каталог сквозных проб консоли. Здесь и только здесь
// живёт проба, видящая продукт целиком: маршрутизацию, федерацию, край и
// материализацию прав. Модульная проба (`jest`) монтирует компонент и ничего из
// перечисленного не видит — см. правило.
const consoleProbeSpecDir = "ui-future/e2e/specs/"

// consoleProbeSpecSuffix — расширение файла спеки playwright.
const consoleProbeSpecSuffix = ".spec.ts"

// consoleProbeLinkFinding — негодная ссылка: координата, текст как написан,
// и вид находки.
type consoleProbeLinkFinding struct {
	File string
	Line int
	Text string
	Why  string
}

func (f consoleProbeLinkFinding) String() string {
	return f.File + ":" + strconv.Itoa(f.Line) + " [" + f.Why + "] " + f.Text
}

// consoleProbeLinkCensus — объём осмотренного. Считается независимо от находок:
// без него молчание гейта не отличить от того, что он ничего не прочитал.
type consoleProbeLinkCensus struct {
	Files   int   // файлов спек прочитано
	Probes  int   // вызовов test(…) распознано
	Markers int   // ссылок `verifies` встречено (в комментариях)
	Good    int   // из них годных: форма верна И место верно
	Issues  []int // различные номера задач, по возрастанию
}

// ─────────────────────────────────────────────────────────────────────────────
// Разбор TypeScript: маска кода и комментарии, БЕЗ потери смещений.
// ─────────────────────────────────────────────────────────────────────────────

// consoleProbeCommentSpan — комментарий: смещение начала и его содержимое.
type consoleProbeCommentSpan struct {
	At   int // смещение первого байта содержимого в исходнике
	Text string
}

// consoleProbeScan разделяет исходник на две части, отвечающие на РАЗНЫЕ вопросы:
//
//	mask      — байт-в-байт длины с исходником; всё, что не исполняется
//	            (комментарии, тела строковых, шаблонных и регулярных литералов),
//	            заменено пробелом, переводы строк сохранены. По маске
//	            спрашивают «где здесь вызов test(…) и где его скобка
//	            закрывается» — и спрашивают безопасно: скобка внутри строки или
//	            регулярного литерала в маску не попадает.
//	comments  — содержимое комментариев со смещениями. По ним спрашивают «где
//	            здесь ссылка на задачу»: ссылка живёт в комментарии, поэтому
//	            обычный для этого пакета разбор «только исполняемая часть» здесь
//	            не годится — он выбросил бы ровно предмет.
//
// Регулярные литералы распознаются намеренно: в этих спеках они несут слэши
// (`/\/vpc\/v1\/networks/`) и кавычки. Принятый за деление, такой литерал
// переключил бы разбор и съел бы половину файла — гейт замолчал бы, не сказав
// об этом.
func consoleProbeScan(src string) (mask []byte, comments []consoleProbeCommentSpan) {
	mask = make([]byte, len(src))
	blank := func(from, to int) {
		for i := from; i < to && i < len(src); i++ {
			if src[i] == '\n' {
				mask[i] = '\n'
				continue
			}
			mask[i] = ' '
		}
	}
	copy(mask, src)

	// prevSignificant — последний значимый байт кода: по нему решается, что
	// такое `/` — начало регулярного литерала или деление.
	prevSignificant := byte(0)
	regexAllowedAfter := func(b byte) bool {
		switch b {
		case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '\n', '+', '*', '~', '%', '<', '>', '^':
			return true
		}
		return false
	}

	i := 0
	for i < len(src) {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			start := i + 2
			j := start
			for j < len(src) && src[j] != '\n' {
				j++
			}
			comments = append(comments, consoleProbeCommentSpan{At: start, Text: src[start:j]})
			blank(i, j)
			i = j

		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			start := i + 2
			j := start
			for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
				j++
			}
			end := j
			if end > len(src) {
				end = len(src)
			}
			comments = append(comments, consoleProbeCommentSpan{At: start, Text: src[start:end]})
			blank(i, min(j+2, len(src)))
			i = min(j+2, len(src))

		case src[i] == '\'' || src[i] == '"' || src[i] == '`':
			quote := src[i]
			j := i + 1
			for j < len(src) && src[j] != quote {
				if src[j] == '\\' && j+1 < len(src) {
					j += 2
					continue
				}
				// Незакрытая строка не глотает остаток файла: одинарные и
				// двойные кавычки в TypeScript строку через перевод строки не
				// продолжают.
				if src[j] == '\n' && quote != '`' {
					break
				}
				j++
			}
			blank(i+1, j)
			i = min(j+1, len(src))
			prevSignificant = quote

		case src[i] == '/' && regexAllowedAfter(prevSignificant):
			j := i + 1
			closed := false
			inClass := false
			for j < len(src) && src[j] != '\n' {
				if src[j] == '\\' && j+1 < len(src) {
					j += 2
					continue
				}
				if src[j] == '[' {
					inClass = true
				} else if src[j] == ']' {
					inClass = false
				} else if src[j] == '/' && !inClass {
					closed = true
					break
				}
				j++
			}
			if !closed {
				// Не регулярный литерал — обычное деление.
				prevSignificant = '/'
				i++
				continue
			}
			blank(i+1, j)
			i = j + 1
			prevSignificant = '/'

		default:
			if src[i] > ' ' {
				prevSignificant = src[i]
			}
			i++
		}
	}
	return mask, comments
}

// consoleProbeCallRe — вызов пробы playwright. `test.describe` сюда НЕ входит
// намеренно: это группа, а не проба, и ссылка в ней переживает пробу, к которой
// относилась. `test.skip`/`test.fixme` тоже не входят — они запрещены
// (`e2e-flow.md` §2), и признавать их местом для ссылки значило бы их узаконить.
//
// Ведущий класс `[^.\w$]` отсекает МЕТОД с тем же именем: `re.test(s)`,
// `ждём.test(c)` — обычная проверка регулярного выражения, а не проба. Без него
// счётчик проб этого набора показывал 11 при семи пробах, то есть перепись
// подтверждала бы работу разбора числом, полученным из его поломки. Просмотр
// назад RE2 не умеет, поэтому класс входит в совпадение; закрывающая скобка
// всё равно берётся с конца, и позиция от этого не смещается.
var consoleProbeCallRe = regexp.MustCompile(`(?m)(?:^|[^.\w$])test(?:\.only)?\s*\(`)

// consoleProbeVerifiesRe — маркер связи. Ищется в СОДЕРЖИМОМ комментария.
var consoleProbeVerifiesRe = regexp.MustCompile(`(?i)\bverifies\b`)

// consoleProbeIssueRe — канонический хвост маркера: `#<N>`.
var consoleProbeIssueRe = regexp.MustCompile(`^[ \t]*#(\d{1,7})(?:[^\d]|$)`)

// consoleProbeSpan — полуинтервал смещений тела вызова test(…).
type consoleProbeSpan struct{ from, to int }

// consoleProbeSpans находит вызовы test(…) и сопоставляет каждой открывающей
// скобке её закрывающую. Считает по МАСКЕ: скобка в строке или в регулярном
// литерале в подсчёт не попадает.
func consoleProbeSpans(mask []byte) []consoleProbeSpan {
	var out []consoleProbeSpan
	for _, loc := range consoleProbeCallRe.FindAllIndex(mask, -1) {
		open := loc[1] - 1 // индекс '('
		depth := 0
		for j := open; j < len(mask); j++ {
			if mask[j] == '(' {
				depth++
				continue
			}
			if mask[j] == ')' {
				depth--
				if depth == 0 {
					out = append(out, consoleProbeSpan{from: open, to: j})
					break
				}
			}
		}
	}
	return out
}

// auditConsoleProbeIssueLinks — чистая функция над корпусом «путь → исходник».
// Гейт по дереву и инъекция зовут ЕЁ ЖЕ: проба, повторяющая логику гейта своей
// копией, доказывала бы свойство копии.
func auditConsoleProbeIssueLinks(sources map[string]string) (consoleProbeLinkCensus, []consoleProbeLinkFinding) {
	var (
		census   consoleProbeLinkCensus
		findings []consoleProbeLinkFinding
		issues   = map[int]bool{}
	)

	paths := make([]string, 0, len(sources))
	for rel := range sources {
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	for _, rel := range paths {
		src := sources[rel]
		census.Files++

		mask, comments := consoleProbeScan(src)
		spans := consoleProbeSpans(mask)
		census.Probes += len(spans)

		lineOf := func(off int) int { return 1 + strings.Count(src[:min(off, len(src))], "\n") }
		insideProbe := func(off int) bool {
			for _, s := range spans {
				if off > s.from && off < s.to {
					return true
				}
			}
			return false
		}

		for _, c := range comments {
			for _, m := range consoleProbeVerifiesRe.FindAllStringIndex(c.Text, -1) {
				at := c.At + m[0]
				census.Markers++

				tail := c.Text[m[1]:]
				line := strings.TrimRight(strings.SplitN(tail, "\n", 2)[0], " \t")
				text := strings.TrimSpace(c.Text[m[0]:m[1]] + line)

				sub := consoleProbeIssueRe.FindStringSubmatch(tail)
				if sub == nil || strings.HasPrefix(sub[1], "0") {
					findings = append(findings, consoleProbeLinkFinding{
						File: rel, Line: lineOf(at), Text: text,
						Why: "ссылка не называет номера задачи",
					})
					continue
				}
				n, err := strconv.Atoi(sub[1])
				if err != nil || n <= 0 {
					findings = append(findings, consoleProbeLinkFinding{
						File: rel, Line: lineOf(at), Text: text,
						Why: "ссылка не называет номера задачи",
					})
					continue
				}

				if !insideProbe(at) {
					findings = append(findings, consoleProbeLinkFinding{
						File: rel, Line: lineOf(at), Text: text,
						Why: "ссылка стоит вне пробы",
					})
					continue
				}

				census.Good++
				issues[n] = true
			}
		}
	}

	for n := range issues {
		census.Issues = append(census.Issues, n)
	}
	sort.Ints(census.Issues)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return census, findings
}

// ─────────────────────────────────────────────────────────────────────────────
// Сверка номеров с трекером — измерение СЕТЕВОЕ, по явной просьбе.
// ─────────────────────────────────────────────────────────────────────────────

// consoleProbeTrackerKnob — ручка, включающая сверку. По умолчанию выключена:
// вердикт гейта не вправе быть функцией доступности GitHub.
const consoleProbeTrackerKnob = "KACHO_ISSUE_TRACKER_CHECK"

// consoleProbeMissingMarker — ответ трекера, доказывающий, что номера НЕТ. Всё
// прочее (сеть, 5xx, отсутствие прав) — не отказ трекера, а несостоявшееся
// измерение: оно считается отдельно и печатается, а не проглатывается
// (`security.md` §Hardening-инварианты, п. 8).
const consoleProbeMissingMarker = "Could not resolve to an issue or pull request"

// resolveConsoleProbeIssues спрашивает трекер про каждый номер. Возвращает:
// сколько сверено, каких номеров нет, и сколько сверить не удалось.
func resolveConsoleProbeIssues(numbers []int) (checked int, missing []int, unresolved int, cfgErr string) {
	if _, err := exec.LookPath("gh"); err != nil {
		return 0, nil, len(numbers),
			"сверка запрошена ручкой " + consoleProbeTrackerKnob + "=1, но `gh` в PATH нет — " +
				"это НАСТРОЙКА, а не сбой: измерение объявлено включённым и не выполняется"
	}
	for _, n := range numbers {
		out, err := exec.Command("gh", "issue", "view", strconv.Itoa(n), "--json", "number").CombinedOutput()
		switch {
		case err == nil:
			checked++
		case strings.Contains(string(out), consoleProbeMissingMarker):
			checked++
			missing = append(missing, n)
		default:
			unresolved++
		}
	}
	return checked, missing, unresolved, ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт по дереву.
// ─────────────────────────────────────────────────────────────────────────────

// consoleProbeSpecSources — исходники сквозных проб консоли. Единица счёта —
// отслеживаемый git-элемент: ровно то множество увидит свежий клон и конвейер.
func consoleProbeSpecSources(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, consoleProbeSpecDir) || !strings.HasSuffix(rel, consoleProbeSpecSuffix) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v — состав спек неизвестен, значит вердикт был бы утверждением ни о чём", rel, err)
		}
		out[rel] = string(b)
	}
	return out
}

// TestConsoleProbeIssueLinksNameATaskAndLiveInsideTheProbe — гейт нормы
// `ui.md` §«Правило 12».
func TestConsoleProbeIssueLinksNameATaskAndLiveInsideTheProbe(t *testing.T) {
	root := repoRoot(t)
	sources := consoleProbeSpecSources(t, root)

	// Предпосылка гейта. Каталог мог переехать, расширение — смениться; в обоих
	// случаях зелёный вердикт ниже был бы получен даром.
	if len(sources) == 0 {
		t.Fatalf("в %s*%s не найдено ни одной спеки — гейт беспредметен. "+
			"«Ноль находок» здесь неотличимо от «ноль прочитанного»",
			consoleProbeSpecDir, consoleProbeSpecSuffix)
	}

	census, findings := auditConsoleProbeIssueLinks(sources)

	// Предпосылка дискриминатора: он делит ссылки на «внутри пробы» и «вне».
	// Если проб не распознано ни одной, делить нечего, и молчание гейта
	// означало бы поломку разбора, а не чистоту набора.
	if census.Probes == 0 {
		t.Error("в спеках не распознано ни одного вызова test(…) — разбор сломан. " +
			"Всякая ссылка была бы объявлена стоящей вне пробы, а её отсутствие — чистотой")
	}

	for _, f := range findings {
		t.Errorf("%s:%d — %s: %q\n\n"+
			"Ссылка связывает пробу с находкой, ради которой она написана.\n"+
			"  · форма — `// verifies #<N>`: номер задачи этого репозитория, по которому "+
			"читатель перейдёт и увидит признак, предмет и предикат снятия;\n"+
			"  · место — ВНУТРИ вызова test(…): на верхнем уровне файла и в теле "+
			"test.describe(…) ссылка переживёт пробу, к которой относилась, и станет "+
			"утверждать про набор то, чего в нём уже нет.\n"+
			"Норма: .claude/rules/ui.md §«Правило 12».",
			f.File, f.Line, f.Why, f.Text)
	}

	// Сверка с трекером — измерение сетевое, по явной просьбе. Состояние
	// измерения называется ВСЕГДА: «сверено 0 из N» не должно выглядеть как
	// «сверено».
	tracker := "НЕ ЗАПРАШИВАЛАСЬ (" + consoleProbeTrackerKnob + "=1)"
	if os.Getenv(consoleProbeTrackerKnob) == "1" {
		checked, missing, unresolved, cfgErr := resolveConsoleProbeIssues(census.Issues)
		if cfgErr != "" {
			t.Error(cfgErr)
		}
		for _, n := range missing {
			t.Errorf("ссылка называет задачу #%d, которой в трекере нет. "+
				"Ссылка в никуда хуже её отсутствия: она выглядит связью и читателя "+
				"никуда не приводит", n)
		}
		tracker = "сверено " + strconv.Itoa(checked) + " из " + strconv.Itoa(len(census.Issues)) +
			", отсутствует " + strconv.Itoa(len(missing)) +
			", не удалось сверить " + strconv.Itoa(unresolved)
	}

	// Пустой перечень ссылок — ЦЕЛЬ, а не поломка: гейт объявляет перепись и
	// проходит. Падение здесь толкало бы заводить ссылку ради зелёного.
	t.Logf("перепись: спек прочитано %d, проб распознано %d, ссылок verifies %d, "+
		"из них годных %d, задач названо %d %v, находок %d; трекер: %s",
		census.Files, census.Probes, census.Markers, census.Good,
		len(census.Issues), census.Issues, len(findings), tracker)
}
