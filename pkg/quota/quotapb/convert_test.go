// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package quotapb_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotapb"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
)

// Ответ арендатору не изменился НИ НА БАЙТ, когда перечисление области перестало
// заимствоваться у контракта выносимой службы (задача #2117, стадия S2).
//
// # Почему утверждается ПРОВОД, а не тип
//
// Тип поля сменился, и это объявленный разрыв: собирающий стабы платформы
// пересоберётся. Арендатор стабов не собирает — он читает JSON, а JSON кодирует
// перечисление ИМЕНЕМ ветви. Значит вопрос «заметил ли арендатор» решается
// именами и номерами, а не именем типа, и проверяться обязан именно он.
//
// # Почему дословно, а не «содержит»
//
// Утверждение «в ответе есть слово PROJECT» зеленеет и на ответе, где ветвь
// переименована, а старое имя осталось в соседнем поле. Здесь закреплён ПОЛНЫЙ
// текст: переименование ветви, смена номера и смена имени поля роняют пробу
// каждое по отдельности и называют себя в разнице.
func TestTenantFacingScopeStaysByteIdenticalOnTheWire(t *testing.T) {
	t.Parallel()

	// Порядок ветвей — от старшей к младшей, как в контракте. Проверяются ВСЕ
	// четыре: три законные области плюс «источник не назван», в которое
	// отображается непрочитанная строка.
	cases := []struct {
		stored string
		want   string
		number int32
	}{
		{stored: "PROJECT", want: "PROJECT", number: 3},
		{stored: "ACCOUNT", want: "ACCOUNT", number: 2},
		{stored: "DEFAULT", want: "DEFAULT", number: 1},
		{stored: "нечитаемое", want: "SCOPE_UNSPECIFIED", number: 0},
	}

	for _, c := range cases {
		t.Run(c.stored, func(t *testing.T) {
			got := quotapb.Scope(c.stored)
			require.Equalf(t, c.want, got.String(),
				"имя ветви — то, что видит арендатор в JSON: смена имени есть ломающее "+
					"изменение для каждого, кто уже читает поле")
			require.EqualValues(t, c.number, got,
				"номер ветви — то, что идёт по проводу: смена номера ломает читающих "+
					"двоичный ответ, и не видна в JSON вовсе")
		})
	}
}

// Полный ответ о ЕДИНСТВЕННОЙ строке учёта закреплён дословно.
//
// Проба закрывает не только область: имена полей ответа (`camelCase` на крае)
// тоже часть контракта, и перевод их не касается — но именно поэтому их смену
// не заметил бы никто, кроме такой пробы.
func TestQuotasRenderTheSameJSONTheTenantAlreadyReads(t *testing.T) {
	t.Parallel()

	out := quotapb.Quotas([]quotaread.State{{
		Kind:          "vpc.subnet",
		Limit:         16,
		Used:          2,
		SourceScope:   "PROJECT",
		SourceScopeID: "prj-mine",
		CarrierType:   "project",
		CarrierID:     "prj-mine",
	}})
	require.Len(t, out, 1)

	// Пробелов нет намеренно: сравнивается ТЕКСТ, а не разобранное дерево —
	// разбор простил бы и переупорядочивание, и смену имени поля на синоним.
	raw, err := protojson.MarshalOptions{}.Marshal(out[0])
	require.NoError(t, err)

	const want = `{"kind":"vpc.subnet","limit":"16","used":"2","sourceScope":"PROJECT",` +
		`"sourceScopeId":"prj-mine","carrierType":"project","carrierId":"prj-mine"}`
	require.Equalf(t, want, stripJSONSpaces(string(raw)),
		"ответ арендателю изменился. Это не «поправьте пробу»: поле, имя ветви и "+
			"номер здесь суть контракт, и менять их можно только объявленным разрывом")
}

// stripJSONSpaces убирает пробелы, которые `protojson` вставляет НЕДЕТЕРМИНИРОВАННО
// (он делает это намеренно, чтобы на его вывод не полагались побайтово).
//
// Убираются только пробелы ВНЕ строковых значений: иначе проба перестала бы
// замечать пробел, появившийся внутри значения, — то есть ослабла бы ровно там,
// где обязана быть точной.
func stripJSONSpaces(s string) string {
	var b []byte
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ' ' || c == '\n' || c == '\t' || c == '\r'):
			continue
		}
		b = append(b, c)
	}
	return string(b)
}
