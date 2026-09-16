// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_ceilings_test.go — ВЕЛИЧИНЫ ПОТОЛКОВ ОБЪЯВЛЯЕТ ПОСАДКА ДОМЕНА (приёмка
// `QUOTA-FATE-1`, стадия `S1`; сценарии `Q-02`, `Q-03`, `Q-07`, `Q-08`).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ КЛЮЧИ ФАЙЛА ПРОБУЮТСЯ ОТДЕЛЬНО ОТ СТРАЖА
//
// Опечатка в ключе (`quota.ceiling` вместо `quota.ceilings`) выглядит В ТОЧНОСТИ
// как «посадка величину не объявила»: viper незнакомый ключ игнорирует, поле
// остаётся незаданным, страж отказывает — и оператор, задавший ровно названное
// отказом, получает тот же отказ, не имея способа отличить свою ошибку от нашей.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// vpcCeilingsShapedYAML — форма файла, которой посадка объявляет ВОСЕМЬ величин
// каталога. Ноль стоит намеренно: он законная величина, и проба обязана его
// пронести.
const vpcCeilingsShapedYAML = `
quota:
  authority: not-deployed
  ceilings:
    address: 10
    cidr-group: 4
    gateway: 0
    network: 3
    network-interface: 12
    route-table: 5
    security-group: 6
    subnet: 9
`

// TestCeilingFileKeysArmTheFields — ключи файла ДОЕЗЖАЮТ до полей, включая явный
// ноль.
func TestCeilingFileKeysArmTheFields(t *testing.T) {
	clearLegacyEnv(t)
	cfg, err := Load(writeTempYAML(t, vpcCeilingsShapedYAML))
	require.NoError(t, err)

	stated := QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings)
	require.Len(t, stated, len(QuotaCeilingCatalog),
		"ключи файла обязаны доехать до КАЖДОГО поля каталога")
	require.Equal(t, int64(3), stated["vpc.network"])
	require.Equal(t, int64(0), stated["vpc.gateway"], "ноль обязан доехать как ноль")
	require.NoError(t, cfg.ValidateQuotaCeilings(), "полный каталог законен (Q-08)")
}

// TestCeilingEnvVarsArmTheFields — переменная, названная ТЕКСТОМ ОТКАЗА, доезжает
// до поля.
//
// Без этой пробы отказ выглядит исчерпывающим и не восстанавливает следующий
// шаг: оператор задаёт ровно названное и получает тот же отказ, потому что у
// ключа нет ни умолчания, ни привязки, а `AutomaticEnv` разрешает переменную
// только для ключа, который viper УЖЕ знает.
func TestCeilingEnvVarsArmTheFields(t *testing.T) {
	clearLegacyEnv(t)
	for _, k := range QuotaCeilingCatalog {
		t.Setenv(k.Env, "7")
	}
	cfg, err := Load("")
	require.NoError(t, err)

	stated := QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings)
	require.Len(t, stated, len(QuotaCeilingCatalog),
		"каждая переменная каталога обязана доехать до своего поля")
	for _, kind := range QuotaCeilingCatalog.Kinds() {
		require.Equal(t, int64(7), stated[kind], "вид %s", kind)
	}
}

// TestEmptyCeilingSetIsLawful — ПЕРВЫЙ близнец (`Q-07`): посадка не объявляет ни
// одной величины, процесс поднимается, страж молчит.
func TestEmptyCeilingSetIsLawful(t *testing.T) {
	clearLegacyEnv(t)
	cfg, err := Load(writeTempYAML(t, "quota:\n  authority: not-deployed\n"))
	require.NoError(t, err)
	require.Empty(t, QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings))
	require.NoError(t, cfg.ValidateQuotaCeilings(), "пустое множество законно (Q-07)")
}

// TestPartialCeilingSetRefusesStartNamingEveryUnstatedKind — `Q-02`: частично
// объявленный набор роняет старт и называет КАЖДЫЙ необъявленный вид с его
// ручкой, а не первый из них.
func TestPartialCeilingSetRefusesStartNamingEveryUnstatedKind(t *testing.T) {
	clearLegacyEnv(t)
	// Семь величин из восьми; не объявлен `vpc.gateway` — ровно вход `Q-02`.
	partial := strings.Replace(vpcCeilingsShapedYAML, "    gateway: 0\n", "", 1)
	cfg, err := Load(writeTempYAML(t, partial))
	require.NoError(t, err)

	err = cfg.ValidateQuotaCeilings()
	require.Error(t, err, "частичный набор обязан ронять старт (Q-02)")
	msg := err.Error()
	require.Contains(t, msg, "vpc.gateway")
	require.Contains(t, msg, "quota.ceilings.gateway")
	require.Contains(t, msg, "KACHO_VPC_QUOTA__CEILINGS__GATEWAY")
	// Ни одно умолчание не подставлено ни одному из семи объявленных.
	stated := QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings)
	require.Len(t, stated, len(QuotaCeilingCatalog)-1)
	require.Equal(t, int64(3), stated["vpc.network"])

	// Агрегатор старта обязан нести этот же вердикт: проверка, не попавшая в
	// него, есть ловушка — она выглядит как часть «полной проверки старта».
	require.Error(t, cfg.ValidateBoot(MTLSConfig{}))
}

// TestQuotaCeilingCatalogShapeIsSound — таблица величин судится сама: непуста,
// виды и ключи не повторяются, у каждой записи есть доступ к полю и довод.
func TestQuotaCeilingCatalogShapeIsSound(t *testing.T) {
	require.NoError(t, QuotaCeilingCatalog.Shape())
	for _, k := range QuotaCeilingCatalog {
		require.True(t, strings.HasPrefix(k.Kind, "vpc."),
			"вид %s не принадлежит домену vpc", k.Kind)
	}
}
