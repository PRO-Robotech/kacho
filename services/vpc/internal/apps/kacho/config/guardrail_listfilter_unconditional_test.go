// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Три свойства одного стража, и каждое было отдельной дырой в одном условии
// `len(scopeFilteredRPCs) == 0 || !c.AuthN.Mode.IsProduction()`.
//
// Формулировка взята у соседа, а не изобретена здесь: nlb
// (internal/authzfilter/subject.go) записал, что привязка контроля к тому, что
// он же и защищает, означает «контроль существует ровно до первой конфигурации,
// которая его не включила». storage (internal/config/validate.go) записал
// зеркальную половину: «страж, которому список нужно вручить, no-op'ится на
// пустом аргументе, и „забыли передать“ внешне неотличимо от „нечего защищать“».
//
// Здесь сходятся обе: список больше не условие, режим больше не условие.

// Страж читает карту САМ, и это его структурное свойство, а не деталь стиля:
// пока список передавался снаружи, «вызывающий передал пустое» было неотличимо
// от «карта пуста», и оба снимали проверку целиком.
//
// Утверждение о ПЕРЕПИСИ: карта на этой ревизии несёт хотя бы одну
// ScopeFiltered-метку. Без него «страж прошёл» было бы неотличимо от «стражу
// нечего было смотреть» — тот же ноль с двумя разными смыслами.
func TestScopeFilteredRPCs_AreReadByTheGuardItself(t *testing.T) {
	marks := scopeFilteredRPCs()
	require.NotEmpty(t, marks,
		"карта обязана нести ScopeFiltered-метки: иначе условная клауза мягкого прохода "+
			"не проверяется ни одним случаем, и её зелёное ничего не значит")

	// Мягкий проход запрещён, пока метки есть, — и отказ обязан назвать, ЧТО
	// именно останется без авторизации, иначе оператор снимет стража, а не ручку.
	c := prodCfg(ModeProduction, "kaname:9091")
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = "kaname:9090"
	c.AuthZ.ListFilter.FailOpen = true

	err := c.ValidateListFilter()
	require.Error(t, err)
	for _, m := range marks {
		require.Containsf(t, err.Error(), m, "отказ обязан назвать незащищённый RPC %q", m)
	}
}

// Пустой набор ScopeFiltered-меток НЕ снимает требование фильтра.
//
// Прежде страж выходил из себя первым же дизъюнктом, поэтому карта без единой
// метки означала «проверять нечего» — при том что публичные List сужаются
// фильтром независимо от меток: метка говорит лишь, что за RPC НЕ задаётся
// per-RPC Check, а не то, что сужение не нужно. Совпадение «меток нет» и
// «фильтр не нужен» никем не обеспечено, и в тот момент, когда метки уйдут из
// карты, страж молча перестанет требовать фильтр для семи публичных List.
func TestValidateListFilter_RequiresFilter_EvenWithNoScopeFilteredMarks(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.AuthZ.ListFilter.Enabled = false

	err := c.validateListFilterAgainst(nil)

	require.Error(t, err, "фильтр обязателен независимо от того, несёт ли карта ScopeFiltered-метки")
	require.Contains(t, err.Error(), "authz.list-filter.enabled=true is required")
}

// Тот же вопрос об адресе: включённый фильтр без резолвимого адреса даёт
// passthrough, то есть ту же нефильтрованную страницу, — и это тоже не зависит
// от наличия меток.
func TestValidateListFilter_RequiresEndpoint_EvenWithNoScopeFilteredMarks(t *testing.T) {
	c := prodCfg(ModeProduction, "") // адрес пуст: предмет — адрес фильтра, S1 сюда не вызывается
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = ""

	err := c.validateListFilterAgainst(nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint")
}

// Развёрнутый стенд в dev-посадке страж покрывает наравне с боевым.
//
// Прежде дизъюнкт `!c.AuthN.Mode.IsProduction()` выводил dev из-под стража, и
// комментарий у стража это заявлял прямым текстом («dev-режим гардом не
// затронут»). Директива (core rule #16) стенды не делит: dev-посадка допустима
// ТОЛЬКО в in-process фикстурах, а этот страж живёт в композиционном корне
// (cmd/vpc/main.go) — то есть исполняется исключительно на РАЗВЁРНУТОМ
// процессе, которому dev-посадка запрещена отдельным правилом. Фикстуры его не
// зовут вовсе, поэтому покрытие dev ничего в них не ломает.
func TestValidateListFilter_Dev_IsGuardedToo(t *testing.T) {
	c := prodCfg(ModeDev, "kaname:9091")
	c.AuthZ.ListFilter.Enabled = false

	err := c.ValidateListFilter()

	require.Error(t, err, "директива не делит стенды: страж старта покрывает dev наравне с production")
	require.Contains(t, err.Error(), "authz.list-filter.enabled=true is required")
}

// ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ к трём отказам выше.
//
// Без него «отказано» неотличимо от «отказывает всегда»: страж, вернувший
// ошибку на любой вход, прошёл бы все три отрицательных случая и не пропустил
// бы ни одной законной посадки. Форма та же, что у отказов, — меняется ровно
// то, что страж проверяет.
func TestValidateListFilter_LegitimateConfiguration_IsAccepted(t *testing.T) {
	for _, mode := range []Mode{ModeDev, ModeProduction, ModeProductionStrict} {
		c := prodCfg(mode, "kaname:9091")
		c.AuthZ.ListFilter.Enabled = true
		c.AuthZ.ListFilter.AuthorizeEndpoint = "kaname:9090"
		c.AuthZ.ListFilter.FailOpen = false

		require.NoErrorf(t, c.ValidateListFilter(),
			"законная посадка (%s) обязана проходить, иначе страж ловит форму, а не существо", mode)
	}
}

// Агрегатный валидатор старта несёт и этого стража.
//
// S3 стоял вне ValidateBoot с обоснованием «ему нужен список ScopeFiltered RPC из
// пакета check, который config не импортирует». Обоснование исчезло вместе с
// параметром: карту страж читает сам. Пока оно оставалось записанным, но неверным,
// агрегатный валидатор был ловушкой — тот, кто перевёл бы композиционный корень на
// него вместо явной пары, тихо остался бы без проверки фильтра.
func TestValidateBoot_IncludesTheListFilterGuard(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.AuthZ.ListFilter.Enabled = false // ровно одно ослабленное измерение
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = true
	m.IAMAuthzMTLS.Enable = true
	m.IAMProjectMTLS.Enable = true
	m.GeoMTLS.Enable = true

	err := c.ValidateBoot(m)

	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.list-filter.enabled=true is required",
		"агрегатный валидатор обязан нести S3, иначе он тихо слабее явной пары")
}
