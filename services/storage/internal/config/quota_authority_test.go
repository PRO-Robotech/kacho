// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority_test.go — страж посадки объявления домена величин.
//
// Приёмка `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
// стадия S1, сценарии KAN-Q1-02, KAN-Q1-05, KAN-Q1-06.

import (
	"testing"

	"github.com/stretchr/testify/require"

	corequota "github.com/PRO-Robotech/corelib/quota"
)

// TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart — незаданное объявление
// отвергается, и текст отказа называет ручку. Без имени ручки стенд не поднять.
func TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	err := c.ValidateQuotaAuthority()
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_STORAGE_QUOTA_AUTHORITY")
	require.Contains(t, err.Error(), corequota.NotDeployed,
		"отказ обязан назвать ОБА законных значения")
}

// TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture — положительный
// близнец к отказам старта: без него «процесс не поднимается» зеленело бы на
// объявлении, которое не принимается никогда.
func TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	c.QuotaAuthority = corequota.NotDeployed

	a, err := c.QuotaAuthorityDeclaration()
	require.NoError(t, err, "объявленное отсутствие — законная посадка, а не отказ")
	require.False(t, a.Deployed())
	require.Equal(t, corequota.AuthorityAbsent, a.State())
}

// TestQuotaAuthority_KAN_Q1_05_HalfAPairRefusesStart — адрес есть,
// удостоверения нет.

// TestQuotaAuthority_HalfAPairIsSilentOutsideProduction — зеркало предыдущего.
//
// Требование транспорта у ребра величин ТО ЖЕ, что у остальных рёбер службы:
// собственная строгость сделала бы локальный стенд неподнимаемым там, где все
// прочие рёбра ходят открытым текстом законно.

// TestQuotaAuthority_ValidateCarriesTheGuard — страж входит в общий валидатор
// посадки.
//
// Проверка, не попавшая туда, становится ловушкой: валидатор выглядит как
// «полная проверка старта», и переведённый на него композиционный корень тихо
// остаётся без неё.
func TestQuotaAuthority_ValidateCarriesTheGuard(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_STORAGE_QUOTA_AUTHORITY",
		"общий валидатор обязан нести стража объявления домена величин")
}

// TestQuotaAuthority_AbsentAuthorityWithDeclaredTransportRefusesStart — ВТОРАЯ
// половина пары, зеркальная KAN-Q1-05: адрес объявляет ОТСУТСТВИЕ домена
// величин, а имя для сверки рукопожатия задано. Обращаться не к кому, поэтому
// имя называет пира, к которому ребро НЕ идёт.

// TestQuotaAuthority_AbsentAuthorityWithDeclaredTransportRefusesOutsideProductionToo —
// режимом этот отказ НЕ смягчается: собеседника нет ни в одном режиме.

// TestQuotaAuthority_AbsentAuthorityWithoutTransportIsSilent — положительный
// близнец: объявленное отсутствие БЕЗ удостоверения — законная посадка.

// TestQuotaAuthority_AddressRefusesStart_NoProducer — адрес отвергается СТАРТОМ:
// производителя у контракта авторитета величин не осталось ни в одном дереве.
//
// ЗАМЕНИЛА пару проб о половине пары «адрес и удостоверение». Их предмет снят
// вместе с ребром — стеречь удостоверение к собеседнику, которого нет, нечего
// (`testing.md` §«Гейт на класс», п. 9: проба, чей предмет снят, ЗАМЕНЯЕТСЯ, а не
// ослабляется).
func TestQuotaAuthority_AddressRefusesStart_NoProducer(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	c.QuotaAuthority = "kaname-internal.kacho.svc:9091"
	err := c.ValidateQuotaAuthority()
	require.Error(t, err, "адрес принят молча — ручка объявляет возможность, которой нет")
	require.Contains(t, err.Error(), "KACHO_STORAGE_QUOTA_AUTHORITY",
		"отказ обязан назвать ручку: без её имени стенд не поднять")
	require.Contains(t, err.Error(), corequota.NotDeployed,
		"отказ обязан назвать СЛЕДУЮЩИЙ ШАГ оператора — единственное действующее значение")
	require.Contains(t, err.Error(), "#2190",
		"отказ обязан назвать предмет, которым состояние снимается")
}

// TestQuotaAuthority_AddressRefusalIsNotSoftenedByPosture — режимом отказ НЕ
// смягчается, и этим он отличается от снятого требования транспорта.
//
// «Требуется ли проверяемый транспорт» было вопросом посадки. «Есть ли
// собеседник» — не вопрос посадки: производителя нет ни в одном режиме.
// Смягчение оставило бы объявление, которое поднимается на стенде и отказывает в
// бою, — ту самую неразличимость, ради устранения которой отказ и заведён.
func TestQuotaAuthority_AddressRefusalIsNotSoftenedByPosture(t *testing.T) {
	c := Config{AuthMode: "dev"}
	c.QuotaAuthority = "kaname-internal.kacho.svc:9091"
	require.Error(t, c.ValidateQuotaAuthority())
}
