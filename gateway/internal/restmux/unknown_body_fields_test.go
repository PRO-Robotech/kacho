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
//
// ВЫБОР ДЕТЕРМИНИРОВАН ПОЛНОСТЬЮ, и это не педантизм. Таблица собирается обходом
// глобального реестра, порядок которого от прогона к прогону РАЗНЫЙ; пока спор
// равных решался «кто встретился первым», вердикт читающих её гейтов менялся
// между прогонами на одном и том же дереве. Наблюдалось на паре
// `POST /vpc/v1/addressPools`, которую объявляют ДВА сервиса: три прогона подряд
// дали два разных сообщения ответа.
func resolveHTTPBinding(method, path string) (httpBinding, bool) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	best := -1
	all := loadedHTTPBindings()
	for i, b := range all {
		if !bindingMatches(b, method, segs) {
			continue
		}
		if best < 0 || moreSpecificBinding(b, all[best]) {
			best = i
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

// moreSpecificBinding — СТРОГИЙ порядок: специфичнее ли `a`, чем `b`.
//
// Оси перечислены по убыванию веса, и последняя (имя RPC) существует только
// затем, чтобы порядок был ПОЛНЫМ: два разных биндинга не имеют права оказаться
// равными, иначе выбор снова отдан порядку обхода реестра.
func moreSpecificBinding(a, b httpBinding) bool {
	if av, bv := bindingLiterals(a), bindingLiterals(b); av != bv {
		return av > bv
	}
	if av, bv := bindingConstrainedWilds(a), bindingConstrainedWilds(b); av != bv {
		return av > bv
	}
	if av, bv := bindingHasDeepWild(a), bindingHasDeepWild(b); av != bv {
		return !av // не-deep биндинг специфичнее deep-wildcard
	}
	if av, bv := len(a.segs), len(b.segs); av != bv {
		return av > bv
	}
	// Публичный биндинг обходит внутренний на ТОЙ ЖЕ паре (метод, шаблон).
	//
	// Это не тай-брейк «лишь бы детерминированно», а воспроизведение того, как
	// пару разводит сам край: снаружи обе ветки диспетчера ведут в публичный
	// мультиплексор, изнутри — во внутренний. Сосуществование объявлено
	// решением у `AddressPoolService`; страница арендатора документирует
	// ПУБЛИЧНУЮ поверхность, и сверять её надо с публичным контрактом.
	if a.internal != b.internal {
		return !a.internal
	}
	return a.fqn < b.fqn
}

func bindingLiterals(b httpBinding) int {
	n := 0
	for _, s := range b.segs {
		if !s.wild {
			n++
		}
	}
	return n
}

// bindingConstrainedWilds — число сужённых подстановок (`{id}:verb`, непустая
// приставка либо окончание).
//
// Ось заведена отдельно, потому что без неё спор
// `/instances/{instance_id}` против `/instances/{instance_id}:serialPortOutput`
// не решался ничем: литеральных сегментов и длины у них поровну, оба совпадают
// с одним путём. Следствие было не «иногда не тот биндинг», а находка не о том:
// пример собственного глагола сверялся с сообщением соседнего RPC.
func bindingConstrainedWilds(b httpBinding) int {
	n := 0
	for _, s := range b.segs {
		if s.wild && (s.prefix != "" || s.suffix != "") {
			n++
		}
	}
	return n
}

func bindingHasDeepWild(b httpBinding) bool {
	for _, s := range b.segs {
		if s.rest {
			return true
		}
	}
	return false
}
