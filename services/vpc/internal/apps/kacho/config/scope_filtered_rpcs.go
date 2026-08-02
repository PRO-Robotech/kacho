// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/check"
)

// scopeFilteredRPCs — методы карты прав, с которых снят per-RPC Check: их
// object-scope авторизация целиком переехала на данные (сужение прочитанной
// страницы фильтром видимости).
//
// Карта читается ЗДЕСЬ, а не передаётся композиционным корнем. Формулировка и
// довод взяты у storage (services/storage/internal/config/validate.go), где тот
// же страж уже устроен так: «страж, которому список нужно вручить, no-op'ится на
// пустом аргументе, и „забыли передать“ внешне неотличимо от „нечего защищать“».
// Здесь это было не гипотезой: страж начинался с `len(scopeFilteredRPCs) == 0 ||`,
// то есть пустой аргумент снимал ВСЕ его проверки, и отличить «карта пуста» от
// «вызывающий не передал список» было нечем ни в коде, ни в логе.
//
// Пакет check зависимостей на config не имеет, поэтому import-цикла нет
// (проверяется сборкой; ровно то же соотношение у storage).
func scopeFilteredRPCs() []string {
	return check.ScopeFilteredRPCs()
}
