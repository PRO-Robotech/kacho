// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// standlogsderived_test.go — гейт против ВЫПИСАННОГО перечня служб в сборе
// журналов стенда.
//
// # Предмет (#1741)
//
// Сбор журналов перечислял носители ИМЕНАМИ, в теле шага, и перечислял ДВАЖДЫ
// (сбор в артефакт и хвост в журнал работы). Перечень называл восемь наших служб
// и не называл службу личности, её курьера и провайдера токенов, поднимаемых тем
// же чартом. Следствие: когда ломается полоса личности, ЕДИНСТВЕННЫЙ журнал, где
// названа причина, выбрасывается вместе с кластером.
//
// Отсутствующий журнал не даёт ни красного, ни зелёного — он делает разбор
// дороже прогона. Цена измерена: половина захода ушла на опровержение трёх
// неверных версий причины, каждая из которых объяснялась бы иначе, будь журнал
// под рукой.
//
// # Что требуется, и почему именно это
//
// Перечень обязан ВЫВОДИТЬСЯ из состава поднятого стенда. Тогда «служба поднята
// и не попала в сбор» невозможна by construction: новая служба в чарте попадает
// в сбор без правки конвейера, снятая — исчезает, не оставляя мёртвого имени.
// Выписанный перечень стареет молча и не имеет владельца: правит его тот, кто
// наткнулся, и правит одно место из двух.
//
// # Признак СТРУКТУРНЫЙ, а не по именам — и это решение, а не удобство
//
// Гейт не держит списка наших служб: такой список был бы ровно тем выписанным
// перечнем, который он запрещает, — вторым местом об одном предмете, стареющим
// с той же скоростью. Он судит ФОРМУ: обход по литеральному перечню слов, в теле
// которого читаются журналы. Обход по выведенному перечню (`$(kubectl … -o
// name)`, массив собранных файлов) под признак не подпадает by construction —
// в нём есть подстановка, а значит перечень не выписан.
//
// # Судится РАЗОБРАННЫЙ конвейер и КОД, а не текст
//
// Имя шага и объяснение к нему встречаются в комментариях — и в этом самом файле
// тоже. Гейт по подстроке краснел бы на собственном объяснении, поэтому
// читаются узлы `jobs.<id>.steps[].run` разобранного YAML, а из них снимаются
// комментарии оболочки (`testing.md` §«Гейт на класс», п. 4).
//
// # Перепись обязательна
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: печатаются
// конвейеры, шаги, шаги-обходы и шаги, ЧИТАЮЩИЕ журналы стенда. Ноль последних
// означает ослепший распознаватель — тогда гейт падает, а не выходит успехом.
package repohygiene

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

// standLogsCollector — скрипт, выводящий перечень из поднятого стенда.
const standLogsCollector = "collect-stand-logs.sh"

var (
	// Обход по перечню: `for d in a b c; do`. Захватывается сам перечень.
	slForLoop = regexp.MustCompile(`(?m)^\s*for\s+\w+\s+in\s+([^\n;]+?)\s*(?:;|\n)`)
	// Признак того, что шаг читает журналы стенда: чтение журналов инструментом
	// либо каталог, в который их складывают. Обе формы нужны: первая — прямое
	// чтение, вторая — потребитель собранного.
	slReadsLogs = regexp.MustCompile(`kubectl[^\n]*\blogs\b|stand-logs`)
	// Подстановка любого вида: перечень выведен, а не выписан.
	slExpansion = regexp.MustCompile(`\$\(|\$\{|\$\w`)
)

type slStep struct {
	file, job, name string
	line            int
	run             string
}

type slCensus struct {
	workflows, steps, loops, logSteps int
}

// slReadWorkflowSteps — шаги конвейеров с их кодом, из РАЗОБРАННОГО YAML.
func slReadWorkflowSteps(dir string) ([]slStep, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("каталог конвейеров %s: %w", dir, err)
	}
	var out []slStep
	files := 0
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || (!strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml")) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, n)) // #nosec G304 -- путь из каталога конвейеров этого дерева
		if err != nil {
			return nil, 0, fmt.Errorf("чтение %s: %w", n, err)
		}
		var root yaml.Node
		if err := yaml.Unmarshal(body, &root); err != nil {
			return nil, 0, fmt.Errorf("%s: конвейер не разбирается: %w", n, err)
		}
		files++
		jobs := yamlMapValue(&root, "jobs")
		if jobs == nil || jobs.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			jobID, jobNode := jobs.Content[i].Value, jobs.Content[i+1]
			steps := yamlMapValue(jobNode, "steps")
			if steps == nil || steps.Kind != yaml.SequenceNode {
				continue
			}
			for _, st := range steps.Content {
				run := yamlMapValue(st, "run")
				if run == nil {
					continue
				}
				title := ""
				if nm := yamlMapValue(st, "name"); nm != nil {
					title = nm.Value
				}
				out = append(out, slStep{
					file: n, job: jobID, name: title, line: run.Line,
					// Комментарии оболочки снимаются: объяснение рядом с кодом
					// не является кодом, и гейт по тексту краснел бы на нём.
					run: clusterDiagStripComments(run.Value),
				})
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].file != out[b].file {
			return out[a].file < out[b].file
		}
		return out[a].line < out[b].line
	})
	return out, files, nil
}

// slScan — шаги, читающие журналы стенда обходом по ВЫПИСАННОМУ перечню.
func slScan(steps []slStep, workflows int) ([]string, slCensus) {
	cen := slCensus{workflows: workflows}
	var finds []string
	for _, s := range steps {
		cen.steps++
		reads := slReadsLogs.MatchString(s.run)
		if reads {
			cen.logSteps++
		}
		for _, m := range slForLoop.FindAllStringSubmatch(s.run, -1) {
			cen.loops++
			list := strings.TrimSpace(m[1])
			if !reads || slExpansion.MatchString(list) {
				continue
			}
			if len(strings.Fields(list)) < 2 {
				continue
			}
			finds = append(finds, fmt.Sprintf(
				"%s:%d  работа %q, шаг %q — журналы читаются обходом по ВЫПИСАННОМУ "+
					"перечню: `%s`", s.file, s.line, s.job, s.name, slShorten(list)))
		}
	}
	return finds, cen
}

func slShorten(s string) string {
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}

func TestStandLogCollectionDerivesItsListFromTheStand(t *testing.T) {
	root := repoRoot(t)
	steps, workflows, err := slReadWorkflowSteps(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		t.Fatalf("обход конвейеров: %v", err)
	}
	finds, cen := slScan(steps, workflows)

	if cen.workflows == 0 || cen.steps == 0 {
		t.Fatalf("перепись пуста: конвейеров %d, шагов %d — «ноль находок» здесь означало бы "+
			"«ноль прочитанного»", cen.workflows, cen.steps)
	}
	// Предпосылка: распознаватель видит хотя бы один шаг, читающий журналы
	// стенда. Ноль означает, что признак перестал совпадать с деревом, а не что
	// дерево чисто.
	if cen.logSteps == 0 {
		t.Fatalf("в %d шагах %d конвейеров не найдено НИ ОДНОГО, читающего журналы стенда — "+
			"распознаватель ослеп; чинить надо гейт, а не выходить успехом",
			cen.steps, cen.workflows)
	}
	t.Logf("осмотрено: конвейеров %d, шагов с кодом %d, из них читающих журналы стенда %d; "+
		"обходов по перечню %d", cen.workflows, cen.steps, cen.logSteps, cen.loops)

	if len(finds) > 0 {
		t.Errorf("шагов, собирающих журналы по выписанному перечню: %d\n  %s\n\n"+
			"Выписанный перечень стареет молча: он называл восемь наших служб и не называл "+
			"службу личности, её курьера и провайдера токенов, поднимаемых тем же чартом, — "+
			"поэтому при поломке полосы личности единственный журнал с причиной выбрасывался "+
			"вместе с кластером (#1741).\n"+
			"Чинится выведением перечня из ПОДНЯТОГО стенда: "+
			"`.github/scripts/%s`, который спрашивает состав у кластера и печатает числа "+
			"«поднято · собрано» рядом.", len(finds), strings.Join(finds, "\n  "), standLogsCollector)
	}
}

// Провязка: гейт выше требует выведенного перечня, а выводит его скрипт. Скрипт,
// которого никто не зовёт, — та же форма без содержания, поэтому проверяется
// обе стороны: скрипт существует И его зовут из конвейера.
func TestStandLogCollectorExistsAndIsWiredIn(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, ".github", "scripts", standLogsCollector)
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("сборщика журналов нет по координате %s: %v — гейт требует того, "+
			"чего в дереве не существует", script, err)
	}
	body, err := os.ReadFile(script) // #nosec G304 -- координата собрана из корня этого дерева
	if err != nil {
		t.Fatal(err)
	}
	// Скрипт обязан СПРАШИВАТЬ состав, а не нести свой перечень: иначе выписанный
	// перечень просто переехал бы на строчку ниже.
	if !regexp.MustCompile(`kubectl[^\n]*\bget\b[^\n]*-o name`).Match(body) {
		t.Errorf("%s не спрашивает состав стенда (`kubectl … get … -o name`) — "+
			"перечень остался выписанным, только переехал в скрипт", standLogsCollector)
	}

	steps, workflows, err := slReadWorkflowSteps(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		t.Fatalf("обход конвейеров: %v", err)
	}
	callers := 0
	for _, s := range steps {
		if strings.Contains(s.run, standLogsCollector) {
			callers++
		}
	}
	if workflows == 0 {
		t.Fatal("конвейеров не прочитано ни одного — провязку не на чем проверять")
	}
	if callers == 0 {
		t.Errorf("сборщик %s существует, но его не зовёт НИ ОДИН шаг %d конвейеров — "+
			"провязка в пустоту: журналы не собираются вовсе", standLogsCollector, workflows)
	}
	t.Logf("осмотрено: конвейеров %d, шагов с кодом %d; зовущих сборщика %d",
		workflows, len(steps), callers)
}
