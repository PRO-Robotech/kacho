// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
)

// underDeclaredRoot — объявлен ли пакет дескриптора под одним из НАШИХ корней
// контракта.
//
// Здесь стоял литерал `"kacho."`, и он был верен ровно до второго корня. После
// переезда службы доступа под собственный корень (KAN-PKG-1) её дескрипторы
// перестали попадать в обход — а обход этот строит таблицу REST-биндингов.
// Следствие наблюдалось пробами дословно: «биндинг GET /iam/v1/roles не
// резолвится», то есть публичная поверхность службы исчезала из таблицы целиком,
// молча и при зелёной сборке.
func underDeclaredRoot(pkg string) bool {
	for _, root := range contractroot.Roots {
		if strings.HasPrefix(pkg, root+".") {
			return true
		}
	}
	return false
}
