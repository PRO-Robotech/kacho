// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verifier_capacity_floor_test.go — СТЕНД, СОБИРАЮЩИЙ СВОЮ ЦЕРЕМОНИЮ, ОБЯЗАН
// ОБЪЯВИТЬ ЁМКОСТЬ ПРОВЕРЯЮЩЕГО НЕ НИЖЕ ПОЛА, КОТОРЫЙ СТАВИТ ПИНЕННАЯ СЛУЖБА
// (kacho#3022).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Проверяющий секретов — общий у полосы входа паролем и у сверки секрета
// клиента на токен-эндпоинте. Сверке церемонии служба отдаёт ДОЛЮ ёмкости и
// отказывает в старте, когда доля пуста: сборка адаптера секретов отвечает
// «declare a capacity of at least 2». Правило стоит в пине
// (`internal/ceremonyport/client_secrets.go`, `NewClientSecrets`) и действует
// ровно тогда, когда церемония собрана (`AuthNConfig.CeremonyAssembled`).
//
// Признак на день заведения: профиль a8f60d объявил `verifierCapacity: 1`
// (сузив её под предел памяти контейнера), пробы каталога пол не читали, и
// конвейер был зелёным при службе, которая на этом стенде не стартует.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПОЛ ЧИТАЕТСЯ У ПИНА, А НЕ ВЫПИСАН ЗДЕСЬ
//
// Тем же доводом, что потолок памяти проверки (own_lane_memory_budget_test.go):
// пакет службы лежит под `internal/`, импортировать его отсюда нельзя, а число,
// выписанное рядом, разошлось бы с правилом молча на первой смене доли. Поэтому
// пол ВЫВОДИТСЯ разбором правила: в теле `NewClientSecrets` ищется ровно одно
// сравнение вида `<x>.Capacity()[ / D] < M` (или `<= M`), и пол — наименьшая
// ёмкость, которую оно пропускает. Нуль таких сравнений или больше одного —
// отказ разбора с координатой, а не «пола нет»: непрочитанное утверждением не
// является. Предпосылки (условие, при котором правило действует, и что сборка
// церемонии его зовёт) тоже сверяются с телом у пина.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Не утверждает, что ёмкость ДОСТАТОЧНА под нагрузкой (это замер) и что она
// умещается в предел памяти контейнера (это own_lane_memory_budget_test.go —
// обе пробы вместе задают коридор величины). Стенд без церемонии предметом не
// является: правило доли там не исполняется, и это законный близнец.
package deploy_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// Имена правила пола у пина. Проба не берёт их на веру: разбор отказывает,
// когда функции нет либо в ней нет ровно одного сравнения ёмкости.
const (
	floorRuleFile       = "internal/ceremonyport/client_secrets.go"
	floorRuleFunc       = "NewClientSecrets"
	floorCapacityMethod = "Capacity"

	ceremonyPredicateFile   = "internal/apps/kaname/config/client_token.go"
	ceremonyPredicateMethod = "CeremonyAssembled"
	ceremonyAssemblyFile    = "cmd/kaname/ceremony.go"
)

// Ключи профиля: ёмкость проверяющего и включатель эндпоинта, собирающего
// церемонию (`authn.client-token.enabled` в настройках службы).
var (
	verifierCapacityKey = []string{"kaname", "config", "authn", "login", "verifierCapacity"}
	ceremonyEnabledKey  = []string{"kaname", "config", "authn", "clientToken", "enabled"}
)

// capacityFloorRule — пол, выведенный из правила пина, и где он прочитан.
type capacityFloorRule struct {
	Floor int64  // наименьшая ёмкость, которую правило пропускает
	Where string // координата сравнения у пина
	Expr  string // само сравнение, как записано
}

// readCapacityFloorRule — вывод пола из тела `NewClientSecrets`.
func readCapacityFloorRule(fset *token.FileSet, file *ast.File) (capacityFloorRule, error) {
	var body *ast.BlockStmt
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == floorRuleFunc && fd.Body != nil {
			body = fd.Body
		}
	}
	if body == nil {
		return capacityFloorRule{}, fmt.Errorf("функция `%s` не найдена — правило пола не с чем читать", floorRuleFunc)
	}
	var (
		rules []capacityFloorRule
		errs  []error
	)
	ast.Inspect(body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || (be.Op != token.LSS && be.Op != token.LEQ) {
			return true
		}
		divisor, isCapacity, err := capacityOperand(be.X)
		if !isCapacity {
			return true
		}
		where := fset.Position(be.Pos()).String()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
			return false
		}
		bound, err := uintLiteral(be.Y)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: правая часть сравнения ёмкости: %w", where, err))
			return false
		}
		// c/D < M  ⇔  c < D·M  (целое деление, c ≥ 0) ⇒ пол D·M;
		// c/D ≤ M  ⇔  c/D < M+1                         ⇒ пол D·(M+1).
		m := int64(bound)
		if be.Op == token.LEQ {
			m++
		}
		rules = append(rules, capacityFloorRule{Floor: divisor * m, Where: where, Expr: exprText(be)})
		return false
	})
	if len(errs) > 0 {
		return capacityFloorRule{}, errors.Join(errs...)
	}
	switch len(rules) {
	case 0:
		return capacityFloorRule{}, fmt.Errorf("в `%s` нет сравнения `<x>.%s()[ / D] < M` — форма правила пола "+
			"сменилась либо правило снято; пола «нет» отсюда не следует", floorRuleFunc, floorCapacityMethod)
	case 1:
		return rules[0], nil
	default:
		return capacityFloorRule{}, fmt.Errorf("в `%s` сравнений ёмкости %d (%v) — какое из них пол, разбор не "+
			"угадывает", floorRuleFunc, len(rules), rules)
	}
}

// capacityOperand — левая часть сравнения: `<x>.Capacity()` либо
// `<x>.Capacity() / D` с целым литералом D. isCapacity=false — сравнение не о
// ёмкости и предметом не является; err — о ёмкости, но в форме, которой
// разбор не знает.
func capacityOperand(e ast.Expr) (divisor int64, isCapacity bool, err error) {
	if isCapacityCall(e) {
		return 1, true, nil
	}
	be, ok := e.(*ast.BinaryExpr)
	if !ok || !isCapacityCall(be.X) {
		if mentionsCapacityCall(e) {
			return 0, true, fmt.Errorf("ёмкость входит в сравнение выражением %s, которого разбор не знает", exprKind(e))
		}
		return 0, false, nil
	}
	if be.Op != token.QUO {
		return 0, true, fmt.Errorf("ёмкость входит в сравнение операцией %s, а разбор знает только деление", be.Op)
	}
	d, err := uintLiteral(be.Y)
	if err != nil {
		return 0, true, fmt.Errorf("делитель ёмкости: %w", err)
	}
	if d == 0 {
		return 0, true, fmt.Errorf("делитель ёмкости нулевой")
	}
	return int64(d), true, nil
}

func isCapacityCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == floorCapacityMethod
}

func mentionsCapacityCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if ex, ok := n.(ast.Expr); ok && isCapacityCall(ex) {
			found = true
		}
		return !found
	})
	return found
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		return exprText(v.X) + " " + v.Op.String() + " " + exprText(v.Y)
	case *ast.BasicLit:
		return v.Value
	case *ast.CallExpr:
		return exprText(v.Fun) + "()"
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.Ident:
		return v.Name
	default:
		return fmt.Sprintf("%T", e)
	}
}

// ceremonyPredicateIsTheEnabledKnob — ПРЕДПОСЫЛКА: церемония собрана ровно
// тогда, когда включён эндпоинт (`return a.ClientToken.Enabled`). Иное тело —
// модель пробы устарела: она судила бы не те стенды.
func ceremonyPredicateIsTheEnabledKnob(file *ast.File) error {
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Name.Name != ceremonyPredicateMethod || fd.Body == nil {
			continue
		}
		if len(fd.Body.List) != 1 {
			return fmt.Errorf("`%s` — не одно выражение возврата; модель «церемония = включён эндпоинт» устарела",
				ceremonyPredicateMethod)
		}
		ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return fmt.Errorf("`%s` не возвращает одно значение — модель устарела", ceremonyPredicateMethod)
		}
		if got := exprText(ret.Results[0]); !isSelectorChain(ret.Results[0], "ClientToken", "Enabled") {
			return fmt.Errorf("`%s` возвращает %s, а не `<x>.ClientToken.Enabled` — модель пробы устарела",
				ceremonyPredicateMethod, got)
		}
		return nil
	}
	return fmt.Errorf("предикат `%s` не найден — условие действия пола не с чем сверить", ceremonyPredicateMethod)
}

func isSelectorChain(e ast.Expr, names ...string) bool {
	for i := len(names) - 1; i >= 0; i-- {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != names[i] {
			return false
		}
		e = sel.X
	}
	return true
}

// ceremonyAssemblyCallsTheFloor — ПРЕДПОСЫЛКА: функция сборки церемонии
// судит её предикатом и зовёт правило пола. Без этого пол — правило, которое
// никто не исполняет, и проба судила бы мёртвую величину.
func ceremonyAssemblyCallsTheFloor(file *ast.File) error {
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if callsMethod(fd.Body, ceremonyPredicateMethod) && callsMethod(fd.Body, floorRuleFunc) {
			return nil
		}
	}
	return fmt.Errorf("ни одна функция не зовёт и `%s`, и `%s` — сборка церемонии разошлась с моделью пробы",
		ceremonyPredicateMethod, floorRuleFunc)
}

func parsePinFile(t *testing.T, fset *token.FileSet, moduleDir, rel string) *ast.File {
	t.Helper()
	path := filepath.Join(moduleDir, filepath.FromSlash(rel))
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("файл пиненного модуля не разбирается (%s): %v.\n"+
			"Это «не выполнилось», а не «ёмкость сходится»", path, err)
	}
	return file
}

// capacityFloorOfPin — пол у пиненного модуля вместе с проверкой предпосылок.
func capacityFloorOfPin(t *testing.T, moduleDir string) capacityFloorRule {
	t.Helper()
	fset := token.NewFileSet()
	if err := ceremonyPredicateIsTheEnabledKnob(parsePinFile(t, fset, moduleDir, ceremonyPredicateFile)); err != nil {
		t.Fatalf("%s: %v", ceremonyPredicateFile, err)
	}
	if err := ceremonyAssemblyCallsTheFloor(parsePinFile(t, fset, moduleDir, ceremonyAssemblyFile)); err != nil {
		t.Fatalf("%s: %v", ceremonyAssemblyFile, err)
	}
	rule, err := readCapacityFloorRule(fset, parsePinFile(t, fset, moduleDir, floorRuleFile))
	if err != nil {
		t.Fatalf("%s: правило пола не прочитано — %v.\nЭто «не выполнилось», а не «ёмкость сходится»: "+
			"почини разбор по правилу пина, а не выписывай число рядом", floorRuleFile, err)
	}
	return rule
}

// capacityFloorFacts — что объявил один стенд.
type capacityFloorFacts struct {
	Stack       string
	Ceremony    bool   // церемония собрана: эндпоинт включён
	Declared    bool   // ёмкость объявлена
	Capacity    int64  // её величина
	Coordinate  string // профиль:строка, где величина объявлена последней
	EnabledFrom string // профиль, последним задавший включатель
}

type capacityFloorCensus struct {
	Stacks, Ceremony, Judged int
	Floor                    int64
}

func (c capacityFloorCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · с собранной церемонией %d · судимо по полу %d · пол %d",
		c.Stacks, c.Ceremony, c.Judged, c.Floor)
}

// judgeVerifierCapacityFloor — НАХОДКИ. Чистая функция: инъекция подаёт ей
// синтетический вход, не трогая ни дерева, ни кэша модулей.
func judgeVerifierCapacityFloor(facts []capacityFloorFacts, floor int64) ([]string, capacityFloorCensus) {
	var (
		findings []string
		census   = capacityFloorCensus{Floor: floor}
	)
	for _, f := range facts {
		census.Stacks++
		if !f.Ceremony {
			continue // правило доли не исполняется — законный близнец
		}
		census.Ceremony++
		census.Judged++
		if !f.Declared {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: церемония собрана (включатель — %s), а ёмкость проверяющего "+
					"`kaname.config.authn.login.verifierCapacity` не объявлена ни одним профилем цепочки — "+
					"проверяющего нет, и сборка церемонии откажет в старте; требуемый пол %d",
				f.Stack, f.EnabledFrom, floor))
			continue
		}
		if f.Capacity < floor {
			findings = append(findings, fmt.Sprintf(
				"стенд %s: ёмкость проверяющего %d (%s) НИЖЕ пола %d, который ставит пиненная служба при "+
					"собранной церемонии (включатель — %s): доля сверки секрета клиента пуста, и служба "+
					"откажет в старте. Объявите ёмкость не ниже %d и сверьте предел памяти контейнера "+
					"(own_lane_memory_budget_test.go)",
				f.Stack, f.Capacity, f.Coordinate, floor, f.EnabledFrom, floor))
		}
	}
	return findings, census
}

// TestVerifierCapacityFloor_EveryCeremonyStackDeclaresTheFloorOfThePin — сверка
// по дереву: стенды из единственной таблицы, пол из пина.
func TestVerifierCapacityFloor_EveryCeremonyStackDeclaresTheFloorOfThePin(t *testing.T) {
	rule := capacityFloorOfPin(t, kanameModuleDir(t, ".."))
	t.Logf("пол прочитан у пина: %d (%s: %s)", rule.Floor, rule.Where, rule.Expr)

	facts := readCapacityFloorFacts(t)
	if len(facts) == 0 {
		t.Fatal("таблица стендов пуста: обход беспредметен, и «находок нет» означало бы «судить было нечего»")
	}
	findings, census := judgeVerifierCapacityFloor(facts, rule.Floor)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		t.Logf("  %s: церемония=%t ёмкость=%d объявлена=%t (%s)", f.Stack, f.Ceremony, f.Capacity, f.Declared, f.Coordinate)
	}
	if census.Judged == 0 {
		t.Fatal("ни один стенд не собирает церемонию — судить по полу нечего, и «находок нет» было бы " +
			"«обход пуст»")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readCapacityFloorFacts — величины цепочки каждого стенда поверх умолчаний
// подчарта, ровно как их получает helm, с координатой последнего объявления.
func readCapacityFloorFacts(t *testing.T) []capacityFloorFacts {
	t.Helper()
	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	subchartValues := filepath.Join(kanameSubchart(t), "values.yaml")
	out := make([]capacityFloorFacts, 0, len(names))
	for _, name := range names {
		// Умолчания подчарта — заново на каждый стенд: mergeValues правит карту на месте.
		declared := map[string]any{"kaname": readYAML(t, subchartValues)}
		f := capacityFloorFacts{Stack: name, Coordinate: "не объявлена", EnabledFrom: "умолчание подчарта"}
		if line := yamlKeyLine(t, subchartValues, verifierCapacityKey[1:]...); line > 0 {
			f.Coordinate = fmt.Sprintf("%s:%d", subchartValues, line)
		}
		for _, p := range stacksTbl[name] {
			path := filepath.Join(umbrellaDir, p)
			layer := readYAML(t, path)
			if _, ok := lookup(layer, verifierCapacityKey...); ok {
				f.Coordinate = fmt.Sprintf("%s:%d", path, yamlKeyLine(t, path, verifierCapacityKey...))
			}
			if _, ok := lookup(layer, ceremonyEnabledKey...); ok {
				f.EnabledFrom = p
			}
			declared = mergeValues(declared, layer)
		}
		if v, ok := lookup(declared, verifierCapacityKey...); ok {
			f.Declared = true
			f.Capacity = asInt64(t, name, "verifierCapacity", v)
		}
		if v, ok := lookup(declared, ceremonyEnabledKey...); ok {
			on, isBool := v.(bool)
			if !isBool {
				t.Fatalf("стенд %s: `kaname.config.authn.clientToken.enabled` = %v (%T) — не логическое; "+
					"собрана ли церемония, не прочитано", name, v, v)
			}
			f.Ceremony = on
		}
		out = append(out, f)
	}
	return out
}

// yamlKeyLine — строка ключа в файле YAML (0 — ключа нет).
func yamlKeyLine(t *testing.T, path string, keys ...string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("разбор %s: %v", path, err)
	}
	if len(doc.Content) == 0 {
		return 0
	}
	node := doc.Content[0]
	line := 0
	for _, k := range keys {
		for node.Kind == yaml.AliasNode {
			node = node.Alias
		}
		if node.Kind != yaml.MappingNode {
			return 0
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == k {
				line, next = node.Content[i].Line, node.Content[i+1]
			}
		}
		if next == nil {
			return 0
		}
		node = next
	}
	return line
}
