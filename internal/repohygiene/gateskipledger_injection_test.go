// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthSkipTree — синтетическое дерево проб. Инъекция обязана идти на нём, а не
// на этом дереве: на живом нельзя ни вернуть дефект, ни поставить рядом законного
// близнеца, не тронув чужой работы.
func synthSkipTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	return root
}

const synthSkipCall = `package synth

import "testing"

func TestSym(t *testing.T) {
	t.Skip("файловая система не поддерживает симлинки: нет прав")
}
`

const synthSkipfCall = `package synth

import "testing"

func TestLs(t *testing.T) {
	t.Skipf("git ls-files недоступен (%v) — гейт пропущен", 1)
}
`

// Причина стоит ТОЛЬКО в комментарии — вызова нет. Поиск по тексту счёл бы
// запись обеспеченной; разбор обязан назвать её просроченной.
const synthReasonOnlyInProse = `package synth

import "testing"

// Здесь когда-то было: t.Skip("корень не является рабочим деревом git — …").
// Вызов снят, объяснение осталось.
func TestProse(t *testing.T) {
	_ = "корень не является рабочим деревом git"
	t.Log("ничего не пропускаем")
}
`

// Конкатенация литералов — форма, которой написаны настоящие пропуски дерева.
const synthConcatCall = `package synth

import "testing"

func TestConcat(t *testing.T) {
	t.Skip("перечень ожидающих перевода пуст" +
		" — истекать нечему")
}
`

// Переменная ВНУТРИ конкатенации обрывает константное начало: всё, что стоит
// после неё, в сообщении окажется не там, где ждёт префикс.
const synthConcatWithVarCall = `package synth

import "testing"

func TestConcatVar(t *testing.T) {
	why := "неважно"
	t.Skip("начало " + why + " хвост")
}
`

func TestInjection_LedgerEntryWithoutACallIsAFinding(t *testing.T) {
	t.Parallel()
	root := synthSkipTree(t, map[string]string{"a/x_test.go": synthSkipCall})
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if c.FilesRead != 1 || len(c.Sites) != 1 {
		t.Fatalf("перепись синтетики: файлов %d, вызовов %d — ждали 1 и 1",
			c.FilesRead, len(c.Sites))
	}

	// (а) ДЕФЕКТ ВОЗВРАЩЁН: записи нечего исключать.
	f := AuditGateSkipLedger([]string{"ствол не разрешается"}, c)
	if len(f) != 1 {
		t.Fatalf("просроченная запись не найдена: находок %d", len(f))
	}
	if !strings.Contains(f[0], "ствол не разрешается") {
		t.Fatalf("находка не называет запись: %q", f[0])
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: запись с предметом — молчание.
	if f := AuditGateSkipLedger([]string{"файловая система не поддерживает симлинки"}, c); len(f) != 0 {
		t.Fatalf("запись с предметом объявлена просроченной: %v", f)
	}
}

func TestInjection_ReasonOnlyInProseDoesNotCoverAnEntry(t *testing.T) {
	t.Parallel()
	root := synthSkipTree(t, map[string]string{
		"a/prose_test.go": synthReasonOnlyInProse,
		"a/live_test.go":  synthSkipCall,
	})
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if c.FilesRead != 2 {
		t.Fatalf("прочитано файлов %d — ждали 2", c.FilesRead)
	}

	// Причина есть в комментарии и в строковом ЛИТЕРАЛЕ не-вызова — и всё равно
	// запись просрочена: перепись судит узлы вызова.
	f := AuditGateSkipLedger([]string{"корень не является рабочим деревом git"}, c)
	if len(f) != 1 {
		t.Fatalf("причина из прозы засчитана за вызов: находок %d", len(f))
	}
	// Положительный контроль в том же дереве: живой вызов рядом обеспечивает свою
	// запись — иначе «находка» означала бы «перепись вообще ничего не видит».
	if f := AuditGateSkipLedger([]string{"файловая система не поддерживает симлинки"}, c); len(f) != 0 {
		t.Fatalf("живой вызов не засчитан: %v", f)
	}
}

func TestInjection_SkipfAndConcatenationAreRead(t *testing.T) {
	t.Parallel()
	root := synthSkipTree(t, map[string]string{
		"a/f_test.go": synthSkipfCall,
		"a/c_test.go": synthConcatCall,
	})
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(c.Sites) != 2 {
		t.Fatalf("вызовов найдено %d — ждали 2 (Skipf и конкатенация)", len(c.Sites))
	}
	if f := AuditGateSkipLedger([]string{
		"git ls-files недоступен",
		"перечень ожидающих перевода пуст — истекать нечему",
	}, c); len(f) != 0 {
		t.Fatalf("формат и конкатенация не прочитаны: %v", f)
	}
	// Обратная сторона: подстановка обрывает константную часть, и запись, залезающая
	// ЗА неё, предметом не обеспечена — иначе ведомость обещала бы то, чего сообщение
	// не гарантирует.
	if f := AuditGateSkipLedger([]string{"git ls-files недоступен (нет такого файла)"}, c); len(f) != 1 {
		t.Fatalf("запись за подстановкой засчитана: находок %d", len(f))
	}
}

func TestInjection_AVariableTruncatesTheConstantPrefix(t *testing.T) {
	t.Parallel()
	root := synthSkipTree(t, map[string]string{"a/v_test.go": synthConcatWithVarCall})
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(c.Sites) != 1 {
		t.Fatalf("вызовов найдено %d — ждали 1", len(c.Sites))
	}
	if got := c.Sites[0].Reason; got != "начало " {
		t.Fatalf("константное начало прочитано как %q — хвост за переменной "+
			"приклеен к префиксу, которого в сообщении не будет никогда", got)
	}
	// Обе стороны: до переменной запись обеспечена, за переменной — нет.
	if f := AuditGateSkipLedger([]string{"начало"}, c); len(f) != 0 {
		t.Fatalf("запись в пределах константного начала объявлена просроченной: %v", f)
	}
	if f := AuditGateSkipLedger([]string{"начало неважно хвост"}, c); len(f) != 1 {
		t.Fatalf("запись за переменной засчитана: находок %d", len(f))
	}
}

func TestInjection_EmptyLedgerIsTheGoalNotABreakage(t *testing.T) {
	t.Parallel()
	root := synthSkipTree(t, map[string]string{"a/x_test.go": synthSkipCall})
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if f := AuditGateSkipLedger(nil, c); len(f) != 0 {
		t.Fatalf("пустая ведомость дала находки: %v", f)
	}
	// И разбор самой ведомости обязан различать пустую и заполненную.
	if got := ParseGateSkipLedger("# только объяснение\n\n"); len(got) != 0 {
		t.Fatalf("комментарий разобран как запись: %v", got)
	}
	if got := ParseGateSkipLedger("причина  # объяснение\n"); len(got) != 1 || got[0] != "причина" {
		t.Fatalf("запись с объяснением разобрана как %v", got)
	}
}

func TestInjection_EmptyCensusIsDistinguishableFromZeroFindings(t *testing.T) {
	t.Parallel()
	root := synthSkipTree(t, map[string]string{"a/notatest.go": "package synth\n"})
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if c.FilesRead != 0 || len(c.Sites) != 0 {
		t.Fatalf("не-пробный файл попал в перепись: файлов %d, вызовов %d",
			c.FilesRead, len(c.Sites))
	}
	// На пустой переписи ЛЮБАЯ запись просрочена — и именно поэтому гейт по дереву
	// обязан ронять пустую перепись отдельно, а не выдавать её за находку.
	if f := AuditGateSkipLedger([]string{"что угодно"}, c); len(f) != 1 {
		t.Fatalf("на пустой переписи находок %d — ждали 1", len(f))
	}
}

// ─── ОБРАТНОЕ НАПРАВЛЕНИЕ: КАЖДЫЙ ПРОПУСК ОБЪЯВЛЕН (#2548) ───────────────────
//
// Инъекция идёт ТРЕМЯ прогонами на ОДНОМ контрольном дереве, и каждый меняет
// РОВНО ОДИН факт против него:
//
//	контроль          — целы оба свойства  → молчат ОБА направления;
//	инъекция нового   — пропуск не объявлен → краснеет ТОЛЬКО обратное;
//	инъекция старого  — запись без предмета → краснеет ТОЛЬКО прямое.
//
// Третий прогон обязателен: без него молчание прямого направления неотличимо от
// молчания мёртвого. А раздельные функции нужны именно затем, чтобы «краснеет
// только оно» можно было утверждать — слитые в одну, направления краснели бы
// вместе и не различались.
//
// Форма «завести ещё один элемент» здесь ЗАКОННА, и это надо сказать: направления
// судят РАЗНЫЕ популяции — прямое судит записи ведомости, обратное судит вызовы.
// Лишний необъявленный вызов нарушает единственное требование к вызовам и не
// трогает ни одного требования к записям, поэтому дельта остаётся одно-фактной.

// synthUndeclaredSkip — вызов, чьей причины нет ни в одной записи ведомости.
const synthUndeclaredSkip = `package synth

import "testing"

func TestUndeclared(t *testing.T) {
	t.Skip("зависимость не поднята — предмета нет")
}
`

// synthSkipNow — пропуск БЕЗ причины: разбору нечего разрешать.
const synthSkipNow = `package synth

import "testing"

func TestNoReason(t *testing.T) {
	t.SkipNow()
}
`

func TestInjection_UndeclaredSkipRedensOnlyTheDeclaredDirection(t *testing.T) {
	t.Parallel()

	// Контрольная ведомость: одна запись, и у неё есть предмет в контрольном дереве.
	const entry = "файловая система не поддерживает симлинки"
	control := synthSkipTree(t, map[string]string{"a/x_test.go": synthSkipCall})

	census := func(root string) SkipCensus {
		t.Helper()
		c, err := CollectSkipSites([]string{root})
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		return c
	}

	// ── ПРОГОН 1: КОНТРОЛЬ. Целы оба свойства — молчат оба направления.
	cControl := census(control)
	if cControl.FilesRead != 1 || len(cControl.Sites) != 1 || len(cControl.Unresolved) != 0 {
		t.Fatalf("перепись контроля: файлов %d, вызовов %d, неразрешённых %d — ждали 1, 1, 0",
			cControl.FilesRead, len(cControl.Sites), len(cControl.Unresolved))
	}
	if f := AuditGateSkipDeclared([]string{entry}, cControl); len(f) != 0 {
		t.Fatalf("контроль: обратное направление краснеет на целом дереве: %v", f)
	}
	if f := AuditGateSkipLedger([]string{entry}, cControl); len(f) != 0 {
		t.Fatalf("контроль: прямое направление краснеет на целом дереве: %v", f)
	}

	// ── ПРОГОН 2: ИНЪЕКЦИЯ НОВОГО СВОЙСТВА. Добавлен вызов, чья причина не
	// объявлена. Запись ведомости предмет НЕ теряет — её держит прежний вызов.
	injected := synthSkipTree(t, map[string]string{
		"a/x_test.go": synthSkipCall,
		"a/y_test.go": synthUndeclaredSkip,
	})
	cInjected := census(injected)
	if len(cInjected.Sites) != 2 {
		t.Fatalf("перепись инъекции: вызовов %d — ждали 2", len(cInjected.Sites))
	}
	fNew := AuditGateSkipDeclared([]string{entry}, cInjected)
	if len(fNew) != 1 {
		t.Fatalf("необъявленный пропуск не найден: находок %d (%v)", len(fNew), fNew)
	}
	if !strings.Contains(fNew[0], "y_test.go") {
		t.Fatalf("находка не называет координату: %q", fNew[0])
	}
	if !strings.Contains(fNew[0], "зависимость не поднята") {
		t.Fatalf("находка не называет причину: %q", fNew[0])
	}
	if f := AuditGateSkipLedger([]string{entry}, cInjected); len(f) != 0 {
		t.Fatalf("инъекция НОВОГО уронила ПРЯМОЕ направление — дельта не одно-фактна, "+
			"и «краснеет только оно» утверждать нельзя: %v", f)
	}

	// ── ПРОГОН 3: ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО СВОЙСТВА. Запись без предмета на том же
	// контрольном дереве. Без этого прогона молчание прямого направления в
	// прогоне 2 неотличимо от молчания мёртвого.
	fOld := AuditGateSkipLedger([]string{entry, "ствол не разрешается"}, cControl)
	if len(fOld) != 1 {
		t.Fatalf("просроченная запись не найдена: находок %d (%v) — прямое "+
			"направление молчит НЕ потому, что цело", len(fOld), fOld)
	}
	if !strings.Contains(fOld[0], "ствол не разрешается") {
		t.Fatalf("находка не называет запись: %q", fOld[0])
	}
	if f := AuditGateSkipDeclared([]string{entry, "ствол не разрешается"}, cControl); len(f) != 0 {
		t.Fatalf("инъекция СТАРОГО уронила обратное направление — дельта не "+
			"одно-фактна: %v", f)
	}
}

// TestInjection_SkipWithoutAResolvableReasonIsAFinding — пропуск, чью причину
// разбор не разрешает, невидим ОБЕИМ проверкам и потому находка.
//
// Законный близнец той же формы — пропуск с причиной-литералом — обязан молчать,
// иначе проверка ловила бы «здесь есть пропуск», а не «причину нельзя прочесть».
func TestInjection_SkipWithoutAResolvableReasonIsAFinding(t *testing.T) {
	t.Parallel()
	const entry = "файловая система не поддерживает симлинки"

	// (а) ДЕФЕКТ: причины нет вовсе.
	c := mustCollect(t, synthSkipTree(t, map[string]string{"a/n_test.go": synthSkipNow}))
	if len(c.Unresolved) != 1 || len(c.Sites) != 0 {
		t.Fatalf("перепись: разрешённых %d, неразрешённых %d — ждали 0 и 1",
			len(c.Sites), len(c.Unresolved))
	}
	f := AuditGateSkipDeclared([]string{entry}, c)
	if len(f) != 1 || !strings.Contains(f[0], "n_test.go") {
		t.Fatalf("пропуск без разрешимой причины не назван поимённо: %v", f)
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ: причина-литерал, объявленная ведомостью — молчание.
	cTwin := mustCollect(t, synthSkipTree(t, map[string]string{"a/x_test.go": synthSkipCall}))
	if len(cTwin.Unresolved) != 0 {
		t.Fatalf("литеральная причина сочтена неразрешимой: %+v", cTwin.Unresolved)
	}
	if f := AuditGateSkipDeclared([]string{entry}, cTwin); len(f) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", f)
	}
}

func mustCollect(t *testing.T, root string) SkipCensus {
	t.Helper()
	c, err := CollectSkipSites([]string{root})
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	return c
}
