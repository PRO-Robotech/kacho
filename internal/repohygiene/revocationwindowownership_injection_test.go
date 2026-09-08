// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Инъекция переписи держателей окна отзыва — В ОБЕ СТОРОНЫ И ПО КАЖДОЙ ФОРМЕ.
//
// # Что здесь доказывается и почему порознь
//
// Держать окно отзыва можно двумя формами (`revocationwindowgate`): процесс
// строит кеш вердиктов сам либо отдаёт величину окна дескриптору носителя.
// Проверка, у которой одна из форм мертва, проходит парой «нашла настоящее ·
// смолчала на близнеце» ЦЕЛИКОМ на живой форме — и именно так она и жила: обход
// знал только вызов конструктора, а пять площадок дерева из восьми держат окно
// ТОЛЬКО дескриптором. Обе переписи одного предмета в одном прогоне печатали 3
// и 8, и меньшая принадлежала проверке, чьё имя обещает «каждый процесс с кешем
// вердиктов объявлен».
//
// Поэтому по каждой форме утверждается СВОЯ пара: незаявленный держатель этой
// формы ⇒ находка, называющая процесс и форму; тот же держатель, объявленный
// политикой ⇒ молчание. Плюс отдельно — что перепись без предмета не выдаётся
// за «нарушений нет», по каждой форме опять же порознь.
//
// Дельта каждого мира против его положительного близнеца — ОДИН факт: либо
// запись в объявленном множестве, либо одна форма владения у одной площадки.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

// ownershipFixture — одна площадка синтетического дерева.
type ownershipFixture struct {
	// process — имя каталога под services/, либо gatewayProcess для края.
	process string
	// body — тело единственного не-тестового файла площадки.
	body string
}

// srcHoldsByConstructor — форма 1: площадка строит кеш вердиктов сама.
const srcHoldsByConstructor = `package wiring

func newDoor(opts Options) *Door {
	return &Door{Cache: authz.NewCache(opts.PositiveTTL)}
}
`

// srcHoldsByDescriptor — форма 2: площадка кеша не строит вовсе, она НАЗЫВАЕТ
// величину окна дескриптору носителя. Обхода, спрашивающего «строит ли процесс
// кеш», для неё не существует: строить тут нечего.
const srcHoldsByDescriptor = `package main

func describe(cfg *config.Config) servicecontract.Spec {
	return servicecontract.Spec{CacheWindow: cfg.AuthZCacheTTL}
}
`

// srcHoldsNothing — законный близнец обеих форм: тот же вид литерала и тот же
// вид вызова, но ни кеша вердиктов, ни окна. Без него утверждение «перепись
// нашла ровно этих» ничего не измеряет — перепись, забирающая всех подряд, тоже
// «находит» нужного.
const srcHoldsNothing = `package main

func describe(cfg *config.Config) servicecontract.Spec {
	return servicecontract.Spec{ClientBudget: cfg.ClientBudget}
}

func newIntrospection(opts Options) *Reader {
	return &Reader{cache: introspection.NewResultCache(opts.TTL)}
}
`

// writeOwnershipTree — синтетическое дерево процессов. Край обязателен: без
// каталога края обход роняет собственную предпосылку, и это не тот отказ,
// который здесь измеряется.
func writeOwnershipTree(t *testing.T, fixtures ...ownershipFixture) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "services"), 0o750); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gateway"), 0o750); err != nil {
		t.Fatalf("mkdir gateway: %v", err)
	}
	for _, f := range fixtures {
		dir := filepath.Join(root, "gateway", "internal")
		if f.process != gatewayProcess {
			dir = filepath.Join(root, "services", f.process, "cmd")
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", f.process, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "wiring.go"), []byte(f.body), 0o600); err != nil {
			t.Fatalf("write %s: %v", f.process, err)
		}
	}
	return root
}

// ownershipRun — перепись синтетического дерева.
func ownershipRun(t *testing.T, root string) (map[string]revocationwindowgate.WindowOwnership, int) {
	t.Helper()
	held, filesRead, err := verdictCacheHoldersUnder(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return held, filesRead
}

// ────────────────────────────────────────────────────────────────────────────
// Форма 1 — вызов конструктора
// ────────────────────────────────────────────────────────────────────────────

func TestOwnershipInjection_Form1_UndeclaredHolderIsAFinding(t *testing.T) {
	root := writeOwnershipTree(t,
		ownershipFixture{process: "alpha", body: srcHoldsByConstructor},
		ownershipFixture{process: gatewayProcess, body: srcHoldsByDescriptor},
	)
	held, _ := ownershipRun(t, root)

	got := undeclaredHolders(held, map[string]bool{gatewayProcess: true})
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("площадка, строящая кеш вердиктов и не объявленная политикой, переписью не "+
			"названа: находки=%v, держатели=%v", got, held)
	}
	if forms := formsOf(held["alpha"]); !strings.Contains(forms, revocationwindowgate.OwnershipFormNames()[0]) {
		t.Errorf("находка не называет ФОРМУ владения (%q). Читатель пойдёт искать вызов "+
			"конструктора там, где его может не быть вовсе", forms)
	}
}

// TestOwnershipInjection_Form1_DeclaredHolderIsSilent — законный близнец.
// Дельта против мира выше — ОДИН факт: запись в объявленном множестве.
func TestOwnershipInjection_Form1_DeclaredHolderIsSilent(t *testing.T) {
	root := writeOwnershipTree(t,
		ownershipFixture{process: "alpha", body: srcHoldsByConstructor},
		ownershipFixture{process: gatewayProcess, body: srcHoldsByDescriptor},
	)
	held, _ := ownershipRun(t, root)

	if got := undeclaredHolders(held, map[string]bool{"alpha": true, gatewayProcess: true}); len(got) != 0 {
		t.Fatalf("объявленная площадка названа находкой: %v", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Форма 2 — окно отдано дескриптору носителя
//
// Ровно эта половина и была слепой: обход спрашивал «строит ли процесс кеш», а
// такая площадка не строит ничего.
// ────────────────────────────────────────────────────────────────────────────

func TestOwnershipInjection_Form2_UndeclaredHolderIsAFinding(t *testing.T) {
	root := writeOwnershipTree(t,
		ownershipFixture{process: "beta", body: srcHoldsByDescriptor},
		ownershipFixture{process: gatewayProcess, body: srcHoldsByConstructor},
	)
	held, _ := ownershipRun(t, root)

	got := undeclaredHolders(held, map[string]bool{gatewayProcess: true})
	if len(got) != 1 || got[0] != "beta" {
		t.Fatalf("площадка, отдающая окно дескриптору полем %q и не объявленная политикой, "+
			"переписью НЕ названа: находки=%v, держатели=%v.\n"+
			"  Эта форма — не редкость: ею и только ею держат окно пять площадок дерева из "+
			"восьми. Обход, спрашивающий «строит ли процесс кеш вердиктов», для неё слеп by "+
			"construction: строить тут нечего, и его молчание читается как «чисто».",
			revocationwindowgate.DescriptorWindowField, got, held)
	}
	if forms := formsOf(held["beta"]); !strings.Contains(forms, revocationwindowgate.OwnershipFormNames()[1]) {
		t.Errorf("находка не называет ФОРМУ владения (%q)", forms)
	}
}

// TestOwnershipInjection_Form2_DeclaredHolderIsSilent — законный близнец формы 2.
func TestOwnershipInjection_Form2_DeclaredHolderIsSilent(t *testing.T) {
	root := writeOwnershipTree(t,
		ownershipFixture{process: "beta", body: srcHoldsByDescriptor},
		ownershipFixture{process: gatewayProcess, body: srcHoldsByConstructor},
	)
	held, _ := ownershipRun(t, root)

	if got := undeclaredHolders(held, map[string]bool{"beta": true, gatewayProcess: true}); len(got) != 0 {
		t.Fatalf("объявленная площадка названа находкой: %v", got)
	}
}

// TestOwnershipInjection_NonHolderIsNotSweptIn — положительный контроль обеих
// форм: площадка, окна НЕ держащая, в перепись не попадает.
//
// Без него отрицания выше зеленели бы и на переписи, забирающей всех подряд.
func TestOwnershipInjection_NonHolderIsNotSweptIn(t *testing.T) {
	root := writeOwnershipTree(t,
		ownershipFixture{process: "gamma", body: srcHoldsNothing},
		ownershipFixture{process: "alpha", body: srcHoldsByConstructor},
		ownershipFixture{process: gatewayProcess, body: srcHoldsByDescriptor},
	)
	held, _ := ownershipRun(t, root)

	if _, swept := held["gamma"]; swept {
		t.Errorf("площадка без окна отзыва засчитана держателем: кеш УДОСТОВЕРЕНИЙ и соседняя "+
			"ось дескриптора приняты за окно гранта. Тогда в перепись поедет каждый, кто "+
			"вообще собирает дескриптор или зовёт что-нибудь с именем кеша: %v", held)
	}
	if len(held) != 2 {
		t.Fatalf("держателей %d, ожидалось 2 (по одному на форму): %v", len(held), held)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Перепись без предмета — ОТДЕЛЬНО по каждой форме
// ────────────────────────────────────────────────────────────────────────────

// TestOwnershipInjection_VacuityIsPerForm — «ноль находок» отличимо от «ноль
// прочитанного», и отличимо ПО КАЖДОЙ форме.
//
// Общая предпосылка («хоть один держатель найден») слепа ровно к тому случаю,
// ради которого этот файл заведён: половина распознавателя умирает, вторая
// продолжает находить свои площадки, и перепись выглядит здоровой.
func TestOwnershipInjection_VacuityIsPerForm(t *testing.T) {
	ctor := revocationwindowgate.WindowOwnership{ByConstructor: true}
	desc := revocationwindowgate.WindowOwnership{ByDescriptor: true}
	names := revocationwindowgate.OwnershipFormNames()

	for _, c := range []struct {
		name      string
		filesRead int
		held      map[string]revocationwindowgate.WindowOwnership
		wantEmpty bool
		wantSays  string
	}{
		{
			name: "обе формы живы — предпосылка выполнена", filesRead: 10,
			held:      map[string]revocationwindowgate.WindowOwnership{"a": ctor, "b": desc},
			wantEmpty: true,
		},
		{
			name: "форма «вызов конструктора» не встретилась", filesRead: 10,
			held:     map[string]revocationwindowgate.WindowOwnership{"b": desc},
			wantSays: names[0],
		},
		{
			name: "форма «окно отдано дескриптору» не встретилась", filesRead: 10,
			held:     map[string]revocationwindowgate.WindowOwnership{"a": ctor},
			wantSays: names[1],
		},
		{
			name: "не прочитано ни одного файла", filesRead: 0,
			held:     map[string]revocationwindowgate.WindowOwnership{"a": ctor, "b": desc},
			wantSays: "ни одного файла",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			why := holderCensusVacuity(c.filesRead, c.held)
			if c.wantEmpty {
				if why != "" {
					t.Fatalf("предпосылка объявлена нарушенной на здоровом дереве: %s", why)
				}
				return
			}
			if why == "" {
				t.Fatalf("перепись без предмета прошла молча — «нарушений нет» стало "+
					"неотличимо от «нечего было измерять» (мир: %s)", c.name)
			}
			if !strings.Contains(why, c.wantSays) {
				t.Errorf("находка не называет ослепшую половину: %q не содержит %q.\n"+
					"Находка, называющая симптом вместо причины, посылает читателя искать "+
					"не там", why, c.wantSays)
			}
		})
	}
}

// TestOwnershipInjection_BothCensusesReadOneWalk — две проверки об одном
// предмете считают ОДНО.
//
// Проба существует потому, что расхождение двух обходов — это и есть класс,
// ради которого файл заведён. Здесь оно становится представимым: если кто-то
// снова заведёт второй обход, эти числа разойдутся.
func TestOwnershipInjection_BothCensusesReadOneWalk(t *testing.T) {
	root := repoRoot(t)
	first, firstRead, err := verdictCacheHoldersUnder(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	second, secondRead, serr := verdictCacheHoldersUnder(root)
	if serr != nil {
		t.Fatalf("%v", serr)
	}
	if firstRead != secondRead || len(first) != len(second) {
		t.Fatalf("обход недетерминирован: прочитано %d/%d, держателей %d/%d",
			firstRead, secondRead, len(first), len(second))
	}
	byCtor, byDescriptor := ownershipByForm(first)
	t.Logf("осмотрено: файлов прочитано=%d, держателей=%d (формой «%s»=%d, формой «%s»=%d)",
		firstRead, len(first),
		revocationwindowgate.OwnershipFormNames()[0], byCtor,
		revocationwindowgate.OwnershipFormNames()[1], byDescriptor)
	if byCtor == 0 || byDescriptor == 0 {
		t.Fatalf("в дереве не осталось площадок одной из форм (%d/%d) — инъекция ниже "+
			"перестала бы что-либо измерять", byCtor, byDescriptor)
	}
}
