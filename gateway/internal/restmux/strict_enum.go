// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// strict_enum.go — значение перечисления, которого в контракте нет, отвергается
// на краю; всё остальное разбирается как прежде.
//
// ПРЕДМЕТ. Тело разбирается protojson с отбрасыванием неизвестного, и слово
// «неизвестное» там шире, чем кажется по названию: под него попадает не только
// КЛЮЧ, которому в сообщении нет поля, но и ИМЯ ЗНАЧЕНИЯ перечисления
// (encoding/protojson/decode.go, unmarshalEnum: имя не найдено и отбрасывание
// включено ⇒ поле просто не выставляется). Сервис получает нулевое значение и
// отличить мусор от отсутствия НЕ МОЖЕТ — по проводу они одинаковы. Наблюдаемо
// это выглядело так: балансировщик создавался с умолчанием, а вызывающему
// отвечали успехом за настройку, которой сервер не делал. Такой исход конвенция
// прямо запрещает: поле запроса либо читается, либо отвергается явно, либо
// снимается с контракта (api-conventions.md, «принято-и-проигнорировано»).
//
// ГРАНИЦА ПРАВКИ — РОВНО ОДНА. Отбрасывание неизвестного КЛЮЧА остаётся: на нём
// стоит клауза маски обновления («mask пустой → immutable из тела silently
// игнорируются»), им же живёт диагностика immutable-полей (тело доходит до
// хендлера и получает контракт-тон «<field> is immutable after <R>.Create»
// вместо безликого «unknown field»). Перепутать два этих «неизвестных» — и есть
// способ сломать конвенцию под видом строгости, поэтому граница проверяется
// тестами наравне с самим отказом.
//
// ПОЧЕМУ ОТДЕЛЬНЫЙ ОБХОД, А НЕ ФЛАГ. У protojson один флаг на оба смысла:
// выключить его — значит отвергать и ключи. Разделить их можно только своим
// проходом по разобранному телу против дескриптора сообщения, что здесь и
// сделано: сначала штатный разбор (поведение не меняется), затем сверка имён
// значений перечислений.
//
// ЧИСЛОВАЯ ФОРМА НЕ СУЖАЕТСЯ. Перечисления proto3 открыты, и номер вне словаря —
// законная часть JSON-отображения. Сужать её значило бы менять контракт, а не
// чинить дефект; здесь проверяются только СТРОКОВЫЕ имена.

// newPublicJSONPb / newInternalJSONPb — базовые JSON-маршаллеры края. Различие
// ровно одно, `EmitUnpopulated`:
//   - public: true — отдаём явные нулевые значения (`""`/`{}`/`[]`/`null`) для
//     proto-полей. На публичной поверхности `description`/`labels`/`cidrBlocks`
//     и т.п. — полезный контракт, клиент должен видеть поле даже пустым.
//   - internal: false — на internal/admin-проекциях часть инфра-полей до
//     материализации пуста; пустые скрываем, чтобы админ видел только реально
//     заполненное.
//
// Обе строятся функциями (а не литералами по месту), чтобы тест собирал ТОТ ЖЕ
// маршаллер, что и боевой путь: проверка, настроенная иначе, чем прод, — форма
// без содержания.
func newPublicJSONPb() *runtime.JSONPb {
	return &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{
			UseProtoNames:   false,
			EmitUnpopulated: true,
		},
		UnmarshalOptions: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}
}

func newInternalJSONPb() *runtime.JSONPb {
	return &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{
			UseProtoNames:   false,
			EmitUnpopulated: false,
		},
		UnmarshalOptions: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}
}

// strictEnumMarshaler — обёртка над JSON-маршаллером края, добавляющая ровно
// одну проверку: имя значения перечисления обязано быть в словаре.
//
// Всё, кроме разбора тела (Marshal / NewEncoder / ContentType / Delimited),
// наследуется от вложенного JSONPb без изменений.
type strictEnumMarshaler struct{ *runtime.JSONPb }

func newStrictEnumMarshaler(j *runtime.JSONPb) *strictEnumMarshaler {
	return &strictEnumMarshaler{JSONPb: j}
}

// Unmarshal разбирает тело штатно, затем сверяет имена значений перечислений.
//
// Порядок важен: сначала protojson: если тело вообще не разбирается (сломанный
// JSON, неверный тип скаляра), отказ обязан прийти от него — своя проверка не
// должна подменять его сообщения.
func (m *strictEnumMarshaler) Unmarshal(data []byte, v any) error {
	if err := m.JSONPb.Unmarshal(data, v); err != nil {
		return err
	}
	return rejectUnknownEnumNames(data, v)
}

// NewDecoder — путь, которым тело разбирают СГЕНЕРИРОВАННЫЕ хендлеры
// (`marshaler.NewDecoder(req.Body).Decode(&protoReq)`). Проверять только
// Unmarshal значило бы оставить боевой путь непокрытым.
//
// io.EOF на пустом теле сохраняется: сгенерированный хендлер отличает «тела
// нет» от «тело плохое» ровно по нему (`err != nil && !errors.Is(err, io.EOF)`),
// и обёртка, потерявшая io.EOF, превратила бы каждый запрос без тела в 400.
func (m *strictEnumMarshaler) NewDecoder(r io.Reader) runtime.Decoder {
	dec := json.NewDecoder(r)
	return runtime.DecoderFunc(func(v any) error {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		return m.Unmarshal(raw, v)
	})
}

// rejectUnknownEnumNames сверяет строковые значения перечислений в теле со
// словарём соответствующего поля.
//
// Возвращает ошибку, которую сгенерированный хендлер превращает в
// `INVALID_ARGUMENT` (HTTP 400). Тон сообщения повторяет protojson
// («invalid value for enum field <json-имя>: "<значение>"»), чтобы отказ читался
// одинаково независимо от того, кто его вынес.
func rejectUnknownEnumNames(data []byte, v any) error {
	msg, ok := v.(proto.Message)
	if !ok {
		// Не-proto цель (край такие тела не биндит) — сверять не с чем.
		return nil
	}
	var body any
	if err := json.Unmarshal(data, &body); err != nil {
		// protojson это тело уже принял; расхождение здесь означало бы разные
		// разборы одного текста — не наш вопрос и не повод отказать.
		return nil
	}
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	var bad []string
	walkEnumValueNames(msg.ProtoReflect().Descriptor(), obj, "", &bad)
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad) // порядок обхода карты в Go случаен — отказ обязан быть стабильным
	return fmt.Errorf("invalid value for enum field %s", strings.Join(bad, "; "))
}

// walkEnumValueNames обходит разобранное тело против дескриптора сообщения.
//
// Приём имени ключа зеркалит protojson: сначала json_name (camelCase), затем
// оригинальное proto-имя. Ключ, которому поля нет, ПРОПУСКАЕТСЯ молча — это и
// есть та граница, за которую правка не заходит.
func walkEnumValueNames(md protoreflect.MessageDescriptor, obj map[string]any, prefix string, bad *[]string) {
	// Well-known-типы принимают произвольные ключи и значения by design
	// (Struct/Value/Any), а Timestamp/Duration/FieldMask и wrapper'ы на JSON —
	// вообще скаляры. Спуск в них дал бы отказ на корректном теле.
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
			continue
		}
		path := prefix + k
		switch {
		case fd.IsMap():
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			vd := fd.MapValue()
			for mk, mv := range m {
				checkEnumValue(vd, mv, fmt.Sprintf("%s[%s]", path, mk), bad)
			}
		case fd.IsList():
			list, ok := v.([]any)
			if !ok {
				continue
			}
			for i, elem := range list {
				checkEnumValue(fd, elem, fmt.Sprintf("%s[%d]", path, i), bad)
			}
		default:
			checkEnumValue(fd, v, path, bad)
		}
	}
}

// checkEnumValue проверяет ОДНО значение: имя перечисления сверяет со словарём,
// в сообщение спускается, всё прочее оставляет protojson.
func checkEnumValue(fd protoreflect.FieldDescriptor, v any, path string, bad *[]string) {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		s, ok := v.(string)
		if !ok {
			// Число — законная открытая форма; null и всё прочее уже решил
			// protojson своими правилами (и отверг, если они это запрещают).
			return
		}
		if s == "" {
			// ПУСТАЯ СТРОКА — «значение не выбрано», а не «значение, которого
			// нет». Это осознанное решение, а не забытый край:
			//
			//   - предмет правки — вызывающий, УТВЕРЖДАЮЩИЙ значение, которого
			//     не существует, и получающий за это `200`. Пустая строка не
			//     утверждает ничего: сервер оставляет поле незаданным, то есть
			//     делает ровно то, о чём его попросили;
			//   - так же читает пустое поле конвенция маски обновления;
			//   - формы (и всякий сборщик тела из полей ввода) кладут `""` в
			//     невыбранный необязательный список — отвергать это значило бы
			//     чинить один класс и ломать штатный путь оператора.
			//
			// Проверено по корпусу: единственное место, где `""` приезжает в
			// поле перечисления, — проба, чей предмет — отказ по ДРУГОЙ причине
			// (нераспознанный префикс идентификатора).
			return
		}
		if fd.Enum().Values().ByName(protoreflect.Name(s)) == nil {
			*bad = append(*bad, fmt.Sprintf("%s: %q", path, s))
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		child, ok := v.(map[string]any)
		if !ok {
			return
		}
		if md := fd.Message(); md != nil {
			walkEnumValueNames(md, child, path+".", bad)
		}
	}
}
