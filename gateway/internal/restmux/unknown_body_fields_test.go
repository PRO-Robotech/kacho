// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// unknown_body_fields_test.go — статический разбор «какие ключи JSON-тела сервер
// не читает».
//
// Файл тестовый намеренно: это инструмент гейта, а не код края. В прод-бинарь
// он не попадает (ban #11 — никакого мёртвого веса в рантайме).
//
// Край разбирает тело в message запроса через protojson. Ключ, которому в
// message не соответствует ни одно поле, для сервера не существует: он либо
// молча выбрасывается (`DiscardUnknown: true`), либо отвергается. И в том, и в
// другом случае знать полный список таких ключей во всём дереве нужно ДО смены
// поведения края — иначе переключение превращает тихий успех в отказ там, где
// этого никто не ждал.
//
// Разбор целиком статический: (метод, путь) -> биндинг -> message тела ->
// множество принимаемых имён полей. Стенд не нужен.

// bodyKeyKind классифицирует исход разбора одного запроса.
type bodyKeyKind string

const (
	// keyUnknown — ключ тела, которому в message запроса не соответствует поле.
	keyUnknown bodyKeyKind = "unknown-field"
	// routeUnresolved — (метод, путь) не совпал ни с одним REST-биндингом.
	// Это тоже находка: молча пропустить такой запрос значит не проверить его.
	routeUnresolved bodyKeyKind = "unresolved-route"
	// bodyNotAccepted — биндинг вообще не объявляет `body`, а запрос тело шлёт.
	bodyNotAccepted bodyKeyKind = "body-not-accepted"
)

// bodyFinding — одно расхождение между отправляемым телом и контрактом RPC.
type bodyFinding struct {
	Kind   bodyKeyKind
	Method string
	Path   string
	FQN    string
	// Key — путь ключа внутри тела (`labels`, `bootDisk.diskSpec.bogus`).
	Key string
	// Message — полное имя message, в котором ключ искали.
	Message string
}

func (f bodyFinding) String() string {
	switch f.Kind {
	case routeUnresolved:
		return fmt.Sprintf("%s: %s %s — no REST binding matches this (method, path)", f.Kind, f.Method, f.Path)
	case bodyNotAccepted:
		return fmt.Sprintf("%s: %s %s -> %s — binding declares no `body`, request sends one", f.Kind, f.Method, f.Path, f.FQN)
	default:
		return fmt.Sprintf("%s: %s %s -> %s — %q is not a field of %s", f.Kind, f.Method, f.Path, f.FQN, f.Key, f.Message)
	}
}

// analyzeRequestBody сопоставляет тело одного REST-запроса с контрактом RPC,
// который его обслуживает, и возвращает все расхождения.
//
// body — уже разобранный JSON-объект тела. Тело не-объект (массив/скаляр)
// сюда не передаётся: у Kachō такого биндинга нет, а если появится — он не
// имеет именованных ключей и проверять в нём нечего.
func analyzeRequestBody(method, path string, body map[string]any) []bodyFinding {
	b, ok := resolveHTTPBinding(method, path)
	if !ok {
		return []bodyFinding{{Kind: routeUnresolved, Method: method, Path: path}}
	}
	md, ok := bodyMessage(b)
	if !ok {
		return []bodyFinding{{Kind: bodyNotAccepted, Method: method, Path: path, FQN: b.fqn}}
	}
	var keys []string
	collectUnknownKeys(md, body, "", &keys)
	sort.Strings(keys)
	out := make([]bodyFinding, 0, len(keys))
	for _, k := range keys {
		out = append(out, bodyFinding{
			Kind:    keyUnknown,
			Method:  method,
			Path:    path,
			FQN:     b.fqn,
			Key:     k,
			Message: string(md.FullName()),
		})
	}
	return out
}

// bodyMessage возвращает message, в который край разбирает тело запроса.
//
//   - `body: "*"` — всё сообщение запроса;
//   - `body: "<поле>"` — только это поле (тогда ключи тела сверяются с ним, а
//     НЕ с сообщением запроса: иначе каждый такой биндинг дал бы ложные срабатывания);
//   - `body` не задан — тела нет, параметры приходят путём и query-строкой.
func bodyMessage(b httpBinding) (protoreflect.MessageDescriptor, bool) {
	switch b.body {
	case "":
		return nil, false
	case "*":
		return b.input, true
	default:
		fd := b.input.Fields().ByTextName(b.body)
		if fd == nil || fd.Message() == nil {
			return nil, false
		}
		return fd.Message(), true
	}
}

// collectUnknownKeys рекурсивно обходит JSON-объект и собирает ключи, которых
// нет в соответствующем message.
//
// Приём имени зеркалит protojson: сначала json_name (camelCase по умолчанию),
// затем оригинальное proto-имя — обе формы валидны на входе, поэтому обе
// считаются известными.
func collectUnknownKeys(md protoreflect.MessageDescriptor, obj map[string]any, prefix string, out *[]string) {
	fields := md.Fields()
	for k, v := range obj {
		fd := fields.ByJSONName(k)
		if fd == nil {
			fd = fields.ByTextName(k)
		}
		if fd == nil {
			*out = append(*out, prefix+k)
			continue
		}
		nested := nestedMessage(fd)
		if nested == nil {
			continue
		}
		descend(nested, fd, v, prefix+k, out)
	}
}

// descend спускается в значение поля-сообщения: map, список или одиночный объект.
func descend(nested protoreflect.MessageDescriptor, fd protoreflect.FieldDescriptor, v any, path string, out *[]string) {
	switch {
	case fd.IsMap():
		// Ключи map — произвольные строки, полями они не являются. Проверять
		// имеет смысл только значения, и только когда они — сообщения.
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		for mk, mv := range m {
			if child, ok := mv.(map[string]any); ok {
				collectUnknownKeys(nested, child, path+"["+mk+"].", out)
			}
		}
	case fd.IsList():
		list, ok := v.([]any)
		if !ok {
			return
		}
		for i, elem := range list {
			if child, ok := elem.(map[string]any); ok {
				collectUnknownKeys(nested, child, fmt.Sprintf("%s[%d].", path, i), out)
			}
		}
	default:
		if child, ok := v.(map[string]any); ok {
			collectUnknownKeys(nested, child, path+".", out)
		}
	}
}

// nestedMessage возвращает message, в который надо спуститься, либо nil, если
// спускаться некуда.
//
// Well-known-типы `google.protobuf.*` исключены намеренно: Struct/Value/Any
// принимают ПРОИЗВОЛЬНЫЕ ключи by design, а Timestamp/Duration/FieldMask и
// wrapper'ы на JSON — вообще скаляры, а не объекты. Спуск в них дал бы ложные
// находки на корректных телах.
func nestedMessage(fd protoreflect.FieldDescriptor) protoreflect.MessageDescriptor {
	if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
		return nil
	}
	var md protoreflect.MessageDescriptor
	if fd.IsMap() {
		vd := fd.MapValue()
		if vd.Kind() != protoreflect.MessageKind && vd.Kind() != protoreflect.GroupKind {
			return nil
		}
		md = vd.Message()
	} else {
		md = fd.Message()
	}
	if md == nil || strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return nil
	}
	return md
}

// resolveHTTPBinding находит биндинг, обслуживающий конкретный (метод, путь).
//
// Порядок перебора — most-specific-first, тот же принцип, что у роутера
// authz-middleware: биндинг с бо́льшим числом литеральных сегментов проверяется
// раньше, `{x=**}` — последним. Иначе catch-all-шаблон репозитория поглотил бы
// более специфичные под-ресурсы.
func resolveHTTPBinding(method, path string) (httpBinding, bool) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	best := -1
	bestScore := -1
	all := loadedHTTPBindings()
	for i, b := range all {
		if !bindingMatches(b, method, segs) {
			continue
		}
		if s := bindingSpecificity(b); s > bestScore {
			best, bestScore = i, s
		}
	}
	if best < 0 {
		return httpBinding{}, false
	}
	return all[best], true
}

// bindingMatches — тот же предикат, что у internalRoute.matches.
func bindingMatches(b httpBinding, method string, segs []string) bool {
	return internalRoute{method: b.method, segs: b.segs}.matches(method, segs)
}

// bindingSpecificity ранжирует шаблоны: больше литеральных сегментов —
// специфичнее; не-deep биндинг обходит deep-wildcard; при равенстве длиннее
// шаблон — специфичнее.
func bindingSpecificity(b httpBinding) int {
	literals, deep := 0, 0
	for _, s := range b.segs {
		switch {
		case s.rest:
			deep = 1
		case s.wild:
		default:
			literals++
		}
	}
	return literals*1000 + (1-deep)*100 + len(b.segs)
}
