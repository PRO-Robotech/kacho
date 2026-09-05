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
//  1. пакет ФИЛЬТРУЕТ СТРАНИЦУ — объявляет функцию, принимающую страницу
//     идентификаторов и возвращающую её видимое подмножество, — И задаёт вопрос
//     хранилищу прав (`Check`/`BatchCheck`…);
//  2. такой пакет ОБЪЯВЛЯЕТ предикат — package-level коллекцию строк, каждая из
//     которых есть отношение, объявленное в `fga_model.fga`.
//
// Оба шага читают ЗНАЧЕНИЕ, а не написание, и это не мелочь оформления: пока
// узнавание держалось на строковом литерале и на скаляре ровно типа `string`, пакет
// снимался с обеих проверок одной правкой стиля — «без магических строк» и доменный
// newtype вместо голой строки, то есть ровно тем, что ruleset этого дерева и
// предписывает. Снимался при этом МОЛЧА: с исходом «делегирует» либо вовсе исчезая
// из переписи. Держит границу TestListReadRelationParity_ReadsValuesNotSpelling —
// одна конструкция в двух написаниях обязана дать один ответ, а то, что строкой не
// является, субъектом не считается.
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
	"reflect"
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
	"services/iam/tools/authzformbench": "" +
		"ПРИБОР ЗАМЕРА, а не код пути запроса: он воспроизводит формы модели доступа своей " +
		"синтетической схемой и меряет их стоимость, решения о видимости не принимает и " +
		"продуктом не импортируется вовсе. Под форму фильтра страницы он попал тем, что " +
		"честно повторяет её сигнатуру — иначе замер мерил бы не то. До линии выноса iam " +
		"прибор лежал в корневом `tools/` и в обход не попадал вовсе; переехал он потому, " +
		"что принадлежит домену iam и обязан уехать вместе с ним, а не потому, что стал " +
		"кодом сервиса. Предикат отношений здесь объявлять НЕ НУЖНО: сверять его было бы " +
		"не с чем — у прибора нет каталога прав, он и есть модель. Запись фиксирует РАЗБОР: " +
		"станет прибор принимать решение о видимости — он уйдёт под сверку.",

	"services/iam/internal/authzcascade": "" +
		"Предиката членства страницы здесь НЕТ, и быть не может: пакет — надстройка над вопросом " +
		"к хранилищу прав (предзагрузка структурных фактов на страницу и порт перечисления " +
		"принципалов), решение о видимости он не принимает. Под форму он попал своими " +
		"`[]string`-поверхностями, а предикат iam живёт в services/iam/internal/authzfilter, " +
		"который этот пакет как раз и обслуживает. Запись фиксирует РАЗБОР, а не послабление: " +
		"появится здесь собственный набор отношений — он станет объявлением и уйдёт под сверку.",

	"services/registry/internal/handler": "" +
		"Строчный фильтр каталога registry сужает страницу по `v_list`, тогда как чтение " +
		"репозитория (манифест/блоб/конфигурация) гейтится `v_get`. Отношение здесь " +
		"передаётся ЯВНО на каждом вызове, а не берётся из карты, — и это записанное " +
		"решение, а не упущение: страница каталога отдаёт голые ИМЕНА, а не сообщение " +
		"ресурса, поэтому «видно в перечне без содержимого» тут реализуемо, в отличие от " +
		"List'ов, возвращающих ту же структуру, что и Get. Разбор — " +
		"services/registry/docs/engineering/architecture/known-divergences.md §«Предикат страницы " +
		"каталога шире отношения чтения». Начнёт каталог отдавать что-то помимо имени — " +
		"запись подлежит пересмотру вместе с предикатом.",

	"services/registry/internal/check": "" +
		"Транспортный адаптер той же страницы: он лишь несёт вопрос до kacho-iam партиями " +
		"(`CheckMany` поверх общего сужателя) и решения о видимости не принимает — " +
		"отношение приходит аргументом от гейта, который его и выбрал (см. запись про " +
		"services/registry/internal/handler). Под форму пакет попал именно передачей " +
		"страницы. Заведи он собственный набор отношений — он станет объявлением и уйдёт " +
		"под сверку.",

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
	// protoPackageDomainRe — второй способ прочитать домен из того же файла.
	//
	// Карта прав перестала быть перечнем FQN и стала выводом из аннотаций: домен
	// теперь назван ОДИН раз, объявлением обслуживаемых proto-пакетов, а не
	// восемьюдесятью повторами в ключах. Первое выражение на таком файле не
	// находит ничего, и гейт объявил бы «домен не выведен» там, где он назван
	// яснее прежнего.
	protoPackageDomainRe = regexp.MustCompile(`"kacho\.cloud\.([a-z0-9]+)\.v1"`)
)

// relationStoreQuestions — закрытый список имён, которыми в этом дереве задаётся
// вопрос хранилищу прав. Список — ПРЕДПОСЫЛКА узнавания механизма, поэтому
// TestListReadRelationParity_PremiseHolds требует, чтобы каждое имя оставалось живым:
// мёртвая запись делает узнавание уже, чем гейт о себе заявляет.
var relationStoreQuestions = []string{"Check", "CheckWithContext", "BatchCheck"}

// sharedNarrowerImportPath — общий сужатель списков. Пакет сервиса, который его
// импортирует и при этом объявляет предикат страницы, задаёт вопрос хранилищу прав
// ЧЕРЕЗ него — и подлежит той же сверке, что и пакет, спрашивающий сам.
//
// Предпосылка проверяется: TestListReadRelationParity_PremiseHolds требует, чтобы
// импорт оставался живым хотя бы у одного пакета, иначе третья форма узнавания
// молча перестала бы что-либо узнавать.
const sharedNarrowerImportPath = "github.com/PRO-Robotech/kacho/pkg/listnarrow"

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

	// inlineRels — отношения модели, встреченные СТРОКОВЫМИ ЛИТЕРАЛАМИ в теле
	// функции-фильтра страницы. Это предикат, объявленный инлайном: он существует
	// и решает видимость, но package-level коллекцией не выражен, поэтому сверке
	// по типам недоступен. Пакет с таким предикатом НЕ делегирует — он спрашивает
	// сам, — и «делегирует либо записан» для него ложный исход.
	inlineRels []string
	inlineAt   string // файл:строка первого литерала — координата для отказа
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

// pkgVocab — СЛОВАРЬ ПАКЕТА: то, чем в нём названы строки и строковые типы.
//
// Без него узнавание держалось на НАПИСАНИИ, а не на значении: и опровержение
// делегирования, и разбор объявления читали только `*ast.BasicLit`, поэтому
// `relViewer` вместо `"viewer"` снимал пакет с обеих проверок разом — одной правкой
// стиля, которую ruleset этого дерева как раз и предписывает («без магических строк»,
// skill evgeniy). Тем же способом уходил субъект: forma требовала скаляр РОВНО типа
// `string`, а доменный newtype (`type Subject string`) ею не признавался вовсе —
// пакет не становился ни находкой, ни исключением, он исчезал из переписи.
//
// Словарь собирается ПЕРВЫМ проходом по всем файлам каталога, потому что константа
// может быть объявлена в файле, который обходчик прочитает позже фильтра.
type pkgVocab struct {
	val        map[string]string // имя package-level const/var -> строковое значение
	stringType map[string]bool   // имя собственного типа пакета, чья основа — string
}

func newPkgVocab() *pkgVocab {
	return &pkgVocab{val: map[string]string{}, stringType: map[string]bool{}}
}

// stringValue — значение выражения как строки: литерал ЛИБО имя, разрешаемое словарём
// пакета. Второй результат — признак успеха: пустая строка здесь ЗНАЧАЩАЯ (ключ «все
// прочие типы»), и спутать её с «не строка» значило бы принять чужое выражение за
// объявление умолчания.
func (v *pkgVocab) stringValue(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := v.val[x.Name]
		return s, ok
	}
	return "", false
}

// isStringScalar — тип скалярного параметра, который МОЖЕТ нести строку: сам `string`
// либо собственный строковый тип пакета. Это и есть «про кого / про что» спрашивает
// фильтр страницы.
func (v *pkgVocab) isStringScalar(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "string" || v.stringType[id.Name]
}

// parsedGoFile — файл каталога, разобранный один раз и использованный обоими проходами.
type parsedGoFile struct {
	path string
	fset *token.FileSet
	file *ast.File
}

// discoverPageFilters обходит services/ и собирает пакеты, фильтрующие страницу
// пообъектно, вместе с объявленным предикатом каждого. Возвращает и перепись —
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
func discoverPageFilters(t *testing.T, root string) (pkgs []pageFilterPkg, filesWalked int) {
	t.Helper()
	relations := modelRelations(t, root)

	byDir := map[string][]parsedGoFile{}
	var dirs []string
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
		if _, seen := byDir[dir]; !seen {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], parsedGoFile{path: path, fset: fset, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("обойти services/: %v", err)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		files := byDir[dir]
		relDir, _ := filepath.Rel(root, dir)
		slashed := filepath.ToSlash(relDir)
		parts := strings.Split(slashed, "/")
		svc := ""
		if len(parts) > 1 {
			svc = parts[1]
		}
		p := &pageFilterPkg{dir: slashed, service: svc, imports: map[string]bool{}}

		// Проход 1 — словарь пакета. До него ни одна форма не узнаётся, иначе
		// узнавание зависит от того, в каком файле лежит константа.
		vocab := newPkgVocab()
		for _, pf := range files {
			collectPkgVocab(pf.file, vocab)
		}

		// Проход 2 — сами формы.
		for _, pf := range files {
			for _, im := range pf.file.Imports {
				if v, uerr := strconv.Unquote(im.Path.Value); uerr == nil {
					p.imports[v] = true
				}
			}
			path, fset := pf.path, pf.fset
			ast.Inspect(pf.file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					if x.Type != nil && funcTakesPageReturnsSubset(x.Type, vocab) {
						p.pageShape = true
						// Предикат, объявленный ИНЛАЙНОМ в теле самого фильтра, —
						// это тоже предикат. Собираем его отдельно: без этого пакет,
						// спрашивающий модель своими отношениями, неотличим от
						// пакета, делегирующего чужому объявлению.
						collectInlineRelations(x.Body, relations, fset, root, p, vocab)
					}
				case *ast.InterfaceType:
					if x.Methods == nil {
						return true
					}
					for _, m := range x.Methods.List {
						if ft, ok := m.Type.(*ast.FuncType); ok && funcTakesPageReturnsSubset(ft, vocab) {
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
						flat, byType, ok := parseRelationDeclaration(x.Values[i], relations, vocab)
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
		}

		// Третья форма узнавания: пакет ОБЪЯВЛЯЕТ предикат и ПЕРЕДАЁТ его общему
		// сужателю.
		//
		// Она заведена вместе с переносом механики в `pkg/listnarrow`. До переноса
		// «фильтрует страницу» и «спрашивает хранилище прав» лежали в одном пакете, и
		// двух признаков хватало. После переноса у сервиса остаётся ровно то, что у
		// него СВОЁ — словарь ресурсов и предикат, — а вопрос задаёт общий код;
		// два прежних признака перестают выполняться, и четыре сервиса ушли бы
		// из-под сверки МОЛЧА, оставив гейт зелёным на том, что он больше не читает.
		//
		// Признак — значение, а не написание: передача предиката общему сужателю
		// видна по ИМПОРТУ его пакета, а не по имени переменной или функции.
		if p.imports[sharedNarrowerImportPath] && p.declares() {
			p.pageShape, p.relStore = true, true
		}
		if !p.pageShape || !p.relStore {
			continue
		}
		p.mapDomain = deriveServiceDomain(filepath.Join(root, "services", svc))
		pkgs = append(pkgs, *p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].dir < pkgs[j].dir })
	return pkgs, filesWalked
}

// collectPkgVocab собирает package-level строковые имена и собственные строковые типы.
// Область — только уровень пакета: локальная переменная функции предикатом пакета не
// является и разрешению не подлежит.
func collectPkgVocab(f *ast.File, v *pkgVocab) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				for i, nm := range s.Names {
					if i >= len(s.Values) {
						continue
					}
					if bl, isLit := s.Values[i].(*ast.BasicLit); isLit && bl.Kind == token.STRING {
						if str, uerr := strconv.Unquote(bl.Value); uerr == nil {
							v.val[nm.Name] = str
						}
					}
				}
			case *ast.TypeSpec:
				if id, isID := s.Type.(*ast.Ident); isID && id.Name == "string" {
					v.stringType[s.Name.Name] = true
				}
			}
		}
	}
}

// funcTakesPageReturnsSubset — «спрашивает ЗА КОГО-ТО про страницу идентификаторов и
// возвращает её подмножество». Это и есть форма пообъектного сужения списка.
//
// Скалярный строковый параметр рядом со страницей обязателен и несёт смысл: фильтр
// всегда спрашивает ПРО КОГО (субъект) и/или ПРО ЧТО (тип объекта), а чистый помощник
// над списком — нет. Без этого условия под форму попадал `dedupe(ids []string) []string`
// в соседнем пакете, и охват объявлял находкой вспомогательную функцию — гейт, красный
// на законной конструкции, отключают первым.
// collectInlineRelations собирает отношения модели, названные В ТЕЛЕ функции-фильтра
// страницы, — литералом ЛИБО именем package-level строковой константы.
//
// Имя разрешается словарём пакета намеренно: опровержение делегирования, читающее
// только литералы, снимается одной правкой стиля (`relViewer` вместо `"viewer"`), и
// снимается МОЛЧА — пакет получает исход «делегирует» и его предикат не сверяется ни
// с чем. Читать надо значение, а не написание.
//
// Область по-прежнему узкая — только тело страницы-фильтра. Отношение, названное в
// СОСЕДНЕЙ функции (проверка полномочий на мутацию, «admin» на объекте области),
// предикатом страницы не является, и расширение области сделало бы гейт красным на
// законной конструкции. Проверено обеими половинами в
// TestListReadRelationParity_DelegationMustBeReal.
func collectInlineRelations(
	body *ast.BlockStmt, relations map[string]bool,
	fset *token.FileSet, root string, p *pageFilterPkg, vocab *pkgVocab,
) {
	if body == nil {
		return
	}
	seen := map[string]bool{}
	for _, r := range p.inlineRels {
		seen[r] = true
	}
	note := func(v string, pos token.Pos) {
		if !relations[v] || seen[v] {
			return
		}
		seen[v] = true
		p.inlineRels = append(p.inlineRels, v)
		if p.inlineAt == "" {
			pp := fset.Position(pos)
			if rel, rerr := filepath.Rel(root, pp.Filename); rerr == nil {
				p.inlineAt = fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pp.Line)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if v, err := strconv.Unquote(x.Value); err == nil {
					note(v, x.Pos())
				}
			}
		case *ast.Ident:
			if v, ok := vocab.val[x.Name]; ok {
				note(v, x.Pos())
			}
		}
		return true
	})
	sort.Strings(p.inlineRels)
}

func funcTakesPageReturnsSubset(ft *ast.FuncType, vocab *pkgVocab) bool {
	takesPage, takesScalar := false, false
	if ft.Params != nil {
		for _, prm := range ft.Params.List {
			switch pt := prm.Type.(type) {
			case *ast.ArrayType:
				if pt.Len == nil && vocab.isStringScalar(pt.Elt) {
					takesPage = true
				}
			default:
				if vocab.isStringScalar(pt) {
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
			if rt.Len == nil && vocab.isStringScalar(rt.Elt) {
				return true
			}
		case *ast.MapType:
			v, vok := rt.Value.(*ast.Ident)
			if vok && vocab.isStringScalar(rt.Key) && v.Name == "bool" {
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
func parseRelationDeclaration(
	v ast.Expr, relations map[string]bool, vocab *pkgVocab,
) (flat []string, byType map[string][]string, ok bool) {
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
			key, kok := vocab.stringValue(kv.Key)
			if !kok {
				return nil, nil, false
			}
			rels, rok := relationSlice(kv.Value, relations, vocab)
			if !rok {
				return nil, nil, false
			}
			byType[key] = rels
		}
		return nil, byType, len(byType) > 0
	}
	rels, rok := relationSlice(cl, relations, vocab)
	if !rok {
		return nil, nil, false
	}
	return rels, nil, true
}

func relationSlice(e ast.Expr, relations map[string]bool, vocab *pkgVocab) ([]string, bool) {
	cl, isCL := e.(*ast.CompositeLit)
	if !isCL || len(cl.Elts) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		s, sok := vocab.stringValue(el)
		if !sok || !relations[s] {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
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
		for _, m := range protoPackageDomainRe.FindAllStringSubmatch(string(b), -1) {
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
			// Пустое отношение при названном объекте выводит тип из сверки МОЛЧА —
			// за этим следит TestListReadRelationParity_PremiseHolds, здесь только
			// пропуск: сверять такую запись не с чем.
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
			// Путь импорта строится ОТОБРАЖЕНИЕМ, а не склейкой корневого
			// префикса с путём дерева: модулей в дереве два, и у службы iam
			// префикс другой. Склейка давала путь, которого не существует, —
			// делегирование переставало распознаваться, и пакет объявлялся
			// «невидимкой», ничего в себе не изменив.
			declarerImportPaths[importOfTreeRel(p.dir)] = true
		}
	}

	accounted := 0
	for _, p := range pkgs {
		reason, ok := pageFilterOutcome(p, declarerImportPaths)
		if !ok {
			t.Error(reason)
			continue
		}
		accounted++
	}
	t.Logf("перепись охвата: пакетов-фильтров = %d, с явным исходом = %d", len(pkgs), accounted)
}

// pageFilterOutcome — РЕШЕНИЕ гейта об одном пакете-фильтре, вынесенное из тела
// теста, чтобы самопроверка ниже исполняла ту же логику, а не её копию.
// Копия самопроверки доказывала бы только собственную непротиворечивость.
//
// ok=true — у пакета есть явный исход; ok=false — находка, текст в reason.
func pageFilterOutcome(p pageFilterPkg, declarerImportPaths map[string]bool) (reason string, ok bool) {
	if p.declares() {
		return "объявляет предикат", true
	}
	if _, recorded := listReadRelationExceptions[p.dir]; recorded {
		return "записанное исключение", true
	}
	// Инлайновый предикат разбирается ДО делегирования, и это не порядок ради
	// порядка. «Делегирует» выводится из РЕБРА ИМПОРТА, а импорт покупается
	// одной строкой: пакет, спрашивающий модель СВОИМИ отношениями, получал
	// исход «делегирует» ровно потому, что рядом стоял импорт объявителя, — и
	// его предикат не сверялся ни с чем. Наличие собственных отношений
	// опровергает делегирование прямо: тот, кто спрашивает сам, чужому
	// объявлению не делегирует.
	if len(p.inlineRels) > 0 {
		return fmt.Sprintf("%s фильтрует страницу пообъектно и задаёт вопрос модели СВОИМИ отношениями %v, "+
			"записанными литералами в теле фильтра (%s), но package-level предиката НЕ объявляет.\n"+
			"  Такой предикат существует и решает видимость, однако сверке по типам недоступен: гейт не может "+
			"сопоставить его с отношением чтения из каталога, поэтому расхождение «страница ≠ чтение» здесь "+
			"не обнаруживается ничем.\n"+
			"  Импорт пакета-объявителя этого НЕ исправляет и исходом «делегирует» не является: делегирует тот, "+
			"кто чужим объявлением и спрашивает, а не тот, кто рядом его импортирует.\n"+
			"  ЧТО ДЕЛАТЬ: поднять отношения в package-level коллекцию (плоскую либо map[тип][]отношение), "+
			"либо задавать вопрос через пакет-объявитель, либо внести запись с ПРИЧИНОЙ в listReadRelationExceptions.",
			p.dir, p.inlineRels, p.inlineAt), false
	}
	for imp := range p.imports {
		if declarerImportPaths[imp] {
			return "делегирует объявителю", true
		}
	}
	return fmt.Sprintf("%s фильтрует страницу пообъектно (принимает страницу id, возвращает подмножество, "+
		"спрашивает хранилище прав), но предиката в сверяемом виде НЕ объявляет и не делегирует "+
		"пакету, который объявляет.\n"+
		"  Это ровно то состояние, в котором пятый экземпляр прожил мимо прошлого гейта: не нарушитель "+
		"и не исключение, а невидимка.\n"+
		"  ЧТО ДЕЛАТЬ: объявить предикат package-level коллекцией отношений модели (плоской либо "+
		"map[тип][]отношение с записью %q для остальных), либо звать пакет, который её объявляет, "+
		"либо внести запись с ПРИЧИНОЙ в listReadRelationExceptions.", p.dir, defaultPredicateKey), false
}

// TestListReadRelationParity_DelegationMustBeReal — самопроверка того, что
// «делегирует» больше не покупается ребром импорта.
//
// Инъекция в обе стороны на ОДНОМ И ТОМ ЖЕ входе: два пакета отличаются ровно
// одним — есть ли у фильтра собственные отношения. Прежняя редакция обе клетки
// считала делегированием, потому что смотрела только на импорт.
func TestListReadRelationParity_DelegationMustBeReal(t *testing.T) {
	declarers := map[string]bool{"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter": true}
	importsDeclarer := map[string]bool{"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter": true}

	t.Run("спрашивает сам, но импортирует объявителя — находка", func(t *testing.T) {
		p := pageFilterPkg{
			dir: "services/vpc/internal/probe", service: "vpc",
			pageShape: true, relStore: true,
			imports:    importsDeclarer,
			inlineRels: []string{"v_list", "viewer"},
			inlineAt:   "services/vpc/internal/probe/probe.go:25",
		}
		reason, ok := pageFilterOutcome(p, declarers)
		if ok {
			t.Fatalf("пакет со СВОИМ предикатом признан имеющим явный исход (%q) — ровно та дыра, "+
				"через которую инлайновый союз проходил незамеченным", reason)
		}
		if !strings.Contains(reason, "services/vpc/internal/probe") || !strings.Contains(reason, "probe.go:25") {
			t.Fatalf("отказ не называет координату: %s", reason)
		}
	})

	t.Run("законный близнец той же формы — делегирует, молчит", func(t *testing.T) {
		p := pageFilterPkg{
			dir: "services/vpc/internal/probe", service: "vpc",
			pageShape: true, relStore: true,
			imports: importsDeclarer,
			// собственных отношений нет: вопрос задаётся объявителем
		}
		if reason, ok := pageFilterOutcome(p, declarers); !ok {
			t.Fatalf("настоящее делегирование признано находкой — гейт ловит форму, а не существо: %s", reason)
		}
	})

	t.Run("ни своего предиката, ни делегирования — по-прежнему находка", func(t *testing.T) {
		p := pageFilterPkg{
			dir: "services/vpc/internal/probe", service: "vpc",
			pageShape: true, relStore: true,
			imports: map[string]bool{},
		}
		if _, ok := pageFilterOutcome(p, declarers); ok {
			t.Fatal("невидимка признана имеющей явный исход — прежняя половина гейта потеряна")
		}
	})
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
			// Форма `"."+имя+"("` к хвосту чужого идентификатора НЕВОСПРИИМЧИВА
			// by construction: точка символом имени не бывает, поэтому слева от
			// имени стоит разделитель, а справа — скобка. Проверено на закрытом
			// списке: `.Check(` не совпадает ни с `.BatchCheck(` (слева буква),
			// ни с `.CheckWithContext(` (справа буква).
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

	// Множество сверки обязано усыхать ГРОМКО. Проверка `covered == 0` ловит только
	// полное обнуление: тип, у записи которого стёрли отношение, выпадает из сверки
	// по одному — 21 пара становится 20, и ни один гейт не краснеет. Ниже — тот
	// самый механизм выпадения, названный прямо: публичная запись `/Get`, которая
	// НАЗЫВАЕТ объект области, обязана нести и отношение чтения.
	//
	// Запись БЕЗ объекта области — другой случай и здесь не рассматривается: у неё
	// нет типа, с которым можно сверять предикат страницы, и решение о ней принимает
	// не этот гейт. Такие записи перечислены ниже поимённо, чтобы их число не росло
	// незаметно.
	var scopedWithoutRelation, unscopedGets []string
	for _, e := range readListReadCatalog(t, root) {
		m := catalogFqnDomainRe.FindStringSubmatch(e.FQN)
		if m == nil || m[3] != "Get" || strings.HasPrefix(m[2], "Internal") {
			continue
		}
		switch {
		case e.ScopeExtractor.ObjectType == "":
			unscopedGets = append(unscopedGets, e.FQN)
		case e.ScopeExtractor.ObjectType != clusterScopeObjectType && e.RequiredRelation == "":
			scopedWithoutRelation = append(scopedWithoutRelation, e.FQN+" (объект "+e.ScopeExtractor.ObjectType+")")
		}
	}
	sort.Strings(scopedWithoutRelation)
	sort.Strings(unscopedGets)
	if len(scopedWithoutRelation) > 0 {
		t.Errorf("публичный `/Get` называет объект области, но НЕ называет отношение чтения (%d): %s\n"+
			"  Такая запись выпадает из сверки этого гейта БЕЗ единого красного: тип просто перестаёт "+
			"попадать в множество пар, и расхождение «страница ≠ чтение» по нему больше не проверяется "+
			"ничем.\n"+
			"  ЧТО ДЕЛАТЬ: вернуть отношение в запись каталога (регенерация из proto), либо — если "+
			"чтение этого типа намеренно не гейтится пообъектно — снять и объект области, чтобы запись "+
			"перестала обещать сужение, которого нет",
			len(scopedWithoutRelation), strings.Join(scopedWithoutRelation, ", "))
	}
	t.Logf("перепись предпосылки: публичных `/Get` без объекта области = %d %v; "+
		"с объектом, но без отношения = %d; сверяемых пар = %d",
		len(unscopedGets), unscopedGets, len(scopedWithoutRelation), catalogGets)

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

// TestListReadRelationParity_ReadsValuesNotSpelling — узнавание держится на ЗНАЧЕНИИ,
// а не на написании.
//
// Прежняя редакция читала только `*ast.BasicLit` — и в опровержении делегирования, и в
// разборе объявления. Поэтому одна правка стиля («без магических строк», ровно то, что
// предписывает ruleset этого дерева) снимала пакет с ОБЕИХ проверок разом и снимала
// МОЛЧА: он получал исход «делегирует», а его предикат не сверялся ни с чем. Тем же
// способом исчезал пакет, у которого субъект — доменный newtype, а не голая строка:
// форма не признавалась вовсе, и он не становился ни находкой, ни исключением.
//
// Проба даёт ОДНУ И ТУ ЖЕ конструкцию в двух написаниях и требует одинакового ответа,
// плюс держит границу: то, что строкой не является, скаляром-субъектом не считается.
func TestListReadRelationParity_ReadsValuesNotSpelling(t *testing.T) {
	relations := map[string]bool{"viewer": true, "v_list": true, "v_get": true}

	parse := func(t *testing.T, src string) (*ast.File, *token.FileSet) {
		t.Helper()
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "synthetic.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разобрать синтетику: %v", err)
		}
		return f, fset
	}
	// scan — то же, что делает обходчик дерева: словарь пакета, затем формы.
	scan := func(t *testing.T, src string) pageFilterPkg {
		t.Helper()
		f, fset := parse(t, src)
		vocab := newPkgVocab()
		collectPkgVocab(f, vocab)
		p := pageFilterPkg{dir: "synthetic", imports: map[string]bool{}}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Type != nil && funcTakesPageReturnsSubset(x.Type, vocab) {
					p.pageShape = true
					collectInlineRelations(x.Body, relations, fset, ".", &p, vocab)
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
					if flat, byType, ok := parseRelationDeclaration(x.Values[i], relations, vocab); ok {
						p.declName, p.flat, p.byType = nm.Name, flat, byType
					}
				}
			}
			return true
		})
		return p
	}

	const bodyTmpl = `package p

import "context"

%s

type C interface{ Check(ctx context.Context, s, r, o string) (bool, error) }

func Visible(ctx context.Context, c C, subject string, ids []string) []string {
	out := []string{}
	for _, id := range ids {
		for _, rel := range %s {
			if ok, _ := c.Check(ctx, subject, rel, id); ok {
				out = append(out, id)
			}
		}
	}
	return out
}
`

	t.Run("инлайновый предикат: литералы и имена читаются одинаково", func(t *testing.T) {
		lit := scan(t, fmt.Sprintf(bodyTmpl, "", `[]string{"viewer", "v_list"}`))
		named := scan(t, fmt.Sprintf(bodyTmpl,
			"const (\n\trelViewer = \"viewer\"\n\trelVList  = \"v_list\"\n)", `[]string{relViewer, relVList}`))
		if len(lit.inlineRels) == 0 {
			t.Fatal("литеральная запись не распознана — предпосылка пробы сломана")
		}
		if !reflect.DeepEqual(lit.inlineRels, named.inlineRels) {
			t.Fatalf("одна и та же конструкция читается по-разному: литералами %v, именами %v.\n"+
				"  Именно этим одна правка стиля снимала пакет с опровержения делегирования: он получал "+
				"исход «делегирует», и его предикат не сверялся ни с чем",
				lit.inlineRels, named.inlineRels)
		}
	})

	t.Run("package-level объявление: литералы и имена читаются одинаково", func(t *testing.T) {
		lit := scan(t, fmt.Sprintf(bodyTmpl,
			"var pageRelations = []string{\"viewer\", \"v_list\"}", "pageRelations"))
		named := scan(t, fmt.Sprintf(bodyTmpl,
			"const (\n\trelViewer = \"viewer\"\n\trelVList  = \"v_list\"\n)\n\n"+
				"var pageRelations = []string{relViewer, relVList}", "pageRelations"))
		if !lit.declares() {
			t.Fatal("литеральное объявление не распознано — предпосылка пробы сломана")
		}
		if !named.declares() || !reflect.DeepEqual(lit.flat, named.flat) {
			t.Fatalf("объявление именами не признано объявлением: литералами %v, именами %v (declares=%v).\n"+
				"  Такой предикат решает видимость, но сверке по типам недоступен — расхождение "+
				"«страница ≠ чтение» по нему не проверяется ничем", lit.flat, named.flat, named.declares())
		}
	})

	t.Run("ключ карты именем — тоже ключ", func(t *testing.T) {
		p := scan(t, fmt.Sprintf(bodyTmpl,
			"const typeSubnet = \"vpc_subnet\"\n\n"+
				"var pageRelations = map[string][]string{typeSubnet: {\"v_get\"}}", `pageRelations[""]`))
		if rels, ok := p.byType["vpc_subnet"]; !ok || !reflect.DeepEqual(rels, []string{"v_get"}) {
			t.Fatalf("ключ, названный константой, не разрешён: byType=%v", p.byType)
		}
	})

	t.Run("субъект доменным типом — форма признаётся", func(t *testing.T) {
		p := scan(t, fmt.Sprintf(bodyTmpl,
			"type Subject string\n\nvar pageRelations = []string{\"v_get\"}", "pageRelations"))
		if !p.pageShape {
			t.Fatal("предпосылка пробы сломана: голая строка перестала признаваться")
		}
		named := scan(t, `package p

import "context"

type Subject string

var pageRelations = []string{"v_get"}

type C interface{ Check(ctx context.Context, s, r, o string) (bool, error) }

func Visible(ctx context.Context, c C, subject Subject, ids []string) []string {
	out := []string{}
	for _, id := range ids {
		for _, rel := range pageRelations {
			if ok, _ := c.Check(ctx, string(subject), rel, id); ok {
				out = append(out, id)
			}
		}
	}
	return out
}
`)
		if !named.pageShape {
			t.Fatal("фильтр, чей субъект — доменный newtype, формой НЕ признан. Такой пакет исчезает " +
				"из переписи целиком: он не находка, не исключение и не делегирование, а невидимка — " +
				"ровно то состояние, в котором мимо гейта прожил пятый экземпляр")
		}
	})

	t.Run("законный близнец: не-строковый скаляр субъектом не считается", func(t *testing.T) {
		p := scan(t, `package p

import "context"

type C interface{ Check(ctx context.Context, s, r, o string) (bool, error) }

// Head — помощник над списком: ни про кого не спрашивает.
func Head(ids []string, limit int) []string { return ids }
`)
		if p.pageShape {
			t.Fatal("помощник над списком признан фильтром страницы — гейт, красный на законной " +
				"конструкции, отключают первым")
		}
	})

	t.Run("законный близнец: чужая строка в наборе — не предикат отношений", func(t *testing.T) {
		p := scan(t, fmt.Sprintf(bodyTmpl,
			"const relViewer = \"viewer\"\nconst notARelation = \"выдумка\"\n\n"+
				"var pageRelations = []string{relViewer, notARelation}", "pageRelations"))
		if p.declares() {
			t.Fatalf("набор с посторонней строкой признан предикатом отношений: %v", p.flat)
		}
	})
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

// TestListReadRelationParity_SeesDeclarersThatDelegateToTheSharedNarrower —
// инъекция ТРЕТЬЕЙ формы узнавания, в обе стороны.
//
// Форма заведена вместе с переносом механики в общий фундамент: у сервиса остаётся
// словарь и предикат, а вопрос задаёт общий код. Два прежних признака («функция
// формы страницы» + «зовёт хранилище прав») при этом перестают выполняться, и без
// третьего четыре сервиса ушли бы из-под сверки МОЛЧА — гейт остался бы зелёным на
// том, чего больше не читает. Это и есть класс, который он ловит у других.
//
// Половина «краснеет» здесь достигается не подменой предиката, а самим фактом
// УЗНАВАНИЯ: если форма не узнаётся, пакета нет в переписи, и сверять нечего.
func TestListReadRelationParity_SeesDeclarersThatDelegateToTheSharedNarrower(t *testing.T) {
	root := repoRoot(t)
	pkgs, _ := discoverPageFilters(t, root)

	byDir := map[string]pageFilterPkg{}
	for _, p := range pkgs {
		byDir[p.dir] = p
	}

	// (а) положительная сторона: каждый сервис, отдавший механику общему сужателю,
	// ОБЯЗАН быть в переписи и объявлять предикат. Иначе он вне сверки.
	delegators := []string{
		"services/vpc/internal/authzfilter",
		"services/compute/internal/authzfilter",
		"services/nlb/internal/authzfilter",
		"services/storage/internal/authzfilter",
	}
	for _, dir := range delegators {
		p, ok := byDir[dir]
		if !ok {
			t.Errorf("%s передаёт предикат общему сужателю, но гейтом НЕ УЗНАН: "+
				"он вне сверки, и его расхождение с каталогом не покраснеет ни при каком значении", dir)
			continue
		}
		if !p.declares() {
			t.Errorf("%s узнан, но предиката не объявляет — сверять не с чем", dir)
			continue
		}
		if !p.imports[sharedNarrowerImportPath] {
			t.Errorf("%s узнан НЕ по передаче предиката общему сужателю — предпосылка формы не выполняется", dir)
		}
	}

	// (б) отрицательная сторона: узнавание держится на ЗНАЧЕНИИ (импорт + объявление),
	// а не на месте в дереве. Пакет с тем же импортом, но БЕЗ предиката, третьей формой
	// не узнаётся — иначе гейт считал бы фильтром любого потребителя общего кода.
	probe := &pageFilterPkg{
		dir:     "services/probe/internal/consumer",
		service: "probe",
		imports: map[string]bool{sharedNarrowerImportPath: true},
	}
	if probe.declares() {
		t.Fatalf("предпосылка пробы сломана: у неё не должно быть объявления")
	}
	if probe.imports[sharedNarrowerImportPath] && probe.declares() {
		t.Errorf("пакет без предиката признан фильтром страницы — узнавание ловит импорт, а не предмет")
	}
}
