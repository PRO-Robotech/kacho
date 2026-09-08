// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Страж шифрования до базы судит ИСХОД, а не намерение (задача продукта #1464).
//
// # ПРЕДМЕТ
//
// `sslmode` приходит в строку подключения ДВУМЯ путями: из поля настройки
// (`repository.postgres.ssl-mode`) и из сырого URL (`repository.postgres.url`).
// Заданный в URL режим поле НЕ перетирает — так устроен `composeDSN`. Значит
// страж, читающий поле, судит НАМЕРЕНИЕ оператора, а в пул уходит другое.
//
// Расхождение двустороннее, и обе стороны наблюдаемы:
//
//   - опасная: поле `require` при `sslmode=disable` в URL — страж пропускал, а
//     открытый канал состоялся бы. Ловил его только центральный дескриптор
//     посадки, и ловил ПОСЛЕ открытия пула, то есть после того, как пароль по
//     этому каналу уже ушёл;
//   - раздражающая: режим задан прямо в URL, поле пусто — страж отвергал старт
//     при исправной посадке.
//
// # ПОЧЕМУ ПАРА, А НЕ ОДНО ОТРИЦАНИЕ
//
// Отрицание в одиночку зеленеет на страже, отвергающем всё. Положительный
// контроль стоит рядом и утверждает вторую сторону той же оси.
func TestValidateProductionJudgesTheSSLModeThatReachesThePool(t *testing.T) {
	t.Run("режим из URL перевешивает поле: disable в URL отвергается", func(t *testing.T) {
		c := prodCfg(ModeProduction, "kaname:9091")
		// Поле объявляет безопасный режим…
		c.Repository.Postgres.SSLMode = "require"
		// …а в пул уходит открытый канал: composeDSN уже заданный sslmode не трогает.
		c.Repository.Postgres.URL = "postgres://u@h:5432/db?sslmode=disable"

		err := c.Validate()
		require.Error(t, err, "открытый канал до базы принят при боевой посадке")
		require.Contains(t, err.Error(), "ssl-mode must be one of require|verify-ca|verify-full")
		require.Contains(t, err.Error(), `got "disable"`,
			"отказ обязан называть режим, который РЕАЛЬНО уходит в пул, а не поле настройки")
	})

	t.Run("режим задан только в URL, поле пусто: старт проходит", func(t *testing.T) {
		c := prodCfg(ModeProduction, "kaname:9091")
		c.Repository.Postgres.SSLMode = ""
		c.Repository.Postgres.URL = "postgres://u@h:5432/db?sslmode=verify-full"

		require.NoError(t, c.Validate(),
			"исправная посадка отвергнута: режим объявлен в строке подключения, а страж смотрел в поле")
	})

	t.Run("положительный контроль: поле disable по-прежнему отвергается", func(t *testing.T) {
		c := prodCfg(ModeProduction, "kaname:9091")
		c.Repository.Postgres.SSLMode = "disable"

		err := c.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "ssl-mode must be one of require|verify-ca|verify-full")
	})
}
