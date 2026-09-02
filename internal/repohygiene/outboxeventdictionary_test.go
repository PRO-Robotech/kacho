// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outboxeventdictionary_test.go — СЛОВАРЬ СОБЫТИЙ очереди, каким его закрывает
// БД, и сверка с ним записки об освобождении от ключа порядка.
//
// # Зачем
//
// Освобождение очереди от ключа порядка (commutativeDrainExempt,
// outboxorderinggate_test.go) держится на утверждении о том, КАКИЕ события эта
// очередь может нести. Гейт предмета проверял только две вещи — что очередь ещё
// дренится и что ключ ей не выдали, — а содержание записки не сверял ни с чем.
// Такое освобождение сертифицирует само себя: записка может утверждать что
// угодно про словарь, и гейт останется зелёным.
//
// Так и вышло. Записка про очередь компенсаций провайдера утверждала «событие
// ровно одного вида, второй половины нет by construction» — и была ложной УЖЕ В
// МОМЕНТ НАПИСАНИЯ: сутками ранее миграция сняла прежнее ограничение и поставила
// словарь из ДВУХ значений, оба вида живые (эмитируются и применяются). Вывод
// («порядок не нужен») уцелел, но по другому основанию, которого в записке не
// было. Это ровно тот класс, который сама эта работа и закрывала: форма проверки
// без содержания.
//
// # Что проверяется здесь
//
// Словарь событий закрыт в БД ограничением вида `CHECK (<колонка> = ANY (ARRAY[…]))`
// либо `CHECK (<колонка> IN (…))`. Он и есть предмет: записка обязана перечислить
// ЕГО ЦЕЛИКОМ, слово в слово. Тогда следующая миграция, расширяющая словарь,
// красит гейт и требует пересмотреть обоснование — вместо того чтобы молча
// сделать его ложным.
//
// Требование «перечислить целиком» не косметическое. У очереди инвалидаций
// словарь содержит пары, которые ВЫГЛЯДЯТ противоположными (`binding_upsert` ↔
// `binding_delete`, `binding_grant` ↔ `binding_revoke`); пока они не выписаны,
// читатель записки не может ни усомниться, ни убедиться.
//
// # Как читается
//
// Проигрыванием Up-секций миграций в порядке версий, а не чтением одной. Словарь
// живёт в ограничении, а ограничение переживает несколько правок: его снимают по
// имени и ставят заново с другим набором значений. Читать «ту миграцию, где
// ограничение ВВЕДЕНО» вместо «той, где оно СТОИТ СЕЙЧАС» — это и есть ошибка,
// из-за которой ложная записка получила отметку «проверено по существу».
//
// # Объявленные слепые зоны
//
//   - БЕЗЫМЯННОЕ ограничение (`CHECK (…)` без `CONSTRAINT <имя>`) снять по имени
//     проигрывание не может: Postgres присваивает такому имя сам, а в тексте
//     миграции его нет. Такие ограничения учитываются как поставленные и никогда
//     не снимаются. В дереве безымянные встречаются (напр. словарь очереди
//     зеркал), но ни одно из них по имени не снимается.
//   - Перечисление, стоящее ВНУТРИ булева выражения, словарём не считается — и
//     это не пропуск, а определение: такое условие ограничивает СОЧЕТАНИЕ полей,
//     а не набор значений колонки. Разбор «найти вхождение» на нём ошибается в
//     обе стороны, и это измерено на дереве: у привязок доступа он давал ДВА
//     значения вместо трёх (пересечение со вложенной веткой ограничения
//     согласованности), у балансировщика — сочинял двузначный словарь там, где
//     допустимых значений три и одно из них пустая строка. Сегодня под это
//     определение подпадает одно ограничение — сочетание типа и размещения
//     балансировщика; словаря у той колонки в инвентаре нет.
//   - УСЛОВНОЕ снятие ограничения (внутри DO-блока) применяется безусловно — та
//     же объявленная зона, что у инвентаря частичных индексов, и по той же
//     причине: обратная эвристика («не применять снятия из DO-блоков») завела бы
//     свою, симметричную.
//   - Разбор читает ТЕКСТ миграций, а не живую базу. Ограничение, поставленное
//     руками мимо миграции, ему невидимо — как и всему остальному в этом файле
//     семейству гейтов. Отдельно: `NOT VALID` разбором не учитывается — такое
//     ограничение действует на новые строки и потому словарь закрывает, а
//     ретроспективная невалидация касается уже лежащих строк, о которых текст
//     миграции ничего не говорит.
//   - Зона «комментарий читается как код» БОЛЬШЕ НЕ ОБЪЯВЛЯЕТСЯ — она снята:
//     разбору отдаётся исполняемая часть, комментарии забелены
//     (`migrationUpSection`, migrationsqltext_test.go). До этого ограничение,
//     ПРОЦИТИРОВАННОЕ в комментарии, становилось для слияния вторым живым
//     ограничением и МОЛЧА сужало словарь пересечением. Живого экземпляра на
//     дереве не было; латентным он быть перестал.
package repohygiene

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// sqlEnumDict — закрытый словарь одной таблицы: колонка → её допустимые
// значения, отсортированные (порядок в ограничении смысла не несёт).
type sqlEnumDict map[string][]string

// enumDictInventory — результат проигрывания миграций.
type enumDictInventory struct {
	// dict — «<сервис>:<таблица>» → словари её колонок. Ключ несёт сервис по той
	// же причине, что и у инвентаря индексов: одно имя таблицы бывает у
	// нескольких сервисов, и сливать их в одну корзину значит сделать вердикт
	// функцией алфавита.
	dict map[string]sqlEnumDict
	// source — «<сервис>:<таблица>:<колонка>» → миграция, поставившая словарь
	// ПОСЛЕДНЕЙ. Нужна, чтобы находка называла координату, а не только значения.
	source map[string]string
	// byName — ИМЯ ограничения → словари, которые оно закрывает СЕЙЧАС. Список, а
	// не одна запись: одно имя ограничения встречается в разных схемах, и там оно
	// закрывает разные наборы. Читается гейтом цитирования
	// (dictionarycitationgate_test.go): комментарий, называющий ограничение по
	// имени, делает утверждение о ЕГО словаре.
	byName map[string][]namedDict
	// filesRead — объём осмотренного: «ноль словарей» обязано быть отличимо от
	// «ноль прочитанных миграций».
	filesRead int
}

// namedDict — словарь, закрытый ОДНИМ именованным ограничением.
type namedDict struct {
	table  string // «<сервис>:<таблица>»
	column string
	values []string
	source string // миграция, поставившая это ограничение последней
}

var (
	// Якорь таблицы: любое из трёх ключевых слов, вводящих оператор о таблице.
	// Ограничение всегда лежит внутри такого оператора, поэтому ближайший
	// ПРЕДШЕСТВУЮЩИЙ якорь и есть его таблица.
	sqlTableAnchorRe = regexp.MustCompile(
		`(?is)\b(CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|ALTER\s+TABLE(?:\s+ONLY)?|DROP\s+TABLE(?:\s+IF\s+EXISTS)?)\s+([\w."]+)`)
	sqlNamedCheckRe = regexp.MustCompile(`(?is)\bCONSTRAINT\s+([\w."]+)\s+CHECK\s*\(`)
	sqlBareCheckRe  = regexp.MustCompile(`(?is)\bCHECK\s*\(`)
	sqlDropConstrRe = regexp.MustCompile(`(?is)\bDROP\s+CONSTRAINT(?:\s+IF\s+EXISTS)?\s+([\w."]+)`)
	// Словарь распознаётся ТОЛЬКО как ЦЕЛОЕ условие ограничения — отсюда якоря
	// начала и конца. Почему так, а не «найти вхождение» — см. sqlEnumOf.
	sqlWholeAnyArrayRe = regexp.MustCompile(`(?is)^([\w.]+)\s*=\s*ANY\s*\(\s*ARRAY\s*\[(.*)\]\s*(?:::[\w ]+(?:\[\s*\])?\s*)?\)$`)
	sqlWholeInListRe   = regexp.MustCompile(`(?is)^([\w.]+)\s+IN\s*\((.*)\)$`)
	sqlStringLiteralRe = regexp.MustCompile(`'((?:[^']|'')*)'`)
	// Приведение типа у литерала словаря (`'ACTIVE'::text`) — шум, а не значение.
	sqlCastRe = regexp.MustCompile(`(?i)::\s*[\w ]+(?:\[\s*\])?`)
	// Нулевая ветка словаря: `<колонка> IS NULL OR <словарь той же колонки>`.
	sqlNullableEnumRe = regexp.MustCompile(`(?is)^([\w.]+)\s+IS\s+NULL\s+OR\s+(.*)$`)
)

// sqlNullValue — член словаря, означающий ОТСУТСТВИЕ значения (колонка
// допускает NULL). Отдельный член, а не пропуск: строка без вида события — тоже
// вид входа, и записка, освобождающая очередь от порядка, обязана его назвать.
const sqlNullValue = "<NULL>"

// sqlBalanced возвращает содержимое скобочной группы, открывающейся в s[open]
// (s[open] обязан быть открывающей скобкой), и смещение сразу за закрывающей.
//
// Одинарные кавычки пропускаются целиком, и это не перестраховка: в дереве есть
// `CHECK`, внутри которого стоит регулярное выражение с обеими скобками
// (`'^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'`). Наивный счётчик на нём разъезжается
// и утаскивает разбор в соседний оператор.
func sqlBalanced(s string, open int, closing byte) (string, bool) {
	depth := 0
	opening := s[open]
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\'':
			for i++; i < len(s); i++ {
				if s[i] != '\'' {
					continue
				}
				if i+1 < len(s) && s[i+1] == '\'' { // '' — экранированная кавычка
					i++
					continue
				}
				break
			}
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return s[open+1 : i], true
			}
		}
	}
	return "", false
}

// sqlUnwrap снимает избыточные скобки, охватывающие ВСЁ условие целиком.
// `((op = ANY (ARRAY[…])))` и `op = ANY (ARRAY[…])` — одно и то же условие,
// записанное дампом схемы и рукой.
func sqlUnwrap(expr string) string {
	for {
		expr = strings.TrimSpace(expr)
		if len(expr) < 2 || expr[0] != '(' {
			return expr
		}
		inner, ok := sqlBalanced(expr, 0, ')')
		if !ok || len(inner)+2 != len(expr) { // скобка закрылась не в конце
			return expr
		}
		expr = inner
	}
}

// sqlEnumOf распознаёт закрытый словарь колонки в тексте ОДНОГО ограничения:
// колонка, её допустимые значения, и признак «это вообще словарь».
//
// # Почему условие обязано быть словарём ЦЕЛИКОМ, а не содержать его
//
// Перечисление, стоящее ВНУТРИ булева выражения, набор значений колонки НЕ
// закрывает — оно ограничивает их СОЧЕТАНИЕ с другими полями. Пример из дерева:
// у привязок доступа рядом живут два ограничения, и оба называют одну колонку —
// `status`. Первое перечисляет три значения и есть словарь. Второе описывает
// согласованность («у отозванной обязано быть время отзыва, у остальных — нет»)
// и внутри своей ветки перечисляет ДВА значения из трёх. Разбор «найти
// вхождение» прочтёт оба и получит либо пересечение (двузначный словарь — уже,
// чем на самом деле), либо «последнее выигрывает» (вердикт как функция порядка
// обхода карты). Обе величины неверны, и вторая ещё и невоспроизводима.
//
// Поэтому распознаётся только условие, ЦЕЛИКОМ равное `<колонка> ∈ перечисление`
// (с точностью до скобок, регистра и приведений типа). Всё остальное — не
// словарь, и это не эвристика, а определение: закрыть набор значений колонки
// может только условие, которое ничего другого не утверждает.
//
// Следствия, полученные даром: `x NOT IN (…)` не проходит по построению (между
// колонкой и `IN` стоит `NOT`, а имя колонки пробела не содержит); `x IN (SELECT
// …)` не проходит (в перечислении обязаны быть только литералы); выражение слева
// (`payload ->> 'kind'`) не проходит (колонка — голый идентификатор).
func sqlEnumOf(expr string) (string, []string, bool) {
	expr = sqlUnwrap(expr)

	// Нулевая ветка: `<колонка> IS NULL OR <словарь той же колонки>` набор
	// значений всё-таки закрывает — просто вместе с отсутствием значения.
	// Отсутствие представимо ОТДЕЛЬНО от значений, поэтому оно и представлено
	// отдельным членом словаря (sqlNullValue), а не молча выброшено: строка без
	// вида события — такой же вид входа, как остальные, и записка об
	// освобождении обязана про него сказать.
	if m := sqlNullableEnumRe.FindStringSubmatch(expr); m != nil {
		col, vals, ok := sqlEnumOf(m[2])
		if !ok || col != unqualify(m[1]) {
			return "", nil, false
		}
		for _, v := range vals {
			if v == sqlNullValue {
				return "", nil, false // коллизия: значение неотличимо от метки отсутствия
			}
		}
		vals = append(vals, sqlNullValue)
		sort.Strings(vals)
		return col, vals, true
	}

	var col, list string
	switch m := sqlWholeAnyArrayRe.FindStringSubmatch(expr); {
	case m != nil:
		col, list = m[1], m[2]
	default:
		if m := sqlWholeInListRe.FindStringSubmatch(expr); m != nil {
			col, list = m[1], m[2]
		} else {
			return "", nil, false
		}
	}

	vals := make([]string, 0, 4)
	for _, lit := range sqlStringLiteralRe.FindAllStringSubmatch(list, -1) {
		vals = append(vals, strings.ReplaceAll(lit[1], "''", "'"))
	}
	if len(vals) == 0 {
		return "", nil, false
	}
	// В перечислении обязаны остаться ТОЛЬКО литералы: иначе это подзапрос или
	// выражение, а не закрытый набор. Проверяется вычитанием — то, что не
	// литерал, не приведение типа и не разделитель, обязано отсутствовать.
	rest := sqlCastRe.ReplaceAllString(sqlStringLiteralRe.ReplaceAllString(list, ""), "")
	if strings.Trim(rest, " \t\n\r,") != "" {
		return "", nil, false
	}
	sort.Strings(vals)
	return unqualify(col), vals, true
}

// sqlEnumDictOf — словарь ограничения в виде карты (пустая, если условие
// словарём не является). Обёртка над sqlEnumOf: у ограничения по построению не
// больше одного словаря, но остальной разбор оперирует картой колонок.
func sqlEnumDictOf(expr string) sqlEnumDict {
	col, vals, ok := sqlEnumOf(expr)
	if !ok {
		return sqlEnumDict{}
	}
	return sqlEnumDict{col: vals}
}

// sqlConstraintOp — один оператор, меняющий состав ограничений, вместе с его
// смещением в тексте. Смещение здесь предмет ровно по той же причине, что и у
// инвентаря индексов: снятие и постановка одноимённого ограничения сплошь и
// рядом стоят в одной миграции, и порядок между ними решает исход.
type sqlConstraintOp struct {
	at        int
	table     string // имя таблицы без схемы
	name      string // имя ограничения; для безымянных — синтетическое
	drop      bool
	dropTable bool
	dict      sqlEnumDict
}

// constraintOpsInTextOrder разбирает Up-секцию одной миграции. fileTag —
// базовое имя миграции; входит в синтетическое имя безымянного ограничения,
// чтобы два безымянных ограничения одной таблицы из РАЗНЫХ миграций не
// схлопнулись в одну запись (второе затёрло бы первое).
func constraintOpsInTextOrder(up, fileTag string) []sqlConstraintOp {
	anchors := sqlTableAnchorRe.FindAllStringSubmatchIndex(up, -1)
	tableAt := func(pos int) string {
		table := ""
		for _, a := range anchors {
			if a[0] >= pos {
				break
			}
			table = unqualify(up[a[4]:a[5]])
		}
		return table
	}

	ops := make([]sqlConstraintOp, 0, 8)
	named := map[int]bool{} // смещения `CHECK (`, у которых имя уже прочитано
	for _, m := range sqlNamedCheckRe.FindAllStringSubmatchIndex(up, -1) {
		named[m[1]-1] = true // позиция открывающей скобки
		if inner, ok := sqlBalanced(up, m[1]-1, ')'); ok {
			ops = append(ops, sqlConstraintOp{
				at: m[0], table: tableAt(m[0]),
				name: unqualify(up[m[2]:m[3]]), dict: sqlEnumDictOf(inner),
			})
		}
	}
	for _, m := range sqlBareCheckRe.FindAllStringIndex(up, -1) {
		if named[m[1]-1] {
			continue
		}
		if inner, ok := sqlBalanced(up, m[1]-1, ')'); ok {
			ops = append(ops, sqlConstraintOp{
				at: m[0], table: tableAt(m[0]),
				// Безымянное ограничение снять по имени нельзя — синтетическое имя
				// уникально по смещению, поэтому его и не снимет никто. Объявлено
				// слепой зоной в шапке файла.
				name: "\x00anon", dict: sqlEnumDictOf(inner),
			})
		}
	}
	for _, m := range sqlDropConstrRe.FindAllStringSubmatchIndex(up, -1) {
		ops = append(ops, sqlConstraintOp{
			at: m[0], table: tableAt(m[0]), name: unqualify(up[m[2]:m[3]]), drop: true,
		})
	}
	for _, a := range anchors {
		if strings.HasPrefix(strings.ToUpper(strings.Join(strings.Fields(up[a[2]:a[3]]), " ")), "DROP TABLE") {
			ops = append(ops, sqlConstraintOp{at: a[0], table: unqualify(up[a[4]:a[5]]), dropTable: true})
		}
	}

	sort.SliceStable(ops, func(i, j int) bool { return ops[i].at < ops[j].at })
	for i := range ops {
		if ops[i].name == "\x00anon" {
			ops[i].name = "\x00anon@" + fileTag + "#" + strconv.Itoa(i)
		}
	}
	return ops
}

// enumDictInventoryCache — разобранный инвентарь словарей на корень дерева.
//
// Проигрывание миграций спрашивают три пробы этого файла и гейт цитирования
// (dictionarycitationgate_test.go), и стоит оно под `-race` около секунды на
// проход. Мотив и границы кэша — те же, что у инвентаря проводок
// (outboxInventoryCache выше): ни одна проба пакета в читаемое дерево не пишет,
// ключ — корень (синтетические деревья проб получают свою запись, а не чужой
// ответ), кладётся только успешный разбор, а `filesRead` лежит внутри инвентаря,
// поэтому «объём осмотренного» остаётся настоящим и у пробы, получившей готовый
// ответ. Пакет живёт у потолка бюджета юнит-прогона — величина запаса и способ
// её перемерить названы у outboxInventoryCache, числом здесь не повторяются.
var (
	enumDictInventoryMu    sync.Mutex
	enumDictInventoryCache = map[string]enumDictInventory{}
)

// enumDictionaryInventory проигрывает Up-секции всех миграций всех сервисов в
// порядке версий и возвращает ИТОГОВЫЕ закрытые словари.
func enumDictionaryInventory(t *testing.T, root string, corpus migrationSQLCorpus) enumDictInventory {
	t.Helper()
	enumDictInventoryMu.Lock()
	cached, ok := enumDictInventoryCache[root]
	enumDictInventoryMu.Unlock()
	if ok {
		return cached
	}

	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("чтение %s: %v", servicesDir, err)
	}

	inv := enumDictInventory{
		dict:   map[string]sqlEnumDict{},
		source: map[string]string{},
		byName: map[string][]namedDict{},
	}
	// nameSource — «<сервис>:<таблица>|<имя ограничения>» → миграция, поставившая
	// его последней.
	nameSource := map[string]string{}
	// live — «<сервис>:<таблица>» → имя ограничения → его словарь.
	live := map[string]map[string]sqlEnumDict{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		dir := filepath.Join(servicesDir, svc, "internal", "migrations")
		sqls, globErr := corpus(dir)
		if globErr != nil {
			t.Fatalf("состав %s: %v", dir, globErr)
		}
		sort.Strings(sqls) // имя начинается с версии → лексикографический порядок = порядок применения
		for _, path := range sqls {
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("чтение %s: %v", path, readErr)
			}
			inv.filesRead++
			// Читается ИСПОЛНЯЕМАЯ часть: комментарии забелены (migrationsqltext_test.go).
			// Иначе ограничение, ПРОЦИТИРОВАННОЕ в комментарии, становится для разбора
			// вторым живым ограничением той же таблицы — а слияние идёт пересечением,
			// то есть цитата МОЛЧА сузила бы словарь.
			up := migrationUpSection(string(body))
			for _, op := range constraintOpsInTextOrder(up, filepath.Base(path)) {
				key := svc + ":" + op.table
				switch {
				case op.dropTable:
					delete(live, key)
				case op.drop:
					delete(live[key], op.name)
				case len(op.dict) > 0:
					if live[key] == nil {
						live[key] = map[string]sqlEnumDict{}
					}
					live[key][op.name] = op.dict
					nameSource[key+"|"+op.name] = filepath.Base(path)
					for col := range op.dict {
						inv.source[key+":"+col] = filepath.Base(path)
					}
				default:
					// Ограничение без перечисления (тип, длина, взаимное исключение).
					// Регистрируем ПУСТЫМ, чтобы последующее снятие по имени имело
					// предмет и не выглядело снятием чужого.
					if live[key] == nil {
						live[key] = map[string]sqlEnumDict{}
					}
					live[key][op.name] = sqlEnumDict{}
				}
			}
		}
	}
	for key, byName := range live {
		// Слияние живых ограничений таблицы — ПЕРЕСЕЧЕНИЕ по колонке, а не
		// «последнее выигрывает»: строка обязана удовлетворять КАЖДОМУ
		// ограничению, поэтому допустимы ровно те значения, что разрешены
		// всеми. «Последнее выигрывает» вдобавок зависело бы от порядка обхода
		// карты, то есть вердикт гейта был бы функцией случайного порядка —
		// ровно тот класс, который эта работа и закрывала.
		merged := sqlEnumDict{}
		for _, name := range sortedKeys1(byName) {
			// Именованное ограничение цитируемо по имени — значит комментарий,
			// назвавший его, делает утверждение о ЕГО наборе. Безымянное не
			// цитируемо by construction (имя ему присваивает Postgres) и в этот
			// индекс не попадает.
			if !strings.HasPrefix(name, "\x00anon") {
				for col, vals := range byName[name] {
					inv.byName[name] = append(inv.byName[name], namedDict{
						table: key, column: col, values: vals, source: nameSource[key+"|"+name],
					})
				}
			}
			for col, vals := range byName[name] {
				prev, seen := merged[col]
				if !seen {
					merged[col] = vals
					continue
				}
				allowed := map[string]bool{}
				for _, v := range vals {
					allowed[v] = true
				}
				keep := make([]string, 0, len(prev))
				for _, v := range prev {
					if allowed[v] {
						keep = append(keep, v)
					}
				}
				merged[col] = keep
			}
		}
		if len(merged) > 0 {
			inv.dict[key] = merged
		}
	}
	enumDictInventoryMu.Lock()
	enumDictInventoryCache[root] = inv
	enumDictInventoryMu.Unlock()
	return inv
}

// sortedKeys1 — отсортированные ключи карты ограничений. Отдельная функция, а не
// sortedKeys: у той другой тип значения.
func sortedKeys1(m map[string]sqlEnumDict) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestEnumDictionaryInventoryReadsTheTree — самопроверка предпосылки разбора на
// НАСТОЯЩЕМ дереве: положительный контроль на словаре, чьё содержимое известно.
//
// Без него «словарь пуст» было бы неотличимо от «разбор сломался», и гейт ниже
// молчал бы «чисто» на любой записке.
func TestEnumDictionaryInventoryReadsTheTree(t *testing.T) {
	root := repoRoot(t)
	inv := enumDictionaryInventory(t, root, trackedMigrationSQL)

	if inv.filesRead == 0 {
		t.Fatalf("разбор не прочитал ни одной миграции — предпосылка сломана, молчание "+
			"ничего не доказывает (корень %s)", root)
	}
	if len(inv.dict) == 0 {
		t.Fatalf("разбор не нашёл НИ ОДНОГО закрытого словаря в %d миграциях — распознавание "+
			"сломано (ищется CHECK (<колонка> = ANY (ARRAY[…])) либо CHECK (<колонка> IN (…)))",
			inv.filesRead)
	}
	t.Logf("прочитано миграций: %d; таблиц с закрытыми словарями: %d", inv.filesRead, len(inv.dict))

	// Положительный контроль: очередь кортежей прав iam. Её словарь задан в
	// начальной миграции и с тех пор не менялся, поэтому он и взят эталоном —
	// разбор, вернувший что-то другое, сломан, а не «нашёл изменение».
	got := inv.dict["iam:fga_outbox"]["event_type"]
	want := []string{"fga.tuple.delete", "fga.tuple.write"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("контрольный словарь iam:fga_outbox.event_type прочитан как %v, а в миграциях "+
			"стоит %v — разбор сломан, и все его вердикты ниже ничего не стоят", got, want)
	}

	// Второй контроль, на ЗАМЕНЁННОМ ограничении: словарь очереди компенсаций
	// провайдера введён одной миграцией и заменён другой. Разбор, читающий ту,
	// где ограничение ВВЕДЕНО, вместо той, где оно СТОИТ СЕЙЧАС, вернёт одно
	// значение вместо двух — это и есть та ошибка, из-за которой записка об
	// освобождении получила ложную отметку «проверено по существу».
	if got := inv.dict["iam:provider_compensation_outbox"]["event_type"]; len(got) < 2 {
		t.Errorf("словарь iam:provider_compensation_outbox.event_type прочитан как %v — "+
			"проигрывание не применило ЗАМЕНУ ограничения (снятие по имени + постановка "+
			"заново). Читать введение ограничения вместо его текущего состояния — ровно та "+
			"ошибка, которую этот файл и закрывает.", got)
	}
}

// Test_SqlEnumOf_RecognisesDictionaryAndRefusesLookalikes — распознаватель
// словаря, проверенный В ОБЕ СТОРОНЫ на формах, взятых из дерева.
//
// Отрицательная половина без положительной зеленела бы на распознавателе,
// который не признаёт НИЧЕГО; положительная без отрицательной — на том, который
// признаёт ЛЮБОЕ условие с литералами. Поэтому обе половины обязательны, и
// каждая форма ниже действительно встречается в миграциях — это не выдуманные
// входы (координаты не цитируются: имя файла миграции в обратных кавычках
// читается как живое утверждение о дереве, а формы важнее адресов).
func Test_SqlEnumOf_RecognisesDictionaryAndRefusesLookalikes(t *testing.T) {
	dictionaries := []struct {
		name string
		expr string
		col  string
		vals []string
	}{
		{
			name: "голое перечисление",
			expr: `event_type = ANY (ARRAY['fga.tuple.write'::text, 'fga.tuple.delete'::text])`,
			col:  "event_type",
			vals: []string{"fga.tuple.delete", "fga.tuple.write"},
		},
		{
			name: "перечисление в лишних скобках (форма дампа схемы)",
			expr: `((op = ANY (ARRAY['binding_upsert'::text, 'binding_delete'::text])))`,
			col:  "op",
			vals: []string{"binding_delete", "binding_upsert"},
		},
		{
			name: "форма IN",
			expr: `resource_type IN ('nlb_load_balancer','nlb_listener')`,
			col:  "resource_type",
			vals: []string{"nlb_listener", "nlb_load_balancer"},
		},
		{
			name: "нулевая ветка — отсутствие значения отдельным членом",
			expr: `status IS NULL OR status IN ('ACTIVE','DELETING')`,
			col:  "status",
			vals: []string{sqlNullValue, "ACTIVE", "DELETING"},
		},
		{
			name: "квалифицированная колонка",
			expr: `t.state = ANY (ARRAY['NEW'::text])`,
			col:  "state",
			vals: []string{"NEW"},
		},
	}
	for _, tc := range dictionaries {
		t.Run("словарь/"+tc.name, func(t *testing.T) {
			col, vals, ok := sqlEnumOf(tc.expr)
			if !ok {
				t.Fatalf("условие НЕ признано словарём, хотя оно им является:\n  %s\n"+
					"положительный контроль провален — распознаватель, ничего не признающий, "+
					"сделал бы зелёной любую отрицательную половину ниже", tc.expr)
			}
			if col != tc.col || strings.Join(vals, "|") != strings.Join(tc.vals, "|") {
				t.Errorf("условие %s прочитано как %s=%v, ожидалось %s=%v",
					tc.expr, col, vals, tc.col, tc.vals)
			}
		})
	}

	lookalikes := []struct {
		name string
		expr string
		why  string
	}{
		{
			name: "перечисление внутри булева выражения (согласованность)",
			expr: `((status = 'REVOKED'::text) AND (revoked_at IS NOT NULL)) OR ` +
				`((status = ANY (ARRAY['PENDING'::text, 'ACTIVE'::text])) AND (revoked_at IS NULL))`,
			why: "ограничивает СОЧЕТАНИЕ полей; принятое за словарь, оно сужает набор до двух " +
				"значений из трёх — гейт объявил бы недостающее значение лишним в записке",
		},
		{
			name: "перечисление под условием другой колонки",
			expr: `(type = 'INTERNAL' AND placement_type IN ('ZONAL','REGIONAL')) ` +
				`OR (type <> 'INTERNAL' AND placement_type = '')`,
			why: "допустимых значений три, и третье — пустая строка вне перечисления: " +
				"принятое за словарь, условие сочиняет набор, которого у колонки нет",
		},
		{
			name: "запретный список",
			expr: `cur_status NOT IN ('INACTIVE','ACTIVE')`,
			why:  "перечисляет ЗАПРЕЩЁННОЕ; принятое за словарь, оно даёт точную инверсию истины",
		},
		{
			name: "подзапрос",
			expr: `region_id IN (SELECT id FROM regions)`,
			why:  "набор не закрыт текстом миграции вовсе",
		},
		{
			name: "выражение слева",
			expr: `payload ->> 'kind' = ANY (ARRAY['a'::text])`,
			why:  "колонку из выражения не назвать; догадка хуже пропуска",
		},
		{
			name: "нулевая ветка про ДРУГУЮ колонку",
			expr: `revoked_at IS NULL OR status IN ('ACTIVE')`,
			why:  "это тоже согласованность двух полей, а не словарь одного",
		},
	}
	for _, tc := range lookalikes {
		t.Run("не словарь/"+tc.name, func(t *testing.T) {
			if col, vals, ok := sqlEnumOf(tc.expr); ok {
				t.Errorf("условие признано словарём %s=%v, хотя словарём НЕ является: %s\n  %s",
					col, vals, tc.why, tc.expr)
			}
		})
	}
}

// declaredQueueEventDictionary — СЛОВАРЬ СОБЫТИЙ очереди, каким его понимают
// записи об освобождении: очередь → колонка → её допустимые значения.
//
// # Почему ОДНО место, а не поле каждой записи
//
// Освобождений у одной очереди бывает несколько — от ключа порядка
// (commutativeDrainExempt) и от разложения по направлению
// (outboxDirectionSplitExempt), — и оба опираются на ОДИН факт: какие события
// очередь несёт. Пересказанный дважды, факт разъезжается молча: расхождение
// прозы с прозой не даёт ни конфликта при слиянии, ни красного гейта.
//
// Это не гипотеза, и цена ошибочного ПРЕДИКАТА переписи здесь тоже измерена.
// Первая перепись искала ФРАЗУ («событие ровно одного вида», `git grep` по 6148
// отслеживаемым файлам) и дала пять живых мест: две записи об освобождении,
// запись гейта индексов, комментарий боевой проводки и страница архитектуры
// сервиса — плюс комментарий миграции, где фраза была ВЕРНА, потому что описывала
// состояние на тот день. Все пять исправили — и объявили радиус закрытым.
//
// Он закрыт не был. ШЕСТОЕ место стояло внутри литерала настроек дренажа и
// говорило то же самое ПЕРИФРАЗОМ («единственный вид события»), поэтому
// буквальный предикат его не видел; СЕДЬМОЕ — в годке порта эмиссии соседней
// очереди, где перечислялись три значения из семи со ссылкой на само ограничение.
// Оба нашлись, когда предикат переписали с ФРАЗЫ на РЕФЕРЕНТ: (а) комментарии
// РЯДОМ С ОБЪЯВЛЕНИЕМ очереди — референт резолвится разбором, а не текстом, ведь
// комментарий внутри литерала свою очередь по имени не называет (16 проводок,
// 88 комментарных областей, 7 кандидатов, из них живое ложное утверждение одно);
// (б) комментарии, ЦИТИРУЮЩИЕ значения словаря (27 файлов, 929 областей, 18 цитат).
//
// Поэтому здесь — единственная перепись, прочие места на неё ССЫЛАЮТСЯ, и оба
// свойства держатся гейтами: TestCommutativeDrainWiringDelegatesTheDecision (не
// пересказывать обоснование у настроек) и
// TestCommentCitingDictionaryConstraintQuotesItWhole (назвал ограничение —
// перечисли словарь целиком).
//
// # Что с этим делает гейт
//
// TestCommutativeDrainExemptionMatchesEventDictionary сверяет эту карту с
// проигрыванием миграций В ОБЕ СТОРОНЫ. Миграция, расширяющая словарь, красит
// гейт и требует пересмотреть обоснования — вместо того чтобы молча сделать их
// ложью.
var declaredQueueEventDictionary = map[string]map[string][]string{
	"kacho_iam.provider_compensation_outbox": {
		"event_type": {"provider.oauth_client.delete", "provider.trust_grant.delete"},
	},
	// Очередь писем приглашения (#1776). Освобождение у неё ОДНО — от разложения
	// по направлению, — и опирается оно ровно на этот словарь: вид события один,
	// поэтому второй половины у потока нет и разложение разложило бы очередь на
	// неё саму и на пустоту. Ключ порядка у очереди ЕСТЬ (`resource_id`), от него
	// она не освобождена.
	//
	// Миграция, добавившая сюда второй вид, покрасит гейт и потребует пересмотреть
	// обоснование — что и требуется: «разотправить» письмо нельзя, но событие,
	// меняющее СОСТАВ письма до отправки, обратную половину бы завело.
	"kacho_iam.invite_mail_outbox": {
		"event_type": {"mail.invite.send"},
	},
	// Журналы аудита. Закрытого словаря у их ГЛАГОЛА нет и не будет: `event_type`
	// ограничен ФОРМОЙ («ресурс.действие»), а не перечнем, потому что перечень
	// пришлось бы расширять миграцией на каждый новый глагол каждого ресурса.
	// Единственный закрытый словарь этих очередей — состояние доставки, и
	// освобождения выше опираются именно на него: пока состояний два и оба про
	// доставку, никакой ЕДИНИЦЫ СОСТОЯНИЯ, за которую записи могли бы бороться,
	// в очереди нет. Миграция, вернувшая сюда третье состояние, покрасит гейт и
	// потребует пересмотреть оба обоснования — что и требуется.
	"kacho_iam.audit_outbox": {
		"status": {"pending", "sent"},
	},
	"public.audit_outbox": {
		"status": {"pending", "sent"},
	},
	// ЗДЕСЬ СТОЯЛ СЛОВАРЬ ЖУРНАЛА СМЕНЫ СУБЪЕКТА — этому инвентарю он больше не
	// нужен (задача #1024).
	//
	// Инвентарь существует ради ОСВОБОЖДЕНИЙ дренажа: от ключа порядка и от
	// разложения по направлению. Оба опираются на один факт — какие события несёт
	// очередь, — и оба сняты вместе с дренажом: очередь перестала быть очередью и
	// стала журналом с курсором, который читает потребитель.
	//
	// Сам словарь событий НЕ осиротел: его по-прежнему держит ограничение базы
	// (`subject_change_op_check`, миграция 754001) и гейт
	// TestQueueEventValueHasAProducer, требующий производителя у каждого значения.
	// Здесь снята только ЗАПИСЬ ИНВЕНТАРЯ, у которой не осталось освобождения,
	// которое она подпирала бы.
}

// exemptQueueService — сервис, чья проводка дренит очередь. Ключ инвентаря
// словарей несёт сервис (одно имя таблицы бывает у нескольких сервисов), а
// запись об освобождении названа схемой БД, поэтому мост между ними —
// координата проводки, а не догадка «схема kacho_<сервис>».
func exemptQueueService(coords []string) (string, error) {
	svc := ""
	for _, c := range coords {
		s, _, ok := strings.Cut(c, ":")
		if !ok {
			return "", fmt.Errorf("координата проводки %q не несёт сервиса", c)
		}
		if svc != "" && svc != s {
			return "", fmt.Errorf("очередь дренится из РАЗНЫХ сервисов (%s и %s): "+
				"словарь у каждого свой, и какой из них проверять — не определено", svc, s)
		}
		svc = s
	}
	if svc == "" {
		return "", errors.New("координат проводки нет")
	}
	return svc, nil
}

// TestCommutativeDrainExemptionMatchesEventDictionary — записка об освобождении
// от ключа порядка перечисляет словарь событий очереди РОВНО так, как его
// закрывает БД.
//
// # Что этот гейт добавляет к соседнему
//
// Соседний (TestCommutativeDrainExemptionsHaveSubject) проверяет, что у записки
// есть ПРЕДМЕТ: очередь ещё дренится и ключа не получила. Он остаётся зелёным на
// записке, которая про свой предмет говорит НЕПРАВДУ, — и именно это произошло:
// записка называла один вид события, БД к тому дню допускала два. Здесь
// проверяется ИСТИННОСТЬ предпосылки, а не наличие предмета.
//
// # Почему сверка идёт в ОБЕ стороны
//
// Одностороннее «каждое значение словаря упомянуто» зеленело бы на записке,
// перечисляющей лишнее (значение, снятое миграцией, — тот же класс: утверждение,
// пережившее свой предмет). Одностороннее «каждое упомянутое есть в словаре»
// зеленело бы на пустой записке. Поэтому равенство, а не включение.
//
// # Предпосылка гейта и её проверка
//
// Гейт держится на том, что словарь очереди ЗАКРЫТ ограничением БД. Очередь без
// закрытого словаря сверить не с чем — и это находка, а не молчание: иначе
// освобождение снова сертифицировало бы само себя, только теперь тише.
func TestCommutativeDrainExemptionMatchesEventDictionary(t *testing.T) {
	root := repoRoot(t)
	wiring := outboxWiringInventory(t, root)
	dicts := enumDictionaryInventory(t, root, trackedMigrationSQL)

	if wiring.filesRead == 0 || dicts.filesRead == 0 {
		t.Fatalf("гейт не прочитал одну из двух картин (файлов проводок: %d, миграций: %d) — "+
			"предпосылка сломана, молчание ничего не доказывает (корень %s)",
			wiring.filesRead, dicts.filesRead, root)
	}

	// Освобождения обоих видов опираются на один и тот же факт, поэтому и
	// проверяются одним проходом: очередь, освобождённая хоть от чего-то, обязана
	// иметь сверенный словарь.
	exempted := map[string]bool{}
	for table := range commutativeDrainExempt {
		exempted[table] = true
	}
	for table := range outboxDirectionSplitExempt {
		exempted[table] = true
	}
	tables := make([]string, 0, len(exempted))
	for table := range exempted {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	checked := 0
	for _, table := range tables {
		declared, hasDeclared := declaredQueueEventDictionary[table]
		coords, drained := wiring.drained[table]
		if !drained {
			continue // предмета нет — это находка СОСЕДНЕГО гейта, здесь не дублируется
		}
		svc, err := exemptQueueService(coords)
		if err != nil {
			t.Errorf("освобождение %s: %v", table, err)
			continue
		}
		if !hasDeclared {
			t.Errorf("очередь %s освобождена от порядка и/или от разложения по направлению, "+
				"но её словарь событий НИГДЕ не объявлен: сверять обоснование не с чем. "+
				"Добавь запись в declaredQueueEventDictionary — она и есть единственная "+
				"перепись факта, на который эти освобождения опираются.", table)
			continue
		}
		key := svc + ":" + unqualify(table)
		got, closed := dicts.dict[key]
		if !closed {
			t.Errorf("освобождение %s опирается на утверждение о видах события, которое "+
				"НЕЧЕМ проверить: у очереди нет ни одного закрытого словаря в миграциях "+
				"(прочитано %d). Такое освобождение сертифицирует само себя. Закрой словарь "+
				"ограничением `CHECK (<колонка> = ANY (ARRAY[…]))` — тогда следующая "+
				"миграция, расширяющая его, покрасит этот гейт, — либо задай ключ порядка "+
				"и сними освобождение.", table, dicts.filesRead)
			continue
		}
		checked++

		cols := map[string]bool{}
		for c := range got {
			cols[c] = true
		}
		for c := range declared {
			cols[c] = true
		}
		colNames := make([]string, 0, len(cols))
		for c := range cols {
			colNames = append(colNames, c)
		}
		sort.Strings(colNames)

		for _, col := range colNames {
			inDB, hasDB := got[col]
			inNote, hasNote := declared[col]
			switch {
			case hasDB && !hasNote:
				t.Errorf("освобождение %s НЕ называет словарь колонки %q, а БД его закрывает: "+
					"%v (миграция %s). Записка обязана перечислить словарь ЦЕЛИКОМ: пока виды "+
					"события не выписаны, читатель не может ни усомниться в выводе о "+
					"коммутативности, ни убедиться в нём.",
					table, col, inDB, dicts.source[key+":"+col])
			case hasNote && !hasDB:
				t.Errorf("освобождение %s называет словарь колонки %q (%v), которого БД не "+
					"закрывает: утверждение пережило свой предмет — колонку переименовали, "+
					"ограничение сняли, либо словаря там не было никогда.",
					table, col, inNote)
			case strings.Join(inDB, "\x00") != strings.Join(inNote, "\x00"):
				t.Errorf("освобождение %s: словарь колонки %q РАЗОШЁЛСЯ с БД.\n"+
					"  записка: %v\n  БД (%s): %v\n"+
					"Вывод о коммутативности выведен из НЕ ТОГО набора событий и потому "+
					"ничем не обеспечен: пересмотри его по действительному словарю и обнови "+
					"обе половины записи — перечень и обоснование.",
					table, col, inNote, dicts.source[key+":"+col], inDB)
			}
		}
	}

	// Запись, которой больше нечего описывать: очередь ушла из обоих списков.
	for table := range declaredQueueEventDictionary {
		if !exempted[table] {
			t.Errorf("объявленный словарь очереди %s больше не имеет предмета: она не "+
				"освобождена ни от ключа порядка, ни от разложения по направлению. Удали "+
				"запись — иначе она переживёт свой предмет и будет читаться как действующая.",
				table)
		}
	}

	t.Logf("прочитано миграций: %d; освобождённых очередей (порядок и/или направление): %d; "+
		"объявленных словарей: %d; сверено со словарём БД: %d",
		dicts.filesRead, len(exempted), len(declaredQueueEventDictionary), checked)
}
