// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tablegrowth.go — разбор ТАБЛИЦ И ПУТЕЙ СНЯТИЯ СТРОК для гейта «у живой
// таблицы назван механизм ограничения роста» (задача #1356; вторая половина
// предиката задачи #1292, приёмка
// `services/iam/docs/engineering/acceptance/retention-sweep-has-a-caller.md`).
//
// # Предмет — то, чего соседний гейт НЕ ВИДИТ BY CONSTRUCTION
//
// Гейт `TestDeclaredRetentionSweepersHaveAProductionCaller` держит первую
// половину: у ОБЪЯВЛЕННОГО уборщика есть прод-вызывающий. Он ищет функцию, чьё
// тело удаляет строки со сравнением колонки-времени, — и у таблицы, у которой
// уборщика НЕТ ВОВСЕ, такой функции не существует. Она ему не находка и не
// зелёное: она невидима.
//
// Ровно этим была третья таблица #1292 (отсечки по субъекту): у неё не было
// даже необслуживаемого уборщика, и нашлась она разбором соседней задачи, а не
// проверкой. Класс закрыли по экземплярам; по классу его держала ПЕРЕПИСЬ В
// ПРИЁМКЕ, привязанная к одной ревизии, — то есть однократный замер.
//
// Здесь заводится обход дерева: перечень таблиц ВЫВОДИТСЯ из применённых
// миграций, у каждой требуется один из трёх исходов, и «ноль находок» печатается
// рядом с «сколько прочитано».
//
// # ЧЕМ ГЕЙТ РАЗЛИЧАЕТ «ВНЕШНИЙ ТЕМП» — решение и его причина
//
// **Он его НЕ ВЫВОДИТ, и это не упрощение, а следствие того, где живёт ответ.**
// «Кто задаёт темп роста» есть свойство ВЫЗЫВАЮЩЕГО — арендатора, потока
// запросов, чужой системы, — а вызывающего в дереве нет. Всякий машинный
// признак, который просится, был проверен и оказался ложным:
//
//   - «пишет ли таблицу код сервиса» — писателя `minted_token_revocations`
//     (той самой третьей таблицы #1292) в Go НЕТ: строку пишет ТРИГГЕР
//     применённой миграции, а срабатывает он на снятии ключа арендатором.
//     Признак объявил бы «наш темп» ровно там, где темп внешний;
//   - «есть ли INSERT с именем таблицы» — очередь дренажа и таблица операций
//     пишутся ОБЩИМ кодом (`pkg/outbox`, `pkg/operations`), где имя таблицы
//     подставляется (`INSERT INTO %s`). Признак объявил бы «никто не пишет» о
//     таблицах, в которые пишет каждая мутация платформы;
//   - «стоит ли таблица на пути публичного RPC» — потребовало бы сквозного
//     разбора потока значений через порты и интерфейсы, то есть заведомо
//     неполного: неполнота такого разбора даёт МОЛЧАНИЕ, а не находку.
//
// Поэтому гейт выводит то, что в дереве действительно записано, — **НАКОПЛЕНИЕ**:
// у таблицы нет НИ ОДНОГО пути снятия строк. Это чисто структурное свойство, и
// оно шире предмета задачи: накапливать может и таблица нашего темпа. Разница
// между «внешний» и «наш» ОБЪЯВЛЯЕТСЯ — полем `Tempo` записи реестра
// (`tableGrowthRegistry`), и гейт проверяет, что объявление есть и взято из
// закрытого словаря, а не что оно верно. Суждение принадлежит человеку; гейт
// держит то, что суждение ВЫНЕСЕНО и записано рядом с предметом.
//
// Цена решения названа честно: перечень требует записи и от таблицы, чей рост
// заведомо ограничен нашими руками (посевной каталог, курсор), — одна строка с
// причиной. Обратный размен был бы хуже: машинный признак темпа молчал бы, и
// молчание нельзя отличить от отсутствия предмета.
//
// # ПРЕДПОСЫЛКИ РАЗБОРА — гейт заявляет их сам
//
//  1. таблицы объявляются goose-миграциями, и применяется секция `-- +goose Up`;
//     секция `Down` — откат, её `DROP TABLE` таблицу не снимает;
//  2. путь снятия строк записывается оператором `DELETE FROM` либо `TRUNCATE`;
//  3. предел, выраженный схемой, — внешний ключ `ON DELETE CASCADE` НА САМОЙ
//     таблице: строка умирает вместе с родителем;
//  4. ВРЕМЕННАЯ таблица (`CREATE TEMP|TEMPORARY TABLE`) живой не является:
//     она не переживает сессию, применившую миграцию, а `ON COMMIT DROP` —
//     то же снятие, записанное модификатором создания. Число таких объявлений
//     печатается отдельной величиной переписи: исключение обязано быть видно.
//
// Каждая предпосылка есть факт о дереве, и факт может измениться. Поэтому гейт
// печатает объём КАЖДОЙ полосы и падает, когда полоса пуста: ноль секций goose,
// ноль живых таблиц, ноль операторов снятия в прод-полосе, ноль каскадов —
// означают слепоту разбора, а не благополучие дерева.
//
// # ЧЕГО РАЗБОР НЕ ВИДИТ — названо, а не спрятано
//
//  1. **имя таблицы, подставляемое в рантайме** (`DELETE FROM %s.%s`,
//     `DELETE FROM %s`) — текста имени в дереве нет by construction. Такие
//     операторы СЧИТАЮТСЯ ОТДЕЛЬНО (`RemovalsUnresolved`) и печатаются: полоса,
//     у которой это число растёт, и есть эта слепая зона. На день заведения
//     таких операторов два, и оба принадлежат уборке #1264, чьи цели объявлены
//     полем структуры, а не литералом.
//
//  2. **однофамильцы в РАЗНЫХ службах**: оператор снятия ищется ПО ВСЕМУ
//     прод-дереву, а не в службе-владельце таблицы, потому что общие библиотеки
//     (`pkg/quota`, `pkg/operations`, `pkg/outbox`) работают с таблицами каждой
//     службы. Сужение до службы объявило бы находкой живую уборку. Остаток
//     сужен обратно там, где это возможно без домысла: если И оператор, И
//     объявление таблицы названы СХЕМОЙ, схемы обязаны совпасть.
//
//  3. **предел, выраженный не схемой, а замыслом** (закрытый словарь значений
//     ключа, одна строка на вид, посев миграцией) — такой предел объявляется
//     записью реестра с причиной, а не выводится.
//
//  4. **снятие каскада безымянным ограничением**: `ALTER TABLE … DROP
//     CONSTRAINT` сверяется ПО ИМЕНИ, поэтому каскад, объявленный в `CREATE
//     TABLE` без `CONSTRAINT <имя>`, снятию по автоимени не поддаётся и остался
//     бы засчитанным. Именованные каскады отслеживаются точно; безымянные
//     считаются отдельно (`CascadesUnnamed`) — и это число печатается.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// TableRef — единица счёта переписи: ВЛАДЕЛЕЦ плюс имя.
//
// Владелец обязателен, потому что одно имя носят таблицы разных служб: таблица
// операций `operations` объявлена восемью миграциями восьми владельцев, и счёт
// по голому имени объявил бы их одной.
type TableRef struct {
	// Owner — «services/iam» | «gateway» | «pkg».
	Owner string
	// Name — имя таблицы без схемы, в нижнем регистре.
	Name string
}

// String — «владелец/имя», для текста отказа.
func (r TableRef) String() string { return r.Owner + "/" + r.Name }

// TableDecl — объявление живой таблицы вместе с координатой.
type TableDecl struct {
	TableRef
	// Schema — схема, если имя объявлено с ней («» иначе: search_path).
	Schema string
	// File, Line — координата оператора создания.
	File string
	Line int
}

// RemovalLane — полоса, в которой найден оператор снятия строк.
//
// Словарь ЗАКРЫТ намеренно: корзины «прочее» нет, потому что различие между
// полосами и есть содержание разбора.
type RemovalLane string

const (
	// RemovalLaneProduction — оператор в прод-коде Go: уборщик либо глагол.
	RemovalLaneProduction RemovalLane = "прод"
	// RemovalLaneTrigger — оператор в теле функции применённой миграции
	// (триггер): механизм действующий, срабатывает на каждом событии.
	RemovalLaneTrigger RemovalLane = "триггер"
	// RemovalLaneOneShot — оператор на ВЕРХНЕМ уровне миграции: разовая правка
	// данных, исполняется единожды при накатке.
	//
	// Механизмом ограничения НЕ считается, и это несущее различие: очередь
	// `fga_outbox` iam «убиралась» одиннадцатью такими операторами в девяти
	// миграциях — все они разовые чистки при обратных заполнениях, а дренаж
	// помечает строку `sent_at` и не удаляет её никогда. Засчитав разовую
	// правку, гейт объявил бы механизм там, где его нет.
	RemovalLaneOneShot RemovalLane = "разовая"
)

// SQLRemoval — оператор, снимающий строки таблицы.
type SQLRemoval struct {
	// Schema — схема, названная оператором («» — не названа либо подставляется).
	Schema string
	// Table — имя таблицы без схемы.
	Table string
	// File — координата.
	File string
	// Lane — полоса.
	Lane RemovalLane
}

// TableGrowthCensus — объём осмотренного. Складывается по файлам.
//
// Полос много, и каждая печатается своим числом: одно суммарное число не
// различает «полоса пуста» и «полоса не читалась» — ровно то, ради чего перепись
// вообще ведётся.
type TableGrowthCensus struct {
	// MigrationFiles — файлов миграций прочитано.
	MigrationFiles int
	// SectionedMigrations — из них несут секции goose.
	SectionedMigrations int
	// GoFiles — файлов Go прод-кода прочитано.
	GoFiles int
	// GoStrings — строковых значений Go осмотрено (литералы плюс собранные
	// пакетные величины).
	GoStrings int
	// Creates, Drops — операторов создания и снятия таблиц в секции Up.
	Creates int
	Drops   int
	// TempTables — из числа Creates: объявления ВРЕМЕННОЙ таблицы. Живыми они
	// не считаются (см. предпосылку 4 в шапке), и потому печатаются ОТДЕЛЬНЫМ
	// числом: без него исключение было бы молчаливым, а «живых таблиц N»
	// перестало бы отличаться от «объявлений N».
	TempTables int
	// CascadesNamed, CascadesUnnamed — каскадных внешних ключей объявлено:
	// именованных (отслеживаются точно) и безымянных (снятию по имени не
	// поддаются, см. остаток 4 в шапке).
	CascadesNamed   int
	CascadesUnnamed int
	// ConstraintDrops — операторов снятия ограничения прочитано.
	ConstraintDrops int
	// RemovalsProduction, RemovalsTrigger, RemovalsOneShot — операторов снятия
	// строк по полосам.
	RemovalsProduction int
	RemovalsTrigger    int
	RemovalsOneShot    int
	// RemovalsUnresolved — операторов снятия строк, чьё имя таблицы
	// подставляется в рантайме. Слепая зона, названная числом.
	RemovalsUnresolved int
}

// Add складывает перепись другого файла.
func (c *TableGrowthCensus) Add(o TableGrowthCensus) {
	c.MigrationFiles += o.MigrationFiles
	c.SectionedMigrations += o.SectionedMigrations
	c.GoFiles += o.GoFiles
	c.GoStrings += o.GoStrings
	c.Creates += o.Creates
	c.Drops += o.Drops
	c.TempTables += o.TempTables
	c.CascadesNamed += o.CascadesNamed
	c.CascadesUnnamed += o.CascadesUnnamed
	c.ConstraintDrops += o.ConstraintDrops
	c.RemovalsProduction += o.RemovalsProduction
	c.RemovalsTrigger += o.RemovalsTrigger
	c.RemovalsOneShot += o.RemovalsOneShot
	c.RemovalsUnresolved += o.RemovalsUnresolved
}

// MigrationOwnerOf — владелец, которому принадлежит каталог.
//
// Та же граница, что у соседнего гейта уборщиков: служба (`services/<имя>`)
// либо корень верхнего уровня (`gateway`, `pkg`, `internal`). Объявление стоит
// здесь, в НЕ-тестовом файле, а `serviceUnitOf` соседнего гейта сведён к вызову
// этой функции: обратный порядок невозможен — объявление из тестового файла
// не-тестовому не видно, — а две реализации одной границы разошлись бы молча.
func MigrationOwnerOf(dir string) string {
	parts := strings.Split(strings.TrimPrefix(dir, "./"), "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[0] + "/" + parts[1]
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return dir
}

// blankSpan затирает пробелами байты [from,to), сохраняя переводы строк.
//
// Затирание, а не вырезание: смещения байт остаются прежними, поэтому номер
// строки, вычисленный по смещению, показывает на настоящую строку исходного
// файла. Вырезание сдвинуло бы все координаты после первой же комментарии.
func blankSpan(b []byte, from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(b) {
		to = len(b)
	}
	for i := from; i < to; i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// gooseUpSection оставляет секцию `-- +goose Up`, затирая остальное.
//
// Возвращает также признак того, что секции в файле ЕСТЬ: файл без них
// применяется целиком, и молча считать его пустым нельзя.
func gooseUpSection(src []byte) ([]byte, bool) {
	out := make([]byte, len(src))
	copy(out, src)

	sectioned := false
	inUp := false
	pos := 0
	for pos <= len(out) {
		end := strings.IndexByte(string(out[pos:]), '\n')
		lineEnd := len(out)
		if end >= 0 {
			lineEnd = pos + end
		}
		line := strings.TrimSpace(string(out[pos:lineEnd]))
		switch {
		case strings.HasPrefix(line, "-- +goose Up"):
			sectioned, inUp = true, true
			blankSpan(out, pos, lineEnd)
		case strings.HasPrefix(line, "-- +goose Down"):
			sectioned, inUp = true, false
			blankSpan(out, pos, lineEnd)
		case !inUp && sectioned:
			blankSpan(out, pos, lineEnd)
		}
		if end < 0 {
			break
		}
		pos = lineEnd + 1
	}
	if !sectioned {
		return src, false
	}
	return out, true
}

// blankSQLComments затирает комментарии `--` и `/* */`, сохраняя смещения.
//
// Комментарий — не исполняемая часть, и разбор, читающий его наравне с кодом,
// находит `CREATE TABLE` в объяснении того, почему таблицы нет. Класс известен:
// проверка, ищущая слово в сыром тексте, находит его в комментарии, объясняющем
// эту же проверку.
func blankSQLComments(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)
	for i := 0; i < len(out); i++ {
		switch {
		case out[i] == '\'':
			// Строковый литерал SQL: комментарием внутри него ничего не начинается.
			j := i + 1
			for j < len(out) {
				if out[j] == '\'' {
					if j+1 < len(out) && out[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			i = j
		case out[i] == '-' && i+1 < len(out) && out[i+1] == '-':
			j := i
			for j < len(out) && out[j] != '\n' {
				j++
			}
			blankSpan(out, i, j)
			i = j
		case out[i] == '/' && i+1 < len(out) && out[i+1] == '*':
			j := i + 2
			for j+1 < len(out) && (out[j] != '*' || out[j+1] != '/') {
				j++
			}
			blankSpan(out, i, min(j+2, len(out)))
			i = j + 1
		}
	}
	return out
}

// dollarQuoted — границы тел, заключённых в `$tag$ … $tag$`.
//
// Тело функции — исполняемый код, срабатывающий на КАЖДОМ событии, поэтому
// оператор снятия строк в нём есть действующий механизм, а не разовая правка.
// Различать их обязательно: разовая чистка при обратном заполнении механизмом
// не является.
var dollarTagRe = regexp.MustCompile(`\$[a-zA-Z_]*\$`)

// splitDollarBodies возвращает текст тел функций и текст верхнего уровня;
// оба — с сохранёнными смещениями относительно исходника.
func splitDollarBodies(src []byte) (bodies, top []byte) {
	bodies = make([]byte, len(src))
	top = make([]byte, len(src))
	copy(top, src)
	for i := range bodies {
		if src[i] == '\n' {
			bodies[i] = '\n'
		} else {
			bodies[i] = ' '
		}
	}
	pos := 0
	for pos < len(src) {
		loc := dollarTagRe.FindIndex(src[pos:])
		if loc == nil {
			break
		}
		start := pos + loc[0]
		tag := string(src[start : pos+loc[1]])
		rest := pos + loc[1]
		idx := strings.Index(string(src[rest:]), tag)
		if idx < 0 {
			break
		}
		bodyEnd := rest + idx
		copy(bodies[rest:bodyEnd], src[rest:bodyEnd])
		blankSpan(top, start, bodyEnd+len(tag))
		pos = bodyEnd + len(tag)
	}
	return bodies, top
}

// identifierRe — имя объекта: необязательная схема плюс имя. Кавычки и
// подстановка (`%s`) допускаются в разборе намеренно — подстановка отмечается
// вызывающим и уходит в счётчик неразрешимых, а не в тишину.
const identifierPattern = `(?:([%a-zA-Z0-9_"$]+)\s*\.\s*)?([%a-zA-Z0-9_"$]+)`

var (
	createTableRe = regexp.MustCompile(`(?is)\bcreate\s+(?:global\s+|local\s+|temp\s+|temporary\s+|unlogged\s+)*table\s+(?:if\s+not\s+exists\s+)?` + identifierPattern)
	dropTableRe   = regexp.MustCompile(`(?is)\bdrop\s+table\s+(?:if\s+exists\s+)?([^;]+);`)
	alterTableRe  = regexp.MustCompile(`(?is)\balter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?` + identifierPattern + `([^;]*);`)
	removalRe     = regexp.MustCompile(`(?is)\b(?:delete\s+from|truncate(?:\s+table)?)\s+(?:only\s+)?` + identifierPattern)
	addConstrRe   = regexp.MustCompile(`(?is)\badd\s+constraint\s+([a-zA-Z0-9_"]+)`)
	dropConstrRe  = regexp.MustCompile(`(?is)\bdrop\s+constraint\s+(?:if\s+exists\s+)?([a-zA-Z0-9_"]+)`)
	constrNameRe  = regexp.MustCompile(`(?is)\bconstraint\s+([a-zA-Z0-9_"]+)`)
	cascadeRe     = regexp.MustCompile(`(?is)\bon\s+delete\s+cascade\b`)
	// tempTableRe — объявление ВРЕМЕННОЙ таблицы. Судится МОДИФИКАТОР между
	// `create` и `table`, а не подстрока `temp` где угодно: имя `temp_ledger` и
	// таблица, названная `temp`, — обычные живые таблицы, и признак по слову
	// снял бы с наблюдения их обе. Обе стороны доказаны инъекцией
	// (`KnowsEveryFormOfDeclaration`, случаи «близнец: `temp` в ИМЕНИ…»).
	//
	// Шумовые слова стандарта `GLOBAL`/`LOCAL` перед `TEMP` Postgres принимает и
	// игнорирует. `UNLOGGED` временной таблицу НЕ делает: она переживает и
	// транзакцию, и сессию, и теряется лишь при аварийном перезапуске, — то есть
	// живая, и случай `UNLOGGED` стоит в инъекции с ожиданием «живых 1».
	tempTableRe = regexp.MustCompile(`(?is)\bcreate\s+(?:global\s+|local\s+)*(?:temp|temporary)\s+table\b`)
)

// unquote снимает кавычки и приводит имя к нижнему регистру.
//
// Postgres складывает незакавыченное имя в нижний регистр сам; закавыченное
// сохраняет регистр. В этом дереве закавыченных имён таблиц нет, и приведение
// обеих форм к одной — упрощение, названное здесь, а не сделанное молча.
func unquote(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), `"`))
}

// isSubstituted отвечает, подставляется ли имя в рантайме.
func isSubstituted(s string) bool { return strings.Contains(s, "%") || strings.Contains(s, "$") }

// lineAt — номер строки по смещению байта.
func lineAt(src []byte, offset int) int {
	if offset > len(src) {
		offset = len(src)
	}
	return 1 + strings.Count(string(src[:offset]), "\n")
}

// ConstraintKey — ключ каскадного ограничения: владелец, таблица, имя.
type ConstraintKey struct {
	Owner string
	Table string
	Name  string
}

// MigrationScan — что прочитано в одной миграции.
type MigrationScan struct {
	// Created — таблицы, созданные секцией Up.
	Created []TableDecl
	// Dropped — таблицы, снятые секцией Up.
	Dropped []TableRef
	// CascadeAdded, CascadeDropped — каскадные ограничения, объявленные и снятые.
	CascadeAdded   []ConstraintKey
	CascadeDropped []ConstraintKey
	// Removals — операторы снятия строк (полосы «триггер» и «разовая»).
	Removals []SQLRemoval
	// Census — объём осмотренного.
	Census TableGrowthCensus
}

// ScanMigrationSQL разбирает одну миграцию: что она создаёт, что снимает, какие
// каскады объявляет и какие строки удаляет.
//
// Читается ТОЛЬКО секция `-- +goose Up`: `DROP TABLE` секции `Down` есть откат,
// и засчитав его, разбор объявил бы снятой каждую таблицу дерева — на этом
// дереве 133 создания против 110 «снятий», из которых настоящих 30.
func ScanMigrationSQL(owner, path string, src []byte) MigrationScan {
	out := MigrationScan{Census: TableGrowthCensus{MigrationFiles: 1}}

	up, sectioned := gooseUpSection(src)
	if sectioned {
		out.Census.SectionedMigrations = 1
	}
	clean := blankSQLComments(up)
	bodies, top := splitDollarBodies(clean)

	for _, m := range createTableRe.FindAllSubmatchIndex(top, -1) {
		schema := ""
		if m[2] >= 0 {
			schema = unquote(string(top[m[2]:m[3]]))
		}
		name := unquote(string(top[m[4]:m[5]]))
		if name == "" || isSubstituted(name) {
			continue
		}
		out.Census.Creates++
		// ВРЕМЕННАЯ таблица живой не является: `ON COMMIT DROP` — то же снятие,
		// записанное МОДИФИКАТОРОМ создания, а не отдельным оператором, а без
		// него таблица не переживает даже сессии, применившей миграцию. Разбор
		// знал вторую форму снятия (`DROP TABLE`) и не знал первую — и три
		// временные таблицы одной миграции читались как живые таблицы службы,
		// у которых «не назван механизм ограничения роста» (kacho#1815).
		//
		// Исключение НЕ молчаливое: оно печатается своим числом переписи. И оно
		// не способно спрятать долговременную таблицу — объявить её словом TEMP
		// нельзя by construction: Postgres делает такую таблицу видимой только
		// своей сессии и снимает её в конце.
		if tempTableRe.Match(top[m[0]:m[1]]) {
			out.Census.TempTables++
			continue
		}
		out.Created = append(out.Created, TableDecl{
			TableRef: TableRef{Owner: owner, Name: name},
			Schema:   schema,
			File:     path,
			Line:     lineAt(src, m[0]),
		})
		// Каскады, объявленные в теле CREATE TABLE.
		body := balancedBody(top, m[1])
		named, unnamed := cascadeConstraints(body)
		for _, n := range named {
			out.CascadeAdded = append(out.CascadeAdded, ConstraintKey{Owner: owner, Table: name, Name: n})
			out.Census.CascadesNamed++
		}
		for i := 0; i < unnamed; i++ {
			out.CascadeAdded = append(out.CascadeAdded,
				ConstraintKey{Owner: owner, Table: name, Name: "\x00безымянный\x00" + path + "\x00" + strconv.Itoa(i)})
			out.Census.CascadesUnnamed++
		}
	}

	for _, m := range dropTableRe.FindAllSubmatchIndex(top, -1) {
		for _, raw := range strings.Split(string(top[m[2]:m[3]]), ",") {
			word := strings.Fields(raw)
			if len(word) == 0 {
				continue
			}
			name := unquote(word[0])
			if idx := strings.LastIndex(name, "."); idx >= 0 {
				name = name[idx+1:]
			}
			if name == "" || isSubstituted(name) {
				continue
			}
			out.Census.Drops++
			out.Dropped = append(out.Dropped, TableRef{Owner: owner, Name: name})
		}
	}

	for _, m := range alterTableRe.FindAllSubmatchIndex(top, -1) {
		name := unquote(string(top[m[4]:m[5]]))
		tail := string(top[m[6]:m[7]])
		if name == "" || isSubstituted(name) {
			continue
		}
		if cascadeRe.MatchString(tail) {
			added := addConstrRe.FindStringSubmatch(tail)
			cname := ""
			if added != nil {
				cname = unquote(added[1])
			}
			if cname == "" {
				out.Census.CascadesUnnamed++
				cname = "\x00безымянный\x00" + path + "\x00" + strconv.Itoa(m[0])
			} else {
				out.Census.CascadesNamed++
			}
			out.CascadeAdded = append(out.CascadeAdded, ConstraintKey{Owner: owner, Table: name, Name: cname})
		}
		for _, d := range dropConstrRe.FindAllStringSubmatch(tail, -1) {
			out.Census.ConstraintDrops++
			out.CascadeDropped = append(out.CascadeDropped,
				ConstraintKey{Owner: owner, Table: name, Name: unquote(d[1])})
		}
	}

	out.Removals = append(out.Removals, removalsIn(bodies, path, RemovalLaneTrigger, &out.Census)...)
	out.Removals = append(out.Removals, removalsIn(top, path, RemovalLaneOneShot, &out.Census)...)
	return out
}

// removalsIn собирает операторы снятия строк из одного текста.
func removalsIn(text []byte, path string, lane RemovalLane, census *TableGrowthCensus) []SQLRemoval {
	var out []SQLRemoval
	for _, m := range removalRe.FindAllSubmatchIndex(text, -1) {
		schema := ""
		if m[2] >= 0 {
			schema = unquote(string(text[m[2]:m[3]]))
		}
		name := unquote(string(text[m[4]:m[5]]))
		if name == "" || isSubstituted(name) {
			census.RemovalsUnresolved++
			continue
		}
		if isSubstituted(schema) {
			schema = ""
		}
		switch lane {
		case RemovalLaneProduction:
			census.RemovalsProduction++
		case RemovalLaneTrigger:
			census.RemovalsTrigger++
		case RemovalLaneOneShot:
			census.RemovalsOneShot++
		}
		out = append(out, SQLRemoval{Schema: schema, Table: name, File: path, Lane: lane})
	}
	return out
}

// balancedBody — тело в круглых скобках, начинающееся не позже чем через
// несколько символов после offset.
func balancedBody(src []byte, offset int) string {
	i := offset
	for i < len(src) && src[i] != '(' {
		if src[i] != ' ' && src[i] != '\n' && src[i] != '\t' && src[i] != '\r' {
			return ""
		}
		i++
	}
	if i >= len(src) {
		return ""
	}
	depth := 0
	start := i
	for ; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return string(src[start+1 : i])
			}
		}
	}
	return string(src[start:])
}

// cascadeConstraints — имена каскадных ограничений тела CREATE TABLE и число
// безымянных.
//
// Тело режется по запятым ВЕРХНЕГО уровня: `REFERENCES t(a, b)` внутри элемента
// не должен разрывать его надвое.
func cascadeConstraints(body string) (named []string, unnamed int) {
	depth := 0
	start := 0
	elements := []string{}
	for i, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				elements = append(elements, body[start:i])
				start = i + 1
			}
		}
	}
	elements = append(elements, body[start:])
	for _, e := range elements {
		if !cascadeRe.MatchString(e) {
			continue
		}
		if m := constrNameRe.FindStringSubmatch(e); m != nil {
			named = append(named, unquote(m[1]))
			continue
		}
		unnamed++
	}
	return named, unnamed
}

// ScanGoSQLRemovals разбирает один файл Go и возвращает операторы снятия строк,
// найденные в его строковых значениях.
//
// # Почему разбор, а не поиск по сырому тексту
//
// Слова `DELETE FROM` в этом дереве стоят и в прозе: в шапках функций, в
// объяснениях того, ПОЧЕМУ уборки нет, и в комментариях самих гейтов. Поиск по
// тексту засчитал бы объяснение за механизм — и молчал бы тем громче, чем лучше
// написан комментарий. Читаются только строковые значения: литералы тела и
// собранные пакетные величины.
//
// # Пакетные величины читаются наравне с литералами
//
// Уборщик называет свой оператор либо литералом в теле, либо ИМЕНЕМ пакетной
// строки того же файла (`dpopPurgeSQL` шлюза, `drainSQL` nlb) — обе формы в
// дереве живые. Разбор берёт обе через тот же `packageStrings`, которым читает
// их соседний гейт уборщиков: вторая реализация разошлась бы с первой молча.
func ScanGoSQLRemovals(path string, src []byte) ([]SQLRemoval, TableGrowthCensus, error) {
	census := TableGrowthCensus{GoFiles: 1}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, census, err
	}

	var values []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			v = lit.Value
		}
		values = append(values, v)
		return true
	})
	for _, v := range packageStrings(f) {
		values = append(values, v)
	}
	census.GoStrings = len(values)

	var out []SQLRemoval
	for _, v := range values {
		out = append(out, removalsIn([]byte(v), path, RemovalLaneProduction, &census)...)
	}
	return out, census, nil
}
