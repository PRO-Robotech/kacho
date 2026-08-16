// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// executor_profile_chart_test.go — ключи профиля исполнителя, которые рендерит
// чарт, суть те самые ключи, которые читает загрузчик.
//
// Соседняя проба в каталоге чарта утверждает, что ключи ОБЪЯВЛЕНЫ и что шаблон на
// них ссылается. Не утверждала ничего ровно та треть, где ошибка молчалива: чарт
// доставляет настройки ФАЙЛОМ, и ключ, написанный в нём с опечаткой
// (`overlapping-tenant-address` вместо `…-addresses`), viper просто игнорирует —
// процесс поднимается с НЕобъявленным профилем, а на боевой посадке это отказ
// старта, причину которого оператор будет искать в values.yaml, где всё написано
// верно.
//
// Поэтому имена ключей берутся ИЗ ШАБЛОНА, а не из собственного литерала: копия
// пути в пробе согласуется сама с собой и молчит ровно тогда, когда чарт разошёлся
// с загрузчиком. Действия шаблона снимаются (renderedConfigTree), helm не нужен.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// executorFileKeys — ключи файла настроек, которые читает загрузчик. Перечень
// закрытый: он и есть контракт между шаблоном и структурой настроек.
var executorFileKeys = []string{
	"overlapping-tenant-addresses",
	"state-tracking-families",
	"named-set-reference-in-rule",
	"guaranteed-payload-bytes",
	"guaranteed-bandwidth-per-interface-mbps",
	"connection-limit-per-interface",
	"tenant-settable-bandwidth-limit",
}

// TestChartRendersTheExecutorProfileKeysTheLoaderReads — шаблон рендерит секцию
// и все её ключи.
func TestChartRendersTheExecutorProfileKeysTheLoaderReads(t *testing.T) {
	tree := renderedConfigTree(t, vpcConfigMapTemplate, "config.yaml: |")

	dataplane, ok := tree["dataplane"].(map[string]any)
	require.True(t, ok,
		"чарт обязан рендерить секцию `dataplane` файла настроек: без неё профиль исполнителя "+
			"не доезжает до процесса вовсе, и боевая посадка не поднимается ни на одном профиле")
	executor, ok := dataplane["executor"].(map[string]any)
	require.True(t, ok, "чарт обязан рендерить `dataplane.executor`")

	for _, k := range executorFileKeys {
		_, ok := executor[k]
		assert.True(t, ok,
			"чарт обязан рендерить ключ `dataplane.executor.%s` — именно его читает загрузчик; "+
				"ключ с другим именем viper молча игнорирует, и профиль остаётся необъявленным", k)
	}
	t.Logf("перепись: ключей в перечне %d, ключей в снятом блоке шаблона %d",
		len(executorFileKeys), len(executor))
}

// TestExecutorProfileFileKeysArmTheFields — вторая половина утверждения: путь из
// чарта не просто существует, а ЧИТАЕТСЯ. Файл подаётся загрузчику целиком, как
// его подаёт чарт.
func TestExecutorProfileFileKeysArmTheFields(t *testing.T) {
	body := "" +
		"dataplane:\n" +
		"  executor:\n" +
		"    overlapping-tenant-addresses: true\n" +
		"    state-tracking-families: [v4, v6]\n" +
		"    named-set-reference-in-rule: true\n" +
		"    guaranteed-payload-bytes: 1450\n" +
		"    guaranteed-bandwidth-per-interface-mbps: 1000\n" +
		"    connection-limit-per-interface: 65536\n" +
		"    tenant-settable-bandwidth-limit: true\n"
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	e := cfg.Dataplane.Executor
	assert.True(t, e.OverlappingTenantAddresses, "признак пересечения адресов обязан взводиться файлом")
	assert.Equal(t, []string{"v4", "v6"}, cfg.StateTrackingFamilies().Declared())
	assert.True(t, e.NamedSetReferenceInRule)
	assert.Equal(t, 1450, e.GuaranteedPayloadBytes)
	assert.Equal(t, 1000, e.GuaranteedBandwidthPerInterfaceMbps)
	assert.Equal(t, 65536, e.ConnectionLimitPerInterface)
	assert.True(t, e.TenantSettableBandwidthLimit)
}

// TestExecutorProfileDefaultsToUndeclared — умолчание загрузчика: профиль НЕ
// объявлен.
//
// Полярность выбрана осознанно и противоположна «удобной»: незаявленный
// исполнитель не считается способным. Иначе посадка, забывшая объявить профиль,
// получала бы «умеет всё» молча — то есть ровно то состояние, ради исключения
// которого страж и написан.
func TestExecutorProfileDefaultsToUndeclared(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)

	e := cfg.Dataplane.Executor
	assert.False(t, e.OverlappingTenantAddresses)
	assert.False(t, cfg.StateTrackingFamilies().IsDeclared())
	assert.False(t, e.NamedSetReferenceInRule)
	assert.Zero(t, e.GuaranteedPayloadBytes)
	assert.Zero(t, e.GuaranteedBandwidthPerInterfaceMbps)
	assert.Zero(t, e.ConnectionLimitPerInterface)
	assert.False(t, e.TenantSettableBandwidthLimit)
}

// TestExecutorProfileFromEnv — ручка достижима из окружения: значение задаёт
// чарт/профиль, а не литерал в коде. Без этого случая ключ может существовать в
// структуре и не приезжать из ConfigMap.
func TestExecutorProfileFromEnv(t *testing.T) {
	t.Setenv("KACHO_VPC_DATAPLANE__EXECUTOR__OVERLAPPING_TENANT_ADDRESSES", "true")
	t.Setenv("KACHO_VPC_DATAPLANE__EXECUTOR__STATE_TRACKING_FAMILIES", "v4,v6")
	t.Setenv("KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_LIMIT_PER_INTERFACE", "1024")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.True(t, cfg.Dataplane.Executor.OverlappingTenantAddresses)
	assert.Equal(t, []string{"v4", "v6"}, cfg.StateTrackingFamilies().Declared())
	assert.Equal(t, 1024, cfg.Dataplane.Executor.ConnectionLimitPerInterface)
}

// TestExecutorProfileLoneCommaFromEnvReadsAsUndeclared — вырожденный вход через
// НАСТОЯЩИЙ загрузчик, а не подстановкой поля.
//
// Одинокая запятая — канонический вход этого класса: сырая настройка непуста
// (после разбора по запятой в ней две записи), а семейств ноль. Пока предикат
// непустоты один и тот же у стража и у читателя, посадка на такой строке не
// поднимается; предикат по длине сырой настройки прочитал бы её как заполненную.
func TestExecutorProfileLoneCommaFromEnvReadsAsUndeclared(t *testing.T) {
	t.Setenv("KACHO_VPC_DATAPLANE__EXECUTOR__STATE_TRACKING_FAMILIES", ",")

	cfg, err := config.Load("")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Dataplane.Executor.StateTrackingFamilies,
		"сырая настройка обязана быть непустой — иначе у случая нет предмета "+
			"(разбор по запятой даёт две пустые записи)")
	assert.False(t, cfg.StateTrackingFamilies().IsDeclared(),
		"единственный предикат непустоты обязан прочитать одинокую запятую как «не объявлено»")
}

// TestExecutorProfileRateKnobsReachableFromEnv — темп и всплеск ДОЕЗЖАЮТ из
// окружения (kacho#290).
//
// Случай заведён не для симметрии, а по факту промаха: разбор настроек резолвит
// переменную только для ключа, который ему уже известен из умолчаний. Поле в
// структуре ключом его не делает — величина остаётся нулём при заданной
// оператором переменной, и остаётся МОЛЧА. Ровно это и произошло: обе ручки
// приехали в чарт и в структуру, умолчаний не получили, и боевой стенд отказывал
// в старте, называя ручку, которую оператор уже задал.
//
// Числа заведомо не круглые и разные между собой: одинаковые не отличили бы
// «приехало своё» от «приехало соседнее».
func TestExecutorProfileRateKnobsReachableFromEnv(t *testing.T) {
	t.Setenv("KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_LIMIT_PER_INTERFACE_PER_SECOND", "4321")
	t.Setenv("KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_BURST_PER_INTERFACE", "17654")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, 4321, cfg.Dataplane.Executor.ConnectionRateLimitPerInterfacePerSecond)
	assert.Equal(t, 17654, cfg.Dataplane.Executor.ConnectionRateBurstPerInterface)
}
