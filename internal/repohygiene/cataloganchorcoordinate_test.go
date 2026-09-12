// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// ГЕЙТ: якорь плагинов каталога назван существующим корнем и назван одинаково
// во всех местах, которые его называют (задача #2467).
//
// Перепись печатается всегда — «ноль расхождений» обязано быть отличимо от
// «ноль прочитанного». Способность падать и молчать доказана инъекцией
// (cataloganchorcoordinate_injection_test.go).
func TestCatalogAnchorCoordinateNamesSomethingThatExists(t *testing.T) {
	t.Parallel()

	census, err := ReadCatalogAnchor(catalogAnchorTree(t))
	if err != nil {
		t.Fatalf("перепись координаты якоря: %v", err)
	}
	if census.FilesRead == 0 || len(census.Sites) == 0 {
		t.Fatalf("обход пуст: прочитано файлов %d, мест %d — гейт судил бы о непрочитанном",
			census.FilesRead, len(census.Sites))
	}
	t.Logf("перепись: якорь %s · пакет %s · прочитано файлов %d · мест, называющих координату, %d",
		census.AnchorFile, census.Package, census.FilesRead, len(census.Sites))

	if c := census.RetiredRootComplaint(); c != "" {
		t.Error(c)
	}
	if c := census.UnknownRootComplaint(); c != "" {
		t.Error(c)
	}
	if c := census.PackageMatchesPathComplaint(); c != "" {
		t.Error(c)
	}
	for _, c := range census.DriftComplaints() {
		t.Error(c)
	}
}

// Положительный контроль ПЕРЕПИСИ: каждое объявленное место действительно
// назвало координату. Место, у которого разбор её не нашёл, покрыто пустым
// случаем — то есть не покрыто вовсе.
func TestCatalogAnchorEveryDeclaredSiteNamesTheCoordinate(t *testing.T) {
	t.Parallel()

	census, err := ReadCatalogAnchor(catalogAnchorTree(t))
	if err != nil {
		t.Fatalf("перепись координаты якоря: %v", err)
	}
	named := map[string]bool{}
	for _, s := range census.Sites {
		named[s.File] = true
	}
	for _, want := range catalogAnchorNamingSites {
		if !named[want] {
			t.Errorf("место %s объявлено называющим координату якоря, а разбор её там не нашёл: "+
				"либо координата уехала из этого файла, либо перечень мест устарел", want)
		}
	}
	t.Logf("перепись мест: объявлено %d · назвали координату %d",
		len(catalogAnchorNamingSites), len(named))
}

// Положительный контроль ФОРМЫ жалобы: на снятой приставке разбор говорит, а не
// молчит, и называет обе стороны — файл и пакет.
func TestCatalogAnchorRetiredRootComplaintNamesBothSides(t *testing.T) {
	t.Parallel()

	c := CatalogAnchorCensus{
		AnchorFile: "gateway/proto/kacho/iam/authz/catalog/v1/" + catalogAnchorBase,
		Package:    "kacho.iam.authz.catalog.v1",
	}
	msg := c.RetiredRootComplaint()
	if msg == "" {
		t.Fatal("на снятой приставке разбор обязан говорить: молчание здесь неотличимо от исправного дерева")
	}
	for _, want := range []string{c.AnchorFile, c.Package} {
		if !strings.Contains(msg, want) {
			t.Errorf("жалоба обязана называть %q, получено: %q", want, msg)
		}
	}
}

// Законный близнец: имя под объявленным корнем, у которого предмет есть,
// жалобы не вызывает. Без этой половины разбор ловил бы форму, а не существо.
func TestCatalogAnchorLivingRootIsNotAFinding(t *testing.T) {
	t.Parallel()

	c := CatalogAnchorCensus{
		AnchorFile: "gateway/proto/corelib/authz/catalog/v1/" + catalogAnchorBase,
		Package:    "corelib.authz.catalog.v1",
	}
	if msg := c.RetiredRootComplaint(); msg != "" {
		t.Errorf("живой корень объявлен находкой: %q", msg)
	}
	if msg := c.UnknownRootComplaint(); msg != "" {
		t.Errorf("объявленный корень объявлен неизвестным: %q", msg)
	}
	if msg := c.PackageMatchesPathComplaint(); msg != "" {
		t.Errorf("согласованные путь и пакет объявлены расходящимися: %q", msg)
	}
}

// catalogAnchorTree — состав дерева из ИНДЕКСА git.
//
// Под gateway/ на машине, где собирали край или поднимали стенд, лежат
// сборочные каталоги и распаковки чартов; обход диска нашёл бы там второй
// «якорь» и отказал на исправном дереве. Авторитет состава — индекс.
func catalogAnchorTree(t *testing.T) *treecorpus.Tree {
	t.Helper()
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	return tree
}
