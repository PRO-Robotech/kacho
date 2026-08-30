// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// reservedcidr_test.go — отказ на пересечении со служебным адресным пространством
// несёт МАШИННЫЙ признак полосы.
//
// ПРЕДМЕТ. Перечень служебных диапазонов задаётся посадкой и наружу не
// выставляется — решение осознанное (`security.md` §«Инфра-чувствительные
// данные»). Следствие для клиента: планировщик адресации ходит кругами «запрос →
// отказ → другой префикс», и число кругов ничем не ограничено. Пока отказ
// различим только прозой, автомат не может даже РАЗВЕТВИТЬСЯ — «этот префикс
// служебный, возьми следующий кандидат» неотличимо от «твой ввод сломан, чинить
// надо код». Разбор прозы контракт прямо запрещает (`api-conventions.md`
// §By-lane code-split): тон сообщения стабилен, но не парсибелен.
//
// ГРАНИЦА, названная прямо: признак НЕ уменьшает число кругов. Он делает их
// машинно различимыми — не больше и не меньше. Раскрытие перечня рассмотрено и
// отвергнуто, разбор — в `services/vpc/docs/engineering/architecture/`.

// TestReservedCIDROverlap_CarriesMachineReadableReason — отказ утверждается
// ТРОЙКОЙ: код · признак полосы · текст. Утверждать один без остальных значило бы
// не заметить смены ровно того, на что клиент ключуется.
func TestReservedCIDROverlap_CarriesMachineReadableReason(t *testing.T) {
	err := ReservedCIDROverlap("v4_cidr_blocks[0]", "192.0.2.0/24")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code(),
		"полоса синхронной валидации ввода: код не меняется")

	// Тон — часть контракта и утверждается дословно.
	assert.Equal(t,
		"v4_cidr_blocks[0] 192.0.2.0/24 overlaps an address range reserved by the platform",
		st.Message())

	info := errorInfoOf(t, err)
	assert.Equal(t, ReasonSubnetCIDRReserved, info.GetReason(),
		"клиент различает полосу по признаку, а не разбором прозы")
	assert.Equal(t, "vpc.kacho.cloud", info.GetDomain())

	// Метаданные несут ТОЛЬКО присланное вызывающим: слот и его собственное
	// значение. Служебный диапазон, с которым вышло пересечение, не называется —
	// иначе отказ стал бы способом получить карту служебного пространства по
	// одному пробному запросу.
	md := info.GetMetadata()
	assert.Equal(t, "v4_cidr_blocks[0]", md["field"])
	assert.Equal(t, "192.0.2.0/24", md["value"])
	for k, v := range md {
		assert.NotContains(t, v, "10.11.12.",
			"метаданные не вправе нести диапазон, которого вызывающий не называл (ключ %q)", k)
	}
}

// TestReservedCIDROverlap_KeepsFieldViolation — признак ДОБАВЛЕН, а не заменил
// собой то, что уже было. Форма отказа для вызывающего, читающего поле, обязана
// остаться прежней: смена признака не должна быть ломающей.
func TestReservedCIDROverlap_KeepsFieldViolation(t *testing.T) {
	err := ReservedCIDROverlap("v6_cidr_blocks[1]", "2001:db8::/32")

	var found bool
	for _, d := range status.Convert(err).Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			found = true
			assert.Equal(t, "v6_cidr_blocks[1]", v.GetField())
			assert.Contains(t, v.GetDescription(), "reserved by the platform")
		}
	}
	assert.True(t, found,
		"деталь поля обязана остаться: признак ДОБАВЛЕН, а не заменил собой прежнюю форму")
}
