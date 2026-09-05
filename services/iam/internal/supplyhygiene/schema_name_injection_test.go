// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// schema_name_injection_test.go — доказательство того, что проверка имени схемы
// СПОСОБНА упасть, и того, что она молчит на законных близнецах.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СИНТЕТИЧЕСКИЙ КОРЕНЬ, А НЕ ПРАВКА ДЕРЕВА
//
// Проверка читает дерево службы, которое читают и соседние сессии. Внести в
// него дефект ради доказательства значило бы править общее состояние. Поэтому
// разбор вынесен в чистую функцию над ПРОИЗВОЛЬНЫМ корнем, а сюда подаётся
// корень, собранный в каталоге прогона.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАЖДАЯ ИНЪЕКЦИЯ МЕНЯЕТ РОВНО ОДИН ФАКТ ПРОТИВ КОНТРОЛЯ
//
// Контроль стоит первым и обязан МОЛЧАТЬ. Дальше по одной оси меняется ровно
// один факт: иначе красное могло бы прийти от соседа, а проверка осталась бы
// вакуумной, не показав этого ничем.
//
// Три оси инъекции соответствуют трём исходам распознавателя, и две из них —
// «молчать»: сосед по приставке и имя базы законны, и проверка, роняющая
// прогон на них, ловила бы приставку, а не имя схемы.
package supplyhygiene

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
	"github.com/stretchr/testify/require"
)

// schemaRootWith собирает корень из ОДНОГО файла с заданным содержимым.
// Единственный файл — намеренно: перепись тогда прямо называет, что прочитано
// ровно то, что подано.
func schemaRootWith(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "sample")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.sql"), []byte(body), 0o600))
	return root
}

// soundSchemaBody — законный близнец: каноническое имя схемы квалификатором и
// значением `search_path`. Проверка обязана молчать.
const soundSchemaBody = `SET search_path TO kaname, public;
SELECT id FROM kaname.users WHERE id = $1;
`

// ── Контроль: годный корень молчит ──────────────────────────────────────────

func TestSchemaInjectionControl_CanonicalRootIsSilent(t *testing.T) {
	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, soundSchemaBody)))
	require.NoError(t, err)
	require.Empty(t, findings, "годный корень объявлен нарушением: проверка ловит форму, а не существо")
	require.Equal(t, 1, census.filesRead, "контроль беспредметен: файлов не прочитано")
	require.NotZero(t, census.canonicalHits, "контроль беспредметен: канонического имени не распознано")
	require.Zero(t, census.retiredHits)
}

// ── Ось 1: отставленное имя схемы — НАХОДКА, и она называет координату ──────

func TestSchemaInjection_RetiredQualifierIsFound(t *testing.T) {
	body := soundSchemaBody + "SELECT id FROM kacho_iam.roles WHERE id = $1;\n"

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Len(t, findings, 1, "отставленное имя схемы не найдено — проверка не способна упасть")
	require.Equal(t, 3, findings[0].line, "находка не называет строку")
	require.Contains(t, findings[0].file, "sample.sql", "находка не называет файл")
	require.Equal(t, 1, census.retiredHits)
}

// Значение `search_path` — тот же предмет, записанный иначе. Форма, о которой
// распознаватель не знает, даёт не красное и не зелёное, а молчание.
func TestSchemaInjection_RetiredSearchPathValueIsFound(t *testing.T) {
	body := soundSchemaBody + "SET search_path TO kacho_iam, public;\n"

	_, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Len(t, findings, 1, "отставленное имя в значении search_path пропущено")
}

// Запрос к системному каталогу называет схему строковым литералом — третья
// законная форма записи ТОГО ЖЕ предмета.
func TestSchemaInjection_RetiredNameInACatalogQueryIsFound(t *testing.T) {
	body := soundSchemaBody + "SELECT 1 FROM information_schema.tables WHERE table_schema = 'kacho_iam';\n"

	_, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Len(t, findings, 1, "отставленное имя строковым литералом пропущено")
}

// ── Ось 2: сосед по приставке МОЛЧИТ и считается отдельно ───────────────────

func TestSchemaInjection_NeighbourNamespaceStaysSilent(t *testing.T) {
	body := soundSchemaBody +
		"PERFORM pg_notify('kacho_iam_subjects', NEW.id::text);\n" +
		"-- метрика kacho_iam_identities_total, тип провайдера kacho_iam_account\n"

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Empty(t, findings,
		"сосед по приставке объявлен именем схемы: проверка судит приставку, а не предмет")
	require.Equal(t, 3, census.skippedNeighbour,
		"пропущенные соседи не сосчитаны — «пропущено 0» стало бы неотличимо от «полоса не рассматривалась»")
}

// ── Ось 3: имя БАЗЫ в строке подключения МОЛЧИТ и считается отдельно ────────

func TestSchemaInjection_DatabaseNameStaysSilent(t *testing.T) {
	body := soundSchemaBody + "-- postgres://u:p@pg-iam:5432/kacho_iam?sslmode=require\n"

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Empty(t, findings, "имя базы объявлено именем схемы: база и схема — разные объекты")
	require.Equal(t, 1, census.skippedDatabase, "пропущенные имена базы не сосчитаны")
}

// ── Ось 4: положительный контроль не выполняется частью слова ───────────────

func TestSchemaInjection_CanonicalInsideALongerWordDoesNotSatisfyTheControl(t *testing.T) {
	body := "-- kanamespace не есть имя схемы\nSELECT 1;\n"

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Empty(t, findings)
	require.Zero(t, census.canonicalHits,
		"каноническое имя засчитано внутри более длинного слова — положительный контроль "+
			"выполнялся бы чем угодно, и отрицание стало бы вакуумным")
}

// ── Ось 5: пустой обход отличим от нуля находок ─────────────────────────────

func TestSchemaInjection_EmptyWalkIsDistinguishableFromZeroFindings(t *testing.T) {
	_, _, err := scanSchemaName(syntheticCorpus(t, t.TempDir()))
	require.Error(t, err,
		"пустой обход прошёл успехом: «ноль находок» стало бы неотличимо от «ноль прочитанного»")
	require.Contains(t, err.Error(), "обход пуст")
}

// ── Ось 6: одобренная приёмка МОЛЧИТ и считается отдельно ───────────────────
//
// Инъекция меняет ОДИН факт против своего близнеца — КАТАЛОГ, в котором лежит
// файл; текст у обоих побайтово один. Без такой пары «приёмки чисты» было бы
// неотличимо от «приёмки не рассматривались».

// schemaRootWithAcceptance кладёт тот же текст в каталог одобренных приёмок.
func schemaRootWithAcceptance(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()

	// Файл вне приёмок — иначе обход пуст и вердикт беспредметен.
	sample := filepath.Join(root, "internal", "sample")
	require.NoError(t, os.MkdirAll(sample, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sample, "sample.sql"), []byte(soundSchemaBody), 0o600))

	acc := filepath.Join(root, filepath.FromSlash(approvedAcceptanceDir))
	require.NoError(t, os.MkdirAll(acc, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(acc, "approved.md"), []byte(body), 0o600))
	return root
}

// retiredInsideAProse — текст, которым отличаются близнецы ниже. Он несёт
// предикат, привязанный к прошлому состоянию дерева: правка имени в нём сделала
// бы ложным утверждение, которое было верным.
const retiredInsideAProse = "| замер | **14** | `git grep -c 'FROM kacho_iam.roles'` |\n"

func TestSchemaInjection_RetiredNameInsideAnApprovedAcceptanceStaysSilent(t *testing.T) {
	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWithAcceptance(t, retiredInsideAProse)))
	require.NoError(t, err)
	require.Empty(t, findings,
		"запись замера в одобренной приёмке объявлена нарушением: её правка отозвала бы вердикт документа")
	require.Equal(t, 1, census.filesApproved, "пропущенные приёмки не сосчитаны")
	require.Equal(t, 1, census.skippedApproved,
		"вхождения в приёмках не сосчитаны — «в приёмках 0» стало бы неотличимо от «приёмки не рассматривались»")
}

// Близнец: ТОТ ЖЕ текст вне каталога приёмок — находка. Различие ровно одно:
// каталог. Без этой пары пропуск приёмок был бы послаблением без доказательства.
func TestSchemaInjection_TheSameProseOutsideAcceptanceIsFound(t *testing.T) {
	body := soundSchemaBody + retiredInsideAProse

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"тот же текст вне приёмок пропущен: пропуск ловит форму записи, а не каталог")
	require.Zero(t, census.filesApproved)
}

// ── Ось 7: ВТОРАЯ форма имени базы — значение ключа профиля ─────────────────
//
// Первая форма (последний сегмент строки подключения) проверена осью 3. Здесь
// вторая: значение ключа профиля развёртывания. Пара обязательна, потому что
// распознаватель, знающий одну форму из двух, на второй даёт находку там, где
// нарушения нет, — и снимают его как непонятный.

func TestSchemaInjection_DatabaseNameAsAProfileKeyStaysSilent(t *testing.T) {
	body := soundSchemaBody + "db:\n  host: pg-iam\n  name: kacho_iam\n"

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Empty(t, findings, "значение ключа профиля объявлено именем схемы: это имя БАЗЫ")
	require.Equal(t, 1, census.skippedDatabase)
}

// Близнец: то же слово в ТОЙ ЖЕ строке, но не значением ключа — находка.
// Различие ровно одно: слева от имени стоит не только ключ с отступом.
func TestSchemaInjection_TheSameWordNotAsAKeyValueIsFound(t *testing.T) {
	body := soundSchemaBody + "  name: тут раньше стояла схема kacho_iam\n"

	_, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"проза после ключа принята за значение ключа: правило судит слово, а не позицию")
}

// ── Ось 8: перечень «предмет = это переименование» УЗОК и НЕПУСТ ────────────
//
// Освобождение без доказательства есть послабление. Здесь доказываются обе его
// стороны: перечень непуст (иначе полоса не рассматривалась вовсе) и он не
// покрывает файл, лежащий рядом с тем же текстом.

func TestSchemaInjection_TheCheckDoesNotJudgeItsOwnDeclaration(t *testing.T) {
	require.NotEmpty(t, checkOwnFiles,
		"перечень файлов самой проверки пуст: полоса объявлена и не рассматривается")

	// Каждый названный файл обязан СУЩЕСТВОВАТЬ в дереве службы: запись,
	// которой нечего освобождать, — находка, а не запас на будущее.
	tree, err := treecorpus.NewTree(serviceRoot)
	require.NoError(t, err)
	for rel := range checkOwnFiles {
		require.Truef(t, tree.HasFile(rel),
			"перечень освобождает %q, которого в дереве нет: освобождению нечего освобождать", rel)
	}
}

// Близнец: файл ВНЕ перечня, объявляющий ту же константу, судится как любой
// другой. Различие ровно одно — путь.
func TestSchemaInjection_TheSameDeclarationOutsideTheListIsFound(t *testing.T) {
	body := soundSchemaBody + "const retiredSchema = \"kacho_iam\"\n"

	census, findings, err := scanSchemaName(syntheticCorpus(t, schemaRootWith(t, body)))
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"освобождение раздано по ФОРМЕ объявления, а не по перечню: тогда всякий прод-код, "+
			"объявивший ту же строку, вышел бы из-под наблюдения")
	require.Zero(t, census.filesOwn)
}
