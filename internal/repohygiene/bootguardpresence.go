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

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
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
	// literalFields — координаты посадочных полей, чьё значение НЕ выведено из
	// конфигурации: `<компонент>` → перечень «поле (файл:строка) = <выражение>».
	literalFields map[string][]string
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
		knobs:         map[string][]string{},
		accepts:       map[string]string{},
		discards:      map[string]string{},
		literalFields: map[string][]string{},
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
				}
			}
			return true
		})
	}
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
func scanContractRefusalWitness(dir string) (contractWitness, error) {
	res := contractWitness{axes: map[string]bool{}}
	fset := token.NewFileSet()
	// Файлы перечисляются по образцу имени, а не `parser.ParseDir`: тот объявлен
	// устаревшим и группирует файлы по пакетам, что здесь не нужно — вопрос
	// задаётся к ТЕЛУ функции, а не к пакету.
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return res, fmt.Errorf("перечень проб %s: %w", dir, err)
	}
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
