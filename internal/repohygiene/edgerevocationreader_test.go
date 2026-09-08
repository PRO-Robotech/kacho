// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edgerevocationreader_test.go — у КРАЯ есть читатель отзыва, и он стоит НА
// ПУТИ ЗАПРОСА (задача продукта #2361).
//
// # Почему этот гейт заведён ЗДЕСЬ, а не оставлен там, где он был
//
// Утверждение о клиенте края жило внутри службы доступа
// (`services/iam/internal/apps/kaname/api/session_revocations/is_revoked_doc_test.go`):
// оно сверяло, что комментарии полосы отзыва не отрицают ЖИВОГО читателя, и
// читателя это утверждение искало у КРАЯ — в дереве платформы.
//
// После разреза носитель уедет со службой и в её клоне честно объявит третий
// исход: клиента края в поставке модуля нет by construction. Клиент при этом
// останется у платформы — и останется БЕЗ СТОРОЖА. Молчание пропущенной пробы
// неотличимо от исправной работы, поэтому платформенная половина переезжает
// сюда ДО разреза.
//
// Это РАЗДЕЛЕНИЕ, а не перенос: прежний носитель продолжает судить СВОИ
// комментарии (они уезжают с ним), здесь судится сторона платформы.
//
// # Предмет — и почему он про безопасность, а не про опрятность
//
// Контроль, действующий на ВЫДАЧЕ и не действующий на ПРЕДЪЯВЛЕНИИ, отзывом не
// является: он лишь не выдаёт нового, а предъявленное продолжает проходить до
// истечения срока (`security.md` §«Контроль, действующий на ВЫДАЧЕ…»). У края
// читатель отзыва — единственное, что превращает запись отзыва в отказ на пути
// запроса. Клиент, объявленный и никем не званый, есть ровно тот случай: код
// присутствует, провязан, читается обзором — и не отказал ни разу.
//
// # Три оси, каждая падает сама
//
//  1. ОБЪЯВЛЕНИЕ. Клиент края объявляет метод чтения. Нет метода — читать
//     отзыв нечем, и всякое утверждение о полосе вакуумно.
//  2. ВЫЗЫВАЮЩИЙ. У метода есть вызывающий в НЕ-тестовом коде края, и он не сам
//     клиент: объявление, которое зовёт только себя, — мёртвый контроль.
//  3. ПОРТ. Вызывающий обращается к методу через объявленный ПОРТ (интерфейс
//     края), а не к типу клиента напрямую. Порт — то, что даёт подменить
//     авторитет в пробе; его пропажа означает, что полоса перестала быть
//     проверяемой.
//
// Разбор идёт ПО УЗЛАМ Go, а не поиском по тексту: имя метода встречается в
// комментариях этого же файла, и предикат по подстроке зеленел бы на
// собственном объяснении.
//
// # Чего гейт НЕ утверждает — сказано, чтобы «зелено» не читалось шире
//
// Он не судит, ВЕРНО ли решение читателя (fail-closed на недоступности,
// длительность кеша, форма ответа): это предмет проб самого края. Здесь
// судится ЖИВОСТЬ полосы — что читатель есть и что его кто-то зовёт.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// edgeRevocationClientRel — файл клиента края, от корня дерева платформы.
	edgeRevocationClientRel = "gateway/internal/clients/session_revocations_client.go"
	// edgeRevocationReadMethod — метод чтения отзыва. Его существование и есть
	// предмет первой оси.
	edgeRevocationReadMethod = "IsSessionRevoked"
	// edgeRevocationScanDir — дерево края, по которому ищется вызывающий.
	edgeRevocationScanDir = "gateway/"
)

// edgeRevocationFacts — вход предиката. Отделён от обхода, чтобы предикат
// прогонялся инъекцией, не трогая дерева.
type edgeRevocationFacts struct {
	// ClientDeclares — объявляет ли клиент края метод чтения.
	ClientDeclares bool
	// CallerFiles — не-тестовые файлы края, где разбор нашёл ВЫЗОВ метода
	// (сам файл клиента исключён: объявление, зовущее себя, вызывающим не является).
	CallerFiles []string
	// PortFiles — не-тестовые файлы края, объявляющие метод в интерфейсе-порте.
	PortFiles []string
}

// auditEdgeRevocationReader — предикат трёх осей. Пусто = норма.
func auditEdgeRevocationReader(f edgeRevocationFacts) []string {
	var found []string
	if !f.ClientDeclares {
		found = append(found, fmt.Sprintf(
			"%s не объявляет %s — читать отзыв краю нечем, и полоса отзыва действует "+
				"только на ВЫДАЧЕ: предъявленное удостоверение продолжает проходить",
			edgeRevocationClientRel, edgeRevocationReadMethod))
	}
	if len(f.CallerFiles) == 0 {
		found = append(found, fmt.Sprintf(
			"у %s нет НИ ОДНОГО вызывающего в прод-коде края — контроль объявлен и "+
				"не исполняется ни на одном запросе", edgeRevocationReadMethod))
	}
	if len(f.PortFiles) == 0 {
		found = append(found, fmt.Sprintf(
			"%s не объявлен НИ ОДНИМ портом края — авторитет нечем подменить, "+
				"и полоса перестала быть проверяемой", edgeRevocationReadMethod))
	}
	sort.Strings(found)
	return found
}

// edgeRevocationScan — обход дерева края: объявление, вызывающие, порты.
func edgeRevocationScan(t *testing.T, root string, tt *trackedTree) (edgeRevocationFacts, int, int) {
	t.Helper()
	var (
		f              edgeRevocationFacts
		scanned, parsd int
		rels           []string
	)
	for rel := range tt.files {
		if !strings.HasPrefix(rel, edgeRevocationScanDir) {
			continue
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		scanned++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if err != nil {
			t.Fatalf("разбор %s: %v — нечитаемый файл есть НАХОДКА, а не «вызывающих нет»", rel, err)
		}
		parsd++

		isClient := rel == edgeRevocationClientRel
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.FuncDecl:
				// Объявление МЕТОДА с этим именем у типа клиента.
				if isClient && v.Recv != nil && v.Name != nil && v.Name.Name == edgeRevocationReadMethod {
					f.ClientDeclares = true
				}
			case *ast.InterfaceType:
				if v.Methods == nil {
					return true
				}
				for _, m := range v.Methods.List {
					for _, name := range m.Names {
						if name.Name == edgeRevocationReadMethod {
							f.PortFiles = append(f.PortFiles, rel)
						}
					}
				}
			case *ast.CallExpr:
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil || sel.Sel.Name != edgeRevocationReadMethod {
					return true
				}
				// Сам клиент вызывающим не считается: объявление, зовущее себя,
				// контроля не исполняет.
				if isClient {
					return true
				}
				f.CallerFiles = append(f.CallerFiles, rel)
			}
			return true
		})
	}
	f.CallerFiles = uniqSorted(f.CallerFiles)
	f.PortFiles = uniqSorted(f.PortFiles)
	return f, scanned, parsd
}

func uniqSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// TestEdgeRevocationLaneHasAReaderOnTheRequestPath — сам гейт.
func TestEdgeRevocationLaneHasAReaderOnTheRequestPath(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	if !tt.hasFile(edgeRevocationClientRel) {
		t.Fatalf("%s нет в индексе дерева — гейт не может назвать предмет, о котором "+
			"он говорит; это НАХОДКА, а не «читателя нет»", edgeRevocationClientRel)
	}

	f, scanned, parsd := edgeRevocationScan(t, root, tt)
	t.Logf("перепись: прод-файлов края осмотрено %d, разобрано %d; клиент объявляет %s: %t; "+
		"вызывающих %d (%s); портов %d (%s)",
		scanned, parsd, edgeRevocationReadMethod, f.ClientDeclares,
		len(f.CallerFiles), strings.Join(f.CallerFiles, ", "),
		len(f.PortFiles), strings.Join(f.PortFiles, ", "))

	if scanned == 0 {
		t.Fatalf("под %s не осмотрено ни одного прод-файла — обход пуст, вердикт беспредметен",
			edgeRevocationScanDir)
	}

	found := auditEdgeRevocationReader(f)
	if len(found) > 0 {
		t.Fatalf("полоса отзыва у края разошлась с деревом — %d находка(и):\n  %s\n\n"+
			"Контроль, действующий на выдаче и не действующий на предъявлении, отзывом "+
			"не является: он не выдаёт нового, а предъявленное проходит до истечения срока.",
			len(found), strings.Join(found, "\n  "))
	}
}
