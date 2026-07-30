// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

// Свободная карта данных машины принималась без единой границы: ни числа ключей,
// ни длины ключа и значения, ни суммарного размера. Ни в проверке запроса, ни в
// домене, ни в объявлении, ни в схеме.
//
// Записанное объяснение этому было — и оно ЛОЖНО в несущей части: «ограничивает
// потолок размера сообщения». Потолок ограничивает ОДИН вызов, а правка СЛИВАЕТСЯ
// в уже накопленное, поэтому карта растёт от вызова к вызову и потолок сообщения
// не ограничивает хранимое вовсе. То есть документ описывал границу, которая не
// ограничивает, — та же форма без содержания, что мы ловим в коде.
//
// Следствие, которого записанное объяснение не касалось ни словом: каждая правка
// навсегда кладёт ВЕСЬ выросший блоб ещё и в две служебные таблицы (ответ операции
// и исходящую очередь), которые не подчищаются никогда, а база у сервиса одна на
// всех тенантов.
//
// Проверяется наблюдаемое: запрос сверх бюджета получает отказ, называющий поле,
// СИНХРОННО — до создания операции; законный запрос проходит.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
)

// bigValue — значение заданной длины.
func bigValue(n int) string { return strings.Repeat("x", n) }

// metadataOverBudget — карта, суммарный размер которой заведомо больше бюджета.
func metadataOverBudget() map[string]string {
	m := make(map[string]string, 4)
	chunk := domain.MaxInstanceMetadataBytes/3 + 1
	for i, k := range []string{"a", "b", "c", "d"} {
		_ = i
		m[k] = bigValue(chunk)
	}
	return m
}

// TestValidateCreate_MetadataOverBudgetRejectedByName — вход создания.
func TestValidateCreate_MetadataOverBudgetRejectedByName(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}
	req.Metadata = metadataOverBudget()

	err := ValidateCreateInstanceReq(req)
	require.Error(t, err, "свободная карта без границы растёт до потолка строки БД и дублируется "+
		"в неподчищаемые служебные таблицы")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "metadata",
		"отказ обязан называть поле")
}

// TestValidateCreate_MetadataTooManyKeysRejected — число ключей тоже граница:
// суммарный размер можно набрать и множеством мелких пар.
func TestValidateCreate_MetadataTooManyKeysRejected(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}
	req.Metadata = make(map[string]string, domain.MaxInstanceMetadataKeys+1)
	for i := 0; i <= domain.MaxInstanceMetadataKeys; i++ {
		req.Metadata[string(rune('a'+i%26))+string(rune('a'+i/26))+"-"+bigValue(1)] = "v"
	}

	err := ValidateCreateInstanceReq(req)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "metadata")
}

// TestValidateCreate_MetadataWithinBudgetPasses — обратная сторона: законная карта
// проходит. Без неё предыдущие зеленели бы и на «отвергать любую карту».
func TestValidateCreate_MetadataWithinBudgetPasses(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}
	req.Metadata = map[string]string{
		"user-data":  bigValue(4096),
		"ssh-keys":   "ssh-ed25519 AAAA...",
		"cloud-init": bigValue(1024),
	}
	require.NoError(t, ValidateCreateInstanceReq(req))
}

// TestUpdateMetadata_OverBudgetDeltaRejectedSynchronously — правка отвергается ДО
// создания операции: асинхронный отказ заставил бы клиента поллить, чтобы узнать о
// собственной опечатке, а строка операции всё равно была бы записана.
func TestUpdateMetadata_OverBudgetDeltaRejectedSynchronously(t *testing.T) {
	kit := newInstanceSvc(t, true)
	op, err := kit.svc.UpdateMetadata(context.Background(), "ins-whatever00000000",
		nil, metadataOverBudget())
	require.Error(t, err, "правка сверх бюджета обязана отвергаться синхронно")
	require.Nil(t, op, "операция не должна создаваться на заведомо неприемлемую правку")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "metadata")
}

// TestUpdateMetadata_LegitimateDeltaProceeds — обратная сторона.
func TestUpdateMetadata_LegitimateDeltaProceeds(t *testing.T) {
	kit := newInstanceSvc(t, true)
	in := seedInstance(kit.repo, domain.InstanceStatusRunning)

	op, err := kit.svc.UpdateMetadata(context.Background(), in.ID, []string{"gone"},
		map[string]string{"user-data": bigValue(2048)})
	require.NoError(t, err)
	require.NotNil(t, op)
}
