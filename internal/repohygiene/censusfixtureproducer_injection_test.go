// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// censusfixtureproducer_injection_test.go — доказательство, что
// TestCensusFixturesSeedThroughTheProducer умеет И краснеть, И молчать.
//
// Инъекция кормит разбор НАСТОЯЩИМ входом (текстом Go, который разбирает тот же
// `censusFactsOf`), а не заранее собранными фактами: иначе доказывалось бы
// согласие двух своих же записей, а не работа предиката.
//
// Пары обязательны. Отрицание без положительного близнеца зеленело бы на гейте,
// который отвергает вообще всё; положительное без отрицания — на гейте, который
// не отвергает ничего.

const (
	// Перепись покрытия: ОДИН литерал, называющий обе таблицы.
	//
	// Именно один, а не два склеенных `+`: примета гейта — квантор в ОДНОМ
	// стейтменте. Первая редакция этой фикстуры разнесла запрос на две строки, и
	// половина инъекций позеленела ВХОЛОСТУЮ — не потому, что гейт молчит на
	// законном, а потому что условия не возникало вовсе. Отсюда injCensusFacts
	// ниже, который проверяет предпосылку самой фикстуры.
	injCensusQuery = `const q = "SELECT 1 FROM kacho_iam.resource_mirror m WHERE NOT EXISTS (SELECT 1 FROM kacho_iam.resource_parent_edge e WHERE e.object_id = m.object_id)"
`
	injProducerImport = `import "github.com/PRO-Robotech/kacho-iam/internal/repo/kacho/pg/resource_mirror"
`
	injProducerCall = `func seed() { _, _ = resource_mirror.UpsertTx(nil, nil, resource_mirror.Row{}) }
`
)

// injFacts разбирает синтетический файл тем же разбором, что и гейт.
func injFacts(t *testing.T, rel, src string) censusFileFacts {
	t.Helper()
	return censusFactsOf(t, rel, []byte("package p_test\n"+src), "resource_mirror")
}

// injCensusFacts — то же, но утверждает ПРЕДПОСЫЛКУ фикстуры: разобранный файл
// действительно опознан как перепись.
//
// Без этой проверки инъекция, ожидающая молчания, зеленеет на фикстуре, которая
// условия не создаёт, — и доказывает не «гейт молчит на законном», а «гейту
// нечего было судить». Наблюдалось на первой редакции этого файла.
func injCensusFacts(t *testing.T, rel, src string) censusFileFacts {
	t.Helper()
	f := injFacts(t, rel, src)
	if !f.census {
		t.Fatalf("фикстура %s не опознана как перепись — условие инъекции не создано, "+
			"и любой вердикт по ней относился бы не к тому предмету", rel)
	}
	return f
}

// TestInjection_CensusWithoutAProducerAnywhereInItsPackageIsFound — настоящий
// класс: перепись есть, производителя не зовёт НИКТО в пакете.
func TestInjection_CensusWithoutAProducerAnywhereInItsPackageIsFound(t *testing.T) {
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {injCensusFacts(t, "svc/pkg/census_test.go", injCensusQuery)},
	}
	findings := judgeCensusFixtures(byDir)
	if len(findings) != 1 {
		t.Fatalf("гейт обязан найти перепись без производителя, а нашёл %d: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "svc/pkg/census_test.go") {
		t.Errorf("отказ обязан называть КООРДИНАТУ, иначе разбирать его негде: %q", findings[0])
	}
}

// TestInjection_CensusSeededByASiblingHelperIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ и
// предмет #778: производитель зовётся из соседнего файла того же тестового
// пакета. Прежняя редакция гейта краснела здесь, и это красное держало линию.
func TestInjection_CensusSeededByASiblingHelperIsSilent(t *testing.T) {
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {
			injCensusFacts(t, "svc/pkg/census_test.go", injCensusQuery),
			injFacts(t, "svc/pkg/helper_test.go", injProducerImport+injProducerCall),
		},
	}
	if findings := judgeCensusFixtures(byDir); len(findings) != 0 {
		t.Fatalf("проба, сеющая через помощника СОСЕДНЕГО ФАЙЛА того же пакета, сеет через "+
			"производителя — гейт обязан молчать, а он нашёл: %v", findings)
	}
}

// TestInjection_CensusCallingTheProducerItselfIsSilent — положительный контроль
// прямого вызова: без него отрицания выше зеленели бы на гейте, отвергающем всё.
func TestInjection_CensusCallingTheProducerItselfIsSilent(t *testing.T) {
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {injCensusFacts(t, "svc/pkg/census_test.go",
			injProducerImport+injCensusQuery+injProducerCall)},
	}
	if findings := judgeCensusFixtures(byDir); len(findings) != 0 {
		t.Fatalf("перепись зовёт производителя в своём же файле — гейт обязан молчать: %v", findings)
	}
}

// TestInjection_AliasedProducerImportIsStillRecognised — импорт под псевдонимом
// остаётся производителем. Иначе гейт краснел бы на переименовании импорта,
// то есть ловил бы написание, а не вызов.
func TestInjection_AliasedProducerImportIsStillRecognised(t *testing.T) {
	src := `import rm "github.com/PRO-Robotech/kacho-iam/internal/repo/kacho/pg/resource_mirror"
` + injCensusQuery + `func seed() { _, _ = rm.UpsertTx(nil, nil, rm.Row{}) }
`
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {injCensusFacts(t, "svc/pkg/census_test.go", src)},
	}
	if findings := judgeCensusFixtures(byDir); len(findings) != 0 {
		t.Fatalf("вызов производителя под псевдонимом импорта — гейт обязан молчать: %v", findings)
	}
}

// TestInjection_SameNamedFunctionOfAnotherPackageIsNotTheProducer — сужение,
// введённое вместе с исправлением #778.
//
// `UpsertTx` в дереве не одна: одноимённую экспортирует
// `.../pg/target_members`. Предикат «встретилось слово UpsertTx» признал бы
// такой вызов посевом рёбер — то есть зеленел бы на фикстуре, не посеявшей
// ничего. Это единственная сторона, где новая редакция СТРОЖЕ прежней.
func TestInjection_SameNamedFunctionOfAnotherPackageIsNotTheProducer(t *testing.T) {
	src := `import "github.com/PRO-Robotech/kacho-iam/internal/repo/kacho/pg/target_members"
` + injCensusQuery + `func seed() { _ = target_members.UpsertTx(nil, nil, target_members.Member{}) }
`
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {injCensusFacts(t, "svc/pkg/census_test.go", src)},
	}
	if findings := judgeCensusFixtures(byDir); len(findings) != 1 {
		t.Fatalf("одноимённая функция ЧУЖОГО пакета производителем рёбер не является — "+
			"гейт обязан найти, а нашёл %d: %v", len(findings), findings)
	}
}

// TestInjection_CensusThatWritesEdgesItselfIsFound — вторая форма находки:
// перепись сама кладёт рёбра прямой записью.
func TestInjection_CensusThatWritesEdgesItselfIsFound(t *testing.T) {
	src := injProducerImport + injCensusQuery +
		`const ins = "INSERT INTO kacho_iam.resource_parent_edge (object_id) VALUES ($1)"
` + injProducerCall
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {injCensusFacts(t, "svc/pkg/census_test.go", src)},
	}
	findings := judgeCensusFixtures(byDir)
	if len(findings) != 1 || !strings.Contains(findings[0], "прямой записью") {
		t.Fatalf("перепись с прямой записью рёбер обязана быть находкой ДАЖЕ при живом "+
			"вызове производителя, а получено: %v", findings)
	}
}

// TestInjection_ReaderProbeMayWriteEdgesDirectly — законный близнец второй
// формы: проба ЧИТАТЕЛЯ (без переписи) вправе класть рёбра прямо.
func TestInjection_ReaderProbeMayWriteEdgesDirectly(t *testing.T) {
	src := `const ins = "INSERT INTO kacho_iam.resource_parent_edge (object_id) VALUES ($1)"
`
	byDir := map[string][]censusFileFacts{
		"svc/pkg": {injFacts(t, "svc/pkg/reader_test.go", src)},
	}
	if findings := judgeCensusFixtures(byDir); len(findings) != 0 {
		t.Fatalf("проба читателя спрашивает об ОДНОМ объекте и вправе сеять прямо — "+
			"гейт обязан молчать: %v", findings)
	}
}

// TestInjection_ProducerInAnotherPackageDoesNotCoverThisOne — пакеты не делят
// помощников: производитель в ЧУЖОМ каталоге не оправдывает перепись здесь.
func TestInjection_ProducerInAnotherPackageDoesNotCoverThisOne(t *testing.T) {
	byDir := map[string][]censusFileFacts{
		"svc/pkg":   {injCensusFacts(t, "svc/pkg/census_test.go", injCensusQuery)},
		"svc/other": {injFacts(t, "svc/other/helper_test.go", injProducerImport+injProducerCall)},
	}
	findings := judgeCensusFixtures(byDir)
	if len(findings) != 1 || !strings.Contains(findings[0], "svc/pkg/census_test.go") {
		t.Fatalf("производитель в чужом пакете не собирается в тот же бинарь — перепись "+
			"обязана остаться находкой, а получено: %v", findings)
	}
}

// TestCensusGatePremise_ProducerPackageIsWhereTheGateThinksItIs — предпосылка на
// НАСТОЯЩЕМ дереве: пакет-производитель существует и функция в нём объявлена.
//
// Переедет или переименуется — падает здесь, с одним внятным отказом, вместо
// того чтобы объявить находкой каждую пробу-перепись сразу.
func TestCensusGatePremise_ProducerPackageIsWhereTheGateThinksItIs(t *testing.T) {
	if got := producerPackageName(t, repoRoot(t)); got != "resource_mirror" {
		t.Fatalf("имя пакета-производителя изменилось на %q: сверь его с приметой гейта", got)
	}
}
