// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ ПАДАТЬ у разбора координаты якоря (задача #2467).
//
// Мир подставной: синтетическое дерево в собственном каталоге. Подменяется ОН,
// а не разбор, — иначе проба доказывала бы свойство своей копии.
//
// Каждая ось идёт ПАРОЙ: внесённый дефект обязан краснеть, законный близнец —
// молчать. Инъекция меняет РОВНО ОДИН факт против близнеца: без этого красное
// могло бы прийти от соседа.

// synthAnchorTree строит дерево с якорем по названному пути и шестью местами,
// называющими названную координату.
func synthAnchorTree(t *testing.T, anchorRel, pkg, namedCoordinate string) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}

	write(anchorRel, "syntax = \"proto3\";\n\npackage "+pkg+";\n")
	for _, rel := range catalogAnchorNamingSites {
		write(rel, "координата: "+namedCoordinate+"\n")
	}
	// Синтетическое дерево репозиторием не является — спрашивать у него индекс
	// нечего, и обход собственного каталога здесь единственный авторитет.
	// Конструктор ОТДЕЛЬНЫЙ: откат внутри общего был бы невидим.
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	return tree
}

// ЗАКОННЫЙ БЛИЗНЕЦ: живой корень, согласованные путь и пакет, все шесть мест
// называют ту же координату — разбор МОЛЧИТ по всем осям.
func TestCatalogAnchorInjection_LegalTwinIsSilent(t *testing.T) {
	t.Parallel()

	const rel = "gateway/proto/corelib/authz/catalog/v1/" + catalogAnchorBase
	tree := synthAnchorTree(t, rel, "corelib.authz.catalog.v1", "corelib/authz/catalog/v1/"+catalogAnchorBase)

	c, err := ReadCatalogAnchor(tree)
	if err != nil {
		t.Fatalf("перепись: %v", err)
	}
	if c.FilesRead != 1+len(catalogAnchorNamingSites) {
		t.Fatalf("прочитано файлов %d, ожидалось %d", c.FilesRead, 1+len(catalogAnchorNamingSites))
	}
	if m := c.RetiredRootComplaint(); m != "" {
		t.Errorf("законный близнец объявлен снятым корнем: %q", m)
	}
	if m := c.UnknownRootComplaint(); m != "" {
		t.Errorf("законный близнец объявлен неизвестным корнем: %q", m)
	}
	if m := c.PackageMatchesPathComplaint(); m != "" {
		t.Errorf("законный близнец объявлен расходящимся с путём: %q", m)
	}
	if d := c.DriftComplaints(); len(d) != 0 {
		t.Errorf("законный близнец объявлен разъехавшимся: %v", d)
	}
}

// ОСЬ 1 — снятый корень. Меняется РОВНО пакет и отвечающий ему путь; все шесть
// мест по-прежнему согласованы, поэтому краснеть обязана только эта ось.
func TestCatalogAnchorInjection_RetiredRootIsAFinding(t *testing.T) {
	t.Parallel()

	const rel = "gateway/proto/kacho/iam/authz/catalog/v1/" + catalogAnchorBase
	tree := synthAnchorTree(t, rel, "kacho.iam.authz.catalog.v1", "kacho/iam/authz/catalog/v1/"+catalogAnchorBase)

	c, err := ReadCatalogAnchor(tree)
	if err != nil {
		t.Fatalf("перепись: %v", err)
	}
	if m := c.RetiredRootComplaint(); m == "" {
		t.Fatal("снятый корень не найден: разбор молчит на дефекте, ради которого заведён")
	}
	if d := c.DriftComplaints(); len(d) != 0 {
		t.Errorf("инъекция уронила СОСЕДНЮЮ ось — красное пришло бы не от предмета: %v", d)
	}
	if m := c.PackageMatchesPathComplaint(); m != "" {
		t.Errorf("инъекция уронила соседнюю ось согласия пути и пакета: %q", m)
	}
}

// ОСЬ 2 — расхождение мест. Пакет и путь живые и согласованные, меняется РОВНО
// то, что называют шесть мест.
func TestCatalogAnchorInjection_DriftedSiteIsAFinding(t *testing.T) {
	t.Parallel()

	const rel = "gateway/proto/corelib/authz/catalog/v1/" + catalogAnchorBase
	tree := synthAnchorTree(t, rel, "corelib.authz.catalog.v1", "kaname/authz/catalog/v1/"+catalogAnchorBase)

	c, err := ReadCatalogAnchor(tree)
	if err != nil {
		t.Fatalf("перепись: %v", err)
	}
	d := c.DriftComplaints()
	if len(d) != len(catalogAnchorNamingSites) {
		t.Fatalf("расхождений найдено %d, ожидалось %d", len(d), len(catalogAnchorNamingSites))
	}
	if m := c.RetiredRootComplaint(); m != "" {
		t.Errorf("инъекция уронила соседнюю ось снятого корня: %q", m)
	}
}

// ОСЬ 3 — пакет разошёлся с каталогом. Корень живой, места согласованы с ПУТЁМ.
func TestCatalogAnchorInjection_PackageAwayFromPathIsAFinding(t *testing.T) {
	t.Parallel()

	const rel = "gateway/proto/corelib/authz/catalog/v1/" + catalogAnchorBase
	tree := synthAnchorTree(t, rel, "corelib.authz.v1", "corelib/authz/catalog/v1/"+catalogAnchorBase)

	c, err := ReadCatalogAnchor(tree)
	if err != nil {
		t.Fatalf("перепись: %v", err)
	}
	if m := c.PackageMatchesPathComplaint(); m == "" {
		t.Fatal("расхождение пакета и каталога не найдено")
	}
	if d := c.DriftComplaints(); len(d) != 0 {
		t.Errorf("инъекция уронила соседнюю ось расхождения мест: %v", d)
	}
	if m := c.RetiredRootComplaint(); m != "" {
		t.Errorf("инъекция уронила соседнюю ось снятого корня: %q", m)
	}
}

// ОСЬ 4 — пустой обход. Якоря нет вовсе: разбор обязан ОТКАЗАТЬ, а не отдать
// чистую перепись, иначе «ноль расхождений» стало бы неотличимо от «ноль
// прочитанного».
func TestCatalogAnchorInjection_EmptyWalkIsRefused(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "gateway", "proto"), 0o750); err != nil {
		t.Fatalf("каталог: %v", err)
	}
	tree, terr := treecorpus.SyntheticTree(root)
	if terr != nil {
		t.Fatalf("состав синтетического дерева: %v", terr)
	}

	_, err := ReadCatalogAnchor(tree)
	if err == nil {
		t.Fatal("пустой обход прошёл: разбор судил бы о непрочитанном")
	}
	if !strings.Contains(err.Error(), "обход пуст") {
		t.Errorf("отказ обязан называть пустой обход, получено: %q", err.Error())
	}
}

// ОСЬ 5 — якорей больше одного. У первичного файла плагина второго экземпляра
// не бывает: два якоря означают, что один из них не эмитирует ничего.
func TestCatalogAnchorInjection_TwoAnchorsAreRefused(t *testing.T) {
	t.Parallel()

	const rel = "gateway/proto/corelib/authz/catalog/v1/" + catalogAnchorBase
	one := synthAnchorTree(t, rel, "corelib.authz.catalog.v1", "corelib/authz/catalog/v1/"+catalogAnchorBase)

	// Второй якорь пишется в ТО ЖЕ дерево, и состав берётся ЗАНОВО: перепись
	// снята до записи о втором файле не знает, и проба доказывала бы свойство
	// прежнего состава.
	second := filepath.Join(one.Root(), "gateway", "proto", "kaname", "authz", "catalog", "v1")
	if err := os.MkdirAll(second, 0o750); err != nil {
		t.Fatalf("каталог: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, catalogAnchorBase),
		[]byte("syntax = \"proto3\";\n\npackage kaname.authz.catalog.v1;\n"), 0o600); err != nil {
		t.Fatalf("файл: %v", err)
	}
	tree, terr := treecorpus.SyntheticTree(one.Root())
	if terr != nil {
		t.Fatalf("состав синтетического дерева: %v", terr)
	}

	if _, err := ReadCatalogAnchor(tree); err == nil {
		t.Fatal("два якоря прошли: разбор не заметил бы, что один из них мёртв")
	}
}
