// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_verdict_carries_provenance_test.go — работа, ПОДНИМАЮЩАЯ СТЕНД, обязана
// спросить, какое ДЕРЕВО этот стенд исполняет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (задача #2180)
//
// Расходятся ДЕРЕВО и СОБРАННОЕ ИЗ НЕГО. Ручка стоит в окружении пода, значение
// верное, профиль безупречен — а процесс отказывает так, будто её нет: читателя
// нет в КОНКРЕТНОЙ СБОРКЕ. Наблюдалось дважды за один заход выкатки (#2177):
// поле стража старта и читающая его ветка введены тем же коммитом, что и значения
// профиля, а образ на стенде собран на 7 ч 16 мин раньше.
//
// НИ ОДИН ГЕЙТ ДЕРЕВА ЭТОГО НЕ УВИДИТ BY CONSTRUCTION — включая этот. Все они
// судят дерево, а расходятся дерево и собранное. Единственный, кто может
// ответить, — работающий контейнер. Инструмент, чтобы его спросить, существовал
// (`deploy/scripts/stand-provenance.sh`), и он НЕ ЗВАЛСЯ: три работы конвейера,
// чья краснота цитировалась в трёх задачах линии, мерили ревизию старше головы на
// шесть часов.
//
// Отсюда предмет ЭТОГО гейта — не расхождение, а ВОПРОС: работа, выносящая
// вердикт о поведении стенда, обязана задать его сама. Это свойство ОБЪЯВЛЕНИЯ
// конвейера, то есть ровно то, что дерево и содержит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДИКАТ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// «Работа подъёма» опознаётся по рецепту (`dev-up`/`dev-prod-up`) — по тому, ЧТО
// работа делает, а не по имени файла или задания. Выписанный перечень работ
// назвал бы сегодняшние и не рос бы вместе с деревом: заведённая завтра работа
// подъёма осталась бы без вопроса молча — ровно тот дефект, который здесь
// закрывают.
//
// Признак и форма разбора объявлены здесь ОДИН раз (`standRecipes`,
// `standWorkflow`). Прежде их объявлял сосед — гейт отчёта о происхождении
// величины обратного вызова; отчёт снят вместе с поставщиком личности, чей
// отправитель эту величину держал (kacho#1276), и признак переехал к
// единственному оставшемуся читателю, а не остался в снятом файле.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА ЧЕСТНО
//
//   - гейт судит ОБЪЯВЛЕНИЕ: что шаг есть и зовёт владельца. О том, что на
//     стенде что-то сошлось, он не утверждает НИЧЕГО — это предмет самого
//     владельца и его доказательства инъекцией
//     (.github/scripts/stand-revision-verdict.sh --self-test, шесть осей);
//   - работа, стенда НЕ поднимающая, вопроса не требует by construction:
//     спрашивать не у кого;
//   - гейт не судит, ГДЕ в работе стоит шаг. Порядок — предмет соседнего гейта
//     связности («условие не создано» доезжает до вердикта), и второе место об
//     одном предмете разошлось бы с первым.
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

// revOwner — единственный владелец вопроса «какое дерево исполняет стенд».
// Координата контракта: тела шагов зовут ЕГО, и он обязан существовать — вызов в
// пустоту зеленел бы неотличимо от настоящего.
const revOwner = "../.github/scripts/stand-revision-verdict.sh"

// revOwnerCall — то, что ищется в теле шага. Базовое имя, а не полный путь:
// работы зовут владельца от `$GITHUB_WORKSPACE`, и полный путь сделал бы предикат
// зависимым от способа адресации, а не от того, что шаг ДЕЛАЕТ.
const revOwnerCall = "stand-revision-verdict.sh"

// standRecipes — рецепты, поднимающие стенд. Признак, а не перечень работ:
// работа опознаётся по тому, ЧТО она делает. `own-up` поднимает цепочку `own` из
// сборки дерева (kacho#2931).
var standRecipes = []string{"dev-prod-up", "dev-up", "own-up"}

// standWorkflow — форма разбора объявления конвейера: задания, их матрица и тела
// шагов. Матрица читается затем, что шаг вправе назвать рецепт измерением
// (`make ${{ matrix.stack }}-up`): буквального имени рецепта в теле тогда нет, и
// поиск по тексту работу подъёма не узнал бы — ровно так он ослеп на
// production-posture.yml при переходе на ноги по цепочкам.
type standWorkflow struct {
	Jobs map[string]struct {
		Strategy struct {
			Matrix any `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// runNamesARecipe — тело шага называет рецепт подъёма: буквально либо через
// измерение ЛИТЕРАЛЬНОЙ матрицы, раскрытое по её значениям. Матрица из выражения
// не раскрывается — её значений в объявлении нет; такой шаг узнаётся, только если
// рецепт назван буквально.
func runNamesARecipe(run string, matrix any) bool {
	texts := []string{run}
	if m, ok := matrix.(map[string]any); ok {
		for dim, vals := range m {
			list, ok := vals.([]any)
			if !ok {
				continue
			}
			ref := regexp.MustCompile(`\$\{\{\s*matrix\.` + regexp.QuoteMeta(dim) + `\s*\}\}`)
			for _, v := range list {
				texts = append(texts, ref.ReplaceAllString(run, fmt.Sprint(v)))
			}
		}
	}
	for _, text := range texts {
		for _, recipe := range standRecipes {
			if strings.Contains(text, recipe) {
				return true
			}
		}
	}
	return false
}

// revJob — то, что гейт вывел об одном задании конвейера.
type revJob struct {
	workflow string
	job      string
	raises   bool // поднимает стенд
	asks     bool // спрашивает, какое дерево этот стенд исполняет
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка была возможна.

func scanStandRevisionCoverage(jobs []revJob) []string {
	var out []string
	for _, j := range jobs {
		if j.raises && !j.asks {
			out = append(out, fmt.Sprintf(
				"%s: задание %q поднимает стенд и НЕ спрашивает, какое дерево он исполняет "+
					"(владелец %s). Вердикт о поведении такого стенда выносится про "+
					"неизвестно что: расходятся дерево и собранное из него, и гейт дерева "+
					"этого не увидит by construction",
				j.workflow, j.job, revOwnerCall))
		}
		if !j.raises && j.asks {
			out = append(out, fmt.Sprintf(
				"%s: задание %q зовёт %s, не поднимая стенда — спрашивать не у кого, "+
					"и «условие не создано» станет там штатным исходом",
				j.workflow, j.job, revOwnerCall))
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБХОД — по признаку подъёма (`standRecipes`) и форме разбора
// (`standWorkflow`), объявленным выше один раз.

func revFacts(t *testing.T) (jobs []revJob, files, steps int) {
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
		var wf standWorkflow
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
			j := revJob{workflow: filepath.Base(p), job: n}
			for _, s := range wf.Jobs[n].Steps {
				steps++
				if runNamesARecipe(s.Run, wf.Jobs[n].Strategy.Matrix) {
					j.raises = true
				}
				if strings.Contains(s.Run, revOwnerCall) {
					j.asks = true
				}
			}
			jobs = append(jobs, j)
		}
	}
	return jobs, files, steps
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestEveryStandJobAsksWhichTreeItRuns(t *testing.T) {
	jobs, files, steps := revFacts(t)

	var raising, asking []string
	for _, j := range jobs {
		if j.raises {
			raising = append(raising, j.workflow+":"+j.job)
		}
		if j.asks {
			asking = append(asking, j.workflow+":"+j.job)
		}
	}

	t.Logf("осмотрено: объявлений конвейера %d, заданий %d, шагов %d; "+
		"поднимают стенд %d (%s); спрашивают ревизию %d (%s)",
		files, len(jobs), steps,
		len(raising), strings.Join(raising, ", "),
		len(asking), strings.Join(asking, ", "))

	if files == 0 || steps == 0 {
		t.Fatalf("прочитано объявлений %d, шагов %d — обход пуст, и «ноль находок» "+
			"здесь неотличимо от «ноль прочитанного»", files, steps)
	}
	if len(raising) == 0 {
		t.Fatalf("не найдено НИ ОДНОГО задания, поднимающего стенд (рецепты %v) — "+
			"либо рецепты переименованы, либо разбор ослеп; в обоих случаях гейт "+
			"перестал что-либо требовать", standRecipes)
	}

	if _, err := os.Stat(revOwner); err != nil {
		t.Fatalf("владелец вопроса %s не найден (%v) — работы зовут скрипт, которого "+
			"нет: их шаги молча отдавали бы «условие не создано»", revOwner, err)
	}

	for _, msg := range scanStandRevisionCoverage(jobs) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestScanStandRevisionCoverage_SelfTest(t *testing.T) {
	base := []revJob{
		{workflow: "посадка.yml", job: "стенд", raises: true, asks: true},
		{workflow: "проверки.yml", job: "разбор", raises: false, asks: false},
	}

	// (0) КОНТРОЛЬ: согласованное объявление молчит. Задание без стенда и без
	//     вопроса — законный близнец: без него отрицание зеленело бы на дереве,
	//     где вообще нет ни одной работы подъёма.
	if got := scanStandRevisionCoverage(base); len(got) != 0 {
		t.Errorf("(0) согласованные задания обязаны молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ — ровно исходный дефект #2180: стенд поднимается, вопрос «какое
	//     дерево он исполняет» не задаётся.
	gap := []revJob{{workflow: "посадка.yml", job: "стенд", raises: true, asks: false}}
	got := scanStandRevisionCoverage(gap)
	if len(got) == 0 || !strings.Contains(got[0], "НЕ спрашивает") {
		t.Errorf("(A) работа подъёма без вопроса ПРОПУЩЕНА: %v", got)
	}
	if !strings.Contains(strings.Join(got, " "), "стенд") {
		t.Errorf("(A) находка не называет задание: %v", got)
	}

	// (B) ИНЪЕКЦИЯ в обратную сторону: владелец зовётся там, где стенда нет.
	//     Без этой оси гейт принимал бы «позвали на всякий случай» за покрытие, а
	//     такой вызов даёт вечное «условие не создано» — исход, который в зачёт не
	//     идёт и потому невидим.
	orphan := []revJob{{workflow: "проверки.yml", job: "линт", raises: false, asks: true}}
	if got := scanStandRevisionCoverage(orphan); len(got) == 0 {
		t.Errorf("(B) вопрос без стенда ПРОПУЩЕН")
	}
}

// Рецепт, названный измерением литеральной матрицы, узнаётся; тот же шаг с
// измерением, не дающим рецепта, — нет (законный близнец); буквальный — как прежде.
func TestRunNamesARecipeThroughALiteralMatrix(t *testing.T) {
	run := `.github/scripts/stand-up.sh --dir deploy -- make ${{ matrix.stack }}-up`
	if !runNamesARecipe(run, map[string]any{"stack": []any{"dev-prod", "own"}}) {
		t.Error("рецепт, названный измерением литеральной матрицы, не узнан — работа подъёма ослепла бы")
	}
	if runNamesARecipe(run, map[string]any{"stack": []any{"lint"}}) {
		t.Error("законный близнец: измерение не даёт рецепта подъёма, а шаг узнан поднимающим")
	}
	if runNamesARecipe(run, "${{ fromJSON(x) }}") {
		t.Error("матрица из выражения раскрыта догадкой")
	}
	if !runNamesARecipe("make dev-up CLUSTER_NAME=x", nil) {
		t.Error("буквальный рецепт перестал узнаваться")
	}
}
