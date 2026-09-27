// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_memory_budget_test.go — ПРОФИЛЬ ЗОНТА, ОБЪЯВИВШИЙ ЧИСЛА БЮДЖЕТА
// ПОЛОСЫ ВХОДА, ОБЯЗАН ОБЪЯВИТЬ И ПРЕДЕЛ ПАМЯТИ КОНТЕЙНЕРА — ПО АРИФМЕТИКЕ
// СТРАЖА, А НЕ «ПОБОЛЬШЕ» (kacho#2709, Ф3-42, ID-PW-1 PWV-15.7).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Полоса входа паролем под `own` сверяет свой бюджет с пределом СРЕДЫ (cgroup
// контейнера), а не с настройкой, и отказывает в старте, когда предел меньше
// бюджета либо не наложен вовсе. Бюджет = ёмкость × память одной проверки на
// потолке формата + резерв.
//
// Признак на день заведения (kacho `origin/main` @ `ac33f733`):
//
//	git show origin/main:deploy/helm/umbrella/charts/kaname/values.yaml \
//	  | grep -A5 '^resources:'                      → limits.memory: 512Mi
//	… values.prod.yaml | python3 -c '…["kaname"].get("resources")'   → None
//	grep -n 'verifierCapacity\|memoryReserveBytes' values.prod.yaml  → 8 · 268435456
//
// То есть боевой профиль объявлял ДВА числа бюджета из трёх, а предел приходил
// умолчанием подчарта — 512 МиБ против бюджета 1280 МиБ. Под, переведённый на
// `own`, прошёл бы рендер и ВСЕХ стражей посадки и лёг бы стражем полосы: профиль
// обещал посадку, которой не будет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПОТОЛОК ЧИТАЕТСЯ У ПИНА, А НЕ ВЫПИСАН ЗДЕСЬ
//
// Своя копия арифметики разошлась бы со стражем молча — на первой же смене
// потолка формата записи пароля. Проба чарта самой службы (kaname#251,
// `deploy/own_lane_memory_ceiling_test.go`) зовёт `ValidateMemoryBudget` прямо;
// отсюда это НЕВОЗМОЖНО: пакет службы лежит под `internal/`, и модуль платформы
// его не импортирует by construction.
//
// Поэтому величина не копируется, а ЧИТАЕТСЯ — разбором таблицы форматов у
// пиненного модуля, по тому же правилу, что применяет сам страж: худший формат
// перечня, память на его потолке. Разбор СТРУКТУРНЫЙ (go/ast), а не по образцу:
// у записи есть и `Ceiling`, и `Floor`, оба несут тот же параметр, и текстовый
// поиск взял бы не ту карту. У каждой записи ТРИ различимых исхода (kacho#2726):
// потолок памяти argon2 прочитан · его ЗАКОННО нет (страж берёт постоянную
// bcrypt) · форма не читается — и тогда разбор ОТКАЗЫВАЕТ с координатой и
// причиной. Прежде третий исход сливался со вторым: потолок, записанный
// выражением, давал «потолка нет», бюджет падал до постоянной bcrypt, и предел
// 512Mi проходил зелёным. Имена поля, ключа и постоянной проба тоже не берёт на
// веру — сверяет их с телом правила стража. Молча устареть разбор не может, и
// это единственное, что отличает чтение источника истины от его копии.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА ВОПРОСА, И ВТОРОЙ НЕ ВАКУУМЕН СЕГОДНЯ
//
//	Р1  стенд на посадке `own` — предел обязан быть объявлен и обязан покрывать
//	    бюджет: там это условие СТАРТА;
//	Р2  стенд, объявивший ёмкость и резерв, — предел обязан покрывать бюджет,
//	    даже если стенд стоит на `external`. Два числа бюджета из трёх есть
//	    обещание, которое стенд не сможет сдержать в тот момент, когда его
//	    переведут; а переносить их в профиль заранее — решение приёмки Ф3 (Р3,
//	    Р11), а не случайность.
//
// Профилей на `own` сегодня может быть ноль — перепись печатает это числом,
// поэтому «находок нет» отличимо от «судить было нечего». Р2 предметен уже
// сейчас: ёмкость и резерв объявляет боевой профиль.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Она не утверждает, что предел ДОСТАТОЧЕН процессу под нагрузкой: это свойство
// прогона, и его судит замер. Она утверждает, что правило бюджета стража старта
// (`ValidateMemoryBudget` вместе с `ValidateCapacity`, которое оно зовёт первым)
// примет три числа профиля — ёмкость, резерв и предел, — то есть что профиль и
// страж говорят об одном числе. Отказы ДО арифметики (ёмкость непозитивна,
// резерв нулевой) проба моделирует у себя и потому сверяет модель с телом
// правила у пина (guardCapacityRuleIsModelled). Прочие ручки полосы (параметры
// формата записи пароля) она не судит.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// laneBudgetFacts — числа бюджета одного стенда, прочитанные из его цепочки.
type laneBudgetFacts struct {
	Stack    string
	Posture  string
	Capacity int64  // authn.login.verifierCapacity
	Reserve  uint64 // authn.login.memoryReserveBytes
	Limit    uint64 // resources.limits.memory, в байтах
	Declared bool   // объявлены ли ёмкость И резерв
	HasLimit bool   // объявлен ли предел памяти
}

// laneBudgetCensus — объём осмотренного: три величины, потому что «находок нет»
// обязано быть отличимо и от «стендов ноль», и от «на own ноль».
type laneBudgetCensus struct {
	Stacks        int
	Own           int
	DeclaringLane int
	PerCheckBytes uint64
}

func (c laneBudgetCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · из них на own %d · объявляют числа полосы %d · "+
		"память одной проверки на потолке %d байт", c.Stacks, c.Own, c.DeclaringLane, c.PerCheckBytes)
}

// judgeLaneMemoryBudget — НАХОДКИ по перечню стендов.
//
// Чистая функция: инъекция подаёт ей синтетический вход и не трогает ни дерева,
// ни кэша модулей.
func judgeLaneMemoryBudget(facts []laneBudgetFacts, perCheck uint64) ([]string, laneBudgetCensus) {
	var (
		findings []string
		census   = laneBudgetCensus{PerCheckBytes: perCheck}
	)
	for _, f := range facts {
		census.Stacks++
		own := f.Posture == "own"
		if own {
			census.Own++
		}
		if f.Declared {
			census.DeclaringLane++
		}
		// Стенд, не объявивший чисел полосы и стоящий не на `own`, предметом не
		// является: законный близнец, проверяется инъекцией.
		if !own && !f.Declared {
			continue
		}
		if own && !f.Declared {
			findings = append(findings, fmt.Sprintf(
				"стенд %s на посадке own: ёмкость и резерв полосы не объявлены — страж не с чем "+
					"сверять бюджет, и служба откажет в старте", f.Stack))
			continue
		}
		// Отказы ДО арифметики — в том же порядке, что у стража: правило бюджета
		// первым зовёт правило ёмкости, и оно отвергает обе величины, каждую своим
		// текстом. Предел судится и при них: оператору нужен весь перечень правок,
		// а не первая.
		rejected := false
		if f.Capacity <= 0 {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: ёмкость проверяющего объявлена непозитивной (%d) — страж отвергает такую "+
					"величину до всякой арифметики", f.Stack, f.Capacity))
			rejected = true
		}
		if f.Reserve == 0 {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: резерв нулевой — страж отвергает его до всякой арифметики "+
					"(«memory-reserve-bytes не задан»): резерв памяти процесса сверх проверок — "+
					"положительное число байт, даже когда предел покрывает бюджет и без него", f.Stack))
			rejected = true
		}
		if !f.HasLimit {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: `kaname.resources.limits.memory` не объявлен, а ёмкость (%d) и резерв (%d) "+
					"объявлены. Предел приносит СРЕДА, и без него страж полосы отказывает в старте: "+
					"«предел памяти средой не наложен — сверять не с чем». Два числа бюджета из трёх "+
					"есть обещание, которого стенд не сдержит",
				f.Stack, f.Capacity, f.Reserve))
			continue
		}
		if rejected {
			continue
		}
		need := uint64(f.Capacity)*perCheck + f.Reserve
		if need > f.Limit {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: бюджет полосы %d байт (ёмкость %d × %d байт на проверку + резерв %d) "+
					"ПРЕВЫШАЕТ предел памяти контейнера %d байт — под пройдёт рендер и всех стражей "+
					"посадки и ляжет стражем полосы (ID-PW-1 PWV-15.7). Поднимите предел до %d байт "+
					"либо уменьшите ёмкость",
				f.Stack, need, f.Capacity, perCheck, f.Reserve, f.Limit, need))
		}
	}
	return findings, census
}

// memoryPerVerificationAtCeiling — память ОДНОЙ проверки на потолке самого
// дорогого формата перечня, прочитанная у пиненного модуля.
//
// Повторяет правило стража дословно: по каждой записи берётся потолок памяти
// argon2, а у записи без него — постоянная bcrypt; из полученных выбирается
// худшее.
func memoryPerVerificationAtCeiling(t *testing.T, moduleDir string) uint64 {
	t.Helper()
	path := filepath.Join(moduleDir, "internal", "domain", "password_hash_format.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("таблица форматов пиненного модуля не разбирается (%s): %v.\n"+
			"Это «не выполнилось», а не «бюджет сходится»: непрочитанное утверждением не является", path, err)
	}
	reading, err := readFormatTable(file)
	if err != nil {
		t.Fatalf("%s: источник истины не прочитан — %v.\n"+
			"Это «не выполнилось», а не «бюджет сходится»: подставить на место непрочитанного "+
			"постоянную bcrypt значило бы уронить бюджет втрое и пропустить ровно тот предел, "+
			"ради которого проба заведена. Почини разбор, а не выписывай число рядом", path, err)
	}
	t.Logf("перепись таблицы форматов: %s", reading)
	return reading.PerCheck
}

// formatTableReading — что прочитано в таблице форматов пина.
type formatTableReading struct {
	PerCheck uint64 // худшая память одной проверки на потолке, байт
	Records  int    // записей перечня прочитано
	Argon2   int    // из них с потолком памяти argon2
	Bcrypt   int    // из них без него — ЗАКОННО: страж берёт постоянную bcrypt
}

func (r formatTableReading) String() string {
	return fmt.Sprintf("записей %d · с потолком памяти argon2 %d · по постоянной bcrypt %d · "+
		"худшее %d байт на проверку", r.Records, r.Argon2, r.Bcrypt, r.PerCheck)
}

// Имена, которыми правило стража (`MemoryPerVerificationAtCeilingBytes` у
// записи перечня) читает потолок. Проба не берёт их на веру: readFormatTable
// сверяет, что тело правила ссылается ровно на них, — переименуют поле или
// ключ, и «у записи нет потолка argon2» станет неотличимо от «потолок назван
// иначе», если модель здесь не устареет вслух.
const (
	guardRuleMethod    = "MemoryPerVerificationAtCeilingBytes"
	guardCeilingField  = "Ceiling"
	guardArgon2MemKey  = "CostParamArgon2Memory"
	guardBcryptConst   = "bcryptMemoryPerVerificationBytes"
	guardFormatsVar    = "passwordHashFormats"
	argon2MemoryUnitKi = 1024 // потолок памяти argon2 записан в КиБ
)

// readFormatTable — чтение таблицы форматов с ТРЕМЯ различимыми исходами у
// каждой записи: потолок памяти argon2 прочитан · его ЗАКОННО нет (карта
// потолка литералом без этого ключа — страж берёт постоянную bcrypt) · форма
// не читается (ошибка с координатой и причиной). Третий исход никогда не
// сливается со вторым (kacho#2726): иначе смена формы литерала при подъёме
// пина уронила бы бюджет до постоянной bcrypt молча.
func readFormatTable(file *ast.File) (formatTableReading, error) {
	var out formatTableReading
	if err := guardRuleReadsTheModelledNames(file); err != nil {
		return out, err
	}

	formats, ok := topLevelValue(file, guardFormatsVar)
	if !ok {
		return out, fmt.Errorf("перечень `%s` не найден", guardFormatsVar)
	}
	table, ok := formats.(*ast.CompositeLit)
	if !ok {
		return out, fmt.Errorf("`%s` объявлен не составным литералом (%T) — не прочитан", guardFormatsVar, formats)
	}

	for i, elt := range table.Elts {
		// Элемент среза вправе нести индекс: `0: {…}` — та же запись.
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			elt = kv.Value
		}
		rec, ok := elt.(*ast.CompositeLit)
		if !ok {
			return out, fmt.Errorf("запись %d перечня не составной литерал (%T) — не прочитана", i, elt)
		}
		out.Records++
		kib, present, err := argon2MemoryOf(rec, guardCeilingField)
		if err != nil {
			return out, fmt.Errorf("запись %d перечня: %w", i, err)
		}
		if !present {
			out.Bcrypt++
			continue
		}
		out.Argon2++
		if b := kib * argon2MemoryUnitKi; b > out.PerCheck {
			out.PerCheck = b
		}
	}
	if out.Records == 0 {
		return out, fmt.Errorf("перечень `%s` пуст — арифметику стража выводить не из чего", guardFormatsVar)
	}

	if out.Bcrypt > 0 {
		// Постоянная нужна ровно тогда, когда есть запись без потолка argon2:
		// читать её «на всякий случай» значило бы краснеть на пине, где bcrypt
		// снят вместе с постоянной.
		lit, ok := topLevelValue(file, guardBcryptConst)
		if !ok {
			return out, fmt.Errorf("записей без потолка памяти argon2 %d, а постоянной `%s` нет — "+
				"бюджет таких записей не прочитан", out.Bcrypt, guardBcryptConst)
		}
		v, err := uintLiteral(lit)
		if err != nil {
			return out, fmt.Errorf("постоянная `%s`: %w", guardBcryptConst, err)
		}
		if v > out.PerCheck {
			out.PerCheck = v
		}
	}
	return out, nil
}

// argon2MemoryOf — потолок памяти argon2 (КиБ) из названной карты записи.
//
// present=false, err=nil — ЗАКОННО нет: карта потолка записана литералом, и
// ключа памяти argon2 в ней нет. Всё, чего читатель не знает, — ошибка:
// позиционная запись, записи без поля потолка, карта не литералом, ключ не
// именем, значение не целым литералом.
func argon2MemoryOf(rec *ast.CompositeLit, field string) (kib uint64, present bool, err error) {
	var ceiling ast.Expr
	for _, e := range rec.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return 0, false, fmt.Errorf("запись позиционная — поле `%s` не прочитано", field)
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == field {
			ceiling = kv.Value
		}
	}
	if ceiling == nil {
		// Норма пина требует потолок у КАЖДОЙ записи (Validate: «ceiling …:
		// required»), поэтому запись без поля — не «потолка argon2 нет», а
		// форма, которой читатель не знает.
		return 0, false, fmt.Errorf("у записи нет поля `%s` — потолок не прочитан", field)
	}
	m, ok := ceiling.(*ast.CompositeLit)
	if !ok {
		return 0, false, fmt.Errorf("поле `%s` не литерал карты (%T) — не прочитано", field, ceiling)
	}
	for _, me := range m.Elts {
		mkv, ok := me.(*ast.KeyValueExpr)
		if !ok {
			return 0, false, fmt.Errorf("элемент карты `%s` без ключа — не прочитан", field)
		}
		mk, ok := mkv.Key.(*ast.Ident)
		if !ok {
			return 0, false, fmt.Errorf("ключ карты `%s` не именем постоянной (%T) — не прочитан: "+
				"это может быть и `%s`", field, mkv.Key, guardArgon2MemKey)
		}
		if mk.Name != guardArgon2MemKey {
			continue
		}
		v, err := uintLiteral(mkv.Value)
		if err != nil {
			return 0, false, fmt.Errorf("`%s.%s`: %w", field, guardArgon2MemKey, err)
		}
		return v, true, nil
	}
	return 0, false, nil
}

// uintLiteral — целый литерал в любой законной форме Go (десятичной,
// шестнадцатеричной, восьмеричной, двоичной, с разделителями `_`). Выражение и
// имя постоянной — не литерал: их величину знает компилятор, а не разбор, и
// читатель отказывает, а не угадывает.
func uintLiteral(e ast.Expr) (uint64, error) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, fmt.Errorf("значение не целый литерал, а %s — не прочитано", exprKind(e))
	}
	v, err := strconv.ParseUint(lit.Value, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("литерал %s не читается как целое (%v) — не прочитан", lit.Value, err)
	}
	return v, nil
}

func exprKind(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return "литерал " + v.Value
	case *ast.Ident:
		return "имя " + v.Name
	case *ast.BinaryExpr:
		return "выражение " + v.Op.String()
	default:
		return fmt.Sprintf("%T", e)
	}
}

// topLevelValue — значение объявления верхнего уровня файла по имени.
func topLevelValue(file *ast.File, name string) (ast.Expr, bool) {
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name == name && i < len(vs.Values) {
					return vs.Values[i], true
				}
			}
		}
	}
	return nil, false
}

// guardRuleReadsTheModelledNames — ПРЕДПОСЫЛКА: правило стража у записи
// перечня читает потолок тем полем и тем ключом, которые моделирует проба, и
// падает на ту же постоянную bcrypt.
func guardRuleReadsTheModelledNames(file *ast.File) error {
	var body *ast.BlockStmt
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == guardRuleMethod {
			body = fd.Body
		}
	}
	if body == nil {
		return fmt.Errorf("правило стража `%s` у записи перечня не найдено — модель пробы не с чем сверить",
			guardRuleMethod)
	}
	seen := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			seen[v.Sel.Name] = true
		case *ast.Ident:
			seen[v.Name] = true
		}
		return true
	})
	for _, name := range []string{guardCeilingField, guardArgon2MemKey, guardBcryptConst} {
		if !seen[name] {
			return fmt.Errorf("правило стража `%s` не ссылается на `%s` — модель пробы устарела, и "+
				"«потолка argon2 нет» стало бы неотличимо от «потолок назван иначе»", guardRuleMethod, name)
		}
	}
	return nil
}

// Имена, которыми правило стража судит числа полосы ДО арифметики. Проба их не
// берёт на веру: guardCapacityRuleIsModelled сверяет модель judgeLaneMemoryBudget
// с телом правила у пина.
const (
	guardCapacityMethod = "ValidateCapacity"
	guardBudgetMethod   = "ValidateMemoryBudget"
	guardCapacityField  = "VerifierCapacity"
	guardReserveField   = "MemoryReserveBytes"
)

// guardCapacityRuleIsModelled — ПРЕДПОСЫЛКА модели отказов до арифметики:
// правило бюджета зовёт правило ёмкости, а оно отвергает ёмкость `<= 0` и
// резерв `== 0` — ровно то, что моделирует judgeLaneMemoryBudget. Порог вместо
// нуля, иной знак или правило, которое больше не зовут, — отказ с именем
// расхождения: модель, разошедшаяся со стражем, судила бы не его.
func guardCapacityRuleIsModelled(file *ast.File) error {
	bodies := map[string]*ast.BlockStmt{}
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Body != nil {
			bodies[fd.Name.Name] = fd.Body
		}
	}
	capacity, ok := bodies[guardCapacityMethod]
	if !ok {
		return fmt.Errorf("правило стража `%s` не найдено — отказы до арифметики сверять не с чем",
			guardCapacityMethod)
	}
	budget, ok := bodies[guardBudgetMethod]
	if !ok {
		return fmt.Errorf("правило бюджета стража `%s` не найдено — сверять не с чем", guardBudgetMethod)
	}
	if !callsMethod(budget, guardCapacityMethod) {
		return fmt.Errorf("`%s` не зовёт `%s` — отказы до арифметики, которые моделирует проба, "+
			"стражем бюджета не исполняются", guardBudgetMethod, guardCapacityMethod)
	}
	for _, rule := range []struct {
		field string
		op    token.Token
	}{{guardCapacityField, token.LEQ}, {guardReserveField, token.EQL}} {
		if !comparesFieldWithZero(capacity, rule.field, rule.op) {
			return fmt.Errorf("правило стража `%s` не судит `%s %s 0` — модель пробы устарела, и её "+
				"отказ до арифметики разошёлся бы с отказом стража", guardCapacityMethod, rule.field, rule.op)
		}
	}
	return nil
}

// callsMethod — есть ли в теле вызов `<x>.<name>(…)`.
func callsMethod(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// comparesFieldWithZero — есть ли в теле сравнение `<x>.<field> <op> 0`.
func comparesFieldWithZero(body *ast.BlockStmt, field string, op token.Token) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != op {
			return !found
		}
		sel, okX := be.X.(*ast.SelectorExpr)
		lit, okY := be.Y.(*ast.BasicLit)
		if okX && okY && sel.Sel.Name == field && lit.Kind == token.INT && lit.Value == "0" {
			found = true
		}
		return !found
	})
	return found
}

// capacityRuleOfTheGuard — сверка модели отказов до арифметики с правилом у пина.
func capacityRuleOfTheGuard(t *testing.T, moduleDir string) {
	t.Helper()
	path := filepath.Join(moduleDir, "internal", "apps", "kaname", "config", "login_lane.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("правило стража пиненного модуля не разбирается (%s): %v.\n"+
			"Это «не выполнилось», а не «бюджет сходится»", path, err)
	}
	if err := guardCapacityRuleIsModelled(file); err != nil {
		t.Fatalf("%s: %v.\nЭто «не выполнилось», а не «бюджет сходится»: почини модель пробы "+
			"по правилу стража, а не наоборот", path, err)
	}
}

// TestOwnLaneMemoryBudget_ProfilesDeclareALimitThatTheGuardAccepts — сверка по
// дереву: перечень стендов из единственной таблицы, потолок формата из пина.
func TestOwnLaneMemoryBudget_ProfilesDeclareALimitThatTheGuardAccepts(t *testing.T) {
	moduleDir := kanameModuleDir(t, "..")
	perCheck := memoryPerVerificationAtCeiling(t, moduleDir)
	capacityRuleOfTheGuard(t, moduleDir)
	facts := readLaneBudgetFacts(t)
	if len(facts) == 0 {
		t.Fatal("таблица стендов пуста: обход беспредметен, и «находок нет» здесь означало бы " +
			"«сверять было не с чем»")
	}

	findings, census := judgeLaneMemoryBudget(facts, perCheck)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		if !f.Declared && f.Posture != "own" {
			continue
		}
		t.Logf("  %s: posture=%s ёмкость=%d резерв=%d предел=%d бюджет=%d",
			f.Stack, f.Posture, f.Capacity, f.Reserve, f.Limit,
			uint64(maxInt64(f.Capacity, 0))*perCheck+f.Reserve)
	}
	if census.DeclaringLane == 0 && census.Own == 0 {
		t.Fatal("ни один стенд не объявил ни посадки own, ни чисел полосы — вердикт беспредметен")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readLaneBudgetFacts — числа из дерева: цепочка каждого стенда накладывается
// слева направо поверх базовых значений подчарта, ровно как её получает helm.
func readLaneBudgetFacts(t *testing.T) []laneBudgetFacts {
	t.Helper()
	stacksTbl := deployStacks(t)

	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]laneBudgetFacts, 0, len(names))
	for _, name := range names {
		// Базовые значения подчарта читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту НА МЕСТЕ, и одна общая карта умолчаний протекала бы из
		// стенда в стенд — ровно так этот предикат сперва и соврал, приписав
		// профилю `prorobotech` числа полосы, объявленные боевым.
		declared := map[string]any{"kaname": readYAML(t, filepath.Join(kanameSubchart(t), "values.yaml"))}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		f := laneBudgetFacts{Stack: name}
		if p, ok := lookup(declared, "kaname", "config", "authn", "identityProvider"); ok {
			f.Posture = fmt.Sprint(p)
		}
		cap, okCap := lookup(declared, "kaname", "config", "authn", "login", "verifierCapacity")
		res, okRes := lookup(declared, "kaname", "config", "authn", "login", "memoryReserveBytes")
		if okCap && okRes {
			f.Declared = true
			f.Capacity = asInt64(t, name, "verifierCapacity", cap)
			reserve, err := declaredReserveBytes(res)
			if err != nil {
				t.Fatalf("стенд %s: `memoryReserveBytes` %v", name, err)
			}
			f.Reserve = reserve
		}
		if lim, ok := lookup(declared, "kaname", "resources", "limits", "memory"); ok {
			// Величина читается КОЛИЧЕСТВОМ Kubernetes, а не строкой: «1280Mi» и
			// «1342177280» — одно и то же для среды и разное для глаза. Десятичной
			// мантиссы («1.28G») разбор не знает и отказывает вслух.
			bytes, err := parseMemoryQuantity(fmt.Sprint(lim))
			if err != nil {
				t.Fatalf("стенд %s: `kaname.resources.limits.memory` = %v не прочитан (%v): разбор знает "+
					"целое число с суффиксом Ki|Mi|Gi|Ti|k|M|G|T. Это либо не количество Kubernetes, "+
					"либо его форма, которой разбор не знает, — «не выполнилось», а не «предел мал»",
					name, lim, err)
			}
			f.HasLimit = true
			f.Limit = bytes
		}
		out = append(out, f)
	}
	return out
}

func asInt64(t *testing.T, stack, key string, v any) int64 {
	t.Helper()
	n, err := declaredInt(v)
	if err != nil {
		t.Fatalf("стенд %s: `%s` %v", stack, key, err)
	}
	return n
}

// declaredInt — целое число значений YAML. Значение, которое не есть то целое,
// что объявлено, — ошибка, а не усечённая величина.
func declaredInt(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case float64:
		// Дробное не усекается: усечённое, оно стало бы другим бюджетом, чем
		// объявленный, а служба читает целое и такую величину отвергнет.
		if n != math.Trunc(n) || n > math.MaxInt64 || n < math.MinInt64 {
			return 0, fmt.Errorf("= %v не целое — не прочитано", n)
		}
		return int64(n), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("= %q не число (%v)", n, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("= %v непонятного вида %T", v, v)
	}
}

// declaredReserveBytes — резерв памяти полосы в байтах. Отрицательный не
// прижимается к нулю: прижатый, он дал бы бюджет МЕНЬШЕ объявленного —
// «не прочитано» под видом числа.
func declaredReserveBytes(v any) (uint64, error) {
	n, err := declaredInt(v)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("= %d отрицателен — байтами не читается, и служба такую величину не "+
			"примет; сверять бюджет не с чем", n)
	}
	return uint64(n), nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// parseMemoryQuantity — количество памяти Kubernetes в байтах.
//
// Разбор ФОРМАТА, а не арифметика стража: суффиксы — часть контракта Kubernetes,
// а не решение службы, и меняться вместе с потолком формата записи пароля они не
// могут. Сама арифметика по-прежнему читается у пина (memoryPerVerificationAtCeiling),
// и вот её копировать было бы нельзя.
//
// Библиотека Kubernetes сюда не тянется ради двадцати строк: `k8s.io/apimachinery`
// модулем платформы не требуется, и заводить ребро сборки ради разбора суффикса
// значило бы платить за это каждой сборке дерева.
func parseMemoryQuantity(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("пустое количество")
	}
	suffixes := []struct {
		suffix string
		mult   uint64
	}{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
		{"k", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12},
	}
	mult := uint64(1)
	num := s
	for _, sf := range suffixes {
		if strings.HasSuffix(s, sf.suffix) {
			mult = sf.mult
			num = strings.TrimSuffix(s, sf.suffix)
			break
		}
	}
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("количество %q: %w", s, err)
	}
	return n * mult, nil
}
