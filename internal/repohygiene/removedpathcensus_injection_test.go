// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// removedpathcensus_injection_test.go — доказательство того, что перепись снятых
// путей УМЕЕТ краснеть и УМЕЕТ молчать.
//
// # Почему инъекция гоняет настоящий репозиторий, а не только чистую функцию
//
// Половина предмета переписи живёт не в суждении, а в ДОБЫЧЕ входа:
// распознавание переименований (`-M`), форма `base...HEAD`, разбор источника на
// БАЗОВОЙ ревизии и отказ на пустом составе базы. Инъекция, подающая срез строк
// прямо в [judgeRemovedPathCensus], доказала бы свойство одной половины и
// промолчала бы о второй.
//
// # Одно-фактность
//
// Каждый мир отличается от своего близнеца РОВНО ОДНИМ названным фактом, иначе
// неизвестно, что дало красное:
//
//	контроль             → носители A и B на месте, надгробия нет        → молчание
//	снятие молча         → …тот же мир, но A СНЯТ                        → находка с именем A
//	объявлено точно      → …тот же мир, но снятие A объявлено путём      → молчание
//	объявлено приставкой → …тот же мир, но снятие A объявлено приставкой → молчание
//	переименование       → …тот же мир, но A ПЕРЕИМЕНОВАН, а не снят     → молчание
//	не носитель          → …тот же мир, но снят файл, дерева НЕ читающий → молчание
//	носитель корпуса     → …тот же мир, но снят носитель ИЗ корпуса      → молчание (судит сосед)
//	живая приставка      → контроль, но надгробие числит снятой живую     → находка
//	приставка без снятий → контроль, но приставка не снимала НИЧЕГО      → находка
//	бланкетная приставка → …тот же мир, но приставка шире снятого        → находка
//	приставка без причины→ снятие объявлено, но причина пуста            → находка
//	приставка без преемника→ снятие объявлено, преемник пуст             → находка
//
// # Изоляция
//
// Репозиторий заводится в `t.TempDir()` и вызывается через `pkg/gitenv`: без
// снятия `GIT_DIR` проба под хуком отправки работала бы с живым клоном и писала
// бы его индекс.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// injCensusCarrierSrc — тело синтетического НОСИТЕЛЯ: проба, читающая дерево.
// Признак — вызов помощника корня, то есть УЗЕЛ разбора, а не слово.
const injCensusCarrierSrc = `package demo

import "testing"

func Test%s(t *testing.T) {
	root := repoRoot(t)
	_ = root
}
`

// injCensusPlainSrc — тело обычной продуктовой пробы: дерева не читает.
// Законный близнец носителя, отличающийся ровно одним фактом.
const injCensusPlainSrc = `package demo

import "testing"

func Test%s(t *testing.T) { _ = t }
`

// injCensusRepo — синтетическое дерево: два носителя вне корпуса, одна обычная
// проба, один носитель ВНУТРИ корпуса. Один коммит.
func injCensusRepo(t *testing.T) (root, base string) {
	t.Helper()
	root = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	run("config", "user.email", "census@example.invalid")
	run("config", "user.name", "census")

	injWrite(t, root, "go.mod", "module synthetic\n\ngo 1.22\n")
	injWrite(t, root, "services/demo/alpha_test.go", strings.Replace(injCensusCarrierSrc, "%s", "Alpha", 1))
	injWrite(t, root, "services/demo/beta_test.go", strings.Replace(injCensusCarrierSrc, "%s", "Beta", 1))
	injWrite(t, root, "services/demo/plain_test.go", strings.Replace(injCensusPlainSrc, "%s", "Plain", 1))
	injWrite(t, root, gateCorpusDir+"/corpus_test.go",
		strings.Replace(strings.Replace(injCensusCarrierSrc, "package demo", "package repohygiene", 1),
			"%s", "Corpus", 1))
	run("add", "-A")
	run("commit", "--quiet", "-m", "дерево: два носителя вне корпуса, один в корпусе, одна обычная проба")

	out, err := gitenv.Command(root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return root, strings.TrimSpace(string(out))
}

// injCensusJudge — весь путь переписи на синтетическом дереве: добыча входа плюс
// суждение. ТЕ ЖЕ функции, что зовёт держатель по дереву.
func injCensusJudge(t *testing.T, root, base string) ([]string, removedPathCensusCounts) {
	t.Helper()
	if _, err := baseTreeSize(root, base); err != nil {
		t.Fatalf("предпосылка: %v", err)
	}
	names, err := removedPathsBetween(root, base)
	if err != nil {
		t.Fatalf("снятые: %v", err)
	}
	removed, err := classifyRemovedPaths(root, base, names)
	if err != nil {
		t.Fatalf("разбор снятого: %v", err)
	}
	ledger, err := readGateCarrierLedger(root)
	if err != nil {
		t.Fatalf("надгробие: %v", err)
	}
	findings, counts := judgeRemovedPathCensus(removed, ledger, scopeProbes{
		LiveUnder:   func(s string) (int, error) { return livePathsUnder(root, s) },
		EverRemoved: func(s string) (bool, error) { return carrierWasEverRemoved(root, s) },
	})
	t.Logf("осмотрено: %s", counts)
	return findings, counts
}

// injCensusLedger кладёт надгробие обеих форм в корпус синтетического дерева.
func injCensusLedger(t *testing.T, root string, exact []GateCarrierRetirement, scopes []GateCarrierScopeRetirement) {
	t.Helper()
	b, err := json.MarshalIndent(gateCarrierLedger{Retired: exact, RetiredScopes: scopes}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	injWrite(t, root, gateCorpusDir+"/"+gateCarrierLedgerName, string(b)+"\n")
}

func injCensusRm(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

// ── Контроль: всё цело → молчание, и перепись НЕ пуста ──────────────────────

func TestRemovedPathCensusControlIsSilentAndNotVacuous(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	// Один коммит поверх, ничего не снимающий: дельта пуста — законный зелёный.
	injWrite(t, root, "services/demo/gamma_test.go", strings.Replace(injCensusPlainSrc, "%s", "Gamma", 1))
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "добавлено, ничего не снято")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 0 {
		t.Fatalf("контроль: ожидалось молчание, получено %v", got)
	}
	if counts.Removed != 0 {
		t.Fatalf("контроль: снятых ожидалось 0, получено %d", counts.Removed)
	}

	// Контроль предиката в обратную сторону: признак носителя ОБЯЗАН находить
	// предмет в этом же дереве, иначе молчание выше ничего не значит.
	live, err := liveGateCarrierCount(root)
	if err != nil {
		t.Fatalf("перепись живых носителей: %v", err)
	}
	if live.Total == 0 || live.OutsideCorpus == 0 {
		t.Fatalf("признак носителя не сработал ни на чём: %+v — вердикт беспредметен", live)
	}
	t.Logf("контроль предиката: носителей %d, вне корпуса %d", live.Total, live.OutsideCorpus)
}

// ── Внесённый факт: носитель снят и не объявлен → находка С ИМЕНЕМ ──────────

func TestRemovedPathCensusFindsASilentlyRemovedCarrier(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusRm(t, root, "services/demo/alpha_test.go")
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снят носитель")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 1 {
		t.Fatalf("ожидалась одна находка, получено %v", got)
	}
	if !strings.Contains(got[0], "services/demo/alpha_test.go") {
		t.Fatalf("находка не называет носителя: %s", got[0])
	}
	if !strings.Contains(got[0], "снят МОЛЧА") {
		t.Fatalf("находка не называет предмет: %s", got[0])
	}
	if counts.Unexplained != 1 || counts.Carriers != 1 || counts.Assertions != 1 {
		t.Fatalf("перепись расходится с находкой: %+v", counts)
	}
}

// ── Законный близнец 1: снятие объявлено ТОЧНЫМ путём → молчание ────────────

func TestRemovedPathCensusIsSilentWhenRemovalIsDeclaredByPath(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusRm(t, root, "services/demo/alpha_test.go")
	injCensusLedger(t, root, []GateCarrierRetirement{{
		Carrier:   "services/demo/alpha_test.go",
		Reason:    "предмет закрыт соседним гейтом",
		Successor: "services/demo/beta_test.go",
	}}, nil)
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снят носитель, снятие объявлено путём")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 0 {
		t.Fatalf("ожидалось молчание, получено %v", got)
	}
	if counts.Covered != 1 || counts.Unexplained != 0 {
		t.Fatalf("перепись расходится с молчанием: %+v", counts)
	}
}

// ── Законный близнец 2: снятие объявлено ПРИСТАВКОЙ → молчание ─────────────

func TestRemovedPathCensusIsSilentWhenRemovalIsDeclaredByScope(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	// Снимается ВЕСЬ каталог: ровно тот случай, ради которого заведена
	// приставка — записи на каждый файл были бы перечнем имён.
	for _, f := range []string{"alpha_test.go", "beta_test.go", "plain_test.go"} {
		injCensusRm(t, root, "services/demo/"+f)
	}
	injCensusLedger(t, root, nil, []GateCarrierScopeRetirement{{
		Scope:     "services/demo",
		Reason:    "служба вынесена в отдельный репозиторий",
		Successor: "гейты уехали вместе со службой и живут в её репозитории",
	}})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снят каталог, снятие объявлено приставкой")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 0 {
		t.Fatalf("ожидалось молчание, получено %v", got)
	}
	if counts.Removed != 3 || counts.Carriers != 2 || counts.Covered != 2 || counts.Unexplained != 0 {
		t.Fatalf("перепись расходится с молчанием: %+v", counts)
	}
	if counts.DeclaredScopes != 1 {
		t.Fatalf("приставка не зачтена: %+v", counts)
	}
}

// ── Переименование — не снятие ─────────────────────────────────────────────

func TestRemovedPathCensusIsSilentOnARename(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injGit(t, root, "mv", "services/demo/alpha_test.go", "services/demo/alpha_renamed_test.go")
	injGit(t, root, "commit", "--quiet", "-m", "носитель переименован")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 0 {
		t.Fatalf("переименование прочитано как снятие: %v", got)
	}
	if counts.Removed != 0 {
		t.Fatalf("переименование попало в снятые: %+v", counts)
	}
}

// ── Снят НЕ носитель → считается, но находкой не является ──────────────────

func TestRemovedPathCensusCountsButDoesNotFindAPlainTestRemoval(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusRm(t, root, "services/demo/plain_test.go")
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снята обычная проба")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 0 {
		t.Fatalf("обычная проба прочитана как носитель: %v", got)
	}
	if counts.Removed != 1 || counts.Carriers != 0 {
		t.Fatalf("перепись не назвала снятое: %+v", counts)
	}
}

// ── Снят носитель КОРПУСА → молчание здесь, но он ПОСЧИТАН ────────────────
//
// Границы предметов: корпус судит соседний разбор. Перепись обязана его
// сосчитать, иначе «ноль находок» означало бы «не смотрели».

func TestRemovedPathCensusLeavesCorpusCarriersToTheNeighbour(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusRm(t, root, gateCorpusDir+"/corpus_test.go")
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снят носитель корпуса")

	got, counts := injCensusJudge(t, root, base)
	if len(got) != 0 {
		t.Fatalf("носитель корпуса судится дважды: %v", got)
	}
	if counts.Carriers != 1 || counts.CarriersInCorpus != 1 || counts.Unexplained != 0 {
		t.Fatalf("носитель корпуса не посчитан: %+v", counts)
	}
}

// ── Самоистечение приставки, направление первое: под ней ЖИВЫЕ пути ────────

func TestRemovedPathCensusScopeExpiresWhenPathsUnderItAreAlive(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusRm(t, root, "services/demo/alpha_test.go")
	injCensusLedger(t, root, nil, []GateCarrierScopeRetirement{{
		Scope:     "services/demo",
		Reason:    "служба вынесена",
		Successor: "гейты уехали с ней",
	}})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "приставка объявлена, а под ней ещё живут пути")

	// Близнец этого мира — TestRemovedPathCensusIsSilentWhenRemovalIsDeclaredByPath:
	// снято то же самое, отличие ровно одно — ФОРМА записи (приставка вместо
	// точного пути), и приставка взята шире снятого.
	got, _ := injCensusJudge(t, root, base)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "ЕЩЁ") || !strings.Contains(joined, "services/demo") {
		t.Fatalf("находка не называет предмет: %s", joined)
	}
	// Отвергнутая запись НЕ покрывает ничего — иначе неполное объявление
	// работало бы как полное, и запись, которую гейт только что назвал
	// негодной, продолжала бы прятать снятое.
	if !strings.Contains(joined, "снят МОЛЧА") {
		t.Fatalf("отвергнутая запись всё ещё покрывает снятое: %s", joined)
	}
	if len(got) != 2 {
		t.Fatalf("ожидались две находки (истечение записи + непокрытое снятие), получено %v", got)
	}
}

// ── Бланкетная приставка отвергается ТЕМ ЖЕ предикатом ────────────────────
//
// Названо отдельной пробой, потому что это единственная защита от записи,
// покрывающей снятие у всех сервисов разом. Одно-фактное отличие от близнеца
// TestRemovedPathCensusIsSilentWhenRemovalIsDeclaredByScope — ШИРИНА приставки.

func TestRemovedPathCensusRefusesABlanketScope(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusRm(t, root, "services/demo/alpha_test.go")
	injCensusLedger(t, root, nil, []GateCarrierScopeRetirement{{
		Scope:     "services",
		Reason:    "бланкет: попытка покрыть снятие у всех сервисов разом",
		Successor: "никто",
	}})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "бланкетная приставка")

	got, _ := injCensusJudge(t, root, base)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, `"services"`) || !strings.Contains(joined, "ЕЩЁ") {
		t.Fatalf("бланкетная приставка принята: %s", joined)
	}
	if !strings.Contains(joined, "снят МОЛЧА") {
		t.Fatalf("отвергнутый бланкет всё ещё покрывает снятое: %s", joined)
	}
}

// ── Самоистечение приставки, направление второе: под ней НИЧЕГО не снимали ─

func TestRemovedPathCensusScopeExpiresWhenNothingWasRemoved(t *testing.T) {
	t.Parallel()
	root, base := injCensusRepo(t)
	injCensusLedger(t, root, nil, []GateCarrierScopeRetirement{{
		Scope:     "services/never",
		Reason:    "выдуманное снятие",
		Successor: "никто",
	}})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "приставка, под которой никогда ничего не снимали")

	got, _ := injCensusJudge(t, root, base)
	if len(got) != 1 {
		t.Fatalf("ожидалась одна находка, получено %v", got)
	}
	if !strings.Contains(got[0], "не снимали НИЧЕГО") {
		t.Fatalf("находка не называет предмет: %s", got[0])
	}
}

// ── Запись без причины и без преемника — не объявление ─────────────────────

func TestRemovedPathCensusScopeWithoutReasonOrSuccessorDeclaresNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		row  GateCarrierScopeRetirement
		want string
	}{
		{"без причины", GateCarrierScopeRetirement{
			Scope: "services/demo", Successor: "уехали со службой"}, "без причины"},
		{"без преемника", GateCarrierScopeRetirement{
			Scope: "services/demo", Reason: "служба вынесена"}, "что стало с предметом"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, base := injCensusRepo(t)
			for _, f := range []string{"alpha_test.go", "beta_test.go", "plain_test.go"} {
				injCensusRm(t, root, "services/demo/"+f)
			}
			injCensusLedger(t, root, nil, []GateCarrierScopeRetirement{tc.row})
			injGit(t, root, "add", "-A")
			injGit(t, root, "commit", "--quiet", "-m", "неполная запись")

			got, _ := injCensusJudge(t, root, base)
			if len(got) == 0 {
				t.Fatalf("неполная запись принята за объявление")
			}
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("находка не называет предмет: %s", joined)
			}
		})
	}
}

// ── Предпосылка: пустой состав базы — ОТКАЗ, а не пустой успех ────────────

func TestRemovedPathCensusRefusesAnUnreadBase(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	run("config", "user.email", "census@example.invalid")
	run("config", "user.name", "census")
	run("commit", "--quiet", "--allow-empty", "-m", "пустая база")
	out, err := gitenv.Command(root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	empty := strings.TrimSpace(string(out))

	if _, err := baseTreeSize(root, empty); err == nil {
		t.Fatal("пустой состав базы принят за чистое дерево — это и есть тот случай, " +
			"ради которого предпосылка проверяется")
	} else if !strings.Contains(err.Error(), "отказ, а не пустой успех") {
		t.Fatalf("отказ не называет предмет: %v", err)
	}

	// Законный близнец: база с составом отказа НЕ даёт.
	root2, base2 := injCensusRepo(t)
	if n, err := baseTreeSize(root2, base2); err != nil || n == 0 {
		t.Fatalf("непустая база отвергнута: n=%d err=%v", n, err)
	}
}

// ── Признак носителя: не слеп и не всеяден ────────────────────────────────

func TestCarrierPredicateSeesTheSubjectAndSpacesTheTwin(t *testing.T) {
	t.Parallel()
	carrier, asserts, why, err := judgeCarrierSource("a_test.go",
		[]byte(strings.Replace(injCensusCarrierSrc, "%s", "A", 1)))
	if err != nil || !carrier {
		t.Fatalf("носитель не опознан: carrier=%v err=%v", carrier, err)
	}
	if asserts != 1 || why == "" {
		t.Fatalf("признак не назван либо утверждения не сосчитаны: asserts=%d why=%q", asserts, why)
	}

	plain, _, _, err := judgeCarrierSource("b_test.go",
		[]byte(strings.Replace(injCensusPlainSrc, "%s", "B", 1)))
	if err != nil {
		t.Fatalf("законный близнец не разобран: %v", err)
	}
	if plain {
		t.Fatal("обычная проба опознана носителем — признак всеяден")
	}

	// Слово в КОММЕНТАРИИ и в СТРОКЕ носителем не делает: судится узел разбора.
	shadow := `package demo

// Эта проба про repoRoot() и treecorpus, но дерева не читает.
import "testing"

func TestShadow(t *testing.T) {
	t.Parallel()
	_ = "github.com/PRO-Robotech/corelib/treecorpus"
	_ = "repoRoot("
	_ = t
}
`
	got, _, _, err := judgeCarrierSource("c_test.go", []byte(shadow))
	if err != nil {
		t.Fatalf("тень не разобрана: %v", err)
	}
	if got {
		t.Fatal("комментарий и строковый литерал прочитаны как предмет — " +
			"признак судит текст, а не узел разбора")
	}

	// Неразбираемый источник — ОТКАЗ, а не «не носитель»: «не знаю» не выдаётся
	// за «нет».
	if _, _, _, err := judgeCarrierSource("d_test.go", []byte("package ??? {")); err == nil {
		t.Fatal("неразбираемый источник принят за «не носитель»")
	}
}
