// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationsqltext.go — исполняемая часть миграции: Up-секция без комментариев.
//
// Разбор вынесен из `migrationsqltext_test.go` в НЕ-тестовый файл того же пакета
// потому, что его зовут НЕСКОЛЬКО инвентарей — среди них инвентарь частичных
// индексов очередей (`outboxpendingindexset_test.go`), инвентарь курсорных
// индексов (`listcursorindex.go`, гейт задачи #708) и инвентарь точек
// невозврата (`schemarollbackform.go`). Числа здесь намеренно нет: перечень
// растёт, а выписанное число устаревает молча — предикат
// `git grep -l 'migrationUpSection\|sqlBlankComments\|sqlBlankStrings'`.
// Второй из названных живёт в не-тестовом файле,
// чтобы его звала та же функция, что и инъекционная проба; из не-тестового файла
// символы тестового не видны, и без переноса пришлось бы завести ВТОРУЮ копию
// забеливания комментариев. Две копии одного разбора разошлись бы молча — и
// разошлись бы именно там, где расхождение не видно: на законном входе обе
// отвечают одинаково.
//
// Поведение переносом не менялось: функции перенесены дословно, а их пробы
// (`Test_SqlBlankComments_KeepsCodeAndOffsets`,
// `TestInventoriesReadExecutablePartNotComments`,
// `TestMigrationCommentsCarryParseableConstructs`) остались на месте и читают их
// по-прежнему.
package repohygiene

import "strings"

// migrationUpSection — исполняемая часть миграции: Up-секция без комментариев.
//
// Порядок двух шагов обязателен и обратим быть не может: маркер `-- +goose Down`
// САМ является комментарием, поэтому секция отрезается по СЫРОМУ тексту, и только
// потом забеливаются комментарии. Забелив сперва, разбор потерял бы границу
// секций и прочитал бы откат как часть наката.
func migrationUpSection(body string) string {
	up := body
	if i := strings.Index(up, "-- +goose Down"); i >= 0 {
		up = up[:i]
	}
	return sqlBlankComments(up)
}

// sqlBlankComments заменяет содержимое SQL-комментариев пробелами, сохраняя
// длину строки и позиции переводов строк.
//
// Приоритет состояний: строка > комментарий > код. Внутри строкового литерала
// `--` комментария не начинает; внутри комментария кавычка строки не открывает.
// Блочные комментарии Postgres ВЛОЖЕННЫЕ — глубина считается, а не ищется первый
// `*/`.
func sqlBlankComments(s string) string {
	out := []byte(s)
	scanSQLComments(s, func(lo, hi int) {
		for i := lo; i < hi && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	})
	return string(out)
}

// sqlBlankStrings заменяет СОДЕРЖИМОЕ строковых литералов пробелами, сохраняя
// длину и позиции переводов строк.
//
// Зачем отдельно от [sqlBlankComments]. Тот забеливает комментарии и при этом
// НАМЕРЕННО пропускает литералы целиком — иначе `--` внутри строки открыл бы
// комментарий. Но литерал не исполняется как SQL: прозаическое сообщение об
// ошибке, содержащее слова конструкции, для инвентаря по образцу неотличимо от
// самой конструкции. В этом дереве такие сообщения есть.
//
// Кавычки-границы ОСТАЮТСЯ на месте: забеливается содержимое, поэтому
// `WHERE x = 'DROP COLUMN'` остаётся синтаксически тем же и по-прежнему не
// склеивает соседние лексемы.
func sqlBlankStrings(s string) string {
	out := []byte(s)
	scanSQLSpans(s, nil, func(lo, hi int) {
		// lo указывает на открывающую кавычку, hi — за закрывающую.
		for i := lo + 1; i < hi-1 && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	})
	return string(out)
}

// scanSQLComments проходит текст один раз и зовёт onComment на каждом
// комментарии. Приоритет состояний: строка > комментарий > код.
func scanSQLComments(s string, onComment func(lo, hi int)) {
	scanSQLSpans(s, onComment, nil)
}

// scanSQLSpans — ЕДИНСТВЕННЫЙ проход по тексту SQL. Зовёт onComment на каждом
// комментарии и onString на каждом строковом литерале; любой из двух может быть
// nil. Приоритет состояний прежний: строка > комментарий > код.
//
// Второй проход здесь не заводится намеренно: два разбора одного текста
// разошлись бы молча — и разошлись бы именно там, где расхождение не видно, на
// законном входе оба отвечают одинаково.
func scanSQLSpans(s string, onComment func(lo, hi int), onString func(lo, hi int)) {
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\'':
			lo := i
			i++
			for i < len(s) {
				if s[i] != '\'' {
					i++
					continue
				}
				if i+1 < len(s) && s[i+1] == '\'' { // '' — экранированная кавычка
					i += 2
					continue
				}
				i++
				break
			}
			if onString != nil {
				onString(lo, i)
			}
		case s[i] == '-' && i+1 < len(s) && s[i+1] == '-':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				j = len(s)
			} else {
				j += i
			}
			if onComment != nil {
				onComment(i, j)
			}
			i = j
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			depth, j := 1, i+2
			for j < len(s) && depth > 0 {
				switch {
				case s[j] == '/' && j+1 < len(s) && s[j+1] == '*':
					depth++
					j += 2
				case s[j] == '*' && j+1 < len(s) && s[j+1] == '/':
					depth--
					j += 2
				default:
					j++
				}
			}
			if onComment != nil {
				onComment(i, j)
			}
			i = j
		default:
			i++
		}
	}
}
