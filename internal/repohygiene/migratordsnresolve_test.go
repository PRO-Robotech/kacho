// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordsnresolve_test.go — гейт: приоритет источников DSN объявлен один раз.
//
// Предмет, требования, перечень признаков и граница разобраны в шапке
// migratordsnresolve.go — здесь они не пересказываются, чтобы не завести двух
// мест об одном предмете.
//
// Доказательство способности упасть и смолчать — в
// migratordsnresolve_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMigratorEntryPointsDoNotResolveDSNThemselves(t *testing.T) {
	root := repoRoot(t)

	census, findings, notDelegating, err := auditMigratorDSNResolve(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Log(census.String())

	// Страж собственной предпосылки: обход, ничего не прочитавший, обязан
	// ронять прогон — иначе «ноль находок» неотличимо от «ноль прочитанного».
	if census.TractFiles == 0 {
		t.Fatalf("обход пуст: файлов тракта наката прочитано 0 — вердикт беспредметен. %s",
			census.String())
	}
	if census.EntryPoints == 0 {
		t.Fatalf("точек наката найдено 0 — положительная половина проверяет пустое "+
			"множество. %s", census.String())
	}

	// Предпосылка ОБОИХ признаков. Переименуй общий резолв — и положительная
	// половина не найдёт ни одной делегирующей точки, объявив нарушителями всех
	// семь. Переименуй константу переменной — и отрицательная половина ослепнет
	// на литерале, продолжая печатать «ноль находок».
	if !census.PremiseResolve {
		t.Fatalf("общий пакет %s не объявляет %s: положительная половина судила бы "+
			"по имени, которого нет, — почини премису, а не перечень нарушителей",
			migratorDSNSharedImport, migratorDSNResolveFunc)
	}
	if !census.PremiseEnv {
		t.Fatalf("общий пакет %s не объявляет %s со значением %q: отрицательная "+
			"половина ослепла бы на чтении, записанном литералом",
			migratorDSNSharedImport, migratorDSNEnvConst, migratorDSNEnvName)
	}

	// ПОЛОЖИТЕЛЬНАЯ половина: каждая точка наката делегирует.
	if len(notDelegating) > 0 {
		for _, dir := range notDelegating {
			t.Errorf("%s: точка наката НЕ зовёт %s.%s — значит держит свой порядок "+
				"источников DSN либо не имеет его вовсе. Порядок один на семь точек "+
				"(--dsn > %s > конфигурация сервиса); запасная конфигурация сервиса "+
				"передаётся замыканием и остаётся законной (%s)",
				dir, migratorDSNSharedImport, migratorDSNResolveFunc,
				migratorDSNEnvName, migratorTractDecisionDoc)
		}
	}

	// ОТРИЦАТЕЛЬНАЯ половина.
	for _, text := range sortedMigratorDSNTexts(findings) {
		t.Error(text)
	}
}

// auditMigratorDSNResolve читает корпус и возвращает перепись с находками.
// Вынесен из пробы, чтобы инъекция звала ТО ЖЕ, что и гейт.
func auditMigratorDSNResolve(root string) (migratorDSNCensus, []migratorDSNFinding, []string, error) {
	var (
		census   migratorDSNCensus
		findings []migratorDSNFinding
	)
	delegating := map[string]bool{}
	entryDirs := map[string]bool{}

	for _, dir := range []string{"pkg", "services"} {
		paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, dir), ".go")
		if err != nil {
			return census, nil, nil, err
		}
		for _, p := range paths {
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return census, nil, nil, rerr
			}
			rel = filepath.ToSlash(rel)

			// Пробы из корпуса исключены намеренно: проба, утверждающая порядок
			// источников, обязана этот порядок воспроизводить — иначе она не
			// проверяет ничего.
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			shared := migratorTractIsShared(rel)
			if !shared && !migratorTractIsEntryPoint(rel) {
				continue
			}

			src, rerr := os.ReadFile(p) // #nosec G304 -- обход собственного дерева
			if rerr != nil {
				return census, nil, nil, rerr
			}
			facts, ferr := migratorDSNFactsOf(rel, string(src))
			if ferr != nil {
				return census, nil, nil, ferr
			}

			census.FilesRead++
			if shared {
				census.SharedFiles++
				if facts.DeclaresResolve {
					census.PremiseResolve = true
				}
				if facts.DeclaresEnvName && facts.DeclaredEnvValue == migratorDSNEnvName {
					census.PremiseEnv = true
				}
				continue
			}

			census.TractFiles++
			if entry := migratorDSNEntryDir(rel); entry != "" {
				entryDirs[entry] = true
				if facts.CallsSharedResolve {
					delegating[entry] = true
				}
			}
			for _, read := range facts.OwnEnvReads {
				census.OwnEnvReads++
				findings = append(findings, migratorDSNFinding{Rel: rel, What: read})
			}
		}
	}

	census.EntryPoints = len(entryDirs)
	census.Delegating = len(delegating)

	// Перечень неделегирующих собирается ЗДЕСЬ, из того же обхода: второй проход
	// по диску брал бы состав мимо индекса — под services/ на всякой машине, где
	// поднимали стенд, лежит игнорируемое, и корпус двух проходов разошёлся бы.
	notDelegating := make([]string, 0, len(entryDirs))
	for dir := range entryDirs {
		if !delegating[dir] {
			notDelegating = append(notDelegating, dir)
		}
	}
	sort.Strings(notDelegating)
	return census, findings, notDelegating, nil
}
