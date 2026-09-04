// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Разбор дерева для гейта «курсорное чтение страницы обязано получать ПОРЯДОК из
// индекса» (задача #708).
//
// # Предмет
//
// Пагинация продукта курсорная: страница берётся `ORDER BY created_at, id LIMIT
// <размер+1>`, а следующая — предикатом `(created_at, id) > (курсор)`. Под такой
// обход нужен составной индекс, у которого ключи курсора идут ПОДРЯД, в ТОМ ЖЕ
// порядке и в СОГЛАСОВАННОМ направлении. Без него база сортирует всё
// отфильтрованное множество на каждую страницу: цена страницы перестаёт быть
// `O(размер страницы)` и становится `O(число строк арендатора)`.
//
// Случаев ровно два, и второй опаснее:
//
//   - индекса нет вовсе — видно при первом же чтении миграции;
//   - индекс есть, ключи те, а НАПРАВЛЕНИЕ не то. Объявлено `(project_id,
//     created_at DESC, id)`, обход идёт `ORDER BY created_at ASC, id ASC`.
//     Смешанное направление обратным чтением не разворачивается, поэтому индекс
//     не применим — но выглядит он как сделанная работа, и обзор диффа его от
//     настоящего не отличает. Расхождение живёт в одном слове.
//
// # Единица счёта
//
// Пара «(сервис, таблица) × подпись порядка». Не таблица: у одной таблицы бывает
// два курсорных обхода с РАЗНЫМИ подписями (у `access_bindings` их две — прямая
// и обратная), и индекс, обслуживающий одну, о другой не говорит ничего. Не
// чтение: чтений с одной подписью на таблицу бывает несколько, а чинятся они
// одним индексом.
//
// # Что считается курсорным чтением страницы
//
// Строковый литерал прод-кода (не `_test.go`), в котором:
//
//   - есть `ORDER BY` с ДВУМЯ ключами, и первый нормализуется в `created_at`;
//   - есть `LIMIT`, за которым стоит ПАРАМЕТР (`$…` или глагол форматирования).
//
// Требование параметра у `LIMIT` — не косметика: оно отделяет страницу от
// поиска. `… WHERE lower(email) = lower($1) ORDER BY created_at ASC, id ASC
// LIMIT 1` — это чтение одной строки по почти-уникальному предикату, и индекс
// курсора ему не нужен. В дереве таких два, и без этого требования они попали бы
// в находки, а находка без предмета обесценивает перечень целиком.
//
// # Объявленная слепая зона: равенство В ЗАПРОСЕ не сверяется с префиксом индекса
//
// Индекс `(project_id, created_at, id)` обслуживает обход только тогда, когда
// запрос несёт равенство по `project_id`. Условия здесь собираются в Go
// динамически (`if f.ProjectID != "" { conditions = append(…) }`), поэтому
// статический разбор не может установить, обязателен ли префикс. Гейт этого и не
// утверждает: он проверяет, что ключи курсора идут в индексе подряд и в
// согласованном направлении, а перед ними стоит НЕ БОЛЕЕ ОДНОЙ колонки —
// ровно та форма, которую и объявляет продукт («ведущее равенство по проекту,
// затем курсор»). Более глубокий префикс отвергается: `(project_id,
// internal_subnet_id, created_at, id)` обслуживает обход, только если запрос
// несёт ОБА равенства, и зачесть его общему списку значило бы объявить покрытым
// то, что покрыто не будет.
//
// # Частичный индекс: засчитывается ТОЛЬКО дословному чтению
//
// Частичный индекс обслуживает только строки своего предиката. Выводится ли этот
// предикат из условий запроса — вопрос к условиям, а они у списочных чтений
// собираются динамически (см. выше), и доказать импликацию по тексту нельзя.
//
// Есть, однако, случай, где доказывать нечего: запрос несёт предикат индекса
// ДОСЛОВНО и всегда — не как одну из собираемых веток, а как константную часть
// оператора. Тогда множество строк запроса и множество строк индекса совпадают
// by construction. Такой индекс засчитывается; всякий другой частичный — нет.
//
// Здесь ДОЛГО стояло безусловное «частичный индекс покрытием не считается» с
// обоснованием «на дереве не даёт ни одной ложной находки». Обоснование
// истекло: вывоз журнала аудита (#812) читает голову очереди константным
// предикатом `status <> 'sent'` и обслуживается частичным индексом ровно по
// нему — и безусловное правило дало ДВЕ ложные находки, по одной на службу.
// Сужение осталось безопасным в прежнюю сторону: `operations_account_id_idx
// (account_id, created_at, id) WHERE account_id IS NOT NULL` общему списку
// операций по-прежнему не засчитывается — тот список по счёту не фильтрует и
// предиката индекса не несёт.

// CursorKey — один ключ порядка: колонка и направление.
type CursorKey struct {
	Column string
	Desc   bool
}

// String — подпись ключа в том виде, в каком её пишут в SQL.
func (k CursorKey) String() string {
	if k.Desc {
		return k.Column + " DESC"
	}
	return k.Column + " ASC"
}

// cursorSignature — подпись порядка целиком.
func cursorSignature(keys []CursorKey) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k.String())
	}
	return strings.Join(parts, ", ")
}

// CursorRead — курсорное чтение страницы, найденное в прод-коде.
type CursorRead struct {
	File string // путь относительно корня репозитория
	Line int
	// SQL — текст запроса, приведённый к сравнимому виду. Нужен ровно для
	// одного вопроса: несёт ли чтение предикат частичного индекса.
	SQL    string
	Table  string // имя таблицы без схемы; пусто, если не разрешилось
	Keys   []CursorKey
	Shared bool // чтение под `pkg/` — общее для КАЖДОГО сервиса, у кого есть такая таблица
	Marked bool // таблица взята из объявления `cursor-list-table:` рядом с чтением
}

// DeclaredIndex — индекс, объявленный миграциями и доживший до конца цепочки.
type DeclaredIndex struct {
	Name    string
	Table   string
	Cols    []CursorKey
	Partial bool
	// Predicate — предикат частичного индекса, приведённый к сравнимому виду
	// (нижний регистр, схлопнутые пробелы). Пусто у полного индекса.
	Predicate string
	File      string
}

// String — индекс в том виде, в каком его читает человек.
func (d DeclaredIndex) String() string {
	s := fmt.Sprintf("%s (%s)", d.Name, cursorSignature(d.Cols))
	if d.Partial {
		s += " [частичный]"
	}
	return s + "  [" + d.File + "]"
}

// CursorFinding — пара «(сервис, таблица) × подпись», для которой служащего
// индекса нет.
type CursorFinding struct {
	Service    string
	Table      string
	Order      string
	Reads      []CursorRead
	Candidates []DeclaredIndex // индексы этой таблицы, чьи ключи содержат created_at — чтобы направление было видно
}

// CursorCensus — исход обхода. Объём осмотренного входит в исход, а не в лог:
// без него «ноль находок» неотличимо от «ноль прочитанного».
type CursorCensus struct {
	GoFiles    int
	GoLiterals int
	SQLFiles   int
	Services   []string
	LiveTables int
	Indexes    int

	Reads      []CursorRead
	Unresolved []CursorRead // чтение есть, таблицу назвать нечем и объявления рядом нет

	Pairs    int // проверено пар «(сервис, таблица) × подпись»
	Covered  int
	Findings []CursorFinding
}

// cursorTableMarker — объявление таблицы рядом с чтением, у которого имя таблицы
// вычисляется в Go и потому в литерале отсутствует.
//
// Такое чтение ровно одно на всё дерево — общий список операций (`FROM %s`, имя
// собирает `pgRepo.tableName()`), и именно его пропустил замер, с которого
// задача #708 началась: его предикат искал `FROM <литерал>`. Пропущено было семь
// физических таблиц — по одной на сервис, и это самый нагруженный список
// продукта. Объявление самоистекает: уйдёт чтение — уйдёт и оно; появится новое
// такое же — гейт потребует объявления, а не промолчит.
var cursorTableMarker = regexp.MustCompile(`cursor-list-table:\s*([a-z_][a-z0-9_]*)`)

var (
	// cursorOrderRe — `ORDER BY <ключ>[ ASC|DESC], <ключ>[ ASC|DESC]`.
	cursorOrderRe = regexp.MustCompile(`(?is)ORDER\s+BY\s+([\w.]+)(?:\s+(ASC|DESC))?\s*,\s*([\w.]+)(?:\s+(ASC|DESC))?`)
	// cursorLimitRe — `LIMIT`, за которым стоит ПАРАМЕТР: `$1`, `$%d`, `%d`.
	cursorLimitRe = regexp.MustCompile(`(?is)\bLIMIT\s+[$%]`)
	// sqlFromRe — источник строк: `FROM t`, `FROM s.t t2`, `JOIN t AS x`.
	sqlFromRe = regexp.MustCompile(`(?is)\b(?:FROM|JOIN)\s+([\w."%]+)(?:\s+(?:AS\s+)?([a-z][\w]*))?`)
	// sqlAliasStop — слова, которые псевдонимом не являются.
	sqlAliasStop = map[string]bool{
		"where": true, "order": true, "group": true, "limit": true, "on": true,
		"left": true, "right": true, "inner": true, "outer": true, "cross": true,
		"join": true, "union": true, "having": true, "offset": true, "for": true,
		"lateral": true, "as": true, "and": true, "or": true, "using": true,
	}

	// createIdxRe / cursorDropIdxRe / createTblRe / dropTblRe / renameTblRe — проигрывание
	// Up-секций. Читается ИСПОЛНЯЕМАЯ часть (`migrationUpSection`): `CREATE INDEX`,
	// приведённый в комментарии примером, индексом не является.
	createIdxRe = regexp.MustCompile(
		`(?is)CREATE\s+(UNIQUE\s+)?INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+NOT\s+EXISTS)?\s+([\w."]+)\s+ON\s+(?:ONLY\s+)?([\w."]+)\s*(?:USING\s+\w+\s*)?\(`)
	cursorDropIdxRe = regexp.MustCompile(`(?is)DROP\s+INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+EXISTS)?\s+([\w."]+)\s*;`)
	createTblRe     = regexp.MustCompile(`(?is)CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+([\w."]+)`)
	dropTblRe       = regexp.MustCompile(`(?is)DROP\s+TABLE(?:\s+IF\s+EXISTS)?\s+([\w."]+)`)
	renameTblRe     = regexp.MustCompile(`(?is)ALTER\s+TABLE(?:\s+IF\s+EXISTS)?\s+([\w."]+)\s+RENAME\s+TO\s+([\w."]+)`)
	// indexPredicateRe — предикат частичного индекса сразу за списком колонок.
	// Вторая группа — САМ предикат: он нужен, чтобы отличить частичный индекс,
	// чей предикат читающий запрос несёт дословно, от частичного индекса, о
	// применимости которого судить нечем (см. §«Частичный индекс» в шапке).
	indexPredicateRe = regexp.MustCompile(`(?is)^\s*WHERE\b(.*)$`)
)

// SurveyCursorIndexes сводит курсорные чтения прод-кода с индексами, которые
// объявляют миграции.
//
// Состав дерева приходит СОСТАВЛЕННЫМ (`treecorpus.Tree`), а не собирается здесь
// обходом диска: под `services/` и `gateway/` на всякой машине, где поднимали
// стенд, лежат игнорируемые каталоги, и вердикт, собранный обходом файловой
// системы, стал бы свойством рабочего каталога, а не коммита. Конструктор
// выбирает ВЫЗЫВАЮЩИЙ: гейт берёт `treecorpus.NewTree` (индекс git),
// инъекционная проба — `treecorpus.SyntheticTree` (её дерево репозиторием не
// является, спрашивать у него индекс нечего).
func SurveyCursorIndexes(tree *treecorpus.Tree) (CursorCensus, error) {
	var c CursorCensus

	tables, indexes, sqlFiles, services, err := replayMigrationIndexes(tree)
	if err != nil {
		return c, err
	}
	c.SQLFiles = sqlFiles
	c.Services = services
	for _, t := range tables {
		c.LiveTables += len(t)
	}
	for _, ix := range indexes {
		c.Indexes += len(ix)
	}

	reads, goFiles, literals, err := collectCursorReads(tree)
	if err != nil {
		return c, err
	}
	c.GoFiles = goFiles
	c.GoLiterals = literals

	// Пары «(сервис, таблица) × подпись»: чтение под `pkg/` — общее, оно
	// принадлежит КАЖДОМУ сервису, у кого такая таблица есть.
	type pairKey struct{ svc, table, order string }
	pairs := map[pairKey][]CursorRead{}
	for _, r := range reads {
		if r.Table == "" {
			c.Unresolved = append(c.Unresolved, r)
			continue
		}
		c.Reads = append(c.Reads, r)
		sig := cursorSignature(r.Keys)
		owners := []string{}
		if r.Shared {
			for _, svc := range services {
				if tables[svc][r.Table] {
					owners = append(owners, svc)
				}
			}
		} else if svc := cursorServiceOfPath(r.File); svc != "" && tables[svc][r.Table] {
			owners = append(owners, svc)
		}
		for _, svc := range owners {
			k := pairKey{svc, r.Table, sig}
			pairs[k] = append(pairs[k], r)
		}
	}

	keys := make([]pairKey, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].svc != keys[j].svc {
			return keys[i].svc < keys[j].svc
		}
		if keys[i].table != keys[j].table {
			return keys[i].table < keys[j].table
		}
		return keys[i].order < keys[j].order
	})

	for _, k := range keys {
		c.Pairs++
		want := pairs[k][0].Keys
		var candidates []DeclaredIndex
		served := false
		for _, ix := range indexes[k.svc] {
			if ix.Table != k.table {
				continue
			}
			if indexMentions(ix, want[0].Column) {
				candidates = append(candidates, ix)
			}
			if indexServesCursor(ix, want, pairs[k]) {
				served = true
			}
		}
		if served {
			c.Covered++
			continue
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
		c.Findings = append(c.Findings, CursorFinding{
			Service:    k.svc,
			Table:      k.table,
			Order:      k.order,
			Reads:      pairs[k],
			Candidates: candidates,
		})
	}
	return c, nil
}

// indexServesCursor — может ли индекс отдать порядок `want` без узла сортировки.
//
// Условия, все обязательны:
//
//   - индекс полный ЛИБО частичный, чей предикат КАЖДОЕ чтение пары несёт
//     дословно (см. §«Частичный индекс» в шапке файла);
//   - ключи `want` лежат в нём ПОДРЯД, теми же колонками и в том же порядке;
//   - перед ними стоит не более ОДНОЙ колонки (ведущее равенство);
//   - направления совпадают ЦЕЛИКОМ либо инвертированы ЦЕЛИКОМ. Второе законно:
//     btree читается в обе стороны, и `(created_at DESC, id DESC)` отдаёт
//     `created_at ASC, id ASC` обратным чтением. Смешанное — не отдаёт ни
//     прямым, ни обратным, и это тот самый случай, который выглядит сделанным.
func indexServesCursor(ix DeclaredIndex, want []CursorKey, reads []CursorRead) bool {
	if len(want) == 0 {
		return false
	}
	if ix.Partial && !partialIndexApplies(ix, reads) {
		return false
	}
	for k := 0; k <= 1; k++ {
		if k+len(want) > len(ix.Cols) {
			return false
		}
		same, flipped := true, true
		for i, w := range want {
			got := ix.Cols[k+i]
			if got.Column != w.Column {
				same, flipped = false, false
				break
			}
			if got.Desc != w.Desc {
				same = false
			} else {
				flipped = false
			}
		}
		if same || flipped {
			return true
		}
	}
	return false
}

// indexMentions — несёт ли индекс названную колонку. Нужен только для
// диагностики: находка обязана показать индекс, который ВЫГЛЯДИТ подходящим, —
// иначе читатель заведёт второй такой же вместо того, чтобы починить
// направление.
func indexMentions(ix DeclaredIndex, column string) bool {
	for _, c := range ix.Cols {
		if c.Column == column {
			return true
		}
	}
	return false
}

// cursorServiceOfPath — сервис, которому принадлежит файл прод-кода.
func cursorServiceOfPath(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[1]
	}
	return ""
}

// replayMigrationIndexes проигрывает Up-секции всех миграций каждого сервиса в
// порядке версий и возвращает то состояние схемы, к которому пришёл бы Postgres:
// живые таблицы и живые индексы.
//
// Порядок операторов — ТЕКСТОВЫЙ: перестройка индекса в одной миграции («снять и
// тут же создать заново») законна и в дереве встречается; двумя раздельными
// проходами она читалась бы как «снят».
//
// Порядок ФАЙЛОВ — числовой по версии, а не лексикографический: номер миграции
// ВЫВОДИТСЯ (сегодня — из метки времени заведения, прежде — из номера задачи),
// поэтому эры перемешаны лексикографически: `1000001` меньше `539001` как
// строка и больше как число. Форма объявлена в
// docs/architecture/migration-version-namespace.md.
func replayMigrationIndexes(tree *treecorpus.Tree) (map[string]map[string]bool, map[string][]DeclaredIndex, int, []string, error) {
	perService := map[string][]string{}
	for _, rel := range tree.SortedFiles() {
		parts := strings.Split(rel, "/")
		if len(parts) < 5 || parts[0] != "services" || parts[2] != "internal" || parts[3] != "migrations" {
			continue
		}
		if !strings.HasSuffix(rel, ".sql") {
			continue
		}
		perService[parts[1]] = append(perService[parts[1]], rel)
	}

	tables := map[string]map[string]bool{}
	indexes := map[string][]DeclaredIndex{}
	var services []string
	files := 0

	for svc, rels := range perService {
		services = append(services, svc)
		sortMigrationsByVersion(rels)

		live := map[string]bool{}
		byName := map[string]DeclaredIndex{}
		var order []string

		for _, rel := range rels {
			body, rerr := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
			if rerr != nil {
				return nil, nil, 0, nil, fmt.Errorf("чтение %s: %w", rel, rerr)
			}
			files++
			up := migrationUpSection(string(body))
			for _, op := range schemaOpsInTextOrder(up, path.Base(rel)) {
				switch op.kind {
				case opCreateTable:
					live[op.name] = true
				case opDropTable:
					delete(live, op.name)
					for n, ix := range byName {
						if ix.Table == op.name {
							delete(byName, n)
						}
					}
				case opRenameTable:
					if live[op.name] {
						delete(live, op.name)
						live[op.to] = true
					}
					for n, ix := range byName {
						if ix.Table == op.name {
							ix.Table = op.to
							byName[n] = ix
						}
					}
				case opCreateIndex:
					if _, seen := byName[op.name]; !seen {
						order = append(order, op.name)
					}
					byName[op.name] = op.index
				case opDropIndex:
					delete(byName, op.name)
				}
			}
		}
		tables[svc] = live
		for _, n := range order {
			if ix, ok := byName[n]; ok {
				indexes[svc] = append(indexes[svc], ix)
			}
		}
	}
	sort.Strings(services)
	return tables, indexes, files, services, nil
}

// sortMigrationsByVersion упорядочивает файлы миграций ЧИСЛОМ версии — так их
// применяет мигратор. Файл без числового префикса уходит в конец по имени.
func sortMigrationsByVersion(paths []string) {
	num := func(p string) (int64, string) {
		base := path.Base(p)
		i := 0
		for i < len(base) && base[i] >= '0' && base[i] <= '9' {
			i++
		}
		if i == 0 {
			return -1, base
		}
		v, err := strconv.ParseInt(base[:i], 10, 64)
		if err != nil {
			return -1, base
		}
		return v, base
	}
	sort.Slice(paths, func(i, j int) bool {
		vi, bi := num(paths[i])
		vj, bj := num(paths[j])
		if vi != vj {
			if vi < 0 {
				return false
			}
			if vj < 0 {
				return true
			}
			return vi < vj
		}
		return bi < bj
	})
}

type schemaOpKind int

const (
	opCreateTable schemaOpKind = iota
	opDropTable
	opRenameTable
	opCreateIndex
	opDropIndex
)

type schemaOp struct {
	at    int
	kind  schemaOpKind
	name  string
	to    string
	index DeclaredIndex
}

// schemaOpsInTextOrder возвращает операторы Up-секции в порядке их смещения в
// тексте — том, в каком их исполнил бы Postgres.
func schemaOpsInTextOrder(up, file string) []schemaOp {
	ops := make([]schemaOp, 0, 8)
	for _, m := range createTblRe.FindAllStringSubmatchIndex(up, -1) {
		ops = append(ops, schemaOp{at: m[0], kind: opCreateTable, name: unqualifyIdent(up[m[2]:m[3]])})
	}
	for _, m := range dropTblRe.FindAllStringSubmatchIndex(up, -1) {
		ops = append(ops, schemaOp{at: m[0], kind: opDropTable, name: unqualifyIdent(up[m[2]:m[3]])})
	}
	for _, m := range renameTblRe.FindAllStringSubmatchIndex(up, -1) {
		ops = append(ops, schemaOp{
			at: m[0], kind: opRenameTable,
			name: unqualifyIdent(up[m[2]:m[3]]), to: unqualifyIdent(up[m[4]:m[5]]),
		})
	}
	for _, m := range cursorDropIdxRe.FindAllStringSubmatchIndex(up, -1) {
		ops = append(ops, schemaOp{at: m[0], kind: opDropIndex, name: unqualifyIdent(up[m[2]:m[3]])})
	}
	for _, m := range createIdxRe.FindAllStringSubmatchIndex(up, -1) {
		// Список колонок читается со СЧЁТОМ скобок: `COALESCE(zone_id, '')` и
		// `((external_ipv4 ->> 'address'))` содержат запятые и скобки внутри, и
		// разбор «до первой закрывающей» разорвал бы их посередине.
		cols, end, ok := balancedParenBody(up, m[1]-1)
		if !ok {
			continue
		}
		partial, predicate := false, ""
		if tail := up[end:]; len(tail) > 0 {
			stop := strings.IndexByte(tail, ';')
			if stop < 0 {
				stop = len(tail)
			}
			if m := indexPredicateRe.FindStringSubmatch(tail[:stop]); m != nil {
				partial, predicate = true, normalizeSQLText(m[1])
			}
		}
		ops = append(ops, schemaOp{
			at: m[0], kind: opCreateIndex, name: unqualifyIdent(up[m[4]:m[5]]),
			index: DeclaredIndex{
				Name:      unqualifyIdent(up[m[4]:m[5]]),
				Table:     unqualifyIdent(up[m[6]:m[7]]),
				Cols:      parseIndexColumns(cols),
				Partial:   partial,
				Predicate: predicate,
				File:      file,
			},
		})
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].at < ops[j].at })
	return ops
}

// balancedParenBody читает тело скобки, начинающейся в позиции open, со счётом
// вложенности и с пропуском строковых литералов.
func balancedParenBody(s string, open int) (body string, end int, ok bool) {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return "", 0, false
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\'':
			i++
			for i < len(s) && s[i] != '\'' {
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[open+1 : i], i + 1, true
			}
		}
	}
	return "", 0, false
}

// parseIndexColumns разбирает список колонок индекса: имя колонки плюс
// направление. Класс операторов (`jsonb_path_ops`) и `NULLS FIRST/LAST`
// отбрасываются — на порядок ключей курсора они не влияют. Выражение
// (`lower(email)`, `((external_ipv4 ->> 'address'))`) остаётся как есть и с
// именем колонки не совпадёт, что и требуется.
func parseIndexColumns(list string) []CursorKey {
	var out []CursorKey
	depth := 0
	start := 0
	flush := func(item string) {
		item = strings.TrimSpace(item)
		if item == "" {
			return
		}
		desc := false
		fields := strings.Fields(item)
		for _, f := range fields[1:] {
			switch strings.ToUpper(f) {
			case "DESC":
				desc = true
			case "ASC":
				desc = false
			}
		}
		out = append(out, CursorKey{Column: strings.Trim(fields[0], `"`), Desc: desc})
	}
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '\'':
			i++
			for i < len(list) && list[i] != '\'' {
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				flush(list[start:i])
				start = i + 1
			}
		}
	}
	flush(list[start:])
	return out
}

// unqualifyIdent отбрасывает схему и кавычки: одна и та же таблица встречается в
// миграциях и с квалификатором, и без него.
func unqualifyIdent(ident string) string {
	ident = strings.ReplaceAll(ident, `"`, "")
	if i := strings.LastIndex(ident, "."); i >= 0 {
		ident = ident[i+1:]
	}
	return ident
}

// collectCursorReads обходит прод-код (`services/`, `pkg/`, `gateway/`) и
// возвращает курсорные чтения страницы.
//
// Разбирается СИНТАКСИЧЕСКОЕ дерево, а не сырой текст: `ORDER BY`, приведённый в
// комментарии, чтением не является, а в дереве таких комментариев много —
// репозитории объясняют свой обход прозой прямо над запросом.
func collectCursorReads(tree *treecorpus.Tree) ([]CursorRead, int, int, error) {
	var reads []CursorRead
	files, literals := 0, 0

	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		top := rel
		if i := strings.Index(rel, "/"); i >= 0 {
			top = rel[:i]
		}
		if top != "services" && top != "pkg" && top != "gateway" {
			continue
		}
		files++
		abs := filepath.Join(tree.Root(), filepath.FromSlash(rel))
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, abs, nil, parser.ParseComments)
		if perr != nil {
			return nil, 0, 0, fmt.Errorf("разбор %s: %w", rel, perr)
		}

		// Все литералы файла — сперва для связывания псевдонимов, затем для
		// поиска чтений. Псевдоним (`ORDER BY v.created_at`) бывает связан в
		// ДРУГОМ литерале того же файла: список колонок и `FROM` хранятся
		// константой (`snapshotFrom`, `diskTypeSelect`), а запрос собирается
		// форматированием.
		var lits []struct {
			text string
			line int
		}
		ast.Inspect(f, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				return true
			}
			text, uerr := strconv.Unquote(bl.Value)
			if uerr != nil {
				return true
			}
			literals++
			lits = append(lits, struct {
				text string
				line int
			}{text, fset.Position(bl.Pos()).Line})
			return true
		})

		aliases := map[string]string{}
		for _, l := range lits {
			for alias, table := range sqlAliasBindings(l.text) {
				if _, seen := aliases[alias]; !seen {
					aliases[alias] = table
				}
			}
		}
		marker := ""
		for _, cg := range f.Comments {
			if m := cursorTableMarker.FindStringSubmatch(cg.Text()); m != nil {
				marker = m[1]
				break
			}
		}

		for _, l := range lits {
			keys, ok := cursorOrderOf(l.text)
			if !ok {
				continue
			}
			table, marked := resolveCursorTable(l.text, keys, aliases, marker)
			reads = append(reads, CursorRead{
				File:   rel,
				Line:   l.line,
				SQL:    normalizeSQLText(l.text),
				Table:  table,
				Keys:   keys,
				Shared: !strings.HasPrefix(rel, "services/"),
				Marked: marked,
			})
		}
	}
	sort.Slice(reads, func(i, j int) bool {
		if reads[i].File != reads[j].File {
			return reads[i].File < reads[j].File
		}
		return reads[i].Line < reads[j].Line
	})
	return reads, files, literals, nil
}

// cursorOrderOf — ключи курсорного порядка литерала, если он им является.
func cursorOrderOf(sql string) ([]CursorKey, bool) {
	if !cursorLimitRe.MatchString(sql) {
		return nil, false
	}
	m := cursorOrderRe.FindStringSubmatch(sql)
	if m == nil {
		return nil, false
	}
	first := stripQualifier(m[1])
	if first != "created_at" {
		return nil, false
	}
	return []CursorKey{
		{Column: first, Desc: strings.EqualFold(m[2], "DESC")},
		{Column: stripQualifier(m[3]), Desc: strings.EqualFold(m[4], "DESC")},
	}, true
}

// stripQualifier отбрасывает псевдоним таблицы у имени колонки: `v.created_at`.
func stripQualifier(col string) string {
	if i := strings.LastIndex(col, "."); i >= 0 {
		return col[i+1:]
	}
	return col
}

// sqlAliasBindings — связывание псевдонимов литерала: `FROM volumes v`,
// `JOIN volume_attachments va ON …`.
func sqlAliasBindings(sql string) map[string]string {
	out := map[string]string{}
	for _, m := range sqlFromRe.FindAllStringSubmatch(sql, -1) {
		table := normalizeSQLTable(m[1])
		alias := strings.ToLower(m[2])
		if table == "" || alias == "" || sqlAliasStop[alias] {
			continue
		}
		out[alias] = table
	}
	return out
}

// resolveCursorTable — таблица, страницу которой читает этот литерал.
//
// Порядок: псевдоним ключа курсора → первая таблица `FROM` самого литерала →
// объявление `cursor-list-table:` рядом. Возвращает ещё и признак того, что имя
// взято из объявления, — чтобы перепись могла отличить разобранное от
// объявленного.
func resolveCursorTable(sql string, keys []CursorKey, aliases map[string]string, marker string) (string, bool) {
	if m := cursorOrderRe.FindStringSubmatch(sql); m != nil {
		if i := strings.LastIndex(m[1], "."); i >= 0 {
			if t, ok := aliases[strings.ToLower(m[1][:i])]; ok {
				return t, false
			}
		}
	}
	for _, m := range sqlFromRe.FindAllStringSubmatch(sql, -1) {
		if t := normalizeSQLTable(m[1]); t != "" {
			return t, false
		}
	}
	if marker != "" {
		return marker, true
	}
	return "", false
}

// partialIndexApplies — обслуживает ли частичный индекс ЭТИ чтения.
//
// Предикат индекса обязан стоять КОНЪЮНКТОМ ВЕРХНЕГО УРОВНЯ в `WHERE` каждого
// чтения пары. Это и есть тот единственный случай, где импликация доказывается
// текстом: строки запроса — подмножество строк индекса, потому что запрос
// требует того же условия И ещё чего-то.
//
// > Здесь стояло `strings.Contains`, и оно было ШИРЕ починки. Опыт на харнессе
// > самого гейта: индекс `WHERE project_id <> ”` засчитывался чтению
// > `WHERE project_id <> ” OR created_at < $1` (чтение ШИРЕ индекса), чтению
// > `WHERE NOT (project_id <> ”)` (дополнение множества) и даже тексту, где
// > предикат стоял в КОММЕНТАРИИ, — то есть на стороне чтения возвращался ровно
// > тот класс, который сосед по файлу закрывает на стороне индекса
// > (Test_CursorIndexGate_RedsOnIndexNamedOnlyInAComment).
//
// Пустой набор чтений — НЕ основание засчитать: «нечего проверить» и «проверено»
// обязаны различаться.
func partialIndexApplies(ix DeclaredIndex, reads []CursorRead) bool {
	if ix.Predicate == "" || len(reads) == 0 {
		return false
	}
	want := trimOuterParens(ix.Predicate)
	if want == "" {
		return false
	}
	for _, r := range reads {
		if r.SQL == "" {
			return false
		}
		found := false
		for _, c := range whereConjuncts(r.SQL) {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// whereConjuncts — конъюнкты `WHERE`, УПРАВЛЯЮЩЕГО упорядоченным обходом.
//
// Уровень выбирается не «нулевой», а ТОТ ЖЕ, на котором стоит `ORDER BY`, — и
// это не педантизм: клейм очереди журнала отбирает строки подзапросом
// (`WHERE id IN (SELECT … WHERE status <> 'sent' … ORDER BY created_at, id)`),
// поэтому предикат индекса живёт на глубине один, а на нулевой стоит совсем
// другое условие. Разбор, знающий только нулевую глубину, объявил бы такое
// чтение непокрытым — то есть ровно свой предмет.
//
// Внутри выбранного уровня конъюнкты режутся по `AND` на ЭТОЙ глубине и вне
// строковых литералов: `AND` в скобках или в литерале конъюнктом уровня не
// является, а `OR` на уровне оставляет всю часть одним конъюнктом — и она с
// предикатом индекса не совпадёт, что и требуется.
func whereConjuncts(sql string) []string {
	type mark struct {
		at, depth, width int
		order            bool
	}
	var marks []mark
	depth, inStr := 0, false
	for k := 0; k < len(sql); k++ {
		switch c := sql[k]; {
		case c == '\'':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case isWordAt(sql, k, "where"):
			marks = append(marks, mark{at: k, depth: depth, width: len("where")})
		case isWordAt(sql, k, "order by"):
			marks = append(marks, mark{at: k, depth: depth, width: len("order by"), order: true})
		}
	}

	// Управляющий `WHERE` — последний перед первым `ORDER BY` на ЕГО глубине.
	// Нет `ORDER BY` (курсорным чтением такой запрос быть не может, но разбор
	// обязан быть тотальным) — берём первый `WHERE` нулевой глубины.
	var w *mark
	var stopAt = len(sql)
	for i := range marks {
		if !marks[i].order {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if !marks[j].order && marks[j].depth == marks[i].depth {
				w, stopAt = &marks[j], marks[i].at
				break
			}
		}
		break
	}
	if w == nil {
		for i := range marks {
			if !marks[i].order && marks[i].depth == 0 {
				w = &marks[i]
				break
			}
		}
	}
	if w == nil {
		return nil
	}

	body := sql[w.at+w.width : stopAt]
	// Хвост отрезается по первому слову, которым `WHERE` заканчивается, если
	// `ORDER BY` его не отрезал. Список закрытый: неизвестное слово останется в
	// последнем конъюнкте, и он просто не совпадёт — в безопасную сторону.
	for _, stop := range []string{"group by", "having", "window", "limit", "offset",
		"fetch", "for update", "for share", "returning", "union"} {
		if j := indexOfWordAtDepth(body, stop, 0); j >= 0 {
			body = body[:j]
		}
	}

	var out []string
	depth, inStr = 0, false
	startIdx := 0
	for k := 0; k < len(body); k++ {
		switch c := body[k]; {
		case c == '\'':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth < 0 {
				return append(out, trimOuterParens(strings.TrimSpace(body[startIdx:k])))
			}
		case depth == 0 && isWordAt(body, k, "and"):
			out = append(out, trimOuterParens(strings.TrimSpace(body[startIdx:k])))
			k += len("and") - 1
			startIdx = k + 1
		}
	}
	return append(out, trimOuterParens(strings.TrimSpace(body[startIdx:])))
}

// indexOfWordAtDepth — позиция слова на заданной скобочной глубине вне литерала.
func indexOfWordAtDepth(s, word string, want int) int {
	depth, inStr := 0, false
	for k := 0; k < len(s); k++ {
		switch c := s[k]; {
		case c == '\'':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == want && isWordAt(s, k, word):
			return k
		}
	}
	return -1
}

// isWordAt — стоит ли в позиции k слово word ЦЕЛИКОМ (не часть идентификатора).
func isWordAt(s string, k int, word string) bool {
	if k+len(word) > len(s) || s[k:k+len(word)] != word {
		return false
	}
	isIdent := func(c byte) bool {
		return c == '_' || c == '$' || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c >= 0x80
	}
	if k > 0 && isIdent(s[k-1]) {
		return false
	}
	if e := k + len(word); e < len(s) && isIdent(s[e]) {
		return false
	}
	return true
}

// trimOuterParens снимает скобки, охватывающие выражение ЦЕЛИКОМ.
func trimOuterParens(s string) string {
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		depth, inStr, whole := 0, false, true
		for k := 0; k < len(s); k++ {
			switch c := s[k]; {
			case c == '\'':
				inStr = !inStr
			case inStr:
			case c == '(':
				depth++
			case c == ')':
				depth--
				if depth == 0 && k != len(s)-1 {
					whole = false
				}
			}
			if !whole {
				break
			}
		}
		if !whole {
			return s
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// normalizeSQLText приводит текст к сравнимому виду: комментарии сняты, пробелы
// схлопнуты, регистр опущен ВНЕ строковых литералов, явное приведение типа СНЯТО
// с строкового литерала.
//
// Каждое из четырёх — необходимость, а не удобство. Комментарий не исполняется, и
// предикат, стоящий в нём, обслуживанием не является. Регистр внутри литерала
// СОХРАНЯЕТСЯ: `'SENT'` и `'sent'` — разные значения, и опускать их к одному
// значило бы засчитывать индекс, построенный по другому множеству строк.
//
// Четвёртое заведено 2026-09-04 вместе со сводом миграций iam и стоит объяснения,
// потому что снимает РАЗЛИЧИЕ, а всякое снятие различия рискует расширить
// засчитываемое. Предикат частичного индекса объявляют двумя формами одного и
// того же: рука пишет `WHERE status <> 'sent'`, `pg_dump` — `WHERE (status <>
// 'sent'::text)`. Форма дампа не редкость и не край: свод — файл, который написал
// инструмент, и на 2026-09-04 в предикатах индексов дерева 45 приведений `::text`,
// 4 `::jsonb`, по одному `::inet` и `::timestamp with time zone`. Разбор, знающий
// одну форму, объявляет обслуживающий индекс не обслуживающим — и находка
// указывает на схему, которая ни в чём не виновата (наблюдалось: `iam.audit_outbox`,
// индекс с точно теми же ключами и тем же предикатом, что и чтение).
//
// Снятие УЗКОЕ намеренно: приведение снимается ТОЛЬКО стоящее непосредственно на
// строковом литерале. Приведение на колонке смысл предиката меняет — `created_at::date
// = $1` и `created_at = $1` отбирают разные строки, — и снять его значило бы вернуть
// тот самый класс «шире починки», против которого написан [partialIndexApplies].
// Предикат узости проверяемый: на 2026-09-04 приведений НЕ на литерале в предикатах
// индексов дерева ноль, то есть узкое правило покрывает весь наблюдаемый корпус.
// Различие значений при этом уцелевает: `'SENT'::text` и `'sent'` остаются разными
// (проба Test_CursorIndexGate_PredicateComparisonKeepsLiteralCase и её близнец
// Test_CursorIndexGate_CastStrippingDoesNotEquateDifferentLiterals).
func normalizeSQLText(s string) string {
	var b strings.Builder
	inStr, line, block, pendingSpace := false, false, false, false
	space := func() {
		if b.Len() > 0 {
			pendingSpace = true
		}
	}
	for k := 0; k < len(s); k++ {
		c := s[k]
		switch {
		case line:
			if c == '\n' {
				line = false
				space()
			}
			continue
		case block:
			if c == '*' && k+1 < len(s) && s[k+1] == '/' {
				block = false
				k++
				space()
			}
			continue
		case !inStr && c == '-' && k+1 < len(s) && s[k+1] == '-':
			line = true
			k++
			continue
		case !inStr && c == '/' && k+1 < len(s) && s[k+1] == '*':
			block = true
			k++
			continue
		}
		if !inStr && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			space()
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		if c == '\'' {
			inStr = !inStr
			b.WriteByte(c)
			continue
		}
		if !inStr && c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return stripCastsOnLiterals(strings.TrimSpace(b.String()))
}

// stripCastsOnLiterals снимает явное приведение типа, стоящее НЕПОСРЕДСТВЕННО на
// строковом литерале: `'sent'::text` → `'sent'`.
//
// Работает по УЖЕ нормализованному тексту (пробелы схлопнуты, регистр вне литералов
// опущен) — поэтому лексика проста и разбор не повторяет работу [normalizeSQLText].
//
// Границы, каждая из которых удерживает узость:
//
//   - приведение засчитывается только сразу за ЗАКРЫВАЮЩЕЙ кавычкой литерала;
//     `created_at::date` не трогается вовсе, потому что перед `::` стоит имя;
//   - многословные имена типов берутся из ЗАКРЫТОГО списка ([castTypeTails]) —
//     иначе `'x'::text and y` съело бы `and` и разорвало конъюнкт;
//   - неузнанный хвост оставляется на месте: разбор возвращает исходную позицию,
//     и различие уцелевает — то есть ошибка идёт в сторону находки, а не молчания.
func stripCastsOnLiterals(s string) string {
	var b strings.Builder
	inStr := false
	for k := 0; k < len(s); k++ {
		c := s[k]
		b.WriteByte(c)
		if c != '\'' {
			continue
		}
		if !inStr {
			inStr = true
			continue
		}
		inStr = false
		if end, ok := castAfterLiteral(s, k+1); ok {
			k = end - 1
		}
	}
	return b.String()
}

// castTypeTails — продолжения многословных имён типов Postgres. Список ЗАКРЫТ:
// слово за именем типа съедается, только если оно принадлежит имени, а не запросу.
var castTypeTails = map[string][][]string{
	"timestamp": {{"with", "time", "zone"}, {"without", "time", "zone"}},
	"time":      {{"with", "time", "zone"}, {"without", "time", "zone"}},
	"double":    {{"precision"}},
	"character": {{"varying"}},
	"bit":       {{"varying"}},
}

// castAfterLiteral — конец приведения типа, начинающегося в позиции i (сразу за
// закрывающей кавычкой), и признак того, что приведение там вообще стоит.
func castAfterLiteral(s string, i int) (int, bool) {
	j := skipCastSpace(s, i)
	if j+1 >= len(s) || s[j] != ':' || s[j+1] != ':' {
		return i, false
	}
	j = skipCastSpace(s, j+2)
	word, next := castWordAt(s, j)
	if word == "" {
		return i, false
	}
	j = next
	// Схемное уточнение имени типа: `pg_catalog.text`.
	if j < len(s) && s[j] == '.' {
		w2, n2 := castWordAt(s, j+1)
		if w2 == "" {
			return i, false
		}
		word, j = w2, n2
	}
	for _, tail := range castTypeTails[word] {
		if n, ok := matchCastWords(s, j, tail); ok {
			j = n
			break
		}
	}
	// Модификатор точности: `numeric(10,2)`, `character varying(64)`.
	if j < len(s) && s[j] == '(' {
		if k := strings.IndexByte(s[j:], ')'); k >= 0 {
			j += k + 1
		}
	}
	// Массив: `text[]`, `text[][]`.
	for j+1 < len(s) && s[j] == '[' && s[j+1] == ']' {
		j += 2
	}
	return j, true
}

// skipCastSpace — позиция за пробелами (текст уже нормализован, поэтому только ' ').
func skipCastSpace(s string, i int) int {
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}

// castWordAt — слово имени типа, стоящее в позиции i, и позиция за ним.
func castWordAt(s string, i int) (string, int) {
	j := i
	for j < len(s) {
		c := s[j]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			j++
			continue
		}
		break
	}
	return s[i:j], j
}

// matchCastWords — стоят ли в позиции i подряд названные слова (через пробел).
func matchCastWords(s string, i int, words []string) (int, bool) {
	j := i
	for _, w := range words {
		j = skipCastSpace(s, j)
		got, n := castWordAt(s, j)
		if got != w {
			return i, false
		}
		j = n
	}
	return j, true
}

// normalizeSQLTable отбрасывает схему, кавычки и глаголы форматирования:
// `%s.registries` → `registries`, `disk_type_bindings%s` → `disk_type_bindings`,
// голый `%s` → пусто (таблица вычисляется в Go — нужно объявление).
func normalizeSQLTable(tok string) string {
	tok = strings.ReplaceAll(tok, `"`, "")
	if i := strings.LastIndex(tok, "."); i >= 0 {
		tok = tok[i+1:]
	}
	for _, verb := range []string{"%s", "%d", "%v", "%q"} {
		tok = strings.ReplaceAll(tok, verb, "")
	}
	tok = strings.Trim(tok, "%")
	if tok == "" {
		return ""
	}
	for _, r := range tok {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return ""
		}
	}
	return strings.ToLower(tok)
}
