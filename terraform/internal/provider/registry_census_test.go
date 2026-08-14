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

// TestEveryConstructorIsReferencedByTheRegistry — написанный ресурс, не внесённый
// в реестр, НЕ СУЩЕСТВУЕТ для Terraform, и это не видно ничем.
//
// Файл компилируется, линтер молчит, пробы его функций зелёные — а `terraform
// plan` отвечает «Invalid resource type». Ровно та форма без содержания, которую
// мы ловим в продукте: артефакт есть, предмета у него нет.
//
// Сверяется ССЫЛКА в теле реестра, а не число зарегистрированных. Причина
// названа, чтобы её не «упростили» обратно: часть ресурсов приезжает
// declarative-каркасом (`newFlatResource(<спека>)`), и такой конструктор
// возвращает не `resource.Resource`, а функцию. Счётчик по возвращаемому типу
// увидел бы одних и не увидел других — то есть сравнивал бы разные множества и
// краснел на исправном дереве.
//
// Перечень имён здесь не выписывается: он разошёлся бы с деревом молча и стал бы
// вторым местом об одном предмете.
func TestEveryConstructorIsReferencedByTheRegistry(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("разбор пакета: %v", err)
	}

	ctors := map[string]string{} // имя конструктора → что он производит
	referenced := map[string]bool{}
	files := 0

	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files++
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				// Тело реестра: собираем ВСЕ упомянутые в нём имена.
				//
				// Реестр — МЕТОДЫ провайдера, поэтому проверка имени стоит ДО
				// отсева по получателю. Обратный порядок давал ноль имён, и
				// проверка предпосылки ниже это поймала: без неё проба зеленела
				// бы, ничего не сверив.
				if fn.Name.Name == "Resources" || fn.Name.Name == "DataSources" {
					ast.Inspect(fn.Body, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok {
							referenced[id.Name] = true
						}
						return true
					})
					continue
				}
				if fn.Recv != nil {
					continue
				}
				if kind := producedKind(fn); kind != "" {
					ctors[fn.Name.Name] = kind
				}
			}
		}
	}

	// Перепись осмотренного: без неё «все зарегистрированы» неотличимо от «не
	// нашлось ни одного конструктора» — например если разбор каталога отвалится.
	t.Logf("осмотрено файлов: %d, конструкторов найдено: %d, имён в теле реестра: %d",
		files, len(ctors), len(referenced))
	if len(ctors) == 0 || len(referenced) == 0 {
		t.Fatal("нечего сверять — проверка ничего не осматривает и зеленела бы всегда")
	}

	for name, kind := range ctors {
		if !referenced[name] {
			t.Errorf("конструктор %s (%s) не упомянут в реестре — написанный, но не "+
				"внесённый в него ресурс для Terraform не существует: план отвечает "+
				"«Invalid resource type», а сборка, линтер и пробы молчат", name, kind)
		}
	}
}

// producedKind — что производит конструктор: ресурс, источник данных или ничего
// из этого.
//
// Учитываются обе формы, которыми пакет их заводит: прямая (`resource.Resource`)
// и каркасная (`func() resource.Resource` — её возвращает `newFlatResource`).
func producedKind(fn *ast.FuncDecl) string {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return ""
	}
	switch typ := fn.Type.Results.List[0].Type.(type) {
	case *ast.SelectorExpr:
		return selectorKind(typ)
	case *ast.FuncType:
		if typ.Results == nil || len(typ.Results.List) != 1 {
			return ""
		}
		if sel, ok := typ.Results.List[0].Type.(*ast.SelectorExpr); ok {
			return selectorKind(sel)
		}
	}
	return ""
}

func selectorKind(sel *ast.SelectorExpr) string {
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if pkgIdent.Name == "resource" && sel.Sel.Name == "Resource" {
		return "ресурс"
	}
	if pkgIdent.Name == "datasource" && sel.Sel.Name == "DataSource" {
		return "источник данных"
	}
	return ""
}

// TestTypeNamesAreDistinctAndPrefixed — имя типа обязано быть своим у каждого.
//
// Положительная сторона предыдущей пробы: ссылка из реестра есть и тогда, когда
// два ресурса объявили ОДНО имя типа — второй молча заслонил бы первого.
func TestTypeNamesAreDistinctAndPrefixed(t *testing.T) {
	p := New().(*kachoProvider)
	seen := map[string]bool{}

	for _, ctor := range p.Resources(context.Background()) {
		name := typeNameOfResource(ctor())
		if seen[name] {
			t.Errorf("имя типа %q объявлено дважды — второй ресурс заслоняет первого", name)
		}
		seen[name] = true
		if !strings.HasPrefix(name, "kacho_") {
			t.Errorf("имя типа %q не несёт префикса провайдера", name)
		}
	}
	for _, ctor := range p.DataSources(context.Background()) {
		name := typeNameOfDataSource(ctor())
		if seen[name] {
			t.Errorf("имя типа %q объявлено дважды", name)
		}
		seen[name] = true
		if !strings.HasPrefix(name, "kacho_") {
			t.Errorf("имя типа %q не несёт префикса провайдера", name)
		}
	}

	t.Logf("имён в реестре: %d", len(seen))
	if len(seen) < 20 {
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
