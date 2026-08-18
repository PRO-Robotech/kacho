// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleconfirmprobe_injection_test.go — доказательство, что гейт
// «подтверждение закреплено пробой либо названо» СПОСОБЕН упасть, способен
// смолчать и способен снять СВОЁ ЖЕ послабление.
//
// # Обе стороны и почему одной мало
//
// Дефект и его ЗАКОННЫЙ БЛИЗНЕЦ отличаются одним: у близнеца рядом лежит проба,
// открывающая подтверждение. Всё остальное совпадает — тот же компонент, тот же
// `<Popconfirm>`, тот же путь. Без близнеца гейт ловил бы форму «в файле есть
// подтверждение» и краснел бы на каждом законно покрытом месте; первый же ложный
// срабат такой гейт выключает.
//
// # Третья сторона: послабление обязано истекать САМО
//
// Перечень непокрытых — не разрешение, а долг с адресом. Две пробы ниже требуют,
// чтобы запись падала, когда исключать больше нечего: подтверждение из файла
// ушло либо рядом появилась проба. Без этого запись переживает свой предмет —
// ровно тот класс, который мы ловим в коде.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthConsoleTree — минимальное дерево консоли: компонент с подтверждением и,
// по желанию вызывающего, проба рядом с ним.
func synthConsoleTree(t *testing.T, component, probe string) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"ui-future/mod/src/Thing.tsx": component,
	}
	if probe != "" {
		files["ui-future/mod/src/Thing.test.tsx"] = probe
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

const synthConfirmComponent = "export const Thing = () => (\n" +
	"  <Popconfirm title=\"Снять?\" okText=\"Снять\" onConfirm={drop}>\n" +
	"    <Button />\n" +
	"  </Popconfirm>\n" +
	");\n"

const synthPlainComponent = "export const Thing = () => <Button />;\n"

// synthConfirmProbe — проба, которая ОТКРЫВАЕТ подтверждение и читает его.
const synthConfirmProbe = "it(\"снимает только после согласия\", () => {\n" +
	"  fireEvent.click(screen.getByRole(\"button\"));\n" +
	"  expect(screen.getByRole(\"tooltip\")).toHaveTextContent(\"Снять?\");\n" +
	"});\n"

// synthShallowProbe — проба рядом ЕСТЬ, но подтверждения она не открывает.
// Это и есть тот случай, ради которого признак — открытие, а не наличие файла.
const synthShallowProbe = "it(\"рисуется\", () => {\n" +
	"  expect(screen.getByRole(\"button\")).toBeInTheDocument();\n" +
	"});\n"

func auditSynthConfirm(t *testing.T, root string, roster map[string]string) ([]confirmFinding, confirmCensus) {
	t.Helper()
	findings, census, err := auditConsoleConfirmProbes(root, roster)
	if err != nil {
		t.Fatalf("судья сорвался на синтетическом дереве: %v", err)
	}
	if census.SourcesScanned == 0 {
		t.Fatalf("синтетическое дерево не осмотрено — вердикт на нём недействителен: %s", census)
	}
	return findings, census
}

func joinConfirmFindings(fs []confirmFinding) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}

// TestConfirmGateRedOnUnprobedConfirmation — направление (а): подтверждение есть,
// пробы рядом нет, в перечне место не названо — гейт КРАСНЕЕТ с координатой.
func TestConfirmGateRedOnUnprobedConfirmation(t *testing.T) {
	root := synthConsoleTree(t, synthConfirmComponent, "")

	findings, census := auditSynthConfirm(t, root, map[string]string{})
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("необратимое действие без пробы и без записи, а гейт молчит — "+
			"он не способен упасть.\n%s", census)
	}
	if !strings.Contains(joinConfirmFindings(findings), "ui-future/mod/src/Thing.tsx") {
		t.Fatalf("находка не называет файл — по ней нечего чинить:\n%s", joinConfirmFindings(findings))
	}
}

// TestConfirmGateSilentOnProbedConfirmation — направление (б): тот же компонент,
// то же подтверждение, но рядом проба, ОТКРЫВАЮЩАЯ его. Гейт МОЛЧИТ.
func TestConfirmGateSilentOnProbedConfirmation(t *testing.T) {
	root := synthConsoleTree(t, synthConfirmComponent, synthConfirmProbe)

	findings, census := auditSynthConfirm(t, root, map[string]string{})
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("покрытое место помечено находкой — гейт ловит форму, а не существо:\n%s",
			joinConfirmFindings(findings))
	}
	if census.Covered != 1 {
		t.Fatalf("покрытое место не засчитано покрытым: %s", census)
	}
}

// TestConfirmGateRedOnProbeThatDoesNotOpenTheConfirmation — признак покрытия —
// ОТКРЫТИЕ подтверждения, а не наличие файла рядом.
//
// Без этого различия гейт засчитывал бы любую соседнюю пробу и был бы зелёным
// ровно там, где необратимый шаг никем не проверен.
func TestConfirmGateRedOnProbeThatDoesNotOpenTheConfirmation(t *testing.T) {
	root := synthConsoleTree(t, synthConfirmComponent, synthShallowProbe)

	findings, census := auditSynthConfirm(t, root, map[string]string{})
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("проба рядом есть, но подтверждения не открывает — гейт обязан "+
			"считать место непокрытым.\n%s", census)
	}
}

// TestConfirmRosterEntryExpiresWhenSubjectIsGone — послабление истекает САМО:
// подтверждения в файле больше нет, а запись осталась.
func TestConfirmRosterEntryExpiresWhenSubjectIsGone(t *testing.T) {
	root := synthConsoleTree(t, synthPlainComponent, "")
	roster := map[string]string{"ui-future/mod/src/Thing.tsx": "когда-то было подтверждение"}

	findings, census := auditSynthConfirm(t, root, roster)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("запись перечня осталась без предмета, а гейт молчит — послабление "+
			"переживёт свой предмет.\n%s", census)
	}
	if !strings.Contains(joinConfirmFindings(findings), "снимите запись") {
		t.Fatalf("находка не говорит, что делать:\n%s", joinConfirmFindings(findings))
	}
}

// TestConfirmRosterEntryExpiresWhenProbeArrives — вторая форма самоистечения:
// место покрылось пробой, а запись о непокрытости осталась.
func TestConfirmRosterEntryExpiresWhenProbeArrives(t *testing.T) {
	root := synthConsoleTree(t, synthConfirmComponent, synthConfirmProbe)
	roster := map[string]string{"ui-future/mod/src/Thing.tsx": "пробы нет"}

	findings, census := auditSynthConfirm(t, root, roster)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("место покрыто пробой, а запись о непокрытости осталась — гейт молчит.\n%s", census)
	}
	if !strings.Contains(joinConfirmFindings(findings), "снимите её") {
		t.Fatalf("находка не говорит, что делать:\n%s", joinConfirmFindings(findings))
	}
}

// TestConfirmGateSilentOnEmptyRoster — гейт НЕ ПАДАЕТ на достижении своей цели.
//
// Пустой перечень непокрытых — то, ради чего перечень заведён. Проверка,
// краснеющая на нём, толкала бы держать запись ради зелёного.
func TestConfirmGateSilentOnEmptyRoster(t *testing.T) {
	root := synthConsoleTree(t, synthConfirmComponent, synthConfirmProbe)

	findings, census := auditSynthConfirm(t, root, map[string]string{})
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("пустой перечень непокрытых — цель, а не поломка:\n%s", joinConfirmFindings(findings))
	}
	if census.Rostered != 0 {
		t.Fatalf("перепись врёт про пустой перечень: %s", census)
	}
}
