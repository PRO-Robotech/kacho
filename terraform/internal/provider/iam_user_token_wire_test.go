// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Форма разбора ответа края о личном токене судится ПРОИЗВОДИТЕЛЕМ этого ответа —
// дескриптором сообщения службы доступа, против которой провайдер собирается (пин в
// go.mod), — а не перечнем имён, выписанным здесь.
//
// # Свойство
//
// Каждый ключ, объявленный формой разбора, называет ЖИВОЕ поле сообщения: такое,
// которое в сообщении есть и контрактом не объявлено устаревшим.
//
//   - Устаревшее поле контракт уже снимает, и снимает по предикату у владельца.
//     Форма, читающая его, держит провайдер привязанным к снимаемому: в день снятия
//     поле молча опустеет, а разбор этого не заметит.
//   - Ключ, которого в сообщении нет вовсе, край не пришлёт никогда. Поле
//     разбирается в пустоту, и нестрогий разбор отсутствующее от пустого не отличит.
//
// # Законный близнец
//
// Тело, которое ПРОИЗВОДИТ маршалер контракта, — с каждым полем заполненным, в том
// числе устаревшими и теми, что форма не читает, и с выдачей пустых значений, как у
// публичной поверхности края, — разбирается формой без ошибки, и каждое объявленное
// поле получает значение. Поэтому ключ, снятый из формы, не ломает разбора ответа
// края, который продолжает присылать устаревшее поле, пока контракт его не снял.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
)

// wireContractPair — форма разбора и сообщение контракта, которое край ей отдаёт.
type wireContractPair struct {
	wire reflect.Type
	zero proto.Message
}

// userTokenWirePairs — формы разбора ответа о личном токене. Вложенный объект токена
// в ответе выпуска обходится по сообщению своего поля, а не отдельной строкой здесь.
func userTokenWirePairs() []wireContractPair {
	return []wireContractPair{
		{wire: reflect.TypeOf(userTokenWire{}), zero: &iamv1.UserOAuthClient{}},
		{wire: reflect.TypeOf(issuedUserTokenWire{}), zero: &iamv1.IssueUserTokenResponse{}},
	}
}

// wireFieldKey — ключ JSON, под которым encoding/json разбирает поле структуры.
// Пусто — поле разбором не читается.
func wireFieldKey(sf reflect.StructField) string {
	if !sf.IsExported() {
		return ""
	}
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name
	}
	return sf.Name
}

// fieldDeprecated — объявил ли контракт поле устаревшим.
func fieldDeprecated(fd protoreflect.FieldDescriptor) bool {
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	return ok && opts.GetDeprecated()
}

// nestedObject — тип объекта, который разбор ждёт в поле: структура, указатель на
// неё или список таких. nil — поле несёт не объект.
func nestedObject(t reflect.Type) (obj reflect.Type, list bool) {
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t, list = t.Elem(), true
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		return t, list
	}
	return nil, false
}

// wireAgainstContract обходит форму разбора по дескриптору сообщения. Возвращает
// число прочитанных полей формы (перепись) и находки.
//
// Корзины «прочее» нет: форма, которую обход сопоставить не умеет, — находка, а не
// пропуск, иначе её поля ушли бы с суда молча.
func wireAgainstContract(path string, wire reflect.Type, msg protoreflect.MessageDescriptor) (read int, findings []string) {
	fields := msg.Fields()
	for i := 0; i < wire.NumField(); i++ {
		sf := wire.Field(i)
		if sf.Anonymous {
			findings = append(findings, fmt.Sprintf(
				"%s.%s: встроенная структура — разбор поднимает её поля уровнем выше, и сопоставить их с %s этот обход не умеет",
				path, sf.Name, msg.FullName()))
			continue
		}
		key := wireFieldKey(sf)
		if key == "" {
			continue
		}
		read++
		fd := fields.ByJSONName(key)
		switch {
		case fd == nil:
			findings = append(findings, fmt.Sprintf(
				"%s.%s: ключ %q не называет ни одного поля %s — край его не пришлёт, и поле разбирается в пустоту молча",
				path, sf.Name, key, msg.FullName()))
		case fieldDeprecated(fd):
			findings = append(findings, fmt.Sprintf(
				"%s.%s: ключ %q читает поле %s, которое контракт объявил устаревшим и снимает",
				path, sf.Name, key, fd.FullName()))
		default:
			obj, list := nestedObject(sf.Type)
			if obj == nil {
				continue
			}
			if fd.Message() == nil || fd.IsMap() || fd.IsList() != list {
				findings = append(findings, fmt.Sprintf(
					"%s.%s: форма ждёт в ключе %q объект, а поле %s несёт не его",
					path, sf.Name, key, fd.FullName()))
				continue
			}
			n, nested := wireAgainstContract(path+"."+sf.Name, obj, fd.Message())
			read += n
			findings = append(findings, nested...)
		}
	}
	return read, findings
}

// TestUserTokenWireReadsOnlyLiveFieldsOfTheEdgeContract — форма разбора ответа о
// личном токене не объявляет ни устаревших, ни несуществующих полей контракта.
func TestUserTokenWireReadsOnlyLiveFieldsOfTheEdgeContract(t *testing.T) {
	pairs := userTokenWirePairs()
	var read int
	var findings []string
	for _, p := range pairs {
		md := p.zero.ProtoReflect().Descriptor()
		n, f := wireAgainstContract(p.wire.Name(), p.wire, md)
		if n == 0 {
			t.Fatalf("не выполнилось: форма %s не отдала обходу ни одного поля — судить нечего", p.wire.Name())
		}
		read += n
		findings = append(findings, f...)
	}
	t.Logf("осмотрено: форм %d, полей формы %d (с вложенными); находок %d", len(pairs), read, len(findings))
	for _, f := range findings {
		t.Error(f)
	}
}

// sampleValue — непустое значение для поля скалярного вида или перечисления.
func sampleValue(t *testing.T, fd protoreflect.FieldDescriptor) protoreflect.Value {
	t.Helper()
	switch fd.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString("v-" + string(fd.Name()))
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes([]byte(fd.Name()))
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(true)
	case protoreflect.EnumKind:
		values := fd.Enum().Values()
		return protoreflect.ValueOfEnum(values.Get(values.Len() - 1).Number())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(1)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(1)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(1)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(1)
	case protoreflect.FloatKind:
		return protoreflect.ValueOfFloat32(1)
	case protoreflect.DoubleKind:
		return protoreflect.ValueOfFloat64(1)
	default:
		t.Fatalf("близнец не умеет заполнить поле %s вида %s — тело без него было бы неполным", fd.FullName(), fd.Kind())
		return protoreflect.Value{}
	}
}

// fillEveryField заполняет КАЖДОЕ поле сообщения непустым значением — и устаревшие,
// и те, что форма разбора не читает: близнец несёт всё, что край может прислать.
func fillEveryField(t *testing.T, m protoreflect.Message) {
	t.Helper()
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if od := fd.ContainingOneof(); od != nil && m.WhichOneof(od) != nil {
			continue
		}
		switch {
		case fd.IsMap():
			mp := m.Mutable(fd).Map()
			key := sampleValue(t, fd.MapKey()).MapKey()
			if fd.MapValue().Message() != nil {
				v := mp.NewValue()
				fillEveryField(t, v.Message())
				mp.Set(key, v)
			} else {
				mp.Set(key, sampleValue(t, fd.MapValue()))
			}
		case fd.IsList():
			lst := m.Mutable(fd).List()
			if fd.Message() != nil {
				v := lst.NewElement()
				fillEveryField(t, v.Message())
				lst.Append(v)
			} else {
				lst.Append(sampleValue(t, fd))
			}
		case fd.Message() != nil:
			fillEveryField(t, m.Mutable(fd).Message())
		default:
			m.Set(fd, sampleValue(t, fd))
		}
	}
}

// TestUserTokenWireParsesTheWholeEdgeBody — законный близнец: полное тело края, в
// том числе с устаревшими полями и полями, которых форма не объявляет, разбирается
// без ошибки, и каждое объявленное поле получает значение.
func TestUserTokenWireParsesTheWholeEdgeBody(t *testing.T) {
	// Маршалер той же настройки, что у публичной поверхности края
	// (newPublicJSONPb, gateway/internal/restmux/strict_enum.go): имена полей —
	// JSON-имена контракта, пустые значения выдаются.
	marshal := protojson.MarshalOptions{EmitUnpopulated: true}
	for _, p := range userTokenWirePairs() {
		m := p.zero.ProtoReflect().New()
		fillEveryField(t, m)
		body, err := marshal.Marshal(m.Interface())
		if err != nil {
			t.Fatalf("%s: маршалер контракта не выдал тела: %v", p.wire.Name(), err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("%s: тело маршалера не объект: %v", p.wire.Name(), err)
		}
		declared := map[string]bool{}
		for i := 0; i < p.wire.NumField(); i++ {
			if key := wireFieldKey(p.wire.Field(i)); key != "" {
				declared[key] = true
			}
		}
		var undeclared []string
		for k := range raw {
			if !declared[k] {
				undeclared = append(undeclared, k)
			}
		}
		sort.Strings(undeclared)
		// Предпосылка близнеца: тело несёт то, чего форма не объявляет, и в том
		// числе КАЖДОЕ устаревшее поле. Без этого близнец зеленел бы на теле,
		// в котором нечего пропускать.
		if len(undeclared) == 0 {
			t.Fatalf("не выполнилось: тело %s не несёт ни одного ключа сверх объявленных формой — нестрогость разбора не проверена",
				p.zero.ProtoReflect().Descriptor().FullName())
		}
		md := p.zero.ProtoReflect().Descriptor()
		for i := 0; i < md.Fields().Len(); i++ {
			fd := md.Fields().Get(i)
			if _, ok := raw[fd.JSONName()]; fieldDeprecated(fd) && !ok {
				t.Errorf("%s: тело близнеца не несёт устаревшего поля %s", p.wire.Name(), fd.FullName())
			}
		}

		dst := reflect.New(p.wire)
		if err := json.Unmarshal(body, dst.Interface()); err != nil {
			t.Fatalf("%s: полное тело края не разобралось: %v", p.wire.Name(), err)
		}
		for i := 0; i < p.wire.NumField(); i++ {
			sf := p.wire.Field(i)
			if key := wireFieldKey(sf); key != "" && dst.Elem().Field(i).IsZero() {
				t.Errorf("%s.%s: ключ %q не получил значения из полного тела края", p.wire.Name(), sf.Name, key)
			}
		}
		t.Logf("%s: ключей в теле %d, форма объявляет %d, пропускает %d (%s); разбор без ошибки",
			p.wire.Name(), len(raw), len(declared), len(undeclared), strings.Join(undeclared, ", "))
	}
}

// TestWireAgainstContractInjection — обход доказан в обе стороны на сообщении,
// которое живёт независимо от предмета: у FileOptions фундамента protobuf есть
// устаревшее поле, и снятие устаревших полей службы доступа эту пробу не обесценит.
func TestWireAgainstContractInjection(t *testing.T) {
	md := (&descriptorpb.FileOptions{}).ProtoReflect().Descriptor()

	type live struct {
		JavaPackage string `json:"javaPackage"`
	}
	type deprecated struct {
		JavaPackage   string `json:"javaPackage"`
		EqualsAndHash bool   `json:"javaGenerateEqualsAndHash"`
	}
	type unknown struct {
		JavaPackage string `json:"javaPakage"`
	}
	type nestedUnknown struct {
		JavaPackage string `json:"javaPackage"`
		Features    *struct {
			FieldPresence string `json:"fieldPresense"`
		} `json:"features"`
	}
	type notAnObject struct {
		JavaPackage struct {
			Value string `json:"value"`
		} `json:"javaPackage"`
	}

	cases := []struct {
		name      string
		wire      reflect.Type
		wantRead  int
		wantFound []string // подстроки ЕДИНСТВЕННОЙ находки; пусто — молчание
	}{
		{"live twin is silent", reflect.TypeOf(live{}), 1, nil},
		{"deprecated key is found", reflect.TypeOf(deprecated{}), 2,
			[]string{`"javaGenerateEqualsAndHash"`, "java_generate_equals_and_hash", "устаревшим"}},
		{"unknown key is found", reflect.TypeOf(unknown{}), 1,
			[]string{`"javaPakage"`, "не называет"}},
		{"nested unknown key is found by its path", reflect.TypeOf(nestedUnknown{}), 3,
			[]string{"nestedUnknown.Features.FieldPresence", `"fieldPresense"`, "FeatureSet"}},
		{"object over a scalar is found", reflect.TypeOf(notAnObject{}), 1,
			[]string{`"javaPackage"`, "несёт не его"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			read, findings := wireAgainstContract(tc.wire.Name(), tc.wire, md)
			if read != tc.wantRead {
				t.Errorf("перепись: полей прочитано %d, ждали %d", read, tc.wantRead)
			}
			if tc.wantFound == nil {
				if len(findings) != 0 {
					t.Errorf("законный близнец не молчит: %v", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("ждали ровно одну находку, получили %d: %v", len(findings), findings)
			}
			for _, want := range tc.wantFound {
				if !strings.Contains(findings[0], want) {
					t.Errorf("находка не называет %s: %s", want, findings[0])
				}
			}
		})
	}
}
