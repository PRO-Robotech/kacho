// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// declaredrpcoverride.go — каждый объявленный RPC переопределён своим
// обработчиком, а не унаследован от встроенной заглушки.
//
// # Предмет
//
// Сгенерированный сервер даёт `Unimplemented<Service>Server` со всеми методами
// контракта. Тип, встроивший его, компилируется даже если не реализует НИ
// ОДНОГО метода — и каждый нереализованный тихо отвечает
// `UNIMPLEMENTED: method X not implemented`.
//
// Это худший из возможных отказов: он приходит вызывающему БЕЗ причины и без
// адреса возможности, а в дереве не оставляет ни следа — ни ветки, ни
// комментария, ни строки, на которую мог бы указать обзор диффа. Найти такое
// можно только сравнением контракта с набором методов реализации, то есть
// ровно тем, что делает этот гейт.
//
// Замер на день заведения: 10 методов в двух сервисах (compute — 7, vpc — 3),
// и у трёх из них сайт документации описывал операцию как рабочую, с примерами
// `curl`, а соседнее предупреждение советовало ими пользоваться.
//
// # Чего гейт НЕ требует
//
// Он не требует РЕАЛИЗАЦИИ. Метод вправе отказывать — но отказ обязан быть
// написан рукой: с причиной, с адресом владельца возможности и под пробой,
// утверждающей сообщение. Переопределение и есть то место, где это пишется;
// поэтому предикат гейта — «метод объявлен реализацией», а не «метод работает».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// serviceWithBodyRe — объявление сервиса ВМЕСТЕ с телом: этому гейту нужна
// принадлежность метода сервису, а соседним (retiredrpcsurface) — только имена,
// поэтому у них свои построчные образцы под тем же предметом. Имена разные
// намеренно: один образец на два разных вопроса разошёлся бы при первой правке
// под чужой вопрос.
var serviceWithBodyRe = regexp.MustCompile(`(?ms)^service\s+(\w+)\s*\{(.*?)^\}`)

// rpcInBodyRe — объявление метода внутри тела сервиса.
var rpcInBodyRe = regexp.MustCompile(`\brpc\s+(\w+)\s*\(`)

// unimplementedEmbedRe — встроенная заглушка сервера в теле структуры.
var unimplementedEmbedRe = regexp.MustCompile(`\bUnimplemented(\w+)Server\b`)

// overrideScanRoots — где живут реализации.
var overrideScanRoots = []string{"services", "gateway"}

// missingOverride — один объявленный, но не переопределённый метод.
type missingOverride struct {
	Service string
	Type    string
	Where   string // файл объявления типа
	Method  string
}

// overrideReport — исход обхода вместе с объёмом осмотренного.
type overrideReport struct {
	Missing     []missingOverride
	ProtoFiles  int
	Services    int
	GoFiles     int
	ImplTypes   int
	RPCsChecked int
	// Foreign — реализации сервисов, чей контракт объявлен ВНЕ этого дерева
	// (стандартные сервисы библиотеки gRPC). Не находки: их поверхность не наше
	// обещание. Печатаются, чтобы «пропущено» было отличимо от «проверено».
	Foreign []string
}

// auditDeclaredRPCOverride сверяет контракт с набором методов реализации.
func auditDeclaredRPCOverride(root string) (overrideReport, error) {
	var rep overrideReport

	// 1) методы каждого proto-сервиса
	svcRPCs := map[string][]string{}
	protoFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "proto"), ".proto")
	if err != nil {
		return rep, fmt.Errorf("состав proto: %w", err)
	}
	for _, abs := range protoFiles {
		rep.ProtoFiles++
		raw, rerr := readFileString(abs)
		if rerr != nil {
			return rep, rerr
		}
		for _, m := range serviceWithBodyRe.FindAllStringSubmatch(raw, -1) {
			var rpcs []string
			for _, r := range rpcInBodyRe.FindAllStringSubmatch(m[2], -1) {
				rpcs = append(rpcs, r[1])
			}
			svcRPCs[m[1]] = rpcs
			rep.Services++
		}
	}
	if rep.Services == 0 {
		return rep, fmt.Errorf("в proto-дереве не найдено ни одного сервиса — предмет гейта исчез")
	}

	// 2) типы, встроившие заглушку, и набор их методов
	type implType struct {
		service string
		where   string
	}
	impls := map[string]implType{}          // имя типа → сервис
	methods := map[string]map[string]bool{} // имя типа → множество методов

	for _, sub := range overrideScanRoots {
		files, ferr := treecorpus.UnderWithSuffix(filepath.Join(root, sub), ".go")
		if ferr != nil {
			return rep, fmt.Errorf("состав %s: %w", sub, ferr)
		}
		for _, abs := range files {
			rel, rerr := filepath.Rel(root, abs)
			if rerr != nil {
				return rep, fmt.Errorf("путь %s: %w", abs, rerr)
			}
			slashed := filepath.ToSlash(rel)
			if strings.HasSuffix(slashed, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, abs, nil, 0)
			if perr != nil {
				return rep, fmt.Errorf("разбор %s: %w", slashed, perr)
			}
			rep.GoFiles++

			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						st, ok := ts.Type.(*ast.StructType)
						if !ok || st.Fields == nil {
							continue
						}
						for _, fld := range st.Fields.List {
							if len(fld.Names) != 0 {
								continue // встраивание — поле без имени
							}
							name := exprIdentText(fld.Type)
							if m := unimplementedEmbedRe.FindStringSubmatch(name); m != nil {
								impls[ts.Name.Name] = implType{service: m[1], where: slashed}
							}
						}
					}
				case *ast.FuncDecl:
					if d.Recv == nil || len(d.Recv.List) == 0 {
						continue
					}
					recv := exprIdentText(d.Recv.List[0].Type)
					recv = strings.TrimPrefix(recv, "*")
					if methods[recv] == nil {
						methods[recv] = map[string]bool{}
					}
					methods[recv][d.Name.Name] = true
				}
			}
		}
	}

	// 3) сверка
	for _, tname := range sortedStringKeysOfImpl(impls) {
		it := impls[tname]
		rpcs, known := svcRPCs[it.service]
		if !known {
			// Контракт живёт ВНЕ нашего дерева — так устроен, например,
			// стандартный сервис проверки здоровья из библиотеки gRPC. Его
			// поверхность мы не объявляем, значит и требовать переопределений не
			// вправе: предмет гейта — обещания ЭТОГО продукта.
			//
			// Пропуск считается и печатается: «пропущено» обязано быть отличимо
			// от «проверено», иначе гейт, у которого предмет однажды уедет
			// целиком, останется зелёным.
			rep.Foreign = append(rep.Foreign, fmt.Sprintf("%s (%s) → %s", tname, it.where, it.service))
			continue
		}
		rep.ImplTypes++
		for _, r := range rpcs {
			rep.RPCsChecked++
			if !methods[tname][r] {
				rep.Missing = append(rep.Missing, missingOverride{
					Service: it.service, Type: tname, Where: it.where, Method: r,
				})
			}
		}
	}

	sort.Slice(rep.Missing, func(i, j int) bool {
		if rep.Missing[i].Type == rep.Missing[j].Type {
			return rep.Missing[i].Method < rep.Missing[j].Method
		}
		return rep.Missing[i].Type < rep.Missing[j].Type
	})
	return rep, nil
}

// readFileString — чтение файла дерева. Путь приходит из индекса git, а не из
// ввода, поэтому включить посторонний файл нечем.
func readFileString(abs string) (string, error) {
	// #nosec G304 -- путь получен от treecorpus (индекс git ЭТОГО дерева).
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("чтение %s: %w", abs, err)
	}
	return string(raw), nil
}

// exprIdentText — текстовое имя типа выражения (для встраивания и получателя).
func exprIdentText(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprIdentText(t.X)
	case *ast.SelectorExpr:
		return exprIdentText(t.X) + "." + t.Sel.Name
	case *ast.IndexExpr: // обобщённый тип
		return exprIdentText(t.X)
	}
	return ""
}

func sortedStringKeysOfImpl[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
