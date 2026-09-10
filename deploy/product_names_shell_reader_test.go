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

	"github.com/PRO-Robotech/corelib/gitenv"
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

// serviceDirsInTree — каталоги под `services/`, взятые у ИНДЕКСА GIT.
//
// Единица счёта та же, что у `product_service_dirs` библиотеки: отслеживаемый
// элемент, а не файл на диске. Под деревом лежат распакованные чужие чарты и
// рабочие каталоги полос, и обход диска считал бы их частями продукта.
func serviceDirsInTree(t *testing.T) map[string]bool {
	t.Helper()
	// Через помощника, а не голым `exec.Command`: `cmd.Dir` не выбирает
	// репозиторий, когда в окружении есть GIT_DIR — переменная сильнее рабочего
	// каталога, и обход пошёл бы по чужому дереву. Держит это гейт
	// `internal/repohygiene` `TestGitCommandsRunWithScrubbedEnvironment`.
	cmd := gitenv.Command("..", "ls-files", "--", "services/*")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("индекс git не читается (%v) — популяция не выводится, "+
			"вердикт был бы вакуумным", err)
	}
	dirs := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(line, "/")
		if len(parts) >= 3 && parts[0] == "services" {
			dirs[parts[1]] = true
		}
	}
	if len(dirs) == 0 {
		t.Fatal("под services/ индекс не назвал ни одного каталога — обход пуст")
	}
	return dirs
}

// TestShellReaderDoesNotCheckThatTheDirectoryExists — ДЕРЖАТЕЛЬ утверждения шапки.
//
// # Что здесь доказывается и почему это не педантизм
//
// Шапка читателя оболочки два места подряд обещала: «Неизвестный каталог —
// ОТКАЗ, а не пустая строка». Отказ не наступал ни при каком входе, кроме
// пустой строки, — и это верно по существу: правило `productnaming.ChartName`
// выводит имя для любого непустого каталога, дерева оно не знает. Обещание было
// вторым местом об одном предмете, и верным было НЕ оно (шапка
// `tools/productnames/cmd/product-names` говорит противоположное и с доводом).
//
// Комментарий сам по себе не держится ничем, поэтому здесь закрепляется
// ПОВЕДЕНИЕ обеих сторон:
//
//   - непустой каталог, которого в дереве нет, имя ДАЁТ и кодом 0;
//   - вход, законный для правила и НЕ являющийся каталогом под `services/`,
//     проходит — иначе рецепт стенда перестал бы собирать свои образы.
//
// Вторая половина — не украшение, а измеренная цена исхода «дать коду обещанное
// поведение». Проверка существования `services/<каталог>` отвергла бы вход,
// который перечень сборки называет ЗАКОННЫМ, и сломала бы работающее. Популяция
// такого входа ВЫВОДИТСЯ из дерева (перечень рецепта минус индекс git), а не
// выписывается: выписанный список разошёлся бы с деревом молча.
func TestShellReaderDoesNotCheckThatTheDirectoryExists(t *testing.T) {
	// Каталог, которого в дереве нет by construction: имя несёт знаки, которых
	// путь под `services/` не несёт ни в одном элементе индекса.
	const absent = "нет-такого-каталога-в-дереве"

	// Путь ПРЯМОЙ: через `product_names_load` код возврата теряется в подстановке
	// `$(product_image_name …)` внутри самого сценария, и «отказ» пришёл бы к
	// пробе как пустая строка при коде 0. Отказ обязан быть виден кодом.
	stdout, stderr, code := askImageNameDirect(t, absent)
	if code != 0 {
		t.Errorf("на каталоге %q читатель ответил %d (%s).\n"+
			"Существование каталога здесь НЕ проверяется — это ответственность "+
			"вызывающего, которому известен корень. Завели проверку — правьте и "+
			"шапку читателя, и шапку tools/productnames/cmd/product-names: они "+
			"обе утверждают обратное", absent, code, strings.TrimSpace(stderr))
	}
	if want := productnaming.ChartName(absent); strings.TrimSpace(stdout) != want {
		t.Errorf("имя для отсутствующего каталога не выведено: %q, ждали %q — "+
			"правило перестало выводить имя для непустого входа", stdout, want)
	}

	// ЗАКОННЫЙ ВХОД, КОТОРОГО НЕТ ПОД services/ — выводится из дерева.
	inTree := serviceDirsInTree(t)
	var outside []string
	for _, svc := range standServices(t) {
		if !inTree[svc] {
			outside = append(outside, svc)
		}
	}
	if len(outside) == 0 {
		t.Skip("перечень сборки не называет ни одной части вне services/ — " +
			"довод против проверки существования сегодня беспредметен")
	}
	var errOutside string
	codeOutside := 0
	for _, svc := range outside {
		if _, e, c := askImageNameDirect(t, svc); c != 0 {
			errOutside, codeOutside = e, c
			break
		}
	}
	if codeOutside != 0 {
		t.Errorf("части %v рецепт стенда собирает, а читатель имён ответил %d (%s).\n"+
			"Каталога services/<она> в дереве нет — и это НОРМА: перечень частей "+
			"продукта и перечень каталогов под services/ РАЗНЫЕ словари",
			outside, codeOutside, strings.TrimSpace(errOutside))
	}
	t.Logf("перепись: каталогов под services/ %d, частей в перечне сборки %d, "+
		"из них вне services/ %d (%v); отсутствующий каталог → код %d",
		len(inTree), len(standServices(t)), len(outside), outside, code)
}

// askImageNameDirect — спросить ИМЯ, не звав перед этим `product_names_load`.
//
// Это ОТДЕЛЬНЫЙ путь, и он живой: `make reload-svc` подключает библиотеку и
// сразу зовёт `product_image_name "$(SVC)"`. Через `askShell` он недостижим —
// там `product_names_load` отказывает раньше и до обращения к ведомости дело не
// доходит, поэтому проба на нём была бы зелёной, не адресовав предмет.
func askImageNameDirect(t *testing.T, dir string) (string, string, int) {
	t.Helper()
	lib := filepath.Join("scripts", "lib", "product-names.sh")
	script := ". ./" + filepath.ToSlash(lib) + `
product_image_name "$1"`
	cmd := exec.Command("bash", "-c", script, "bash", dir)
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

// TestShellReaderRefusalNamesTheSubjectOnly — отказ на пустом имени читает
// ОПЕРАТОР, и он обязан называть предмет, а не внутренности оболочки.
//
// Пустое имя — единственный вход, на котором читатель отказывает, поэтому его
// диагностика и есть всё, что оператор увидит.
//
// Путь здесь ПРЯМОЙ (`product_image_name` без `product_names_load`), и это
// несущее свойство пробы, а не деталь: обращение к ведомости пустым ключом
// заставляло bash напечатать СВОЮ диагностику раньше стража — с номером строки
// библиотеки и именем внутренней переменной. По пути через `product_names_load`
// этого не видно вовсе: там отказ наступает раньше, и первая редакция этой
// пробы была ЗЕЛЁНОЙ именно поэтому — заголовок про отказ, проверенный там, где
// предмета нет.
//
// ЧТО ЗДЕСЬ НЕ УТВЕРЖДАЕТСЯ. Прямой путь в дереве один (`make reload-svc`), и у
// него СВОЯ проверка имени, отсекающая пустое прежде, — значит сегодня этой
// диагностики не видит никто. Проба закрепляет качество отказа вперёд, а не
// чинит наблюдаемый симптом: своя проверка вызывающего есть свойство ЕГО кода,
// и следующий вызывающий её не унаследует.
func TestShellReaderRefusalNamesTheSubjectOnly(t *testing.T) {
	_, stderr, code := askImageNameDirect(t, "")
	if code == 0 {
		t.Fatal("на пустом имени читатель ответил успехом — отказ потерян по дороге, " +
			"и рецепт собрал бы образ под пустым именем")
	}
	leaked := []string{}
	for _, leak := range []string{
		"_PRODUCT_IMAGE_NAME", // имя внутренней переменной библиотеки
		"product-names.sh: ",  // диагностика самого bash, с номером строки
	} {
		if strings.Contains(stderr, leak) {
			leaked = append(leaked, leak)
		}
	}
	if len(leaked) != 0 {
		t.Errorf("отказ несёт внутренности %v:\n%s\nОператор `make reload-svc` читает "+
			"это как поломку читателя имён, а не как свою пустую переменную", leaked, stderr)
	}
	named := strings.Contains(stderr, "имя части пусто")
	if !named {
		t.Errorf("отказ не назвал предмет (%q) — оператор не поймёт, что отказало", stderr)
	}
	t.Logf("перепись: пустое имя прямым путём → код %d, предмет назван: %t, "+
		"внутренностей в отказе %d", code, named, len(leaked))
}
