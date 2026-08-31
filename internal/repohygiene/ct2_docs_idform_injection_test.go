// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_docs_idform_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что анализатор
// способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`ct2_docs_idform_test.go`) о способности падать не говорит ничего —
// зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, каждая — на своей странице. К каждой приложен
// законный близнец той же формы, обязанный молчать: без него «краснеет» было бы
// неотличимо от «краснеет всегда».
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// idFormStand — синтетическое дерево. ЗАКОННОЕ состояние: каждая страница
// показывает идентификатор в той форме, которую чеканит код.
type idFormStand struct{ root string }

func newIDFormStand(t *testing.T) *idFormStand {
	t.Helper()
	s := &idFormStand{root: t.TempDir()}

	// Генератор. Каталог `pkg/ids` — координата затравки анализатора; переедет
	// она — словарь опустеет, и премиса вердикта об этом скажет.
	s.write(t, "pkg/ids/ids.go", `
package ids

func NewID(prefix string) string       { return prefix + "00000000000000000" }
func NewHyphenID(prefix string) string { return prefix + "-00000000000000000" }
`)

	// Посредник: префикс приезжает ПАРАМЕТРОМ. Ровно так чеканится
	// идентификатор операции, и без транзитивного резолва его префикс остался бы
	// анализатору неизвестен.
	s.write(t, "pkg/ops/ops.go", `
package ops

import "example.test/probe/pkg/ids"

func New(domainPrefix string, desc string) string { return ids.NewID(domainPrefix) }
`)

	// Объявления префиксов. `PrefixAliased` объявлен ЧЕРЕЗ другую константу —
	// собиратель, читающий только строковые литералы, оставил бы его нерезолвимым.
	s.write(t, "services/probe/internal/domain/prefixes.go", `
package domain

const (
	PrefixDirect  = "thg"
	PrefixVia     = "tho"
	PrefixSource  = "tal"
	PrefixAliased = PrefixSource
	PrefixHyphen  = "hyp"
)
`)

	s.write(t, "services/probe/internal/app/mint.go", `
package app

import (
	"example.test/probe/pkg/ids"
	"example.test/probe/pkg/ops"
	"example.test/probe/services/probe/internal/domain"
)

func Direct() string  { return ids.NewID(domain.PrefixDirect) }
func Via() string     { return ops.New(domain.PrefixVia, "create thing") }
func Aliased() string { return ids.NewID(domain.PrefixAliased) }
func Hyphen() string  { return ids.NewHyphenID(domain.PrefixHyphen) }
`)

	// Законная страница: все четыре чеканимых префикса написаны своей формой,
	// плюс префикс, которого код не чеканит вовсе.
	s.write(t, "services/probe/docs/content/api/thing.mdx", `
Прямая чеканка — слитная форма: `+"`thga1b2c3d4e5f6g7h8j`"+`.
Через посредника — тоже слитная: `+"`thoa1b2c3d4e5f6g7h8j`"+`.
Через псевдоним константы — слитная: `+"`tala1b2c3d4e5f6g7h8j`"+`.
Дефисная чеканка — дефисная форма: `+"`hyp-a1b2c3d4e5f6g7h8j`"+`.
Префикс, которого код не чеканит: `+"`unk-a1b2c3d4e5f6g7h8j`"+` — вне суда.
`)
	return s
}

func (s *idFormStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *idFormStand) opts() DocsIDFormOptions {
	return DocsIDFormOptions{
		Root:          s.root,
		ModulePath:    "example.test/probe",
		JudgedDomains: []string{"services/probe/docs/content"},
	}
}

func (s *idFormStand) run(t *testing.T) ([]DocsIDFormFinding, DocsIDFormCensus) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditDocsIDForm(s.opts(), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

// TestDocsIDFormInjection_SilentOnALawfulTree — положительный контроль.
// Без него «краснеет на инъекции» было бы неотличимо от «краснеет всегда».
func TestDocsIDFormInjection_SilentOnALawfulTree(t *testing.T) {
	s := newIDFormStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве найдено %d: %v", len(findings), findings)
	}
	if census.Judged == 0 {
		t.Fatalf("рассужено 0 токенов — молчание получено пустым обходом, а не законностью дерева")
	}
	// Словарь обязан знать ВСЕ четыре способа объявить префикс, иначе молчание
	// выше означало бы «не знаю», а не «верно».
	forms := strings.Join(census.PrefixForms, " ")
	for _, want := range []string{"thg:" + idFormConcat, "tho:" + idFormConcat,
		"tal:" + idFormConcat, "hyp:" + idFormHyphen} {
		if !strings.Contains(forms, want) {
			t.Errorf("в словаре нет %q — резолв этого способа объявления не работает; словарь: %s", want, forms)
		}
	}
}

// TestDocsIDFormInjection_UnknownPrefixIsNotAFinding — молчание на неизвестном
// префиксе означает «не знаю» и обязано быть СЧИТАНО, а не проглочено.
func TestDocsIDFormInjection_UnknownPrefixIsNotAFinding(t *testing.T) {
	s := newIDFormStand(t)
	_, census := s.run(t)
	if census.UnknownPrefix == 0 {
		t.Fatalf("неизвестных префиксов насчитано 0 — слепая зона не видна в переписи")
	}
	if !strings.Contains(strings.Join(census.UnknownPrefixes, " "), "unk") {
		t.Errorf("перепись не назвала неизвестный префикс `unk`: %v", census.UnknownPrefixes)
	}
}

// injectPage вносит ОДНУ инъекцию отдельной страницей и возвращает находки.
func (s *idFormStand) injectPage(t *testing.T, rel, body string) []DocsIDFormFinding {
	t.Helper()
	s.write(t, rel, body)
	findings, _ := s.run(t)
	return findings
}

// requireOneFinding — находка ровно одна, и она называет координату и токен.
func requireOneFinding(t *testing.T, findings []DocsIDFormFinding, wantFile, wantToken string) {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	f := findings[0]
	if f.File != wantFile {
		t.Errorf("находка называет файл %q, а инъекция внесена в %q", f.File, wantFile)
	}
	if f.Line == 0 {
		t.Errorf("находка не называет строку — по ней нельзя дойти до места")
	}
	if f.Token != wantToken {
		t.Errorf("находка называет токен %q, ожидался %q", f.Token, wantToken)
	}
	if !strings.Contains(f.String(), wantToken) {
		t.Errorf("текст находки не несёт токен: %s", f.String())
	}
}

// TestDocsIDFormInjection_HyphenWrittenWhereConcatMinted — форма 1: страница
// пишет дефисом то, что чеканится слитно. Ровно дефект kacho#1641.
func TestDocsIDFormInjection_HyphenWrittenWhereConcatMinted(t *testing.T) {
	s := newIDFormStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/injected.mdx",
		"Идентификатор тома — `thg-a1b2c3d4e5f6g7h8j`.\n")
	requireOneFinding(t, got, "services/probe/docs/content/api/injected.mdx", "thg-a1b2c3d4e5f6g7h8j")
}

// TestDocsIDFormInjection_ConcatWrittenWhereHyphenMinted — форма 2, обратная
// сторона той же оси: страница пишет слитно то, что чеканится дефисом.
func TestDocsIDFormInjection_ConcatWrittenWhereHyphenMinted(t *testing.T) {
	s := newIDFormStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/injected.mdx",
		"Идентификатор типа машины — `hypa1b2c3d4e5f6g7h8j`.\n")
	requireOneFinding(t, got, "services/probe/docs/content/api/injected.mdx", "hypa1b2c3d4e5f6g7h8j")
}

// TestDocsIDFormInjection_TransitivePrefixIsJudged — префикс, приезжающий в
// чеканку ПАРАМЕТРОМ через посредника, обязан судиться наравне с прямым.
// Без транзитивного резолва эта инъекция осталась бы зелёной — молча.
func TestDocsIDFormInjection_TransitivePrefixIsJudged(t *testing.T) {
	s := newIDFormStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/injected.mdx",
		"Идентификатор операции — `tho-a1b2c3d4e5f6g7h8j`.\n")
	requireOneFinding(t, got, "services/probe/docs/content/api/injected.mdx", "tho-a1b2c3d4e5f6g7h8j")
}

// TestDocsIDFormInjection_AliasedConstantIsJudged — префикс, объявленный через
// ДРУГУЮ константу, тоже обязан судиться.
func TestDocsIDFormInjection_AliasedConstantIsJudged(t *testing.T) {
	s := newIDFormStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/injected.mdx",
		"Идентификатор — `tal-a1b2c3d4e5f6g7h8j`.\n")
	requireOneFinding(t, got, "services/probe/docs/content/api/injected.mdx", "tal-a1b2c3d4e5f6g7h8j")
}

// TestDocsIDFormInjection_EmptyWalkIsVisible — обход, которому нечего читать,
// обязан быть ОТЛИЧИМ от обхода без находок: премиса вердикта опирается на это.
func TestDocsIDFormInjection_EmptyWalkIsVisible(t *testing.T) {
	s := newIDFormStand(t)
	opts := s.opts()
	opts.JudgedDomains = []string{"services/probe/docs/nothing-here"}
	var log strings.Builder
	findings, census, err := AuditDocsIDForm(opts, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом обходе найдено %d", len(findings))
	}
	if census.DocFiles != 0 || census.Judged != 0 {
		t.Fatalf("пустой обход отчитался как непустой: страниц %d, рассужено %d",
			census.DocFiles, census.Judged)
	}
}
