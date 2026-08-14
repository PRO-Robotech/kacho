// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// reserved_prefixes_chart_test.go — ключ, которым чарт объявляет перечень
// служебных диапазонов, обязан быть тем самым ключом, который читает загрузчик.
//
// Соседние пробы утверждают ДВЕ трети цепочки: что умолчание — «не объявлено» и
// что переменная окружения доезжает до поля. Не утверждала ничего ровно та треть,
// где ошибка молчалива: чарт доставляет настройки ФАЙЛОМ, и ключ, написанный в
// нём с опечаткой (`reserved-prefix` вместо `reserved-prefixes`), viper просто
// игнорирует — боевая посадка тогда не поднимается, а причину оператор будет
// искать в values.yaml, где всё написано верно.
//
// Поэтому имя ключа берётся ИЗ ШАБЛОНА, а не из собственного литерала: копия пути
// в пробе согласуется сама с собой и молчит ровно тогда, когда чарт разошёлся с
// загрузчиком. Действия шаблона снимаются (renderedConfigTree), helm не нужен.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// TestChartRendersTheReservedPrefixesKeyTheLoaderReads — шаблон рендерит ключ.
func TestChartRendersTheReservedPrefixesKeyTheLoaderReads(t *testing.T) {
	tree := renderedConfigTree(t, vpcConfigMapTemplate, "config.yaml: |")

	dataplane, ok := tree["dataplane"].(map[string]any)
	require.True(t, ok,
		"чарт обязан рендерить секцию `dataplane` файла настроек: без неё перечень служебных "+
			"диапазонов не доезжает до процесса вовсе, и боевая посадка не поднимается ни на одном профиле")
	_, ok = dataplane["reserved-prefixes"]
	require.True(t, ok,
		"чарт обязан рендерить ключ `dataplane.reserved-prefixes` — именно его читает загрузчик "+
			"(DataplaneConfig.ReservedPrefixes, mapstructure `reserved-prefixes`); ключ с другим "+
			"именем viper молча игнорирует, и перечень остаётся необъявленным")
}

// TestReservedPrefixesFileKeyArmsTheField — вторая половина утверждения: путь из
// чарта не просто существует, а ЧИТАЕТСЯ. Файл подаётся загрузчику целиком, как
// его подаёт чарт.
func TestReservedPrefixesFileKeyArmsTheField(t *testing.T) {
	body := "" +
		"dataplane:\n" +
		"  reserved-prefixes:\n" +
		"    - 169.254.0.0/16\n" +
		"    - fe80::/10\n"
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	require.Equal(t, []string{"169.254.0.0/16", "fe80::/10"}, cfg.Dataplane.ReservedPrefixes)
	reserved := cfg.ReservedPrefixes()
	assert.True(t, reserved.IsDeclared(), "ключ из чарта обязан объявлять перечень")
	assert.Equal(t, 2, reserved.Len())
	assert.Empty(t, reserved.Rejected())
}

// TestReservedPrefixesDefaultsToUndeclared — умолчание загрузчика: перечень НЕ
// объявлен.
//
// Полярность выбрана осознанно и противоположна «удобной»: посадка, забывшая
// объявить перечень, не получает «ничего не зарезервировано» молча — она не
// поднимается (ValidateReservedPrefixes). Умолчание с готовым перечнем было бы
// хуже вдвойне: оно и описывало бы один стенд, и выглядело бы работающей защитой
// на всех остальных.
func TestReservedPrefixesDefaultsToUndeclared(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)

	assert.Empty(t, cfg.Dataplane.ReservedPrefixes)
	assert.False(t, cfg.ReservedPrefixes().IsDeclared())
}

// TestReservedPrefixesFromEnv — ручка достижима из окружения: значение задаёт
// профиль посадки, а не литерал в коде. Без этого случая ключ может существовать
// в структуре и не приезжать из ConfigMap.
func TestReservedPrefixesFromEnv(t *testing.T) {
	t.Setenv("KACHO_VPC_DATAPLANE__RESERVED_PREFIXES", "169.254.0.0/16,10.64.0.0/10")

	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, []string{"169.254.0.0/16", "10.64.0.0/10"}, cfg.Dataplane.ReservedPrefixes)
	assert.Equal(t, 2, cfg.ReservedPrefixes().Len())
}

// TestReservedPrefixesLoneCommaFromEnvReadsAsUndeclared — вырожденный вход через
// НАСТОЯЩИЙ загрузчик, а не подстановкой поля.
//
// Одинокая запятая — канонический вход этого класса: сырая настройка непуста
// (после разбора по запятой в ней две записи), а объявленных диапазонов ноль. Пока
// предикат непустоты один и тот же у стража и у читателя, посадка на такой строке
// не поднимается; предикат по длине сырой настройки прочитал бы её как заполненную.
func TestReservedPrefixesLoneCommaFromEnvReadsAsUndeclared(t *testing.T) {
	t.Setenv("KACHO_VPC_DATAPLANE__RESERVED_PREFIXES", ",")

	cfg, err := config.Load("")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Dataplane.ReservedPrefixes,
		"сырая настройка обязана быть непустой — иначе у случая нет предмета "+
			"(разбор по запятой даёт две пустые записи)")
	assert.False(t, cfg.ReservedPrefixes().IsDeclared(),
		"единственный предикат непустоты обязан прочитать одинокую запятую как «не объявлено»")
}
