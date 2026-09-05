// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package authz

// hide_existence_parity_package_test.go — разрешение объявления края ПО ПАКЕТУ.
//
// Единица — пакет, а не файл: файл внутри пакета переносится свободно и это
// законная правка, а координата файла делает её отказом стража. Отказ при этом
// не красный, а «не выполнилось», поданное как красное, — то есть о паритете он
// не говорит НИЧЕГО, но выглядит как говорящий.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// parseFormatsInPackage — записи объявления `hideExistenceNotFoundFormats` в
// не-тестовых файлах пакета.
//
// Возвращает ОШИБКУ, а не роняет пробу: разбор, обращающийся к `*testing.T`,
// инъекции не поддаётся — падение подставного пакета уронило бы саму пробу
// способности падать.
//
// Отказов три, и все три — отказы: обход пуст · объявления нет · объявлений два.
// Каждый называет объём прочитанного, потому что «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func parseFormatsInPackage(dir string) (map[string]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("пакет %s не прочитан (прочитано 0 файлов): %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	read := 0
	out := map[string]string{}
	var declaredIn []string
	for _, name := range names {
		f, perr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if perr != nil {
			return nil, read, fmt.Errorf("файл %s пакета не разобран (прочитано %d файлов): %w",
				name, read, perr)
		}
		read++
		if got := formatsFromFile(f); len(got) > 0 {
			declaredIn = append(declaredIn, name)
			for k, v := range got {
				out[k] = v
			}
		}
	}

	switch {
	case read == 0:
		return nil, 0, fmt.Errorf("в пакете %s нет ни одного не-тестового файла Go "+
			"(прочитано 0 файлов) — страж судил бы о непрочитанном", dir)
	case len(declaredIn) == 0:
		return nil, read, fmt.Errorf("объявление hideExistenceNotFoundFormats не найдено ни в одном "+
			"не-тестовом файле пакета %s (прочитано %d файлов) — край отвечает нейтральным текстом, "+
			"различимым от текста владельца, и это оракул существования", dir, read)
	case len(declaredIn) > 1:
		return nil, read, fmt.Errorf("объявлений hideExistenceNotFoundFormats в пакете %s больше одного: %s "+
			"(прочитано %d файлов) — какое из них исполняется, решает сборка, и страж выбрал бы наугад",
			dir, strings.Join(declaredIn, ", "), read)
	case len(out) == 0:
		return nil, read, fmt.Errorf("объявление разобралось в ноль записей (прочитано %d файлов) — "+
			"страж прошёл бы вакуумно", read)
	}
	return out, read, nil
}

// formatsFromFile — записи объявления в одном разобранном файле.
func formatsFromFile(f *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "hideExistenceNotFoundFormats" {
			return true
		}
		if len(vs.Values) != 1 {
			return false
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return false
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, kok := kv.Key.(*ast.BasicLit)
			v, vok := kv.Value.(*ast.BasicLit)
			if !kok || !vok {
				continue
			}
			key, kerr := strconv.Unquote(k.Value)
			val, verr := strconv.Unquote(v.Value)
			if kerr != nil || verr != nil {
				continue
			}
			out[key] = val
		}
		return false
	})
	return out
}
