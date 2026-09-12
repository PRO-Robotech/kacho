// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// cataloganchorcoordinate.go — разбор «якорь плагинов каталога назван корнем,
// который существует, и назван ОДИНАКОВО во всех местах» (задача #2467).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Якорь — файл, к которому buf цепляет оба генератора края. Сам он ничего не
// эмитирует: его имя нужно плагину, чтобы узнать себя в списке файлов к
// порождению. Отсюда два свойства, и оба легко теряются молча.
//
//  1. ИМЯ ЯКОРЯ — ПЕРВОЕ, что видит разбирающий тракт порождения. Приставка
//     `kacho.iam.authz` пережила оба своих основания: словарь разметки уехал
//     под нейтральный корень, служба доступа адресуется своим. Имя называло
//     продуктовый корень, домен внутри него и подсистему — и ни одного из трёх
//     в этой форме не существовало. Читателю это стоит захода: он идёт искать
//     каталог контрактов, которого нет.
//
//  2. КООРДИНАТА ЖИВЁТ В ШЕСТИ МЕСТАХ — путь файла, объявление пакета в нём,
//     имя первичного файла у каждого из двух плагинов, умолчание ручки в каждом
//     из двух сборочных скриптов и путь в скрипте сверки. Разъезжаются они
//     МОЛЧА: плагин, не узнавший себя, эмитирует пустой вывод, а пустой вывод
//     отличим от прежнего только сверкой, которая идёт в другом шаге.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ, А ЧТО НЕТ
//
// Судится СОГЛАСИЕ шести мест и КОРЕНЬ имени: первый сегмент пакета обязан быть
// одним из объявленных корней дерева контрактов. О том, хорошо ли выбраны
// остальные сегменты, разбор не утверждает ничего — это суждение, а не предикат.
//
// Приставка снятого корня названа ПОИМЁННО и отдельно: `kacho.iam.` — форма,
// у которой не существует ни каталога контрактов, ни службы с таким адресом.
// Перечень узкий намеренно: широкое правило «сегмент обязан что-то называть»
// предикатом не является.
//
// Перепись печатается всегда: «ноль расхождений» обязано быть отличимо от
// «ноль прочитанного», поэтому разбор называет число прочитанных мест и падает
// на пустом обходе.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// CatalogAnchorSite — одно место, называющее координату якоря.
type CatalogAnchorSite struct {
	// File — файл дерева, в котором координата стоит.
	File string
	// Coordinate — сама координата в форме ПУТИ внутри модуля
	// (`<корень>/<…>/permissions_catalog_root.proto`).
	Coordinate string
}

// CatalogAnchorCensus — что разбор прочитал.
type CatalogAnchorCensus struct {
	// AnchorFile — путь файла-якоря относительно корня дерева.
	AnchorFile string
	// Package — объявление пакета внутри якоря.
	Package string
	// Sites — места, называющие координату (кроме самого якоря).
	Sites []CatalogAnchorSite
	// FilesRead — сколько файлов разбор открыл.
	FilesRead int
}

// catalogAnchorBase — имя файла якоря. Оно и есть признак, по которому
// координата опознаётся в чужом тексте: путь до него меняется, имя — нет.
const catalogAnchorBase = "permissions_catalog_root.proto"

// catalogAnchorRetiredPrefixes — приставки имени пакета, у которых предмета
// больше нет. Перечень поимённый: широкое правило предикатом не является.
var catalogAnchorRetiredPrefixes = []string{"kacho.iam."}

// catalogAnchorRoots — корни дерева контрактов. Зеркалит pkg/contractroot: имя
// вне этого перечня выпадает из популяции разборов, судящих контракты, и
// выпадает МОЛЧА.
var catalogAnchorRoots = []string{"kacho", "kaname", "corelib"}

// catalogAnchorNamingSites — файлы, обязанные называть ту же координату.
// Перечень выписан потому, что каждый из них называет её СВОЕЙ формой (литерал
// Go, умолчание ручки оболочки, ячейка таблицы страницы), и вывести его обходом
// значило бы завести распознаватель шире предмета.
var catalogAnchorNamingSites = []string{
	"gateway/cmd/protoc-gen-kacho-permissions/main.go",
	"gateway/cmd/protoc-gen-kacho-rest-routes/main.go",
	"gateway/scripts/gen-permission-catalog.sh",
	"gateway/scripts/gen-rest-route-table.sh",
	"gateway/scripts/check-domain-generation.sh",
	"gateway/docs/engineering/architecture/domain-scoped-generation.md",
}

var (
	catalogAnchorPackageRe = regexp.MustCompile(`(?m)^package\s+([A-Za-z0-9_.]+)\s*;`)
	catalogAnchorPathRe    = regexp.MustCompile(`([A-Za-z0-9_./-]*` + regexp.QuoteMeta(catalogAnchorBase) + `)`)
)

// ReadCatalogAnchor собирает перепись координаты якоря по составу дерева.
//
// Состав приезжает ПАРАМЕТРОМ, а не собирается здесь обходом диска: под
// gateway/ на машине, где собирали край или поднимали стенд, лежат сборочные
// каталоги и распаковки чартов, и обход диска считал бы их деревом — то есть
// нашёл бы второй «якорь» там, где его нет, и отказал на исправном дереве.
// Авторитет состава — индекс git (`treecorpus.NewTree`), а у синтетического
// дерева инъекции — её собственный каталог (`treecorpus.SyntheticTree`).
func ReadCatalogAnchor(tree *treecorpus.Tree) (CatalogAnchorCensus, error) {
	var c CatalogAnchorCensus
	root := tree.Root()

	anchor, err := findCatalogAnchor(tree)
	if err != nil {
		return c, err
	}
	c.AnchorFile = anchor
	c.FilesRead++

	// #nosec G304 -- путь собран из корня СОСТАВА дерева и имени, пришедшего
	// из индекса git (см. findCatalogAnchor). Переменной, подконтрольной
	// кому-либо извне, здесь нет ни одной: разбор читает собственное дерево,
	// а вызывающий у него один — прогон проверок.
	raw, err := os.ReadFile(filepath.Join(root, anchor))
	if err != nil {
		return c, fmt.Errorf("якорь %s не прочитан: %w", anchor, err)
	}
	m := catalogAnchorPackageRe.FindSubmatch(raw)
	if m == nil {
		return c, fmt.Errorf("якорь %s не объявляет пакета", anchor)
	}
	c.Package = string(m[1])

	for _, rel := range catalogAnchorNamingSites {
		// #nosec G304 -- путь собран из корня СОСТАВА дерева и элемента
		// ЗАКРЫТОГО перечня, объявленного константой этого файла
		// (catalogAnchorNamingSites). Ввода извне у него нет.
		body, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			return c, fmt.Errorf("место %s не прочитано: %w", rel, rerr)
		}
		c.FilesRead++
		seen := map[string]bool{}
		for _, hit := range catalogAnchorPathRe.FindAllStringSubmatch(string(body), -1) {
			coord := catalogAnchorCoordinate(hit[1])
			if coord == "" || seen[coord] {
				continue
			}
			seen[coord] = true
			c.Sites = append(c.Sites, CatalogAnchorSite{File: rel, Coordinate: coord})
		}
	}
	return c, nil
}

// catalogAnchorCoordinate обрезает найденный путь до координаты ВНУТРИ модуля:
// всё, что стоит слева от корня контрактов, принадлежит рабочей копии, а не
// координате, и сравнивать его между местами нечем.
func catalogAnchorCoordinate(path string) string {
	for _, r := range catalogAnchorRoots {
		if i := strings.Index(path, "/"+r+"/"); i >= 0 {
			return path[i+1:]
		}
		if strings.HasPrefix(path, r+"/") {
			return path
		}
	}
	return ""
}

// catalogAnchorTreePrefix — поддерево контрактов края, в котором живёт якорь.
const catalogAnchorTreePrefix = "gateway/proto/"

// findCatalogAnchor находит единственный файл якоря под деревом контрактов края.
//
// Спрашивает СОСТАВ, а не диск: отбор идёт по отсортированному перечню, поэтому
// вход детерминирован, а игнорируемое в него не попадает вовсе.
func findCatalogAnchor(tree *treecorpus.Tree) (string, error) {
	var found []string
	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, catalogAnchorTreePrefix) {
			continue
		}
		if path.Base(rel) != catalogAnchorBase {
			continue
		}
		found = append(found, rel)
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("обход пуст: якоря %s под %s в составе дерева нет — "+
			"разбор судил бы о непрочитанном", catalogAnchorBase, catalogAnchorTreePrefix)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("якорей %s больше одного: %v — у первичного файла плагина "+
			"второго экземпляра не бывает", catalogAnchorBase, found)
	}
}

// RetiredRootComplaint — жалоба на приставку, у которой предмета больше нет.
func (c CatalogAnchorCensus) RetiredRootComplaint() string {
	for _, p := range catalogAnchorRetiredPrefixes {
		if strings.HasPrefix(c.Package, p) {
			return fmt.Sprintf(
				"якорь %s объявляет пакет %s: приставка %s пережила своё основание — "+
					"ни каталога контрактов, ни службы с таким адресом в дереве нет, "+
					"и разбирающий тракт порождения идёт искать несуществующее",
				c.AnchorFile, c.Package, strings.TrimSuffix(p, "."))
		}
	}
	return ""
}

// UnknownRootComplaint — жалоба на корень вне объявленных.
func (c CatalogAnchorCensus) UnknownRootComplaint() string {
	head, _, _ := strings.Cut(c.Package, ".")
	for _, r := range catalogAnchorRoots {
		if head == r {
			return ""
		}
	}
	return fmt.Sprintf(
		"якорь %s объявляет пакет %s: корень %q не значится среди объявленных (%s), "+
			"поэтому имя выпадает из популяции разборов, судящих контракты, — и выпадает молча",
		c.AnchorFile, c.Package, head, strings.Join(catalogAnchorRoots, ", "))
}

// PackageMatchesPathComplaint — жалоба на расхождение пакета и каталога.
func (c CatalogAnchorCensus) PackageMatchesPathComplaint() string {
	want := strings.ReplaceAll(strings.TrimSuffix(
		strings.TrimPrefix(c.AnchorFile, "gateway/proto/"), "/"+catalogAnchorBase), "/", ".")
	if want == c.Package {
		return ""
	}
	return fmt.Sprintf(
		"якорь %s лежит под каталогом, отвечающим пакету %s, а объявляет %s — "+
			"buf судит их вместе, и расходятся они молча",
		c.AnchorFile, want, c.Package)
}

// DriftComplaints — места, называющие координату, отличную от координаты якоря.
func (c CatalogAnchorCensus) DriftComplaints() []string {
	want := strings.TrimPrefix(c.AnchorFile, "gateway/proto/")
	var out []string
	for _, s := range c.Sites {
		if s.Coordinate == want {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s называет якорь как %s, а он лежит по %s: плагин, не узнавший себя, "+
				"эмитирует ПУСТОЙ вывод, и пустой отличим от прежнего только сверкой в чужом шаге",
			s.File, s.Coordinate, want))
	}
	return out
}
