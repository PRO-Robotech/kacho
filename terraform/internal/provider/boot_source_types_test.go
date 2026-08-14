// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// productValidatorPath — где сервис объявляет закрытый набор.
	productValidatorPath = "../../../services/compute/internal/apps/kacho/api/instance/instance.go"

	// modulePath — модуль, который проверяет тот же вход у себя.
	moduleVariablesPath = "../../modules/compute-machine/variables.tf"
)

var bootSourceConstPattern = regexp.MustCompile(`bootSource(?:Storage|Registry)Image\s*=\s*"([^"]+)"`)

// TestBootSourceTypesMatchTheProduct — набор провайдера равен набору сервиса.
//
// Читается ИСХОДНИК сервиса, а не переписывается его перечень: константы лежат в
// `internal/` чужого поддерева, импортировать их нельзя, а вторая рукописная
// копия разошлась бы с первой молча — что уже и произошло на модуле.
//
// Чем это доказано: модуль `compute-machine` проверял вход набором
// `IMAGE`/`SNAPSHOT`/`VOLUME`, край не принимает из него НИ ОДНОГО значения, и
// проба модуля утверждала тот же неверный набор. Два артефакта согласны между
// собой и расходятся с продуктом — вечнозелёные, пока не дойдёт до применения.
func TestBootSourceTypesMatchTheProduct(t *testing.T) {
	raw, err := os.ReadFile(productValidatorPath)
	if err != nil {
		t.Fatalf("исходник сервиса не прочитан (%s): %v\n"+
			"Если файл переехал — правьте координату здесь: проба, потерявшая предмет, "+
			"обязана падать, а не молчать.", productValidatorPath, err)
	}

	var product []string
	for _, m := range bootSourceConstPattern.FindAllStringSubmatch(string(raw), -1) {
		product = append(product, m[1])
	}

	// Проверка собственной предпосылки: перечень выведен разбором, и если разбор
	// ничего не нашёл, «наборы совпадают» означало бы «сравнили с пустотой».
	if len(product) == 0 {
		t.Fatalf("в %s не найдено ни одной константы вида источника — предикат устарел "+
			"(константы переименованы или перенесены). Пустая сторона сравнения делает "+
			"эту пробу вечнозелёной, поэтому она падает здесь, а не молчит.",
			productValidatorPath)
	}
	t.Logf("в сервисе объявлено видов: %d (%s); у провайдера: %d (%s)",
		len(product), strings.Join(sorted(product), ", "),
		len(bootSourceTypes), strings.Join(sorted(bootSourceTypes), ", "))

	if strings.Join(sorted(product), ",") != strings.Join(sorted(bootSourceTypes), ",") {
		t.Errorf("наборы разошлись:\n  сервис:    %s\n  провайдер: %s\n"+
			"Провайдер, принимающий больше сервиса, откладывает отказ до применения; "+
			"принимающий меньше — отвергает законную конфигурацию на плане.",
			strings.Join(sorted(product), ", "), strings.Join(sorted(bootSourceTypes), ", "))
	}
}

// TestModuleBootSourceTypesMatchTheProvider — модуль проверяет тот же набор.
//
// Модуль не компилируется ничем, поэтому его перечень — единственное место, где
// расхождение не поймает ни сборка, ни линтер, ни пробы Go.
func TestModuleBootSourceTypesMatchTheProvider(t *testing.T) {
	raw, err := os.ReadFile(moduleVariablesPath)
	if err != nil {
		t.Fatalf("переменные модуля не прочитаны (%s): %v", moduleVariablesPath, err)
	}

	// Берётся строка проверки, а не весь файл: те же слова стоят в описании
	// переменной и в тексте отказа, и по всему файлу предикат считал бы прозу.
	line := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, "contains(") && strings.Contains(l, "boot_source_type") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("в %s не найдено проверки вида источника — предикат устарел "+
			"или проверку сняли. Снятая проверка обязана быть находкой, а не тишиной.",
			moduleVariablesPath)
	}

	inModule := regexp.MustCompile(`"([a-z]+\.[a-z]+)"`).FindAllStringSubmatch(line, -1)
	var got []string
	for _, m := range inModule {
		got = append(got, m[1])
	}
	t.Logf("в модуле объявлено видов: %d", len(got))

	if strings.Join(sorted(got), ",") != strings.Join(sorted(bootSourceTypes), ",") {
		t.Errorf("модуль проверяет не тот набор:\n  модуль:    %s\n  провайдер: %s\n"+
			"Модуль ничем не компилируется: такое расхождение не поймает ни сборка, "+
			"ни линтер — только эта проба.\nПравьте %s",
			strings.Join(sorted(got), ", "), strings.Join(sorted(bootSourceTypes), ", "),
			moduleVariablesPath)
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
