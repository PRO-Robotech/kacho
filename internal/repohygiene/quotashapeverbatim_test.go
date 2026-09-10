// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	quotav1 "github.com/PRO-Robotech/corelib/api/kacho/cloud/quota/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

// Своя форма ответа службы доступа совпадает с платформенной ДОСЛОВНО.
//
// ПРЕДМЕТ — решение `Д9` (`kacho#2362`), часть 3: служба доступа объявляет форму
// ответа СВОЮ, «сохраняя имена и номера полей дословно — так, что провод и JSON
// не меняются». Это утверждение о проводе, и оно обязано держаться предикатом:
// провод кодирует поле НОМЕРОМ, а JSON — ИМЕНЕМ, поэтому переименование или
// перенумерация ломает всякого арендатора, уже читающего ответ, и ломает молча.
//
// СРАВНИВАЮТСЯ ДЕСКРИПТОРЫ, А НЕ ТЕКСТ. Разбор текста контракта сказал бы о
// написанном; дескриптор говорит о том, что поедет на провод.
//
// ПРЕДПОСЫЛКА НАЗВАНА, И ОНА ИСТЕКАЕТ САМА. Проверка сравнивает с платформенной
// формой, а развилка `kacho#2190` вправе снять пакет учёта платформы целиком.
// Тогда этот файл перестанет СОБИРАТЬСЯ — отказ громкий, в отличие от молчания,
// — и его снимут вместе с предметом, а не оставят вечно зелёным.
func TestIdentityQuotaShapeMatchesThePlatformShapeVerbatim(t *testing.T) {
	t.Parallel()

	own := (&iamv1.Quota{}).ProtoReflect().Descriptor()
	platform := (&quotav1.Quota{}).ProtoReflect().Descriptor()

	// --- поля: имя ↔ номер, в обе стороны -----------------------------------
	fieldsOf := func(d protoreflect.MessageDescriptor) map[string]int32 {
		out := map[string]int32{}
		for i := 0; i < d.Fields().Len(); i++ {
			f := d.Fields().Get(i)
			out[string(f.Name())] = int32(f.Number())
		}
		return out
	}
	ownFields, platformFields := fieldsOf(own), fieldsOf(platform)

	require.NotEmpty(t, ownFields,
		"у своей формы не прочитано НИ ОДНОГО поля — «расхождений нет» было бы "+
			"«ничего не сравнивали»")

	var findings []string
	for name, num := range platformFields {
		got, ok := ownFields[name]
		switch {
		case !ok:
			findings = append(findings, fmt.Sprintf(
				"поле %q есть у платформенной формы и отсутствует у своей — JSON арендатора "+
					"потерял бы ключ", name))
		case got != num:
			findings = append(findings, fmt.Sprintf(
				"поле %q: номер платформенный %d, свой %d — провод разошёлся", name, num, got))
		}
	}
	for name := range ownFields {
		if _, ok := platformFields[name]; !ok {
			findings = append(findings, fmt.Sprintf(
				"поле %q заведено сверх платформенной формы: переезд обязан быть переездом, "+
					"а не редизайном", name))
		}
	}

	// --- армы перечисления области: имя ↔ номер, в обе стороны ---------------
	armsOf := func(d protoreflect.MessageDescriptor) map[string]int32 {
		out := map[string]int32{}
		e := d.Enums().Get(0)
		for i := 0; i < e.Values().Len(); i++ {
			v := e.Values().Get(i)
			out[string(v.Name())] = int32(v.Number())
		}
		return out
	}
	require.Equal(t, 1, own.Enums().Len(),
		"своя форма обязана нести ровно одно вложенное перечисление — область значения")
	require.Equal(t, 1, platform.Enums().Len(),
		"платформенная форма обязана нести ровно одно вложенное перечисление")
	ownArms, platformArms := armsOf(own), armsOf(platform)

	require.NotEmpty(t, ownArms, "у своего перечисления не прочитано НИ ОДНОЙ армы")

	for name, num := range platformArms {
		got, ok := ownArms[name]
		switch {
		case !ok:
			findings = append(findings, fmt.Sprintf(
				"арма %q есть у платформенного перечисления и отсутствует у своего — "+
					"JSON кодирует арму ИМЕНЕМ", name))
		case got != num:
			findings = append(findings, fmt.Sprintf(
				"арма %q: номер платформенный %d, свой %d — провод разошёлся", name, num, got))
		}
	}
	for name := range ownArms {
		if _, ok := platformArms[name]; !ok {
			findings = append(findings, fmt.Sprintf(
				"арма %q заведена сверх платформенной формы", name))
		}
	}
	sort.Strings(findings)

	t.Logf("перепись: полей сверено %d (своих %d, платформенных %d); "+
		"арм сверено %d (своих %d, платформенных %d)",
		len(platformFields), len(ownFields), len(platformFields),
		len(platformArms), len(ownArms), len(platformArms))

	require.Empty(t, findings,
		"переезд объявления обязан сохранить провод и JSON дословно (kacho#2362, `Д9` п.3):\n%s",
		joinLines(findings))
}

func joinLines(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s + "\n"
	}
	return out
}
