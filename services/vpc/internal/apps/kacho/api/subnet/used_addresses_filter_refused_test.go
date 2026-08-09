// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// TestListUsedAddressesRejectsFilterByName — `filter` на занятости подсети
// отвергается ЯВНО и с именем поля.
//
// ПРЕДМЕТ (api-conventions.md, «Принято-и-проигнорировано — ЗАПРЕЩЕНО»). Поле
// принималось и выбрасывалось: вызывающий получал 200 и полный список, будучи
// уверен, что сузил выборку. Из трёх законных исходов выбран второй — явный
// синхронный отказ с именем поля.
//
// ПОЧЕМУ НЕ «РЕАЛИЗОВАТЬ». Ответ этого RPC — не ресурс Address, а проекция
// занятости: `address`, `ipVersion`, `references`. Грамматика выражения
// (`pkg/filter`) сегодня — `<поле>="<значение>"` по белому списку, и ни одного
// поля проекции в него положить нельзя: `name` у проекции нет, а фильтровать по
// невидимому в ответе имени значит вернуть выборку, сужение которой вызывающий
// проверить не может.
//
// ПОЧЕМУ НЕ «СНЯТЬ С КОНТРАКТА». Край REST выбрасывает неизвестные ключи
// запроса молча, поэтому удаление поля вернуло бы ровно то, что здесь чинится:
// `?filter=…` снова уезжал бы в 200 с несуженным списком. Тот же довод записан
// у compute над `CreateInstanceRequest.ssh_public_keys`.
//
// Пара обязательна: без положительного контроля («без фильтра проходит»)
// отрицание зеленело бы и на RPC, отвергающем вообще всё.
func TestListUsedAddressesRejectsFilterByName(t *testing.T) {
	// Все use-case'ы — nil: отказ обязан произойти ПЕРВЫМ, до любого обращения к
	// ним. Дойди проверка до use-case — тест упал бы паникой, а не вердиктом.
	h := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil)

	t.Run("непустой filter — отказ с именем поля", func(t *testing.T) {
		_, err := h.ListUsedAddresses(context.Background(), &vpcv1.ListUsedAddressesRequest{
			SubnetId: "sub00000000000000000",
			Filter:   `name="web"`,
		})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "filter",
			"отказ обязан НАЗЫВАТЬ поле, иначе вызывающий не узнает, что именно не применено")
	})

	t.Run("пустой filter — до отказа не доходит (положительный контроль)", func(t *testing.T) {
		// Нулевой use-case означает: если бы проверка отвергала и пустой фильтр,
		// вердикт был бы InvalidArgument; а раз она его пропускает — управление
		// уходит дальше и падает паникой на nil. Ловим её и тем доказываем, что
		// путь ПРОЙДЕН, а не отвергнут.
		defer func() {
			assert.NotNil(t, recover(),
				"пустой filter обязан пройти проверку насквозь, а не быть отвергнутым")
		}()
		_, err := h.ListUsedAddresses(context.Background(), &vpcv1.ListUsedAddressesRequest{
			SubnetId: "sub00000000000000000",
		})
		t.Fatalf("ожидалась передача управления дальше по обработчику, получен err=%v", err)
	})
}
