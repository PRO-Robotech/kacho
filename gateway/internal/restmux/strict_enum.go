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
// ГРАНИЦА ПРАВКИ. Отбрасывание НИКОГДА НЕ СУЩЕСТВОВАВШЕГО ключа остаётся: на
// нём стоит клауза маски обновления («mask пустой → immutable из тела silently
// игнорируются»), им же живёт диагностика immutable-полей (тело доходит до
// хендлера и получает контракт-тон «<field> is immutable after <R>.Create»
// вместо безликого «unknown field»). Перепутать два этих «неизвестных» — и есть
// способ сломать конвенцию под видом строгости, поэтому граница проверяется
// тестами наравне с самим отказом.
//
// ИЗ НЕЁ ВЫВЕДЕН РОВНО ОДИН СЛУЧАЙ — имя, объявленное сообщением `reserved`
// (kacho#1628). Такое поле СУЩЕСТВОВАЛО и снято осознанно, поэтому отправитель
// помнит его рабочим, а контракт про снятый слот прямо обещает: «запрос со
// старым blockSize отвергается как неизвестное поле — а не принимается молча,
// оставляя отправителя в уверенности, что размер блока задан им». Обещание
// исполняется здесь. Случай узкий by construction: он опирается на объявление
// контракта, а не на отсутствие поля, поэтому опечатка и поле будущей версии
// под него не подпадают.
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
// ВТОРОЙ РАЗБОР СЫРОГО ТЕЛА ЗДЕСЬ НЕУСТРАНИМ, и это не небрежность. Обойти
// «уже разобранное сообщение» вместо повторного разбора текста нельзя by
// construction: разбор идёт с отбрасыванием неизвестного, поэтому имя значения,
// которого нет в словаре, к этому моменту УЖЕ ВЫБРОШЕНО — в сообщении на его
// месте ноль, неотличимый от «поле не задано». Именно этот выброшенный текст и
// есть предмет проверки. Цена — ещё одно полное представление тела в памяти;
// она ограничена не здесь, а на входе: middleware.HTTPMaxBodyBytes ставит
// потолок телу запроса ДО того, как оно доедет до разбора.
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
	var found bodyRefusals
	walkEnumValueNames(msg.ProtoReflect().Descriptor(), obj, "", &found)

	// СНЯТОЕ ПОЛЕ ДОКЛАДЫВАЕТСЯ ПЕРВЫМ. Тело, несущее снятый ключ, собрано по
	// прежней версии контракта; значения перечислений в нём — того же возраста,
	// и называть их «неизвестными» значило бы обвинять следствие вместо причины.
	if len(found.retired) > 0 {
		sort.Strings(found.retired) // порядок обхода карты в Go случаен
		return fmt.Errorf("field %s has been retired and is no longer accepted: remove it from the request",
			strings.Join(found.retired, "; "))
	}
	if len(found.enums) == 0 {
		return nil
	}
	sort.Slice(found.enums, func(i, j int) bool { return found.enums[i].Path < found.enums[j].Path })
	parts := make([]string, 0, len(found.enums))
	for _, ev := range found.enums {
		parts = append(parts, ev.String())
	}
	return fmt.Errorf("invalid value for enum field %s", strings.Join(parts, "; "))
}

// enumViolation — одно значение перечисления, которого в словаре поля нет.
//
// Хранится РАЗОБРАННЫМ, а не готовой строкой. Прежде обход отдавал уже
// собранный текст `<путь>: "<значение>"`, и соседний гейт разбирал его обратно
// своим `strings.LastIndex(": ")` — два места об одном формате, из которых
// достаточно поправить одно, чтобы разошлись оба. Формат собирается ровно там,
// где идёт наружу, — в [enumViolation.String].
type enumViolation struct {
	// Path — путь к полю в теле, в той же записи имён, что прислал клиент.
	Path string
	// Value — отвергнутое имя значения.
	Value string
	// Allowed — имена значений словаря В ПОРЯДКЕ ОБЪЯВЛЕНИЯ контракта.
	//
	// Перечень полный, включая нулевое значение: оно тоже принимается, и
	// вычеркнуть его значило бы подменить факт контракта нашим суждением о
	// полезности. Порядок — контрактный, а не алфавитный: нулевое значение
	// стоит первым там же, где оно объявлено.
	Allowed []string
}

// String — текст отказа по одному значению: что прислали и что принимается.
//
// Перечень допустимых здесь не украшение. Форму значения перечисления в этом
// дереве нельзя узнать заранее — часть словарей пишется без префикса типа,
// часть с полным, — а машинного описания API нет вовсе. Отказ, называющий
// только отвергнутое, оставляет отправителю перебор (kacho#1622).
func (e enumViolation) String() string {
	return fmt.Sprintf("%s: %q (allowed: %s)", e.Path, e.Value, strings.Join(e.Allowed, ", "))
}

// bodyRefusals — находки одного обхода тела, РАЗДЕЛЁННЫЕ ПО ПРЕДМЕТУ.
//
// Две разные беды с разными текстами отказа: значение перечисления, которого
// нет в словаре, и ключ поля, снятого с контракта. Складывать их в один список
// значило бы объявить второе «неверным значением перечисления».
type bodyRefusals struct {
	enums   []enumViolation
	retired []string
}

// foldFieldName приводит имя поля к форме, не зависящей от записи (`block_size`
// и `blockSize` дают одно и то же).
//
// Своего преобразователя camelCase↔snake_case здесь намеренно НЕ заводится:
// он был бы вторым местом об одном правиле рядом с protojson и разошёлся бы с
// ним молча на первом же имени с цифрой (`ipv4_cidr_primary`). Свёртка
// отвечает на более узкий вопрос — «одно ли это имя», — и для него достаточна.
//
// Столкнуть ДВА РАЗНЫХ имени свёртка не может во вред: живое поле резолвится
// дескриптором ДО этой проверки, поэтому под неё попадает лишь ключ, которому
// в сообщении не соответствует ничего.
func foldFieldName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == '_' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// retiredNames — свёрнутые имена, снятые сообщением с контракта (`reserved`).
//
// Считается по дескриптору при каждом обращении: сообщений в теле единицы, а
// кэш здесь был бы состоянием ради экономии, которой не измеряли.
func retiredNames(md protoreflect.MessageDescriptor) map[string]string {
	names := md.ReservedNames()
	if names.Len() == 0 {
		return nil
	}
	out := make(map[string]string, names.Len())
	for i := 0; i < names.Len(); i++ {
		n := string(names.Get(i))
		out[foldFieldName(n)] = n
	}
	return out
}

// walkEnumValueNames обходит разобранное тело против дескриптора сообщения.
//
// Приём имени ключа зеркалит protojson: сначала json_name (camelCase), затем
// оригинальное proto-имя.
//
// КЛЮЧ, КОТОРОМУ ПОЛЯ НЕТ, РАЗБИРАЕТСЯ НА ДВА СЛУЧАЯ, и это единственное, чем
// граница правки шире прежней:
//
//   - имя объявлено сообщением `reserved` — поле СУЩЕСТВОВАЛО и снято осознанно.
//     Отправитель помнит его рабочим, поэтому молчание оставляет его в
//     уверенности, что настройка задана им. Контракт прямо обещает обратное
//     («запрос со старым blockSize отвергается как неизвестное поле — а не
//     принимается молча»), и обещание исполняется здесь (kacho#1628);
//   - имя не встречалось никогда (опечатка, поле будущей версии) — ПРОПУСКАЕТСЯ
//     МОЛЧА, ровно как прежде. На этом стоит клауза пустой маски обновления
//     («mask пустой → immutable из тела silently игнорируются») и контракт-тон
//     диагностики immutable-полей: тело обязано доехать до хендлера, чтобы он
//     ответил «<field> is immutable after <R>.Create», а не безликим «unknown
//     field». Расширить отказ на весь этот случай значило бы сломать конвенцию
//     под видом строгости.
func walkEnumValueNames(md protoreflect.MessageDescriptor, obj map[string]any, prefix string, found *bodyRefusals) {
	// Well-known-типы принимают произвольные ключи и значения by design
	// (Struct/Value/Any), а Timestamp/Duration/FieldMask и wrapper'ы на JSON —
	// вообще скаляры. Спуск в них дал бы отказ на корректном теле.
	if strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return
	}
	fields := md.Fields()
	var retired map[string]string
	retiredLoaded := false
	for k, v := range obj {
		fd := fields.ByJSONName(k)
		if fd == nil {
			fd = fields.ByTextName(k)
		}
		if fd == nil {
			if !retiredLoaded {
				retired, retiredLoaded = retiredNames(md), true
			}
			if _, ok := retired[foldFieldName(k)]; ok {
				found.retired = append(found.retired, fmt.Sprintf("%q", prefix+k))
			}
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
				checkEnumValue(vd, mv, fmt.Sprintf("%s[%s]", path, mk), found)
			}
		case fd.IsList():
			list, ok := v.([]any)
			if !ok {
				continue
			}
			for i, elem := range list {
				checkEnumValue(fd, elem, fmt.Sprintf("%s[%d]", path, i), found)
			}
		default:
			checkEnumValue(fd, v, path, found)
		}
	}
}

// checkEnumValue проверяет ОДНО значение: имя перечисления сверяет со словарём,
// в сообщение спускается, всё прочее оставляет protojson.
func checkEnumValue(fd protoreflect.FieldDescriptor, v any, path string, found *bodyRefusals) {
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
		ed := fd.Enum()
		if ed.Values().ByName(protoreflect.Name(s)) != nil {
			return
		}
		vals := ed.Values()
		allowed := make([]string, 0, vals.Len())
		for i := 0; i < vals.Len(); i++ {
			allowed = append(allowed, string(vals.Get(i).Name()))
		}
		found.enums = append(found.enums, enumViolation{Path: path, Value: s, Allowed: allowed})
	case protoreflect.MessageKind, protoreflect.GroupKind:
		child, ok := v.(map[string]any)
		if !ok {
			return
		}
		if md := fd.Message(); md != nil {
			walkEnumValueNames(md, child, path+".", found)
		}
	}
}
