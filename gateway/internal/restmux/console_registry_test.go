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
	"strings"
)

// consoleField — одно поле формы ресурса консоли.
type consoleField struct {
	Name string
	// Line — строка объявления поля (для адресуемости находки).
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
	// SanitizeSource — исходный текст `sanitize`, если ресурс его объявляет.
	// Это последнее, что видит тело перед отправкой, и без него набор ключей
	// формы — не набор ключей провода.
	SanitizeSource string
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
}

// parseConsoleRegistry разбирает один `resource-registry.tsx`.
//
// extern — строковые константы, экспортированные другими модулями консоли
// (`GEO_REGIONS_PATH` и родня). Реестр импортирует их, а разбор одного файла о
// них знать не может; неразрешённое имя остаётся ошибкой, а не пропуском.
func parseConsoleRegistry(file, src string, extern map[string]string) (consoleParse, error) {
	out := consoleParse{File: file}

	consts, err := jsTopLevelConsts(src)
	if err != nil {
		return out, err
	}
	for name, v := range extern {
		if _, local := consts[name]; local {
			continue
		}
		consts[name] = jsValue{kind: jsString, str: v}
	}

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
	for _, c := range consts {
		out.FieldDecls += countFieldDecls(c)
	}

	for _, prop := range registry.obj {
		spec, tmpl, err := parseConsoleSpec(file, prop.key, prop.val, consts)
		if err != nil {
			return out, err
		}
		// Объект, который возвращает `template`, живёт внутри стрелки, а стрелка
		// для разбора — непонятое выражение; в обход константного дерева его
		// ключи в счёт не попали бы, и счётчики разошлись бы на ровном месте.
		out.FieldDecls += countFieldDecls(tmpl)
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

// parseConsoleSpec вытаскивает из одного ресурса реестра то, что решает состав
// тела: путь мутации, поддерживаемые операции, поля и шаблон.
func parseConsoleSpec(file, id string, v jsValue, consts map[string]jsValue) (consoleSpec, jsValue, error) {
	if v.kind != jsObject {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q is %s, expected an object literal", file, v.line, id, v.kind)
	}
	s := consoleSpec{File: file, Line: v.line, ID: id}

	apiPath, err := requireStringProp(file, id, v, "apiPath", consts)
	if err != nil {
		return consoleSpec{}, jsValue{}, err
	}
	s.MutationBasePath = apiPath
	if adminV, ok := v.prop("admin"); ok {
		resolved, err := resolveConst(adminV, consts)
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
		baseStr, err := resolveString(base, consts)
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
		s.Fields, err = parseConsoleFields(file, id, fieldsV, consts)
		if err != nil {
			return consoleSpec{}, jsValue{}, err
		}
	}

	tmplV, ok := v.prop("template")
	if !ok {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q has no `template`: the create body starts from it", file, v.line, id)
	}
	obj, err := arrowReturnedObject(tmplV)
	if err != nil {
		return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `template`: %w", file, tmplV.line, id, err)
	}
	s.TemplateKeys = objectKeyPaths(obj, "")

	if sanV, ok := v.prop("sanitize"); ok {
		if sanV.kind != jsOpaque {
			return consoleSpec{}, jsValue{}, fmt.Errorf("%s:%d: resource %q: `sanitize` is %s, expected a function", file, sanV.line, id, sanV.kind)
		}
		s.SanitizeSource = sanV.raw
	}
	return s, obj, nil
}

// parseConsoleFields разбирает массив `fields`. Элемент — либо объект-описание
// поля, либо ссылка на константу того же файла. Всё прочее — ошибка: молча
// пропущенный элемент это ровно то «ничего не нашли», ради невозможности
// которого гейт и пишется.
func parseConsoleFields(file, id string, v jsValue, consts map[string]jsValue) ([]consoleField, error) {
	if v.kind != jsArray {
		return nil, fmt.Errorf("%s:%d: resource %q: `fields` is %s, expected an array literal", file, v.line, id, v.kind)
	}
	out := make([]consoleField, 0, len(v.arr))
	for _, elem := range v.arr {
		resolved, err := resolveConst(elem, consts)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: resource %q: `fields`: %w", file, elem.line, id, err)
		}
		if resolved.kind != jsObject {
			return nil, fmt.Errorf("%s:%d: resource %q: `fields` entry is %s, expected a field object or a const naming one", file, resolved.line, id, resolved.kind)
		}
		f, err := parseConsoleField(file, id, resolved, consts)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	// Каждый элемент массива обязан дать ровно одно поле. Пропуск здесь — это
	// поле, которое форма отправит, а гейт не посмотрит; сверка с сырым текстом
	// его НЕ поймает, потому что дерево значений при этом остаётся полным.
	if len(out) != len(v.arr) {
		return nil, fmt.Errorf("%s:%d: resource %q: `fields` has %d entries, extraction produced %d — extraction is dropping fields the parser already read", file, v.line, id, len(v.arr), len(out))
	}
	return out, nil
}

func parseConsoleField(file, id string, v jsValue, consts map[string]jsValue) (consoleField, error) {
	nameV, ok := v.prop("name")
	if !ok {
		return consoleField{}, fmt.Errorf("%s:%d: resource %q: a field without `name`", file, v.line, id)
	}
	name, err := resolveString(nameV, consts)
	if err != nil {
		return consoleField{}, fmt.Errorf("%s:%d: resource %q: field `name`: %w", file, nameV.line, id, err)
	}
	f := consoleField{Name: name, Line: nameV.line}
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
		f.ItemFields, err = parseConsoleFields(file, id, itemsV, consts)
		if err != nil {
			return consoleField{}, err
		}
	}
	return f, nil
}

// countFieldDecls считает объявления поля формы внутри значения: объект с
// ключом `name` строкой. Обход рекурсивный, поэтому `itemFields` учитываются.
func countFieldDecls(v jsValue) int {
	n := 0
	switch v.kind {
	case jsObject:
		if nv, ok := v.prop("name"); ok && nv.kind == jsString {
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

func requireStringProp(file, id string, v jsValue, key string, consts map[string]jsValue) (string, error) {
	pv, ok := v.prop(key)
	if !ok {
		return "", fmt.Errorf("%s:%d: resource %q has no `%s`", file, v.line, id, key)
	}
	s, err := resolveString(pv, consts)
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

// resolveConst разворачивает ссылку на константу того же файла. Неизвестное имя
// — ошибка: поле, объявленное там, куда разбор не дотянулся, иначе просто
// исчезло бы из проверки.
func resolveConst(v jsValue, consts map[string]jsValue) (jsValue, error) {
	if v.kind != jsIdent {
		return v, nil
	}
	c, ok := consts[v.str]
	if !ok {
		return jsValue{}, fmt.Errorf("reference to %q, which is not a top-level const of this file", v.str)
	}
	return c, nil
}

func resolveString(v jsValue, consts map[string]jsValue) (string, error) {
	r, err := resolveConst(v, consts)
	if err != nil {
		return "", err
	}
	if r.kind != jsString {
		return "", fmt.Errorf("value is %s, expected a string literal", r.kind)
	}
	return r.str, nil
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
