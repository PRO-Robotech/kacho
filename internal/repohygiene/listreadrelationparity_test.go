// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listreadrelationparity_test.go — гейт против списочного предиката, дизъюнктного
// с отношением чтения.
//
// # Что охраняется
//
// Публичный List читает страницу курсором из своей БД и спрашивает модель, какие id
// этой страницы видны вызывающему. Отношение, которым задан ЭТОТ вопрос, обязано
// совпадать с отношением, которым per-RPC Check гейтит одиночный Get того же ресурса.
// Пока они расходятся, объект попадает в страницу по одному отношению, а читается по
// другому — и вызывающий узнаёт о существовании объекта, которого не вправе читать.
// Это не только оракул существования: List возвращает ТО ЖЕ сообщение, что и Get,
// поэтому членство в странице раскрывает содержимое целиком.
//
// Расхождение измерено, а не предположено: три сервиса спрашивали страницу союзом
// `viewer ∪ v_list`, а Get гейтили `v_get`; четвёртый ушёл на ярус; пятый —
// kacho-iam — держал союз на семи публичных списках. В модели это РАЗНЫЕ множества:
// ярусные (`viewer`/`editor`/`admin`) и глагольные (`v_*`) отношения развязаны
// намеренно, как анти-over-grant guard (fga_model.fga, миграция iam 0040). Поэтому
// расхождение работает в обе стороны: держатель яруса без глагола видит чужое
// содержимое, держатель глагола без яруса не находит собственный читаемый ресурс.
//
// # Почему гейт ищет МЕХАНИЗМ, а не сервис
//
// Прежняя редакция искала объявление ПО ИМЕНИ переменной (`visibilityRelations`).
// Четыре сервиса это имя носили, пятый назвал своё иначе — и не попал под гейт вовсе:
// ни нарушителем, ни исключением, его просто не существовало для проверки. Имя
// переменной — не свойство механизма, а привычка автора.
//
// Механизм ищется как СВОЙСТВО пакета, в двух шагах:
//
//  1. пакет ФИЛЬТРУЕТ СТРАНИЦУ — объявляет функцию, принимающую `[]string` (страницу
//     идентификаторов) и возвращающую `[]string` либо `map[string]bool` (её видимое
//     подмножество), — И задаёт вопрос хранилищу прав (`Check`/`BatchCheck`…);
//  2. такой пакет ОБЪЯВЛЯЕТ предикат — package-level коллекцию строк, каждая из
//     которых есть отношение, объявленное в `fga_model.fga`.
//
// Шаг 2 без шага 1 отсекает наборы отношений, которые предикатом страницы не являются
// (`MutateRelations` стража, `callerAuthorityRelations`, словари `authzmap`). Шаг 1 без
// шага 2 — это либо потребитель, делегирующий чужому объявлению, либо новое слепое
// пятно; TestListReadRelationParity_EveryPageFilterIsAccountedFor требует по каждому
// такому пакету явного исхода, поэтому «предикат, который никто не может прочитать»
// краснеет, а не молчит.
//
// # Почему предмет берётся из КАТАЛОГА
//
// Отношение чтения не выписывается здесь рукой: рукописный список расходится с
// деревом — это свойство механизма, а не аккуратности автора. Оно читается из
// сгенерированного каталога прав (той же копии, которую исполняет шлюз), по записи
// `<Service>/Get` ТОГО ЖЕ типа объекта. Сменится гейт чтения — гейт увидит это сам.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// defaultPredicateKey — ключ записи «все прочие типы» в объявлении вида
// map[тип][]отношение. Объявление с такой записью ТОТАЛЬНО: у типа, не названного
// поимённо, предикат всё равно объявлен, а не спрятан в теле функции.
const defaultPredicateKey = ""

// clusterScopeObjectType — якорь глобального справочника. Записи каталога с этим
// scope'ом из сверки исключены: это не тенантские объекты с индивидуальными
// владельцами, их List гейтится на уровне кластера и пообъектным сужением не проходит
// вовсе. Исключение проверяемо: TestListReadRelationParity_PremiseHolds требует, чтобы
// у него был предмет, иначе запись переживёт то, ради чего написана.
const clusterScopeObjectType = "cluster"

// listReadRelationExceptions — пакеты, которые фильтруют страницу пообъектно, но
// предиката в сверяемом виде не объявляют, И это записанное решение, а не упущение.
//
// Запись обязана называть ПРИЧИНУ, по которой сверка здесь не применима, и
// самоистекать: TestListReadRelationParity_PremiseHolds требует, чтобы у каждой записи
// оставался предмет (пакет всё ещё фильтрует страницу). Запись, которой больше нечего
// исключать, — находка: она унаследует следующее слепое пятно.
var listReadRelationExceptions = map[string]string{
	"services/iam/internal/authzcascade": "" +
		"Предиката членства страницы здесь НЕТ, и быть не может: пакет — надстройка над вопросом " +
		"к хранилищу прав (предзагрузка структурных фактов на страницу и порт перечисления " +
		"принципалов), решение о видимости он не принимает. Под форму он попал своими " +
		"`[]string`-поверхностями, а предикат iam живёт в services/iam/internal/authzfilter, " +
		"который этот пакет как раз и обслуживает. Запись фиксирует РАЗБОР, а не послабление: " +
		"появится здесь собственный набор отношений — он станет объявлением и уйдёт под сверку.",

	"services/registry/internal/dataplane": "" +
		"OCI-каталог (`GET /v2/_catalog`) сужает страницу пообъектно по `v_list`, тогда как чтение " +
		"репозитория (манифест/блоб) гейтится `v_get`. Расхождение здесь НЕ раскрывает содержимого: " +
		"страница каталога — это голые ИМЕНА, а не сообщение ресурса, поэтому «видно в перечне без " +
		"содержимого» тут реализуемо, в отличие от List'ов, возвращающих ту же структуру, что и Get. " +
		"Раскрытие имени ограничено субъектами, которым `v_list` на этот репозиторий выдан явно, а " +
		"курсор `last=` намеренно опакован, чтобы окно не называло чужих имён. Если каталог когда-нибудь " +
		"начнёт отдавать что-то помимо имени, запись подлежит пересмотру вместе с предикатом.",
}

var (
	catalogFqnDomainRe    = regexp.MustCompile(`^kacho\.cloud\.([a-z0-9]+)\.v1\.([A-Za-z0-9]+)/([A-Za-z0-9]+)$`)
	permissionMapDomainRe = regexp.MustCompile(`/kacho\.cloud\.([a-z0-9]+)\.v1\.`)
)

// relationStoreQuestions — закрытый список имён, которыми в этом дереве задаётся
// вопрос хранилищу прав. Список — ПРЕДПОСЫЛКА узнавания механизма, поэтому
// TestListReadRelationParity_PremiseHolds требует, чтобы каждое имя оставалось живым:
// мёртвая запись делает узнавание уже, чем гейт о себе заявляет.
var relationStoreQuestions = []string{"Check", "CheckWithContext", "BatchCheck"}

// listReadCatalogEntry — запись сгенерированного каталога прав (нужные поля).
type listReadCatalogEntry struct {
	FQN              string `json:"fqn"`
	RequiredRelation string `json:"required_relation"`
	ScopeExtractor   struct {
		ObjectType string `json:"object_type"`
	} `json:"scope_extractor"`
}

// pageFilterPkg — пакет, признанный фильтром страницы.
type pageFilterPkg struct {
	dir      string // относительно корня репо — координата для отказа
	service  string // каталог в services/
	declFile string // где объявлен предикат ("" — не объявлен)
	declName string // имя объявления, только для сообщения

	flat   []string            // плоский предикат на все типы сервиса
	byType map[string][]string // тотальный предикат по типам

	imports   map[string]bool
	mapDomain string // домен из карты прав сервиса ("" — карты нет)
	relStore  bool   // задаёт вопрос хранилищу прав
	pageShape bool   // принимает страницу id и возвращает её подмножество
}

func (p pageFilterPkg) declares() bool { return len(p.flat) > 0 || len(p.byType) > 0 }

// predicateFor — предикат страницы для одного типа объекта.
func (p pageFilterPkg) predicateFor(objectType string) ([]string, bool) {
	if len(p.byType) > 0 {
		if rels, ok := p.byType[objectType]; ok {
			return rels, true
		}
		if rels, ok := p.byType[defaultPredicateKey]; ok {
			return rels, true
		}
		return nil, false
	}
	return p.flat, len(p.flat) > 0
}

// readGate — отношение, которым каталог гейтит пообъектное чтение ОДНОГО типа.
type readGate struct {
	objectType string
	relation   string
	methods    []string
}

// parityFinding — одно расхождение, по одному типу объекта.
type parityFinding struct {
	pkg        pageFilterPkg
	objectType string
	pageRels   []string
	read       readGate
}

func (f parityFinding) String() string {
	methods := append([]string(nil), f.read.methods...)
	sort.Strings(methods)
	if len(methods) > 3 {
		methods = append(methods[:3:3], fmt.Sprintf("…+%d", len(f.read.methods)-3))
	}
	page := f.pageRels
	if len(page) == 0 {
		page = []string{"<пусто>"}
	}
	return fmt.Sprintf(
		"%s / тип %q: страница спрашивается {%s}, а чтение гейтится {%s} (%s)\n"+
			"  объявление: %s — %s\n"+
			"  следствие: объект попадает в страницу по одному отношению, а читается по другому — "+
			"вызывающий узнаёт о существовании объекта, который ему не отдаст Get, и получает его "+
			"содержимое (List возвращает то же сообщение)",
		f.pkg.service, f.objectType, strings.Join(page, ", "),
		f.read.relation, strings.Join(methods, ", "), f.pkg.declFile, f.pkg.declName)
}

// findParityViolations — ЧИСТОЕ ядро гейта: сверяет предикат страницы с отношением
// чтения ПО КАЖДОМУ типу объекта. Вынесено отдельно, чтобы инъекция проверяла ровно
// то, что исполняется на дереве, а не его пересказ.
func findParityViolations(pkgs []pageFilterPkg, reads map[string][]readGate) []parityFinding {
	var out []parityFinding
	for _, p := range pkgs {
		if !p.declares() {
			continue // за такие отвечает проверка охвата, не эта
		}
		for _, dom := range domainsOf(p, reads) {
			for _, g := range reads[dom] {
				pageRels, ok := p.predicateFor(g.objectType)
				if !ok {
					continue // непокрытый тип ловит проверка предпосылки
				}
				if len(pageRels) != 1 || pageRels[0] != g.relation {
					out = append(out, parityFinding{
						pkg: p, objectType: g.objectType, pageRels: pageRels, read: g,
					})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pkg.service != out[j].pkg.service {
			return out[i].pkg.service < out[j].pkg.service
		}
		return out[i].objectType < out[j].objectType
	})
	return out
}

// domainsOf — домены каталога, чьи типы этот пакет фильтрует.
//
// Для тотального объявления по типам домен ВЫВОДИТСЯ из самих ключей: они и есть типы
// объектов каталога, поэтому сервису не нужно иметь карту прав (у kacho-iam её нет —
// именно на этом прежняя редакция и слепла бы вторым способом). Для плоского
// объявления — из карты прав сервиса: догадка по имени каталога не годится,
// `services/nlb` обслуживает домен `loadbalancer`.
func domainsOf(p pageFilterPkg, reads map[string][]readGate) []string {
	if len(p.byType) > 0 {
		best, bestHits := "", 0
		doms := make([]string, 0, len(reads))
		for dom := range reads {
			doms = append(doms, dom)
		}
		sort.Strings(doms)
		for _, dom := range doms {
			hits := 0
			for _, g := range reads[dom] {
				if _, named := p.byType[g.objectType]; named {
					hits++
				}
			}
			if hits > bestHits {
				best, bestHits = dom, hits
			}
		}
		if best == "" {
			return nil
		}
		return []string{best}
	}
	if p.mapDomain == "" {
		return nil
	}
	return []string{p.mapDomain}
}

// discoverPageFilters обходит services/ и собирает пакеты, фильтрующие страницу
// пообъектно, вместе с объявленным предикатом каждого. Возвращает и перепись —
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
func discoverPageFilters(t *testing.T, root string) (pkgs []pageFilterPkg, filesWalked int) {
	t.Helper()
	relations := modelRelations(t, root)

	byDir := map[string]*pageFilterPkg{}
	svcDirOf := map[string]string{}

	err := filepath.WalkDir(filepath.Join(root, "services"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil //nolint:nilerr // недоступный подкаталог не должен ронять обход
		}
		filesWalked++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		dir := filepath.Dir(path)
		p := byDir[dir]
		if p == nil {
			relDir, _ := filepath.Rel(root, dir)
			slashed := filepath.ToSlash(relDir)
			parts := strings.Split(slashed, "/")
			svc := ""
			if len(parts) > 1 {
				svc = parts[1]
			}
			p = &pageFilterPkg{dir: slashed, service: svc, imports: map[string]bool{}}
			byDir[dir] = p
			svcDirOf[dir] = filepath.Join(root, "services", svc)
		}
		for _, im := range f.Imports {
			if v, uerr := strconv.Unquote(im.Path.Value); uerr == nil {
				p.imports[v] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Type != nil && funcTakesPageReturnsSubset(x.Type) {
					p.pageShape = true
				}
			case *ast.InterfaceType:
				if x.Methods == nil {
					return true
				}
				for _, m := range x.Methods.List {
					if ft, ok := m.Type.(*ast.FuncType); ok && funcTakesPageReturnsSubset(ft) {
						p.pageShape = true
					}
				}
			case *ast.SelectorExpr:
				if isRelationStoreQuestion(x.Sel.Name) {
					p.relStore = true
				}
			case *ast.ValueSpec:
				for i, nm := range x.Names {
					if i >= len(x.Values) {
						continue
					}
					flat, byType, ok := parseRelationDeclaration(x.Values[i], relations)
					if !ok {
						continue
					}
					relPath, _ := filepath.Rel(root, path)
					p.declFile = filepath.ToSlash(relPath)
					p.declName = nm.Name
					p.flat, p.byType = flat, byType
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обойти services/: %v", err)
	}

	for dir, p := range byDir {
		if !p.pageShape || !p.relStore {
			continue
		}
		p.mapDomain = deriveServiceDomain(svcDirOf[dir])
		pkgs = append(pkgs, *p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].dir < pkgs[j].dir })
	return pkgs, filesWalked
}

// funcTakesPageReturnsSubset — «спрашивает ЗА КОГО-ТО про страницу идентификаторов и
// возвращает её подмножество». Это и есть форма пообъектного сужения списка.
//
// Скалярный строковый параметр рядом со страницей обязателен и несёт смысл: фильтр
// всегда спрашивает ПРО КОГО (субъект) и/или ПРО ЧТО (тип объекта), а чистый помощник
// над списком — нет. Без этого условия под форму попадал `dedupe(ids []string) []string`
// в соседнем пакете, и охват объявлял находкой вспомогательную функцию — гейт, красный
// на законной конструкции, отключают первым.
func funcTakesPageReturnsSubset(ft *ast.FuncType) bool {
	takesPage, takesScalar := false, false
	if ft.Params != nil {
		for _, prm := range ft.Params.List {
			switch pt := prm.Type.(type) {
			case *ast.ArrayType:
				if id, ok := pt.Elt.(*ast.Ident); ok && pt.Len == nil && id.Name == "string" {
					takesPage = true
				}
			case *ast.Ident:
				if pt.Name == "string" {
					takesScalar = true
				}
			}
		}
	}
	if !takesPage || !takesScalar || ft.Results == nil {
		return false
	}
	for _, r := range ft.Results.List {
		switch rt := r.Type.(type) {
		case *ast.ArrayType:
			if id, ok := rt.Elt.(*ast.Ident); ok && rt.Len == nil && id.Name == "string" {
				return true
			}
		case *ast.MapType:
			k, kok := rt.Key.(*ast.Ident)
			v, vok := rt.Value.(*ast.Ident)
			if kok && vok && k.Name == "string" && v.Name == "bool" {
				return true
			}
		}
	}
	return false
}

func isRelationStoreQuestion(sel string) bool {
	for _, n := range relationStoreQuestions {
		if sel == n {
			return true
		}
	}
	return false
}

// parseRelationDeclaration разбирает объявление предиката РАЗБОРОМ AST, а не текстом:
// строковый литерал в комментарии или в соседнем тесте не должен ни находиться, ни
// маскировать объявление. Признаётся два вида, и оба — по СОДЕРЖИМОМУ:
//
//	[...]string{…} / []string{…}       — плоский предикат на все типы сервиса;
//	map[string][]string{тип: {…}, …}   — тотальный предикат по типам.
//
// Условие признания одно: КАЖДЫЙ строковый лист объявления есть отношение, объявленное
// в модели. Набор с одной посторонней строкой — уже не предикат отношений.
func parseRelationDeclaration(v ast.Expr, relations map[string]bool) (flat []string, byType map[string][]string, ok bool) {
	cl, isCL := v.(*ast.CompositeLit)
	if !isCL || len(cl.Elts) == 0 {
		return nil, nil, false
	}
	if _, isMap := cl.Type.(*ast.MapType); isMap {
		byType = map[string][]string{}
		for _, el := range cl.Elts {
			kv, isKV := el.(*ast.KeyValueExpr)
			if !isKV {
				return nil, nil, false
			}
			key, kerr := relationStringLit(kv.Key)
			if kerr != nil {
				return nil, nil, false
			}
			rels, rok := relationSlice(kv.Value, relations)
			if !rok {
				return nil, nil, false
			}
			byType[key] = rels
		}
		return nil, byType, len(byType) > 0
	}
	rels, rok := relationSlice(cl, relations)
	if !rok {
		return nil, nil, false
	}
	return rels, nil, true
}

func relationSlice(e ast.Expr, relations map[string]bool) ([]string, bool) {
	cl, isCL := e.(*ast.CompositeLit)
	if !isCL || len(cl.Elts) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		s, err := relationStringLit(el)
		if err != nil || !relations[s] {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// relationStringLit — строковый литерал с признаком успеха. Отдельно от пакетного
// stringLit намеренно: тот возвращает "" и на пустом литерале, и на не-литерале, а
// здесь пустая строка — ЗНАЧАЩИЙ ключ (запись «все прочие типы»), и спутать эти два
// случая значило бы принять чужое выражение за объявление умолчания.
func relationStringLit(e ast.Expr) (string, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", fmt.Errorf("не строковый литерал")
	}
	return strconv.Unquote(bl.Value)
}

// modelRelations — словарь отношений из канонической модели. Это и есть предпосылка
// узнавания: предикат страницы состоит из отношений модели.
func modelRelations(t *testing.T, root string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, fgaModelPath))
	if err != nil {
		t.Fatalf("прочитать модель %s: %v", fgaModelPath, err)
	}
	out := map[string]bool{}
	for _, ln := range strings.Split(string(b), "\n") {
		rest, cut := strings.CutPrefix(strings.TrimSpace(ln), "define ")
		if !cut {
			continue
		}
		if i := strings.Index(rest, ":"); i > 0 {
			out[strings.TrimSpace(rest[:i])] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("в %s не разобрано ни одного отношения — узнавать объявления не по чему", fgaModelPath)
	}
	return out
}

// deriveServiceDomain выводит домен proto из карты прав сервиса — того файла, где
// перечислены его собственные RPC.
func deriveServiceDomain(svcDir string) string {
	if svcDir == "" {
		return ""
	}
	counts := map[string]int{}
	_ = filepath.WalkDir(svcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "permission_map.go" {
			return nil //nolint:nilerr // недоступный подкаталог не должен ронять обход
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range permissionMapDomainRe.FindAllStringSubmatch(string(b), -1) {
			counts[m[1]]++
		}
		return nil
	})
	best, bestN := "", 0
	for dom, n := range counts {
		if n > bestN || (n == bestN && dom < best) {
			best, bestN = dom, n
		}
	}
	return best
}

// loadReadGates читает из каталога отношения ПООБЪЕКТНОГО чтения: домен → перечень
// (тип объекта, отношение). Записи без отношения пропускаются намеренно: у такого
// чтения нет отношения, с которым сверять, и это отдельный предмет, а не расхождение.
func loadReadGates(t *testing.T, root string) (map[string][]readGate, int) {
	t.Helper()
	entries := readListReadCatalog(t, root)
	byDomType := map[string]map[string]*readGate{}
	scanned := 0
	for _, e := range entries {
		m := catalogFqnDomainRe.FindStringSubmatch(e.FQN)
		if m == nil || m[3] != "Get" || strings.HasPrefix(m[2], "Internal") {
			continue
		}
		ot := e.ScopeExtractor.ObjectType
		if ot == "" || ot == clusterScopeObjectType || e.RequiredRelation == "" {
			continue
		}
		dom := m[1]
		if byDomType[dom] == nil {
			byDomType[dom] = map[string]*readGate{}
		}
		g := byDomType[dom][ot]
		if g == nil {
			g = &readGate{objectType: ot, relation: e.RequiredRelation}
			byDomType[dom][ot] = g
		}
		g.methods = append(g.methods, m[2])
		scanned++
	}
	out := map[string][]readGate{}
	for dom, m := range byDomType {
		for _, g := range m {
			out[dom] = append(out[dom], *g)
		}
		sort.Slice(out[dom], func(i, j int) bool { return out[dom][i].objectType < out[dom][j].objectType })
	}
	return out, scanned
}

func readListReadCatalog(t *testing.T, root string) []listReadCatalogEntry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("прочитать каталог %s: %v", catalogEmbedPath, err)
	}
	var entries []listReadCatalogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("разобрать каталог %s: %v", catalogEmbedPath, err)
	}
	if len(entries) == 0 {
		t.Fatalf("каталог %s пуст — сверять не с чем", catalogEmbedPath)
	}
	return entries
}

// ─────────────────────────────── сам гейт ───────────────────────────────────

func TestListReadRelationParity(t *testing.T) {
	root := repoRoot(t)
	pkgs, filesWalked := discoverPageFilters(t, root)
	reads, catalogGets := loadReadGates(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение: «ноль расхождений» обязано быть отличимо от
	// «ноль осмотренного».
	declarers, compared := 0, 0
	for _, p := range pkgs {
		if !p.declares() {
			t.Logf("осмотрено: %-44s фильтрует страницу; предикат НЕ объявлен (делегирует либо записан)", p.dir)
			continue
		}
		declarers++
		doms := domainsOf(p, reads)
		var types []string
		for _, dom := range doms {
			for _, g := range reads[dom] {
				if _, ok := p.predicateFor(g.objectType); ok {
					types = append(types, g.objectType+"→"+g.relation)
					compared++
				}
			}
		}
		sort.Strings(types)
		t.Logf("осмотрено: %-44s домены=%v сверено типов=%d [%s]",
			p.dir, doms, len(types), strings.Join(types, " "))
	}
	t.Logf("перепись: файлов .go (не тестов) под services/ прочитано = %d; пакетов-фильтров страницы = %d; "+
		"из них объявляют предикат = %d; записанных исключений = %d; записей `/Get` каталога с отношением = %d; "+
		"сверено пар (тип, отношение) = %d",
		filesWalked, len(pkgs), declarers, len(listReadRelationExceptions), catalogGets, compared)

	if findings := findParityViolations(pkgs, reads); len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "предикат страницы разошёлся с отношением чтения (%d):\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "\n%s\n", f)
		}
		b.WriteString("\nЧТО ДЕЛАТЬ: сузить предикат страницы до отношения, которым гейтится Get того же " +
			"ресурса, либо — если список ШИРЕ чтения намеренно — записать это решением с обоснованием и " +
			"привести List к выдаче, которая содержимого не раскрывает. Расширять чтение под список — " +
			"неверное направление.")
		t.Error(b.String())
	}
}

// TestListReadRelationParity_EveryPageFilterIsAccountedFor — та половина, отсутствие
// которой и пропустило пятый экземпляр.
//
// Пакет, фильтрующий страницу пообъектно, обязан иметь ИСХОД: либо он объявляет
// предикат в сверяемом виде, либо делегирует пакету, который объявляет, либо о нём
// есть запись с причиной. Четвёртого не бывает — «гейт его не увидел» это не исход, а
// слепое пятно, и именно так оно и выглядело в прошлый раз.
func TestListReadRelationParity_EveryPageFilterIsAccountedFor(t *testing.T) {
	root := repoRoot(t)
	pkgs, _ := discoverPageFilters(t, root)

	declarerImportPaths := map[string]bool{}
	for _, p := range pkgs {
		if p.declares() {
			declarerImportPaths["github.com/PRO-Robotech/kacho/"+p.dir] = true
		}
	}

	accounted := 0
	for _, p := range pkgs {
		if p.declares() {
			accounted++
			continue
		}
		delegates := false
		for imp := range p.imports {
			if declarerImportPaths[imp] {
				delegates = true
				break
			}
		}
		if delegates {
			accounted++
			continue
		}
		if _, recorded := listReadRelationExceptions[p.dir]; recorded {
			accounted++
			continue
		}
		t.Errorf("%s фильтрует страницу пообъектно (принимает страницу id, возвращает подмножество, "+
			"спрашивает хранилище прав), но предиката в сверяемом виде НЕ объявляет и не делегирует "+
			"пакету, который объявляет.\n"+
			"  Это ровно то состояние, в котором пятый экземпляр прожил мимо прошлого гейта: не нарушитель "+
			"и не исключение, а невидимка.\n"+
			"  ЧТО ДЕЛАТЬ: объявить предикат package-level коллекцией отношений модели (плоской либо "+
			"map[тип][]отношение с записью %q для остальных), либо звать пакет, который её объявляет, "+
			"либо внести запись с ПРИЧИНОЙ в listReadRelationExceptions.", p.dir, defaultPredicateKey)
	}
	t.Logf("перепись охвата: пакетов-фильтров = %d, с явным исходом = %d", len(pkgs), accounted)
}

// TestListReadRelationParity_PremiseHolds — гейт проверяет СВОЮ предпосылку.
//
// Каждый запрет опирается на факт о дереве. Факт меняется — запрет тихо становится
// ложью, и первым это заметит не тот, кто должен.
func TestListReadRelationParity_PremiseHolds(t *testing.T) {
	root := repoRoot(t)
	pkgs, filesWalked := discoverPageFilters(t, root)
	reads, catalogGets := loadReadGates(t, root)

	if filesWalked == 0 {
		t.Fatalf("под services/ не прочитано ни одного не-тестового .go — гейт осматривает пустоту")
	}
	if catalogGets == 0 {
		t.Fatalf("в каталоге нет ни одной пообъектной записи `/Get` с отношением — сверять не с чем")
	}
	if len(pkgs) == 0 {
		t.Fatalf("ни один пакет не признан фильтром страницы. Либо форма механизма изменилась "+
			"(функция больше не принимает []string и не возвращает подмножество, либо вопрос хранилищу "+
			"прав задаётся иначе — см. relationStoreQuestions), либо пообъектное сужение снято отовсюду. "+
			"В первом случае чинить здесь, во втором гейт больше не нужен. Файлов прочитано: %d", filesWalked)
	}

	// Каждое имя закрытого списка обязано где-то встречаться: список — предпосылка
	// узнавания, и мёртвая запись в нём означает, что узнавание держится на меньшем,
	// чем гейт о себе заявляет.
	seen := map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(root, "services"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil //nolint:nilerr // недоступный подкаталог не должен ронять обход
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, n := range relationStoreQuestions {
			if strings.Contains(string(b), "."+n+"(") {
				seen[n] = true
			}
		}
		return nil
	})
	for _, n := range relationStoreQuestions {
		if !seen[n] {
			t.Errorf("имя %q больше не встречается под services/ — оно в закрытом списке вопросов к "+
				"хранилищу прав, по которому гейт узнаёт механизм. Мёртвая запись в предпосылке делает "+
				"узнавание уже, чем гейт о себе заявляет: сними её либо почини", n)
		}
	}

	declarers := 0
	for _, p := range pkgs {
		if !p.declares() {
			continue
		}
		declarers++
		doms := domainsOf(p, reads)
		if len(doms) == 0 {
			t.Errorf("%s: домен не выведен — ни по ключам объявления (они должны быть типами объектов "+
				"каталога), ни по карте прав сервиса. Сверять с каталогом нечем", p.dir)
			continue
		}
		covered := 0
		for _, dom := range doms {
			for _, g := range reads[dom] {
				if _, ok := p.predicateFor(g.objectType); !ok {
					t.Errorf("%s: тип %q гейтит чтение отношением %q, но предикат страницы для него НЕ "+
						"объявлен. Объявление по типам обязано быть тотальным — добавь запись типа либо "+
						"запись %q для остальных, иначе предикат этого типа спрятан от гейта",
						p.dir, g.objectType, g.relation, defaultPredicateKey)
					continue
				}
				covered++
			}
		}
		if covered == 0 {
			t.Errorf("%s: ни одного типа доменов %v не сверено — сверка для этого пакета ПУСТА, и его "+
				"расхождение гейт бы не увидел", p.dir, doms)
		}
	}
	if declarers == 0 {
		t.Errorf("ни один пакет-фильтр не объявляет предикат в сверяемом виде — сверять нечего")
	}

	// Исключение живёт, пока у него есть предмет — оба вида исключений.
	var clusterGets int
	for _, e := range readListReadCatalog(t, root) {
		m := catalogFqnDomainRe.FindStringSubmatch(e.FQN)
		if m != nil && m[3] == "Get" && e.ScopeExtractor.ObjectType == clusterScopeObjectType {
			clusterGets++
		}
	}
	if clusterGets == 0 {
		t.Errorf("исключению %q больше нечего исключать: в каталоге нет ни одной записи `/Get` с этим "+
			"scope'ом. Запись, пережившая свой предмет, — находка: сними её, иначе она унаследует чужое "+
			"слепое пятно", clusterScopeObjectType)
	}
	live := map[string]bool{}
	for _, p := range pkgs {
		live[p.dir] = true
	}
	for dir, reason := range listReadRelationExceptions {
		if !live[dir] {
			t.Errorf("записанному исключению %q больше нечего исключать: этот пакет больше не признаётся "+
				"фильтром страницы. Сними запись — пережившая свой предмет, она молча накроет следующий "+
				"пакет, который окажется по этому пути", dir)
		}
		if len(strings.TrimSpace(reason)) < 80 {
			t.Errorf("исключение %q не называет причины: запись без обоснования неотличима от упущения", dir)
		}
	}
}

// TestListReadRelationParity_GateDiscriminates — инъекция в ОБЕ стороны на синтетике.
//
// Гейт, который только срабатывает, ничего не говорит о том, что он пропускает.
func TestListReadRelationParity_GateDiscriminates(t *testing.T) {
	reads := map[string][]readGate{
		"vpc":     {{objectType: "vpc_subnet", relation: "v_get", methods: []string{"SubnetService"}}},
		"storage": {{objectType: "storage_volume", relation: "viewer", methods: []string{"VolumeService"}}},
	}
	vpcPkg := func(rels ...string) pageFilterPkg {
		return pageFilterPkg{
			dir: "services/vpc/internal/authzfilter", service: "vpc",
			declFile: "services/vpc/internal/authzfilter/filter.go", declName: "visibilityRelations",
			flat: rels, mapDomain: "vpc",
		}
	}

	t.Run("расхождение краснеет и называет координату", func(t *testing.T) {
		got := findParityViolations([]pageFilterPkg{vpcPkg("viewer", "v_list")}, reads)
		if len(got) != 1 {
			t.Fatalf("расхождение {viewer,v_list} против чтения {v_get} обязано быть находкой; findings=%d", len(got))
		}
		msg := got[0].String()
		for _, want := range []string{
			"services/vpc/internal/authzfilter/filter.go", "vpc_subnet", "v_get", "viewer", "v_list",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("в отказе нет %q — по такому сообщению не найти, что чинить:\n%s", want, msg)
			}
		}
	})

	t.Run("законный близнец той же формы молчит", func(t *testing.T) {
		// Та же форма (пакет + предикат + домен), но предикат СОВПАДАЕТ с чтением.
		// Без этой половины гейт ловил бы форму, а не существо.
		if got := findParityViolations([]pageFilterPkg{vpcPkg("v_get")}, reads); len(got) != 0 {
			t.Errorf("совпадающий предикат не может быть находкой; получено: %v", got)
		}
		// И сервис, чьё чтение гейтится ЯРУСОМ: тот же ярус в предикате обязан молчать,
		// иначе гейт запрещал бы конкретное отношение, а не расхождение.
		storagePkg := pageFilterPkg{
			dir: "services/storage/internal/authzfilter", service: "storage",
			declFile: "services/storage/internal/authzfilter/filter.go", declName: "visibilityRelations",
			flat: []string{"viewer"}, mapDomain: "storage",
		}
		if got := findParityViolations([]pageFilterPkg{storagePkg}, reads); len(got) != 0 {
			t.Errorf("ярус в предикате при ярусном чтении — совпадение, а не находка; получено: %v", got)
		}
	})

	t.Run("недостача тоже находка", func(t *testing.T) {
		// Обратная сторона: страница УЖЕ чтения — собственный читаемый ресурс не виден в
		// своём же списке. Одно направление проверять нельзя.
		if got := findParityViolations([]pageFilterPkg{vpcPkg("v_list")}, reads); len(got) != 1 {
			t.Errorf("предикат, не содержащий отношения чтения, обязан быть находкой")
		}
	})
}

// TestListReadRelationParity_CatchesTheFifthInstance — инъекция НАСТОЯЩИМ входом.
//
// Прошлый гейт был доказан на синтетике и всё равно пропустил пятый экземпляр: его
// синтетика описывала форму, которую он умел находить, а не ту, которая в дереве
// существовала. Поэтому здесь дефект возвращается ТОМУ САМОМУ объявлению, прочитанному
// из дерева, и сверяется с НАСТОЯЩИМ каталогом — подменяется ровно одно: предикат.
//
// Пара обязательна. Возвращённый союз краснеет и называет координату; то же объявление
// как есть — молчит, включая тип, чьё чтение отношения не несёт вовсе (`iam_role`): он
// законный близнец той же формы, и гейт не вправе на него краснеть.
func TestListReadRelationParity_CatchesTheFifthInstance(t *testing.T) {
	root := repoRoot(t)
	pkgs, _ := discoverPageFilters(t, root)
	reads, _ := loadReadGates(t, root)

	const fifth = "services/iam/internal/authzfilter"
	var iam *pageFilterPkg
	for i := range pkgs {
		if pkgs[i].dir == fifth {
			iam = &pkgs[i]
		}
	}
	if iam == nil {
		t.Fatalf("пятый экземпляр (%s) не найден механизмом — инъекции не на чем стоять", fifth)
	}
	if len(iam.byType) == 0 {
		t.Fatalf("%s: предикат прочитан не по типам — эта инъекция подменяет именно записи типов", fifth)
	}

	// Положительный контроль СНАЧАЛА: как есть — молчит.
	if got := findParityViolations([]pageFilterPkg{*iam}, reads); len(got) != 0 {
		t.Fatalf("объявление как есть обязано быть чистым, иначе инъекция ниже ничего не доказывает: %v", got)
	}
	// И `iam_role` при этом не выпал из осмотра по недосмотру: он существует в
	// объявлении и несёт союз — молчание по нему обязано следовать из того, что его
	// чтение отношения не несёт, а не из того, что тип потерялся.
	roleRels, ok := iam.byType["iam_role"]
	if !ok || len(roleRels) < 2 {
		t.Fatalf("ожидался законный близнец `iam_role` с союзом в объявлении; получено %v (ok=%v)", roleRels, ok)
	}

	// Инъекция: вернуть союз ВСЕМ типам, чьё чтение каталог гейтит.
	injected := *iam
	injected.byType = map[string][]string{}
	for k, v := range iam.byType {
		injected.byType[k] = v
	}
	var restored []string
	for _, dom := range domainsOf(*iam, reads) {
		for _, g := range reads[dom] {
			injected.byType[g.objectType] = []string{"viewer", "v_list"}
			restored = append(restored, g.objectType)
		}
	}
	sort.Strings(restored)
	if len(restored) == 0 {
		t.Fatalf("не нашлось ни одного типа с гейтом чтения — возвращать союз некуда")
	}

	got := findParityViolations([]pageFilterPkg{injected}, reads)
	if len(got) != len(restored) {
		t.Fatalf("союз возвращён %d типам (%v), а находок %d — гейт видит не все",
			len(restored), restored, len(got))
	}
	msg := got[0].String()
	for _, want := range []string{"services/iam/internal/authzfilter/visibility.go", "viewer", "v_list"} {
		if !strings.Contains(msg, want) {
			t.Errorf("в отказе нет %q — по такому сообщению не найти, что чинить:\n%s", want, msg)
		}
	}
	t.Logf("инъекция: союз возвращён %d типам (%v) → %d находок, координата названа",
		len(restored), restored, len(got))
}
