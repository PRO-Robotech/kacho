// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// validationfamilyretired_test.go — гейт: семейство `kacho.cloud.validation` СНЯТО
// с контрактов и не вернулось (задача #1255, приёмка PROTO-1, полоса А).
//
// # ПРЕДМЕТ
//
// Девять расширений описателя — `required`, `pattern`, `value`, `size`, `length`,
// `unique`, `map_key`, `bytes` (на поле) и `exactly_one` (на группе `oneof`) —
// объявляли ограничения полей, у которых НЕ БЫЛО НИ ОДНОГО ИСПОЛНИТЕЛЯ на пути
// запроса. Поле принимало что угодно независимо от того, что о нём объявлено, а
// объявление при этом ВЫГЛЯДЕЛО гарантией. Хуже: на 45 из 45 полей `name`
// объявленный образец отвергал законный вход (имя с ведущей цифрой) и принимал
// незаконный (пустое имя — 39 объявлений). Семейство снято целиком.
//
// # ПОЧЕМУ ГЕЙТ, ЕСЛИ ФАЙЛ УДАЛЁН
//
// Удаление `validation.proto` уже держит свойство ПОСТРОЕНИЕМ: `[(required) = true]`
// без объявляющего файла не компилируется вовсе (полоса З). Этот гейт закрывает
// другое: возврат семейства ПОД ДРУГИМ ИМЕНЕМ, но с ТЕМИ ЖЕ НОМЕРАМИ расширений.
// Номер — то, что уезжает на провод; имя расширения на провод не уезжает никогда.
// Файл с иным именем пакета и теми же номерами прошёл бы `buf build` и вернул бы
// ровно снятое.
//
// # СУДИТСЯ ОПИСАТЕЛЬ, А НЕ ТЕКСТ, И СУДИТСЯ ПО НОМЕРУ
//
// Текстовый предикат `git grep -c '(required) = true'` на ревизии замера давал
// **318** при **316** объявлениях: две лишние строки — комментарии, объясняющие
// СНЯТОЕ объявление. То есть предикат по подстроке считал находкой собственное
// объяснение. Здесь читаются установленные расширения описателя.
//
// Ищутся ОБЕ формы, и это не перестраховка:
//
//   - расширение ИЗВЕСТНО прогону (тип зарегистрирован — так было ДО снятия, пока
//     пакет `pkg/api/kacho/cloud` существовал и линковался);
//   - расширение НЕИЗВЕСТНО (типа в наборе нет) — тогда оно лежит в неразобранных
//     байтах опций, и увидеть его можно ТОЛЬКО по номеру. После снятия семейства
//     вернуть его чужим файлом — это ровно вторая форма, и предикат, знающий лишь
//     первую, о ней сказал бы «находок ноль», ничего не прочитав.
//
// # ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ, ПУСТОЙ ОБХОД РОНЯЕТ ПРОГОН
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного» — ошибка, породившая
// две отвергнутые редакции приёмки этой задачи.
//
// # ЕДИНИЦА СЧЁТА НАЗВАНА
//
// Обход идёт ПО ФАЙЛАМ `kacho/`; вендоренные `google/**` в перепись не входят.
// От единицы зависит число: полный обход набора даёт другие величины, и сценарий,
// не назвавший единицу, покраснел бы у второго исполнителя по неверной причине.

// retiredFieldOption — номер расширения `google.protobuf.FieldOptions` снятого
// семейства → имя, каким оно значилось в `kacho/cloud/validation.proto`.
//
// Имя здесь — ТОЛЬКО для сообщения об отказе: судит гейт по номеру, потому что на
// провод уезжает номер. Возврат семейства под другим именем с теми же номерами —
// ровно тот случай, ради которого гейт заведён.
var retiredFieldOption = map[protowire.Number]string{
	101501: "required",
	101502: "pattern",
	101503: "value",
	101504: "size",
	101505: "length",
	101506: "unique",
	101510: "map_key",
	101511: "bytes",
}

// retiredOneofOption — то же для `google.protobuf.OneofOptions`.
var retiredOneofOption = map[protowire.Number]string{
	101400: "exactly_one",
}

type retiredOptionCensus struct {
	files    int
	messages int
	fields   int
	oneofs   int
	byOption map[string]int
}

func (c retiredOptionCensus) total() int {
	n := 0
	for _, v := range c.byOption {
		n += v
	}
	return n
}

// TestValidationFamilyIsRetiredFromTheContracts — полоса А-01/А-02 приёмки PROTO-1.
func TestValidationFamilyIsRetiredFromTheContracts(t *testing.T) {
	assertDescriptorSetCoversTheContractTree(t)

	var findings []string
	census := retiredOptionCensus{byOption: map[string]int{}}

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if len(string(fd.Path())) < 6 || string(fd.Path())[:6] != "kacho/" {
			return true
		}
		census.files++

		var walk func(ms protoreflect.MessageDescriptors)
		walk = func(ms protoreflect.MessageDescriptors) {
			for i := 0; i < ms.Len(); i++ {
				md := ms.Get(i)
				census.messages++

				fl := md.Fields()
				for j := 0; j < fl.Len(); j++ {
					f := fl.Get(j)
					census.fields++
					for _, opt := range setOptionNumbers(f.Options()) {
						name, retired := retiredFieldOption[opt]
						if !retired {
							continue
						}
						census.byOption[name]++
						findings = append(findings, fmt.Sprintf(
							"%s.%s — расширение %d (`%s` снятого семейства)",
							md.FullName(), f.Name(), opt, name))
					}
				}

				ol := md.Oneofs()
				for j := 0; j < ol.Len(); j++ {
					o := ol.Get(j)
					census.oneofs++
					for _, opt := range setOptionNumbers(o.Options()) {
						name, retired := retiredOneofOption[opt]
						if !retired {
							continue
						}
						census.byOption[name]++
						findings = append(findings, fmt.Sprintf(
							"%s.%s — расширение %d (`%s` снятого семейства)",
							md.FullName(), o.Name(), opt, name))
					}
				}

				walk(md.Messages())
			}
		}
		walk(fd.Messages())
		return true
	})

	if census.files == 0 || census.fields == 0 {
		t.Fatalf("обход ПУСТ (файлов %d, полей %d) — «находок ноль» здесь означало бы "+
			"«прочитано ноль». Проверьте провязку стабов контрактов в этот пакет",
			census.files, census.fields)
	}

	names := make([]string, 0, len(census.byOption))
	for n := range census.byOption {
		names = append(names, n)
	}
	sort.Strings(names)
	perOption := ""
	for _, n := range names {
		perOption += fmt.Sprintf(" %s=%d", n, census.byOption[n])
	}
	t.Logf("перепись (по файлам kacho/): файлов %d · сообщений %d · полей %d · групп oneof %d; "+
		"объявлений снятого семейства %d%s",
		census.files, census.messages, census.fields, census.oneofs, census.total(), perOption)

	if len(findings) > 0 {
		sort.Strings(findings)
		shown := findings
		if len(shown) > 25 {
			shown = shown[:25]
		}
		t.Fatalf("семейство ограничений полей ВЕРНУЛОСЬ в контракты: объявлений %d.\n"+
			"У него нет исполнителя на пути запроса, поэтому объявление не ограничивает "+
			"ничего и при этом выглядит гарантией (задача #1255, приёмка PROTO-1).\n"+
			"Первые %d:\n  %v",
			len(findings), len(shown), shown)
	}
}

// setOptionNumbers — номера расширений, УСТАНОВЛЕННЫХ на этих опциях, в обеих
// формах: известной прогону (тип зарегистрирован) и неизвестной (лежит в
// неразобранных байтах). Знать лишь первую значило бы не увидеть ровно тот
// возврат, ради которого гейт написан: чужой файл с теми же номерами.
func setOptionNumbers(opts proto.Message) []protowire.Number {
	if opts == nil {
		return nil
	}
	var out []protowire.Number
	m := opts.ProtoReflect()

	proto.RangeExtensions(opts, func(xt protoreflect.ExtensionType, _ any) bool {
		out = append(out, protowire.Number(xt.TypeDescriptor().Number()))
		return true
	})

	// Неразобранные байты — последовательность полей формата провода. Номер
	// читается из ключа; значение пропускается по своему виду.
	b := m.GetUnknown()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return out
		}
		b = b[n:]
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return out
		}
		b = b[n:]
		out = append(out, num)
	}
	return out
}
