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
//	own        корень композиции пиненной службы      НАША церемония повышения —
//	           доступа: перечень, из которого         OWN_STEP_UP_METHODS
//	           самоотчёт старта выводит
//	           `lane_presentable_acrs`
//
// Прежняя редакция судила ВСЕ стенды настройкой поставщика личности. На посадке
// `own` настройка поставщика не включает ничего — компонента на стенде нет, — а
// консоль не ведёт поток поставщика; поэтому гейт зеленел («30 из 30 достижимы»)
// ровно там, где пол «2» поднять было нечем. Теперь стороны берутся у посадки.
//
// Строки `external` В ТАБЛИЦЕ НЕТ, и это снятие вместе с предметом, а не
// пропуск (#2857). У строки было два предмета, и оба сняты в той же волне:
// последний стенд посадки `external` ушёл на `own` (#2735), а консоль поток
// поставщика `aal=aal2` больше не ведёт и его перечня не объявляет (приёмка F8,
// Р1; #1274). Вместе со строкой сняты разбор настройки поставщика как стороны 1
// и проверка, что стенд `external` доводит её до процесса: судить им стало
// некого. Стенд, вновь объявивший `external`, — отказ с именем посадки
// (sidesOfLanding), а не молчание: пол «2» на ней поднимать нечем. Компонент
// поставщика в чарте остаётся до своего снятия (Ф10, #1276), и его настройку
// читают три других стража личности (потоки доставки, зеркало требования
// подтверждённого адреса, решённые замещения списков — раздел «Кого судят
// четыре стража личности» ниже).
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
// Толкование читает ЛИТЕРАЛ, а служба исполняет ЗНАЧЕНИЕ собранной программы
// (#2691, круг 3). Разрыв между ними закрыт в четырёх местах, и каждое — отказ с
// координатой, а не догадка: пакет читается файлами, которые компилирует сборка
// (goPackageSource), а не всем каталогом; таблица и словарь не пишутся нигде,
// кроме своих объявлений, — ни в пакете, ни в пакетах модуля, его импортирующих
// (ruleStateIsItsDeclaration); перечень корня — возвращаемое значение
// производителя, а не постоянные в любом месте его тела (ownWiredMethods);
// предъявление способа собирает конструктор этого способа
// (presentationsCarryTheirMethod). Имена связываются областью видимости, как их
// связывает компилятор, а не написанием.
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
// бы по обещанию. Опровергает его само поле предъявления: переключатель
// способов `SecondFactorCodeField` строится ИЗ этого перечня
// (`ui-future/shared/src/components/molecules/auth/SecondFactorCodeField/`), и
// способ, объявленный здесь, — это ровно способ, который консоль кладёт в тело
// нашего глагола, а не второе место о том же предмете.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер, ни браузер не нужны,
// поэтому проверка не умеет пропускаться. Сторону службы на `own` он читает
// исходником пиненного модуля (go.mod даёт версию, GOMODCACHE — каталог; тот же
// приём, что у own_ceilings_and_access_keys_umbrella_test.go): подняли пин —
// гейт судит новый корень и новое правило сам.
//
// Какой конструктор предъявления служба зовёт на каком слове записи сессии,
// решает её пакет сессий (`presentationsOf` в
// `internal/apps/kaname/api/humansession`), и его гейт НЕ читает: он толкует
// конструкторы и требует, чтобы каждый способ собирался ровно одним, но не то,
// что слово `totp` уходит именно в конструктор кода. Держат это пробы службы у
// пина: `internal/assurance/level_test.go` (TestLevelOf_WholeTableMatchesRuleR2 —
// конструктор каждого способа по всей таблице правила) и
// `internal/apps/kaname/api/humansession/second_factor_usecase_test.go` (уровень
// «2» сессии после предъявления кода). Правя разбор сессий службы, правь и их.
//
// Состояние пакета правила пишется, по разбору этого гейта, только
// объявлениями и пакетами своего модуля. Директива `//go:linkname` из
// стороннего модуля и запись через `unsafe` по адресу, взятому не оператором `&`,
// разбором не видны; у пина директив нет вовсе (`grep -rl go:linkname` по
// каталогу модуля — 0 файлов @16b5cadead2a). Появятся — толкование обязано их
// читать, а не подразумевать.
package deploy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// ownStepUpMethodsLiteral — объявление перечня нашей церемонии. Граница слова
// стоит перед именем: без неё перечень узнавался бы и внутри любого имени,
// оканчивающегося на `OWN_STEP_UP_METHODS`.
var ownStepUpMethodsLiteral = regexp.MustCompile(`(?s)\bOWN_STEP_UP_METHODS\s*=\s*\[(.*?)\]`)
var quotedToken = regexp.MustCompile(`"([a-z_]+)"`)

// parseOwnStepUpMethods читает сторону КОНСОЛИ на посадке `own` — способы,
// которые окно умеет довести до конца в НАШЕЙ церемонии повышения.
func parseOwnStepUpMethods(src string) []string {
	m := ownStepUpMethodsLiteral.FindStringSubmatch(src)
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

// goPackageSource — пакет чужого модуля В ТОМ ВИДЕ, В КАКОМ ЕГО КОМПИЛИРУЕТ
// СБОРКА (#2691, круг 3).
//
// Разбор каталога целиком читал бы не то, что исполняет служба: файл под
// ограничением сборки `ignore`, файл с префиксом «_» или файл чужой ОС лежат в
// каталоге и в двоичный файл не входят. Круг 3 показал цену этого: такой файл нёс
// прежнюю мягкую таблицу, гейт брал её вместо строгой, которую исполняет служба,
// и зеленел. Поэтому файлы отбираются правилами сборки (go/build MatchFile) на
// платформах образа, и пакет, которого сборка не собрала бы либо собрала бы по-
// разному, — отказ errPackageNotAsBuilt, а не догадка:
//
//   - набор файлов зависит от архитектуры (гейт не знает, под какую собран образ);
//   - сборка компилирует не-Go исходник (ассемблер, C) или файл cgo — они пишут
//     состояние пакета мимо того, что читает разбор;
//   - файлы объявляют разные пакеты либо одно имя уровня пакета дважды (прежняя
//     редакция брала первое `rows` и давала позднему файлу перетереть словарь).
//
// Имена связываются ПО ОБЛАСТИ ВИДИМОСТИ, как их связывает компилятор (go/types),
// а не по написанию: `true`, объявленное пакетом, — не предобъявленное `true`, а
// параметр по имени rows — не таблица правила. Импорты при этом не читаются:
// каждый подставляется пустым пакетом, и ошибки, которые это порождает (имя
// импорта не найдено), к связыванию имён самого пакета отношения не имеют.
type goPackageSource struct {
	dir      string // корень модуля
	pkg      string // каталог пакета от корня модуля
	path     string // путь импорта пакета
	fset     *token.FileSet
	bodies   map[string]string    // путь от корня модуля → текст каждого файла каталога, кроме проб `_test.go`
	files    map[string]*ast.File // путь от корня модуля → файл Go, который компилирует сборка
	excluded []string             // файлы Go каталога, которые сборка исключает
	info     *types.Info          // идентификатор → объект, который он называет
	scope    *types.Scope         // область уровня пакета
	outside  *outsideState        // записи в состояние пакета из ДРУГИХ пакетов модуля
}

// errPackageNotAsBuilt — пакет у пина не читается так, как его компилирует сборка:
// вердикта нет.
var errPackageNotAsBuilt = errors.New("пакет у пина не читается так, как его компилирует сборка")

// buildPlatforms — платформы образа службы: стенды поднимаются на linux, а
// архитектуру образа гейт не знает, поэтому набор файлов пакета обязан быть
// одним на обеих, иначе вердикт, верный на одной, был бы догадкой на другой.
var buildPlatforms = []struct{ goos, goarch string }{{"linux", "amd64"}, {"linux", "arm64"}}

func parseGoPackage(moduleDir, modulePath, pkg string) (goPackageSource, error) {
	src := goPackageSource{dir: moduleDir, pkg: pkg, path: modulePath + "/" + pkg, fset: token.NewFileSet(),
		bodies: map[string]string{}}
	entries, err := os.ReadDir(filepath.Join(moduleDir, filepath.FromSlash(pkg)))
	if err != nil {
		return src, err
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := pkg + "/" + name
		body, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(rel))) // #nosec G304 -- путь собран из пина go.mod
		if err != nil {
			return src, err
		}
		src.bodies[rel] = string(body)
	}
	return src, src.compile()
}

// with — копия пакета, в которой один файл заменён (либо добавлен) телом
// инъекции; настоящий разбор при этом не меняется. Файлы отбираются и имена
// связываются заново: инъекция проходит тот же путь, что и пин.
func (s goPackageSource) with(rel, body string) (goPackageSource, error) {
	out := s
	out.bodies = make(map[string]string, len(s.bodies)+1)
	for k, v := range s.bodies {
		out.bodies[k] = v
	}
	out.bodies[rel] = body
	return out, out.compile()
}

// compile — файлы, которые компилирует сборка, их разбор и связывание имён.
func (s *goPackageSource) compile() error {
	built, excluded, err := s.buildSelection()
	if err != nil {
		return err
	}
	s.files, s.excluded = map[string]*ast.File{}, excluded
	files := make([]*ast.File, 0, len(built))
	for _, rel := range built {
		f, err := parser.ParseFile(s.fset, rel, s.bodies[rel], 0)
		if err != nil {
			return err
		}
		if len(files) > 0 && f.Name.Name != files[0].Name.Name {
			return fmt.Errorf("%w: %s объявляет пакет %s, а %s — %s", errPackageNotAsBuilt, rel, f.Name.Name,
				s.fset.Position(files[0].Package).Filename, files[0].Name.Name)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value == `"C"` {
				return fmt.Errorf("%w: %s: файл cgo — код на C пишет состояние пакета мимо разбора", errPackageNotAsBuilt,
					s.coordinate(imp))
			}
		}
		s.files[rel] = f
		files = append(files, f)
	}
	if len(files) == 0 {
		return fmt.Errorf("в %s/%s сборка не компилирует ни одного файла Go", s.dir, s.pkg)
	}
	if err := s.namesDeclaredOnce(); err != nil {
		return err
	}
	s.info = &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: emptyImporter{}, Error: func(error) {}}
	pkg, _ := conf.Check(s.path, s.fset, files, s.info)
	s.scope = pkg.Scope()
	return nil
}

// buildSelection — какие файлы каталога компилирует сборка на платформах образа.
func (s goPackageSource) buildSelection() (built, excluded []string, err error) {
	rels := make([]string, 0, len(s.bodies))
	for rel := range s.bodies {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	var first map[string]bool
	for i, pl := range buildPlatforms {
		got, err := matchedOn(pl.goos, pl.goarch, filepath.Join(s.dir, filepath.FromSlash(s.pkg)), rels, s.bodies)
		if err != nil {
			return nil, nil, err
		}
		if i == 0 {
			first = got
			continue
		}
		for _, rel := range rels {
			if got[rel] != first[rel] {
				return nil, nil, fmt.Errorf("%w: набор файлов пакета зависит от архитектуры: %s компилируется на %s/%s "+
					"(%t) и на %s/%s (%t)", errPackageNotAsBuilt, rel, buildPlatforms[0].goos, buildPlatforms[0].goarch,
					first[rel], pl.goos, pl.goarch, got[rel])
			}
		}
	}
	for _, rel := range rels {
		isGo := strings.HasSuffix(rel, ".go")
		switch {
		case first[rel] && isGo:
			built = append(built, rel)
		case first[rel]:
			return nil, nil, fmt.Errorf("%w: %s: сборка компилирует не-Go исходник — он пишет состояние пакета мимо "+
				"разбора", errPackageNotAsBuilt, rel)
		case isGo:
			excluded = append(excluded, rel)
		}
	}
	return built, excluded, nil
}

// matchedOn — файлы, которые сборка на платформе goos/goarch включает в пакет
// каталога dir; тела берутся из bodies (инъекция не пишет на диск).
func matchedOn(goos, goarch, dir string, rels []string, bodies map[string]string) (map[string]bool, error) {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = goos, goarch, false
	byName := make(map[string]string, len(rels))
	for _, rel := range rels {
		byName[path.Base(rel)] = bodies[rel]
	}
	ctx.OpenFile = func(p string) (io.ReadCloser, error) {
		body, ok := byName[filepath.Base(p)]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return io.NopCloser(strings.NewReader(body)), nil
	}
	out := map[string]bool{}
	for _, rel := range rels {
		ok, err := ctx.MatchFile(dir, path.Base(rel))
		if err != nil {
			return nil, fmt.Errorf("%w: %s: правила сборки не прочитаны: %w", errPackageNotAsBuilt, rel, err)
		}
		out[rel] = ok
	}
	return out, nil
}

// namesDeclaredOnce — каждое имя уровня пакета (и каждый метод типа) объявлено
// компилируемыми файлами один раз. Пакет с повтором сборка не собирает, и
// толкование одного из двух объявлений было бы выбором, которого служба не делает.
func (s goPackageSource) namesDeclaredOnce() error {
	at := map[string][]string{}
	note := func(name string, n ast.Node) {
		if name != "_" && name != "init" {
			at[name] = append(at[name], s.coordinate(n))
		}
	}
	for _, rel := range s.sortedFiles() {
		for _, decl := range s.files[rel].Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					note(d.Name.Name, d.Name)
				} else {
					note(recvTypeName(d)+"."+d.Name.Name, d.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						note(sp.Name.Name, sp.Name)
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							note(n.Name, n)
						}
					}
				}
			}
		}
	}
	names := make([]string, 0, len(at))
	for n, where := range at {
		if len(where) > 1 {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return fmt.Errorf("%w: имя %s объявлено %d раза (%s) — такой пакет сборка не собирает", errPackageNotAsBuilt,
		names[0], len(at[names[0]]), strings.Join(at[names[0]], ", "))
}

// emptyImporter — импорт, подставленный пустым пакетом: связыванию имён самого
// пакета содержимое импортов не нужно.
type emptyImporter struct{}

func (emptyImporter) Import(p string) (*types.Package, error) {
	pkg := types.NewPackage(p, path.Base(p))
	pkg.MarkComplete()
	return pkg, nil
}

// coordinate — `файл:строка` узла.
func (s goPackageSource) coordinate(n ast.Node) string {
	pos := s.fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
}

// isPackageObject — выражение есть имя, которое называет объект УРОВНЯ ПАКЕТА с
// этим именем (а не локальное, затеняющее его).
func (s goPackageSource) isPackageObject(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name != name {
		return false
	}
	obj := s.scope.Lookup(name)
	return obj != nil && s.info.Uses[id] == obj
}

// isPredeclared — выражение есть предобъявленное имя языка (`true`, `false`,
// `nil`), а не одноимённое объявление пакета.
func (s goPackageSource) isPredeclared(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name && s.info.Uses[id] == types.Universe.Lookup(name)
}

// importOf — выражение есть имя импорта пакета importPath.
func (s goPackageSource) importOf(e ast.Expr, importPath string) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := s.info.Uses[id].(*types.PkgName)
	return ok && pn.Imported().Path() == importPath
}

// posNode — позиция объекта, которому нужен отказ с координатой, а узла под
// рукой нет (объявление найдено областью видимости, а не обходом).
type posNode token.Pos

func (p posNode) Pos() token.Pos { return token.Pos(p) }
func (p posNode) End() token.Pos { return token.Pos(p) }

// writeTargets — выражения, в чьё хранилище пишет файл: левые части
// присваиваний, операнды `++`/`--` и `&` (взятый адрес — запись, отложенная до
// любого места, куда он уйдёт), переменные обхода `for k, v = range`.
func writeTargets(f *ast.File) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			out = append(out, n.Lhs...)
		case *ast.IncDecStmt:
			out = append(out, n.X)
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				out = append(out, n.X)
			}
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil {
						out = append(out, e)
					}
				}
			}
		}
		return true
	})
	return out
}

// accessRoot — идентификатор переменной, чьё хранилище называет выражение
// `v`, `v.f`, `v[i]`, `*v.f`, `(v)`, и путь импорта, если переменная названа
// через него (`pkg.v`, `pkg.v.f`). Выражение, не называющее переменную (вызов,
// литерал), корня не имеет.
func (s goPackageSource) accessRoot(e ast.Expr) (id *ast.Ident, importPath string) {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.SelectorExpr:
			if q, ok := x.X.(*ast.Ident); ok {
				if pn, ok := s.info.Uses[q].(*types.PkgName); ok {
					return x.Sel, pn.Imported().Path()
				}
			}
			e = x.X
		case *ast.Ident:
			return x, ""
		default:
			return nil, ""
		}
	}
}

// predeclaredStayTheLanguage — пакет не объявляет имён, которые толкование читает
// как предобъявленные: `true`, объявленное пакетом, превратило бы «подошедшая
// строка выдаёт сессию» в «выдаёт то, что пакет назвал true» (круг 3: `const true
// = false` гейт читал выдачей, служба — отказом).
func predeclaredStayTheLanguage(src goPackageSource, names ...string) error {
	for _, name := range names {
		if obj := src.scope.Lookup(name); obj != nil {
			return src.notInterpretable(posNode(obj.Pos()), "пакет объявляет предобъявленное имя `%s` — толкование "+
				"читало бы его языком, а служба исполняет объявление пакета", name)
		}
	}
	return nil
}

// ruleStateIsItsDeclaration — значение, которое исполняет служба, есть ЛИТЕРАЛ
// объявления, который толкует гейт (круг 3).
//
// Толкование читает литерал `rows` и литералы словаря, а служба исполняет их
// ЗНАЧЕНИЯ после инициализации программы: таблица, переписанная в `init`, давала
// гейту «30 достижимы» при правиле службы, по которому пароль с кодом — «1».
// Поэтому отказ с координатой — всякая иная связь с этим состоянием:
//
//   - таблица `rows` употреблена где-либо, кроме своего объявления и аргумента
//     `LevelOf` (запись, чтение с псевдонимом, адрес — всё одно: значение уходит
//     туда, где толкование его не видит);
//   - постоянная словаря переписана, у неё взят адрес — в этом пакете либо в
//     любом пакете модуля, который его импортирует; пакет импортирован точкой
//     (имена словаря тогда пишутся без квалификатора, и разбор чужого пакета их
//     не свяжет);
//   - у типа способа есть метод с получателем-указателем: вызов его на постоянной
//     берёт адрес неявно.
//
// Чтение словаря — не отказ: значение от него не меняется. Имена связываются
// областью видимости, поэтому параметр `rows` чужой функции таблицей не является.
func ruleStateIsItsDeclaration(src goPackageSource, tableArg *ast.Ident, vocab map[string]string) error {
	table := src.info.Uses[tableArg]
	for _, rel := range src.sortedFiles() {
		var at []*ast.Ident
		ast.Inspect(src.files[rel], func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id != tableArg && src.info.Uses[id] == table {
				at = append(at, id)
			}
			return true
		})
		if len(at) > 0 {
			return src.notInterpretable(at[0], "`%s` употреблена вне объявления и вызова исполнителя — служба "+
				"исполняет её значение, а толкование читает литерал", tableArg.Name)
		}
	}

	vocabVar := map[types.Object]string{}
	var vocabType *types.TypeName
	for c := range vocab {
		obj, ok := src.scope.Lookup(c).(*types.Var)
		if !ok {
			return fmt.Errorf("%w: постоянная словаря %s — не переменная уровня пакета", errRuleNotInterpretable, c)
		}
		vocabVar[obj] = c
		if named, ok := obj.Type().(*types.Named); ok {
			vocabType = named.Obj()
		}
	}
	for _, rel := range src.sortedFiles() {
		for _, e := range writeTargets(src.files[rel]) {
			if id, qual := src.accessRoot(e); id != nil && qual == "" && vocabVar[src.info.Uses[id]] != "" {
				return src.notInterpretable(id, "постоянная словаря %s переписывается вне объявления: `%s`",
					id.Name, src.text(e))
			}
		}
		for _, decl := range src.files[rel].Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
				continue
			}
			star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			if id := typeNameIdent(star.X); id != nil && vocabType != nil && src.info.Uses[id] == vocabType {
				return src.notInterpretable(fd, "у способа словаря метод с получателем-указателем `%s` — его вызов "+
					"на постоянной переписывает её мимо объявления", fd.Name.Name)
			}
		}
	}

	if src.outside == nil || src.outside.walked == 0 || len(src.outside.importers) == 0 {
		return fmt.Errorf("%w: пакеты модуля, импортирующие пакет правила, не осмотрены — запись в словарь "+
			"снаружи не исключена", errRuleNotInterpretable)
	}
	for _, p := range src.outside.importers {
		for _, rel := range p.sortedFiles() {
			for _, imp := range p.files[rel].Imports {
				if path, _ := strconv.Unquote(imp.Path.Value); path == src.path && imp.Name != nil && imp.Name.Name == "." {
					return fmt.Errorf("%w: %s: пакет правила импортирован точкой — имена словаря там пишутся без "+
						"квалификатора, и запись в них разбор не свяжет", errRuleNotInterpretable, p.coordinate(imp))
				}
			}
			for _, e := range writeTargets(p.files[rel]) {
				if id, qual := p.accessRoot(e); id != nil && qual == src.path && vocab[id.Name] != "" {
					return fmt.Errorf("%w: %s: постоянная словаря %s переписывается вне объявления — пакетом %s: `%s`",
						errRuleNotInterpretable, p.coordinate(id), id.Name, p.pkg, p.text(e))
				}
			}
		}
	}
	return nil
}

// typeNameIdent — имя типа в записи `T`, `T[P]`, `T[P, Q]`.
func typeNameIdent(e ast.Expr) *ast.Ident {
	switch x := e.(type) {
	case *ast.Ident:
		return x
	case *ast.IndexExpr:
		return typeNameIdent(x.X)
	case *ast.IndexListExpr:
		return typeNameIdent(x.X)
	}
	return nil
}

// outsideState — пакеты модуля, импортирующие пакет правила: только они могут
// писать в его состояние снаружи. Пакет правила внутренний (`internal/`), и
// импортировать его может лишь код своего модуля, поэтому обход модуля — полный
// перечень таких писателей, а не выборка.
type outsideState struct {
	importers []goPackageSource // в том виде, в каком их компилирует сборка
	walked    int               // файлов Go модуля (без проб) прочитано обходом
}

// withImporter — копия пакета правила, у которой пакет-импортёр заменён (либо
// добавлен) инъекцией; настоящий обход при этом не меняется.
func (s goPackageSource) withImporter(p goPackageSource) goPackageSource {
	out := s
	o := &outsideState{walked: s.outside.walked}
	replaced := false
	for _, q := range s.outside.importers {
		if q.pkg == p.pkg {
			q, replaced = p, true
		}
		o.importers = append(o.importers, q)
	}
	if !replaced {
		o.importers = append(o.importers, p)
	}
	out.outside = o
	return out
}

// outsideByModule — обход модуля один на каталог: он читает весь модуль, а
// пробы пакета спрашивают его много раз об одном и том же пине.
var outsideByModule sync.Map // каталог модуля и путь пакета правила → *outsideState

// readOutside — пакеты модуля, импортирующие importPath, кроме самого пакета
// skipPkg. Каталоги, которые сборка не читает (`testdata`, начинающиеся с «.» и
// «_»), не обходятся; пакет-импортёр читается так же, как пакет правила, —
// файлами, которые компилирует сборка.
func readOutside(moduleDir, modulePath, importPath, skipPkg string) (*outsideState, error) {
	key := moduleDir + "\x00" + importPath
	if o, ok := outsideByModule.Load(key); ok {
		return o.(*outsideState), nil
	}
	o := &outsideState{}
	dirs := map[string]bool{}
	err := filepath.WalkDir(moduleDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != moduleDir && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		o.walked++
		rel, err := filepath.Rel(moduleDir, filepath.Dir(p))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == skipPkg || dirs[rel] {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			// Файл, который не разбирается, решит сборка: пакет читается ниже
			// её правилами, и компилируемый файл с ошибкой — отказ, а не пропуск.
			dirs[rel] = true
			return nil
		}
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == importPath {
				dirs[rel] = true
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		p, err := parseGoPackage(moduleDir, modulePath, d)
		if err != nil {
			return nil, fmt.Errorf("пакет-импортёр %s: %w", d, err)
		}
		o.importers = append(o.importers, p)
	}
	outsideByModule.Store(key, o)
	return o, nil
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
// самоотчёту старта. Возвращает имена словаря и координату перечня.
//
// Путь чтения — тот же, что у самой службы: вызов наблюдателя провязки →
// аргумент способов → метод, который его производит → ЗНАЧЕНИЕ, которое метод
// возвращает. Толкуется каждый `return` производителя (кроме возвратов из
// замыканий в его теле — это не его значение):
//
//	return nil                                   — полоса не поднята, способов нет;
//	return []assurance.Method{assurance.MethodX, …} — голый литерал перечня, ровно один.
//
// Иное — отказ errNoSignInList с координатой возврата: перечень, собранный
// помощником, срезом или переменной, — не тот, что написан в литерале (круг 3:
// разбор собирал постоянные В ЛЮБОМ месте тела, и помощник, отдававший из
// перечня первый способ, читался перечнем целиком).
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
				where = append(where, root.coordinate(fd))
			}
		}
	}
	if len(found) != 1 {
		return nil, "", fmt.Errorf("%w: метод %s объявлен %d раз (%v)", errNoSignInList, producer, len(found), where)
	}

	assurancePath := productModuleprefix + kanameModulePart + "/" + kanameAssurancePackage
	var lists []*ast.CompositeLit
	var odd []*ast.ReturnStmt
	ast.Inspect(found[0].Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			switch {
			case len(n.Results) == 1 && root.isPredeclared(n.Results[0], "nil"):
			case len(n.Results) == 1 && isMethodListLiteral(root, n.Results[0], assurancePath):
				lists = append(lists, n.Results[0].(*ast.CompositeLit))
			default:
				odd = append(odd, n)
			}
		}
		return true
	})
	if len(odd) > 0 {
		return nil, "", fmt.Errorf("%w: %s: %s возвращает `%s` — не голый литерал `[]assurance.Method{…}` и не `nil`: "+
			"самоотчёт получает значение, которое разбор литерала не видит", errNoSignInList, root.coordinate(odd[0]),
			producer, root.text(odd[0]))
	}
	if len(lists) != 1 {
		at := where[0]
		var each []string
		for _, l := range lists {
			each = append(each, root.coordinate(l))
		}
		if len(each) > 0 {
			at = each[0]
		}
		return nil, "", fmt.Errorf("%w: %s: %s возвращает перечней %d (%s), ждали ровно один — какой из них получает "+
			"самоотчёт, решает ход исполнения, а не литерал", errNoSignInList, at, producer, len(lists),
			strings.Join(each, ", "))
	}
	list := lists[0]
	at := root.coordinate(list)

	seen := map[string]bool{}
	var unknown []string
	for _, el := range list.Elts {
		sel, ok := el.(*ast.SelectorExpr)
		if !ok || !root.importOf(sel.X, assurancePath) {
			return nil, at, fmt.Errorf("%w: %s: элемент перечня `%s` — не постоянная словаря службы", errNoSignInList,
				root.coordinate(el), root.text(el))
		}
		name, ok := vocab[sel.Sel.Name]
		if !ok {
			unknown = append(unknown, sel.Sel.Name)
			continue
		}
		seen[name] = true
	}
	if len(unknown) > 0 {
		return nil, at, fmt.Errorf("%w: %s называет постоянные вне словаря службы: %v", errNoSignInList, at, unknown)
	}
	// Пустой литерал — не «служба не провязала ничего»: ветка «полоса не поднята»
	// возвращает nil рядом с перечнем, а не вместо него.
	if len(seen) == 0 {
		return nil, at, fmt.Errorf("%w: %s не называет ни одной постоянной словаря службы", errNoSignInList, at)
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, at, nil
}

// isMethodListLiteral — `[]<импорт пакета правила>.Method{…}`.
func isMethodListLiteral(root goPackageSource, e ast.Expr, assurancePath string) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	arr, ok := lit.Type.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	sel, ok := arr.Elt.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Method" && root.importOf(sel.X, assurancePath)
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
	if id := typeNameIdent(typ); id != nil {
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

// ruleLadderIsTheEvaluator — правило служба исполняет ЭТОЙ таблицей, над ВСЕМ
// предъявленным и ПЕРВОЙ подошедшей строкой. Толкуется каждый узел исполнителя, а
// не форма цикла (круг 2: оценщик пропускал любое присваивание и любой return, не
// судил второй аргумент `LevelOf` и аргумент условия строки — таблица, обрезанная
// до обхода, часть предъявленного и пустое множество толковались как правило пина):
//
//	func LevelOf(p []Presentation) (Level, bool) { return levelOf(rows, p) }
//	func levelOf(table []row, p []Presentation) (Level, bool) {
//		s := presented(p)                 // связывание необязательно: presented(p) можно подать прямо в условие
//		for _, r := range table { if r.holds(s) { return r.level, true } }
//		return "", false
//	}
//
// Имена свободны, привязки — нет. Шаг вне этой формы — отказ с координатой узла:
// судить по нему значило бы судить правилом, которое служба исполняет иначе.
func ruleLadderIsTheEvaluator(src goPackageSource) (tableArg *ast.Ident, err error) {
	entry, err := src.funcDecl("", "LevelOf")
	if err != nil {
		return nil, err
	}
	given := paramNames(entry.Type)
	if len(given) != 1 {
		return nil, src.notInterpretable(entry, "`LevelOf` принимает %d параметров, ждали одно предъявленное", len(given))
	}
	switch len(entry.Body.List) {
	case 0:
		return nil, src.notInterpretable(entry, "`LevelOf` с пустым телом")
	case 1:
	default:
		return nil, src.notInterpretable(entry.Body.List[0], "`LevelOf` несёт шаг помимо вызова исполнителя: `%s`",
			src.text(entry.Body.List[0]))
	}
	var call *ast.CallExpr
	var fn *ast.Ident
	if ret, ok := entry.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
		if c, ok := ret.Results[0].(*ast.CallExpr); ok {
			call = c
			fn, _ = c.Fun.(*ast.Ident)
		}
	}
	if fn == nil || len(call.Args) != 2 {
		return nil, src.notInterpretable(entry.Body.List[0], "`LevelOf` не возвращает вызов исполнителя над таблицей "+
			"и предъявленным: `%s`", src.text(entry.Body.List[0]))
	}
	if !src.isPackageObject(call.Args[0], "rows") {
		return nil, src.notInterpretable(call.Args[0], "`LevelOf` не считает уровень таблицей `rows` (подано `%s`) — "+
			"толкование судило бы таблицей, которую служба не исполняет", src.text(call.Args[0]))
	}
	if !isIdent(call.Args[1], given[0]) {
		return nil, src.notInterpretable(call.Args[1], "`LevelOf` подаёт исполнителю не предъявленное целиком (`%s`), "+
			"а `%s` — служба судила бы не всё, что предъявлено", given[0], src.text(call.Args[1]))
	}
	eval, err := src.funcDecl("", fn.Name)
	if err != nil {
		return nil, err
	}
	l, err := newRuleLadder(src, eval)
	if err != nil {
		return nil, err
	}
	if err := l.walk(eval.Body.List); err != nil {
		return nil, err
	}
	return call.Args[0].(*ast.Ident), nil
}

// ruleLadder — исполнитель правила с привязками его узлов: имя таблицы, имя
// предъявленного, тип множества, о котором спрашивают строки, и имя множества,
// если оно связано до обхода.
type ruleLadder struct {
	src       goPackageSource
	evaluator string
	table     string
	given     string
	setType   string
	set       string
	at        ast.Node
}

// newRuleLadder — привязки параметров исполнителя: первый — таблица строк
// `[]<строка>`, второй — предъявленное; тип множества берётся у поля `holds`
// строки, то есть ровно тот, о котором спрашивают условия таблицы.
func newRuleLadder(src goPackageSource, eval *ast.FuncDecl) (ruleLadder, error) {
	l := ruleLadder{src: src, evaluator: eval.Name.Name, at: eval}
	params := paramNames(eval.Type)
	if len(params) != 2 || len(eval.Type.Params.List) != 2 {
		return l, src.notInterpretable(eval, "исполнитель %s принимает не (таблица, предъявленное): параметров %d",
			l.evaluator, len(params))
	}
	l.table, l.given = params[0], params[1]
	arr, ok := eval.Type.Params.List[0].Type.(*ast.ArrayType)
	var rowType *ast.Ident
	if ok && arr.Len == nil {
		rowType, _ = arr.Elt.(*ast.Ident)
	}
	if rowType == nil {
		return l, src.notInterpretable(eval.Type.Params.List[0], "таблица исполнителя %s — не срез именованных строк",
			l.evaluator)
	}
	setType, err := rowHoldsSetType(src, rowType.Name)
	if err != nil {
		return l, err
	}
	l.setType = setType
	return l, nil
}

// rowHoldsSetType — тип множества, которое принимает условие строки:
// `holds func(<тип>) bool` у структуры строки.
func rowHoldsSetType(src goPackageSource, rowType string) (string, error) {
	for _, rel := range src.sortedFiles() {
		for _, decl := range src.files[rel].Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != rowType {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					for _, f := range st.Fields.List {
						if len(f.Names) != 1 || f.Names[0].Name != "holds" {
							continue
						}
						if ft, ok := f.Type.(*ast.FuncType); ok && len(ft.Params.List) == 1 && len(ft.Params.List[0].Names) <= 1 {
							if id, ok := ft.Params.List[0].Type.(*ast.Ident); ok {
								return id.Name, nil
							}
						}
						return "", src.notInterpretable(f, "условие строки %s — не `holds func(<множество>) bool`", rowType)
					}
				}
				return "", src.notInterpretable(ts, "строка %s без условия `holds`", rowType)
			}
		}
	}
	return "", fmt.Errorf("%w: тип строки правила %s не найден", errRuleNotInterpretable, rowType)
}

// walk — тело исполнителя по шагам лестницы: [связывание множества], обход
// таблицы, «сессия не выдаётся». Иной шаг, иной порядок или недостающий шаг —
// отказ.
func (l *ruleLadder) walk(body []ast.Stmt) error {
	const (
		beforeLadder = iota
		afterLadder
		done
	)
	stage := beforeLadder
	for _, st := range body {
		switch st := st.(type) {
		case *ast.AssignStmt:
			if stage != beforeLadder || l.set != "" || st.Tok != token.DEFINE || len(st.Lhs) != 1 || len(st.Rhs) != 1 ||
				!l.isSetConversionCall(st.Rhs[0]) {
				return l.src.notInterpretable(st, "исполнитель %s несёт шаг вне лестницы: `%s`", l.evaluator, l.src.text(st))
			}
			name, ok := st.Lhs[0].(*ast.Ident)
			if !ok || name.Name == "_" || name.Name == l.table || name.Name == l.given {
				return l.src.notInterpretable(st, "исполнитель %s связывает множество предъявленного не новым именем: `%s`",
					l.evaluator, l.src.text(st))
			}
			if !l.isSetConversion(st.Rhs[0]) {
				return l.src.notInterpretable(st.Rhs[0], "множество предъявленного собрано не из `%s` целиком: `%s`",
					l.given, l.src.text(st.Rhs[0]))
			}
			l.set = name.Name
		case *ast.RangeStmt:
			if stage != beforeLadder {
				return l.src.notInterpretable(st, "исполнитель %s несёт шаг вне лестницы: второй обход таблицы", l.evaluator)
			}
			if err := l.firstHoldingRowWins(st); err != nil {
				return err
			}
			stage = afterLadder
		case *ast.ReturnStmt:
			if stage != afterLadder {
				return l.src.notInterpretable(st, "исполнитель %s несёт шаг вне лестницы: `%s`", l.evaluator, l.src.text(st))
			}
			if len(st.Results) != 2 || !isEmptyString(st.Results[0]) || !l.src.isPredeclared(st.Results[1], "false") {
				return l.src.notInterpretable(st, "исполнитель %s без подошедшей строки возвращает `%s`, ждали «сессия "+
					"не выдаётся» (`\"\", false`)", l.evaluator, l.src.text(st))
			}
			stage = done
		default:
			return l.src.notInterpretable(st, "исполнитель %s несёт шаг вне лестницы: `%s`", l.evaluator, l.src.text(st))
		}
	}
	if stage != done {
		return l.src.notInterpretable(l.at, "исполнитель %s: лестница не завершена — ждали обход таблицы и после "+
			"него «сессия не выдаётся»", l.evaluator)
	}
	return nil
}

// firstHoldingRowWins — `for _, r := range <таблица> { if r.holds(<множество>) { return r.level, true } }`.
func (l *ruleLadder) firstHoldingRowWins(st *ast.RangeStmt) error {
	notFirstWins := func(n ast.Node) error {
		return l.src.notInterpretable(n, "исполнитель %s обходит таблицу не «первая строка с истинным условием даёт "+
			"уровень»: `%s`", l.evaluator, l.src.text(n))
	}
	if !isIdent(st.X, l.table) {
		return l.src.notInterpretable(st.X, "исполнитель %s обходит не таблицу `%s`, а `%s`", l.evaluator, l.table,
			l.src.text(st.X))
	}
	row, ok := st.Value.(*ast.Ident)
	if !ok || (st.Key != nil && !isIdent(st.Key, "_")) {
		return notFirstWins(st)
	}
	if row.Name == l.set || row.Name == l.table || row.Name == l.given || row.Name == l.setType {
		return l.src.notInterpretable(st.Value, "строка обхода `%s` затеняет привязку исполнителя %s", row.Name,
			l.evaluator)
	}
	if len(st.Body.List) != 1 {
		return notFirstWins(st)
	}
	ifs, ok := st.Body.List[0].(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil || len(ifs.Body.List) != 1 {
		return notFirstWins(st.Body.List[0])
	}
	call, ok := ifs.Cond.(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, row.Name, "holds") {
		return notFirstWins(ifs.Cond)
	}
	if len(call.Args) != 1 || !(l.set != "" && isIdent(call.Args[0], l.set) || l.isSetConversion(call.Args[0])) {
		return l.src.notInterpretable(call, "исполнитель %s спрашивает строку не о множестве предъявленного, а `%s`",
			l.evaluator, l.src.text(call))
	}
	ret, ok := ifs.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 2 || !isSelector(ret.Results[0], row.Name, "level") || !l.src.isPredeclared(ret.Results[1], "true") {
		return l.src.notInterpretable(ifs.Body.List[0], "исполнитель %s: подошедшая строка возвращает `%s`, ждали её "+
			"уровень и выданную сессию (`%s.level, true`)", l.evaluator, l.src.text(ifs.Body.List[0]), row.Name)
	}
	return nil
}

// isSetConversionCall — `<тип множества>(…)`, с любым аргументом.
func (l *ruleLadder) isSetConversionCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	return ok && l.src.isPackageObject(call.Fun, l.setType)
}

// isSetConversion — `<тип множества>(<предъявленное>)`: множество всего предъявленного.
func (l *ruleLadder) isSetConversion(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	return ok && l.src.isPackageObject(call.Fun, l.setType) && len(call.Args) == 1 && isIdent(call.Args[0], l.given)
}

// isEmptyString — литерал `""`.
func isEmptyString(e ast.Expr) bool {
	bl, ok := e.(*ast.BasicLit)
	return ok && bl.Kind == token.STRING && (bl.Value == `""` || bl.Value == "``")
}

// text — исходник узла одной строкой: отказ называет то, что увидел.
func (s goPackageSource) text(n ast.Node) string {
	var b strings.Builder
	if err := printer.Fprint(&b, s.fset, n); err != nil {
		return fmt.Sprintf("%T", n)
	}
	return strings.Join(strings.Fields(b.String()), " ")
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
	if ret, ok := fd.Body.List[1].(*ast.ReturnStmt); !ok || len(ret.Results) != 1 || !src.isPredeclared(ret.Results[0], "false") {
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
	if ret, ok := ifs.Body.List[0].(*ast.ReturnStmt); !ok || len(ret.Results) != 1 || !src.isPredeclared(ret.Results[0], "true") {
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
				if !ok || vocab[id.Name] == "" || !src.isPackageObject(id, id.Name) {
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

// presentationsCarryTheirMethod — предъявление способа m несёт способ m (круг 3).
//
// Гейт считает предъявление провязанного способа предъявлением ЭТОГО способа:
// множество {password, totp} он спрашивает у правила как has(password) и
// has(totp). Служба же предъявление СОБИРАЕТ конструктором пакета правила, и
// конструктор, собирающий под своим именем другой способ, дал бы службе иной
// уровень, чем гейту (круг 3: конструктор кода по времени, собиравший код
// восстановления, — гейт «30 достижимы», служба: пароль с кодом — «1»). Поэтому
// толкуется каждый экспортированный конструктор типа предъявления:
//
//	func <Имя>(…) Presentation { return Presentation{method: MethodX, <флаги>…} }
//
// и каждый способ словаря обязан собираться ровно одним из них. Иная форма,
// способ без конструктора либо с двумя — отказ с координатой. Какой конструктор
// служба зовёт на каком слове записи, решает её пакет сессий, а не этот, —
// названо посылкой в шапке файла.
func presentationsCarryTheirMethod(src goPackageSource, vocab map[string]string) error {
	entry, err := src.funcDecl("", "LevelOf")
	if err != nil {
		return err
	}
	var elt *ast.Ident
	if arr, ok := entry.Type.Params.List[0].Type.(*ast.ArrayType); ok && arr.Len == nil {
		elt, _ = arr.Elt.(*ast.Ident)
	}
	ptype, ok := src.info.Uses[elt].(*types.TypeName)
	if elt == nil || !ok {
		return src.notInterpretable(entry.Type, "`LevelOf` принимает не срез предъявлений пакета")
	}
	isPresentation := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && src.info.Uses[id] == ptype
	}

	byMethod := map[string][]*ast.FuncDecl{}
	constructors := 0
	for _, rel := range src.sortedFiles() {
		for _, decl := range src.files[rel].Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !fd.Name.IsExported() || fd.Type.Results == nil {
				continue
			}
			mentions := false
			ast.Inspect(fd.Type.Results, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && src.info.Uses[id] == ptype {
					mentions = true
				}
				return true
			})
			if !mentions {
				continue
			}
			constructors++
			res := fd.Type.Results.List
			if len(res) != 1 || len(res[0].Names) > 1 || !isPresentation(res[0].Type) {
				return src.notInterpretable(fd, "%s возвращает не одно предъявление: `%s` — толкование не знает, "+
					"что в нём собрано", fd.Name.Name, src.text(fd.Type))
			}
			method, err := constructedMethod(src, fd, isPresentation, vocab)
			if err != nil {
				return err
			}
			byMethod[method] = append(byMethod[method], fd)
		}
	}
	if constructors == 0 {
		return src.notInterpretable(elt, "у типа предъявления %s нет ни одного экспортированного конструктора — "+
			"толковать, что предъявляет служба, не на чем", elt.Name)
	}
	names := map[string]bool{}
	for _, m := range vocab {
		names[m] = true
	}
	methods := make([]string, 0, len(names))
	for m := range names {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	for _, m := range methods {
		if n := len(byMethod[m]); n > 1 {
			each := make([]string, 0, n)
			for _, fd := range byMethod[m] {
				each = append(each, src.coordinate(fd)+" "+fd.Name.Name)
			}
			return src.notInterpretable(byMethod[m][0], "способ %s собирают конструкторов %d (%s) — какой из них "+
				"служба зовёт под каким именем, толкование не знает", m, n, strings.Join(each, ", "))
		}
	}
	for _, m := range methods {
		if len(byMethod[m]) == 0 {
			return fmt.Errorf("%w: способ %s не собирает ни один конструктор предъявления (%s) — служба его не "+
				"предъявляет, а гейт считал бы предъявленным", errRuleNotInterpretable, m, src.coordinate(elt))
		}
	}
	return nil
}

// constructedMethod — способ, который собирает конструктор
// `return <Предъявление>{method: MethodX, …}`.
func constructedMethod(src goPackageSource, fd *ast.FuncDecl, isPresentation func(ast.Expr) bool,
	vocab map[string]string,
) (string, error) {
	fail := func(n ast.Node) (string, error) {
		return "", src.notInterpretable(n, "конструктор предъявления %s не вида `return %s{method: MethodX, …}`: "+
			"толкование не знает, какой способ он собирает", fd.Name.Name, src.text(fd.Type.Results.List[0].Type))
	}
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return fail(fd)
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return fail(fd.Body.List[0])
	}
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	if !ok || !isPresentation(lit.Type) {
		return fail(ret)
	}
	var method *ast.Ident
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return fail(el)
		}
		if isIdent(kv.Key, "method") {
			id, ok := kv.Value.(*ast.Ident)
			if !ok || vocab[id.Name] == "" || !src.isPackageObject(id, id.Name) {
				return fail(kv)
			}
			method = id
		}
	}
	if method == nil {
		return fail(lit)
	}
	return vocab[method.Name], nil
}

// readOwnRule — правило уровня пина, толкованное целиком.
func readOwnRule(src goPackageSource, vocab map[string]string) (ownRule, error) {
	levels, err := assuranceLevels(src)
	if err != nil {
		return ownRule{}, err
	}
	if err := predeclaredStayTheLanguage(src, "true", "false"); err != nil {
		return ownRule{}, err
	}
	tableArg, err := ruleLadderIsTheEvaluator(src)
	if err != nil {
		return ownRule{}, err
	}
	if err := ruleStateIsItsDeclaration(src, tableArg, vocab); err != nil {
		return ownRule{}, err
	}
	if err := presentationsCarryTheirMethod(src, vocab); err != nil {
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
			// Имя словаря, связанное не с его объявлением, — не способ словаря;
			// имя вне словаря называется отказом ниже, своей причиной.
			if _, named := vocab[id.Name]; named && !src.isPackageObject(id, id.Name) {
				return nil, src.notInterpretable(e, "способ `%s` в %s связан не с постоянной словаря пакета", id.Name,
					sel.Sel.Name)
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
	// до «2» поднимают обе стороны сразу. Считает его строитель сторон посадки
	// правилом этой посадки (own — правило службы у пина).
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
	modulePath := productModuleprefix + kanameModulePart
	pin = productModulePins(t, "..")[kanameModulePart]
	var err error
	if root, err = parseGoPackage(moduleDir, modulePath, kanameMainPackage); err != nil {
		t.Fatalf("корень службы доступа у пина %s не разобран: %v", pin, err)
	}
	if assurance, err = parseGoPackage(moduleDir, modulePath, kanameAssurancePackage); err != nil {
		t.Fatalf("пакет правила уровня у пина %s не разобран: %v", pin, err)
	}
	if assurance.outside, err = readOutside(moduleDir, modulePath, assurance.path, kanameAssurancePackage); err != nil {
		t.Fatalf("пакеты модуля у пина %s, импортирующие пакет правила, не прочитаны: %v", pin, err)
	}
	return pin, root, assurance
}

// sidesOfLanding — стороны 1 и 2 посадки, прочитанные у дерева и пина.
//
// Посадка вне таблицы — не «судить нечего», а отказ: гейт не знает, кто на ней
// включает способы и какую церемонию ведёт консоль, и молчание на ней было бы
// тем самым зелёным без предмета. Посадка `external` названа отдельно, потому что
// её строка СНЯТА, а не забыта (шапка файла): отказ говорит, почему.
func sidesOfLanding(t *testing.T, landing, console string) (secondFactorSides, error) {
	t.Helper()
	switch landing {
	case landingExternal:
		return secondFactorSides{}, fmt.Errorf("посадка %q: строка снята вместе с предметом — консоль поток "+
			"поставщика `aal=aal2` не ведёт (приёмка F8, Р1), и пол «2» на этой посадке поднимать нечем. "+
			"Вернуть посадку стенду — значит вернуть строку вместе с её сторонами", landing)
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
	// Объём прочитанного: какие файлы пакета толковались (их компилирует сборка),
	// какие нет и почему, и сколько пакетов модуля судилось на запись в словарь.
	t.Logf("перепись пакета правила у пина %s: файлов Go компилирует сборка %d (%s) · исключает %d (%s) · "+
		"файлов Go модуля обойдено %d · пакетов-импортёров судится на запись в словарь %d", pin,
		len(assurance.files), strings.Join(assurance.sortedFiles(), " "), len(assurance.excluded),
		strings.Join(assurance.excluded, " "), assurance.outside.walked, len(assurance.outside.importers))
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
