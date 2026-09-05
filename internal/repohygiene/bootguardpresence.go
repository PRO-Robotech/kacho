// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// bootguardpresence.go — обход дерева для гейта посадки. Живёт в НЕ-тестовом
// файле намеренно: инъекция (`bootguardpresence_injection_test.go`) обязана
// звать ТУ ЖЕ функцию, что и гейт, иначе она доказывает свойство своей копии.

// postureKnobRe — посадочная ручка: та, чьё значение решает БЕЗОПАСНОСТЬ старта,
// а не поведение домена. Перечень закрыт намеренно: широкий предикат («любая
// envconfig-ручка») сделал бы находкой каждый сервисный параметр.
//
// ФОРМ ЗАПИСИ В ЭТОМ ДЕРЕВЕ ЧЕТЫРЕ, И РАСПОЗНАВАТЕЛЬ ОБЯЗАН ЗНАТЬ ВСЕ
// (`testing.md` §«Гейт на класс», п.7). Форма, о которой он не знает, не даёт ни
// красного, ни зелёного — она МОЛЧИТ, и всё записанное в ней оказывается вне
// наблюдения:
//
//   - `envconfig:"..._AUTH_MODE"` — шесть сервисов;
//   - `mapstructure:"ssl-mode"` и родня — три сервиса на viper/koanf;
//   - `envconfig:"..._AUTHN_MODE"` — край. Отличается от первой формы ОДНОЙ
//     буквой, и этого хватило, чтобы край не наблюдался вовсе;
//   - `envconfig:"..._CLIENTAUTHMODE"` — режим проверки клиентского сертификата
//     на слушателе iam; без подчёркивания, поэтому под первую форму не подпадал.
//
// Плюс `MTLS_ENABLE` (взведён ли транспорт вообще) и `ALLOWED_SPIFFE` (круг
// законных отправителей у края) — обе ban #16 дословно.
var postureKnobRe = regexp.MustCompile(
	`envconfig:"((?:[A-Z_]*)(?:AUTHN?_MODE|CLIENTAUTHMODE|DB_SSLMODE|` +
		`TRUSTED_FORWARDER_SANS|TRUST_ANY_FORWARDER|ALLOWED_SPIFFE|MTLS_ENABLE))"` +
		`|mapstructure:"([a-z-]*(?:auth-?n?-?mode|ssl-?mode|trusted-?forwarder[a-z-]*|` +
		`trust-any-forwarder|allowed-?spiffe|mtls-?enable))"`)

// postureReachRelaxation — послабление ведомости: компонент, чьи посадочные
// ручки до центрального дескриптора пока не доведены.
//
// У записи ОБЯЗАТЕЛЬНО есть номер задачи и причина. Это не украшение: маска
// прячет предмет, а послабление его НАЗЫВАЕТ — и снимается вместе с ним.
type postureReachRelaxation struct {
	issue int
	why   string
}

// postureReach — что обход установил о компоненте.
type postureReach struct {
	// components — все осмотренные компоненты, отсортированы.
	components []string
	// knobs — объявленные посадочные ручки компонента (уникальные, отсортированы).
	knobs map[string][]string
	// accepts — координата, где компонент ПРИНИМАЕТ центральный дескриптор так,
	// что отказ доезжает до вызывающего.
	accepts map[string]string
	// discards — координата, где дескриптор принят, а его отказ ВЫБРОШЕН в `_`.
	// Отдельная величина, а не отсутствие принятия: находка тут другая, и текст
	// её обязан отличаться, иначе чинить будут не то.
	discards map[string]string
	// filesRead — объём осмотренного: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	filesRead int
	// fieldsSeen — посадочных полей `Spec`, найденных в композиционных корнях.
	// fieldsWired — из них ПОЛУЧАЮЩИХ значение из конфигурации.
	//
	// Величины печатаются ПОРОЗНЬ намеренно: одно число скрывает ровно тот
	// случай, ради которого ось заведена, — поле объявлено, страж его читает, а
	// значение подставлено литералом.
	fieldsSeen  int
	fieldsWired int
	// fieldsUnjudged — посадочных полей, чьё значение приходит ПАРАМЕТРОМ, а ни
	// одного вызова этой функции в компоненте не нашлось. Судить нечем, и это
	// говорится ЧИСЛОМ, а не молчанием: «не судится» и «провязано» — разное.
	fieldsUnjudged int
	// fieldsWithdrawn — посадочных полей, ОБЪЯВЛЕННЫХ НЕПРИМЕНИМЫМИ с причиной
	// (`servicecontract.NotApplicable`). Третье состояние оси, и оно не сводится
	// ни к «провязано», ни к «подставлена константа»: у процесса без своей базы
	// или без входящей переданной личности ручки НЕТ, и выводить значение неоткуда.
	//
	// Величина печатается ОТДЕЛЬНО, а не прибавляется к провязанным: иначе
	// «изъято» стало бы неотличимо от «выведено из настройки», и послабление
	// растворилось бы в числе, которое читают как успех.
	fieldsWithdrawn int
	// literalFields — координаты посадочных полей, чьё значение НЕ выведено из
	// конфигурации: `<компонент>` → перечень «поле (файл:строка) = <выражение>».
	literalFields map[string][]string
	// staleWithdrawals — изъятия, ПЕРЕЖИВШИЕ свой предмет: ось объявлена
	// неприменимой, а ручку этой оси компонент объявляет. Это и есть
	// самоистечение: пока предмета нет — молчим, появился — находка.
	staleWithdrawals map[string][]string
	// providersSeen — поставщиков дескриптора найдено (включая обёртки).
	// callersSeen — их вызовов осмотрено.
	// callersReach — из них таких, чей ОТКАЗ доезжает до остановки процесса.
	//
	// Величины печатаются ПОРОЗНЬ намеренно: одно число скрывает ровно тот
	// случай, ради которого ось заведена, — вызов на месте, отказ погашен.
	providersSeen int
	callersSeen   int
	callersReach  int
	// providersUncalled — экспортированных поставщиков без единого вызова внутри
	// компонента. Судить нечем: звать мог чужой модуль, а разбор без типов этого
	// не знает. Говорится ЧИСЛОМ, а не молчанием.
	providersUncalled int
	// quenched — координаты, где отказ поставщика до остановки НЕ доходит.
	quenched map[string][]string
}

// postureSpecFields — ЗАКРЫТЫЙ перечень посадочных полей `servicecontract.Spec`:
// тех, чьё значение приходит из посадочной ручки.
//
// ПОЧЕМУ ЗАКРЫТЫЙ. Широкий предикат («каждое поле Spec обязано быть выведено из
// конфигурации») сделал бы находкой законный литерал — и таких в дереве
// большинство: `Service: "kacho-geo"` есть имя, а не значение ручки.
//
// ЧТО СЮДА НЕ ВХОДИТ И ПОЧЕМУ ЭТО ЗАКОННЫЙ БЛИЗНЕЦ. `ForwarderKnobs.SANs` и
// `ForwarderKnobs.TrustAny` — ИМЕНА ручек, а не их значения: они обязаны быть
// литералами, иначе текст отказа не назовёт оператору, что править. Судится
// только `ForwarderKnobs.OptIn` — величина, которую ручка несёт.
var postureSpecFields = []string{
	"Mode",
	"DBSSLMode",
	"Forwarders",
	"ForwarderKnobs.OptIn",
}

// componentOf — имя компонента по пути от корня репозитория. Край — компонент
// наравне с сервисами: он объявляет посадочные ручки и потому подлежит тому же
// вопросу.
func componentOf(rel string) string {
	switch {
	case strings.HasPrefix(rel, "gateway/"):
		return "gateway"
	case strings.HasPrefix(rel, "services/"):
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			return ""
		}
		return parts[1]
	default:
		return ""
	}
}

// isContractNewCall отвечает, является ли выражение вызовом
// `servicecontract.New(servicecontract.Spec{…})`.
//
// Проверяется И имя вызова, И форма аргумента: `New` с уже собранной
// переменной-спекой встречается в пробах пакета, и засчитывать её за принятие
// дескриптора КОМПОЗИЦИОННЫМ КОРНЕМ нельзя.
func isContractNewCall(e ast.Expr, requireSpecLiteral bool) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "servicecontract" || sel.Sel.Name != "New" {
		return false
	}
	if !requireSpecLiteral {
		return true
	}
	if len(c.Args) != 1 {
		return false
	}
	cl, ok := c.Args[0].(*ast.CompositeLit)
	if !ok {
		return false
	}
	lit, ok := cl.Type.(*ast.SelectorExpr)
	return ok && lit.Sel.Name == "Spec"
}

// scanPostureReach обходит `services/` и `gateway/` и отвечает на ОДИН вопрос:
// доводит ли компонент объявленные посадочные ручки до стража, который на
// небезопасном значении ОТКАЗЫВАЕТ.
func scanPostureReach(root string) (postureReach, error) {
	res := postureReach{
		knobs:            map[string][]string{},
		accepts:          map[string]string{},
		discards:         map[string]string{},
		literalFields:    map[string][]string{},
		staleWithdrawals: map[string][]string{},
		quenched:         map[string][]string{},
	}
	seenComp := map[string]bool{}
	seenKnob := map[string]map[string]bool{}
	fset := token.NewFileSet()
	parsed := map[string][]parsedFile{}

	for _, dir := range []string{"services", "gateway"} {
		abs := filepath.Join(root, dir)
		all, err := treecorpus.Under(abs)
		if err != nil {
			// Отсутствие каталога — не отказ: синтетическое дерево инъекции
			// заводит только тот, который проверяет.
			if strings.Contains(err.Error(), "no such file") {
				continue
			}
			return res, fmt.Errorf("состав дерева %s: %w", dir, err)
		}
		for _, path := range all {
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				continue
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return res, fmt.Errorf("относительный путь %s: %w", path, err)
			}
			rel = filepath.ToSlash(rel)
			comp := componentOf(rel)
			if comp == "" {
				continue
			}
			if !seenComp[comp] {
				seenComp[comp] = true
				seenKnob[comp] = map[string]bool{}
			}
			res.filesRead++

			af, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return res, fmt.Errorf("разбор %s: %w", rel, perr)
			}

			// Ручки — по тексту объявления структуры конфигурации: тег читается
			// как строка литерала, и разбор здесь ничего не добавляет.
			raw, rerr := os.ReadFile(path) // #nosec G304 -- путь из индекса git этого модуля
			if rerr != nil {
				return res, fmt.Errorf("чтение %s: %w", rel, rerr)
			}
			src := string(raw)
			for _, m := range postureKnobRe.FindAllStringSubmatch(src, -1) {
				k := m[1]
				if k == "" {
					k = m[2]
				}
				if !seenKnob[comp][k] {
					seenKnob[comp][k] = true
					res.knobs[comp] = append(res.knobs[comp], k)
				}
			}

			// Разобранный файл откладывается: ось провязки судится ВТОРЫМ
			// проходом. Причина — форма «значение приходит параметром, а
			// разбирается у вызывающего»: чтобы её судить, надо знать вызовы, а
			// они лежат в других функциях и других файлах компонента.
			parsed[comp] = append(parsed[comp], parsedFile{rel: rel, file: af})
		}
	}

	// ВТОРОЙ ПРОХОД: принятие дескриптора и провязка посадочных полей.
	for comp, files := range parsed {
		auditComponentDescriptor(&res, comp, files, fset)
	}

	for c := range seenComp {
		res.components = append(res.components, c)
		sort.Strings(res.knobs[c])
		sort.Strings(res.literalFields[c])
		sort.Strings(res.staleWithdrawals[c])
		sort.Strings(res.quenched[c])
	}
	sort.Strings(res.components)
	return res, nil
}

// parsedFile — разобранный файл компонента: путь для находки и дерево для
// разбора.
type parsedFile struct {
	rel  string
	file *ast.File
}

// funcRec — функция компонента вместе с тем, что о ней нужно знать оси
// провязки: где лежит, какие её идентификаторы несут конфигурацию и как
// называются её параметры по позициям.
type funcRec struct {
	rel    string
	decl   *ast.FuncDecl
	carry  map[string]bool
	params []string
}

// auditComponentDescriptor — принятие дескриптора и провязка его посадочных
// полей, разом по всем файлам компонента.
func auditComponentDescriptor(res *postureReach, comp string, files []parsedFile, fset *token.FileSet) {
	byName := map[string]funcRec{}
	var all []funcRec
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			r := funcRec{rel: pf.rel, decl: fd, carry: configCarryingIdents(fd), params: paramNames(fd)}
			all = append(all, r)
			// Методы в карту имён не идут: разрешение по голому имени спутало бы
			// их с одноимённой функцией. Форма «Spec собирается методом» в дереве
			// не встречается, а угадывать приёмник разбор без типов не может.
			if fd.Recv == nil {
				byName[fd.Name.Name] = r
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].rel != all[j].rel {
			return all[i].rel < all[j].rel
		}
		return all[i].decl.Pos() < all[j].decl.Pos()
	})

	// seeds — функции, в чьём теле стоит ПРИНЯТЫЙ вызов дескриптора: их отказ и
	// есть отказ посадки, и потому спрашивать надо с их вызывающих.
	var seeds []descriptorProvider
	seen := map[descriptorProvider]bool{}
	addSeed := func(fn funcRec) {
		if fn.decl.Recv != nil {
			return
		}
		p := descriptorProvider{name: fn.decl.Name.Name, dir: funcDir(fn.rel)}
		if seen[p] {
			return
		}
		seen[p] = true
		seeds = append(seeds, p)
	}

	for _, r := range all {
		fn := r
		ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.ReturnStmt:
				// `return servicecontract.New(servicecontract.Spec{…})` — обе
				// величины уезжают наверх, отказ теряться негде.
				for _, e := range st.Results {
					if isContractNewCall(e, true) {
						markAccept(res, comp, fn.rel, fset, e.Pos())
						auditSpecWiring(res, comp, fn, e, fset, byName, all)
						addSeed(fn)
					}
				}
			case *ast.AssignStmt:
				for _, rhs := range st.Rhs {
					if !isContractNewCall(rhs, true) {
						continue
					}
					if len(st.Lhs) == 2 {
						if id, ok := st.Lhs[1].(*ast.Ident); ok && id.Name == "_" {
							markDiscard(res, comp, fn.rel, fset, rhs.Pos())
							continue
						}
					}
					markAccept(res, comp, fn.rel, fset, rhs.Pos())
					auditSpecWiring(res, comp, fn, rhs, fset, byName, all)
					addSeed(fn)
				}
			}
			return true
		})
	}

	// Третья ось: отказ поставщика обязан доехать до ОСТАНОВКИ. Две предыдущие
	// про неё не спрашивают — между ними и живёт форма, гасящая посадку одним
	// символом у вызывающего.
	auditDescriptorRefusalReach(res, comp, seeds, all, fset)
}

// paramNames — имена параметров по позициям (для резолва довода у вызывающего).
func paramNames(fd *ast.FuncDecl) []string {
	var out []string
	if fd.Type.Params == nil {
		return out
	}
	for _, f := range fd.Type.Params.List {
		if len(f.Names) == 0 {
			out = append(out, "_")
			continue
		}
		for _, nm := range f.Names {
			out = append(out, nm.Name)
		}
	}
	return out
}

func markAccept(res *postureReach, comp, rel string, fset *token.FileSet, pos token.Pos) {
	if res.accepts[comp] == "" {
		res.accepts[comp] = fmt.Sprintf("%s:%d", rel, fset.Position(pos).Line)
	}
}

func markDiscard(res *postureReach, comp, rel string, fset *token.FileSet, pos token.Pos) {
	if res.discards[comp] == "" {
		res.discards[comp] = fmt.Sprintf("%s:%d", rel, fset.Position(pos).Line)
	}
}

// ── О2: свидетель отказа у центрального контракта ───────────────────────────

// contractWitness — что обход установил о пробах центрального контракта.
type contractWitness struct {
	funcsRead  int
	direct     []string        // свидетели, у которых `New` и утверждение в одном теле
	delegating []string        // свидетели, зовущие прямого
	axes       map[string]bool // оси Spec, тронутые хотя бы одним свидетелем
}

// refusalAssertNames — имена утверждений, УТВЕРЖДАЮЩИХ ОШИБКУ.
//
// Полярность несущая: `require.NoError` и `if err != nil { t.Fatal }` — это
// ПОЛОЖИТЕЛЬНЫЙ контроль («законное принято»), и засчитывать его за свидетеля
// отказа значило бы зеленеть на дескрипторе, который не отвергает ничего.
var refusalAssertNames = map[string]bool{
	"Error": true, "ErrorIs": true, "ErrorAs": true, "ErrorContains": true,
	"Is": true, "As": true,
}

// scanContractRefusalWitness разбирает пробы пакета центрального контракта и
// ищет СВИДЕТЕЛЯ ОТКАЗА — тело, в котором утверждение об ошибке стоит НА
// РЕЗУЛЬТАТЕ ТОГО ЖЕ вызова `servicecontract.New`.
//
// ПОЧЕМУ РАЗБОР, А НЕ ПОДСТРОКА. Предшественник этого гейта признавал свидетеля
// ПО ФАЙЛУ: где-то в файле встретился вызов стража, где-то — `require.Error(`, и
// связи между ними не требовалось. Проверено опытом: страж, сведённый к раннему
// `return nil`, проходил гейт и весь набор `internal/repohygiene` насквозь.
// Утверждение «подделать её нельзя, не написав настоящей проверки» было ложным.
//
// ФОРМЫ ЗАПИСИ, КОТОРЫЕ ЭТОТ РАЗБОР ЗНАЕТ (все законны и все встречаются):
//
//	err := ...New(s); if err == nil { t.Fatal }   — присваивание отдельной строкой
//	if _, err := ...New(s); err == nil { … }      — единым выражением
//	require.Error(t, err) / assert.Error(t, err)  — библиотечное утверждение
//	require.ErrorIs / errors.Is(err, target)      — утверждение о виде ошибки
//	вызов функции-свидетеля                        — делегация на один уровень
//
// ГЛУБИНА ДЕЛЕГАЦИИ — ОДИН УРОВЕНЬ, и это названо, а не подразумевается.
// Свидетель через двух посредников этим разбором не признаётся; сегодня таких в
// дереве нет, а признавать произвольную глубину значило бы строить граф вызовов
// ради вопроса, который решается одним хелпером.
// Состав ПРИНИМАЕТСЯ списком, а не собирается здесь: тот же разбор исполняется
// и на настоящем дереве (состав из индекса git), и на синтетическом пакете,
// собранном инъекцией во временном каталоге (состав с диска). Собирай эта
// функция состав сама, одна из двух полос оказалась бы неверной: индекса у
// временного каталога нет, а обход диска на настоящем дереве читал бы
// игнорируемое и делал бы вердикт свойством рабочего каталога.
func scanContractRefusalWitness(paths []string) (contractWitness, error) {
	res := contractWitness{axes: map[string]bool{}}
	fset := token.NewFileSet()
	sort.Strings(paths)

	type fnRec struct {
		name string
		body *ast.BlockStmt
	}
	var fns []fnRec
	for _, path := range paths {
		af, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return res, fmt.Errorf("разбор %s: %w", path, perr)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fns = append(fns, fnRec{name: fd.Name.Name, body: fd.Body})
		}
	}
	res.funcsRead = len(fns)

	directSet := map[string]bool{}
	axesOf := map[string][]string{}
	for _, fn := range fns {
		ok, axes := bodyWitnessesRefusal(fn.body)
		axesOf[fn.name] = axes
		if ok {
			directSet[fn.name] = true
			res.direct = append(res.direct, fn.name)
		}
	}
	for _, fn := range fns {
		if directSet[fn.name] {
			continue
		}
		if bodyCallsAny(fn.body, directSet) {
			res.delegating = append(res.delegating, fn.name)
		}
	}
	for _, n := range append(append([]string{}, res.direct...), res.delegating...) {
		for _, a := range axesOf[n] {
			res.axes[a] = true
		}
	}
	sort.Strings(res.direct)
	sort.Strings(res.delegating)
	return res, nil
}

// bodyWitnessesRefusal — правда ли, что в ЭТОМ теле утверждение об ошибке стоит
// на результате вызова `servicecontract.New`. Возвращает вдобавок имена полей
// Spec, которым тело присваивает: по ним видно, КАКУЮ ось свидетель трогает.
func bodyWitnessesRefusal(body *ast.BlockStmt) (bool, []string) {
	errNames := map[string]bool{}
	var axes []string

	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, rhs := range as.Rhs {
			if !isContractNewCall(rhs, false) {
				continue
			}
			// Ошибка — последняя величина результата.
			if len(as.Lhs) >= 1 {
				if id, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident); ok && id.Name != "_" {
					errNames[id.Name] = true
				}
			}
		}
		// Ось: `s.DBSSLMode = …`, `s.Forwarders = …`, `s.ForwarderKnobs.OptIn = …`.
		for _, l := range as.Lhs {
			if sel, ok := l.(*ast.SelectorExpr); ok {
				axes = append(axes, sel.Sel.Name)
			}
		}
		return true
	})
	if len(errNames) == 0 {
		return false, axes
	}

	witnessed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.BinaryExpr:
			// ТОЛЬКО `err == nil` — то есть «ожидался отказ». `err != nil`
			// утверждает обратное и свидетелем не является.
			if e.Op != token.EQL {
				return true
			}
			x, xok := e.X.(*ast.Ident)
			y, yok := e.Y.(*ast.Ident)
			if xok && yok && errNames[x.Name] && y.Name == "nil" {
				witnessed = true
			}
		case *ast.CallExpr:
			sel, ok := e.Fun.(*ast.SelectorExpr)
			if !ok || !refusalAssertNames[sel.Sel.Name] {
				return true
			}
			for _, a := range e.Args {
				if id, ok := a.(*ast.Ident); ok && errNames[id.Name] {
					witnessed = true
				}
			}
		}
		return true
	})
	return witnessed, axes
}

// bodyCallsAny — зовёт ли тело хоть одну из названных функций пакета.
func bodyCallsAny(body *ast.BlockStmt, names map[string]bool) bool {
	hit := false
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := c.Fun.(*ast.Ident); ok && names[id.Name] {
			hit = true
		}
		return true
	})
	return hit
}

// ── адъюдикация: перепись + ведомость → находки ─────────────────────────────

// postureReachVerdict — что решено по обходу и ведомости.
type postureReachVerdict struct {
	findings   []string
	withKnobs  int
	reached    int
	relaxed    int
	relaxNotes []string
}

// adjudicatePostureReach принимает перепись и ВЕДОМОСТЬ отдельным аргументом —
// чтобы инъекция гоняла ту же ветку решения на синтетической ведомости, а не
// свою копию условий.
func adjudicatePostureReach(reach postureReach, ledger map[string]postureReachRelaxation) []string {
	return adjudicatePostureReachFull(reach, ledger).findings
}

func adjudicatePostureReachFull(reach postureReach, ledger map[string]postureReachRelaxation) postureReachVerdict {
	var v postureReachVerdict

	for _, comp := range reach.components {
		knobs := reach.knobs[comp]
		rel, hasRel := ledger[comp]

		if len(knobs) == 0 {
			// Послаблению у компонента БЕЗ посадочных ручек исключать нечего.
			if hasRel {
				v.findings = append(v.findings, fmt.Sprintf(
					"%s — послаблению (#%d) НЕЧЕГО ИСКЛЮЧАТЬ: компонент больше не "+
						"объявляет посадочных ручек. Снимите запись из ведомости", comp, rel.issue))
			}
			continue
		}
		v.withKnobs++

		if at := reach.discards[comp]; at != "" {
			v.findings = append(v.findings, fmt.Sprintf(
				"%s — дескриптор принят (%s), а его ОТКАЗ ВЫБРОШЕН в `_`: страж "+
					"исполняется и не может ничего остановить", comp, at))
			continue
		}

		if at := reach.accepts[comp]; at != "" {
			v.reached++
			if hasRel {
				v.findings = append(v.findings, fmt.Sprintf(
					"%s — послаблению (#%d) НЕЧЕГО ИСКЛЮЧАТЬ: компонент принимает "+
						"центральный дескриптор (%s). Снимите запись из ведомости и "+
						"закройте задачу", comp, rel.issue, at))
			}
			// Принять дескриптор мало: значение ручки обязано до него ДОЙТИ.
			// Литерал на месте посадочного поля делает стража довольным всегда,
			// а ручку — недействующей никогда, и вызов при этом стоит на месте.
			if lits := reach.literalFields[comp]; len(lits) > 0 {
				v.findings = append(v.findings, fmt.Sprintf(
					"%s — дескриптор принят (%s), но посадочное поле НЕ ПОЛУЧАЕТ "+
						"значения из конфигурации: %s. Ручка объявлена и читается, а "+
						"до стража доезжает подставленная величина — страж доволен "+
						"всегда, ручка не действует никогда",
					comp, at, strings.Join(lits, "; ")))
			}
			// Изъятие оси ИСТЕКАЕТ САМО: пока у компонента нет ручки этой оси,
			// «мне это не адресовано» — законный ответ; появилась ручка — ответ
			// пережил свой предмет, и молчать здесь значило бы выдать слепую
			// зону вперёд.
			if st := reach.staleWithdrawals[comp]; len(st) > 0 {
				v.findings = append(v.findings, fmt.Sprintf(
					"%s — дескриптор принят (%s), но ИЗЪЯТИЕ ОСИ ПЕРЕЖИЛО СВОЙ ПРЕДМЕТ: %s. "+
						"Ось объявлена неприменимой, а ручка у компонента есть — значит "+
						"величина, которую оператор выставляет, до стража не доезжает вовсе",
					comp, at, strings.Join(st, "; ")))
			}
			// Принять дескриптор и провязать ручки мало: ОТКАЗ стража обязан
			// доехать до остановки процесса. Погашенный у вызывающего отказ
			// оставляет вызов на месте, перепись — неизменной, а посадку —
			// непроверенной.
			if q := reach.quenched[comp]; len(q) > 0 {
				v.findings = append(v.findings, fmt.Sprintf(
					"%s — дескриптор принят (%s), но его ОТКАЗ до остановки процесса "+
						"НЕ ДОХОДИТ: %s", comp, at, strings.Join(q, "; ")))
			}
			continue
		}

		if hasRel {
			v.relaxed++
			v.relaxNotes = append(v.relaxNotes,
				fmt.Sprintf("%s — #%d, %s", comp, rel.issue, rel.why))
			continue
		}
		v.findings = append(v.findings, fmt.Sprintf(
			"%s — объявляет посадочные ручки (%s), а до центрального дескриптора "+
				"их не доводит: `servicecontract.New(servicecontract.Spec{…})` в "+
				"композиционном корне отсутствует", comp, strings.Join(knobs, ", ")))
	}

	// Запись о компоненте, которого в дереве нет вовсе, — тоже истёкшее
	// послабление. Без этой ветки переименованный компонент унёс бы своё
	// послабление в невидимость.
	present := map[string]bool{}
	for _, c := range reach.components {
		present[c] = true
	}
	var names []string
	for c := range ledger {
		names = append(names, c)
	}
	sort.Strings(names)
	for _, c := range names {
		if !present[c] {
			v.findings = append(v.findings, fmt.Sprintf(
				"%s — послаблению (#%d) НЕЧЕГО ИСКЛЮЧАТЬ: такого компонента в дереве "+
					"нет. Снимите запись из ведомости", c, ledger[c].issue))
		}
	}
	return v
}

// ── ось «ЗНАЧЕНИЕ РУЧКИ ДОХОДИТ ДО СТРАЖА» ─────────────────────────────────
//
// ПРЕДМЕТ, И ОН ОТДЕЛЬНЫЙ ОТ «ДЕСКРИПТОР ПРИНЯТ». Компонент может объявить
// ручку, прочитать её из окружения — и подставить в дескриптор ПРАВДОПОДОБНУЮ
// КОНСТАНТУ. Тогда страж доволен ВСЕГДА, ручка не действует НИКОГДА, а вызов
// стоит на месте. Проверено опытом на живом дереве: подстановка
// `DBSSLMode: "require"` вместо `cfg.DBSSLMode` собирается и проходит ось
// принятия насквозь.
//
// ПОЧЕМУ ЭТО НЕ ВИДНО В ОБЗОРЕ. Достаточно ОДНОЙ строки в композиционном корне,
// и она выглядит настройкой, а не подделкой. Подделка тиха by construction:
// негодная константа уронила бы старт громко, поэтому подставляют ГОДНУЮ.
//
// ЧЕГО ЭТА ОСЬ НЕ СУДИТ. Она не судит, что значение доехало ВЕРНЫМ: конфигурация
// может отдать не ту величину, и разбор этого не увидит. Она судит связь —
// значение поля выведено из конфигурации, а не назначено на месте.

// configCarryingIdents — идентификаторы функции, несущие конфигурацию:
// параметры и приёмник типа `…Config`, плюс локальные переменные, полученные из
// них (замыкание по присваиваниям до неподвижной точки).
//
// ФОРМЫ, КОТОРЫЕ ЭТО ПОКРЫВАЕТ, названы поимённо — форма, о которой
// распознаватель не знает, уходит в НЕВИДИМОСТЬ, а не в находку
// (`testing.md` §«Гейт на класс», п.7):
//
//	cfg.DBSSLMode                          — поле конфигурации
//	cfg.Repository.Postgres.URL            — вложенное поле
//	cfg.TrustedForwarders()                — метод конфигурации
//	coredb.SSLModeFromDSN(cfg.DSN())       — чужой вызов с доводом из конфигурации
//	mode  ← ParseMode(cfg.AuthMode)        — промежуточная переменная
//	dsn := cfg.DSN(); … SSLModeFromDSN(dsn) — цепочка промежуточных
func configCarryingIdents(fd *ast.FuncDecl) map[string]bool {
	carry := map[string]bool{}
	addParams := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if !typeLooksLikeConfig(f.Type) {
				continue
			}
			for _, nm := range f.Names {
				if nm.Name != "_" {
					carry[nm.Name] = true
				}
			}
		}
	}
	addParams(fd.Recv)
	addParams(fd.Type.Params)

	// Замыкание: переменная, полученная из уже известной, тоже несёт
	// конфигурацию. Повторяем, пока набор растёт, — иначе порядок присваиваний в
	// теле решал бы исход.
	for grew := true; grew; {
		grew = false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			// `a, b := f(cfg)` — обе величины считаются производными: разбор без
			// типов не знает, какая из них что несёт, и осторожность здесь в
			// сторону МОЛЧАНИЯ, а не ложной находки.
			derived := false
			for _, rhs := range as.Rhs {
				if exprFromConfig(rhs, carry) {
					derived = true
				}
			}
			if !derived {
				return true
			}
			for _, l := range as.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok || id.Name == "_" || carry[id.Name] {
					continue
				}
				carry[id.Name] = true
				grew = true
			}
			return true
		})
	}
	return carry
}

// typeLooksLikeConfig — тип, чьё конечное имя `Config`. Имя параметра при этом
// не читается: привязка к `cfg` сломалась бы на первом же корне, назвавшем его
// иначе.
func typeLooksLikeConfig(t ast.Expr) bool {
	switch e := t.(type) {
	case *ast.StarExpr:
		return typeLooksLikeConfig(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name == "Config"
	case *ast.Ident:
		return e.Name == "Config"
	default:
		return false
	}
}

// exprFromConfig — выведено ли значение выражения из конфигурации.
func exprFromConfig(e ast.Expr, carry map[string]bool) bool {
	switch x := e.(type) {
	case nil:
		return false
	case *ast.Ident:
		return carry[x.Name]
	case *ast.SelectorExpr:
		return exprFromConfig(x.X, carry)
	case *ast.CallExpr:
		if exprFromConfig(x.Fun, carry) {
			return true
		}
		for _, a := range x.Args {
			if exprFromConfig(a, carry) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return exprFromConfig(x.X, carry)
	case *ast.StarExpr:
		return exprFromConfig(x.X, carry)
	case *ast.UnaryExpr:
		return exprFromConfig(x.X, carry)
	case *ast.BinaryExpr:
		return exprFromConfig(x.X, carry) || exprFromConfig(x.Y, carry)
	case *ast.IndexExpr:
		return exprFromConfig(x.X, carry) || exprFromConfig(x.Index, carry)
	case *ast.TypeAssertExpr:
		return exprFromConfig(x.X, carry)
	case *ast.KeyValueExpr:
		return exprFromConfig(x.Value, carry)
	case *ast.CompositeLit:
		for _, el := range x.Elts {
			if exprFromConfig(el, carry) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// specFieldValue достаёт значение поля `Spec` по пути вида `A` либо `A.B`.
func specFieldValue(lit *ast.CompositeLit, path string) (ast.Expr, bool) {
	head, tail, nested := strings.Cut(path, ".")
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != head {
			continue
		}
		if !nested {
			return kv.Value, true
		}
		inner, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return nil, false
		}
		return specFieldValue(inner, tail)
	}
	return nil, false
}

// auditSpecWiring судит КАЖДОЕ посадочное поле принятого дескриптора: получает
// ли оно значение из конфигурации.
//
// ФОРМА «ЗНАЧЕНИЕ ПРИХОДИТ ПАРАМЕТРОМ» разрешается ОДНИМ уровнем вверх — по
// вызывающим той же функции внутри компонента. Она законна и распространена:
// режим разбирается один раз в корне и передаётся в сборщик дескриптора
// доводом. Первая редакция этой оси её не знала и объявила находкой исправный
// сервис — ровно тот класс, которого ось и стережёт: форма, о которой
// распознаватель не знает, уходит в невидимость либо в ложную находку.
//
// РЕШАЕТ КАЖДЫЙ ВЫЗОВ, А НЕ ЛУЧШИЙ ИЗ НИХ. Довод обязан быть выведен из
// конфигурации у ВСЕХ найденных вызывающих: один вызов с подставленной величиной
// и есть та дыра, ради которой ось заведена. Вызывающих не нашлось вовсе —
// судить нечем, и это идёт в перепись отдельной величиной, а не в находку.
func auditSpecWiring(res *postureReach, comp string, fn funcRec, newCall ast.Expr,
	fset *token.FileSet, byName map[string]funcRec, all []funcRec) {
	call, ok := newCall.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return
	}
	for _, path := range postureSpecFields {
		val, found := specFieldValue(lit, path)
		if !found {
			// Поле не задано вовсе — это ДРУГОЙ случай, и он ловится самим
			// дескриптором: он отказывает на незаполненной оси. Здесь судится
			// связь, а не полнота, поэтому в перепись такое поле не идёт.
			continue
		}
		res.fieldsSeen++

		if exprFromConfig(val, fn.carry) {
			res.fieldsWired++
			continue
		}

		// ТРЕТЬЕ состояние оси: изъятие с причиной. Оно не «литерал вместо
		// значения» и не «выведено из настройки»: у процесса, которому эта ось
		// не адресована, РУЧКИ НЕТ, и выводить значение неоткуда. Молчаливое
		// заполнение правдоподобной константой исходом при этом не становится —
		// её здесь и не бывает, потому что изъятие обязано нести причину, и
		// конструктор дескриптора без причины его не принимает.
		//
		// Изъятие ИСТЕКАЕТ САМО: если ручка этой оси у компонента объявлена,
		// изъятие пережило свой предмет — и это находка, а не тишина.
		if because, ok := withdrawnAxis(val); ok {
			if knob := postureKnobFor(path, res.knobs[comp]); knob != "" {
				res.staleWithdrawals[comp] = append(res.staleWithdrawals[comp], fmt.Sprintf(
					"%s (%s:%d) объявлено неприменимым (%q), а ручка %s компонентом ОБЪЯВЛЕНА",
					path, fn.rel, fset.Position(val.Pos()).Line, because, knob))
				continue
			}
			res.fieldsWithdrawn++
			continue
		}

		// Значение — голый параметр этой функции? Тогда спрашиваем вызывающих.
		if id, ok := val.(*ast.Ident); ok && fn.decl.Recv == nil {
			if pos := paramIndex(fn.params, id.Name); pos >= 0 {
				verdict, where := argFromConfigAtEveryCaller(fn.decl.Name.Name, pos, all, fset)
				switch verdict {
				case callerWired:
					res.fieldsWired++
					continue
				case callerNone:
					res.fieldsUnjudged++
					continue
				case callerLiteral:
					res.literalFields[comp] = append(res.literalFields[comp], fmt.Sprintf(
						"%s (%s:%d) = %s — довод %q у вызывающего %s тоже не из конфигурации",
						path, fn.rel, fset.Position(val.Pos()).Line,
						postureExprText(fset, val), id.Name, where))
					continue
				}
			}
		}

		res.literalFields[comp] = append(res.literalFields[comp], fmt.Sprintf(
			"%s (%s:%d) = %s", path, fn.rel, fset.Position(val.Pos()).Line,
			postureExprText(fset, val)))
	}
	_ = byName
}

// paramIndex — позиция параметра по имени, либо -1.
func paramIndex(params []string, name string) int {
	for i, p := range params {
		if p == name {
			return i
		}
	}
	return -1
}

type callerVerdict int

const (
	callerNone    callerVerdict = iota // вызывающих не нашлось — судить нечем
	callerWired                        // все вызывающие подают значение из конфигурации
	callerLiteral                      // хотя бы один подаёт подставленную величину
)

// argFromConfigAtEveryCaller — довод на позиции pos у ВСЕХ вызовов функции
// внутри компонента выведен из конфигурации?
func argFromConfigAtEveryCaller(fnName string, pos int, all []funcRec,
	fset *token.FileSet) (callerVerdict, string) {
	seen := false
	for _, caller := range all {
		bad := ""
		ast.Inspect(caller.decl.Body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := c.Fun.(*ast.Ident)
			if !ok || id.Name != fnName || pos >= len(c.Args) {
				return true
			}
			seen = true
			if !exprFromConfig(c.Args[pos], caller.carry) {
				bad = fmt.Sprintf("%s:%d", caller.rel, fset.Position(c.Args[pos].Pos()).Line)
			}
			return true
		})
		if bad != "" {
			return callerLiteral, bad
		}
	}
	if !seen {
		return callerNone, ""
	}
	return callerWired, ""
}

// postureExprText — исходный текст выражения, чтобы находка НАЗЫВАЛА подставленное
// значение. Находка без него отсылает читателя искать самому.
func postureExprText(fset *token.FileSet, e ast.Expr) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "<не восстановлено>"
	}
	return b.String()
}

// ── ось «ОТКАЗ ПОСТАВЩИКА ДЕСКРИПТОРА ДОХОДИТ ДО ВЫХОДА» ────────────────────
//
// ПРЕДМЕТ, И ОН ОТДЕЛЬНЫЙ ОТ ДВУХ ПРЕДЫДУЩИХ. Ось принятия спрашивает «стоит ли
// вызов дескриптора в композиционном корне»; ось провязки — «доехало ли до него
// значение ручки». Ни одна не спрашивает третьего: ДОШЁЛ ЛИ ОТКАЗ ДЕСКРИПТОРА ДО
// ОСТАНОВКИ ПРОЦЕССА. Между ними живёт форма, гасящая посадку ОДНИМ символом.
//
// ФОРМА, ЖИВУЩАЯ В ЭТОМ ДЕРЕВЕ, — вызов у ВЫЗЫВАЮЩЕГО, а не в теле с `New`:
//
//	desc, err := describe(cfg, …)   // поставщик возвращает New(Spec{…})
//	if err != nil { return err }    // ← вот это и есть достижение выхода
//
// Гашение тихо by construction: `desc, _ := describe(…)` собирается, `go vet`
// молчит, `errcheck` присваивание в `_` по умолчанию не судит (`check-blank`
// в настройках линта не объявлен). Проверено опытом на живом дереве.
//
// ЕЩЁ ТИШЕ — ГАШЕНИЕ ПРИ СОХРАНЁННОЙ ПРОВЕРКЕ. Там, где `err` объявлен выше по
// телу, после гашения остаётся `if err != nil { return err }`, судящий
// УСТАРЕВШУЮ переменную. Код собирается и ВЫГЛЯДИТ проверяющим отказ стража.
//
// НАПРАВЛЕНИЕ ОСТОРОЖНОСТИ ЗДЕСЬ ОБРАТНО ОСИ ПРОВЯЗКИ, и это сказано вслух.
// Там незнакомая форма уходит в молчание (ложная находка отключила бы гейт);
// здесь незнакомая форма обработки отказа даёт НАХОДКУ. Причина: молчание тут
// означало бы «посадка проверена», а это неправда, и заметить её нечем.
// Популяция мала (поставщиков единицы), поэтому цена ложной находки — одна
// строка в распознавателе, а цена слепоты — незамеченный обход стража.

// descriptorProvider — функция, ЧЕЙ ОТКАЗ И ЕСТЬ ОТКАЗ ПОСАДКИ: в её теле стоит
// принятый вызов `servicecontract.New(Spec{…})`, либо она голым `return`
// передаёт наверх результат другого поставщика.
type descriptorProvider struct {
	name string
	dir  string
}

// providerCallVerdict — что установлено об ОДНОМ вызове поставщика.
type providerCallVerdict int

const (
	// callReaches — отказ доезжает до выхода: проверен и ведёт к остановке,
	// либо передан наверх целиком.
	callReaches providerCallVerdict = iota
	// callQuenched — отказ погашен либо не доводится до выхода.
	callQuenched
)

// funcDir — каталог пакета, в котором лежит функция. Разрешение вызовов идёт ПО
// КАТАЛОГУ, а не по компоненту: голый `describe(…)` в Go резолвится только
// внутри своего пакета, и поиск по компоненту спутал бы одноимённые функции
// разных пакетов. Экспортированное имя ищется шире — по всему компоненту.
func funcDir(rel string) string {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return "."
	}
	return rel[:i]
}

func isExportedName(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}

// collectDescriptorProviders — поставщики компонента, с замыканием по обёрткам.
//
// ЗАМЫКАНИЕ ОБЯЗАТЕЛЬНО, а не «на будущее»: без него обёртка
// `func build() (D, error) { return describe() }` засчиталась бы достижением
// (она честно передаёт отказ наверх), а её собственный вызывающий остался бы
// вне наблюдения — то есть гашение уровнем выше стало бы невидимым.
func collectDescriptorProviders(seeds []descriptorProvider, all []funcRec) map[descriptorProvider]bool {
	set := map[descriptorProvider]bool{}
	for _, p := range seeds {
		set[p] = true
	}
	// Замыкание до неподвижной точки. Предел на число проходов — от длины
	// перечня функций: цикл по построению конечен, но пусть это будет видно.
	for grew, pass := true, 0; grew && pass <= len(all); pass++ {
		grew = false
		for _, fn := range all {
			if fn.decl.Recv != nil {
				continue
			}
			cand := descriptorProvider{name: fn.decl.Name.Name, dir: funcDir(fn.rel)}
			if set[cand] {
				continue
			}
			if funcBarePropagatesAnyProvider(fn, set) {
				set[cand] = true
				grew = true
			}
		}
	}
	return set
}

// funcBarePropagatesAnyProvider — есть ли в теле `return <поставщик>(…)`, где
// результат уезжает наверх целиком.
func funcBarePropagatesAnyProvider(fn funcRec, providers map[descriptorProvider]bool) bool {
	hit := false
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		if hit {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			if providerCallName(r, providers, funcDir(fn.rel)) != "" {
				hit = true
			}
		}
		return true
	})
	return hit
}

// providerCallName — имя поставщика, если выражение есть вызов одного из них,
// достижимый из каталога fromDir.
func providerCallName(e ast.Expr, providers map[descriptorProvider]bool, fromDir string) string {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	id, ok := c.Fun.(*ast.Ident)
	if !ok {
		return ""
	}
	for p := range providers {
		if p.name != id.Name {
			continue
		}
		if p.dir == fromDir || isExportedName(p.name) {
			return p.name
		}
	}
	return ""
}

// auditDescriptorRefusalReach судит КАЖДЫЙ вызов КАЖДОГО поставщика: доезжает ли
// его отказ до остановки процесса.
//
// РЕШАЕТ КАЖДЫЙ ВЫЗОВ, А НЕ ЛУЧШИЙ ИЗ НИХ — по той же причине, что и ось
// провязки: один вызов с погашенным отказом и есть та дыра, ради которой ось
// заведена.
func auditDescriptorRefusalReach(res *postureReach, comp string, seeds []descriptorProvider,
	all []funcRec, fset *token.FileSet) {
	if len(seeds) == 0 {
		return
	}
	providers := collectDescriptorProviders(seeds, all)
	res.providersSeen += len(providers)

	called := map[descriptorProvider]bool{}
	for _, fn := range all {
		auditCallerRefusalReach(res, comp, fn, providers, called, fset)
	}

	// Поставщик без единого вызова. Неэкспортированное имя из своего пакета
	// вызвать больше неоткуда — значит страж не исполняется вовсе, и это
	// НАХОДКА. Экспортированное мог позвать чужой модуль, чего разбор без типов
	// не знает, — такое идёт в перепись отдельной величиной, а не в находку:
	// молчание здесь означало бы «проверено», а это неправда.
	var names []descriptorProvider
	for p := range providers {
		names = append(names, p)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].dir != names[j].dir {
			return names[i].dir < names[j].dir
		}
		return names[i].name < names[j].name
	})
	for _, p := range names {
		if called[p] {
			continue
		}
		if isExportedName(p.name) {
			res.providersUncalled++
			continue
		}
		res.quenched[comp] = append(res.quenched[comp], fmt.Sprintf(
			"поставщик дескриптора %s (%s) не вызывается НИ РАЗУ в своём пакете — "+
				"страж собран и не исполняется", p.name, p.dir))
	}
}

// auditCallerRefusalReach — разбор одного тела: где в нём зовут поставщика и что
// делают с его отказом.
func auditCallerRefusalReach(res *postureReach, comp string, fn funcRec,
	providers map[descriptorProvider]bool, called map[descriptorProvider]bool,
	fset *token.FileSet) {
	dir := funcDir(fn.rel)

	// Все вызовы поставщиков в этом теле — ДО классификации. Разность множеств
	// даёт форму, которую распознаватель не знает: она обязана стать находкой, а
	// не тишиной.
	allCalls := map[token.Pos]string{}
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if name := providerCallName(e, providers, dir); name != "" {
				allCalls[e.Pos()] = name
			}
		}
		return true
	})
	if len(allCalls) == 0 {
		return
	}

	classified := map[token.Pos]bool{}
	mark := func(pos token.Pos, name string, verdict providerCallVerdict, why string) {
		// Один вызов — одна запись переписи. Форма «единым выражением» приходит
		// сюда ДВАЖДЫ: обход видит развилку, а следом её же присваивание внутри
		// `Init`. Без этой отсечки перепись назвала бы два вызова там, где он
		// один, — и число, ради которого ось заведена, перестало бы сходиться.
		if classified[pos] {
			return
		}
		classified[pos] = true
		res.callersSeen++
		// Вызванным помечается поставщик ИЗ ТОГО ЖЕ каталога (либо
		// экспортированный): два одноимённых поставщика в разных пакетах одного
		// компонента иначе засчитали бы друг другу чужой вызов.
		for p := range providers {
			if p.name == name && (p.dir == dir || isExportedName(p.name)) {
				called[p] = true
			}
		}
		if verdict == callReaches {
			res.callersReach++
			return
		}
		res.quenched[comp] = append(res.quenched[comp], fmt.Sprintf(
			"%s:%d — %s", fn.rel, fset.Position(pos).Line, why))
	}

	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.ReturnStmt:
			// `return <поставщик>(…)` — отказ уезжает наверх целиком.
			for _, r := range st.Results {
				if name := providerCallName(r, providers, dir); name != "" {
					mark(r.Pos(), name, callReaches, "")
				}
			}
		case *ast.ExprStmt:
			// Голым выражением: результат отброшен весь, вместе с отказом.
			if name := providerCallName(st.X, providers, dir); name != "" {
				mark(st.X.Pos(), name, callQuenched,
					"вызов поставщика дескриптора стоит голым выражением: отказ стража "+
						"отброшен вместе с результатом")
			}
		case *ast.IfStmt:
			// `if _, err := describe(…); err != nil { … }` — единым выражением.
			as, ok := st.Init.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range as.Rhs {
				name := providerCallName(rhs, providers, dir)
				if name == "" {
					continue
				}
				errName := assignedErrName(as)
				if errName == "" {
					mark(rhs.Pos(), name, callQuenched, quenchNoteBlank())
					continue
				}
				if exprMentionsIdent(st.Cond, errName) && ifBranchTerminates(st) {
					mark(rhs.Pos(), name, callReaches, "")
					continue
				}
				mark(rhs.Pos(), name, callQuenched, quenchNoteNoExit(errName))
			}
		case *ast.AssignStmt:
			for _, rhs := range st.Rhs {
				name := providerCallName(rhs, providers, dir)
				if name == "" {
					continue
				}
				errName := assignedErrName(st)
				if errName == "" {
					mark(rhs.Pos(), name, callQuenched, quenchNoteBlank())
					continue
				}
				if refusalReachesExit(fn.decl.Body, errName, rhs.Pos()) {
					mark(rhs.Pos(), name, callReaches, "")
					continue
				}
				mark(rhs.Pos(), name, callQuenched, quenchNoteNoExit(errName))
			}
		}
		return true
	})

	// Форма, которую разбор не отнёс ни к одной ветке.
	var rest []token.Pos
	for pos := range allCalls {
		if !classified[pos] {
			rest = append(rest, pos)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	for _, pos := range rest {
		mark(pos, allCalls[pos], callQuenched,
			"форма вызова поставщика дескриптора РАСПОЗНАВАТЕЛЮ НЕ ИЗВЕСТНА, поэтому "+
				"судьба отказа не установлена. Если форма законна — расширьте "+
				"распознаватель: молчание здесь означало бы «посадка проверена»")
	}
}

func quenchNoteBlank() string {
	return "отказ поставщика дескриптора ПОГАШЕН в `_`: страж собран, исполняется и " +
		"не может ничего остановить. Стоящая рядом проверка `if err != nil` (если она " +
		"есть) судит переменную, объявленную выше по телу, а не отказ стража"
}

func quenchNoteNoExit(errName string) string {
	return fmt.Sprintf(
		"отказ поставщика дескриптора принят в %q, но до ОСТАНОВКИ не доводится: ни "+
			"проверки, ведущей к выходу (`return`/`log.Fatal`/`os.Exit`/`panic`), ни "+
			"передачи наверх. Страж исполняется и не может ничего остановить", errName)
}

// assignedErrName — имя, в которое принят ОТКАЗ (последняя величина результата).
// Пустая строка означает, что отказ никуда не принят: `_`, либо результат
// разобран не полностью.
func assignedErrName(as *ast.AssignStmt) string {
	if len(as.Lhs) < 2 {
		return ""
	}
	id, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !ok || id.Name == "_" {
		return ""
	}
	return id.Name
}

// ── ПЕРВОЕ СОБЫТИЕ ПОСЛЕ ВЫЗОВА, А НЕ ЛЮБОЕ НИЖЕ ПО ТЕЛУ ───────────────────
//
// ПОЧЕМУ ПОРЯДОК НЕСУЩИЙ. Первая редакция этой оси искала ЛЮБУЮ развилку по
// имени отказа где угодно ниже по телу и не спрашивала, не перезаписано ли к
// тому моменту само имя. В композиционном корне на две-три сотни строк,
// переиспользующем `err`, такая развилка есть ВСЕГДА — то есть ответ «доходит»
// был тождественно истинным и от наличия проверки у стража не зависел.
//
// Замер, из-за которого редакция развёрнута: снятие блока проверки СРАЗУ ЗА
// вызовом оставляло гейт зелёным у пяти живых компонентов из шести, при
// неизменной переписи. Краснел единственный, у которого развилка по `err` после
// вызова ровно одна. Перепись развилок (предикат — `err != nil` от строки
// вызова до конца тела): 3 · 4 · 6 · 3 · 3 · 1.
//
// ПОЧЕМУ ИНЪЕКЦИЯ ЭТОГО НЕ ПОКАЗАЛА. Все её фикстуры были синтетические, на
// пять-восемь строк, с ОДНИМ вызовом и одним упоминанием имени. Это
// `testing.md` §«Гейт на класс», п.3 дословно: предпосылка верна ОТНОСИТЕЛЬНО
// ПОПУЛЯЦИИ, на которой гейт писался, и ложна на настоящей. Узкая популяция
// предпосылку не подтверждает — она её СКРЫВАЕТ. Поэтому у каждой стороны
// инъекции теперь есть близнец из настоящей популяции: вызывающий, у которого
// ниже по телу стоит ЕЩЁ ОДНА развилка по тому же имени.

// Вид события, решающего судьбу принятого отказа.
const (
	// reachEventOverwrite — в имя отказа записали другое значение: то, что было
	// принято от поставщика, до проверки уже не доживёт.
	reachEventOverwrite = iota
	// reachEventCheck — развилка по имени отказа, ведущая к остановке.
	reachEventCheck
)

// reachEvent — событие с позицией: порядок здесь и есть предмет.
type reachEvent struct {
	pos  token.Pos
	kind int
}

// refusalReachesExit — доводится ли отказ, принятый в errName, до остановки.
//
// РЕШАЕТ ПЕРВОЕ событие после вызова, а не наличие какого-нибудь события ниже:
// проверка → доходит; перезапись имени → погашен. Событий после вызова нет
// вовсе — тоже погашен.
//
// ЗАКОННЫЕ ФОРМЫ ПРОВЕРКИ, каждая встречается либо в дереве, либо в фикстурах:
//
//	if err != nil { return err }                 — проверка с выходом
//	if err != nil { return fmt.Errorf("…: %w") } — она же с обёрткой
//	if err != nil { log.Fatalf(…) }              — остановка процесса
//	if err != nil { os.Exit(1) } / panic(err)    — она же другими словами
//	return err                                   — голая передача наверх
//	return d, err                                — она же в паре
func refusalReachesExit(body *ast.BlockStmt, errName string, after token.Pos) bool {
	var events []reachEvent
	collectReachEvents(body, errName, after, false, &events)
	sort.Slice(events, func(i, j int) bool { return events[i].pos < events[j].pos })
	for _, e := range events {
		if e.pos <= after {
			continue
		}
		return e.kind == reachEventCheck
	}
	return false
}

// collectReachEvents собирает события по телу с учётом ОБЛАСТИ ВИДИМОСТИ.
//
// ЧТО ЗДЕСЬ ЗНАЧИТ ПЕРЕКРЫТИЕ. `:=` во ВЛОЖЕННОЙ области объявляет ДРУГУЮ
// переменную того же имени: развилка по ней о нашем отказе не говорит ничего и
// за проверку не считается, а запись в неё нашего значения не трогает. `=` на
// любой глубине пишет в нашу — это перезапись. `:=` в области, где наше имя и
// объявлено (оператор, содержащий сам вызов), перекрытием НЕ является: это и
// есть объявление нашей переменной.
//
// ТЕЛА ФУНКЦИОНАЛЬНЫХ ЛИТЕРАЛОВ НЕ ОБХОДЯТСЯ. Они исполняются не здесь и не в
// этом порядке; считать запись внутри отложенного обработчика перезаписью,
// случившейся до проверки, значило бы краснеть на исправном коде. Та же
// граница, что у `blockTerminates`.
func collectReachEvents(n ast.Node, errName string, after token.Pos, shadowed bool, out *[]reachEvent) {
	switch st := n.(type) {
	case nil:
		return

	case *ast.FuncLit:
		return

	case *ast.BlockStmt:
		// НАША область — та, в чьём перечне операторов лежит присваивание,
		// содержащее сам вызов. Только в ней `:=` по правилу языка пишет в уже
		// объявленное имя; во всякой вложенной он объявляет ДРУГУЮ переменную.
		own := false
		for _, inner := range st.List {
			if as, ok := inner.(*ast.AssignStmt); ok && as.Pos() <= after && after <= as.End() {
				own = true
				break
			}
		}
		sh := shadowed
		for _, inner := range st.List {
			as, ok := inner.(*ast.AssignStmt)
			if !ok || !assignWritesIdent(as, errName) {
				collectReachEvents(inner, errName, after, sh, out)
				continue
			}
			switch {
			case as.Pos() <= after && after <= as.End():
				// Это объявление нашей переменной — точка отсчёта, не событие.
			case sh:
				// Имя уже перекрыто: пишут в чужую переменную.
			case as.Tok == token.DEFINE && !own:
				// `:=` во вложенной области — перекрытие с этого места и ниже.
				sh = true
			default:
				*out = append(*out, reachEvent{pos: as.Pos(), kind: reachEventOverwrite})
			}
		}
		return

	case *ast.IfStmt:
		// РАЗВИЛКА, ЧЕЙ `Init` ПИШЕТ В ЭТО ЖЕ ИМЯ, ПРОВЕРКОЙ НЕ ЯВЛЯЕТСЯ — и это
		// не тонкость, а живой случай. Форма `if err = f(); err != nil { … }`
		// судит РЕЗУЛЬТАТ СВОЕГО вызова, а не принятый ранее отказ: к моменту
		// вычисления условия наше значение уже затёрто. По позиции же `if`
		// стоит ПЕРЕД своим `Init`, поэтому наивный порядок поставил бы проверку
		// раньше перезаписи и объявил отказ дошедшим.
		//
		// Замер, из-за которого ветка заведена: сплошная проба по всем шести
		// компонентам — снять блок проверки, посмотреть цвет — оставила один
		// зелёным ровно на этой форме. Пять из шести ничего бы не показали.
		inner := shadowed
		initWrites := false
		if as, ok := st.Init.(*ast.AssignStmt); ok && assignWritesIdent(as, errName) {
			initWrites = true
			if as.Tok == token.DEFINE {
				// Объявление в собственной области развилки — перекрытие.
				inner = true
			} else if !shadowed {
				// Перезапись отнесена к позиции САМОЙ развилки, а не её `Init`:
				// иначе порядок между ними решался бы разметкой исходника.
				*out = append(*out, reachEvent{pos: st.Pos(), kind: reachEventOverwrite})
			}
		}
		if !inner && !initWrites && exprMentionsIdent(st.Cond, errName) && ifBranchTerminates(st) {
			*out = append(*out, reachEvent{pos: st.Pos(), kind: reachEventCheck})
		}
		collectReachEvents(st.Body, errName, after, inner, out)
		collectReachEvents(st.Else, errName, after, inner, out)
		return

	case *ast.SwitchStmt:
		// Тот же класс у переключателя: `switch err = f(); { … }`.
		inner := shadowed
		if as, ok := st.Init.(*ast.AssignStmt); ok && assignWritesIdent(as, errName) {
			if as.Tok == token.DEFINE {
				inner = true
			} else if !shadowed {
				*out = append(*out, reachEvent{pos: st.Pos(), kind: reachEventOverwrite})
			}
		}
		collectReachEvents(st.Body, errName, after, inner, out)
		return

	case *ast.ForStmt:
		inner := shadowed
		if as, ok := st.Init.(*ast.AssignStmt); ok && assignWritesIdent(as, errName) {
			if as.Tok == token.DEFINE {
				inner = true
			} else if !shadowed {
				*out = append(*out, reachEvent{pos: st.Pos(), kind: reachEventOverwrite})
			}
		}
		collectReachEvents(st.Body, errName, after, inner, out)
		return

	case *ast.ReturnStmt:
		if !shadowed {
			for _, r := range st.Results {
				if exprMentionsIdent(r, errName) {
					*out = append(*out, reachEvent{pos: st.Pos(), kind: reachEventCheck})
					return
				}
			}
		}
		return

	case *ast.AssignStmt:
		if assignWritesIdent(st, errName) && (st.Pos() > after || after > st.End()) {
			if st.Tok == token.DEFINE && shadowed {
				return
			}
			*out = append(*out, reachEvent{pos: st.Pos(), kind: reachEventOverwrite})
		}
		return

	case *ast.DeclStmt:
		// `var err error` во вложенной области — тоже перекрытие. Собственных
		// событий не даёт; сам факт перекрытия учитывает обходчик блока выше
		// через `shadowed`, поэтому здесь достаточно молчания.
		return
	}

	// Прочие составные операторы — обход по вложенным телам с сохранением
	// перекрытия. Перечень закрыт: узел, не названный здесь, событий не даёт, и
	// это та же осторожность в сторону НАХОДКИ — отсутствие события означает
	// «проверки не нашлось», то есть погашено.
	switch st := n.(type) {
	case *ast.RangeStmt:
		collectReachEvents(st.Body, errName, after, shadowed, out)
	case *ast.TypeSwitchStmt:
		collectReachEvents(st.Body, errName, after, shadowed, out)
	case *ast.SelectStmt:
		collectReachEvents(st.Body, errName, after, shadowed, out)
	case *ast.CaseClause:
		for _, inner := range st.Body {
			collectReachEvents(inner, errName, after, shadowed, out)
		}
	case *ast.CommClause:
		for _, inner := range st.Body {
			collectReachEvents(inner, errName, after, shadowed, out)
		}
	case *ast.LabeledStmt:
		collectReachEvents(st.Stmt, errName, after, shadowed, out)
	}
}

// assignWritesIdent — пишет ли присваивание в идентификатор с этим именем.
func assignWritesIdent(as *ast.AssignStmt, name string) bool {
	for _, l := range as.Lhs {
		if id, ok := l.(*ast.Ident); ok && id.Name == name {
			return true
		}
	}
	return false
}

// ifBranchTerminates — останавливает ли ХОТЬ ОДНА ветка развилки. Обе стороны
// смотрятся намеренно: `if err == nil { … } else { return err }` законен.
func ifBranchTerminates(st *ast.IfStmt) bool {
	if blockTerminates(st.Body) {
		return true
	}
	if st.Else == nil {
		return false
	}
	return blockTerminates(st.Else)
}

// blockTerminates — есть ли в блоке остановка. Тела функциональных литералов
// НЕ считаются: `return` внутри замыкания выходит из замыкания, а не из
// процесса, и засчитывать его значило бы зеленеть на отложенном обработчике.
func blockTerminates(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found || x == nil {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		switch s := x.(type) {
		case *ast.ReturnStmt:
			found = true
		case *ast.ExprStmt:
			if isProcessStoppingCall(s.X) {
				found = true
			}
		}
		return !found
	})
	return found
}

// isProcessStoppingCall — вызов, останавливающий процесс: `panic`, `os.Exit`,
// `log.Fatal*` и родня по имени.
func isProcessStoppingCall(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "panic"
	case *ast.SelectorExpr:
		n := fn.Sel.Name
		return n == "Exit" || strings.HasPrefix(n, "Fatal")
	default:
		return false
	}
}

// exprMentionsIdent — упоминает ли выражение идентификатор с этим именем.
func exprMentionsIdent(e ast.Expr, name string) bool {
	hit := false
	ast.Inspect(e, func(n ast.Node) bool {
		if hit {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			hit = true
		}
		return !hit
	})
	return hit
}

// withdrawnAxis — объявлена ли ось изъятой с причиной, и какой.
//
// Распознаётся форма `servicecontract.NotApplicable[T]("причина")` и её вариант
// без явного параметра типа. Судится ВЫЗОВ, а не текст: имя `NotApplicable`
// встречается и в комментариях, и в строках разбора — по подстроке проверка
// краснела бы на собственном объяснении.
func withdrawnAxis(e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	fun := call.Fun
	// `NotApplicable[T](...)` — параметр типа приезжает узлом индекса.
	switch idx := fun.(type) {
	case *ast.IndexExpr:
		fun = idx.X
	case *ast.IndexListExpr:
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "servicecontract" || sel.Sel.Name != "NotApplicable" {
		return "", false
	}
	return firstStringOperand(call.Args), true
}

// firstStringOperand — начало причины, для текста находки. Причина обычно
// собрана конкатенацией нескольких строк; целиком она в находке не нужна —
// нужна узнаваемая голова.
func firstStringOperand(args []ast.Expr) string {
	if len(args) == 0 {
		return ""
	}
	var walk func(ast.Expr) string
	walk = func(e ast.Expr) string {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				return strings.Trim(x.Value, "`\"")
			}
		case *ast.BinaryExpr:
			if head := walk(x.X); head != "" {
				return head
			}
			return walk(x.Y)
		case *ast.ParenExpr:
			return walk(x.X)
		}
		return ""
	}
	head := walk(args[0])
	if len(head) > 60 {
		return head[:60] + "…"
	}
	return head
}

// postureAxisKnobPattern — по какому признаку узнаётся РУЧКА посадочной оси.
//
// Перечень закрыт и идёт от тех же имён, что знает распознаватель ручек
// (`postureKnobRe`): изъятие оси истекает ровно тогда, когда компонент завёл
// ручку, которую этот же обход уже видит. Разойтись с ним нельзя — иначе
// самоистечение объявляло бы предмет там, где перепись его не считает.
var postureAxisKnobPattern = map[string]*regexp.Regexp{
	"DBSSLMode":            regexp.MustCompile(`(?i)DB_SSLMODE|ssl-?mode`),
	"Forwarders":           regexp.MustCompile(`(?i)TRUSTED_FORWARDER_SANS|TRUST_ANY_FORWARDER|trusted-?forwarder|trust-any-forwarder`),
	"ForwarderKnobs.OptIn": regexp.MustCompile(`(?i)TRUST_ANY_FORWARDER|trust-any-forwarder`),
	"Mode":                 regexp.MustCompile(`(?i)AUTHN?_MODE|auth-?n?-?mode`),
}

// postureKnobFor — ручка этой оси, объявленная компонентом, либо пустая строка.
func postureKnobFor(axis string, knobs []string) string {
	re, ok := postureAxisKnobPattern[axis]
	if !ok {
		return ""
	}
	for _, k := range knobs {
		if re.MatchString(k) {
			return k
		}
	}
	return ""
}
