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
// # Наполнитель бывает В ДРУГОМ ЗАДАНИИ, и это законно
//
// Раскладка «прогреть один раз, разойтись по полосам» (`unit-plan` наполняет
// кэш сборки под -race, тринадцать шардов его читают) наполнителя в СВОЁМ
// задании не имеет и иметь не должна: сохранение из тринадцати полос под одним
// ключом — тринадцать записей ради одной, а под разными — вытеснение чужих
// записей из предела кэша репозитория.
//
// Поэтому восстановление без сохранения законно РОВНО ТОГДА, когда тот же
// КЛЮЧ сохраняет задание того же процесса, от которого это зависит по `needs`
// (в том числе через посредника). Оба условия несущие: сохранение под другим
// ключом наполняет другую запись, а сохранение в задании, которого мы не ждём,
// может не успеть — тогда промах остаётся промахом, то есть ровно та находка,
// ради которой ось заведена. Свойство против обхода запрета сохраняется:
// удалили сохранение — снова красное.
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
	Jobs map[string]cacheJob `yaml:"jobs"`
}

type cacheJob struct {
	Needs yaml.Node   `yaml:"needs"`
	Steps []cacheStep `yaml:"steps"`
}

type cacheStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
	If   string `yaml:"if"`
	With struct {
		Key string `yaml:"key"`
	} `yaml:"with"`
}

// needs — имена заданий, от которых зависит это, в обеих законных формах
// (скаляр и список). Форма у площадки обе, и читать одну значило бы объявлять
// зависимость отсутствующей там, где она записана короче.
func (j cacheJob) needs() []string {
	switch j.Needs.Kind {
	case yaml.ScalarNode:
		var s string
		if j.Needs.Decode(&s) == nil && s != "" {
			return []string{s}
		}
	case yaml.SequenceNode:
		var s []string
		if j.Needs.Decode(&s) == nil {
			return s
		}
	}
	return nil
}

// cacheSaversReachable — задания, от которых `from` зависит по `needs`, включая
// посредников. Обход по посещённым: цикл в объявлении площадка не примет, но
// гейт не имеет права зависеть от этого и зависать.
func cacheSaversReachable(jobs map[string]cacheJob, from string) map[string]bool {
	seen := map[string]bool{}
	queue := jobs[from].needs()
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == "" || seen[next] {
			continue
		}
		seen[next] = true
		queue = append(queue, jobs[next].needs()...)
	}
	return seen
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

	// Кто какой ключ сохраняет — собирается ДО разбора находок: наполнитель
	// восстановления может стоять в задании, объявленном ниже по файлу.
	savedBy := map[string]map[string]bool{} // ключ -> задания, сохраняющие его
	for name, job := range doc.Jobs {
		for _, st := range job.Steps {
			if actionOf(st.Uses) != "actions/cache/save" {
				continue
			}
			k := strings.TrimSpace(st.With.Key)
			if savedBy[k] == nil {
				savedBy[k] = map[string]bool{}
			}
			savedBy[k][name] = true
		}
	}

	var findings []string
	for name, job := range doc.Jobs {
		restores, saves := 0, 0
		var restoreKeys []string
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
				restoreKeys = append(restoreKeys, strings.TrimSpace(st.With.Key))
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
			upstream := cacheSaversReachable(doc.Jobs, name)
			for _, k := range restoreKeys {
				filler := ""
				for j := range savedBy[k] {
					if upstream[j] {
						filler = j
						break
					}
				}
				if filler != "" {
					continue
				}
				elsewhere := len(savedBy[k]) > 0
				why := "нигде в процессе не СОХРАНЯЕТСЯ"
				if elsewhere {
					why = "сохраняется только заданием, которого это НЕ ЖДЁТ по `needs` " +
						"(значит наполнение может не успеть к чтению)"
				}
				findings = append(findings, path+": job "+name+" — кэш ВОССТАНАВЛИВАЕТСЯ "+
					"(`actions/cache/restore`) по ключу `"+k+"`, но "+why+". "+
					"Наполнять его нечем: промах кэша останется промахом на каждом прогоне, "+
					"и мера будет выглядеть принятой, работая никогда. Законны два исхода: "+
					"сохранение В ЭТОМ задании с `if`, переживающим красный шаг, либо "+
					"сохранение ТОГО ЖЕ ключа в задании, от которого это зависит по `needs`")
			}
		}
	}
	sort.Strings(findings)
	return findings, census
}

// TestCacheFillsOnTheRunThatPaidForIt — по дереву.
func TestCacheFillsOnTheRunThatPaidForIt(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
			// ЗАКОННЫЙ БЛИЗНЕЦ раскладки «прогреть один раз, разойтись по полосам».
			name: "восстановление без сохранения, но ТОТ ЖЕ ключ сохраняет задание из needs — молчит",
			yaml: "jobs:\n  plan:\n    steps:\n      - uses: actions/cache/save@v6\n" +
				"        if: ${{ !cancelled() }}\n        with:\n          path: ~/.cache/x\n          key: k\n" +
				"  shard:\n    needs: plan\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: false,
		},
		{
			// То же через ПОСРЕДНИКА: зависимость транзитивна, и читать только
			// прямые `needs` значило бы объявлять наполнитель отсутствующим.
			name: "наполнитель через посредника в needs — молчит",
			yaml: "jobs:\n  plan:\n    steps:\n      - uses: actions/cache/save@v6\n" +
				"        if: ${{ always() }}\n        with:\n          path: ~/.cache/x\n          key: k\n" +
				"  mid:\n    needs: [plan]\n    steps:\n      - run: echo\n" +
				"  shard:\n    needs: [mid]\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: false,
		},
		{
			// ИНЪЕКЦИЯ, отличающаяся от близнеца РОВНО ОДНИМ фактом: снят `needs`.
			// Наполнение может не успеть к чтению — промах остаётся промахом.
			name: "сохранение есть, но в задании вне needs — находка",
			yaml: "jobs:\n  plan:\n    steps:\n      - uses: actions/cache/save@v6\n" +
				"        if: ${{ !cancelled() }}\n        with:\n          path: ~/.cache/x\n          key: k\n" +
				"  shard:\n    steps:\n      - uses: actions/cache/restore@v6\n" +
				"        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: true,
		},
		{
			// Вторая инъекция того же близнеца: `needs` на месте, а ключ другой —
			// наполняется другая запись, и читаемая остаётся пустой.
			name: "сохранение в needs, но ДРУГОГО ключа — находка",
			yaml: "jobs:\n  plan:\n    steps:\n      - uses: actions/cache/save@v6\n" +
				"        if: ${{ !cancelled() }}\n        with:\n          path: ~/.cache/x\n          key: other\n" +
				"  shard:\n    needs: plan\n    steps:\n      - uses: actions/cache/restore@v6\n" +
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
