// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestcheckwiring_test.go — гейт «единственный судья формы манифеста
// обязан иметь вызывающего» (задача #1851).
//
// # Что охраняется
//
// Форму манифеста домена судит ОДИН исполнитель — загрузчик
// services/iam/internal/manifest, поднимаемый целью `module-manifest-check`
// (services/iam/Makefile). Решение #1778 назначило судью единственным намеренно,
// и второго заводить нельзя. У единственности есть цена: если у этого судьи нет
// вызывающего, то ось, которую судит только он, не встречает НИ ОДНОЙ
// автоматической проверки — и молчание неотличимо от исправной работы.
//
// Так и было измерено в #1851: гейт `TestManifestIsNotASecondDeclarationOfARight`
// судит `resources[]` тремя ветвями, и ни одна не высказывается о ключе
// `producer` вне закрытого набора; отверг бы такую запись загрузчик — а цель,
// которая его зовёт, не исполнялась ни разу за свою жизнь.
//
// # Чем этот гейт НЕ является
//
// Он не судит форму манифеста и не дублирует загрузчика — второй судья формы
// запрещён решением #1778. Его предмет — ПРОВЯЗКА: существует ли у цели
// вызывающий среди того, что исполняется само. Это вторая половина класса
// «проверка с формой, но без содержания»: страж, который ничего не проверяет, и
// страж, которого никто не зовёт. Здесь — вторая.
//
// # Почему перечень ВЫВОДИТСЯ, а не выписан
//
// Рукописный перечень расходится с деревом — свойство механизма, а не
// аккуратности автора; в этом же дереве так уже отказывал гейт сужения списков.
// Поэтому множество «кто объявляет цель» берётся из services/*/Makefile, а
// множество «кто её зовёт» — из носителей, которые исполняются сами (шаги
// конвейера и локальный прогонщик, которым отправка ветки проверяется хуком).
// Сверка идёт в ОБЕ стороны: вызов цели, которой в Makefile нет, — такая же
// находка, потому что шаг молча позеленел бы на несуществующей цели.
//
// # Почему читается ИСПОЛНЯЕМАЯ часть, а не текст
//
// Имя цели встречается в прозе, которая эту же цель объясняет: в шапке самого
// Makefile оно стоит пять раз, и один из них — готовый пример вызова. Гейт,
// ищущий имя подстрокой по сырому тексту, зеленел бы на собственном объяснении и
// оставался бы зелёным при снятой провязке. Поэтому строки-комментарии
// отбрасываются до сопоставления, у обоих видов носителя признак комментария
// один и тот же (`#`).
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// moduleManifestCheckTarget — цель, поднимающая единственного судью формы.
const moduleManifestCheckTarget = "module-manifest-check"

// localRunnerRel — локальный прогонщик: его зовёт хук отправки ветки, поэтому
// он исполняется сам, а не по памяти отправляющего.
const localRunnerRel = "scripts/ci-local.sh"

// manifestCheckDeclRe — ОБЪЯВЛЕНИЕ цели в Makefile: строка правила, а не
// `.PHONY` и не присваивание переменной с таким именем.
var manifestCheckDeclRe = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(moduleManifestCheckTarget) + `\s*:(?:[^=]|$)`)

// manifestCheckCallRe — ВЫЗОВ цели у названного сервиса.
//
// Имя сервиса берётся литералом намеренно. Раскрытие цикла (`for svc in …`)
// здесь не поддержано, и это выбор, а не упущение: цель одна и сервис у неё
// один, а нераспознанная форма вызова уводит гейт в БЕЗОПАСНУЮ сторону — он
// скажет «объявляет, но никто не зовёт», то есть покраснеет, вместо того чтобы
// молча зачесть непонятую строку за провязку.
var manifestCheckCallRe = regexp.MustCompile(`make\s+-C\s+["']?services/([A-Za-z0-9_.-]+)["']?\s+` + regexp.QuoteMeta(moduleManifestCheckTarget))

// manifestCheckWiring — провязка глазами этого гейта, каждый объём своим числом.
type manifestCheckWiring struct {
	// Declaring — сервисы, чей Makefile ОБЪЯВЛЯЕТ цель. Факт о дереве.
	Declaring []string
	// CalledByWorkflow — сервис → носители конвейера, зовущие цель.
	CalledByWorkflow map[string][]string
	// CalledByLocalRunner — сервисы, которые зовёт локальный прогонщик.
	CalledByLocalRunner map[string]bool
	// Объёмы осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного», поэтому каждый обход отчитывается своим числом.
	MakefilesRead, WorkflowsRead, LocalRunnersRead int
}

// readManifestCheckWiring — состав провязки в дереве `root`.
//
// Возвращает ошибку вместо падения, чтобы этой же функцией пользовалась
// инъекция: она обязана наблюдать исход, а не завершать прогон.
func readManifestCheckWiring(root string) (manifestCheckWiring, error) {
	w := manifestCheckWiring{
		CalledByWorkflow:    map[string][]string{},
		CalledByLocalRunner: map[string]bool{},
	}

	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		return w, fmt.Errorf("не прочитан services/: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(root, "services", e.Name(), "Makefile"))
		if rerr != nil {
			continue
		}
		w.MakefilesRead++
		if manifestCheckDeclRe.Match(executablePartOf(string(raw))) {
			w.Declaring = append(w.Declaring, e.Name())
		}
	}
	sort.Strings(w.Declaring)

	wfDir := filepath.Join(root, ".github", "workflows")
	wfEntries, werr := os.ReadDir(wfDir)
	if werr != nil {
		return w, fmt.Errorf("не прочитан %s: %w", wfDir, werr)
	}
	for _, e := range wfEntries {
		n := e.Name()
		if e.IsDir() || (!strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml")) {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(wfDir, n))
		if rerr != nil {
			return w, fmt.Errorf("не прочитан %s: %w", n, rerr)
		}
		w.WorkflowsRead++
		for _, svc := range callsInCarrier(string(raw)) {
			w.CalledByWorkflow[svc] = append(w.CalledByWorkflow[svc], n)
		}
	}

	raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(localRunnerRel)))
	if rerr != nil {
		return w, fmt.Errorf("не прочитан %s: %w", localRunnerRel, rerr)
	}
	w.LocalRunnersRead++
	for _, svc := range callsInCarrier(string(raw)) {
		w.CalledByLocalRunner[svc] = true
	}

	return w, nil
}

// executablePartOf выбрасывает строки-комментарии: имя цели встречается в прозе,
// которая её же объясняет, и сопоставление по сырому тексту зеленело бы на
// собственном объяснении.
func executablePartOf(body string) []byte {
	var b strings.Builder
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			// Строка выбрасывается, но перевод строки остаётся: иначе соседние
			// строки склеились бы и `^`-якорь объявления цели сместился бы.
			b.WriteByte('\n')
			continue
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// callsInCarrier — сервисы, у которых носитель зовёт цель в исполняемой части.
func callsInCarrier(body string) []string {
	seen := map[string]bool{}
	for _, m := range manifestCheckCallRe.FindAllSubmatch(executablePartOf(body), -1) {
		seen[string(m[1])] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// findManifestCheckWiringFaults — расхождения провязки, в ОБЕ стороны.
func findManifestCheckWiringFaults(w manifestCheckWiring) []string {
	var out []string
	declared := map[string]bool{}
	for _, svc := range w.Declaring {
		declared[svc] = true
	}

	for _, svc := range w.Declaring {
		if len(w.CalledByWorkflow[svc]) == 0 {
			out = append(out, fmt.Sprintf(
				"services/%s объявляет цель %s, но её не зовёт ни один шаг конвейера — судья есть, "+
					"исполнять его некому. Ось, которую судит ТОЛЬКО он (ключ `producer` вне закрытого "+
					"набора), не встречает ни одной автоматической проверки, и молчание неотличимо от "+
					"исправной работы. Чинится ПРОВЯЗКОЙ существующего судьи, а не вторым судьёй формы",
				svc, moduleManifestCheckTarget))
		}
		if !w.CalledByLocalRunner[svc] {
			out = append(out, fmt.Sprintf(
				"services/%s объявляет цель %s, но её не зовёт %s — отправка ветки уходит, ни разу "+
					"не спросив судью формы. Внутри накопительной линии вердикта конвейера не будет "+
					"вовсе, поэтому локальный прогон здесь единственное, что стоит между правкой и стволом",
				svc, moduleManifestCheckTarget, localRunnerRel))
		}
	}
	for svc, carriers := range w.CalledByWorkflow {
		if !declared[svc] {
			out = append(out, fmt.Sprintf(
				"%s зовёт %s для services/%s, но такой цели там не объявлено — шаг позеленеет ни на чём",
				strings.Join(carriers, ", "), moduleManifestCheckTarget, svc))
		}
	}
	for svc := range w.CalledByLocalRunner {
		if !declared[svc] {
			out = append(out, fmt.Sprintf(
				"%s зовёт %s для services/%s, но такой цели там не объявлено — шаг позеленеет ни на чём",
				localRunnerRel, moduleManifestCheckTarget, svc))
		}
	}
	sort.Strings(out)
	return out
}

// TestModuleManifestCheckTargetHasACaller — у единственного судьи формы обязан
// быть вызывающий среди того, что исполняется само.
//
// Что делать, если гейт сработал: провязать цель `module-manifest-check` шагом
// конвейера и группой локального прогонщика, читая её код возврата ЧЕТЫРЬМЯ
// исходами (0 годно · 1 находка · 2 проверять нечего · 3 не исполнялась). VOID
// не схлопывать в успех: пустое дерево отчитывалось бы зелёным так же уверенно,
// как проверенное. Заводить вторую проверку формы взамен провязки — нельзя.
func TestModuleManifestCheckTargetHasACaller(t *testing.T) {
	root := repoRoot(t)
	w, err := readManifestCheckWiring(root)
	if err != nil {
		t.Fatalf("провязка не прочитана: %v — вердикт беспредметен", err)
	}

	// Предпосылки обхода: пустой обход обязан ронять прогон, иначе «находок
	// ноль» означало бы «прочитано ноль».
	if w.MakefilesRead == 0 {
		t.Fatal("не прочитано ни одного services/*/Makefile — гейт смотрит не туда, его вердикт беспредметен")
	}
	if w.WorkflowsRead == 0 {
		t.Fatal("не прочитано ни одного файла .github/workflows — гейт смотрит не туда")
	}
	if w.LocalRunnersRead == 0 {
		t.Fatalf("не прочитан %s — гейт смотрит не туда", localRunnerRel)
	}
	// Положительный контроль: цель обязана быть НАЙДЕНА объявленной. Ноль
	// объявляющих означает, что распознавание сломалось либо цель сняли, и тогда
	// «провязка в порядке» — не свойство дерева, а слепота гейта.
	if len(w.Declaring) == 0 {
		t.Fatalf("ни один из %d прочитанных Makefile не объявляет цель %s — распознавание сломано "+
			"либо цель сняли; в обоих случаях молчание про провязку ничего не значит",
			w.MakefilesRead, moduleManifestCheckTarget)
	}

	for _, f := range findManifestCheckWiringFaults(w) {
		t.Errorf("%s", f)
	}

	// Перепись печатает объём ПО КАЖДОМУ НОСИТЕЛЮ отдельно: одно число скрыло бы
	// ровно тот случай, ради которого гейт заведён, — провязку, оставшуюся в
	// одном носителе из двух.
	byWorkflow := make([]string, 0, len(w.CalledByWorkflow))
	for svc, carriers := range w.CalledByWorkflow {
		byWorkflow = append(byWorkflow, svc+"←"+strings.Join(carriers, "+"))
	}
	sort.Strings(byWorkflow)
	local := make([]string, 0, len(w.CalledByLocalRunner))
	for svc := range w.CalledByLocalRunner {
		local = append(local, svc)
	}
	sort.Strings(local)
	t.Logf("перепись: Makefile прочитано %d · объявляют цель %d (%s) · workflow прочитано %d, "+
		"зовут %d (%s) · прогонщиков прочитано %d, зовут %d (%s)",
		w.MakefilesRead, len(w.Declaring), strings.Join(w.Declaring, ", "),
		w.WorkflowsRead, len(byWorkflow), strings.Join(byWorkflow, ", "),
		w.LocalRunnersRead, len(local), strings.Join(local, ", "))
}
