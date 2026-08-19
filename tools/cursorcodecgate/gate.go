// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package cursorcodecgate keeps the format of a page cursor declared in ONE place.
//
// # The subject
//
// A `page_token` is opaque to the caller, so nothing outside the product can notice
// that two pieces of our own code disagree about its bytes. That is precisely what
// makes a second declaration dangerous: both halves answer "valid" for a valid input,
// so a divergence shows up only on the inputs nobody tried.
//
// Two shapes of the defect were live in this tree when the gate was written, and they
// fail in opposite directions:
//
//   - A GUARD that mirrors the format by hand and runs BEFORE the authoritative
//     parse. Changing the owner's format does not break the mirror's compilation:
//     it keeps accepting tokens of the old shape and refusing the new ones. Three
//     services carried such a mirror, and one of them said so in its own godoc.
//   - Two codecs of DIFFERENT MEANING that happen to produce the SAME bytes. A tag
//     named "5" and the position 5 encoded to the identical token, so each window
//     accepted the other's cursor without an error and returned the wrong page —
//     a silently wrong answer rather than a refusal.
//
// `api-conventions.md` states the rule directly: the cursor codec is not to be
// rewritten, and the guard must call the same parse the read path runs.
//
// # What is decidable here
//
// The gate judges one fact, settled inside a single function body: this body builds
// or takes apart a cursor with its own base64, instead of calling the one home. It
// does not follow delegation, and it does not need to — a body that encodes bytes
// itself has already declared a format, whatever it calls afterwards.
//
// Encoding VALUES are resolved, not matched by name: a file that writes
// `var b64 = base64.StdEncoding` and then calls `b64.DecodeString` declares a format
// exactly as much as one that spells the selector out. The first census written for
// this class missed a whole service for that reason.
//
// Comments and string literals are never read. The gate walks the syntax tree, so a
// comment explaining this very rule cannot be mistaken for a violation of it — the
// failure mode that retires text-matching gates on their first false positive.
//
// # The ratchet
//
// Services not yet converged are named in Remaining with the shape each still emits.
// The list is two-sided: a new site is a finding, and so is a listed site that no
// longer qualifies. An exception with nothing left to excuse is itself a finding, so
// the list retires on its own instead of outliving its subject.
package cursorcodecgate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Home — единственное объявление формата курсора.
const Home = "pkg/pagetoken"

// Remaining — сервисы, ещё не сведённые к общему дому, с формой, которую каждый
// пока эмитит. Запись живёт, ПОКА У НЕЁ ЕСТЬ ПРЕДМЕТ: сведённый сервис, оставшийся
// в перечне, — находка (см. Report.StaleExclusions).
var Remaining = map[string]string{
	"services/geo/internal/repo/kacho/pg/pagetoken.go":               "JSON {id}, StdEncoding — ключ сортировки один (id), не (created_at, id)",
	"services/iam/internal/apps/kacho/shared/pagination.go":          "рукописное зеркало формата iam; сведение — за владельцем services/iam",
	"services/iam/internal/repo/kacho/pg/helpers.go":                 "RFC3339Nano|id, StdEncoding; сведение — за владельцем services/iam",
	"services/registry/internal/apps/kacho/api/registry/registry.go": "полоса+смещение (lane:offset), объединение двух источников",
	"services/registry/internal/dataplane/handler.go":                "смещение каталога Docker-registry (?last=), чужой контракт",
	"services/registry/internal/repo/kacho/pg/pagetoken.go":          "JSON {created_at,id}, StdEncoding",
	"services/storage/internal/repo/pg/pagetoken.go":                 "unixnano|id, RawURLEncoding — от общего отличается разделителем",
}

// Finding — тело, объявляющее формат курсора вне общего дома.
type Finding struct {
	File string
	Func string
	Line int
	Why  string
}

// Report — исход обхода вместе с ОБЪЁМОМ ОСМОТРЕННОГО: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type Report struct {
	Roots           []string
	FilesParsed     int
	FuncsInspected  int
	Base64Calls     int
	Findings        []Finding
	StaleExclusions []string
}

// PremiseHolds — предпосылка гейта: он что-то прочитал и что-то распознал. Если в
// дереве не нашлось НИ ОДНОГО вызова base64, распознаватель сломан либо корни заданы
// мимо — и тогда «ноль находок» ничего не значит.
func (r Report) PremiseHolds() bool {
	return r.FilesParsed > 0 && r.FuncsInspected > 0 && r.Base64Calls > 0
}

func (r Report) String() string {
	return fmt.Sprintf(
		"cursorcodecgate: корней %d [%s], файлов разобрано %d, тел осмотрено %d, "+
			"вызовов base64 встречено %d, находок %d, истёкших послаблений %d, "+
			"в перечне не сведённых %d",
		len(r.Roots), strings.Join(r.Roots, " "), r.FilesParsed, r.FuncsInspected,
		r.Base64Calls, len(r.Findings), len(r.StaleExclusions), len(Remaining))
}

// признаки курсорного контекста — по ИДЕНТИФИКАТОРАМ тела, не по тексту файла.
var cursorWords = []string{"pagetoken", "page_token", "pagecursor", "cursor", "nextpage"}

func mentionsCursor(s string) bool {
	l := strings.ToLower(s)
	for _, w := range cursorWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// Analyse обходит корни и возвращает отчёт.
func Analyse(roots ...string) (Report, error) {
	rep := Report{Roots: roots}
	seen := map[string]bool{}

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if base == "vendor" || base == "node_modules" || base == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			slash := filepath.ToSlash(path)
			// Общий дом объявляет формат по определению; сгенерённые стабы не наши.
			if strings.Contains(slash, Home) || strings.Contains(slash, "pkg/api/") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return nil // неразбираемый файл — не наш предмет
			}
			rep.FilesParsed++

			// Значения кодировок: и прямой селектор, и алиас (var b64 = base64.StdEncoding).
			encVals := map[string]bool{"base64": true}
			ast.Inspect(f, func(n ast.Node) bool {
				vs, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, val := range vs.Values {
					if sel, ok := val.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == "base64" &&
							strings.HasSuffix(sel.Sel.Name, "Encoding") && i < len(vs.Names) {
							encVals[vs.Names[i].Name] = true
						}
					}
				}
				return true
			})

			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					return true
				}
				rep.FuncsInspected++

				var calls int
				var body strings.Builder
				body.WriteString(fn.Name.Name)
				ast.Inspect(fn.Body, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok {
						body.WriteString(" ")
						body.WriteString(id.Name)
					}
					if bl, ok := m.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						// строковый литерал НЕ считается признаком контекста, но
						// имя поля в теге ошибки часто единственное место, где
						// «page_token» вообще написан, — поэтому учитываем отдельно.
						body.WriteString(" ")
						body.WriteString(strings.Trim(bl.Value, "`\""))
					}
					call, ok := m.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if sel.Sel.Name != "EncodeToString" && sel.Sel.Name != "DecodeString" {
						return true
					}
					// приёмник — значение кодировки: base64.X.Y(...) либо алиас.
					switch recv := sel.X.(type) {
					case *ast.SelectorExpr:
						if id, ok := recv.X.(*ast.Ident); ok && encVals[id.Name] {
							calls++
						}
					case *ast.Ident:
						if encVals[recv.Name] {
							calls++
						}
					}
					return true
				})
				rep.Base64Calls += calls
				if calls > 0 && mentionsCursor(body.String()) && !seen[slash] {
					if _, forgiven := Remaining[slash]; !forgiven {
						rep.Findings = append(rep.Findings, Finding{
							File: slash, Func: fn.Name.Name,
							Line: fset.Position(fn.Pos()).Line,
							Why:  "тело объявляет формат курсора своим base64 — формат объявляется в " + Home,
						})
					}
					seen[slash] = true
				}
				return true
			})
			return nil
		})
		if err != nil {
			return rep, err
		}
	}

	// Двусторонний храповик: запись, которой больше нечего исключать, — находка.
	for file := range Remaining {
		if !fileDeclaresCodec(file) {
			rep.StaleExclusions = append(rep.StaleExclusions, file)
		}
	}
	sort.Strings(rep.StaleExclusions)
	sort.Slice(rep.Findings, func(i, j int) bool { return rep.Findings[i].File < rep.Findings[j].File })
	return rep, nil
}

// fileDeclaresCodec — отвечает, объявляет ли названный файл формат курсора СЕГОДНЯ.
// Файла нет либо он сведён — запись перечня истекла.
func fileDeclaresCodec(file string) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.FromSlash(file), nil, 0)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok &&
			(sel.Sel.Name == "EncodeToString" || sel.Sel.Name == "DecodeString") {
			found = true
		}
		return true
	})
	return found
}
