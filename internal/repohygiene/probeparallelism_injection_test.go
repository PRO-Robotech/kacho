// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Инъекция гейта параллелизма проб: обе стороны гоняют ТУ ЖЕ функцию
// [AuditProbeParallelism], что и вердикт по дереву. Подменяется ровно один вход —
// откуда взят перечень файлов; признак, разбор и ведомость общие.
//
// Каждый случай отличается от своего законного близнеца ОДНИМ фактом, и
// вычисляется эта разница из самих фикстур, а не объявляется в комментарии.

// Фикстуры параметризованы ИМЕНЕМ пробы: в одном пакете Go двух одноимённых
// функций не бывает, а ведомость ключуется парой «каталог + имя». Синтетика с
// повторяющимся именем описывала бы мир, которого не существует, и разбор
// сводил бы две пробы в одну — молча.

// injParallel — проба, объявившая себя параллельной.
func injParallel(name string) string {
	return "package x\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {\n\tt.Parallel()\n\t_ = 1\n}\n"
}

// injSequential — та же проба без объявления. Отличается ОДНИМ фактом.
func injSequential(name string) string {
	return "package x\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {\n\t_ = 1\n}\n"
}

// injLateParallel — тот же вызов, но НЕ первым оператором: подготовка осталась
// последовательной, значит «объявлено» не означает «исполняется параллельно».
func injLateParallel(name string) string {
	return "package x\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {\n\t_ = 1\n\tt.Parallel()\n}\n"
}

// injProbeInsideLiteral — ЗАКОННЫЙ БЛИЗНЕЦ формы: объявление пробы внутри
// строкового литерала фикстуры. Поиск по образцу признал бы его пробой и
// потребовал объявления от текста, который никто не исполняет.
func injProbeInsideLiteral(name string) string {
	return "package x\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {\n" +
		"\tt.Parallel()\n\tconst fixture = `\nfunc TestInsideLiteral(t *testing.T) {\n\t_ = 1\n}\n`\n" +
		"\t_ = fixture\n}\n"
}

// injTestMain — ЗАКОННЫЙ БЛИЗНЕЦ: TestMain пробой не является, параллелизма у
// него нет — он запускает прогон.
const injTestMain = `package x

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(m.Run()) }
`

// injectParallelismTree — синтетический каталог проб; возвращает вход разбора.
func injectParallelismTree(t *testing.T, files map[string]string, allow ...SequentialProbeAllowance) ProbeParallelismOptions {
	t.Helper()
	root := t.TempDir()
	dir := "internal/repohygiene"
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
	}
	return ProbeParallelismOptions{
		Root:  root,
		Dir:   dir,
		Allow: allow,
		Index: func(base string) ([]string, error) {
			var out []string
			for rel := range files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if strings.HasPrefix(abs, base+string(filepath.Separator)) &&
					strings.HasSuffix(abs, "_test.go") {
					out = append(out, abs)
				}
			}
			sort.Strings(out)
			return out, nil
		},
	}
}

func injectParallelismFindings(t *testing.T, o ProbeParallelismOptions) ([]string, ProbeParallelismCensus) {
	t.Helper()
	findings, census, err := AuditProbeParallelism(o)
	if err != nil {
		t.Fatalf("разбор синтетического каталога: %v", err)
	}
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		got = append(got, f.String())
	}
	return got, census
}

// TestParallelismGateRedOnAnUndeclaredSequentialProbe — проба без объявления и
// без записи ведомости краснит гейт И названа по имени.
func TestParallelismGateRedOnAnUndeclaredSequentialProbe(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/a_test.go": injSequential("TestAlpha"),
		"internal/repohygiene/b_test.go": injParallel("TestBeta"),
	}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "internal/repohygiene TestAlpha") {
		t.Errorf("находка обязана называть координату пробы, получено: %s", got[0])
	}
	if census.Probes != 2 || census.Parallel != 1 {
		t.Errorf("перепись обязана считать обе пробы: %s", census)
	}
}

// TestParallelismGateSilentOnADeclaredParallelProbe — ЗАКОННЫЙ БЛИЗНЕЦ: тот же
// каталог, отличается ОДНИМ фактом — проба объявила себя параллельной.
func TestParallelismGateSilentOnADeclaredParallelProbe(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/a_test.go": injParallel("TestAlpha"),
		"internal/repohygiene/b_test.go": injParallel("TestBeta"),
	}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 0 {
		t.Fatalf("законный близнец обязан молчать, получено: %v", got)
	}
	if census.Parallel != 2 {
		t.Errorf("перепись обязана насчитать две параллельные пробы: %s", census)
	}
}

// TestParallelismGateSilentOnALedgeredSequentialProbe — вторая законная сторона:
// та же последовательная проба, но названная в ведомости.
func TestParallelismGateSilentOnALedgeredSequentialProbe(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/a_test.go": injSequential("TestAlpha"),
		"internal/repohygiene/b_test.go": injParallel("TestBeta"),
	}, SequentialProbeAllowance{Dir: "internal/repohygiene", Probe: "TestAlpha", Why: "проба синтетическая"}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 0 {
		t.Fatalf("запись ведомости обязана снимать находку, получено: %v", got)
	}
	if census.Sequential != 1 {
		t.Errorf("перепись обязана насчитать одну последовательную по ведомости: %s", census)
	}
}

// TestParallelismGateRedOnALedgerEntryWithoutItsProbe — САМОИСТЕЧЕНИЕ: записи
// больше нечего исключать.
func TestParallelismGateRedOnALedgerEntryWithoutItsProbe(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/b_test.go": injParallel("TestBeta"),
	}, SequentialProbeAllowance{Dir: "internal/repohygiene", Probe: "TestGone", Why: "проба снята"}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 1 || !strings.Contains(got[0], "TestGone") {
		t.Fatalf("истёкшая запись обязана быть находкой и назвать себя, получено: %v", got)
	}
}

// TestParallelismGateRedOnALedgerEntryOverAParallelProbe — вторая половина
// самоистечения: проба стала параллельной, исключать записи нечего.
func TestParallelismGateRedOnALedgerEntryOverAParallelProbe(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/a_test.go": injParallel("TestAlpha"),
	}, SequentialProbeAllowance{Dir: "internal/repohygiene", Probe: "TestAlpha", Why: "проба когда-то была последовательной"}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 1 || !strings.Contains(got[0], "TestAlpha") {
		t.Fatalf("запись на параллельной пробе обязана быть находкой, получено: %v", got)
	}
}

// TestParallelismGateRedOnParallelDeclaredAfterSetup — объявление не первым
// оператором: подготовка осталась последовательной.
func TestParallelismGateRedOnParallelDeclaredAfterSetup(t *testing.T) {
	t.Parallel()
	got, _ := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/a_test.go": injLateParallel("TestAlpha"),
		"internal/repohygiene/b_test.go": injParallel("TestBeta"),
	}))
	if len(got) != 1 || !strings.Contains(got[0], "TestAlpha") {
		t.Fatalf("позднее объявление обязано быть находкой, получено: %v", got)
	}
}

// TestParallelismGateReadsTheParseTreeNotTheText — законный близнец, который
// поиск по образцу отличить НЕ МОЖЕТ: та же строка внутри строкового литерала.
func TestParallelismGateReadsTheParseTreeNotTheText(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/a_test.go": injProbeInsideLiteral("TestAlpha"),
	}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 0 {
		t.Fatalf("проба внутри строкового литерала пробой не является, получено: %v", got)
	}
	if census.Probes != 1 {
		t.Errorf("осмотрена обязана быть ровно одна настоящая проба: %s", census)
	}
}

// TestParallelismGateDoesNotDemandParallelismFromTestMain — TestMain пробой не
// является.
func TestParallelismGateDoesNotDemandParallelismFromTestMain(t *testing.T) {
	t.Parallel()
	got, census := injectParallelismFindings(t, injectParallelismTree(t, map[string]string{
		"internal/repohygiene/main_test.go": injTestMain,
		"internal/repohygiene/b_test.go":    injParallel("TestBeta"),
	}))
	t.Logf("%s; находок %d", census, len(got))
	if len(got) != 0 {
		t.Fatalf("TestMain объявления не требует, получено: %v", got)
	}
	if census.Probes != 1 {
		t.Errorf("TestMain не должен попадать в перепись проб: %s", census)
	}
}

// TestParallelismGateRefusesAnEmptyCorpus — пустой обход есть ОТКАЗ, а не
// зелёное: «находок ноль» на «прочитано ноль» неотличимо от чистого дерева.
func TestParallelismGateRefusesAnEmptyCorpus(t *testing.T) {
	t.Parallel()
	_, _, err := AuditProbeParallelism(injectParallelismTree(t, map[string]string{}))
	if err == nil {
		t.Fatal("пустой обход обязан быть отказом, получен успех")
	}
	if !strings.Contains(err.Error(), "ноль прочитанного") {
		t.Errorf("отказ обязан называть предмет, получено: %v", err)
	}
}
