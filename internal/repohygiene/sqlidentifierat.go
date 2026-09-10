// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sqlidentifierat.go — чтение первого идентификатора SQL начиная с позиции.
//
// # Откуда он здесь
//
// Помощник приехал из носителя гейта словаря алгоритма ключа
// (`keyalgorithmdictionary.go`). Предметом того гейта были миграции службы
// доступа, и вместе со службой они ушли в отдельный продукт — гейт снят.
// Помощник пережил его своим предметом: его зовёт `clientexpiryimmutable.go`,
// который со службой доступа не связан.
//
// Файл назван по тому, что он делает, а не по гейту, которого больше нет.
package repohygiene

// sqlIdentifierAt — первый идентификатор начиная с позиции i.
func sqlIdentifierAt(s string, i int) string {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	start := i
	for i < len(s) && (s[i] == '_' || s[i] == '.' ||
		(s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') ||
		(s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	return s[start:i]
}
