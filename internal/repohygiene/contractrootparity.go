// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Перечень корней дерева контрактов объявлен ДВАЖДЫ: `pkg/contractroot.Roots`
// (Go) и `KACHO_PROTO_ROOTS` (оболочка). Вторая копия НЕИЗБЕЖНА — оболочка не
// может импортировать Go-пакет, — но её расхождение с первой обязано КРАСНЕТЬ,
// а сегодня оно молчит (#2339, смежный предмет).
//
// Чем расхождение опасно: обе стороны отбирают ПОПУЛЯЦИЮ. Корень, известный
// одной и неизвестный другой, даёт не отказ, а СУЖЕНИЕ обхода — то есть ровно
// то молчание, ради которого заведён `contractrootliteral.go` и его скриптовый
// близнец. Перечень, разошедшийся с соседним, — это литерал корня, только
// растянутый на два файла.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧИТАЕТСЯ ПРИСВАИВАНИЕ, А НЕ УПОМИНАНИЕ
//
// Имя `KACHO_PROTO_ROOTS` встречается в оболочке двадцать с лишним раз, и
// БОЛЬШИНСТВО вхождений — комментарии, объясняющие сам перечень, плюс обходы
// `"${KACHO_PROTO_ROOTS[@]}"`. Проверка по подстроке нашла бы объяснение и
// покраснела бы на нём — тот же класс, который весь этот корпус ловит. Поэтому
// распознаётся ПРИСВАИВАНИЕ массива, и только оно.

// contractRootShellAssign — объявление массива корней в оболочке.
//
// Форма закрыта намеренно: она называет то, как перечень записан в этом дереве.
// Запись в другой форме (`declare -a`, сборка в цикле) распознана НЕ будет, и
// это не молчание, а отказ: ноль присваиваний роняет пробу.
var contractRootShellAssign = regexp.MustCompile(`(?m)^[ \t]*KACHO_PROTO_ROOTS=\(([^)]*)\)`)

// ContractRootParityCensus — объём осмотренного.
type ContractRootParityCensus struct {
	FilesRead   int
	Mentions    int
	Assignments int
	ShellRoots  []string
	GoRoots     []string
}

func (c ContractRootParityCensus) String() string {
	return fmt.Sprintf(
		"файлов оболочки прочитано %d · упоминаний имени %d · ПРИСВАИВАНИЙ %d · "+
			"перечень оболочки %v · перечень Go %v",
		c.FilesRead, c.Mentions, c.Assignments, c.ShellRoots, c.GoRoots)
}

// ParseShellContractRoots — перечень корней из ПРИСВАИВАНИЯ в тексте оболочки.
//
// Возвращает (nil, false), если присваивания в тексте нет: вызывающий обязан
// отличить «перечень пуст» от «перечня не нашли», а не свести оба к пустому
// срезу.
func ParseShellContractRoots(src string) ([]string, bool) {
	m := contractRootShellAssign.FindStringSubmatch(src)
	if m == nil {
		return nil, false
	}
	var out []string
	for _, f := range strings.Fields(m[1]) {
		f = strings.Trim(f, `"'`)
		if f != "" {
			out = append(out, f)
		}
	}
	return out, true
}
