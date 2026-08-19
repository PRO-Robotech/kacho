// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleruncategory_test.go — «условие не создано» обязано быть отличимо от
// «пробы упали», и различение обязано ВИДЕТЬ все шаги джобы.
//
// # Предмет
//
// Сводка запроса на слияние показывает одну строку с именем проверки и её цвет.
// Цвет один и тот же у «продукт сломан» и у «условие не создано» — а это разные
// исходы: второй из вердикта не вычитается и в зелёное не зачитывается
// (`e2e-flow.md` §1, §6). Читатель сводки различить их не может: он видит
// красноту, а не журнал.
//
// Наблюдалось (#726): шаг установки системных пакетов браузера не уложился в
// свой предел, пробы не начинались вовсе — и это пришло в сводку неотличимым от
// падения проб. Двадцать пять минут стенда потрачены, вердикта по консоли не
// получила НИ ОДНА проба, а разбирали продукт.
//
// # Что здесь считается защитой
//
// Джоба, выносящая вердикт прогоном проб браузером, обязана нести РАЗМЕТЧИК
// ИСХОДА — шаг, который читает исходы всех остальных шагов и называет категорию
// словом. Требования к нему три, и каждое несущее:
//
//  1. Разметчик исполняется, ПЕРЕЖИВАЯ красный шаг. Условие без функции
//     состояния вычисляется с подразумеваемым `success()`, поэтому разметчик,
//     объявленный без неё, не исполнится ровно тогда, когда он и нужен.
//  2. Ему назван шаг проб, и такой шаг в джобе действительно есть. Опечатка в
//     имени иначе даёт молчаливый отказ разметчика уже на прогоне.
//  3. У КАЖДОГО шага джобы есть `id`. Шаг без него в контекст `steps` не
//     попадает вовсе — разметчик его не видит и отнести к «условие не создано»
//     не может. Это и есть та дыра, через которую класс возвращается: добавили
//     шаг подготовки без `id`, он упал, и категория снова стала «красное».
//
// Само правило классификации (что считать условием, что вердиктом) проверяется
// не здесь, а самопроверкой разметчика: `console-run-category.py --self-test`,
// одиннадцать случаев в обе стороны. Здесь — только то, что он провязан и видит
// весь предмет.
//
// # Читается разобранный документ, а не текст
//
// Имена `playwright test` и `console-run-category.py` стоят в этом файле в
// объяснении, и гейт, ищущий их подстрокой в сыром тексте, покраснел бы на
// собственном комментарии. Поэтому шаги берутся из РАЗОБРАННОГО YAML, где
// комментария не существует как узла.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько файлов конвейеров прочитал, сколько джоб с прогоном проб нашёл и
// сколько шагов в них осмотрел. Ноль джоб с прогоном проб — провал: у гейта не
// осталось предмета, и молчать об этом значит обещать защиту, которой нет.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// categoryWorkflowDoc — то немногое из workflow, что нужно этому гейту.
type categoryWorkflowDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
			Uses string `yaml:"uses"`
			Run  string `yaml:"run"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// shellExecutablePart — тело шага БЕЗ строк-комментариев оболочки.
//
// Гейт обязан читать исполняемое, а не текст: упоминание в комментарии вызовом
// не является. Наш собственный шаг подготовки объясняет соседей и называет их
// по имени — без этой очистки гейт нашёл бы разметчика в комментарии и судил бы
// не тот шаг. Снимаются строки, у которых первый непробельный символ — решётка;
// концевые комментарии не трогаем намеренно: решётка встречается внутри строк и
// адресов, и «умная» очистка врала бы чаще, чем помогала.
func shellExecutablePart(run string) string {
	var b strings.Builder
	for _, line := range strings.Split(run, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// reProbeRunStep — прогон сквозных проб браузером. Именно он выносит вердикт,
// и именно от него отсчитывается «до» и «после».
var reProbeRunStep = regexp.MustCompile(`playwright\s+test\b`)

// reCategoriserCall — вызов разметчика исхода.
var reCategoriserCall = regexp.MustCompile(`console-run-category\.py`)

// reProbeStepArg — имя шага проб, названное разметчику.
//
// Значение берётся как ЛЮБАЯ непробельная последовательность, а не по перечню
// разрешённых символов: перечень «латиница, цифры, дефис» описывал бы принятое у
// нас именование, а не то, что бывает написано. Опечатка кириллицей — ровно тот
// случай, ради которого гейт и заведён, — под такой перечень не подпадала бы и
// проезжала молча. Проверено инъекцией: первая редакция этого выражения на ней
// и провалилась.
var reProbeStepArg = regexp.MustCompile(`--probe-step\s+(\S+)`)

// reNamedStepArgs — прочие имена шагов, названные разметчику (вердиктные и
// служебные). Опечатка в них молча меняет разряд шага, поэтому существование
// каждого проверяется.
var reNamedStepArgs = regexp.MustCompile(`--(?:verdict-step|bookkeeping)\s+(\S+)`)

// runCategoryCensus — сколько чего осмотрено.
type runCategoryCensus struct {
	Files     int
	ProbeJobs int // джоб, выносящих вердикт прогоном проб
	Steps     int // шагов в таких джобах
}

func (c *runCategoryCensus) add(o runCategoryCensus) {
	c.Files += o.Files
	c.ProbeJobs += o.ProbeJobs
	c.Steps += o.Steps
}

// checkRunCategory — находки одного файла плюс его перепись. Вынесено отдельно,
// чтобы обход можно было доказать инъекцией на синтетическом содержимом, не
// трогая дерево.
func checkRunCategory(path, raw string) ([]string, runCategoryCensus) {
	var doc categoryWorkflowDoc
	census := runCategoryCensus{Files: 1}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, census
	}

	var findings []string
	for name, job := range doc.Jobs {
		probeIdx, categoriserIdx := -1, -1
		exec := make([]string, len(job.Steps))
		for i, st := range job.Steps {
			exec[i] = shellExecutablePart(st.Run)
			if reProbeRunStep.MatchString(exec[i]) && probeIdx < 0 {
				probeIdx = i
			}
			if reCategoriserCall.MatchString(exec[i]) &&
				!strings.Contains(exec[i], "--self-test") && categoriserIdx < 0 {
				categoriserIdx = i
			}
		}
		if probeIdx < 0 {
			continue // не наш предмет: джоба вердикта пробами не выносит
		}
		census.ProbeJobs++
		census.Steps += len(job.Steps)

		if categoriserIdx < 0 {
			findings = append(findings, path+": job "+name+" — вердикт выносится прогоном "+
				"проб (шаг #"+strconv.Itoa(probeIdx+1)+"), но разметчика исхода в джобе нет. "+
				"Тогда «условие не создано» и «продукт сломан» приходят в сводку одним "+
				"красным, и читатель уходит разбирать продукт там, где сломалась подготовка")
			continue
		}

		cat := job.Steps[categoriserIdx]
		if !ifSurvivesRed(cat.If) {
			findings = append(findings, path+": job "+name+" — разметчик исхода (шаг #"+
				strconv.Itoa(categoriserIdx+1)+", «"+cat.Name+"») объявлен с условием `"+
				strings.TrimSpace(cat.If)+"`, которое красный шаг отменяет. Условие без функции "+
				"состояния вычисляется с подразумеваемым `success()` — то есть разметчик не "+
				"исполнится ровно тогда, когда он и нужен. Нужна `always()`, `!cancelled()` "+
				"или `failure()`")
		}

		// Все id джобы. Шаг без id невидим контексту `steps`, а значит и разметчику.
		ids := map[string]bool{}
		for i, st := range job.Steps {
			if strings.TrimSpace(st.ID) != "" {
				ids[st.ID] = true
				continue
			}
			what := st.Name
			if what == "" {
				what = st.Uses
			}
			findings = append(findings, path+": job "+name+" — шаг #"+strconv.Itoa(i+1)+
				" («"+what+"») без `id`. Шаг без него не попадает в контекст `steps` вовсе, "+
				"поэтому разметчик исхода его не видит: его отказ не будет отнесён к "+
				"«условие не создано» и придёт в сводку неотличимым от красноты проб")
		}

		// Шаг проб назван разметчику — и назван верно.
		m := reProbeStepArg.FindStringSubmatch(exec[categoriserIdx])
		switch {
		case m == nil:
			findings = append(findings, path+": job "+name+" — разметчику исхода не назван "+
				"шаг проб (`--probe-step`). Без него он не отличит вердикт от подготовки")
		case job.Steps[probeIdx].ID == "":
			// уже названо выше как шаг без id — второй раз не повторяем
		case strings.TrimSuffix(m[1], "\\") != job.Steps[probeIdx].ID:
			findings = append(findings, path+": job "+name+" — разметчику назван шаг проб `"+
				m[1]+"`, а прогон проб идёт шагом с id `"+job.Steps[probeIdx].ID+"`. "+
				"Разметчик откажется судить, и различения не будет ни на одном прогоне")
		}

		// Прочие названные разметчику шаги обязаны существовать: опечатка молча
		// переводит шаг из вердиктных в условные и меняет категорию исхода.
		for _, mm := range reNamedStepArgs.FindAllStringSubmatch(exec[categoriserIdx], -1) {
			for _, id := range strings.Split(strings.TrimSuffix(mm[1], "\\"), ",") {
				id = strings.TrimSpace(id)
				if id == "" || ids[id] {
					continue
				}
				findings = append(findings, path+": job "+name+" — разметчику назван шаг `"+
					id+"`, которого в джобе нет. Опечатка здесь не видна ничем: шаг молча "+
					"остаётся в другом разряде, и категория исхода получается не та")
			}
		}
	}
	sort.Strings(findings)
	return findings, census
}

// TestEveryStepOfTheProbeJobIsVisibleToTheCategoriser — по дереву.
func TestEveryStepOfTheProbeJobIsVisibleToTheCategoriser(t *testing.T) {
	root := repoRoot(t)
	files := listWorkflows(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение: обход, переставший находить workflow,
	// выходит зелёным на пустом множестве.
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто", workflowsDir)
	}

	var total runCategoryCensus
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", rel, err)
			continue
		}
		findings, census := checkRunCategory(rel, string(raw))
		total.add(census)
		for _, msg := range findings {
			t.Error(msg)
		}
	}

	t.Logf("осмотрено: файлов конвейеров %d, джоб с прогоном проб браузером %d, шагов в них %d",
		total.Files, total.ProbeJobs, total.Steps)

	// Предпосылка гейта: предмет существует. Ноль таких джоб значит, что
	// сторожить больше нечего, — и об этом надо сказать, а не молча зеленеть.
	if total.ProbeJobs == 0 {
		t.Fatalf("ни одной джобы с прогоном проб браузером не найдено — у гейта не " +
			"осталось предмета. Либо пробы вынесли из конвейеров, либо обход перестал " +
			"их узнавать; молчать здесь значило бы обещать защиту, которой нет")
	}
}

// TestRunCategoryDetectorSeesBothWays — инъекция в обе стороны: заведомый
// экземпляр обязан быть пойман, законный близнец той же формы — пропущен.
func TestRunCategoryDetectorSeesBothWays(t *testing.T) {
	// Законная джоба: прогон проб, у всех шагов есть id, разметчик переживает
	// красное и назван верно.
	legit := "jobs:\n  p:\n    steps:\n" +
		"      - uses: actions/checkout@v7\n        id: checkout\n" +
		"      - name: подготовка\n        id: prep\n        run: npm ci\n" +
		"      - name: прогон проб\n        id: probes\n        run: npx playwright test\n" +
		"      - name: разметка\n        id: category\n        if: ${{ always() }}\n" +
		"        run: python3 .github/scripts/console-run-category.py --steps s.json --probe-step probes --bookkeeping category\n"

	cases := []struct {
		name    string
		yaml    string
		wantHit bool
	}{
		{name: "законная джоба — молчит", yaml: legit, wantHit: false},
		{
			name:    "шаг подготовки без id — находка",
			yaml:    strings.Replace(legit, "        id: prep\n", "", 1),
			wantHit: true,
		},
		{
			name:    "разметчик с условием, которое красное отменяет — находка",
			yaml:    strings.Replace(legit, "if: ${{ always() }}", "if: ${{ steps.probes.outcome == 'success' }}", 1),
			wantHit: true,
		},
		{
			name: "разметчика в джобе нет вовсе — находка",
			yaml: "jobs:\n  p:\n    steps:\n" +
				"      - name: прогон проб\n        id: probes\n        run: npx playwright test\n",
			wantHit: true,
		},
		{
			name:    "разметчику назван несуществующий шаг проб — находка",
			yaml:    strings.Replace(legit, "--probe-step probes", "--probe-step probe", 1),
			wantHit: true,
		},
		{
			name:    "разметчику назван несуществующий служебный шаг — находка",
			yaml:    strings.Replace(legit, "--bookkeeping category", "--bookkeeping category,опечатка", 1),
			wantHit: true,
		},
		{
			name: "джоба без прогона проб — НЕ наш предмет, молчит даже без id",
			yaml: "jobs:\n  lint:\n    steps:\n      - uses: actions/checkout@v7\n" +
				"      - name: линт\n        run: golangci-lint run\n",
			wantHit: false,
		},
		{
			name: "самопроверка разметчика вызовом не считается — молчит",
			yaml: strings.Replace(legit,
				"      - name: подготовка\n        id: prep\n        run: npm ci\n",
				"      - name: самопроверка\n        id: st\n"+
					"        run: python3 .github/scripts/console-run-category.py --self-test\n", 1),
			wantHit: false,
		},
		{
			name: "имя разметчика только в комментарии шага — молчит",
			yaml: strings.Replace(legit, "        run: npm ci\n",
				"        run: |\n          # console-run-category.py здесь только упомянут\n          npm ci\n", 1),
			wantHit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, _ := checkRunCategory("синтетика.yml", tc.yaml)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v: %v", tc.wantHit, got, findings)
			}
		})
	}
}
