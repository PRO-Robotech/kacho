// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// leaserefusalassuccess_test.go — гейт: полоса освобождения аренды не читает
// отказ владельца как доказательство снятой аренды.
//
// # Предмет
//
// Задача #439. Освобождение аренды адреса выводило «работа сделана» из КОДА
// ОШИБКИ соседа. Код ошибки для такого вывода непригоден by construction: у
// владельца есть законные и обязательные причины отвечать одинаково на разные
// положения дел — промах чужого проекта намеренно неотличим от «нет доступа»,
// а опрос операции схлопывает «строки нет», «владелец другой» и «нет ключа
// владельца» в один ответ. Полоса принимала одно из этих положений за успех,
// после чего сносила строку потребителя — единственную координату, по которой
// аренду вообще можно найти: реконсайлер идёт ОТ неё, обратной развёртки у
// владельца нет.
//
// Починка сняла сам вопрос: аренда снимается глаголом, анкоренным на ПРОЕКТЕ, и
// исход приезжает ПОЛЕМ ответа. Гейт стережёт, чтобы вопрос не вернулся.
//
// # Что гейт держит — два СЧЁТНЫХ свойства
//
//	(а) вызовов `AddressServiceClient.Delete` из `services/nlb` — НОЛЬ.
//	    Публичное удаление адреса анкорено пообъектно, поэтому его ответ
//	    неоднозначен для вызывающего by construction. Полоса освобождения не
//	    вправе его звать.
//
//	(б) мест, где отказ соседа в пакете `services/nlb/internal/clients/vpc`
//	    возвращается как УСПЕХ, — НОЛЬ. Свойство сформулировано ПО ИСХОДУ, а не
//	    по форме ветки: до починки два из трёх мест были записаны как
//	    `if st.Code() == codes.NotFound`, и лишь одно — как `case codes.NotFound:`.
//	    Предикат, написанный на `case`, промахнулся бы мимо ОБОИХ главных мест.
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
// Он смотрит на ОДНУ полосу — освобождение аренды у nlb. Тот же класс живёт
// шире: ответ соседа читается как доказательство отсутствия и в дренажах
// очередей (строка помечается отправленной), и в уборке провайдера, и в
// удалении тега реестра. Радиус измерен и заведён отдельными задачами; гейт их
// НЕ закрывает и не притворяется, что закрывает — расширять его на чужие полосы
// без их починки значило бы завести перечень прощённых, а каждая запись в таком
// перечне есть место, куда дефект вносят незамеченным.
//
// # Перепись
//
// Гейт печатает объём осмотренного: файлов прочитано, функций обойдено,
// ветвлений на «не найдено» рассмотрено. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного»; ноль прочитанных файлов при существующем пакете —
// поломка разбора, а не чистота.
//
// # Проверка СВОЕЙ предпосылки
//
// Запрет осмыслен, пока пакет-предмет существует и nlb по-прежнему импортирует
// стабы vpc. Исчезни это — запрет стал бы ложью молча, поэтому гейт заявляет
// предпосылку сам и падает, когда её не стало.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `leaserefusalassuccess_injection_test.go`.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// releaseLanePkg — пакет-предмет: клиент vpc у nlb, где живёт полоса
// освобождения аренды.
const releaseLanePkg = "services/nlb/internal/clients/vpc"

// nlbTree — поддерево, из которого публичное удаление адреса звать нельзя.
const nlbTree = "services/nlb"

// laneCensus — объём осмотренного. Печатается всегда.
type laneCensus struct {
	files     int
	funcs     int
	notFounds int
}

func (c laneCensus) String() string {
	return fmt.Sprintf("файлов %d · функций %d · ветвлений на «не найдено» %d",
		c.files, c.funcs, c.notFounds)
}

// leaseRelTo — путь относительно корня репозитория (координата в тексте падения).
func leaseRelTo(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRoot(t), path)
	if err != nil {
		return path
	}
	return rel
}

func TestReleaseLaneNeverReadsARefusalAsProofOfRelease(t *testing.T) {
	root := repoRoot(t)

	// --- предпосылка: предмет запрета существует ------------------------------
	pkgDir := filepath.Join(root, releaseLanePkg)
	if st, err := os.Stat(pkgDir); err != nil || !st.IsDir() {
		t.Fatalf("предпосылка гейта не выполнена: пакета %s нет (%v). "+
			"Запрет обоснован тем, что полоса освобождения живёт здесь; "+
			"переехала полоса — переезжает и гейт, иначе он молча стал ложью", releaseLanePkg, err)
	}
	if !treeImportsVPCStubs(t, filepath.Join(root, nlbTree)) {
		t.Fatalf("предпосылка гейта не выполнена: %s больше не импортирует стабы vpc — "+
			"запрет на публичное удаление адреса стал беспредметным", nlbTree)
	}

	// --- (а) публичное удаление адреса из nlb не зовётся ----------------------
	deleteCalls, censusA := publicAddressDeleteCallSites(t, filepath.Join(root, nlbTree))
	// У (а) считаются файлы и функции: ветвления на «не найдено» — предмет (б),
	// и печатать здесь их ноль значило бы утверждать о дереве неправду.
	t.Logf("перепись (а): файлов %d · функций %d", censusA.files, censusA.funcs)
	if censusA.files == 0 {
		t.Fatalf("перепись (а) прочитала ноль файлов — это поломка разбора, а не чистота")
	}
	for _, c := range deleteCalls {
		t.Errorf("%s: полоса зовёт публичное удаление адреса (`AddressServiceClient.Delete`). "+
			"Оно анкорено пообъектно, поэтому его отказ неотличим от «объекта нет»: "+
			"успех, выведенный из такого ответа, снесёт строку потребителя вместе с "+
			"последней координатой аренды (#439). Снятие аренды идёт глаголом с "+
			"названным исходом", c)
	}

	// --- (б) отказ соседа не возвращается успехом -----------------------------
	swallows, censusB := refusalsReturnedAsSuccess(t, pkgDir)
	t.Logf("перепись (б): %s", censusB)
	if censusB.files == 0 || censusB.funcs == 0 {
		t.Fatalf("перепись (б) прочитала ноль файлов/функций — поломка разбора, а не чистота")
	}
	for _, s := range swallows {
		t.Errorf("%s: ветка на «не найдено» возвращает УСПЕХ. "+
			"Ответ соседа не несёт утверждения «ресурса нет» — им же отвечают на "+
			"промах чужого проекта и на опрос без ключа владельца. Исход обязан "+
			"приезжать ПОЛЕМ ответа, а отказ оставаться отказом (#439)", s)
	}
}

// treeImportsVPCStubs — предпосылка (а): дерево всё ещё говорит со стабами vpc.
//
// Состав берётся у ИНДЕКСА, а не с диска: под `services/` на всякой машине, где
// поднимали стенд, лежит игнорируемое, и обход диска сделал бы вердикт
// свойством рабочего каталога, а не коммита.
func treeImportsVPCStubs(t *testing.T, dir string) bool {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		t.Fatalf("предпосылка гейта: состав дерева %s не взять: %v", dir, err)
	}
	for _, path := range files {
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		if strings.Contains(string(b), "kacho/pkg/api/kacho/cloud/vpc/v1") {
			return true
		}
	}
	return false
}

// publicAddressDeleteCallSites — селекторы `.Delete(` на значении, чьё имя
// указывает на клиент публичного AddressService.
//
// Разбор AST, а не подстрока: слово `Delete` встречается в прозе комментариев
// об этой самой полосе, и текстовый предикат краснел бы на собственном
// объяснении.
func publicAddressDeleteCallSites(t *testing.T, dir string) ([]string, laneCensus) {
	t.Helper()
	// Состав — у индекса, а не с диска (см. treeImportsVPCStubs).
	files, ferr := treecorpus.UnderWithSuffix(dir, ".go")
	if ferr != nil {
		t.Fatalf("перепись (а): состав дерева %s не взять: %v", dir, ferr)
	}
	return scanPublicAddressDelete(t, files)
}

// scanPublicAddressDelete — сам разбор. Отделён от получения состава, чтобы
// инъекция могла подать СВОЙ перечень путей: синтетика лежит вне рабочего
// дерева git, и брать её состав у индекса нечем.
func scanPublicAddressDelete(t *testing.T, files []string) ([]string, laneCensus) {
	t.Helper()
	var hits []string
	var c laneCensus
	fset := token.NewFileSet()

	for _, path := range files {
		// Пробы исключены намеренно: дублёр владельца ВПРАВЕ реализовать любой
		// его метод, в том числе публичное удаление, — он моделирует владельца,
		// а не полосу освобождения.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			continue
		}
		c.files++
		ast.Inspect(f, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncDecl); ok {
				c.funcs++
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Delete" {
				return true
			}
			recv, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// Приёмник — поле клиента публичного AddressService. Имя поля
			// названо в самом клиенте (`addrs`), и другого публичного адресного
			// стаба у nlb нет.
			if recv.Sel.Name == "addrs" {
				hits = append(hits, fmt.Sprintf("%s:%d", leaseRelTo(t, path), fset.Position(call.Pos()).Line))
			}
			return true
		})
	}
	return hits, c
}

// refusalsReturnedAsSuccess — ветки, где сравнение с «не найдено» приводит к
// возврату успеха.
//
// Свойство берётся ПО ИСХОДУ: узнаётся и `if`, и `case`, а решает то, что
// возвращает тело — `nil` в позиции ошибки означает «работа сделана».
func refusalsReturnedAsSuccess(t *testing.T, dir string) ([]string, laneCensus) {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		t.Fatalf("перепись (б): состав пакета %s не взять: %v", dir, err)
	}
	return scanRefusalsReturnedAsSuccess(t, files)
}

// scanRefusalsReturnedAsSuccess — сам разбор; отделён по той же причине, что и
// у свойства (а).
func scanRefusalsReturnedAsSuccess(t *testing.T, files []string) ([]string, laneCensus) {
	t.Helper()
	var hits []string
	var c laneCensus
	fset := token.NewFileSet()

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			continue
		}
		c.files++
		ast.Inspect(f, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncDecl); ok {
				c.funcs++
			}
			switch node := n.(type) {
			case *ast.IfStmt:
				if !mentionsNotFound(node.Cond) {
					return true
				}
				c.notFounds++
				if bodyReturnsSuccess(node.Body) {
					hits = append(hits, fmt.Sprintf("%s:%d", leaseRelTo(t, path), fset.Position(node.Pos()).Line))
				}
			case *ast.CaseClause:
				matched := false
				for _, expr := range node.List {
					if mentionsNotFound(expr) {
						matched = true
					}
				}
				if !matched {
					return true
				}
				c.notFounds++
				if stmtsReturnSuccess(node.Body) {
					hits = append(hits, fmt.Sprintf("%s:%d", leaseRelTo(t, path), fset.Position(node.Pos()).Line))
				}
			}
			return true
		})
	}
	return hits, c
}

// mentionsNotFound — выражение сравнивается с «не найдено» соседа. Узнаются обе
// формы, которыми это записано в дереве: код gRPC и полоса общего носителя.
func mentionsNotFound(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "NotFound" || sel.Sel.Name == "OutcomeMissing" {
			found = true
		}
		return true
	})
	return found
}

// bodyReturnsSuccess — тело возвращает успех.
func bodyReturnsSuccess(b *ast.BlockStmt) bool {
	if b == nil {
		return false
	}
	return stmtsReturnSuccess(b.List)
}

// stmtsReturnSuccess — среди операторов есть `return`, чья позиция ошибки —
// `nil`. Именно это и есть «отказ прочитан как выполненная работа»; форма
// ветвления к делу не относится.
func stmtsReturnSuccess(stmts []ast.Stmt) bool {
	for _, st := range stmts {
		ret, ok := st.(*ast.ReturnStmt)
		if !ok {
			continue
		}
		if len(ret.Results) == 0 {
			// Голый `return` внутри функции, возвращающей ошибку, — тот же
			// успех, но named-result здесь не используется ни разу, поэтому
			// такая форма означала бы функцию без результата. Не находка.
			continue
		}
		last := ret.Results[len(ret.Results)-1]
		if id, ok := last.(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
	}
	return false
}
