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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
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
	return prodCfg(mode, "kaname.kacho.svc:9091")
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
	c := prodCfg(ModeDev, "kaname.kacho.svc:9091")
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

// S5-14: объявленный полезный размер кадра НИЖЕ обещания продукта → отказ старта.
//
// Обещание — величина продукта (domain.GuaranteedPayloadFloorBytes), а не
// настройка: арендатор читает его в документации и рассчитывает на него, не зная
// ни этого стенда, ни его исполнителя. Посадка, объявившая меньше, делает обещание
// ложным для каждого, кто на неё придёт, — и делает это молча: контур принимает
// тот же трафик, что и раньше.
//
// Отказ обязан назвать ОБА числа: объявленное и обещанное. Без объявленного
// оператор не знает, что чинить; без обещанного — до какой величины.
func TestValidateExecutorProfile_Production_PayloadBelowProductFloor_Fails(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.GuaranteedPayloadBytes = domain.GuaranteedPayloadFloorBytes - 1

	err := c.ValidateExecutorProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.guaranteed-payload-bytes")
	assert.Contains(t, err.Error(), "KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_PAYLOAD_BYTES")
	assert.Contains(t, err.Error(), strconv.Itoa(domain.GuaranteedPayloadFloorBytes-1),
		"отказ обязан назвать ОБЪЯВЛЕННОЕ число — иначе непонятно, что чинить")
	assert.Contains(t, err.Error(), strconv.Itoa(domain.GuaranteedPayloadFloorBytes),
		"отказ обязан назвать ОБЕЩАННОЕ число — иначе непонятно, до какой величины чинить")
	assert.Contains(t, err.Error(), "mode production")
}

// S5-15 (положительный контроль к S5-14): РОВНО обещание проходит.
//
// Без этой пары отрицание зеленело бы на страже, который отвергает любое значение:
// граница включающая, и это часть обещания — «не ниже», а не «строго выше».
func TestValidateExecutorProfile_Production_PayloadExactlyAtProductFloor_Passes(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.GuaranteedPayloadBytes = domain.GuaranteedPayloadFloorBytes

	require.NoError(t, c.ValidateExecutorProfile())
}

// S5-16 (второй положительный контроль): исполнитель, проносящий БОЛЬШЕ обещанного,
// законен. Обещание — нижняя граница, а не равенство: контур не вправе требовать от
// посадки ровно своё число.
func TestValidateExecutorProfile_Production_PayloadAboveProductFloor_Passes(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.GuaranteedPayloadBytes = domain.GuaranteedPayloadFloorBytes + 50

	require.NoError(t, c.ValidateExecutorProfile())
}

// S5-17: на dev нижняя граница не требуется — по той же причине, что и весь блок
// требований к посадке (S5-04): исполнителя там нет вовсе, обещать нечего и некому.
//
// Это НЕ послабление: любой РАЗВЁРНУТЫЙ стенд работает в боевом режиме (core rule
// #16). Отличие от S5-11 существенно: отрицательное число — негодное ОБЪЯВЛЕНИЕ и
// отвергается в любом режиме, а 1200 — объявление годное, но недостаточное для
// обещания, то есть требование к посадке.
func TestValidateExecutorProfile_Dev_PayloadBelowProductFloor_Passes(t *testing.T) {
	c := prodCfg(ModeDev, "kaname.kacho.svc:9091")
	c.Dataplane.Executor.GuaranteedPayloadBytes = domain.GuaranteedPayloadFloorBytes - 200

	require.NoError(t, c.ValidateExecutorProfile())
}

// S5-18: умение задавать ограничение полосы объявлено, а гарантия НЕ ВЫШЕ
// опубликованного пола продукта — принимаемый промежуток ПУСТ.
//
// Арендаторское ограничение принимается строго выше пола и не выше гарантии
// стенда (`domain.BandwidthLimitPolicy`). Гарантия на уровне пола делает эти два
// края несовместимыми: умение объявлено, и воспользоваться им нельзя ни разу —
// то есть посадка обещает возможность, которой на ней нет. Это негодность самого
// ОБЪЯВЛЕНИЯ, поэтому отказ в любом режиме.
//
// Отказ обязан назвать ОБА числа: без объявленного оператор не знает, что чинить,
// без обещанного — до какой величины.
func TestValidateExecutorProfile_TenantLimitWithEmptyRange_FailsInEveryMode(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodExecutorCfg(mode)
		c.Dataplane.Executor.TenantSettableBandwidthLimit = true
		c.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps = domain.GuaranteedInterfaceBandwidthFloorMbps

		err := c.ValidateExecutorProfile()
		require.Error(t, err, "режим %s", mode)
		assert.Contains(t, err.Error(), "dataplane.executor.tenant-settable-bandwidth-limit")
		assert.Contains(t, err.Error(), "dataplane.executor.guaranteed-bandwidth-per-interface-mbps")
		assert.Contains(t, err.Error(), strconv.Itoa(domain.GuaranteedInterfaceBandwidthFloorMbps),
			"отказ обязан назвать ОБЕЩАННОЕ число — иначе оператор не знает, до какой величины поднимать")
	}
}

// S5-19 (положительный контроль к S5-18): гарантия СТРОГО выше пола — промежуток
// непуст, объявление законно.
//
// Без него отказ выше был бы неотличим от «умение нельзя объявить никогда».
func TestValidateExecutorProfile_TenantLimitWithNonEmptyRange_Passes(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodExecutorCfg(mode)
		c.Dataplane.Executor.TenantSettableBandwidthLimit = true
		c.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps = domain.GuaranteedInterfaceBandwidthFloorMbps + 1

		require.NoError(t, c.ValidateExecutorProfile(), "режим %s", mode)
	}
}

// S5-20: объявленная полоса НИЖЕ обещания продукта — то же требование к посадке,
// что и у размера кадра (S5-14), и по той же причине: обещание «не менее 1 Гбит/с
// на интерфейс» арендатор читает в документации и проверить на этом стенде не
// может, поэтому стенд, чей исполнитель несёт меньше, делает его ложным ТИХО.
//
// Прежняя редакция комментария рядом с проверкой размера кадра называла его
// ЕДИНСТВЕННОЙ гарантией профиля с обещанием продукта. Это перестало быть верным
// вместе с публикацией полосы — и эта проба и есть то, что делает расхождение
// видимым, а не устная договорённость.
func TestValidateExecutorProfile_Production_BandBelowProductFloor_Fails(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps = domain.GuaranteedInterfaceBandwidthFloorMbps - 100

	err := c.ValidateExecutorProfile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.executor.guaranteed-bandwidth-per-interface-mbps")
	assert.Contains(t, err.Error(), "KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_BANDWIDTH_PER_INTERFACE_MBPS")
	assert.Contains(t, err.Error(), "mode production")
}

// S5-21 (положительный контроль): ровно обещанное законно — граница ВКЛЮЧАЮЩАЯ,
// обещание звучит «не менее».
func TestValidateExecutorProfile_Production_BandExactlyProductFloor_Passes(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps = domain.GuaranteedInterfaceBandwidthFloorMbps

	require.NoError(t, c.ValidateExecutorProfile())
}

// S5-22: на dev нижняя граница полосы не требуется — по той же причине, что и у
// размера кадра (S5-17): исполнителя там нет вовсе.
func TestValidateExecutorProfile_Dev_BandBelowProductFloor_Passes(t *testing.T) {
	c := prodCfg(ModeDev, "kaname.kacho.svc:9091")
	c.Dataplane.Executor.GuaranteedBandwidthPerInterfaceMbps = domain.GuaranteedInterfaceBandwidthFloorMbps - 100

	require.NoError(t, c.ValidateExecutorProfile())
}

// Объявление ЧАРТА сверяется с обещанием там, где уже живёт читатель файла
// значений — `services/vpc/deploy/executor_profile_test.go`. Второй разбор YAML
// здесь был бы вторым предикатом об одном предмете, и разошёлся бы он молча.

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

// ─── #290: три потолка интерфейса обязаны иметь ПРОИЗВОДИТЕЛЯ на нашей стороне ──
//
// Продукт публикует арендатору четыре величины одного интерфейса. Полосу наша
// сторона сверяла с посадкой давно; три остальные — предел одновременных
// соединений, темп их установления и всплеск — до этой правки не читал НИКТО:
// объявление стояло в домене, повторялось в документации и не участвовало ни в
// одной ветке кода. То есть арендатору обещали то, чего никто не проверял даже у
// СЕБЯ, не говоря об исполнителе.
//
// # Почему направление сравнения то же, что у пола, хотя это ПОТОЛКИ
//
// Разница «пол против потолка» — про то, как число читает АРЕНДАТОР: полоса
// гарантируется снизу («не менее»), число соединений ограничивает сверху («не
// более»). Со стороны СТЕНДА обе величины означают одно: он обязан УМЕТЬ выдать
// опубликованное. Стенд, отслеживающий меньше опубликованного потолка, ломает
// арендатора, который на опубликованное число рассчитывал, — и ломает молча,
// потому что проверить стенд арендатору нечем.
//
// Обратная сторона (стенд умеет БОЛЬШЕ) законна и не отвергается: опубликованное
// число не уменьшается никогда, а запас железа обещания не ломает.
//
// # Граница ВКЛЮЧАЮЩАЯ
//
// Ровно опубликованное — законно. Это утверждается отдельным случаем, иначе
// отрицание зеленело бы и от проверки «строго больше», которая отвергала бы
// законную посадку.

// Стенд, умеющий МЕНЬШЕ опубликованного потолка, не поднимается в боевом режиме —
// по каждой из трёх величин, и отказ НАЗЫВАЕТ ручку и оба числа.
func TestValidateExecutorProfile_Production_InterfaceCeilingBelowPublished_Fails(t *testing.T) {
	cases := []struct {
		name      string
		lower     func(*ExecutorProfileConfig)
		knob      string
		published int
	}{
		{
			"одновременные соединения",
			func(e *ExecutorProfileConfig) { e.ConnectionLimitPerInterface = domain.InterfaceConnectionCeiling - 1 },
			"dataplane.executor.connection-limit-per-interface",
			domain.InterfaceConnectionCeiling,
		},
		{
			"темп установления",
			func(e *ExecutorProfileConfig) {
				e.ConnectionRateLimitPerInterfacePerSecond = domain.InterfaceConnectionRateCeilingPerSecond - 1
			},
			"dataplane.executor.connection-rate-limit-per-interface-per-second",
			domain.InterfaceConnectionRateCeilingPerSecond,
		},
		{
			"всплеск",
			func(e *ExecutorProfileConfig) {
				e.ConnectionRateBurstPerInterface = domain.InterfaceConnectionRateBurstCeiling - 1
			},
			"dataplane.executor.connection-rate-burst-per-interface",
			domain.InterfaceConnectionRateBurstCeiling,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := prodExecutorCfg(ModeProduction)
			tc.lower(&c.Dataplane.Executor)

			err := c.ValidateExecutorProfile()
			require.Error(t, err, "стенд умеет меньше опубликованного — посадка обязана отказать")
			assert.Contains(t, err.Error(), tc.knob, "отказ обязан назвать ручку, которую чинить")
			assert.Contains(t, err.Error(), strconv.Itoa(tc.published),
				"отказ обязан назвать опубликованное число — иначе оператору нечего с ним сравнить")
		})
	}
}

// Ровно опубликованное — законно. Положительный контроль к отрицанию выше: без
// него оно зеленело бы и от проверки «строго больше».
func TestValidateExecutorProfile_Production_InterfaceCeilingExactlyPublished_Passes(t *testing.T) {
	c := prodExecutorCfg(ModeProduction)
	c.Dataplane.Executor.ConnectionLimitPerInterface = domain.InterfaceConnectionCeiling
	c.Dataplane.Executor.ConnectionRateLimitPerInterfacePerSecond = domain.InterfaceConnectionRateCeilingPerSecond
	c.Dataplane.Executor.ConnectionRateBurstPerInterface = domain.InterfaceConnectionRateBurstCeiling

	require.NoError(t, c.ValidateExecutorProfile(),
		"обещание звучит как граница, и ровно обещанное её удовлетворяет")
}

// Незаявленный темп и всплеск в боевом режиме — отказ, каждый со своей ручкой.
//
// Полярность та же, что у остальных гарантий профиля: ноль означает ОТСУТСТВИЕ
// объявления, а не «ограничения нет». Обратное умолчание давало бы посадке,
// забывшей объявить, тихое «умею всё».
func TestValidateExecutorProfile_Production_ZeroRateGuarantees_FailNamingEachKnob(t *testing.T) {
	cases := []struct {
		name  string
		unset func(*ExecutorProfileConfig)
		knob  string
	}{
		{"темп", func(e *ExecutorProfileConfig) { e.ConnectionRateLimitPerInterfacePerSecond = 0 },
			"dataplane.executor.connection-rate-limit-per-interface-per-second"},
		{"всплеск", func(e *ExecutorProfileConfig) { e.ConnectionRateBurstPerInterface = 0 },
			"dataplane.executor.connection-rate-burst-per-interface"},
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

// Dev не отвергается: датаплейна в нём нет вовсе, а требование к ПОСАДКЕ
// осмысленно только там, где исполнитель есть. Случай симметричен уже стоящим
// выше «Dev_…Passes» и держит границу между «негодное объявление» (любой режим) и
// «требование к посадке» (боевой).
func TestValidateExecutorProfile_Dev_InterfaceCeilingBelowPublished_Passes(t *testing.T) {
	c := prodExecutorCfg(ModeDev)
	c.Dataplane.Executor.ConnectionLimitPerInterface = domain.InterfaceConnectionCeiling - 1
	c.Dataplane.Executor.ConnectionRateLimitPerInterfacePerSecond = domain.InterfaceConnectionRateCeilingPerSecond - 1
	c.Dataplane.Executor.ConnectionRateBurstPerInterface = domain.InterfaceConnectionRateBurstCeiling - 1

	require.NoError(t, c.ValidateExecutorProfile())
}
