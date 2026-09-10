// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// greenstepnote_test.go — заметка ЗЕЛЁНОГО шага не должна быть неотличима от
// вердикта, и копия чужого сопоставителя обязана истекать сама.
//
// # Предмет
//
// `actions/setup-go` регистрирует на всё задание сопоставитель вывода (владелец
// `go`). Его образец
//
//	^\s*(.+\.go):(?:(\d+):(\d+):)? (.*)
//
// НЕ несёт поля `severity`, поэтому уровень аннотации по умолчанию — `error`, то
// есть ОТКАЗ. Совпавшая строка становится аннотацией уровня отказа независимо от
// того, каким кодом вышел напечатавший её шаг.
//
// Отсюда класс: гейт печатает заявленное исключение обычным текстом, выходит
// нулём — и читатель видит аннотацию УРОВНЯ ОТКАЗА рядом с настоящими отказами
// того же задания. Отличить можно только сверив номер шага. Цену заплатили целой
// полосой: такую аннотацию прочли как причину красноты и ушли чинить класс,
// которого нет (PRO-Robotech/kacho#2542).
//
// # Что здесь охраняется, а что — в другом месте
//
// Судить ВЫВОД гейтов может только тот, кто их запускает, поэтому судьёй работает
// `.github/scripts/assert-green-gate-notes.py`, и его способность упасть доказывает
// он сам (`--self-test`, инъекция в обе стороны). Здесь охраняется то, чего судья
// о себе знать не может:
//
//  1. **Копия сопоставителя обязана истекать сама.** Образец в
//     `.github/scripts/go-problem-matcher.json` — копия файла, которым мы не
//     владеем: он приезжает ВНУТРИ действия. Два места об одном предмете
//     расходятся молча, поэтому рядом с копией записан ПИН, с которого она снята,
//     а здесь он сверяется с КАЖДЫМ `uses: actions/setup-go@…` разобранного
//     workflow. Подняли действие — гейт краснеет, и копию перечитывают вместо
//     того, чтобы поверить ей.
//
//  2. **Судья обязан быть ПОЗВАН.** Гейт, который существует и никого не судит, —
//     эндемический класс этого дерева: у audit-list-filter он отказывал дважды
//     (страж, ничего не проверявший, и страж, которого не звали). Судья без
//     вызова — третий раз того же.
//
//  3. **Прямой вызов цели в обход судьи — находка.** Иначе запрет снимается одной
//     строкой `make -C services/<x> audit-list-filter`, и класс возвращается без
//     единого упоминания сопоставителя, которое можно найти.
//
// # Читается РАЗОБРАННЫЙ документ, а не текст
//
// Имена `actions/setup-go`, `audit-list-filter` и самого судьи стоят в этом файле
// в объяснении, и гейт, ищущий их подстрокой в сыром тексте, покраснел бы на
// собственном комментарии. Поэтому шаги берутся из РАЗОБРАННОГО YAML, где
// комментария не существует как узла.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: печатается, сколько
// workflow прочитано, сколько шагов установки Go найдено, сколько из них сошлись
// с пином, сколько вызовов судьи и сколько его самопроверок. Пустой обход — провал.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// matcherCopyPath — копия сопоставителя, снятая с пина действия.
const matcherCopyPath = ".github/scripts/go-problem-matcher.json"

// judgeScript — судья вывода зелёных гейтов.
const judgeScript = "assert-green-gate-notes.py"

// setupGoAction — действие, которое регистрирует сопоставитель.
const setupGoAction = "actions/setup-go"

// greenNoteDoc — то немногое из workflow, что нужно этому гейту.
type greenNoteDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Uses string `yaml:"uses"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// greenNoteCensus — сколько чего осмотрено.
type greenNoteCensus struct {
	SetupGo   int // шагов `uses: actions/setup-go@…`
	PinsOK    int // из них сошлись с пином копии
	JudgeRuns int // шагов, делегирующих судье
	SelfTests int // шагов самопроверки судьи
	DirectMk  int // шагов, зовущих цель через make напрямую
}

func (c *greenNoteCensus) add(o greenNoteCensus) {
	c.SetupGo += o.SetupGo
	c.PinsOK += o.PinsOK
	c.JudgeRuns += o.JudgeRuns
	c.SelfTests += o.SelfTests
	c.DirectMk += o.DirectMk
}

// pinOf — ссылка на версию действия. `actions/setup-go@abc` → `abc`.
func pinOf(uses string) string {
	u := strings.TrimSpace(uses)
	if i := strings.IndexByte(u, '@'); i >= 0 {
		return u[i+1:]
	}
	return ""
}

// shellCodeOf — тело `run:` без строк-комментариев оболочки.
//
// Комментарий шага не является исполняемой частью, и объяснение, называющее цель
// или судью, не есть их вызов. Разбор YAML снимает комментарии САМОГО документа,
// но `#` ВНУТРИ `run:` — обычный текст скрипта и до разбора не доходит: его надо
// снять здесь, иначе гейт краснеет на прозе, которая его же и объясняет.
func shellCodeOf(run string) string {
	var code []string
	for _, ln := range strings.Split(run, "\n") {
		if s := strings.TrimSpace(ln); strings.HasPrefix(s, "#") {
			continue
		}
		code = append(code, ln)
	}
	return strings.Join(code, "\n")
}

// callsTargetViaMake — шаг зовёт цель гейта через make напрямую.
//
// Оба признака требуются вместе: слово `audit-list-filter` встречается и в имени
// самого судьи, а `make` — в десятке чужих шагов.
func callsTargetViaMake(code string) bool {
	return strings.Contains(code, "make") &&
		strings.Contains(code, listFilterTarget) &&
		!strings.Contains(code, judgeScript)
}

// checkGreenStepNote — находки одного файла плюс его перепись. Вынесено
// отдельно, чтобы обход можно было доказать инъекцией на синтетическом
// содержимом, не трогая дерево.
func checkGreenStepNote(path, raw, wantPin string) ([]string, greenNoteCensus) {
	var doc greenNoteDoc
	var census greenNoteCensus
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, census
	}

	var findings []string
	for name, job := range doc.Jobs {
		for i, st := range job.Steps {
			where := path + ": job " + name + ", шаг #" + itoa(i+1)
			if st.Name != "" {
				where += " («" + st.Name + "»)"
			}

			if actionOf(st.Uses) == setupGoAction {
				census.SetupGo++
				if got := pinOf(st.Uses); got != wantPin {
					findings = append(findings, where+" — устанавливает Go с пина `"+got+
						"`, а копия сопоставителя в `"+matcherCopyPath+"` снята с `"+wantPin+
						"`. Сопоставитель приезжает ВНУТРИ действия: подняли действие — "+
						"образец мог смениться, и копия стала вторым местом об одном "+
						"предмете, из которых верно одно. Перечитай matchers.json с нового "+
						"пина и обнови копию вместе с записью о пине — верить старой копии "+
						"нельзя, а молча она не расходится только потому, что краснеет здесь")
				} else {
					census.PinsOK++
				}
			}

			if st.Run == "" {
				continue
			}
			code := shellCodeOf(st.Run)
			if strings.Contains(code, judgeScript) {
				if strings.Contains(code, "--self-test") {
					census.SelfTests++
				} else {
					census.JudgeRuns++
				}
			}
			if callsTargetViaMake(code) {
				census.DirectMk++
				findings = append(findings, where+" — зовёт цель `"+listFilterTarget+
					"` через make В ОБХОД судьи `"+judgeScript+"`. Вывод такого шага никто "+
					"не судит: строка вида `<файл>.go:<строка>:<столбец>: <текст>`, "+
					"напечатанная ЗЕЛЁНЫМ гейтом, станет аннотацией УРОВНЯ ОТКАЗА — у "+
					"сопоставителя `go` нет поля severity, — и будет неотличима от "+
					"настоящего вердикта. Пропусти перечень сервисов через судью")
			}
		}
	}
	sort.Strings(findings)
	return findings, census
}

// pinFromMatcherCopy — пин, с которого снята копия сопоставителя.
func pinFromMatcherCopy(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, matcherCopyPath))
	if err != nil {
		t.Fatalf("не прочитан %s: %v — предпосылка гейта не выполняется, "+
			"сверять пин не с чем", matcherCopyPath, err)
	}
	var doc struct {
		Provenance struct {
			Upstream string `json:"upstream"`
			Pin      string `json:"pin"`
		} `json:"_provenance"`
		ProblemMatcher []struct {
			Owner   string `json:"owner"`
			Pattern []struct {
				Regexp string `json:"regexp"`
			} `json:"pattern"`
		} `json:"problemMatcher"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s не разобран: %v", matcherCopyPath, err)
	}
	if doc.Provenance.Upstream != setupGoAction {
		t.Fatalf("%s снят с %q, а гейт сверяет пины %q — копия описывает не то действие",
			matcherCopyPath, doc.Provenance.Upstream, setupGoAction)
	}
	if doc.Provenance.Pin == "" {
		t.Fatalf("%s не называет пин, с которого снят, — копия чужого файла без "+
			"пина не может истечь сама и разойдётся с оригиналом молча", matcherCopyPath)
	}
	if len(doc.ProblemMatcher) != 1 || len(doc.ProblemMatcher[0].Pattern) != 1 ||
		doc.ProblemMatcher[0].Pattern[0].Regexp == "" {
		t.Fatalf("%s не несёт ровно одного образца — судья рассчитан на один, "+
			"копия разошлась с оригиналом", matcherCopyPath)
	}
	if doc.ProblemMatcher[0].Owner != "go" {
		t.Fatalf("%s описывает сопоставителя %q, а предмет — `go`",
			matcherCopyPath, doc.ProblemMatcher[0].Owner)
	}
	return doc.Provenance.Pin
}

// TestGreenStepNoteIsNotRaisedToAFailureAnnotation — по дереву.
func TestGreenStepNoteIsNotRaisedToAFailureAnnotation(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wantPin := pinFromMatcherCopy(t, root)
	files := listWorkflows(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение: обход, переставший находить workflow,
	// выходит зелёным на пустом множестве.
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто", workflowsDir)
	}

	var total greenNoteCensus
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		findings, census := checkGreenStepNote(f, string(raw), wantPin)
		total.add(census)
		for _, msg := range findings {
			t.Error(msg)
		}
	}

	// Предпосылка гейта: сопоставитель вообще регистрируется. Исчезнет установка
	// Go из конвейера — сверять пин станет не с чем, и «пинов сошлось 0» обязано
	// быть отличимо от «нарушений нет».
	if total.SetupGo == 0 {
		t.Errorf("ни один шаг не устанавливает Go (`%s`) — предпосылка гейта отпала: "+
			"сопоставитель, ради которого он написан, больше некому регистрировать. "+
			"Либо конвейер сменил форму, либо обход смотрит не туда; в обоих случаях "+
			"копия в %s осталась без предмета", setupGoAction, matcherCopyPath)
	}

	// Судья обязан быть позван — иначе он существует и никого не судит.
	if total.JudgeRuns == 0 {
		t.Errorf("ни один шаг не зовёт судью `%s` — он существует и не исполняется, "+
			"а значит вывод зелёных гейтов никто не судит. Это тот же класс, что дважды "+
			"отказывал у самой цели `%s`: страж, которого не зовут", judgeScript, listFilterTarget)
	}
	if total.SelfTests == 0 {
		t.Errorf("ни один шаг не зовёт `%s --self-test` — способность судьи упасть не "+
			"доказывается ничем, и «поднимаемых 0» становится неотличимо от «судья ослеп»",
			judgeScript)
	}

	t.Logf("осмотрено workflow: %d; шагов установки Go: %d (сошлись с пином %s: %d); "+
		"вызовов судьи: %d; самопроверок судьи: %d; прямых вызовов цели в обход судьи: %d",
		len(files), total.SetupGo, wantPin, total.PinsOK, total.JudgeRuns,
		total.SelfTests, total.DirectMk)
}
