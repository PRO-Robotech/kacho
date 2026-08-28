// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iamknowsnoedge.go — анализатор «владелец прав не знает своего потребителя».
//
// # Предмет
//
// iam объявлен ЛИСТОМ графа рёбер: его зовут, он не зовёт никого. Толчок от iam
// к краю нарушал это дважды — ребром в графе и отказом старта: адрес края был
// ОБЯЗАТЕЛЬНОЙ ручкой, поэтому iam не поднимался там, где края нет вовсе. Второе
// и делает вынос iam отдельным продуктом невыразимым.
//
// Направление, оставленное решением владельца: соединение открывает ПОТРЕБИТЕЛЬ.
// Тогда ребро остаётся потребитель→владелец, ацикличность цела, а владелец о
// потребителе не знает — и знать ему нечем.
//
// # Что судится — ДВЕ половины, и одной мало
//
//  1. ТИП. Прод-файл iam не импортирует контракт края (`pkg/api/.../apigateway`).
//     Импорт — узел синтаксического дерева, поэтому упоминание имени пакета в
//     комментарии находкой не является by construction.
//  2. АДРЕС. Ни прод-код iam, ни его чарт не объявляют ручки адреса края
//     (`KACHO_IAM_GATEWAY_INTERNAL*`). Половина без второй ничего не держит:
//     снятый импорт при живой ручке оставляет отказ старта, а снятая ручка при
//     живом импорте оставляет ребро.
//
// # ГРАНИЦА, названная вслух
//
// В Go имя ручки судится как СТРОКОВЫЙ ЛИТЕРАЛ (узел дерева) — проза о ручке
// законна. В шаблоне чарта дерева нет: до подстановки это не YAML, и разобрать
// его нечем. Поэтому там снимается комментарий (от `#` до конца строки) и
// судится остаток. Это приближение, и оно названо: строка `#` внутри
// цитированного значения была бы прочитана как начало комментария. Такой строки
// в чартах iam нет, а цена точного разбора — свой парсер шаблонов.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// edgeContractImportMarker — по чему узнаётся контракт края в пути импорта.
const edgeContractImportMarker = "pkg/api/kacho/cloud/apigateway"

// edgeAddressKnobPrefix — приставка ручек адреса края у iam.
const edgeAddressKnobPrefix = "KACHO_IAM_GATEWAY_INTERNAL"

// IamKnowsNoEdgeOptions — посадка анализатора.
type IamKnowsNoEdgeOptions struct {
	// Root — корень дерева.
	Root string
	// GoRoot — каталог прод-кода владельца прав.
	GoRoot string
	// ChartRoots — каталоги чарта владельца прав.
	ChartRoots []string
}

// IamKnowsNoEdgeCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type IamKnowsNoEdgeCensus struct {
	GoFiles    int
	ChartFiles int
	GoImports  int
	GoLiterals int
}

// IamKnowsNoEdgeFinding — одно нарушение с координатой.
type IamKnowsNoEdgeFinding struct {
	Path string
	Line int
	What string
}

func (f IamKnowsNoEdgeFinding) String() string {
	return fmt.Sprintf("%s:%d — %s", f.Path, f.Line, f.What)
}

// AuditIamKnowsNoEdge обходит дерево и возвращает находки с переписью.
func AuditIamKnowsNoEdge(opts IamKnowsNoEdgeOptions, log io.Writer) ([]IamKnowsNoEdgeFinding, IamKnowsNoEdgeCensus, error) {
	var (
		findings []IamKnowsNoEdgeFinding
		census   IamKnowsNoEdgeCensus
	)

	goDir := filepath.Join(opts.Root, opts.GoRoot)
	err := filepath.WalkDir(goDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		census.GoFiles++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return fmt.Errorf("разбор %s: %w", path, perr)
		}
		rel, _ := filepath.Rel(opts.Root, path)

		for _, imp := range file.Imports {
			census.GoImports++
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			if strings.Contains(p, edgeContractImportMarker) {
				findings = append(findings, IamKnowsNoEdgeFinding{
					Path: rel, Line: fset.Position(imp.Pos()).Line,
					What: "импорт контракта края " + p + " — владелец прав типизирован своим потребителем",
				})
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.GoLiterals++
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if strings.Contains(v, edgeAddressKnobPrefix) {
				findings = append(findings, IamKnowsNoEdgeFinding{
					Path: rel, Line: fset.Position(lit.Pos()).Line,
					What: "ручка адреса края " + v + " — iam не поднимется там, где края нет",
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, census, err
	}

	for _, chartRoot := range opts.ChartRoots {
		dir := filepath.Join(opts.Root, chartRoot)
		werr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".yaml" && ext != ".yml" && ext != ".tpl" {
				return nil
			}
			census.ChartFiles++
			raw, rerr := os.ReadFile(path) // #nosec G304 -- путь производится обходом дерева
			if rerr != nil {
				return rerr
			}
			rel, _ := filepath.Rel(opts.Root, path)
			for i, line := range strings.Split(string(raw), "\n") {
				if idx := strings.Index(line, "#"); idx >= 0 {
					line = line[:idx]
				}
				if strings.Contains(line, edgeAddressKnobPrefix) {
					findings = append(findings, IamKnowsNoEdgeFinding{
						Path: rel, Line: i + 1,
						What: "чарт объявляет ручку адреса края — посадка обязывает владельца знать потребителя",
					})
				}
			}
			return nil
		})
		if werr != nil {
			return nil, census, werr
		}
	}

	if log != nil {
		fmt.Fprintf(log, "перепись: файлов Go прочитано %d · импортов осмотрено %d · строковых литералов осмотрено %d · файлов чарта прочитано %d · находок %d\n",
			census.GoFiles, census.GoImports, census.GoLiterals, census.ChartFiles, len(findings))
	}
	return findings, census, nil
}
