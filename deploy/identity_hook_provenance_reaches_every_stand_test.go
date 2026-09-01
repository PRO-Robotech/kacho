// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_hook_provenance_reaches_every_stand_test.go — работа, ПОДНИМАЮЩАЯ
// СТЕНД, обязана собирать отчёт о происхождении величины обратного вызова.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (задача #1803)
//
// Отчёт существует и написан верно, но его звали ДВА конвейера из трёх: работа
// боевой посадки — единственная, где отказ шага подстановки и наблюдался, —
// диагноза не собирала. Величина, о которой спрашивает отчёт, живёт ТОЛЬКО в
// памяти двух контейнеров: ни в карту настроек, ни в рендер чарта она не
// попадает, а стенда прогона к моменту разбора уже нет. Значит несобранный
// отчёт — это не «неудобство», а НЕВОССТАНОВИМАЯ потеря: вопрос «одну ли
// величину держат стороны» после прогона задать больше некому.
//
// Цена измерена: разбор того же класса стоил трёх полных прогонов стенда и
// девяти опровергнутых гипотез.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДИКАТ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// «Работа посадки» опознаётся по ПРИЗНАКУ — шаг, поднимающий стенд рецептом
// `dev-up`/`dev-prod-up`, — а не по имени файла или задания. Выписанный перечень
// назвал бы сегодняшние три работы и не рос бы вместе с деревом: четвёртая,
// заведённая завтра, осталась бы без отчёта молча — ровно тот дефект, который
// здесь закрывают.
//
// Имя самого отчёта — КООРДИНАТА КОНТРАКТА, и она одна: гейт требует, чтобы
// названный скрипт в дереве существовал. Вызов в пустоту зеленел бы иначе так
// же, как настоящий.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА ЧЕСТНО
//
//   - гейт судит ОБЪЯВЛЕНИЕ конвейера: что шаг есть и зовёт отчёт. О том, что
//     отчёт на стенде что-то измерил, он не утверждает ничего — это предмет
//     самого отчёта и его доказательства инъекцией
//     (deploy/tests/helm/identity-hook-credential-provenance-inject.sh);
//   - работа, стенда НЕ поднимающая, отчёта не требует by construction:
//     измерять там нечего.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// provReportScript — координата отчёта, читаемая от каталога deploy.
const provReportScript = "scripts/identity-hook-credential-provenance.sh"

// provStandRecipes — рецепты, поднимающие стенд. Признак, а не перечень работ:
// работа опознаётся по тому, ЧТО она делает.
var provStandRecipes = []string{"dev-prod-up", "dev-up"}

// provJob — то, что гейт вывел об одном задании конвейера.
type provJob struct {
	workflow string
	job      string
	raises   bool // поднимает стенд
	reports  bool // собирает отчёт о происхождении величины
}

type provWorkflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// provFacts — обход объявлений конвейера. Возвращает факты и ОБЪЁМ
// ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль прочитанного».
func provFacts(t *testing.T) (jobs []provJob, files, steps int) {
	t.Helper()
	paths, err := filepath.Glob("../.github/workflows/*.y*ml")
	if err != nil {
		t.Fatalf("обход объявлений конвейера: %v", err)
	}
	sort.Strings(paths)
	for _, p := range paths {
		raw, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			continue
		}
		var wf provWorkflow
		if yaml.Unmarshal(raw, &wf) != nil {
			// Неразбираемое объявление — предмет соседних гейтов; здесь оно
			// пропускается, но остаётся ВИДНЫМ в переписи (files растёт, steps нет).
			files++
			continue
		}
		files++
		names := make([]string, 0, len(wf.Jobs))
		for n := range wf.Jobs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			f := provJob{workflow: filepath.Base(p), job: n}
			for _, s := range wf.Jobs[n].Steps {
				steps++
				for _, recipe := range provStandRecipes {
					if strings.Contains(s.Run, recipe) {
						f.raises = true
					}
				}
				if strings.Contains(s.Run, filepath.Base(provReportScript)) {
					f.reports = true
				}
			}
			jobs = append(jobs, f)
		}
	}
	return jobs, files, steps
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка была возможна.

func scanProvenanceCoverage(jobs []provJob) []string {
	var out []string
	for _, j := range jobs {
		if j.raises && !j.reports {
			out = append(out, fmt.Sprintf(
				"%s: задание %q поднимает стенд и НЕ собирает отчёт %s. "+
					"Величина обратного вызова живёт только в памяти контейнеров: "+
					"после прогона спросить её будет негде, и разбор пойдёт "+
					"воспроизведением — это полные прогоны стенда, а не минуты",
				j.workflow, j.job, provReportScript))
		}
		if !j.raises && j.reports {
			out = append(out, fmt.Sprintf(
				"%s: задание %q собирает отчёт %s, не поднимая стенда — "+
					"мерить нечего, и «условие не создано» станет там штатным исходом",
				j.workflow, j.job, provReportScript))
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestProvenanceReportIsCollectedByEveryStandJob(t *testing.T) {
	jobs, files, steps := provFacts(t)

	var raising, reporting []string
	for _, j := range jobs {
		if j.raises {
			raising = append(raising, j.workflow+":"+j.job)
		}
		if j.reports {
			reporting = append(reporting, j.workflow+":"+j.job)
		}
	}

	t.Logf("осмотрено: объявлений конвейера %d, заданий %d, шагов %d; "+
		"поднимают стенд %d (%s); собирают отчёт %d (%s)",
		files, len(jobs), steps,
		len(raising), strings.Join(raising, ", "),
		len(reporting), strings.Join(reporting, ", "))

	if files == 0 || steps == 0 {
		t.Fatalf("прочитано объявлений %d, шагов %d — обход пуст, и «ноль находок» "+
			"здесь неотличимо от «ноль прочитанного»", files, steps)
	}
	if len(raising) == 0 {
		t.Fatalf("не найдено НИ ОДНОГО задания, поднимающего стенд (искали рецепты %v) — "+
			"либо рецепты переименованы, либо разбор ослеп; в обоих случаях гейт "+
			"перестал что-либо требовать", provStandRecipes)
	}

	// Координата контракта: отчёт, который зовут, обязан существовать. Вызов в
	// пустоту зеленел бы неотличимо от настоящего.
	if _, err := os.Stat(provReportScript); err != nil {
		t.Fatalf("отчёт %s не найден (%v) — задания зовут скрипт, которого нет: "+
			"их шаги молча отдавали бы «условие не создано»", provReportScript, err)
	}

	for _, msg := range scanProvenanceCoverage(jobs) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestScanProvenanceCoverage_SelfTest(t *testing.T) {
	base := []provJob{
		{workflow: "посадка.yml", job: "стенд", raises: true, reports: true},
		{workflow: "проверки.yml", job: "разбор", raises: false, reports: false},
	}

	// (0) КОНТРОЛЬ: согласованное объявление молчит. Задание без стенда и без
	//     отчёта — законный близнец: без него отрицание зеленело бы на дереве,
	//     где вообще нет ни одной работы посадки.
	if got := scanProvenanceCoverage(base); len(got) != 0 {
		t.Errorf("(0) согласованные задания обязаны молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ — ровно исходный дефект #1803: стенд поднимается, отчёт не
	//     собирается.
	gap := []provJob{{workflow: "посадка.yml", job: "стенд", raises: true, reports: false}}
	got := scanProvenanceCoverage(gap)
	if len(got) == 0 || !strings.Contains(got[0], "НЕ собирает отчёт") {
		t.Errorf("(A) работа посадки без отчёта ПРОПУЩЕНА: %v", got)
	}
	if !strings.Contains(strings.Join(got, " "), "стенд") {
		t.Errorf("(A) находка не называет задание: %v", got)
	}

	// (B) ИНЪЕКЦИЯ в обратную сторону: отчёт зовётся там, где стенда нет.
	//     Без этой оси гейт принимал бы «позвали на всякий случай» за покрытие,
	//     а такой вызов даёт вечное «условие не создано» — исход, который в
	//     зачёт не идёт и потому невидим.
	orphan := []provJob{{workflow: "проверки.yml", job: "линт", raises: false, reports: true}}
	if got := scanProvenanceCoverage(orphan); len(got) == 0 {
		t.Errorf("(B) отчёт без стенда ПРОПУЩЕН")
	}
}
