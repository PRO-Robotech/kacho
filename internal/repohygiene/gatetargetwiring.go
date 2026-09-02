// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gatetargetwiring.go — механизм гейтов «у судьи обязан быть вызывающий».
//
// # Почему механизм ОДИН, а предметов два
//
// Судей-целей в services/iam уже два, и вопрос к обоим один и тот же: зовёт ли
// их хоть что-нибудь из того, что исполняется само. Первый механизм был написан
// под `module-manifest-check` (#1851) и знал имя цели константой; второй судья
// (`model-canon-check`, #1893) оказался вне его поля зрения by construction — не
// потому, что механизм неверен, а потому, что он знал ровно одно имя.
//
// Второй копией это не чинится: две копии одного механизма расходятся молча, и
// расходятся именно там, где расхождение не видно, — обе зелены на исправном
// дереве. Поэтому механизм вынесен сюда и принимает имя цели параметром, а
// каждый гейт остаётся при СВОЁМ предмете: своей прозе о том, что судит именно
// его судья, и своей переписи.
//
// # Носителей ДВА, и они не взаимозаменяемы
//
// Конвейер даёт вердикт на MR в ствол; локальный прогонщик — единственное, что
// стоит между правкой и стволом внутри накопительной линии, где вердикта
// конвейера не будет вовсе. Провязка, оставшаяся в одном носителе из двух, — это
// ровно тот случай, ради которого перепись печатает носители порознь.
//
// # Конвейер читается РАЗОБРАННЫМ, а не подстрокой
//
// Имя цели встречается в прозе, которая эту же цель объясняет: в шапке Makefile
// оно стоит пять раз, и один из них — готовый пример вызова. Гейт, ищущий имя
// подстрокой по сырому тексту, зеленел бы на собственном объяснении и оставался
// бы зелёным при снятой провязке.
//
// Для конвейера этого мало. Отбрасывания комментариев хватает, пока имя цели не
// попало в НЕисполняемое поле шага — в `name:`, в `if:`, в имя артефакта: такая
// строка комментарием не является, а вызовом не становится. Поэтому workflow
// разбирается как YAML и вызов ищется ТОЛЬКО в теле `run:` шага. Для прогонщика
// разбора нет и быть не может — это shell, и там исполняемая часть выделяется
// отбрасыванием строк-комментариев.
//
// # Куда уводит НЕРАСПОЗНАННАЯ форма вызова
//
// В безопасную сторону. Раскрытие цикла (`for svc in …`) здесь не поддержано, и
// это выбор: гейт скажет «объявляет, но никто не зовёт», то есть покраснеет,
// вместо того чтобы молча зачесть непонятую строку за провязку.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// localRunnerRel — локальный прогонщик: его зовёт хук отправки ветки, поэтому
// он исполняется сам, а не по памяти отправляющего.
const localRunnerRel = "scripts/ci-local.sh"

// judgeTargetWiring — провязка одной цели глазами гейта, каждый объём своим
// числом.
type judgeTargetWiring struct {
	// Target — имя цели, о которой всё нижеследующее.
	Target string
	// Declaring — сервисы, чей Makefile ОБЪЯВЛЯЕТ цель. Факт о дереве.
	Declaring []string
	// CalledByWorkflow — сервис → носители конвейера, зовущие цель.
	CalledByWorkflow map[string][]string
	// CalledByLocalRunner — сервисы, которые зовёт локальный прогонщик.
	CalledByLocalRunner map[string]bool
	// Объёмы осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного», поэтому каждый обход отчитывается своим числом.
	// WorkflowStepsRead считает шаги с телом `run:` — именно они и есть
	// популяция, в которой вызов вообще может стоять.
	MakefilesRead, WorkflowsRead, WorkflowStepsRead, LocalRunnersRead int
}

// workflowRunSteps — минимальная форма файла конвейера: нас интересует только
// исполняемое тело шага. Всё остальное (`name`, `if`, `uses`, `env`) намеренно
// не читается — вызовом оно не является ни в одной форме.
type workflowRunSteps struct {
	Jobs map[string]struct {
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// targetDeclarationRe — ОБЪЯВЛЕНИЕ цели в Makefile: строка правила, а не
// `.PHONY` и не присваивание переменной с таким именем.
func targetDeclarationRe(target string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\s*:(?:[^=]|$)`)
}

// targetCallRe — ВЫЗОВ цели у названного сервиса.
func targetCallRe(target string) *regexp.Regexp {
	return regexp.MustCompile(`make\s+-C\s+["']?services/([A-Za-z0-9_.-]+)["']?\s+` + regexp.QuoteMeta(target))
}

// readJudgeTargetWiring — состав провязки цели `target` в дереве `root`.
//
// Возвращает ошибку вместо падения, чтобы этой же функцией пользовалась
// инъекция: она обязана наблюдать исход, а не завершать прогон.
func readJudgeTargetWiring(root, target string) (judgeTargetWiring, error) {
	w := judgeTargetWiring{
		Target:              target,
		CalledByWorkflow:    map[string][]string{},
		CalledByLocalRunner: map[string]bool{},
	}
	declRe := targetDeclarationRe(target)
	callRe := targetCallRe(target)

	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		return w, fmt.Errorf("не прочитан services/: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// #nosec G304 -- имя каталога пришло из перечня services/ ЭТОГО дерева, а
		// хвост пути — константа: подставить посторонний файл извне нечем.
		raw, rerr := os.ReadFile(filepath.Join(root, "services", e.Name(), "Makefile"))
		if rerr != nil {
			continue
		}
		w.MakefilesRead++
		if declRe.Match(executablePartOf(string(raw))) {
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
		// #nosec G304 -- имя файла пришло из перечня .github/workflows ЭТОГО дерева,
		// корень каталога — константа: подставить посторонний файл извне нечем.
		raw, rerr := os.ReadFile(filepath.Join(wfDir, n))
		if rerr != nil {
			return w, fmt.Errorf("не прочитан %s: %w", n, rerr)
		}
		var doc workflowRunSteps
		// Файл конвейера, который не разбирается, — это НЕ «вызова нет»: о нём
		// не прочитано ничего, и молчаливый пропуск сделал бы «находок ноль»
		// неотличимым от «прочитано ноль».
		if uerr := yaml.Unmarshal(raw, &doc); uerr != nil {
			return w, fmt.Errorf("не разобран .github/workflows/%s: %w", n, uerr)
		}
		w.WorkflowsRead++
		for _, job := range doc.Jobs {
			for _, st := range job.Steps {
				if strings.TrimSpace(st.Run) == "" {
					continue
				}
				w.WorkflowStepsRead++
				for _, svc := range servicesCalledIn(st.Run, callRe) {
					w.CalledByWorkflow[svc] = append(w.CalledByWorkflow[svc], n)
				}
			}
		}
	}
	for svc := range w.CalledByWorkflow {
		sort.Strings(w.CalledByWorkflow[svc])
		w.CalledByWorkflow[svc] = uniqueStrings(w.CalledByWorkflow[svc])
	}

	// #nosec G304 -- путь склеен из корня дерева и КОНСТАНТЫ localRunnerRel;
	// переменной части у него нет вовсе.
	raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(localRunnerRel)))
	if rerr != nil {
		return w, fmt.Errorf("не прочитан %s: %w", localRunnerRel, rerr)
	}
	w.LocalRunnersRead++
	for _, svc := range servicesCalledIn(string(executablePartOf(string(raw))), callRe) {
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

// servicesCalledIn — сервисы, у которых данное тело зовёт цель.
//
// Тело `run:` шага конвейера — тоже shell, и комментарий внутри него вызовом не
// является: отбрасывание строк-комментариев применяется и здесь.
func servicesCalledIn(body string, callRe *regexp.Regexp) []string {
	seen := map[string]bool{}
	for _, m := range callRe.FindAllSubmatch(executablePartOf(body), -1) {
		seen[string(m[1])] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// uniqueStrings — соседние повторы прочь: один носитель, зовущий цель дважды,
// не есть два носителя.
func uniqueStrings(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i > 0 && s == prev {
			continue
		}
		out = append(out, s)
		prev = s
	}
	return out
}

// findJudgeTargetWiringFaults — расхождения провязки, в ОБЕ стороны.
func findJudgeTargetWiringFaults(w judgeTargetWiring) []string {
	var out []string
	declared := map[string]bool{}
	for _, svc := range w.Declaring {
		declared[svc] = true
	}

	for _, svc := range w.Declaring {
		if len(w.CalledByWorkflow[svc]) == 0 {
			out = append(out, fmt.Sprintf(
				"services/%s объявляет цель %s, но её не зовёт ни один шаг конвейера — судья есть, "+
					"исполнять его некому. Ось, которую судит ТОЛЬКО он, не встречает ни одной "+
					"автоматической проверки, и молчание неотличимо от исправной работы. Чинится "+
					"ПРОВЯЗКОЙ существующего судьи, а не вторым судьёй",
				svc, w.Target))
		}
		if !w.CalledByLocalRunner[svc] {
			out = append(out, fmt.Sprintf(
				"services/%s объявляет цель %s, но её не зовёт %s — отправка ветки уходит, ни разу "+
					"не спросив судью. Внутри накопительной линии вердикта конвейера не будет вовсе, "+
					"поэтому локальный прогон здесь единственное, что стоит между правкой и стволом",
				svc, w.Target, localRunnerRel))
		}
	}
	for svc, carriers := range w.CalledByWorkflow {
		if !declared[svc] {
			out = append(out, fmt.Sprintf(
				"%s зовёт %s для services/%s, но такой цели там не объявлено — шаг позеленеет ни на чём",
				strings.Join(carriers, ", "), w.Target, svc))
		}
	}
	for svc := range w.CalledByLocalRunner {
		if !declared[svc] {
			out = append(out, fmt.Sprintf(
				"%s зовёт %s для services/%s, но такой цели там не объявлено — шаг позеленеет ни на чём",
				localRunnerRel, w.Target, svc))
		}
	}
	sort.Strings(out)
	return out
}

// judgeTargetWiringCensus — перепись объёма осмотренного, одной строкой.
//
// Носители печатаются ПОРОЗНЬ: одно число скрыло бы ровно тот случай, ради
// которого гейт заведён, — провязку, оставшуюся в одном носителе из двух.
func judgeTargetWiringCensus(w judgeTargetWiring) string {
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
	return fmt.Sprintf("перепись цели %s: Makefile прочитано %d · объявляют цель %d (%s) · "+
		"workflow прочитано %d, шагов с телом run %d, зовут %d (%s) · прогонщиков прочитано %d, зовут %d (%s)",
		w.Target,
		w.MakefilesRead, len(w.Declaring), strings.Join(w.Declaring, ", "),
		w.WorkflowsRead, w.WorkflowStepsRead, len(byWorkflow), strings.Join(byWorkflow, ", "),
		w.LocalRunnersRead, len(local), strings.Join(local, ", "))
}
