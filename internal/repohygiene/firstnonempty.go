// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// firstnonempty.go — первый непустой элемент перечня.
//
// # Откуда он здесь
//
// Помощник приехал из носителя гейта полноты перечислений набора модулей
// службы доступа (`clienttruth_iam_moduleset.go`). Предметом того гейта был
// пакет `services/iam/internal/authzmap`, ушедший вместе со службой в
// отдельный продукт, — гейт снят. Помощник пережил его своим предметом: его
// зовёт `clienttruth_requestbody.go`.
//
// Файл назван по тому, что он делает, а не по гейту, которого больше нет.
package repohygiene

// firstNonEmpty — первый непустой элемент, либо пустая строка.
func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
