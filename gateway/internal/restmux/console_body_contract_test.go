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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
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
	exportedStringConst = regexp.MustCompile(`(?m)^export const ([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*"([^"]*)";`)

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
	extern, err := consoleExportedStringConsts(consoleRoot)
	if err != nil {
		t.Fatalf("collect exported string consts: %v", err)
	}

	var findings []consoleFinding
	specs, mutable, createBodies, updateBodies := 0, 0, 0, 0
	sanitized, synthesized := 0, 0

	for _, file := range files {
		blob, err := os.ReadFile(file) //nolint:gosec // путь получен обходом дерева репозитория
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		text := string(blob)
		rel := mustRel(root, file)

		parsed, err := parseConsoleRegistry(rel, text, extern)
		if err != nil {
			// Непонятая конструкция — отказ, а не пропуск: разбор, который
			// «не смог» и промолчал, и есть тот самый ноль без содержания.
			t.Fatalf("%s: the registry could not be read, so nothing in it was checked: %v", rel, err)
		}

		// Сверка с независимым счётом — до любых выводов о находках.
		if want := len(apiPathLiteral.FindAllString(text, -1)); want != len(parsed.Specs) {
			t.Errorf("%s: scanner sees %d resources, the raw text declares %d — the scanner is losing resources, and every body in them would go unchecked",
				rel, len(parsed.Specs), want)
		}
		if want := len(fieldNameLiteral.FindAllString(text, -1)); want != parsed.FieldDecls {
			t.Errorf("%s: scanner sees %d field declarations, the raw text declares %d — the scanner is losing fields, and every one of them would go unchecked",
				rel, parsed.FieldDecls, want)
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

	t.Logf("scanned %d console registries, %d resources (%d offer a mutation): %d create bodies, %d update bodies; sanitize removes %d key(s) and synthesises %d; %d finding(s)",
		len(files), specs, mutable, createBodies, updateBodies, sanitized, synthesized, len(findings))

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

// consoleRegistryFiles ищет реестры ОБХОДОМ дерева, а не списком путей: новый
// remote попадает под проверку сам, без правки гейта. Список путей означал бы,
// что забытая правка списка выглядит как «нарушений нет».
func consoleRegistryFiles(root string) ([]string, error) {
	var out []string
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
		if d.Name() == consoleRegistryFileName {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
		blob, err := os.ReadFile(path) //nolint:gosec // путь получен обходом дерева репозитория
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
		out = append(out, consoleFinding{
			bodyFinding: f,
			File:        spec.File,
			Line:        consoleFieldLine(spec, f.Key),
			SpecID:      spec.ID,
			Op:          op,
		})
	}
	return out
}

// consoleFieldLine адресует находку строкой объявления поля; для ключа, пришедшего
// из `template`, адресом остаётся сам ресурс.
func consoleFieldLine(spec consoleSpec, key string) int {
	head := key
	if i := strings.IndexAny(head, ".["); i >= 0 {
		head = head[:i]
	}
	for _, f := range spec.Fields {
		if f.Name == key || f.Name == head || strings.HasPrefix(f.Name, head+".") {
			return f.Line
		}
		for _, it := range f.ItemFields {
			if it.Name == key {
				return it.Line
			}
		}
	}
	return spec.Line
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
var consoleMutationCall = regexp.MustCompile(
	`\bapi\.(?:create|update|post|patch|put)\(` +
		`|\bfetch\([^)]*\{(?s:.*?)\bbody\s*:` +
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
	"vpc/src/pages/iam/AccessBindingsPage.tsx":                                                              "выдача и отзыв доступа со страницы, тело из локального состояния",
	"vpc/src/pages/iam/AccessPage.tsx":                                                                      "выдача и отзыв доступа со страницы, тело из локального состояния",
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
	"shared/src/api/cluster.ts": "тонкая обёртка над кластерным RPC, тело — аргумент вызывающего",

	// Транспорт. Он ВЫПОЛНЯЕТ запрос, который собрали типизированные обёртки, и
	// тело получает аргументом (`opts.body`) — своего состава у него нет.
	// Виден здесь только потому, что детектор теперь замечает и сырой fetch.
	"compute/src/lib/api-client.ts":  "транспорт: выполняет запрос, тело — аргумент",
	"nlb/src/lib/api-client.ts":      "транспорт: выполняет запрос, тело — аргумент",
	"registry/src/lib/api-client.ts": "транспорт: выполняет запрос, тело — аргумент",
	"storage/src/lib/api-client.ts":  "транспорт: выполняет запрос, тело — аргумент",

	// Провайдер личности. Эти тела уезжают НЕ на наш REST-край, а в Kratos, и
	// нашим контрактом не описаны — сверять их с нашими message нечем.
	"compute/src/lib/kratos.ts":          "клиент провайдера личности, не наш край",
	"nlb/src/lib/kratos.ts":              "клиент провайдера личности, не наш край",
	"registry/src/lib/kratos.ts":         "клиент провайдера личности, не наш край",
	"storage/src/lib/kratos.ts":          "клиент провайдера личности, не наш край",
	"shared/src/lib/kratos.ts":           "клиент провайдера личности, не наш край",
	"shared/src/pages/auth/Register.tsx": "регистрация идёт в провайдер личности (csrf+webauthn), не на наш край",
	"shared/src/api/iam.ts":              "тонкая обёртка над RPC IAM, тело — аргумент вызывающего",
	"shared/src/api/tokens.ts":           "тонкая обёртка над RPC токенов, тело — аргумент вызывающего",
	"compute/src/api/resources.ts":       "тонкая обёртка над RPC ресурса, тело — аргумент вызывающего",
	"nlb/src/api/resources.ts":           "тонкая обёртка над RPC ресурса, тело — аргумент вызывающего",
	"registry/src/api/resources.ts":      "тонкая обёртка над RPC ресурса, тело — аргумент вызывающего",
	"storage/src/api/resources.ts":       "тонкая обёртка над RPC ресурса, тело — аргумент вызывающего",
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
		blob, err := os.ReadFile(path) //nolint:gosec // путь получен обходом дерева репозитория
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
