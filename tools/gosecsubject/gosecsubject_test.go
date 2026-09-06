// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gosecsubject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── РАСПОЗНАВАТЕЛЬ ДИРЕКТИВЫ ────────────────────────────────────────────────
//
// Гейт судит директивы, а значит обязан видеть ИМЕННО ТЕ, что видит gosec, — и
// не видеть того, чего он не видит. Обе стороны проверяются здесь, потому что
// распознаватель, не знающий одной из законных форм, не даёт ни красного, ни
// зелёного: он молчит.

func TestDirectiveFormsGosecReads(t *testing.T) {
	src := `package p

import "os"

func trailing(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec G304 -- хвостовая форма
}

// #nosec G304 -- форма отдельной строкой над узлом
func standalone(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Длинный разбор, занимающий несколько строк, и лишь потом сама директива —
// gosec склеивает группу и ищет тег в начале ЛЮБОЙ строки.
// #nosec G304 -- многострочная форма
func multiline(path string) ([]byte, error) {
	return os.ReadFile(path)
}

//gosec:disable G304 -- второй диалект, который gosec тоже читает
func directiveForm(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// #nosec -- голая форма подавляет ВСЕ правила
func naked(path string) ([]byte, error) {
	return os.ReadFile(path)
}
`
	got, err := FindDirectives("synth/forms.go", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("распознано директив %d, ожидалось 5 — распознаватель не знает "+
			"одной из законных форм: %+v", len(got), got)
	}
	for _, d := range got[:4] {
		if len(d.Rules) != 1 || d.Rules[0] != "G304" {
			t.Errorf("%s:%d: правила разобраны как %v, ожидалось [G304]", d.File, d.Line, d.Rules)
		}
	}
	if len(got[4].Rules) != 1 || got[4].Rules[0] != AllRules {
		t.Errorf("голая директива разобрана как %v, ожидалось [%s] — она подавляет всё",
			got[4].Rules, AllRules)
	}
	if got[0].Reason != "хвостовая форма" {
		t.Errorf("причина разобрана как %q", got[0].Reason)
	}
}

// Законный близнец: те же буквы в ПРОЗЕ директивой не являются. Без этой пробы
// гейт считал бы собственное объяснение предметом — класс, который корпус ловит
// в гейтах по подстроке.
func TestProseMentionIsNotADirective(t *testing.T) {
	src := `package p

// Директива вида #nosec G304 читается только gosec: golangci-lint её не
// разбирает. Здесь она названа, а не применена.
func doc() {}

const help = "передайте // #nosec G304 -- причина, если срабатывание ложно"
`
	got, err := FindDirectives("synth/prose.go", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("распознано %d директив, ожидался ноль — упоминание в прозе и в "+
			"строковом литерале директивой не является: %+v", len(got), got)
	}
}

// ── ПРЕДМЕТ ДИРЕКТИВЫ ───────────────────────────────────────────────────────

const subjectSrc = `package p

import "os"

func live(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec G304 -- предмет живой
}

func dead(path string) ([]byte, error) {
	return os.ReadFile("/etc/hosts") // #nosec G304 -- предмет снят
}
`

// Директива, чьё правило по её координате сработало, — молчит; та, по чьей
// координате находки нет, — находка. Отрицание идёт В ПАРЕ с положительным
// близнецом: без него «ноль находок» зеленело бы и на распознавателе, который
// не видит ничего.
func TestInertDirectiveIsFoundAndLiveOneIsNot(t *testing.T) {
	dirs, err := FindDirectives("synth/subject.go", []byte(subjectSrc))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("распознано %d директив, ожидалось 2", len(dirs))
	}
	// Сканер сообщил находку ТОЛЬКО по первой координате.
	findings := []Finding{{File: "synth/subject.go", Rule: "G304", Start: 6, End: 6}}

	live, inert := SplitBySubject(dirs, findings)
	if len(live) != 1 || !strings.Contains(live[0].Reason, "живой") {
		t.Errorf("с предметом признано %d директив, ожидалась одна (живая): %+v", len(live), live)
	}
	if len(inert) != 1 || !strings.Contains(inert[0].Reason, "снят") {
		t.Fatalf("инертных найдено %d, ожидалась одна (та, чей предмет снят): %+v", len(inert), inert)
	}
	if inert[0].Line == 0 {
		t.Error("находка без координаты — по такой находке искать нечего")
	}
}

// Правило чужой директивы предметом не является: находка G304 не оправдывает
// директиву, подавляющую G115.
func TestFindingOfAnotherRuleIsNotASubject(t *testing.T) {
	dirs, err := FindDirectives("synth/subject.go", []byte(subjectSrc))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	findings := []Finding{{File: "synth/subject.go", Rule: "G115", Start: 6, End: 6}}
	_, inert := SplitBySubject(dirs, findings)
	if len(inert) != 2 {
		t.Fatalf("инертных %d, ожидались обе: находка ЧУЖОГО правила предметом не "+
			"является: %+v", len(inert), inert)
	}
}

// ── ВЕДОМОСТЬ ───────────────────────────────────────────────────────────────

func TestLedgerRequiresAWrittenReason(t *testing.T) {
	_, err := ParseLedger("synth/ledger.tsv", []byte(
		"# шапка\nservices/x/a.go\tG304\t1\t\n"))
	if err == nil {
		t.Fatal("запись без причины принята. Подавление бывает решением, а бывает " +
			"проглоченным отказом; машинно они неразличимы, поэтому причина обязательна")
	}
	if !strings.Contains(err.Error(), "причин") {
		t.Errorf("отказ не называет предмет: %v", err)
	}
}

func TestLedgerEntryWithoutSubjectIsAFinding(t *testing.T) {
	rows, err := ParseLedger("synth/ledger.tsv", []byte(
		"services/x/a.go\tG304\t1\tзаведена вместе с гейтом\n"))
	if err != nil {
		t.Fatalf("разбор ведомости: %v", err)
	}
	// В дереве инертных директив по этой координате больше нет.
	_, stale := ApplyLedger(rows, nil)
	if len(stale) != 1 {
		t.Fatalf("устаревших записей %d, ожидалась одна: записи, которой больше нечего "+
			"исключать, в ведомости не место", len(stale))
	}
	if !strings.Contains(stale[0].Why, "нечего") && !strings.Contains(stale[0].Why, "предмет") {
		t.Errorf("отказ не называет предмет: %q", stale[0].Why)
	}
}

func TestLedgerCountIsExactNotACeiling(t *testing.T) {
	rows, err := ParseLedger("synth/ledger.tsv", []byte(
		"services/x/a.go\tG304\t3\tзаведена вместе с гейтом\n"))
	if err != nil {
		t.Fatalf("разбор ведомости: %v", err)
	}
	inert := []Directive{
		{File: "services/x/a.go", Line: 10, Rules: []string{"G304"}},
		{File: "services/x/a.go", Line: 20, Rules: []string{"G304"}},
	}
	uncovered, stale := ApplyLedger(rows, inert)
	if len(uncovered) != 0 {
		t.Errorf("непокрытых %d, ожидался ноль — все инертные названы записью", len(uncovered))
	}
	if len(stale) != 1 {
		t.Fatalf("устаревших записей %d, ожидалась одна: число в ведомости — ТОЧНОЕ, "+
			"а не потолок. Потолок не краснеет на сокращении долга и потому не истекает", len(stale))
	}
}

func TestLedgerDoesNotExcuseANewDirective(t *testing.T) {
	rows, err := ParseLedger("synth/ledger.tsv", []byte(
		"services/x/a.go\tG304\t1\tзаведена вместе с гейтом\n"))
	if err != nil {
		t.Fatalf("разбор ведомости: %v", err)
	}
	inert := []Directive{
		{File: "services/x/a.go", Line: 10, Rules: []string{"G304"}},
		{File: "services/y/b.go", Line: 7, Rules: []string{"G115"}},
	}
	uncovered, stale := ApplyLedger(rows, inert)
	if len(stale) != 0 {
		t.Errorf("устаревших %d, ожидался ноль: у записи предмет на месте", len(stale))
	}
	if len(uncovered) != 1 || uncovered[0].File != "services/y/b.go" {
		t.Fatalf("непокрытых %d, ожидалась одна — новая директива в другом файле: %+v",
			len(uncovered), uncovered)
	}
}

// ── ТРЕТИЙ ИСХОД: ФАЙЛ ВНЕ ОСМОТРА ──────────────────────────────────────────
//
// Урок соседнего гейта IaC-скана, оплаченный дважды: «находки нет» в файле,
// который сканер НЕ ЧИТАЛ, свидетельствует об отсутствии ОСМОТРА, а не об
// отсутствии предмета. Пробы gosec не читает вовсе (флага -tests нет), и таких
// директив в дереве сотни.
func TestDirectiveInAnUnscannedFileIsNotJudged(t *testing.T) {
	dirs := []Directive{
		{File: "services/x/a.go", Line: 10, Rules: []string{"G304"}},
		{File: "services/x/a_test.go", Line: 12, Rules: []string{"G304"}},
	}
	scanned := map[string]bool{"services/x/a.go": true}

	judged, unjudged := SplitByScanned(dirs, scanned)
	if len(judged) != 1 || judged[0].File != "services/x/a.go" {
		t.Errorf("судимых %d, ожидалась одна: %+v", len(judged), judged)
	}
	if len(unjudged) != 1 || unjudged[0].File != "services/x/a_test.go" {
		t.Fatalf("вне осмотра %d, ожидалась одна. Директива в непрочитанном файле "+
			"не судится: там нет находки, потому что не было ОСМОТРА", len(unjudged))
	}
}

// ── ПРЕДПОСЫЛКА ГЕЙТА ───────────────────────────────────────────────────────
//
// Распознаватель — вторая реализация того, что gosec делает у себя. Разойдись
// они, гейт молча судил бы не тот набор. Сверять есть с чем: сканер печатает
// СВОЁ число найденных директив (Stats.nosec), и оно обязано совпасть с числом,
// которое насчитал гейт по тем же файлам.
func TestRecognizerDisagreementWithScannerRefusesVerdict(t *testing.T) {
	err := CheckRecognizerAgrees("root", 42, 41)
	if err == nil {
		t.Fatal("расхождение с собственным счётом сканера принято молча — значит гейт " +
			"судит не тот набор директив и об этом никто не узнает")
	}
	if !strings.Contains(err.Error(), "41") || !strings.Contains(err.Error(), "42") {
		t.Errorf("отказ не называет обе величины: %v", err)
	}
	if err := CheckRecognizerAgrees("root", 42, 42); err != nil {
		t.Errorf("совпадение объявлено расхождением: %v", err)
	}
}

// Перепись сканера по файлам обязана сойтись с числом строк «Checking file:»:
// если сканер перестанет их печатать, множество осмотренного станет пустым, а
// «вне осмотра» — вердиктом обо всём дереве сразу.
func TestScanLogSilenceRefusesVerdict(t *testing.T) {
	_, err := ParseScanLog(strings.NewReader("[gosec] ничего про файлы\n"), 1192)
	if err == nil {
		t.Fatal("пустой перечень осмотренного принят молча — тогда весь тревожный " +
			"вердикт свёлся бы к «файлы не читались», и это выглядело бы как чистота")
	}
	files, err := ParseScanLog(strings.NewReader(
		"[gosec] Checking file: /r/a.go\n[gosec] Checking file: /r/b.go\n"), 2)
	if err != nil {
		t.Fatalf("законный журнал отвергнут: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("разобрано файлов %d, ожидалось 2", len(files))
	}
}

// ── ПРОВЯЗКА ────────────────────────────────────────────────────────────────

// gatePkg — пакет гейта. Ищется без ведущего «./»: вызывающие пишут путь
// по-разному (`./tools/…` в объявлении конвейера, `"$ROOT/tools/…"` в скрипте),
// и требование одной формы записи проверяло бы стиль, а не провязку.
const gatePkg = "tools/gosecsubject/cmd/verify-gosec-suppression-subject"

// callsOfGate — строки файла, ЗОВУЩИЕ гейт: комментарии отсеиваются, иначе
// проза об этом гейте засчитывалась бы за его вызов. Тот же класс, что гейт по
// подстроке, краснеющий на собственном объяснении.
func callsOfGate(t *testing.T, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		code := line
		if i := strings.Index(code, "#"); i >= 0 {
			code = code[:i]
		}
		if strings.Contains(code, gatePkg) {
			out = append(out, strings.TrimSpace(code))
		}
	}
	return out
}

// Гейт, которого конвейер не зовёт, стоит ровно столько же, сколько его
// отсутствие. Эта пара уже случалась в этом репозитории.
func TestCIRunsThisGate(t *testing.T) {
	calls := callsOfGate(t, ".github/workflows/security-scan.yml")
	if len(calls) == 0 {
		t.Fatalf("security-scan.yml не зовёт %s — провяжи обратно", gatePkg)
	}
	t.Logf("вызовов гейта в объявлении конвейера: %d", len(calls))
}

// Исходов у гейта ТРИ, и вызывающий обязан их различать. `go run` СХЛОПЫВАЕТ
// любой ненулевой код программы в единицу — замерено, а не предположено:
// программа, вышедшая двойкой, доходит до вызывающего единицей, и «гейт не смог
// вынести вердикт» становится неотличимо от «гейт нашёл нарушение».
//
// Цена различения ровно одна строка `go build`, и в дереве это уже сделано ради
// того же свойства (ci.yaml, шаг адъюдикации разрывов контракта).
//
// Проба судит ВЫЗЫВАЮЩЕГО, а не программу: коды возврата у неё свои и
// правильные, но без этой строки их никто не прочитал бы.
func TestCallersDoNotCollapseTheThirdOutcome(t *testing.T) {
	callers := []string{
		".github/workflows/security-scan.yml",
		"scripts/ci-local.sh",
		"scripts/gosec-subject-inject.sh",
	}
	seen := 0
	for _, rel := range callers {
		calls := callsOfGate(t, rel)
		if len(calls) == 0 {
			t.Errorf("%s больше не зовёт гейт — проба о его поведении не утверждает "+
				"ничего, а перечень вызывающих пережил свой предмет", rel)
			continue
		}
		seen += len(calls)
		for _, c := range calls {
			if strings.Contains(c, "go run") {
				t.Errorf("%s зовёт гейт через `go run`: %s\n"+
					"`go run` схлопывает любой ненулевой код в единицу, и третий исход "+
					"(«вердикт не вынесен», код 2) стал бы неотличим от находки. "+
					"Собери бинарь `go build -o …` и зови его.", rel, c)
			}
		}
	}
	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	t.Logf("осмотрено вызывающих %d, строк с вызовом гейта %d", len(callers), seen)
}

// Скан обязан ОСТАВЛЯТЬ то, что читает гейт. Без этой пробы шаг конвейера выше
// звал бы гейт, которому нечего читать, и тот честно отвечал бы «не выполнилось».
func TestScanScriptLeavesTheSuppressionCensus(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "gosec-scan-modules.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-track-suppressions", ManifestName} {
		if !strings.Contains(string(b), want) {
			t.Errorf("scripts/gosec-scan-modules.sh не оставляет %q — гейту нечего будет читать", want)
		}
	}
}

// Отдельным прогоном, а не флагом к основному: с -track-suppressions подавленные
// находки попадают в SARIF с level=error, и основной вердикт покраснел бы на
// чистом дереве. Замерено, а не предположено.
func TestSuppressionPassIsSeparateFromTheBlockingReport(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "gosec-scan-modules.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		code := line
		if i := strings.Index(code, "#"); i >= 0 {
			code = code[:i]
		}
		if !strings.Contains(code, "-track-suppressions") {
			continue
		}
		if strings.Contains(code, "$MERGED") || strings.Contains(code, "-fmt sarif") {
			t.Fatalf("подавления собираются тем же прогоном, что и блокирующий отчёт: %q.\n"+
				"С -track-suppressions подавленные находки уезжают в SARIF с level=error, "+
				"и gosec-verdict.sh покраснел бы на чистом дереве.", strings.TrimSpace(line))
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
