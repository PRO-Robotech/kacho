// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// product_names_shell_reader_test.go — ОБОЛОЧКА И GO ЧИТАЮТ ОДНО.
//
// # Что именно здесь доказывается
//
// Соответствие «каталог исходников ↔ имя чарта ↔ имя образа» объявлено один раз
// (`internal/productnaming`). Рецепт стенда — не Go, и вопрос «а точно ли он
// получает ТО ЖЕ имя» без пробы остаётся обещанием.
//
// Доказательство прямое: проба ЗОВЁТ настоящий читатель оболочки
// (`deploy/scripts/lib/product-names.sh`) в настоящей оболочке и сверяет каждый
// его ответ с `productnaming.ChartName`. Не «разбирает ту же ведомость своей
// копией» — копия сошлась бы с собой и молчала бы ровно тогда, когда разошлись
// бы стороны.
//
// # Что проба сторожит, а что — нет
//
// Читатель оболочки правило не толкует: он собирает `tools/productnames/cmd/
// product-names` и спрашивает ответ у него, то есть у того же пакета. Второй
// РЕАЛИЗАЦИИ правила в дереве нет — значит толкования разойтись не могут.
//
// Утверждать из этого «расхождение невозможно by construction» — ЛОЖЬ, и она
// здесь стояла. Инструмент собирается в файл, и пока путь этого файла не
// зависел от дерева, соседняя рабочая копия перезаписывала его своим: в одном
// процессе `vpc` отвечал по моему дереву, а `iam` — уже по чужому,
// правдоподобным неверным именем и без единого отказа. Путь теперь отпечатан
// корнем дерева (TestShellReaderCacheIsPerTree ниже), и это и есть то, чем
// утверждение сделано верным.
//
// Проба сторожит РАЗРЫВ ПЕРЕНОСА: потерянную табуляцию, обрезанную строку,
// проглоченный код возврата, подставленное умолчание. Всё это тихо и даёт
// правдоподобный неверный ответ.
//
// Чего проба НЕ покрывает: гонку между сборкой инструмента и его вызовом.
// Естественную гонку воспроизвести не удалось (в рецептах сборка и вызов стоят
// вплотную), и доказательства на неё здесь нет.
//
// # Популяция выводится из рецепта стенда
//
// Перечень служб берётся из `SERVICES` рецепта, а не выписывается здесь: вторая
// рукописная копия перечня разошлась бы с первой молча, и проба сверяла бы
// имена служб, которых стенд не поднимает.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

var servicesDecl = regexp.MustCompile(`(?m)^SERVICES\s*:?=\s*(.+)$`)

// standServices — службы стенда, выведенные из рецепта.
func standServices(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("рецепт стенда не читается (%v) — предпосылка пробы исчезла", err)
	}
	m := servicesDecl.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("объявления SERVICES в рецепте стенда нет — популяция не выводится, " +
			"вердикт был бы вакуумным")
	}
	svcs := strings.Fields(m[1])
	if len(svcs) == 0 {
		t.Fatal("перечень служб прочитался пустым — сверять нечего")
	}
	return svcs
}

// askShell — спросить читатель оболочки о названных службах.
func askShell(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	lib := filepath.Join("scripts", "lib", "product-names.sh")
	if _, err := os.Stat(lib); err != nil {
		t.Fatalf("читателя имён для оболочки нет (%v) — доказывать нечего", err)
	}
	script := ". ./" + filepath.ToSlash(lib) + `
product_names_load "$@" || exit $?
for s in "$@"; do printf '%s\t%s\n' "$s" "$(product_image_name "$s")"; done`

	cmd := exec.Command("bash", append([]string{"-c", script, "bash"}, args...)...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("оболочку не запустить (%v) — это «не выполнилось», а не расхождение", err)
	}
	return out.String(), errb.String(), code
}

func TestShellReaderAgreesWithTheDeclaredNames(t *testing.T) {
	svcs := standServices(t)

	stdout, stderr, code := askShell(t, svcs...)
	if code != 0 {
		t.Fatalf("читатель оболочки вернул %d — имена НЕ прочитаны, и это не «сошлось».\n%s",
			code, stderr)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("строка ответа оболочки не разбирается: %q — перенос порвался", line)
		}
		got[parts[0]] = parts[1]
	}
	if len(got) == 0 {
		t.Fatal("оболочка не назвала ни одного имени — обход пуст, вердикт беспредметен")
	}

	agreed := 0
	for _, svc := range svcs {
		want := productnaming.ChartName(svc)
		have, ok := got[svc]
		if !ok {
			t.Errorf("служба %q: оболочка имени не назвала, Go называет %q — перенос потерял строку",
				svc, want)
			continue
		}
		if have != want {
			t.Errorf("служба %q: оболочка называет %q, Go — %q. Стороны разошлись, "+
				"а рецепт стенда собирает по ответу оболочки", svc, have, want)
			continue
		}
		agreed++
	}
	if agreed == 0 {
		t.Error("ни одно имя не сошлось — сверка вакуумна")
	}
	t.Logf("перепись: служб стенда %d, имён прочитано оболочкой %d, сошлось %d",
		len(svcs), len(got), agreed)
}

// TestShellReaderCarriesTheRefusal — перенос сохраняет ОТКАЗ, а не только имена.
//
// Пустое имя — реальный вход: пустое раскрытие переменной у вызывающего. Если
// отказ теряется по дороге, рецепт получает пустую строку и собирает `:dev` —
// образ, которого никто не просил. Проба сторожит именно это звено переноса.
func TestShellReaderCarriesTheRefusal(t *testing.T) {
	_, stderr, code := askShell(t, "")
	if code == 0 {
		t.Error("на пустом имени читатель оболочки ответил успехом — отказ потерян " +
			"по дороге, и рецепт собрал бы образ под пустым именем")
	}
	named := strings.Contains(stderr, "product-names")
	if !named {
		t.Errorf("отказ не назвал себя (%q) — читатель рецепта не поймёт, что отказало", stderr)
	}
	t.Logf("перепись: пустое имя → код %d, отказ назвал себя: %t", code, named)
}

// TestShellReaderCacheIsPerTree — путь сборки инструмента ЗАВИСИТ ОТ ДЕРЕВА.
//
// Это держатель утверждения из шапки. Пока путь ключевался только на TMPDIR и
// uid, две рабочие копии делили один двоичный файл, и ответ доставался от той,
// что собрала последней.
func TestShellReaderCacheIsPerTree(t *testing.T) {
	lib := filepath.Join("scripts", "lib", "product-names.sh")
	ask := func(root string) string {
		t.Helper()
		cmd := exec.Command("bash", "-c",
			". ./"+filepath.ToSlash(lib)+"; _product_names_cache_dir \"$1\"", "bash", root)
		var out, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("путь кеша не вычисляется для %s (%v): %s — это «не выполнилось»", root, err, errb.String())
		}
		return strings.TrimSpace(out.String())
	}

	// ВЕЛИЧИНЫ ПЕРЕПИСИ ВЫЧИСЛЯЮТСЯ ИЗ ТОГО ЖЕ СОСТОЯНИЯ, ПО КОТОРОМУ СУДЯТ
	// УТВЕРЖДЕНИЯ, и печатаются ПОСЛЕ них.
	//
	// Прежняя редакция печатала «корней сверено 2, путей различных 2, повтор
	// устойчив» ЛИТЕРАЛОМ — то есть утверждала «путей различных 2» строкой ниже
	// собственной находки «две копии дерева дали ОДИН путь». Перепись затем и
	// нужна, чтобы «ноль находок» было отличимо от «ноль прочитанного»;
	// печатаемая безусловно, она это свойство ОТМЕНЯЕТ, оставляя его видимость.
	roots := []string{"/tmp/дерево-A", "/tmp/дерево-B"}
	paths := make([]string, 0, len(roots))
	distinct := map[string]bool{}
	for _, r := range roots {
		got := ask(r)
		if got == "" {
			t.Fatalf("путь кеша для %s прочитался пустым — сверять нечего, "+
				"вердикт беспредметен", r)
		}
		paths = append(paths, got)
		distinct[got] = true
	}
	if len(distinct) != len(roots) {
		t.Errorf("копий дерева %d, а путей сборки различных %d (%v) — соседняя копия "+
			"перезапишет инструмент этой, и ответ придёт по чужой ведомости",
			len(roots), len(distinct), paths)
	}
	stable := ask(roots[0]) == paths[0]
	if !stable {
		t.Errorf("путь для одного и того же дерева непостоянен — инструмент " +
			"пересобирался бы на каждом обращении")
	}
	t.Logf("перепись: корней сверено %d, путей различных %d, повтор устойчив: %t",
		len(roots), len(distinct), stable)
}
