// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// workflowconcurrencyref_test.go — у процесса, запускаемого запросом слияния,
// группа `concurrency` различает ВЕТКУ; у production-posture отмены нет.
//
// ПРЕДМЕТ И ПОЧЕМУ ЭТО ТИХО. Группа `concurrency` без выражения ветки — одна
// очередь на все запросы: идёт один прогон, ждёт один, а новый вытесняет
// ждущего (так провайдер обходится с группой). Вытесненный прогон вердикта не
// выносит, и красным это не выглядит — выглядит задержкой старта. Цена измерена
// на production-posture (kacho#3267): общая группа `production-posture` дала
// задержку старта p90 8,3 мин и максимум 30 мин на 215 прогонах за неделю, а
// ждущие прогоны чужих веток вытеснялись молча.
//
// ЧТО СУДИТСЯ. Класс — процесс с триггером `pull_request` либо
// `pull_request_target`, а не перечень файлов: перечень процессов выводится
// обходом каталога. У такого процесса каждая группа `concurrency` — и уровня
// файла, и уровня задания, в обеих законных формах записи (строкой и
// отображением с `group:`) — несёт в выражении `${{ }}` различитель ветки
// либо прогона: `github.ref` (и `github.ref_name`), `github.head_ref`,
// `github.event.pull_request.number`, `github.event.pull_request.head.ref`,
// `github.run_id`. Процесс без триггера запроса (публикация выпуска, ночной
// прогон) общую группу держать вправе: очередь там — намеренная
// сериализация, а не чужие ветки.
//
// ВТОРОЕ СВОЙСТВО — production-posture БЕЗ ОТМЕНЫ. Вердикт этого процесса —
// «стенд ПОДНЯЛСЯ», и отменённый прогон его не выносит, а прерванный не доходит
// до шага уборки. Поэтому `cancel-in-progress` у него — `false` литералом либо
// отсутствует (умолчание провайдера — `false`); `true` и вычисляемое выражение —
// находка. Предмет второго свойства назван путём, и гейт проверяет его
// предпосылку: файла нет либо группы в нём нет — красное, а не тишина; снят
// процесс — снимается и эта половина гейта, одним изменением.
//
// ПОЧЕМУ РАЗБОР, А НЕ ПОИСК ПО ОБРАЗЦУ. Слово `github.ref` в комментарии рядом с
// группой ничего не различает; разбор YAML отдаёт значение узла, и комментарий в
// обход не попадает by construction.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

// postureWorkflowPath — предмет второго свойства (уборка без отмены).
const postureWorkflowPath = ".github/workflows/production-posture.yml"

// concurrencyExpr — выражение провайдера внутри значения группы.
var concurrencyExpr = regexp.MustCompile(`\$\{\{(.*?)\}\}`)

// concurrencyDiscriminator — различитель ветки или прогона внутри выражения.
// Граница справа — не буква, не цифра, не `_` и не `.`: `github.ref` засчитан,
// `github.ref_name` засчитан отдельной ветвью, `github.refx` — нет.
var concurrencyDiscriminator = regexp.MustCompile(
	`github\.(ref|ref_name|head_ref|run_id|event\.pull_request\.number|event\.pull_request\.head\.ref)([^A-Za-z0-9_.]|$)`)

// workflowConcurrencyCensus — объём осмотренного: «ноль находок» отличимо от
// «ноль прочитанного».
type workflowConcurrencyCensus struct {
	files      int // объявлений разобрано
	prFiles    int // из них с триггером запроса слияния
	groups     int // групп concurrency у процессов запроса осмотрено
	postureHit int // предмет второго свойства найден и разобран (0 или 1)
}

// workflowTriggers — имена событий из `on:` во всех трёх формах записи.
func workflowTriggers(root *yaml.Node) map[string]bool {
	out := map[string]bool{}
	on := mappingValue(root, "on")
	if on == nil {
		return out
	}
	switch on.Kind {
	case yaml.ScalarNode:
		out[on.Value] = true
	case yaml.SequenceNode:
		for _, n := range on.Content {
			out[n.Value] = true
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			out[on.Content[i].Value] = true
		}
	}
	return out
}

// concurrencyGroup — значение группы и строка, в обеих формах записи.
func concurrencyGroup(c *yaml.Node) (string, int, bool) {
	switch c.Kind {
	case yaml.ScalarNode:
		return c.Value, c.Line, true
	case yaml.MappingNode:
		if g := mappingValue(c, "group"); g != nil && g.Kind == yaml.ScalarNode {
			return g.Value, g.Line, true
		}
	}
	return "", c.Line, false
}

// groupDiscriminatesBranch — несёт ли группа различитель внутри выражения.
func groupDiscriminatesBranch(group string) bool {
	for _, m := range concurrencyExpr.FindAllStringSubmatch(group, -1) {
		if concurrencyDiscriminator.MatchString(m[1]) {
			return true
		}
	}
	return false
}

// auditWorkflowConcurrency — находки одного объявления и его перепись.
func auditWorkflowConcurrency(path, raw string) ([]string, workflowConcurrencyCensus) {
	var census workflowConcurrencyCensus
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, census
	}
	census.files = 1
	root := documentBody(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, census
	}

	var findings []string
	triggers := workflowTriggers(root)
	onPR := triggers["pull_request"] || triggers["pull_request_target"]

	type site struct {
		where string
		node  *yaml.Node
	}
	var sites []site
	if c := mappingValue(root, "concurrency"); c != nil {
		sites = append(sites, site{"процесса", c})
	}
	if jobs := mappingValue(root, "jobs"); jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			job := jobs.Content[i+1]
			if job.Kind != yaml.MappingNode {
				continue
			}
			if c := mappingValue(job, "concurrency"); c != nil {
				sites = append(sites, site{"задания `" + jobs.Content[i].Value + "`", c})
			}
		}
	}

	if onPR {
		census.prFiles = 1
		for _, s := range sites {
			census.groups++
			group, line, ok := concurrencyGroup(s.node)
			if !ok {
				findings = append(findings, path+":"+strconv.Itoa(line)+": группа concurrency "+s.where+
					" не прочитана (нет скалярного `group:`) — свойство НЕ проверено")
				continue
			}
			if !groupDiscriminatesBranch(group) {
				findings = append(findings, path+":"+strconv.Itoa(line)+": группа concurrency "+s.where+
					" `"+group+"` у процесса, запускаемого запросом слияния, не различает ветку — "+
					"в выражении `${{ }}` нет ни github.ref, ни github.head_ref, ни номера запроса, "+
					"ни github.run_id. Это одна очередь на все запросы: идёт один прогон, ждёт один, "+
					"новый вытесняет ждущего чужой ветки, и вытесненный вердикта не выносит "+
					"(задержка старта p90 8,3 мин, максимум 30 мин на 215 прогонах, kacho#3267). "+
					"Добавь различитель, например `<имя>-${{ github.ref }}`")
			}
		}
	}

	if filepath.ToSlash(path) == postureWorkflowPath {
		var top *yaml.Node
		for _, s := range sites {
			if s.where == "процесса" {
				top = s.node
			}
		}
		if top == nil {
			findings = append(findings, path+": у production-posture нет группы concurrency уровня процесса — "+
				"предпосылка гейта не выполняется, свойство «без отмены» НЕ проверено")
		} else {
			census.postureHit = 1
			if top.Kind == yaml.MappingNode {
				if c := mappingValue(top, "cancel-in-progress"); c != nil &&
					!(c.Kind == yaml.ScalarNode && c.ShortTag() == "!!bool" && c.Value == "false") {
					findings = append(findings, path+":"+strconv.Itoa(c.Line)+": cancel-in-progress `"+c.Value+
						"` у production-posture — вердикт процесса «стенд ПОДНЯЛСЯ», отменённый прогон его "+
						"не выносит, а прерванный не доходит до шага уборки. Допустимо только `false` "+
						"литералом либо отсутствие ключа")
				}
			}
		}
	}
	sort.Strings(findings)
	return findings, census
}

// TestPullRequestWorkflowConcurrencyDiscriminatesTheBranch — прогон по дереву.
func TestPullRequestWorkflowConcurrencyDiscriminatesTheBranch(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := listWorkflows(t, root)
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного объявления процесса — обход ослеп, а не дерево чисто", workflowsDir)
	}

	var total workflowConcurrencyCensus
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		findings, census := auditWorkflowConcurrency(f, string(raw))
		total.files += census.files
		total.prFiles += census.prFiles
		total.groups += census.groups
		total.postureHit += census.postureHit
		for _, msg := range findings {
			t.Error(msg)
		}
	}

	if total.prFiles == 0 || total.groups == 0 {
		t.Fatalf("объявлений разобрано %d, процессов запроса %d, групп осмотрено %d — "+
			"зелёное здесь означало бы «ноль прочитанного», а не «ноль находок»",
			total.files, total.prFiles, total.groups)
	}
	if total.postureHit != 1 {
		t.Fatalf("предмет второго свойства %s не найден обходом либо без группы — "+
			"процесс снят: сними вместе с ним и эту половину гейта; иначе предпосылка не выполняется",
			postureWorkflowPath)
	}
	t.Logf("объявлений разобрано: %d, процессов запроса слияния: %d, групп concurrency осмотрено: %d, "+
		"production-posture разобран: %d", total.files, total.prFiles, total.groups, total.postureHit)
}
