// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listcursorindex_injection_test.go — доказательство того, что гейт курсорных
// индексов СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Прочтения диффа тут недостаточно: гейт сравнивает две стороны дерева, и
// ошибиться он может в обе — объявить находкой законный индекс либо не заметить
// дефект. Поэтому каждое утверждение ставится ПАРОЙ: дефект возвращается на
// синтетическом дереве и обязан дать красное С ИМЕНЕМ таблицы, а рядом стоит
// законный близнец той же формы, на котором гейт обязан молчать. Отрицание без
// положительного контроля зеленело бы на гейте, объявляющем находкой всё; одно
// положительное — на гейте, не делающем ничего.
//
// Дерево синтетическое (`t.TempDir`), поэтому проба детерминирована, не требует
// ни Docker, ни git, и не поплывёт от следующей миграции продукта. Настоящее
// дерево остаётся предметом самого гейта (`listcursorindex_test.go`).
//
// Состав синтетического дерева берётся `treecorpus.SyntheticTree` — тем самым
// конструктором, который пакет `treecorpus` предусмотрел для деревьев, не
// являющихся репозиторием. Это не откат к обходу диска: у временного каталога
// индекса git нет by construction, и спрашивать его не у чего.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// cursorInjectionTree собирает дерево из одного сервиса: миграция с таблицей и
// индексами (indexDDL) плюс репозиторий с курсорным чтением (readSQL).
func cursorInjectionTree(t *testing.T, indexDDL, readSQL string) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()

	mig := filepath.Join(root, "services", "alpha", "internal", "migrations")
	if err := os.MkdirAll(mig, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", mig, err)
	}
	body := `-- +goose Up
CREATE TABLE kacho_alpha.widgets (
    id         text PRIMARY KEY,
    project_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
` + indexDDL + `
-- +goose Down
DROP TABLE IF EXISTS kacho_alpha.widgets;
`
	if err := os.WriteFile(filepath.Join(mig, "0001_initial.sql"), []byte(body), 0o600); err != nil {
		t.Fatalf("пишу миграцию: %v", err)
	}

	repo := filepath.Join(root, "services", "alpha", "internal", "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", repo, err)
	}
	src := "package repo\n\nconst listWidgets = `" + readSQL + "`\n"
	if err := os.WriteFile(filepath.Join(repo, "widget.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("пишу репозиторий: %v", err)
	}

	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	return tree
}

// pageRead — обход, который продукт и делает: страница по курсору (created_at, id).
const pageRead = `SELECT id FROM kacho_alpha.widgets WHERE project_id = $1 ` +
	`ORDER BY created_at ASC, id ASC LIMIT $2`

// findingNames — имена таблиц, названные находками. Утверждать надо ИМЯ, а не
// счётчик: гейт, краснеющий не на том, счётчиком неотличим от исправного.
func findingNames(t *testing.T, tree *treecorpus.Tree) []string {
	t.Helper()
	c, err := SurveyCursorIndexes(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if c.Pairs == 0 && len(c.Unresolved) == 0 {
		t.Fatalf("синтетическое дерево не дало ни одной пары и ни одного нерешённого чтения — "+
			"проба утверждала бы о пустоте (файлов Go %d, миграций %d)", c.GoFiles, c.SQLFiles)
	}
	var out []string
	for _, f := range c.Findings {
		out = append(out, f.Service+"."+f.Table)
	}
	return out
}

// Test_CursorIndexGate_RedsOnMixedDirection — ВОЗВРАЩЁННЫЙ дефект nlb: ключи те,
// направление второго — обратное. Гейт обязан покраснеть и назвать таблицу.
func Test_CursorIndexGate_RedsOnMixedDirection(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_created_idx ON kacho_alpha.widgets (project_id, created_at DESC, id);",
		pageRead)
	got := findingNames(t, tree)
	if len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("смешанное направление индекса обязано быть находкой с именем таблицы; получено %v", got)
	}
}

// Test_CursorIndexGate_SilentOnMatchingDirection — законный близнец той же
// формы: направление совпадает. Гейт обязан молчать.
//
// Без этой половины предыдущее утверждение зеленело бы на гейте, объявляющем
// находкой любой составной индекс.
func Test_CursorIndexGate_SilentOnMatchingDirection(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id);",
		pageRead)
	if got := findingNames(t, tree); len(got) != 0 {
		t.Fatalf("совпадающее направление находкой не является; получено %v", got)
	}
}

// Test_CursorIndexGate_SilentOnFullyInvertedIndex — второй законный близнец, и
// он не косметика: btree читается в обе стороны, поэтому ЦЕЛИКОМ инвертированный
// индекс порядок отдаёт. Гейт, требующий дословного совпадения направлений,
// краснел бы на исправной схеме — а ложно краснеющий гейт снимают.
func Test_CursorIndexGate_SilentOnFullyInvertedIndex(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at DESC, id DESC);",
		pageRead)
	if got := findingNames(t, tree); len(got) != 0 {
		t.Fatalf("целиком инвертированный индекс отдаёт порядок обратным чтением и находкой не является; получено %v", got)
	}
}

// Test_CursorIndexGate_RedsOnLeadingKeyOnly — второй экземпляр того же класса:
// индекс несёт ТОЛЬКО первый ключ курсора. Так выглядят восемь таблиц vpc до
// фикса (`<t>_created_at_idx (created_at)`): ничьи не разрешаются, предикат
// продолжения диапазоном не выражается, порядок достраивается сортировкой.
func Test_CursorIndexGate_RedsOnLeadingKeyOnly(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_created_at_idx ON kacho_alpha.widgets (created_at);",
		pageRead)
	if got := findingNames(t, tree); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("индекс с одним ведущим ключом порядка не даёт; получено %v", got)
	}
}

// Test_CursorIndexGate_RedsOnPartialIndex — объявленная слепая зона проверяется
// как СВОЙСТВО, а не остаётся заявлением в шапке: частичный индекс покрытием не
// считается. Парный положительный контроль — тот же индекс без предиката.
func Test_CursorIndexGate_RedsOnPartialIndex(t *testing.T) {
	partial := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id) WHERE project_id <> '';",
		pageRead)
	if got := findingNames(t, partial); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("частичный индекс покрытием не считается; получено %v", got)
	}
	full := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id);",
		pageRead)
	if got := findingNames(t, full); len(got) != 0 {
		t.Fatalf("тот же индекс без предиката покрытием является; получено %v", got)
	}
}

// Test_CursorIndexGate_SilentOnPartialIndexWhoseePredicateTheReadCarries —
// послабление, заведённое вместе с вывозом журнала аудита (#812): частичный
// индекс ЗАСЧИТЫВАЕТСЯ, когда чтение несёт его предикат дословно.
//
// Пара обязательна и проверяется обеими половинами В ОДНОЙ пробе: тот же
// индекс с тем же чтением МИНУС предикат обязан снова стать находкой. Иначе
// «засчитан» было бы неотличимо от «частичные засчитываются всегда».
func Test_CursorIndexGate_SilentOnPartialIndexWhosePredicateTheReadCarries(t *testing.T) {
	const partialIdx = "CREATE INDEX widgets_live_cursor_idx ON kacho_alpha.widgets " +
		"(created_at, id) WHERE project_id <> '';"

	carries := cursorInjectionTree(t, partialIdx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> '' "+
			"ORDER BY created_at ASC, id ASC LIMIT $1")
	if got := findingNames(t, carries); len(got) != 0 {
		t.Fatalf("чтение несёт предикат индекса дословно — индекс обязан засчитаться; получено %v", got)
	}

	// Законный близнец наоборот: тот же индекс, чтение БЕЗ предиката.
	silentAbout := cursorInjectionTree(t, partialIdx,
		"SELECT id FROM kacho_alpha.widgets ORDER BY created_at ASC, id ASC LIMIT $1")
	if got := findingNames(t, silentAbout); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("чтение без предиката индекса покрытым не является; получено %v", got)
	}

	// И третья половина: предикат, ПОХОЖИЙ на индексный, но не тот.
	nearMiss := cursorInjectionTree(t, partialIdx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> $1 "+
			"ORDER BY created_at ASC, id ASC LIMIT $2")
	if got := findingNames(t, nearMiss); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("похожий, но иной предикат покрытием не является; получено %v", got)
	}
}

// Test_CursorIndexGate_PartialIndexEasingIsNoWiderThanImplication — послабление
// выше НЕ ШИРЕ того, что оно доказывает.
//
// # Зачем отдельная проба
//
// Первая редакция послабления искала предикат ПОДСТРОКОЙ, и этого достаточно
// ровно для одного вывода — «текст встретился», — а нужен другой: «строки
// запроса суть подмножество строк индекса». Три случая ниже разошлись у этих
// выводов, и все три засчитывались:
//
//   - дизъюнкция — чтение ШИРЕ индекса, строки вне предиката ему нужны;
//   - отрицание — чтению нужно ДОПОЛНЕНИЕ множества индекса;
//   - предикат в КОММЕНТАРИИ — он не исполняется вовсе; на стороне индекса тот
//     же класс закрыт соседней пробой Test_CursorIndexGate_RedsOnIndexNamedOnlyInAComment.
//
// Законный близнец идёт ЗДЕСЬ ЖЕ: предикат конъюнктом верхнего уровня рядом с
// другим условием обязан по-прежнему засчитываться. Без него проба зеленела бы
// на послаблении, снятом целиком.
func Test_CursorIndexGate_PartialIndexEasingIsNoWiderThanImplication(t *testing.T) {
	const partialIdx = "CREATE INDEX widgets_live_cursor_idx ON kacho_alpha.widgets " +
		"(created_at, id) WHERE project_id <> '';"
	const tail = " ORDER BY created_at ASC, id ASC LIMIT $9"

	for _, tc := range []struct {
		name  string
		where string
		serve bool
	}{
		{"дизъюнкция", "WHERE project_id <> '' OR created_at < $1", false},
		{"отрицание", "WHERE NOT (project_id <> '')", false},
		{"предикат в комментарии", "/* project_id <> '' */", false},
		{"AND внутри скобок", "WHERE (project_id <> '' AND created_at < $1) OR id = $2", false},
		{"конъюнкт верхнего уровня", "WHERE project_id <> '' AND created_at < $1", true},
		{"он же вторым конъюнктом", "WHERE created_at < $1 AND project_id <> ''", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := cursorInjectionTree(t, partialIdx,
				"SELECT id FROM kacho_alpha.widgets "+tc.where+tail)
			got := findingNames(t, tree)
			switch {
			case tc.serve && len(got) != 0:
				t.Fatalf("предикат стоит конъюнктом верхнего уровня — индекс обязан засчитаться; получено %v", got)
			case !tc.serve && (len(got) != 1 || got[0] != "alpha.widgets"):
				t.Fatalf("чтение не является подмножеством индекса — засчитывать нельзя; получено %v", got)
			}
		})
	}
}

// Test_CursorIndexGate_PredicateComparisonKeepsLiteralCase — регистр ВНУТРИ
// строкового литерала значим: `'SENT'` и `'sent'` отбирают разные строки.
//
// Проба стоит рядом с послаблением, потому что опускание регистра было бы
// естественной «нормализацией» и молча расширило бы его на индекс, построенный
// по другому множеству.
func Test_CursorIndexGate_PredicateComparisonKeepsLiteralCase(t *testing.T) {
	const idx = "CREATE INDEX widgets_live_cursor_idx ON kacho_alpha.widgets " +
		"(created_at, id) WHERE project_id <> 'SENT';"
	same := cursorInjectionTree(t, idx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> 'SENT' "+
			"ORDER BY created_at ASC, id ASC LIMIT $1")
	if got := findingNames(t, same); len(got) != 0 {
		t.Fatalf("дословно тот же литерал обязан засчитаться; получено %v", got)
	}
	other := cursorInjectionTree(t, idx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> 'sent' "+
			"ORDER BY created_at ASC, id ASC LIMIT $1")
	if got := findingNames(t, other); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("иной регистр литерала — иное множество строк; получено %v", got)
	}
}

// Test_CursorIndexGate_SilentOnPredicateWrittenInTheDumpForm — предикат,
// объявленный формой `pg_dump`, засчитывается наравне с рукописным.
//
// # Зачем отдельная проба
//
// Форма записи предиката в этом дереве ДВЕ, и обе законны: рука пишет
// `WHERE status <> 'sent'`, инструмент — `WHERE (status <> 'sent'::text)`. Вторая
// пришла со сводом миграций iam 2026-09-04 (файл написан `pg_dump`) и накрывает
// не край, а весь свод: приведений `::text` в предикатах индексов дерева 45.
//
// Разбор, знающий одну форму, объявляет обслуживающий индекс не обслуживающим —
// то есть даёт находку о схеме, которая ни в чём не виновата. Наблюдалось на
// `iam.audit_outbox`: те же ключи, тот же предикат, находка.
//
// Обе половины В ОДНОЙ пробе: форма дампа обязана засчитаться, а тот же индекс
// с ИНЫМ предикатом — остаться находкой. Иначе «засчитан» было бы неотличимо
// от «приведение снимает различие вообще».
func Test_CursorIndexGate_SilentOnPredicateWrittenInTheDumpForm(t *testing.T) {
	// Скобки вокруг предиката и приведение на литерале — ровно то, что пишет `pg_dump`.
	const dumpIdx = "CREATE INDEX widgets_live_cursor_idx ON kacho_alpha.widgets " +
		"USING btree (created_at, id) WHERE (project_id <> ''::text);"

	handWritten := cursorInjectionTree(t, dumpIdx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> '' "+
			"ORDER BY created_at ASC, id ASC LIMIT $1")
	if got := findingNames(t, handWritten); len(got) != 0 {
		t.Fatalf("форма дампа объявляет ТОТ ЖЕ предикат — индекс обязан засчитаться; получено %v", got)
	}

	// Зеркало: чтение тоже в форме дампа — приведение снимается с ОБЕИХ сторон.
	bothDump := cursorInjectionTree(t, dumpIdx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> ''::text "+
			"ORDER BY created_at ASC, id ASC LIMIT $1")
	if got := findingNames(t, bothDump); len(got) != 0 {
		t.Fatalf("обе стороны в форме дампа — индекс обязан засчитаться; получено %v", got)
	}

	// Законный близнец: тот же индекс формы дампа, чтение с ИНЫМ предикатом.
	other := cursorInjectionTree(t, dumpIdx,
		"SELECT id FROM kacho_alpha.widgets WHERE project_id <> $1 "+
			"ORDER BY created_at ASC, id ASC LIMIT $2")
	if got := findingNames(t, other); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("иной предикат покрытием не является и в форме дампа; получено %v", got)
	}
}

// Test_CursorIndexGate_CastStrippingDoesNotEquateDifferentLiterals — снятие
// приведения НЕ ШИРЕ того, что оно доказывает.
//
// # Зачем отдельная проба
//
// Снятие приведения — единственное место разбора, где различие в тексте
// намеренно ГАСИТСЯ. Всякое такое гашение рискует расширить засчитываемое, и
// проверяется это не прочтением, а входом: значение, тип и предмет сравнения
// обязаны уцелеть. Три оси, каждая своим случаем.
func Test_CursorIndexGate_CastStrippingDoesNotEquateDifferentLiterals(t *testing.T) {
	cases := []struct {
		name, idx, read string
		covered         bool
	}{{
		name: "приведение снято — значение то же",
		idx:  "WHERE (status <> 'sent'::text)",
		read: "WHERE status <> 'sent'", covered: true,
	}, {
		// Регистр внутри литерала значим и после снятия приведения.
		name: "иной регистр литерала остаётся иным множеством",
		idx:  "WHERE (status <> 'SENT'::text)",
		read: "WHERE status <> 'sent'", covered: false,
	}, {
		name: "иное значение литерала остаётся иным",
		idx:  "WHERE (status <> 'sent'::text)",
		read: "WHERE status <> 'draft'", covered: false,
	}, {
		// Приведение на КОЛОНКЕ смысл предиката меняет — и не снимается.
		name: "приведение на колонке не снимается",
		idx:  "WHERE (created_at::date = '2026-01-01')",
		read: "WHERE created_at = '2026-01-01'", covered: false,
	}, {
		// Многословное имя типа съедается целиком и не утаскивает соседний конъюнкт.
		name: "многословный тип не утаскивает соседний конъюнкт",
		idx:  "WHERE (revoked_at > '2000-01-01'::timestamp with time zone)",
		read: "WHERE revoked_at > '2000-01-01' AND status <> 'sent'", covered: true,
	}, {
		// Слово за литералом, приведением НЕ являющееся, остаётся на месте.
		name: "слово за литералом не съедается",
		idx:  "WHERE (status <> 'sent'::text)",
		read: "WHERE status <> 'sent' AND kind = 'a'", covered: true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := cursorInjectionTree(t,
				"CREATE INDEX widgets_live_cursor_idx ON kacho_alpha.widgets "+
					"USING btree (created_at, id) "+tc.idx+";",
				"SELECT id FROM kacho_alpha.widgets "+tc.read+
					" ORDER BY created_at ASC, id ASC LIMIT $1")
			got := findingNames(t, tree)
			if tc.covered && len(got) != 0 {
				t.Fatalf("предикат тот же — индекс обязан засчитаться; получено %v", got)
			}
			if !tc.covered && (len(got) != 1 || got[0] != "alpha.widgets") {
				t.Fatalf("предикат ИНОЙ — индекс засчитываться не должен; получено %v", got)
			}
		})
	}
}

// Test_CursorIndexGate_RedsOnDeepEqualityPrefix — префикс глубже одной колонки
// обслуживает обход только тогда, когда запрос несёт ОБА равенства. Зачесть его
// общему списку значило бы объявить покрытым то, что покрыто не будет (в дереве
// такой индекс БЫЛ — постраничный по паре (проект, подсеть), снят по #963
// именно потому, что второе равенство несёт не всякое чтение).
func Test_CursorIndexGate_RedsOnDeepEqualityPrefix(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_deep_idx ON kacho_alpha.widgets (project_id, zone_id, created_at, id);",
		pageRead)
	if got := findingNames(t, tree); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("префикс глубже одной колонки покрытием не считается; получено %v", got)
	}
}

// Test_CursorIndexGate_SilentOnDroppedIndexBeingRebuilt — проигрывание обязано
// прийти к тому же состоянию схемы, к какому пришёл бы Postgres. Законная
// перестройка «снять и тут же создать заново» ДВУМЯ раздельными проходами
// читалась бы как «снят», и гейт покраснел бы на ПРАВИЛЬНОЙ схеме.
func Test_CursorIndexGate_SilentOnDroppedIndexBeingRebuilt(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id);\n"+
			"DROP INDEX widgets_project_cursor_idx;\n"+
			"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id);",
		pageRead)
	if got := findingNames(t, tree); len(got) != 0 {
		t.Fatalf("перестройка индекса в одной миграции — законная форма; получено %v", got)
	}
}

// Test_CursorIndexGate_RedsOnDroppedIndex — обратная половина того же: если
// индекс СНЯТ и заново не создан, находка обязана быть. Без неё предыдущее
// утверждение зеленело бы на разборе, который `DROP INDEX` не читает вовсе.
func Test_CursorIndexGate_RedsOnDroppedIndex(t *testing.T) {
	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id);\n"+
			"DROP INDEX widgets_project_cursor_idx;",
		pageRead)
	if got := findingNames(t, tree); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("снятый и не восстановленный индекс обязан быть находкой; получено %v", got)
	}
}

// Test_CursorIndexGate_RedsOnIndexNamedOnlyInAComment — гейт читает ИСПОЛНЯЕМУЮ
// часть миграции. Индекс, приведённый в комментарии примером, индексом не
// является, поэтому таблица остаётся находкой. Положительная половина пары —
// Test_CursorIndexGate_SilentOnMatchingDirection: тот же текст, стоящий в коде,
// покрытием засчитывается.
func Test_CursorIndexGate_RedsOnIndexNamedOnlyInAComment(t *testing.T) {
	tree := cursorInjectionTree(t,
		"-- CREATE INDEX widgets_project_cursor_idx ON kacho_alpha.widgets (project_id, created_at, id);",
		pageRead)
	if got := findingNames(t, tree); len(got) != 1 || got[0] != "alpha.widgets" {
		t.Fatalf("индекс из комментария индексом не является; получено %v", got)
	}
}

// Test_CursorIndexGate_SilentOnSingleRowLookup — граница предмета проверяется с
// обеих сторон: чтение одной строки по почти-уникальному предикату (`LIMIT 1`)
// страницей не является и курсорного индекса не требует. Без этой половины гейт
// нёс бы две находки без предмета уже на дереве продукта, а перечень с
// беспредметными записями перестают читать целиком.
func Test_CursorIndexGate_SilentOnSingleRowLookup(t *testing.T) {
	lookup := `SELECT id FROM kacho_alpha.widgets WHERE project_id = $1 ` +
		`ORDER BY created_at ASC, id ASC LIMIT 1`
	tree := cursorInjectionTree(t, "-- индексов нет вовсе", lookup)
	c, err := SurveyCursorIndexes(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(c.Findings) != 0 {
		t.Fatalf("чтение с LIMIT 1 страницей не является; находки: %v", c.Findings)
	}
	if c.Pairs != 0 {
		t.Fatalf("чтение с LIMIT 1 не должно попадать в пары; пар %d", c.Pairs)
	}
}

// Test_CursorIndexGate_ReportsUnresolvedTable — чтение с ВЫЧИСЛЯЕМЫМ именем
// таблицы не «пропускается»: именно на нём прежний замер потерял семь таблиц.
// Гейт обязан отдать его отдельным перечнем, а объявление рядом — снять вопрос.
func Test_CursorIndexGate_ReportsUnresolvedTable(t *testing.T) {
	computed := "SELECT id FROM %s ORDER BY created_at ASC, id ASC LIMIT $1"

	tree := cursorInjectionTree(t,
		"CREATE INDEX widgets_cursor_idx ON kacho_alpha.widgets (created_at, id);",
		computed)
	c, err := SurveyCursorIndexes(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(c.Unresolved) != 1 {
		t.Fatalf("чтение с вычисляемым именем таблицы обязано попасть в нерешённые; получено %d", len(c.Unresolved))
	}
	if !strings.HasSuffix(c.Unresolved[0].File, "widget.go") {
		t.Fatalf("нерешённое чтение обязано называть координату; получено %q", c.Unresolved[0].File)
	}

	// Законный близнец: то же чтение с объявлением рядом — таблица названа,
	// нерешённых нет, покрытие считается по настоящему индексу.
	root := t.TempDir()
	mig := filepath.Join(root, "services", "alpha", "internal", "migrations")
	if err := os.MkdirAll(mig, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", mig, err)
	}
	if err := os.WriteFile(filepath.Join(mig, "0001_initial.sql"), []byte(
		"-- +goose Up\nCREATE TABLE kacho_alpha.widgets (id text PRIMARY KEY, created_at timestamptz);\n"+
			"CREATE INDEX widgets_cursor_idx ON kacho_alpha.widgets (created_at, id);\n"), 0o600); err != nil {
		t.Fatalf("пишу миграцию: %v", err)
	}
	repo := filepath.Join(root, "services", "alpha", "internal", "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", repo, err)
	}
	src := "package repo\n\n// cursor-list-table: widgets\nconst listWidgets = `" + computed + "`\n"
	if err := os.WriteFile(filepath.Join(repo, "widget.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("пишу репозиторий: %v", err)
	}
	declared, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	c2, err := SurveyCursorIndexes(declared)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(c2.Unresolved) != 0 || len(c2.Findings) != 0 || c2.Pairs != 1 {
		t.Fatalf("объявление рядом с чтением обязано разрешить таблицу: нерешённых %d, находок %d, пар %d",
			len(c2.Unresolved), len(c2.Findings), c2.Pairs)
	}
}

// Test_CursorIndexGate_ResolvesAliasBoundInAnotherLiteral — второе место, где
// прежний предикат слеп: `FROM` живёт в отдельной константе, а запрос
// собирается форматированием из неё (`diskTypeSelect`, `snapshotFrom`).
// Псевдоним ключа связывается по ВСЕМУ файлу, а не по одному литералу.
func Test_CursorIndexGate_ResolvesAliasBoundInAnotherLiteral(t *testing.T) {
	root := t.TempDir()
	mig := filepath.Join(root, "services", "alpha", "internal", "migrations")
	if err := os.MkdirAll(mig, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", mig, err)
	}
	if err := os.WriteFile(filepath.Join(mig, "0001_initial.sql"), []byte(
		"-- +goose Up\nCREATE TABLE kacho_alpha.widgets (id text PRIMARY KEY, created_at timestamptz);\n"), 0o600); err != nil {
		t.Fatalf("пишу миграцию: %v", err)
	}
	repo := filepath.Join(root, "services", "alpha", "internal", "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", repo, err)
	}
	src := "package repo\n\n" +
		"const widgetSelect = `SELECT w.id FROM kacho_alpha.widgets w`\n\n" +
		"const listWidgets = `%s%s ORDER BY w.created_at ASC, w.id ASC LIMIT $1`\n"
	if err := os.WriteFile(filepath.Join(repo, "widget.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("пишу репозиторий: %v", err)
	}
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	c, err := SurveyCursorIndexes(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(c.Unresolved) != 0 {
		t.Fatalf("псевдоним связан в соседнем литерале того же файла — чтение обязано разрешиться; нерешённых %d", len(c.Unresolved))
	}
	if len(c.Findings) != 1 || c.Findings[0].Table != "widgets" {
		t.Fatalf("таблица без курсорного индекса обязана быть находкой по имени; получено %v", c.Findings)
	}
}
