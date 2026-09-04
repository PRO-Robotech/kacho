// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dropguard_test.go — репо-широкий гейт: ни одна таблица не роняется по рассуждению.
//
// Каждая таблица, которую этот репозиторий когда-либо дропал, дропалась под абзац
// прозы в миграции: «сюда ничего не сеялось», «тенант сюда не писал», «связующая
// таблица ушла раньше, а не переносилась». Каждое из этих утверждений было верным в
// момент написания. Ни одно из них не проверяется — значит, ни одно не остаётся
// верным само по себе, и когда одно перестанет быть верным, об этом никто не узнает:
// миграция всё так же выполнится, и строки всё так же уйдут.
//
// Гейт требует по каждому DROP TABLE в Up-секции ЧИСЛО — ожидаемое количество
// уничтожаемых строк — в `dropguard.json` рядом с миграциями. Здесь проверяется
// статическая половина (объявлено / не протухло / вид совпадает с тем, что миграция
// делает / ненулевое ожидание обосновано INSERT'ом в самих миграциях). Само число
// сверяется с базой в измеряющих гейтах `services/*/internal/migrations`.
//
// Почему репо-широко, а не в одном сервисе: класс измеряется по дереву, а не по
// диффу, в котором его заметили. На момент написания — 28 DROP TABLE в 5 сервисах,
// и ни один из них не был подкреплён числом.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/dropguard"
)

// migrationDirs — каждая директория миграций в дереве. Список ВЫЧИСЛЯЕТСЯ, а не
// перечисляется: сервис, добавленный завтра, попадает под гейт сам, без правки
// этого файла. Пустой результат — провал, а не «чисто».
func migrationDirs(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("read %s: %v", servicesDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(servicesDir, e.Name(), "internal", "migrations")
		if st, serr := os.Stat(dir); serr == nil && st.IsDir() {
			out[e.Name()] = dir
		}
	}
	if len(out) == 0 {
		t.Fatalf("no migration directories found under %s — this gate would assert nothing", servicesDir)
	}
	return out
}

// TestEveryDropIsDeclaredWithANumber — статическая половина.
//
// Проваливается на: дропе без записи (число не названо), записи без дропа
// (исключение пережило свой предмет), несовпадении вида с тем, что миграция реально
// делает, и на ненулевом ожидании, под которое в миграциях нет ни одного INSERT.
func TestEveryDropIsDeclaredWithANumber(t *testing.T) {
	root := repoRoot(t)
	dirs := migrationDirs(t, root)

	services := make([]string, 0, len(dirs))
	for svc := range dirs {
		services = append(services, svc)
	}
	sort.Strings(services)

	totalFiles, totalDrops, totalDecls := 0, 0, 0
	for _, svc := range services {
		dir := dirs[svc]
		inv, err := dropguard.Inventory(svc, os.DirFS(dir))
		if err != nil {
			t.Errorf("%s: %v", svc, err)
			continue
		}
		totalFiles += inv.FilesScanned
		totalDrops += len(inv.Drops)

		manifestPath := filepath.Join(dir, dropguard.ManifestName)
		m, merr := dropguard.LoadManifest(manifestPath)
		if merr != nil {
			if len(inv.Drops) == 0 && os.IsNotExist(underlying(merr)) {
				continue // сервис ничего не дропал — объявлять нечего
			}
			rel, relErr := filepath.Rel(root, dir)
			if relErr != nil {
				rel = dir
			}
			t.Errorf("%s: %d drop(s) in %s and no readable %s: %v",
				svc, len(inv.Drops), rel, dropguard.ManifestName, merr)
			continue
		}
		totalDecls += len(m.Drops)
		if m.Service != svc {
			t.Errorf("%s: %s declares service %q", svc, dropguard.ManifestName, m.Service)
		}
		for _, v := range dropguard.Reconcile(inv, m) {
			t.Errorf("%s", v.Error())
		}
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if totalFiles == 0 {
		t.Fatal("zero migration files were read across the whole tree — this gate asserted nothing")
	}
	if totalDrops == 0 {
		t.Fatalf("zero DROP TABLE statements found across %d migration files — either the tree stopped dropping tables, or the parser stopped reading them; both are findings", totalFiles)
	}
	t.Logf("census: %d service(s), %d migration file(s), %d Up-section DROP TABLE statement(s), %d declaration(s)",
		len(services), totalFiles, totalDrops, totalDecls)
}

func underlying(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		next := u.Unwrap()
		if next == nil {
			return err
		}
		err = next
	}
}
