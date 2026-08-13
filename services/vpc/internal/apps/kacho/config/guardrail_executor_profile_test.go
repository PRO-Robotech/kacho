// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// guardrail_executor_profile_test.go — страж S5: профиль возможностей исполнителя
// датаплейна.
//
// Управляющий контур принимает от арендатора то, что исполнять будет НЕ он:
// адресные диапазоны (уникальные лишь в пределах сети — `EXCLUDE USING gist
// (network_id WITH =, …)`), правила со ссылкой на именованный набор, ограничение
// полосы. Возможностей исполнителя контур не знает — их объявляет посадка. Пока
// объявления нет, «принято» и «реализуемо» неотличимы.
//
// Случаи ниже устроены как инъекция в обе стороны: законная посадка (prodCfg +
// полный профиль) обязана ПРОХОДИТЬ, и каждое отрицание ломает в ней РОВНО ОДНУ
// вещь. Без положительного контроля отрицания зеленели бы на страже, который
// отказывает всегда.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prodExecutorCfg — законная боевая посадка с полностью объявленным профилем.
// Отправная точка каждого отрицания: ломается ровно одно поле, остальное остаётся
// законным, поэтому отказ нельзя списать на общую негодность входа.
//
// Само объявление живёт в общей фикстуре пакета (prodCfg), а не здесь: «законная
// боевая посадка» — одна величина, и второе её описание разошлось бы с первым на
// первом же новом требовании. Обёртка оставлена ради читаемости случаев ниже — она
// называет, ОТ ЧЕГО отступает каждый из них.
func prodExecutorCfg(mode Mode) Config {
	return prodCfg(mode, "kacho-iam.kacho.svc:9091")
}

// S5-01 (положительный контроль): полностью объявленный профиль проходит.
func TestValidateExecutorProfile_Production_FullyDeclared_Passes(t *testing.T) {
	require.NoError(t, prodExecutorCfg(ModeProduction).ValidateExecutorProfile())
	require.NoError(t, prodExecutorCfg(ModeProductionStrict).ValidateExecutorProfile())
}

// S5-02: пересечение адресов не объявлено → отказ старта, и отказ НАЗЫВАЕТ ручку.
// Текст читает оператор, поднимающий стенд: без имени настройки стенд не поднять.
func TestValidateExecutorProfile_Production_OverlapNotDeclared_Fails(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.OverlappingTenantAddresses = false

	err := c.ValidateExecutorProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.overlapping-tenant-addresses")
	assert.Contains(t, err.Error(), "KACHO_VPC_DATAPLANE__EXECUTOR__OVERLAPPING_TENANT_ADDRESSES")
	assert.Contains(t, err.Error(), "mode production")
}

// S5-03: то же в strict — режим не смягчает (любой IsProduction).
func TestValidateExecutorProfile_ProductionStrict_OverlapNotDeclared_Fails(t *testing.T) {
	c := prodExecutorCfg(ModeProductionStrict)
	c.Dataplane.Executor.OverlappingTenantAddresses = false

	err := c.ValidateExecutorProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.overlapping-tenant-addresses")
	assert.Contains(t, err.Error(), "mode production-strict")
}

// S5-04: dev не несёт исполнителя вовсе — требований к профилю у него нет.
//
// Это НЕ послабление посадки: любой РАЗВЁРНУТЫЙ стенд работает в боевом режиме
// (core rule #16), а dev остаётся режимом внутрипроцессных фикстур, где
// датаплейна нет. Отдельно: испорченное ОБЪЯВЛЕНИЕ отвергается и на dev — см.
// S5-08/S5-09, где предмет не посадка, а невозможное значение.
func TestValidateExecutorProfile_Dev_NoProfile_Passes(t *testing.T) {
	c := prodCfg(ModeDev, "kacho-iam.kacho.svc:9091")
	require.NoError(t, c.ValidateExecutorProfile())
}

// S5-05: отслеживание состояния не объявлено → отказ. Пустое объявление значит
// «неизвестно», а не «ничего не отслеживаем»: обратный трафик разрешается
// состоянием, и на неизвестной статусности правило принимается, а исполниться
// не может.
func TestValidateExecutorProfile_Production_NoStateTrackingFamilies_Fails(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.StateTrackingFamilies = nil

	err := c.ValidateExecutorProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.state-tracking-families")
}

// S5-06 — ВЫРОЖДЕННЫЙ ВХОД: одинокая запятая.
//
// Предикат непустоты обязан быть ТЕМ ЖЕ, что читает страж. Сырая настройка здесь
// НЕПУСТА (две записи после разбора по запятой), а семейств в ней ноль. Считать
// такую строку заполненной значило бы пропустить неизвестную статусность мимо
// стража — ровно то расхождение, из-за которого «непусто для стража» и «пусто для
// проверки» уживаются в одной посадке.
func TestValidateExecutorProfile_Production_LoneCommaReadsAsEmpty(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.StateTrackingFamilies = []string{"", ""} // то, во что разбирается ","

	require.NotEmpty(t, c.Dataplane.Executor.StateTrackingFamilies,
		"сырая настройка непуста — иначе у случая нет предмета")
	assert.False(t, c.StateTrackingFamilies().IsDeclared(),
		"единственный предикат непустоты обязан прочитать это как «не объявлено»")

	err := c.ValidateExecutorProfile()
	require.Error(t, err, "страж обязан читать ТОТ ЖЕ предикат, а не длину сырой настройки")
	assert.Contains(t, err.Error(), "dataplane.executor.state-tracking-families")
}

// S5-07: пробелы по краям срезаются, повторы схлопываются, регистр не значим —
// оператор, написавший список через «запятая-пробел», получает объявленный
// профиль, а не отказ. Парный положительный контроль к S5-06: нормализация не
// делает пустым то, что оператор действительно написал.
func TestStateTrackingFamilies_NormalisesWhatTheOperatorWrote(t *testing.T) {
	var c Config
	c.Dataplane.Executor.StateTrackingFamilies = []string{" V4 ", "v4", "v6", ""}

	f := c.StateTrackingFamilies()
	assert.True(t, f.IsDeclared())
	assert.Equal(t, []string{"v4", "v6"}, f.Declared())
	assert.True(t, f.Has("v4"))
	assert.True(t, f.Has("v6"))
	assert.Empty(t, f.Unknown())
}

// S5-08: неизвестное семейство — ОПЕЧАТКА, а не посадка, поэтому отвергается в
// ЛЮБОМ режиме и с именем того, что написано. Молча отброшенная запись означала
// бы профиль, который оператор считает объявленным, а страж — нет.
func TestValidateExecutorProfile_UnknownFamily_FailsInEveryMode(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodExecutorCfg(mode)
		c.Dataplane.Executor.StateTrackingFamilies = []string{"v4", "ipv6"}

		err := c.ValidateExecutorProfile()
		require.Error(t, err, "режим %s", mode)
		assert.Contains(t, err.Error(), "ipv6")
		assert.Contains(t, err.Error(), "dataplane.executor.state-tracking-families")
	}
}

// S5-09: ограничение полосы арендатором объявлено, а гарантированной полосы нет —
// самопротиворечие объявления (ограничивать не от чего), поэтому отказ в любом
// режиме.
func TestValidateExecutorProfile_TenantLimitWithoutGuaranteedBand_FailsInEveryMode(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodExecutorCfg(mode)
		c.Dataplane.Executor.TenantSettableBandwidthLimit = true
		c.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps = 0

		err := c.ValidateExecutorProfile()
		require.Error(t, err, "режим %s", mode)
		assert.Contains(t, err.Error(), "dataplane.executor.tenant-settable-bandwidth-limit")
		assert.Contains(t, err.Error(), "dataplane.executor.guaranteed-bandwidth-per-interface-mbps")
	}
}

// S5-10: гарантия, равная нулю, — это отсутствие гарантии, а не гарантия нуля.
// Каждое из трёх чисел проверяется отдельно: общий отказ «что-то не объявлено»
// не сказал бы оператору, что именно.
func TestValidateExecutorProfile_Production_ZeroGuarantees_FailNamingEachNumber(t *testing.T) {
	cases := []struct {
		name  string
		unset func(*ExecutorProfileConfig)
		knob  string
	}{
		{"payload", func(e *ExecutorProfileConfig) { e.GuaranteedPayloadBytes = 0 }, "dataplane.executor.guaranteed-payload-bytes"},
		{"bandwidth", func(e *ExecutorProfileConfig) { e.GuaranteedBandwidthPerInterfaceMbps = 0 }, "dataplane.executor.guaranteed-bandwidth-per-interface-mbps"},
		{"connections", func(e *ExecutorProfileConfig) { e.ConnectionLimitPerInterface = 0 }, "dataplane.executor.connection-limit-per-interface"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := prodExecutorCfg(ModeProduction)
			tc.unset(&c.Dataplane.Executor)

			err := c.ValidateExecutorProfile()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.knob)
		})
	}
}

// S5-11: отрицательное число — не «меньше нуля значит без границы», а негодное
// объявление; отвергается в любом режиме.
func TestValidateExecutorProfile_NegativeGuarantee_FailsInEveryMode(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodExecutorCfg(mode)
		c.Dataplane.Executor.GuaranteedPayloadBytes = -1

		err := c.ValidateExecutorProfile()
		require.Error(t, err, "режим %s", mode)
		assert.Contains(t, err.Error(), "dataplane.executor.guaranteed-payload-bytes")
	}
}

// S5-12: ссылка на именованный набор в правиле не объявлена → отказ.
//
// Предмет не гипотетический: цель правила группы безопасности задаётся ИЛИ
// диапазонами, ИЛИ идентификатором группы (взаимоисключающие ветви контракта).
// Вторая ветвь — уже принятая поверхность, поэтому исполнитель, который её не
// умеет, оставляет часть принятых правил неисполнимой.
func TestValidateExecutorProfile_Production_NamedSetReferenceNotDeclared_Fails(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.NamedSetReferenceInRule = false

	err := c.ValidateExecutorProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.named-set-reference-in-rule")
}

// S5-13: страж входит в агрегатор старта. Агрегатор выглядит как «полная проверка
// старта», поэтому пропущенная в нём проверка — ловушка: тот, кто переведёт на
// него композиционный корень, тихо останется без неё.
func TestValidateBoot_IncludesTheExecutorProfileGuard(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.OverlappingTenantAddresses = false
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = true
	m.IAMAuthzMTLS.Enable = true
	m.IAMProjectMTLS.Enable = true
	m.IAMRegisterMTLS.Enable = true
	m.GeoMTLS.Enable = true

	err := c.ValidateBoot(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.overlapping-tenant-addresses")
}
