// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// ratelimit_chart_test.go — ключи, которыми чарт объявляет величины допуска,
// обязаны быть теми самыми ключами, которые читает загрузчик.
//
// Соседние пробы утверждают ДВЕ трети цепочки: что умолчание — «не объявлено» и
// что страж отказывает боевой посадке. Не утверждала ничего ровно та треть, где
// ошибка молчалива: чарт доставляет настройки ФАЙЛОМ, и ключ, написанный в нём с
// опечаткой (`in-flight-limit` вместо `in-flight`), viper просто игнорирует —
// боевая посадка тогда не поднимается, а причину оператор будет искать в
// values.yaml, где всё написано верно.
//
// Поэтому имена ключей берутся ИЗ ШАБЛОНА, а не из собственного литерала: копия
// пути в пробе согласуется сама с собой и молчит ровно тогда, когда чарт
// разошёлся с загрузчиком. Действия шаблона снимаются (renderedConfigTree), helm
// не нужен — проба, требующая внешнего инструмента, пропускается там, где его
// нет, а пропускающаяся проба на измерении «ключа нет вовсе» бесполезна по
// построению.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// vpcChartValues — умолчания чарта. Читаются как обычный YAML: helm здесь не
// нужен и не должен быть нужен — проба, требующая внешнего инструмента,
// пропускается там, где его нет.
const vpcChartValues = "../../../../deploy/values.yaml"

// rateLimitAxes — четыре оси, которые обязан рендерить КАЖДЫЙ листенер.
// Перечень один на пробу: два его описания разъехались бы на первой же новой оси.
var rateLimitAxes = []string{"read-per-sec", "mutation-per-sec", "burst-factor", "in-flight"}

// TestChartRendersTheRateLimitKeysTheLoaderReads — шаблон рендерит все восемь
// ключей (четыре оси × два листенера).
//
// Перепись печатается через число осмотренных ключей: «ноль находок» обязано быть
// отличимо от «ноль прочитанного», иначе проба, разъехавшаяся с шаблоном, молчит.
func TestChartRendersTheRateLimitKeysTheLoaderReads(t *testing.T) {
	tree := renderedConfigTree(t, vpcConfigMapTemplate, "config.yaml: |")

	apiServer, ok := tree["api-server"].(map[string]any)
	require.True(t, ok,
		"чарт обязан рендерить секцию `api-server` файла настроек")
	rateLimit, ok := apiServer["rate-limit"].(map[string]any)
	require.True(t, ok,
		"чарт обязан рендерить ключ `api-server.rate-limit` — именно его читает загрузчик "+
			"(APIServerConfig.RateLimit, mapstructure `rate-limit`); ключ с другим именем viper "+
			"молча игнорирует, и боевая посадка не поднимается с причиной, которой нет в values.yaml")

	seen := 0
	for _, listener := range []string{"public", "internal"} {
		section, ok := rateLimit[listener].(map[string]any)
		require.True(t, ok,
			"чарт обязан рендерить листенер %q: у публичного и внутреннего РАЗНЫЕ вызывающие "+
				"и разные величины, поэтому объявленный один не покрывает другого", listener)
		for _, axis := range rateLimitAxes {
			_, ok := section[axis]
			require.True(t, ok, "чарт обязан рендерить ось `api-server.rate-limit.%s.%s`", listener, axis)
			seen++
		}
	}
	require.Equal(t, len(rateLimitAxes)*2, seen,
		"осмотрено ключей: %d — если их меньше восьми, проба читает не тот блок шаблона", seen)
}

// TestRateLimitFileKeysArmTheFields — вторая половина утверждения: путь из чарта
// не просто существует, а ЧИТАЕТСЯ. Файл подаётся загрузчику целиком, как его
// подаёт чарт.
func TestRateLimitFileKeysArmTheFields(t *testing.T) {
	body := "" +
		"api-server:\n" +
		"  rate-limit:\n" +
		"    public:\n" +
		"      read-per-sec: 100\n" +
		"      mutation-per-sec: 20\n" +
		"      burst-factor: 5\n" +
		"      in-flight: 16\n" +
		"    internal:\n" +
		"      read-per-sec: 1000\n" +
		"      mutation-per-sec: 500\n" +
		"      burst-factor: 5\n" +
		"      in-flight: 256\n"
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	pub := cfg.PublicAdmissionLimits()
	require.True(t, pub.IsDeclared(), "ключи из чарта обязаны объявлять величины публичного листенера")
	assert.Equal(t, float64(100), pub.ReadPerSec)
	assert.Equal(t, float64(20), pub.MutationPerSec)
	assert.Equal(t, float64(5), pub.BurstFactor)
	assert.Equal(t, 16, pub.InFlight)

	internal := cfg.InternalAdmissionLimits()
	require.True(t, internal.IsDeclared())
	assert.Equal(t, float64(1000), internal.ReadPerSec)
	assert.Equal(t, 256, internal.InFlight)
}

// TestRateLimitDefaultsToUndeclared — умолчание загрузчика: величины НЕ объявлены.
//
// Полярность выбрана осознанно и противоположна «удобной»: посадка, забывшая
// объявить величины, не получает «не ограничиваем» молча — она не поднимается
// (ValidateRequestRateLimits). Умолчание с готовыми числами было бы хуже вдвойне:
// предел исполняется ведром В ПРОЦЕССЕ, поэтому вписанное число описывало бы стенд
// с одной репликой и выглядело бы работающей защитой на всех остальных.
func TestRateLimitDefaultsToUndeclared(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)

	assert.False(t, cfg.PublicAdmissionLimits().IsDeclared())
	assert.False(t, cfg.InternalAdmissionLimits().IsDeclared())
	assert.True(t, cfg.PublicAdmissionLimits().IsBlank(),
		"незаданные величины обязаны читаться как канонический ноль, а не как негодное объявление: "+
			"иначе dev-фикстура не поднимется вовсе")
	assert.Empty(t, cfg.PublicAdmissionLimits().Unusable())
}

// TestRateLimitFromEnv — ручка достижима из окружения: значение задаёт профиль
// посадки, а не литерал в коде.
//
// Без этого случая ключ может существовать в структуре и не приезжать из
// ConfigMap: viper подхватывает переменную окружения только для ИЗВЕСТНОГО ключа,
// а известным его делает SetDefault в defaults.go.
func TestRateLimitFromEnv(t *testing.T) {
	t.Setenv("KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__READ_PER_SEC", "250")
	t.Setenv("KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__MUTATION_PER_SEC", "40")
	t.Setenv("KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__BURST_FACTOR", "3")
	t.Setenv("KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__IN_FLIGHT", "24")

	cfg, err := config.Load("")
	require.NoError(t, err)

	pub := cfg.PublicAdmissionLimits()
	require.True(t, pub.IsDeclared())
	assert.Equal(t, float64(250), pub.ReadPerSec)
	assert.Equal(t, 24, pub.InFlight)
	assert.False(t, cfg.InternalAdmissionLimits().IsDeclared(),
		"переменные одного листенера не должны объявлять другой")
}

// TestChartValuesDeclareAUsableAdmissionSet — числа, которые чарт объявляет
// умолчанием стенда, обязаны проходить стража старта.
//
// Проба читает `values.yaml` НАПРЯМУЮ (обычный YAML, helm не нужен) и подаёт
// прочитанное тому же стражу, который решает судьбу процесса. Без неё «чарт
// объявляет ключи» и «объявленное поднимается» оставались бы разными
// утверждениями: ключи на месте, а ноль или всплеск ниже единицы в значении
// уронили бы боевую посадку, и узнали бы мы об этом на стенде.
//
// Числа здесь НЕ сверяются с фикстурами проб: у стенда и у фикстуры разные
// предметы, и требование к ним одно — быть не снисходительнее продукта.
func TestChartValuesDeclareAUsableAdmissionSet(t *testing.T) {
	raw, err := os.ReadFile(vpcChartValues)
	require.NoError(t, err, "values.yaml чарта обязан быть там, где его ищет проба")

	// Имена в values.yaml — camelCase (соглашение чарта), в файле настроек —
	// через дефис (соглашение загрузчика). Перевод между ними делает шаблон;
	// здесь читается СТОРОНА ЧАРТА, поэтому теги свои, а не взятые у типа
	// настроек. Скопированный тип молча дал бы нули на всех полях — то есть
	// проба «умолчание чарта негодно» зеленела бы на любом values.yaml.
	type chartAxes struct {
		ReadPerSec     float64 `yaml:"readPerSec"`
		MutationPerSec float64 `yaml:"mutationPerSec"`
		BurstFactor    float64 `yaml:"burstFactor"`
		InFlight       int     `yaml:"inFlight"`
	}
	var values struct {
		APIServer struct {
			RateLimit struct {
				Public   chartAxes `yaml:"public"`
				Internal chartAxes `yaml:"internal"`
			} `yaml:"rateLimit"`
		} `yaml:"apiServer"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &values))

	toConfig := func(a chartAxes) config.AdmissionLimitsConfig {
		return config.AdmissionLimitsConfig{
			ReadPerSec:     a.ReadPerSec,
			MutationPerSec: a.MutationPerSec,
			BurstFactor:    a.BurstFactor,
			InFlight:       a.InFlight,
		}
	}

	var cfg config.Config
	cfg.AuthN.Mode = config.ModeProduction
	cfg.APIServer.RateLimit.Public = toConfig(values.APIServer.RateLimit.Public)
	cfg.APIServer.RateLimit.Internal = toConfig(values.APIServer.RateLimit.Internal)

	// Предпосылка: чтение чарта что-то нашло. Ноль означал бы, что проба читает не
	// тот файл или не тот ключ, и её вердикт ничего не доказывает.
	require.NotZero(t, values.APIServer.RateLimit.Public.ReadPerSec,
		"проба не прочитала из values.yaml ни одного числа: она смотрит не туда")

	require.NoError(t, cfg.ValidateRequestRateLimits(),
		"умолчание чарта обязано поднимать боевую посадку: профиль, который не проходит стража, "+
			"роняет стенд, а причину оператор ищет в values.yaml, где всё написано")

	// Внутренний листенер — заведомо выше публичного. Это решение, а не
	// косметика: ограничитель, задушивший наш собственный поток намерения,
	// воспроизводит заклинивание головы очереди.
	require.Greater(t, cfg.InternalAdmissionLimits().ReadPerSec, cfg.PublicAdmissionLimits().ReadPerSec)
	require.Greater(t, cfg.InternalAdmissionLimits().InFlight, cfg.PublicAdmissionLimits().InFlight)
}
