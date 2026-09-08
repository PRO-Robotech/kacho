// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// gatecarrierremoval_injection_test.go — доказательство того, что держатель
// снятия носителей УМЕЕТ краснеть и УМЕЕТ молчать.
//
// # Почему инъекция гоняет настоящий репозиторий, а не только чистую функцию
//
// Половина предмета этого гейта живёт не в суждении, а в ДОБЫЧЕ входа:
// распознавание переименований (`-M`), форма `base...HEAD` и отказ на пустом
// обходе. Инъекция, подающая срез строк прямо в [judgeGateCarrierRemoval],
// доказала бы свойство одной половины и промолчала бы о второй — то есть о той,
// где живёт единственная измеренная причина ложных срабатываний
// (переименование). Поэтому фикстура — синтетический репозиторий во ВРЕМЕННОМ
// каталоге, и гоняются те же функции, что в дереве.
//
// # Одно-фактность
//
// Каждый отрицательный мир отличается от своего положительного близнеца РОВНО
// ОДНИМ названным фактом, иначе неизвестно, что дало красное:
//
//	контроль          → носители A и B на месте, надгробия нет           → молчание
//	снятие молча      → …тот же мир, но B СНЯТ                           → находка с именем B
//	законный близнец  → …тот же мир, но снятие B ОБЪЯВЛЕНО надгробием    → молчание
//	переименование    → …тот же мир, но B ПЕРЕИМЕНОВАН, а не снят        → молчание
//	истечение записи  → …контроль, но надгробие числит снятым живой A     → находка с именем A
//
// # Изоляция
//
// Репозиторий заводится в `t.TempDir()` и вызывается через `pkg/gitenv`: без
// снятия `GIT_DIR` проба под хуком отправки работала бы с живым клоном и писала
// бы его индекс (`multi-agent-flow.md` §«НЕПРИКОСНОВЕННОСТЬ ЧУЖОГО СОСТОЯНИЯ»).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// injCarrierSrc — тело синтетического носителя. Форма та же, что в дереве:
// файл корпуса, несущий гейтовое утверждение.
const injCarrierSrc = `package repohygiene

import "testing"

func TestSynthetic%s(t *testing.T) { _ = t }
`

// injGateRepo — синтетический корпус: два носителя, один коммит.
//
// Возвращает корень и имя базовой ревизии.
func injGateRepo(t *testing.T) (root, base string) {
	t.Helper()
	root = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "-b", "main")
	run("config", "user.email", "gate@example.invalid")
	run("config", "user.name", "gate")

	injWrite(t, root, "go.mod", "module synthetic\n\ngo 1.22\n")
	injWrite(t, root, gateCorpusDir+"/alpha_test.go", strings.Replace(injCarrierSrc, "%s", "Alpha", 1))
	injWrite(t, root, gateCorpusDir+"/beta_test.go", strings.Replace(injCarrierSrc, "%s", "Beta", 1))
	run("add", "-A")
	run("commit", "--quiet", "-m", "корпус: два носителя")

	out, err := gitenv.Command(root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return root, strings.TrimSpace(string(out))
}

func injWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func injGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if out, err := gitenv.Command(root, args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// injJudge — весь путь гейта на синтетическом дереве: добыча входа + суждение.
// ТЕ ЖЕ функции, что зовёт держатель по дереву.
//
// База передаётся явно там, где мир строится вокруг известной точки; миры,
// проверяющие САМ вывод базы, зовут [injJudgeDerivedBase].
func injJudge(t *testing.T, root, base string) []string {
	t.Helper()
	present, err := gateCarrierCensus(root)
	if err != nil {
		t.Fatalf("перепись: %v", err)
	}
	deleted, err := deletedGateCarriers(root, base)
	if err != nil {
		t.Fatalf("снятые: %v", err)
	}
	ledger, err := readGateCarrierLedger(root)
	if err != nil {
		t.Fatalf("надгробие: %v", err)
	}
	t.Logf("осмотрено: носителей %d; снято %d; записей надгробия %d",
		len(present), len(deleted), injLedgerRows(ledger))
	return judgeGateCarrierRemoval(deleted, present, ledger,
		func(c string) (bool, error) { return carrierWasEverRemoved(root, c) })
}

// injJudgeDerivedBase — то же, но базу ВЫВОДИТ [gateCarrierBase]. Ею проверяется
// сам вывод базы: именно он был неверен в первой редакции.
func injJudgeDerivedBase(t *testing.T, root string) ([]string, string) {
	t.Helper()
	base, how, err := gateCarrierBase(root)
	if err != nil {
		t.Fatalf("вывести базу: %v", err)
	}
	return injJudge(t, root, base), how
}

func injLedgerRows(l *gateCarrierLedger) int {
	if l == nil {
		return 0
	}
	return len(l.Retired)
}

func injWriteLedger(t *testing.T, root string, rows ...GateCarrierRetirement) {
	t.Helper()
	b, err := json.MarshalIndent(gateCarrierLedger{Retired: rows}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	injWrite(t, root, gateCorpusDir+"/"+gateCarrierLedgerName, string(b)+"\n")
}

// ── КОНТРОЛЬ: целый корпус молчит ──────────────────────────────────────────

func TestGateCarrierRemoval_ControlIntactCorpusIsSilent(t *testing.T) {
	root, base := injGateRepo(t)

	// Мир движется, носителей не теряя: добавлен третий. Без этого коммита
	// контроль сравнивал бы ревизию саму с собой и молчал бы by construction.
	injWrite(t, root, gateCorpusDir+"/gamma_test.go", strings.Replace(injCarrierSrc, "%s", "Gamma", 1))
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "добавлен носитель")

	if f := injJudge(t, root, base); len(f) != 0 {
		t.Fatalf("целый корпус обязан молчать, получено:\n%s", strings.Join(f, "\n"))
	}
}

// ── ИНЪЕКЦИЯ: носитель снят МОЛЧА ──────────────────────────────────────────

func TestGateCarrierRemoval_SilentRemovalIsFoundByName(t *testing.T) {
	root, base := injGateRepo(t)

	injGit(t, root, "rm", "--quiet", gateCorpusDir+"/beta_test.go")
	injGit(t, root, "commit", "--quiet", "-m", "правка на 911 файлов")

	f := injJudge(t, root, base)
	if len(f) == 0 {
		t.Fatal("молчаливое снятие носителя обязано быть находкой — гейт промолчал")
	}
	joined := strings.Join(f, "\n")
	if !strings.Contains(joined, "beta_test.go") {
		t.Fatalf("находка обязана называть ИМЯ снятого носителя, получено:\n%s", joined)
	}
	if !strings.Contains(joined, "МОЛЧА") {
		t.Fatalf("находка обязана называть предмет, а не симптом, получено:\n%s", joined)
	}
}

// ── ЗАКОННЫЙ БЛИЗНЕЦ: снятие ОБЪЯВЛЕНО ─────────────────────────────────────

func TestGateCarrierRemoval_DeclaredRemovalIsSilent(t *testing.T) {
	root, base := injGateRepo(t)

	injGit(t, root, "rm", "--quiet", gateCorpusDir+"/beta_test.go")
	injWriteLedger(t, root, GateCarrierRetirement{
		Carrier:   gateCorpusDir + "/beta_test.go",
		Reason:    "предмет снят вместе с контрактом, который он стерёг",
		Successor: "предмета больше нет: поле снято с контракта, номер зарезервирован",
	})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "носитель снят вместе с предметом")

	if f := injJudge(t, root, base); len(f) != 0 {
		t.Fatalf("объявленное снятие обязано молчать, получено:\n%s", strings.Join(f, "\n"))
	}
}

// ── ЗАКОННЫЙ БЛИЗНЕЦ: ПЕРЕИМЕНОВАНИЕ снятием не является ───────────────────
//
// Единственная измеренная причина ложных срабатываний: по 120 переходам ствола
// признак по имени утверждения сработал 7 раз, из них 6 — переименования.
// Сходство считает git, а не порог, придуманный здесь.

func TestGateCarrierRemoval_RenameIsNotARemoval(t *testing.T) {
	root, base := injGateRepo(t)

	injGit(t, root, "mv", gateCorpusDir+"/beta_test.go", gateCorpusDir+"/betarenamed_test.go")
	injGit(t, root, "commit", "--quiet", "-m", "носитель переименован")

	if f := injJudge(t, root, base); len(f) != 0 {
		t.Fatalf("переименование носителя снятием не является, получено:\n%s", strings.Join(f, "\n"))
	}
}

// ── САМОИСТЕЧЕНИЕ: запись, чей носитель ЖИВ ────────────────────────────────

func TestGateCarrierRemoval_LedgerRowOverALiveCarrierExpires(t *testing.T) {
	root, base := injGateRepo(t)

	// Ничего не снято. Надгробие числит снятым носитель, который в дереве ЕСТЬ,
	// — такая запись прикрывала бы живую координату.
	injWriteLedger(t, root, GateCarrierRetirement{
		Carrier:   gateCorpusDir + "/alpha_test.go",
		Reason:    "причина есть",
		Successor: "держатель назван",
	})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "надгробие пережило свой предмет")

	f := injJudge(t, root, base)
	if len(f) == 0 {
		t.Fatal("запись надгробия над ЖИВЫМ носителем обязана истечь — гейт промолчал")
	}
	if !strings.Contains(strings.Join(f, "\n"), "alpha_test.go") {
		t.Fatalf("находка обязана называть имя записи, получено:\n%s", strings.Join(f, "\n"))
	}
}

// ── ЗАПИСЬ БЕЗ СОДЕРЖАНИЯ: объявление, которое ничего не объявляет ──────────

func TestGateCarrierRemoval_DeclarationWithoutSubstanceIsAFinding(t *testing.T) {
	root, base := injGateRepo(t)

	injGit(t, root, "rm", "--quiet", gateCorpusDir+"/beta_test.go")
	injWriteLedger(t, root, GateCarrierRetirement{
		Carrier:   gateCorpusDir + "/beta_test.go",
		Reason:    "",
		Successor: "",
	})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снятие объявлено пустой записью")

	f := injJudge(t, root, base)
	if len(f) == 0 {
		t.Fatal("запись без причины ничего не объявляет — обязана быть находкой")
	}
}

// ── ПУСТОЙ ОБХОД: отказ, а не зелёное ──────────────────────────────────────
//
// Два мира, и второй важнее первого. Отказ на ОТСУТСТВУЮЩЕМ каталоге даёт сам
// [treecorpus]. А вот каталог, который ЕСТЬ и отслеживается, но не содержит ни
// одного носителя, treecorpus пропускает: `UnderWithSuffix` фильтрует уже
// прочитанный корпус и на пустом отборе возвращает (nil, nil). Именно этот мир
// и был бы вакуумным зелёным — «снято 0» при нуле прочитанного, — поэтому отказ
// на нём объявлен здесь, в [gateCarrierCensus], и доказывается отдельно.

func TestGateCarrierRemoval_EmptyWalkRefusesInsteadOfReportingNoFindings(t *testing.T) {
	newRepo := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		injGit(t, root, "init", "--quiet", "-b", "main")
		injGit(t, root, "config", "user.email", "gate@example.invalid")
		injGit(t, root, "config", "user.name", "gate")
		injWrite(t, root, "go.mod", "module synthetic\n\ngo 1.22\n")
		return root
	}

	t.Run("каталога корпуса нет вовсе", func(t *testing.T) {
		root := newRepo(t)
		injGit(t, root, "add", "-A")
		injGit(t, root, "commit", "--quiet", "-m", "дерево без корпуса гейтов")

		_, err := gateCarrierCensus(root)
		if err == nil {
			t.Fatal("отсутствующий корпус обязан быть ОТКАЗОМ, а не пустым успехом")
		}
		if !strings.Contains(err.Error(), gateCorpusDir) {
			t.Fatalf("отказ обязан называть каталог, получено: %v", err)
		}
		t.Logf("отказ: %v", err)
	})

	t.Run("каталог есть, носителей ноль", func(t *testing.T) {
		root := newRepo(t)
		// Каталог отслеживается и НЕ пуст — но ни одного `.go` в нём нет.
		// treecorpus здесь молчит: отбор по суффиксу опустошает уже прочитанный
		// корпус, и возвращается (nil, nil). Отказ обязан дать сам гейт.
		injWrite(t, root, gateCorpusDir+"/README.md", "корпус переехал\n")
		injGit(t, root, "add", "-A")
		injGit(t, root, "commit", "--quiet", "-m", "корпус без носителей")

		if files, err := treecorpus.UnderWithSuffix(
			filepath.Join(root, filepath.FromSlash(gateCorpusDir)), ".go"); err != nil || len(files) != 0 {
			t.Fatalf("предпосылка мира неверна: ожидался пустой отбор без ошибки, "+
				"получено files=%d err=%v", len(files), err)
		}

		_, err := gateCarrierCensus(root)
		if err == nil {
			t.Fatal("корпус без единого носителя обязан быть ОТКАЗОМ, а не пустым успехом: " +
				"«ноль находок» неотличимо от «ноль прочитанного»")
		}
		if !strings.Contains(err.Error(), "смотреть было не на что") {
			t.Fatalf("отказ обязан называть причину, получено: %v", err)
		}
		t.Logf("отказ: %v", err)
	})
}

// ── БАЗА: носитель, ЗАВЕДЁННЫЙ И СНЯТЫЙ ВНУТРИ ЛИНИИ ───────────────────────
//
// Миниатюра инцидента, из-за которого первая редакция была отвергнута. Носитель
// рождается после точки ответвления линии от ствола и умирает до HEAD: на обоих
// концах сравнения СО СТВОЛОМ его нет, поэтому видом `D` он не приходит никогда
// и гейт с базой-стволом молчит. Проверяется здесь не суждение, а ВЫВОД БАЗЫ.

// injLineRepo — ствол `main`, линия `release/lineX`, носитель, заведённый линией.
func injLineRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	injGit(t, root, "init", "--quiet", "-b", "main")
	injGit(t, root, "config", "user.email", "gate@example.invalid")
	injGit(t, root, "config", "user.name", "gate")

	injWrite(t, root, "go.mod", "module synthetic\n\ngo 1.22\n")
	injWrite(t, root, gateCorpusDir+"/alpha_test.go", strings.Replace(injCarrierSrc, "%s", "Alpha", 1))
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "ствол")

	injGit(t, root, "checkout", "--quiet", "-b", "release/lineX")
	injWrite(t, root, gateCorpusDir+"/born_test.go", strings.Replace(injCarrierSrc, "%s", "Born", 1))
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "линия завела свой носитель")
	return root
}

func TestGateCarrierRemoval_CarrierBornAndKilledInsideTheLineIsFound(t *testing.T) {
	root := injLineRepo(t)

	// Живая полоса линии снимает носитель, которого ствол не видел никогда.
	injGit(t, root, "checkout", "--quiet", "-b", "lane/pkg-rename")
	injGit(t, root, "rm", "--quiet", gateCorpusDir+"/born_test.go")
	injGit(t, root, "commit", "--quiet", "-m", "правка на 911 файлов")

	// Контроль ПРЕЖНЕГО правила: со стволом сравнение слепо by construction.
	mb, err := gitenv.Command(root, "merge-base", "HEAD", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	if f := injJudge(t, root, strings.TrimSpace(string(mb))); len(f) != 0 {
		t.Fatalf("предпосылка мира неверна: со стволом гейт обязан МОЛЧАТЬ "+
			"(в этом и был дефект), получено:\n%s", strings.Join(f, "\n"))
	}

	// Действующее правило: база — точка ответвления линии.
	f, how := injJudgeDerivedBase(t, root)
	t.Logf("база выведена как: %s", how)
	if len(f) == 0 {
		t.Fatal("носитель, заведённый и снятый внутри линии, обязан быть находкой — гейт промолчал")
	}
	if !strings.Contains(strings.Join(f, "\n"), "born_test.go") {
		t.Fatalf("находка обязана называть имя, получено:\n%s", strings.Join(f, "\n"))
	}
}

// ── БАЗА: изменение УЖЕ принадлежит линии → судится последнее ──────────────
//
// Форма коммита инцидента: снятие лежит на самой линии. Точка её ответвления
// совпала бы с HEAD либо уехала за него, поэтому базой становится `HEAD^`.

func TestGateCarrierRemoval_LandedChangeIsJudgedAgainstItsParent(t *testing.T) {
	root := injLineRepo(t)

	injGit(t, root, "rm", "--quiet", gateCorpusDir+"/born_test.go")
	injGit(t, root, "commit", "--quiet", "-m", "снятие легло прямо на линию")

	f, how := injJudgeDerivedBase(t, root)
	t.Logf("база выведена как: %s", how)
	if !strings.Contains(how, "HEAD^") {
		t.Fatalf("изменение принадлежит линии — базой обязан быть HEAD^, получено: %s", how)
	}
	if len(f) == 0 || !strings.Contains(strings.Join(f, "\n"), "born_test.go") {
		t.Fatalf("снятие на самой линии обязано быть находкой с именем, получено:\n%s",
			strings.Join(f, "\n"))
	}
}

// ── ИСТЕЧЕНИЕ ВТОРОГО НАПРАВЛЕНИЯ: строке нечего покрывать ─────────────────
//
// Запись над путём, которого не снимали никогда. Без этой проверки всякая
// строка после посадки линии выродилась бы в такую форму и жила бы вечно
// неопровержимой: дельта её уже не проверяет, а носителя в дереве и так нет.

func TestGateCarrierRemoval_LedgerRowOverANeverRemovedPathExpires(t *testing.T) {
	root, base := injGateRepo(t)

	injWriteLedger(t, root, GateCarrierRetirement{
		Carrier:   gateCorpusDir + "/never_existed_test.go",
		Reason:    "причина есть",
		Successor: "держатель назван",
	})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "надгробие над тем, чего не было")

	f := injJudge(t, root, base)
	if len(f) == 0 {
		t.Fatal("запись над путём, которого не снимали, покрывать нечего — обязана быть находкой")
	}
	joined := strings.Join(f, "\n")
	if !strings.Contains(joined, "never_existed_test.go") {
		t.Fatalf("находка обязана называть запись, получено:\n%s", joined)
	}
	if !strings.Contains(joined, "НИКОГДА") {
		t.Fatalf("находка обязана называть предмет, получено:\n%s", joined)
	}
}

// Законный близнец предыдущего: путь, который ДЕЙСТВИТЕЛЬНО снимали, молчит
// даже когда снятие уехало за базу — история помнит его всегда.
func TestGateCarrierRemoval_LedgerRowSurvivesWhenItsRemovalLeavesTheDelta(t *testing.T) {
	root, _ := injGateRepo(t)

	injGit(t, root, "rm", "--quiet", gateCorpusDir+"/beta_test.go")
	injWriteLedger(t, root, GateCarrierRetirement{
		Carrier:   gateCorpusDir + "/beta_test.go",
		Reason:    "предмет снят вместе с контрактом",
		Successor: "предмета больше нет",
	})
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "снятие объявлено")

	// База двигается ВПЕРЁД за снятие: дельта его больше не содержит.
	out, err := gitenv.Command(root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.TrimSpace(string(out))
	injWrite(t, root, gateCorpusDir+"/later_test.go", strings.Replace(injCarrierSrc, "%s", "Later", 1))
	injGit(t, root, "add", "-A")
	injGit(t, root, "commit", "--quiet", "-m", "линия поехала дальше")

	if f := injJudge(t, root, moved); len(f) != 0 {
		t.Fatalf("запись над действительно снятым путём обязана молчать и после того, "+
			"как снятие ушло из дельты, получено:\n%s", strings.Join(f, "\n"))
	}
}
