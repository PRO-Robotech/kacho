// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pythonprobes держит половину вопроса «настоящая ли эта проба?», на которую
// сама проба ответить не может: зовёт ли её кто-нибудь.
//
// ПРЕДМЕТ. `services/*/tests/newman/scripts/*_test.py` — регрессионные пробы вокруг
// оснастки сюит (генератор коллекций, гейт покрытия, гейт ИСПОЛНЕННОСТИ прогона).
// Их не запускал НИКТО: ни workflow, ни Makefile, ни другой гейт. Слово `pytest`
// встречалось во всём дереве ОДИН раз — в `.dockerignore`, где закрыт кэш его
// прогонов; то есть их гоняли руками, и след остался только от кэша.
//
// Среди них — доказательство гейта, который в своё время нашёл, что прогон послал
// не все запросы, а сюита отчиталась зелёной. Проверка, ловящая ложное зелёное,
// сама была ложно-зелёной: её вердикт не доходил ни до чьего кода выхода.
//
// ПОЧЕМУ СВОЙСТВО, А НЕ ОДНА ПРАВКА. Шаг конвейера можно снять, и заметить это
// будет снова некому: непрогоняемая проба ничем не отличается от прогоняемой, пока
// не спросишь конвейер. Поэтому проводка проверяется ЧТЕНИЕМ workflow, а не
// доверием к комментарию, который утверждает, что CI это гоняет.
//
// Тот же приём и по той же причине — `tools/newmancensus/ci_wiring_test.go`
// (сверщики переписи кейсов). Разница в предмете: там сверщик на сюиту, здесь
// набор проб на сюиту.
package pythonprobes

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// runner — прогонщик проб. Он же обязан быть тем, кого зовёт конвейер.
const runner = ".github/scripts/run-python-probes.py"

// probeSuffix — признак файла проб. Совпадает с хвостом образца в самом прогонщике;
// сверяется на них обоих (TestRunnerPatternCoversEveryTrackedProbeFile).
const probeSuffix = "_test.py"

// probeDirGlob — где живут пробы сюит. Каталог, а не имя файла: `scripts/` заведён
// под оснастку сюиты, и другого назначения у файла проб здесь нет.
const probeDirGlob = "services/*/tests/newman/scripts"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for range 12 {
		if fi, err := os.Stat(filepath.Join(dir, ".github", "workflows")); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("не найден .github/workflows выше этого файла")
	return ""
}

type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// runnerSteps — шаги всех workflow, которые зовут прогонщика.
func runnerSteps(t *testing.T, root string) []string {
	t.Helper()
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("чтение %s: %v", wfDir, err)
	}
	var out []string
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		files++
		raw, rerr := os.ReadFile(filepath.Join(wfDir, name))
		if rerr != nil {
			t.Fatalf("чтение %s: %v", name, rerr)
		}
		var wf workflowFile
		if uerr := yaml.Unmarshal(raw, &wf); uerr != nil {
			t.Fatalf("разбор %s: %v", name, uerr)
		}
		for _, job := range wf.Jobs {
			for _, step := range job.Steps {
				if strings.Contains(step.Run, filepath.Base(runner)) {
					out = append(out, step.Run)
				}
			}
		}
	}
	// Объём осмотренного — ОТДЕЛЬНОЕ утверждение: обход, переставший читать
	// workflow'ы, выходит зелёным на пустом множестве.
	if files == 0 {
		t.Fatal("ни одного файла workflow не прочитано — обход ничего не нашёл, " +
			"значит и утверждать ему нечего")
	}
	t.Logf("прочитано файлов workflow: %d; шагов, зовущих прогонщика: %d", files, len(out))
	return out
}

// findProbeFiles — все файлы проб дерева, обходом НЕЗАВИСИМО от образца прогонщика.
//
// Если проба лежит не там, где её ищет прогонщик, это находка, а не «её нет».
//
// ЕДИНИЦА СЧЁТА — ОТСЛЕЖИВАЕМЫЙ ЭЛЕМЕНТ git, а не файл на диске, и это не
// педантизм. Прогонщик берёт состав через `git ls-files`; обход файловой системы
// здесь означал бы, что гейт и его предмет считают РАЗНЫЕ множества, и они
// разойдутся на первом же черновике рядом с пробами: гейт краснеет, прогонщик
// молчит, вердикты противоречат друг другу. Поймано инъекцией при написании этого
// файла. Отслеживаемое множество — то же, что увидит CI на свежем checkout'е.
func findProbeFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := gitenv.Command(root, "ls-files", "-z", "--", "services/")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files сорвался: %v — предпосылка гейта не выполняется", err)
	}
	var out []string
	for _, rel := range strings.Split(string(raw), "\x00") {
		if rel == "" || !strings.HasSuffix(rel, probeSuffix) {
			continue
		}
		// Только пробы оснастки сюит: `*_test.py` в другом месте — не наш предмет.
		if ok, _ := filepath.Match(probeDirGlob, path.Dir(rel)); ok {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// declaredPattern — образец состава, ОБЪЯВЛЕННЫЙ самим прогонщиком.
//
// Читается из его исходника, а не выписывается здесь второй раз: два места об одном
// предмете расходятся молча, и разойдутся они именно там, где расхождение не видно.
func declaredPattern(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(runner)))
	if err != nil {
		t.Fatalf("прогонщик %s не читается: %v — проводить нечего", runner, err)
	}
	m := regexp.MustCompile(`(?m)^DEFAULT_PATTERN\s*=\s*"([^"]+)"`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("в %s не найдено объявление DEFAULT_PATTERN — прогонщик перестал "+
			"называть свой образец состава, и сверять его с деревом больше нечем", runner)
	}
	return string(m[1])
}

// TestCIRunsThePythonProbeRunner — конвейер зовёт прогонщика, и его отказ доезжает.
func TestCIRunsThePythonProbeRunner(t *testing.T) {
	root := repoRoot(t)
	steps := runnerSteps(t, root)
	if len(steps) == 0 {
		t.Fatalf("ни один шаг конвейера не зовёт %s: 48 регрессионных проб на python "+
			"лежат в дереве и не исполняются. Проба, которой никто не запускал, — "+
			"украшение: она не может ни покраснеть, ни отчитаться.", runner)
	}

	// Доказательство инъекцией обязано ИСПОЛНЯТЬСЯ, а не существовать как проза:
	// прогонщик — тоже проверка, и его способность краснеть проверяется там же,
	// где он работает.
	var withSelfTest bool
	for _, s := range steps {
		if strings.Contains(s, "--self-test") {
			withSelfTest = true
			break
		}
	}
	if !withSelfTest {
		t.Error("шаг зовёт прогонщика, но не зовёт его `--self-test`: доказательство " +
			"того, что прогонщик умеет краснеть, не исполняется — а его зелёный " +
			"обычный проход без этого ничего не значит")
	}

	// Шаг не вправе глотать отказ. `|| true`, потеря кода возврата в конвейере и
	// вердикт с последнего звена — запрещённые способы получить зелёное.
	for _, s := range steps {
		for _, line := range strings.Split(s, "\n") {
			if !strings.Contains(line, filepath.Base(runner)) {
				continue
			}
			if strings.Contains(line, "|| true") || strings.Contains(line, "|| :") {
				t.Errorf("шаг гасит отказ прогонщика: %q", strings.TrimSpace(line))
			}
			if strings.Contains(line, "| tee") && !strings.Contains(s, "pipefail") {
				t.Errorf("вердикт прогонщика теряется в конвейере без pipefail: %q",
					strings.TrimSpace(line))
			}
		}
	}
}

// TestRunnerPatternCoversEveryTrackedProbeFile — образец прогонщика видит КАЖДУЮ
// пробу дерева.
//
// Иначе «обход» — форма без содержания: он существует, а кого-то не видит, и
// невидимая проба выглядит исполненной.
func TestRunnerPatternCoversEveryTrackedProbeFile(t *testing.T) {
	root := repoRoot(t)
	pattern := declaredPattern(t, root)
	all := findProbeFiles(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение. Обход, переставший доходить до проб
	// (каталог переименовали, суффикс сменили), выходит ЗЕЛЁНЫМ на пустом
	// множестве — ровно тот класс, который здесь искореняют.
	if len(all) == 0 {
		t.Fatalf("в дереве не найдено ни одной пробы %q в %q — обход сломан, "+
			"а не дерево чисто", probeSuffix, probeDirGlob)
	}
	t.Logf("проб-файлов в дереве: %d; образец прогонщика: %q", len(all), pattern)

	for _, rel := range all {
		ok, err := filepath.Match(pattern, rel)
		if err != nil {
			t.Fatalf("образец %q не разбирается: %v", pattern, err)
		}
		if !ok {
			t.Errorf("проба %s лежит вне образца %q — прогонщик её не увидит, "+
				"а сюита будет выглядеть проверенной", rel, pattern)
		}
	}
}

// TestEveryProbeFileHasAnExecutableShape — файл проб обязан быть ИСПОЛНИМ.
//
// Прогонщик исполняет два вида: набор pytest (функции `test_*` верхнего уровня) и
// гейт со своим `main`. Файл без того и без другого соберёт ноль проб и промолчит —
// и промолчит он именно так, как выглядит успех. Здесь это проверяется независимо
// от прогонщика, чтобы «вид не опознан» не зависело от того, дошёл ли до файла он.
func TestEveryProbeFileHasAnExecutableShape(t *testing.T) {
	root := repoRoot(t)
	all := findProbeFiles(t, root)
	if len(all) == 0 {
		t.Fatal("в дереве не найдено ни одной пробы — тест ничего не утверждает")
	}

	// Признак — объявление в начале строки либо ветка `__main__`. Оба читаются как
	// код: строка `def test_...` внутри объяснения стоит с отступом или в кавычках.
	reProbe := regexp.MustCompile(`(?m)^(async def|def) test_`)
	reMain := regexp.MustCompile(`(?m)^if __name__`)

	shaped := 0
	for _, rel := range all {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		probes := reProbe.FindAllIndex(raw, -1)
		hasMain := reMain.Match(raw)
		switch {
		case len(probes) > 0:
			shaped++
			t.Logf("%s: набор pytest, проб %d", rel, len(probes))
		case hasMain:
			shaped++
			t.Logf("%s: гейт со своим main", rel)
		default:
			t.Errorf("%s: ни функции `test_*` верхнего уровня, ни ветки `__main__` — "+
				"такой файл собрал бы ноль проб и промолчал; это немота, а не чистота", rel)
		}
	}
	if shaped == 0 {
		t.Fatal("ни один файл проб не опознан — предикат вида сломан")
	}
}
