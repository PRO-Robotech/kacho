// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// fuzzsubject_test.go — гейт против фаз-цели без предмета.
//
// Фаз-цель ценна ровно тем, что гоняет ПРОДАКШН-код на враждебном входе. Цель,
// которая вместо этого зовёт заглушку, объявленную рядом в том же `_test.go`,
// не может дать красный ни по какой причине: она проверяет сама себя. При этом
// она занимает строку в матрице ночного прогона, тратит его час и каждое утро
// отчитывается зелёным — то есть активно утверждает, что предмет проверен.
//
// Такое уже находили здесь однажды и починили ровно одну цель из шести
// (`FuzzInstanceSpecValidate`, compute r9b#2 — «hollow-coverage»). Остальные
// пять пережили тот раунд, потому что возразить было некому: сверка матрицы с
// деревом (`continuous-fuzz.yml`, джоба `matrix-complete`) сличает ИМЕНА и
// структурно не способна увидеть, есть ли у имени предмет.
//
// Что считается предметом: вызов чего-либо из непубличного дерева этого модуля —
// `services/<svc>/internal/...`, `gateway/...`, `pkg/...`. НЕ считается:
//
//   - идентификаторы, определённые в самом `_test.go` (заглушка);
//   - стандартная библиотека (фаззить `netip.ParsePrefix` — это фаззить Go, а не
//     продукт);
//   - `pkg/api/...` — сгенерированные proto-стабы. Цель, которая ТОЛЬКО
//     раскодирует вход в стаб, фаззит protobuf-go. Стабы не исключены из
//     импортов — они исключены из ответа на вопрос «а что тут наше».
//
// Анализ идёт по вызовам, а не по импортам: импорт можно поставить и не
// использовать, и тогда гейт был бы обойдён одной строкой. Учитываются и
// непрямые вызовы — если цель зовёт помощника из своего же пакета, а тот уже
// идёт в прод-код, предмет есть (замыкание по графу вызовов пакета).
//
// Инициализаторы переменных ПАКЕТА предметом намеренно не считаются: такая
// переменная общая на все цели пакета, и одна настоящая цель отмывала бы ею
// всех соседей-заглушек — ровно тот случай, что здесь и был (три цели в одном
// пакете iam, из них ни одной с предметом). Предмет собирается в самой цели.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestEveryFuzzTargetDrivesProductionCode — ни одна `func Fuzz*(f *testing.F)`
// не проверяет саму себя.
//
// Что делать, если гейт сработал, — ровно два исхода, третьего нет:
//
//  1. предмет существует -> навести цель на него (вызвать реальный прод-код —
//     тот самый путь, по которому идёт запрос);
//  2. предмета нет -> УДАЛИТЬ цель вместе с посевами и строкой в матрице
//     `continuous-fuzz.yml`. Заглушка, притворяющаяся целью, хуже отсутствия
//     цели: отсутствие видно, а заглушка отчитывается зелёным.
//
// «Доработаем позже» исходом не является: пока цель стоит в матрице, она
// утверждает, что предмет проверяется.
func TestEveryFuzzTargetDrivesProductionCode(t *testing.T) {
	root := repoRoot(t)
	targets, census := analyzeFuzzSubjects(t, newTrackedTree(t, root), moduleImportPaths(t, root))

	// ПЕРЕПИСЬ печатается ВСЕГДА, включая зелёный прогон: «ноль целей без
	// предмета» обязано отличаться от «ноль прочитанных файлов».
	t.Logf("%s; фаз-целей найдено %d", census, len(targets))
	if len(census.unparsed) > 0 {
		t.Errorf("не разобрано %d файлов — по ним у гейта НЕТ ответа, и молчание про них "+
			"неотличимо от «предмет есть»:\n  %s\n\nНеразобранный файл не «чистый»: его цели "+
			"выпадают из множества, а сверка с матрицей потом объявляет их отсутствующими в "+
			"коде — верный красный с неверной причиной.",
			len(census.unparsed), strings.Join(census.unparsed, "\n  "))
	}
	if census.testFiles == 0 {
		t.Fatal("из индекса не отобрано НИ ОДНОГО *_test.go — это слепой обход, а не «чисто». " +
			"Гейт на пустом множестве зелен всегда: почини отбор файлов.")
	}

	var hollow []string
	for _, tg := range targets {
		if !tg.hasSubject {
			hollow = append(hollow, tg.String())
		}
	}
	if len(hollow) > 0 {
		t.Errorf("найдено %d фаз-целей без предмета: тело цели не доходит ни до одной "+
			"строки прод-кода этого модуля и проверяет заглушку, объявленную рядом в "+
			"том же _test.go. Красный такая цель дать не может, а строку в матрице "+
			"ночного прогона занимает и каждое утро отчитывается зелёным:\n  %s\n\n"+
			"Исходы: навести цель на реальный прод-путь / удалить цель вместе с "+
			"посевами и строкой в .github/workflows/continuous-fuzz.yml. Предметом "+
			"считается вызов в services/<svc>/internal/…, gateway/… или pkg/… (кроме "+
			"сгенерированных стабов pkg/api/…).",
			len(hollow), strings.Join(hollow, "\n  "))
	}
}

// TestFuzzSubjectAnalyzerSeesEveryScheduledTarget — предпосылка гейта выше.
//
// Гейт разбирает дерево сам. Обход, который перестал доходить до целей (папку
// переименовали, форма объявления изменилась), выходит ЗЕЛЁНЫМ на пустом
// множестве — ровно тот класс, который здесь искореняют. Поэтому разобранное
// множество сверяется с расписанием: каждая цель, которую ночной прогон
// запускает, обязана быть видна анализатору.
//
// Обратное направление (цель в коде, но не в матрице) — в
// TestScheduledMatrixCoversEveryFuzzTarget ниже, на том же разборе.
func TestFuzzSubjectAnalyzerSeesEveryScheduledTarget(t *testing.T) {
	root := repoRoot(t)
	targets, census := analyzeFuzzSubjects(t, newTrackedTree(t, root), moduleImportPaths(t, root))
	t.Logf("%s; фаз-целей найдено %d", census, len(targets))
	seen := map[string]bool{}
	for _, tg := range targets {
		seen[tg.name] = true
	}
	if len(seen) == 0 {
		t.Fatal("анализатор не нашёл в дереве НИ ОДНОЙ функции `func Fuzz*(f *testing.F)`. " +
			"Это не «чисто», это слепой обход: гейт на пустом множестве зелен всегда. " +
			"Почини обход или форму распознавания.")
	}

	for _, name := range scheduledFuzzTargets(t, root) {
		if !seen[name] {
			t.Errorf("цель %s стоит в матрице .github/workflows/continuous-fuzz.yml, но "+
				"анализатор её в дереве не видит. Либо функции нет (тогда ночной прогон "+
				"каждую ночь запускает пустоту), либо ослеп обход — и тогда гейт предмета "+
				"молча перестал покрывать часть целей.", name)
		}
	}
}

// TestScheduledMatrixCoversEveryFuzzTarget — матрица ночного прогона и дерево
// обязаны совпадать, и проверяется это ДЕТЕРМИНИРОВАННО.
//
// Направление «цель в коде, но не в матрице» — единственная защита от того, что
// написанную фаз-цель не фаззит никто и никогда. Раньше оно жило джобой
// `matrix-complete` внутри `continuous-fuzz.yml`, а у того workflow триггеры
// только `schedule` и `workflow_dispatch`. Расписание у провайдера исполняется
// НЕНАДЁЖНО (пропуски под нагрузкой; на ветке, отличной от основной, крон вообще
// не заводится), поэтому единственный контроль этого направления не исполнялся
// предсказуемо ни разу. Здесь он идёт обычным `go test ./...` — то есть на каждом
// PR и на каждом пуше в интеграционную ветку.
//
// Джоба `matrix-complete` за ненадобностью снята: две реализации одного правила
// разъезжаются, а разбор здесь строже — он требует ПОДПИСЬ `func Fuzz…(f
// *testing.F)`, а не совпадение текста `func Fuzz` где угодно.
func TestScheduledMatrixCoversEveryFuzzTarget(t *testing.T) {
	root := repoRoot(t)

	targets, census := analyzeFuzzSubjects(t, newTrackedTree(t, root), moduleImportPaths(t, root))
	var inCode []string
	for _, tg := range targets {
		inCode = append(inCode, tg.name)
	}
	inMatrix := scheduledFuzzTargets(t, root)

	// Предпосылка: пустое множество с любой стороны означает сломанный разбор, а
	// не согласие. Сравнение двух пустот зелено всегда.
	if len(inCode) == 0 {
		t.Fatal("в дереве не найдено НИ ОДНОЙ фаз-цели — сломан обход, а не «матрица совпадает»")
	}
	if len(inMatrix) == 0 {
		t.Fatal("в continuous-fuzz.yml не разобрано НИ ОДНОЙ цели матрицы — сломан разбор, " +
			"а не «матрица совпадает»")
	}

	missingFromMatrix, missingFromCode := matrixDivergence(inCode, inMatrix)
	for _, name := range missingFromMatrix {
		t.Errorf("фаз-цель %s есть в дереве, но НЕ в матрице .github/workflows/continuous-fuzz.yml — "+
			"её не фаззит никто и никогда, и об этом никто не узнает. Внеси её в матрицу либо удали "+
			"цель вместе с посевами.", name)
	}
	for _, name := range missingFromCode {
		t.Errorf("цель %s стоит в матрице .github/workflows/continuous-fuzz.yml, но такой функции в "+
			"дереве нет — ночной прогон каждую ночь запускает пустоту и зеленеет. Убери строку из "+
			"матрицы либо напиши цель.", name)
	}
	t.Logf("%s; сверено целей: в дереве %d, в матрице %d", census, len(inCode), len(inMatrix))
}

// TestMatrixDivergenceIsSymmetric — гейт выше проверен инъекцией в ОБЕ стороны,
// на фикстурах, а не правкой файлов дерева.
//
// Без этого «расхождений нет» на настоящем дереве не отличить от предиката,
// который расхождения не умеет видеть, — ровно то, чем этот контроль и был, пока
// висел на расписании.
func TestMatrixDivergenceIsSymmetric(t *testing.T) {
	cases := []struct {
		name              string
		inCode, inMatrix  []string
		wantMissingMatrix []string
		wantMissingCode   []string
	}{
		{
			name:     "совпадают (порядок не важен)",
			inCode:   []string{"FuzzB", "FuzzA"},
			inMatrix: []string{"FuzzA", "FuzzB"},
		},
		{
			name:              "цель в коде, в матрице нет — не фаззится никогда",
			inCode:            []string{"FuzzA", "FuzzNew"},
			inMatrix:          []string{"FuzzA"},
			wantMissingMatrix: []string{"FuzzNew"},
		},
		{
			name:            "цель в матрице, в коде нет — прогон гоняет пустоту",
			inCode:          []string{"FuzzA"},
			inMatrix:        []string{"FuzzA", "FuzzGone"},
			wantMissingCode: []string{"FuzzGone"},
		},
		{
			name:              "расхождение сразу в обе стороны",
			inCode:            []string{"FuzzA", "FuzzNew"},
			inMatrix:          []string{"FuzzA", "FuzzGone"},
			wantMissingMatrix: []string{"FuzzNew"},
			wantMissingCode:   []string{"FuzzGone"},
		},
		{
			name:              "дубли в матрице не создают ложного расхождения",
			inCode:            []string{"FuzzA"},
			inMatrix:          []string{"FuzzA", "FuzzA"},
			wantMissingMatrix: nil,
			wantMissingCode:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMatrix, gotCode := matrixDivergence(tc.inCode, tc.inMatrix)
			if strings.Join(gotMatrix, ",") != strings.Join(tc.wantMissingMatrix, ",") {
				t.Errorf("нет в матрице: %v, ожидалось %v", gotMatrix, tc.wantMissingMatrix)
			}
			if strings.Join(gotCode, ",") != strings.Join(tc.wantMissingCode, ",") {
				t.Errorf("нет в коде: %v, ожидалось %v", gotCode, tc.wantMissingCode)
			}
		})
	}
}

// matrixDivergence — расхождение двух множеств имён в обе стороны, отсортированное.
func matrixDivergence(inCode, inMatrix []string) (missingFromMatrix, missingFromCode []string) {
	code, matrix := map[string]bool{}, map[string]bool{}
	for _, n := range inCode {
		code[n] = true
	}
	for _, n := range inMatrix {
		matrix[n] = true
	}
	for n := range code {
		if !matrix[n] {
			missingFromMatrix = append(missingFromMatrix, n)
		}
	}
	for n := range matrix {
		if !code[n] {
			missingFromCode = append(missingFromCode, n)
		}
	}
	sort.Strings(missingFromMatrix)
	sort.Strings(missingFromCode)
	return missingFromMatrix, missingFromCode
}

// TestFuzzWorkflowDoesNotSwallowExitCode — код выхода прогона обязан доходить
// до вывода шага.
//
// `go test -fuzz … | tee fuzz-output.log` отдаёт шагу статус `tee`, а не
// прогона: tee успешно записал файл — значит шаг зелёный, что бы фаззер ни
// нашёл. Ночной слой в таком виде не может сообщить о находке вообще: краши
// оседают в артефакте, который никто не смотрит, потому что смотреть зовёт
// красный.
//
// Лечится `set -o pipefail` (или чтением `PIPESTATUS`) в том же шаге. Проверка
// живёт здесь, а не в самом workflow, потому что workflow о себе ничего
// утверждать не может: его вердикт и есть предмет спора.
func TestFuzzWorkflowDoesNotSwallowExitCode(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "continuous-fuzz.yml"))
	if err != nil {
		t.Fatalf("не прочитан continuous-fuzz.yml: %v", err)
	}

	if len(shellRunBlocks(string(raw))) == 0 {
		t.Fatal("в continuous-fuzz.yml не найдено ни одного блока `run:` — читать нечего, " +
			"а значит проверка ничего не утверждает. Почини разбор.")
	}

	for _, b := range pipedRunsWithoutPipefail(string(raw)) {
		t.Errorf("continuous-fuzz.yml:%d — прогон фаззера уходит в пайп, а статус шага "+
			"берётся от последней команды конвейера, не от `go test`. Шаг зеленеет при "+
			"любой находке фаззера: краш оседает в артефакте, за которым никто не "+
			"приходит, потому что приходить зовёт красный.\n\nПоставь `set -o pipefail` "+
			"в этом же блоке `run:` либо читай ${PIPESTATUS[0]}.", b.line)
	}
}

// TestPipefailCheckReadsCodeNotProse — гейт выше обязан быть проверен на
// воспроизведённом дефекте, и один такой дефект у него уже был.
//
// Первая редакция искала слово `pipefail` во всём тексте шага. Комментарий,
// объясняющий защиту, это слово содержит — поэтому при СНЯТОЙ защите гейт
// оставался зелёным, читая собственное объяснение. Проверка формы без предмета
// на месте проверки предмета.
func TestPipefailCheckReadsCodeNotProse(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantFlags int
	}{
		{
			name: "защита стоит",
			body: "    steps:\n      - name: run fuzz\n        run: |\n" +
				"          set -o pipefail\n          go test -fuzz=X ./p | tee out.log\n",
		},
		{
			name: "защита снята, объяснение осталось",
			body: "    steps:\n      - name: run fuzz\n        run: |\n" +
				"          # без pipefail статус берётся от tee\n" +
				"          go test -fuzz=X ./p | tee out.log\n",
			wantFlags: 1,
		},
		{
			name: "команда разложена переносом строки",
			body: "    steps:\n      - name: run fuzz\n        run: |\n" +
				"          go test -fuzz=X \\\n            ./p 2>&1 | tee out.log\n",
			wantFlags: 1,
		},
		{
			name: "защита в СОСЕДНЕМ шаге не считается",
			body: "    steps:\n      - name: a\n        run: |\n          set -o pipefail\n" +
				"      - name: run fuzz\n        run: |\n          go test -fuzz=X ./p | tee out.log\n",
			wantFlags: 1,
		},
		{
			name: "конвейера нет — терять статус негде",
			body: "    steps:\n      - name: run fuzz\n        run: |\n          go test -fuzz=X ./p\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(pipedRunsWithoutPipefail(tc.body)); got != tc.wantFlags {
				t.Fatalf("помечено блоков: %d, ожидалось %d", got, tc.wantFlags)
			}
		})
	}
}

// pipedRunsWithoutPipefail — блоки `run:`, где `go test` уходит в конвейер, а
// статус конвейера не проверяется.
//
// Читается ТОЛЬКО исполняемая часть блока: слово `pipefail` в объяснении
// защитой не является. Продолжения строк склеиваются — статус берётся от ВСЕГО
// конвейера, как бы тот ни был разложен по строкам. Защита засчитывается лишь в
// СВОЁМ блоке: `set -o pipefail` действует на оболочку шага, а не на файл.
func pipedRunsWithoutPipefail(body string) []shellRunBlock {
	var out []shellRunBlock
	for _, b := range shellRunBlocks(body) {
		code := strings.ReplaceAll(stripShellComments(b.text), "\\\n", " ")
		var piped bool
		for _, line := range strings.Split(code, "\n") {
			if strings.Contains(line, "go test") && strings.Contains(line, "|") {
				piped = true
			}
		}
		if !piped {
			continue
		}
		if !strings.Contains(code, "pipefail") && !strings.Contains(code, "PIPESTATUS") {
			out = append(out, b)
		}
	}
	return out
}

// stripShellComments убирает из тела шага всё, что оболочка не исполняет.
// Кавычек в этих скриптах нет вокруг `#`, поэтому достаточно отрезать хвост от
// первой решётки.
func stripShellComments(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "#"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// shellRunBlock — тело одного шага `run:` вместе с номером строки, где оно
// начинается.
type shellRunBlock struct {
	line int
	text string
}

// shellRunBlocks вырезает тела `run: |` по отступу: блок кончается первой
// непустой строкой с отступом не больше, чем у самого ключа `run:`.
func shellRunBlocks(body string) []shellRunBlock {
	lines := strings.Split(body, "\n")
	var out []shellRunBlock
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "run: |" && !strings.HasPrefix(trimmed, "run: |") {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				body = append(body, lines[j])
				continue
			}
			if len(lines[j])-len(strings.TrimLeft(lines[j], " ")) <= indent {
				break
			}
			body = append(body, lines[j])
		}
		out = append(out, shellRunBlock{line: i + 1, text: strings.Join(body, "\n")})
		i = j - 1
	}
	return out
}

// TestFuzzSubjectAnalyzerFlagsAStub — гейт обязан быть проверен на
// воспроизведённом дефекте, иначе он сам экземпляр «формы без содержания».
//
// Синтетическое дерево: одна цель зовёт заглушку из своего же файла, вторая
// доходит до прод-пакета напрямую, третья — через помощника своего пакета
// (непрямой предмет тоже предмет), четвёртая имеет импорт прод-пакета, но не
// зовёт из него ничего (импорт-без-вызова гейт обойти не должен), пятая
// раскодирует вход только в сгенерированный стаб.
func TestFuzzSubjectAnalyzerFlagsAStub(t *testing.T) {
	const mod = "example.com/m"
	root := t.TempDir()

	mustWriteFile(t, root, "svc/internal/fuzz/stub_test.go", `package fuzz_test
import "testing"
	func FuzzStub(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { _ = parseStub(s) }) }
func parseStub(s string) bool { return len(s) > 0 }
`)
	mustWriteFile(t, root, "svc/internal/fuzz/direct_test.go", `package fuzz_test
import (
	"testing"
	"example.com/m/svc/internal/domain"
)
	func FuzzDirect(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { _ = domain.Parse(s) }) }
`)
	mustWriteFile(t, root, "svc/internal/fuzz/indirect_test.go", `package fuzz_test
import (
	"testing"
	"example.com/m/svc/internal/domain"
)
	func FuzzIndirect(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { _ = viaHelper(s) }) }
func viaHelper(s string) bool { return domain.Parse(s) }
`)
	mustWriteFile(t, root, "svc/internal/fuzz/importonly_test.go", `package fuzz_test
import (
	"testing"
	_ "example.com/m/svc/internal/domain"
)
	func FuzzImportOnly(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { _ = len(s) }) }
`)
	mustWriteFile(t, root, "svc/internal/fuzz/genonly_test.go", `package fuzz_test
import (
	"testing"
	pb "example.com/m/pkg/api/kacho/cloud/x/v1"
)
	func FuzzGeneratedOnly(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) { _ = pb.Thing{} })
}
`)

	targets, census := analyzeFuzzSubjects(t, newSyntheticTree(t, root), []string{mod})
	got := map[string]bool{}
	for _, tg := range targets {
		got[tg.name] = tg.hasSubject
	}

	// Перепись обязана НАЗЫВАТЬ прочитанное, а не украшать вывод: на дереве из
	// пяти файлов она обязана сказать «пять». Число, которое никто не проверил,
	// со временем начинает лгать так же уверенно, как отсутствующее.
	if census.testFiles != 5 || census.packages != 1 {
		t.Errorf("перепись: %s — ожидалось 5 *_test.go в 1 пакете", census)
	}
	if len(census.unparsed) != 0 {
		t.Errorf("перепись объявила неразобранные файлы там, где все пять разбираемы: %v", census.unparsed)
	}
	want := map[string]bool{
		"FuzzStub":          false,
		"FuzzDirect":        true,
		"FuzzIndirect":      true,
		"FuzzImportOnly":    false,
		"FuzzGeneratedOnly": false,
	}
	for name, wantSubject := range want {
		gotSubject, ok := got[name]
		if !ok {
			t.Errorf("анализатор не нашёл цель %s в синтетическом дереве", name)
			continue
		}
		if gotSubject != wantSubject {
			t.Errorf("%s: предмет=%v, ожидалось %v", name, gotSubject, wantSubject)
		}
	}
}

// TestFuzzCensusNamesUnparsedFiles — обнаружение неразобранного проверено
// НАСТОЯЩИМ битым входом, а не только отсутствием находок на здоровом дереве.
//
// Утверждение «не разобрано 0» без парного положительного неотличимо от
// предиката, который неразобранное видеть не умеет, — и зеленеет тем увереннее,
// чем сильнее всё сломано. Здесь рядом лежат оба файла: битый обязан быть
// НАЗВАН, здоровый из того же каталога — по-прежнему разобран (иначе отсев
// грубее своего предмета и роняет весь пакет из-за одного соседа).
func TestFuzzCensusNamesUnparsedFiles(t *testing.T) {
	const mod = "example.com/m"
	root := t.TempDir()

	mustWriteFile(t, root, "svc/internal/fuzz/broken_test.go", `package fuzz_test
this is not go(((
`)
	mustWriteFile(t, root, "svc/internal/fuzz/healthy_test.go", `package fuzz_test
import (
	"testing"
	"example.com/m/svc/internal/domain"
)
func FuzzHealthy(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { _ = domain.Parse(s) }) }
`)

	targets, census := analyzeFuzzSubjects(t, newSyntheticTree(t, root), []string{mod})

	// (а) ПОЛОЖИТЕЛЬНОЕ НАПРАВЛЕНИЕ: битый файл назван, с координатой.
	if len(census.unparsed) != 1 {
		t.Fatalf("неразобранных: %d, ожидался ровно один (битый файл) — %v", len(census.unparsed), census.unparsed)
	}
	if !strings.Contains(census.unparsed[0], "svc/internal/fuzz/broken_test.go") {
		t.Errorf("находка не называет координату битого файла: %q", census.unparsed[0])
	}
	if !strings.Contains(census.String(), "НЕ РАЗОБРАНО") {
		t.Errorf("перепись молчит про неразобранное: %q", census.String())
	}

	// (б) ОТРИЦАТЕЛЬНОЕ НАПРАВЛЕНИЕ: здоровый сосед по каталогу не пострадал —
	// его цель найдена и предмет у неё есть.
	if len(targets) != 1 || targets[0].name != "FuzzHealthy" {
		t.Fatalf("здоровый сосед потерян вместе с битым: %v", targets)
	}
	if !targets[0].hasSubject {
		t.Error("у здоровой цели пропал предмет — разбор пакета сорвался из-за соседа")
	}
	if census.testFiles != 2 {
		t.Errorf("перепись: %s — ожидалось 2 отобранных файла (битый тоже ОТОБРАН, просто не разобран)", census)
	}
}

// ── анализатор ────────────────────────────────────────────────────

// fuzzTarget — одна найденная `func Fuzz*(f *testing.F)`.
type fuzzTarget struct {
	name       string
	file       string // путь относительно корня репозитория
	line       int
	hasSubject bool
}

func (t fuzzTarget) String() string {
	return t.file + ":" + strconv.Itoa(t.line) + " — " + t.name
}

// analyzeFuzzSubjects обходит дерево и отвечает по каждой фаз-цели, доходит ли
// её тело до прод-кода модуля.
//
// Разбор идёт по пакетам (директориям), потому что цель вправе звать помощника
// из своего же файла-соседа: сначала по каждой функции пакета собирается, зовёт
// ли она прод-пакет напрямую и каких локальных функций касается, затем предмет
// распространяется по графу вызовов до неподвижной точки.
//
// СОСТАВ ДЕРЕВА БЕРЁТСЯ ИЗ ИНДЕКСА GIT, А НЕ С ДИСКА. Прежний обход диска от
// корня читал `.claude/worktrees/` — рабочие копии дерева, которые git
// игнорирует. Доказано инъекцией: ОДИН файл `func FuzzGhostTarget(f *testing.F)`
// внутри игнорируемого каталога роняет `TestEveryFuzzTargetDrivesProductionCode`
// и `TestScheduledMatrixCoversEveryFuzzTarget` требованием внести в матрицу
// цель, которой в репозитории не существует. Строгое равенство «в дереве ==
// в матрице» здесь и правильно, и хрупко: любой лишний файл сдвигает левую
// часть, поэтому левая часть обязана означать РЕПОЗИТОРИЙ.
// fuzzCensus — ОБЪЁМ ОСМОТРЕННОГО. Печатается каждым вызывающим, потому что
// «ноль целей без предмета» и «ноль прочитанных файлов» — разные утверждения, а
// выглядят они одинаково: зелёной строкой.
//
// Предпосылки этого гейта живут в дереве, а не в его коде: список читается из
// индекса git, файлы отбираются по суффиксу `_test.go`, каталоги-исключения
// заданы префиксами. Любая из этих опор может уехать (переименовали каталог,
// сменили раскладку, сломали индекс) — и тогда обход честно вернёт пустоту, а
// гейт честно скажет «нарушений нет». Число прочитанного — единственное, что
// отличает одно от другого без запуска отладчика.
type fuzzCensus struct {
	indexFiles int      // файлов в индексе git — сколько всего предъявило дерево
	testFiles  int      // из них отобрано `_test.go` (за вычетом исключённых префиксов)
	packages   int      // каталогов, разобранных как пакет
	unparsed   []string // файлы, которые разобрать не удалось: ответа по ним НЕТ
}

// String — одна строка переписи для t.Logf.
func (c fuzzCensus) String() string {
	s := "осмотрено: индекс " + strconv.Itoa(c.indexFiles) + " файлов -> " +
		strconv.Itoa(c.testFiles) + " *_test.go в " + strconv.Itoa(c.packages) + " пакетах"
	if len(c.unparsed) > 0 {
		s += "; НЕ РАЗОБРАНО " + strconv.Itoa(len(c.unparsed))
	}
	return s
}

func analyzeFuzzSubjects(t *testing.T, tt *trackedTree, modulePaths []string) ([]fuzzTarget, fuzzCensus) {
	t.Helper()
	root := tt.root
	census := fuzzCensus{indexFiles: tt.count()}

	byDir := map[string][]string{}
	for rel := range tt.files {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if strings.HasPrefix(rel, "vendor/") ||
			strings.HasPrefix(rel, "node_modules/") ||
			strings.HasPrefix(rel, "ui-future/") ||
			rel == "testdata" || strings.Contains(rel, "/testdata/") ||
			strings.HasPrefix(rel, "testdata/") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		dir := filepath.Dir(path)
		byDir[dir] = append(byDir[dir], path)
		census.testFiles++
	}
	for dir := range byDir {
		sort.Strings(byDir[dir]) // разбор пакета не должен зависеть от порядка карты
	}
	census.packages = len(byDir)

	var out []fuzzTarget
	for dir, files := range byDir {
		targets, unparsed := analyzePackage(t, root, modulePaths, dir, files)
		out = append(out, targets...)
		census.unparsed = append(census.unparsed, unparsed...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].line < out[j].line
	})
	sort.Strings(census.unparsed)
	return out, census
}

// funcFacts — что известно про одну функцию пакета.
type funcFacts struct {
	callsProd  bool     // зовёт что-то из прод-дерева модуля напрямую
	callsLocal []string // зовёт функции своего же пакета (по имени)
}

func analyzePackage(t *testing.T, root string, modulePaths []string, dir string, files []string) ([]fuzzTarget, []string) {
	t.Helper()

	fset := token.NewFileSet()
	facts := map[string]*funcFacts{}
	type declRef struct {
		name string
		file string
		line int
	}
	var fuzzDecls []declRef
	var unparsed []string

	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// Неразобранный файл — НЕ «чистый»: ответа про его цели у гейта нет.
			// Прежняя редакция молча его пропускала, ссылаясь на сборку. Но этот
			// пакет собирается сам по себе (`go test ./internal/repohygiene/`) и
			// чужие `_test.go` не компилирует, поэтому «поймает сборка» здесь
			// никого не ловит: цель из такого файла просто исчезала из множества,
			// а сверка с матрицей объявляла её ОТСУТСТВУЮЩЕЙ В КОДЕ и советовала
			// «напиши цель» — верный красный с неверной причиной.
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			unparsed = append(unparsed, filepath.ToSlash(rel)+": "+err.Error())
			continue
		}
		prodPkgs := prodImportNames(f, modulePaths)

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			ff := &funcFacts{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					ff.callsLocal = append(ff.callsLocal, fn.Name)
				case *ast.SelectorExpr:
					if x, ok := fn.X.(*ast.Ident); ok && prodPkgs[x.Name] {
						ff.callsProd = true
					}
				}
				return true
			})
			// Обращение к прод-пакету не обязано быть вызовом: композитный литерал,
			// константа, переменная-ошибка — тоже касание прод-кода.
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && prodPkgs[x.Name] {
					ff.callsProd = true
				}
				return true
			})
			facts[fd.Name.Name] = ff

			if isFuzzTargetDecl(fd) {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				fuzzDecls = append(fuzzDecls, declRef{
					name: fd.Name.Name,
					file: filepath.ToSlash(rel),
					line: fset.Position(fd.Pos()).Line,
				})
			}
		}
	}

	// Неподвижная точка: предмет распространяется вверх по локальным вызовам.
	for changed := true; changed; {
		changed = false
		for _, ff := range facts {
			if ff.callsProd {
				continue
			}
			for _, callee := range ff.callsLocal {
				if c, ok := facts[callee]; ok && c.callsProd {
					ff.callsProd = true
					changed = true
					break
				}
			}
		}
	}

	out := make([]fuzzTarget, 0, len(fuzzDecls))
	for _, d := range fuzzDecls {
		out = append(out, fuzzTarget{
			name:       d.name,
			file:       d.file,
			line:       d.line,
			hasSubject: facts[d.name] != nil && facts[d.name].callsProd,
		})
	}
	return out, unparsed
}

// isFuzzTargetDecl — `func Fuzz…(f *testing.F)`: имя и подпись, как их требует
// `go test -fuzz`.
func isFuzzTargetDecl(fd *ast.FuncDecl) bool {
	if !strings.HasPrefix(fd.Name.Name, "Fuzz") || fd.Name.Name == "Fuzz" {
		return false
	}
	params := fd.Type.Params
	if params == nil || len(params.List) != 1 {
		return false
	}
	star, ok := params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "F"
}

// prodImportNames — локальные имена импортов, которые считаются прод-кодом
// этого модуля.
//
// Сгенерированные стабы `pkg/api/…` исключены намеренно: цель, которая только
// раскодирует вход в стаб, фаззит protobuf-go, а не продукт. Blank- и
// dot-импорты не дают имени для вызова и в множество не попадают — импорт без
// вызова предметом не является.
func prodImportNames(f *ast.File, modulePaths []string) map[string]bool {
	out := map[string]bool{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		own, generated := false, false
		for _, mod := range modulePaths {
			if strings.HasPrefix(path, mod+"/") {
				own = true
				if strings.HasPrefix(path, mod+"/pkg/api/") {
					generated = true
				}
				break
			}
		}
		if !own || generated {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			name = imp.Name.Name
		}
		out[name] = true
	}
	return out
}

// scheduledFuzzTargets — цели, перечисленные в матрице ночного прогона.
func scheduledFuzzTargets(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "continuous-fuzz.yml"))
	if err != nil {
		t.Fatalf("не прочитан continuous-fuzz.yml: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		idx := strings.Index(line, "{ target:")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len("{ target:"):])
		if c := strings.Index(rest, ","); c >= 0 {
			rest = rest[:c]
		}
		// Тот же файл несёт скрипт джобы `matrix-complete`, а в нём та же
		// подстрока стоит внутри регулярного выражения. Берём только настоящие
		// Go-идентификаторы — иначе гейт «не вижу цель» падал бы на куске regexp.
		if name := strings.TrimSpace(rest); isGoIdentifier(name) && strings.HasPrefix(name, "Fuzz") {
			out = append(out, name)
		}
	}
	return out
}

// moduleImportPath — путь модуля из go.mod. Префикс «своё vs чужое» берётся отсюда,
// а не из константы: константа пережила бы переименование модуля молча, и тогда
// прод-импорты перестали бы считаться своими — гейт начал бы валить всё подряд.
// moduleImportPaths — пути ВСЕХ модулей дерева, ВЫВЕДЕННЫЕ из него.
//
// Модуль в дереве перестал быть единственным: служба iam несёт свой `go.mod`,
// потому что выносится отдельным репозиторием. Распознаватель прод-кода ниже
// сверял импорт с ОДНИМ путём и после разделения перестал видеть прод-код
// службы вовсе — её фаз-цель объявилась «заглушкой», ничего не изменив в себе.
// Класс известный: распознаватель, не знающий одной из законных форм записи
// предмета, не даёт ни красного, ни зелёного — он молчит.
//
// Перечень ВЫВОДИТСЯ обходом, а не выписывается: выписанный разошёлся бы с
// деревом при заведении следующего модуля — и разошёлся бы молча.
func moduleImportPaths(t *testing.T, root string) []string {
	t.Helper()

	// Состав берётся у ИНДЕКСА, а не у диска. Обход диска от корня прочитал бы
	// каталоги, которых в репозитории нет — рабочие копии агентов, отчёты
	// прогонов, локальные оверлеи, — и вердикт стал бы свойством чужого рабочего
	// каталога, а не коммита. Держит это `TestTreeWalkersAskTheIndex`, и он же
	// поймал первую редакцию этой функции.
	tt := newTrackedTree(t, root)

	var out []string
	for rel := range tt.files {
		if filepath.Base(rel) != "go.mod" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("не прочитан %s: %v", rel, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if after, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				out = append(out, strings.TrimSpace(after))
				break
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("в составе дерева не найдено ни одного go.mod — обход пуст, вердикт беспредметен")
	}
	sort.Strings(out)
	return out
}

func moduleImportPath(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("не прочитан go.mod: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(after)
		}
	}
	t.Fatalf("в go.mod нет строки module")
	return ""
}

// isGoIdentifier — строка, которую вообще может звать `go test -fuzz`.
func isGoIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func mustWriteFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
