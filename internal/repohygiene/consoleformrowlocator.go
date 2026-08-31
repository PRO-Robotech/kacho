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

// formRowClass — имя ряда формы.
//
// Здесь стояла вторая константа — тот же класс В КАВЫЧКАХ СЕЛЕКТОРА. Она сузила
// разбор до одной формы записи и осталась мёртвой, как только разбор научился
// второй; линт это и поймал. Форма записи — дело выражения, а не константы:
// проверять надо принадлежность к ряду, а не то, каким синтаксисом её выразили.
//
// ЗАЧЕМ ВТОРАЯ ФОРМА. Ряд адресуют двумя способами, и оба живут в спеках:
// селектором класса (`locator(".ant-form-item")`) и осью xpath
// (`ancestor::*[contains(@class, " ant-form-item ")]`). Разбор, знающий только
// первую, ослеп ровно тогда, когда пробы перешли на вторую, — и сказал об этом
// сам (предпосылка «распознано ноль выражений» — отказ, а не молчание). Это и
// есть цена узкого предиката: он мерит ФОРМУ ЗАПИСИ, а предмет — адресацию ряда.
const formRowClass = "ant-form-item"

// mentionsFormRow — выражение адресует ряд формы в любой из двух форм записи.
//
// Имя вызова здесь спрашивается ПОДСТРОКОЙ, и это названо, а не умолчано.
// Хвост чужого идентификатора (`xlocator(`) дал бы ложную НАХОДКУ — направление,
// которое себя выдаёт: кто-то упирается в красное. Смягчает его конъюнкция с
// классом ряда в том же операторе. Замер на день правки: спек 22, операторов
// 1846, хвостовых совпадений `locator(` в операторе с классом ряда — ноль.
// Целоимённый предикат пакета (`callsJSName`) живёт в файле проб и отсюда, из
// не-тестового кода, недоступен; заводить его вторую копию — значит заводить
// второе место об одном предмете. Появится предмет — предикат переезжает в
// не-тестовый файл, а не копируется.
func mentionsFormRow(stmt string) bool {
	return strings.Contains(stmt, "locator(") && strings.Contains(stmt, formRowClass)
}

// nearestAncestorRow — ряд взят БЛИЖАЙШИЙ по оси `ancestor`, то есть который
// именно из вложенных рядов имеется в виду, сказано явно.
//
// Ось `ancestor` идёт в ОБРАТНОМ порядке документа, поэтому `[1]` — ближайший
// предок, а не самый внешний. Это отсечение СИЛЬНЕЕ, чем `hasNot`: оно не
// «выбрасывает лишние совпадения», а с самого начала называет одно.
//
// Без индекса ось возвращает ВСЕХ предков-рядов, и `.first()` снова берёт
// самого внешнего — то есть исходный дефект, только записанный иначе.
func nearestAncestorRow(stmt string) bool {
	for _, at := range indexesOf(stmt, "ancestor::") {
		tail := stmt[at:]
		if !strings.Contains(tail, formRowClass) {
			continue
		}
		if i := strings.Index(tail, formRowClass); i >= 0 {
			if rest := tail[i:]; strings.Contains(rest, "][1]") || strings.Contains(rest, ")][1]") {
				return true
			}
		}
	}
	return false
}

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
		if at := strings.Index(stmt, formRowClass); at >= 0 {
			start += strings.Count(stmt[:at], "\n")
		}
		line += strings.Count(stmt, "\n")
		if !mentionsFormRow(stmt) {
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

// guardsNestedRows — цепочка однозначно называет, КОТОРЫЙ из вложенных рядов
// имеется в виду. Законных способов два, и оба встречаются в спеках:
//
//   - отсечь вложенные — `hasNot` с тем же селектором;
//   - взять ближайший — ось `ancestor` с индексом `[1]`.
//
// Второй появился позже и строго сильнее первого; разбор, знавший только
// первый, объявил бы его находкой — то есть потребовал бы вернуть худшую форму
// ради зелёного гейта.
func guardsNestedRows(stmt string) bool {
	for _, at := range indexesOf(stmt, "hasNot") {
		if strings.Contains(stmt[at:], formRowClass) {
			return true
		}
	}
	return nearestAncestorRow(stmt)
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
  Исход — назвать, КОТОРЫЙ ряд имеется в виду, одним из двух способов:
    взять ближайший  — `+"`.locator('xpath=ancestor::*[contains(@class, \" ant-form-item \")][1]')`"+`
    отсечь вложенные — `+"`.filter({ hasNot: page.locator(\".ant-form-item\") })`"+`.`,
		file, f.Line, f.Text)
}
