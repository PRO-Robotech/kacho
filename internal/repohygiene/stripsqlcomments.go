// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stripsqlcomments.go — снятие `--`-комментариев из текста SQL.
//
// # Откуда он здесь
//
// Помощник приехал из носителя гейта сравнения по колонкам
// (`readpathconcat_test.go`). Предметом того гейта был путь чтения службы
// доступа, ушедший вместе со службой в отдельный продукт, — гейт снят.
// Помощник пережил его своим предметом: его зовёт `quotaabsentauthority_test.go`.
//
// Зачем он нужен всякому разбору SQL по образцу: без него гейт краснеет на
// ОБЪЯСНЕНИИ собственного запрета — комментарий у исправленного места называет
// прежнюю форму дословно, и это правильно.
//
// Файл назван по тому, что он делает, а не по гейту, которого больше нет.
package repohygiene

import "strings"

// stripSQLComments — снятие `--`-комментариев внутри литерала.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
