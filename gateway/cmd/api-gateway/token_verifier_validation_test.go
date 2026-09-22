// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
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
	b, err := os.ReadFile("main.go")
	require.NoError(t, err)
	require.Regexp(t,
		regexp.MustCompile(`validateProductionTokenVerifierConfig\(\s*cfg\.AppEnv,\s*jverr\s*\)`),
		string(b),
		"композиционный корень обязан отдать стражу ИМЕННО ошибку сборки проверяющего подпись; "+
			"страж, которому её не передали, зелен всегда")
}

// У ВХОДА ЭТОГО СТРАЖА ЕСТЬ ПРОИЗВОДИТЕЛЬ — и он найден в дереве, а не выдуман.
//
// Проверка, чей вход никем не производится, не может упасть никогда: она
// выглядит защитой и ею не является. Поэтому здесь берётся НАСТОЯЩАЯ
// конфигурация края, НАСТОЯЩИЙ разбор объявления приёма и НАСТОЯЩИЙ
// конструктор проверяющего подпись, а не подставленная ошибка.
//
// Производитель — незаявленный адресат. Прежде им служил и вырожденный
// скалярный пин издателя; пин снят вместе с выводом издателя из домена, и
// записи приёма теперь строит только объявленный перечень, который разбор
// пропускает лишь годным. ГРАНИЦА НАЗВАНА: в боевом классе окружения тот же
// вход раньше отвергает страж адресата (validateProductionTokenAudience), и
// до этого стража он там не доходит.
func TestTokenVerifier_TheGuardsInputHasAProducer(t *testing.T) {
	cfg := config.Config{
		AppEnv:             "dev",
		TokenIssuers:       "https://issuer.kacho.test",
		TokenIssuerKeySets: "https://issuer.kacho.test=https://kaname-internal.kacho.svc:9097/.well-known/jwks.json",
	}
	acceptance, err := cfg.TokenAcceptance()
	require.NoError(t, err, "объявление приёма обязано разобраться — иначе красное придёт не от адресата")
	records := make([]middleware.IssuerKeySet, 0, len(acceptance))
	for _, b := range acceptance {
		records = append(records, middleware.IssuerKeySet{
			Issuer: b.Issuer, KeySetURL: b.KeySetURL, TokenTypes: b.TokenTypes,
			TolerateAbsentTokenType: b.TolerateAbsentTokenType, ReadRevocation: b.ReadRevocation,
		})
	}
	require.NoError(t, validateProductionTokenAudience(cfg.AppEnv, cfg.DeclaredTokenAudience()),
		"в классе разработки незаявленный адресат страж адресата пропускает — вход доходит до конструктора")

	_, err = middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: records, ExpectedAudience: cfg.DeclaredTokenAudience(),
	})
	require.Error(t, err, "конструктор обязан отказать на незаявленном адресате")
	require.Contains(t, err.Error(), "audience", "отказ обязан прийти от АДРЕСАТА, а не от записей приёма")

	require.NoError(t, validateProductionTokenVerifierConfig(cfg.AppEnv, err),
		"в классе разработки этот отказ даёт мягкий проход с предупреждением")
	require.Error(t, validateProductionTokenVerifierConfig("production", err),
		"и тот же отказ обязан ронять старт в боевом классе окружения")
	t.Logf("ОСМОТРЕНО: записей приёма %d · производитель отказа конструктора — незаявленный адресат", len(records))
}
