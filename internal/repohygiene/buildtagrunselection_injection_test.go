// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtagrunselection_injection_test.go — доказательство, что гейт «проба под
// признаком сборки попадает в ОТБОР прогона» СПОСОБЕН упасть и способен
// смолчать.
//
// # Почему инъекция настоящим входом
//
// Предикат гейта — чтение НАСТОЯЩИХ объявлений и РАЗБОР настоящих файлов проб.
// Проба, подсунувшая судье готовый список имён, доказала бы, что список
// содержит то, что в него положили. Здесь поэтому строится настоящее дерево:
// `go.mod`, индекс git (судья спрашивает индекс, а не диск), объявление с
// вызовом и пакет с двумя пробами под признаком.
//
// # Дефект и его законный близнец отличаются ОДНИМ
//
// Дефект: `-run` называет одно имя из двух. Близнец: тот же тег, тот же пакет,
// та же форма файла, тот же вызов — `-run` называет оба. Без близнеца гейт
// ловил бы форму «в вызове есть `-run`» и краснел бы на каждом законном
// сужении; первый же ложный срабат такой гейт выключает.
//
// # Инъекция роняет ТОЛЬКО проверяемое
//
// Форма «завести ещё одну пробу» негодна: новая проба нарушает всё, что
// требуется от проб вообще, и красное пришло бы от соседа. Поэтому здесь
// меняется ОДНО — текст `-run` в объявлении, при неизменном составе дерева.
// Прогонов три: контроль (всё цело — молчат оба гейта) · инъекция нового
// свойства (краснеет только новый) · инъекция старого (краснеет только
// соседний гейт достижимости).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const synthSelectModule = "synthselect"

// synthSelectTree — дерево с одним объявлением и пакетами, несущими ИМЕНОВАННЫЕ
// пробы под признаком.
//
// Признак назван `synthtag`, а не `helmcharts`: настоящий тег дерева здесь
// означал бы, что проба зависит от того, жив ли этот тег. Класс же — про ЛЮБОЙ
// признак сборки и ЛЮБОЕ сужение.
func synthSelectTree(t *testing.T, runFlag string, pkg string, names []string) string {
	t.Helper()
	root := t.TempDir()

	body := "//go:build synthtag\n\npackage " + filepath.Base(pkg) + "\n\nimport \"testing\"\n"
	for _, n := range names {
		body += "\nfunc " + n + "(t *testing.T) { _ = t }\n"
	}
	// Помощник с именем на `Test` и ЧУЖОЙ сигнатурой: `-run` его не отбирает,
	// поэтому требовать его отбора значило бы производить находку, которую
	// нечем закрыть. Он обязан остаться вне переписи.
	body += "\nfunc TestHelperNotAProbe(s string) string { return s }\n"
	// Упоминание имени пробы в комментарии и в литерале: разбор судит УЗЕЛ
	// объявления, поэтому ни то, ни другое пробой не считается.
	body += "\n// func TestOnlyInAComment(t *testing.T) {}\n" +
		"\nvar synthLiteral = \"func TestOnlyInALiteral(t *testing.T) {}\"\n"

	files := map[string]string{
		"go.mod": "module " + synthSelectModule + "\n\ngo 1.24\n",
		".github/workflows/ci.yaml": "jobs:\n  probe:\n    steps:\n" +
			"      # Проза о прогоне: `go test -tags=synthtag ./" + pkg + "/ -run 'TestProse'`\n" +
			"      - name: прогон\n" +
			"        run: go test -tags=synthtag ./" + pkg + "/ " + runFlag + " -count=1\n",
		pkg + "/probe_test.go": body,
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

// auditSynthSelect — ТОТ ЖЕ судья, что судит дерево, плюс проверка предпосылки
// самой инъекции: синтетика обязана быть ПРОЧИТАНА, её признак — РАСПОЗНАН,
// объявление — найдено, а пробы — разобраны. Иначе «ноль находок» на ней не
// значит ничего.
func auditSynthSelect(t *testing.T, root string) ([]tagSelectionFinding, tagSelectionCensus) {
	t.Helper()
	findings, census, err := auditTaggedTestsAreSelected(root, synthSelectModule)
	if err != nil {
		t.Fatalf("судья сорвался на синтетическом дереве: %v", err)
	}
	if census.FilesScanned == 0 || census.FilesWithTag == 0 ||
		census.DeclarationFiles == 0 || census.FuncsUnderTag == 0 {
		t.Fatalf("синтетическое дерево не осмотрено — вердикт на нём недействителен: %s", census)
	}
	return findings, census
}

func joinSelectionFindings(fs []tagSelectionFinding) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}

// пара имён, на которой ставятся все прогоны ниже.
var synthTwoProbes = []string{"TestAlpha", "TestBeta"}

// TestSelectionGateRedOnAProbeTheRunDoesNotName — направление (а): `-run`
// называет одно имя из двух — гейт КРАСНЕЕТ и называет пробу с координатой.
//
// Это ровно тот дефект, ради которого гейт написан (#1678).
func TestSelectionGateRedOnAProbeTheRunDoesNotName(t *testing.T) {
	root := synthSelectTree(t, "-run 'TestAlpha'", "deploy", synthTwoProbes)

	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("проба вне отбора, а гейт молчит — он не способен упасть.\n%s", census)
	}
	joined := joinSelectionFindings(findings)
	if !strings.Contains(joined, "TestBeta") {
		t.Fatalf("находка не называет пробу — по ней нечего чинить:\n%s", joined)
	}
	if strings.Contains(joined, "TestAlpha:") {
		t.Fatalf("отобранная проба помечена находкой — гейт судит форму, а не отбор:\n%s", joined)
	}
	if !strings.Contains(joined, "probe_test.go:") {
		t.Fatalf("находка не называет координату:\n%s", joined)
	}
	if !strings.Contains(joined, "ci.yaml:") {
		t.Fatalf("находка не называет рассмотренный прогон — её нечем опровергнуть:\n%s", joined)
	}
	// Перепись обязана печатать ОБЕ величины: одно число скрывает ровно этот случай.
	if !strings.Contains(census.String(), "проб под признаком 2") ||
		!strings.Contains(census.String(), "отбирается 1") {
		t.Fatalf("перепись не называет обе величины — «ноль находок» неотличимо от "+
			"«ноль прочитанного»: %s", census)
	}
}

// TestSelectionGateSilentWhenTheRunNamesEveryProbe — направление (б): законный
// близнец. Всё то же самое, `-run` называет ОБА имени — гейт МОЛЧИТ.
func TestSelectionGateSilentWhenTheRunNamesEveryProbe(t *testing.T) {
	root := synthSelectTree(t, "-run 'TestAlpha|TestBeta'", "deploy", synthTwoProbes)

	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("сужение перечисляет все пробы, а гейт краснеет — он ловит форму "+
			"«есть -run», а не существо:\n%s", joinSelectionFindings(findings))
	}
	if census.FuncsSelected != 2 {
		t.Fatalf("отобранных проб %d, ожидалось 2: %s", census.FuncsSelected, census)
	}
}

// TestSelectionGateReadsAnAbsentRunAsAll — пустой `-run` означает «все», а не
// «никого».
//
// Это тот вид ручки, о котором предупреждает корпус: пустое значение обязано
// читаться в ту сторону, в какую его читает исполнитель. Прочти гейт отсутствие
// `-run` как «не отбирает никого» — он краснел бы на КАЖДОМ пакете под
// признаком, то есть был бы отключён в первый же день.
func TestSelectionGateReadsAnAbsentRunAsAll(t *testing.T) {
	root := synthSelectTree(t, "", "deploy", synthTwoProbes)

	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("вызов без `-run` исполняет все пробы пакета, а гейт считает их "+
			"неотобранными:\n%s", joinSelectionFindings(findings))
	}
	if census.FuncsSelected != 2 {
		t.Fatalf("отобранных проб %d, ожидалось 2: %s", census.FuncsSelected, census)
	}
}

// TestSelectionGateRecognisesEveryLegalFormOfTheFlag — распознаватель знает ВСЕ
// законные формы записи сужения.
//
// Форма, о которой распознаватель не знает, делает сужение НЕВИДИМЫМ, а не
// редким: проба под ним оказывается вне наблюдения, и гейт молчит — ни красного,
// ни зелёного (`testing.md` §«Гейт на класс» п.7). Поэтому каждая форма
// проверяется своим прогоном, а не одной.
func TestSelectionGateRecognisesEveryLegalFormOfTheFlag(t *testing.T) {
	forms := []struct{ name, flag string }{
		{"кавычки одинарные", "-run 'TestAlpha'"},
		{"кавычки двойные", `-run "TestAlpha"`},
		{"через знак равенства", "-run=TestAlpha"},
		{"голое значение", "-run TestAlpha"},
		{"полное имя флага", "-test.run='TestAlpha'"},
		{"вложенная проба через слэш", "-run 'TestAlpha/podprobe'"},
		{"повторённый флаг — последний побеждает", "-run 'TestAlpha|TestBeta' -run 'TestAlpha'"},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			root := synthSelectTree(t, f.flag, "deploy", synthTwoProbes)
			findings, census := auditSynthSelect(t, root)
			t.Log(census.String())

			if len(findings) == 0 {
				t.Fatalf("форма %q не распознана — сужение стало НЕВИДИМЫМ, и проба под "+
					"ним вне наблюдения.\n%s", f.flag, census)
			}
			if !strings.Contains(joinSelectionFindings(findings), "TestBeta") {
				t.Fatalf("находка не называет непопавшую пробу:\n%s",
					joinSelectionFindings(findings))
			}
		})
	}
}

// TestSelectionGateRefusesAnUnreadablePattern — нечитаемый образец даёт
// НАХОДКУ, а не молчание.
//
// Послабление «не смогли прочесть — считаем, что берёт всё» зеленело бы ровно
// там, где объявление сломано. Направление отказа обязано быть обратным.
func TestSelectionGateRefusesAnUnreadablePattern(t *testing.T) {
	root := synthSelectTree(t, "-run 'Test(Alpha'", "deploy", synthTwoProbes)

	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if len(findings) != 2 {
		t.Fatalf("образец не компилируется, значит отбор не доказан НИ для одной пробы; "+
			"находок %d, ожидалось 2.\n%s", len(findings), census)
	}
	joined := joinSelectionFindings(findings)
	if !strings.Contains(joined, "Test(Alpha") {
		t.Fatalf("находка не показывает образец из объявления — читателю нечего чинить:\n%s",
			joined)
	}
}

// TestSelectionGateAppliesTheDeclaredSkip — `-skip` тоже сужает, и гейт читает
// его так же, как исполнитель.
//
// В дереве `-skip` сегодня не встречается ни в одном вызове `go test`. Форма
// закрыта ВПЕРЁД: она законна, и появись она — проба, исключённая ею, оказалась
// бы вне наблюдения молча, ровно как это случилось с `-run`.
func TestSelectionGateAppliesTheDeclaredSkip(t *testing.T) {
	root := synthSelectTree(t, "-run 'TestAlpha|TestBeta' -skip 'TestBeta'", "deploy", synthTwoProbes)

	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("проба исключена `-skip`, а гейт считает её отобранной.\n%s", census)
	}
	if !strings.Contains(joinSelectionFindings(findings), "TestBeta") {
		t.Fatalf("находка не называет исключённую пробу:\n%s", joinSelectionFindings(findings))
	}
}

// TestSelectionGateCountsOnlyRealProbes — под перепись попадает объявление
// пробы, а не всякое упоминание её формы.
//
// Синтетика несёт три ловушки сразу: помощник с именем на `Test` и чужой
// сигнатурой, закомментированное объявление и строковый литерал. Все три —
// законные жители этого дерева (в пакете гейтов литералы с `func Test` есть), и
// гейт по образцу над текстом требовал бы их отбора, то есть производил бы
// находки, которые нечем закрыть.
func TestSelectionGateCountsOnlyRealProbes(t *testing.T) {
	root := synthSelectTree(t, "-run 'TestAlpha|TestBeta'", "deploy", synthTwoProbes)

	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if census.FuncsUnderTag != 2 {
		t.Fatalf("проб под признаком насчитано %d, а объявлено 2: под перепись попало "+
			"то, что пробой не является: %s", census.FuncsUnderTag, census)
	}
	if len(findings) != 0 {
		t.Fatalf("находки на дереве без дефекта:\n%s", joinSelectionFindings(findings))
	}
}

// TestSelectionGateDoesNotCountACommentAsARun — прогоном является ВЫЗОВ, а не
// проза о нём.
//
// В синтетике комментарий несёт `-tags=synthtag` и `-run 'TestProse'` дословно —
// ровно как в настоящем `ci.yaml`, где абзац рядом с вызовом объясняет его и
// содержит те же слова. Гейт по сырому тексту принял бы прозу за второй прогон
// и счёл бы `TestProse` неотобранной пробой, а `TestBeta` — покрытой.
func TestSelectionGateDoesNotCountACommentAsARun(t *testing.T) {
	root := synthSelectTree(t, "-run 'TestAlpha'", "deploy", synthTwoProbes)

	_, census := auditSynthSelect(t, root)
	t.Log(census.String())

	if census.RunsFound != 1 {
		t.Fatalf("прогонов найдено %d, а вызов в дереве один — проза принята за вызов: %s",
			census.RunsFound, census)
	}
}

// TestSelectionGateLeavesTheUnreachedPackageToItsOwner — граница предмета.
//
// Пакет ВНЕ области всякого прогона — предмет соседнего гейта достижимости, и
// он о нём уже говорит. Этот гейт обязан молчать: две находки на один дефект
// заставляют читателя чинить дважды, а починка нужна одна.
//
// Прогон третий из трёх, предписанных корпусом: инъекция СТАРОГО свойства
// обязана ронять существующий контроль и НЕ ронять новый. Без него молчание
// соседнего гейта неотличимо от молчания мёртвого.
func TestSelectionGateLeavesTheUnreachedPackageToItsOwner(t *testing.T) {
	root := synthSelectTree(t, "-run 'TestAlpha|TestBeta'", "internal/apps/thing", synthTwoProbes)
	// Объявление называет область `./internal/apps/thing/`, поэтому уведём вызов
	// в сторону: пакет останется под признаком, а прогон перестанет его покрывать.
	ci := filepath.Join(root, ".github", "workflows", "ci.yaml")
	raw, err := os.ReadFile(ci) // #nosec G304 — путь внутри временного дерева пробы
	if err != nil {
		t.Fatalf("чтение синтетического объявления: %v", err)
	}
	moved := strings.ReplaceAll(string(raw), "./internal/apps/thing/", "./services/x/internal/repo/")
	if moved == string(raw) {
		t.Fatal("предпосылка инъекции не выполнена: область в объявлении не заменилась")
	}
	if err := os.WriteFile(ci, []byte(moved), 0o644); err != nil {
		t.Fatalf("запись синтетического объявления: %v", err)
	}
	synthTrack(t, root)

	// новый гейт — МОЛЧИТ: это чужой предмет.
	findings, census := auditSynthSelect(t, root)
	t.Log(census.String())
	if len(findings) != 0 {
		t.Fatalf("пакет вне всякой области — предмет соседнего гейта, а этот выдал "+
			"находку: один дефект стал двумя:\n%s", joinSelectionFindings(findings))
	}

	// соседний гейт — КРАСНЕЕТ: контроль жив, а не мёртв.
	reach, reachCensus, err := auditTaggedPackagesAreExecuted(root, synthSelectModule)
	if err != nil {
		t.Fatalf("соседний судья сорвался: %v", err)
	}
	t.Log(reachCensus.String())
	if len(reach) == 0 {
		t.Fatalf("пакет вне всякой области, а гейт достижимости молчит — его молчание "+
			"в прогонах выше означало бы «контроль мёртв», а не «нарушения нет».\n%s",
			reachCensus)
	}
}
