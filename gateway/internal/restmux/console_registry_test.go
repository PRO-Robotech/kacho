// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// console_registry_test.go — статическое извлечение того, ЧТО КОНСОЛЬ КЛАДЁТ В
// ТЕЛО запроса, из реестра ресурсов `ui-future/*/src/lib/resource-registry.tsx`.
//
// Файл тестовый намеренно: инструмент гейта, не код края (ban #11 — в прод-бинарь
// он не попадает).
//
// Зачем вообще. Класс «ключ, которого сервер не читает» измерялся до сих пор
// только для чёрного ящика (регрессионные suite'ы), а для интерфейса, через
// который ходят ЛЮДИ, — не измерялся вовсе. Именно этой дырой поле создания
// реестра доехало до провода, хотя сообщение создания его не несёт: оператор
// выбирал значение, край молча выбрасывал ключ, ресурс возвращался с другим —
// за успешным тостом.
//
// Что именно извлекается. Консоль собирает тело создания как
// `buildCreateBody(applyFieldDefaults(spec.fields, spec.template(ctx)))`, тело
// правки — из полей, названных маской (`buildUpdateBody`). Значения на контракт
// не влияют: сверяются ИМЕНА. Поэтому статически достаточно взять из каждого
// ресурса реестра три вещи — путь мутации, набор `fields` с их флагами и ключи
// `template` — и из них вывести множество путей, которые попадут в тело.
//
// Почему не «запустить консоль». Реестр — TSX-модуль; выполнить его значило бы
// притащить node_modules в гейт Go и сделать проверку зависимой от того, что в
// среде есть тулчейн. Гейт, который «не смог запуститься», — это гейт, который
// молча ничего не проверил. Разбор же целиком статический и работает на голом
// дереве.
//
// Чем этот разбор НЕ может тихо промолчать (это его главное свойство):
//   - файлы ищутся ОБХОДОМ дерева по имени, а не списком путей — новый remote
//     попадает в область сам;
//   - число разобранных ресурсов сверяется с НЕЗАВИСИМЫМ подсчётом по сырому
//     тексту, число разобранных полей — тоже; расхождение роняет гейт;
//   - конструкция, которую разбор не понимает (поле-ссылка на неизвестную
//     константу, `fields` не массивом, `template` не стрелкой с объектом),
//     не пропускается, а ЛОМАЕТ разбор с указанием файла и строки.
//
// Чего разбор НЕ видит и почему это записано, а не умолчано: `spec.sanitize`
// — произвольная функция, её вывод статически не вычислим. Ключи, которые
// sanitize СИНТЕЗИРУЕТ (переносит значение формы в ветку oneof), в область не
// попадают. Ключи, которые она УДАЛЯЕТ, наоборот, в области остаются — поэтому
// каждая находка проверяется глазами перед тем, как считаться дефектом.
// Форменные ключи формы (ведущее `_`) исключены не догадкой, а по коду:
// `buildCreateBody`/`buildUpdateBody` прогоняют тело через `stripFormOnlyKeys`,
// который снимает их на любой глубине.

import (
	"fmt"
	"regexp"
	"strings"
)

// consoleField — одно поле формы ресурса консоли.
type consoleField struct {
	Name string
	// File / Line — координата ОБЪЯВЛЕНИЯ поля.
	//
	// Файл хранится рядом со строкой, а не берётся у ресурса: поле, пришедшее из
	// общего модуля, объявлено в другом файле, и адрес «файл реестра + строка
	// помощника» посылает читателя искать в место, где этих строк нет.
	File string
	Line int
	// Hidden — поле не рендерится, но значение в теле есть (project_id из контекста).
	Hidden bool
	// EditHidden / CreateOnly — поле есть при создании, в правке его нет.
	EditHidden bool
	CreateOnly bool
	// UpdateOnly — поле есть ТОЛЬКО в сообщении правки; при создании его быть не должно.
	UpdateOnly bool
	// Immutable — в маску не попадает (край всё равно откажет).
	Immutable bool
	// ItemFields — под-поля одного элемента массива (`type: "array"`).
	ItemFields []consoleField
}

// consoleSpec — один ресурс реестра консоли.
type consoleSpec struct {
	File string
	Line int
	ID   string
	// MutationBasePath — путь, по которому консоль шлёт создание и правку:
	// `admin.basePath ?? apiPath` (`mutationBasePath()` в самом реестре).
	MutationBasePath string
	CanCreate        bool
	CanUpdate        bool
	Fields           []consoleField
	// TemplateKeys — точечные пути ключей объекта, который возвращает `template`.
	TemplateKeys []string
	// ExpandedFields — сколько полей ресурса пришло РАСКРЫТИЕМ набора-помощника.
	// Провенанс нужен затем, что «раскрытие работает» и «раскрытие дало пусто»
	// выглядят одинаково зелёными: без счётчика утверждать первое нельзя.
	ExpandedFields int
	// SanitizeSource — исходный текст `sanitize`, если ресурс его объявляет.
	// Это последнее, что видит тело перед отправкой, и без него набор ключей
	// формы — не набор ключей провода.
	SanitizeSource string
	// TemplateExtern — шаблон пришёл из общего модуля: его ключи в сыром тексте
	// этого файла не видны, поэтому в независимый счёт они не идут.
	TemplateExtern bool
	// ExternFields — сколько объявлений поля пришло из ДРУГОГО файла (общий
	// модуль). Провенанс нужен сверке с сырым текстом: этих объявлений в тексте
	// своего файла нет, и без вычитания сверка краснела бы на единственном
	// источнике.
	ExternFields int
	// ExternHelperFields — из них пришедших раскрытием ПОМОЩНИКА общего модуля.
	//
	// Считается отдельно от ExternFields намеренно: тот включает и целый массив,
	// взятый по имени, и перепись по нему отвечала бы на другой вопрос, чем
	// заданный. Число, верное для другого предиката, — не число.
	ExternHelperFields int
	// SharedRef — идентификатор в ОБЩЕМ реестре, если запись не объявляет спеку,
	// а ссылается на уже объявленную (`SHARED_REGISTRY["<id>"]`).
	//
	// ЗАЧЕМ ОТДЕЛЬНЫЙ ВИД. Раздел, который монтируют два приложения, обязан иметь
	// ОДНО объявление: вторая копия разошлась бы с первой молча — так уже
	// разошлись копии формы ресурса. Но для сканера такая запись — выражение, и
	// прежде он отказывался читать ВЕСЬ файл, то есть переставал проверять
	// реестр целиком. «Не прочитал» выглядело как «нет находок».
	//
	// Свойство при этом не теряется: спеку проверяют там, где она объявлена, а
	// здесь проверяется, что ссылка ведёт в существующую запись общего реестра
	// (см. TestConsoleSharedRefsResolve). Пустая строка означает обычную спеку.
	SharedRef string
}

// consoleParse — результат разбора одного файла реестра вместе с числами, по
// которым проверяется, что разбор ничего не потерял.
type consoleParse struct {
	File  string
	Specs []consoleSpec
	// FieldDecls — сколько объявлений поля формы разбор увидел ВСЕГО (включая
	// объявленные константой и не использованные ни одним ресурсом). Сверяется с
	// независимым подсчётом по сырому тексту.
	FieldDecls int
	// ExternSpecs / ExternFieldDecls — сколько пришло НЕ из этого файла.
	//
	// Сверка разбора с независимым счётом по сырому тексту — второй слой защиты
	// от недосчёта, и он верен ровно пока всё объявлено здесь. Ресурс, ссылающийся
	// на общее объявление (`SHARED_REGISTRY["…"]`, импортированный набор полей),
	// в сыром тексте своего файла не виден — без этих счётчиков сверка объявила
	// бы находкой ровно то, ради чего единый источник и заводился.
	ExternSpecs      int
	ExternFieldDecls int
}

// consoleScope — что видно в точке разбора значения.
//
// Помощник, разворачиваемый в `fields`, читается в СВОЕЙ области видимости:
// параметры связаны литеральными аргументами вызова, его `const`-связывания —
// уже разрешёнными значениями. Без этого имя поля, собранное из параметра, не
// раскрыть — а именно так реестр выражает симметричные наборы.
type consoleScope struct {
	consts map[string]jsValue
	funcs  map[string]jsFuncRef
	local  map[string]jsValue
}

// jsFuncRef — помощник-набор полей вместе с областью, в которой читается ЕГО тело.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ВИД, а не просто функция. Набор полей, который объявляют два
// реестра, обязан быть объявлен ОДИН раз и в общем модуле — иначе копии
// расходятся молча (так уже разошлись ветви проверки живости, #375). Реестр
// тогда разворачивает помощника, объявленного НЕ ЗДЕСЬ, и его тело обязано
// читаться в области видимости СВОЕГО файла: соседний помощник, на который оно
// ссылается, живёт там и в файле-потребителе не виден вовсе.
//
// Прежде такой помощник был для сканера просто неизвестным именем, и разбор
// отказывался читать ВЕСЬ файл — то есть переставал проверять реестр целиком.
// «Не прочитал» выглядело как «нет находок».
type jsFuncRef struct {
	fn jsFunc
	// base — область модуля, объявившего помощника. nil означает «тот же файл».
	base *consoleScope
}

func (s consoleScope) lookup(name string) (jsValue, bool) {
	if v, ok := s.local[name]; ok {
		return v, true
	}
	v, ok := s.consts[name]
	return v, ok
}

// lookupString — вид области видимости, нужный подстановке в шаблон.
func (s consoleScope) lookupString(name string) (string, bool) {
	v, ok := s.lookup(name)
	if !ok || v.kind != jsString {
		return "", false
	}
	return v.str, true
}

func (s consoleScope) with(local map[string]jsValue) consoleScope {
	return consoleScope{consts: s.consts, funcs: s.funcs, local: local}
}

// parseConsoleRegistry разбирает один `resource-registry.tsx`.
//
// ext — всё, что реестр берёт ИЗ ДРУГИХ файлов дерева: строковые константы
// путей, значения-наборы полей и помощники-наборы полей. Разбор одного файла о
// них знать не может; неразрешённое имя остаётся ошибкой, а не пропуском.
//
// Один аргумент, а не три: каждый новый вид импортируемого прежде добавлял
// параметр во все девять точек вызова, и пропущенная точка выглядела бы не
// ошибкой сборки, а тихо суженной областью видимости — то есть ровно тем
// «прочитал не всё», ради невозможности которого сканер и написан.
func parseConsoleRegistry(file, src string, ext consoleExterns) (consoleParse, error) {
	out := consoleParse{File: file}
	extern, externVals, externFuncs := ext.strings, ext.values, ext.helpers

	consts, err := jsTopLevelConsts(src)
	if err != nil {
		return out, err
	}
	// Имена, объявленные ЗДЕСЬ, — до подмешивания внешних. По ним считается
	// независимый счётчик объявлений поля: внешние значения в сыром тексте этого
	// файла не видны, и их учёт разошёлся бы со сверкой ровно на их размер.
	own := make(map[string]bool, len(consts))
	for name := range consts {
		own[name] = true
	}
	for name, v := range extern {
		if _, local := consts[name]; local {
			continue
		}
		consts[name] = jsValue{kind: jsString, str: v}
	}
	// Значения, экспортированные другими модулями (набор полей формы, объект
	// пустого состояния). Своё объявление файла всегда сильнее внешнего: имя,
	// объявленное здесь, здесь и разрешается.
	for name, v := range externVals {
		if _, local := consts[name]; local {
			continue
		}
		consts[name] = v
	}
	// Помощники ЭТОГО файла держатся отдельно от импортированных: по ним, и
	// только по ним, считается независимый счётчик объявлений поля — тела
	// импортированных в сыром тексте этого файла не видны by construction.
	ownFuncs := jsTopLevelFieldSetFuncs(file, src)
	funcs := make(map[string]jsFuncRef, len(ownFuncs)+len(externFuncs))
	for name, ref := range externFuncs {
		funcs[name] = ref
	}
	// Своё объявление файла всегда сильнее импортированного — тот же порядок,
	// что у констант выше.
	for name, fn := range ownFuncs {
		funcs[name] = jsFuncRef{fn: fn}
	}
	scope := consoleScope{consts: consts, funcs: funcs}

	registry, ok := consts["REGISTRY"]
	if !ok {
		return out, fmt.Errorf("%s: no top-level `REGISTRY` const: the file is a resource registry or it is not, and a scanner that shrugs here checks nothing", file)
	}
	if registry.kind != jsObject {
		return out, fmt.Errorf("%s:%d: `REGISTRY` is %s, expected an object literal", file, registry.line, registry.kind)
	}

	// Объявления полей считаются по ВСЕМ константам файла, а не только по
	// достижимым из REGISTRY: независимый счётчик по сырому тексту тоже не знает
	// про достижимость, и сравнивать надо сравнимое.
	for name, c := range consts {
		if !own[name] {
			continue
		}
		out.FieldDecls += countFieldDecls(c)
	}
	// То же и для помощников: набор, вынесенный в функцию, объявлен ОДИН раз,
	// сколько бы раз его ни разворачивали. Считать по разворотам значило бы
	// сверять число объявлений с числом употреблений. Берутся ТОЛЬКО свои:
	// импортированный помощник объявлен в другом файле и считается там.
	for _, fn := range ownFuncs {
		out.FieldDecls += countFieldDecls(fn.body)
	}

	for _, prop := range registry.obj {
		spec, tmpl, err := parseConsoleSpec(file, prop.key, prop.val, scope)
		if err != nil {
			return out, err
		}
		// Объект, который возвращает `template`, живёт внутри стрелки, а стрелка
		// для разбора — непонятое выражение; в обход константного дерева его
		// ключи в счёт не попали бы, и счётчики разошлись бы на ровном месте.
		if !spec.TemplateExtern {
			out.FieldDecls += countFieldDecls(tmpl)
		}
		if spec.SharedRef != "" {
			out.ExternSpecs++
		}
		out.ExternFieldDecls += spec.ExternFields
		out.Specs = append(out.Specs, spec)
	}
	// Сверка по мощности — второй слой защиты от недосчёта, и он ловит другое,
	// чем сверка с сырым текстом. Та сверяет, что РАЗБОР ЗНАЧЕНИЙ не потерял
	// объект; эта — что ИЗВЛЕЧЕНИЕ не потеряло того, что разбор уже увидел.
	// Первый слой на второй дефект не реагирует вовсе: счёт по дереву значений
	// остаётся верным, пока извлечение выбрасывает элементы молча.
	if len(out.Specs) != len(registry.obj) {
		return out, fmt.Errorf("%s: `REGISTRY` declares %d resources, extraction produced %d — extraction is dropping entries the parser already read", file, len(registry.obj), len(out.Specs))
	}
	return out, nil
}

// sharedRegistryRefPattern — единственная форма ссылки, которую сканер признаёт.
//
// Форма НАМЕРЕННО узкая: она распознаётся текстом, а не разбором произвольного
// выражения, поэтому любое отступление от неё остаётся громким отказом. Гейт,
// который начал бы «понимать» всё подряд, перестал бы что-либо находить.
var sharedRegistryRefPattern = regexp.MustCompile(`^SHARED_REGISTRY\[\s*"([A-Za-z0-9._-]+)"\s*\]$`)

// sharedRegistryRef — идентификатор общей записи, на которую ссылается значение.
func sharedRegistryRef(v jsValue) (string, bool) {
	if v.kind != jsOpaque {
		return "", false
	}
	m := sharedRegistryRefPattern.FindStringSubmatch(strings.TrimSpace(v.raw))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// parseConsoleSpec вытаскивает из одного ресурса реестра то, что решает состав
// тела: путь мутации, поддерживаемые операции, поля и шаблон.
func parseConsoleSpec(file, id string, v jsValue, scope consoleScope) (consoleSpec, jsValue, error) {
	if ref, ok := sharedRegistryRef(v); ok {
		// Ссылка на общее объявление — не спека, а указатель на неё. Полей у
		// записи нет by construction, поэтому дальше её вести некуда.
		return consoleSpec{File: file, Line: v.line, ID: id, SharedRef: ref}, jsValue{}, nil
	}
	if v.kind != jsObject {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q is %s, expected an object literal", file, v.line, id, v.kind)
	}
	s := consoleSpec{File: file, Line: v.line, ID: id}

	apiPath, err := requireStringProp(file, id, v, "apiPath", scope)
	if err != nil {
		return consoleSpec{}, jsValue{}, err
	}
	s.MutationBasePath = apiPath
	if adminV, ok := v.prop("admin"); ok {
		resolved, err := resolveConst(adminV, scope)
		if err != nil {
			return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `admin`: %w", file, adminV.line, id, err)
		}
		if resolved.kind != jsObject {
			return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `admin` is %s, expected an object literal", file, resolved.line, id, resolved.kind)
		}
		base, ok := resolved.prop("basePath")
		if !ok {
			return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `admin` without `basePath`", file, resolved.line, id)
		}
		baseStr, err := resolveString(base, scope)
		if err != nil {
			return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `admin.basePath`: %w", file, base.line, id, err)
		}
		s.MutationBasePath = baseStr
	}

	opsV, ok := v.prop("ops")
	if !ok {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q has no `ops`: which mutations the console offers is exactly what decides whether its body is checked", file, v.line, id)
	}
	if opsV.kind != jsObject {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `ops` is %s, expected an object literal", file, opsV.line, id, opsV.kind)
	}
	s.CanCreate, err = boolProp(file, id, opsV, "create")
	if err != nil {
		return consoleSpec{}, jsValue{}, err
	}
	s.CanUpdate, err = boolProp(file, id, opsV, "update")
	if err != nil {
		return consoleSpec{}, jsValue{}, err
	}

	if fieldsV, ok := v.prop("fields"); ok {
		set, err := parseConsoleFields(file, id, fieldsV, scope)
		if err != nil {
			return consoleSpec{}, jsValue{}, err
		}
		s.Fields, s.ExpandedFields = set.fields, set.expanded
		// Пришедшее из общего модуля: в сыром тексте ЭТОГО файла его объявлений
		// нет, и сверка счётчиков обязана это знать. Считается и целый массив,
		// взятый по имени, и отдельный набор, раскрытый импортированным
		// помощником, — источник у них один, и провенанс обязан быть один.
		s.ExternFields, s.ExternHelperFields = set.extern, set.extern
		if set.whole {
			s.ExternFields = len(s.Fields)
		}
	}

	tmplV, ok := v.prop("template")
	if !ok {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q has no `template`: the create body starts from it", file, v.line, id)
	}
	// Шаблон, объявленный ОДИН раз в общем модуле: тот же ресурс монтируют два
	// приложения. Резолв тот же, что у полей; неразрешимое имя остаётся отказом.
	templateExtern := false
	if tmplV.kind == jsIdent {
		if resolved, ok := scope.lookup(tmplV.str); ok {
			tmplV, templateExtern = resolved, true
		}
	}
	obj, err := arrowReturnedObject(tmplV)
	if err != nil {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `template`: %w", file, tmplV.line, id, err)
	}
	s.TemplateKeys = objectKeyPaths(obj, "")
	s.TemplateExtern = templateExtern

	if sanV, ok := v.prop("sanitize"); ok {
		if sanV.kind != jsOpaque {
			return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `sanitize` is %s, expected a function", file, sanV.line, id, sanV.kind)
		}
		s.SanitizeSource = sanV.raw
	}
	return s, obj, nil
}

// parseConsoleFields разбирает массив `fields`. Элемент — объект-описание поля,
// ссылка на константу того же файла либо развёрнутый набор от помощника этого же
// файла. Всё прочее — ошибка: молча пропущенный элемент это ровно то «ничего не
// нашли», ради невозможности которого гейт и пишется.
func parseConsoleFields(file, id string, v jsValue, scope consoleScope) (consoleFieldSet, error) {
	var set consoleFieldSet
	// Набор полей, объявленный ОДИН раз в общем модуле и импортированный сюда:
	// тот же ресурс монтируют два приложения, и выписанные дважды поля разошлись
	// бы молча. Резолв — тот же, что у прочих ссылок на константы; неразрешимое
	// имя остаётся громким отказом строкой ниже.
	if v.kind == jsIdent {
		if resolved, ok := scope.lookup(v.str); ok {
			v, set.whole = resolved, true
		}
	}
	if v.kind != jsArray {
		return consoleFieldSet{}, fmt.Errorf("%s:%d: resource %q: `fields` is %s, expected an array literal", file, v.line, id, v.kind)
	}
	// want считается ОТДЕЛЬНО от построения out и по другому источнику: длина
	// литерала (для развёрнутого набора — длина массива, который возвращает
	// помощник). Иначе сверка была бы тавтологией и пропуск элемента остался бы
	// незамеченным: дерево значений при таком дефекте полное, и сверка с сырым
	// текстом на него не реагирует.
	want := 0
	out := make([]consoleField, 0, len(v.arr))
	for _, elem := range v.arr {
		if elem.kind == jsSpread {
			fields, n, extern, err := expandConsoleFieldSpread(file, id, elem, scope)
			if err != nil {
				return consoleFieldSet{}, err
			}
			want += n
			set.expanded += n
			if extern {
				set.extern += n
			}
			out = append(out, fields...)
			continue
		}
		want++
		// Помощник, возвращающий ОДНО поле, стоит элементом, а не спредом:
		// `targetsField()`. Форма та же и по той же причине — поле объявляют два
		// реестра, значит объявить его надо один раз. Ветка стоит ДО резолва
		// константы: вызов — не идентификатор, и без неё он доехал бы до отказа
		// «это не объект», указывающего на форму записи вместо её источника.
		if f, n, extern, ok, err := expandConsoleFieldCall(file, id, elem, scope); ok {
			if err != nil {
				return consoleFieldSet{}, err
			}
			set.expanded += n
			if extern {
				set.extern += n
			}
			out = append(out, f)
			continue
		}
		resolved, err := resolveConst(elem, scope)
		if err != nil {
			return consoleFieldSet{}, fmt.Errorf("%s:%d: resource %q: `fields`: %w", file, elem.line, id, err)
		}
		if resolved.kind != jsObject {
			return consoleFieldSet{}, fmt.Errorf("%s:%d: resource %q: `fields` entry is %s, expected a field object, a const naming one, or a call of a helper declared in this file or in a shared module", file, resolved.line, id, resolved.kind)
		}
		f, err := parseConsoleField(file, id, resolved, scope)
		if err != nil {
			return consoleFieldSet{}, err
		}
		out = append(out, f)
	}
	if len(out) != want {
		return consoleFieldSet{}, fmt.Errorf("%s:%d: resource %q: `fields` declares %d field(s), extraction produced %d — extraction is dropping fields the parser already read", file, v.line, id, want, len(out))
	}
	set.fields = out
	return set, nil
}

// consoleFieldSet — разобранный `fields` вместе с провенансом.
//
// Провенанс нужен не для отчёта: «раскрытие работает» и «раскрытие дало пусто»
// выглядят одинаково зелёными, а сверка с сырым текстом файла обязана знать,
// какие объявления в этом тексте отсутствуют by construction.
type consoleFieldSet struct {
	fields []consoleField
	// expanded — сколько полей пришло раскрытием помощника (своего или общего).
	expanded int
	// extern — сколько пришло из ДРУГОГО файла.
	extern int
	// whole — весь массив взят по имени из общего модуля (`fields: SHARED_FIELDS`).
	whole bool
}

// expandConsoleFieldSpread раскрывает `...helper(args)` в набор полей.
//
// Почему это МОДЕЛИРУЕТСЯ, а не запрещается. Два семейства VIP-источника
// описываются одним и тем же набором полей, различающимся подставляемым
// именем семейства; вынести их в помощник — нормальная запись, а не уловка.
// Требовать литералов значило бы дублировать набор ради ограничения гейта, то
// есть навязывать стиль зоне, которой гейт не владеет, по причине, не имеющей
// отношения к продукту.
//
// Почему это НЕ послабление. Раскрывается ровно одна форма: вызов помощника
// ЭТОГО ЖЕ файла, чьё тело — связывания и возврат массива, с ЛИТЕРАЛЬНЫМИ
// аргументами. Состав набора при этом целиком виден в исходнике и зависит
// только от аргументов. Всё, что за пределами формы — помощник из другого
// модуля, вычисляемый аргумент, тело с ветвлением, имя поля из выражения, —
// по-прежнему ЛОМАЕТ разбор с указанием места.
func expandConsoleFieldSpread(file, id string, elem jsValue, scope consoleScope) ([]consoleField, int, bool, error) {
	fail := func(format string, args ...any) ([]consoleField, int, bool, error) {
		return nil, 0, false, fmt.Errorf("%s:%d: resource %q: `fields` spreads %s",
			file, elem.line, id, fmt.Sprintf(format, args...))
	}
	ref, inner, err := consoleHelperCall(elem.raw, scope)
	if err != nil {
		return fail("%v", err)
	}
	if ref.fn.body.kind != jsArray {
		return fail("%s(…), whose body returns %s — a spread needs the set itself", ref.fn.name, ref.fn.body.kind)
	}

	// Тело помощника читается в области видимости ЕГО файла (см. jsFuncRef):
	// соседний помощник и константы модуля видны там, и только там.
	set, err := parseConsoleFields(helperFile(file, ref), id, ref.fn.body, inner)
	if err != nil {
		return nil, 0, false, err
	}
	// Возвращается ПРОВЕРЕННОЕ рекурсией число, а не длина массива-литерала:
	// помощник сам вправе разворачивать помощника (симметричные ветви `http` и
	// `https` объявлены один раз и подставляются именем), и тогда элементов в
	// литерале меньше, чем полей. Свойство сверки при этом не теряется: длину
	// СВОЕГО литерала каждый уровень сверяет со своим извлечением сам, и
	// пропущенный элемент остаётся находкой на том уровне, где он пропущен.
	return set.fields, len(set.fields), ref.base != nil, nil
}

// expandConsoleFieldCall раскрывает `helper()` СТОЯЩИЙ ЭЛЕМЕНТОМ — помощника,
// возвращающего одно поле.
//
// Возвращает ok=false, если элемент вообще не похож на вызов помощника: тогда
// решает общий путь (объект, ссылка на константу), и отказ приходит оттуда с его
// формулировкой. Если элемент вызовом ЯВЛЯЕТСЯ, но раскрыть его нельзя, —
// ok=true и ошибка: пропустить такой элемент значило бы потерять поле молча.
func expandConsoleFieldCall(file, id string, elem jsValue, scope consoleScope) (consoleField, int, bool, bool, error) {
	if elem.kind != jsOpaque {
		return consoleField{}, 0, false, false, nil
	}
	call, err := jsParseCall(elem.raw)
	if err != nil {
		return consoleField{}, 0, false, false, nil
	}
	if _, known := scope.funcs[call.callee]; !known {
		// Незнакомое имя — не наша ветка: общий путь скажет о нём точнее.
		return consoleField{}, 0, false, false, nil
	}
	fail := func(format string, args ...any) (consoleField, int, bool, bool, error) {
		return consoleField{}, 0, false, true, fmt.Errorf("%s:%d: resource %q: `fields` calls %s",
			file, elem.line, id, fmt.Sprintf(format, args...))
	}
	ref, inner, err := consoleHelperCall(elem.raw, scope)
	if err != nil {
		return fail("%v", err)
	}
	if ref.fn.body.kind != jsObject {
		return fail("%s(…), whose body returns %s — an entry needs one field object; a set is spread, not called", ref.fn.name, ref.fn.body.kind)
	}
	f, err := parseConsoleField(helperFile(file, ref), id, ref.fn.body, inner)
	if err != nil {
		return consoleField{}, 0, false, true, err
	}
	return f, 1, ref.base != nil, true, nil
}

// helperFile — чей файл называть в находке внутри помощника: его собственный,
// если помощник импортирован. Иначе координата вела бы в файл-потребитель, где
// этих строк нет вовсе.
func helperFile(file string, ref jsFuncRef) string {
	if ref.base != nil && ref.fn.file != "" {
		return ref.fn.file
	}
	return file
}

// consoleHelperCall разрешает вызов помощника и связывает его параметры.
//
// Область возвращается ГОТОВОЙ к чтению тела: для помощника своего файла это
// текущая область, для импортированного — область его модуля. Смешивать их
// нельзя: имя, видимое в одном файле, в другом означает другое или ничего.
func consoleHelperCall(raw string, scope consoleScope) (jsFuncRef, consoleScope, error) {
	call, err := jsParseCall(raw)
	if err != nil {
		return jsFuncRef{}, consoleScope{}, err
	}
	ref, ok := scope.funcs[call.callee]
	if !ok {
		return jsFuncRef{}, consoleScope{}, fmt.Errorf("%s(…), which is neither a helper of this file nor an exported helper of a shared module, whose body is `const…; return [ … ]` — a set of fields assembled anywhere else is not declared where it is used", call.callee)
	}
	if len(call.args) != len(ref.fn.params) {
		return jsFuncRef{}, consoleScope{}, fmt.Errorf("%s(…) with %d argument(s), but it declares %d parameter(s)", call.callee, len(call.args), len(ref.fn.params))
	}

	base := scope
	if ref.base != nil {
		base = *ref.base
	}
	local := make(map[string]jsValue, len(ref.fn.params)+len(ref.fn.locals))
	for i, p := range ref.fn.params {
		local[p] = call.args[i]
	}
	inner := base.with(local)
	// Связывания раскрываются по порядку: следующее видит предыдущие.
	for _, l := range ref.fn.locals {
		v := l.val
		if v.kind == jsTemplate {
			s, err := jsResolveTemplate(v.raw, inner.lookupString)
			if err != nil {
				return jsFuncRef{}, consoleScope{}, fmt.Errorf("%s(…): local %s: %v", call.callee, l.name, err)
			}
			v = jsValue{kind: jsString, str: s, line: l.val.line}
		}
		local[l.name] = v
	}
	return ref, inner, nil
}

func parseConsoleField(file, id string, v jsValue, scope consoleScope) (consoleField, error) {
	nameV, ok := v.prop("name")
	if !ok {
		return consoleField{}, fmt.Errorf("%s:%d: resource %q: a field without `name`", file, v.line, id)
	}
	name, err := resolveString(nameV, scope)
	if err != nil {
		return consoleField{}, fmt.Errorf("%s:%d: resource %q: field `name`: %w", file, nameV.line, id, err)
	}
	f := consoleField{Name: name, File: file, Line: nameV.line}
	for _, flag := range []struct {
		key string
		dst *bool
	}{
		{"hidden", &f.Hidden},
		{"editHidden", &f.EditHidden},
		{"createOnly", &f.CreateOnly},
		{"updateOnly", &f.UpdateOnly},
		{"immutable", &f.Immutable},
	} {
		fv, ok := v.prop(flag.key)
		if !ok {
			continue
		}
		if fv.kind != jsBool {
			return consoleField{}, fmt.Errorf("%s:%d: resource %q: field %q: `%s` is %s, expected a boolean literal", file, fv.line, id, name, flag.key, fv.kind)
		}
		*flag.dst = fv.boolV
	}
	if itemsV, ok := v.prop("itemFields"); ok {
		set, err := parseConsoleFields(file, id, itemsV, scope)
		if err != nil {
			return consoleField{}, err
		}
		f.ItemFields = set.fields
	}
	return f, nil
}

// countFieldDecls считает объявления поля формы внутри значения: объект с
// ключом `name` строкой. Обход рекурсивный, поэтому `itemFields` учитываются.
func countFieldDecls(v jsValue) int {
	n := 0
	switch v.kind {
	case jsObject:
		// Имя поля записывается тремя способами — литералом, шаблоном и ссылкой
		// на связывание. Считать только литералы значило бы объявить остальные
		// два «не объявлениями» и вывести их из-под сверки.
		if nv, ok := v.prop("name"); ok && isFieldNameValue(nv.kind) {
			n++
		}
		for _, p := range v.obj {
			n += countFieldDecls(p.val)
		}
	case jsArray:
		for _, e := range v.arr {
			n += countFieldDecls(e)
		}
	}
	return n
}

func requireStringProp(file, id string, v jsValue, key string, scope consoleScope) (string, error) {
	pv, ok := v.prop(key)
	if !ok {
		return "", fmt.Errorf("%s:%d: resource %q has no `%s`", file, v.line, id, key)
	}
	s, err := resolveString(pv, scope)
	if err != nil {
		return "", fmt.Errorf("%s:%d: resource %q: `%s`: %w", file, pv.line, id, key, err)
	}
	return s, nil
}

func boolProp(file, id string, ops jsValue, key string) (bool, error) {
	pv, ok := ops.prop(key)
	if !ok {
		return false, nil
	}
	if pv.kind != jsBool {
		return false, fmt.Errorf("%s:%d: resource %q: `ops.%s` is %s, expected a boolean literal", file, pv.line, id, key, pv.kind)
	}
	return pv.boolV, nil
}

// isFieldNameValue — формы, в которых может быть записано имя поля.
func isFieldNameValue(k jsKind) bool {
	return k == jsString || k == jsTemplate || k == jsIdent
}

// resolveConst разворачивает ссылку на имя, видимое в текущей области. Имя, не
// разрешающееся ни во что, — ошибка: поле, объявленное там, куда разбор не
// дотянулся, иначе просто исчезло бы из проверки.
func resolveConst(v jsValue, scope consoleScope) (jsValue, error) {
	if v.kind != jsIdent {
		return v, nil
	}
	c, ok := scope.lookup(v.str)
	if !ok {
		return jsValue{}, fmt.Errorf("reference to %q, which resolves to nothing visible here: neither a top-level const of this file nor a binding of the helper being expanded", v.str)
	}
	return c, nil
}

// resolveString приводит значение к строке в текущей области видимости.
//
// Шаблон раскрывается подстановкой ГОЛЫХ имён; выражение внутри `${…}` означает,
// что имя поля вычисляется, а вычисленного имени в контракте нет — отказ.
func resolveString(v jsValue, scope consoleScope) (string, error) {
	r, err := resolveConst(v, scope)
	if err != nil {
		return "", err
	}
	switch r.kind {
	case jsString:
		return r.str, nil
	case jsTemplate:
		return jsResolveTemplate(r.raw, scope.lookupString)
	default:
		return "", fmt.Errorf("value is %s, expected a string literal", r.kind)
	}
}

// objectKeyPaths раскладывает объектный литерал в точечные пути ключей.
// Ключ с ведущим `_` — форменный, до провода не доходит (`stripFormOnlyKeys`).
func objectKeyPaths(v jsValue, prefix string) []string {
	var out []string
	for _, p := range v.obj {
		if strings.HasPrefix(p.key, "_") {
			continue
		}
		path := prefix + p.key
		if p.val.kind == jsObject && len(p.val.obj) > 0 {
			out = append(out, objectKeyPaths(p.val, path+".")...)
			continue
		}
		out = append(out, path)
	}
	return out
}
