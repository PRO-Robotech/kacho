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

// Test_CursorIndexGate_RedsOnDeepEqualityPrefix — префикс глубже одной колонки
// обслуживает обход только тогда, когда запрос несёт ОБА равенства. Зачесть его
// общему списку значило бы объявить покрытым то, что покрыто не будет (в дереве
// такой индекс есть — `addresses_project_subnet_page_idx`).
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
