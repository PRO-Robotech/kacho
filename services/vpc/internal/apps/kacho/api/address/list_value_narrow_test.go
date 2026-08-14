// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Сужение списка по ЗНАЧЕНИЮ адреса — форма значения проверяется, а не уносится в SQL.
//
// Предмет. `ip_address` — замена снятому поиску по значению, и он отвечает на вопрос
// «чей это адрес?». Строка, которая адресом быть не может, ответа на этот вопрос не
// имеет НИ ПРИ КАКИХ данных: ни одна строка хранилища не несёт значения `не-адрес`.
// Пустая страница с кодом 200 на такой ввод — утверждение «такого адреса ни у кого
// нет», то есть ложное утверждение об отсутствии в ответ на запрос, у которого нет
// предмета. Вызывающий при этом не отличает «вы прислали мусор» от «значение никому
// не принадлежит» — а различать их и есть назначение поля.
//
// Почему это ТОТ ЖЕ класс, что уже закрыт у соседнего поля. `subnet_id` того же
// запроса проверяется по форме (`list_subnet_narrow_test.go`), и его обоснование в
// коде сформулировано дословно так же: «пустая страница утверждала бы „в этой подсети
// адресов нет“ про строку, которая подсетью быть не может». Оба поля — сужения одного
// списка, оба landed одной волной как замена снятым методам; проверку получило одно.
// Это и есть «починили там, где заметили»: класс закрыт у того поля, на котором его
// увидели.
//
// Отдельно — почему замена обязана нести этот запрет, а не унаследовать послабление.
// Снятый метод сняли в том числе за ложное «не найдено» на запрос, ответа на который
// у него не было (см. `declared-breaks.yaml`, `GetByValue`). Замена, отвечающая пустой
// страницей на невозможное значение, воспроизводит ровно то, ради чего метод сняли.
//
// Порядок утверждается тоже: проверка формата стоит ДО замыкания по личности
// вызывающего — иначе ответ на некорректный ввод зависел бы от того, что вызывающему
// выдано (тот же класс, что `list_pagination_order_test.go`).
func TestListNarrowByValue_MalformedValueRejectedBeforeIdentityShortCircuit(t *testing.T) {
	const malformed = "not-an-ip"

	t.Run("названный вызывающий — отказ по формату", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", IPAddress: malformed}, Pagination{PageSize: 50})

		require.Error(t, err,
			"пустая страница вместо отказа — продукт утверждает «такого адреса ни у кого нет» "+
				"про строку, которая адресом быть не может")
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, "Illegal argument ip_address", status.Convert(err).Message(),
			"текст отказа — часть контракта; он называет ПОЛЕ, из-за которого произошёл")
	})

	t.Run("вызывающий не опознан — тот же отказ по формату", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(context.Background(),
			AddressFilter{ProjectID: "prj_1", IPAddress: malformed}, Pagination{PageSize: 50})

		require.Error(t, err,
			"пустая страница вместо отказа: замыкание по личности опередило проверку формата")
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Положительные контроли — по ОДНОМУ НА СЕМЕЙСТВО. Замена покрывает четыре формы
	// владения в двух семействах; проверка, принимающая только v4, обрубила бы половину
	// замены, и отрицание выше зеленело бы на этом обрубке.
	t.Run("законное значение IPv4 проходит", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", IPAddress: "10.1.0.5"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})

	t.Run("законное значение IPv6 проходит", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", IPAddress: "fd00::5"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})

	t.Run("сужение не задано — список остаётся списком проекта", func(t *testing.T) {
		// Парный контроль к отказу: пустая строка означает «без сужения», а не
		// «малформед». Без него проверка формата могла бы отвергать каждый список без
		// значения, и отрицание выше зеленело бы на сломанном.
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})

	t.Run("формат страницы проверяется раньше формата значения", func(t *testing.T) {
		// Оба — проверки ввода, но у пагинации первый стейтмент: она не зависит ни от
		// чего в запросе. Утверждение пиннит порядок ТЕКСТОМ отказа, иначе
		// «InvalidArgument» был бы верен при любом из двух.
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", IPAddress: malformed},
			Pagination{PageToken: "not-a-real-token!!"})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.NotContains(t, status.Convert(err).Message(), "ip_address",
			"первым обязан отвечать формат страницы")
	})

	t.Run("значение с зоной интерфейса отвергается", func(t *testing.T) {
		// Хранимое значение — голый адрес, зоны в нём нет никогда. Значит
		// зона-квалифицированная строка не совпадёт ни с одной строкой, то есть даёт
		// то же ложное «ни у кого нет». Отдельный подслучай: `netip.ParseAddr` такую
		// строку РАЗБИРАЕТ, поэтому одна лишь успешность разбора класс не закрывает.
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", IPAddress: "fe80::1%eth0"}, Pagination{PageSize: 50})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}
