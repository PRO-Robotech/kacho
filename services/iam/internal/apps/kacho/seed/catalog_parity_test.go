// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package seed_test

// catalog_parity_test.go — СВОЯ проба стража старта (Н7 приёмки
// `module-withdrawal-is-described.md`, §3.4).
//
// # Зачем она, если страж уже «проверен»
//
// До неё способность `AssertCatalogParity` ОТКАЗАТЬ доказывалась ПОБОЧНО —
// одним утверждением внутри чужой интеграционной пробы снимка
// (`repo/kacho/pg/catalog_snapshot_integration_test.go:232`). Замер, ради
// которого проба заведена:
//
//	git grep -ln 'AssertCatalogParity' -- 'services/iam/**/*_test.go'   → 1 файл
//
// и этот единственный файл требует Postgres. Под `-short` его TestMain не
// поднимает контейнера и пропускает всё (`internal/pgtest/pgtest.go:334`),
// поэтому в быстрой полосе страж, решающий, ПОДНИМЕТСЯ ЛИ iam, не исполнялся
// ни разу. Проверка, чья способность упасть не показана, от отсутствующей
// отличается только тем, что занимает слот.
//
// # Почему без базы
//
// Страж читает живое множество через ПОРТ (`catalog.RowSource`), а не своим
// запросом. Значит его предмет — не SQL, а РЕШЕНИЕ: что он считает
// расхождением, что пустотой, и называет ли он строку, из-за которой отказал.
// Ровно это здесь и подаётся — множествами в памяти. База сюда не нужна, и
// проба остаётся в короткой полосе.
//
// # Форма утверждений: инъекция ОДНОЙ строки, всегда в паре
//
// Каждое отрицание («страж отказал») стоит рядом с положительным контролем
// («на полном множестве он молчит»). Без пары «отказал» неотличимо от
// «отказывает всегда» — то есть от стража, снятого с предмета.
//
// Вход обеих сторон производит ОДИН и тот же `seed.LiteralRows()`, и это не
// леность: подай сторонам два разных производителя — сравнение начнёт мерить
// расхождение ПРОИЗВОДИТЕЛЕЙ, а не решение стража. Здесь различие ровно одно и
// оно внесено пробой.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/seed"
	"github.com/PRO-Robotech/kacho/services/iam/internal/catalog"
)

// fixedRows — подставной порт живого каталога: отдаёт то, что ему дали.
//
// Он НЕ снисходительнее настоящего: настоящий возвращает `(Rows, error)`, и
// обе ветви здесь представимы. Порт, у которого отказ невыразим, скрыл бы
// ровно ту полосу, ради которой страж fail-closed.
type fixedRows struct {
	rows catalog.Rows
	err  error
}

func (f fixedRows) ReadLiveCatalog(context.Context) (catalog.Rows, error) {
	return f.rows, f.err
}

// copyRows — глубокая копия: перечни литерала общие на весь пакет, и правка
// среза на месте протекла бы в соседнюю пробу через общий массив.
func copyRows(in catalog.Rows) catalog.Rows {
	out := catalog.Rows{
		Modules:   append([]string(nil), in.Modules...),
		Resources: append([]catalog.ResourceRow(nil), in.Resources...),
		Verbs:     append([]catalog.VerbRow(nil), in.Verbs...),
	}
	return out
}

// requireMaterial — ФИКСТУРА САМА ПО СЕБЕ ПРОВЕРЯЕТСЯ.
//
// «Снял одну строку» на пустом перечне не снимает ничего, и тогда каждое
// утверждение ниже зеленело бы вакуумно. Поэтому объём материала утверждается
// до всякой инъекции, а не предполагается.
func requireMaterial(t *testing.T, rows catalog.Rows) {
	t.Helper()
	require.NotEmpty(t, rows.Modules, "литерал не назвал ни одного модуля — снимать нечего")
	require.NotEmpty(t, rows.Resources, "литерал не назвал ни одного ресурса — снимать нечего")
	require.NotEmpty(t, rows.Verbs, "литерал не назвал ни одного глагола — снимать нечего")
}

// TestCatalogParityIsSilentOnFullAgreement — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ко всему
// файлу. Живое множество равно литералу — страж молчит и служба поднимается.
//
// Без него каждое «страж отказал» ниже осталось бы верным и у стража, который
// отказывает при любом входе.
func TestCatalogParityIsSilentOnFullAgreement(t *testing.T) {
	rows := seed.LiteralRows()
	requireMaterial(t, rows)

	census, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: rows})
	require.NoError(t, err, "страж отказал на множестве, равном литералу — тогда его отказ ниже ничего не различает")
	require.False(t, census.Diverged(), "расхождение объявлено там, где стороны совпадают")
	require.Empty(t, census.MissingRows)
	require.Empty(t, census.ExtraRows)

	// Перепись обязана быть непустой и на молчаливом исходе: «ноль расхождений»
	// обязано быть отличимо от «ноль прочитанного».
	require.Equal(t, census.LiteralModules, census.RowModules, "перепись сторон разошлась при совпавших множествах")
	require.Equal(t, census.LiteralResources, census.RowResources)
	require.Equal(t, census.LiteralVerbs, census.RowVerbs)
	require.Positive(t, census.RowResources, "прочитано ноль строк, а страж промолчал — это «ноль прочитанного», а не «ноль расхождений»")
}

// TestCatalogParityRefusesAndNamesTheMissingRow — Н7 ДОСЛОВНО: множество без
// одной строки → отказ И текст, называющий строку.
//
// Три вида строк проверяются порознь, потому что `diffInto` зовётся для них
// тремя разными вызовами: молчание на одном виде не выводится из отказа на
// другом.
func TestCatalogParityRefusesAndNamesTheMissingRow(t *testing.T) {
	literal := seed.LiteralRows()
	requireMaterial(t, literal)

	cases := []struct {
		name string
		// drop снимает ровно одну строку и возвращает её ожидаемое имя в тексте.
		drop func(catalog.Rows) (catalog.Rows, string)
	}{
		{
			name: "модуль",
			drop: func(r catalog.Rows) (catalog.Rows, string) {
				gone := r.Modules[0]
				r.Modules = r.Modules[1:]
				return r, "модуль " + gone
			},
		},
		{
			name: "ресурс",
			drop: func(r catalog.Rows) (catalog.Rows, string) {
				gone := r.Resources[0]
				r.Resources = r.Resources[1:]
				return r, "ресурс " + gone.Module + "." + gone.Resource
			},
		},
		{
			name: "глагол",
			drop: func(r catalog.Rows) (catalog.Rows, string) {
				gone := r.Verbs[0]
				r.Verbs = r.Verbs[1:]
				kind := " (ярусный)"
				if gone.PerObject {
					kind = " (пообъектный)"
				}
				return r, "глагол " + gone.Module + "." + gone.Resource + "." + gone.Verb + kind
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			live, want := tc.drop(copyRows(literal))

			census, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: live})

			require.Error(t, err,
				"снятая строка %q не отказала в старте — отзыв внутри работающего процесса "+
					"перестал быть виден следующему старту", want)
			require.Contains(t, err.Error(), want,
				"отказ не НАЗЫВАЕТ снятую строку: оператор видит «iam не поднимается» и не видит, что снято")
			require.Equal(t, []string{want}, census.MissingRows,
				"перепись назвала не ту строку либо не одну — инъекция снимала ровно одну")
			require.Empty(t, census.ExtraRows,
				"снятие строки объявлено ещё и лишней строкой — расхождение посчитано дважды")
			require.False(t, census.Empty(),
				"неполное множество названо ПУСТЫМ: тогда отказ говорил бы про непринятые миграции, "+
					"а предмет другой — расхождение чинится посевом")
		})
	}
}

// TestCatalogParityFindsTheRowBeyondTheLiteral — вторая сторона `diffInto`.
//
// Одностороннее сравнение (включение литерала в живое) молчало бы на строке,
// которой в литерале нет: она даёт правилу референт, по которому правило
// резолвится, а проекция — нет.
func TestCatalogParityFindsTheRowBeyondTheLiteral(t *testing.T) {
	live := copyRows(seed.LiteralRows())
	requireMaterial(t, live)
	live.Resources = append(live.Resources, catalog.ResourceRow{Module: "vpc", Resource: "kacho-nonexistent-probe"})

	census, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: live})

	require.Error(t, err, "строка сверх литерала не отказала в старте")
	require.Contains(t, err.Error(), "ресурс vpc.kacho-nonexistent-probe",
		"отказ не назвал лишнюю строку")
	require.Equal(t, []string{"ресурс vpc.kacho-nonexistent-probe"}, census.ExtraRows)
	require.Empty(t, census.MissingRows, "лишняя строка объявлена ещё и недостающей")
}

// TestCatalogParitySeparatesTheTwoVerbDictionaries — признак словаря входит в
// ключ сверки (задача #1863).
//
// Строка, посеянная с НЕВЕРНЫМ признаком, существует в обоих множествах и по
// тройке (модуль, ресурс, глагол) прошла бы сверку молча — а разошлись бы ровно
// две величины, ради которых словари и разделены: что ключ пропускает и что
// материализуется. Поэтому подмена признака обязана быть видна с ОБЕИХ сторон.
func TestCatalogParitySeparatesTheTwoVerbDictionaries(t *testing.T) {
	literal := seed.LiteralRows()
	requireMaterial(t, literal)

	live := copyRows(literal)
	flipped := live.Verbs[0]
	flipped.PerObject = !flipped.PerObject
	live.Verbs[0] = flipped

	census, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: live})

	require.Error(t, err,
		"подмена признака словаря прошла молча — тогда ярусная строка попадала бы в набор "+
			"пообъектных, и снятое отношение вернулось бы целиком")
	require.Len(t, census.MissingRows, 1, "форма из литерала не названа недостающей")
	require.Len(t, census.ExtraRows, 1, "форма из живых строк не названа лишней")
	require.Contains(t, census.MissingRows[0], literal.Verbs[0].Verb)
	require.Contains(t, census.ExtraRows[0], literal.Verbs[0].Verb)
	require.NotEqual(t, census.MissingRows[0], census.ExtraRows[0],
		"обе стороны названы одинаково — значит признак словаря в ключ не вошёл")
}

// TestCatalogParityCallsEmptinessByItsOwnName — IAM-MW-1-22. Пустой каталог
// отличается от расхождения, и отказ обязан называть ИМЕННО пустоту.
//
// Предметы разные: расхождение чинится повторным посевом, пустой каталог —
// применением миграций. Схлопни их в один — и оператор пойдёт сеять там, где
// не применены миграции.
func TestCatalogParityCallsEmptinessByItsOwnName(t *testing.T) {
	census, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: catalog.Rows{}})

	require.Error(t, err, "пустой каталог не отказал в старте — он отверг бы ВСЕ правила разом")
	require.True(t, census.Empty(), "пустое множество не названо пустым")
	require.Contains(t, err.Error(), "каталог модуля пуст",
		"отказ на пустом каталоге назвался расхождением — предмет подменён")
	require.NotContains(t, err.Error(), "разошлись",
		"пустота подана как расхождение: чинить будут посевом вместо миграций")
	// Перепись остаётся ЧИТАЕМОЙ и на пустоте: сторона литерала прочитана,
	// сторона строк — нет, и это видно числами.
	require.Positive(t, census.LiteralResources, "перепись литерала пуста — «ноль находок» неотличимо от «ноль прочитанного»")
	require.Zero(t, census.RowResources)
}

// TestCatalogParityFailsClosedWhenTheReadFails — недоступное чтение НЕ есть
// «расхождений нет».
//
// Неполученный ответ — не «да». Мягкий проход здесь дал бы контроль, который
// не откажет ни разу за свою жизнь ровно тогда, когда база отвечает плохо.
func TestCatalogParityFailsClosedWhenTheReadFails(t *testing.T) {
	boom := errors.New("соединение закрыто")

	census, err := seed.AssertCatalogParity(context.Background(), fixedRows{err: boom})

	require.Error(t, err, "отказ чтения каталога прошёл как согласие сторон")
	require.ErrorIs(t, err, boom, "исходная причина потеряна — оператор не узнает, что читать не удалось")
	require.Empty(t, census.Live, "живое множество наполнено при неудавшемся чтении")
	// Сторона литерала прочитана и на этой полосе: перепись отвечает на вопрос
	// «сколько прочитано», а не только «сколько разошлось».
	require.Positive(t, census.LiteralResources)
}

// TestCatalogParityHandsTheLiveSetToItsCaller — страж отдаёт ПРОЧИТАННОЕ им
// множество наружу.
//
// Снимок каталога наполняется тем же чтением, а не своим: второй запрос об
// одном предмете — два места, и разойдутся они молча.
func TestCatalogParityHandsTheLiveSetToItsCaller(t *testing.T) {
	rows := seed.LiteralRows()
	requireMaterial(t, rows)

	census, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: rows})
	require.NoError(t, err)

	require.Equal(t, len(rows.Modules), len(census.Live.Modules),
		"наружу отдано не то множество, которое прочитано, — снимок наполнялся бы вторым чтением")
	require.Equal(t, len(rows.Resources), len(census.Live.Resources))
	require.Equal(t, len(rows.Verbs), len(census.Live.Verbs))
}

// TestCatalogParityCensusPrintsBothSidesOnRefusal — перепись обеих сторон
// присутствует в ТЕКСТЕ отказа, а не только в структуре.
//
// Оператор читает журнал, а не поля структуры: отказ без чисел не отличает
// «прочитано ноль» от «разошлась одна строка».
func TestCatalogParityCensusPrintsBothSidesOnRefusal(t *testing.T) {
	live := copyRows(seed.LiteralRows())
	requireMaterial(t, live)
	live.Resources = live.Resources[1:]

	_, err := seed.AssertCatalogParity(context.Background(), fixedRows{rows: live})
	require.Error(t, err)

	msg := err.Error()
	require.True(t, strings.Contains(msg, "Прочитано из литерала"),
		"в отказе нет переписи прочитанного: %s", msg)
	require.Contains(t, msg, "строками", "в отказе не названа сторона живых строк")
}
