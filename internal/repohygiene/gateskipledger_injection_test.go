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
