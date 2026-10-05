// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/subscription"
)

// deletion — эмиссия снятия в коде vpc и то, несёт ли её нагрузка ключ имени.
type deletion struct {
	pos     string
	kind    string
	hasName bool
	literal bool // нагрузка — составной литерал карты, ключи видны разбору
}

// goDeletions — перепись эмиссий `Outbox().Emit(ctx, вид, id, якорь, "DELETED", нагрузка)`
// по всему не-тестовому дереву vpc; заодно — число строковых литералов `s`.
func goDeletions(t *testing.T, s string) (dels []deletion, files, literalHits int) {
	t.Helper()
	root, err := filepath.Abs(serviceRoot)
	if err != nil {
		t.Fatalf("корень сервиса не разрешился: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		files++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, uerr := strconv.Unquote(lit.Value); uerr == nil && v == s {
					literalHits++
				}
				return true
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Emit" {
				return true
			}
			inner, ok := sel.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isel, ok := inner.Fun.(*ast.SelectorExpr); !ok || isel.Sel.Name != "Outbox" {
				return true
			}
			if len(call.Args) < argMinimum || literal(call.Args[argChange]) != changeDeleted {
				return true
			}
			del := deletion{pos: fset.Position(call.Pos()).String(), kind: literal(call.Args[argKind])}
			if cl, ok := call.Args[argMinimum-1].(*ast.CompositeLit); ok {
				del.literal = true
				for _, el := range cl.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if literal(kv.Key) == subscription.NamePayloadKey {
						del.hasName = true
					}
				}
			}
			dels = append(dels, del)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева сервиса не удался: %v", err)
	}
	return dels, files, literalHits
}

// TestVpcJournal_NTF359_EveryDeletionOfANamedKindCarriesTheNameSnapshot — NTF3-59,
// З2 (CX3B-30): строка снятия вида с именем несёт снимок имени под ключом
// `name`, взятый из `RETURNING` удаляющего оператора. Без него событие
// `DELETED` уходит без имени, и уведомление о снятии назвать предмет не может.
//
// Вид, объявивший `NameFormNone`, от требования освобождён объявлением; вид без
// объявления — нет (объявление и есть то, что эта полоса заводит).
func TestVpcJournal_NTF359_EveryDeletionOfANamedKindCarriesTheNameSnapshot(t *testing.T) {
	dels, files, _ := goDeletions(t, "")
	if files == 0 || len(dels) == 0 {
		t.Fatalf("осмотрено файлов %d, эмиссий снятия %d — пустой обход вердиктом не является", files, len(dels))
	}
	kinds := Journal().Mapping.Kinds
	perKind := map[string]int{}
	for _, d := range dels {
		perKind[d.kind]++
		if k, ok := kinds[d.kind]; ok && k.NameForm == subscription.NameFormNone {
			continue
		}
		switch {
		case d.kind == "":
			t.Errorf("%s: вид снятия задан не литералом — вне наблюдения переписи", d.pos)
		case !d.literal:
			t.Errorf("%s: нагрузка снятия %s — не литерал карты, ключ имени разбору не виден", d.pos, d.kind)
		case !d.hasName:
			t.Errorf("%s: снятие %s без снимка имени (ключ %q)", d.pos, d.kind, subscription.NamePayloadKey)
		}
	}
	keys := make([]string, 0, len(perKind))
	for k, n := range perKind {
		keys = append(keys, k+"="+strconv.Itoa(n))
	}
	sort.Strings(keys)
	t.Logf("осмотрено файлов %d; эмиссий снятия %d: %v", files, len(dels), keys)
}

// TestVpcJournal_NTF362_NoRowOfTheNetworkDefaultWordIsWritten — NTF3-62 (часть
// AddressPool), Р2: строки вида `AddressPoolNetworkDefault` сняты — у них нет ни
// типа модели, ни читателя. Число строковых литералов этого слова в не-тестовом
// коде vpc — 0 (`git grep -c 'AddressPoolNetworkDefault'` приёмки, разбором по
// узлу, а не по тексту: комментарий о снятом слове законен).
func TestVpcJournal_NTF362_NoRowOfTheNetworkDefaultWordIsWritten(t *testing.T) {
	const word = "AddressPoolNetworkDefault"
	_, files, hits := goDeletions(t, word)
	if files == 0 {
		t.Fatal("осмотрено файлов 0 — пустой обход вердиктом не является")
	}
	// Предпосылка: распознаватель видит литералы вообще — слово, которое
	// пишется в журнал сегодня и снято не будет, находится.
	if _, _, control := goDeletions(t, "AddressPool"); control == 0 {
		t.Fatal("контроль: литерал \"AddressPool\" не найден ни разу — распознаватель слеп")
	}
	if hits != 0 {
		t.Errorf("строковых литералов %q в не-тестовом коде vpc %d, ожидалось 0", word, hits)
	}
	t.Logf("осмотрено файлов %d; литералов %q %d", files, word, hits)
}
