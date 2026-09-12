// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package quotapb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/PRO-Robotech/corelib/quota/quotaread"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotapb"
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

// ОБЪЯВЛЕННОЕ ОТСУТСТВИЕ ДОМЕНА ВЕЛИЧИН — НЕ СБОЙ ПЛАТФОРМЫ (#2515).
//
// # Предмет
//
// Ручка домена величин принимает два законных значения: адрес соседа либо слово
// «домена величин в этой установке нет». Второе — ОБЪЯВЛЕННЫЙ ВЫБОР ОПЕРАТОРА, и
// путь запроса ему уже подчиняется: считаемые виды списываются, но не
// отвергаются никогда (миграция `…_quota_absent_authority_charges_unconditionally`).
//
// Витрина об этом выборе не знала. Полоса чтения собирается только под
// развёрнутый домен, поэтому на объявленном отсутствии владелец оставлял её
// несобранной — а следом не строил обработчика и НЕ РЕГИСТРИРОВАЛ метод вовсе,
// так что арендатор получал `UNIMPLEMENTED`, «такой возможности в этой сборке
// нет». Здесь, в общем теле, то же состояние приезжало как `INTERNAL` «quota
// read band is not wired»: оба ответа утверждают поломку на посадке, которую
// оператор выбрал сам.
//
// Класс — «недоступен ≠ не развёрнут» (`polyrepo.md`): два состояния, у которых
// разное следствие для арендатора, имели одно представление (`states == nil`) и
// один ответ. Хуже того, ответом было ровно то, ради устранения чего витрина и
// заведена: её собственная шапка говорит, что отказ по пределу без витрины
// «неотличим для арендатора от сбоя платформы».
//
// # Что утверждается — ПАРА, а не одна сторона
//
// Отрицание («не INTERNAL») зеленело бы на любом отказе, поэтому рядом стоит
// положительный близнец, отличающийся РОВНО ОДНИМ фактом — посадкой. При
// объявленном отсутствии ответ обязан назвать посадку; при объявленном адресе
// несобранная полоса обязана остаться `INTERNAL`, потому что там это и есть
// настоящая поломка провязки.
func TestDeclaredAbsentAuthorityIsNotReportedAsAPlatformFailure(t *testing.T) {
	t.Parallel()

	const projectID = "prj-1"

	t.Run("объявленное отсутствие: посадка названа, а не выдана за поломку", func(t *testing.T) {
		t.Parallel()

		_, err := quotapb.ListQuotas(
			context.Background(), projectID, nil, quotaread.AbsentAuthority("vpc"))
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok, "отказ обязан быть статусом gRPC: арендатор читает код, а не текст")
		require.Equal(t, codes.FailedPrecondition, st.Code(),
			"INTERNAL здесь утверждает поломку платформы на посадке, которую выбрал "+
				"оператор; предусловие не выполнено — потолков в этой установке нет")

		// Признак — то, ПО ЧЕМУ клиент различает полосы машинно: проза меняется
		// осознанно, и разбор её вернул бы пустоту при первой же правке тона.
		var info *errdetails.ErrorInfo
		for _, d := range st.Details() {
			if ei, is := d.(*errdetails.ErrorInfo); is {
				info = ei
			}
		}
		require.NotNil(t, info, "полоса без машинного признака неотличима от соседней: "+
			"«потолок не назван» лечится действием администратора, а эта полоса — ничем")
		require.Equal(t, "QUOTA_AUTHORITY_ABSENT", info.GetReason())
		require.Equal(t, "vpc.kacho.cloud", info.GetDomain(),
			"источник ответа называется доменом сервиса, иначе разбирающий не знает, кто отказал")
	})

	t.Run("положительный близнец: домен объявлен адресом, полоса не собрана — это поломка", func(t *testing.T) {
		t.Parallel()

		_, err := quotapb.ListQuotas(
			context.Background(), projectID, nil, quotaread.AuthorityDeclared())
		require.Error(t, err)

		st, _ := status.FromError(err)
		require.Equal(t, codes.Internal, st.Code(),
			"непровязанная полоса при объявленном адресе — настоящий дефект сборки, "+
				"и смягчать его посадкой нельзя: тогда поломка провязки стала бы невидимой")
		require.Equal(t, "quota read band is not wired", st.Message())
	})

	t.Run("положительный близнец: собранная полоса отвечает строками, посадка не мешает", func(t *testing.T) {
		t.Parallel()

		states := func(context.Context, string) ([]quotaread.State, error) {
			return []quotaread.State{{
				Kind: "vpc.network", Limit: 5, Used: 2,
				SourceScope: "PROJECT", SourceScopeID: projectID,
				CarrierType: "project", CarrierID: projectID,
			}}, nil
		}

		out, err := quotapb.ListQuotas(
			context.Background(), projectID, states, quotaread.AuthorityDeclared())
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, "vpc.network", out[0].GetKind())
	})

	t.Run("неверный ввод судится РАНЬШЕ посадки", func(t *testing.T) {
		t.Parallel()

		// Иначе арендатор, не назвавший проект, получал бы на объявленном
		// отсутствии рассказ о посадке вместо указания на свою ошибку.
		_, err := quotapb.ListQuotas(
			context.Background(), "", nil, quotaread.AbsentAuthority("vpc"))
		st, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, st.Code())
		require.Equal(t, "project_id: required", st.Message())
	})
}
