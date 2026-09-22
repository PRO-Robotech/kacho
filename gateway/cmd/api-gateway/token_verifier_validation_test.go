// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"errors"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Тот же класс, что и у первичной установки прав: мягкий проход, не
// различающий НАСТРОЙКУ и СБОЙ.
//
// Проверяющий подпись собирается из конфигурации и только из неё —
// `NewJWTVerifier` отказывает исключительно на пустом адресе набора ключей и
// пустом издателе, к сети он на сборке не ходит. Значит его отказ повтором не
// исправится НИКОГДА: это настройка. Пока он поглощался предупреждением, край
// продолжал работу без проверяющего подпись — постоянная неправильная
// настройка становилась штатным режимом, и «ноль отказов за всю жизнь» было
// неотличимо от «контроль не собран вовсе».
//
// Классы окружения — те же, что у соседних стражей (`validateProductionAuthzConfig`,
// `validateProductionRevocationConfig`): послабление получают ТОЛЬКО явные
// dev-ярлыки; пустой и опечатанный ярлык — боевой класс.

func TestTokenVerifier_ProductionEnvRefusesToStartWhenNotConstructed(t *testing.T) {
	for _, env := range []string{"prod", "production", "production-strict", "staging", "prd", "live", ""} {
		t.Run("env="+env, func(t *testing.T) {
			err := validateProductionTokenVerifierConfig(env, errors.New("jwt verifier: JWKSURL is required"))
			require.Error(t, err,
				"в боевом классе окружения несобранный проверяющий подпись обязан ронять старт: "+
					"иначе край объявляет себя проверяющим и не проверяет")
			require.Contains(t, err.Error(), "refuse to start")
			require.Contains(t, err.Error(), "JWKSURL",
				"отказ обязан назвать причину, иначе оператор не поднимет стенд")
		})
	}
}

func TestTokenVerifier_DevEnvKeepsTheSoftPass(t *testing.T) {
	for _, env := range []string{"dev", "local", "test", "DEV", " local "} {
		t.Run("env="+env, func(t *testing.T) {
			require.NoError(t,
				validateProductionTokenVerifierConfig(env, errors.New("jwt verifier: JWKSURL is required")),
				"in-process dev-ярлык сохраняет прежний мягкий проход — предупреждение в main")
		})
	}
}

func TestTokenVerifier_ConstructedVerifierPassesEverywhere(t *testing.T) {
	// Положительный контроль в паре с отрицанием: без него «отвергнуто»
	// неотличимо от «отвергается всё».
	for _, env := range []string{"prod", "production", "", "dev", "local"} {
		require.NoError(t, validateProductionTokenVerifierConfig(env, nil),
			"собранный проверяющий подпись обязан проходить в любом окружении (env=%q)", env)
	}
}

// Страж, которого никто не зовёт, — форма без содержания. main() из теста не
// исполнить (он дозванивается до соседей и занимает порты), поэтому провязка
// утверждается там, где живёт: в исходнике композиционного корня — ровно как у
// соседнего admin_hop_wiring_test.go.
func TestCompositionRoot_FeedsTheVerifierErrorToTheGuard(t *testing.T) {
	require.Regexp(t,
		regexp.MustCompile(`validateProductionTokenVerifierConfig\(\s*cfg\.AppEnv,\s*jverr\s*\)`),
		compositionRoot(t),
		"композиционный корень обязан отдать стражу ИМЕННО ошибку сборки проверяющего подпись; "+
			"страж, которому её не передали, зелен всегда")
}

// У ВХОДА ЭТОГО СТРАЖА ЕСТЬ ПРОИЗВОДИТЕЛЬ — и он найден в дереве, а не выдуман.
//
// Проверка, чей вход никем не производится, не может упасть никогда: она
// выглядит защитой и ею не является. Поэтому здесь берётся НАСТОЯЩАЯ
// конфигурация края, а не подставленная ошибка.
//
// ПРОИЗВОДИТЕЛЬ ПЕРЕЕХАЛ, И ЭТО ЧАСТЬ ПРЕДМЕТА. Прежде им был скалярный пин
// издателя: вырожденное значение из одних косых черт непусто, поэтому «издатель
// задан» по любому взгляду на профиль, а после снятия хвостовых черт от него не
// оставалось ничего — и пустой издатель доезжал до конструктора проверяющего.
// Пин снят вместе с ветвью вывода, и вход этой формы стал непредставим.
//
// Класс никуда не делся, производитель у него прежний по существу — настройка,
// непустая как строка и пустая как перечень, — но живёт он теперь в ОБЪЯВЛЕНИИ
// приёма. Разница в пользу края: отказ наступает РАНЬШЕ конструктора, при
// разборе объявления, и потому называет оператору ту ручку, которую править.
func TestTokenVerifier_TheGuardsInputHasAProducer(t *testing.T) {
	produced := 0
	for _, issuers := range []string{" ", ",", " , , ", "\t"} {
		cfg := config.Config{
			AppEnv:       "production",
			APIDomain:    "kacho.local",
			TokenIssuers: issuers,
		}
		require.NotEmpty(t, issuers, "настройка НЕПУСТА — профиль выглядит заполненным")

		bindings, err := cfg.TokenAcceptance()
		require.Error(t, err,
			"вырожденный перечень %q принят: записей приёма %d — пустой перечень означает "+
				"«принимаем любого издателя»", issuers, len(bindings))
		require.Contains(t, err.Error(), "KACHO_API_GATEWAY_TOKEN_ISSUERS",
			"отказ обязан назвать ручку, которую оператору править")
		produced++

		require.Error(t, validateProductionTokenVerifierConfig("production", err),
			"и этот отказ обязан ронять старт в боевом классе окружения")
	}
	require.Positive(t, produced,
		"ноль произведённых входов означал бы стража, который не может упасть")

	// ЗАКОННЫЙ БЛИЗНЕЦ: против ряда выше меняется РОВНО ОДИН факт — перечень
	// даёт элемент. Без него проба зеленеет на разборе, отвергающем всё.
	twin := config.Config{
		AppEnv:             "production",
		APIDomain:          "kacho.local",
		TokenIssuers:       "https://kaname.kacho.local",
		TokenIssuerKeySets: "https://kaname.kacho.local=https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json",
	}
	twinBindings, twinErr := twin.TokenAcceptance()
	require.NoError(t, twinErr, "законное объявление отвергнуто")
	require.Len(t, twinBindings, 1)
	require.NoError(t, validateProductionTokenVerifierConfig("production", nil),
		"страж роняет старт там, где отказа не было")

	t.Logf("ОСМОТРЕНО вырожденных значений перечня: %d, произведено отказов разбора: %d; "+
		"законных близнецов: 1", produced, produced)
}
