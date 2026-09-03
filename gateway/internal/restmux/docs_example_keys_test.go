// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// docs_example_keys_test.go — ГЕЙТ: каждый ключ примера на странице арендатора
// существует в том сообщении, телом которого пример объявлен.
//
// # Предмет
//
// Клиент читает пример как факт о ресурсе и пишет по нему разбор. Ключ, которого
// край не отдаёт, даёт ТИХИЙ промах: поле просто не приходит, отказа нет, и
// узнаётся это в чужой отладке. Тот же класс, что у соседнего гейта имён
// перечислений (`docs_enum_names_test.go`), только предмет не значение, а ключ.
//
// # Полоса примера ОБЪЯВЛЯЕТСЯ, а не угадывается по составу ключей
//
// Внутри одной `<ApiOperation>` законно живут ТРИ разных тела, и у каждого своё
// сообщение:
//
//	ответ           — сообщение ОТВЕТА операции (`httpBinding.output`);
//	ответ операции  — сообщение, названное САМИМ RPC в аннотации
//	                  `(kacho.cloud.api.operation).response`. У мутирующего
//	                  глагола ответ — конверт `Operation` (ban #9), а страница
//	                  показывает читателю то, ради чего он звал: полезную
//	                  нагрузку, которая приедет в `Operation.response`. Полоса
//	                  заведена, чтобы это можно было СКАЗАТЬ и ПРОВЕРИТЬ, а не
//	                  выдавать нагрузку за немедленный ответ;
//	запрос          — сообщение ЗАПРОСА либо то его поле, в которое разбирается
//	                  тело (`httpBinding.input` + `httpBinding.body`);
//	отказ           — `google.rpc.Status` (`code`/`message`/`details`).
//
// Различить их по СОСТАВУ КЛЮЧЕЙ нельзя, и попытка была бы ровно тем
// распознавателем, который молчит там, где предмет: тело отказа `{code,message}`
// неотличимо от ответа, у которого есть поля с такими именами, а тело запроса от
// ответа отличается лишь тем, каких полей в нём НЕ хватает. Поэтому полоса
// берётся из ОБЪЯВЛЕНИЯ — атрибута `title` у блока:
//
//	<CodeBlock language="json" title="Ответ">          → ответ (явно)
//	<CodeBlock language="json" title="Ответ операции"> → полезная нагрузка операции
//	<CodeBlock language="json" title="Тело запроса">   → запрос
//	<CodeBlock language="json" title="Отказ">          → отказ
//	<CodeBlock language="json">                        → ответ (умолчание)
//
// Умолчание названо здесь, а не подразумевается: пример внутри `<ApiOperation>`
// — это тело ответа этой операции, ради него обёртка и заведена. Требовать
// `title="Ответ"` от каждого из ста с лишним таких блоков значило бы написать
// одно и то же слово сто раз и ничего не сообщить читателю; объявляются
// ОТСТУПЛЕНИЯ — те два случая, которые читатель иначе не отличит от ответа.
// Перепись печатает объявленное и умолчание РАЗНЫМИ числами, чтобы «объявлено»
// не читалось шире, чем есть.
//
// Заголовок, которого нет в словаре полос, — НАХОДКА, а не третий вид умолчания:
// иначе опечатка в заголовке молча меняла бы полосу, и гейт судил бы тело запроса
// против сообщения ответа.
//
// # Граница, названная прямо
//
// Пример запроса, забывший объявиться и состоящий ТОЛЬКО из ключей, которые
// есть и в ответе, судится как ответ и проходит. Находки под предикатом этого
// гейта («ключ не существует в своём сообщении») здесь нет — все ключи
// существуют, — но в переписи такой пример стоит в полосе ответа. Это цена
// умолчания, и она названа, чтобы её не приняли за отсутствие класса.
//
// Проза страницы гейтом не судится: слово в тексте не привязано ни к полю, ни к
// сообщению. Согласие прозы с примерами держится обзором.

// docsExampleLane — объявленная полоса примера.
type docsExampleLane int

const (
	docsLaneResponse docsExampleLane = iota
	docsLaneOperationResponse
	docsLaneRequest
	docsLaneRefusal
)

func (l docsExampleLane) String() string {
	switch l {
	case docsLaneOperationResponse:
		return "ответ операции"
	case docsLaneRequest:
		return "запрос"
	case docsLaneRefusal:
		return "отказ"
	default:
		return "ответ"
	}
}

// docsAllLanes — полосы в порядке объявления, для переписи.
func docsAllLanes() []docsExampleLane {
	return []docsExampleLane{docsLaneResponse, docsLaneOperationResponse, docsLaneRequest, docsLaneRefusal}
}

// docsLaneByTitle — СЛОВАРЬ полос: заголовок блока → полоса.
//
// Закрытый и объявленный здесь один раз. Заголовок вне словаря отвергается
// (см. шапку), поэтому расширение словаря — осознанный шаг, а не побочный
// эффект правки страницы.
var docsLaneByTitle = map[string]docsExampleLane{
	"Ответ":          docsLaneResponse,
	"Ответ операции": docsLaneOperationResponse,
	"Тело запроса":   docsLaneRequest,
	"Отказ":          docsLaneRefusal,
}

// docsLaneTitles — словарь в порядке объявления, для текста отказа.
func docsLaneTitles() []string {
	out := make([]string, 0, len(docsLaneByTitle))
	for k := range docsLaneByTitle {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// docsKeyFinding — один ключ примера, которого в его сообщении нет.
type docsKeyFinding struct {
	Example docsJSONExample
	Lane    docsExampleLane
	FQN     string
	Message protoreflect.FullName
	Key     string
}

func (f docsKeyFinding) String() string {
	return fmt.Sprintf("%s:%d [%s %s · полоса «%s» → %s] ключ %q сообщением не объявлен",
		f.Example.File, f.Example.Line, f.Example.Method, f.Example.Endpoint,
		f.Lane, f.Message, f.Key)
}

// docsExampleLaneOf — полоса примера по его объявлению.
//
// Второе возвращаемое — распознан ли заголовок. Ложь означает заголовок вне
// словаря, и это находка, а не полоса по умолчанию.
func docsExampleLaneOf(ex docsJSONExample) (docsExampleLane, bool) {
	if ex.Title == "" {
		return docsLaneResponse, true
	}
	lane, ok := docsLaneByTitle[ex.Title]
	return lane, ok
}

// docsLaneMessage — сообщение, против которого судится пример этой полосы.
//
// Для запроса берётся не только сообщение запроса, но и клауза `body` биндинга:
// при `body: "<поле>"` телом является ПОД-СООБЩЕНИЕ, а не весь запрос, и сверка
// с целым дала бы молчание на ключах, которых в теле быть не может.
func docsLaneMessage(b httpBinding, lane docsExampleLane) (protoreflect.MessageDescriptor, string) {
	switch lane {
	case docsLaneRefusal:
		md, err := protoregistry.GlobalFiles.FindDescriptorByName("google.rpc.Status")
		if err != nil {
			return nil, "google.rpc.Status не в реестре типов этого бинаря"
		}
		msg, ok := md.(protoreflect.MessageDescriptor)
		if !ok {
			return nil, "google.rpc.Status разрешился не сообщением"
		}
		return msg, ""
	case docsLaneOperationResponse:
		if b.operationResponse == nil {
			return nil, "RPC не объявляет `(kacho.cloud.api.operation).response` — " +
				"полосы «ответ операции» у него нет; если ответ приходит сразу, полоса здесь «Ответ»"
		}
		return b.operationResponse, ""
	case docsLaneRequest:
		if b.input == nil {
			return nil, "у биндинга нет сообщения запроса"
		}
		switch b.body {
		case "", "*":
			return b.input, ""
		default:
			fd := b.input.Fields().ByTextName(b.body)
			if fd == nil || fd.Kind() != protoreflect.MessageKind {
				return nil, fmt.Sprintf("клауза body: %q не резолвится полем-сообщением запроса", b.body)
			}
			return fd.Message(), ""
		}
	default:
		if b.output == nil {
			return nil, "у биндинга нет сообщения ответа"
		}
		return b.output, ""
	}
}

// docsUnresolvedKeys обходит разобранное тело против дескриптора и возвращает
// ключи, которым в сообщении не соответствует ни одно поле.
//
// Приём имени зеркалит protojson: сначала json_name (camelCase), затем
// оригинальное proto-имя, — то же, что делает `walkEnumValueNames` соседнего
// гейта. Своего преобразователя записи здесь не заводится: он был бы вторым
// местом об одном правиле.
//
// Well-known-типы (`Any`, `Struct`, `Value`, `Timestamp`, …) принимают
// произвольные ключи by design, поэтому спуск в них прекращается: `metadata`
// операции — это `Any`, и её содержимое сообщением не описано.
func docsUnresolvedKeys(md protoreflect.MessageDescriptor, obj map[string]any, prefix string, out *[]string) {
	if strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return
	}
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
		path := prefix + k
		switch {
		case fd.IsMap():
			// Ключи карты задаёт отправитель — сообщением они не объявлены и
			// объявлены быть не могут. Спуск идёт в ЗНАЧЕНИЕ, если оно
			// сообщение.
			vd := fd.MapValue()
			if vd.Kind() != protoreflect.MessageKind && vd.Kind() != protoreflect.GroupKind {
				continue
			}
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			for mk, mv := range m {
				sub, ok := mv.(map[string]any)
				if !ok {
					continue
				}
				docsUnresolvedKeys(vd.Message(), sub, fmt.Sprintf("%s[%s].", path, mk), out)
			}
		case fd.IsList():
			if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
				continue
			}
			list, ok := v.([]any)
			if !ok {
				continue
			}
			for i, elem := range list {
				sub, ok := elem.(map[string]any)
				if !ok {
					continue
				}
				docsUnresolvedKeys(fd.Message(), sub, fmt.Sprintf("%s[%d].", path, i), out)
			}
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			sub, ok := v.(map[string]any)
			if !ok {
				continue
			}
			docsUnresolvedKeys(fd.Message(), sub, path+".", out)
		}
	}
}

// docsExampleKeyFindings — суждение по ОДНОМУ примеру.
//
// Вынесено отдельной функцией не ради красоты: ею правится проба инъекции, и
// правится ТА ЖЕ, что исполняется обходом дерева.
//
// Возвращает находки и признак того, что пример БЫЛ обойдён. Ложь означает
// «сверять не с чем» (фрагмент, не-объект, полоса без сообщения), и это
// отдельная величина переписи, а не чистота.
func docsExampleKeyFindings(b httpBinding, lane docsExampleLane, ex docsJSONExample) ([]docsKeyFinding, bool) {
	md, why := docsLaneMessage(b, lane)
	if why != "" {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(ex.Body), &obj); err != nil || obj == nil {
		return nil, false
	}
	var keys []string
	docsUnresolvedKeys(md, obj, "", &keys)
	sort.Strings(keys)
	out := make([]docsKeyFinding, 0, len(keys))
	for _, k := range keys {
		out = append(out, docsKeyFinding{
			Example: ex, Lane: lane, FQN: b.fqn, Message: md.FullName(), Key: k,
		})
	}
	return out, true
}

func TestTenantDocExampleKeysExistInTheirMessage(t *testing.T) {
	root := repoRoot(t)
	pages := docsContentPages(t, root)
	if len(pages) == 0 {
		t.Fatal("страниц арендатора не найдено: гейту нечего сверять, и это отказ, а не проход")
	}

	var findings []docsKeyFinding
	var unjudgeable []string
	examples, outsideOperation, unbound, unparsed := 0, 0, 0, 0
	byLane := map[docsExampleLane]int{}
	declared, defaulted := 0, 0

	for _, page := range pages {
		body, err := osReadFile(page)
		if err != nil {
			t.Fatalf("чтение %s: %v", page, err)
		}
		rel := mustRel(root, page)
		for _, ex := range extractDocsJSONExamples(rel, string(body)) {
			examples++
			if ex.Method == "" || ex.Endpoint == "" {
				outsideOperation++
				continue
			}
			lane, known := docsExampleLaneOf(ex)
			if !known {
				unjudgeable = append(unjudgeable, fmt.Sprintf(
					"  %s:%d [%s %s] заголовок %q вне словаря полос (объявлены: %s)",
					ex.File, ex.Line, ex.Method, ex.Endpoint, ex.Title,
					strings.Join(docsLaneTitles(), ", ")))
				continue
			}
			b, ok := resolveHTTPBinding(ex.Method, docsPathParam.ReplaceAllString(ex.Endpoint, "x"))
			if !ok {
				unbound++
				continue
			}
			if ex.Title == "" {
				defaulted++
			} else {
				declared++
			}
			if _, why := docsLaneMessage(b, lane); why != "" {
				unjudgeable = append(unjudgeable, fmt.Sprintf(
					"  %s:%d [%s %s · полоса «%s»] %s",
					ex.File, ex.Line, ex.Method, ex.Endpoint, lane, why))
				continue
			}
			got, ok := docsExampleKeyFindings(b, lane, ex)
			if !ok {
				// Фрагмент с многоточием или не-объект: сверять не с чем.
				// Считается отдельно — «не обойдено» не растворяется в
				// «обойдено и чисто».
				unparsed++
				continue
			}
			byLane[lane]++
			findings = append(findings, got...)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Example.File != findings[j].Example.File {
			return findings[i].Example.File < findings[j].Example.File
		}
		if findings[i].Example.Line != findings[j].Example.Line {
			return findings[i].Example.Line < findings[j].Example.Line
		}
		return findings[i].Key < findings[j].Key
	})

	walked := 0
	lanes := make([]string, 0, len(docsAllLanes()))
	for _, l := range docsAllLanes() {
		walked += byLane[l]
		lanes = append(lanes, fmt.Sprintf("«%s» %d", l, byLane[l]))
	}
	t.Logf("страниц осмотрено %d; примеров JSON %d = вне операции %d + внутри %d; "+
		"из внутренних: без биндинга %d, полоса не рассудима %d, неразбираемых/не-объектов %d, обойдено %d; "+
		"по полосам — %s; полоса объявлена заголовком %d, взята умолчанием %d; "+
		"ключей вне своего сообщения %d",
		len(pages), examples, outsideOperation, examples-outsideOperation,
		unbound, len(unjudgeable), unparsed, walked,
		strings.Join(lanes, ", "), declared, defaulted, len(findings))

	if walked == 0 {
		t.Fatal("ни один пример не обойдён: вердикт беспредметен, и его молчание не есть согласие")
	}

	var b strings.Builder
	if len(unjudgeable) > 0 {
		sort.Strings(unjudgeable)
		fmt.Fprintf(&b, "%d пример(ов) объявляют полосу, которую судить не с чем:\n%s\n\n",
			len(unjudgeable), strings.Join(unjudgeable, "\n"))
	}
	if len(findings) > 0 {
		fmt.Fprintf(&b, "%d ключ(ей) примеров не объявлены сообщением своей полосы — "+
			"клиент пишет по ним разбор и не получает поля, отказа при этом нет:\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		fmt.Fprintf(&b, "\nЕсли пример — не немедленный ответ, объявите полосу заголовком блока:\n"+
			"  <CodeBlock language=\"json\" title=\"Ответ операции\">  — нагрузка, приезжающая в Operation.response\n"+
			"  <CodeBlock language=\"json\" title=\"Тело запроса\">    — тело, которое ОТПРАВЛЯЮТ\n"+
			"  <CodeBlock language=\"json\" title=\"Отказ\">           — google.rpc.Status\n")
	}
	if b.Len() > 0 {
		t.Fatal(b.String())
	}
}
