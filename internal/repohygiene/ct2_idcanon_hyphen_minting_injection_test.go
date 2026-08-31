// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_idcanon_hyphen_minting_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Помимо синтетики здесь есть проба на НАСТОЯЩЕМ дереве против ДОФИКСОВОГО
// каталога: она воспроизводит красное, ради которого гейт заведён, на живых
// данных, а не на макете.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

type canonStand struct{ root string }

// newCanonStand — синтетическое дерево. Префиксы объявлены КОНСТАНТАМИ ДОМЕНА и
// приходят в чеканку через импорт: строкового литерала в вызове нет ни одного,
// поэтому распознаватель, ищущий `NewHyphenID("…")` по тексту, не увидел бы
// здесь ничего.
func newCanonStand(t *testing.T) *canonStand {
	t.Helper()
	s := &canonStand{root: t.TempDir()}

	s.write(t, "pkg/ids/ids.go", `
package ids

func NewID(prefix string) string       { return prefix + "00000000000000000" }
func NewHyphenID(prefix string) string { return prefix + "-00000000000000000" }
`)
	s.write(t, "services/probe/internal/domain/prefixes.go", `
package domain

const (
	PrefixKnownHyphen   = "khp"
	PrefixMissingHyphen = "mhp"
	PrefixConcatOnly    = "cnc"
)
`)
	s.write(t, "services/probe/internal/app/mint.go", `
package app

import (
	"example.test/probe/pkg/ids"
	"example.test/probe/services/probe/internal/domain"
)

func Known() string   { return ids.NewHyphenID(domain.PrefixKnownHyphen) }
func Missing() string { return ids.NewHyphenID(domain.PrefixMissingHyphen) }
// Слитная чеканка префиксом, которого в дефисном каталоге нет, — НЕ находка:
// предмет гейта — дефисная форма.
func Concat() string { return ids.NewID(domain.PrefixConcatOnly) }
`)
	return s
}

func (s *canonStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *canonStand) run(t *testing.T, canon map[string]struct{}) ([]HyphenCanonFinding, HyphenCanonCensus) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditHyphenMintedPrefixesInCanon(
		DocsIDFormOptions{Root: s.root, ModulePath: "example.test/probe"}, canon, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

func canonOf(prefixes ...string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, p := range prefixes {
		m[p] = struct{}{}
	}
	return m
}

// TestCanonInjection_SilentWhenCanonCoversMinting — положительный контроль.
// Без него «краснеет на инъекции» было бы неотличимо от «краснеет всегда».
func TestCanonInjection_SilentWhenCanonCoversMinting(t *testing.T) {
	s := newCanonStand(t)
	findings, census := s.run(t, canonOf("khp", "mhp"))
	if len(findings) != 0 {
		t.Fatalf("на полном каталоге найдено %d: %v", len(findings), findings)
	}
	if census.MintedHyphen != 2 {
		t.Fatalf("чеканимых дефисной формой насчитано %d, ожидалось 2 (%v) — молчание получено "+
			"пустым обходом, а не полнотой каталога", census.MintedHyphen, census.MintedNames)
	}
}

// TestCanonInjection_MissingPrefixIsFoundThroughADomainConstant — предмет гейта.
// Литерала в вызове нет: префикс приезжает константой чужого пакета.
func TestCanonInjection_MissingPrefixIsFoundThroughADomainConstant(t *testing.T) {
	s := newCanonStand(t)

	// Предпосылка самой инъекции: литерального `NewHyphenID("mhp")` в стенде
	// действительно НЕТ. Иначе проба доказывала бы не то, что заявляет.
	raw, err := os.ReadFile(filepath.Join(s.root, "services/probe/internal/app/mint.go"))
	if err != nil {
		t.Fatalf("прочитать стенд: %v", err)
	}
	if strings.Contains(string(raw), `NewHyphenID("`) {
		t.Fatalf("в стенде есть литеральный вызов — инъекция доказывала бы разбор литерала, " +
			"а не резолв константы домена")
	}

	findings, _ := s.run(t, canonOf("khp"))
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	f := findings[0]
	if f.Prefix != "mhp" {
		t.Errorf("находка называет префикс %q, ожидался \"mhp\"", f.Prefix)
	}
	if len(f.Sites) == 0 {
		t.Errorf("находка не называет координату чеканки — по ней нельзя дойти до места")
	}
	if !strings.Contains(f.String(), "mint.go") {
		t.Errorf("текст находки не несёт координату: %s", f.String())
	}
}

// TestCanonInjection_ConcatMintingIsNotJudged — контроль границы предмета:
// слитная чеканка префиксом, которого нет в ДЕФИСНОМ каталоге, находкой не
// является. Без него гейт объявил бы нарушением каждый legacy-префикс дерева.
func TestCanonInjection_ConcatMintingIsNotJudged(t *testing.T) {
	s := newCanonStand(t)
	findings, census := s.run(t, canonOf("khp", "mhp"))
	for _, f := range findings {
		if f.Prefix == "cnc" {
			t.Fatalf("слитно чеканимый префикс объявлен нарушением дефисного каталога: %v", findings)
		}
	}
	for _, n := range census.MintedNames {
		if n == "cnc" {
			t.Fatalf("слитно чеканимый префикс попал в перепись дефисной чеканки: %v", census.MintedNames)
		}
	}
}

// TestCanonInjection_EmptyWalkIsVisible — обход, которому нечего читать, обязан
// быть ОТЛИЧИМ от обхода без находок: премиса вердикта опирается на это.
func TestCanonInjection_EmptyWalkIsVisible(t *testing.T) {
	empty := t.TempDir()
	var log strings.Builder
	findings, census, err := AuditHyphenMintedPrefixesInCanon(
		DocsIDFormOptions{Root: empty, ModulePath: "example.test/probe"}, canonOf("khp"), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом обходе найдено %d", len(findings))
	}
	if census.GoFiles != 0 || census.MintedHyphen != 0 {
		t.Fatalf("пустой обход отчитался как непустой: исходников %d, чеканимых %d",
			census.GoFiles, census.MintedHyphen)
	}
}

// TestCanonInjection_RealTreeAgainstThePreFixCanon — КРАСНОЕ на живых данных.
//
// Каталог берётся настоящий и из него изымаются ровно те две записи, что внесены
// починкой #1722. Это дословно дофиксовое состояние продукта, поэтому проба
// показывает не макет, а то самое красное, ради которого гейт заведён.
func TestCanonInjection_RealTreeAgainstThePreFixCanon(t *testing.T) {
	canon := ids.KnownHyphenPrefixes()
	for _, p := range []string{"sb", "dtb"} {
		if _, ok := canon[p]; !ok {
			t.Fatalf("в настоящем каталоге нет %q — починка #1722 отозвана, и эта проба "+
				"воспроизводит не дофиксовое состояние, а сегодняшнее", p)
		}
		delete(canon, p)
	}

	var log strings.Builder
	findings, census, err := AuditHyphenMintedPrefixesInCanon(
		DocsIDFormOptions{Root: repoRoot(t), ModulePath: "github.com/PRO-Robotech/kacho"}, canon, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	got := map[string]HyphenCanonFinding{}
	for _, f := range findings {
		got[f.Prefix] = f
	}
	for _, want := range []string{"sb", "dtb"} {
		f, ok := got[want]
		if !ok {
			t.Fatalf("на дофиксовом каталоге префикс %q не найден — гейт не воспроизводит "+
				"красное, ради которого заведён; найдено: %v", want, findings)
		}
		if len(f.Sites) == 0 {
			t.Errorf("находка про %q не называет координату чеканки", want)
		}
		t.Logf("ДОФИКСОВОЕ КРАСНОЕ: %s", f.String())
	}
	if len(findings) != 2 {
		t.Errorf("находок %d, ожидались ровно две — лишние означают, что изъятие каталога "+
			"задело не только предмет починки: %v", len(findings), findings)
	}
	if census.MintedHyphen == 0 {
		t.Fatalf("чеканимых дефисной формой 0 — красное получено пустым обходом")
	}
}
