// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_database_named_for_its_product_injection_test.go — доказательство того,
// что соседняя проверка СПОСОБНА упасть, и падает ровно на своём предмете.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ ДОКАЗЫВАЕТСЯ
//
// Инъекция кормит ТУ ЖЕ функцию auditIamDatabase, которую на настоящем дереве
// зовёт гейт, — поэтому доказанное здесь верно там. По каждой оси доказываются
// ОБЕ стороны: дефект даёт находку И называет координату, а законный близнец
// той же формы МОЛЧИТ. Односторонняя ось зеленела бы на распознавателе,
// отвечающем одно и то же на любой вход.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОСЬ СОГЛАСИЯ ПОЛОВИН ИЗОЛИРОВАНА ДВУМЯ ПРОГОНАМИ, А НЕ ОДНИМ
//
// Каноничность и согласие связаны: переименовав ОДНУ половину, получаешь и
// «половина неканонична», и «половины разошлись» — то есть инъекция роняет
// сразу два утверждения, и молчание третьего доказать нечем. Поэтому осей две,
// и они разводят предметы:
//
//	обе половины отставлены  → каноничность падает ДВАЖДЫ, согласие МОЛЧИТ;
//	одна половина отставлена → каноничность падает ОДИН раз, согласие ГОВОРИТ.
//
// Без первого прогона молчание согласия было бы неотличимо от согласия
// мёртвого: утверждение, которое не выполняется никогда, не проверяет ничего.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// soundIamDatabaseDecl — заведомо годное объявление одного стенда. Инъекции
// портят ЕГО КОПИЮ ровно в одном месте: мир кейса отличается от близнеца одним
// названным фактом, и дельта потому вычисляема, а не объявлена.
func soundIamDatabaseDecl() iamDatabaseDecl {
	return iamDatabaseDecl{
		stack:    "проба",
		chain:    "values.проба.yaml",
		consumer: canonicalIamDatabase,
		provider: canonicalIamDatabase,
	}
}

// findingsMentioning — находки, где встречается подстрока. Помощник существует
// затем, чтобы ось утверждала ТЕКСТ находки, а не только её число: находка,
// назвавшая не ту координату, посылает читателя править не то место.
func findingsMentioning(findings []string, sub string) []string {
	var out []string
	for _, f := range findings {
		if strings.Contains(f, sub) {
			out = append(out, f)
		}
	}
	return out
}

// ── Ось 0: положительный контроль — годное объявление МОЛЧИТ ────────────────
//
// Без неё всякая ось ниже зеленела бы на проверке, которая краснеет всегда.

func TestIamDatabaseInjection_SoundDeclarationStaysSilent(t *testing.T) {
	findings, census := auditIamDatabase([]iamDatabaseDecl{soundIamDatabaseDecl()})

	require.Empty(t, findings, "годное объявление объявлено находкой: проверка краснеет на всём")
	require.Equal(t, 1, census.stacks, "стенд не сосчитан — обход пуст, вердикт беспредметен")
	require.Equal(t, 2, census.canonicalHits,
		"канонические половины не сосчитаны: положительный контроль гейта не наполнится")
}

// ── Ось 1: ПОТРЕБИТЕЛЬ зовёт базу отставленным именем ──────────────────────

func TestIamDatabaseInjection_RetiredConsumerIsFound(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.consumer = retiredIamDatabase

	findings, _ := auditIamDatabase([]iamDatabaseDecl{d})

	require.NotEmpty(t, findingsMentioning(findings, "ПОТРЕБИТЕЛЬ"),
		"имя базы у потребителя не судится: служба соберёт адрес к несуществующей базе")
	require.NotEmpty(t, findingsMentioning(findings, "kaname.db.name"),
		"находка не назвала координату объявления")
}

// ── Ось 2: ПОСТАВЩИК создаёт базу отставленным именем ──────────────────────

func TestIamDatabaseInjection_RetiredProviderIsFound(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.provider = retiredIamDatabase

	findings, _ := auditIamDatabase([]iamDatabaseDecl{d})

	require.NotEmpty(t, findingsMentioning(findings, "ПОСТАВЩИК"),
		"имя базы у поставщика не судится: подчарт создаст базу отставленного имени")
	require.NotEmpty(t, findingsMentioning(findings, "pg-iam.auth.database"),
		"находка не назвала координату объявления")
}

// ── Ось 3а: обе половины отставлены — СОГЛАСИЕ МОЛЧИТ ──────────────────────
//
// Первая половина изоляции: половины согласны между собой и обе неверны.
// Согласие обязано молчать, иначе его молчание в оси 3б ничего не доказывает.

func TestIamDatabaseInjection_BothHalvesRetiredDoNotTripTheAgreement(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.consumer = retiredIamDatabase
	d.provider = retiredIamDatabase

	findings, census := auditIamDatabase([]iamDatabaseDecl{d})

	require.Len(t, findings, 2,
		"ожидались ровно две находки каноничности; лишняя означает, что согласие "+
			"сработало на согласных половинах")
	require.Empty(t, findingsMentioning(findings, "РАЗОШЛИСЬ"),
		"согласие сработало там, где половины совпадают: утверждение ложно-положительно")
	require.Zero(t, census.canonicalHits, "канонических половин быть не должно")
}

// ── Ось 3б: переименована ОДНА половина — согласие ГОВОРИТ и называет обе ───
//
// Вторая половина изоляции и главный предмет всей проверки: ровно так выглядит
// правка, внесённая в одно место из двух. Стенд поднимется и не заработает.

func TestIamDatabaseInjection_HalfRenamedTripsTheAgreementAndNamesBothSides(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.provider = retiredIamDatabase // база СОЗДАНА старым именем, служба стучится в новое

	findings, _ := auditIamDatabase([]iamDatabaseDecl{d})

	split := findingsMentioning(findings, "РАЗОШЛИСЬ")
	require.Len(t, split, 1,
		"расхождение половин не найдено: именно оно даёт «database does not exist» "+
			"на каждом соединении при исправном на вид стенде")
	require.Contains(t, split[0], retiredIamDatabase, "находка не назвала сторону поставщика")
	require.Contains(t, split[0], canonicalIamDatabase, "находка не назвала сторону потребителя")
}

// ── Ось 4: половина НЕ ОБЪЯВЛЕНА — это отдельный исход, не «объявлена неверно» ──
//
// У двух исходов разные причины: у первого — цепочка профилей, у второго —
// значение. Находка, их смешивающая, посылает читателя править не тот файл.

func TestIamDatabaseInjection_UndeclaredHalfIsAnOutcomeOfItsOwn(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.consumer = ""

	findings, _ := auditIamDatabase([]iamDatabaseDecl{d})

	require.NotEmpty(t, findingsMentioning(findings, "не объявлено"),
		"незаявленная половина не отличена от заявленной неверно")
	require.Empty(t, findingsMentioning(findings, "РАЗОШЛИСЬ"),
		"незаявленная половина принята за расхождение: у неё другая причина и другая починка")
}

// ── Ось 5: адрес, объявленный ЗНАЧЕНИЕМ, судится ───────────────────────────

func TestIamDatabaseInjection_RetiredDatabaseInADeclaredAddressIsFound(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.dsns = []iamDatabaseDSN{{path: "kaname.config.repository.postgres.url", database: retiredIamDatabase}}

	findings, census := auditIamDatabase([]iamDatabaseDecl{d})

	require.NotEmpty(t, findingsMentioning(findings, "адрес подключения"),
		"объявленный значением адрес не судится: это второе место об имени базы")
	require.Equal(t, 1, census.dsnJudged, "адреса не сосчитаны")
}

// Близнец: адрес с КАНОНИЧЕСКОЙ базой молчит и наполняет положительный контроль.
func TestIamDatabaseInjection_CanonicalAddressStaysSilent(t *testing.T) {
	d := soundIamDatabaseDecl()
	d.dsns = []iamDatabaseDSN{{path: "kaname.config.repository.postgres.url", database: canonicalIamDatabase}}

	findings, census := auditIamDatabase([]iamDatabaseDecl{d})

	require.Empty(t, findings, "канонический адрес объявлен находкой")
	require.Equal(t, 3, census.canonicalHits, "адрес не засчитан в положительный контроль")
}

// ── Ось 6: разбор адреса знает свои формы и НЕ ловит чужую базу ────────────
//
// Отбор идёт по имени базы В САМОМ АДРЕСЕ, поэтому база СОСЕДНЕЙ службы обязана
// пройти мимо: те службы остаются Kachō, и их имена верны. Распознаватель,
// хватающий соседей, дал бы находку там, где нарушения нет, — и его снимут.

func TestIamDatabaseInjection_AddressCollectorKnowsItsFormsAndSparesNeighbours(t *testing.T) {
	tree := map[string]any{
		"ours":      map[string]any{"url": "postgres://iam@pg-iam:5432/" + retiredIamDatabase},
		"oursQuery": map[string]any{"url": "postgresql://iam@pg-iam:5432/" + canonicalIamDatabase + "?sslmode=require"},
		"neighbour": map[string]any{"url": "postgres://vpc@pg-vpc:5432/kacho_vpc"},
		"notAnAddr": "просто строка, в которой встречается " + retiredIamDatabase,
		"nested":    []any{map[string]any{"url": "postgres://iam@pg-iam:5432/" + retiredIamDatabase}},
	}

	var got []iamDatabaseDSN
	collectIamDatabaseDSNs(tree, nil, &got)

	require.Len(t, got, 3,
		"собрано не три адреса: распознаватель либо не знает формы, либо хватает чужие базы")

	var dbs, paths []string
	for _, d := range got {
		dbs = append(dbs, d.database)
		paths = append(paths, d.path)
	}
	require.ElementsMatch(t, []string{retiredIamDatabase, canonicalIamDatabase, retiredIamDatabase}, dbs)
	require.Contains(t, strings.Join(paths, " "), "nested.[0].url",
		"адрес внутри списка не найден: обход не спускается в последовательности")
	require.NotContains(t, strings.Join(paths, " "), "neighbour",
		"схвачена база соседней службы: те службы остаются Kachō, их имена верны")
	require.NotContains(t, strings.Join(paths, " "), "notAnAddr",
		"строка, адресом не являющаяся, принята за адрес")
}

// ── Ось 7: согласие ДВУХ ЧАРТОВ службы ─────────────────────────────────────
//
// Чарт службы живёт в дереве дважды, и неверным оказывается место БЕЗ читателя.

func TestIamDatabaseInjection_ChartParityAxis(t *testing.T) {
	sound := []iamDatabaseChartPair{
		{chart: "чарт-А", name: canonicalIamDatabase},
		{chart: "чарт-Б", name: canonicalIamDatabase},
	}
	require.Empty(t, auditIamDatabaseChartParity(sound),
		"согласные чарты объявлены находкой")

	drifted := []iamDatabaseChartPair{
		{chart: "чарт-А", name: canonicalIamDatabase},
		{chart: "чарт-Б", name: retiredIamDatabase},
	}
	got := auditIamDatabaseChartParity(drifted)
	require.Len(t, got, 1, "разошедшийся чарт не найден")
	require.Contains(t, got[0], "чарт-Б", "находка не назвала разошедшийся чарт")

	silent := []iamDatabaseChartPair{{chart: "чарт-В", name: ""}}
	require.NotEmpty(t, auditIamDatabaseChartParity(silent),
		"чарт, не объявивший имени базы, пропущен")
}
