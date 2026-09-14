// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourcedcontractparity.go — КОНТРАКТ, ПО КОТОРОМУ ГОВОРЯТ ДВА ДВОИЧНЫХ,
// ОБЯЗАН ЛЕЖАТЬ У ОБОИХ ПО ОДНОМУ ПУТИ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#2650)
//
// Контракт межслужебного разговора живёт в фундаменте и приезжает обеим
// сторонам ПИНОМ ВЕРСИИ. Пины у сторон РАЗНЫЕ by construction: платформа и
// вынесенная служба — отдельные модули, каждый собирается своим выбором версий.
// Пока контракт стоит на месте, это безразлично. В день, когда он ПЕРЕЕЗЖАЕТ,
// стороны начинают говорить разными именами, и узнать об этом неоткуда:
//
//	край        собран от фундамента v1.7.0 → зовёт  /corelib.subscription.InternalSubscriptionService/Subscribe
//	служба      собрана от фундамента v1.5.0 → служит /kacho.cloud.subscription.InternalSubscriptionService/Subscribe
//
// gRPC отвечает `Unimplemented` — имени нет на сервере. Край честно читает это
// как «владелец глагола не служит» и отдаёт `501`. Цена расхождения измерена
// пробой-ценой рядом (`outsourcedcontractparity_cost_test.go`): различие ровно
// одной строки — полного имени метода — и есть разница между рабочим потоком и
// `501` каждому.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ НЕ ЛОВИТ НИ ОДНА СУЩЕСТВУЮЩАЯ ПРОВЕРКА
//
// Каждое дерево по отдельности ИСПРАВНО, и это худший из возможных случаев.
//
//   - сборка обеих сторон зелена: каждая линкует СВОЙ контракт, и он есть;
//   - `buf breaking` судит контракты ОДНОГО дерева и чужого пина не видит;
//   - гейт согласия двух пинов службы
//     (`deploy/outsourced_image_pin_agrees_with_module_pin_test.go`) сверяет
//     ВЕРСИЮ: `go.mod` называет `kaname v0.4.0`, чарт — образ `v0.4.0`, стороны
//     согласны — и зелен ровно тогда, когда контракт уже разошёлся. Он сам
//     называет свою границу: «судит согласие двух ОБЪЯВЛЕНИЙ».
//
// Отказ приходит ВРЕМЕНИ ВЫПОЛНЕНИЯ и ТРАНЗИТИВНО — у потребителя, который ни
// одного из двух путей не называл: проба консоли упала на модуле «сводка», хотя
// ни сводка, ни подписка к её предмету отношения не имеют.
//
// ─────────────────────────────────────────────────────────────────────────────
// СУДИТСЯ ПУТЬ, А НА ПРОВОДЕ — ИМЯ. ГРАНИЦА НАЗВАНА ЧЕСТНО
//
// Путь совпал ⇒ обе стороны линкуют ОДИН файл контракта ⇒ имя совпало by
// construction. Это сильная сторона предиката, и ради неё он выбран.
//
// Обратное строго слабее: путь разошёлся ⇒ имя МОГЛО уцелеть (переезд каталога
// без переименования proto-пакета). Тогда находка требует взгляда, а не правки.
// В этом дереве обе наблюдавшиеся пары переезжали ВМЕСТЕ с именем — переезд и
// был выносом контракта из платформы в фундамент, — но предикат этого не знает
// и знать не может: имя чужой стороны лежит в фундаменте ТОЙ версии, а её в
// графе сборки нет и скачивать её ради вердикта значит завести сетевой гейт.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ИМПОРТ, А НЕ ПОДСТРОКА, И ПОЧЕМУ КЛЮЧ ВЫВОДИТСЯ
//
// Сторона читается узлом импорта синтаксического дерева: имя пакета встречается
// и в комментарии (вот в этом, например), и предикат по слову краснел бы на
// собственном объяснении.
//
// Ключ, по которому стороны сопоставляются, — последний НЕВЕРСИОННЫЙ сегмент
// пути (`corelib/quota/v1` и `kacho/cloud/quota/v1` — оба `quota`). Он
// ВЫВОДИТСЯ из пути, а не выписывается ведомостью: ведомость разошлась бы с
// деревом молча, и разошлась бы в сторону невидимости.
//
// ГРАНИЦА КЛЮЧА названа: два РАЗНЫХ контракта с одинаковым последним
// неверсионным сегментом в разных доменах фундамента дали бы ложную находку.
// Сегодня таких нет — перепись печатает число ключей, и оно проверяемо.
package repohygiene

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// OutsourcedContractParityOptions — вход анализатора.
//
// Каталоги, а не готовые перечни импортов: инъекция обязана подавать анализатору
// НАСТОЯЩИЙ вход — дерево с файлами, — иначе она доказывала бы разбор, которого
// в рабочем прогоне нет.
type OutsourcedContractParityOptions struct {
	// OurRoots — каталоги ЭТОГО дерева, чьи импорты образуют нашу сторону.
	OurRoots []string
	// Pinned — пиненные модули продукта: путь модуля → каталог его исходников.
	// Пустой каталог означает «модуль не скачан» — третья категория, не находка.
	Pinned map[string]string
	// FoundationAPI — приставка путей контрактов фундамента.
	FoundationAPI string
	// LinkedWitness — путь пакета контракта, ЛИНКОВАННЫЙ этим двоичным, взятый
	// у самого стаба, а не выписанный. Обход обязан его найти: не нашёл —
	// значит читает не то дерево, и «ноль находок» означало бы «ноль
	// прочитанного».
	LinkedWitness string
}

// OutsourcedContractParityFinding — один контракт, разошедшийся между сторонами.
type OutsourcedContractParityFinding struct {
	Contract string // ключ контракта
	Ours     string // путь пакета у ЭТОГО дерева
	Module   string // пиненный модуль, чья сторона разошлась
	Theirs   string // путь пакета у него
	Coord    string // файл:строка объявления импорта у него
}

// OutsourcedContractParityCensus — объём осмотренного. «Ноль находок» обязано
// быть отличимо от «ноль прочитанного», поэтому печатаются ОБЕ стороны.
type OutsourcedContractParityCensus struct {
	OurFiles         int // файлов Go осмотрено у нас
	OurImports       int // из них импортов контрактов фундамента
	OurContracts     int // различных ключей контракта у нас
	Modules          int // пиненных модулей продукта
	ModulesReachable int // из них с доступным каталогом исходников
	TheirFiles       int // файлов Go осмотрено у них
	TheirImports     int // из них импортов контрактов фундамента
	SharedContracts  int // ключей, названных ОБЕИМИ сторонами
}

// versionSegment — версионный сегмент пути пакета Go.
var versionSegment = regexp.MustCompile(`^v[0-9]+(?:(?:alpha|beta)[0-9]*)?$`)

// contractKey — ключ сопоставления сторон: последний НЕВЕРСИОННЫЙ сегмент пути.
//
// Пустая строка означает «это не контракт фундамента» — вызывающий пропускает.
func contractKey(foundationAPI, pkg string) string {
	if pkg == foundationAPI || !strings.HasPrefix(pkg, foundationAPI+"/") {
		return ""
	}
	segments := strings.Split(strings.TrimPrefix(pkg, foundationAPI+"/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if !versionSegment.MatchString(segments[i]) {
			return segments[i]
		}
	}
	return ""
}

// contractImport — один импорт контракта фундамента с координатой объявления.
type contractImport struct {
	Pkg   string
	Coord string
}

// collectContractImports — импорты контрактов фундамента в дереве, по УЗЛУ
// импорта.
//
// Тестовые файлы не читаются: предмет — то, что дерево ЛИНКУЕТ в поставку.
// Импорт в пробе о разговоре двух двоичных не утверждает ничего.
func collectContractImports(root, foundationAPI string) (map[string]contractImport, int, int, error) {
	out := map[string]contractImport{}
	files, imports := 0, 0
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "node_modules", "testdata", ".git":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		files++
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			// Нечитаемый файл ПРОПУСКАЕТСЯ, а не роняет обход: чужое дерево
			// пина может нести исходник под сборочным условием, которого у нас
			// нет. Полноту обхода держит перепись, а не отказ на первом файле.
			return nil //nolint:nilerr // объяснено выше
		}
		for _, spec := range f.Imports {
			pkg := strings.Trim(spec.Path.Value, `"`)
			key := contractKey(foundationAPI, pkg)
			if key == "" {
				continue
			}
			imports++
			if _, seen := out[key]; !seen {
				pos := fset.Position(spec.Pos())
				out[key] = contractImport{Pkg: pkg, Coord: fmt.Sprintf("%s:%d", pos.Filename, pos.Line)}
			}
		}
		return nil
	})
	return out, files, imports, err
}

// AuditOutsourcedContractParity — вердикт о согласии контрактов между этим
// деревом и деревьями пиненных модулей продукта.
func AuditOutsourcedContractParity(
	opts OutsourcedContractParityOptions, log io.Writer,
) ([]OutsourcedContractParityFinding, OutsourcedContractParityCensus, error) {
	var census OutsourcedContractParityCensus
	if opts.FoundationAPI == "" {
		return nil, census, fmt.Errorf("приставка контрактов фундамента не названа — сопоставлять нечем")
	}

	ours := map[string]contractImport{}
	for _, root := range opts.OurRoots {
		found, files, imports, err := collectContractImports(root, opts.FoundationAPI)
		if err != nil {
			return nil, census, fmt.Errorf("обход %s: %w", root, err)
		}
		census.OurFiles += files
		census.OurImports += imports
		for key, imp := range found {
			if _, seen := ours[key]; !seen {
				ours[key] = imp
			}
		}
	}
	census.OurContracts = len(ours)

	// ПРЕМИСА ОБХОДА: путь, который двоичное ЛИНКУЕТ, обязан найтись среди
	// прочитанного. Без неё пустой обход давал бы зелёное — то есть «ноль
	// находок» было бы неотличимо от «ноль прочитанного».
	if opts.LinkedWitness != "" {
		witnessKey := contractKey(opts.FoundationAPI, opts.LinkedWitness)
		if witnessKey == "" {
			return nil, census, fmt.Errorf(
				"свидетель %q не опознан как контракт фундамента %q — ключ не выводится",
				opts.LinkedWitness, opts.FoundationAPI)
		}
		got, ok := ours[witnessKey]
		if !ok {
			return nil, census, fmt.Errorf(
				"обход не нашёл контракт %q, который двоичное ЛИНКУЕТ (%s): прочитано файлов %d — "+
					"вердикт беспредметен", witnessKey, opts.LinkedWitness, census.OurFiles)
		}
		if got.Pkg != opts.LinkedWitness {
			return nil, census, fmt.Errorf(
				"обход прочитал контракт %q по пути %q, а двоичное линкует %q — обход читает не то дерево",
				witnessKey, got.Pkg, opts.LinkedWitness)
		}
	}

	var findings []OutsourcedContractParityFinding
	shared := map[string]struct{}{}
	modules := make([]string, 0, len(opts.Pinned))
	for module := range opts.Pinned {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	for _, module := range modules {
		census.Modules++
		dir := opts.Pinned[module]
		if dir == "" {
			continue
		}
		census.ModulesReachable++
		theirs, files, imports, err := collectContractImports(dir, opts.FoundationAPI)
		if err != nil {
			return nil, census, fmt.Errorf("обход пина %s (%s): %w", module, dir, err)
		}
		census.TheirFiles += files
		census.TheirImports += imports
		for key, imp := range theirs {
			our, ok := ours[key]
			if !ok {
				continue
			}
			shared[key] = struct{}{}
			if our.Pkg == imp.Pkg {
				continue
			}
			findings = append(findings, OutsourcedContractParityFinding{
				Contract: key, Ours: our.Pkg, Module: module, Theirs: imp.Pkg,
				Coord: strings.TrimPrefix(imp.Coord, dir+string(filepath.Separator)),
			})
		}
	}
	census.SharedContracts = len(shared)

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Module != findings[j].Module {
			return findings[i].Module < findings[j].Module
		}
		return findings[i].Contract < findings[j].Contract
	})

	if log != nil {
		_, _ = fmt.Fprintf(log, "осмотрено: у нас файлов Go %d, импортов контракта %d, контрактов %d\n",
			census.OurFiles, census.OurImports, census.OurContracts)
		_, _ = fmt.Fprintf(log, "пиненных модулей продукта %d, из них с доступным деревом %d; "+
			"у них файлов Go %d, импортов контракта %d\n",
			census.Modules, census.ModulesReachable, census.TheirFiles, census.TheirImports)
		_, _ = fmt.Fprintf(log, "контрактов, названных ОБЕИМИ сторонами: %d; расхождений: %d\n",
			census.SharedContracts, len(findings))
		for _, f := range findings {
			_, _ = fmt.Fprintf(log, "  РАСХОЖДЕНИЕ контракта %q: мы линкуем %q, %s линкует %q (%s)\n",
				f.Contract, f.Ours, f.Module, f.Theirs, f.Coord)
		}
	}
	return findings, census, nil
}
