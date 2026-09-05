// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationsqltext_test.go — ИСПОЛНЯЕМАЯ часть текста миграции: то, что Postgres
// действительно исполнит, без комментариев.
//
// # Предмет
//
// Разборы миграций в этом пакете (инвентарь словарей
// outboxeventdictionary_test.go, инвентарь частичных индексов
// outboxpendingindexset_test.go) читали Up-секцию ЦЕЛИКОМ, вместе с
// комментариями. Комментарий при этом сплошь и рядом содержит ровно ту
// конструкцию, которую ищет разбор. Число ниже держится ПРОБОЙ, а не памятью
// (TestMigrationCommentsCarryParseableConstructs печатает его на каждом прогоне),
// и единица счёта названа, потому что «областей» и «вхождений» — разные величины
// об одном предмете: в Up-секциях 194 миграций 1453 связные комментарные области,
// в них 19 вхождений `CHECK (`, 33 — `CREATE INDEX`, 5 — `DROP INDEX`/`DROP
// CONSTRAINT`. Пример, за который никто не отвечает нарочно: комментарий,
// объясняющий, ПОЧЕМУ ограничение выглядит так, а не иначе, цитирует его текст —
// и цитата становится для разбора вторым, живым ограничением.
//
// Это ровно тот класс, который `testing.md` называет прямо: гейт обязан читать
// исполняемую часть, а не текст, и различать код, строковый литерал и
// комментарий. Живого экземпляра на 194 миграциях сегодня нет (проверено:
// инвентари до и после вырезания комментариев идентичны — проба ниже), но зона
// латентная, а не отсутствующая: первая же миграция, показавшая пример
// ограничения или индекса в комментарии, сдвинула бы инвентарь МОЛЧА.
//
// # Как это сделано и почему именно так
//
// Комментарии не вырезаются, а ЗАБЕЛИВАЮТСЯ пробелами той же длины, переводы
// строк сохраняются. Причина не в аккуратности: оба инвентаря применяют
// операторы одной миграции в порядке их СМЕЩЕНИЯ в тексте — снятие и постановка
// одноимённого ограничения/индекса стоят в одном файле, и порядок между ними
// решает исход. Вырезание сдвинуло бы смещения, а забеливание оставляет их
// побайтово теми же, поэтому текстовый порядок остаётся тем же самым.
//
// # Объявленные слепые зоны разбора текста
//
//   - Одинарная кавычка внутри комментария КАВЫЧКОЙ НЕ СЧИТАЕТСЯ (приоритет у
//     комментария), и это не мелочь: в трёх миграциях дерева тело `DO $$…$$`
//     содержит комментарий с апострофом внутри слова («payload'а»). Обратный
//     приоритет открыл бы на нём строку до конца файла и забелил бы половину
//     миграции.
//   - Доллар-кавычки (`$$…$$`) ПРОЗРАЧНЫ: внутри тела `DO`-блока комментарии
//     забеливаются так же, как снаружи. Тело такого блока — PL/pgSQL, где `--`
//     тоже комментарий, поэтому это верно по существу; ценой служит гипотетическое
//     тело, где `--` является данными.
//   - Комментарий внутри ДВОЙНЫХ кавычек (кавычный идентификатор) забеливается
//     как комментарий. Идентификатор с последовательностью `--` внутри — в дереве
//     таких нет, предикат ниже это утверждает.
package repohygiene

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// sqlCommentText — ТОЛЬКО комментарии, каждый отдельной записью.
//
// Отдельная функция, а не «разница сырого и забелённого»: разница по байтам
// СЪЕДАЕТ пробелы (пробел внутри комментария забеливание не меняет, поэтому он
// читается как совпадение и выпадает). Предикат, построенный на такой разнице,
// давал `CHECK(` = 19 и `CREATE INDEX` = 0 на одном и том же дереве — ноль не
// потому, что примеров нет, а потому, что `CREATE UNIQUE INDEX` склеивалось в
// `CREATEUNIQUEINDEX` и ни одно `\s+` не совпадало. Ложный ноль в предикате,
// измеряющем предмет защиты, — тот же класс, что и защита без предмета.
func sqlCommentText(s string) []string {
	var out []string
	scanSQLComments(s, func(lo, hi int) { out = append(out, s[lo:hi]) })
	return out
}

// Test_SqlBlankComments_KeepsCodeAndOffsets — забеливание проверено В ОБЕ
// СТОРОНЫ: конструкция в комментарии исчезает, та же конструкция в коде остаётся,
// а длина текста не меняется ни в одном случае.
//
// Отрицательная половина без положительной зеленела бы на функции, забеливающей
// ВСЁ; положительная без отрицательной — на функции, не делающей ничего.
func Test_SqlBlankComments_KeepsCodeAndOffsets(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		keep    []string // обязано остаться в исполняемой части
		removed []string // обязано исчезнуть
	}{
		{
			name: "ограничение в строчном комментарии исчезает, в коде остаётся",
			in: "-- CHECK (event_type = ANY (ARRAY['ghost'::text]))\n" +
				"ALTER TABLE t ADD CONSTRAINT c CHECK (event_type = ANY (ARRAY['real'::text]));\n",
			keep:    []string{"'real'"},
			removed: []string{"'ghost'"},
		},
		{
			name: "индекс в комментарии исчезает, снятие в коде остаётся",
			in: "-- CREATE INDEX ghost_idx ON t (a) WHERE sent_at IS NULL;\n" +
				"DROP INDEX real_idx;\n",
			keep:    []string{"DROP INDEX real_idx"},
			removed: []string{"ghost_idx"},
		},
		{
			name:    "блочный комментарий, в том числе вложенный",
			in:      "/* CREATE INDEX a /* вложенный */ ON t (x) */ DROP INDEX real_idx;",
			keep:    []string{"DROP INDEX real_idx"},
			removed: []string{"CREATE INDEX a", "вложенный"},
		},
		{
			name:    "двойной дефис ВНУТРИ строки комментария не начинает",
			in:      "INSERT INTO t VALUES ('a--b'); DROP INDEX real_idx;",
			keep:    []string{"'a--b'", "DROP INDEX real_idx"},
			removed: nil,
		},
		{
			name: "апостроф ВНУТРИ комментария строки не открывает",
			in: "-- payload'а тут апостроф\n" +
				"CREATE INDEX real_idx ON t (a) WHERE sent_at IS NULL;\n",
			keep:    []string{"CREATE INDEX real_idx"},
			removed: []string{"payload"},
		},
		{
			name:    "экранированная кавычка внутри строки не закрывает её",
			in:      "INSERT INTO t VALUES ('it''s -- not a comment'); DROP INDEX real_idx;",
			keep:    []string{"it''s -- not a comment", "DROP INDEX real_idx"},
			removed: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sqlBlankComments(tc.in)
			if len(got) != len(tc.in) {
				t.Fatalf("длина изменилась: %d → %d. Смещения операторов — единственный "+
					"источник порядка их применения; сдвинув их, разбор перестанет "+
					"сходиться с Postgres на перестройке индекса в одной миграции",
					len(tc.in), len(got))
			}
			if strings.Count(got, "\n") != strings.Count(tc.in, "\n") {
				t.Fatalf("число переводов строки изменилось: %d → %d",
					strings.Count(tc.in, "\n"), strings.Count(got, "\n"))
			}
			for _, want := range tc.keep {
				if !strings.Contains(got, want) {
					t.Errorf("исполняемая часть потеряла %q — забеливание съело код:\n%s", want, got)
				}
			}
			for _, gone := range tc.removed {
				if strings.Contains(got, gone) {
					t.Errorf("комментарий уцелел: %q всё ещё читается разбором как код:\n%s", gone, got)
				}
			}
		})
	}
}

// syntheticCommentedTree строит дерево из одной миграции, где ТРИ конструкции —
// расширение словаря, создание индекса и снятие настоящего индекса — стоят под
// префиксом prefix. При prefix="-- " они комментарий, при prefix="" — код.
// Текст в обоих случаях один и тот же, поэтому расхождение инвентарей может
// произойти ТОЛЬКО от забеливания.
func syntheticCommentedTree(t *testing.T, prefix string) string {
	t.Helper()
	root := t.TempDir()
	writeSyntheticMigration(t, root, "alpha", "0001_queue.sql", `
-- +goose Up
CREATE TABLE kacho_alpha.q (
    id            bigserial PRIMARY KEY,
    event_type    text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    sent_at       timestamptz,
    CONSTRAINT q_event_type_check CHECK (event_type = ANY (ARRAY['real.one'::text, 'real.two'::text]))
);
CREATE INDEX q_claim_order_idx ON kacho_alpha.q (attempt_count, id) WHERE sent_at IS NULL;
`+prefix+`ALTER TABLE kacho_alpha.q ADD CONSTRAINT q_ghost_check CHECK (event_type = ANY (ARRAY['real.one'::text]));
`+prefix+`CREATE INDEX q_ghost_idx ON kacho_alpha.q (event_type, id) WHERE sent_at IS NULL;
`+prefix+`DROP INDEX q_claim_order_idx;
-- +goose Down
DROP TABLE kacho_alpha.q;
`)
	return root
}

// TestInventoriesReadExecutablePartNotComments — ОБА инвентаря судят по коду, а
// не по тексту, и это доказано ОДНИМ И ТЕМ ЖЕ текстом в двух ролях.
//
// Половина «комментарий» без половины «код» зеленела бы на разборе, который
// вообще перестал видеть свои конструкции; половина «код» без половины
// «комментарий» — на разборе, который читает текст целиком, как читал раньше.
func TestInventoriesReadExecutablePartNotComments(t *testing.T) {
	asComment := syntheticCommentedTree(t, "-- ")
	asCode := syntheticCommentedTree(t, "")

	dictComment := enumDictionaryInventory(t, asComment, syntheticMigrationSQL).dict["alpha:q"]["event_type"]
	dictCode := enumDictionaryInventory(t, asCode, syntheticMigrationSQL).dict["alpha:q"]["event_type"]
	idxComment, filesC := pendingIndexInventory(t, asComment, syntheticMigrationSQL)
	idxCode, filesK := pendingIndexInventory(t, asCode, syntheticMigrationSQL)
	if filesC == 0 || filesK == 0 {
		t.Fatalf("синтетическое дерево не прочитано (%d / %d миграций) — проба ничего "+
			"не доказывает", filesC, filesK)
	}

	// КОММЕНТАРИЙ: словарь остаётся полным, настоящий индекс жив, призрака нет.
	if got, want := strings.Join(dictComment, "|"), "real.one|real.two"; got != want {
		t.Errorf("словарь прочитан как [%s], ожидалось [%s]: ограничение, ПРОЦИТИРОВАННОЕ "+
			"в комментарии, зачлось живым и сузило набор пересечением — разбор читает "+
			"текст, а не исполняемую часть", got, want)
	}
	if _, alive := idxComment["alpha:q"]["q_claim_order_idx"]; !alive {
		t.Errorf("настоящий индекс q_claim_order_idx исчез: снятие, ЗАКОММЕНТИРОВАННОЕ в "+
			"миграции, применилось как настоящее (инвентарь: %v)", idxComment["alpha:q"])
	}
	if _, ghost := idxComment["alpha:q"]["q_ghost_idx"]; ghost {
		t.Errorf("индекс q_ghost_idx, приведённый в комментарии ПРИМЕРОМ, попал в инвентарь "+
			"живым (инвентарь: %v)", idxComment["alpha:q"])
	}

	// КОД: тот же текст без префикса комментария обязан быть виден целиком —
	// иначе забеливание съело бы не комментарий, а разбор.
	if got := strings.Join(dictCode, "|"); got != "real.one" {
		t.Errorf("тот же текст КОДОМ прочитан как [%s], ожидалось [real.one] (пересечение "+
			"двух живых ограничений): положительный контроль провален, и отрицательная "+
			"половина выше зеленеет на любом сломанном разборе", got)
	}
	if _, alive := idxCode["alpha:q"]["q_claim_order_idx"]; alive {
		t.Errorf("тот же DROP КОДОМ не применился (инвентарь: %v) — положительный контроль "+
			"провален", idxCode["alpha:q"])
	}
	if _, ghost := idxCode["alpha:q"]["q_ghost_idx"]; !ghost {
		t.Errorf("тот же CREATE INDEX КОДОМ не увиден (инвентарь: %v) — положительный "+
			"контроль провален", idxCode["alpha:q"])
	}
}

var (
	commentedCheckRe  = regexp.MustCompile(`(?i)CHECK\s*\(`)
	commentedIndexRe  = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX`)
	commentedRemoveRe = regexp.MustCompile(`(?i)DROP\s+(?:INDEX|CONSTRAINT)`)
)

// TestMigrationCommentsCarryParseableConstructs — ПРЕДПОСЫЛКА предыдущей пробы,
// измеренная на настоящем дереве: комментарии миграций действительно содержат
// конструкции, которые разбор ищет.
//
// Без этой пробы забеливание было бы защитой от гипотезы: «мы вырезаем
// комментарии, потому что в них МОГЛО БЫ что-то лежать». Здесь названо число, и
// оно проверяемо. Если однажды оно станет нулём — проба покраснеет и потребует
// либо снять забеливание как беспредметное, либо признать, что предикат больше
// не читает то, что читал.
func TestMigrationCommentsCarryParseableConstructs(t *testing.T) {
	root := repoRoot(t)
	files, err := migrationFiles(root)
	if err != nil {
		t.Fatalf("состав миграций взять неоткуда: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("прочитано ноль миграций — предпосылка сломана, молчание ничего не "+
			"доказывает (корень %s)", root)
	}

	checks, indexes, removals, withComments, areas := 0, 0, 0, 0, 0
	for _, path := range files {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("чтение %s: %v", path, readErr)
		}
		raw := string(body)
		if i := strings.Index(raw, "-- +goose Down"); i >= 0 {
			raw = raw[:i]
		}
		// Считаем по СВЯЗНОЙ комментарной области (соседние строчные комментарии —
		// одна область): конструкция в комментарии сплошь и рядом переносится через
		// строку, и поштучный счёт строк её бы не увидел.
		regions := sqlCommentAreas(raw)
		if len(regions) == 0 {
			continue
		}
		withComments++
		areas += len(regions)
		for _, r := range regions {
			checks += len(commentedCheckRe.FindAllString(r, -1))
			indexes += len(commentedIndexRe.FindAllString(r, -1))
			removals += len(commentedRemoveRe.FindAllString(r, -1))
		}
	}

	t.Logf("прочитано миграций: %d (с комментариями в Up-секции: %d); комментарных "+
		"областей: %d; в них вхождений: CHECK( = %d, CREATE INDEX = %d, "+
		"DROP INDEX/CONSTRAINT = %d",
		len(files), withComments, areas, checks, indexes, removals)

	if checks+indexes+removals == 0 {
		t.Errorf("в комментариях %d миграций не нашлось НИ ОДНОЙ конструкции, которую "+
			"читают инвентари словарей и индексов. Либо предикат перестал их видеть "+
			"(тогда чинить его), либо дерево изменилось настолько, что забеливание "+
			"комментариев больше не имеет предмета (тогда снять его вместе с этой пробой) "+
			"— молчаливо оставлять защиту без предмета нельзя.", len(files))
	}
}

// sqlCommentAreas — СВЯЗНЫЕ комментарные области: соседние строчные комментарии
// склеиваются в одну, блочный — сам по себе. Единица счёта названа здесь, потому
// что «областей» и «вхождений» — разные числа об одном предмете, и путать их
// значит получить величину, верную для чужого предиката.
func sqlCommentAreas(s string) []string {
	var areas []string
	var cur strings.Builder
	prevEnd := -1
	flush := func() {
		if cur.Len() > 0 {
			areas = append(areas, cur.String())
			cur.Reset()
		}
	}
	scanSQLComments(s, func(lo, hi int) {
		// Соседние строки: между концом прошлого комментария и началом этого —
		// только перевод строки и пробелы.
		if prevEnd >= 0 && strings.TrimSpace(s[prevEnd:lo]) == "" && strings.Count(s[prevEnd:lo], "\n") == 1 {
			cur.WriteString("\n")
		} else {
			flush()
		}
		cur.WriteString(s[lo:hi])
		prevEnd = hi
	})
	flush()
	return areas
}

// migrationFiles — все миграции всех сервисов, отсортированные (имя начинается с
// версии, поэтому лексикографический порядок и есть порядок применения).
// migrationSQLCorpus — КАК перечисляются файлы миграций службы.
//
// Полос две, и выбирает их ВЫЗЫВАЮЩИЙ, а не помощник. Один и тот же разбор
// исполняется на настоящем дереве и на синтетическом, собранном самой пробой во
// временном каталоге, — а состав у них берётся из разных мест по существу:
// у репозитория есть индекс, и вердикт обязан быть свойством коммита; у
// временного каталога индекса нет и быть не может.
//
// Собирай помощник состав сам, одна из полос была бы неверной: обход диска на
// настоящем дереве читал бы игнорируемое (рабочие копии агентов, распаковки
// чартов, отчёты прогонов), а запрос к индексу на синтетике отказывал бы.
type migrationSQLCorpus func(dir string) ([]string, error)

// trackedMigrationSQL — состав из ИНДЕКСА git. Полоса НАСТОЯЩЕГО дерева.
//
// Служба без каталога миграций — законный ПУСТОЙ ответ; недоступный git — отказ,
// и он доходит до вызывающего. Сведение обоих в `nil, nil` проглотило бы второе.
func trackedMigrationSQL(dir string) ([]string, error) {
	sqls, err := treecorpus.Glob(filepath.Join(dir, "*.sql"))
	if errors.Is(err, treecorpus.ErrEmptyCorpus) {
		return nil, nil
	}
	return sqls, err
}

// syntheticMigrationSQL — состав с ДИСКА. Полоса дерева, собранного САМОЙ
// пробой во временном каталоге: репозиторием оно не является, спрашивать у него
// индекс нечего, и обход файловой системы здесь законен.
//
// Настоящему дереву эта полоса не передаётся НИКОГДА — там она вернула бы
// вердикт о рабочем каталоге вместо вердикта о коммите.
//
// ПРЕДЕЛ, названный вслух: гейт дерева (`TestTreeWalkersAskTheIndex`) этого не
// удержит. Состав уезжает сюда ЗНАЧЕНИЕМ-функцией, а граф вызовов строится по
// имени — предел объявлен там же, в его собственной шапке. Значит правило
// «синтетике — диск, репозиторию — индекс» здесь держится ИМЕНЕМ и этой
// строкой, а не проверкой. Передав `syntheticMigrationSQL` настоящий корень,
// вернёшь ровно тот дефект, ради которого разделение и заведено, и ни одна
// проверка об этом не скажет.
func syntheticMigrationSQL(dir string) ([]string, error) {
	return filepath.Glob(filepath.Join(dir, "*.sql"))
}

func migrationFiles(root string) ([]string, error) {
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sqls, globErr := treecorpus.Glob(filepath.Join(servicesDir, e.Name(), "internal", "migrations", "*.sql"))
		// Служба без каталога миграций — законный ПУСТОЙ ответ; недоступный git —
		// отказ, и он обязан дойти до вызывающего, а не стать пустым перечнем.
		if errors.Is(globErr, treecorpus.ErrEmptyCorpus) {
			continue
		}
		if globErr != nil {
			return nil, globErr
		}
		sort.Strings(sqls)
		out = append(out, sqls...)
	}
	return out, nil
}
