// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// workflowjobidentifier_test.go — идентификатор задания и шага в объявлении
// процесса обязан быть машиночитаемым: латиница, цифры, `_`, `-`, и первый знак —
// буква или `_`.
//
// ПРЕДМЕТ И ПОЧЕМУ ЭТО ТИХО. Ключ под `jobs:` — не подпись, а ИМЯ, на которое
// ссылаются `needs:`, `outputs:` и контекст `jobs.<id>`. Провайдер проверяет его
// форму ДО того, как что-либо исполнит. Ключ вне допустимого набора делает
// объявление неразбираемым целиком, и наблюдаемое выглядит так:
//
//	prod-исход: прогон помечен провалом, заданий в нём НОЛЬ,
//	            а поле `name` в ответе API приходит ПУТЁМ К ФАЙЛУ вместо имени
//	            процесса — верный признак того, что объявление не разобрано;
//	условие `if` не вычисляется вовсе — падение наступает раньше;
//	триггер тоже не читается: прогоны создаются на событиях, которых
//	            объявление не называет.
//
// Цена измерена на #1073: процесс сводного вердикта не запустился на своём
// предмете НИ РАЗУ за всю жизнь (прогонов на запрос слияния — 0), а на push дал
// сотню красных подряд. Красное, которое есть всегда, перестают читать — и
// следующее, настоящее, не отличат.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ПРАВКА ОДНОГО ФАЙЛА. Класс закрыт в одном файле и открыт в
// остальных: `TestIdentifiersAreASCII` судит исходники Go, консольный близнец —
// TypeScript, а объявления процессов не осматривал НИКТО. Следующий ключ,
// написанный кириллицей по образцу соседней подписи, вернул бы тот же исход.
//
// ПОЧЕМУ РАЗБОР, А НЕ ПОИСК ПО ОБРАЗЦУ. Кириллица в подписи (`name:`) и в
// комментарии ЗАКОННА и обязана остаться — запрет про идентификатор, а не про
// язык. Проверено опытом на паре процессов-однодневок, различавшихся ровно
// ключом: с латинским ключом объявление разобрано, задание исполнено, подписи и
// комментарии кириллические. Поиск по подстроке этого различить не может и
// краснел бы на собственном объяснении — в этом файле кириллицы больше, чем
// где-либо.
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

// workflowIdentifierForm — форма, которую провайдер принимает у имени задания и
// шага: первый знак — латинская буква или `_`, дальше латиница, цифры, `_`, `-`.
var workflowIdentifierForm = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// workflowIdentifierCensus — объём осмотренного. Отдельная величина, потому что
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
type workflowIdentifierCensus struct {
	files   int // объявлений процессов прочитано
	jobs    int // ключей под jobs: осмотрено
	stepIDs int // шагов с объявленным id осмотрено
}

// auditWorkflowIdentifiers — находки одного объявления плюс его перепись.
//
// Читает РАЗОБРАННЫЙ документ и судит только по узлам-КЛЮЧАМ: подпись `name:` и
// текст комментария под запрет не подпадают и в обход не попадают by construction.
func auditWorkflowIdentifiers(path, raw string) ([]string, workflowIdentifierCensus) {
	var census workflowIdentifierCensus

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, census
	}
	census.files = 1

	root := documentBody(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, census
	}

	jobs := mappingValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil, census // объявление без заданий — законно (например, только `on:`)
	}

	var findings []string
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		key, job := jobs.Content[i], jobs.Content[i+1]
		census.jobs++
		if !workflowIdentifierForm.MatchString(key.Value) {
			findings = append(findings, workflowIdentifierFinding(path, key.Line, "задания", key.Value,
				"на него ссылаются `needs:`, `outputs:` и контекст `jobs.<id>`"))
		}
		if job == nil || job.Kind != yaml.MappingNode {
			continue
		}
		steps := mappingValue(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			if step.Kind != yaml.MappingNode {
				continue
			}
			id := mappingValue(step, "id")
			if id == nil || id.Kind != yaml.ScalarNode {
				continue
			}
			census.stepIDs++
			if !workflowIdentifierForm.MatchString(id.Value) {
				findings = append(findings, workflowIdentifierFinding(path, id.Line, "шага", id.Value,
					"на него ссылается контекст `steps.<id>.outputs`"))
			}
		}
	}
	sort.Strings(findings)
	return findings, census
}

// workflowIdentifierFinding — текст находки. Называет предмет прямо: сообщение
// падения гейта есть описание защищаемого свойства, и выхолащивать его нельзя —
// непонятную проверку следующий читатель снимет.
func workflowIdentifierFinding(path string, line int, what, id, why string) string {
	return path + ":" + strconv.Itoa(line) + ": идентификатор " + what + " `" + id +
		"` вне машиночитаемой формы " + workflowIdentifierForm.String() + " — " + why + ". " +
		"Провайдер проверяет форму ДО исполнения: объявление с таким ключом не разбирается " +
		"ЦЕЛИКОМ, поэтому прогон получает ноль заданий, помечается провалом, а поле `name` " +
		"в ответе API приходит путём к файлу; условие `if` не вычисляется вовсе, и триггер " +
		"тоже не читается. Переименуй КЛЮЧ латиницей — подпись `name:` и комментарий рядом " +
		"под запрет не подпадают и остаются как есть (#1073)"
}

// documentBody — содержимое документа (yaml.Unmarshal в Node отдаёт узел-документ).
func documentBody(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// mappingValue — значение по ключу отображения, либо nil.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// TestWorkflowJobAndStepIdentifiersAreMachineReadable — прогон по дереву.
func TestWorkflowJobAndStepIdentifiersAreMachineReadable(t *testing.T) {
	root := repoRoot(t)
	files := listWorkflows(t, root)

	// Пустой обход — поломка гейта, а не чистота дерева: на пустом множестве
	// любая проверка зелена, и отличить это можно только отдельным утверждением.
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного объявления процесса — обход ослеп, а не дерево чисто", workflowsDir)
	}

	var total workflowIdentifierCensus
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		findings, census := auditWorkflowIdentifiers(f, string(raw))
		total.files += census.files
		total.jobs += census.jobs
		total.stepIDs += census.stepIDs
		for _, msg := range findings {
			t.Error(msg)
		}
	}

	if total.jobs == 0 {
		t.Fatalf("объявлений прочитано %d, а заданий осмотрено 0 — разбор не доходит до `jobs:`, "+
			"и зелёное здесь означало бы «ноль прочитанного», а не «ноль находок»", total.files)
	}
	t.Logf("объявлений процессов прочитано: %d, заданий осмотрено: %d, шагов с объявленным id: %d",
		total.files, total.jobs, total.stepIDs)
}
