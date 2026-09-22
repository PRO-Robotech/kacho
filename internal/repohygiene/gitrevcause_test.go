// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// gitrevcause_test.go — держатель двух свойств дома причин (gitrevcause.go):
// ПРИЧИНА ОСТАЁТСЯ ПРЕДЪЯВИМОЙ сквозь обёртку, и СЛОВО РЕМОНТА берётся из
// общего источника, а не переписывается рядом.

// gitRevToolAbsentChildEnv — маркер подпроцесса и одновременно путь дерева,
// которое подпроцесс спросит. Пустое значение означает «я не ребёнок».
const gitRevToolAbsentChildEnv = "KACHO_GITREV_TOOL_ABSENT_TREE"

// gitRevToolAbsentChildMark — префикс строки, которой ребёнок отчитывается.
const gitRevToolAbsentChildMark = "KACHO_GITREV_CAUSE="

// TestGitRevCauseToolAbsentChild — тело подпроцесса. Вне подпроцесса не делает
// ничего: это НЕ пропуск, а вторая половина одной пробы, и своего предмета у
// неё в родительском процессе нет.
//
// Подпроцесс нужен ровно потому, что «инструмента нет вовсе» — состояние
// ПРОЦЕССА, а не дерева: `exec.Command` ищет `git` в PATH того, кто его зовёт.
// Снять PATH в общем процессе нельзя — пробы этого пакета идут параллельно, и
// снятие уронило бы соседей на чужой причине.
func TestGitRevCauseToolAbsentChild(t *testing.T) {
	t.Parallel()
	dir := os.Getenv(gitRevToolAbsentChildEnv)
	if dir == "" {
		return
	}
	err := foreignIDPRevisionResolverAt(dir)("0123456789a")
	fmt.Printf("%ssentinel=%v notfound=%v exit=%v text=%q\n",
		gitRevToolAbsentChildMark,
		errors.Is(err, errForeignIDPTreeNotAsked),
		errors.Is(err, exec.ErrNotFound),
		errors.As(err, new(*exec.ExitError)),
		fmt.Sprint(err))
}

// gitRevScratchRepo — СВОЁ дерево под пробу: один пустой коммит.
//
// Вход отрицательной ветви создаётся, а не наследуется: `t.TempDir()` ложится
// туда, куда укажет TMPDIR, и система контроля версий поднимается по родителям —
// внутри нашего же дерева «не репозиторий» оказался бы репозиторием.
func gitRevScratchRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("каталог дерева пробы: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v в дереве пробы: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", ".")
	run("-c", "user.email=probe@example.invalid", "-c", "user.name=probe",
		"commit", "-q", "--allow-empty", "-m", "only")
	return dir
}

// TestGitRevTreeNotAskedKeepsItsCauseInspectable — ПРИЧИНА, СПРЯТАННАЯ В ТЕКСТ,
// ПЕРЕСТАЁТ БЫТЬ ПРИЧИНОЙ.
//
// Внутри «дерево не спрошено» врезка разрешителя разводит ТРИ подслучая с
// разными ремонтами — каталога нет · он не репозиторий · инструмента нет вовсе.
// Разводит их СЛОВАМИ; сигналом их разводит только то, что исходная ошибка
// доехала до вызывающего обёрнутой. Отдать `%w` сентинелу и оставить причине
// `%v` значит объявить классификацию, которой код больше не даёт: три подслучая
// становятся одной строкой без единого предъявимого признака.
//
// Пара на каждый подслучай одно-фактна: дерево пробы одно и то же, меняется
// ровно одно обстоятельство — путь · наличие репозитория по пути · наличие
// инструмента у процесса.
func TestGitRevTreeNotAskedKeepsItsCauseInspectable(t *testing.T) {
	t.Parallel()
	repo := gitRevScratchRepo(t)

	// Законный близнец ВСЕЙ группы: дерево на месте, инструмент на месте,
	// ревизия своя — отказа нет вовсе. Без него красное ниже достигалось бы
	// отказом на чём угодно.
	head, err := gitenv.Command(repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("голова дерева пробы не снята: %v — близнец взять неоткуда", err)
	}
	if err := foreignIDPRevisionResolverAt(repo)(strings.TrimSpace(string(head))); err != nil {
		t.Fatalf("своя ревизия своего дерева не разрешилась: %v — близнец не зелёный", err)
	}

	// Подслучай 1 — КАТАЛОГА НЕТ. Смена рабочего каталога не удаётся ещё до
	// запуска инструмента, и причина приходит ошибкой файловой системы.
	t.Run("каталога нет", func(t *testing.T) {
		t.Parallel()
		err := foreignIDPRevisionResolverAt(filepath.Join(t.TempDir(), "нет-такого"))("0123456789a")
		if err == nil {
			t.Fatal("несуществующий каталог разрешил ревизию — fail-open")
		}
		t.Logf("текст отказа: %v", err)
		if !errors.Is(err, errForeignIDPTreeNotAsked) {
			t.Errorf("подслучай отнесён не к своей причине: %v", err)
		}
		var pathErr *fs.PathError
		if !errors.As(err, &pathErr) {
			t.Errorf("причина не предъявима: errors.As(*fs.PathError) ложно на %v — "+
				"«каталога нет» неотличимо от «он не репозиторий»", err)
		} else if pathErr.Op != "chdir" {
			t.Errorf("предъявлена не та ошибка файловой системы: op=%q", pathErr.Op)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("причина не предъявима: errors.Is(fs.ErrNotExist) ложно на %v", err)
		}
	})

	// Подслучай 2 — КАТАЛОГ ЕСТЬ, НО ОН НЕ РЕПОЗИТОРИЙ. Инструмент запускается и
	// отказывает сам, кодом 128.
	t.Run("не репозиторий", func(t *testing.T) {
		t.Parallel()
		notrepo := filepath.Join(t.TempDir(), "плоский-каталог")
		if err := os.MkdirAll(notrepo, 0o750); err != nil {
			t.Fatalf("каталог: %v", err)
		}
		err := foreignIDPRevisionResolverAt(notrepo)("0123456789a")
		if err == nil {
			t.Fatal("не-репозиторий разрешил ревизию — fail-open")
		}
		t.Logf("текст отказа: %v", err)
		if !errors.Is(err, errForeignIDPTreeNotAsked) {
			t.Errorf("подслучай отнесён не к своей причине: %v", err)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("причина не предъявима: errors.As(*exec.ExitError) ложно на %v — "+
				"«он не репозиторий» неотличимо от «каталога нет»", err)
		} else if exitErr.ExitCode() != 128 {
			t.Errorf("предъявлен код %d, а не 128", exitErr.ExitCode())
		}
		// Отделение от СОСЕДНЕЙ группы: код 1 — это «объекта нет», и он обязан
		// разбираться другой ветвью, а не этой.
		var fsErr *fs.PathError
		if errors.As(err, &fsErr) {
			t.Errorf("не-репозиторий предъявил ошибку файловой системы: %v", err)
		}
	})

	// Подслучай 3 — ИНСТРУМЕНТА НЕТ ВОВСЕ. Состояние процесса, не дерева:
	// создаётся подпроцессом с пустым PATH (см. [TestGitRevCauseToolAbsentChild]).
	t.Run("инструмента нет вовсе", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command(os.Args[0], "-test.run=^TestGitRevCauseToolAbsentChild$", "-test.v")
		env := []string{gitRevToolAbsentChildEnv + "=" + repo, "PATH="}
		for _, kv := range os.Environ() {
			if name, _, ok := strings.Cut(kv, "="); ok && (name == "PATH" || name == gitRevToolAbsentChildEnv) {
				continue
			}
			env = append(env, kv)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("подпроцесс не отработал: %v\n%s — условие пробы не создано, и её "+
				"молчание ничего не значило бы", err, out)
		}
		var line string
		for _, l := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), gitRevToolAbsentChildMark); ok {
				line = v
				break
			}
		}
		if line == "" {
			t.Fatalf("подпроцесс не отчитался; вывод:\n%s", out)
		}
		t.Logf("подпроцесс: %s", line)
		if !strings.Contains(line, "sentinel=true") {
			t.Errorf("отсутствие инструмента отнесено не к «дерево не спрошено»: %s", line)
		}
		if !strings.Contains(line, "notfound=true") {
			t.Errorf("причина не предъявима: errors.Is(exec.ErrNotFound) ложно — "+
				"«инструмента нет вовсе» неотличимо от «он не репозиторий»: %s", line)
		}
		// Отделение от подслучая 2: отсутствие инструмента кодом возврата не
		// сопровождается, инструмент не запускался.
		if !strings.Contains(line, "exit=false") {
			t.Errorf("отсутствие инструмента предъявило код возврата — значит "+
				"инструмент запускался, и предпосылка подслучая не выполнена: %s", line)
		}
	})
}

// TestGitRevRemedyVocabularyHasASingleSource — СЛОВО РЕМОНТА ОБЪЯВЛЕНО ОДИН РАЗ,
// И ОБЕ СТОРОНЫ БЕРУТ ЕГО ОТТУДА.
//
// Проверка ремонта подстрокой по ОДНОМУ слову ошибается в обе стороны:
// переименуешь слово — краснеет зря, хотя смысл цел; сохранишь слово и
// перевернёшь предложение — промолчит, хотя ремонта в отказе больше нет. Общий
// источник снимает первое (обе стороны двигаются вместе), целая клауза вместо
// слова — второе.
//
// Пара здесь на сам распознаватель, и он тот же, которым судятся настоящие
// отказы: инъекция, зовущая свою копию, доказывала бы свойство копии.
func TestGitRevRemedyVocabularyHasASingleSource(t *testing.T) {
	t.Parallel()

	// Законный близнец: текст собран ИЗ общего источника, проза вокруг него
	// своя и другая — распознаватель молчит.
	lawful := fmt.Errorf("%w: сначала %s, и только потом всё остальное",
		errForeignIDPRevisionUndelivered, gitRevRemedyCloneDepth)
	if !gitRevRemedyNamed(lawful, gitRevRemedyCloneDepth) {
		t.Errorf("отказ, собранный из общего источника, не признан называющим ремонт: %v", lawful)
	}

	// Дефект: СЛОВО сохранено, предложение переписано мимо источника —
	// распознаватель обязан покраснеть. Ровно этот вход проходил у проверки
	// подстрокой по слову.
	reworded := fmt.Errorf("%w: начните с клона побольше ГЛУБИНЫ, там разберётесь",
		errForeignIDPRevisionUndelivered)
	if !strings.Contains(reworded.Error(), "ГЛУБИНЫ") {
		t.Fatal("инъекция не воспроизводит свой предмет: слова в тексте нет, " +
			"и её молчание ничего не значило бы")
	}
	if gitRevRemedyNamed(reworded, gitRevRemedyCloneDepth) {
		t.Errorf("своя проза, сохранившая одно слово, прошла за общий источник: %v", reworded)
	}

	// Настоящие отказы разрешителя называют КАЖДЫЙ СВОЙ ремонт — и тем же
	// источником. Без этой половины пара доказывала бы свойство синтетики.
	origin, older, newer := foreignIDPScratchRepo(t)
	shallow := filepath.Join(t.TempDir(), "shallow")
	if out, err := gitenv.Command(filepath.Dir(shallow), "clone", "-q", "--depth=1",
		"file://"+origin, shallow).CombinedOutput(); err != nil {
		t.Fatalf("мелкий клон не создан (%v): %s — условие пробы не создано", err, out)
	}
	_ = newer

	absent := foreignIDPRevisionResolverAt(origin)("0123456789a")
	undelivered := foreignIDPRevisionResolverAt(shallow)(older)
	notasked := foreignIDPRevisionResolverAt(filepath.Join(t.TempDir(), "нет-такого"))(older)

	for _, c := range []struct {
		name   string
		err    error
		want   string
		absent []string
	}{
		{"объекта нет", absent, gitRevRemedyLedger, []string{gitRevRemedyCloneDepth, gitRevRemedyWorkingDir}},
		{"не довезён", undelivered, gitRevRemedyCloneDepth, []string{gitRevRemedyLedger, gitRevRemedyWorkingDir}},
		{"дерево не спрошено", notasked, gitRevRemedyWorkingDir, []string{gitRevRemedyLedger, gitRevRemedyCloneDepth}},
	} {
		if c.err == nil {
			t.Errorf("%s: отказа нет вовсе — предпосылка не выполнена", c.name)
			continue
		}
		t.Logf("%s: %v", c.name, c.err)
		if !gitRevRemedyNamed(c.err, c.want) {
			t.Errorf("%s: отказ не называет своего ремонта %q — читающий пойдёт чинить "+
				"не то: %v", c.name, c.want, c.err)
		}
		for _, other := range c.absent {
			if gitRevRemedyNamed(c.err, other) {
				t.Errorf("%s: отказ называет ЧУЖОЙ ремонт %q — две причины сошлись в "+
					"один совет: %v", c.name, other, c.err)
			}
		}
	}
}
