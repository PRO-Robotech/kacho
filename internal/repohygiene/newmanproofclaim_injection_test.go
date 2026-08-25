// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «утверждение о доказанности называет координату,
// которая резолвится» СПОСОБЕН упасть — и что падает он на существе, а не на
// форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало (молчание
// бывает от того, что читать не стали):
//
//	обещание доказательства, файла нет            → КРАСНЕЕТ, называя файл, строку, ссылку;
//	то же обещание, файл на месте                 → молчит (законный близнец);
//	координата резолвится ОТ КОРНЯ НАБОРА         → молчит (так её зовёт CI);
//	координата резолвится ОТ КОРНЯ РЕПОЗИТОРИЯ    → молчит;
//	ОБРАЗЕЦ (`cases/*.py`) при том же утверждении  → молчит: он не координата;
//	голое имя без каталога                        → молчит: оно не говорит, где искать;
//	мёртвая координата БЕЗ утверждения о доказательстве → молчит: не предмет гейта;
//	утверждение по-английски (`proven`)           → КРАСНЕЕТ: корпус двуязычен;
//	перенос комментария — координата на след. строке → КРАСНЕЕТ;
//	корпус без единого утверждения                → находок 0, и перепись это НАЗЫВАЕТ.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditProofClaims`), что и прогон по дереву.
package repohygiene_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// resolver — подставной резолвер: существует ровно то, что названо. Инъекция не
// трогает дерево, поэтому её вердикт не зависит от чужого рабочего каталога.
func resolver(present ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range present {
		set[p] = true
	}
	return func(rel string) bool { return set[rel] }
}

const suiteGen = "services/storage/tests/newman/scripts/gen.py"

// ДЕФЕКТ 1 — обещано доказательство, файла нет. Ровно #1277.
func TestProofClaimGateRedsWhenThePromisedProofIsAbsent(t *testing.T) {
	t.Parallel()

	src := "# Доказательство в обе стороны — `scripts/selftest_autowrap.py`: инъекция\n" +
		"# настоящего пропуска и законные близнецы.\n"

	census, findings := auditProofClaims(
		map[string]string{suiteGen: src},
		resolver("services/vpc/tests/newman/scripts/selftest_autowrap.py"))

	require.Len(t, findings, 1, "обещание доказательства без предмета обязано быть находкой")
	require.Equal(t, suiteGen, findings[0].File, "находка обязана называть КООРДИНАТУ")
	require.Equal(t, 1, findings[0].Line, "находка обязана называть СТРОКУ")
	require.Equal(t, "scripts/selftest_autowrap.py", findings[0].Ref,
		"находка обязана называть саму ССЫЛКУ, иначе не приводит к предмету")
	require.Equal(t, 1, census.Coordinates, "перепись обязана заявить объём осмотренного")
}

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — то же обещание там, где файл ЕСТЬ. Без этой половины гейт
// ловил бы форму (наличие обещания), а не существо (отсутствие предмета).
func TestProofClaimGateSilentWhenTheProofSitsNextToTheFile(t *testing.T) {
	t.Parallel()

	vpcGen := "services/vpc/tests/newman/scripts/gen.py"
	src := "# Доказательство в обе стороны — `scripts/selftest_autowrap.py`.\n"

	_, findings := auditProofClaims(
		map[string]string{vpcGen: src},
		// Резолв ОТ КОРНЯ НАБОРА — именно так координату читает CI, задавая
		// working-directory каталогом набора.
		resolver("services/vpc/tests/newman/scripts/selftest_autowrap.py"))
	require.Empty(t, findings, "координата резолвится от корня набора — предмет на месте")
}

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — координата от КОРНЯ РЕПОЗИТОРИЯ.
func TestProofClaimGateSilentOnARepoRootCoordinate(t *testing.T) {
	t.Parallel()

	src := "# Доказано гейтом дерева — `internal/repohygiene/artifactgates/x_test.go`.\n"
	_, findings := auditProofClaims(map[string]string{suiteGen: src},
		resolver("internal/repohygiene/artifactgates/x_test.go"))
	require.Empty(t, findings, "координата от корня репозитория обязана резолвиться")
}

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — ОБРАЗЕЦ координатой не является. Требовать от него резолва
// значило бы завести перечень прощённых, а каждая запись в нём — место, куда
// неправда вносится незамеченной.
func TestProofClaimGateSilentOnAGlobAndOnABareName(t *testing.T) {
	t.Parallel()

	src := "# Доказательство — обходом `cases/*.py`, см. также `setup.sh`.\n"
	census, findings := auditProofClaims(map[string]string{suiteGen: src}, resolver())
	require.Empty(t, findings,
		"образец и голое имя без каталога координатами не являются: они не говорят, "+
			"где искать")
	require.Equal(t, 0, census.Coordinates)
	require.Equal(t, 1, census.ClaimLines,
		"строку с утверждением перепись обязана посчитать — иначе «координат 0» "+
			"неотличимо от «утверждений 0»")
}

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — мёртвая координата БЕЗ утверждения о доказательстве. Не
// предмет этого гейта: он стережёт обещание доказанности, а не всякую ссылку.
func TestProofClaimGateSilentOnADeadPathWithoutAProofClaim(t *testing.T) {
	t.Parallel()

	src := "# Форма шага взята из `services/nlb/tests/newman/collections/gone.py`.\n"
	census, findings := auditProofClaims(map[string]string{suiteGen: src}, resolver())
	require.Empty(t, findings, "гейт стережёт обещание ДОКАЗАННОСТИ, а не всякую ссылку")
	require.Equal(t, 0, census.ClaimLines)
}

// ДЕФЕКТ 2 — утверждение по-АНГЛИЙСКИ. Корпус двуязычен, и предикат, ищущий на
// одном языке, недобирает молча — именно там, где предмет объясняли словами.
func TestProofClaimGateRedsOnAnEnglishProofClaim(t *testing.T) {
	t.Parallel()

	src := "# Both directions are proven by `scripts/selftest_gone.py`.\n"
	_, findings := auditProofClaims(map[string]string{suiteGen: src}, resolver())
	require.Len(t, findings, 1,
		"англоязычное утверждение о доказанности обязано читаться так же, как русское")
}

// ДЕФЕКТ 3 — координата на СЛЕДУЮЩЕЙ строке (комментарий перенесён). Привязка
// только к своей строке недобрала бы ровно там, где утверждение длинное.
func TestProofClaimGateRedsWhenTheCoordinateWrapsToTheNextLine(t *testing.T) {
	t.Parallel()

	src := "# Доказательство в обе стороны —\n# `scripts/selftest_gone.py`: инъекция.\n"
	_, findings := auditProofClaims(map[string]string{suiteGen: src}, resolver())
	require.Len(t, findings, 1, "перенос комментария не отменяет утверждения")
	require.Equal(t, "scripts/selftest_gone.py", findings[0].Ref)
}

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — корпус без единого утверждения. Пустой перечень находок
// есть ЦЕЛЬ, а не поломка: падать на достижении собственной цели гейт не вправе.
// Предпосылку («координат ноль — разбор сломан») держит прогон по ДЕРЕВУ, где
// корпус заведомо непуст, а не эта проба.
func TestProofClaimGateSilentOnACorpusWithoutClaims(t *testing.T) {
	t.Parallel()

	src := "def build():\n    return []\n"
	census, findings := auditProofClaims(map[string]string{suiteGen: src}, resolver())
	require.Empty(t, findings)
	require.Equal(t, 1, census.Files, "перепись обязана заявить объём осмотренного")
	require.Equal(t, 0, census.ClaimLines)
}
