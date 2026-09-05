// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// descriptorchainlink_test.go — гейт против ПОЛЯ ДЕСКРИПТОРА, через которое
// сервис мог бы принести в контур своё звено цепочки.
//
// # Предмет и почему он важнее, чем кажется
//
// Весь смысл носителя держится на одном свойстве: порядок звеньев один на все
// сервисы, потому что принести своё звено НЕЛЬЗЯ. `Serve` возвращает `error`, а
// не сервер, регистрация получает интерфейс с единственным методом — до сервера
// вызывающему не дотянуться. Остаётся одна дверь: поле дескриптора
// интерсепторного типа. Заведи такое поле — и восьмой сервис получит свой
// порядок, не нарушив ни одного правила, потому что правила про это нет.
//
// Сегодня свойство верно (замер печатается ниже), но держалось оно ничем: гейт
// полей судит ЧИТАТЕЛЕЙ и про типы полей не спрашивает, а проба усыновления
// целится в конструкторы серверов у сервисов, не в дескриптор.
//
// # Проверка СВОЕЙ предпосылки
//
// Запрет обоснован фактом о дереве: поле интерсепторного типа невыразимо без
// импорта grpc, а пакет дескриптора импортирует из grpc ровно `credentials`.
// Факт может измениться — поэтому гейт заявляет его САМ и роняет прогон, когда
// он перестаёт держаться, вместо того чтобы молча остаться верным по форме.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// chainLinkTypeNames — типы, которыми выражается ЗВЕНО цепочки либо доступ к
// собранному серверу. Перечень закрытый и поимённый: признак «что-нибудь из
// grpc» был бы корзиной «прочее», в которую однажды попадёт законное поле
// (креденшелы транспорта им уже являются).
var chainLinkTypeNames = map[string]string{
	"UnaryServerInterceptor":  "звено unary-цепочки",
	"StreamServerInterceptor": "звено stream-цепочки",
	"UnaryClientInterceptor":  "звено исходящей unary-цепочки",
	"StreamClientInterceptor": "звено исходящей stream-цепочки",
	"ServerOption":            "опция сборки сервера (через неё приезжает любое звено)",
	"Server":                  "сам сервер: получив его, вызывающий приделает звено сам",
}

// TestDescriptorCarriesNoChainLink — сам гейт.
func TestDescriptorCarriesNoChainLink(t *testing.T) {
	root := repoRoot(t)
	files, err := treecorpus.Under(filepath.Join(root, contractPkgRel))
	if err != nil {
		t.Fatalf("состав пакета дескриптора: %v", err)
	}

	var (
		fields   int
		read     int
		findings []string
		grpcImps = map[string]string{} // путь импорта -> координата
	)
	fset := token.NewFileSet()
	for _, path := range files {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		read++

		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil || !strings.HasPrefix(p, "google.golang.org/grpc") {
				continue
			}
			grpcImps[p] = fmt.Sprintf("%s:%d", rel, fset.Position(imp.Pos()).Line)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != descriptorTypeName {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				fields++
				sel, ok := unwrapSelector(field.Type)
				if !ok {
					continue
				}
				why, forbidden := chainLinkTypeNames[sel]
				if !forbidden {
					continue
				}
				name := "<встроенное>"
				if len(field.Names) > 0 {
					name = field.Names[0].Name
				}
				findings = append(findings, fmt.Sprintf("%s (%s:%d): тип поля есть %s. "+
					"Порядок звеньев держится тем, что принести своё звено НЕЛЬЗЯ; поле такого "+
					"типа открывает эту дверь, и восьмой сервис получит свой порядок, не нарушив "+
					"ни одного правила", name, rel, fset.Position(field.Pos()).Line, why))
			}
			return false
		})
	}

	t.Logf("осмотрено: файлов пакета %s — %d, полей дескриптора %d, импортов grpc %d",
		contractPkgRel, read, fields, len(grpcImps))

	if read == 0 || fields == 0 {
		t.Fatalf("прочитано файлов %d, полей %d — «находок нет» здесь означало бы «ничего не смотрели»",
			read, fields)
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("поля дескриптора, через которые приезжает своё звено (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}

	// ПРЕДПОСЫЛКА гейта, заявленная им самим: перечень имён выше закрыт, и
	// закрытым его позволяет держать то, что из grpc пакет дескриптора берёт
	// ровно креденшелы транспорта. Появится другой импорт — перечень может
	// перестать покрывать способы выразить звено, и об этом обязан сказать гейт,
	// а не следующий обзор диффа.
	const allowed = "google.golang.org/grpc/credentials"
	var extra []string
	for p, where := range grpcImps {
		if p == allowed {
			continue
		}
		extra = append(extra, fmt.Sprintf("%s (%s)", p, where))
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("предпосылка гейта перестала держаться: пакет дескриптора импортирует из grpc не "+
			"только %s, а ещё (%d): %s.\nПеречень запрещённых имён типов был закрыт в расчёте на то, "+
			"что выразить звено нечем. Перепроверьте перечень против нового импорта — либо снимите "+
			"импорт", allowed, len(extra), strings.Join(extra, ", "))
	}
	if len(grpcImps) == 0 {
		t.Errorf("пакет дескриптора не импортирует grpc вовсе — предпосылка стала тождественно " +
			"истинной, и гейт перестал что-либо утверждать. Проверьте, там ли ещё живёт дескриптор")
	}
}

// unwrapSelector достаёт имя типа из `pkg.Name`, `[]pkg.Name`, `*pkg.Name`,
// `map[K]pkg.Name`. Возвращает ok=false для всего остального: предикат обязан
// молчать там, где не уверен, а не угадывать.
func unwrapSelector(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name, true
	case *ast.StarExpr:
		return unwrapSelector(v.X)
	case *ast.ArrayType:
		return unwrapSelector(v.Elt)
	case *ast.MapType:
		return unwrapSelector(v.Value)
	case *ast.IndexExpr: // Axis[grpc.UnaryServerInterceptor]
		return unwrapSelector(v.Index)
	default:
		return "", false
	}
}
