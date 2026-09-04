// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// console_body_contract_test.go — гейт: ни одна форма консоли не кладёт в тело
// запроса ключ, которого нет в сообщении обслуживающего RPC.
//
// Это тот же класс и тот же способ измерения, что у гейта регрессионных suite'ов
// рядом (`newman_body_contract_test.go`), только с другой стороны провода. Класс
// был измерен для чёрного ящика и не измерен для интерфейса, через который ходят
// ЛЮДИ, — и ровно этой дырой поле создания реестра доехало до провода, хотя
// сообщение создания его не несёт: оператор выбирал значение, край выбрасывал
// ключ, ресурс возвращался с другим, за успешным тостом.
//
// Разбор целиком статический (см. console_registry_test.go): ни стенда, ни
// браузера, ни node_modules.
//
// Вердикт — по СОДЕРЖИМОМУ: тест падает счётчиком находок и печатает их все,
// с адресом в файле реестра. Отдельно утверждается КОЛИЧЕСТВО ОСМОТРЕННОГО:
// ноль находок должен быть отличим от нуля прочитанных файлов.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// consoleFinding — расхождение между тем, что кладёт в тело форма, и контрактом
// RPC, который это тело принимает.
type consoleFinding struct {
	bodyFinding
	File   string
	Line   int
	SpecID string
	// Op — какая форма это отправляет: create или update.
	Op string
}

func (f consoleFinding) String() string {
	return fmt.Sprintf("%s:%d [%s %s] %s", f.File, f.Line, f.SpecID, f.Op, f.bodyFinding.String())
}

// consoleRegistryFileName — имя файла реестра ресурсов в каждом remote консоли.
const consoleRegistryFileName = "resource-registry.tsx"

// apiPathLiteral / fieldNameLiteral — НЕЗАВИСИМЫЙ от разбора счёт того, сколько
// ресурсов и сколько объявлений поля лежит в файле.
//
// Он существует затем, что «разбор нашёл ноль нарушений» и «разбор нашёл ноль
// ресурсов» выглядят одинаково. Две цифры считаются разными механизмами по
// одному источнику — расхождение означает дефект одного из них, и гейт обязан
// падать, а не молчать.
var (
	apiPathLiteral = regexp.MustCompile(`(?m)^\s+apiPath:\s*(?:"|[A-Z])`)
	// fieldNameLiteral считает объявления поля по сырому тексту. Имя поля
	// записывается тремя способами — литералом, шаблоном и ссылкой на
	// связывание, — и счётчик обязан видеть все три: считать только литералы
	// значило бы вывести из-под сверки ровно те наборы, которые собраны
	// помощником, то есть самое неочевидное место файла.
	//
	// Требование позиции свойства (начало строки либо после `{`/`,`) отсекает
	// `name:` внутри прозы описаний («repo/name:tag»): без него счётчик по
	// сырому тексту расходился бы с разбором на тексте, к полям отношения не
	// имеющем, и расхождение пришлось бы объяснять каждый раз заново.
	fieldNameLiteral = regexp.MustCompile("(?m)(?:^\\s*|[{,]\\s*)name:\\s*[\"`A-Za-z_$]")
	// exportedStringConst — `export const NAME = "…";` любого модуля консоли.
	// Реестр импортирует такие константы (пути geo-ресурсов), а разбор одного
	// файла о них знать не может.
	// exportedConstName — имя экспортированной константы, без её значения.
	// Нужно затем, чтобы сбор ЗНАЧЕНИЙ брал только импортируемое: внутренние
	// константы компонентов совпадают между файлами сплошь и рядом.
	exportedConstName = regexp.MustCompile(`(?m)^export const ([A-Za-z_$][\w$]*)`)

	exportedStringConst = regexp.MustCompile(`(?m)^export const ([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*"([^"]*)";`)

	// exportedFuncName — `export function NAME(` любого модуля консоли.
	// Импортировать можно ровно экспортированное; внутренние помощники модуля
	// видны только внутри него (см. consoleExportedFieldHelpers).
	exportedFuncName = regexp.MustCompile(`(?m)^export function ([A-Za-z_$][\w$]*)`)

	// sanitizeCarrier — переменная, в которую `sanitize` копирует тело
	// (`const out: … = { ...obj };`). Дальнейшие правки идут по ней.
	sanitizeCarrier = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)[^=;]*=\s*\{\s*\.\.\.\s*obj\s*\}`)
	// sanitizeDelete / sanitizeAssign — снятие и добавление ключа телу.
	sanitizeDelete = regexp.MustCompile(`delete\s+([A-Za-z_$][\w$]*)(?:\.([A-Za-z_$][\w$]*)|\[\s*"([^"]+)"\s*\])`)
	sanitizeAssign = regexp.MustCompile(`([A-Za-z_$][\w$]*)(?:\.([A-Za-z_$][\w$]*)|\[\s*"([^"]+)"\s*\])\s*=[^=]`)
)

// sanitizeEffects — что `sanitize` делает с телом до отправки.
//
// Без этого набор ключей ФОРМЫ выдавался бы за набор ключей ПРОВОДА, а это
// разные вещи: `sanitize` — последнее, что видит тело. Два ресурса собирают
// значение под именем, которого в сообщении нет (`size_gib` → `size_bytes`,
// `vip_source` → ветка oneof), и снимают его сами; объявить это дефектом значило
// бы держать гейт вечно красным на том, чего не происходит.
type sanitizeEffects struct {
	// Removed — ключи, снимаемые БЕЗУСЛОВНО. Условное снятие (`if (…) delete …`)
	// сюда не входит: «иногда снимается» — это «иногда уходит».
	Removed map[string]bool
	// Added — ключи, которые `sanitize` кладёт в тело сам. Их в `fields` нет, и
	// без этого разбора они не проверялись бы вовсе — а именно туда уезжает
	// выбранная ветка oneof.
	Added map[string]bool
}

// analyzeSanitize читает `sanitize` ресурса.
//
// Разбор нарочно узкий: снятие и присваивание ПО ПЕРЕМЕННОЙ, в которую скопировано
// тело. Присваивание локальной вложенной структуре к телу отношения не имеет,
// и приписывать его телу значило бы выдумывать ключи.
func analyzeSanitize(src string) sanitizeEffects {
	eff := sanitizeEffects{Removed: map[string]bool{}, Added: map[string]bool{}}
	carrier := sanitizeCarrier.FindStringSubmatch(src)
	if carrier == nil {
		return eff
	}
	name := carrier[1]

	for _, m := range sanitizeDelete.FindAllStringSubmatchIndex(src, -1) {
		g := sanitizeGroups(src, m)
		if g.varName != name || g.key == "" {
			continue
		}
		if statementIsGuarded(src, m[0]) {
			continue
		}
		eff.Removed[g.key] = true
	}
	for _, m := range sanitizeAssign.FindAllStringSubmatchIndex(src, -1) {
		g := sanitizeGroups(src, m)
		if g.varName != name || g.key == "" {
			continue
		}
		eff.Added[g.key] = true
	}

	// Снятие ключа, за которым идёт присваивание ТОГО ЖЕ ключа, — не удаление,
	// а ПЕРЕСБОРКА: значение уходит на провод, просто в другой форме.
	//
	// Считать такое удалением — тихая потеря охвата, и потеря самая дорогая:
	// вместе с ключом из проверки выпадает всё его поддерево. Ровно так вышло с
	// источником VIP — форма объявляет `v4_source.subnet_id`/`.address_id`,
	// sanitize снимает `v4_source` и тут же кладёт его обратно, и все шесть
	// полей семейств уходили из сверки, пока гейт рапортовал, что прочитал их.
	// Значение, которое там окажется, статически не вычислить; форма, объявленная
	// реестром, — лучшее, что можно сверить, и содержание в ней есть.
	for key := range eff.Added {
		delete(eff.Removed, key)
	}
	return eff
}

type sanitizeTarget struct {
	varName string
	key     string
}

func sanitizeGroups(src string, m []int) sanitizeTarget {
	group := func(i int) string {
		if m[2*i] < 0 {
			return ""
		}
		return src[m[2*i]:m[2*i+1]]
	}
	t := sanitizeTarget{varName: group(1), key: group(2)}
	if t.key == "" {
		t.key = group(3)
	}
	return t
}

// guardedStatementHead — условие непосредственно перед оператором.
var guardedStatementHead = regexp.MustCompile(`\b(?:if|else)\b|\?`)

// statementIsGuarded — стоит ли перед оператором условие. Условно снятый ключ
// в какой-то ветке всё же уходит, поэтому снятием он не считается.
func statementIsGuarded(src string, at int) bool {
	start := strings.LastIndexAny(src[:at], ";{}\n")
	return guardedStatementHead.MatchString(src[start+1 : at])
}

func TestConsoleFormsSendNoUnknownRequestFields(t *testing.T) {
	root := repoRoot(t)
	consoleRoot := filepath.Join(root, "ui-future")
	files, err := consoleRegistryFiles(consoleRoot)
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	if len(files) == 0 {
		// Пустой набор входов — это НЕ «зелено».
		t.Fatal("no console resource registry found under ui-future: the gate has nothing to check, which is a failure, not a pass")
	}
	ext, err := consoleTreeExterns(consoleRoot)
	if err != nil {
		t.Fatal(err)
	}

	var findings []consoleFinding
	var forms consoleRegistryForms
	specs, mutable, createBodies, updateBodies := 0, 0, 0, 0
	sanitized, synthesized := 0, 0

	for _, file := range files {
		blob, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		text := string(blob)
		rel := mustRel(root, file)

		parsed, err := parseConsoleRegistry(rel, text, ext)
		if err != nil {
			// Непонятая конструкция — отказ, а не пропуск: разбор, который
			// «не смог» и промолчал, и есть тот самый ноль без содержания.
			t.Fatalf("%s: the registry could not be read, so nothing in it was checked: %v", rel, err)
		}

		forms.add(parsed)

		// Сверка с независимым счётом — до любых выводов о находках.
		// Из счёта вычитается пришедшее ИЗВНЕ: ресурс, ссылающийся на общее
		// объявление, и набор полей, импортированный из общего модуля, в сыром
		// тексте своего файла не видны by construction. Без вычитания сверка
		// объявила бы находкой единый источник — то есть ровно то, ради чего он
		// и заводится.
		if want := len(apiPathLiteral.FindAllString(text, -1)); want != len(parsed.Specs)-parsed.ExternSpecs {
			t.Errorf("%s: scanner sees %d own resources (%d in all, %d by reference), the raw text declares %d — the scanner is losing resources, and every body in them would go unchecked",
				rel, len(parsed.Specs)-parsed.ExternSpecs, len(parsed.Specs), parsed.ExternSpecs, want)
		}
		// `FieldDecls` уже считается ТОЛЬКО по своим константам файла, поэтому
		// вычитать импортированное второй раз не нужно: это дало бы недосчёт
		// ровно на его размер.
		if want := len(fieldNameLiteral.FindAllString(text, -1)); want != parsed.FieldDecls {
			t.Errorf("%s: scanner sees %d own field declarations (%d imported from a shared module and counted where they are declared), the raw text declares %d — the scanner is losing fields, and every one of them would go unchecked",
				rel, parsed.FieldDecls, parsed.ExternFieldDecls, want)
		}

		for _, spec := range parsed.Specs {
			specs++
			if !spec.CanCreate && !spec.CanUpdate {
				continue
			}
			mutable++
			eff := analyzeSanitize(spec.SanitizeSource)
			sanitized += len(eff.Removed)
			synthesized += len(eff.Added)
			if spec.CanCreate {
				createBodies++
			}
			if spec.CanUpdate {
				updateBodies++
			}
			findings = append(findings, consoleSpecFindings(spec)...)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].SpecID != findings[j].SpecID {
			return findings[i].SpecID < findings[j].SpecID
		}
		return findings[i].Key < findings[j].Key
	})

	t.Logf("перепись: %s; %d resources (%d offer a mutation): %d create bodies, %d update bodies; sanitize removes %d key(s) and synthesises %d; %d finding(s)",
		forms, specs, mutable, createBodies, updateBodies, sanitized, synthesized, len(findings))

	// Количество осмотренного утверждается, а не только печатается: гейт,
	// прочитавший ноль ресурсов, обязан быть отличим от гейта, не нашедшего
	// нарушений.
	if specs == 0 {
		t.Fatal("console registries parsed, but not one resource came out of them: the scanner read nothing")
	}
	if createBodies == 0 || updateBodies == 0 {
		t.Fatalf("no %s body was assembled at all: the console offers mutations, so a zero here means the scanner lost them",
			map[bool]string{true: "create", false: "update"}[createBodies == 0])
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d console form field(s) reach the edge as a key the request message does not carry — the operator sets it, the call returns 200, the setting is never applied:\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	t.Fatal(b.String())
}

// ── Куда ведёт проекция ─────────────────────────────────────────────────────
//
// Реестр, сведённый к ре-экспорту, из обхода НЕ исключается. Исключить его
// значило бы объявить непроверенным целый домен и получить за это зелёное:
// спеки, которые он объявлял, никуда не делись — они переехали, и сканер обязан
// видеть их там. Поэтому проекция обязана разрешаться в файл, который сканер
// осматривает КАК РЕЕСТР; неразрешимая проекция — находка, а не пропуск.

// consoleAppConfigFileName — tsconfig приложения консоли: там, и только там,
// объявлены алиасы модулей.
const consoleAppConfigFileName = "tsconfig.app.json"

// consoleAppDir — ближайший предок файла, несущий tsconfig приложения.
//
// Глубина раскладки не выписывается: приложение — это то, у чего есть свой
// tsconfig, и выписанное «два уровня вверх» разошлось бы с деревом молча.
func consoleAppDir(consoleRoot, file string) (string, error) {
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, consoleAppConfigFileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || len(dir) <= len(consoleRoot) {
			return "", fmt.Errorf("no %s above %s: the module aliases of this app are declared nowhere the scanner can read", consoleAppConfigFileName, file)
		}
		dir = parent
	}
}

// consoleModuleAliases читает объявленные приложением алиасы модулей.
//
// Читаются из ДЕРЕВА, а не выписываются здесь: алиас — факт настройки сборки,
// и вторая его копия в гейте разошлась бы с первой ровно тогда, когда её
// поменяют. Нечитаемый tsconfig — отказ: алиас, которого гейт не разобрал, он
// не вправе подставить наугад.
func consoleModuleAliases(appDir string) (map[string]string, error) {
	blob, err := os.ReadFile(filepath.Join(appDir, consoleAppConfigFileName))
	if err != nil {
		return nil, err
	}
	var cfg struct {
		CompilerOptions struct {
			Paths map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(appDir, consoleAppConfigFileName), err)
	}
	out := make(map[string]string, len(cfg.CompilerOptions.Paths))
	for pattern, targets := range cfg.CompilerOptions.Paths {
		// Моделируется одна форма — `<prefix>/*` в один каталог. Всё прочее
		// остаётся неизвестным алиасом, то есть громким отказом при подстановке.
		if !strings.HasSuffix(pattern, "/*") || len(targets) != 1 || !strings.HasSuffix(targets[0], "/*") {
			continue
		}
		prefix := strings.TrimSuffix(pattern, "*")
		out[prefix] = filepath.Join(appDir, strings.TrimSuffix(targets[0], "*"))
	}
	return out, nil
}

// consoleResolveModule разрешает спецификатор ре-экспорта в путь файла.
//
// Расширение приписывается одно — то же, что у самих реестров: цель проекции
// обязана быть файлом, который обход И НАХОДИТ, а он ищет строго по имени.
// Подбор расширений дал бы путь, которого в наборе осмотренного нет, и находка
// назвала бы координату, никем не читаемую.
func consoleResolveModule(fileDir string, aliases map[string]string, spec string) (string, error) {
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
		return filepath.Join(fileDir, spec) + filepath.Ext(consoleRegistryFileName), nil
	}
	for prefix, dir := range aliases {
		if !strings.HasPrefix(spec, prefix) {
			continue
		}
		return filepath.Join(dir, strings.TrimPrefix(spec, prefix)) + filepath.Ext(consoleRegistryFileName), nil
	}
	return "", fmt.Errorf("%q resolves through no alias this app declares and is not a relative path: a projection that names a module the scanner cannot locate points nowhere", spec)
}

// consoleProjectionTarget — файл, чей реестр проецирует этот файл.
//
// Три шага собраны здесь, а не в цикле пробы, потому что отказать вправе каждый
// из них, и вызывающему нужен один ответ: куда ведёт проекция либо почему это
// установить нельзя. Молчаливого «никуда» среди исходов нет.
func consoleProjectionTarget(consoleRoot, file, spec string) (string, error) {
	appDir, err := consoleAppDir(consoleRoot, file)
	if err != nil {
		return "", err
	}
	aliases, err := consoleModuleAliases(appDir)
	if err != nil {
		return "", err
	}
	return consoleResolveModule(filepath.Dir(file), aliases, spec)
}

// consoleProjectionsWithoutARegistry — проекции, чей реестр никем не осматривается.
//
// Отдельной функцией, а не тремя строками в цикле пробы: свойство «проекция
// ведёт К ОСМАТРИВАЕМОМУ реестру» и есть то, чем эта форма отличается от маски,
// и его обязано быть можно уронить инъекцией без подъёма дерева.
func consoleProjectionsWithoutARegistry(targets map[string]string, declaring map[string]bool) []string {
	var out []string
	for file, target := range targets {
		if declaring[target] {
			continue
		}
		out = append(out, fmt.Sprintf("%s: проекция ведёт в %s, который сканер реестром не читает — спеки домена не проверялись бы ни здесь, ни там", file, target))
	}
	sort.Strings(out)
	return out
}

// consoleRegistryFiles ищет реестры ОБХОДОМ дерева, а не списком путей: новый
// remote попадает под проверку сам, без правки гейта. Список путей означал бы,
// что забытая правка списка выглядит как «нарушений нет».
// Состав — из ИНДЕКСА git, а не с диска. Под `ui-future/` на всякой машине, где
// собирали фронтенд, лежит игнорируемое, и обход диска считал бы его частью
// репозитория: вердикт стал бы свойством рабочего каталога, а не коммита.
//
// Отдельные пропуски `node_modules` и `dist` отсюда ушли не по недосмотру — они
// стали не нужны: индекс их не содержит by construction, а рукописный перечень
// исключений отстаёт от дерева молча (следующий сборочный каталог с другим
// именем в него бы не попал).
func consoleRegistryFiles(root string) ([]string, error) {
	files, err := treecorpus.Under(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, path := range files {
		if filepath.Base(path) == consoleRegistryFileName {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

// consoleExportedStringConsts собирает `export const NAME = "…"` со всего дерева
// консоли.
//
// Разрешение импортов «по-настоящему» (алиасы `@shared/…`, реэкспорты) стоило бы
// собственной модульной системы; вместо неё — одно плоское пространство имён с
// жёстким условием: одно имя, объявленное с РАЗНЫМИ значениями, — ошибка. Тогда
// подстановка не может оказаться тихо неправильной: она либо однозначна, либо
// гейт падает.
// consoleExportedValueConsts — экспортированные ЗНАЧЕНИЯ модулей консоли
// (массивы полей, объекты пустого состояния), а не только строки.
//
// ЗАЧЕМ. Ресурс, который монтируют два приложения, обязан объявлять свои поля
// ОДИН раз: выписанные дважды, они расходятся молча — так уже разошлись копии
// формы ресурса. Реестр тогда ссылается на импортированное имя, и разбор одного
// файла увидел бы идентификатор без значения. Прежде это было отказом читать
// ВЕСЬ файл, то есть гейт переставал проверять реестр целиком, а «не прочитал»
// выглядело как «нет находок».
//
// ГРАНИЦА НАЗВАНА ЧЕСТНО: собираются только те константы, чьё значение сканер
// понимает; непонятое не подставляется, и ссылка на него остаётся громким
// отказом — гейт, который начал бы принимать что угодно, ничего бы не находил.
//
// Расхождение одного имени между модулями — ошибка по той же причине, что и у
// строк: сканер не может знать, какой из двух реестр импортирует, а догадка
// сделала бы его тихо неверным.
func consoleExportedValueConsts(root string) (map[string]jsValue, error) {
	out := make(map[string]jsValue)
	origin := make(map[string]string)
	clashing := make(map[string]bool)
	// ОБЛАСТЬ СБОРА — только `shared`, и это не сужение ради зелёного.
	//
	// Единый источник живёт там by construction: значение, на которое ссылаются
	// два реестра, обязано быть объявлено один раз и в общем месте. Сбор по
	// всему дереву ловил бы одноимённые константы РАЗНЫХ приложений (их там
	// десятки — каждое приложение объявляет свою навигацию, свои умолчания) и
	// объявлял бы расхождением то, что расхождением не является: они и не
	// должны совпадать, потому что никто их друг у друга не импортирует.
	//
	// Следствие названо честно: ссылка реестра на значение из НЕ-`shared`
	// модуля останется громким отказом. Это верно — такая ссылка и есть форк.
	shared := filepath.Join(root, "shared", "src")
	// Состав берётся у ИНДЕКСА дерева, а не обходом диска: обход видел бы
	// невыгруженные артефакты сборки и чужие рабочие копии, а гейт обходчиков
	// (`internal/repohygiene`) справедливо требует единого источника состава.
	files, err := treecorpus.UnderWithSuffix(shared, ".ts", ".tsx")
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		blob, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		// Сами реестры пропускаем: их `REGISTRY` разбирается своим путём, и
		// подмешивать его в область видимости соседа значило бы разрешать
		// ссылку туда, куда импорта нет.
		if filepath.Base(path) == "resource-registry.tsx" {
			continue
		}
		text := string(blob)
		consts, perr := jsTopLevelConsts(text)
		if perr != nil {
			// Файл вне понимаемого подмножества — не находка: большинство
			// модулей консоли им и не являются. Значения из него просто не
			// собираются, и ссылка на них останется отказом.
			continue
		}
		// Только ЭКСПОРТИРОВАННЫЕ имена: импортировать можно ровно их, а
		// внутренние константы компонентов совпадают между файлами сплошь и
		// рядом (`rows`, `ROW_H`) — считать это расхождением значило бы объявить
		// находкой то, что никого не касается.
		exported := make(map[string]bool)
		for _, m := range exportedConstName.FindAllStringSubmatch(text, -1) {
			exported[m[1]] = true
		}
		for name, v := range consts {
			if !exported[name] {
				continue
			}
			// Непонятое значение собирается ТОЖЕ, и это не послабление: стрелка,
			// возвращающая литерал (`template`), для разбора значений именно
			// непонятое выражение — её читает отдельный разбор по сырому тексту.
			// Ссылка на действительно непонятное всё равно останется отказом:
			// его выдаст тот, кто эту ссылку разворачивает.
			if prev, ok := origin[name]; ok && prev != path {
				clashing[name] = true
			}
			out[name], origin[name] = v, path
		}
	}
	// Имя, объявленное в `shared` дважды, из таблицы ВЫВОДИТСЯ, а не роняет сбор.
	//
	// Причина не в снисходительности: сканер действительно не может знать, какое
	// из двух объявлений импортирует реестр, — но большинство таких имён ни один
	// реестр и не спрашивает. Уронив сбор целиком, гейт перестал бы проверять
	// ВСЕ реестры из-за константы, к делу не относящейся. Выведенное имя
	// остаётся неразрешимым: ссылка на него даёт громкий отказ с координатой,
	// то есть ровно тогда, когда двусмысленность действительно мешает.
	for n := range clashing {
		delete(out, n)
	}
	return out, nil
}

// consoleExterns — всё, что реестр берёт из ДРУГИХ файлов дерева.
type consoleExterns struct {
	// strings — `export const NAME = "…"` (пути ресурсов geo и родня).
	strings map[string]string
	// values — экспортированные ЗНАЧЕНИЯ (массив полей, объект пустого состояния).
	values map[string]jsValue
	// helpers — экспортированные ПОМОЩНИКИ-наборы полей вместе с областью
	// видимости своего модуля.
	helpers map[string]jsFuncRef
}

// consoleTreeExterns собирает всё импортируемое ОДНИМ вызовом.
//
// Собирается по дереву, а не по перечню модулей: перечень разошёлся бы с
// деревом молча, и следующий общий модуль оказался бы невидим — ровно так
// сканер и перестал читать реестр nlb целиком (#554).
func consoleTreeExterns(consoleRoot string) (consoleExterns, error) {
	strs, err := consoleExportedStringConsts(consoleRoot)
	if err != nil {
		return consoleExterns{}, fmt.Errorf("collect exported string consts: %w", err)
	}
	vals, err := consoleExportedValueConsts(consoleRoot)
	if err != nil {
		return consoleExterns{}, fmt.Errorf("collect exported value consts: %w", err)
	}
	helpers, err := consoleExportedFieldHelpers(consoleRoot)
	if err != nil {
		return consoleExterns{}, fmt.Errorf("collect exported field helpers: %w", err)
	}
	return consoleExterns{strings: strs, values: vals, helpers: helpers}, nil
}

// consoleExportedFieldHelpers — ПОМОЩНИКИ-наборы полей, экспортированные общими
// модулями консоли.
//
// ЗАЧЕМ. Набор полей, который рисуют ДВА реестра, обязан быть объявлен один раз
// и в общем месте — выписанный дважды, он расходится молча (так уже разошлись
// ветви проверки живости: они были заведены в одном реестре и остались
// невыразимыми в том, который эту форму пользователю и показывает, #375).
// Единый источник вынесли в общий модуль — и сканер перестал читать реестр
// ЦЕЛИКОМ: имя помощника ему было неизвестно, а неизвестное имя он честно
// считает отказом. Четыре пробы края покраснели разом, и три из них — те, чья
// работа как раз в том, чтобы «ноль находок» было отличимо от «ноль
// прочитанного» (#554). То есть про поля всего реестра не утверждалось ничего.
//
// ГРАНИЦЫ, БЕЗ КОТОРЫХ ЭТО СТАЛО БЫ ПОСЛАБЛЕНИЕМ:
//
//   - состав модулей ВЫВОДИТСЯ из дерева (индекс git под `shared/src`), а не
//     выписывается: выписанный перечень разойдётся с деревом молча, и следующий
//     общий модуль окажется невидим ровно так же, как этот;
//   - форма помощника прежняя и узкая — `const…; return [ … ]` либо
//     `const…; return { … }`, литеральные аргументы; всё за её пределами
//     по-прежнему ЛОМАЕТ разбор с координатой;
//   - наружу отдаются только ЭКСПОРТИРОВАННЫЕ имена (импортировать можно ровно
//     их), а тело помощника читается в области видимости СВОЕГО модуля: его
//     непубличные соседи видны там и нигде больше;
//   - имя, объявленное в двух общих модулях, из таблицы выводится — сканер не
//     может знать, какое из двух импортирует реестр, и догадка сделала бы его
//     тихо неверным. Выведенное имя остаётся неразрешимым, то есть даёт громкий
//     отказ ровно тогда, когда двусмысленность действительно мешает.
func consoleExportedFieldHelpers(root string) (map[string]jsFuncRef, error) {
	// Область та же, что у значений, и по той же причине: единый источник живёт
	// в `shared` by construction, а сбор по всему дереву объявлял бы расхождением
	// одноимённых помощников разных приложений, которые никто друг у друга не
	// импортирует.
	shared := filepath.Join(root, "shared", "src")
	files, err := treecorpus.UnderWithSuffix(shared, ".ts", ".tsx")
	if err != nil {
		return nil, err
	}
	sources := make(map[string]string, len(files))
	for _, path := range files {
		// Сами реестры пропускаем: их содержимое разбирается своим путём.
		if filepath.Base(path) == consoleRegistryFileName {
			continue
		}
		blob, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		rel, rerr := filepath.Rel(filepath.Dir(root), path)
		if rerr != nil {
			rel = path
		}
		sources[filepath.ToSlash(rel)] = string(blob)
	}
	return consoleFieldHelpersFromModules(sources), nil
}

// consoleFieldHelpersFromModules — та же сборка, но по исходникам в памяти.
//
// Вынесено затем, чтобы пробы кормили сборщик СВОИМИ модулями, проходя через
// ТОТ ЖЕ код, что исполняется на дереве. Дублёр, собранный отдельно, принимал бы
// не то же самое, что настоящий, и прятал бы ровно тот дефект, ради которого его
// подставляют.
func consoleFieldHelpersFromModules(sources map[string]string) map[string]jsFuncRef {
	out := make(map[string]jsFuncRef)
	origin := make(map[string]string)
	clashing := make(map[string]bool)

	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	// Детерминизм входа — часть контракта проверки: порядок обхода не должен
	// решать, чьё объявление окажется в таблице последним.
	sort.Strings(paths)

	for _, path := range paths {
		text := sources[path]
		module := jsTopLevelFieldSetFuncs(path, text)
		if len(module) == 0 {
			continue
		}
		// Область модуля: его константы и ВСЕ его помощники, включая
		// непубличные. Именно в ней читается тело — сосед, на которого оно
		// ссылается, живёт здесь и в файле-потребителе не виден вовсе.
		consts, cerr := jsTopLevelConsts(text)
		if cerr != nil {
			// Файл вне понимаемого подмножества: помощники из него не
			// собираются, и ссылка на них останется громким отказом.
			continue
		}
		base := &consoleScope{consts: consts, funcs: make(map[string]jsFuncRef, len(module))}
		for name, fn := range module {
			base.funcs[name] = jsFuncRef{fn: fn, base: base}
		}

		exported := make(map[string]bool)
		for _, m := range exportedFuncName.FindAllStringSubmatch(text, -1) {
			exported[m[1]] = true
		}
		for name := range module {
			if !exported[name] {
				continue
			}
			if prev, ok := origin[name]; ok && prev != path {
				clashing[name] = true
			}
			out[name], origin[name] = base.funcs[name], path
		}
	}
	for n := range clashing {
		delete(out, n)
	}
	return out
}

func consoleExportedStringConsts(root string) (map[string]string, error) {
	out := make(map[string]string)
	clashing := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range exportedStringConst.FindAllStringSubmatch(string(blob), -1) {
			name, value := m[1], m[2]
			if prev, ok := out[name]; ok && prev != value {
				clashing[name] = true
			}
			out[name] = value
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(clashing) > 0 {
		var names []string
		for n := range clashing {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("exported string const(s) %v are declared with different values in different modules: the scanner cannot tell which one a registry imports, and guessing would make it quietly wrong", names)
	}
	return out, nil
}

// consoleSpecFindings — все расхождения одного ресурса: тело создания и тело
// правки против контрактов обслуживающих их RPC.
func consoleSpecFindings(spec consoleSpec) []consoleFinding {
	if !spec.CanCreate && !spec.CanUpdate {
		return nil
	}
	eff := analyzeSanitize(spec.SanitizeSource)
	var out []consoleFinding
	if spec.CanCreate {
		out = append(out, checkConsoleBody(spec, "create", "POST",
			concretePath(spec.MutationBasePath), consoleCreateBody(spec, eff))...)
	}
	if spec.CanUpdate {
		out = append(out, checkConsoleBody(spec, "update", "PATCH",
			concretePath(spec.MutationBasePath)+"/Xid", consoleUpdateBody(spec))...)
	}
	return out
}

// checkConsoleBody сверяет собранное тело с контрактом RPC ТЕМ ЖЕ разбором, что
// и тела регрессионных suite'ов: одна реализация на оба гейта — разойтись они
// не могут.
func checkConsoleBody(spec consoleSpec, op, method, path string, body map[string]any) []consoleFinding {
	var out []consoleFinding
	for _, f := range analyzeRequestBody(method, path, body) {
		file, line := consoleFieldSite(spec, f.Key)
		out = append(out, consoleFinding{
			bodyFinding: f,
			File:        file,
			Line:        line,
			SpecID:      spec.ID,
			Op:          op,
		})
	}
	return out
}

// consoleFieldSite адресует находку МЕСТОМ объявления поля; для ключа, пришедшего
// из `template`, адресом остаётся сам ресурс.
//
// Возвращается пара «файл + строка», а не одна строка: поле, объявленное в общем
// модуле, живёт в другом файле, и адрес «файл реестра + строка помощника» —
// координата, которой не существует. Читателя она отправляет искать не туда, а
// выглядит при этом точной.
func consoleFieldSite(spec consoleSpec, key string) (string, int) {
	head := key
	if i := strings.IndexAny(head, ".["); i >= 0 {
		head = head[:i]
	}
	site := func(f consoleField) (string, int) {
		if f.File != "" {
			return f.File, f.Line
		}
		return spec.File, f.Line
	}
	for _, f := range spec.Fields {
		if f.Name == key || f.Name == head || strings.HasPrefix(f.Name, head+".") {
			return site(f)
		}
		for _, it := range f.ItemFields {
			if it.Name == key {
				return site(it)
			}
		}
	}
	return spec.File, spec.Line
}

// consoleCreateBody воспроизводит тело создания:
// `buildCreateBody(applyFieldDefaults(spec.fields, spec.template(ctx)))` — ключи
// шаблона плюс поля формы, кроме тех, что живут только в сообщении правки.
// Значения не важны: сверяются имена.
func consoleCreateBody(spec consoleSpec, eff sanitizeEffects) map[string]any {
	body := map[string]any{}
	for _, path := range spec.TemplateKeys {
		setJSONPath(body, path, "x")
	}
	for _, f := range spec.Fields {
		if f.UpdateOnly || isFormOnlyPath(f.Name) {
			continue
		}
		setConsoleField(body, f)
	}
	return applySanitize(body, eff)
}

// applySanitize доводит собранное тело до того вида, в котором оно уходит:
// снимает то, что `sanitize` снимает безусловно, и добавляет то, что она
// синтезирует. Второе — не послабление, а РАСШИРЕНИЕ охвата: ветка oneof,
// собранная в `sanitize`, в `fields` не объявлена и иначе не проверялась бы.
func applySanitize(body map[string]any, eff sanitizeEffects) map[string]any {
	for key := range eff.Removed {
		delete(body, key)
	}
	for key := range eff.Added {
		if _, ok := body[key]; !ok {
			body[key] = "x"
		}
	}
	return body
}

// consoleUpdateBody воспроизводит тело правки: `buildUpdateBody(current, mask)` —
// ровно поля, названные маской, плюс сама маска. Отбор зеркалит
// `computeUpdateMask`: скрытое, неизменяемое, невидимое в правке и создающееся
// один раз в маску не попадает.
//
// `sanitize` на этом пути тоже вызывается, но её правки в тело не проходят:
// маска строится по ИМЕНАМ ПОЛЕЙ, а `buildUpdateBody` кладёт только названное
// маской. Ключ, синтезированный `sanitize`, именем поля не является и в маску не
// попадает — поэтому применять её эффекты здесь значило бы придумать ключи,
// которых форма правки не отправляет.
func consoleUpdateBody(spec consoleSpec) map[string]any {
	body := map[string]any{"update_mask": "x"}
	for _, f := range spec.Fields {
		if f.Hidden || f.Immutable || f.EditHidden || f.CreateOnly || isFormOnlyPath(f.Name) {
			continue
		}
		setConsoleField(body, f)
	}
	return body
}

// isFormOnlyPath — путь, живущий только ради виджета. `stripFormOnlyKeys`
// снимает такие ключи на любой глубине, поэтому до провода они не доходят.
func isFormOnlyPath(name string) bool {
	for _, seg := range strings.Split(name, ".") {
		if strings.HasPrefix(seg, "_") {
			return true
		}
	}
	return false
}

func setConsoleField(body map[string]any, f consoleField) {
	if len(f.ItemFields) == 0 {
		setJSONPath(body, f.Name, "x")
		return
	}
	item := map[string]any{}
	for _, it := range f.ItemFields {
		if isFormOnlyPath(it.Name) {
			continue
		}
		setJSONPath(item, it.Name, "x")
	}
	setJSONPath(body, f.Name, []any{item})
}

// setJSONPath кладёт значение по точечному пути, создавая промежуточные объекты
// — как `setByPath` консоли.
func setJSONPath(obj map[string]any, path string, value any) {
	segs := strings.Split(path, ".")
	cur := obj
	for i, seg := range segs {
		if i == len(segs)-1 {
			cur[seg] = value
			return
		}
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
}

// consoleMutationCall — вызов, которым консоль отправляет краю тело.
// consoleMutationCall — «здесь консоль отправляет серверу тело».
//
// Типизированная обёртка — не единственная форма: консоль уже носит сырые
// `fetch` с мутирующим методом (страницы выдач ходили именно так, минуя
// преобразование регистра и не проверяя, отказал ли сервер). Детектор, знающий
// только обёртку, их не видел — и тогда учёт поверхности ниже сообщал бы о
// полноте, которой нет. Это слепое пятно САМОГО детектора, на уровень глубже
// того, которое он существует, чтобы называть.
//
// Сырой `fetch` считается мутирующим по наличию тела: `body:` в инициализаторе.
// Чтение (`fetch(url)`) телом не сопровождается и сюда не попадает.
//
// Хвост инициализатора ограничен КОНЦОМ ОПЕРАТОРА (`[^;]`), а не «чем угодно».
// Прежде здесь стояло `(?s:.*?)`, и оно уходило за скобку вызова до первого
// `body:` ГДЕ УГОДНО ниже по файлу. Тогда обычное чтение потока событий
// (`fetch(url, { headers: … })`, а тремя строками ниже `body: body.slice(…)` —
// тело ОТВЕТА, упакованное в возврат) объявлялось местом, которое шлёт краю
// тело. То есть детектор нарушал ровно то, что обещают две строки над ним, и
// требовал внести в ведомость файл, ничего не отправляющий: слепое пятно
// выдавалось бы за осознанное решение «эта поверхность вне охвата».
var consoleMutationCall = regexp.MustCompile(
	`\bapi\.(?:create|update|post|patch|put)\(` +
		`|\bfetch\([^)]*\{(?s:[^;]*?)\bbody\s*:` +
		`|\bfetch\((?s:[^;]*?)\bbody\s*:`)

// specDrivenFormComponent — общие формы, чьё тело собирает РЕЕСТР
// (`spec.fields` / `spec.template` / `spec.sanitize`). Ровно их и проверяет гейт
// выше, поэтому вызов отсюда — покрытый вызов.
var specDrivenFormComponent = map[string]bool{
	"ResourceCreatePage.tsx":       true,
	"ResourceEditPage.tsx":         true,
	"InlineResourceCreateForm.tsx": true,
	"InlineResourceEditForm.tsx":   true,
	"ResourceFormDialog.tsx":       true,
}

// bespokeConsoleMutationSites — поимённый список мест, которые собирают тело
// САМИ, минуя реестр.
//
// Зачем список, а не молчание. Охват выше покрывает ровно тот путь, где состав
// тела ОБЪЯВЛЕН (реестр ресурсов) и потому вычислим статически. Форма, которая
// держит собственное состояние и собирает тело из локальных переменных, к набору
// ключей статически не сводится — и честнее сказать это, чем построить хрупкий
// разбор, который однажды перестанет что-либо находить и не скажет об этом.
//
// Но «не покрыто» обязано быть ВИДНО. Список печатается каждый прогон; новое
// место, собирающее тело в обход реестра, роняет гейт — не потому что оно
// ошибочно, а потому что решение «эта поверхность остаётся вне охвата» должно
// приниматься явно, а не появляться само. Разрешение, которому нечего разрешать,
// тоже роняет гейт: неиспользуемое право наследует следующая ошибка.
var bespokeConsoleMutationSites = map[string]string{
	"iam/src/components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx":                  "выдача доступа: тело собирается из состояния формы, состав объявлен не реестром",
	"iam/src/pages/iam/AccessPage/AccessPage.tsx":                                                           "выдача и отзыв доступа со страницы, тело из локального состояния",
	"shared/src/components/organisms/iam/IamCommon/IamCommon.tsx":                                           "общие действия IAM, тело из локального состояния",
	"shared/src/components/organisms/InlineAddressPoolCreateForm/InlineAddressPoolCreateForm.tsx":           "адресный пул: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/InlineAddressPoolEditForm/InlineAddressPoolEditForm.tsx":               "адресный пул: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/InlineNetworkInterfaceCreateForm/InlineNetworkInterfaceCreateForm.tsx": "сетевой интерфейс: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/InlineNetworkInterfaceEditForm/InlineNetworkInterfaceEditForm.tsx":     "сетевой интерфейс: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/InlineSecurityGroupEditForm/InlineSecurityGroupEditForm.tsx":           "группа безопасности: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/InlineSubnetCreateForm/InlineSubnetCreateForm.tsx":                     "подсеть: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/InlineSubnetEditForm/InlineSubnetEditForm.tsx":                         "подсеть: собственная форма, тело из локального состояния",
	"shared/src/components/organisms/RoutesPanel/RoutesPanel.tsx":                                           "маршруты таблицы маршрутизации: отдельный RPC, тело из локального состояния",
	"shared/src/components/organisms/SgRulesPanel/SgRulesPanel.tsx":                                         "правила группы безопасности: отдельный RPC, тело из локального состояния",
	"shared/src/api/cluster.ts":              "тонкая обёртка над кластерным RPC, тело — аргумент вызывающего",
	"shared/src/pages/system/LimitsPage.tsx": "правка предела: административный RPC с маской, тело из локального состояния",

	// Провайдер личности. Эти тела уезжают НЕ на наш REST-край, а в Kratos, и
	// нашим контрактом не описаны — сверять их с нашими message нечем.
	//
	// Копий в модулях (compute/nlb/registry/storage) здесь больше нет: #405 свёл
	// их к ре-экспорту из `shared/`, тела запроса они не собирают, и освобождать
	// у них стало нечего. Реализация — одна, и она названа строкой ниже.
	"shared/src/lib/kratos.ts": "клиент провайдера личности, не наш край",

	// Обёртки над RPC: глагол принимает тело АРГУМЕНТОМ и своего состава не
	// имеет — состав объявляет вызывающий, а вызывающие форм ресурса как раз и
	// проверены охватом выше.
	//
	// Здесь стояли ЧЕТЫРЕ копии обёртки ресурса (по одной на домен) и четыре
	// копии транспорта. Их больше нет: копии обёртки сведены к общей фабрике
	// `resourceApi` (#1466, #409, #1471, #406), а транспорт модулей был мёртвой
	// копией и снят вместе со своей пробой. Одна строка вместо восьми — не
	// сокращение ведомости, а следствие того, что предмет у неё теперь один.
	"shared/src/api/resources.ts": "фабрика глаголов ресурса, тело — аргумент вызывающего",
	"shared/src/api/iam.ts":       "тонкая обёртка над RPC IAM, тело — аргумент вызывающего",
	"shared/src/api/tokens.ts":    "тонкая обёртка над RPC токенов, тело — аргумент вызывающего",
}

// TestConsoleMutationSurfaceIsAccountedFor — каждая точка, из которой консоль
// отправляет краю тело, либо покрыта охватом выше, либо названа как непокрытая.
//
// Без этого граница охвата была бы невидима: гейт зеленел бы, а половина
// поверхности молча оставалась бы за его пределами — тот самый класс, ради
// которого он и написан, только этажом выше.
func TestConsoleMutationSurfaceIsAccountedFor(t *testing.T) {
	root := repoRoot(t)
	consoleRoot := filepath.Join(root, "ui-future")

	var covered, bespoke, unaccounted []string
	err := filepath.WalkDir(consoleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if ext := filepath.Ext(name); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		if strings.Contains(name, ".test.") {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !consoleMutationCall.Match(blob) {
			return nil
		}
		rel, err := filepath.Rel(consoleRoot, path)
		if err != nil {
			return err
		}
		switch {
		case specDrivenFormComponent[name]:
			covered = append(covered, rel)
		case bespokeConsoleMutationSites[rel] != "":
			bespoke = append(bespoke, rel)
		default:
			unaccounted = append(unaccounted, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	sort.Strings(covered)
	sort.Strings(bespoke)
	sort.Strings(unaccounted)

	if len(covered) == 0 {
		t.Fatal("not one spec-driven form was found: the walk read nothing, which is a failure, not a pass")
	}
	t.Logf("console sends a body from %d place(s): %d through the registry (checked), %d hand-built (named below)",
		len(covered)+len(bespoke), len(covered), len(bespoke))

	var w strings.Builder
	fmt.Fprintf(&w, "%d place(s) build the request body themselves, outside the registry, and are therefore NOT covered by the body-contract gate:\n", len(bespoke))
	for _, rel := range bespoke {
		fmt.Fprintf(&w, "  %s\n      %s\n", rel, bespokeConsoleMutationSites[rel])
	}
	t.Log(w.String())

	var stale []string
	seen := map[string]bool{}
	for _, rel := range bespoke {
		seen[rel] = true
	}
	for rel := range bespokeConsoleMutationSites {
		if !seen[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)

	if len(unaccounted) == 0 && len(stale) == 0 {
		return
	}
	var b strings.Builder
	if len(unaccounted) > 0 {
		fmt.Fprintf(&b, "%d place(s) send the edge a body from outside the registry and outside the named list — decide which it is: route it through the registry, or name it as deliberately uncovered:\n", len(unaccounted))
		for _, rel := range unaccounted {
			fmt.Fprintf(&b, "  %s\n", rel)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(&b, "%d entr(y/ies) in bespokeConsoleMutationSites match nothing — delete them, an unused exemption is one the next blind spot inherits:\n", len(stale))
		for _, rel := range stale {
			fmt.Fprintf(&b, "  %s\n", rel)
		}
	}
	t.Fatal(b.String())
}
