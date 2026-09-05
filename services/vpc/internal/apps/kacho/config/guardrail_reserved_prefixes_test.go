// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// guardrail_reserved_prefixes_test.go — страж S6: перечень адресных диапазонов,
// которые посадка держит за собой.
//
// Часть адресного пространства обслуживает саму платформу (служебные адреса
// узлов, адреса служб внутри подсети, точка получения метаданных экземпляра).
// Подсеть арендатора поверх такого диапазона принимается контуром и не работает,
// причём отладка уходит в сеть. Перечень зависит от посадки, поэтому объявляется
// конфигурацией; а пустой перечень означает «не сужаем», а не «нечего сужать» —
// именно поэтому у него есть страж, а не умолчание.
//
// Случаи устроены как инъекция в обе стороны: законная боевая посадка (prodCfg с
// объявленным перечнем) обязана ПРОХОДИТЬ, и каждое отрицание ломает в ней ровно
// одну вещь. Без положительного контроля отрицания зеленели бы на страже, который
// отказывает всегда.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prodReservedCfg — законная боевая посадка. Отправная точка каждого отрицания.
//
// Само объявление живёт в общей фикстуре пакета (prodCfg), а не здесь: «законная
// боевая посадка» — одна величина, и второе её описание разошлось бы с первым на
// первом же новом требовании.
func prodReservedCfg(mode Mode) Config {
	return prodCfg(mode, "kaname.kacho.svc:9091")
}

// S6-01 (положительный контроль): объявленный перечень проходит.
func TestValidateReservedPrefixes_Production_Declared_Passes(t *testing.T) {
	require.NoError(t, prodReservedCfg(ModeProduction).ValidateReservedPrefixes())
	require.NoError(t, prodReservedCfg(ModeProductionStrict).ValidateReservedPrefixes())
}

// S6-02: перечень не объявлен → отказ старта, и отказ НАЗЫВАЕТ ручку — и ключ
// файла настроек, и переменную окружения. Текст читает оператор, которому стенд
// отказал в старте: без имени настройки стенд не поднять.
func TestValidateReservedPrefixes_Production_NotDeclared_Fails(t *testing.T) {
	c := prodReservedCfg(ModeProduction)
	c.Dataplane.ReservedPrefixes = nil

	err := c.ValidateReservedPrefixes()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.reserved-prefixes")
	assert.Contains(t, err.Error(), "KACHO_VPC_DATAPLANE__RESERVED_PREFIXES")
	assert.Contains(t, err.Error(), "mode production")

	// Текст печатается: его читает оператор, которому стенд отказал в старте, и
	// рецензировать диагностику надо по тому, что он увидит, а не по перечню
	// подстрок, которые утверждает случай.
	t.Logf("отказ старта, который увидит оператор:\n%v", err)
}

// S6-03: то же в strict — режим не смягчает (любой IsProduction).
func TestValidateReservedPrefixes_ProductionStrict_NotDeclared_Fails(t *testing.T) {
	c := prodReservedCfg(ModeProductionStrict)
	c.Dataplane.ReservedPrefixes = nil

	err := c.ValidateReservedPrefixes()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.reserved-prefixes")
	assert.Contains(t, err.Error(), "mode production-strict")
}

// S6-04: dev — внутрипроцессные фикстуры, у которых посадки нет вовсе, поэтому
// требования объявить перечень к ним не предъявляется.
//
// Это НЕ послабление развёрнутого стенда: любой РАЗВЁРНУТЫЙ стенд работает в
// боевом режиме (core rule #16). Отдельно: испорченное ОБЪЯВЛЕНИЕ отвергается и
// на dev — см. S6-06, где предмет не посадка, а невозможное значение.
func TestValidateReservedPrefixes_Dev_NotDeclared_Passes(t *testing.T) {
	c := prodCfg(ModeDev, "kaname.kacho.svc:9091")
	c.Dataplane.ReservedPrefixes = nil

	require.NoError(t, c.ValidateReservedPrefixes())
}

// S6-05 — ВЫРОЖДЕННЫЙ ВХОД: одинокая запятая.
//
// Сырая настройка НЕПУСТА (разбор по запятой даёт две пустые записи), а
// объявленных диапазонов ноль. Страж обязан читать ТОТ ЖЕ предикат непустоты, что
// читает путь запроса, — иначе «непусто для стража» и «пусто для проверки»
// уживутся в одной посадке.
func TestValidateReservedPrefixes_Production_LoneCommaReadsAsEmpty(t *testing.T) {
	c := prodReservedCfg(ModeProduction)
	c.Dataplane.ReservedPrefixes = []string{"", ""} // то, во что разбирается ","

	require.NotEmpty(t, c.Dataplane.ReservedPrefixes,
		"сырая настройка непуста — иначе у случая нет предмета")
	assert.False(t, c.ReservedPrefixes().IsDeclared(),
		"единственный предикат непустоты обязан прочитать это как «не объявлено»")

	err := c.ValidateReservedPrefixes()
	require.Error(t, err, "страж обязан читать ТОТ ЖЕ предикат, а не длину сырой настройки")
	assert.Contains(t, err.Error(), "dataplane.reserved-prefixes")
}

// S6-06: негодная запись отвергается в ЛЮБОМ режиме и НАЗЫВАЕТСЯ.
//
// Предмет здесь не посадка, а само объявление: опечатка не становится
// диапазоном ни в боевом режиме, ни в dev. Молча отброшенная запись оставила бы
// диапазон, который оператор считает зарезервированным, а контур — нет, и подсеть
// поверх него была бы принята.
func TestValidateReservedPrefixes_UnusableEntry_FailsInAnyMode(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodCfg(mode, "kaname.kacho.svc:9091")
		c.Dataplane.ReservedPrefixes = []string{"169.254.0.0/16", "10.0.0.0/33"}

		err := c.ValidateReservedPrefixes()
		require.Error(t, err, "режим %s: негодная запись обязана отвергаться", mode)
		assert.Contains(t, err.Error(), "10.0.0.0/33", "отказ обязан назвать саму запись")
		assert.Contains(t, err.Error(), "dataplane.reserved-prefixes", "и ручку, в которой она лежит")
	}
}

// S6-07: запись с непустыми host-битами отвергается, и отказ называет форму, в
// которой её надо переписать.
func TestValidateReservedPrefixes_HostBitsEntry_NamesTheNetworkAddress(t *testing.T) {
	c := prodReservedCfg(ModeProduction)
	c.Dataplane.ReservedPrefixes = []string{"10.0.0.1/24"}

	err := c.ValidateReservedPrefixes()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10.0.0.0/24")
}

// S6-08: запись, покрывающая всё семейство, отвергается как самопротиворечие —
// контуру не осталось бы ни одного адреса для выдачи, а увидеть это можно было бы
// только в трафике арендатора.
func TestValidateReservedPrefixes_WholeFamilyEntry_Fails(t *testing.T) {
	c := prodReservedCfg(ModeProduction)
	c.Dataplane.ReservedPrefixes = []string{"0.0.0.0/0"}

	err := c.ValidateReservedPrefixes()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0.0.0.0/0")

	// Положительный контроль: широкая, но не всеобъемлющая запись проходит.
	c.Dataplane.ReservedPrefixes = []string{"10.0.0.0/8"}
	require.NoError(t, c.ValidateReservedPrefixes())
}

// S6-09: страж входит в агрегатор.
//
// Проверка, не попавшая в ValidateBoot, становится ловушкой: агрегатор выглядит
// как «полная проверка старта», и тот, кто переведёт на него композиционный
// корень, тихо останется без забытой проверки. Требование держит ещё и гейт
// cmd/vpc/bootguards_wired_test.go — здесь та же связь утверждается исходом, а не
// разбором текста.
func TestValidateBoot_IncludesTheReservedPrefixesGuard(t *testing.T) {
	c := prodReservedCfg(ModeProduction)
	c.Dataplane.ReservedPrefixes = nil
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = true
	m.IAMAuthzMTLS.Enable = true
	m.IAMProjectMTLS.Enable = true
	m.GeoMTLS.Enable = true
	m.IAMRegisterMTLS.Enable = true
	c.AuthZ.ListFilter.Enabled = true

	err := c.ValidateBoot(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dataplane.reserved-prefixes",
		"ValidateBoot обязан агрегировать страж перечня: иначе он выглядит полной "+
			"проверкой старта и не является ею")
}
