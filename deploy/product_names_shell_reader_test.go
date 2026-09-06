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
// # Почему расхождение НЕВОЗМОЖНО by construction, и зачем тогда проба
//
// Читатель оболочки правило не толкует: он собирает `tools/productnames/cmd/
// product-names` и спрашивает ответ у него, то есть у того же пакета. Второй
// реализации правила в дереве нет — значит расходиться нечему.
//
// Проба сторожит не расхождение реализаций, а РАЗРЫВ ПЕРЕНОСА: потерянную
// табуляцию, обрезанную строку, проглоченный код возврата, подставленное
// умолчание. Всё это тихо и даёт правдоподобный неверный ответ.
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
	if !strings.Contains(stderr, "product-names") {
		t.Errorf("отказ не назвал себя (%q) — читатель рецепта не поймёт, что отказало", stderr)
	}
	t.Logf("перепись: пустое имя → код %d, отказ назван", code)
}
