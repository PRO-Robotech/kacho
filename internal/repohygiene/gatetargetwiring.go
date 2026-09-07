// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gatetargetwiring.go — механизм гейтов «у судьи обязан быть вызывающий».
//
// # Почему механизм ОДИН, а предметов три
//
// Судей-целей в дереве уже три, и вопрос ко всем один и тот же: зовёт ли их хоть
// что-нибудь из того, что исполняется само. Первый механизм был написан под
// `module-manifest-check` (#1851) и знал имя цели константой; второй судья
// (`model-canon-check`, #1893) оказался вне его поля зрения by construction — не
// потому, что механизм неверен, а потому, что он знал ровно одно имя. Третий
// (`permission-catalog-check`, #2084) оказался вне поля зрения по другой оси:
// он объявлен НЕ в services/*, а в gateway/Makefile, и зовут его не через `-C`,
// а рабочим каталогом шага.
//
// Второй копией это не чинится: две копии одного механизма расходятся молча, и
// расходятся именно там, где расхождение не видно, — обе зелены на исправном
// дереве. Поэтому механизм вынесен сюда и принимает КАТАЛОГ и ИМЯ ЦЕЛИ
// параметрами, а каждый гейт остаётся при СВОЁМ предмете: своей прозе о том, что
// судит именно его судья, и своей переписи.
//
// # Форм вызова ДВЕ, и вторая появилась не для красоты
//
//	make -C <каталог> <цель>     адрес назван явно, рабочий каталог не важен
//	make <цель>                  адрес — РАБОЧИЙ КАТАЛОГ шага (`working-directory`)
//
// Вторая форма — та, которой конвейер зовёт каталожного судью, и без неё гейт
// объявил бы провязку отсутствующей при живой провязке. Рабочий каталог берётся
// с учётом умолчаний: `defaults.run.working-directory` файла, то же у джобы,
// `working-directory` шага — по возрастанию частности. Умолчание файла в этом
// дереве не гипотетическое: `.github/workflows/ui.yml` объявляет его.
//
// Оттого же вторая форма СТРОГА к каталогу: `make <цель>` из чужого рабочего
// каталога адресован чужому Makefile и провязкой не является. Это не педантизм —
// это ровно тот способ, каким провязку ломают, не тронув ни одной строки со
// словом `make`: снимают `working-directory`.
//
// # Носителей ДВА, и они не взаимозаменяемы
//
// Конвейер даёт вердикт на MR в ствол; локальный прогонщик — единственное, что
// стоит между правкой и стволом внутри накопительной линии, где вердикта
// конвейера не будет вовсе. Провязка, оставшаяся в одном носителе из двух, — это
// ровно тот случай, ради которого перепись печатает носители порознь.
//
// Требование обоих носителей принадлежит ПРЕДМЕТУ гейта, а не механизму:
// судей services/* зовут оба, каталожного судью — только конвейер, и вменять ему
// отсутствие второго носителя значило бы краснеть на дереве, которое никто не
// ломал.
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
// В безопасную сторону. Раскрытие цикла (`for svc in …`), каталог из переменной
// (`make -C "$ROOT/gateway" …`) и цепочка целей длиннее одного звена здесь не
// поддержаны, и это выбор: гейт скажет «объявляет, но никто не зовёт», то есть
// покраснеет, вместо того чтобы молча зачесть непонятую строку за провязку.
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

// ─────────────────────────────────────────────────────────────────────────────
// ЧТЕНИЕ КОНВЕЙЕРА — один читатель на всех потребителей
// ─────────────────────────────────────────────────────────────────────────────

// workflowRunStep — один шаг конвейера с ИСПОЛНЯЕМЫМ телом.
//
// WorkDir — ЭФФЕКТИВНЫЙ рабочий каталог относительно корня дерева; пустая строка
// означает корень. Он здесь не справочная подробность: во второй форме вызова
// (`make <цель>` без `-C`) именно он и есть адрес вызова.
type workflowRunStep struct {
	File    string
	WorkDir string
	Run     string
}

// workflowRunDirDefaults — умолчание рабочего каталога, объявляемое файлом и джобой.
type workflowRunDirDefaults struct {
	Run struct {
		WorkingDirectory string `yaml:"working-directory"`
	} `yaml:"run"`
}

// workflowRunDoc — минимальная форма файла конвейера: исполняемое тело шага и то,
// ГДЕ оно исполняется. Всё остальное (`name`, `if`, `uses`, `env`) намеренно не
// читается — вызовом оно не является ни в одной форме.
type workflowRunDoc struct {
	Defaults workflowRunDirDefaults `yaml:"defaults"`
	Jobs     map[string]struct {
		Defaults workflowRunDirDefaults `yaml:"defaults"`
		Steps    []struct {
			Run              string `yaml:"run"`
			WorkingDirectory string `yaml:"working-directory"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// normalizeWorkDir приводит каталог к форме «путь от корня дерева через `/`»;
// корень — пустая строка.
//
// Нераскрытая подстановка (`${{ github.workspace }}/…`) остаётся как есть и ни с
// каким каталогом не совпадёт — то есть уводит в безопасную сторону.
func normalizeWorkDir(dir string) string {
	d := strings.TrimSpace(dir)
	d = strings.ReplaceAll(d, `\`, "/")
	d = strings.TrimPrefix(d, "./")
	d = strings.TrimSuffix(d, "/")
	if d == "." {
		return ""
	}
	return d
}

// readWorkflowRunSteps — шаги с телом `run:` из ВСЕХ объявленных процессов, плюс
// имена самих процессов.
//
// Перечень процессов ВЫВОДИТСЯ из каталога, а не выписывается: рукописный
// перечень расходится с деревом — свойство механизма, а не аккуратности автора.
// Именно поэтому имя процесса не является параметром гейта: параметром был бы
// литерал, который завтра назовёт файл, которого нет.
func readWorkflowRunSteps(root string) (steps []workflowRunStep, files []string, err error) {
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, rerr := os.ReadDir(wfDir)
	if rerr != nil {
		return nil, nil, fmt.Errorf("не прочитан %s: %w", wfDir, rerr)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || (!strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml")) {
			continue
		}
		// #nosec G304 -- имя файла пришло из перечня .github/workflows ЭТОГО дерева,
		// корень каталога — константа: подставить посторонний файл извне нечем.
		raw, rerr := os.ReadFile(filepath.Join(wfDir, n))
		if rerr != nil {
			return nil, nil, fmt.Errorf("не прочитан %s: %w", n, rerr)
		}
		var doc workflowRunDoc
		// Файл конвейера, который не разбирается, — это НЕ «вызова нет»: о нём
		// не прочитано ничего, и молчаливый пропуск сделал бы «находок ноль»
		// неотличимым от «прочитано ноль».
		if uerr := yaml.Unmarshal(raw, &doc); uerr != nil {
			return nil, nil, fmt.Errorf("не разобран .github/workflows/%s: %w", n, uerr)
		}
		files = append(files, n)
		fileDefault := normalizeWorkDir(doc.Defaults.Run.WorkingDirectory)
		for _, job := range doc.Jobs {
			jobDefault := fileDefault
			if d := normalizeWorkDir(job.Defaults.Run.WorkingDirectory); d != "" {
				jobDefault = d
			}
			for _, st := range job.Steps {
				if strings.TrimSpace(st.Run) == "" {
					continue
				}
				wd := jobDefault
				if d := normalizeWorkDir(st.WorkingDirectory); d != "" {
					wd = d
				}
				steps = append(steps, workflowRunStep{File: n, WorkDir: wd, Run: st.Run})
			}
		}
	}
	sort.Strings(files)
	return steps, files, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАНИЕ ВЫЗОВА — один распознаватель на обе формы
// ─────────────────────────────────────────────────────────────────────────────

// targetTerminator — то, чем имя цели вправе закончиться.
//
// `\b` здесь НЕ годится: дефис не буквенно-цифровой, поэтому граница слова
// нашлась бы внутри `permission-catalog-check` при поиске `permission-catalog`,
// и вызов одной цели зачёлся бы за вызов другой. Терминатор перечислен явно.
const targetTerminator = `(?:$|[^A-Za-z0-9_-])`

// makeCallMatcher — обе формы вызова одной цели, скомпилированные однажды.
type makeCallMatcher struct {
	target string
	dashC  *regexp.Regexp
	bare   *regexp.Regexp
}

// newMakeCallMatcher — распознаватель вызова цели `target`.
func newMakeCallMatcher(target string) makeCallMatcher {
	q := regexp.QuoteMeta(target)
	return makeCallMatcher{
		target: target,
		// Форма 1: адрес назван явно.
		dashC: regexp.MustCompile(`(?m)\bmake\s+-C\s+["']?([^\s"';|&]+)["']?\s+` + q + targetTerminator),
		// Форма 2: адрес — рабочий каталог. Короткие флаги make между командой
		// и целью пропускаются, кроме `-C`: его разбирает форма 1. Длинные флаги
		// НЕ пропускаются намеренно — среди них есть `--directory`, синоним `-C`,
		// и пропуск длинного флага молча приписал бы такой вызов рабочему
		// каталогу вместо названного. В дереве этой формы нет; появится — гейт
		// покраснеет («никто не зовёт»), а не зачтёт непонятую строку.
		bare: regexp.MustCompile(`(?m)\bmake\s+(?:-[A-BD-Za-z]\s+)*` + q + targetTerminator),
	}
}

// dirsIn — каталоги, которым данное тело адресует вызов цели.
//
// Комментарии отбрасываются здесь, а не у вызывающего: тело `run:` шага — тоже
// shell, и комментарий внутри него вызовом не является.
func (m makeCallMatcher) dirsIn(body, workDir string) []string {
	exec := string(executablePartOf(body))
	seen := map[string]bool{}
	for _, sub := range m.dashC.FindAllStringSubmatch(exec, -1) {
		seen[normalizeWorkDir(sub[1])] = true
	}
	if m.bare.MatchString(exec) {
		seen[normalizeWorkDir(workDir)] = true
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
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

// serviceOfDir — сервис, чьему Makefile адресован вызов.
//
// Ровно два сегмента: `services/vpc/tests/newman` — не Makefile сервиса, и вызов
// оттуда адресован не ему.
func serviceOfDir(dir string) (string, bool) {
	parts := strings.Split(dir, "/")
	if len(parts) != 2 || parts[0] != "services" || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБЪЯВЛЕНИЕ ЦЕЛИ И ДОСТИЖИМОСТЬ ВНУТРИ ОДНОГО Makefile
// ─────────────────────────────────────────────────────────────────────────────

// targetDeclarationRe — ОБЪЯВЛЕНИЕ цели в Makefile: строка правила, а не
// `.PHONY` и не присваивание переменной с таким именем.
func targetDeclarationRe(target string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\s*:(?:[^=]|$)`)
}

// makefileReach — что известно про достижимость цели внутри одного Makefile.
type makefileReach struct {
	// Declared — цель объявлена правилом.
	Declared bool
	// Reaching — цели, ИЗ КОТОРЫХ достижима искомая, включая её саму: вызов
	// любой из них исполняет искомую.
	Reaching []string
	// RecipeLines — объём прочитанного: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	RecipeLines int
}

// makefileTargetsReaching — цели этого Makefile, из которых достижима `target`.
//
// Достижимость засчитывается в ДВУХ формах, и обе исполняемы: зависимость в
// заголовке правила и вызов `$(MAKE) <цель>` в рецепте. Имя цели, стоящее в
// комментарии, достижимостью не является — заголовок правила и тело рецепта
// читаются порознь именно поэтому.
//
// Звено ОДНО: транзитивная цепочка (A зовёт B, B зовёт искомую) не раскрывается.
// Это тот же выбор, что и у нераспознанной формы вызова: гейт скорее покраснеет
// на живой провязке через два звена, чем молча зачтёт непонятую цепочку. Второе
// звено, если оно понадобится, заводится вместе со своей инъекцией.
func makefileTargetsReaching(path, target string) (makefileReach, error) {
	var out makefileReach
	// #nosec G304 -- путь склеен вызывающим из корня дерева и КОНСТАНТНОГО хвоста.
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	recipes, perr := parseMakefileRecipes(path)
	if perr != nil {
		return out, fmt.Errorf("рецепты %s не разобраны: %w", path, perr)
	}
	out.RecipeLines = len(recipes.Lines)

	reaching := map[string]bool{}
	for _, rl := range recipes.Lines {
		if rl.Target == target {
			out.Declared = true
			reaching[target] = true
		}
		// `$(MAKE) <цель>` в рецепте: исполняется, значит достигает.
		if rl.Target != target && strings.Contains(rl.Text, target) &&
			newMakeCallMatcher(target).bare.MatchString(strings.ReplaceAll(rl.Text, "$(MAKE)", "make")) {
			reaching[rl.Target] = true
		}
	}
	// Заголовки правил: цель в списке зависимостей — тоже достижимость, и она
	// не видна из рецептов вовсе.
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		head := ruleHeadRe.FindStringSubmatch(line)
		if head == nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(head[1]))
		if len(fields) == 0 {
			continue
		}
		owner := fields[0]
		// Специальные цели make (`.PHONY`, `.SUFFIXES`, `.DEFAULT_GOAL`) и
		// шаблонные правила достижимостью НЕ являются: `.PHONY: <цель>` объявляет
		// цель фиктивной, а не зовёт её, и позвать `.PHONY` нельзя. Без этого
		// отсечения перепись называла бы `.PHONY` среди достигающих — то есть
		// утверждала бы о дереве неправду, а гейт зеленел бы на шаге,
		// «зовущем» специальную цель.
		if strings.HasPrefix(owner, ".") || strings.Contains(owner, "%") {
			continue
		}
		if owner == target {
			out.Declared = true
			reaching[target] = true
		}
		_, deps, _ := strings.Cut(line, ":")
		for _, d := range strings.Fields(deps) {
			if d == target && owner != target {
				reaching[owner] = true
			}
		}
	}
	if !out.Declared {
		// Цели нет — достигать нечего, и перечень достигающих обязан быть пуст:
		// иначе гейт объявил бы провязанной цель, которой не существует.
		return out, nil
	}
	for t := range reaching {
		out.Reaching = append(out.Reaching, t)
	}
	sort.Strings(out.Reaching)
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЯЗКА ЦЕЛИ ОДНОГО КАТАЛОГА (gateway/Makefile и подобные)
// ─────────────────────────────────────────────────────────────────────────────

// makeTargetWiring — провязка цели, объявленной в ОДНОМ известном каталоге.
type makeTargetWiring struct {
	// Dir — каталог Makefile относительно корня дерева.
	Dir string
	// Target — цель, о которой всё нижеследующее.
	Target string
	// Reach — объявление и достижимость внутри самого Makefile.
	Reach makefileReach
	// CalledBy — достигающая цель → процессы конвейера, зовущие её в Dir.
	CalledBy map[string][]string
	// Miscalled — процесс зовёт достигающую цель в ЧУЖОМ каталоге, где её не
	// объявлено. Обратное направление сверки.
	Miscalled map[string][]string
	// Объёмы осмотренного, каждый своим числом.
	WorkflowFiles                    []string
	WorkflowsRead, WorkflowStepsRead int
}

// readMakeTargetWiring — состав провязки цели `target`, объявленной в `dir`.
//
// Возвращает ошибку вместо падения, чтобы этой же функцией пользовалась
// инъекция: она обязана наблюдать исход, а не завершать прогон.
func readMakeTargetWiring(root, dir, target string) (makeTargetWiring, error) {
	w := makeTargetWiring{
		Dir:       normalizeWorkDir(dir),
		Target:    target,
		CalledBy:  map[string][]string{},
		Miscalled: map[string][]string{},
	}
	reach, err := makefileTargetsReaching(filepath.Join(root, filepath.FromSlash(dir), "Makefile"), target)
	if err != nil {
		return w, err
	}
	w.Reach = reach

	steps, files, serr := readWorkflowRunSteps(root)
	if serr != nil {
		return w, serr
	}
	w.WorkflowFiles = files
	w.WorkflowsRead = len(files)
	w.WorkflowStepsRead = len(steps)

	for _, entry := range reach.Reaching {
		m := newMakeCallMatcher(entry)
		for _, st := range steps {
			for _, called := range m.dirsIn(st.Run, st.WorkDir) {
				switch {
				case called == w.Dir:
					w.CalledBy[entry] = append(w.CalledBy[entry], st.File)
				case makefileDeclaresTarget(root, called, entry):
					// Чужой каталог, где такая цель ЕСТЬ: это вызов чужого
					// одноимённого судьи, а не промах по нашему.
				default:
					w.Miscalled[entry] = append(w.Miscalled[entry], st.File+" → "+dirLabel(called))
				}
			}
		}
	}
	for k := range w.CalledBy {
		sort.Strings(w.CalledBy[k])
		w.CalledBy[k] = uniqueStrings(w.CalledBy[k])
	}
	for k := range w.Miscalled {
		sort.Strings(w.Miscalled[k])
		w.Miscalled[k] = uniqueStrings(w.Miscalled[k])
	}
	return w, nil
}

// makefileDeclaresTarget — объявляет ли Makefile каталога `dir` цель `target`.
func makefileDeclaresTarget(root, dir, target string) bool {
	// #nosec G304 -- каталог пришёл из тела шага ЭТОГО дерева, хвост пути — константа.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), "Makefile"))
	if err != nil {
		return false
	}
	return targetDeclarationRe(target).Match(executablePartOf(string(raw)))
}

// dirLabel — читаемое имя каталога для находки: корень дерева именуется словом,
// а не пустой строкой, иначе находка называет пустоту.
func dirLabel(dir string) string {
	if dir == "" {
		return "корень дерева"
	}
	return dir
}

// findMakeTargetWiringFaults — расхождения провязки, в ОБЕ стороны.
//
// Находка называет ПРЕДМЕТ — процесс и цель, — а не «связь не найдена»: находка,
// называющая симптом, посылает читателя искать не там, и на неё тратят прогон,
// прежде чем снять гейт как непонятный.
func findMakeTargetWiringFaults(w makeTargetWiring) []string {
	var out []string
	callers := 0
	for _, files := range w.CalledBy {
		callers += len(files)
	}
	if w.Reach.Declared && callers == 0 {
		out = append(out, fmt.Sprintf(
			"цель %s объявлена в %s/Makefile, но её не зовёт ни один шаг ни одного процесса из "+
				"прочитанных (процессов %d: %s) — судья исполним и не исполняется. Целей, достигающих "+
				"её, %d (%s), и не позвана ни одна: значит проверка не встречает НИ ОДНОГО "+
				"автоматического прогона, а молчание неотличимо от исправной работы. Чинится "+
				"ПРОВЯЗКОЙ — шагом процесса с `working-directory: %s` и телом `make %s` либо "+
				"`make -C %s %s`, — а не вторым судьёй",
			w.Target, w.Dir, w.WorkflowsRead, strings.Join(w.WorkflowFiles, ", "),
			len(w.Reach.Reaching), strings.Join(w.Reach.Reaching, ", "),
			w.Dir, w.Target, w.Dir, w.Target))
	}
	for entry, where := range w.Miscalled {
		out = append(out, fmt.Sprintf(
			"процесс зовёт цель %s не в том каталоге (%s), а объявлена она в %s/Makefile — "+
				"шаг не исполнит судью и упадёт ни на чём либо позеленеет ни на чём. Провязка ломается "+
				"снятием `working-directory`, не тронув ни одной строки со словом make",
			entry, strings.Join(where, ", "), w.Dir))
	}
	sort.Strings(out)
	return out
}

// makeTargetWiringCensus — перепись объёма осмотренного, одной строкой.
//
// Печатается ВСЕГДА и по каждой оси порознь: одно число скрыло бы ровно тот
// случай, ради которого гейт заведён.
func makeTargetWiringCensus(w makeTargetWiring) string {
	calls := make([]string, 0, len(w.CalledBy))
	total := 0
	for entry, files := range w.CalledBy {
		calls = append(calls, entry+"←"+strings.Join(files, "+"))
		total += len(files)
	}
	sort.Strings(calls)
	return fmt.Sprintf("перепись цели %s (%s/Makefile): строк рецепта прочитано %d · объявлена %v · "+
		"достигают её %d (%s) · процессов прочитано %d (%s) · шагов с телом run осмотрено %d · "+
		"вызовов найдено %d (%s)",
		w.Target, w.Dir, w.Reach.RecipeLines, w.Reach.Declared,
		len(w.Reach.Reaching), strings.Join(w.Reach.Reaching, ", "),
		w.WorkflowsRead, strings.Join(w.WorkflowFiles, ", "), w.WorkflowStepsRead,
		total, strings.Join(calls, ", "))
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЯЗКА ЦЕЛИ, ОБЪЯВЛЯЕМОЙ КАЖДЫМ СЕРВИСОМ (services/*/Makefile)
// ─────────────────────────────────────────────────────────────────────────────

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
	m := newMakeCallMatcher(target)

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

	steps, files, serr := readWorkflowRunSteps(root)
	if serr != nil {
		return w, serr
	}
	w.WorkflowsRead = len(files)
	w.WorkflowStepsRead = len(steps)
	for _, st := range steps {
		for _, dir := range m.dirsIn(st.Run, st.WorkDir) {
			if svc, ok := serviceOfDir(dir); ok {
				w.CalledByWorkflow[svc] = append(w.CalledByWorkflow[svc], st.File)
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
	// Рабочий каталог прогонщика — корень дерева: `cd` внутри него здесь не
	// раскрывается, и голый `make <цель>` адресуется корню, а не сервису. Это
	// та же безопасная сторона: провязка через `cd` будет объявлена
	// отсутствующей, а не зачтена непонятой.
	for _, dir := range m.dirsIn(string(raw), "") {
		if svc, ok := serviceOfDir(dir); ok {
			w.CalledByLocalRunner[svc] = true
		}
	}

	return w, nil
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
