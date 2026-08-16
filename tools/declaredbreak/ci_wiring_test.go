// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Проводка гейта в конвейер проверяется ЧТЕНИЕМ workflow, а не доверием к комментарию,
// который утверждает, что CI это гоняет. Конвенция дерева, и она выведена из реального
// провала у соседей: гейт, которого никто не исполняет, стоит ровно столько же, сколько
// гейт, который ничего не проверяет.
//
// Здесь проверяются ПЯТЬ свойств шага, и каждое — то, без чего гейт становится
// украшением:
//
//  1. адъюдикатор ВЫЗЫВАЕТСЯ (иначе перечень объявленных разрывов не читает никто);
//  2. buf зовётся с машинным форматом вывода (без него адъюдикатору нечего разбирать);
//  3. КОД ВОЗВРАТА buf разбирается: 100 — «есть находки», 0 — «разрывов нет», любой
//     другой — гейт не сделал своей работы. Без этого разбора сетевой отказ читался бы
//     как «разрывов нет» — ровно тот исход, прецедент которого записан в этом же файле
//     конвейера (при исчерпании квоты три шага получали skipped, а вердикт выдавался);
//  4. перечню передаётся ПУТЬ, а не подразумевается умолчание рабочего каталога — шаг
//     меняет каталог по ходу, и умолчание разошлось бы с местом запуска молча;
//  5. шаг ИСПОЛНЯЕТСЯ ТАМ, ГДЕ ЕГО ПРЕДМЕТ ИСТЕКАЕТ, — то есть на push в ствол, а не
//     только на запросах на слияние (TestGateRunsWhereItsSubjectExpires, kacho#490).
package declaredbreak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type ciWorkflow struct {
	On struct {
		Push struct {
			Branches []string `yaml:"branches"`
			Paths    []string `yaml:"paths"`
		} `yaml:"push"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

const ciWorkflowPath = "ci.yaml"

func loadCI(t *testing.T) ciWorkflow {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", ciWorkflowPath)
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

// adjudicationStep — шаг, зовущий адъюдикатор. Возвращает его условие и признак того,
// что шаг вообще найден: «шага нет» и «у шага нет условия» — разные исходы, и слить их
// значило бы объявить свойство выполненным на конвейере, где шага не существует.
func adjudicationStep(wf ciWorkflow) (name, ifCond string, found bool) {
	for _, job := range wf.Jobs {
		for _, st := range job.Steps {
			if strings.Contains(st.Run, "adjudicate-declared-breaks") {
				return st.Name, st.If, true
			}
		}
	}
	return "", "", false
}

// narrowsByEvent — условие сужает исполнение ПО ВИДУ СОБЫТИЯ.
//
// Предикат намеренно синтаксический, потому что вычислять выражение GitHub статически
// нечем: `always()`, `!cancelled()`, `steps.x.outcome` — законные условия, и запрещать
// условие как таковое значило бы ловить форму. Запрещается ровно одно: привязка к виду
// события, потому что она и есть тот способ, которым шаг перестаёт исполняться на стволе.
func narrowsByEvent(cond string) bool {
	return strings.Contains(cond, "event_name") || strings.Contains(cond, "event.pull_request")
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
	if stepsRead == 0 {
		t.Fatal("прочитано ноль шагов — «шаг не найден» неотличимо от «читать было нечего»")
	}

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

// TestGateRunsWhereItsSubjectExpires — свойство (5): шаг исполняется ТАМ, ГДЕ ЕГО ПРЕДМЕТ
// ИСТЕКАЕТ.
//
// # Что здесь за предмет и когда он истекает
//
// Запись перечня объявленных разрывов живёт от объявления разрыва в ветке до ВЛИВАНИЯ
// этой ветки в ствол: база сравнения поднимается, разрыв становится историей, и запись
// обязана быть снята. То есть момент истечения — push в ствол, а не правка контракта.
//
// # Чем это было и почему возвращаться нельзя
//
// Шаг был объявлен `if: github.event_name == 'pull_request'` и на стволе не исполнялся
// ВОВСЕ. Ствол при этом зеленел не оттого, что перечень чист, а оттого, что там его никто
// не спрашивал; красное доставалось первому же PR, догнавшему ствол, и выглядело как
// дефект ЭТОГО PR. Класс повторился четыре раза (21 запись → 2 → 4 → 9), и в последний
// раз одни и те же девять записей снимали две линии независимо. Разбор — kacho#490.
//
// # Что именно утверждается, и почему предикат синтаксический
//
// Вычислить выражение `if` статически нечем, поэтому запрещается не условие вообще
// (`always()`, `!cancelled()` законны), а привязка к ВИДУ СОБЫТИЯ — единственный способ,
// которым шаг перестаёт исполняться на стволе. Рядом проверяется вторая половина того же
// свойства: конвейер обязан ИДТИ на push в ствол, иначе снятое условие ничего не даёт, а
// фильтр по путям обязан покрывать каталог контрактов, иначе слепая зона возвращается
// другой дверью.
//
// Способность пробы упасть доказана инъекцией в обе стороны — TestSubjectExpiryProbeCanFail.
func TestGateRunsWhereItsSubjectExpires(t *testing.T) {
	wf := loadCI(t)

	name, cond, found := adjudicationStep(wf)
	if !found {
		t.Fatal("шаг адъюдикации не найден — свойство «исполняется на стволе» утверждать не о чем")
	}
	t.Logf("осмотрено: конвейер %s, джоб %d; шаг %q, условие %q, триггер push branches=%v paths=%v",
		ciWorkflowPath, len(wf.Jobs), name, cond, wf.On.Push.Branches, wf.On.Push.Paths)

	// (5а) шаг не сужен видом события
	if narrowsByEvent(cond) {
		t.Errorf("шаг адъюдикации сужен видом события (%q): на стволе он не исполнится, "+
			"а предмет записи перечня истекает именно там — ствол будет зелен оттого, что его "+
			"не спрашивают, и красное достанется первому же PR, догнавшему ствол (kacho#490)", cond)
	}

	// (5б) конвейер вообще идёт на push в ствол
	switch {
	case !hasPushTrigger(t):
		t.Error("конвейер не объявляет триггер push вовсе — снятого условия у шага мало: " +
			"исполняться ему всё равно негде")
	case !branchesCoverTrunk(wf.On.Push.Branches):
		t.Errorf("push сужен ветками %v, ствола среди них нет — адъюдикация не отработает "+
			"там, где предмет записей истекает", wf.On.Push.Branches)
	}

	// (5в) фильтр по путям, если он появится, обязан покрывать каталог контрактов
	if len(wf.On.Push.Paths) > 0 && !pathsCoverContracts(wf.On.Push.Paths) {
		t.Errorf("push сужен путями %v, и ни один не покрывает proto/ — вливание, поднявшее "+
			"базу сравнения, не запустит адъюдикацию, то есть слепая зона вернётся другой дверью",
			wf.On.Push.Paths)
	}
}

// hasPushTrigger — у конвейера объявлен push. Читается отдельно от branches, потому что
// «push без сужения ветками» и «push не объявлен вовсе» дают одинаковый пустой срез, а
// это разные исходы: первый идёт на ствол, второй не идёт никуда.
func hasPushTrigger(t *testing.T) bool {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", ciWorkflowPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("конвейер не прочитан (%s): %v", path, err)
	}
	var probe struct {
		On map[string]any `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("конвейер не разобран: %v", err)
	}
	_, ok := probe.On["push"]
	return ok
}

// branchesCoverTrunk — среди веток триггера есть ствол. Пустой срез означает «push без
// сужения ветками», то есть на все ветки, включая ствол.
func branchesCoverTrunk(branches []string) bool {
	if len(branches) == 0 {
		return true
	}
	for _, b := range branches {
		if b == "main" || b == "*" || b == "**" {
			return true
		}
	}
	return false
}

// pathsCoverContracts — хоть один образец фильтра захватывает каталог контрактов.
func pathsCoverContracts(paths []string) bool {
	for _, p := range paths {
		if strings.HasPrefix(p, "proto") || strings.HasPrefix(p, "**") {
			return true
		}
	}
	return false
}

// TestSubjectExpiryProbeCanFail — инъекция в ОБЕ стороны для пробы выше.
//
// Без отрицательной половины «условия нет» неотличимо от «проба ничего не смотрела»; без
// положительной проба ловила бы форму (любое `if`), а не существо (привязку к событию).
func TestSubjectExpiryProbeCanFail(t *testing.T) {
	cases := []struct {
		name       string
		cond       string
		wantNarrow bool
	}{
		{"вернули прежнее условие", "github.event_name == 'pull_request'", true},
		{"та же привязка другой формой", "${{ github.event.pull_request.number != '' }}", true},
		{"законный близнец: always()", "always()", false},
		{"законный близнец: по исходу шага", "${{ always() && steps.buf.outcome == 'success' }}", false},
		{"условия нет вовсе", "", false},
	}
	for _, c := range cases {
		if got := narrowsByEvent(c.cond); got != c.wantNarrow {
			t.Errorf("%s: условие %q — сужение по событию %v, ожидалось %v", c.name, c.cond, got, c.wantNarrow)
		}
	}

	// И то же на форме конвейера целиком: предикат обязан достать условие ИЗ ШАГА, а не
	// из соседнего, — иначе он молчал бы, читая чужое `always()`.
	const narrowed = `
jobs:
  proto:
    steps:
      - name: buf lint
        if: always()
        run: buf lint
      - name: adjudicate breaking (vs main)
        if: github.event_name == 'pull_request'
        run: adjudicate-declared-breaks proto/declared-breaks.yaml
`
	var wf ciWorkflow
	if err := yaml.Unmarshal([]byte(narrowed), &wf); err != nil {
		t.Fatalf("синтетический конвейер не разобран: %v", err)
	}
	_, cond, found := adjudicationStep(wf)
	if !found {
		t.Fatal("шаг адъюдикации не найден в синтетическом конвейере, где он есть")
	}
	if !narrowsByEvent(cond) {
		t.Fatalf("предикат не увидел сужения по событию на конвейере, где оно возвращено: %q", cond)
	}

	// Зеркально: тот же конвейер без условия у шага — предикат молчит.
	const wide = `
jobs:
  proto:
    steps:
      - name: adjudicate breaking (vs main)
        run: adjudicate-declared-breaks proto/declared-breaks.yaml
`
	var wf2 ciWorkflow
	if err := yaml.Unmarshal([]byte(wide), &wf2); err != nil {
		t.Fatalf("синтетический конвейер не разобран: %v", err)
	}
	if _, cond2, _ := adjudicationStep(wf2); narrowsByEvent(cond2) {
		t.Fatalf("предикат нашёл сужение там, где условия нет: %q", cond2)
	}

	// Фильтр по путям: покрывающий каталог контрактов — законен, не покрывающий — находка.
	if pathsCoverContracts([]string{"ui-future/**", "deploy/**"}) {
		t.Error("фильтр, не касающийся контрактов, признан покрывающим")
	}
	if !pathsCoverContracts([]string{"ui-future/**", "proto/**"}) {
		t.Error("фильтр, покрывающий контракты, признан непокрывающим — проба ловила бы законное")
	}

	// Ветки триггера: ствол среди них — законно, только релизные — находка, пустой срез —
	// «push на все ветки», тоже законно.
	for _, c := range []struct {
		branches []string
		want     bool
	}{
		{nil, true},
		{[]string{"main"}, true},
		{[]string{"**"}, true},
		{[]string{"release/**"}, false},
	} {
		if got := branchesCoverTrunk(c.branches); got != c.want {
			t.Errorf("branchesCoverTrunk(%v) = %v, ожидалось %v", c.branches, got, c.want)
		}
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
