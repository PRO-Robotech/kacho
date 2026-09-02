// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tablegrowth_injection_test.go — доказательство, что гейт «у живой таблицы
// назван механизм ограничения роста» СПОСОБЕН УПАСТЬ и СПОСОБЕН СМОЛЧАТЬ
// (задача #1356).
//
// # Инъекция идёт ТЕМ ЖЕ разбором и ТЕМ ЖЕ суждением
//
// Синтетический корпус подаётся в `ScanMigrationSQL` / `ScanGoSQLRemovals` /
// `foldMigrationScans` / `tableGrowthVerdict` — то есть в код, который
// исполняется гейтом. Своей копии предиката здесь нет: копия разошлась бы с
// оригиналом молча и доказывала бы способность упасть у кода, который не
// работает. Дерева инъекция не трогает вовсе — ни файла, ни индекса.
//
// # Формы перечислены, а не подразумеваются
//
// Проверка, ищущая предмет по образцу, слепа к форме, о которой не знает, и
// слепота эта даёт НЕ красное и НЕ зелёное, а молчание. Поэтому каждая форма,
// в которой в этом дереве законно записывается объявление таблицы, оператор
// снятия строк и каскадный ключ, названа здесь поимённо и доказана отдельным
// случаем — вместе с законным близнецом, на котором гейт обязан молчать.
package repohygiene

import (
	"strings"
	"testing"
)

// injectedOwner — владелец синтетических таблиц.
const injectedOwner = "services/synthetic"

// scanInjectedMigration — разбор одной синтетической миграции тем же кодом,
// что и у гейта.
func scanInjectedMigration(t *testing.T, path, src string) MigrationScan {
	t.Helper()
	scan := ScanMigrationSQL(injectedOwner, path, []byte(src))
	if scan.Census.MigrationFiles != 1 {
		t.Fatalf("синтетика %s не прочитана — доказывать нечем", path)
	}
	return scan
}

// scanInjectedGo — разбор одного синтетического файла Go.
func scanInjectedGo(t *testing.T, path, src string) []SQLRemoval {
	t.Helper()
	out, census, err := ScanGoSQLRemovals(path, []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики %s: %v", path, err)
	}
	if census.GoStrings == 0 {
		t.Fatalf("в синтетике %s не осмотрено ни одного строкового значения — "+
			"доказывать нечем", path)
	}
	return out
}

// createLedger — объявление синтетической таблицы, у которой механизма нет.
const createLedger = `-- +goose Up
CREATE TABLE kacho_synth.ledger (
    id         text        PRIMARY KEY,
    seen_at    timestamptz NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE kacho_synth.ledger;
`

// growthGoFileWith — файл Go, чей единственный оператор задан вызывающим.
//
// Имя своё, а не общее с соседней инъекцией: тот `goFileWith` собирает файл под
// СВОЙ предмет, и связать два доказательства одной оболочкой значило бы, что
// правка ради одного молча меняет вход другого.
func growthGoFileWith(body string) string {
	return "package pg\n\nimport \"context\"\n\ntype R struct{ pool any }\n\n" +
		"func (r *R) Run(ctx context.Context) error {\n" + body + "\n\treturn nil\n}\n"
}

// verdictOn — суждение по синтетическому состоянию.
func verdictOn(scans []MigrationScan, goRemovals []SQLRemoval, registry []TableGrowthDecl) ([]string, []string, tableGrowthCounts) {
	state := foldMigrationScans(scans)
	state.Removals = append(state.Removals, goRemovals...)
	return tableGrowthVerdict(state, registry)
}

// TestTableGrowthGate_Injection_FindsTheTableAndNamesIt — таблица без
// механизма есть находка, и находка НАЗЫВАЕТ ЕЁ.
//
// Имя обязательно: находка без координаты неотличима от промаха разбора, и
// чинить по ней нечего.
func TestTableGrowthGate_Injection_FindsTheTableAndNamesIt(t *testing.T) {
	scan := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)
	if scan.Census.Creates != 1 {
		t.Fatalf("объявлений таблицы распознано %d, ожидалось 1", scan.Census.Creates)
	}

	findings, stale, counts := verdictOn([]MigrationScan{scan}, nil, nil)
	if len(findings) != 1 {
		t.Fatalf("таблица без механизма находкой НЕ стала: находок %d, осмотрено %d",
			len(findings), counts.Tables)
	}
	if !strings.Contains(findings[0], "ledger") {
		t.Errorf("находка не называет таблицу: %s", findings[0])
	}
	if !strings.Contains(findings[0], "0001_ledger.sql:2") {
		t.Errorf("находка не называет координату объявления: %s", findings[0])
	}
	if len(stale) != 0 {
		t.Errorf("реестр пуст, а просроченных записей насчитано %d", len(stale))
	}
}

// TestTableGrowthGate_Injection_KnowsEveryFormOfDeclaration — формы объявления
// и снятия ТАБЛИЦЫ.
//
// Форма, о которой разбор не знает, даёт молчание: таблица не попадает в
// перепись вовсе и требований не получает. Поэтому каждая форма — свой случай,
// и рядом стоят близнецы, на которых объявления нет.
func TestTableGrowthGate_Injection_KnowsEveryFormOfDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want int // сколько живых таблиц ожидается
	}{
		{
			name: "имя со схемой",
			src:  "-- +goose Up\nCREATE TABLE kacho_synth.ledger (id text);\n",
			want: 1,
		},
		{
			name: "имя без схемы (search_path)",
			src:  "-- +goose Up\nCREATE TABLE ledger (id text);\n",
			want: 1,
		},
		{
			name: "IF NOT EXISTS",
			src:  "-- +goose Up\nCREATE TABLE IF NOT EXISTS kacho_synth.ledger (id text);\n",
			want: 1,
		},
		{
			name: "UNLOGGED",
			src:  "-- +goose Up\nCREATE UNLOGGED TABLE kacho_synth.ledger (id text);\n",
			want: 1,
		},
		{
			// TEMP-таблица живой не является: она не переживает даже транзакцию
			// миграции, а `ON COMMIT DROP` — то же снятие, записанное
			// МОДИФИКАТОРОМ создания, а не отдельным оператором. Разбор знал
			// вторую форму снятия и не знал первую, и три такие таблицы одной
			// миграции читались как живые (kacho#1815, §БИ3).
			name: "TEMP … ON COMMIT DROP живой таблицей НЕ является",
			src: "-- +goose Up\nCREATE TEMP TABLE _sys_rule (id text) ON COMMIT DROP;\n" +
				"INSERT INTO _sys_rule SELECT id FROM kacho_synth.roles;\n",
			want: 0,
		},
		{
			name: "TEMPORARY — та же форма полным словом",
			src:  "-- +goose Up\nCREATE TEMPORARY TABLE _seg_scan (id text);\n",
			want: 0,
		},
		{
			name: "GLOBAL TEMPORARY — шумовые слова стандарта",
			src:  "-- +goose Up\nCREATE GLOBAL TEMPORARY TABLE _trace (id text);\n",
			want: 0,
		},
		{
			// Близнец, без которого предыдущие три были бы «гейт судит по СЛОВУ»:
			// `temp` в ИМЕНИ таблицы модификатором не является, и такая таблица
			// живая. Ровно этой подменой снимается вся полоса разом.
			name: "близнец: `temp` в ИМЕНИ таблицы — таблица живая",
			src:  "-- +goose Up\nCREATE TABLE kacho_synth.temp_ledger (id text);\n",
			want: 1,
		},
		{
			name: "близнец: таблица, НАЗВАННАЯ temp, — живая",
			src:  "-- +goose Up\nCREATE TABLE kacho_synth.temp (id text);\n",
			want: 1,
		},
		{
			name: "файл БЕЗ секций goose применяется целиком",
			src:  "CREATE TABLE kacho_synth.ledger (id text);\n",
			want: 1,
		},
		{
			name: "близнец: объявление в КОММЕНТАРИИ таблицей не является",
			src:  "-- +goose Up\n-- было: CREATE TABLE kacho_synth.ledger (id text);\nSELECT 1;\n",
			want: 0,
		},
		{
			name: "близнец: объявление в секции Down НЕ применяется",
			src:  "-- +goose Up\nSELECT 1;\n-- +goose Down\nCREATE TABLE kacho_synth.ledger (id text);\n",
			want: 0,
		},
		{
			name: "снятие в секции Up снимает таблицу",
			src:  "-- +goose Up\nCREATE TABLE kacho_synth.ledger (id text);\nDROP TABLE kacho_synth.ledger;\n",
			want: 0,
		},
		{
			name: "близнец: снятие в секции Down есть ОТКАТ и таблицу не снимает",
			src:  "-- +goose Up\nCREATE TABLE kacho_synth.ledger (id text);\n-- +goose Down\nDROP TABLE kacho_synth.ledger;\n",
			want: 1,
		},
		{
			name: "снятие перечнем через запятую снимает обе",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (id text);\n" +
				"CREATE TABLE kacho_synth.other (id text);\nDROP TABLE IF EXISTS kacho_synth.ledger, kacho_synth.other;\n",
			want: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scan := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_x.sql", tc.src)
			state := foldMigrationScans([]MigrationScan{scan})
			if len(state.Tables) != tc.want {
				t.Fatalf("живых таблиц %d, ожидалось %d: %+v", len(state.Tables), tc.want, state.Tables)
			}
		})
	}
}

// TestTableGrowthGate_Injection_KnowsEveryFormOfRemoval — формы записи
// ОПЕРАТОРА СНЯТИЯ СТРОК.
//
// Каждая форма списана с дерева, а не выдумана: подстановка схемы `%s.` — так
// записана уборка registry; пакетная строковая величина — так записаны
// `dpopPurgeSQL` шлюза и `drainSQL` nlb; тело триггера — так снимает строки
// применённая миграция.
//
// Близнецы — вторая половина утверждения. Без них разбор ловил бы «файл со
// словом DELETE», а не механизм: слово стоит и в прозе, и в разовой правке
// данных, и в операторе, чьё имя таблицы подставляется в рантайме.
func TestTableGrowthGate_Injection_KnowsEveryFormOfRemoval(t *testing.T) {
	for _, tc := range []struct {
		name string
		// goSrc — синтетический прод-файл Go («» — нет).
		goSrc string
		// migrationSrc — синтетическая миграция сверх объявления («» — нет).
		migrationSrc string
		// wantFindings — 0, если механизм найден; 1, если таблица осталась без него.
		wantFindings int
	}{
		{
			name:  "литерал Go, имя без схемы",
			goSrc: growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM ledger WHERE seen_at <= now()`)"),
		},
		{
			name:  "литерал Go, имя со схемой",
			goSrc: growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_synth.ledger WHERE id = $1`)"),
		},
		{
			name:  "литерал Go, имя в кавычках",
			goSrc: growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM \"kacho_synth\".\"ledger\" WHERE id = $1`)"),
		},
		{
			name:  "литерал Go, схема ПОДСТАВЛЯЕТСЯ (%s.таблица)",
			goSrc: growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM %s.ledger WHERE seen_at <= $1`)"),
		},
		{
			name:  "TRUNCATE TABLE",
			goSrc: growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `TRUNCATE TABLE kacho_synth.ledger`)"),
		},
		{
			name:  "TRUNCATE без слова TABLE",
			goSrc: growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `TRUNCATE kacho_synth.ledger`)"),
		},
		{
			// Имя таблицы стоит в ТОМ ЖЕ литерале, что и глагол: этой формой
			// записаны `dpopPurgeSQL` шлюза и `drainSQL` nlb.
			name: "ПАКЕТНАЯ строковая величина, собранная склейкой",
			goSrc: "package pg\n\nimport \"context\"\n\n" +
				"const expiredPredicate = \"seen_at <= now()\"\n\n" +
				"const purgeSQL = \"DELETE FROM kacho_synth.ledger WHERE \" + expiredPredicate\n\n" +
				"type R struct{ pool any }\n\n" +
				"func (r *R) Run(ctx context.Context) error {\n\t_, _ = r.pool.Exec(ctx, purgeSQL)\n\treturn nil\n}\n",
		},
		{
			// А здесь имя таблицы вынесено В ОТДЕЛЬНУЮ величину, и ни один
			// литерал файла целого оператора не содержит. Без сборки пакетных
			// строк уборка была бы НЕВИДИМА — не находкой, а молчанием.
			//
			// Случай заведён после того, как проба мутацией показала: предыдущий
			// случай проходит и БЕЗ сборки пакетных строк, потому что имя стоит в
			// первом же литерале. То есть он доказывал не то, о чём был заголовок.
			name: "ПАКЕТНАЯ величина, в которой ИМЯ ТАБЛИЦЫ вынесено отдельно",
			goSrc: "package pg\n\nimport \"context\"\n\n" +
				"const ledgerTable = \"kacho_synth.ledger\"\n\n" +
				"const purgeSQL = \"DELETE FROM \" + ledgerTable + \" WHERE seen_at <= now()\"\n\n" +
				"type R struct{ pool any }\n\n" +
				"func (r *R) Run(ctx context.Context) error {\n\t_, _ = r.pool.Exec(ctx, purgeSQL)\n\treturn nil\n}\n",
		},
		{
			name: "оператор в ТЕЛЕ ФУНКЦИИ применённой миграции (триггер)",
			migrationSrc: "-- +goose Up\nCREATE FUNCTION kacho_synth.prune() RETURNS trigger AS $$\n" +
				"BEGIN\n  DELETE FROM kacho_synth.ledger WHERE seen_at <= now() - interval '1 day';\n  RETURN NEW;\nEND;\n$$ LANGUAGE plpgsql;\n",
		},
		{
			name:         "близнец: РАЗОВАЯ правка на верхнем уровне миграции механизмом не является",
			migrationSrc: "-- +goose Up\nDELETE FROM kacho_synth.ledger WHERE seen_at <= now() - interval '1 day';\n",
			wantFindings: 1,
		},
		{
			name:         "близнец: оператор в КОММЕНТАРИИ Go механизмом не является",
			goSrc:        growthGoFileWith("\t// уборки нет: `DELETE FROM kacho_synth.ledger` никто не зовёт\n\t_ = ctx"),
			wantFindings: 1,
		},
		{
			name:         "близнец: оператор в КОММЕНТАРИИ SQL механизмом не является",
			migrationSrc: "-- +goose Up\n-- здесь стояло DELETE FROM kacho_synth.ledger WHERE seen_at <= now();\nSELECT 1;\n",
			wantFindings: 1,
		},
		{
			name:         "близнец: имя таблицы ПОДСТАВЛЯЕТСЯ целиком (%s.%s) — неразрешимо",
			goSrc:        growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM %s.%s WHERE id = ANY($1)`)"),
			wantFindings: 1,
		},
		{
			name:         "близнец: снимаются строки ДРУГОЙ таблицы",
			goSrc:        growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_synth.other WHERE id = $1`)"),
			wantFindings: 1,
		},
		{
			name:         "близнец: ЧУЖАЯ схема при названной своей",
			goSrc:        growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_other.ledger WHERE id = $1`)"),
			wantFindings: 1,
		},
		{
			name:         "близнец: чтение по времени БЕЗ удаления",
			goSrc:        growthGoFileWith("\t_, _ = r.pool.Query(ctx, `SELECT id FROM kacho_synth.ledger WHERE seen_at <= now()`)"),
			wantFindings: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scans := []MigrationScan{
				scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger),
			}
			if tc.migrationSrc != "" {
				scans = append(scans,
					scanInjectedMigration(t, "services/synthetic/internal/migrations/0002_x.sql", tc.migrationSrc))
			}
			var goRemovals []SQLRemoval
			if tc.goSrc != "" {
				goRemovals = scanInjectedGo(t, "services/synthetic/internal/repo/pg/x.go", tc.goSrc)
			}
			findings, stale, counts := verdictOn(scans, goRemovals, nil)
			if len(findings) != tc.wantFindings {
				t.Fatalf("находок %d, ожидалось %d (уборка %d): %v",
					len(findings), tc.wantFindings, counts.Swept, findings)
			}
			if len(stale) != 0 {
				t.Errorf("реестр пуст, а просроченных записей %d", len(stale))
			}
		})
	}
}

// TestTableGrowthGate_Injection_OwnerIsPartOfTheUnitOfCount — уборка ЧУЖОГО
// владельца своей таблице не помогает.
//
// Форма списана с дерева: имя `operations` носят таблицы восьми владельцев,
// `quota_sync_cursor` — пяти, `fga_register_outbox` — четырёх. Единица счёта,
// забывшая владельца, объявила бы одну убираемую таблицу за все восемь — то
// есть замолчала бы ровно там, где положена находка.
func TestTableGrowthGate_Injection_OwnerIsPartOfTheUnitOfCount(t *testing.T) {
	first := ScanMigrationSQL("services/first", "services/first/internal/migrations/0001.sql",
		[]byte("-- +goose Up\nCREATE TABLE kacho_first.ledger (id text);\n"))
	second := ScanMigrationSQL("services/second", "services/second/internal/migrations/0001.sql",
		[]byte("-- +goose Up\nCREATE TABLE kacho_second.ledger (id text);\n"))
	// Уборка есть ТОЛЬКО у первого владельца — и схему называет его.
	swept := scanInjectedGo(t, "services/first/internal/repo/pg/x.go",
		growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_first.ledger WHERE id = $1`)"))

	findings, _, counts := verdictOn([]MigrationScan{first, second}, swept, nil)
	if counts.Tables != 2 {
		t.Fatalf("живых таблиц %d, ожидалось 2 — одноимённые таблицы разных владельцев "+
			"схлопнулись в одну", counts.Tables)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (у второго владельца уборки нет): %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "services/second/ledger") {
		t.Errorf("находка называет не того владельца: %s", findings[0])
	}
}

// TestTableGrowthGate_Injection_KnowsEveryFormOfCascade — формы записи
// КАСКАДНОГО ключа, то есть предела, выраженного схемой.
//
// Близнецы здесь особенно нужны: `ON DELETE RESTRICT` и `ON DELETE SET NULL`
// выглядят почти так же и строк НЕ снимают. Разбор, ловящий слова `ON DELETE`,
// объявил бы предел там, где строка переживает родителя, — и это было бы
// молчание, а не находка.
func TestTableGrowthGate_Injection_KnowsEveryFormOfCascade(t *testing.T) {
	for _, tc := range []struct {
		name         string
		src          string
		wantFindings int
	}{
		{
			name: "каскад в колонке CREATE TABLE",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (\n" +
				"    id text PRIMARY KEY,\n    owner_id text NOT NULL REFERENCES kacho_synth.owners(id) ON DELETE CASCADE\n);\n",
		},
		{
			name: "каскад ИМЕНОВАННЫМ ограничением в CREATE TABLE",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (\n" +
				"    id text PRIMARY KEY,\n    owner_id text NOT NULL,\n" +
				"    CONSTRAINT ledger_owner_fk FOREIGN KEY (owner_id) REFERENCES kacho_synth.owners(id) ON DELETE CASCADE\n);\n",
		},
		{
			name: "каскад добавлен ALTER TABLE",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (id text PRIMARY KEY, owner_id text);\n" +
				"ALTER TABLE kacho_synth.ledger ADD CONSTRAINT ledger_owner_fk FOREIGN KEY (owner_id) REFERENCES kacho_synth.owners(id) ON DELETE CASCADE;\n",
		},
		{
			name: "близнец: ON DELETE RESTRICT пределом НЕ является",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (\n" +
				"    id text PRIMARY KEY,\n    owner_id text NOT NULL REFERENCES kacho_synth.owners(id) ON DELETE RESTRICT\n);\n",
			wantFindings: 1,
		},
		{
			name: "близнец: ON DELETE SET NULL строк не снимает",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (\n" +
				"    id text,\n    owner_id text REFERENCES kacho_synth.owners(id) ON DELETE SET NULL\n);\n",
			wantFindings: 1,
		},
		{
			name: "близнец: каскад НА ДРУГОЙ таблице своей не помогает",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (id text PRIMARY KEY);\n" +
				"CREATE TABLE kacho_synth.other (\n    id text PRIMARY KEY,\n" +
				"    owner_id text REFERENCES kacho_synth.owners(id) ON DELETE CASCADE\n);\n" +
				"ALTER TABLE kacho_synth.other ADD CONSTRAINT other_x FOREIGN KEY (owner_id) REFERENCES kacho_synth.owners(id) ON DELETE CASCADE;\n",
			wantFindings: 1,
		},
		{
			name: "каскад, СНЯТЫЙ позднейшим DROP CONSTRAINT, пределом быть перестаёт",
			src: "-- +goose Up\nCREATE TABLE kacho_synth.ledger (\n" +
				"    id text PRIMARY KEY,\n    owner_id text NOT NULL,\n" +
				"    CONSTRAINT ledger_owner_fk FOREIGN KEY (owner_id) REFERENCES kacho_synth.owners(id) ON DELETE CASCADE\n);\n" +
				"ALTER TABLE kacho_synth.ledger DROP CONSTRAINT ledger_owner_fk;\n",
			wantFindings: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scan := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_x.sql", tc.src)
			findings, _, counts := verdictOn([]MigrationScan{scan}, nil, nil)
			// Находка ждётся ровно по таблице `ledger`; соседняя `other` в
			// случае с чужим каскадом своей записи не получает и находкой не
			// становится — иначе счёт был бы не о том.
			var about []string
			for _, f := range findings {
				if strings.Contains(f, "/ledger ") {
					about = append(about, f)
				}
			}
			if len(about) != tc.wantFindings {
				t.Fatalf("находок по ledger %d, ожидалось %d (предел каскадом %d): %v",
					len(about), tc.wantFindings, counts.Cascaded, findings)
			}
		})
	}
}

// TestTableGrowthGate_Injection_SilentOnEachOfThreeOutcomes — обратная сторона:
// таблица с ЛЮБЫМ из трёх исходов молчит.
//
// Без этой половины гейт ловил бы форму, а не существо: он краснел бы на всякой
// таблице дерева, и первый же ложный срабат его отключил бы.
func TestTableGrowthGate_Injection_SilentOnEachOfThreeOutcomes(t *testing.T) {
	base := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)

	for _, tc := range []struct {
		name      string
		goSrc     string
		registry  []TableGrowthDecl
		wantSwept int
		wantCasc  int
		wantDecl  int
	}{
		{
			name:      "исход «уборка»",
			goSrc:     growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_synth.ledger WHERE seen_at <= now()`)"),
			wantSwept: 1,
		},
		{
			name: "исход «объявленный предел»",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: tempoOurs, Verdict: verdictBound,
				Reason: "одна строка на вид: ключ перечисляет виды, а не события",
			}},
			wantDecl: 1,
		},
		{
			name: "исход «объявленное удержание»",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: tempoExternal, Verdict: verdictRetained,
				Reason: "журнал: удержание намеренно, срок хранения — политика",
			}},
			wantDecl: 1,
		},
		{
			name: "исход «объявленный долг» с номером задачи",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: tempoExternal, Verdict: verdictDebt,
				Reason: "механизма нет", Issue: "#1360",
			}},
			wantDecl: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var goRemovals []SQLRemoval
			if tc.goSrc != "" {
				goRemovals = scanInjectedGo(t, "services/synthetic/internal/repo/pg/x.go", tc.goSrc)
			}
			findings, stale, counts := verdictOn([]MigrationScan{base}, goRemovals, tc.registry)
			if len(findings) != 0 {
				t.Fatalf("исход назван, а гейт нашёл находку: %v", findings)
			}
			if len(stale) != 0 {
				t.Fatalf("исход назван, а запись объявлена потерявшей предмет: %v", stale)
			}
			if counts.Swept != tc.wantSwept || counts.Cascaded != tc.wantCasc || counts.Declared != tc.wantDecl {
				t.Errorf("перепись исходов: уборка %d/%d, каскад %d/%d, объявлено %d/%d",
					counts.Swept, tc.wantSwept, counts.Cascaded, tc.wantCasc, counts.Declared, tc.wantDecl)
			}
		})
	}
}

// TestTableGrowthGate_Injection_RegistryExpiresByItself — запись, у которой
// предмет ЗАКРЫЛСЯ или ИСЧЕЗ, есть находка.
//
// Послабление, которое не истекает само, унаследует следующая слепая зона: под
// записью «механизма нет, задача заведена» окажется таблица, у которой механизм
// давно есть, и никто об этом не узнает.
func TestTableGrowthGate_Injection_RegistryExpiresByItself(t *testing.T) {
	base := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)
	debt := TableGrowthDecl{
		Owner: injectedOwner, Table: "ledger",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "механизма нет", Issue: "#1360",
	}

	t.Run("у таблицы появилась уборка — запись потеряла предмет", func(t *testing.T) {
		goRemovals := scanInjectedGo(t, "services/synthetic/internal/repo/pg/x.go",
			growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_synth.ledger WHERE seen_at <= now()`)"))
		_, stale, _ := verdictOn([]MigrationScan{base}, goRemovals, []TableGrowthDecl{debt})
		if len(stale) != 1 {
			t.Fatalf("запись с закрывшимся предметом просроченной НЕ объявлена: %v", stale)
		}
		if !strings.Contains(stale[0], "ledger") {
			t.Errorf("отказ не называет запись: %s", stale[0])
		}
	})

	t.Run("у таблицы появился каскад — запись потеряла предмет", func(t *testing.T) {
		withCascade := scanInjectedMigration(t, "services/synthetic/internal/migrations/0002_fk.sql",
			"-- +goose Up\nALTER TABLE kacho_synth.ledger ADD CONSTRAINT ledger_owner_fk "+
				"FOREIGN KEY (owner_id) REFERENCES kacho_synth.owners(id) ON DELETE CASCADE;\n")
		_, stale, _ := verdictOn([]MigrationScan{base, withCascade}, nil, []TableGrowthDecl{debt})
		if len(stale) != 1 {
			t.Fatalf("запись при появившемся каскаде просроченной НЕ объявлена: %v", stale)
		}
	})

	t.Run("таблицы в дереве больше нет — запись потеряла предмет", func(t *testing.T) {
		dropped := scanInjectedMigration(t, "services/synthetic/internal/migrations/0003_drop.sql",
			"-- +goose Up\nDROP TABLE kacho_synth.ledger;\n")
		_, stale, counts := verdictOn([]MigrationScan{base, dropped}, nil, []TableGrowthDecl{debt})
		if counts.Tables != 0 {
			t.Fatalf("таблица снята, а перепись насчитала %d живых", counts.Tables)
		}
		if len(stale) != 1 {
			t.Fatalf("запись о несуществующей таблице просроченной НЕ объявлена: %v", stale)
		}
	})

	t.Run("близнец: запись, у которой предмет ЖИВ, молчит", func(t *testing.T) {
		findings, stale, _ := verdictOn([]MigrationScan{base}, nil, []TableGrowthDecl{debt})
		if len(stale) != 0 || len(findings) != 0 {
			t.Fatalf("живая запись объявлена просроченной: находок %v, просрочено %v", findings, stale)
		}
	})
}

// TestTableGrowthGate_Injection_RegistryFormIsChecked — форма записи.
//
// Запись без причины и долг без номера — это «подразумевается» в оболочке
// решения: выглядит как вынесенное суждение и им не является.
func TestTableGrowthGate_Injection_RegistryFormIsChecked(t *testing.T) {
	base := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)

	for _, tc := range []struct {
		name     string
		registry []TableGrowthDecl
		want     string
	}{
		{
			name: "долг БЕЗ номера задачи",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: tempoExternal, Verdict: verdictDebt, Reason: "механизма нет",
			}},
			want: "НОМЕРА ЗАДАЧИ",
		},
		{
			name: "запись без ПРИЧИНЫ",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: tempoOurs, Verdict: verdictBound,
			}},
			want: "без ПРИЧИНЫ",
		},
		{
			name: "темп не из закрытого словаря",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: growthTempo("наверное наш"), Verdict: verdictBound, Reason: "потому что",
			}},
			want: "не называет темп",
		},
		{
			name: "вердикт не из закрытого словаря",
			registry: []TableGrowthDecl{{
				Owner: injectedOwner, Table: "ledger",
				Tempo: tempoOurs, Verdict: growthVerdict(""), Reason: "потому что",
			}},
			want: "не называет вердикт",
		},
		{
			name: "ДВЕ записи об одной таблице",
			registry: []TableGrowthDecl{
				{Owner: injectedOwner, Table: "ledger", Tempo: tempoOurs, Verdict: verdictBound, Reason: "а"},
				{Owner: injectedOwner, Table: "ledger", Tempo: tempoExternal, Verdict: verdictRetained, Reason: "б"},
			},
			want: "ДВЕ записи",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, _, _ := verdictOn([]MigrationScan{base}, nil, tc.registry)
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("негодная форма записи находкой не стала: ждали %q, получили %v", tc.want, findings)
			}
		})
	}
}

// TestTableGrowthGate_Injection_EmptyRegistryOnCleanTreeIsGreen — идеал не
// превращён в поломку.
//
// Пустой реестр при дереве, где у каждой таблицы есть уборка, — это ЦЕЛЬ, а не
// отсутствие проверки. Гейт, падающий на достижении своей цели, подталкивал бы
// держать запись ради зелёного.
func TestTableGrowthGate_Injection_EmptyRegistryOnCleanTreeIsGreen(t *testing.T) {
	base := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)
	goRemovals := scanInjectedGo(t, "services/synthetic/internal/repo/pg/x.go",
		growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_synth.ledger WHERE seen_at <= now()`)"))

	findings, stale, counts := verdictOn([]MigrationScan{base}, goRemovals, nil)
	if len(findings) != 0 || len(stale) != 0 {
		t.Fatalf("на чистом дереве с пустым реестром гейт краснеет: находок %v, просрочено %v", findings, stale)
	}
	if counts.Tables != 1 || counts.Swept != 1 {
		t.Fatalf("перепись: осмотрено %d, с уборкой %d — ожидалось 1 и 1", counts.Tables, counts.Swept)
	}
}

// TestTableGrowthGate_Injection_TouchesOnlyThisGate — ТРЕТИЙ ПРОГОН.
//
// Инъекция, попутно роняющая соседний контроль, доказательством не является:
// красное пришло бы от соседа, а новый гейт мог бы оказаться вакуумным, не
// показав этого ничем (`testing.md` §«Гейт на класс», п. 2в). Поэтому прогонов
// три, и каждый называет ОБА вердикта.
//
// Заодно это прямое доказательство предмета задачи #1356: состояние, которое
// ловит новый гейт, соседнему НЕВИДИМО by construction, и наоборот. Ни один из
// двух не покрывает другого.
func TestTableGrowthGate_Injection_TouchesOnlyThisGate(t *testing.T) {
	// Соседний гейт судит уборщиков, объявленных в Go. Его вердикт берётся тем
	// же кодом, каким он судит дерево, — своей копии предиката здесь нет.
	sweeperVerdict := func(t *testing.T, goSrc string, withCaller bool) int {
		t.Helper()
		if goSrc == "" {
			return 0
		}
		sw, census, err := ScanRetentionSweepers("services/synthetic/internal/repo/pg/x.go",
			"services/synthetic/internal/repo/pg", []byte(goSrc))
		if err != nil {
			t.Fatalf("разбор синтетики соседним гейтом: %v", err)
		}
		if census.Functions == 0 {
			t.Fatalf("соседний гейт не осмотрел ни одной функции — его молчание ничего не значит")
		}
		callers := map[string]map[string][]string{}
		if withCaller {
			callers["Reap"] = map[string][]string{injectedOwner: {"Loop.Pass"}}
		}
		findings, _, _ := retentionSweeperVerdict(sw, callers, map[string]string{})
		return len(findings)
	}

	const sweeperWithSQL = `package pg

import "context"

type R struct{ pool any }

func (r *R) Reap(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, ` + "`DELETE FROM kacho_synth.ledger WHERE seen_at <= now()`" + `)
	return err
}
`

	for _, tc := range []struct {
		name string
		// goSrc — прод-файл Go синтетической службы («» — файла нет вовсе).
		goSrc string
		// withCaller — есть ли у уборщика прод-вызывающий.
		withCaller bool
		// wantThis, wantNeighbour — сколько находок ждём у нового и у соседнего.
		wantThis      int
		wantNeighbour int
	}{
		{
			name:       "контроль: уборщик есть И провязан — молчат ОБА",
			goSrc:      sweeperWithSQL,
			withCaller: true,
		},
		{
			name:          "инъекция НОВОГО свойства: уборщика нет вовсе — краснеет только новый",
			goSrc:         "",
			wantThis:      1,
			wantNeighbour: 0,
		},
		{
			name:          "инъекция СТАРОГО свойства: уборщик объявлен, вызывающего нет — краснеет только соседний",
			goSrc:         sweeperWithSQL,
			withCaller:    false,
			wantThis:      0,
			wantNeighbour: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)
			var goRemovals []SQLRemoval
			if tc.goSrc != "" {
				goRemovals = scanInjectedGo(t, "services/synthetic/internal/repo/pg/x.go", tc.goSrc)
			}
			findings, stale, _ := verdictOn([]MigrationScan{base}, goRemovals, nil)
			neighbour := sweeperVerdict(t, tc.goSrc, tc.withCaller)

			if len(findings) != tc.wantThis {
				t.Errorf("новый гейт: находок %d, ожидалось %d: %v", len(findings), tc.wantThis, findings)
			}
			if len(stale) != 0 {
				t.Errorf("новый гейт: реестр пуст, а просроченных %d", len(stale))
			}
			if neighbour != tc.wantNeighbour {
				t.Errorf("соседний гейт: находок %d, ожидалось %d", neighbour, tc.wantNeighbour)
			}
		})
	}
}

// TestTableGrowthGate_Injection_BlockedRemovalIsAFindingNotAResolvedEntry —
// у таблицы, чья запись объявила `RemovalBlocked`, появление оператора снятия
// строк есть НАХОДКА, а не закрывшийся предмет (задача #1712).
//
// # Что здесь доказывается и почему одного случая мало
//
// Полоса `stale` говорит «механизм нашёлся, снимите запись» — и для почти всякой
// таблицы это верно. Для журнала, читатель которого не обнаруживает пропуска,
// оно неверно ровно наоборот: уборка не разрешает запись, а реализует ту самую
// беду, о которой запись предупреждает. Гейт, не различающий двух исходов,
// подсказывал бы снять единственное место, где написано, почему уборка опасна.
//
// Поэтому случаев ТРИ, и третий — законный близнец: та же таблица, тот же
// оператор снятия, запись БЕЗ объявленного блока обязана по-прежнему давать
// `stale`. Без него проверка доказывала бы лишь, что гейт умеет ругаться на
// всякую появившуюся уборку, — то есть ловила бы форму, а не существо.
func TestTableGrowthGate_Injection_BlockedRemovalIsAFindingNotAResolvedEntry(t *testing.T) {
	base := scanInjectedMigration(t, "services/synthetic/internal/migrations/0001_ledger.sql", createLedger)
	sweep := func(t *testing.T) []SQLRemoval {
		t.Helper()
		return scanInjectedGo(t, "services/synthetic/internal/repo/pg/x.go",
			growthGoFileWith("\t_, _ = r.pool.Exec(ctx, `DELETE FROM kacho_synth.ledger WHERE seen_at <= now()`)"))
	}

	blocked := TableGrowthDecl{
		Owner: injectedOwner, Table: "ledger",
		Tempo: tempoExternal, Verdict: verdictDebt,
		Reason: "журнал: читатель идёт по диапазону позиций", Issue: "#1712",
		RemovalBlocked: "читатель не обнаруживает пропуска: снятая строка неотличима от " +
			"«строк не было», и отзыв доступа не применится молча",
	}

	t.Run("уборка при объявленном блоке — НАХОДКА, называющая таблицу и причину", func(t *testing.T) {
		findings, stale, _ := verdictOn([]MigrationScan{base}, sweep(t), []TableGrowthDecl{blocked})
		if len(findings) != 1 {
			t.Fatalf("уборка у таблицы с объявленным блоком находкой НЕ объявлена: находок %v, просрочено %v",
				findings, stale)
		}
		if !strings.Contains(findings[0], "ledger") {
			t.Errorf("находка не называет таблицу: %s", findings[0])
		}
		if !strings.Contains(findings[0], "не обнаруживает пропуска") {
			t.Errorf("находка не называет ПРИЧИНУ блока — чинить по ней нечего: %s", findings[0])
		}
		if len(stale) != 0 {
			t.Errorf("запись с блоком объявлена ещё и просроченной — два исхода об одном предмете: %v", stale)
		}
	})

	t.Run("близнец: та же уборка БЕЗ блока — по-прежнему просроченная запись", func(t *testing.T) {
		open := blocked
		open.RemovalBlocked = ""
		findings, stale, _ := verdictOn([]MigrationScan{base}, sweep(t), []TableGrowthDecl{open})
		if len(stale) != 1 {
			t.Fatalf("запись без блока при появившейся уборке просроченной НЕ объявлена: %v", stale)
		}
		if len(findings) != 0 {
			t.Errorf("запись без блока дала находку — гейт ловит форму, а не существо: %v", findings)
		}
	})

	t.Run("близнец: блок объявлен, а уборки НЕТ — молчание", func(t *testing.T) {
		findings, stale, _ := verdictOn([]MigrationScan{base}, nil, []TableGrowthDecl{blocked})
		if len(findings) != 0 || len(stale) != 0 {
			t.Fatalf("блок без появившейся уборки заговорил: находок %v, просрочено %v", findings, stale)
		}
	})

	t.Run("блок без НОМЕРА ЗАДАЧИ — находка: он не истёк бы никогда", func(t *testing.T) {
		noIssue := blocked
		noIssue.Verdict = verdictRetained
		noIssue.Issue = ""
		findings, _, _ := verdictOn([]MigrationScan{base}, nil, []TableGrowthDecl{noIssue})
		if len(findings) != 1 {
			t.Fatalf("блок без номера задачи находкой НЕ объявлен: %v", findings)
		}
		if !strings.Contains(findings[0], "ledger") {
			t.Errorf("находка не называет запись: %s", findings[0])
		}
	})
}
