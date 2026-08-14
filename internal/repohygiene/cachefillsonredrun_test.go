// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cachefillsonredrun_test.go — кэш конвейера обязан наполняться прогоном,
// который за него заплатил, а не только зелёным.
//
// # Предмет
//
// Комбинированное действие `actions/cache@…` объявляет своё сохранение шагом
// «после», и это объявление читается в его собственном описании:
//
//	runs:
//	  post: 'dist/save/index.js'
//	  post-if: "success()"
//
// То есть добытое содержимое уезжает в кэш ТОЛЬКО если задание дошло до конца
// зелёным. Прогон, который заплатил за содержимое полную цену и упал позже,
// не оставляет после себя ничего — следующий прогон платит ту же цену снова.
//
// Два следствия, и оба наблюдались в этом дереве:
//
//  1. **Замкнутый круг.** Если кэшируется ПРЕДПОСЫЛКА шага, чей отказ и роняет
//     задание, наполнение недостижимо ПО ПОСТРОЕНИЮ: кэш наполнится только на
//     прогоне, которому он не был нужен. Мера выглядит принятой, работает
//     никогда. Наблюдалось на волне сквозных проб консоли: шаг добычи браузера
//     не укладывался в свой предел, задание падало, сохранение не исполнялось,
//     следующий прогон снова начинал с промаха. В журнале это видно прямо —
//     из шагов «после» отработал только `Post Run actions/checkout@v7`
//     (у checkout `post-if: always()`), а шага «после» у кэша в журнале нет
//     вовсе.
//
//  2. **Перевёрнутый смысл.** Если кэшируется то, что прогон НАКАПЛИВАЕТ
//     (корпус фаззера), то на красном — то есть ровно тогда, когда накопленное
//     интереснее всего, — накопленное в кэш не уезжает. Корпус растёт только
//     теми ночами, которые ничего не нашли.
//
// # Почему `save-always: true` не принимается
//
// Ручка существует и по имени обещает ровно нужное, но её собственное описание
// в действии несёт предупреждение об отказе от неё: «save-always does not work
// as intended and will be removed in a future release. A separate
// `actions/cache/restore` step should be used instead». Принять её значило бы
// принять меру, о неработоспособности которой заявляет её же автор, — тот самый
// класс «форма проверки без содержания», только на уровне конвейера.
//
// # Что здесь считается защитой
//
// Разделённая форма: `actions/cache/restore` восстанавливает, `actions/cache/save`
// сохраняет — и у сохранения есть СВОЙ `if`, ПЕРЕЖИВАЮЩИЙ красный шаг.
// Второе обязательно и не косметика: условие шага БЕЗ функции состояния
// вычисляется так, как если бы к нему приписали `success()`, поэтому
// `if: steps.x.outcome == 'success'` после упавшего соседа не исполнится —
// разделение формы куплено, а свойство нет. Принимается `always()`,
// `!cancelled()`, `failure()`.
//
// Задание, которое ВОССТАНАВЛИВАЕТ, но нигде не СОХРАНЯЕТ, — та же находка с
// другой стороны: иначе запрет снимается удалением сохранения, и круг
// замыкается снова, но уже без единого упоминания кэша, которое можно найти.
//
// # Читается разобранный документ, а не текст
//
// Имена `actions/cache@…` и `save-always` стоят в этом файле в объяснении, и
// гейт, ищущий их подстрокой в сыром тексте, покраснел бы на собственном
// комментарии. Поэтому шаги берутся из РАЗОБРАННОГО YAML, где комментария не
// существует как узла.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько файлов конвейеров прочитал и сколько шагов работы с кэшем в них
// нашёл — по каждой форме отдельно. Пустой обход — провал.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// cacheWorkflowDoc — то немногое из workflow, что нужно этому гейту.
type cacheWorkflowDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Uses string `yaml:"uses"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// cacheCensus — сколько шагов какой формы осмотрено.
type cacheCensus struct {
	Combined int // actions/cache@…
	Restore  int // actions/cache/restore@…
	Save     int // actions/cache/save@…
}

func (c *cacheCensus) add(o cacheCensus) {
	c.Combined += o.Combined
	c.Restore += o.Restore
	c.Save += o.Save
}

// actionOf — имя действия без ссылки на версию. `actions/cache@v6` → `actions/cache`.
func actionOf(uses string) string {
	u := strings.TrimSpace(uses)
	if i := strings.IndexByte(u, '@'); i >= 0 {
		u = u[:i]
	}
	return u
}

// ifSurvivesRed — условие шага переживает упавшего соседа.
//
// Условие БЕЗ функции состояния вычисляется с подразумеваемым `success()`,
// поэтому пустое условие и любое условие без такой функции красным шагом
// отменяются.
func ifSurvivesRed(cond string) bool {
	c := strings.TrimSpace(cond)
	if c == "" {
		return false
	}
	for _, fn := range []string{"always()", "cancelled()", "failure()"} {
		if strings.Contains(c, fn) {
			return true
		}
	}
	return false
}

// checkCacheFillOnRed — находки одного файла плюс его перепись. Вынесено
// отдельно, чтобы обход можно было доказать инъекцией на синтетическом
// содержимом, не трогая дерево.
func checkCacheFillOnRed(path, raw string) ([]string, cacheCensus) {
	var doc cacheWorkflowDoc
	var census cacheCensus
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, census
	}

	var findings []string
	for name, job := range doc.Jobs {
		restores, saves := 0, 0
		for i, st := range job.Steps {
			where := path + ": job " + name + ", шаг #" + itoa(i+1)
			if st.Name != "" {
				where += " («" + st.Name + "»)"
			}
			switch actionOf(st.Uses) {
			case "actions/cache":
				census.Combined++
				findings = append(findings, where+" — комбинированное `actions/cache`. "+
					"Его сохранение объявлено `post-if: success()`, то есть кэш наполняется "+
					"ТОЛЬКО зелёным прогоном: прогон, заплативший за содержимое полную цену и "+
					"упавший позже, не оставляет ничего, и следующий платит снова. Если "+
					"кэшируется предпосылка шага, чей отказ и роняет задание, наполнение "+
					"недостижимо по построению. Разнеси на `actions/cache/restore` и "+
					"`actions/cache/save` с `if`, переживающим красный шаг. "+
					"`save-always: true` не принимается: само действие объявляет её нерабочей")
			case "actions/cache/restore":
				census.Restore++
				restores++
			case "actions/cache/save":
				census.Save++
				saves++
				if !ifSurvivesRed(st.If) {
					findings = append(findings, where+" — `actions/cache/save` с условием "+
						"`"+strings.TrimSpace(st.If)+"`, которое красный шаг отменяет. Условие без "+
						"функции состояния вычисляется с подразумеваемым `success()`, поэтому "+
						"разделённая форма куплена, а свойство — нет: сохранение по-прежнему "+
						"исполняется только на зелёном. Нужна `always()`, `!cancelled()` или `failure()`")
				}
			}
		}
		if restores > 0 && saves == 0 {
			findings = append(findings, path+": job "+name+" — кэш ВОССТАНАВЛИВАЕТСЯ "+
				"(`actions/cache/restore`), но нигде в этом задании не СОХРАНЯЕТСЯ. "+
				"Наполнять его нечем: промах кэша останется промахом на каждом прогоне, "+
				"и мера будет выглядеть принятой, работая никогда")
		}
	}
	sort.Strings(findings)
	return findings, census
}

// TestCacheFillsOnTheRunThatPaidForIt — по дереву.
func TestCacheFillsOnTheRunThatPaidForIt(t *testing.T) {
	root := repoRoot(t)
	files := listWorkflows(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение: обход, переставший находить workflow,
	// выходит зелёным на пустом множестве.
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто", workflowsDir)
	}

	var total cacheCensus
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		findings, census := checkCacheFillOnRed(f, string(raw))
		total.add(census)
		for _, msg := range findings {
			t.Error(msg)
		}
	}
	t.Logf("осмотрено workflow: %d; шагов работы с кэшем: комбинированных %d, "+
		"восстановлений %d, сохранений %d",
		len(files), total.Combined, total.Restore, total.Save)
}

// TestCacheFillDetectorSeesBothForms — инъекция в обе стороны: заведомый
// экземпляр обязан быть пойман, законный близнец той же формы — пропущен.
func TestCacheFillDetectorSeesBothForms(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantHit bool
	}{
		{
			name: "комбинированное actions/cache — находка",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: true,
		},
		{
			name: "комбинированное actions/cache с save-always — всё равно находка",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n          save-always: true\n",
			wantHit: true,
		},
		{
			name: "разделённая форма с always() — законный близнец, молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        id: c\n        with:\n          path: ~/.cache/x\n          key: k\n" +
				"      - uses: actions/cache/save@v6\n" +
				"        if: ${{ always() && steps.c.outputs.cache-hit != 'true' }}\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: false,
		},
		{
			name: "разделённая форма с !cancelled() — тоже законна, молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n" +
				"      - uses: actions/cache/save@v6\n        if: ${{ !cancelled() }}\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: false,
		},
		{
			name: "разделённая форма, но сохранение без функции состояния — находка",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        id: c\n        with:\n          path: ~/.cache/x\n          key: k\n" +
				"      - uses: actions/cache/save@v6\n" +
				"        if: steps.c.outputs.cache-hit != 'true'\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: true,
		},
		{
			name: "разделённая форма, но сохранение вовсе без условия — находка",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n" +
				"      - uses: actions/cache/save@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: true,
		},
		{
			name: "восстановление без сохранения — находка",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: true,
		},
		{
			name: "кэш внутри setup-действия — не наш предмет, молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/setup-node@v7\n" +
				"        with:\n          cache: npm\n",
			wantHit: false,
		},
		{
			name: "имя действия только в комментарии — молчит",
			yaml: "jobs:\n  b:\n    steps:\n      # actions/cache@v6 здесь только упомянуто\n" +
				"      - uses: actions/checkout@v7\n",
			wantHit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, _ := checkCacheFillOnRed("синтетика.yml", tc.yaml)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v: %v", tc.wantHit, got, findings)
			}
		})
	}
}
