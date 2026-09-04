// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// deliverymarker.go — разбор КОЛОНКИ-ПРИЗНАКА ДОСТАВКИ у таблиц дерева.
//
// # Зачем отдельный разбор, если рядом уже есть разбор таблиц
//
// Соседний `tablegrowth.go` отвечает на вопрос «у живой таблицы назван ли
// механизм ограничения роста». Он видит таблицы и операторы снятия строк — и НЕ
// видит колонок. Поэтому запись реестра вправе объявить таблицу очередью
// дренажа, у которой колонки-признака доставки нет ВОВСЕ, и гейт промолчит:
// объявление взято из закрытого словаря, причина написана, номер задачи стоит.
//
// Молчание тут не безобидно. Реестр читают как перечень предметов работы: по
// нему делят полосы и по нему пишут предикат уборки. Тринадцать записей,
// объявивших один и тот же механизм («дренаж помечает доставленную `sent_at`»),
// отправили бы исполнителя строить ОДИН предикат на все тринадцать — а он
// неразбираем у той части, где колонки нет: `42703` на каждом проходе у каждого
// владельца.
//
// Здесь заводится вторая ось реестра — СЕМЬЯ таблицы, — и, в отличие от темпа и
// вердикта, она проверяема по схеме: у очереди дренажа признак доставки обязан
// БЫТЬ, у журнала подписки его обязано НЕ БЫТЬ. Обе стороны проверяются, потому
// что односторонняя проверка зеленела бы на реестре, объявившем журналом всё.
//
// # ПРЕДПОСЫЛКИ РАЗБОРА — заявляются гейтом, а не подразумеваются
//
//  1. колонка объявляется либо в теле `CREATE TABLE`, либо `ALTER TABLE … ADD
//     COLUMN`, а снимается `ALTER TABLE … DROP COLUMN`; миграции читаются В
//     ПОРЯДКЕ ПРИМЕНЕНИЯ, потому что колонку заводят и снимают. Снятий в дереве
//     сегодня НОЛЬ, и это не слепота: свод миграций iam (2026-09-04) написан
//     `pg_dump`, то есть конечным состоянием, а состояние записи об удалении не
//     содержит никогда — две таблицы, где снятие прежде стояло, объявлены сразу
//     без признака. Способность ветви снятия читать своё доказана ИНЪЕКЦИЕЙ, а
//     не корпусом;
//  2. читается ТОЛЬКО секция `-- +goose Up`: `DROP COLUMN` секции `Down` есть
//     откат, и засчитав его, разбор объявил бы снятой каждую заведённую колонку;
//  3. признак доставки в этом дереве носит ОДНО имя — [DeliveryMarkerColumn].
//     Словарь закрыт намеренно: вторая форма пометки («status = 'sent'») у
//     очередей платформы не встречается, и её появление обязано быть находкой, а
//     не молча принятым синонимом.
//
// # ЧЕГО РАЗБОР НЕ ВИДИТ — названо, а не спрятано
//
//   - колонку, заведённую в теле функции миграции (`EXECUTE format(…)`):
//     разбор читает верхний уровень, а не тела `$$…$$`. Таких в дереве нет, и
//     появление первой будет означать, что предпосылка 1 перестала быть верной;
//   - имя таблицы, подставляемое в рантайме, — его текста в дереве нет by
//     construction, и такие операторы пропускаются, как и в соседнем разборе.
package repohygiene

import "strings"

// DeliveryMarkerColumn — имя колонки, которой очередь дренажа помечает
// ДОСТАВЛЕННУЮ строку.
//
// Одно имя, а не перечень: `pkg/outbox/drainer` пишет эту колонку ровно в одном
// месте (`markSuccess`), и её `IS NOT NULL` есть определение доставленности для
// клейма, для сверщика и для метрик. Заведётся вторая форма пометки — она обязана
// прийти сюда своим изменением, а не быть принятой молча.
const DeliveryMarkerColumn = "sent_at"

// ColumnEvent — что одна миграция сделала с признаком доставки у одной таблицы.
type ColumnEvent struct {
	TableRef
	// Present — колонка появилась (объявлена телом создания либо добавлена);
	// false — снята.
	Present bool
	// File, Line — координата оператора, чтобы находка называла место.
	File string
	Line int
}

// DeliveryMarkerCensus — объём осмотренного.
//
// Полосы печатаются порознь: одно суммарное число не отличает «полоса пуста» от
// «полоса не читалась», а обе полосы здесь живые и обе несущие.
type DeliveryMarkerCensus struct {
	// MigrationFiles — миграций прочитано.
	MigrationFiles int
	// CreateBodies — тел `CREATE TABLE` разобрано.
	CreateBodies int
	// Declared — объявлений признака в теле создания.
	Declared int
	// Added — добавлений признака оператором `ALTER … ADD COLUMN`.
	Added int
	// Dropped — снятий признака оператором `ALTER … DROP COLUMN`.
	Dropped int
}

// Add складывает переписи по файлам.
func (c *DeliveryMarkerCensus) Add(o DeliveryMarkerCensus) {
	c.MigrationFiles += o.MigrationFiles
	c.CreateBodies += o.CreateBodies
	c.Declared += o.Declared
	c.Added += o.Added
	c.Dropped += o.Dropped
}

// ScanDeliveryMarker разбирает одну миграцию: что она сделала с признаком
// доставки.
//
// События возвращаются В ПОРЯДКЕ ВСТРЕЧИ, потому что внутри одной миграции
// колонку можно добавить и снять, и последнее слово за последним оператором.
func ScanDeliveryMarker(owner, path string, src []byte) ([]ColumnEvent, DeliveryMarkerCensus) {
	census := DeliveryMarkerCensus{MigrationFiles: 1}
	var out []ColumnEvent

	up, _ := gooseUpSection(src)
	clean := blankSQLComments(up)
	_, top := splitDollarBodies(clean)

	for _, m := range createTableRe.FindAllSubmatchIndex(top, -1) {
		name := unquote(string(top[m[4]:m[5]]))
		if name == "" || isSubstituted(name) {
			continue
		}
		body := balancedBody(top, m[1])
		if body == "" {
			continue
		}
		census.CreateBodies++
		declares := bodyDeclaresColumn(body, DeliveryMarkerColumn)
		if declares {
			census.Declared++
		}
		// Событие эмитится и для таблицы БЕЗ признака — «видел, признака нет».
		// Отличать это от «не видел вовсе» несущее: у журнала признака нет by
		// construction, поэтому без такого события суждение о журнале выполняется
		// и для таблицы, которой разбор не встречал, — то есть молчит вакуумно.
		out = append(out, ColumnEvent{
			TableRef: TableRef{Owner: owner, Name: name},
			Present:  declares,
			File:     path,
			Line:     lineAt(src, m[0]),
		})
	}

	for _, m := range alterTableRe.FindAllSubmatchIndex(top, -1) {
		name := unquote(string(top[m[4]:m[5]]))
		if name == "" || isSubstituted(name) {
			continue
		}
		tail := string(top[m[6]:m[7]])
		// Порядок ветвей значения не имеет: один оператор `ALTER TABLE` не
		// добавляет и не снимает одну и ту же колонку разом.
		if columnActionRe(tail, "add") {
			census.Added++
			out = append(out, ColumnEvent{
				TableRef: TableRef{Owner: owner, Name: name},
				Present:  true,
				File:     path,
				Line:     lineAt(src, m[0]),
			})
		}
		if columnActionRe(tail, "drop") {
			census.Dropped++
			out = append(out, ColumnEvent{
				TableRef: TableRef{Owner: owner, Name: name},
				Present:  false,
				File:     path,
				Line:     lineAt(src, m[0]),
			})
		}
	}

	return out, census
}

// bodyDeclaresColumn отвечает, объявляет ли тело `CREATE TABLE` колонку с этим
// именем.
//
// Тело режется по запятым ВЕРХНЕГО уровня, и имя сверяется с ПЕРВЫМ словом
// элемента: иначе `REFERENCES q(sent_at)` и `WHERE sent_at IS NULL` внутри
// ограничения были бы засчитаны за объявление колонки.
func bodyDeclaresColumn(body, column string) bool {
	for _, part := range topLevelParts(body) {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		if unquote(fields[0]) == column {
			return true
		}
	}
	return false
}

// topLevelParts режет тело по запятым верхнего уровня.
func topLevelParts(body string) []string {
	var (
		out   []string
		depth int
		start int
	)
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

// columnActionRe отвечает, несёт ли хвост `ALTER TABLE` действие над колонкой
// признака.
//
// Сверка идёт по ГРАНИЦЕ ИМЕНИ, а не по вхождению подстроки: колонка
// `notified_at` не обязана считаться `sent_at`, а `sent_at_backup` — тем более.
func columnActionRe(tail, verb string) bool {
	low := strings.ToLower(tail)
	for i := 0; ; {
		idx := strings.Index(low[i:], verb+" ")
		if idx < 0 {
			return false
		}
		i += idx + len(verb) + 1
		rest := strings.TrimSpace(low[i:])
		rest = strings.TrimPrefix(rest, "column ")
		rest = strings.TrimSpace(rest)
		rest = strings.TrimPrefix(rest, "if exists ")
		rest = strings.TrimPrefix(rest, "if not exists ")
		rest = strings.TrimSpace(rest)
		if fields := strings.Fields(rest); len(fields) > 0 &&
			unquote(strings.TrimSuffix(fields[0], ",")) == DeliveryMarkerColumn {
			return true
		}
	}
}

// FoldDeliveryMarker сводит события в ответ «несёт ли таблица признак доставки
// СЕГОДНЯ».
//
// События обязаны приходить В ПОРЯДКЕ ПРИМЕНЕНИЯ миграций: колонку заводят и
// снимают, и порядок есть единственное, что различает «очередь» и «журнал,
// ПЕРЕСТАВШИЙ быть очередью».
//
// Ключ карты означает «таблицу разбор ВИДЕЛ», значение — «несёт ли она признак».
// Отсутствие ключа — третье состояние, и путать его со значением `false` нельзя:
// суждение о журнале («признака быть не должно») на невиданной таблице
// выполняется тождественно, то есть молчит, ничего не проверив.
func FoldDeliveryMarker(events []ColumnEvent) map[TableRef]bool {
	out := map[TableRef]bool{}
	for _, e := range events {
		out[e.TableRef] = e.Present
	}
	return out
}
