// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority_test.go — страж посадки объявления домена величин.
//
// Приёмка `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
// стадия S1, сценарии KAN-Q1-02, KAN-Q1-04, KAN-Q1-05, KAN-Q1-06.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	corequota "github.com/PRO-Robotech/corelib/quota"
)

// TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart — незаданное объявление
// отвергается, и текст отказа называет ручку.
//
// Без имени ручки в тексте стенд не поднять: это одно из трёх мест, прямо
// выведенных из-под запрета `security.md` §«Публичные артефакты».
func TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart(t *testing.T) {
	var c Config
	err := c.ValidateQuotaAuthority()
	require.Error(t, err)
	require.Contains(t, err.Error(), "quota.authority")
	require.Contains(t, err.Error(), corequota.NotDeployed,
		"отказ обязан назвать ОБА законных значения, иначе он не восстанавливает "+
			"следующий шаг оператора")
}

// TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture — положительный
// близнец к отказам старта.
//
// Без него утверждения «процесс не поднимается» зеленели бы на объявлении,
// которое не принимается никогда.
func TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture(t *testing.T) {
	c := Config{Quota: QuotaConfig{Authority: corequota.NotDeployed}}
	c.AuthN.Mode = ModeProductionStrict

	a, err := c.QuotaAuthority()
	require.NoError(t, err, "объявленное отсутствие — законная посадка, а не отказ")
	require.False(t, a.Deployed())
	require.Equal(t, corequota.AuthorityAbsent, a.State())
}

// TestQuotaAuthority_KAN_Q1_05_HalfAPairRefusesStart — адрес есть,
// удостоверения нет.

// TestQuotaAuthority_HalfAPairIsSilentOutsideProduction — зеркало предыдущего.
//
// TestQuotaAuthority_KAN_Q1_04_AddressIsNotDerivedFromAuthz — объявление величин
// НЕ выводится из адреса соседа по авторизации.
//
// ПЕРЕПИСАНА под то, что дерево производит сегодня. Прежняя редакция разводила
// два АДРЕСА; адрес домена величин больше не бывает законным, и утверждать о нём
// нечего. Свойство при этом не исчезло, а стало сильнее: при живом адресе
// авторизации объявление величин обязано остаться ОТСУТСТВУЮЩИМ. Выведись оно из
// чужого ребра — вернулся бы ровно тот дефект, ради которого ручка и заведена
// (`security.md` §Hardening п. 9: адрес, от которого зависит решение, не
// выводится из чужого адреса).
func TestQuotaAuthority_KAN_Q1_04_AddressIsNotDerivedFromAuthz(t *testing.T) {
	c := Config{Quota: QuotaConfig{Authority: corequota.NotDeployed}}
	c.AuthN.Mode = ModeDev
	c.AuthZ.IAMEndpoint = "kaname-internal.kacho.svc:9091"

	a, err := c.QuotaAuthority()
	require.NoError(t, err)
	require.False(t, a.Deployed(),
		"живой адрес авторизации не делает домен величин развёрнутым")
	require.Empty(t, a.Endpoint(),
		"объявление величин не заимствует чужой адрес")

	// Смена адреса авторизации на объявление величин не влияет ничем.
	c.AuthZ.IAMEndpoint = "kaname-internal.other.svc:9091"
	b, err := c.QuotaAuthority()
	require.NoError(t, err)
	require.Equal(t, a.State(), b.State())

	// И наоборот: объявление величин не трогает адрес авторизации.
	before := c.AuthZ.IAMEndpoint
	_, err = c.QuotaAuthority()
	require.NoError(t, err)
	require.Equal(t, before, c.AuthZ.IAMEndpoint)
}

// TestQuotaAuthority_ValidateBootCarriesTheGuard — страж входит в АГРЕГАТОР.
//
// Проверка, не попавшая в агрегатор, становится ловушкой: он выглядит как
// «полная проверка старта», и переведённый на него композиционный корень тихо
// остаётся без неё.
func TestQuotaAuthority_ValidateBootCarriesTheGuard(t *testing.T) {
	var c Config
	err := c.ValidateBoot(MTLSConfig{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "quota.authority"),
		"ValidateBoot обязан нести стража объявления домена величин; получено: %v", err)
}

// TestQuotaAuthority_KnobReachesTheField — ручка ДОЕЗЖАЕТ до поля.
//
// Проба существует потому, что дефект был ровно здесь и он тихий: viper
// подхватывает переменную окружения только для ИЗВЕСТНОГО ключа. Без объявления
// ключа в умолчаниях профиль задавал бы `KACHO_VPC_QUOTA__AUTHORITY`, чарт
// доставлял бы её, а процесс читал бы пустую строку и отказывал в старте, называя
// ручку, которую оператор ЗАДАЛ. Класс «принято-и-проигнорировано» с обратным
// знаком: не «принято и выброшено», а «задано и не прочитано».
//
// Утверждается ИСХОД загрузки, а не наличие вызова SetDefault: проба на вызов
// зеленела бы и на ключе, написанном с опечаткой.
func TestQuotaAuthority_KnobReachesTheField(t *testing.T) {
	t.Setenv("KACHO_VPC_QUOTA__AUTHORITY", "limits.kacho.svc:9091")

	cfg, err := Load("")
	require.NoError(t, err, "загрузка настроек")
	require.Equal(t, "limits.kacho.svc:9091", cfg.Quota.Authority,
		"значение ручки обязано доехать до поля; иначе процесс отказывает в старте, "+
			"называя ручку, которую оператор задал")
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
	c := Config{Quota: QuotaConfig{Authority: "kaname-internal.kacho.svc:9091"}}
	c.AuthN.Mode = ModeProductionStrict
	err := c.ValidateQuotaAuthority()
	require.Error(t, err, "адрес принят молча — ручка объявляет возможность, которой нет")
	require.Contains(t, err.Error(), "quota.authority",
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
	c := Config{Quota: QuotaConfig{Authority: "kaname-internal.kacho.svc:9091"}}
	c.AuthN.Mode = ModeDev
	require.Error(t, c.ValidateQuotaAuthority())
}
