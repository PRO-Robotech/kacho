// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"strings"
)

// Разбор локаторов строки формы в сквозных пробах консоли, вынесенный из гейта,
// чтобы проба инъекции могла подать сюда синтетический вход и доказать, что
// разбор умеет и краснеть, и молчать.
//
// ПРЕДМЕТ. `.ant-form-item` ВКЛАДЫВАЮТСЯ: составной контрол сам стоит в строке
// формы и рисует внутри неё по строке на под-контрол. Поэтому выражение вида
// «строка формы, содержащая такой-то текст» совпадает и с вложенной строкой, и
// с ОБЪЕМЛЮЩИМ блоком, а порядок обхода дерева ставит предка РАНЬШЕ потомка —
// значит `.first()` берёт объемлющий блок ВСЕГДА, когда вложенность есть.
//
// Промах не даёт отказа: он даёт действие над ЧУЖОЙ строкой, и увидеть это
// можно лишь по последствию через несколько шагов. В #636 проба балансировщика
// так выключала семейство, которое собиралась сохранить, — зелёной она не могла
// быть ни при каком состоянии продукта.

// FormRowLocatorFinding — одно выражение, выбирающее строку формы по тексту без
// отсечения вложенных строк.
type FormRowLocatorFinding struct {
	Line int
	Text string
}

// StripTSComments убирает построчные и блочные комментарии, оставляя строковые
// литералы нетронутыми.
//
// БЕЗ ЭТОГО ГЕЙТ КРАСНЕЕТ НА СОБСТВЕННОМ ОБЪЯСНЕНИИ: разбор этого самого класса
// написан в шапках помощников и называет `.ant-form-item` прозой. Проверка по
// сырому тексту не отличает код от комментария — записанный класс
// (`testing.md` §«Гейт на класс», п.4).
func StripTSComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	inLine, inBlock, inStr := false, false, byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				out.WriteByte(c)
			}
		case inBlock:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				i++
			} else if c == '\n' {
				// Перевод строки сохраняется: номера строк в находке обязаны
				// совпадать с номерами в файле, иначе координата уводит не туда.
				out.WriteByte(c)
			}
		case inStr != 0:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
			} else if c == inStr {
				inStr = 0
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			inLine = true
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			inBlock = true
			i++
		case c == '"' || c == '\'' || c == '`':
			inStr = c
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

const formRowSelector = `".ant-form-item"`

// FindUnguardedFormRowLocators — выражения, которые выбирают строку формы ПО
// ТЕКСТУ и не отсекают вложенные строки.
//
// Единица разбора — ОПЕРАТОР (до `;`), а не строка файла: цепочка локатора
// переносится по строкам, и построчный разбор объявил бы находкой каждое звено.
// Возвращает также число рассмотренных операторов — «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func FindUnguardedFormRowLocators(src string) (findings []FormRowLocatorFinding, examined int) {
	code := StripTSComments(src)
	line := 1
	for _, stmt := range strings.SplitAfter(code, ";") {
		// Координата — строка, где стоит САМ СЕЛЕКТОР, а не где начался
		// оператор: разрез идёт по `;`, поэтому кусок открывается ещё и
		// заголовком функции, и «начало оператора» уводило бы читателя на
		// строку выше предмета.
		start := line
		if at := strings.Index(stmt, formRowSelector); at >= 0 {
			start += strings.Count(stmt[:at], "\n")
		}
		line += strings.Count(stmt, "\n")
		if !strings.Contains(stmt, "locator(") || !strings.Contains(stmt, formRowSelector) {
			continue
		}
		// Выбор ПО ТЕКСТУ — только он и создаёт неоднозначность предок/потомок:
		// текст содержат оба, а сам по себе селектор строки формы — нет.
		if !strings.Contains(stmt, "getByText") && !strings.Contains(stmt, "hasText") {
			continue
		}
		examined++
		if guardsNestedRows(stmt) {
			continue
		}
		findings = append(findings, FormRowLocatorFinding{Line: start, Text: squeeze(stmt)})
	}
	return findings, examined
}

// guardsNestedRows — цепочка отсекает вложенные строки формы, то есть несёт
// `hasNot` с тем же селектором.
func guardsNestedRows(stmt string) bool {
	for _, at := range indexesOf(stmt, "hasNot") {
		if strings.Contains(stmt[at:], formRowSelector) {
			return true
		}
	}
	return false
}

func indexesOf(s, sub string) []int {
	var out []int
	for at, off := 0, 0; ; {
		i := strings.Index(s[off:], sub)
		if i < 0 {
			return out
		}
		at = off + i
		out = append(out, at)
		off = at + len(sub)
	}
}

func squeeze(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return strings.TrimSpace(s)
}

// DescribeFormRowFinding — текст находки: что не так и чем это кончается.
func DescribeFormRowFinding(file string, f FormRowLocatorFinding) string {
	return fmt.Sprintf(`%s:%d — строка формы выбирается по тексту без отсечения вложенных строк:
    %s
  `+"`.ant-form-item`"+` вкладываются, и обход дерева ставит ПРЕДКА раньше потомка: при вложенности
  такое выражение берёт ОБЪЕМЛЮЩИЙ блок, а не строку. Промах не даёт отказа — он даёт действие
  над чужой строкой, и видно это лишь по последствию через несколько шагов (#636).
  Исход: добавить отсечение вложенных строк — `+"`.filter({ hasNot: page.locator(\".ant-form-item\") })`"+`.`,
		file, f.Line, f.Text)
}
