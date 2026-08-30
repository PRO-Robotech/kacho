// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"
	"testing"
)

// Страж шифрования до базы читает строку подключения ТЕМ ЖЕ разбором, что и
// пул (задача продукта #1464).
//
// # ПРЕДМЕТ
//
// Здесь жил СВОЙ разбор DSN и СВОЙ перечень безопасных значений. Перечень
// расходился бы молча; разбор расходился уже — на строке, которую `url.Parse`
// не осиливает (пробел или управляющий символ в пароле — законное содержимое
// секрета). Запасной путь копии бил строку по пробелам и ловил `sslmode=`
// только в keyword-форме; URL — один токен, поэтому реальный `require` читался
// как «режим не задан», и исправная боевая посадка отвергалась.
//
// # ПОЧЕМУ ПАРА
//
// Одно отрицание зеленеет на страже, отвергающем всё: положительный контроль
// ниже утверждает вторую сторону той же оси.
func TestValidateProductionReadsTheSSLModeTheSameWayThePoolDoes(t *testing.T) {
	const insecureTransport = "insecure Postgres transport"

	t.Run("пароль с пробелом не мешает увидеть require", func(t *testing.T) {
		c := productionSecureConfig()
		c.Repository.Postgres.URL = "postgres://u:pa ss@h:5432/db?sslmode=require"

		err := c.Validate()
		if err != nil && strings.Contains(err.Error(), insecureTransport) {
			t.Fatalf("исправная посадка отвергнута: режим `require` объявлен в строке, "+
				"а страж его не увидел.\n%v", err)
		}
	})

	t.Run("положительный контроль: disable отвергается и на такой же строке", func(t *testing.T) {
		c := productionSecureConfig()
		c.Repository.Postgres.URL = "postgres://u:pa ss@h:5432/db?sslmode=disable"

		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), insecureTransport) {
			t.Fatalf("открытый канал до базы принят при боевой посадке: %v", err)
		}
	})
}
