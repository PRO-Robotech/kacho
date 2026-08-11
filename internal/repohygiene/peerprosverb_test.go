// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Проза полосы несёт ровно столько глаголов, сколько полоса заполнит.
//
// Гейт на класс, а не на три известных объявления: следующее ребро к соседу
// заведёт свою прозу, и расхождение обязано краснеть в момент появления.
func TestPeerProseVerbMatchesItsLane(t *testing.T) {
	root := repoRoot(t)
	findings, census, err := auditPeerProse(root)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	t.Logf("перепись: файлов разобрано %d, литералов peer.Prose %d, значений проверено %d, "+
		"значений вне зрения гейта %d", census.Files, census.Literals, census.Values, len(census.Unresolved))
	for _, u := range census.Unresolved {
		t.Logf("вне зрения гейта (значение вычисляется в рантайме): %s", u)
	}
	if census.Literals == 0 {
		t.Fatalf("литералов peer.Prose в дереве ноль — либо носитель полос снят, либо область " +
			"обхода гейта сломана; «ноль находок» на нуле прочитанного ничего не значит")
	}
	for _, f := range findings {
		t.Errorf("%s: %s", f.Where, f.What)
	}
}

// Основание гейта — механика носителя: идентификатор подставляется только в
// полосы, которые чужой ресурс НАЗЫВАЮТ. Изменится механика — запрет станет
// ложью, поэтому гейт проверяет собственную предпосылку, а не полагается на
// память автора.
func TestPeerProseGatePremiseStillHolds(t *testing.T) {
	path := filepath.Join(repoRoot(t), "pkg", "peer", "outcome.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("разбор носителя полос: %v", err)
	}

	var declared, calledInStatus bool
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		switch fn.Name.Name {
		case "NamesResource":
			declared = true
		case "Status":
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if ok && sel.Sel.Name == "NamesResource" {
					calledInStatus = true
				}
				return true
			})
		}
	}
	if !declared {
		t.Fatalf("у полосы нет предиката NamesResource — основание гейта исчезло: если решение " +
			"«называет ли полоса ресурс» переехало, гейт обязан переехать за ним тем же изменением")
	}
	if !calledInStatus {
		t.Fatalf("Status больше не спрашивает NamesResource — подстановка идентификатора снова " +
			"принимается не там, где объявлена; требование гейта к прозе перестало следовать из кода")
	}
}

// --- инъекция: обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву ---

const synthPeerCaller = `package clients

import "github.com/PRO-Robotech/kacho/pkg/peer"

const zoneUnavailableText = "geo zone validation unavailable"

func legit(o peer.Outcome, id string) error {
	return o.Status(
		peer.Ref{Service: "storage", ResourceType: "geo.zone", ResourceID: id},
		peer.Prose{Missing: "unknown zone id '%s'", Unavailable: zoneUnavailableText})
}

func opaqueLegit(o peer.Outcome, id string) error {
	return o.Status(
		peer.Ref{Service: "nlb", ResourceType: "vpc.address", ResourceID: id},
		peer.Prose{Missing: "Illegal argument addressId", Opaque: true})
}

// %s в комментарии — не проза, и текстовый поиск принял бы его за неё.
func runtimeValued(o peer.Outcome, id, text string) error {
	return o.Status(
		peer.Ref{Service: "storage", ResourceType: "geo.region", ResourceID: id},
		peer.Prose{Missing: "unknown region id '%s'", Unavailable: text})
}
`

// synthWrapperDefect — тот же дефект, пронесённый ЧЕРЕЗ ОБЁРТКУ: в литерале
// стоит параметр, глагол приходит с вызова. Без шага к вызывающим гейт видел бы
// здесь имя, а не текст, и обёртка стала бы необъявленным послаблением.
const synthWrapperDefect = `package wrapped

import "github.com/PRO-Robotech/kacho/pkg/peer"

func lane(o peer.Outcome, id, unavailable string) error {
	return o.Status(
		peer.Ref{Service: "storage", ResourceType: "geo.zone", ResourceID: id},
		peer.Prose{Missing: "unknown zone id '%s'", Unavailable: unavailable})
}

func caller(o peer.Outcome, id string) error {
	return lane(o, id, "zone %s lookup unavailable")
}
`

// synthDefect — глагол в прозе полосы, которая его не заполнит.
const synthDefect = `package broken

import "github.com/PRO-Robotech/kacho/pkg/peer"

func lane(o peer.Outcome, id string) error {
	return o.Status(
		peer.Ref{Service: "nlb", ResourceType: "geo.region", ResourceID: id},
		peer.Prose{Missing: "Region %s not found", Unavailable: "region %s lookup unavailable"})
}
`

func synthPeerTree(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range peerProseScanRoots {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	write := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("services/storage/internal/clients/geo_client.go", synthPeerCaller)
	for rel, body := range extra {
		write(rel, body)
	}
	return root
}

// Законная сторона: гейт молчит на прозе, согласованной со своей полосой, —
// включая непрозрачную форму (глаголов ноль во всех полях) и значение, которое
// вычисляется в рантайме (его гейт не видит и обязан это назвать, а не выдумать
// находку).
func TestPeerProseGateStaysSilentOnLawfulProse(t *testing.T) {
	findings, census, err := auditPeerProse(synthPeerTree(t, nil))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл дефект в законной прозе: %+v", findings)
	}
	if census.Literals != 3 {
		t.Fatalf("литералов осмотрено %d, ожидалось 3 — гейт читает не то, что положено", census.Literals)
	}
	if len(census.Unresolved) != 1 || !strings.Contains(census.Unresolved[0], "Unavailable") {
		t.Fatalf("граница зрения гейта названа неверно: %v", census.Unresolved)
	}
}

// Сторона дефекта: гейт краснеет и НАЗЫВАЕТ координату.
func TestPeerProseGateCatchesTheVerbMismatch(t *testing.T) {
	root := synthPeerTree(t, map[string]string{"services/nlb/internal/clients/geo/broken.go": synthDefect})
	findings, _, err := auditPeerProse(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Where, "broken.go") || !strings.Contains(findings[0].Where, "Unavailable") {
		t.Fatalf("гейт покраснел, но координату не назвал: %+v", findings[0])
	}
}

// Обёртка не проносит дефект мимо гейта: текст приходит с вызова, и гейт
// доходит до вызова.
func TestPeerProseGateSeesThroughAWrapper(t *testing.T) {
	root := synthPeerTree(t, map[string]string{"services/nlb/internal/clients/wrapped/lane.go": synthWrapperDefect})
	findings, census, err := auditPeerProse(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Where, "lane.go") {
		t.Fatalf("глагол пронесён через обёртку незамеченным: findings=%+v, вне зрения=%v",
			findings, census.Unresolved)
	}
}
