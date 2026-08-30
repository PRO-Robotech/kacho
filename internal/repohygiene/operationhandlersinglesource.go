// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// operationStubsPath — путь стабов контракта операции.
	//
	// Алиас импорта у него в дереве РАЗНЫЙ, поэтому распознаватель обязан
	// резолвить его пофайлово, а не знать один литерал. Числа здесь НЕ
	// приводятся: перепись зависит от популяции (гейт видит своё подмножество и
	// печатает его в выводе каждым прогоном), а выписанное число устаревает
	// молча — прежняя редакция несла величину, снятую ДО этого же коммита, в
	// трёх местах сразу.
	operationStubsPath = "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	// sharedOperationPbPath — общий слой, к которому ведёт законная прослойка.
	sharedOperationPbPath = "github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	// operationsPkgPath — владелец строки операции; полоса владения выражается
	// его глаголами.
	operationsPkgPath = "github.com/PRO-Robotech/kacho/pkg/operations"
	// sharedLayerDir — каталог общего слоя относительно корня.
	sharedLayerDir = "pkg/operations/operationspb"
)

// opSourceFinding — одно место, где предмет объявлен вне общего слоя.
type opSourceFinding struct {
	Where string
	What  string
	Why   string
}

// opSourceCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного», а по осям — порознь, потому что
// одно суммарное число скрыло бы ровно тот случай, ради которого ось заведена.
//
// Счётчики НЕ включают исключённые пути: иначе тревога «распознаватель ослеп»
// глушится ровно тем местом, которое из суждения выведено. Так и было — ось
// обработчика показывала три попадания, все из края, и на них держалось
// молчание стража предпосылки при ослепшей оси преобразователя.
type opSourceCensus struct {
	FilesRead  int
	Handlers   int
	Converters int
	Ownership  int
	// Proxies — объявлений ПРОКСИ контракта: тип реализует серверную сторону и
	// держит КЛИЕНТ того же контракта. Печатается отдельно, потому что от этого
	// числа зависит смысл нуля по оси обработчика: прокси выведен из неё ПО
	// ПОСТРОЕНИЮ, и «обработчиков 0» при нулевом числе прокси означает совсем не
	// то же, что при ненулевом.
	Proxies int
	// RecordedOwnership — функций, читающих владельца С САМОЙ СТРОКИ ответа.
	// Это ВТОРАЯ полоса владения, и до её заведения гейт не видел её вовсе:
	// ось `Ownership` считает глаголы владельца строки, а эта полоса их не
	// зовёт by construction — она получает владельца уже прочитанным.
	RecordedOwnership int
	Exempted          int            // подавлено записями ведомости — отдельно от осей
	Aliases           map[string]int // какие алиасы стабов встретились — перепись слепых зон
}

// auditOperationSingleSource — судящая функция гейта.
//
// Судит ПРЕДМЕТ по разобранному дереву, а не имена по образцу. Три оси:
//
//   - обработчик: метод, принимающий запрос контракта, либо тип, вкладывающий
//     его серверную заглушку. Имя конструктора не судится вовсе — прежняя
//     редакция искала `func NewOperationHandler(`, и форк, назвавший его иначе,
//     проезжал (доказано инъекцией: `NewOpsHandler` + алиас `oppb`);
//   - преобразователь: функция, ВОЗВРАЩАЮЩАЯ `*<алиас>.Operation`. Имя не
//     судится: прежняя редакция искала `[Oo]perationToProto`, и `toProtoOperation`
//     со своим телом проезжал. Законна ровно одна форма тела — возврат вызова
//     общего слоя; возврат в СВОЮ функцию с тем же суффиксом имени прежде
//     принимался за прослойку;
//   - полоса владения: глаголы `pkg/operations`, собирающие ключ владения.
//
// exempt — пути, которым предмет разрешён, с причиной. Запись, которой нечего
// исключать, — находка: послабление обязано истекать само.
func auditOperationSingleSource(root string, files []string, exempt map[string]string) ([]opSourceFinding, opSourceCensus, error) {
	cen := opSourceCensus{Aliases: map[string]int{}}
	var findings []opSourceFinding
	seenExempt := map[string]bool{}

	// ПРОХОД ПЕРВЫЙ: псевдонимы типов собираются по ПАКЕТУ, а не по файлу.
	//
	// Псевдоним, объявленный в соседнем файле того же пакета, виден всему пакету —
	// это законный Go и в эффекте неотличимо от объявления рядом. Пофайловая
	// таблица давала полный обход: файл-потребитель стабов не импортирует, значит
	// `stubs == ""`, и ВСЕ ТРИ оси пропускали его молча. Форк при этом вкладывал
	// заглушку, реализовывал оба глагола и нёс своё тело преобразователя.
	pkgAliases := map[string]map[string]string{} // каталог → местное имя → имя типа контракта
	// pkgProxyTypes — каталог → имена типов-ПРОКСИ контракта. Собирается первым
	// проходом по той же причине, что и псевдонимы: тип объявлен в одном файле
	// пакета, а его методы лежат в другом, и пофайловая таблица разошлась бы с
	// языком.
	pkgProxyTypes := map[string]map[string]bool{}
	parsed := map[string]*ast.File{}
	fsets := map[string]*token.FileSet{}
	for _, rel := range files {
		slashed := filepath.ToSlash(rel)
		if strings.HasPrefix(slashed, sharedLayerDir+"/") || strings.HasPrefix(slashed, "pkg/api/") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		parsed[slashed], fsets[slashed] = f, fset
		stubs, _, opsPkg := importAliases(f)
		dir := filepath.ToSlash(filepath.Dir(slashed))
		if pkgAliases[dir] == nil {
			pkgAliases[dir] = map[string]string{}
		}
		// Псевдонимы обоих пакетов: и контракта, и ДОМЕННОГО владельца строки.
		// `type domOp = operations.Operation` прятал приём доменной строки так же,
		// как `type opPB = operationpb.Operation` прятал возврат.
		for k, v := range typeAliases(f, stubs) {
			pkgAliases[dir][k] = v
		}
		for k, v := range typeAliases(f, opsPkg) {
			pkgAliases[dir][k] = v
		}
		// Псевдоним псевдонима: `type wire = opPB` при `type opPB = pb.Operation`.
		// Собирается тем же проходом как «местное имя → местное имя», а
		// разрешается ниже до неподвижной точки: одна лишняя строка снимала обе
		// оси разом.
		for k, v := range localTypeAliases(f) {
			if _, known := pkgAliases[dir][k]; !known {
				pkgAliases[dir][k] = "=" + v // помечаем как неразрешённую ссылку
			}
		}
		if stubs == "." {
			for _, n := range []string{"GetOperationRequest", "CancelOperationRequest", "UnimplementedOperationServiceServer", "Operation"} {
				pkgAliases[dir][n] = n
			}
		}
	}

	// Разрешение цепочек псевдонимов до неподвижной точки: `wire → opPB →
	// Operation`. Без него достаточно одного лишнего звена, чтобы снять обе оси.
	for _, table := range pkgAliases {
		for range table { // итераций не больше длины таблицы: длиннее цепочки не бывает
			changed := false
			for k, v := range table {
				if !strings.HasPrefix(v, "=") {
					continue
				}
				if target, okT := table[strings.TrimPrefix(v, "=")]; okT && !strings.HasPrefix(target, "=") {
					table[k] = target
					changed = true
				}
			}
			if !changed {
				break
			}
		}
		for k, v := range table { // неразрешённые ссылки предметом не являются
			if strings.HasPrefix(v, "=") {
				delete(table, k)
			}
		}
	}

	// ПРОХОД ПОЛУТОРНЫЙ: типы-ПРОКСИ контракта.
	//
	// Считается ПОСЛЕ разрешения псевдонимов — иначе прокси, спрятавший заглушку
	// за местным именем, в таблицу не попадёт и будет судим осью обработчика,
	// то есть ровно тем судом, из-под которого прокси выведен по построению.
	for _, rel := range files {
		slashed := filepath.ToSlash(rel)
		f := parsed[slashed]
		if f == nil {
			continue
		}
		stubs, _, _ := importAliases(f)
		dir := filepath.ToSlash(filepath.Dir(slashed))
		for _, name := range contractProxyTypes(f, stubs, pkgAliases[dir]) {
			if pkgProxyTypes[dir] == nil {
				pkgProxyTypes[dir] = map[string]bool{}
			}
			pkgProxyTypes[dir][name] = true
		}
	}
	for _, rel := range files {
		slashed := filepath.ToSlash(rel)
		// Общий слой — ЦЕЛЬ свойства, а не его нарушитель, и выведен он по
		// построению, а не записью ведомости: запись подразумевает, что предмет
		// когда-нибудь исчезнет, а этот — определение единственного источника.
		if strings.HasPrefix(slashed, sharedLayerDir+"/") {
			continue
		}
		// Сгенерённые стабы руками не правят (`polyrepo.md`: «РУКАМИ НЕ ПРАВИТЬ»),
		// и предметом гейта они быть не могут: судить их значило бы требовать
		// свойства от вывода генератора.
		if strings.HasPrefix(slashed, "pkg/api/") {
			continue
		}
		f := parsed[slashed]
		if f == nil {
			continue // выведен по построению первым проходом
		}
		cen.FilesRead++

		stubs, shared, ops := importAliases(f)
		if stubs != "" {
			cen.Aliases[stubs]++
		}

		exemptFor, exemptPrefix := "", ""
		for prefix, why := range exempt {
			// Граница пути обязательна: без разделителя запись про
			// `gateway/internal/opsproxy` прощала и соседний
			// `gateway/internal/opsproxyv2/` — то есть послабление расползалось
			// на каталоги, о которых его автор не решал.
			if strings.HasPrefix(filepath.ToSlash(rel), strings.TrimSuffix(prefix, "/")+"/") {
				exemptFor, exemptPrefix = why, prefix
				break
			}
		}

		// Отметка «запись сработала» ставится в момент ПОДАВЛЕНИЯ находки, а не
		// при совпадении пути. Различие несущее: по пути запись переживала
		// исчезновение своего предмета, пока в каталоге лежал хоть один файл, —
		// то есть послабление НЕ истекало, хотя гейт это обещал.
		add := func(what, why string) {
			if exemptFor != "" {
				seenExempt[exemptPrefix] = true
				cen.Exempted++
				return
			}
			findings = append(findings, opSourceFinding{Where: rel, What: what, Why: why})
		}
		// Ось засчитывает только НЕисключённое: см. godoc opSourceCensus.
		count := func(axis *int) {
			if exemptFor == "" {
				*axis++
			}
		}

		// Псевдонимы типов контракта, объявленные ЭТИМ файлом: `type getReq =
		// operationpb.GetOperationRequest`. Без них форк, спрятавший контракт за
		// собственным именем типа, компилируется, регистрируется сервером
		// операций и остаётся для гейта обычным файлом — перепись совпадает с
		// чистым деревом побайтово, то есть сигнала нет никакого.
		// Таблица — ПАКЕТНАЯ (собрана первым проходом): точечный импорт и
		// псевдонимы соседнего файла сводятся к одной форме.
		aliases := pkgAliases[filepath.ToSlash(filepath.Dir(slashed))]

		proxies := pkgProxyTypes[filepath.ToSlash(filepath.Dir(slashed))]

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, name := range serverStubTypeNames(d, stubs, aliases) {
					if proxies[name] {
						// ПРОКСИ, а не форк обработчика: он реализует серверную
						// сторону, потому что иначе его нечем зарегистрировать на
						// мультиплексоре, и держит КЛИЕНТ того же контракта,
						// потому что читает у владельца. Своей таблицы операций
						// у него нет — сводить его к общему обработчику не к чему.
						// Выведен ПО ПОСТРОЕНИЮ, а не записью ведомости: запись
						// подразумевает, что предмет когда-нибудь исчезнет, а этот
						// не исчезнет, пока у платформы один вход. Взамен прокси
						// судится осью прочитанного владельца — см. ниже.
						cen.Proxies++
						continue
					}
					count(&cen.Handlers)
					add("тип вкладывает серверную заглушку контракта", "обработчик")
				}
			case *ast.FuncDecl:
				if (stubs != "" || len(aliases) > 0) && takesOperationRequest(d, stubs, aliases) {
					// Имя получателя берётся УЖЕ СУЩЕСТВУЮЩИМ в пакете
					// помощником (`replicafanout.go`), а не своим: он читает все
					// четыре формы получателя, включая обобщённые, и заводить
					// рядом вторую копию значило бы делать ровно то, что этот
					// гейт запрещает.
					if d.Recv != nil && len(d.Recv.List) > 0 && proxies[receiverName(d.Recv.List[0].Type)] {
						cen.Proxies++
					} else {
						count(&cen.Handlers)
						add("метод принимает запрос контракта операции", "обработчик")
					}
				}
				// ОСЬ ПРОЧИТАННОГО ВЛАДЕЛЬЦА. Полоса, получающая владельца уже
				// прочитанным (из ответа соседа), — вторая в дереве, и глаголов
				// владельца строки она не зовёт, поэтому ось `Ownership` её не
				// видит by construction.
				if stubs != "" && readsRecordedOwner(d) {
					count(&cen.RecordedOwnership)
					if !delegatesOwnershipDecision(d, ops) {
						add("читает владельца с самой строки и решает по нему сама", "полоса прочитанного владельца")
					}
				}
				if (stubs != "" || len(aliases) > 0) && ops != "" && convertsOperation(d, stubs, ops, aliases) {
					count(&cen.Converters)
					if !delegatesToShared(d, shared) {
						add("функция возвращает `*"+stubs+".Operation` со своим телом", "преобразователь")
					}
				}
				if ops != "" && collectsOwnerKey(d, ops) {
					count(&cen.Ownership)
					add("собирает ключ владения операцией", "полоса владения")
				}
			}
		}
	}

	// Самоистечение: запись, которой нечего исключать, наследует следующую
	// слепую зону.
	for prefix := range exempt {
		if !seenExempt[prefix] {
			findings = append(findings, opSourceFinding{
				Where: prefix,
				What:  "исключению нечего исключать — предмет исчез, а запись осталась",
				Why:   "истёкшее послабление",
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Where == findings[j].Where {
			return findings[i].What < findings[j].What
		}
		return findings[i].Where < findings[j].Where
	})
	return findings, cen, nil
}

// importAliases — фактические алиасы трёх путей в ЭТОМ файле.
func importAliases(f *ast.File) (stubs, shared, ops string) {
	for _, im := range f.Imports {
		path, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if im.Name != nil {
			name = im.Name.Name
		}
		switch path {
		case operationStubsPath:
			if name == "." {
				stubs = "."
				continue
			}
			if name == "" {
				// Имя ПАКЕТА, а не последнего сегмента пути: `package operationv1`.
				// Прежде здесь стояло `operation`, и первый же безалиасный импорт
				// стал бы слепой зоной — гейт искал бы `*operation.Operation` там,
				// где написано `*operationv1.Operation`, и промолчал.
				name = "operationv1"
			}
			stubs = name
		case sharedOperationPbPath:
			if name == "" {
				name = "operationspb"
			}
			shared = name
		case operationsPkgPath:
			if name == "" {
				name = "operations"
			}
			ops = name
		}
	}
	return stubs, shared, ops
}

// isPtrTo — выражение типа есть `*<pkg>.<name>`.
func isPtrTo(e ast.Expr, pkg, name string) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	return isSel(star.X, pkg, name)
}

func isSel(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// takesOperationRequest — метод принимает запрос контракта. Имена параметров не
// судятся: `ctx`/`req` — привычка, а не контракт (инъекция с `c`/`in` прежнюю
// редакцию проезжала).
func takesOperationRequest(d *ast.FuncDecl, stubs string, aliases map[string]string) bool {
	if d.Recv == nil || d.Type.Params == nil {
		return false
	}
	for _, p := range d.Type.Params.List {
		for _, req := range []string{"GetOperationRequest", "CancelOperationRequest"} {
			if isPtrTo(p.Type, stubs, req) || isPtrToAlias(p.Type, aliases, req) {
				return true
			}
		}
	}
	return false
}

// typeAliases — псевдонимы типов контракта, объявленные файлом:
// `type getReq = <stubs>.GetOperationRequest`. Ключ — местное имя, значение —
// имя типа контракта.
func typeAliases(f *ast.File, stubs string) map[string]string {
	out := map[string]string{}
	if stubs == "" {
		return out
	}
	for _, decl := range f.Decls {
		gd, okDecl := decl.(*ast.GenDecl)
		if !okDecl || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, okSpec := spec.(*ast.TypeSpec)
			if !okSpec || !ts.Assign.IsValid() {
				continue // не псевдоним, а свой тип
			}
			sel, okSel := ts.Type.(*ast.SelectorExpr)
			if !okSel {
				continue
			}
			id, okID := sel.X.(*ast.Ident)
			if okID && id.Name == stubs {
				out[ts.Name.Name] = sel.Sel.Name
			}
		}
	}
	return out
}

// isPtrToAlias — выражение типа есть `*<местное имя>`, где местное имя —
// псевдоним искомого типа контракта.
func isPtrToAlias(e ast.Expr, aliases map[string]string, want string) bool {
	star, okStar := e.(*ast.StarExpr)
	if !okStar {
		return false
	}
	id, okID := star.X.(*ast.Ident)
	return okID && aliases[id.Name] == want
}

// serverStubTypeNames — ИМЕНА типов, вкладывающих
// `<stubs>.UnimplementedOperationServiceServer`.
//
// Возвращает имена, а не «да/нет»: по имени вызывающий отличает форк обработчика
// от ПРОКСИ, а без имени различить их нечем — оба вкладывают одну заглушку.
func serverStubTypeNames(d *ast.GenDecl, stubs string, aliases map[string]string) []string {
	if stubs == "" && len(aliases) == 0 {
		return nil
	}
	var names []string
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			continue
		}
		for _, fld := range st.Fields.List {
			if len(fld.Names) != 0 {
				continue // вкладывание — поле БЕЗ имени
			}
			if isSel(fld.Type, stubs, "UnimplementedOperationServiceServer") {
				names = append(names, ts.Name.Name)
				break
			}
			if id, okID := fld.Type.(*ast.Ident); okID && aliases[id.Name] == "UnimplementedOperationServiceServer" {
				names = append(names, ts.Name.Name)
				break
			}
		}
	}
	return names
}

// contractProxyTypes — имена типов, являющихся ПРОКСИ контракта операции.
//
// # Дискриминатор — СТРУКТУРНЫЙ, и это несущее свойство
//
// Прокси узнаётся по паре, которую нельзя получить случайно: тип вкладывает
// СЕРВЕРНУЮ заглушку контракта и держит полем КЛИЕНТ ТОГО ЖЕ контракта. Первое
// без второго — обработчик поверх своего хранилища (домены), второе без первого
// — обычный клиент соседа (пакеты SDK, клиенты сервисов). Вместе они означают
// ровно одно: точка, принимающая запрос и переспрашивающая его у владельца.
//
// Дискриминатор НЕ по пути и НЕ по имени намеренно. Прежде это место держала
// запись ведомости на каталог края, и держала неверно дважды: она подавляла ось
// ОБРАБОТЧИКА (форк тенантского обработчика), тогда как называла своей причиной
// расхождение полосы ВЛАДЕНИЯ — оси, которой у гейта не было вовсе. То есть
// послабление выдавалось не за то, что происходило, и предмета, который она
// обещала стеречь, не касалось ничем.
//
// # Почему ПОЛЕ, а не «пакет где-то держит клиент»
//
// Пакетная проверка дала бы выход: домен, добавивший рядом клиент операции,
// выводил бы СВОЙ форк обработчика из-под оси, ничего для этого не сделав.
// Поле принадлежит типу, и получить его случайно нельзя.
func contractProxyTypes(f *ast.File, stubs string, aliases map[string]string) []string {
	if stubs == "" && len(aliases) == 0 {
		return nil
	}
	var names []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		embeds := map[string]bool{}
		for _, n := range serverStubTypeNames(gd, stubs, aliases) {
			embeds[n] = true
		}
		for _, spec := range gd.Specs {
			ts, okSpec := spec.(*ast.TypeSpec)
			if !okSpec || !embeds[ts.Name.Name] {
				continue
			}
			st, okStruct := ts.Type.(*ast.StructType)
			if !okStruct || st.Fields == nil {
				continue
			}
			for _, fld := range st.Fields.List {
				if len(fld.Names) == 0 {
					continue // вкладывание — не хранение клиента
				}
				if mentionsContractClient(fld.Type, stubs, aliases) {
					names = append(names, ts.Name.Name)
					break
				}
			}
		}
	}
	return names
}

// mentionsContractClient — выражение типа поля упоминает клиент контракта.
//
// Обход, а не сравнение: клиент лежит и голым полем, и в карте
// (`map[string]<stubs>.OperationServiceClient` — форма края), и в срезе. Судить
// одну форму значило бы объявить остальные не-прокси и вернуть их под ось
// обработчика.
func mentionsContractClient(e ast.Expr, stubs string, aliases map[string]string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if isSel(x, stubs, "OperationServiceClient") {
				found = true
				return false
			}
		case *ast.Ident:
			if aliases[x.Name] == "OperationServiceClient" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// readsRecordedOwner — тело читает владельца У САМОЙ СТРОКИ ответа
// (`GetPrincipalType`/`GetPrincipalId` контрактной операции).
//
// Это НАЧАЛО второй полосы владения. Первая (ось `Ownership`) сужает выдачу
// предикатом хранилища и владельца из строки не читает вовсе; вторая получает
// строку уже прочитанной — у соседа — и решает по её полям. Полосы разные,
// и до заведения этой оси вторую не видел никто.
//
// Файл обязан импортировать стабы контракта: те же имена геттеров носят ответы
// ДРУГИХ контрактов (у края — ответ фасада личности), и без этого условия ось
// краснела бы на предмете, к операциям отношения не имеющем.
func readsRecordedOwner(d *ast.FuncDecl) bool {
	if d.Body == nil {
		return false
	}
	found := false
	ast.Inspect(d.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "GetPrincipalType" || sel.Sel.Name == "GetPrincipalId" {
			found = true
			return false
		}
		return true
	})
	return found
}

// ownershipDecisionVerb — санкционированный глагол решения о владении по
// ПРОЧИТАННОЙ строке. Живёт у владельца полосы (`pkg/operations`); здесь стоит
// его имя, потому что гейт судит форму вызова, а не тип.
const ownershipDecisionVerb = "CheckRecordedOwnership"

// delegatesOwnershipDecision — КАЖДЫЙ выход функции возвращает результат
// санкционированного глагола владельца.
//
// Требуется именно «каждый», а не «хотя бы один»: форма «свой обход, потом
// общий глагол» проходила бы проверку на присутствие вызова, и обход — ровно
// то, ради чего полосы разошлись. Пустая строка отказа (`return nil`) выходом
// через глагол тоже не является: это решение, принятое здесь.
//
// ГРАНИЦА НАЗВАНА: судится ФОРМА выхода, а не то, ЧТО подано глаголу на вход.
// Функция, вычислившая не ту личность и честно отдавшая её общему решению,
// этой осью не ловится — такой предикат судил бы намерение, и его ложные
// находки начались бы на первом же журнале аудита. Что подано на вход, держат
// пробы самой полосы.
func delegatesOwnershipDecision(d *ast.FuncDecl, ops string) bool {
	if d.Body == nil || ops == "" {
		return false
	}
	exits, delegated := 0, 0
	ast.Inspect(d.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false // выход замыкания — не выход этой функции
		}
		ret, isRet := n.(*ast.ReturnStmt)
		if !isRet {
			return true
		}
		exits++
		if len(ret.Results) != 1 {
			return true
		}
		call, isCall := ret.Results[0].(*ast.CallExpr)
		if isCall && isSel(call.Fun, ops, ownershipDecisionVerb) {
			delegated++
		}
		return true
	})
	return exits > 0 && exits == delegated
}

// convertsOperation — функция ПЕРЕВОДИТ доменную строку операции в контракт:
// принимает `*<ops>.Operation` и возвращает `*<stubs>.Operation`.
//
// Одного возврата НЕДОСТАТОЧНО, и это измерено: `*<stubs>.Operation` возвращает
// КАЖДЫЙ мутирующий RPC (ban #9 — мутации отвечают операцией), поэтому предикат
// «возвращает операцию» даёт на порядок больше попаданий, чем настоящих
// преобразователей. Числа здесь не выписаны намеренно: прежняя редакция несла
// пару «194 при 13» без ревизии и без команды, и ни одна из величин не
// воспроизвелась — это тот же класс, что перепись алиасов, вынесенная отсюда
// ранее. Обе величины печатает сам гейт на каждом прогоне.
//
// Пара «принимает доменную строку → возвращает контракт» переименованию не
// поддаётся: оба типа принадлежат чужим пакетам.
func convertsOperation(d *ast.FuncDecl, stubs, ops string, aliases map[string]string) bool {
	if d.Type.Results == nil || d.Type.Params == nil {
		return false
	}
	// Преобразователь — функция, у которой операция ЕДИНСТВЕННЫЙ вход.
	//
	// Здесь стояло ещё «и нет получателя» (`d.Recv != nil`), и обоснование
	// приписывало этой клаузе работу СОСЕДНЕГО условия. Замер снятием: перепись
	// не меняется ни на единицу (`1535 / 0 / 10 / 0 / 0 / 3`), собственные
	// контроли остаются зелёными — методы use-case вида `finish(ctx, op, limit)`
	// отсекает `len(params) != 1`, а не получатель. Зато клауза открывала
	// обычную форму Go: преобразователь-метод со своим телом проезжал молча.
	// Условие, не покрытое ни одной пробой и ничего не покупающее, — это
	// слепая зона, выданная вперёд.
	//
	//   - приём по ЗНАЧЕНИЮ судится наравне с указателем (`operations.New`
	//     возвращает значение, поэтому такая форма естественна, и прежде
	//     `func toPB(op operations.Operation)` проезжал);
	//   - но одно лишь «принимает операцию и возвращает контракт» ловит методы
	//     use-case вида `finish(ctx, op, limit)`: на дереве это дало две ложные
	//     находки из двух новых. Распознаватель, у которого все новые находки
	//     ложные, отключат первым прочтением. Отсекает их ИМЕННО число входов.
	if len(d.Type.Params.List) != 1 || len(d.Type.Params.List[0].Names) > 1 {
		return false
	}
	pt := d.Type.Params.List[0].Type
	if !isPtrTo(pt, ops, "Operation") && !isSel(pt, ops, "Operation") && !isAliasIdent(pt, aliases, "Operation") {
		return false
	}
	for _, r := range d.Type.Results.List {
		// Возврат тоже может стоять за псевдонимом: `type opPB = pb.Operation`.
		if isPtrTo(r.Type, stubs, "Operation") || isPtrToAlias(r.Type, aliases, "Operation") {
			return true
		}
	}
	return false
}

// isAliasIdent — тип есть местное имя (со звёздочкой или без), являющееся
// псевдонимом искомого. Покрывает и `*domOp`, и `domOp`.
func isAliasIdent(e ast.Expr, aliases map[string]string, want string) bool {
	if star, okStar := e.(*ast.StarExpr); okStar {
		e = star.X
	}
	id, okID := e.(*ast.Ident)
	return okID && aliases[id.Name] == want
}

// delegatesToShared — тело есть РОВНО один возврат вызова общего слоя.
//
// Требуется именно общий слой, а не «однострочный возврат чего-нибудь с
// подходящим именем»: прежняя редакция принимала `return legacyToProto(op)`,
// то есть своя реализация проезжала за прослойку, отличаясь от отрицательной
// фикстуры гейта одной буквой.
func delegatesToShared(d *ast.FuncDecl, shared string) bool {
	if shared == "" || d.Body == nil || len(d.Body.List) != 1 {
		return false
	}
	ret, ok := d.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	// Прослойка бывает и с двумя возвращаемыми: `return operationspb.ToProto(op), nil`.
	// Прежняя редакция требовала ровно одного и объявляла такую форму находкой —
	// ложной. Судится ПЕРВОЕ значение; остальные обязаны быть пустыми
	// идентификаторами, иначе это уже не перевод, а логика.
	for _, extra := range ret.Results[1:] {
		id, okID := extra.(*ast.Ident)
		if !okID || id.Name != "nil" {
			return false
		}
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || !isSel(call.Fun, shared, "ToProto") {
		return false
	}
	// Аргумент обязан быть СВОИМ параметром неизменным. Прежде проверялся
	// только вызов, и `return operationspb.ToProto(stripOwner(op))` считалось
	// образцовой прослойкой: одна строка, возврат, вызов общего слоя — а полоса
	// при этом расходилась (владелец стирался из ответа).
	if len(call.Args) != 1 {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	if !ok || d.Type.Params == nil {
		return false
	}
	for _, p := range d.Type.Params.List {
		for _, n := range p.Names {
			if n.Name == arg.Name {
				return true
			}
		}
	}
	return false
}

// collectsOwnerKey — функция собирает ключ владения операцией ЧЕРЕЗ САНКЦИОНИРОВАННЫЕ
// ГЛАГОЛЫ владельца (`operations.AsOwned`, `operations.OwnerFromContext`).
//
// ГРАНИЦА ОСИ НАЗВАНА, потому что молчание этой оси легко прочесть шире, чем оно
// есть. «Мест полосы владения: 0» означает «никто в наблюдаемом дереве не зовёт
// эти два глагола», а НЕ «самодельных полос владения нет». Самодельная полоса в
// дереве есть — край сравнивает личность вызывающего с полями операции своими
// руками, — и ось её не видит by construction, а не по ведомости.
//
// Расширять ось до «любое сравнение личности с полями операции» я не стал
// осознанно: такой предикат судит намерение, а не форму, и его ложные находки
// начнутся на первом же журнале аудита. Предмет самодельной полосы края заведён
// задачей продукта #1370; ось смотрит вперёд — она поймает сервис, который
// начнёт собирать полосу санкционированными глаголами вместо общего слоя.
func collectsOwnerKey(d *ast.FuncDecl, ops string) bool {
	if d.Body == nil {
		return false
	}
	found := false
	ast.Inspect(d.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isSel(call.Fun, ops, "AsOwned") || isSel(call.Fun, ops, "OwnerFromContext") {
			found = true
			return false
		}
		return true
	})
	return found
}

// localTypeAliases — псевдонимы, ссылающиеся на ДРУГОЕ МЕСТНОЕ имя:
// `type wire = opPB`. Разрешаются вызывающим до неподвижной точки.
func localTypeAliases(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, okDecl := decl.(*ast.GenDecl)
		if !okDecl || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, okSpec := spec.(*ast.TypeSpec)
			if !okSpec || !ts.Assign.IsValid() {
				continue
			}
			if id, okID := ts.Type.(*ast.Ident); okID {
				out[ts.Name.Name] = id.Name
			}
		}
	}
	return out
}
