// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictgateabsenceisnotaskip.go — ОТСУТСТВИЕ ВЕРДИКТНОГО ГЕЙТА ОБЯЗАНО БЫТЬ
// ГРОМКИМ: прогонщик не вправе обращать «файла гейта нет» в пропуск.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Прогонщики сквозных наборов адресуют вердиктный гейт ПУТЁМ и на
// нерезолвящемся пути переходили на сырой счёт по суитам:
//
//	if [ "$GATE" != "true" ] || [ ! -f "$GATE_SCRIPT" ]; then
//	  echo "CI gate skipped …"
//	  exit "$RC"          ← сырой счёт выдан за вердикт
//	fi
//
// Пропуск молчалив BY CONSTRUCTION: исход выглядит ровно так же, как у прогона
// с исполненным гейтом. А не исполнились при этом три исхода, перепись
// «исполнено N из M» и отказ на немом отчёте — то есть всё, ради чего гейт и
// существует. Вердиктный гейт в этом дереве ОДИН на все наборы и лежит в
// каталоге вынесенной службы: он уезжает вместе с ней, и ветвь «файла нет ⇒
// пропущено» после переезда становится истинной НАВСЕГДА. Послабление без
// предиката снятия.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА СОСТОЯНИЯ, КОТОРЫЕ НЕЛЬЗЯ ДЕРЖАТЬ В ОДНОМ УСЛОВИИ
//
//	GATE != true        решение ОПЕРАТОРА: гейт выключен ручкой осознанно.
//	                    Сырой счёт здесь — законный вердикт.
//	! -f "$GATE_SCRIPT" гейта НЕТ ПО АДРЕСУ. Это «не выполнилось»: не зелено,
//	                    не красно, вердикта нет вовсе.
//
// Слитые в одно условие, они дают одну ветвь на две причины — и читатель
// журнала различить их не может даже по тексту, если тот не называет пути.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОПУЛЯЦИЯ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// Перечня имён прогонщиков здесь нет и не будет: выписанный, он разошёлся бы с
// деревом на первом же новом прогонщике — молча, потому что новый в него просто
// не попал бы. Судится всякая переменная рецепта, которая ОДНОВРЕМЕННО:
//
//  1. несёт путь скрипта (значение оканчивается на `.sh`);
//  2. проверяется на существование (`-f` / `-e`);
//  3. ИСПОЛНЯЕТСЯ (`bash "$VAR"` либо `"$VAR"` командой);
//  4. её проверка охраняет ветвь, КОТОРАЯ ЗАВЕРШАЕТ ПРОГОН (`exit`).
//
// Первые три — определение «внешнего проверяющего»: скрипт, который только
// зовут, под ось не подпадает (его отсутствие уронит сам вызов), а переменная,
// которую только проверяют, проверяющим не является.
//
// # Почему четвёртый признак несущий, а не уточняющий
//
// Первых трёх МАЛО, и это измерено, а не предположено: на них гейт дал две
// ложные находки из двух в первом же прогоне. Обе — прогонщики ВОЛН
// (`run-failclosed.sh`, `run-ceremony.sh`). Их отсутствие вердикта не подменяет:
// волна просто не идёт, отчётов не оставляет, и вердиктный гейт ниже докладывает
// по её коллекциям `(no-report)` и КРАСНЕЕТ. Отсутствие там уже громкое — только
// производитель другой.
//
// Различает их ровно ветвь: вердиктный гейт есть последнее слово прогонщика, и
// его отсутствие ведёт к `exit`; волна — блок посреди рецепта, из которого
// исполнение продолжается. Признак, краснеющий на верной работе, отключают
// первым, поэтому ось сужена ПО СВОЙСТВУ, а не списком прощённых имён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ГЕЙТ НЕ ДЕЛАЕТ
//
// Он НЕ переносит вердиктный гейт и не судит, где тот должен лежать: это
// отдельная работа. Его предмет — только то, что отсутствие обязано быть
// громким, куда бы гейт ни переехал.
//
// Он НЕ судит текст сообщения. Требование «сообщение называет путь» проверяемо
// лишь по подстроке, а подстрока нашла бы имя переменной и в комментарии — то
// есть гейт краснел бы на собственном объяснении. Требование остаётся нормой и
// держится обзором; машинно держится КОД ВОЗВРАТА, который подделать нечем.
//
// Разбор идёт по строкам с СРЕЗАННЫМИ комментариями: `#` в этом дереве не
// открывает строку кода, а объяснение запрета стоит рядом с самим запретом.
package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// VerdictGateFinding — одна находка с координатой.
type VerdictGateFinding struct {
	File string
	Line int
	Var  string
	Kind string // "два-состояния-в-одном-условии" | "отсутствие-выдано-за-вердикт"
	Why  string
}

func (f VerdictGateFinding) String() string {
	return fmt.Sprintf("%s:%d [%s] переменная %s — %s", f.File, f.Line, f.Kind, f.Var, f.Why)
}

// VerdictGateCensus — объём осмотренного. Четыре числа, а не одно: «прогонщиков
// 0» означает непрочитанный корпус, «проверяющих 0» — сломанный распознаватель,
// «условий 0» — что ни одно существование не проверяется вовсе, и только при
// живых трёх «находок 0» есть чистота.
type VerdictGateCensus struct {
	Files      int
	ScriptVars int
	Checkers   int
	Conditions int
	// Skipped — условий, отведённых ЧЕТВЁРТЫМ признаком: охраняемая ветвь
	// прогон не завершает, значит проверяющий вердиктным гейтом не является.
	// Печатается отдельно, иначе сужение оси было бы неотличимо от её слепоты.
	Skipped int
	PerFile map[string]int
}

func (c VerdictGateCensus) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "рецептов прочитано %d, переменных с путём скрипта %d, "+
		"из них проверяемых-и-исполняемых %d, условий существования осмотрено %d "+
		"(из них отведено как не завершающие прогон %d)",
		c.Files, c.ScriptVars, c.Checkers, c.Conditions, c.Skipped)
	names := make([]string, 0, len(c.PerFile))
	for n := range c.PerFile {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "\n  %-40s проверяющих %d", n, c.PerFile[n])
	}
	return b.String()
}

var (
	// verdictGateAssignRe — присваивание пути скрипта переменной.
	verdictGateAssignRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=.*\.sh"?\s*$`)
	// verdictGateExistsRe — проверка существования переменной: `-f "$X"` / `-e "$X"`.
	verdictGateExistsRe = regexp.MustCompile(`-[fe]\s+"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?`)
	// verdictGateExecRe — исполнение: `bash "$X"`, `sh "$X"`, `"$X"` командой.
	verdictGateExecRe = regexp.MustCompile(`(?:^|[;&|(]\s*|\b(?:bash|sh|source|\.)\s+)"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?`)
	// verdictGateExitVarRe — выход ЗНАЧЕНИЕМ ПЕРЕМЕННОЙ, то есть сырым счётом.
	verdictGateExitVarRe = regexp.MustCompile(`\bexit\s+"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?`)
)

// shellCodeLines — строки файла без комментариев, с исходными номерами.
//
// Срезается ХВОСТОВОЙ комментарий и отбрасывается строка, начинающаяся с `#`.
// Полного разбора оболочки здесь нет намеренно: `#` внутри строкового литерала
// был бы срезан ошибочно, но условие с `-f` и `exit` внутри литерала — форма, в
// дереве не встречающаяся, и заводить под неё разборщик оболочки значило бы
// платить за случай, которого нет.
func shellCodeLines(src string) []struct {
	N    int
	Text string
} {
	var out []struct {
		N    int
		Text string
	}
	for i, raw := range strings.Split(src, "\n") {
		line := raw
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") {
			continue
		}
		if j := strings.Index(line, " #"); j >= 0 {
			line = line[:j]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, struct {
			N    int
			Text string
		}{i + 1, line})
	}
	return out
}

// scanVerdictGateRunner — обе оси по одному рецепту.
//
// Второй результат — переменные-проверяющие: они же перепись, и без них «ноль
// находок» неотличимо от «распознаватель ничего не узнал».
func scanVerdictGateRunner(rel, src string) ([]VerdictGateFinding, []string, int, int, int) {
	lines := shellCodeLines(src)

	scriptVars := map[string]bool{}
	tested := map[string]bool{}
	executed := map[string]bool{}
	for _, l := range lines {
		for _, part := range strings.Split(l.Text, ";") {
			if m := verdictGateAssignRe.FindStringSubmatch(strings.TrimSpace(part)); m != nil {
				scriptVars[m[1]] = true
			}
		}
		for _, m := range verdictGateExistsRe.FindAllStringSubmatch(l.Text, -1) {
			tested[m[1]] = true
		}
		for _, m := range verdictGateExecRe.FindAllStringSubmatch(l.Text, -1) {
			executed[m[1]] = true
		}
	}

	var checkers []string
	for v := range scriptVars {
		if tested[v] && executed[v] {
			checkers = append(checkers, v)
		}
	}
	sort.Strings(checkers)
	isChecker := map[string]bool{}
	for _, v := range checkers {
		isChecker[v] = true
	}

	var findings []VerdictGateFinding
	conditions, skipped := 0, 0

	for i, l := range lines {
		ms := verdictGateExistsRe.FindAllStringSubmatch(l.Text, -1)
		if len(ms) == 0 {
			continue
		}
		var subject string
		for _, m := range ms {
			if isChecker[m[1]] {
				subject = m[1]
				break
			}
		}
		if subject == "" {
			continue
		}
		conditions++

		// ПРИЗНАК ЧЕТВЁРТЫЙ: ветвь, которую охраняет эта проверка, обязана
		// ЗАВЕРШАТЬ прогон. Блок, из которого исполнение продолжается, есть
		// прогонщик волны, а не вердиктный гейт (разбор — в шапке).
		if !verdictGateBranchTerminates(lines, i) {
			skipped++
			continue
		}

		// ОСЬ ПЕРВАЯ: существование проверяющего слито в одно условие с
		// проверкой ЧУЖОГО предмета — обычно ручки. Тогда «выключено ручкой» и
		// «файла нет» ведут в одну ветвь, и различить их нельзя даже читателю.
		if verdictGateConditionMixesSubjects(l.Text, subject) {
			findings = append(findings, VerdictGateFinding{
				File: rel, Line: l.N, Var: subject,
				Kind: "два-состояния-в-одном-условии",
				Why: "проверка существования вердиктного гейта стоит в ОДНОМ условии с " +
					"проверкой чужого предмета (ручки): «выключено осознанно» и «гейта нет " +
					"по адресу» ведут в одну ветвь. Первое — решение оператора, второе — " +
					"«не выполнилось»; разведите их по разным условиям",
			})
		}

		// ОСЬ ВТОРАЯ: ветвь, которую берут при ОТСУТСТВИИ, выходит значением
		// переменной — то есть сырым счётом, выданным за вердикт.
		if !verdictGateAbsenceBranch(l.Text, subject) {
			continue
		}
		for j := i; j < len(lines) && j <= i+12; j++ {
			if j > i && strings.TrimSpace(lines[j].Text) == "fi" {
				break
			}
			m := verdictGateExitVarRe.FindStringSubmatch(lines[j].Text)
			if m == nil {
				continue
			}
			findings = append(findings, VerdictGateFinding{
				File: rel, Line: lines[j].N, Var: subject,
				Kind: "отсутствие-выдано-за-вердикт",
				Why: "ветвь «гейта нет» выходит значением $" + m[1] + " — сырым счётом " +
					"прогонщика. Это НЕ вердикт: три исхода, перепись исполненности и " +
					"отказ на немом отчёте не исполнились, а прогон выглядит так же, как " +
					"прогон с исполненным гейтом. Отсутствие обязано быть отказом " +
					"(ненулевой КОНСТАНТОЙ — тем кодом, которым сам прогонщик называет недействительный прогон)",
			})
			break
		}
	}

	sort.Slice(findings, func(a, b int) bool {
		if findings[a].Line != findings[b].Line {
			return findings[a].Line < findings[b].Line
		}
		return findings[a].Kind < findings[b].Kind
	})
	return findings, checkers, len(scriptVars), conditions, skipped
}

// verdictGateBranchTerminates — блок `if`, начинающийся на строке start,
// содержит `exit` до своего закрывающего `fi`.
//
// Глубина считается по ключевым словам, а не полным разбором оболочки: форм
// внутри одного рецепта немного, а разборщик стоил бы дороже различаемого.
// Признак ОДНОСТОРОННИЙ намеренно — «не нашли exit» толкуется в пользу того, что
// предмет не наш, поэтому ошибка распознавателя даёт пропуск, а не ложную
// находку. Пропуск при этом СЧИТАЕТСЯ переписью и печатается.
func verdictGateBranchTerminates(lines []struct {
	N    int
	Text string
}, start int) bool {
	depth := 0
	for j := start; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j].Text)
		switch {
		case strings.HasPrefix(t, "if ") || strings.HasPrefix(t, "if["):
			depth++
		case t == "fi" || strings.HasPrefix(t, "fi "):
			depth--
			if depth <= 0 {
				return false
			}
		}
		if j > start && strings.Contains(lines[j].Text, "exit ") {
			return true
		}
	}
	return false
}

// verdictGateConditionMixesSubjects — условие проверяет ЕЩЁ и чужой предмет.
//
// «Чужой» значит «не эта переменная»: `[ -f "$X" ] && [ -x "$X" ]` — две
// проверки одного предмета и находкой не является; `[ "$GATE" != "true" ] ||
// [ ! -f "$X" ]` — двух разных, и это она.
func verdictGateConditionMixesSubjects(line, subject string) bool {
	idx := strings.Index(line, "&&")
	if j := strings.Index(line, "||"); j >= 0 && (idx < 0 || j < idx) {
		idx = j
	}
	if idx < 0 {
		return false
	}
	for _, part := range regexp.MustCompile(`\|\||&&`).Split(line, -1) {
		if strings.Contains(part, "$"+subject) || strings.Contains(part, "${"+subject+"}") {
			continue
		}
		if strings.Contains(part, "$") || strings.Contains(part, "=") {
			return true
		}
	}
	return false
}

// verdictGateAbsenceBranch — ветвь, которую берут при ОТСУТСТВИИ файла.
//
// Это `if [ ! -f "$X" ]`: отрицание стоит перед проверкой. Форма `if [ -f "$X" ]`
// ведёт в ветвь ПРИСУТСТВИЯ, и выход сырым счётом там законен — гейт исполнился.
func verdictGateAbsenceBranch(line, subject string) bool {
	re := regexp.MustCompile(`!\s*-[fe]\s+"?\$\{?` + regexp.QuoteMeta(subject) + `\}?"?`)
	return re.MatchString(line)
}

// judgeVerdictGateRunners — свод по корпусу плюс предпосылки.
func judgeVerdictGateRunners(findings []VerdictGateFinding, census VerdictGateCensus) []string {
	switch {
	case census.Files == 0:
		return []string{"рецептов прогона не прочитано ни одного: обход пуст, и вердикт " +
			"относился бы к непрочитанному"}
	case census.ScriptVars == 0:
		return []string{"ни одной переменной с путём скрипта не распознано: разбор " +
			"разошёлся с деревом. Это отказ, а не чистота"}
	case census.Checkers == 0:
		return []string{"ни один рецепт не проверяет существование скрипта, который сам же " +
			"исполняет: популяция оси пуста — либо вердиктный гейт больше никем не " +
			"зовётся, либо распознаватель ослеп. Это отказ, а не чистота"}
	}
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.String())
	}
	return out
}
