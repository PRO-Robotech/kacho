// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// productimagename_test.go — имя образа части продукта СПРАШИВАЕТСЯ у
// объявленного источника, а не выводится приставкой в каждом рецепте заново.
//
// Осей три, и первая без второй вакуумна:
//
//  1. ОТРИЦАНИЕ: в рецептах стенда нет вывода имени образа приставкой;
//  2. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: рецепт стенда ЧИТАЕТ объявленный источник.
//     Без него отрицание выполняется удалением самого цикла сборки — то есть
//     зеленеет на дереве, где образы не собираются вовсе;
//  3. СОГЛАСИЕ ВТОРОЙ ВЕДОМОСТИ: конвейер сборки образов держит свой перечень
//     имён, и он обязан сходиться с объявленным источником. Ведомость,
//     разошедшаяся с источником, — это ровно та беда, из-за которой заведён
//     весь предмет: на 2026-09 конвейер собирал `kaname`, а рецепт стенда —
//     `kacho-iam`, и правой была ОДНА из двух копий.
//
// Перепись печатается всегда.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

func gateRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("корень дерева не разрешается (%v) — предпосылка проверки исчезла", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("в %s нет go.mod — корень разрешён неверно, вердикт был бы о чужом дереве", root)
	}
	return root
}

// Ось 1 — отрицание.
func TestStandRecipesNeverDeriveAnImageNameByPrefix(t *testing.T) {
	root := gateRepoRoot(t)

	found, scope, err := productImageDerivations(root)
	if err != nil {
		t.Fatalf("обход популяции не состоялся (%v) — это НЕ «ноль находок»", err)
	}
	if scope.FilesRead == 0 {
		t.Fatal("рецептов стенда прочитано ноль — обход пуст, вердикт беспредметен")
	}
	if scope.PrefixWords == 0 {
		t.Fatal("слов с приставкой платформы не встретилось ни одного — " +
			"распознаватель не видит даже законных вхождений, значит молчал бы и на находке")
	}
	for _, f := range found {
		t.Errorf("%s", f)
	}
	t.Logf("перепись: рецептов %d, строк %d, слов с приставкой %d, находок %d",
		scope.FilesRead, scope.LinesRead, scope.PrefixWords, len(found))
}

// Ось 2 — КАЖДАЯ ссылка на образ в целях сборки приходит от читателя имён.
//
// Это и положительный контроль (без него отрицание оси 1 выполняется удалением
// самого цикла сборки), и держатель самого инварианта полосы: «стенд собирает
// то, что просит чарт». Прежняя редакция проверяла ВХОЖДЕНИЕ подстроки и была
// вакуумна с обеих сторон — разбор в шапке productimagename.go.
func TestEveryImageArgumentComesFromTheDeclaredNames(t *testing.T) {
	root := gateRepoRoot(t)

	lib := filepath.Join(root, "deploy", "scripts", "lib", "product-names.sh")
	if _, err := os.Stat(lib); err != nil {
		t.Fatalf("читателя имён для оболочки в дереве нет (%v) — рецепту стенда "+
			"неоткуда взять имя, и отрицание оси 1 вакуумно", err)
	}

	b, err := os.ReadFile(filepath.Join(root, "deploy", "Makefile"))
	if err != nil {
		t.Fatalf("рецепт стенда не читается (%v) — предпосылка исчезла", err)
	}
	mk := string(b)

	total := 0
	for _, target := range []string{"build-services", "reload-svc"} {
		body, ok := makeTargetBody(mk, target)
		if !ok {
			t.Errorf("цели %q в рецепте стенда нет — популяция изменилась, "+
				"проверь, чем теперь собираются образы", target)
			continue
		}
		found, seen := imageArgumentFindings(target, body)
		total += seen
		if seen == 0 {
			t.Errorf("в цели %q не разобрано ни одной ссылки на образ — разбор ослеп "+
				"(сменились формы команд), и «ноль находок» здесь неотличимо от "+
				"«ноль прочитанного»", target)
		}
		for _, f := range found {
			t.Errorf("%s", f)
		}
	}
	t.Logf("перепись: целей сборки 2, ссылок на образ осмотрено %d", total)
}

// makeTargetBody — тело цели: строки от заголовка до первой строки, начатой не
// с табуляции. Разбор грубый и достаточный: предмет — «зовёт ли цель читателя».
func makeTargetBody(mk, target string) (string, bool) {
	lines := strings.Split(mk, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, target+":") {
			continue
		}
		var body []string
		for _, b := range lines[i+1:] {
			if b != "" && !strings.HasPrefix(b, "\t") && !strings.HasPrefix(b, " ") &&
				!strings.HasPrefix(b, "ifndef") && !strings.HasPrefix(b, "endif") &&
				!strings.HasPrefix(b, "ifeq") {
				break
			}
			body = append(body, b)
		}
		return strings.Join(body, "\n"), true
	}
	return "", false
}

// Ось 3 — вторая ведомость сходится с объявленным источником.
// ciImageEntry — запись перечня конвейера «образ:каталог:контекст».
//
// Границей записи служит начало строки ЛИБО кавычка: первая запись перечня
// стоит на одной строке с открывающим `images="`, и якорь только по началу
// строки её НЕ ВИДЕЛ — разборщик молчал о ней и объявлял, что конвейер не
// собирает `kaname`, тогда как он собирает его первой же записью. Ложная
// находка была бы ровно того класса, что и предмет гейта: распознаватель,
// не знающий одной законной формы записи, не краснеет — он её не видит.
var ciImageEntry = regexp.MustCompile(`(?m)(?:^|["\s])([A-Za-z0-9][A-Za-z0-9._-]*):(services/[A-Za-z0-9-]+|gateway):`)

func TestImageBuildPipelineAgreesWithTheDeclaredNames(t *testing.T) {
	root := gateRepoRoot(t)

	b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "docker-build.yml"))
	if err != nil {
		t.Fatalf("конвейер сборки образов не читается (%v) — предпосылка исчезла", err)
	}
	ms := ciImageEntry.FindAllStringSubmatch(string(b), -1)
	if len(ms) == 0 {
		t.Fatal("в конвейере сборки не разобрано ни одной записи об образе — " +
			"обход пуст, вердикт беспредметен: форма перечня могла смениться")
	}

	var mismatched []string
	ci := map[string]bool{}
	for _, m := range ms {
		image, dir := m[1], m[2]
		ci[image] = true
		svc := strings.TrimPrefix(dir, "services/")
		if dir == "gateway" {
			svc = "api-gateway"
		}
		if want := productnaming.ChartName(svc); want != image {
			mismatched = append(mismatched, dir+": конвейер собирает "+image+
				", объявленный источник называет "+want)
		}
	}
	for _, m := range mismatched {
		t.Errorf("%s", m)
	}

	// Обратная сторона: каждое имя, объявленное источником для служб стенда,
	// конвейер обязан собирать. Иначе образ существует только на стенде.
	for _, svc := range makeServices(t, root) {
		if want := productnaming.ChartName(svc); !ci[want] {
			t.Errorf("служба %q: объявленный источник называет образ %q, "+
				"а конвейер такого не собирает", svc, want)
		}
	}
	t.Logf("перепись: записей об образах в конвейере %d, расхождений %d", len(ms), len(mismatched))
}

// makeServices — перечень служб, ВЫВЕДЕННЫЙ из рецепта стенда, а не выписанный
// здесь второй копией.
func makeServices(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "deploy", "Makefile"))
	if err != nil {
		t.Fatalf("рецепт стенда не читается (%v)", err)
	}
	re := regexp.MustCompile(`(?m)^SERVICES\s*:?=\s*(.+)$`)
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("перечень служб в рецепте стенда не найден — обход беспредметен")
	}
	out := strings.Fields(m[1])
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("перечень служб прочитался пустым — вердикт был бы вакуумным")
	}
	return out
}
