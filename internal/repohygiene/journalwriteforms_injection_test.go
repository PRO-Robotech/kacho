// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalwriteforms_injection_test.go — способность переписи форм записи журнала
// РАЗЛИЧАТЬ, доказанная в обе стороны ПО КАЖДОЙ ФОРМЕ.
//
// # Почему инъекция здесь особенная
//
// У обычного гейта доказательство одно: верни дефект — покраснеет; поставь
// законный близнец — смолчит. У ПЕРЕПИСИ доказывать надо третье, и именно оно
// составляет предмет задачи (#1573): что ноль по форме означает «искали и не
// нашли», а не «не искали».
//
// Отсюда форма доказательства: для каждой формы стенд ПЕРЕНАЦЕЛИВАЕТ её точку на
// НЕ-журнальную таблицу. Тогда журнальных точек этой формы становится ноль, а
// распознаватель формы по-прежнему видит её экземпляр — то есть перепись
// печатает ноль, ЗНАЯ форму. Простое удаление точки этого не доказало бы: оно
// обнуляет и распознаватель, и тогда ноль снова означает «не искали».
//
// # Изоляция
//
// Дерево строится в `t.TempDir()` обычной записью файлов. Ни `git init`, ни
// `git add`, ни `git config`: запись в индекс репозитория, из которого идёт
// прогон, делает лживыми ВСЕ гейты, читающие дерево.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── СОДЕРЖИМОЕ СТЕНДА ───────────────────────────────────────────────────────

// standJournalDecl — объявление владельца: каталог `subscriptionjournal` с
// константой `Table`. Отсюда перепись выводит владельцев — их перечень не
// выписан ни в гейте, ни здесь.
const standJournalDecl = `package subscriptionjournal

const (
	Table       = "demo_outbox"
	KindThing   = "Thing"
	ChangeMoved = "MOVED"
)
`

// standPortCall — форма 1 плюс ДВА законных близнеца.
//
// Близнец первый — `FGARegisterOutbox().Emit(ctx, …)`: ОЧЕРЕДЬ ПРАВ, не журнал.
// Текстовый предикат `Outbox().Emit(ctx` считает её журналом, потому что это
// ПОДСТРОКА; разбор по узлу требует, чтобы приёмник был вызовом именно
// `Outbox()`. На настоящем дереве расхождение этих двух предикатов у одного
// владельца — тринадцать точек из тридцати одной.
//
// Близнец второй — вызов `Emit` у чего-то, что вовсе не выражение вызова.
const standPortCall = `package thing

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/demo/internal/subscriptionjournal"
)

func Create(ctx context.Context, w writer, id string) error {
	if err := w.Outbox().Emit(ctx, subscriptionjournal.KindThing, id, "CREATED", nil); err != nil {
		return err
	}
	if _, err := w.FGARegisterOutbox().Emit(ctx, "REGISTER", id); err != nil {
		return err
	}
	return w.plain.Emit(ctx, "Thing", id)
}
`

// standLibraryCall — форма 2: обёртка сервиса над общей библиотекой. Имя таблицы
// стоит константой пакета и потому разрешается разбором; вид и род приходят
// параметрами, значит точка ничего не называет сама и идёт ПЕРЕНОСОМ.
const standLibraryCall = `package repo

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/outbox"
)

const demoOutboxTable = "demo_outbox"

func emitDemo(ctx context.Context, tx pgx.Tx, kind, id, eventType string, payload map[string]any) error {
	return outbox.Emit(ctx, tx, demoOutboxTable, kind, id, eventType, payload)
}
`

// standLiteralStatement — форма 3 и три законных близнеца:
//
//	«не журнал» — вставка в СОСЕДНЮЮ таблицу того же сервиса;
//	«не оператор» — текст ОТКАЗА, где `insert into %s` стоит прозой. Без разбора
//	  формы оператора он считался бы точкой записи, и перепись росла бы от
//	  объяснений, а не от кода;
//	«за границей оператора» — литерал СЛЕДУЮЩЕГО оператора: слово `Moved` стоит
//	  после `;` в ПРАВКЕ, а не во вставке. Взят именно `UPDATE`, а не вторая вставка:
//	  у второй вставки границу держал бы сам маркер `INSERT INTO`, и проба зеленела бы
//	  при снятой границе `;` — то есть доказывала бы не то, что заявляет.
const standLiteralStatement = `package pg

import "fmt"

const insertJournal = ` + "`" + `INSERT INTO demo_outbox (resource_kind, resource_id, event_type)
	VALUES ('Thing', $1, 'CREATED');
	UPDATE demo_state SET note = 'Moved'` + "`" + `

const insertNeighbour = ` + "`" + `INSERT INTO demo_audit (note) VALUES ('Thing')` + "`" + `

func wrap(err error, table string) error {
	return fmt.Errorf("demo: insert into %s: %w", table, err)
}
`

// standSchemaFormatted — форма 4: схема подставляется, имя таблицы литералом.
// Предикат по ПОЛНОМУ имени таблицы такую точку не находит; предикат по имени
// без схемы находит.
const standSchemaFormatted = `package pg

import "fmt"

func insertBySchema(schema string) string {
	return fmt.Sprintf(` + "`" + `INSERT INTO %s.demo_outbox (resource_kind) VALUES ('Thing')` + "`" + `, schema)
}
`

// standNameFormatted — форма 5: имя таблицы форматируется ЦЕЛИКОМ и
// РАЗРЕШАЕТСЯ до константы пакета. Рядом законный близнец — та же форма,
// нацеленная на не-журнальную таблицу.
const standNameFormatted = `package pg

import "fmt"

const (
	journalTable  = "demo_outbox"
	auditTable    = "demo_audit"
)

func insertByName() (string, string) {
	return fmt.Sprintf(` + "`" + `INSERT INTO %s (resource_kind) VALUES ('Thing')` + "`" + `, journalTable),
		fmt.Sprintf(` + "`" + `INSERT INTO %s (note) VALUES ('Thing')` + "`" + `, auditTable)
}
`

// standTrigger — форма 6 и законный близнец в той же миграции.
const standTrigger = `-- +goose Up
CREATE TABLE demo_outbox (sequence_no BIGSERIAL PRIMARY KEY);

-- +goose StatementBegin
CREATE FUNCTION demo_outbox_emit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO demo_outbox (resource_kind, resource_id, event_type)
    VALUES ('Thing', NEW.id, 'UPDATED');
    INSERT INTO demo_audit (note) VALUES ('not the journal');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
`

// standTestFileTwin — вставка в журнал, стоящая в ПРОБЕ. Перепись судит
// прод-дерево: засчитать пробу значило бы замкнуть наблюдение на само себя.
const standTestFileTwin = `package pg

const probeInsert = ` + "`" + `INSERT INTO demo_outbox (resource_kind) VALUES ('Thing')` + "`" + `
`

// standSharedLibrary — общая библиотека: имя таблицы приходит ПАРАМЕТРОМ.
// Предмет вставки решает вызывающий, поэтому это ПЕРЕНОС, а не неустановленный
// предмет. Законный близнец к инъекции «предмет не установлен».
const standSharedLibrary = `package outbox

import "fmt"

func emit(table string) string {
	return fmt.Sprintf(` + "`" + `INSERT INTO %s (resource_kind) VALUES ($1)` + "`" + `, table)
}
`

// standLocalTransport — ПЕРЕНОС СЕРВИСА: своя функция принимает имя таблицы
// параметром. Её вызывающих перепись НЕ разбирает — разбор вызывающих сделан
// ровно для общей библиотеки. Граница названа числом
// (`TransportsOutsideSharedLibrary`), а не подразумевается.
const standLocalTransport = `package pg

import "fmt"

func insertInto(table string) string {
	return fmt.Sprintf(` + "`" + `INSERT INTO %s (resource_kind) VALUES ($1)` + "`" + `, table)
}
`

// standPortCallElsewhere — та же форма вызова порта, но в сервисе БЕЗ журнала.
// Служит перенацеливанием для инъекции формы 1: у формы нет имени таблицы,
// поэтому «не журнал» для неё означает «не каталог владельца».
const standPortCallElsewhere = `package thing

import "context"

func CreateElsewhere(ctx context.Context, w writer, id string) error {
	return w.Outbox().Emit(ctx, "Thing", id, "CREATED", nil)
}
`

// standUnresolvedInsert — ДЕФЕКТ: имя таблицы форматируется целиком и приходит
// из локальной переменной. Разбор его не разрешает, значит про оператор не
// известно даже того, пишет ли он журнал.
const standUnresolvedInsert = `package pg

import "fmt"

func insertSomewhere() string {
	target := lookupTable()
	return fmt.Sprintf(` + "`" + `INSERT INTO %s (resource_kind) VALUES ('Thing')` + "`" + `, target)
}
`

// ── СБОРКА СТЕНДА ───────────────────────────────────────────────────────────

// journalStandFiles — полный стенд. Ключ — путь от корня.
func journalStandFiles() map[string]string {
	return map[string]string{
		"services/demo/internal/subscriptionjournal/journal.go":  standJournalDecl,
		"services/demo/internal/apps/kaname/api/thing/create.go": standPortCall,
		"services/demo/internal/repo/outbox.go":                  standLibraryCall,
		"services/demo/internal/repo/pg/literal.go":              standLiteralStatement,
		"services/demo/internal/repo/pg/schema.go":               standSchemaFormatted,
		"services/demo/internal/repo/pg/named.go":                standNameFormatted,
		"services/demo/internal/repo/pg/probe_test.go":           standTestFileTwin,
		"services/demo/internal/repo/pg/localtransport.go":       standLocalTransport,
		"services/demo/internal/migrations/0001_initial.sql":     standTrigger,
		"pkg/outbox/emit.go":                                     standSharedLibrary,
	}
}

// writeJournalStand раскладывает стенд и возвращает его корень.
func writeJournalStand(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	return root
}

// censusOfStand собирает перепись синтетического дерева.
func censusOfStand(t *testing.T, files map[string]string) JournalWriteFormCensus {
	t.Helper()
	root := writeJournalStand(t, files)
	tree := newSyntheticTree(t, root)
	census, err := CensusJournalWriteForms(root, tree.files)
	if err != nil {
		t.Fatalf("перепись стенда не собрана: %v", err)
	}
	return census
}

// ── КОНТРОЛЬ: ЦЕЛЫЙ СТЕНД ───────────────────────────────────────────────────

// TestJournalWriteFormCensusControl — ПЕРВЫЙ ИЗ ТРЁХ ПРОГОНОВ.
//
// Всё цело: каждая форма найдена, каждый законный близнец НЕ засчитан, находок
// ноль. Без этого прогона молчание гейта на инъекции соседнего свойства было бы
// неотличимо от молчания мёртвого.
func TestJournalWriteFormCensusControl(t *testing.T) {
	c := censusOfStand(t, journalStandFiles())

	if len(c.Owners) != 1 || c.Owners[0].Service != "demo" || c.Owners[0].Bare != "demo_outbox" {
		t.Fatalf("владелец выведен неверно: %+v", c.Owners)
	}
	want := map[JournalWriteForm]int{
		JournalFormPortCall:         1,
		JournalFormLibraryCall:      1,
		JournalFormLiteralStatement: 1,
		JournalFormSchemaFormatted:  1,
		JournalFormNameFormatted:    1,
		JournalFormTrigger:          1,
	}
	for _, form := range JournalWriteForms {
		if got := len(c.PointsOf("demo", form)); got != want[form] {
			t.Errorf("контроль: форма %q — журнальных точек %d, ожидалось %d.\n"+
				"Перепись, не находящая целой формы, объявляет ноль там, где не искала",
				form, got, want[form])
		}
		if c.Recognizer[form] == 0 {
			t.Errorf("контроль: распознаватель формы %q не нашёл НИ ОДНОГО экземпляра", form)
		}
	}
	if got := len(c.Unresolved()); got != 0 {
		t.Errorf("контроль: неустановленных предметов %d, ожидался 0: %v", got, c.Unresolved())
	}
	if got := len(c.Formatted); got != 4 {
		t.Errorf("контроль: операторов с ЦЕЛИКОМ форматируемым именем %d, ожидалось 4 "+
			"(журнал · соседняя таблица · общая библиотека · перенос сервиса), нашлось: %+v",
			got, c.Formatted)
	}
	if got := len(c.TransportsOutsideSharedLibrary()); got != 1 {
		t.Errorf("контроль: переносов ВНЕ общей библиотеки %d, ожидался 1. Величина названа "+
			"числом именно затем, что вызывающих такого переноса перепись не разбирает — "+
			"это край, и он обязан быть виден: %+v", got, c.TransportsOutsideSharedLibrary())
	}
	if fails := JournalCensusPremiseFailures(c); len(fails) != 0 {
		t.Errorf("контроль: предпосылка не выполнена: %v", fails)
	}
	if found := JournalCensusFindings(c); len(found) != 0 {
		t.Errorf("контроль: находок %d, ожидался 0 — гейт краснеет на исправном стенде:\n%s",
			len(found), strings.Join(found, "\n"))
	}
}

// TestJournalWriteFormCensusIgnoresLegitimateTwins — ЗАКОННЫЕ БЛИЗНЕЦЫ МОЛЧАТ.
//
// Каждый из них — форма, ПОХОЖАЯ на точку записи журнала. Без этих утверждений
// перепись ловила бы форму, а не существо, и первый ложный срабат её отключил бы.
func TestJournalWriteFormCensusIgnoresLegitimateTwins(t *testing.T) {
	c := censusOfStand(t, journalStandFiles())
	all := append([]JournalWritePoint(nil), c.Points...)

	// Порт: точка ровно одна, хотя вызовов `Emit` в файле три.
	if got := len(c.PointsOf("demo", JournalFormPortCall)); got != 1 {
		t.Errorf("очередь прав засчитана журналом: точек формы порта %d, ожидалась 1. "+
			"Разбор обязан требовать приёмника `Outbox()`, а не подстроки", got)
	}
	// Проба.
	for _, p := range all {
		if strings.Contains(p.Pos, "_test.go") {
			t.Errorf("%s: вставка из ПРОБЫ засчитана точкой записи. Перепись судит "+
				"прод-дерево: засчитать пробу значит замкнуть наблюдение на само себя", p.Pos)
		}
	}
	// Проза отказа `insert into %s: %w`.
	for _, f := range c.Formatted {
		if strings.Contains(f.Pos, "literal.go") {
			t.Errorf("%s: текст ОТКАЗА засчитан оператором вставки. Тогда перепись росла "+
				"бы от объяснений, а не от кода", f.Pos)
		}
	}
	// Общая библиотека — ПЕРЕНОС, а не неустановленный предмет.
	for _, f := range c.Formatted {
		if strings.HasPrefix(f.Pos, "pkg/outbox/") && !f.Transport {
			t.Errorf("%s: имя таблицы приходит ПАРАМЕТРОМ, а перепись не считает это "+
				"переносом. Тогда общая библиотека объявлялась бы неустановленным "+
				"предметом при каждом прогоне, и находка перестала бы что-либо значить",
				f.Pos)
		}
	}
	// Слово соседнего оператора не приписано этой точке.
	for _, p := range c.PointsOf("demo", JournalFormLiteralStatement) {
		for _, w := range p.Literals {
			if w == "Moved" {
				t.Errorf("%s: слово соседнего оператора приписано этой точке — граница "+
					"оператора не соблюдена, и перенос стал бы неотличим от точки", p.Pos)
			}
		}
	}
}

// ── ИНЪЕКЦИЯ ПО КАЖДОЙ ФОРМЕ: НОЛЬ ЗНАЧИТ «ИСКАЛИ И НЕ НАШЛИ» ───────────────

// TestJournalWriteFormZeroMeansSearchedAndNotFound — ВТОРОЙ ПРОГОН.
//
// По каждой форме точка ПЕРЕНАЦЕЛИВАЕТСЯ на не-журнальную таблицу. Журнальных
// точек этой формы становится ноль, а распознаватель формы её по-прежнему видит.
// Именно эта пара и есть предмет задачи: ноль печатается ЗНАЯ форму.
//
// Прочие формы при этом обязаны остаться на месте — инъекция роняет только
// проверяемое.
func TestJournalWriteFormZeroMeansSearchedAndNotFound(t *testing.T) {
	for _, tc := range []struct {
		form    JournalWriteForm
		file    string
		from    string
		to      string
		addFile string
		addBody string
	}{
		{
			form: JournalFormPortCall,
			file: "services/demo/internal/apps/kaname/api/thing/create.go",
			from: "w.Outbox().Emit(ctx,",
			to:   "w.FGARegisterOutbox().Emit(ctx,",
			// Форма порта имени таблицы не называет вовсе, поэтому
			// «перенацелить» её значит перенести в сервис БЕЗ журнала: там она
			// остаётся экземпляром формы и перестаёт быть точкой владельца.
			addFile: "services/other/internal/apps/kaname/api/thing/create.go",
			addBody: standPortCallElsewhere,
		},
		{
			form: JournalFormLibraryCall,
			file: "services/demo/internal/repo/outbox.go",
			from: `demoOutboxTable = "demo_outbox"`,
			to:   `demoOutboxTable = "demo_audit"`,
		},
		{
			form: JournalFormLiteralStatement,
			file: "services/demo/internal/repo/pg/literal.go",
			from: "INSERT INTO demo_outbox (resource_kind, resource_id, event_type)",
			to:   "INSERT INTO demo_audit (resource_kind, resource_id, event_type)",
		},
		{
			form: JournalFormSchemaFormatted,
			file: "services/demo/internal/repo/pg/schema.go",
			from: "INSERT INTO %s.demo_outbox",
			to:   "INSERT INTO %s.demo_audit",
		},
		{
			form: JournalFormNameFormatted,
			file: "services/demo/internal/repo/pg/named.go",
			from: `journalTable  = "demo_outbox"`,
			to:   `journalTable  = "demo_audit"`,
		},
		{
			form: JournalFormTrigger,
			file: "services/demo/internal/migrations/0001_initial.sql",
			from: "    INSERT INTO demo_outbox (resource_kind, resource_id, event_type)",
			to:   "    INSERT INTO demo_audit (resource_kind, resource_id, event_type)",
		},
	} {
		t.Run(string(tc.form), func(t *testing.T) {
			files := journalStandFiles()
			body, ok := files[tc.file]
			if !ok || !strings.Contains(body, tc.from) {
				t.Fatalf("инъекция негодна: в %s нет %q — фикстура разошлась со стендом, и "+
					"её молчание ничего не доказывает", tc.file, tc.from)
			}
			files[tc.file] = strings.Replace(body, tc.from, tc.to, 1)
			if tc.addFile != "" {
				files[tc.addFile] = tc.addBody
			}
			c := censusOfStand(t, files)

			if got := len(c.PointsOf("demo", tc.form)); got != 0 {
				t.Errorf("форма %q перенацелена на не-журнальную таблицу, а перепись всё "+
					"ещё видит у журнала %d точек: распознаватель не различает предмет и "+
					"считает форму, а не таблицу", tc.form, got)
			}
			if c.Recognizer[tc.form] == 0 {
				t.Errorf("форма %q после перенацеливания исчезла из дерева ВООБЩЕ "+
					"(распознаватель 0). Тогда ноль журнальных точек означает «не искали», "+
					"а доказать надо обратное — «искали и не нашли»", tc.form)
			}
			for _, other := range JournalWriteForms {
				if other == tc.form {
					continue
				}
				if len(c.PointsOf("demo", other)) != 1 {
					t.Errorf("инъекция формы %q задела форму %q (точек %d, ожидалась 1): "+
						"инъекция обязана ронять ТОЛЬКО проверяемое, иначе красное приходит "+
						"от соседа", tc.form, other, len(c.PointsOf("demo", other)))
				}
			}
		})
	}
}

// ── ИНЪЕКЦИЯ НАХОДОК ────────────────────────────────────────────────────────

// TestJournalCensusFindsUnresolvedInsert — ТРЕТИЙ ПРОГОН, НОВОЕ СВОЙСТВО.
//
// Вносится ровно один дефект: имя таблицы форматируется целиком и приходит из
// локальной переменной. Рядом, в том же стенде, стоят ДВА законных близнеца той
// же формы — константа пакета (`named.go`) и параметр функции (`pkg/outbox`), —
// и они обязаны молчать.
func TestJournalCensusFindsUnresolvedInsert(t *testing.T) {
	files := journalStandFiles()
	files["services/demo/internal/repo/pg/unresolved.go"] = standUnresolvedInsert
	c := censusOfStand(t, files)

	unresolved := c.Unresolved()
	if len(unresolved) != 1 {
		t.Fatalf("неустановленных предметов %d, ожидался 1: %+v.\nЛибо распознаватель не "+
			"видит дефекта, либо он объявляет неустановленным законного близнеца",
			len(unresolved), unresolved)
	}
	if !strings.Contains(unresolved[0].Pos, "unresolved.go") {
		t.Errorf("находка указывает на %s, а дефект внесён в unresolved.go — гейт называет "+
			"не ту координату, и читателя пошлют искать не туда", unresolved[0].Pos)
	}
	found := JournalCensusFindings(c)
	if len(found) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (только неустановленный предмет):\n%s",
			len(found), strings.Join(found, "\n"))
	}
	if !strings.Contains(found[0], "unresolved.go") ||
		!strings.Contains(found[0], "ПИШЕТ ЛИ ОН ЖУРНАЛ") {
		t.Errorf("текст находки не называет ни координаты, ни того, чем она опасна:\n%s", found[0])
	}
}

// TestJournalCensusFindsJournalWithoutProducer — ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО
// СВОЙСТВА, отдельным прогоном.
//
// Владелец объявлен, а производителя у него нет ни в одной форме. Проверка,
// зелёная на таком дереве, объявляла бы поток наполняемым, тогда как он не
// наполнится никогда.
//
// Прогон отдельный намеренно: инъекция «завести ещё один элемент» нарушила бы
// всё, что требуется от элементов вообще, и молчание соседней проверки стало бы
// неотличимо от её смерти (`testing.md` §«Гейт на класс», п. 2в).
func TestJournalCensusFindsJournalWithoutProducer(t *testing.T) {
	files := journalStandFiles()
	files["services/quiet/internal/subscriptionjournal/journal.go"] =
		strings.Replace(standJournalDecl, "demo_outbox", "quiet_outbox", 1)
	c := censusOfStand(t, files)

	found := JournalCensusFindings(c)
	if len(found) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (журнал без производителя):\n%s",
			len(found), strings.Join(found, "\n"))
	}
	if !strings.Contains(found[0], "quiet") || !strings.Contains(found[0], "НОЛЬ") {
		t.Errorf("находка не называет ни владельца, ни существа:\n%s", found[0])
	}
	if len(c.Unresolved()) != 0 {
		t.Errorf("инъекция задела соседнее свойство: неустановленных предметов %d, "+
			"ожидался 0", len(c.Unresolved()))
	}
}

// TestJournalCensusFindsDeadRecognizer — форма, ушедшая из дерева, обязана быть
// НАЗВАНА, а не молча дать ноль.
//
// Это второе направление истечения: перепись, чей распознаватель перестал
// находить свой предмет, печатает ноль по форме — и ноль этот означает уже «не
// искали». Отличить одно от другого читатель не может ничем, поэтому гейт
// обязан сказать это сам.
func TestJournalCensusFindsDeadRecognizer(t *testing.T) {
	files := journalStandFiles()
	// Форма триггера уходит из дерева целиком: миграций не остаётся.
	delete(files, "services/demo/internal/migrations/0001_initial.sql")
	files["services/demo/internal/migrations/0001_initial.sql"] = "-- +goose Up\nSELECT 1;\n"
	c := censusOfStand(t, files)

	found := JournalCensusFindings(c)
	if len(found) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (умерший распознаватель формы триггера):\n%s",
			len(found), strings.Join(found, "\n"))
	}
	if !strings.Contains(found[0], string(JournalFormTrigger)) {
		t.Errorf("находка не называет умершую форму:\n%s", found[0])
	}
}

// TestJournalCensusRefusesAnEmptyWalk — предпосылка проверяется САМА.
//
// Пустой обход даёт ноль находок так же, как исправное дерево. Различие обязано
// быть названо, иначе «ноль находок» неотличимо от «ноль прочитанного».
func TestJournalCensusRefusesAnEmptyWalk(t *testing.T) {
	c := censusOfStand(t, map[string]string{"README.md": "нечего осматривать\n"})
	fails := JournalCensusPremiseFailures(c)
	if len(fails) != 2 {
		t.Fatalf("причин беспредметности %d, ожидалось 2 (пустой обход и ноль владельцев):\n%s",
			len(fails), strings.Join(fails, "\n"))
	}
	if got := len(JournalCensusFindings(c)); got != 0 {
		t.Errorf("на пустом дереве найдено %d нарушений — перепись обвиняет там, где ничего "+
			"не прочитала. Свойство принадлежит ВЕРДИКТУ, а не порядку его печати: "+
			"вызывающий, забывший спросить предпосылку, обязан получить пустой список, "+
			"а не шесть обвинений в смерти распознавателей", got)
	}
}
