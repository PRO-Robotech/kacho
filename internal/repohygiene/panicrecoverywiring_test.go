// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// panicrecoverywiring_test.go — гейт против отказа в обслуживании: у каждого
// gRPC-листенера обязано быть звено, восстанавливающее панику обработчика.
//
// Предмет. grpc-go панику обработчика НЕ восстанавливает: она уходит из
// серверной горутины и валит процесс. Значит дефект в обработке ОДНОГО запроса
// одного тенанта прекращает обслуживание ВСЕХ тенантов сервиса — вместе с
// in-flight запросами, исполнителем операций и дренажами. Один nil-разыменование
// на пути запроса равносилен kill -9 по сервису, и вызвать его может кто угодно,
// кому этот RPC доступен.
//
// Почему гейт, а не разовая правка. Класс жил в ТРЁХ сервисах из семи
// одновременно, а у четырёх оставшихся звено было заведено четырьмя
// независимыми реализациями (пятая — на крае) — то есть свойство держалось
// привычкой автора, а не чем-либо в дереве. Пока переписи не было, у края
// вдобавок оставался ВТОРОЙ листенер, о котором не помнил никто.
//
// Единица проверки — ЛИСТЕНЕР (вызов NewServer), а область обхода ВЫВЕДЕНА из
// дерева (services/*/cmd плюс gateway/cmd), а не выписана списком. Поэтому
// восьмой сервис и второй листенер существующего краснеют в момент появления,
// а не тогда, когда кто-то снова пройдёт по дереву руками.
//
// # Ловушка этой темы, ради которой гейт читает исполняемую часть
//
// Слово «recovery» в этом дереве занято ДРУГИМ предметом. Под именем
// `recovery.go` в композиционном корне ШЕСТИ сервисов лежит разрешитель
// осиротевших операций (`pkg/operations.NewReconciler`) — восстановление
// незавершённых операций после перезапуска процесса. Он не имеет никакого
// отношения к панике: не возвращает интерсептора и не зовёт `recover()`.
// Текстовый гейт, ищущий «Recovery», нашёл бы эти файлы во ВСЕХ сервисах и
// позеленел бы при полностью снятой защите от паники — то есть был бы ровно тем
// классом «форма без содержания», который мы ловим в продуктовом коде.
//
// Поэтому распознавание идёт по СУЩЕСТВУ, а не по имени: звеном считается
// функция, которая (а) возвращает `grpc.UnaryServerInterceptor` либо
// `grpc.StreamServerInterceptor` и (б) в своём теле зовёт `recover()`.
// Ни одно из двух условий не выполняется ни для разрешителя операций, ни для
// упоминания в комментарии, ни для строкового литерала. Имя функции гейт не
// читает вовсе — переименование звена его не обманет и не сломает.
package repohygiene

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
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// grpcImportPath — пакет, по объявлению импорта которого опознаётся локальное
// имя `grpc` в каждом файле. Файл вправе импортировать его под своим именем;
// текстовый поиск «grpc.ChainUnaryInterceptor» был бы на этом слеп.
const grpcImportPath = "google.golang.org/grpc"

// carrierImportPath — носитель входящего пути. Компонент, переведённый на него,
// СВОИХ листенеров не поднимает: их поднимает `servicehost.Serve`, и звено
// восстановления паники стоит в ЕГО цепочке (вторым, сразу под журналом
// доступа). Поэтому «у компонента ноль листенеров» перестало быть однозначным
// признаком сломанного распознавания — но однозначным оно обязано остаться, и
// различает два случая именно этот импорт.
const carrierImportPath = "github.com/PRO-Robotech/kacho/pkg/servicehost"

// panicRecoveryScanRoots — где ищем сами звенья. Звено вправе жить в общем
// фундаменте, в сервисе или на крае; гейт не требует конкретного адреса, он
// требует, чтобы у листенера оно было.
var panicRecoveryScanRoots = []string{"pkg", "services", "gateway"}

// interceptorTypeOfKind — тип, возвращаемый звеном соответствующего вида.
//
// Оба вида обязательны у КАЖДОГО листенера. «У нас тут нет стримов» — неверное
// основание: стримы несёт и InternalWatchService, и server-reflection, а
// stream-обработчик паникует ровно так же, как unary.
var interceptorTypeOfKind = map[string]string{
	"unary":  "UnaryServerInterceptor",
	"stream": "StreamServerInterceptor",
}

// serverConstructorName — вызов, поднимающий листенер. ЕДИНИЦА СЧЁТА ГЕЙТА —
// именно он, а не отдельная опция цепочки: `grpc.ChainUnaryInterceptor`
// НАКАПЛИВАЕТСЯ (server.go: `o.chainUnaryInts = append(...)`), поэтому один
// листенер вправе собираться несколькими опциями из разных мест, и судить
// каждую по отдельности значило бы объявлять находкой ту, чьё звено пришло
// соседней опцией. Ровно это и произошло на крае при первом прогоне.
const serverConstructorName = "NewServer"

// TestEveryGRPCListenerRecoversHandlerPanics — сам гейт.
//
// Что делать, если сработал, — два исхода, третьего нет:
//
//  1. листенер обслуживает трафик -> провязать звено восстановления паники в
//     его цепочку. Общее звено — `pkg/grpcsrv.UnaryPanicRecovery` /
//     `StreamPanicRecovery`; своё писать не нужно и не следует (пять
//     независимых реализаций уже расходились между собой по тому, что они
//     отдают клиенту и переживают ли отсутствие журнала);
//  2. это не листенер, а совпадение формы -> уточнить распознавание ниже,
//     а не заводить список исключений: списка исключений у этого гейта нет
//     намеренно — исключение пережило бы свой предмет и унаследовало бы
//     следующую слепую зону.
//
// Проверено инъекцией в обе стороны (panicrecoverywiring_injection_test.go):
// TestPanicRecoveryGateRedOnInjectedDefect — снятие звена красит гейт и он
// печатает координату; TestPanicRecoveryGateSilentOnLawfulSameShape — законная
// конструкция той же формы его не задевает;
// TestPanicRecoveryGateIgnoresLRORecoveryDecoyEvenWhenUnwired — приманка
// «recovery» рядом со СНЯТЫМ звеном гейт не зеленит, то есть распознавание
// действительно не по имени.
//
// Сверх синтетики то же проверено на живом дереве: снятие ОДНОГО stream-звена
// из шестнадцати листенеров даёт находку с точной координатой и указанием
// недостающего вида.
func TestEveryGRPCListenerRecoversHandlerPanics(t *testing.T) {
	res := auditPanicRecoveryWiring(t, repoRoot(t))
	t.Log(res.summary)
	if len(res.findings) > 0 {
		sort.Strings(res.findings)
		t.Fatalf("листенеры без звена восстановления паники (%d из %d):\n  %s",
			len(res.findings), res.listeners, strings.Join(res.findings, "\n  "))
	}
}

// panicRecoveryAudit — исход обхода: находки плюс объём осмотренного, чтобы
// «ноль находок» было отличимо от «ноль прочитанного».
type panicRecoveryAudit struct {
	// serviceBuilders — сколько мест формы `NewServer` отсеяно распознаванием как
	// СБОРКА СЛУЖБЫ. Печатается и доступно пробам: «находок ноль» обязано быть
	// отличимо от «ветвь не исполнялась».
	serviceBuilders int
	findings        []string
	listeners       int
	covered         int
	summary         string
}

// auditPanicRecoveryWiring — ядро гейта, вынесенное отдельно, чтобы пробы
// инъекции гоняли ТО ЖЕ САМОЕ, что гоняется по дереву. Проба, повторяющая
// логику гейта своей копией, доказывала бы свойство копии.
func auditPanicRecoveryWiring(t *testing.T, root string) panicRecoveryAudit {
	t.Helper()

	known, scannedRecoveryFiles := panicRecoveryConstructors(t, root)
	if len(known) == 0 {
		t.Fatalf("в дереве не нашлось НИ ОДНОГО звена восстановления паники "+
			"(осмотрено файлов: %d) — распознавание сломано, гейт стал бы "+
			"вечно красным или вечно зелёным в зависимости от порядка проверок",
			scannedRecoveryFiles)
	}

	// Область — все компоненты, поднимающие gRPC-листенеры: семь сервисов и
	// край. Край включён не для полноты счёта: именно у него нашёлся ВТОРОЙ
	// листенер (internal :9091, InternalAuthzCacheService + reflection), у
	// которого звена не было, — а перепись «по семи сервисам» его не видела бы
	// by construction.
	components, err := listenerComponents(root)
	if err != nil {
		t.Fatalf("область обхода гейта сломана: %v", err)
	}

	var (
		findings        []string
		scannedServices int
		scannedPkgs     int
		scannedFiles    int
		listeners       int
		covered         int
		withListeners   []string
		compListeners   = map[string]int{}
		// serviceBuilders — места формы `NewServer`, ОТСЕЯННЫЕ распознаванием как
		// собираемые службы. Печатаются, а не проглатываются: иначе листенер,
		// научившийся отказывать, исчез бы из наблюдения молча.
		serviceBuilders int
		compUsesCarrier = map[string]bool{}
	)
	for _, comp := range components {
		svc := comp.name
		scannedServices++

		cmdRoot := comp.cmdRoot
		binDirs, derr := os.ReadDir(cmdRoot)
		if derr != nil {
			continue // сервис без композиционного корня — предмета нет
		}
		serviceHasListener := false
		for _, b := range binDirs {
			if !b.IsDir() {
				continue
			}
			pkgDir := filepath.Join(cmdRoot, b.Name())
			pkg, perr := loadPkgForChainScan(pkgDir)
			if perr != nil {
				t.Fatalf("%s: пакет не разбирается (%v) — молчание гейта по нему "+
					"ничего не доказывает", relTo(root, pkgDir), perr)
			}
			scannedPkgs++
			scannedFiles += len(pkg.files)
			for _, f := range pkg.files {
				if importLocalNameOf(f, carrierImportPath) != "" {
					compUsesCarrier[svc] = true
				}
			}

			sites, refusing := pkg.listenerSites()
			serviceBuilders += refusing
			if len(sites) == 0 {
				continue
			}
			serviceHasListener = true
			compListeners[svc] += len(sites)
			for _, s := range sites {
				listeners++
				var calls []recoveryKey
				for _, a := range s.args {
					calls = append(calls, pkg.resolveChainCalls(a, "", s.fn, 0)...)
				}
				var missing []string
				for _, kind := range []string{"unary", "stream"} {
					if !hasRecoveryLink(calls, known, kind) {
						missing = append(missing, kind)
					}
				}
				if len(missing) == 0 {
					covered++
					continue
				}
				findings = append(findings, relTo(root, s.file)+":"+strconv.Itoa(s.line)+
					" — листенер "+s.label+" поднят без звена восстановления паники ("+
					strings.Join(missing, "+")+"): паника обработчика или любого звена "+
					"ниже уронит процесс компонента "+svc)
			}
		}
		if serviceHasListener {
			withListeners = append(withListeners, svc)
		}
	}

	carrierless, carrierBorne := componentsWithoutAListenerOrACarrier(components, compListeners, compUsesCarrier)

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	//
	// Ноль ЛИСТЕНЕРОВ сам по себе таким признаком быть перестал: компонент на
	// носителе контура своих не поднимает, и дерево, где переведены все, дало бы
	// здесь честный ноль. Поэтому предметом стало «ноль И ни одного компонента на
	// носителе» — то есть обход не нашёл НИ ОДНОГО поднимающего слушатели, ни
	// прямо, ни через носитель.
	if scannedServices == 0 || scannedFiles == 0 || (listeners == 0 && carrierBorne == 0) {
		t.Fatalf("гейт осмотрел %d компонентов, %d композиционных пакетов, %d файлов, "+
			"нашёл %d листенеров и %d компонентов на носителе — обход ничего не прочитал, "+
			"молчание ничего не доказывает",
			scannedServices, scannedPkgs, scannedFiles, listeners, carrierBorne)
	}
	sort.Strings(withListeners)

	// Предпосылка распознавания, и она ДВУСОСТАВНАЯ с тех пор, как часть
	// компонентов переехала на носитель контура.
	//
	// Прежняя редакция требовала «листенеров не меньше, чем компонентов»,
	// опираясь на то, что каждый поднимает свои два (public :9090 + internal
	// :9091). Компонент на носителе своих не поднимает вовсе — их поднимает
	// `servicehost.Serve`, — поэтому такое требование объявляло бы находкой
	// САМ ПЕРЕВОД и краснело бы ровно по мере его продвижения. Одновременно
	// отбросить требование нельзя: «ноль листенеров» обязано оставаться
	// различимым, иначе сломанное распознавание читалось бы как чистое дерево.
	//
	// Поэтому предпосылка теперь такая: НУЛЕВОЕ число своих листенеров у
	// компонента законно РОВНО тогда, когда он зовёт носитель. Судится именно
	// ноль — он и есть неоднозначный случай: найденный листенер доказывает, что
	// распознавание работает, а ненайденный не доказывает ничего, пока не назван
	// тот, кто поднимает листенеры вместо него.
	if len(carrierless) > 0 {
		sort.Strings(carrierless)
		t.Fatalf("компоненты без своих листенеров и без носителя контура (%d):\n  %s\n"+
			"Либо распознавание листенеров сломано (и молчание гейта ничего не доказывает), "+
			"либо компонент не служит ничего.", len(carrierless), strings.Join(carrierless, "\n  "))
	}

	return panicRecoveryAudit{
		findings:        findings,
		serviceBuilders: serviceBuilders,
		listeners:       listeners,
		covered:         covered,
		summary: "осмотрено: компонентов " + strconv.Itoa(scannedServices) +
			", композиционных пакетов " + strconv.Itoa(scannedPkgs) +
			", файлов " + strconv.Itoa(scannedFiles) +
			", файлов при поиске звеньев " + strconv.Itoa(scannedRecoveryFiles) +
			"; распознано звеньев восстановления паники " + strconv.Itoa(len(known)) +
			"; листенеров " + strconv.Itoa(listeners) +
			", из них со звеном " + strconv.Itoa(covered) +
			"; отсеяно как собираемые службы (конструктор умеет отказать) " + strconv.Itoa(serviceBuilders) +
			"; компонентов на носителе контура (своих листенеров нет) " + strconv.Itoa(carrierBorne) +
			"; листенеры у: " + strings.Join(withListeners, ", "),
	}
}

// listenerComponent — компонент, поднимающий gRPC-листенеры, и каталог его
// композиционных корней.
type listenerComponent struct {
	name    string
	cmdRoot string
}

// listenerComponents — область обхода, ВЫВЕДЕННАЯ из дерева, а не выписанная:
// каждый каталог services/<svc>/cmd плюс gateway/cmd. Восьмой сервис попадает
// под гейт в момент появления, без правки этого файла.
func listenerComponents(root string) ([]listenerComponent, error) {
	var out []listenerComponent
	svcRoot := filepath.Join(root, "services")
	entries, err := os.ReadDir(svcRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, listenerComponent{
			name:    e.Name(),
			cmdRoot: filepath.Join(svcRoot, e.Name(), "cmd"),
		})
	}
	gwCmd := filepath.Join(root, "gateway", "cmd")
	if fi, serr := os.Stat(gwCmd); serr == nil && fi.IsDir() {
		out = append(out, listenerComponent{name: "gateway", cmdRoot: gwCmd})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// ── распознавание звена по существу ──────────────────────────────────────────

// recoveryKey — адрес функции-звена: каталог её пакета + имя. Имя одно на
// пакет, поэтому пары достаточно; сравнение идёт по этому ключу, а не по тексту.
type recoveryKey struct {
	dir  string
	name string
}

// panicRecoveryConstructors переписывает дерево и возвращает функции, которые
// ЯВЛЯЮТСЯ звеном восстановления паники: возвращают grpc-интерсептор и зовут
// recover(). Возвращает также число прочитанных файлов — чтобы «ничего не
// нашёл» было отличимо от «ничего не читал».
func panicRecoveryConstructors(t *testing.T, root string) (map[recoveryKey]string, int) {
	t.Helper()
	known := map[recoveryKey]string{}
	files := 0
	for _, r := range panicRecoveryScanRoots {
		walkGoASTFilesForPanicGate(t, filepath.Join(root, r), func(path string, f *ast.File, fset *token.FileSet) {
			files++
			grpcName := importLocalNameOf(f, grpcImportPath)
			if grpcName == "" {
				return
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Type.Results == nil {
					continue
				}
				kind := interceptorResultKind(fn.Type.Results, grpcName)
				if kind == "" {
					continue
				}
				if !bodyCallsRecover(fn.Body) {
					continue
				}
				known[recoveryKey{dir: filepath.Dir(path), name: fn.Name.Name}] = kind
			}
		})
	}
	return known, files
}

// interceptorResultKind — возвращает вид интерсептора, если функция отдаёт
// именно его (единственным результатом), иначе "".
func interceptorResultKind(res *ast.FieldList, grpcName string) string {
	if res.NumFields() != 1 {
		return ""
	}
	sel, ok := res.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != grpcName {
		return ""
	}
	for kind, typ := range interceptorTypeOfKind {
		if sel.Sel.Name == typ {
			return kind
		}
	}
	return ""
}

// bodyCallsRecover — есть ли в теле вызов встроенного recover(). Именно вызов:
// упоминание в комментарии или строковом литерале узлом CallExpr не является.
func bodyCallsRecover(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "recover" && len(call.Args) == 0 {
			found = true
			return false
		}
		return true
	})
	return found
}

// hasRecoveryLink — попал ли в цепочку вызов признанного звена нужного вида.
func hasRecoveryLink(calls []recoveryKey, known map[recoveryKey]string, kind string) bool {
	for _, c := range calls {
		if k, ok := known[c]; ok && k == kind {
			return true
		}
	}
	return false
}

// ── разбор композиционного пакета ────────────────────────────────────────────

type chainPkgInfo struct {
	dir     string
	fset    *token.FileSet
	files   []*ast.File
	paths   []string
	funcs   map[string]*ast.FuncDecl // функции этого пакета по имени
	fileOf  map[*ast.FuncDecl]*ast.File
	grpcOf  map[*ast.File]string // локальное имя пакета grpc в файле
	imports map[*ast.File]map[string]string
}

type listenerChainSite struct {
	file  string
	line  int
	label string
	args  []ast.Expr
	fn    *ast.FuncDecl
}

func loadPkgForChainScan(dir string) (*chainPkgInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	p := &chainPkgInfo{
		dir: dir, fset: fset,
		funcs:   map[string]*ast.FuncDecl{},
		fileOf:  map[*ast.FuncDecl]*ast.File{},
		grpcOf:  map[*ast.File]string{},
		imports: map[*ast.File]map[string]string{},
	}
	for _, pkg := range pkgs {
		names := make([]string, 0, len(pkg.Files))
		for name := range pkg.Files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			f := pkg.Files[name]
			p.files = append(p.files, f)
			p.paths = append(p.paths, name)
			p.grpcOf[f] = importLocalNameOf(f, grpcImportPath)
			p.imports[f] = importMapOfFile(f)
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
					p.funcs[fn.Name.Name] = fn
					p.fileOf[fn] = f
				}
			}
		}
	}
	return p, nil
}

// listenerSites — все места, где поднимается gRPC-листенер: вызовы NewServer
// (grpc.NewServer, grpcsrv.NewServer, proxy.NewServer — их объединяет предмет,
// а не пакет). Опции цепочек, из скольких бы мест они ни пришли, сходятся
// именно здесь, поэтому здесь и проверяются.
func (p *chainPkgInfo) listenerSites() ([]listenerChainSite, int) {
	var out []listenerChainSite
	var refusing int
	for i, f := range p.files {
		path := p.paths[i]
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// Вызовы, чей результат разбирается ПАРОЙ, собраны заранее: у
			// `ast.Inspect` нет родителя узла, а решение принимается именно по
			// нему.
			pair := serviceBuilderCalls(fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != serverConstructorName {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				// Только пакетный вызов: метод `x.NewServer()` на значении —
				// другой предмет.
				if _, isPkg := p.imports[f][pkgIdent.Name]; !isPkg {
					return true
				}
				// СОВПАДЕНИЕ ФОРМЫ, а не листенер: конструктор, УМЕЮЩИЙ
				// ОТКАЗАТЬ, собирает службу, а не поднимает слушателя.
				//
				// Листенер в этом дереве поднимается тотально — ему нечем
				// отказать, и все четыре его места разбирают результат одним
				// значением. Конструктор, отдающий пару с ошибкой, судит
				// объявление и вправе его отвергнуть; собранное им РЕГИСТРИРУЮТ
				// НА уже поднятом слушателе, чья цепочка звеньев здесь и
				// проверяется. Считать такое место листенером значит требовать
				// звена от того, у кого цепочки нет вовсе.
				//
				// Список исключений тут не заводится намеренно (см. исход 2 в
				// шапке гейта): уточняется РАСПОЗНАВАНИЕ. Цена уточнения названа
				// и наблюдаема — пропущенные места печатает перепись, поэтому
				// листенер, научившийся отказывать, не исчезнет из наблюдения
				// молча: он появится в её счёте, а число листенеров не вырастет.
				if pair[call] {
					refusing++
					return true
				}
				out = append(out, listenerChainSite{
					file:  path,
					line:  p.fset.Position(call.Pos()).Line,
					label: pkgIdent.Name + "." + sel.Sel.Name + "(…)",
					args:  call.Args,
					fn:    fn,
				})
				return true
			})
		}
	}
	return out, refusing
}

// serviceBuilderCalls — вызовы формы `NewServer`, которые СЛУШАТЕЛЯ НЕ ПОДНИМАЮТ.
//
// Признаков ДВА, и оба обязательны — вместе, а не по отдельности:
//
//  1. результат разбирается ПАРОЙ (`v, err := f()`): конструктор, умеющий
//     отказать, судит объявление, а листенер в этом дереве поднимается тотально;
//  2. полученное значение НЕ ИСПОЛЬЗУЕТСЯ КАК СЛУШАТЕЛЬ: на нём не служат
//     (`.Serve`, `.Stop`, `.GracefulStop`, `.GetServiceInfo`) и на нём ничего не
//     регистрируют (`Register…(v, …)` первым аргументом).
//
// # Почему одного первого признака НЕДОСТАТОЧНО
//
// Он про ФОРМУ ПРИСВАИВАНИЯ, а не про предмет. Научись общий конструктор
// слушателя отдавать пару со ошибкой — и настоящие листенеры отсеялись бы МОЛЧА,
// а гейт остался бы зелёным ровно там, где обязан краснеть. Второй признак это
// закрывает: слушатель узнаётся по тому, ЧТО С НИМ ДЕЛАЮТ, и остаётся под
// наблюдением независимо от того, как объявлен его конструктор.
//
// Граница названа: слушатель, собранный здесь и переданный регистрировать в
// ЧУЖУЮ функцию, обоими признаками не опознаётся. Это тот же предел, что у гейта
// был и до уточнения (разрешение идёт по телу одной функции), и он не расширен.
func serviceBuilderCalls(body *ast.BlockStmt) map[*ast.CallExpr]bool {
	paired := map[*ast.CallExpr]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) < 2 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			paired[call] = id.Name
		}
		return true
	})
	if len(paired) == 0 {
		return map[*ast.CallExpr]bool{}
	}

	servedNames := servedAsListener(body)
	out := make(map[*ast.CallExpr]bool, len(paired))
	for call, name := range paired {
		if !servedNames[name] {
			out[call] = true
		}
	}
	return out
}

// servedAsListener — имена значений, с которыми обращаются КАК СО СЛУШАТЕЛЕМ.
func servedAsListener(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// `v.Serve(…)` и родня: на слушателе служат и его останавливают.
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			switch sel.Sel.Name {
			case "Serve", "Stop", "GracefulStop", "GetServiceInfo":
				if id, ok := sel.X.(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		}
		// `RegisterXServer(v, impl)`: на слушателе РЕГИСТРИРУЮТ, и он стоит
		// первым аргументом. Второй и далее — реализации, и они слушателями не
		// являются: ровно так регистрируется общий сервер потока.
		if len(call.Args) == 0 {
			return true
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		}
		if !strings.HasPrefix(name, "Register") {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// resolveChainCalls — какие функции реально участвуют в сборке этой цепочки.
//
// Разрешение идёт по потоку значения, а не по тексту файла: имя переменной
// раскрывается её присваиваниями В ТОЙ ЖЕ функции (иначе одноимённая
// переменная соседней функции дала бы ложно-зелёный), вызов внутрипакетного
// строителя — его аргументами И тем, что он возвращает в СООТВЕТСТВУЮЩЕЙ
// позиции результата. Позиция важна: nlb собирает все четыре цепочки одним
// строителем, и «звено есть где-то в теле» не означало бы «есть в ЭТОЙ цепочке».
// want — имя переменной, ради которой мы раскрываем выражение. Оно необходимо
// для многозначного присваивания: у `a, b, c, d := build(...)` тело строителя
// одно, а цепочек четыре, и «звено есть где-то в теле» НЕ означает «есть в
// ЭТОЙ». Первая редакция гейта теряла имя и всегда читала позицию 0 — из-за
// чего объявила находкой stream-цепочки, куда звено было провязано.
func (p *chainPkgInfo) resolveChainCalls(expr ast.Expr, want string, in *ast.FuncDecl, depth int) []recoveryKey {
	if depth > 12 || expr == nil || in == nil {
		return nil
	}
	var out []recoveryKey
	switch e := expr.(type) {
	case *ast.Ident:
		for _, a := range assignmentsToName(in, e.Name) {
			out = append(out, p.resolveChainCalls(a.expr, e.Name, in, depth+1)...)
		}
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "append" {
			for _, a := range e.Args {
				out = append(out, p.resolveChainCalls(a, want, in, depth+1)...)
			}
			return out
		}
		if key, ok := p.calleeKey(e, in); ok {
			out = append(out, key)
			if fn, isLocal := p.funcs[key.name]; isLocal && key.dir == p.dir {
				out = append(out, p.resolveReturn(fn, p.resultIndexFor(e, in, want), depth+1)...)
			}
		}
		for _, a := range e.Args {
			out = append(out, p.resolveChainCalls(a, "", in, depth+1)...)
		}
	case *ast.CompositeLit:
		for _, el := range e.Elts {
			out = append(out, p.resolveChainCalls(el, "", in, depth+1)...)
		}
	case *ast.SliceExpr:
		out = append(out, p.resolveChainCalls(e.X, want, in, depth+1)...)
	case *ast.ParenExpr:
		out = append(out, p.resolveChainCalls(e.X, want, in, depth+1)...)
	}
	return out
}

// resultIndexFor — в какой позиции результата вызова стоит переменная want.
func (p *chainPkgInfo) resultIndexFor(call *ast.CallExpr, in *ast.FuncDecl, want string) int {
	if want == "" || in.Body == nil {
		return 0
	}
	idx := 0
	ast.Inspect(in.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || as.Rhs[0] != call {
			return true
		}
		for i, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == want {
				idx = i
				return false
			}
		}
		return true
	})
	return idx
}

// resolveReturn — что строитель отдаёт в позиции idx.
func (p *chainPkgInfo) resolveReturn(fn *ast.FuncDecl, idx, depth int) []recoveryKey {
	if fn.Body == nil || depth > 12 {
		return nil
	}
	var out []recoveryKey
	// Именованные результаты: возврат бывает и голым (`return`), поэтому имя
	// результата раскрывается отдельно от выражений return.
	if fn.Type.Results != nil {
		if names := resultNamesOf(fn.Type.Results); idx < len(names) && names[idx] != "" {
			for _, a := range assignmentsToName(fn, names[idx]) {
				out = append(out, p.resolveChainCalls(a.expr, names[idx], fn, depth+1)...)
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || idx >= len(ret.Results) {
			return true
		}
		out = append(out, p.resolveChainCalls(ret.Results[idx], "", fn, depth+1)...)
		return true
	})
	return out
}

// calleeKey — адрес вызываемой функции: свой пакет или импортированный.
func (p *chainPkgInfo) calleeKey(call *ast.CallExpr, in *ast.FuncDecl) (recoveryKey, bool) {
	file := p.fileOf[in]
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if _, ok := p.funcs[fun.Name]; ok {
			return recoveryKey{dir: p.dir, name: fun.Name}, true
		}
		return recoveryKey{}, false
	case *ast.SelectorExpr:
		x, ok := fun.X.(*ast.Ident)
		if !ok || file == nil {
			return recoveryKey{}, false
		}
		path, ok := p.imports[file][x.Name]
		if !ok {
			return recoveryKey{}, false
		}
		dir, ok := dirOfModuleImport(p.dir, path)
		if !ok {
			return recoveryKey{}, false
		}
		return recoveryKey{dir: dir, name: fun.Sel.Name}, true
	}
	return recoveryKey{}, false
}

// ── мелкие помощники разбора ─────────────────────────────────────────────────

type assignedExpr struct{ expr ast.Expr }

// assignmentsToName — все выражения, присвоенные имени в теле функции, включая
// `x = append(x, …)`. Область — ТОЛЬКО эта функция.
func assignmentsToName(fn *ast.FuncDecl, name string) []assignedExpr {
	if fn.Body == nil {
		return nil
	}
	var out []assignedExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name {
				continue
			}
			switch {
			case len(as.Rhs) == len(as.Lhs):
				out = append(out, assignedExpr{expr: as.Rhs[i]})
			case len(as.Rhs) == 1:
				// многозначное присваивание из одного вызова: позиция i важна,
				// её раскрывает resolveChainCalls через resultIndexOfCall.
				out = append(out, assignedExpr{expr: as.Rhs[0]})
			}
		}
		return true
	})
	return out
}

func resultNamesOf(res *ast.FieldList) []string {
	var out []string
	for _, f := range res.List {
		if len(f.Names) == 0 {
			out = append(out, "")
			continue
		}
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

func importLocalNameOf(f *ast.File, path string) string {
	for name, p := range importMapOfFile(f) {
		if p == path {
			return name
		}
	}
	return ""
}

func importMapOfFile(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = p
	}
	return out
}

// dirOfModuleImport — каталог пакета внутри этого модуля; чужие модули не наши.
func dirOfModuleImport(fromDir, importPath string) (string, bool) {
	const mod = "github.com/PRO-Robotech/kacho/"
	if !strings.HasPrefix(importPath, mod) {
		return "", false
	}
	root := fromDir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", false
		}
		root = parent
	}
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(importPath, mod))), true
}

// walkGoASTFilesForPanicGate обходит прод-исходники под root и отдаёт каждый
// разобранным.
//
// Состав берётся у индекса отслеживаемых файлов, а НЕ обходом диска: под
// `.claude/worktrees/` лежат рабочие копии всего дерева, которые git не
// отслеживает, а `filepath.WalkDir` видит. Гейт, считающий по диску, померил бы
// листенеры копий вместе с настоящими, и его число перестало бы что-либо
// означать. То же требование энфорсит `TestTreeWalkersAskTheIndex`, чей перечень
// исключений закрыт для пополнения, — первая редакция этого гейта его роняла.
func walkGoASTFilesForPanicGate(t *testing.T, root string, fn func(string, *ast.File, *token.FileSet)) {
	t.Helper()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		// Каталога нет — обходить нечего. Прежняя редакция проглатывала это
		// через os.IsNotExist от самого обхода; здесь проверяется явно, потому
		// что на несуществующий путь индекс отвечает не тем же.
		return
	}
	tracked, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева под %s взять неоткуда: %v", root, err)
	}
	fset := token.NewFileSet()
	for _, path := range tracked {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			continue
		}
		fn(path, f, fset)
	}
}

func exprLabelOf(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok {
			return id.Name + "(…)"
		}
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			return sel.Sel.Name + "(…)"
		}
	}
	return "цепочка"
}

func relTo(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}

// componentsWithoutAListenerOrACarrier — ПРЕДПОСЫЛКА распознавания, вынесенная
// отдельной функцией, чтобы пробы инъекции гоняли ТО ЖЕ САМОЕ, что гоняется по
// дереву: проба, повторяющая предикат своей копией, доказывала бы свойство
// копии.
//
// Возвращает компоненты, у которых нет ни своих листенеров, ни носителя, и
// счётчик тех, чьи листенеры поднимает носитель. Второе значение нужно переписи:
// «ноль находок» обязано быть отличимо от «ноль прочитанного», а компонент на
// носителе иначе не виден в отчёте вовсе.
func componentsWithoutAListenerOrACarrier(
	components []listenerComponent,
	listenersOf map[string]int,
	usesCarrier map[string]bool,
) (carrierless []string, carrierBorne int) {
	for _, comp := range components {
		if listenersOf[comp.name] > 0 {
			continue
		}
		if usesCarrier[comp.name] {
			carrierBorne++
			continue
		}
		carrierless = append(carrierless, fmt.Sprintf("%s: своих листенеров нет и носитель не позван",
			comp.name))
	}
	sort.Strings(carrierless)
	return carrierless, carrierBorne
}
