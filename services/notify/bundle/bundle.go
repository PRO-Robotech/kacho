// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package bundle — сборка шаблонов notify (NTF-1 Р6, Р7; З22): проверенный
// шаблон по пространству ленты и имени. Реализует порт deliver.Build.
//
// Шаблоны попадают сюда только шагом сборки (`make -C services/notify
// bundle`, пакет internal/assemble): каталог catalog/<пространство>/<шаблон>
// — копия каталога шаблонов источника, встроенная в бинарь. Регистрации в
// рантайме и кастомных шаблонов установки нет (Р7, NTF1-G19). Свежесть копии
// против дерева держит `make -C services/notify bundle-check` (NTF1-G17).
//
// New проверяет встроенный каталог тем же валидатором формата, что и шаг
// сборки (`notify/spec`, NTF1-A09): каталог, который валидатор не принял,
// либо шаблон без ревизии — отказ, и notify без сборки не стартует.
package bundle

import (
	"embed"
	"fmt"
	"io/fs"
	"path"

	"github.com/PRO-Robotech/corelib/notify/spec"
)

// catalogRoot — корень встроенного каталога; имя совпадает с
// assemble.CatalogDir (директива embed принимает только литерал).
const catalogRoot = "catalog"

//go:embed catalog
var catalogFS embed.FS

// Build — проверенная сборка шаблонов. Неизменяема после New и безопасна для
// одновременного чтения.
type Build struct {
	// templates — «<пространство>/<имя>» → шаблон.
	templates map[string]*spec.Template
}

// New читает встроенный каталог сборки.
func New() (*Build, error) {
	return load(catalogFS, catalogRoot)
}

// load — сборка из каталога root в fsys: каждый подкаталог root — пространство
// ленты, его подкаталоги — шаблоны.
func load(fsys fs.FS, root string) (*Build, error) {
	spaces, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("bundle: каталог сборки не читается: %w", err)
	}
	b := &Build{templates: map[string]*spec.Template{}}
	for _, ns := range spaces {
		if !ns.IsDir() {
			return nil, fmt.Errorf("bundle: %s в корне каталога сборки — не пространство", ns.Name())
		}
		cat, census, err := spec.LoadFS(fsys, path.Join(root, ns.Name()))
		if err != nil {
			return nil, fmt.Errorf("bundle: пространство %s: %w", ns.Name(), err)
		}
		if census.Templates == 0 {
			return nil, fmt.Errorf("bundle: пространство %s без шаблонов", ns.Name())
		}
		for i := range cat.Templates {
			tpl := &cat.Templates[i]
			if !tpl.HasRevision {
				return nil, fmt.Errorf("bundle: %s/%s: у шаблона нет ревизии", ns.Name(), tpl.Name)
			}
			b.templates[ns.Name()+"/"+tpl.Name] = tpl
		}
	}
	if len(b.templates) == 0 {
		return nil, fmt.Errorf("bundle: в каталоге сборки 0 шаблонов")
	}
	return b, nil
}

// Template — шаблон сборки по пространству ленты и имени (Р6): шаблон ищется
// только в пространстве строки, поэтому модуль не отправит шаблон другого.
func (b *Build) Template(namespace, name string) (*spec.Template, bool) {
	tpl, ok := b.templates[namespace+"/"+name]
	return tpl, ok
}
