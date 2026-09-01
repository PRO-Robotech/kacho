// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция гейта «досев дотягивается до писателя через порт» — В ОБЕ СТОРОНЫ.
//
// Признак воспроизводится над СИНТЕТИЧЕСКИМ входом: правкой настоящего дерева
// инъекция не ставится — она рвала бы чужие прогоны в общей рабочей копии.
//
// По каждой оси ТРИ прогона: контроль · инъекция проверяемого · законный
// близнец. Без третьего молчание проверки неотличимо от её смерти.

// writerPkgOf — обёртка над признаком «файл пишет проекцию».
func writerPkgOf(t *testing.T, rel, src string) (string, bool) {
	t.Helper()
	pkg, isWriter, err := roleVerbWriterPackageOf(rel, src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	return pkg, isWriter
}

const injWriterSrc = `package pg

func (w *roleWriter) ReplaceRoleVerbs(ctx context.Context) error {
	_, err := w.tx.Exec(ctx, ` + "`DELETE FROM kacho_iam.role_verb WHERE role_id = $1`" + `)
	return err
}
`

const injReaderSrc = `package scalegrid

func count(ctx context.Context) error {
	return row(ctx, ` + "`SELECT count(*)::bigint FROM kacho_iam.role_verb`" + `)
}
`

// TestReseedWiring_WriterPackageIsRecognisedAndReaderIsNot — ось «кто писатель»
// различает ЗАПИСЬ и ЧТЕНИЕ: без этого весь гейт указывал бы на чужой пакет.
func TestReseedWiring_WriterPackageIsRecognisedAndReaderIsNot(t *testing.T) {
	pkg, isWriter := writerPkgOf(t, "services/iam/internal/repo/kacho/pg/role_repo.go", injWriterSrc)
	if !isWriter {
		t.Fatal("признак МОЛЧИТ на файле, который пишет проекцию, — гейт не способен " +
			"найти предмет, и его зелёный на дереве ничего не значит")
	}
	if want := modulePathPrefix + "services/iam/internal/repo/kacho/pg"; pkg != want {
		t.Errorf("путь пакета писателя %q, а ожидался %q — находка укажет не туда", pkg, want)
	}

	// Законный близнец: ЧИТАТЕЛЬ той же таблицы пакетом-писателем не становится.
	if _, isWriter := writerPkgOf(t, "services/iam/internal/repo/kacho/pg/scalegrid/census.go", injReaderSrc); isWriter {
		t.Error("читатель проекции признан ПИСАТЕЛЕМ — тогда гейт запретил бы слою " +
			"use-case импортировать пакет переписи, к записи отношения не имеющий")
	}
}

// TestReseedWiring_InjectionRedOnAUseCaseImportingTheWriter — файл use-case,
// импортирующий пакет писателя, → находка С КООРДИНАТОЙ.
func TestReseedWiring_InjectionRedOnAUseCaseImportingTheWriter(t *testing.T) {
	writerPkgs := map[string]string{
		modulePathPrefix + "services/iam/internal/repo/kacho/pg/roleverb": "services/iam/internal/repo/kacho/pg/roleverb/roleverb.go",
	}
	apps := []useCaseFileImports{{
		Rel: "services/iam/internal/apps/kacho/seed/role_verb_reseed.go",
		Imports: []string{
			"context",
			modulePathPrefix + "services/iam/internal/repo/kacho/pg/roleverb",
		},
	}}
	got := useCaseWriterImportFindings(apps, writerPkgs)
	if len(got) != 1 {
		t.Fatalf("находок %d, а обязана быть одна: %v — гейт не способен покраснеть "+
			"на прямом импорте адаптера из use-case", len(got), got)
	}
	if !strings.HasPrefix(got[0], "services/iam/internal/apps/kacho/seed/role_verb_reseed.go ") {
		t.Errorf("находка не названа координатой файла: %q — читатель пойдёт искать "+
			"её и не найдёт", got[0])
	}
}

// TestReseedWiring_InjectionSilentOnANeighbouringAdapterImport — законный
// близнец: use-case импортирует ДРУГОЙ пакет того же слоя адаптера, писателем
// не являющийся. Гейт обязан молчать: он запрещает импорт ПИСАТЕЛЯ, а не слоя.
func TestReseedWiring_InjectionSilentOnANeighbouringAdapterImport(t *testing.T) {
	writerPkgs := map[string]string{
		modulePathPrefix + "services/iam/internal/repo/kacho/pg": "services/iam/internal/repo/kacho/pg/role_repo.go",
	}
	apps := []useCaseFileImports{{
		Rel: "services/iam/internal/apps/kacho/seed/migrate_backfill.go",
		Imports: []string{
			modulePathPrefix + "services/iam/internal/repo/kacho/pg/fga_outbox",
			modulePathPrefix + "services/iam/internal/repo/kacho",
			modulePathPrefix + "services/iam/internal/apps/kacho/shared",
		},
	}}
	if got := useCaseWriterImportFindings(apps, writerPkgs); len(got) != 0 {
		t.Errorf("гейт покраснел на законной форме: %v — импорт ПОРТА и соседнего "+
			"адаптера нарушением не является, и ложная находка отключит гейт первой", got)
	}
}

// TestReseedWiring_LayerPredicateSeparatesUseCaseFromRepo — сам писатель лежит в
// `repo/` и обязан оставаться вне выборки: иначе гейт краснел бы на себе.
func TestReseedWiring_LayerPredicateSeparatesUseCaseFromRepo(t *testing.T) {
	if !isUseCaseLayer("services/iam/internal/apps/kacho/seed/role_verb_reseed.go") {
		t.Error("файл слоя use-case не опознан — выборка гейта пуста, и его молчание " +
			"ничего не значит")
	}
	if isUseCaseLayer("services/iam/internal/repo/kacho/pg/role_repo.go") {
		t.Error("файл слоя repo/ отнесён к use-case — гейт краснел бы на самом писателе")
	}
}

const injSeedInsideCallSrc = `package seed

func BackfillOwnerBindings(ctx context.Context, pool *pgxpool.Pool) error {
	if _, rerr := ReseedSystemRoleVerbs(ctx, pool, nil); rerr != nil {
		return rerr
	}
	return nil
}
`

const injSeedNeighbourCallSrc = `package seed

func BackfillOwnerBindings(ctx context.Context, pool *pgxpool.Pool) error {
	if err := SyncAllSystemRoleSelectors(ctx, pool); err != nil {
		return err
	}
	return nil
}
`

// TestReseedWiring_InjectionRedOnAnInPackageReseedCall — вызов пересчёта изнутри
// чужого досева → находка, названная объемлющей функцией.
func TestReseedWiring_InjectionRedOnAnInPackageReseedCall(t *testing.T) {
	entries := map[string]bool{"ReseedSystemRoleVerbs": true}
	got, err := roleVerbReseedCallsInside("migrate_backfill.go", injSeedInsideCallSrc, entries)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("находок %d, а обязана быть одна: %v — гейт не способен покраснеть "+
			"на внутрипакетном вызове пересчёта", len(got), got)
	}
	if !strings.Contains(got[0], "::BackfillOwnerBindings → ReseedSystemRoleVerbs") {
		t.Errorf("находка не приписана объемлющей функции: %q", got[0])
	}
}

// TestReseedWiring_InjectionSilentOnANeighbouringSeedCall — законный близнец:
// тот же пакет зовёт ДРУГОЙ свой досев. Это не предмет оси, и гейт молчит.
func TestReseedWiring_InjectionSilentOnANeighbouringSeedCall(t *testing.T) {
	entries := map[string]bool{"ReseedSystemRoleVerbs": true}
	got, err := roleVerbReseedCallsInside("migrate_backfill.go", injSeedNeighbourCallSrc, entries)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("гейт покраснел на вызове СОСЕДНЕГО досева: %v — ось запрещает "+
			"внутрипакетный вызов ПЕРЕСЧЁТА, а не всякий вызов вообще", got)
	}
}

// TestReseedWiring_InjectionSilentOnTheDeclarationItself — объявление самой точки
// входа вызовом не является: иначе гейт краснел бы всегда, включая верное дерево.
func TestReseedWiring_InjectionSilentOnTheDeclarationItself(t *testing.T) {
	src := `package seed

func ReseedSystemRoleVerbs(ctx context.Context, repo kachorepo.Repository) error {
	return nil
}
`
	entries := map[string]bool{"ReseedSystemRoleVerbs": true}
	got, err := roleVerbReseedCallsInside("role_verb_reseed.go", src, entries)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("объявление точки входа принято за её вызов: %v — гейт краснел бы "+
			"на любом дереве, где пересчёт вообще существует", got)
	}
}

// TestReseedWiring_EntryPointDetectionSeparatesExportedFromHelpers — точка входа
// опознаётся ЭКСПОРТИРОВАННЫМ именем: неэкспортированный помощник с тем же
// корнем в имени точкой входа не является, иначе перепись насчитала бы предметов
// больше, чем их есть, и «ноль внутрипакетных вызовов» стало бы недостижимым.
func TestReseedWiring_EntryPointDetectionSeparatesExportedFromHelpers(t *testing.T) {
	src := `package seed

func ReseedSystemRoleVerbs(ctx context.Context) error { return nil }

func replaceRoleVerbsInOwnTx(ctx context.Context) error { return nil }

func BackfillOwnerBindings(ctx context.Context) error { return nil }

func (s *sweeper) ReseedRoleVerbsMethod(ctx context.Context) error { return nil }
`
	got, err := exportedRoleVerbEntryPointsIn("role_verb_reseed.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 1 || got[0] != "ReseedSystemRoleVerbs" {
		t.Fatalf("точек входа %v, а обязана быть одна — `ReseedSystemRoleVerbs`.\n"+
			"Неэкспортированный помощник и метод с получателем точкой входа пакета "+
			"не являются: их зовут изнутри по построению, и находка на них была бы ложной.", got)
	}
}

// TestReseedWiring_EntryPointDetectionIsEmptyWithoutTheSubject — предпосылка:
// файл без пересчёта точек входа не даёт, и гейт обязан на этом ОТКАЗАТЬ, а не
// молчать. Здесь проверяется сам признак; отказ — в теле гейта.
func TestReseedWiring_EntryPointDetectionIsEmptyWithoutTheSubject(t *testing.T) {
	src := `package seed

func BackfillOwnerBindings(ctx context.Context) error { return nil }
`
	got, err := exportedRoleVerbEntryPointsIn("migrate_backfill.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("точки входа найдены там, где предмета нет: %v", got)
	}
}
