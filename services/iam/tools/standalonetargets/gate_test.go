// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package standalonetargets_test

// gate_test.go — гейт доказывается инъекцией в ОБЕ стороны и объявляет объём
// осмотренного.
//
// Каждая инъекция меняет РОВНО ОДИН факт против своего положительного близнеца:
// иначе неизвестно, какой из двух дал красное, и вердикт недействителен, хотя
// выглядит как обычный зелёный.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"

	"github.com/PRO-Robotech/kaname/tools/standalonetargets"
)

// waivers — цели, которых гейт не зовёт, и НАЗВАННАЯ причина у каждой.
//
// Предмет у всех один по форме: ресурс, которого у пробы нет BY CONSTRUCTION.
// Это не отсрочка — такая цель не станет проверяемой оттого, что кто-то ею
// займётся: её предпосылку создаёт стенд, а не код. Запись, которой больше
// нечего покрывать, — находка (сверка ведомости в обе стороны ниже).
var waivers = []standalonetargets.Waiver{
	{Target: "lint", Reason: "зовёт golangci-lint — его на машине пробы может не быть вовсе, и это «не выполнилось», а не находка"},
	{Target: "docker", Reason: "зовёт демон сборки образов; посадку клона он не проверяет — образ собирается от того же дерева"},
	{Target: "migrate-up", Reason: "требует живую базу: предпосылку создаёт стенд, а не дерево"},
	{Target: "migrate-down", Reason: "требует живую базу: предпосылку создаёт стенд, а не дерево"},
	{Target: "migrate-status", Reason: "требует живую базу: предпосылку создаёт стенд, а не дерево"},
	{Target: "proto-install-plugins", Reason: "тянет плагины генерации из сети; сеть — предпосылка стенда, а не свойство рецепта"},
	{Target: "operator-docs", Reason: "порождает документацию в дерево клона; предмет сверки — соседняя цель operator-docs-check, которая её и судит"},
}

// TestStandaloneTargetsWorkInAStandaloneClone — боевой прогон: каждая цель,
// которую рецепт НЕ пометил `[монорепо]`, зовётся в самостоятельном клоне.
//
// Клон собирается из состава КОММИТА, а не рабочего каталога: арендатору едет
// то, что отслеживается деревом, и судить надо именно его.
func TestStandaloneTargetsWorkInAStandaloneClone(t *testing.T) {
	if testing.Short() {
		t.Skip("собирает клон и зовёт цели сборки — прогон интеграционного порядка")
	}

	wd, err := os.Getwd()
	require.NoError(t, err)
	moduleRoot, err := standalonetargets.ModuleRootFrom(wd)
	require.NoError(t, err, "проверка НЕ ИСПОЛНЯЛАСЬ: корень модуля не установлен")

	raw, err := os.ReadFile(filepath.Join(moduleRoot, "Makefile"))
	require.NoError(t, err, "проверка НЕ ИСПОЛНЯЛАСЬ: рецепт модуля не прочитан")

	targets := standalonetargets.ParseTargets(raw)
	judged, census := standalonetargets.Judged(targets, waivers)

	clone := buildStandaloneClone(t, moduleRoot)
	census.Posture = clone
	t.Log(census.String())

	// Пустой обход — отказ, а не успех: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	require.NotEmpty(t, targets,
		"в рецепте не прочитано ни одной строки объявления цели — обход беспредметен, вердикта нет")
	require.NotEmpty(t, judged,
		"судить нечего: каждая цель либо помечена %s, либо прощена ведомостью — "+
			"перечень перестал обещать арендатору что-либо, и это решение, а не зелёное",
		"[монорепо]")

	// Ведомость сверяется в обе стороны: запись без предмета переживает свою цель
	// и достанется следующей, случайно совпавшей по имени.
	require.Empty(t, census.StaleWaiv,
		"записи ведомости, которым больше нечего прощать: %v — послабление живёт, пока у него есть предмет",
		census.StaleWaiv)

	for _, tgt := range judged {
		t.Run(tgt.Name, func(t *testing.T) {
			rc, out := runMake(t, clone, tgt.Name)
			require.Zerof(t, rc,
				"цель %q объявлена рабочей вне монорепо (пометки %s в её строке нет), а в самостоятельном клоне отказала кодом %d\n"+
					"  объявлено:  ## %s — %s\n"+
					"  посадка:    %s\n"+
					"  хвост вывода:\n%s\n"+
					"  исходов два: либо цель работает у арендатора, либо рецепт помечает её %s и\n"+
					"  отказывает СЛОВАМИ, называя, что делать вместо. Молчаливое красное у всякого,\n"+
					"  кто склонирует, исходом не является",
				tgt.Name, "[монорепо]", rc, tgt.Name, tgt.Desc, clone, tail(out, 25), "[монорепо]")
		})
	}
}

// buildStandaloneClone — самостоятельная посадка модуля во временном каталоге.
//
// Отказ здесь — «проверка НЕ ИСПОЛНЯЛАСЬ», а не находка: гейт, не собравший
// клон, о целях не утверждает ничего и не вправе выглядеть успехом.
func buildStandaloneClone(t *testing.T, moduleRoot string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, standalonetargets.AssertOutsideAnyRepository(dir),
		"проверка НЕ ИСПОЛНЯЛАСЬ: фикстуру негде собрать")

	// Путь модуля ОТНОСИТЕЛЬНО корня дерева: в монорепо это services/iam, у
	// арендатора — точка. Спрашивается у git, а не складывается из `..`: число
	// уровней верно ровно для одной посадки, а гейт обязан работать в обеих.
	top, err := gitenv.Command(moduleRoot, "rev-parse", "--show-toplevel").Output()
	require.NoError(t, err, "проверка НЕ ИСПОЛНЯЛАСЬ: дерево модуля не установлено")
	treeRoot := strings.TrimSpace(string(top))
	rel, err := filepath.Rel(treeRoot, moduleRoot)
	require.NoError(t, err, "проверка НЕ ИСПОЛНЯЛАСЬ: модуль не сводится под корень дерева")

	treeish := "HEAD"
	if rel != "." {
		treeish = "HEAD:" + filepath.ToSlash(rel)
	}

	tar := exec.Command("tar", "-x", "-C", dir)
	ar := gitenv.Command(moduleRoot, "archive", "--format=tar", treeish)
	pipe, err := ar.StdoutPipe()
	require.NoError(t, err)
	tar.Stdin = pipe
	require.NoError(t, tar.Start())
	require.NoError(t, ar.Run(), "проверка НЕ ИСПОЛНЯЛАСЬ: состав коммита модуля не выгружен")
	require.NoError(t, tar.Wait(), "проверка НЕ ИСПОЛНЯЛАСЬ: состав коммита модуля не распакован")

	// Клон обязан быть репозиторием: цели спрашивают у git и корень дерева, и
	// состав коммита. Распакованный архив дал бы им «проверка не исполнялась» —
	// то есть гейт мерил бы отсутствие git, а не посадку.
	for _, args := range [][]string{
		{"init", "-q", "."},
		{"-c", "user.email=probe@example.invalid", "-c", "user.name=probe", "add", "-A"},
		{"-c", "user.email=probe@example.invalid", "-c", "user.name=probe", "commit", "-q", "-m", "самостоятельная посадка модуля"},
	} {
		out, err := gitenv.Command(dir, args...).CombinedOutput()
		require.NoErrorf(t, err, "проверка НЕ ИСПОЛНЯЛАСЬ: git %v в фикстуре: %s", args, out)
	}

	n, err := gitenv.Command(dir, "ls-files").Output()
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(n)),
		"проверка НЕ ИСПОЛНЯЛАСЬ: в собранном клоне ноль отслеживаемых файлов")

	return dir
}

func runMake(t *testing.T, dir, target string) (int, string) {
	t.Helper()
	cmd := exec.Command("make", target)
	cmd.Dir = dir
	cmd.Env = append(gitenv.Env(), "GOFLAGS=-mod=mod")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	t.Logf("цель %s: %s, вывода %d байт", target, time.Since(start).Round(time.Millisecond), len(out))
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if ok := asExit(err, &ee); ok {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: make %s не запустился: %v", target, err)
	return -1, ""
}

func asExit(err error, dst **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*dst = ee
		return true
	}
	return false
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "    " + strings.Join(lines, "\n    ")
}
