// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// thirdcategorysignal_test.go — СВИДЕТЕЛЬСТВО ТРЕТЬЕЙ КАТЕГОРИИ ОБЯЗАНО ДОЕЗЖАТЬ.
//
// # Предмет
//
// Гейт по отчёту проб — ЕДИНСТВЕННЫЙ, кто читает тексты отказов, и потому
// единственный, кто отличает «продукт ответил не то» от «до продукта не дошли».
// Разметчик исхода видит только исходы шагов, а в этом случае условные шаги
// честно отработали: стенд был достижим, когда их спрашивали, и перестал быть
// достижим позже.
//
// Наблюдалось (#1041, прогон 32599157014): 66 проб упали за 500–800 мс с одним и
// тем же отказом разрешения имени; вердикт пришёл красным, и разбор ушёл в
// консоль — при том что ветка проб не касалась её ни строкой.
//
// # Почему это гейт, а не «и так видно»
//
// Свидетельство едет через ТРИ места, и каждое правится отдельно: код возврата
// гейта, запись выхода шага и аргумент разметчика. Убери любое — различение
// молча перестаёт работать, а обе полосы останутся зелёными на зелёном прогоне.
// Это два места об одном предмете в чистом виде: расходятся они там, где
// расхождение не видно.
//
// Гейт НЕ проверяет саму классификацию — её доказывают самопроверки обоих
// скриптов (`--self-test`). Здесь предмет один: провязка между ними.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const thirdCategoryWorkflow = ".github/workflows/console-e2e.yml"

// signalDoc — то немногое из workflow, что нужно этому гейту.
type signalDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			ID  string            `yaml:"id"`
			Run string            `yaml:"run"`
			Env map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

type thirdCategoryWiring struct {
	VerdictStepFound bool
	EmitsUnmetOutput bool // шаг вердикта пишет выход `unmet`
	NamesCodeThree   bool // ...по коду 3, а не по любому отказу
	CategoryFound    bool
	PassesFlag       bool // разметчик получает --verdict-unmet
	ReadsVerdictStep bool // ...из выхода ИМЕННО вердиктного шага
}

// adjudicateThirdCategorySignal — суждение, отделённое от чтения дерева.
func adjudicateThirdCategorySignal(w thirdCategoryWiring) []string {
	var out []string
	if !w.VerdictStepFound {
		out = append(out, "шага `report-gate` в "+thirdCategoryWorkflow+" нет: "+
			"свидетельству третьей категории неоткуда взяться")
		return out
	}
	if !w.CategoryFound {
		out = append(out, "шага `category` в "+thirdCategoryWorkflow+" нет: "+
			"свидетельству некуда приехать")
		return out
	}
	if !w.EmitsUnmetOutput {
		out = append(out, "шаг `report-gate` не пишет выход `unmet`.\n"+
			"    Тогда «до продукта не дошла ни одна проба» остаётся в журнале, куда "+
			"читатель сводки не заходит, и приходит в неё красным — то есть разбор "+
			"снова уйдёт в продукт, которого запрос не касался.")
	}
	if !w.NamesCodeThree {
		out = append(out, "шаг `report-gate` не различает код 3.\n"+
			"    Свидетельство обязано выдаваться по СВОЕМУ коду, а не по любому "+
			"отказу: иначе третьей категорией станет всякое падение проб, и "+
			"послабление превратится в маску для красного.")
	}
	if !w.PassesFlag {
		out = append(out, "шаг `category` не передаёт `--verdict-unmet`: разметчик судит "+
			"по исходам шагов, а они в этом случае чисты")
	}
	if !w.ReadsVerdictStep {
		out = append(out, "значение `--verdict-unmet` берётся не из выхода `report-gate`.\n"+
			"    Свидетельство обязано приходить от того, кто читал ТЕКСТЫ отказов; "+
			"любой другой источник называет категорию, не имея на это оснований.")
	}
	return out
}

func TestThirdCategorySignalReachesTheSummary(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, thirdCategoryWorkflow)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s не читается: %v — гейт не может судить о дереве, которого не видит",
			thirdCategoryWorkflow, err)
	}
	var doc signalDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s не разобран: %v — файл НЕ проверен", thirdCategoryWorkflow, err)
	}

	w, steps := readThirdCategoryWiring(doc)
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: %s — %d байт, шагов %d; вердиктный найден %v, "+
		"пишет выход %v, различает код 3 %v; разметчик найден %v, передаёт флаг %v, "+
		"берёт его у вердиктного %v",
		thirdCategoryWorkflow, len(raw), steps,
		w.VerdictStepFound, w.EmitsUnmetOutput, w.NamesCodeThree,
		w.CategoryFound, w.PassesFlag, w.ReadsVerdictStep)
	if steps == 0 {
		t.Fatal("шагов в конвейере ноль — «ноль находок» означало бы «ноль прочитанного»")
	}
	for _, finding := range adjudicateThirdCategorySignal(w) {
		t.Error(finding)
	}
}

// readThirdCategoryWiring — чтение дерева, отделённое от суждения.
func readThirdCategoryWiring(doc signalDoc) (thirdCategoryWiring, int) {
	var w thirdCategoryWiring
	total := 0
	for _, job := range doc.Jobs {
		for _, st := range job.Steps {
			total++
			switch st.ID {
			case "report-gate":
				w.VerdictStepFound = true
				w.EmitsUnmetOutput = strings.Contains(st.Run, "unmet=true") &&
					strings.Contains(st.Run, "GITHUB_OUTPUT")
				w.NamesCodeThree = strings.Contains(st.Run, "= 3") ||
					strings.Contains(st.Run, "-eq 3") ||
					strings.Contains(st.Run, `"3"`)
			case "category":
				w.CategoryFound = true
				w.PassesFlag = strings.Contains(st.Run, "--verdict-unmet")
				for _, v := range st.Env {
					if strings.Contains(v, "report-gate") && strings.Contains(v, "unmet") {
						w.ReadsVerdictStep = true
					}
				}
			}
		}
	}
	return w, total
}
