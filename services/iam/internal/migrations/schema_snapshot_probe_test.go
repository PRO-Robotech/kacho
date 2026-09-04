//go:build integration_snapshot

// Снимок схемы — ЭТАЛОН для сведения миграций.
//
// Слепок берётся ЗАПРОСОМ к каталогу, а не дампом: дамп зависит от порядка
// вывода и версии инструмента, структура — нет. Сравнивать надо РЕЗУЛЬТАТ
// применения, а не текст, которым его записали.
package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/migrations"
)

func itoa(i int) string { return strconv.Itoa(i) }

func TestSchemaSnapshot(t *testing.T) {
	out := os.Getenv("KACHO_SCHEMA_SNAPSHOT_OUT")
	if out == "" {
		t.Skip("KACHO_SCHEMA_SNAPSHOT_OUT не задан — снимок не запрошен")
	}
	dsn := pgtest.NewEmptyDB(t)
	if err := pgtest.Goose(migrations.FS)(context.Background(), dsn); err != nil {
		t.Fatalf("проиграть цепочку миграций: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	defer func() { _ = db.Close() }()

	var lines []string
	add := func(q string) {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatalf("запрос слепка: %v", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("чтение слепка: %v", err)
			}
			lines = append(lines, s)
		}
	}
	// колонки с типами и умолчаниями
	add(`SELECT 'COL '||table_name||'.'||column_name||' '||data_type||' null='||is_nullable||
	       ' def='||coalesce(column_default,'-')
	     FROM information_schema.columns WHERE table_schema='kacho_iam'`)
	// ограничения с их выражениями
	add(`SELECT 'CON '||rel.relname||' '||con.conname||' '||pg_get_constraintdef(con.oid)
	     FROM pg_constraint con JOIN pg_class rel ON rel.oid=con.conrelid
	     JOIN pg_namespace n ON n.oid=rel.relnamespace WHERE n.nspname='kacho_iam'`)
	// индексы
	add(`SELECT 'IDX '||indexname||' '||indexdef FROM pg_indexes WHERE schemaname='kacho_iam'`)
	// триггеры
	add(`SELECT 'TRG '||c.relname||' '||t.tgname||' '||pg_get_triggerdef(t.oid)
	     FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
	     JOIN pg_namespace n ON n.oid=c.relnamespace
	     WHERE n.nspname='kacho_iam' AND NOT t.tgisinternal`)
	// функции
	add(`SELECT 'FUN '||p.proname||' '||md5(pg_get_functiondef(p.oid))
	     FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='kacho_iam'`)

	// Дамп снимается ТЕМ ЖЕ соединением, что и слепок: он даст SQL, которым
	// сведённая миграция и воспроизведёт схему. Слепок остаётся ПРЕДИКАТОМ
	// равенства, дамп — исходником; путать их нельзя.
	if dumpTo := os.Getenv("KACHO_SCHEMA_DUMP_OUT"); dumpTo != "" {
		cmd := exec.Command("pg_dump", "--schema-only", "--no-owner", "--no-privileges",
			"--schema=kacho_iam", dsn)
		out, derr := cmd.Output()
		if derr != nil {
			t.Fatalf("снять дамп: %v", derr)
		}
		if werr := os.WriteFile(dumpTo, out, 0o644); werr != nil {
			t.Fatalf("запись дампа: %v", werr)
		}
		t.Logf("дамп схемы: байт %d → %s", len(out), dumpTo)
	}

	// Перепись СТРОК: дамп структуры их не несёт, а 47 миграций сеют. Различить
	// справочник продукта и данные стенда можно только увидев, что осталось.
	if rowsTo := os.Getenv("KACHO_SCHEMA_ROWS_OUT"); rowsTo != "" {
		var rl []string
		rs, rerr := db.Query(`SELECT table_name FROM information_schema.tables
		    WHERE table_schema='kacho_iam' AND table_type='BASE TABLE' ORDER BY table_name`)
		if rerr != nil {
			t.Fatalf("перечень таблиц: %v", rerr)
		}
		var names []string
		for rs.Next() {
			var n string
			if err := rs.Scan(&n); err != nil {
				t.Fatalf("имя таблицы: %v", err)
			}
			names = append(names, n)
		}
		_ = rs.Close()
		for _, n := range names {
			var c int
			if err := db.QueryRow(`SELECT count(*) FROM kacho_iam.` + n).Scan(&c); err != nil {
				t.Fatalf("счёт строк %s: %v", n, err)
			}
			if c > 0 {
				rl = append(rl, n+" "+itoa(c))
			}
		}
		if werr := os.WriteFile(rowsTo, []byte(strings.Join(rl, "\n")+"\n"), 0o644); werr != nil {
			t.Fatalf("запись переписи строк: %v", werr)
		}
		t.Logf("таблиц с данными: %d из %d", len(rl), len(names))
	}

	sort.Strings(lines)
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("запись слепка: %v", err)
	}
	t.Logf("слепок схемы: строк %d → %s", len(lines), out)
}
