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
// и ИСПОЛНЯЮТСЯ здесь закрытым толкованием (readOwnRule): своей таблицы «какой
// способ что поднимает» гейт не держит. Сменилась логика строки — сменился и
// вердикт; строка вне толкования — отказ с координатой, а не догадка
// (TestIdentity_OwnRuleIsReadFromThePin). Прежняя редакция держала свою таблицу и
// сверяла с пином лишь наборы имён в строках — круг 1 показал, что снятое в
// таблице требование пароля и «и» вместо «или» в строке правила она пропускала.
//
// Каталог, в котором судить нечего (записей нет, пол не объявлен ни у одной,
// запись без имени метода), — отказ, а не «достижимых 0 из 0».
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
// гейт судит новый корень и новое правило сам.
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
	byFloor, err := catalogFloorsFrom(raw)
	if err != nil {
		t.Fatalf("каталог прав (%s): %v", permissionCatalogEmbed, err)
	}
	return byFloor
}

// catalogFloorsFrom — записи каталога по полу уровня.
//
// Каталог, в котором судить нечего, — отказ, а не пустая перепись: «достижимых 0
// из 0» неотличимо от «ничего не прочитано» (круг 1: пустой каталог давал код 0
// без единой строки переписи). Нечего судить трижды: записей нет; ни одна запись
// не объявляет пола (разбор перестал видеть поле либо поле переехало); у записи
// нет имени метода (находка называла бы пустое место).
func catalogFloorsFrom(raw []byte) (map[string][]string, error) {
	var entries []catalogEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("не разобран: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("записей 0 — судить достижимость полов не на чем")
	}
	byFloor := map[string][]string{}
	nameless, floored := 0, 0
	for _, e := range entries {
		if e.FQN == "" {
			nameless++
		}
		if e.RequiredACRMin != "" {
			floored++
		}
		byFloor[e.RequiredACRMin] = append(byFloor[e.RequiredACRMin], e.FQN)
	}
	if nameless > 0 {
		return nil, fmt.Errorf("записей без поля fqn %d из %d — разбор перестал видеть имя метода", nameless, len(entries))
	}
	if floored == 0 {
		return nil, fmt.Errorf("ни одна из %d записей не объявляет required_acr_min — разбор перестал видеть "+
			"поле пола либо оно переехало; «ни один пол не недостижим» здесь значило бы «ни одного не прочитано»",
			len(entries))
	}
	return byFloor, nil
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
	// Производитель, не назвавший ни одной постоянной, перечень собирает иначе —
	// чтением настройки, помощником, — и разбор его не видит. Это отказ, а не
	// «служба не провязала ничего»: ветка «полоса не поднята» возвращает nil
	// рядом с перечнем, а не вместо него.
	if len(seen) == 0 {
		return nil, where[0], fmt.Errorf("%w: %s не называет ни одной постоянной словаря службы",
			errNoSignInList, where[0])
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, where[0], nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРАВИЛО УРОВНЯ own ТОЛКУЕТСЯ У ПИНА, А НЕ ПЕРЕСКАЗЫВАЕТСЯ ГЕЙТОМ (#2691, круг 2)
//
// Классификатор способов на посадке own — не таблица гейта, а само правило
// службы. Строки `rows` читаются разбором и исполняются здесь так же, как их
// исполняет служба: первая строка лестницы с истинным условием даёт уровень.
// Прежняя редакция держала рядом свою таблицу «какой способ что поднимает» и
// сверяла с пином лишь НАБОРЫ ИМЁН в строках. Круг 1 показал цену этого:
// мутация, снявшая в таблице требование пароля, не роняла ни одной пробы, а
// переписанная логика строки («и» вместо «или») проходила сверку.
//
// Толкование ЗАКРЫТО. Узнаётся ровно следующее:
//   - `s.has(MethodX)` — способ X предъявлен;
//   - `s.<помощник>()` — метод множества вида «есть предъявление способа X с
//     такими-то флагами». Флаги гейту неизвестны, поэтому значение «ложно», если X
//     не предъявлен, и «неизвестно», если предъявлен;
//   - `&&`, `||`, скобки.
// Всё прочее — отказ errRuleNotInterpretable с координатой: судить правилом,
// которого толкование не понимает, значило бы судить догадкой. Отрицания в
// правиле пина нет, поэтому толкование его и не узнаёт: неисполненная ветвь
// разбора была бы утверждением, которое ни одна проба не опровергает.
//
// «Неизвестно» исполняется трёхзначно. Вердикт «достижимо» выносится только на
// достоверном значении, поэтому неизвестная строка может лишь отнять
// достижимость, а добавить её не может.
//
// Что правило исполняет ИМЕННО эта таблица и ИМЕННО первой подошедшей строкой,
// тоже читается у пина (ruleLadderIsTheEvaluator). Без этого толкование судило
// бы таблицей, которую служба, может быть, уже не читает.

// errRuleNotInterpretable — форма правила уровня вне закрытого толкования:
// вердикта нет, а не «пол недостижим».
var errRuleNotInterpretable = errors.New("правило уровня у пина не толкуется")

// ruleValue — трёхзначное значение условия строки; порядок ложно < неизвестно <
// истинно делает «и» минимумом, а «или» — максимумом.
type ruleValue uint8

const (
	ruleFalse ruleValue = iota
	ruleUnknown
	ruleTrue
)

// ruleExpr — толкованное условие строки правила.
type ruleExpr interface {
	eval(presented map[string]bool) ruleValue
	String() string
}

// rulePresented — «способ предъявлен». qualified — имя помощника, который
// требует от предъявления ещё и флагов; пусто — голое `has`.
type rulePresented struct {
	method    string
	qualified string
}

func (e rulePresented) eval(presented map[string]bool) ruleValue {
	switch {
	case !presented[e.method]:
		return ruleFalse
	case e.qualified != "":
		return ruleUnknown
	default:
		return ruleTrue
	}
}

func (e rulePresented) String() string {
	if e.qualified != "" {
		return e.qualified + "(" + e.method + ")"
	}
	return "has(" + e.method + ")"
}

// ruleJoin — «и» (and) либо «или» двух условий.
type ruleJoin struct {
	and         bool
	left, right ruleExpr
}

func (e ruleJoin) eval(presented map[string]bool) ruleValue {
	l, r := e.left.eval(presented), e.right.eval(presented)
	if e.and {
		return min(l, r)
	}
	return max(l, r)
}

func (e ruleJoin) String() string {
	op := " || "
	if e.and {
		op = " && "
	}
	return "(" + e.left.String() + op + e.right.String() + ")"
}

// ruleRow — строка правила: уровень (значение постоянной службы и его ранг на
// лестнице), имя строки и толкованное условие.
type ruleRow struct {
	level string
	rank  int
	name  string
	cond  ruleExpr
}

// ownRule — правило уровня пина в порядке лестницы и координата таблицы.
type ownRule struct {
	rows  []ruleRow
	where string
}

// levelRange — уровень множества предъявленного так, как его даёт служба:
// первая строка лестницы, чьё условие истинно. Строка со значением «неизвестно»
// может дать свой уровень, а может пропустить ход дальше, поэтому ответ —
// диапазон: lo — достоверный нижний уровень, hi — возможный верхний; 0 — «сессия
// не выдаётся».
func (r ownRule) levelRange(presented map[string]bool) (lo, hi int) {
	lo = -1
	note := func(n int) {
		if lo < 0 || n < lo {
			lo = n
		}
		hi = max(hi, n)
	}
	for _, row := range r.rows {
		switch row.cond.eval(presented) {
		case ruleTrue:
			note(row.rank)
			return lo, hi
		case ruleUnknown:
			note(row.rank)
		case ruleFalse:
		}
	}
	note(0)
	return lo, hi
}

// signIn — провязанные способы, которыми сессия ВЫДАЁТСЯ: одного предъявления
// способа правилу достоверно хватает на уровень.
func (r ownRule) signIn(wired []string) []string {
	var out []string
	for _, m := range wired {
		if lo, _ := r.levelRange(map[string]bool{m: true}); lo >= 1 {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// lifting — способы, которыми церемония повышения поднимает сессию до пола floor.
//
// Сессия выдана входом одним способом f. Церемония предъявляет D — способы,
// которые служба провязала И консоль на церемонии ведёт. Подъём состоялся, если
// уровень f∪D достоверно не ниже пола, а уровень самого f достоверно ниже пола
// (иначе поднимал не D, а вход, которого этот гейт не судит). Исключение — f,
// который ведёт сама церемония. Берутся только МИНИМАЛЬНЫЕ D: способ, без
// которого подъём состоялся бы и так, пригодным не называется.
func (r ownRule) lifting(wired, drivable []string, floor int) []string {
	var ceremony []string
	for _, m := range wired {
		if contains(drivable, m) {
			ceremony = append(ceremony, m)
		}
	}
	sort.Strings(ceremony)
	subsets := nonEmptySubsets(ceremony)

	used := map[string]bool{}
	for _, f := range r.signIn(wired) {
		_, loginHi := r.levelRange(map[string]bool{f: true})
		var lifts [][]string
		for _, d := range subsets {
			if coversAny(d, lifts) {
				continue
			}
			presented := map[string]bool{f: true}
			for _, m := range d {
				presented[m] = true
			}
			if lo, _ := r.levelRange(presented); lo >= floor && (loginHi < floor || contains(d, f)) {
				lifts = append(lifts, d)
			}
		}
		for _, d := range lifts {
			for _, m := range d {
				used[m] = true
			}
		}
	}
	out := make([]string, 0, len(used))
	for m := range used {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// floors — какие полы достижимы на посадке own: «1» — вход, ступени выше —
// подъём церемонией (lifting).
func (r ownRule) floors(wired, drivable []string) map[string]bool {
	out := map[string]bool{"0": true, "1": len(r.signIn(wired)) > 0}
	for _, row := range r.rows {
		if row.rank >= 2 {
			out[row.level] = len(r.lifting(wired, drivable, row.rank)) > 0
		}
	}
	return out
}

// nonEmptySubsets — непустые подмножества перечня по возрастанию размера.
func nonEmptySubsets(ms []string) [][]string {
	var out [][]string
	for mask := 1; mask < 1<<len(ms); mask++ {
		var s []string
		for i, m := range ms {
			if mask&(1<<i) != 0 {
				s = append(s, m)
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) < len(out[j]) })
	return out
}

// coversAny — содержит ли d целиком одно из уже найденных подмножеств.
func coversAny(d []string, found [][]string) bool {
	for _, f := range found {
		all := true
		for _, m := range f {
			if !contains(d, m) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// ── разбор правила у пина ────────────────────────────────────────────────────

// notInterpretable — отказ толкования с координатой узла.
func (s goPackageSource) notInterpretable(n ast.Node, format string, args ...any) error {
	pos := s.fset.Position(n.Pos())
	return fmt.Errorf("%w: %s:%d: %s", errRuleNotInterpretable, pos.Filename, pos.Line, fmt.Sprintf(format, args...))
}

// funcDecl — единственное объявление функции (recv == "") либо метода типа recv.
func (s goPackageSource) funcDecl(recv, name string) (*ast.FuncDecl, error) {
	var found []*ast.FuncDecl
	for _, rel := range s.sortedFiles() {
		for _, decl := range s.files[rel].Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != name || fd.Body == nil {
				continue
			}
			if recvTypeName(fd) == recv {
				found = append(found, fd)
			}
		}
	}
	if len(found) != 1 {
		return nil, fmt.Errorf("%w: %s.%s объявлен %d раз", errRuleNotInterpretable, recv, name, len(found))
	}
	return found[0], nil
}

func recvTypeName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) != 1 {
		return ""
	}
	typ := fd.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// paramNames — имена параметров функции по порядку.
func paramNames(ft *ast.FuncType) []string {
	var out []string
	for _, f := range ft.Params.List {
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// isSelector — `x.sel` при голом идентификаторе x.
func isSelector(e ast.Expr, x, sel string) bool {
	se, ok := e.(*ast.SelectorExpr)
	return ok && isIdent(se.X, x) && se.Sel.Name == sel
}

// assuranceLevels — постоянные уровня службы: имя → значение и ранг.
func assuranceLevels(src goPackageSource) (map[string]ruleRow, error) {
	out := map[string]ruleRow{}
	for _, rel := range src.sortedFiles() {
		for _, decl := range src.files[rel].Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || !isIdent(vs.Type, "Level") || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				bl, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return nil, src.notInterpretable(vs, "уровень %s — не строковая постоянная", vs.Names[0].Name)
				}
				val, err := strconv.Unquote(bl.Value)
				if err != nil {
					return nil, src.notInterpretable(bl, "уровень %s: %v", vs.Names[0].Name, err)
				}
				rank, err := strconv.Atoi(val)
				if err != nil || rank < 1 {
					return nil, src.notInterpretable(bl, "уровень %s = %q — не ступень лестницы", vs.Names[0].Name, val)
				}
				out[vs.Names[0].Name] = ruleRow{level: val, rank: rank}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: постоянных уровня (тип Level) не найдено", errRuleNotInterpretable)
	}
	return out, nil
}

// ruleLadderIsTheEvaluator — правило служба исполняет ЭТОЙ таблицей и ПЕРВОЙ
// подошедшей строкой: `LevelOf` зовёт исполнителя с таблицей `rows`, а
// исполнитель обходит таблицу и возвращает уровень первой строки, чьё условие
// истинно.
func ruleLadderIsTheEvaluator(src goPackageSource) error {
	entry, err := src.funcDecl("", "LevelOf")
	if err != nil {
		return err
	}
	var evaluator string
	if len(entry.Body.List) == 1 {
		if ret, ok := entry.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			if call, ok := ret.Results[0].(*ast.CallExpr); ok && len(call.Args) == 2 && isIdent(call.Args[0], "rows") {
				if fn, ok := call.Fun.(*ast.Ident); ok {
					evaluator = fn.Name
				}
			}
		}
	}
	if evaluator == "" {
		return src.notInterpretable(entry, "`LevelOf` не считает уровень таблицей `rows` — толкование судило бы "+
			"таблицей, которую служба не исполняет")
	}
	eval, err := src.funcDecl("", evaluator)
	if err != nil {
		return err
	}
	params := paramNames(eval.Type)
	if len(params) == 0 {
		return src.notInterpretable(eval, "исполнитель %s без параметра таблицы", evaluator)
	}
	ladders := 0
	for _, st := range eval.Body.List {
		switch st := st.(type) {
		case *ast.AssignStmt, *ast.ReturnStmt:
		case *ast.RangeStmt:
			if !firstHoldingRowWins(st, params[0]) {
				return src.notInterpretable(st, "исполнитель %s обходит таблицу не «первая строка с истинным "+
					"условием даёт уровень»", evaluator)
			}
			ladders++
		default:
			return src.notInterpretable(st, "исполнитель %s несёт шаг вне лестницы", evaluator)
		}
	}
	if ladders != 1 {
		return src.notInterpretable(eval, "исполнитель %s: обходов таблицы %d, ждали один", evaluator, ladders)
	}
	return nil
}

// firstHoldingRowWins — `for _, r := range table { if r.holds(…) { return r.level, … } }`.
func firstHoldingRowWins(st *ast.RangeStmt, table string) bool {
	row, ok := st.Value.(*ast.Ident)
	if !ok || !isIdent(st.X, table) || len(st.Body.List) != 1 {
		return false
	}
	ifs, ok := st.Body.List[0].(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil || len(ifs.Body.List) != 1 {
		return false
	}
	call, ok := ifs.Cond.(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, row.Name, "holds") {
		return false
	}
	ret, ok := ifs.Body.List[0].(*ast.ReturnStmt)
	return ok && len(ret.Results) >= 1 && isSelector(ret.Results[0], row.Name, "level")
}

// presentedPredicate — толкованный метод множества предъявленного.
type presentedPredicate struct {
	byParam  bool   // способ подаётся аргументом (`has(m)`)
	constant string // иначе — постоянная словаря в теле
	flags    bool   // от предъявления требуются ещё и флаги
}

// readPresentedPredicate узнаёт ровно одну форму метода множества:
//
//	for _, p := range s { if p.method == <m | MethodX> [&& p.flag]… { return true } }
//	return false
//
// Истинным такой метод бывает только при предъявленном способе, поэтому
// толкование «ложно без способа, неизвестно со способом» верно при любых флагах.
func readPresentedPredicate(src goPackageSource, recvType, name string, vocab map[string]string) (presentedPredicate, error) {
	fd, err := src.funcDecl(recvType, name)
	if err != nil {
		return presentedPredicate{}, err
	}
	fail := func(n ast.Node) (presentedPredicate, error) {
		return presentedPredicate{}, src.notInterpretable(n, "метод %s.%s не вида «есть предъявление способа "+
			"с флагами»", recvType, name)
	}
	recv := fd.Recv.List[0].Names
	params := paramNames(fd.Type)
	if len(recv) != 1 || len(params) > 1 || len(fd.Body.List) != 2 {
		return fail(fd)
	}
	loop, ok := fd.Body.List[0].(*ast.RangeStmt)
	if !ok || !isIdent(loop.X, recv[0].Name) || len(loop.Body.List) != 1 {
		return fail(fd)
	}
	if ret, ok := fd.Body.List[1].(*ast.ReturnStmt); !ok || len(ret.Results) != 1 || !isIdent(ret.Results[0], "false") {
		return fail(fd)
	}
	item, ok := loop.Value.(*ast.Ident)
	if !ok {
		return fail(loop)
	}
	ifs, ok := loop.Body.List[0].(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil || len(ifs.Body.List) != 1 {
		return fail(loop)
	}
	if ret, ok := ifs.Body.List[0].(*ast.ReturnStmt); !ok || len(ret.Results) != 1 || !isIdent(ret.Results[0], "true") {
		return fail(ifs)
	}

	var conj []ast.Expr
	var flatten func(e ast.Expr)
	flatten = func(e ast.Expr) {
		if be, ok := e.(*ast.BinaryExpr); ok && be.Op == token.LAND {
			flatten(be.X)
			flatten(be.Y)
			return
		}
		conj = append(conj, e)
	}
	flatten(ifs.Cond)

	var pred presentedPredicate
	methodTests := 0
	for _, c := range conj {
		if be, ok := c.(*ast.BinaryExpr); ok && be.Op == token.EQL {
			other := be.Y
			if !isSelector(be.X, item.Name, "method") {
				other = be.X
				if !isSelector(be.Y, item.Name, "method") {
					return fail(c)
				}
			}
			switch {
			case len(params) == 1 && isIdent(other, params[0]):
				pred.byParam = true
			case len(params) == 0:
				id, ok := other.(*ast.Ident)
				if !ok || vocab[id.Name] == "" {
					return fail(c)
				}
				pred.constant = id.Name
			default:
				return fail(c)
			}
			methodTests++
			continue
		}
		flag := c
		if ue, ok := c.(*ast.UnaryExpr); ok && ue.Op == token.NOT {
			flag = ue.X
		}
		if se, ok := flag.(*ast.SelectorExpr); !ok || !isIdent(se.X, item.Name) || se.Sel.Name == "method" {
			return fail(c)
		}
		pred.flags = true
	}
	if methodTests != 1 {
		return fail(ifs)
	}
	return pred, nil
}

// readOwnRule — правило уровня пина, толкованное целиком.
func readOwnRule(src goPackageSource, vocab map[string]string) (ownRule, error) {
	levels, err := assuranceLevels(src)
	if err != nil {
		return ownRule{}, err
	}
	if err := ruleLadderIsTheEvaluator(src); err != nil {
		return ownRule{}, err
	}
	table, fields, err := ruleTable(src)
	if err != nil {
		return ownRule{}, err
	}
	pos := src.fset.Position(table.Pos())
	rule := ownRule{where: fmt.Sprintf("%s:%d", pos.Filename, pos.Line)}
	for _, el := range table.Elts {
		row, err := readRuleRow(src, el, fields, levels, vocab)
		if err != nil {
			return ownRule{}, err
		}
		rule.rows = append(rule.rows, row)
	}
	if len(rule.rows) == 0 {
		return ownRule{}, src.notInterpretable(table, "таблица `rows` пуста")
	}
	return rule, nil
}

// ruleTable — литерал таблицы `rows` и имена полей её строки по порядку.
func ruleTable(src goPackageSource) (*ast.CompositeLit, []string, error) {
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
					return nil, nil, src.notInterpretable(vs, "`rows` — не литерал таблицы")
				}
				arr, ok := table.Type.(*ast.ArrayType)
				if !ok {
					return nil, nil, src.notInterpretable(table, "`rows` — не срез строк")
				}
				elt, ok := arr.Elt.(*ast.Ident)
				if !ok {
					return nil, nil, src.notInterpretable(table, "`rows` — не срез именованных строк")
				}
				fields, err := structFields(src, elt.Name)
				if err != nil {
					return nil, nil, err
				}
				return table, fields, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("%w: таблица строк правила `rows` не найдена", errRuleNotInterpretable)
}

// structFields — имена полей именованной структуры пакета по порядку.
func structFields(src goPackageSource, name string) ([]string, error) {
	for _, rel := range src.sortedFiles() {
		for _, decl := range src.files[rel].Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != name {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return nil, src.notInterpretable(ts, "строка правила %s — не структура", name)
				}
				var out []string
				for _, f := range st.Fields.List {
					for _, n := range f.Names {
						out = append(out, n.Name)
					}
				}
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: тип строки правила %s не найден", errRuleNotInterpretable, name)
}

// readRuleRow — одна строка таблицы: уровень, имя и толкованное условие.
func readRuleRow(src goPackageSource, el ast.Expr, fields []string, levels map[string]ruleRow,
	vocab map[string]string,
) (ruleRow, error) {
	lit, ok := el.(*ast.CompositeLit)
	if !ok {
		return ruleRow{}, src.notInterpretable(el, "строка правила — не литерал")
	}
	byField := map[string]ast.Expr{}
	for i, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok {
				byField[k.Name] = kv.Value
			}
			continue
		}
		if i < len(fields) {
			byField[fields[i]] = e
		}
	}
	lvl, ok := byField["level"].(*ast.Ident)
	if !ok {
		return ruleRow{}, src.notInterpretable(lit, "уровень строки — не постоянная")
	}
	row, ok := levels[lvl.Name]
	if !ok {
		return ruleRow{}, src.notInterpretable(lvl, "уровень %s вне постоянных службы", lvl.Name)
	}
	if bl, ok := byField["name"].(*ast.BasicLit); ok && bl.Kind == token.STRING {
		row.name, _ = strconv.Unquote(bl.Value)
	}
	fn, ok := byField["holds"].(*ast.FuncLit)
	if !ok {
		return ruleRow{}, src.notInterpretable(lit, "условие строки — не литерал функции")
	}
	params := paramNames(fn.Type)
	if len(params) != 1 || len(fn.Type.Params.List) != 1 || len(fn.Body.List) != 1 {
		return ruleRow{}, src.notInterpretable(fn, "условие строки — не `func(s …) bool { return … }`")
	}
	recvType, ok := fn.Type.Params.List[0].Type.(*ast.Ident)
	if !ok {
		return ruleRow{}, src.notInterpretable(fn, "параметр условия — не именованный тип множества")
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ruleRow{}, src.notInterpretable(fn, "условие строки — не единственный `return`")
	}
	cond, err := readRuleCond(src, ret.Results[0], params[0], recvType.Name, vocab)
	if err != nil {
		return ruleRow{}, err
	}
	row.cond = cond
	return row, nil
}

// readRuleCond — условие строки в закрытом толковании.
func readRuleCond(src goPackageSource, e ast.Expr, set, setType string, vocab map[string]string) (ruleExpr, error) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return readRuleCond(src, e.X, set, setType, vocab)
	case *ast.BinaryExpr:
		if e.Op != token.LAND && e.Op != token.LOR {
			break
		}
		l, err := readRuleCond(src, e.X, set, setType, vocab)
		if err != nil {
			return nil, err
		}
		r, err := readRuleCond(src, e.Y, set, setType, vocab)
		if err != nil {
			return nil, err
		}
		return ruleJoin{and: e.Op == token.LAND, left: l, right: r}, nil
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || !isIdent(sel.X, set) {
			break
		}
		pred, err := readPresentedPredicate(src, setType, sel.Sel.Name, vocab)
		if err != nil {
			return nil, err
		}
		constant := pred.constant
		switch {
		case pred.byParam && len(e.Args) == 1:
			id, ok := e.Args[0].(*ast.Ident)
			if !ok {
				return nil, src.notInterpretable(e, "способ в %s — не постоянная словаря", sel.Sel.Name)
			}
			constant = id.Name
		case !pred.byParam && len(e.Args) == 0:
		default:
			return nil, src.notInterpretable(e, "вызов %s с %d аргументами", sel.Sel.Name, len(e.Args))
		}
		method, ok := vocab[constant]
		if !ok {
			return nil, src.notInterpretable(e, "постоянная %s вне словаря службы", constant)
		}
		out := rulePresented{method: method}
		if pred.flags {
			out.qualified = sel.Sel.Name
		}
		return out, nil
	}
	return nil, src.notInterpretable(e, "условие %T вне толкования («has», помощник предъявления, «&&», «||», скобки)", e)
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
	// Floors и Usable — вердикт посадки: какие полы достижимы и какими способами
	// до «2» поднимают обе стороны сразу. Считает его строитель сторон посадки —
	// у каждой посадки своё правило (external — таблица поставщика, own — правило
	// службы у пина).
	Floors map[string]bool
	Usable []string
}

// judgeSecondFactorReach — перепись каталога и находки ОДНОЙ посадки.
//
// Чистая: стороны подаются значением, поэтому инъекция подаёт ей и настоящие
// стороны дерева, и стороны с возвращённым дефектом — не трогая ни дерева, ни пина.
func judgeSecondFactorReach(s secondFactorSides, byFloor map[string][]string) (census, findings []string, usable []string) {
	floors, usable := s.Floors, s.Usable
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
	sides := secondFactorSides{
		Landing:     landingExternal,
		ServiceFrom: identityConfigTemplate,
		ConsoleFrom: stepUpMethodsDeclaration + " STEP_UP_METHODS",
		First:       firstFactorMethods(methods),
		Second:      secondFactorMethods(methods),
		Drivable:    drivable,
	}
	sides.Floors, sides.Usable = attainableFloors(sides.Second, sides.First, sides.Drivable)
	return sides, nil
}

// ownSecondFactorSides — посадка `own`: корень пиненной службы, правило уровня
// пина и перечень нашей церемонии. Какие способы что поднимают, решает правило
// (ownRule), а не таблица гейта.
//
// Пустой перечень консоли здесь — НЕ отсутствие вердикта, а сам предмет: консоль,
// не объявившая способов нашей церемонии, поднимать уровень на этой посадке не
// умеет, и это находка по полу, а не молчание. Отсутствие перечня службы —
// наоборот, отказ: разбор корня обязан что-то увидеть.
func ownSecondFactorSides(pin string, root goPackageSource, vocab map[string]string, rule ownRule,
	console string,
) (secondFactorSides, error) {
	wired, where, err := ownWiredMethods(root, vocab)
	if err != nil {
		return secondFactorSides{}, fmt.Errorf("корень службы доступа у пина %s: %w", pin, err)
	}
	if len(rule.rows) == 0 {
		return secondFactorSides{}, fmt.Errorf("%w: правило у пина %s не подано", errRuleNotInterpretable, pin)
	}
	drivable := parseOwnStepUpMethods(console)
	return secondFactorSides{
		Landing:     landingOwn,
		ServiceFrom: fmt.Sprintf("%s@%s %s · правило %s", productModuleprefix+kanameModulePart, pin, where, rule.where),
		ConsoleFrom: stepUpMethodsDeclaration + " OWN_STEP_UP_METHODS",
		First:       rule.signIn(wired),
		Second:      rule.lifting(wired, wired, 2),
		Drivable:    drivable,
		Floors:      rule.floors(wired, drivable),
		Usable:      rule.lifting(wired, drivable, 2),
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
		vocab := assuranceVocabulary(assurance)
		rule, err := readOwnRule(assurance, vocab)
		if err != nil {
			return secondFactorSides{}, fmt.Errorf("правило уровня у пина %s: %w", pin, err)
		}
		return ownSecondFactorSides(pin, root, vocab, rule, console)
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

// TestIdentity_OwnRuleIsReadFromThePin — посадку own гейт судит правилом уровня
// пина, и правило это толкуется ЦЕЛИКОМ.
//
// Правило живёт во внутреннем пакете службы и сюда не импортируется. Поэтому гейт
// не заводит своей таблицы «способ → уровень»: он читает строки `rows` у пина и
// исполняет их так же, как служба (readOwnRule, закрытое толкование). Строка вне
// толкования — отказ с координатой: судить по ней догадкой значило бы вернуть
// второе правило, расходящееся с первым молча.
func TestIdentity_OwnRuleIsReadFromThePin(t *testing.T) {
	pin, _, assurance := readPinnedKaname(t)
	vocab := assuranceVocabulary(assurance)
	if len(vocab) == 0 {
		t.Fatalf("словарь способов у пина %s не разобран — толковать правило нечем", pin)
	}
	rule, err := readOwnRule(assurance, vocab)
	if err != nil {
		t.Fatalf("правило уровня у пина %s не толкуется: %v — посадку own судить нечем", pin, err)
	}
	rungs := map[string]int{}
	for _, r := range rule.rows {
		rungs[r.level]++
		t.Logf("строка правила у пина %s: «%s» %s — %s", pin, r.level, r.cond, r.name)
	}
	t.Logf("перепись правила у пина %s (%s): словарь %d способов · строк %d · по ступеням %v "+
		"(строка вне толкования — отказ, пропуска нет)", pin, rule.where, len(vocab), len(rule.rows), rungs)
	// Каждый пол выше анонимного, который объявляет каталог, обязан быть ступенью
	// правила: иначе служба его не выдаёт ни при каком предъявлении.
	for floor := range readCatalogFloors(t) {
		if floor == "" || floor == "0" {
			continue
		}
		if rungs[floor] == 0 {
			t.Errorf("каталог объявляет пол «%s», а у правила пина %s такой ступени нет (ступени %v) — "+
				"служба этот уровень не выдаёт ни при каком предъявлении", floor, pin, rungs)
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
