// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_second_factor_reachable_test.go — ПОЛ, КОТОРЫЙ НЕЧЕМ УДОВЛЕТВОРИТЬ,
// ЗАКРЫВАЕТ ГЛАГОЛ НАВСЕГДА (#1213).
//
// # Предмет
//
// Каталог прав объявляет части глаголов пол уровня уверенности «2». Край этот
// пол спрашивает — на всех полосах личности человека, включая браузерную
// (#1201). Значит у арендатора обязан существовать СПОСОБ поднять уровень; иначе
// объявленный пол означает не «подтвердите второй фактор», а «этого действия из
// браузера не существует» — и означает это для ВСЕХ, а не для нарушителей.
//
// Достижимость уровня — свойство ТРЁХ сторон сразу, и ни одна не видит его
// целиком:
//
//  1. сторона, которая ВКЛЮЧАЕТ способы второго фактора;
//  2. консоль — какие способы она умеет довести до конца;
//  3. каталог прав — сколько глаголов этим полом связано.
//
// Каждая сторона по отдельности валидна: настройки рендерятся, консоль
// собирается и её пробы зелены, каталог проходит свои гейты. Неверна их
// РАЗНИЦА — и увидеть её может только тот, кто читает все три.
//
// # Стороны 1 и 2 — СВОИ У КАЖДОЙ ПОСАДКИ (#2691)
//
// Кто включает способы и какую церемонию ведёт консоль, решает посадка личности
// стенда — наша ручка `identityProvider` (раздел «Кого судят четыре стража
// личности» ниже):
//
//	посадка    сторона 1 (включает способы)          сторона 2 (консоль ведёт)
//	external   настройки поставщика личности          поток поставщика `aal=aal2` —
//	           (identityConfigTemplate)               STEP_UP_METHODS
//	own        корень композиции пиненной службы      НАША церемония повышения —
//	           доступа: перечень, из которого         OWN_STEP_UP_METHODS
//	           самоотчёт старта выводит
//	           `lane_presentable_acrs`
//
// Прежняя редакция судила ВСЕ стенды по первой строке. На посадке `own`
// настройка поставщика не включает ничего — компонента на стенде нет, — а консоль
// не ведёт поток поставщика; поэтому гейт зеленел («30 из 30 достижимы») ровно
// там, где пол «2» поднять было нечем. Судьёй стороны был компонент, которого на
// стенде нет, — и с его снятием (Ф10, #1276) у гейта не осталось бы первой
// стороны вовсе. Теперь стороны берутся у посадки, и снятие компонента снимает
// ровно строку `external` вместе с её предметом.
//
// Правило уровня на `own` — правило службы (приёмка Ф11, Р2: «2» — утверждение
// ключа доступа либо пароль вместе с одноразовым или запасным кодом). Пакет
// правила внутренний и сюда не импортируется, поэтому его строки ЧИТАЮТСЯ у пина
// (TestIdentity_OwnRulePremiseMatchesThePin): сменился набор способов в строках
// «1» или «2» — гейт краснеет и просит перемерить свою классификацию, а не
// продолжает судить по прежнему правилу.
//
// # Почему перепись печатает ДВА числа
//
// «Записей с полом „2“ — 32» само по себе выглядит как утверждение о защите. Оно
// скрывает ровно тот случай, ради которого задача заведена: из этих 32
// достижимых может быть НОЛЬ. Поэтому строка переписи несёт посадку и обе
// величины, и вторая вычисляется, а не предполагается.
//
// # Что здесь НЕ утверждается
//
// Не утверждается, что второй фактор устойчив к посреднику: это отдельный и
// более широкий предмет (#1188). Пол каталога сегодня нигде не выше «2», и
// проверяется ровно достижимость объявленных полов. Пол «1» судится стороной 1:
// экран входа консоли — предмет своих проб, а не этого гейта.
//
// # На что этот гейт опирается — названо, а не подразумевается
//
// Сторону КОНСОЛИ он читает объявлением (`step-up-methods.ts`), а не разбором
// вёрстки. Значит объявление обязано быть кем-то опровергаемо, иначе гейт судил
// бы по обещанию: держит его проба рядом с самим окном
// (`StepUpModal.secondfactor.test.tsx`) — она прогоняет окно ПО КАЖДОМУ
// названному способу. Правя одно, правь второе. Это относится к обоим перечням:
// перечень нашей церемонии заводится вместе с церемонией (Ф8, #1274) и пробой,
// прогоняющей окно по каждому его способу, — объявленный без неё, он был бы
// обещанием, которое некому опровергнуть.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер, ни браузер не нужны,
// поэтому проверка не умеет пропускаться. Сторону службы на `own` он читает
// исходником пиненного модуля (go.mod даёт версию, GOMODCACHE — каталог; тот же
// приём, что у own_ceilings_and_access_keys_umbrella_test.go): подняли пин —
// гейт судит новый корень сам.
package deploy_test

import (
	"encoding/json"
	"errors"
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

	"gopkg.in/yaml.v3"
)

// Значения нашей ручки посадки личности (`identityProvider`).
const (
	landingExternal = "external"
	landingOwn      = "own"
)

const (
	// stepUpMethodsDeclaration — сторона КОНСОЛИ, объявленная одной строкой на
	// каждую церемонию.
	stepUpMethodsDeclaration = "../ui-future/shared/src/lib/step-up-methods.ts"
	// permissionCatalogEmbed — сторона КАТАЛОГА ПРАВ (обе встроенные копии
	// побайтово равны и держатся своим гейтом; читаем ту, что энфорсит край).
	permissionCatalogEmbed = "../gateway/internal/middleware/embed/permission_catalog.json"
	// identityRenderedConfigPath — путь, по которому процесс службы личности
	// ЧИТАЕТ наши настройки. Профиль, называющий его, доводит объявление до
	// процесса; не называющий — оставляет процесс на умолчаниях поставщика.
	identityRenderedConfigPath = "/etc/kaname-identity-rendered/kratos.yaml"
)

// identityMethodDecl — объявление одного метода службы личности.
type identityMethodDecl struct {
	Enabled bool
	Config  map[string]string
}

// parseIdentityMethods разбирает блок `selfservice.methods` тела настроек службы
// личности по отступам.
//
// Разбор строчный, а не через YAML-библиотеку, и это не лень: тело — Go-шаблон,
// в котором величины стоят подстановками (`{{ $app }}`), поэтому валидным YAML
// оно не является ни на одной ревизии. Тот же приём и у соседней проверки полос
// регистрации.
func parseIdentityMethods(body string) map[string]identityMethodDecl {
	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	lines := strings.Split(body, "\n")

	out := map[string]identityMethodDecl{}
	inMethods := false
	methodsIndent := -1
	method := ""
	inConfig := false

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{{") {
			continue
		}
		if !inMethods {
			if trimmed == "methods:" {
				inMethods, methodsIndent = true, indentOf(ln)
			}
			continue
		}
		ind := indentOf(ln)
		if ind <= methodsIndent {
			break // вышли из `methods:`
		}
		switch {
		case ind == methodsIndent+2 && strings.HasSuffix(trimmed, ":"):
			method = strings.TrimSuffix(trimmed, ":")
			inConfig = false
			if _, ok := out[method]; !ok {
				out[method] = identityMethodDecl{Config: map[string]string{}}
			}
		case method == "":
			// величина до первого имени метода — не наша
		case ind == methodsIndent+4:
			inConfig = false
			key, val, ok := splitYAMLPair(trimmed)
			if !ok {
				continue
			}
			if key == "config" && val == "" {
				inConfig = true
				continue
			}
			if key == "enabled" {
				d := out[method]
				d.Enabled = val == "true"
				out[method] = d
			}
		case inConfig && ind == methodsIndent+6:
			if key, val, ok := splitYAMLPair(trimmed); ok {
				out[method].Config[key] = val
			}
		}
	}
	return out
}

// splitYAMLPair режет `ключ: величина`, отбрасывая хвостовой комментарий.
func splitYAMLPair(trimmed string) (key, val string, ok bool) {
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:idx])
	val = strings.TrimSpace(trimmed[idx+1:])
	if i := strings.Index(val, "#"); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	return key, val, key != ""
}

// secondFactorMethods отбирает из объявленных методов те, что дают ВТОРОЙ
// фактор, то есть поднимают сессию до `aal2`.
//
// ПРЕДПОСЫЛКА, НАЗВАННАЯ ЯВНО (служба личности версии, объявленной в том же
// теле — `version: v1.3.1`):
//
//	totp, lookup_secret          — второй фактор всегда;
//	webauthn                     — второй фактор ТОЛЬКО когда `passwordless`
//	                               не включён; в беспарольной посадке это
//	                               ПЕРВЫЙ фактор (`aal1`), и в потоке
//	                               `aal=aal2` служба его не предлагает вовсе;
//	code                         — второй фактор только при `mfa_enabled`;
//	passkey, password, oidc, …   — первый фактор by construction.
//
// Предпосылка проверяема: она привязана к объявленной версии службы, и её
// смена обязана идти вместе с перемером этой функции.
func secondFactorMethods(decls map[string]identityMethodDecl) []string {
	var out []string
	for name, d := range decls {
		if !d.Enabled {
			continue
		}
		switch name {
		case "totp", "lookup_secret":
			out = append(out, name)
		case "webauthn":
			if d.Config["passwordless"] != "true" {
				out = append(out, name)
			}
		case "code":
			if d.Config["mfa_enabled"] == "true" {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// firstFactorMethods — методы, которыми арендатор входит вообще (дают `aal1`).
func firstFactorMethods(decls map[string]identityMethodDecl) []string {
	var out []string
	for name, d := range decls {
		if !d.Enabled {
			continue
		}
		switch name {
		case "password", "passkey", "oidc":
			out = append(out, name)
		case "webauthn":
			if d.Config["passwordless"] == "true" {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Оба перечня консоли — в одном файле, и имя одного есть хвост имени другого:
// без границы слова `STEP_UP_METHODS` узнавался бы и внутри
// `OWN_STEP_UP_METHODS`, и перечень потока поставщика читался бы из объявления
// нашей церемонии.
var stepUpMethodsLiteral = regexp.MustCompile(`(?s)\bSTEP_UP_METHODS\s*=\s*\[(.*?)\]`)
var ownStepUpMethodsLiteral = regexp.MustCompile(`(?s)\bOWN_STEP_UP_METHODS\s*=\s*\[(.*?)\]`)
var quotedToken = regexp.MustCompile(`"([a-z_]+)"`)

// parseStepUpMethods читает сторону КОНСОЛИ на посадке `external` — способы,
// которые окно повторного подтверждения умеет довести до конца в потоке
// поставщика.
func parseStepUpMethods(src string) []string {
	return parseDeclaredMethods(stepUpMethodsLiteral, src)
}

// parseOwnStepUpMethods читает сторону КОНСОЛИ на посадке `own` — способы,
// которые окно умеет довести до конца в НАШЕЙ церемонии повышения.
func parseOwnStepUpMethods(src string) []string {
	return parseDeclaredMethods(ownStepUpMethodsLiteral, src)
}

func parseDeclaredMethods(literal *regexp.Regexp, src string) []string {
	m := literal.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	var out []string
	for _, g := range quotedToken.FindAllStringSubmatch(m[1], -1) {
		out = append(out, g[1])
	}
	sort.Strings(out)
	return out
}

// catalogEntry — ровно то, что нужно этой проверке.
type catalogEntry struct {
	FQN            string `json:"fqn"`
	RequiredACRMin string `json:"required_acr_min"`
}

func readCatalogFloors(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(permissionCatalogEmbed)) // #nosec G304 -- путь-константа собственного дерева
	if err != nil {
		t.Fatalf("каталог прав не прочитан (%s): %v", permissionCatalogEmbed, err)
	}
	var entries []catalogEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("каталог прав не разобран (%s): %v", permissionCatalogEmbed, err)
	}
	byFloor := map[string][]string{}
	for _, e := range entries {
		byFloor[e.RequiredACRMin] = append(byFloor[e.RequiredACRMin], e.FQN)
	}
	return byFloor
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- путь-константа собственного дерева
	if err != nil {
		t.Fatalf("объявление не прочитано (%s): %v", path, err)
	}
	return string(raw)
}

// attainableFloors — какие полы уровня браузерная сессия способна предъявить.
//
// «Способна» означает пару: служба личности метод ВКЛЮЧАЕТ и консоль его ВЕДЁТ.
// Включённый, но неведомый консоли метод достижимости не даёт — арендатору
// нечем им воспользоваться; ведомый, но выключенный — тем более.
func attainableFloors(secondFactor, firstFactor, drivable []string) (floors map[string]bool, usable []string) {
	set := map[string]bool{}
	for _, m := range drivable {
		set[m] = true
	}
	for _, m := range secondFactor {
		if set[m] {
			usable = append(usable, m)
		}
	}
	sort.Strings(usable)
	return map[string]bool{
		// «0» — анонимный пол: удовлетворяется любой живой сессией.
		"0": true,
		"1": len(firstFactor) > 0,
		"2": len(usable) > 0,
		// «3» — аппаратно-связанный уровень. Служба личности объявленной версии
		// его не выдаёт вовсе, поэтому пол «3», появившись в каталоге, был бы
		// недостижим by construction — и это находка, а не умолчание.
		"3": false,
	}, usable
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНЫ ПОСАДКИ own: КОРЕНЬ СЛУЖБЫ И ПРАВИЛО УРОВНЯ — У ПИНА
//
// Корень композиции службы доступа — пакет `main` чужого модуля, правило уровня —
// его внутренний пакет: ни то ни другое не импортируется. Поэтому оба читаются
// РАЗБОРОМ исходника пиненного модуля, и читается ровно то, что читает сама
// служба: перечень, который корень подаёт самоотчёту старта
// (`observeLaneWiring`, аргумент способов входа), и строки правила «1» и «2».

// kanameMainPackage и kanameAssurancePackage — дома двух объявлений в модуле
// службы, от его корня.
const (
	kanameMainPackage      = "cmd/kaname"
	kanameAssurancePackage = "internal/assurance"
	// laneWiringObserver — функция корня, которая получает провязанные способы
	// входа и выводит из них `lane_presentable_acrs` самоотчёта.
	laneWiringObserver = "observeLaneWiring"
	// laneWiringSignInArg — позиция аргумента способов входа у наблюдателя.
	laneWiringSignInArg = 3
)

// goPackageSource — разобранные файлы одного пакета (без проб `_test.go`).
type goPackageSource struct {
	dir   string
	fset  *token.FileSet
	files map[string]*ast.File // путь от корня модуля → файл
}

func parseGoPackage(moduleDir, pkg string) (goPackageSource, error) {
	src := goPackageSource{dir: moduleDir, fset: token.NewFileSet(), files: map[string]*ast.File{}}
	entries, err := os.ReadDir(filepath.Join(moduleDir, pkg))
	if err != nil {
		return src, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := pkg + "/" + name
		body, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(rel))) // #nosec G304 -- путь собран из пина go.mod
		if err != nil {
			return src, err
		}
		if err := src.put(rel, string(body)); err != nil {
			return src, err
		}
	}
	if len(src.files) == 0 {
		return src, fmt.Errorf("в %s/%s нет ни одного файла Go", moduleDir, pkg)
	}
	return src, nil
}

// put разбирает тело под путём — и настоящий файл, и инъекцию в него.
func (s goPackageSource) put(rel, body string) error {
	f, err := parser.ParseFile(s.fset, rel, body, 0)
	if err != nil {
		return err
	}
	s.files[rel] = f
	return nil
}

// with — копия пакета, в которой один файл заменён телом инъекции; настоящий
// разбор при этом не меняется.
func (s goPackageSource) with(rel, body string) (goPackageSource, error) {
	out := goPackageSource{dir: s.dir, fset: s.fset, files: make(map[string]*ast.File, len(s.files))}
	for k, v := range s.files {
		out.files[k] = v
	}
	return out, out.put(rel, body)
}

// sortedFiles — файлы в устойчивом порядке: находка «два места» называет их
// одинаково на каждом прогоне.
func (s goPackageSource) sortedFiles() []string {
	out := make([]string, 0, len(s.files))
	for rel := range s.files {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// assuranceVocabulary — словарь способов службы: имя постоянной → имя способа
// (`MethodTOTP = Method{"totp"}` → MethodTOTP: totp).
func assuranceVocabulary(src goPackageSource) map[string]string {
	vocab := map[string]string{}
	for _, rel := range src.sortedFiles() {
		for _, decl := range src.files[rel].Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok || len(lit.Elts) != 1 {
					continue
				}
				if typ, ok := lit.Type.(*ast.Ident); !ok || typ.Name != "Method" {
					continue
				}
				bl, ok := lit.Elts[0].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				if name, err := strconv.Unquote(bl.Value); err == nil {
					vocab[vs.Names[0].Name] = name
				}
			}
		}
	}
	return vocab
}

// errNoSignInList — корень найден, а перечня способов входа в узнаваемой форме
// у него нет: вердикта нет, это не «служба не провязала ничего».
var errNoSignInList = errors.New("перечень способов входа корня не разобран")

// ownWiredMethods — способы входа человека, которые корень службы подаёт
// самоотчёту старта. Возвращает имена словаря и координату объявления.
//
// Путь чтения — тот же, что у самой службы: вызов наблюдателя провязки →
// аргумент способов → метод, который его производит → постоянные словаря в его
// теле. Ветка `return nil` (полоса не поднята) способов не добавляет — это
// посадка без полосы, а не другой перечень.
func ownWiredMethods(root goPackageSource, vocab map[string]string) ([]string, string, error) {
	var producers []string
	for _, rel := range root.sortedFiles() {
		ast.Inspect(root.files[rel], func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != laneWiringObserver || len(call.Args) <= laneWiringSignInArg {
				return true
			}
			arg, ok := call.Args[laneWiringSignInArg].(*ast.CallExpr)
			if !ok {
				producers = append(producers, "")
				return true
			}
			sel, ok := arg.Fun.(*ast.SelectorExpr)
			if !ok {
				producers = append(producers, "")
				return true
			}
			producers = append(producers, sel.Sel.Name)
			return true
		})
	}
	if len(producers) != 1 || producers[0] == "" {
		return nil, "", fmt.Errorf("%w: вызовов %s с методом-производителем способов найдено %d (%v), ждали ровно один",
			errNoSignInList, laneWiringObserver, len(producers), producers)
	}
	producer := producers[0]

	var found []*ast.FuncDecl
	var where []string
	for _, rel := range root.sortedFiles() {
		for _, decl := range root.files[rel].Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if ok && fd.Recv != nil && fd.Name.Name == producer && fd.Body != nil {
				found = append(found, fd)
				where = append(where, fmt.Sprintf("%s:%d", rel, root.fset.Position(fd.Pos()).Line))
			}
		}
	}
	if len(found) != 1 {
		return nil, "", fmt.Errorf("%w: метод %s объявлен %d раз (%v)", errNoSignInList, producer, len(found), where)
	}

	seen := map[string]bool{}
	var unknown []string
	ast.Inspect(found[0].Body, func(n ast.Node) bool {
		// `assurance.Method` — имя ТИПА (перечень объявлен `[]assurance.Method`),
		// способом он не является; постоянные словаря — `Method<Имя>`.
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "Method") || sel.Sel.Name == "Method" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "assurance" {
			return true
		}
		name, ok := vocab[sel.Sel.Name]
		if !ok {
			unknown = append(unknown, sel.Sel.Name)
			return true
		}
		seen[name] = true
		return true
	})
	if len(unknown) > 0 {
		return nil, where[0], fmt.Errorf("%w: %s называет постоянные вне словаря службы: %v",
			errNoSignInList, where[0], unknown)
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, where[0], nil
}

// ownRulePremise — строки правила уровня, по которым классифицирует этот гейт:
// уровень → наборы постоянных словаря, названных в условии строки. Классификация
// ownSecondFactorMethods/ownFirstFactorMethods верна ровно при этом правиле.
var ownRulePremise = map[string][]string{
	"1": {"MethodPassword", "MethodRecoveryCode"},
	"2": {"MethodLookupSecret+MethodPassword+MethodTOTP", "MethodWebAuthn"},
}

// assuranceRuleRows — строки правила уровня службы, прочитанные у пина: для
// уровней «1» и «2» — какие постоянные словаря названы в условии каждой строки.
func assuranceRuleRows(src goPackageSource) (map[string][]string, error) {
	for _, rel := range src.sortedFiles() {
		for _, decl := range src.files[rel].Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "rows" || len(vs.Values) != 1 {
					continue
				}
				table, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok {
					return nil, fmt.Errorf("%s: `rows` — не литерал таблицы", rel)
				}
				out := map[string][]string{}
				for _, el := range table.Elts {
					row, ok := el.(*ast.CompositeLit)
					if !ok || len(row.Elts) == 0 {
						continue
					}
					lvl, ok := row.Elts[0].(*ast.Ident)
					if !ok || !strings.HasPrefix(lvl.Name, "Level") {
						continue
					}
					level := strings.TrimPrefix(lvl.Name, "Level")
					if level != "1" && level != "2" {
						continue
					}
					names := map[string]bool{}
					ast.Inspect(row, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Method") {
							names[id.Name] = true
						}
						return true
					})
					set := make([]string, 0, len(names))
					for n := range names {
						set = append(set, n)
					}
					sort.Strings(set)
					out[level] = append(out[level], strings.Join(set, "+"))
				}
				for l := range out {
					sort.Strings(out[l])
				}
				return out, nil
			}
		}
	}
	return nil, errors.New("таблица строк правила `rows` не найдена")
}

// ownSecondFactorMethods — провязанные способы, которыми сессия поднимается до
// «2» (правило Р2 службы, сверенное с пином ownRulePremise): утверждение ключа
// доступа — само; код по времени и запасной код — только вместе с паролем.
func ownSecondFactorMethods(wired []string) []string {
	has := map[string]bool{}
	for _, m := range wired {
		has[m] = true
	}
	var out []string
	if has["webauthn"] {
		out = append(out, "webauthn")
	}
	if has["password"] {
		for _, m := range []string{"totp", "lookup_secret"} {
			if has[m] {
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ownFirstFactorMethods — провязанные способы, которыми сессия выдаётся вообще.
func ownFirstFactorMethods(wired []string) []string {
	var out []string
	for _, m := range wired {
		switch m {
		case "password", "recovery_code", "webauthn":
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// СУД ПО ПОСАДКЕ

// secondFactorSides — стороны 1 и 2 одной посадки и откуда каждая прочитана.
type secondFactorSides struct {
	Landing     string
	ServiceFrom string   // координата стороны, включающей способы
	ConsoleFrom string   // координата перечня консоли
	First       []string // способы, которыми сессия выдаётся
	Second      []string // способы, поднимающие сессию до «2»
	Drivable    []string // способы, которые консоль ведёт на этой посадке
}

// judgeSecondFactorReach — перепись каталога и находки ОДНОЙ посадки.
//
// Чистая: стороны подаются значением, поэтому инъекция подаёт ей и настоящие
// стороны дерева, и стороны с возвращённым дефектом — не трогая ни дерева, ни пина.
func judgeSecondFactorReach(s secondFactorSides, byFloor map[string][]string) (census, findings []string, usable []string) {
	floors, usable := attainableFloors(s.Second, s.First, s.Drivable)
	total := 0
	floorNames := make([]string, 0, len(byFloor))
	for f, v := range byFloor {
		total += len(v)
		floorNames = append(floorNames, f)
	}
	sort.Strings(floorNames)

	for _, f := range floorNames {
		n := len(byFloor[f])
		if f == "" {
			census = append(census, fmt.Sprintf("посадка %s: записей всего %d · без объявленного пола %d "+
				"(пол не спрашивается)", s.Landing, total, n))
			continue
		}
		reachable := 0
		if floors[f] {
			reachable = n
		}
		census = append(census, fmt.Sprintf("посадка %s: записей всего %d · записей с полом «%s» %d · достижимых %d",
			s.Landing, total, f, n, reachable))
		if reachable == n {
			continue
		}
		sample := append([]string(nil), byFloor[f]...)
		sort.Strings(sample)
		if len(sample) > 3 {
			sample = sample[:3]
		}
		findings = append(findings, fmt.Sprintf(
			"посадка %s: пол уровня «%s» объявлен у %d записей каталога и НЕДОСТИЖИМ из браузера: "+
				"достижимых %d.\n"+
				"  включает вторым фактором (%s): %v\n"+
				"  консоль ведёт (%s): %v\n"+
				"  пригодны обеим сторонам: %v\n"+
				"Пол, который нечем удовлетворить, означает не «подтвердите второй фактор», "+
				"а «этого действия из браузера не существует» — и означает это для ВСЕХ на этой посадке. "+
				"Чинится ПАРОЙ: способ включается стороной службы И ведётся консолью на церемонии этой "+
				"посадки. Одной стороны мало: включённый, но неведомый способ так же недостижим, как "+
				"выключенный. Например: %v",
			s.Landing, f, n, reachable, s.ServiceFrom, s.Second, s.ConsoleFrom, s.Drivable, usable, sample))
	}
	return census, findings, usable
}

// externalSecondFactorSides — посадка `external`: настройки поставщика и
// перечень потока поставщика.
func externalSecondFactorSides(settings, console string) (secondFactorSides, error) {
	methods := parseIdentityMethods(settings)
	if len(methods) == 0 {
		return secondFactorSides{}, fmt.Errorf("методов поставщика личности не разобрано ни одного (%s) — "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного». Либо блок `selfservice.methods` "+
			"переехал, либо разбор перестал его видеть", identityConfigTemplate)
	}
	drivable := parseStepUpMethods(console)
	if len(drivable) == 0 {
		return secondFactorSides{}, fmt.Errorf("перечень STEP_UP_METHODS консоли пуст либо не разобран (%s) — "+
			"вердикта нет: достижимость считалась бы по одной стороне из двух", stepUpMethodsDeclaration)
	}
	return secondFactorSides{
		Landing:     landingExternal,
		ServiceFrom: identityConfigTemplate,
		ConsoleFrom: stepUpMethodsDeclaration + " STEP_UP_METHODS",
		First:       firstFactorMethods(methods),
		Second:      secondFactorMethods(methods),
		Drivable:    drivable,
	}, nil
}

// ownSecondFactorSides — посадка `own`: корень пиненной службы и перечень
// нашей церемонии.
//
// Пустой перечень консоли здесь — НЕ отсутствие вердикта, а сам предмет: консоль,
// не объявившая способов нашей церемонии, поднимать уровень на этой посадке не
// умеет, и это находка по полу, а не молчание. Отсутствие перечня службы —
// наоборот, отказ: разбор корня обязан что-то увидеть.
func ownSecondFactorSides(pin string, root goPackageSource, vocab map[string]string, console string) (secondFactorSides, error) {
	wired, where, err := ownWiredMethods(root, vocab)
	if err != nil {
		return secondFactorSides{}, fmt.Errorf("корень службы доступа у пина %s: %w", pin, err)
	}
	return secondFactorSides{
		Landing:     landingOwn,
		ServiceFrom: fmt.Sprintf("%s@%s %s", productModuleprefix+kanameModulePart, pin, where),
		ConsoleFrom: stepUpMethodsDeclaration + " OWN_STEP_UP_METHODS",
		First:       ownFirstFactorMethods(wired),
		Second:      ownSecondFactorMethods(wired),
		Drivable:    parseOwnStepUpMethods(console),
	}, nil
}

// readPinnedKaname — оба пакета пиненной службы, которые читает этот гейт.
func readPinnedKaname(t *testing.T) (pin string, root, assurance goPackageSource) {
	t.Helper()
	moduleDir := kanameModuleDir(t, "..")
	pin = productModulePins(t, "..")[kanameModulePart]
	var err error
	if root, err = parseGoPackage(moduleDir, kanameMainPackage); err != nil {
		t.Fatalf("корень службы доступа у пина %s не разобран: %v", pin, err)
	}
	if assurance, err = parseGoPackage(moduleDir, kanameAssurancePackage); err != nil {
		t.Fatalf("пакет правила уровня у пина %s не разобран: %v", pin, err)
	}
	return pin, root, assurance
}

// sidesOfLanding — стороны 1 и 2 посадки, прочитанные у дерева и пина.
//
// Посадка вне таблицы — не «судить нечего», а отказ: гейт не знает, кто на ней
// включает способы и какую церемонию ведёт консоль, и молчание на ней было бы
// тем самым зелёным без предмета.
func sidesOfLanding(t *testing.T, landing, console string) (secondFactorSides, error) {
	t.Helper()
	switch landing {
	case landingExternal:
		return externalSecondFactorSides(readFileForTest(t, identityConfigTemplate), console)
	case landingOwn:
		pin, root, assurance := readPinnedKaname(t)
		return ownSecondFactorSides(pin, root, assuranceVocabulary(assurance), console)
	default:
		return secondFactorSides{}, fmt.Errorf("посадка %q: сторон достижимости у неё не объявлено — "+
			"гейт не знает, кто на ней включает способы и какую церемонию ведёт консоль", landing)
	}
}

// side — половина посадки, решающая, кто проверяет человека: служба
// доступа; край читается, когда служба посадку не назвала.
func (l identityLanding) side() string {
	if l.IAM != "" {
		return l.IAM
	}
	return l.Edge
}

// stacksByLanding — стенды таблицы, сгруппированные по посадке личности.
func stacksByLanding(t *testing.T) map[string][]string {
	t.Helper()
	stacks := deployStacks(t)
	out := map[string][]string{}
	for name, chain := range stacks {
		texts := make([]string, 0, len(chain))
		for _, prof := range chain {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		if l := identityLandingOfChain(t, texts); l.lands() {
			out[l.side()] = append(out[l.side()], name)
		}
	}
	for l := range out {
		sort.Strings(out[l])
	}
	return out
}

func TestIdentity_SecondFactorReachesTheBrowser(t *testing.T) {
	landings := stacksByLanding(t)
	if len(landings) == 0 {
		t.Fatal("ни один стенд не объявляет посадку личности — судить достижимость пола не на чем, " +
			"и зелёный здесь означал бы «сверять было не с чем»")
	}
	console := readFileForTest(t, stepUpMethodsDeclaration)
	byFloor := readCatalogFloors(t)

	names := make([]string, 0, len(landings))
	for l := range landings {
		names = append(names, l)
	}
	sort.Strings(names)

	for _, landing := range names {
		sides, err := sidesOfLanding(t, landing, console)
		if err != nil {
			t.Errorf("посадка %s: вердикта нет — %v", landing, err)
			continue
		}

		census, findings, usable := judgeSecondFactorReach(sides, byFloor)
		t.Logf("посадка %s (стендов %d: %s): первым фактором %d (%s) · вторым фактором %d (%s) — %s · "+
			"консоль ведёт %d (%s) — %s · пригодны обеим сторонам %d (%s)",
			landing, len(landings[landing]), strings.Join(landings[landing], " "),
			len(sides.First), strings.Join(sides.First, " "),
			len(sides.Second), strings.Join(sides.Second, " "), sides.ServiceFrom,
			len(sides.Drivable), strings.Join(sides.Drivable, " "), sides.ConsoleFrom,
			len(usable), strings.Join(usable, " "))
		for _, c := range census {
			t.Logf("перепись каталога: %s", c)
		}
		for _, f := range findings {
			t.Error(f)
		}
	}
}

// TestIdentity_OwnRulePremiseMatchesThePin — классификация посадки `own` верна
// ровно при том правиле уровня, которое держит пин.
//
// Правило живёт во внутреннем пакете службы и сюда не импортируется; гейт
// классифицирует способы своей таблицей. Таблица без сверки была бы вторым
// правилом, расходящимся с первым молча, — поэтому строки «1» и «2» читаются у
// пина и сравниваются с ownRulePremise.
func TestIdentity_OwnRulePremiseMatchesThePin(t *testing.T) {
	pin, _, assurance := readPinnedKaname(t)
	rows, err := assuranceRuleRows(assurance)
	if err != nil {
		t.Fatalf("правило уровня у пина %s не прочитано: %v — классификацию посадки own сверять не с чем", pin, err)
	}
	vocab := assuranceVocabulary(assurance)
	t.Logf("перепись правила у пина %s: словарь %d способов · строк «1» %d (%s) · строк «2» %d (%s)",
		pin, len(vocab), len(rows["1"]), strings.Join(rows["1"], " "), len(rows["2"]), strings.Join(rows["2"], " "))
	for _, level := range []string{"1", "2"} {
		if strings.Join(rows[level], " ") != strings.Join(ownRulePremise[level], " ") {
			t.Errorf("правило уровня «%s» у пина %s: строки %v, гейт классифицирует по %v.\n"+
				"Правило сменилось — ownSecondFactorMethods/ownFirstFactorMethods судят по прежнему. "+
				"Перемерь классификацию вместе с ownRulePremise тем же изменением, что поднимает пин",
				level, pin, rows[level], ownRulePremise[level])
		}
	}
	for _, name := range []string{"MethodPassword", "MethodTOTP", "MethodLookupSecret", "MethodWebAuthn", "MethodRecoveryCode"} {
		if _, ok := vocab[name]; !ok {
			t.Errorf("словарь службы у пина %s не называет %s — разбор перестал его видеть либо словарь "+
				"сменился; классификация посадки own без него беспредметна", pin, name)
		}
	}
}

// identityChainMountsOurConfig — цепочка профилей ДОВОДИТ наши настройки до
// процесса службы личности (а не только рендерит их).
//
// Спрашивается у ЦЕПОЧКИ, а не у отдельного профиля: накладка (`values.fe3455-*`)
// поднимает продукт вместе со слоем под собой и своей провязки не несёт — судить
// её в одиночку значило бы называть находкой нормальную раскладку слоёв.
func identityChainMountsOurConfig(texts []string) bool {
	for _, t := range texts {
		if strings.Contains(t, identityRenderedConfigPath) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// КОГО СУДЯТ ЧЕТЫРЕ СТРАЖА ЛИЧНОСТИ
//
// Четыре проверки развёртывания (второй фактор, потоки доставки, зеркало
// требования подтверждённого адреса, решённые замещения списков) отбирают
// стенды одним предикатом. Предикат этот СУДИЛ ПО ЧУЖОМУ ФЛАГУ — по
// `kratos.enabled` подчарта внешнего поставщика удостоверений.
//
// Чем это кончалось, измерено инъекцией 2026-09-21: выключение
// чужих флагов в боевом профиле (`values.prod.yaml`: `kratos.enabled: false`,
// `hydra.enabled: false`) уводило из-под суда РОВНО боевой стенд и стенд
// посадки `own` — 7 стендов превращались в 5, и все четыре стража оставались
// ЗЕЛЁНЫМИ. То есть чужая ручка, к которой у стражей нет ни предмета, ни
// владения, отключала их ровно на тех двух стендах, ради которых они заведены.
//
// Поэтому предикат переутверждён на НАШ признак: посадку личности объявляет
// наша ручка `identityProvider`, по одной у каждой из двух половин стенда —
//
//	kaname.config.authn.identityProvider    — служба доступа
//	api-gateway.authn.identityProvider      — край
//
// Признак переживает снятие чужих служб ЦЕЛИКОМ: ручка живёт в нашем чарте, её
// значения (`external` · `own`) объявляют, КТО проверяет человека, а не какой
// подчарт поднят. Снять kratos и hydra из профиля можно, не тронув ни одного её
// значения, — и стенд останется под судом, потому что о личности он по-прежнему
// решает.
//
// ОТСУТСТВИЕ значения у обеих половин и у баз подчартов предикат считает НЕ
// «стендом без личности», а исчезнувшей предпосылкой: он отвечает «нет», и у
// каждого из четырёх стражей нулевая перепись — отказ (`t.Fatal`), а не тишина.

const (
	// iamLandingDefaults — база подчарта службы доступа: значение посадки,
	// которое стенд получает, не объявив её сам.
	iamLandingDefaults = umbrellaDir + "/charts/kaname/values.yaml"
	// edgeLandingDefaults — то же у края.
	edgeLandingDefaults = "../gateway/deploy/values.yaml"
)

// identityLanding — посадка личности стенда: что объявлено каждой половине и
// откуда значение взято.
type identityLanding struct {
	IAM  string // kaname.config.authn.identityProvider
	Edge string // api-gateway.authn.identityProvider
	Base bool   // ни один профиль цепочки посадку не назвал — значение с базы подчарта
}

// lands — стенд решает о личности человека, то есть подлежит суду стражей.
func (l identityLanding) lands() bool { return l.IAM != "" || l.Edge != "" }

// posture — значение посадки для переписи. Половины, разошедшиеся о посадке,
// судит отдельная проба зонта (identity_posture_profiles_test.go), поэтому
// здесь расхождение печатается, а не замалчивается.
func (l identityLanding) posture() string {
	switch {
	case l.IAM != "" && l.Edge != "" && l.IAM != l.Edge:
		return "iam=" + l.IAM + "/gateway=" + l.Edge
	case l.IAM != "":
		return l.IAM
	default:
		return l.Edge
	}
}

// identityLandingOfProfile — что ОДИН профиль объявил половинам.
//
// Профиль, который не разбирается как YAML, значения не даёт: его форму судит
// своя проверка, и подменять её вердикт молчаливым «посадки нет» здесь нельзя.
func identityLandingOfProfile(text string) (iam, edge string) {
	var tree map[string]any
	if err := yaml.Unmarshal([]byte(text), &tree); err != nil {
		return "", ""
	}
	if v, ok := lookup(tree, "kaname", "config", "authn", "identityProvider"); ok {
		iam, _ = v.(string)
	}
	if v, ok := lookup(tree, "api-gateway", "authn", "identityProvider"); ok {
		edge, _ = v.(string)
	}
	return iam, edge
}

// identityLandingOfChain — посадка, которую получает ЦЕПОЧКА профилей.
//
// Спрашивается у цепочки, а не у отдельного профиля, по той же причине, что и
// провязка настроек: накладка поднимает продукт вместе со слоем под собой.
// Профили накладываются слева направо — ровно как их накладывает helm, —
// поэтому последнее непустое объявление побеждает.
func identityLandingOfChain(t *testing.T, texts []string) identityLanding {
	t.Helper()
	var l identityLanding
	for _, text := range texts {
		iam, edge := identityLandingOfProfile(text)
		if iam != "" {
			l.IAM = iam
		}
		if edge != "" {
			l.Edge = edge
		}
	}
	if l.lands() {
		return l
	}
	// Цепочка промолчала — значение стенду даёт база подчарта. Это не
	// умолчание «на всякий случай»: базы обеих половин держит проба зонта
	// TestIdentityPostureIsDeclaredByBothSubchartDefaults, и исчезни они —
	// не поднимется ни один стенд.
	l.Base = true
	l.IAM = identityLandingDefault(t, iamLandingDefaults, "config", "authn", "identityProvider")
	l.Edge = identityLandingDefault(t, edgeLandingDefaults, "authn", "identityProvider")
	return l
}

// identityLandingDefault — значение посадки из базовых значений подчарта.
func identityLandingDefault(t *testing.T, path string, keys ...string) string {
	t.Helper()
	v, ok := lookup(readYAML(t, path), keys...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// identityChainLandsIdentity — цепочка объявляет посадку личности, то есть
// стенд решает о человеке и подлежит суду стражей личности.
func identityChainLandsIdentity(t *testing.T, texts []string) bool {
	t.Helper()
	return identityLandingOfChain(t, texts).lands()
}

// shadowedSecondFactors — методы второго фактора, О КОТОРЫХ ПРОФИЛЬ ВЫСКАЗАЛСЯ
// САМ, в обход единственного объявления.
func shadowedSecondFactors(text string) []string {
	var out []string
	for _, m := range secondFactorKey.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// secondFactorKey — имена, высказывание о которых в профиле есть второе мнение
// о втором факторе.
//
// `code` сюда НЕ входит намеренно: вторым фактором он становится только при
// `mfa_enabled`, а само слово в профилях встречается в чужих значениях — гейт с
// ним ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
// Появится посадка, где `code` объявлен вторым фактором, — имя добавляется сюда
// вместе с ней.
var secondFactorKey = regexp.MustCompile(`(?m)^\s+(totp|lookup_secret|webauthn|passkey):`)

// judgedByProviderSettings — судится ли стенд настройкой поставщика личности.
//
// Только посадка `external`: там способы второго фактора включает настройка
// поставщика, и стенд, не доведший её до процесса, оставляет пол «2» без стороны
// службы. На посадке `own` поставщика на стенде нет, и провязка его настройки ни
// о чём не говорит: сторона службы там — корень композиции (стороны посадки выше).
// Судить её здесь значило бы засчитывать строку профиля за провязку компонента,
// которого нет, — ровно так прежняя редакция насчитывала стенд `own` среди
// «доводящих настройки до процесса».
func (l identityLanding) judgedByProviderSettings() bool { return l.side() == landingExternal }

func TestIdentity_EveryStackDeclaresTheSecondFactor(t *testing.T) {
	stacks := deployStacks(t)

	raising, judged, mounting, clean := 0, 0, 0, 0
	postures := map[string]int{}
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		chain := stacks[name]
		texts := make([]string, 0, len(chain))
		for _, prof := range chain {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		landing := identityLandingOfChain(t, texts)
		if !landing.lands() {
			continue
		}
		raising++
		postures[landing.posture()]++
		if !landing.judgedByProviderSettings() {
			continue
		}
		judged++

		if !identityChainMountsOurConfig(texts) {
			t.Errorf("стенд %q (%v) объявляет посадку личности и НЕ доводит до службы наши "+
				"настройки (%s): процесс работает на умолчаниях подчарта поставщика, "+
				"где метода второго фактора нет вовсе. Значит объявленный каталогом "+
				"прав пол уровня «2» на этом стенде недостижим ДЛЯ ВСЕХ",
				name, chain, identityRenderedConfigPath)
			continue
		}
		mounting++

		shadowed := map[string][]string{}
		for i, prof := range chain {
			if sh := shadowedSecondFactors(texts[i]); len(sh) > 0 {
				shadowed[prof] = sh
			}
		}
		if len(shadowed) == 0 {
			clean++
			continue
		}
		t.Errorf("стенд %q доводит наши настройки службы личности до процесса и ПРИ ЭТОМ "+
			"его профили сами высказываются о методах второго фактора: %v.\n"+
			"Процесс получает два источника настроек и сливает их по порядку — то есть "+
			"какая из двух величин победит, решает порядок, которого никто не выбирал. "+
			"Два места об одном предмете, из которых верно одно: метод объявляется "+
			"ОДИН раз, в %s.\n"+
			"Наблюдалось: профиль объявлял метод второго фактора выключенным рядом с "+
			"включённым в единственном объявлении, и замер на стенде читал выключенным "+
			"то, что дерево включает",
			name, shadowed, identityConfigTemplate)
	}

	if raising == 0 {
		t.Fatal("ни один стенд не объявляет посадку личности — ни цепочкой профилей, " +
			"ни базами подчартов. Это не «личности на стендах нет», а исчезнувшая " +
			"предпосылка: проверка беспредметна, и её зелёный ничего не значит")
	}
	if judged == 0 {
		t.Fatalf("ни один стенд не на посадке %s — у проверки провязки настройки поставщика не "+
			"осталось предмета. Это не «все стенды исправны»: снимите проверку вместе с компонентом "+
			"(Ф10, #1276) и назовите это в шапке гейта", landingExternal)
	}
	shapes := make([]string, 0, len(postures))
	for p, n := range postures {
		shapes = append(shapes, fmt.Sprintf("%s %d", p, n))
	}
	sort.Strings(shapes)
	t.Logf("перепись стендов: объявлено %d · объявляют посадку личности %d (%s) · "+
		"судятся настройкой поставщика (посадка %s) %d · доводят её до процесса %d · "+
		"не заводят второго мнения о втором факторе %d",
		len(stacks), raising, strings.Join(shapes, " · "), landingExternal, judged, mounting, clean)
}
