// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_chain_is_raised_by_the_conveyor_test.go — КАЖДАЯ цепочка таблицы стендов
// либо ПОДНИМАЕТСЯ конвейером, судящим запрос, либо называет, почему нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (kacho#2931, предикат kacho#1276 «values.prod реально поднимается»)
//
// Норма требует от боевого профиля подъёма, а не рендера: страж старта
// отказывает в РАНТАЙМЕ, шаблон при этом рендерится идеально. Цепочку, корнем
// которой стоит боевой слой и которая поднимается на kind, — `own` — не поднимала
// НИ ОДНА работа конвейера (разбор процессов @87329e2935c, 2026-09-30): её подъём
// держался одним рендером. Задание `production-posture` поднимало `dev-prod`, и
// «боевая посадка поднимается» читалось шире, чем было осмотрено.
//
// Свойство держится СЦЕПКОЙ трёх мест, и разрыв любого снимает его молча:
// работа конвейера зовёт владельца подъёма с целью make, рецепт цели накладывает
// цепочку и судит поднятое, таблица стендов называет цепочку. Поэтому здесь
// ни одно звено не выписано: цели читаются из работ, цепочки — из рецептов,
// состав — из таблицы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ «ПОДНИМАЕТСЯ»
//
//   - работа процесса, который идёт на запросе слияния (`on: pull_request`), —
//     тогда её проверка входит в набор, которым сводный вердикт судит запрос;
//   - её шаг зовёт владельца подъёма (`.github/scripts/stand-up.sh … -- make
//     <цель>`); цель с измерением литеральной матрицы раскрывается по ногам;
//   - цепочка — ПОСЛЕДНЯЯ, которую накладывает рецепт цели (с его
//     предпосылками): её значения переживают подъём;
//   - после неё рецепт судит поднятое: `assert-rollout-ready` (каждый объект
//     дошёл до готовности) И `assert-production-posture` (живой процесс и сама
//     база). Подъём без суда — это «helm прошёл», а не «поднялось».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОСЛАБЛЕНИЕ — ЗАКРЫТЫЙ СЛОВАРЬ С ДОВОДОМ, И ОНО ИСТЕКАЕТ САМО
//
// Цепочка, которую конвейер поднять не может по построению (слой вне git,
// управляемый кластер), названа ниже с причиной. Запись о цепочке, которую
// конвейер УЖЕ поднимает, — находка: исключать ей нечего. Запись о цепочке,
// которой нет в таблице, — тоже.
//
// ЧЕГО ПРОВЕРКА НЕ ДЕЛАЕТ: не доказывает, что стенд ПОДНИМАЕТСЯ. Это исход
// прогона — третья категория у владельца подъёма и красное у стража старта; его
// выносит сама работа конвейера. Здесь судится только то, что спросить её об
// этом ЕСТЬ КОМУ.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// conveyorWorkflowsGlob — процессы конвейера. Каталог один на дерево.
const conveyorWorkflowsGlob = "../.github/workflows/*.y*ml"

// chainsTheConveyorDoesNotRaise — цепочки, которых конвейер не поднимает, и ПОЧЕМУ.
// Довод обязан называть свойство, делающее подъём на kind конвейера невозможным
// либо беспредметным, — не «потом».
var chainsTheConveyorDoesNotRaise = map[string]string{
	"dev": "промежуточная фаза подъёма: dev-up накладывает цепочку разработки первой и тем же " +
		"подъёмом замещает её цепочкой dev-prod; посадку она не объявляет по устройству " +
		"(stand_posture_terminal_stack_test.go)",
	"prod": "боевой слой намеренно неполон (шапка deploy/stacks.txt): адрес хранилища, перенос схемы " +
		"и снятие зеркала образов приходят слоем площадки; поднимаемая форма этого слоя на kind — " +
		"цепочка own, и её поднимает конвейер",
	"fe3455": "цепочка требует слоя учётных данных площадки, который лежит вне git и вне машины " +
		"конвейера (шапка deploy/stacks.txt); раскатывается helm/umbrella/cutover-fe3455.sh на своём кластере",
	"prorobotech": "третий слой объявляет только пины образов публичного реестра поверх dev и dev-prod: " +
		"подъём на конвейере судил бы опубликованные образы, а не сборку запроса, а посадку цепочки " +
		"несут dev и dev-prod, которые конвейер поднимает",
	"a8f60d": "слой управляемого кластера: cert-manager оператора, IngressClass и StorageClass нет, " +
		"внешний доступ — LoadBalancer площадки (шапка values.a8f60d.yaml); на kind конвейера эти " +
		"отличия не воспроизводятся, раскатывается stack-up на своём кластере",
}

// standLeg — одна нога подъёма стенда в работе конвейера.
type standLeg struct {
	Workflow, Job, Name string
	RunsOn              string // метка ранера работы (`runs-on`) — по ней судится ёмкость узла
	OnPullRequest       bool
	Target              string   // цель make, названная шагом подъёма
	Phases              []string // цепочки, которые накладывает рецепт цели, ПО ПОРЯДКУ
	Judged              bool     // после последней цепочки рецепт судит готовность И посадку
}

func (l standLeg) terminal() string {
	if len(l.Phases) == 0 {
		return ""
	}
	return l.Phases[len(l.Phases)-1]
}

var (
	// ownerCall — вызов владельца подъёма в позиции команды. Та же форма, что у
	// гейта связности (deploy/scripts/assert-stand-precondition-wiring.py,
	// calls_owner): вызов в прозе или в подсказке оператору подъёмом не является.
	ownerCall = regexp.MustCompile(`(?m)(^|[;&|(]|\$\()[ \t]*\S*stand-up\.sh\b`)
	// ownerMakeTarget — цель make, которую владелец исполняет после `--`. Слово
	// цели может нести выражение платформы (`${{ matrix.stack }}-up`) — пробелы
	// внутри выражения слова не рвут.
	ownerMakeTarget = regexp.MustCompile(`stand-up\.sh\b.*?\s--\s+make\s+((?:\$\{\{[^}]*\}\}|\S)+)`)
	// matrixRef — ссылка на измерение матрицы в выражении платформы.
	matrixRef = regexp.MustCompile(`\$\{\{\s*matrix\.([A-Za-z_][A-Za-z0-9_-]*)\s*\}\}`)
	// makeAssert — вызов цели суда из рецепта (`$(MAKE) … <цель>`).
	makeAssert = func(target string) *regexp.Regexp {
		return regexp.MustCompile(`\$\(MAKE\)[^;]*\b` + regexp.QuoteMeta(target) + `\b`)
	}
	rolloutAssert = makeAssert("assert-rollout-ready")
	postureAssert = makeAssert("assert-production-posture")
)

// makefileTargets — цель → предпосылки и строки рецепта. Рецепт читается так же,
// как его читает соседняя проверка цепочек (makeRecipeStacks): строки с
// табуляцией до следующей строки без неё.
type recipeTarget struct {
	prereqs []string
	recipe  []string
}

func parseRecipeTargets(text string) map[string]recipeTarget {
	out := map[string]recipeTarget{}
	cur := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "\t") {
			if cur != "" {
				t := out[cur]
				t.recipe = append(t.recipe, line)
				out[cur] = t
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := makeTargetLine.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, ".") {
			cur = m[1]
			rest := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			t := out[cur]
			t.prereqs = append(t.prereqs, strings.Fields(rest)...)
			out[cur] = t
			continue
		}
		cur = ""
	}
	return out
}

// flattenRecipe — строки рецепта цели вместе с рецептами её предпосылок, в
// порядке исполнения (make проходит предпосылки до рецепта).
func flattenRecipe(targets map[string]recipeTarget, name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	t, ok := targets[name]
	if !ok {
		return nil
	}
	var out []string
	for _, p := range t.prereqs {
		out = append(out, flattenRecipe(targets, p, seen)...)
	}
	return append(out, t.recipe...)
}

// resolveLeg — цепочки и суд рецепта цели.
func resolveLeg(targets map[string]recipeTarget, target string) (phases []string, judged bool, err error) {
	if _, ok := targets[target]; !ok {
		return nil, false, fmt.Errorf("цели %q в deploy/Makefile нет", target)
	}
	lines := flattenRecipe(targets, target, map[string]bool{})
	last := -1
	for i, line := range lines {
		for _, m := range stackArgsCall.FindAllStringSubmatch(line, -1) {
			phases = append(phases, m[1])
			last = i
		}
	}
	if last < 0 {
		return nil, false, nil
	}
	after := strings.Join(lines[last+1:], "\n")
	return phases, rolloutAssert.MatchString(after) && postureAssert.MatchString(after), nil
}

// workflowLegs — ноги подъёма в одном процессе.
func workflowLegs(file string, doc map[string]any, targets map[string]recipeTarget) ([]standLeg, []string) {
	var legs []standLeg
	var problems []string
	onPR := false
	switch on := doc["on"].(type) {
	case map[string]any:
		_, onPR = on["pull_request"]
	case []any:
		for _, e := range on {
			onPR = onPR || e == "pull_request"
		}
	case string:
		onPR = on == "pull_request"
	}
	jobs, _ := doc["jobs"].(map[string]any)
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		job, _ := jobs[id].(map[string]any)
		steps, _ := job["steps"].([]any)
		for _, s := range steps {
			step, _ := s.(map[string]any)
			run, _ := step["run"].(string)
			if !ownerCall.MatchString(run) {
				continue
			}
			flat := regexp.MustCompile(`\\\n\s*`).ReplaceAllString(run, " ")
			m := ownerMakeTarget.FindStringSubmatch(flat)
			if m == nil {
				problems = append(problems, fmt.Sprintf("%s / %s: шаг зовёт владельца подъёма, а цель make "+
					"после `--` не распознана — вердикт о цепочке вынести не о чем", file, id))
				continue
			}
			name, _ := job["name"].(string)
			if name == "" {
				name = id
			}
			for _, leg := range expandMatrix(job, strings.Trim(m[1], `"'`), name) {
				if leg.err != "" {
					problems = append(problems, fmt.Sprintf("%s / %s: %s", file, id, leg.err))
					continue
				}
				phases, judged, err := resolveLeg(targets, leg.target)
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s / %s: %v", file, id, err))
					continue
				}
				legs = append(legs, standLeg{Workflow: file, Job: id, Name: leg.name, RunsOn: runsOnLabel(job["runs-on"]), OnPullRequest: onPR,
					Target: leg.target, Phases: phases, Judged: judged})
			}
		}
	}
	return legs, problems
}

type matrixLeg struct{ target, name, err string }

// expandMatrix — цель и имя по ногам ЛИТЕРАЛЬНОЙ матрицы. Цель без ссылки на
// матрицу — одна нога. Цель со ссылкой на матрицу из выражения либо с
// include/exclude не вычисляется — это отказ, а не догадка.
func expandMatrix(job map[string]any, target, name string) []matrixLeg {
	if !matrixRef.MatchString(target) {
		return []matrixLeg{{target: target, name: name}}
	}
	strategy, _ := job["strategy"].(map[string]any)
	matrix, ok := strategy["matrix"].(map[string]any)
	if !ok {
		return []matrixLeg{{err: "цель " + target + " ссылается на матрицу, а матрица не литеральна — ноги не вычислены"}}
	}
	if _, inc := matrix["include"]; inc {
		return []matrixLeg{{err: "матрица с include — ноги цели " + target + " не вычислены"}}
	}
	if _, exc := matrix["exclude"]; exc {
		return []matrixLeg{{err: "матрица с exclude — ноги цели " + target + " не вычислены"}}
	}
	dims := make([]string, 0, len(matrix))
	for d := range matrix {
		dims = append(dims, d)
	}
	sort.Strings(dims)
	combos := []map[string]string{{}}
	for _, d := range dims {
		vals, ok := matrix[d].([]any)
		if !ok || len(vals) == 0 {
			return []matrixLeg{{err: "измерение матрицы " + d + " не литеральный список — ноги не вычислены"}}
		}
		var next []map[string]string
		for _, c := range combos {
			for _, v := range vals {
				switch v.(type) {
				case map[string]any, []any:
					return []matrixLeg{{err: "значение измерения " + d + " не скаляр — ноги не вычислены"}}
				}
				n := map[string]string{d: fmt.Sprint(v)}
				for k, vv := range c {
					n[k] = vv
				}
				next = append(next, n)
			}
		}
		combos = next
	}
	subst := func(s string, c map[string]string) (string, bool) {
		ok := true
		out := matrixRef.ReplaceAllStringFunc(s, func(ref string) string {
			d := matrixRef.FindStringSubmatch(ref)[1]
			v, has := c[d]
			ok = ok && has
			return v
		})
		return out, ok
	}
	out := make([]matrixLeg, 0, len(combos))
	for _, c := range combos {
		tgt, ok1 := subst(target, c)
		nm, ok2 := subst(name, c)
		if !ok1 || !ok2 {
			return []matrixLeg{{err: "цель или имя ссылаются на измерение, которого у матрицы нет"}}
		}
		out = append(out, matrixLeg{target: tgt, name: nm})
	}
	return out
}

// conveyorChainFindings — РЕШЕНИЕ гейта, вынесенное чистой функцией: пробе
// падучести подаётся настоящий вход, а не подделанное дерево.
func conveyorChainFindings(chains map[string][]string, legs []standLeg, exempt map[string]string) []string {
	raisedBy := map[string][]string{}
	for _, l := range legs {
		if l.OnPullRequest && l.Judged && l.terminal() != "" {
			raisedBy[l.terminal()] = append(raisedBy[l.terminal()], l.Workflow+" / «"+l.Name+"»")
		}
	}
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		_, isExempt := exempt[n]
		switch by := raisedBy[n]; {
		case len(by) > 0 && isExempt:
			out = append(out, fmt.Sprintf("цепочка %s поднимается конвейером (%s), а послабление о ней "+
				"осталось — исключать ему нечего, снимите запись", n, strings.Join(by, ", ")))
		case len(by) == 0 && !isExempt:
			out = append(out, fmt.Sprintf("цепочку %s (%s) не поднимает ни одна работа конвейера, судящая "+
				"запрос: её подъём держится одним рендером, а страж старта отказывает только в рантайме. "+
				"Поднимите её работой, которая после наложения судит готовность и посадку, либо назовите "+
				"довод в chainsTheConveyorDoesNotRaise", n, strings.Join(chains[n], ",")))
		}
	}
	for n := range exempt {
		if _, ok := chains[n]; !ok {
			out = append(out, fmt.Sprintf("послабление о цепочке %s, которой в таблице стендов нет — "+
				"запись пережила свой предмет", n))
		}
	}
	sort.Strings(out)
	return out
}

// conveyorLegs — ноги подъёма всего дерева.
func conveyorLegs(t *testing.T) []standLeg {
	t.Helper()
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("рецепты подъёма %s не читаются (%v) — предпосылка проверки исчезла", standMakefile, err)
	}
	targets := parseRecipeTargets(string(raw))
	files, err := filepath.Glob(conveyorWorkflowsGlob)
	if err != nil || len(files) == 0 {
		t.Fatalf("процессов конвейера по %s не найдено (%v) — судить не о чем, а не «всё поднимается»",
			conveyorWorkflowsGlob, err)
	}
	var legs []standLeg
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("процесс %s не читается: %v", f, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("процесс %s не разбирается: %v", f, err)
		}
		got, problems := workflowLegs(filepath.Base(f), doc, targets)
		for _, p := range problems {
			t.Errorf("%s", p)
		}
		legs = append(legs, got...)
	}
	if len(legs) == 0 {
		t.Fatalf("ни одного шага подъёма в %d процессах — распознаватель перестал узнавать работы, "+
			"и «цепочка не поднимается» было бы объявлено о непрочитанном", len(files))
	}
	return legs
}

// TestEveryStandChainIsRaisedByTheConveyorOrNamesWhyNot — сам гейт.
func TestEveryStandChainIsRaisedByTheConveyorOrNamesWhyNot(t *testing.T) {
	chains := deployStacks(t)
	legs := conveyorLegs(t)
	judged := 0
	for _, l := range legs {
		if l.Judged {
			judged++
		}
		t.Logf("нога: %s / %s «%s» → make %s; цепочки %v; на запросе %v; судит готовность и посадку %v",
			l.Workflow, l.Job, l.Name, l.Target, l.Phases, l.OnPullRequest, l.Judged)
	}
	for _, f := range conveyorChainFindings(chains, legs, chainsTheConveyorDoesNotRaise) {
		t.Error(f)
	}
	t.Logf("осмотрено: цепочек %d, ног подъёма %d, из них судящих поднятое %d, послаблений %d",
		len(chains), len(legs), judged, len(chainsTheConveyorDoesNotRaise))
}
