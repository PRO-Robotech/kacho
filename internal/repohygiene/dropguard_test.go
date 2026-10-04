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

	"github.com/PRO-Robotech/corelib/dropguard"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// chainDir — каталог цепочки миграций и служба, чью цепочку он несёт.
type chainDir struct {
	Service string
	Label   string // служба, а у строки таблицы цепочек — служба/база
	Dir     string // абсолютный путь
}

// migrationDirs — каждая цепочка миграций дерева у ЕДИНСТВЕННОГО её вывода,
// [migrationchains.List] (kacho#2915, CX1-114): каталог цепочки из имени службы
// не выводится — у services/notify цепочка пробы лежит в
// internal/probemigrations, и вывод из имени её не видел бы. Список
// ВЫЧИСЛЯЕТСЯ, а не перечисляется; отказ перечня (точек нет, таблица без
// строк, цепочка без точки) — провал, а не «чисто». Печатает число цепочек по
// services/notify и краснеет на нуле.
func migrationDirs(t *testing.T, root string) []chainDir {
	t.Helper()
	chains, err := migrationchains.List(root)
	if err != nil {
		t.Fatalf("перечень цепочек дерева: %v — этот гейт не утверждал бы ничего", err)
	}
	out := make([]chainDir, 0, len(chains))
	notify := 0
	for _, c := range chains {
		label := c.Service
		if c.Database != "" {
			label = c.Service + "/" + c.Database
		}
		if c.Service == "notify" {
			notify++
		}
		out = append(out, chainDir{Service: c.Service, Label: label, Dir: filepath.Join(root, filepath.FromSlash(c.Dir))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	t.Logf("цепочек осмотрено %d, из них по services/notify %d", len(out), notify)
	if notify == 0 {
		t.Fatalf("по services/notify цепочек 0 — точка наката каталога не видна перечню")
	}
	return out
}

// TestEveryDropIsDeclaredWithANumber — статическая половина.
//
// Проваливается на: дропе без записи (число не названо), записи без дропа
// (исключение пережило свой предмет), несовпадении вида с тем, что миграция реально
// делает, и на ненулевом ожидании, под которое в миграциях нет ни одного INSERT.
func TestEveryDropIsDeclaredWithANumber(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dirs := migrationDirs(t, root)

	totalFiles, totalDrops, totalDecls := 0, 0, 0
	for _, cd := range dirs {
		svc, dir := cd.Service, cd.Dir
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
	t.Logf("census: %d chain(s), %d migration file(s), %d Up-section DROP TABLE statement(s), %d declaration(s)",
		len(dirs), totalFiles, totalDrops, totalDecls)
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
