// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Boot-guard S3: если permission-map несёт хотя бы один ScopeFiltered RPC, его
// object-scope авторизация зависит от data-level list-filter. В production фильтр
// ОБЯЗАН быть включён и иметь резолвимый authorize/iam эндпоинт — иначе
// авторизация ScopeFiltered-RPC деградирует до header-trusted ownership
// (cross-project enumeration). Guard закрывает "helm default fail-open" residual.
//
// scopeFilteredRPCs передаётся снаружи (composition root извлекает из
// check.PermissionMap()), чтобы config не импортировал check — Validate остаётся
// чистой и без import-цикла.

// vpc8-C-12: production + ScopeFiltered RPC + list-filter выключен → отказ старта.
func TestValidateListFilter_Production_ScopeFiltered_FilterDisabled_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = false
	err := c.ValidateListFilter([]string{"/kacho.cloud.vpc.v1.NetworkService/List"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.list-filter.enabled=true is required")
	require.Contains(t, err.Error(), "production mode (production)")
	require.Contains(t, err.Error(), "/kacho.cloud.vpc.v1.NetworkService/List")
}

// vpc8-C-13: production-strict + ScopeFiltered + filter выключен → тот же отказ.
func TestValidateListFilter_ProductionStrict_ScopeFiltered_FilterDisabled_Fails(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = false
	err := c.ValidateListFilter([]string{"/svc/List"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.list-filter.enabled=true is required")
	require.Contains(t, err.Error(), "production mode (production-strict)")
}

// vpc8-C-14: production + ScopeFiltered + filter включён но без резолвимого
// authorize/iam эндпоинта → buildListFilter деградирует в passthrough → отказ.
func TestValidateListFilter_Production_ScopeFiltered_FilterEnabled_NoEndpoint_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "", true) // iam-endpoint пуст, breakglass чтобы S1 не мешал
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = ""
	err := c.ValidateListFilter([]string{"/svc/List"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list-filter")
	require.Contains(t, err.Error(), "endpoint")
}

// vpc8-C-15: production + ScopeFiltered + filter включён + authorize-endpoint задан → OK.
func TestValidateListFilter_Production_ScopeFiltered_FilterEnabled_WithAuthorizeEndpoint_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = "kacho-iam:9090"
	require.NoError(t, c.ValidateListFilter([]string{"/svc/List"}))
}

// vpc8-C-16: production + ScopeFiltered + filter включён + fallback на iam-endpoint
// (authorize-endpoint пуст, но iam-endpoint задан) → OK (buildListFilter resolvable).
func TestValidateListFilter_Production_ScopeFiltered_FilterEnabled_IAMEndpointFallback_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = ""
	require.NoError(t, c.ValidateListFilter([]string{"/svc/List"}))
}

// vpc8-C-17: НЕТ ScopeFiltered RPC → guard no-op даже при выключенном фильтре
// (текущее состояние карты после Round-1: NetworkService/List не ScopeFiltered).
func TestValidateListFilter_NoScopeFiltered_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = false
	require.NoError(t, c.ValidateListFilter(nil))
	require.NoError(t, c.ValidateListFilter([]string{}))
}

// vpc8-C-18: dev-режим гардрейлом не затронут (dev может гонять unfiltered).
func TestValidateListFilter_Dev_NoGuard(t *testing.T) {
	var c Config
	c.AuthN.Mode = ModeDev
	c.AuthZ.ListFilter.Enabled = false
	require.NoError(t, c.ValidateListFilter([]string{"/svc/List"}))
}

// --- Включён ≠ решает. Для ScopeFiltered RPC фильтр — ЕДИНСТВЕННОЕ место, где
// вызывающего сверяют с объектами: per-RPC Check по такому методу не задаётся
// вовсе. Значит требование к нему не «включён», а «включён И отказывает при
// сбое»: на мягком проходе первая же ошибка соседа возвращает нефильтрованный
// список, то есть авторизация снимается целиком и молча. ---

// vpc8-C-19: production + ScopeFiltered RPC + фильтр включён, но мягкий проход →
// отказ старта.
func TestValidateListFilter_Production_ScopeFiltered_FailOpen_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = "kacho-iam:9090"
	c.AuthZ.ListFilter.FailOpen = true
	err := c.ValidateListFilter([]string{"/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.list-filter.fail-open")
	require.Contains(t, err.Error(), "production mode (production)")
	// Гард обязан назвать, ЧТО именно останется без авторизации.
	require.Contains(t, err.Error(), "/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance")
}

// vpc8-C-20: production-strict — тот же отказ (любой IsProduction()).
func TestValidateListFilter_ProductionStrict_ScopeFiltered_FailOpen_Fails(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.FailOpen = true
	err := c.ValidateListFilter([]string{"/svc/List"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.list-filter.fail-open")
	require.Contains(t, err.Error(), "production mode (production-strict)")
}

// vpc8-C-21: fail-closed (default) → проходит. Контроль на ту же форму: гард
// обязан молчать на законной конфигурации, иначе он ловит форму, а не существо.
func TestValidateListFilter_Production_ScopeFiltered_FailClosed_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.FailOpen = false
	require.NoError(t, c.ValidateListFilter([]string{"/svc/List"}))
}

// vpc8-C-22: нет ScopeFiltered RPC → мягкий проход допустим (за такими List стоит
// per-RPC Check, фильтр там — сужение поверх, а не единственная авторизация).
func TestValidateListFilter_NoScopeFiltered_FailOpen_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kacho-iam:9091", false)
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.FailOpen = true
	require.NoError(t, c.ValidateListFilter(nil))
}

// vpc8-C-23: dev — гарда нет.
func TestValidateListFilter_Dev_FailOpen_NoGuard(t *testing.T) {
	var c Config
	c.AuthN.Mode = ModeDev
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.FailOpen = true
	require.NoError(t, c.ValidateListFilter([]string{"/svc/List"}))
}
