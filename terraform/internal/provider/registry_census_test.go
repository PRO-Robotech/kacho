// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// TestEveryConstructorIsRegistered — написанный ресурс, не внесённый в реестр,
// НЕ СУЩЕСТВУЕТ для Terraform, и это не видно ничем.
//
// Файл компилируется, линтер молчит, пробы его функций зелёные — а `terraform
// plan` отвечает «Invalid resource type». Ровно та форма без содержания, которую
// мы ловим в продукте: артефакт есть, предмета у него нет.
//
// Проверка идёт по РАЗБОРУ пакета, а не по перечню имён: перечень пришлось бы
// править вручную, и он разошёлся бы с деревом молча — то есть был бы вторым
// местом об одном предмете.
func TestEveryConstructorIsRegistered(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("разбор пакета: %v", err)
	}

	var resourceCtors, dataSourceCtors []string
	files := 0
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files++
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
					continue
				}
				sel, ok := fn.Type.Results.List[0].Type.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				switch {
				case pkgIdent.Name == "resource" && sel.Sel.Name == "Resource":
					resourceCtors = append(resourceCtors, fn.Name.Name)
				case pkgIdent.Name == "datasource" && sel.Sel.Name == "DataSource":
					dataSourceCtors = append(dataSourceCtors, fn.Name.Name)
				}
			}
		}
	}

	// Перепись осмотренного: без неё «все зарегистрированы» неотличимо от «не
	// нашлось ни одного конструктора» — например если разбор каталога отвалится.
	t.Logf("осмотрено файлов: %d, конструкторов ресурсов: %d, источников данных: %d",
		files, len(resourceCtors), len(dataSourceCtors))
	if len(resourceCtors) == 0 || len(dataSourceCtors) == 0 {
		t.Fatal("конструкторов не найдено — проверка ничего не осматривает и зеленела бы всегда")
	}

	p := New()
	registeredR := map[string]bool{}
	for _, ctor := range p.(*kachoProvider).Resources(context.Background()) {
		registeredR[typeNameOfResource(ctor())] = true
	}
	registeredD := map[string]bool{}
	for _, ctor := range p.(*kachoProvider).DataSources(context.Background()) {
		registeredD[typeNameOfDataSource(ctor())] = true
	}

	if got, want := len(registeredR), len(resourceCtors); got != want {
		t.Errorf("конструкторов ресурсов в пакете %d, зарегистрировано %d — "+
			"написанный, но не внесённый в реестр ресурс для Terraform не существует.\n"+
			"Конструкторы: %v", want, got, resourceCtors)
	}
	if got, want := len(registeredD), len(dataSourceCtors); got != want {
		t.Errorf("конструкторов источников данных в пакете %d, зарегистрировано %d.\n"+
			"Конструкторы: %v", want, got, dataSourceCtors)
	}
}

// TestTypeNamesAreDistinctAndPrefixed — имя типа обязано быть своим у каждого.
//
// Положительная сторона предыдущей пробы: совпадение счётчиков зеленело бы и
// тогда, когда два ресурса объявили ОДНО имя типа — второй молча заслонил бы
// первого.
func TestTypeNamesAreDistinctAndPrefixed(t *testing.T) {
	p := New().(*kachoProvider)
	seen := map[string]bool{}

	for _, ctor := range p.Resources(context.Background()) {
		name := typeNameOfResource(ctor())
		if seen[name] {
			t.Errorf("имя типа %q объявлено дважды — второй ресурс заслоняет первого", name)
		}
		seen[name] = true
		if len(name) < 6 || name[:6] != "kacho_" {
			t.Errorf("имя типа %q не несёт префикса провайдера", name)
		}
	}
	for _, ctor := range p.DataSources(context.Background()) {
		name := typeNameOfDataSource(ctor())
		if seen[name] {
			t.Errorf("имя типа %q объявлено дважды", name)
		}
		seen[name] = true
	}

	if len(seen) < 6 {
		t.Errorf("в реестре %d имён — меньше, чем заведено ресурсов и источников; "+
			"проверка различимости осматривает не всё", len(seen))
	}
}

func typeNameOfResource(r resource.Resource) string {
	var resp resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "kacho"}, &resp)
	return resp.TypeName
}

func typeNameOfDataSource(d datasource.DataSource) string {
	var resp datasource.MetadataResponse
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "kacho"}, &resp)
	return resp.TypeName
}

var _ provider.Provider = (*kachoProvider)(nil)
