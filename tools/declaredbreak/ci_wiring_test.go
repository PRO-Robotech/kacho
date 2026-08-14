// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Проводка гейта в конвейер проверяется ЧТЕНИЕМ workflow, а не доверием к комментарию,
// который утверждает, что CI это гоняет. Конвенция дерева, и она выведена из реального
// провала у соседей: гейт, которого никто не исполняет, стоит ровно столько же, сколько
// гейт, который ничего не проверяет.
//
// Здесь проверяются ЧЕТЫРЕ свойства шага, и каждое — то, без чего гейт становится
// украшением:
//
//  1. адъюдикатор ВЫЗЫВАЕТСЯ (иначе перечень объявленных разрывов не читает никто);
//  2. buf зовётся с машинным форматом вывода (без него адъюдикатору нечего разбирать);
//  3. КОД ВОЗВРАТА buf разбирается: 100 — «есть находки», 0 — «разрывов нет», любой
//     другой — гейт не сделал своей работы. Без этого разбора сетевой отказ читался бы
//     как «разрывов нет» — ровно тот исход, прецедент которого записан в этом же файле
//     конвейера (при исчерпании квоты три шага получали skipped, а вердикт выдавался);
//  4. перечню передаётся ПУТЬ, а не подразумевается умолчание рабочего каталога — шаг
//     меняет каталог по ходу, и умолчание разошлось бы с местом запуска молча.
package declaredbreak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type ciWorkflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func loadCI(t *testing.T) ciWorkflow {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", "ci.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("конвейер не прочитан (%s): %v", path, err)
	}
	var wf ciWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("конвейер не разобран: %v", err)
	}
	if len(wf.Jobs) == 0 {
		t.Fatal("в конвейере ноль джоб — прочитано не то, что нужно")
	}
	return wf
}

func TestGateIsWiredIntoCI(t *testing.T) {
	wf := loadCI(t)

	var (
		stepsRead int
		found     bool
		run       string
		stepName  string
		guardIf   string
	)
	for _, job := range wf.Jobs {
		for _, st := range job.Steps {
			stepsRead++
			if strings.Contains(st.Run, "adjudicate-declared-breaks") {
				found = true
				run = st.Run
				stepName = st.Name
				guardIf = st.If
			}
		}
	}
	t.Logf("осмотрено: джоб %d, шагов %d", len(wf.Jobs), stepsRead)

	if !found {
		t.Fatal("ни один шаг конвейера не зовёт adjudicate-declared-breaks — перечень объявленных разрывов не читает никто")
	}
	t.Logf("шаг: %q, условие: %q", stepName, guardIf)

	// (2) машинный формат вывода
	if !strings.Contains(run, "--error-format=json") {
		t.Error("buf зовётся без --error-format=json — адъюдикатору нечего разбирать")
	}
	// (2а) buf вообще зовётся именно на breaking
	if !strings.Contains(run, "buf breaking") {
		t.Error("шаг не зовёт buf breaking — сравнивать не с чем")
	}
	// (2б) сравнение идёт против ствола, а не против самого себя
	if !strings.Contains(run, "branch=main") {
		t.Error("сравнение идёт не против main — разрыв относительно ствола не обнаружится")
	}
	// (3) код возврата разбирается, и «непонятный код» — отдельный исход
	if !strings.Contains(run, "rc=$?") || !strings.Contains(run, "0|100") {
		t.Error("код возврата buf не разбирается: сетевой отказ читался бы как «разрывов нет»")
	}
	if !strings.Contains(run, "exit 2") {
		t.Error("у исхода «гейт не сделал своей работы» нет отдельного кода возврата")
	}
	// (4) путь перечня передан явно
	if !strings.Contains(run, "proto/declared-breaks.yaml") {
		t.Error("путь перечня не передан явно — шаг меняет каталог, и умолчание разошлось бы с местом запуска")
	}
}

// TestWiringProbeCanFail — положительный контроль САМОЙ пробы провязки. Без него
// «шаг найден» неотличимо от «проба ничего не искала».
func TestWiringProbeCanFail(t *testing.T) {
	const withoutStep = `
jobs:
  build:
    steps:
      - name: buf lint
        run: buf lint
`
	var wf ciWorkflow
	if err := yaml.Unmarshal([]byte(withoutStep), &wf); err != nil {
		t.Fatalf("синтетический конвейер не разобран: %v", err)
	}
	for _, job := range wf.Jobs {
		for _, st := range job.Steps {
			if strings.Contains(st.Run, "adjudicate-declared-breaks") {
				t.Fatal("проба нашла адъюдикатор там, где его нет — искала не то")
			}
		}
	}
	// И зеркально: на конвейере, где шаг ЕСТЬ, тот же предикат его находит.
	const withStep = `
jobs:
  build:
    steps:
      - name: adjudicate breaking (vs main)
        run: adjudicate-declared-breaks proto/declared-breaks.yaml
`
	var wf2 ciWorkflow
	if err := yaml.Unmarshal([]byte(withStep), &wf2); err != nil {
		t.Fatalf("синтетический конвейер не разобран: %v", err)
	}
	var hit bool
	for _, job := range wf2.Jobs {
		for _, st := range job.Steps {
			if strings.Contains(st.Run, "adjudicate-declared-breaks") {
				hit = true
			}
		}
	}
	if !hit {
		t.Fatal("предикат не находит шаг, который заведомо есть — проба провязки негодна")
	}
}
