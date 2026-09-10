// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Инъекция держателя «названное доказательство существует» — В ОБЕ СТОРОНЫ И ПО
// КАЖДОЙ ФОРМЕ КООРДИНАТЫ.
//
// Форм четыре, и форма, о которой распознаватель не знает, даёт не красное и не
// зелёное, а МОЛЧАНИЕ: обещание уходит из наблюдения целиком. Поэтому по каждой
// форме утверждается СВОЯ пара — «доказательство есть ⇒ тишина» и «его нет ⇒
// находка», — а дельта миров всегда ОДИН факт: наличие одного файла.
//
// Отдельно доказывается граница, объявленная шапкой разбора: координата в
// СТРОКОВОМ ЛИТЕРАЛЕ не судится (там живут синтетические имена чужих фикстур), но
// СЧИТАЕТСЯ переписью — молчание о целом виде вхождений неотличимо от их
// отсутствия.
package repohygiene

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/tools/injectionproofgate"
)

// proofName — имя файла-доказательства, собранное СКЛЕЙКОЙ.
//
// Записанное одним литералом, оно стало бы координатой в строковом литерале
// ЭТОГО файла и попало бы в перепись держателя как вхождение, которого в дереве
// нет. Проба не вправе менять вердикт о дереве самим фактом своего существования.
const proofName = "probe" + "_injection" + "_test.go"

// namingBody — тело пробы, называющей доказательство КОМЕНТАРИЕМ.
func namingBody(coord string) []byte {
	return []byte("package probe\n\n// Способность упасть доказана инъекцией — " + coord + ".\nvar _ = 1\n")
}

func auditWorld(t *testing.T, corpus map[string][]byte) ([]injectionproofgate.Finding, injectionproofgate.Census) {
	t.Helper()
	findings, census, err := injectionproofgate.Audit(corpus)
	require.NoError(t, err)
	return findings, census
}

func TestInjectionProof_NeighbourFormResolves(t *testing.T) {
	t.Parallel()
	findings, census := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go": namingBody(proofName),
		"internal/probe/" + proofName:  []byte("package probe\n"),
	})
	require.Equal(t, 1, census.InComments)
	require.Equal(t, 1, census.Resolved)
	require.Empty(t, findings, "СОСЕД: доказательство в каталоге называющего")
}

func TestInjectionProof_MissingProofIsAFinding(t *testing.T) {
	t.Parallel()
	// ЗАКОННЫЙ БЛИЗНЕЦ предыдущего мира: отличается ОДНИМ фактом — файла нет.
	findings, census := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go": namingBody(proofName),
	})
	require.Equal(t, 1, census.InComments)
	require.Zero(t, census.Resolved)
	require.Len(t, findings, 1)
	require.Equal(t, "internal/probe/probe_test.go", findings[0].NamedBy)
}

func TestInjectionProof_ByTreeFormResolves(t *testing.T) {
	t.Parallel()
	findings, _ := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go": namingBody(proofName),
		"internal/other/" + proofName:  []byte("package other\n"),
	})
	require.Emptyf(t, findings, "ПО ДЕРЕВУ: проба одного пакета вправе ссылаться на "+
		"инъекцию соседнего")
}

func TestInjectionProof_WalkRootCoordinateResolves(t *testing.T) {
	t.Parallel()
	findings, _ := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go": namingBody("internal/other/" + proofName),
		"internal/other/" + proofName:  []byte("package other\n"),
	})
	require.Empty(t, findings, "ОТ КОРНЯ ОБХОДА")
}

func TestInjectionProof_ModuleRootCoordinateResolves(t *testing.T) {
	t.Parallel()
	// Проба службы называет координату от СВОЕГО модуля: в самостоятельном клоне
	// приставки `services/iam/` нет вовсе.
	findings, census := auditWorld(t, map[string][]byte{
		"go.mod":              nil,
		"services/iam/go.mod": nil,
		"services/iam/internal/probe/probe_test.go": namingBody("internal/probe/" + proofName),
		"services/iam/internal/probe/" + proofName:  []byte("package probe\n"),
	})
	require.Equal(t, 2, census.ModuleRoots)
	require.Emptyf(t, findings, "ОТ КОРНЯ МОДУЛЯ: без этой формы каждая такая координата "+
		"стала бы ЛОЖНОЙ находкой, а гейт с ложными находками отключают первым")
}

func TestInjectionProof_ModuleRootFormIsWhatSavesIt(t *testing.T) {
	t.Parallel()
	// Тот же мир БЕЗ объявления модуля службы — дельта в один факт. Координата
	// перестаёт резолвиться, и это доказывает, что тишина выше принадлежит
	// четвёртой форме, а не совпадению.
	findings, census := auditWorld(t, map[string][]byte{
		"go.mod": nil,
		"services/iam/internal/probe/probe_test.go": namingBody("internal/probe/" + proofName),
		"services/iam/internal/probe/" + proofName:  []byte("package probe\n"),
	})
	require.Equal(t, 1, census.ModuleRoots)
	require.Len(t, findings, 1)
}

func TestInjectionProof_EnclosingRootsAreAUnionByDecision(t *testing.T) {
	t.Parallel()
	// Проба внутри модуля службы называет координату ОТ КОРНЯ МОНОРЕПО — вторая
	// законная запись того же предмета, и она живёт в дереве. Союз объемлющих
	// корней обязан её принять: требование «только ближайший корень» сделало бы
	// её ложной находкой.
	findings, census := auditWorld(t, map[string][]byte{
		"go.mod":              nil,
		"services/iam/go.mod": nil,
		"services/iam/internal/probe/probe_test.go": namingBody(
			"services/iam/internal/probe/" + proofName),
		"services/iam/internal/probe/" + proofName: []byte("package probe\n"),
	})
	require.Equal(t, 2, census.ModuleRoots)
	require.Empty(t, findings)
}

func TestInjectionProof_StringLiteralIsNotJudgedButIsCounted(t *testing.T) {
	t.Parallel()
	body := []byte("package probe\n\nvar fixture = \"internal/b/c" + "_injection" + "_test.go\"\n")
	findings, census := auditWorld(t, map[string][]byte{"internal/probe/probe_test.go": body})
	require.Emptyf(t, findings, "литерал — синтетическое имя чужой фикстуры; судить его "+
		"значило бы краснеть на собственном доказательстве")
	require.Zero(t, census.InComments)
	require.Equalf(t, 1, census.InStrings, "но полоса обязана быть СОСЧИТАНА: молчание о "+
		"целом виде вхождений неотличимо от их отсутствия")
}

func TestInjectionProof_UnparsableSourceIsNotCounted(t *testing.T) {
	t.Parallel()
	_, census := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go": namingBody(proofName),
		"internal/probe/" + proofName:  []byte("package probe\n"),
		"internal/broken/broken.go":    []byte("это не Go\n"),
	})
	require.Equalf(t, 2, census.GoFiles, "неразбираемый исходник — предмет компилятора, а "+
		"перепись обязана недосчитать его вслух")
}

func TestInjectionProof_EmptyCorpusIsAnError(t *testing.T) {
	t.Parallel()
	_, _, err := injectionproofgate.Audit(map[string][]byte{})
	require.Errorf(t, err, "пустой корпус обязан быть ОШИБКОЙ, а не тихим зелёным")
}

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОСА ДОКАЗАТЕЛЬСТВ ДРУГОГО ЯЗЫКА (#2519)
//
// Обещание, называющее доказательство оболочкой (`<тема>-inject.sh`) либо пробой
// на Python, — та же форма утверждения о дереве, и мёртвая координата в нём
// читается так же, как само доказательство. До этой полосы разбор её не судил и
// НЕ СЧИТАЛ: обещание уходило из наблюдения целиком, а молчание о целом виде
// вхождений неотличимо от их отсутствия.
//
// Дельта каждого мира против положительного близнеца — ОДИН факт: наличие одного
// файла-доказательства. Обе стороны утверждаются порознь, и порознь же
// проверяется, что снятие ОДНОГО доказательства роняет ТОЛЬКО его полосу.

// shellProofName / goProofName — имена, собранные СКЛЕЙКОЙ: записанные одним
// литералом, они стали бы координатами в строковом литерале ЭТОГО файла и
// изменили бы перепись держателя о настоящем дереве.
const (
	shellProofName = "topic" + "-inject" + ".sh"
	goProofName    = "topic" + "_injection" + "_test.go"
)

// bothPromises — тело пробы, называющей ОБА доказательства: и своё, и соседнее.
func bothPromises() []byte {
	return []byte("package probe\n\n// Способность упасть доказана инъекцией — " +
		goProofName + " и deploy/tests/helm/" + shellProofName + ".\nvar _ = 1\n")
}

// intactWorld — контроль: оба доказательства на месте.
func intactWorld() map[string][]byte {
	return map[string][]byte{
		"internal/probe/probe_test.go":        bothPromises(),
		"internal/probe/" + goProofName:       []byte("package probe\n"),
		"deploy/tests/helm/" + shellProofName: nil,
	}
}

func TestInjectionProof_ForeignFormControlIsSilentAndCounted(t *testing.T) {
	t.Parallel()
	findings, census := auditWorld(t, intactWorld())
	require.Emptyf(t, findings, "КОНТРОЛЬ: оба доказательства в дереве — молчат обе полосы")
	require.Equal(t, 2, census.InComments, "обещаний две: своё и соседнего языка")
	require.Equal(t, 2, census.Resolved)
	require.Equalf(t, 1, census.InOtherLanguages,
		"полоса другого языка обязана быть СОСЧИТАНА: без числа её молчание "+
			"неотличимо от отсутствия обещаний в ней")
}

func TestInjectionProof_ForeignProofMissingIsAFindingAndOnlyItsOwn(t *testing.T) {
	t.Parallel()
	// ИНЪЕКЦИЯ НОВОГО: снято доказательство ОБОЛОЧКИ, Go-доказательство на месте.
	world := intactWorld()
	delete(world, "deploy/tests/helm/"+shellProofName)

	findings, census := auditWorld(t, world)
	require.Len(t, findings, 1, "краснеет ТОЛЬКО новая полоса")
	require.Equal(t, "deploy/tests/helm/"+shellProofName, findings[0].Coordinate)
	require.Equal(t, 1, census.Resolved, "старая полоса резолвится по-прежнему")
	require.Equal(t, 1, census.InOtherLanguages)
}

func TestInjectionProof_GoProofMissingIsAFindingAndOnlyItsOwn(t *testing.T) {
	t.Parallel()
	// ИНЪЕКЦИЯ СТАРОГО: снято Go-доказательство, доказательство оболочки на месте.
	// Без этого прогона молчание существующей полосы неотличимо от молчания мёртвой.
	world := intactWorld()
	delete(world, "internal/probe/"+goProofName)

	findings, census := auditWorld(t, world)
	require.Len(t, findings, 1, "краснеет ТОЛЬКО существующая полоса")
	require.Equal(t, goProofName, findings[0].Coordinate)
	require.Equal(t, 1, census.Resolved, "полоса другого языка резолвится по-прежнему")
	require.Equal(t, 1, census.InOtherLanguages)
}

// TestInjectionProof_ForeignFormPatternIsNotACoordinate — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// В прозе форма доказательства называется ОБРАЗЦОМ (`*-inject.sh`,
// `<тема>-inject.sh`). Образец дерева не называет, и судить его значило бы
// краснеть на собственном объяснении. Без этой пары расширение распознавателя
// завело бы ложную находку в первом же файле, который форму объясняет.
func TestInjectionProof_ForeignFormPatternIsNotACoordinate(t *testing.T) {
	t.Parallel()
	body := []byte("package probe\n\n// Доказательство инъекцией называется " +
		"`*-inject" + ".sh`, у службы — `<тема>-inject" + ".sh`.\nvar _ = 1\n")
	findings, census := auditWorld(t, map[string][]byte{"internal/probe/probe_test.go": body})
	require.Empty(t, findings, "образец — не координата")
	require.Zero(t, census.InOtherLanguages, "и в перепись полосы он не идёт")
}

// ─────────────────────────────────────────────────────────────────────────────
// ПАРА, ЗАВИСЯЩАЯ ТОЛЬКО ОТ НОВОЙ ОСИ
//
// Миры выше называют ОБА доказательства сразу — так доказывается, что находка
// принадлежит своей полосе. Но проверка, зависящая от обеих осей, краснеет и на
// сломе ЧУЖОЙ: по ней нельзя отличить «умерла новая ось» от «умерла старая».
// Поэтому здесь мир, в котором Go-координаты нет ВОВСЕ, — и он остаётся зелёным,
// когда снимают старую ось, доказывая, что новая жива сама по себе.

// shellOnlyPromise — обещание, называющее ТОЛЬКО доказательство оболочки.
func shellOnlyPromise() []byte {
	return []byte("package probe\n\n// Способность упасть доказана инъекцией — deploy/tests/helm/" +
		shellProofName + ".\nvar _ = 1\n")
}

func TestInjectionProof_ForeignFormAloneResolves(t *testing.T) {
	t.Parallel()
	findings, census := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go":        shellOnlyPromise(),
		"deploy/tests/helm/" + shellProofName: nil,
	})
	require.Empty(t, findings)
	require.Equal(t, 1, census.InComments)
	require.Equal(t, 1, census.InOtherLanguages)
	require.Equal(t, 1, census.Resolved)
}

func TestInjectionProof_ForeignFormAloneMissingIsAFinding(t *testing.T) {
	t.Parallel()
	// ЗАКОННЫЙ БЛИЗНЕЦ предыдущего мира: дельта — ОДИН факт, файла нет.
	findings, census := auditWorld(t, map[string][]byte{
		"internal/probe/probe_test.go": shellOnlyPromise(),
	})
	require.Len(t, findings, 1)
	require.Equal(t, "deploy/tests/helm/"+shellProofName, findings[0].Coordinate)
	require.Equal(t, 1, census.InOtherLanguages)
	require.Zero(t, census.Resolved)
}
