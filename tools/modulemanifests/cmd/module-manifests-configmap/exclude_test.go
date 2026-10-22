// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// exclude_test.go — единый ConfigMap манифестов НЕ несёт модуль, исключённый
// посадкой (`kaname.manifests.unifiedExclude`, kacho#3356, SD-3356-2).
//
// # Предмет
//
// Решение владельца: на установках, где модуль registry поставляет другой чарт
// (registry-platform кладёт свой манифест в ConfigMap с меткой подбора), единый
// ConfigMap этого дерева модуль registry не несёт; по умолчанию — как прежде.
// Иначе каталог доставки получает два манифеста одного модуля, и служба доступа
// отказывает в старте (`ErrModuleCollision`).
//
// # Как судится
//
// Производитель собирается и зовётся как в рецепте подъёма — двоичным с
// цепочкой профилей, а не вызовом функции: судится то, что применяет стенд.
// Отсутствие столкновения судит НАСТОЯЩИЙ судья формы манифестов (kaname
// `tools/modulemanifestcheck`, собирается из пина go.mod), а не копия его
// правила: дерево, собранное из вывода производителя и манифеста второго
// поставщика модуля, судится обходом, у которого правило столкновения то же,
// что у чтения доставки на старте (`declaredModules`).
//
// Второй поставщик представлен КОПИЕЙ манифеста registry этого дерева:
// столкновение решает имя модуля, а копия заведомо годна каноном судьи — значит,
// исход «годно» на дереве с исключением означает именно отсутствие столкновения,
// а не случайную годность чужого текста.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// collisionText — дословный текст причины судьи (kaname
// `manifest.ErrModuleCollision`).
const collisionText = "module is declared by more than one manifest"

// formJudgePackage — судья формы манифестов, приезжающий пином go.mod.
const formJudgePackage = "github.com/PRO-Robotech/kaname/tools/modulemanifestcheck"

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("корень дерева не вычислен: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("корень дерева %s без go.mod — путь к производителю изменился: %v", root, err)
	}
	return root
}

func build(t *testing.T, dir, pkg, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s не собирается: %v\n%s", pkg, err, out)
	}
	return bin
}

// run — двоичный с аргументами: вывод, поток ошибок, код возврата.
func run(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout.String(), stderr.String(), ee.ExitCode()
	}
	t.Fatalf("%s не запустился: %v", bin, err)
	return "", "", -1
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// profile — файл профиля с объявлением доставки и, если задано, исключения.
func profile(t *testing.T, exclude string) string {
	t.Helper()
	body := "kaname:\n  manifests:\n    configMapName: probe-module-manifests\n"
	if exclude != "" {
		body += "    unifiedExclude: " + exclude + "\n"
	}
	p := filepath.Join(t.TempDir(), "values.yaml")
	writeFile(t, p, body)
	return p
}

// dataKeys — ключи data напечатанного ConfigMap.
func dataKeys(t *testing.T, out string) map[string]string {
	t.Helper()
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal([]byte(out), &cm); err != nil {
		t.Fatalf("вывод производителя не разобран как ConfigMap: %v\n%s", err, out)
	}
	return cm.Data
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// synthRoot — дерево из двух служб с манифестами и одной без.
func synthRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "services", "alpha", "manifest.yaml"), "module: alpha\n")
	writeFile(t, filepath.Join(root, "services", "registry", "manifest.yaml"), "module: registry\n")
	if err := os.MkdirAll(filepath.Join(root, "services", "bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestUnifiedConfigMapLeavesOutTheExcludedServiceAndOnlyIt — исключение снимает
// из единого ConfigMap ровно названный каталог; без ручки состав прежний.
func TestUnifiedConfigMapLeavesOutTheExcludedServiceAndOnlyIt(t *testing.T) {
	bin := build(t, ".", ".", "producer")
	root := synthRoot(t)

	cases := []struct {
		name     string
		profiles []string
		want     []string
	}{
		// Близнец: ручки нет — состав прежний, registry в едином.
		{"без ручки", []string{profile(t, "")}, []string{"alpha.manifest.yaml", "registry.manifest.yaml"}},
		{"ручка пуста", []string{profile(t, "[]")}, []string{"alpha.manifest.yaml", "registry.manifest.yaml"}},
		{"исключён registry", []string{profile(t, "[registry]")}, []string{"alpha.manifest.yaml"}},
		// Побеждает последнее объявление цепочки — как у helm.
		{"позднее пустое объявление перебивает", []string{profile(t, "[registry]"), profile(t, "[]")},
			[]string{"alpha.manifest.yaml", "registry.manifest.yaml"}},
		{"позднее объявление исключает", []string{profile(t, "[]"), profile(t, "[registry]")},
			[]string{"alpha.manifest.yaml"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, code := run(t, bin, append([]string{"-root", root}, c.profiles...)...)
			if code != 0 {
				t.Fatalf("код %d, ждали 0\n%s", code, errOut)
			}
			got := sortedKeys(dataKeys(t, out))
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("ключи единого ConfigMap %v, ждали %v\n%s", got, c.want, errOut)
			}
			excluded := len(c.want) == 1
			if said := strings.Contains(errOut, "исключено из единого: registry"); said != excluded {
				t.Fatalf("перепись об исключении: есть=%v, ждали=%v — исключение обязано называться переписью\n%s",
					said, excluded, errOut)
			}
		})
	}
}

// TestUnifiedExcludeRefusesWhatItCannotExclude — исключение, которому нечего
// исключать, и исключение всего — отказ, а не молча изменённый состав.
func TestUnifiedExcludeRefusesWhatItCannotExclude(t *testing.T) {
	bin := build(t, ".", ".", "producer")
	root := synthRoot(t)

	cases := []struct {
		name, exclude string
		code          int
		says          string
	}{
		{"каталога нет", "[nope]", 1, "nope"},
		{"каталог без манифеста", "[bare]", 1, "bare"},
		{"пустое имя", `[""]`, 1, "пуст"},
		{"повтор", "[registry, registry]", 1, "повтор"},
		{"не перечень", "registry", 1, "unifiedExclude"},
		{"исключено всё", "[alpha, registry]", 2, "ни одного"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, code := run(t, bin, "-root", root, profile(t, c.exclude))
			if code != c.code || !strings.Contains(errOut, c.says) {
				t.Fatalf("код %d (ждали %d), поток ошибок обязан назвать «%s»:\n%s", code, c.code, c.says, errOut)
			}
			if out != "" {
				t.Fatalf("на отказе производитель напечатал объект — подъём применил бы его:\n%s", out)
			}
		})
	}
}

// TestUnifiedExcludeRemovesTheModuleCollisionWithASecondSupplier — дерево
// этой ветки, единый ConfigMap с исключением и второй поставщик модуля registry:
// судья формы не находит столкновения; без исключения — находит, называя оба
// места.
func TestUnifiedExcludeRemovesTheModuleCollisionWithASecondSupplier(t *testing.T) {
	root := repoRoot(t)
	producer := build(t, ".", ".", "producer")
	judge := build(t, root, formJudgePackage, "modulemanifestcheck")
	secondSupplier, err := os.ReadFile(filepath.Join(root, "services", "registry", "manifest.yaml"))
	if err != nil {
		t.Fatalf("манифест registry дерева не прочитан: %v", err)
	}

	// delivered — каталог, судимый обходом: каждый ключ единого — свой каталог
	// с manifest.yaml, второй поставщик — свой. Обход судьи читает индекс git.
	delivered := func(t *testing.T, exclude string) (string, int) {
		t.Helper()
		out, errOut, code := run(t, producer, "-root", root, profile(t, exclude))
		if code != 0 {
			t.Fatalf("производитель на дереве ветки: код %d\n%s", code, errOut)
		}
		keys := dataKeys(t, out)
		tree := t.TempDir()
		for key, body := range keys {
			writeFile(t, filepath.Join(tree, "unified", strings.TrimSuffix(key, ".manifest.yaml"), "manifest.yaml"), body)
		}
		writeFile(t, filepath.Join(tree, "registry-platform", "manifest.yaml"), string(secondSupplier))
		for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = tree
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, b)
			}
		}
		return tree, len(keys) + 1
	}

	// Близнец: без исключения — столкновение названо.
	tree, want := delivered(t, "")
	out, errOut, code := run(t, judge, "-root="+tree)
	all := out + errOut
	if code == 0 || !strings.Contains(all, collisionText) ||
		!strings.Contains(all, "registry-platform/manifest.yaml") || !strings.Contains(all, "unified/registry/manifest.yaml") {
		t.Fatalf("без исключения судья обязан назвать столкновение модуля registry обоими местами (код %d):\n%s", code, all)
	}
	if n := strings.Count(out, "  прочитан: "); n != want {
		t.Fatalf("судья прочитал %d манифестов, в дереве %d — вердикт о другом наборе:\n%s", n, want, out)
	}

	// С исключением — столкновения нет, и дерево годно целиком.
	tree, want = delivered(t, "[registry]")
	out, errOut, code = run(t, judge, "-root="+tree)
	if strings.Contains(out+errOut, collisionText) || code != 0 {
		t.Fatalf("с исключением судья нашёл столкновение либо иную находку (код %d):\n%s%s", code, out, errOut)
	}
	if n := strings.Count(out, "  прочитан: "); n != want {
		t.Fatalf("судья прочитал %d манифестов, в дереве %d — «годно» о другом наборе:\n%s", n, want, out)
	}
}
