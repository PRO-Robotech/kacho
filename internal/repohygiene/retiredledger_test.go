// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredledger_test.go — гейт надгробия сведённых миграций.
//
// Сведение каталога миграций в одну (решение владельца: прода нет, деструктивные
// операции допустимы) уничтожает ИМЕНА отдельных миграций. Имена эти рассеяны по
// живым документам обоих репозиториев как исторический след: «эту таблицу дропнула
// такая-то миграция». След верен и переписыванию не подлежит — но проверка свежести
// документации читает имя в обратных кавычках как утверждение о дереве и справедливо
// объявляет несуществующее находкой.
//
// Разрешает это надгробие — ведомость послаблений рядом с миграциями. И у всякой
// ведомости послаблений есть два способа стать хуже своего отсутствия:
//
//  1. прикрыть ЖИВУЮ координату — тогда проверка молчит там, где предмет есть;
//  2. пережить свои цитаты — тогда послабление не истечёт никогда.
//
// Здесь держится ПЕРВЫЙ: запись, называющая снятой миграцию, которая в каталоге
// есть, — находка. Второй держится проверкой свежести документации: она одна видит
// корпус обоих репозиториев и потому одна может сказать, называет ли запись ещё
// хоть кто-нибудь. Разделение по владельцу, а не по удобству: гейт дерева о
// цитатах в воркспейсе не знает by construction.
//
// Ноль надгробий — ПРОХОД, а не поломка: это состояние дерева до первого сведения
// и цель после того, как цитаты уйдут. Перепись печатается всегда, поэтому «ноль
// находок» отличимо от «ноль прочитанного».
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRetiredMigrationLedgerNeverCoversALiveMigration(t *testing.T) {
	root := repoRoot(t)
	dirs := migrationDirs(t, root)

	services := make([]string, 0, len(dirs))
	for svc := range dirs {
		services = append(services, svc)
	}
	sort.Strings(services)

	ledgers, entries, filesSeen := 0, 0, 0
	for _, svc := range services {
		dir := dirs[svc]
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: read %s: %v", svc, dir, err)
		}
		present := map[string]bool{}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			present[e.Name()] = true
			filesSeen++
		}

		l, err := ReadRetiredLedger(dir)
		if err != nil {
			rel, rerr := filepath.Rel(root, dir)
			if rerr != nil {
				rel = dir
			}
			t.Errorf("%s: %s: %v", svc, rel, err)
			continue
		}
		if l == nil {
			continue // каталог не сводили — объявлять нечего
		}
		ledgers++
		entries += len(l.Retired)
		for _, v := range RetiredLedgerViolations(svc, l, present) {
			t.Errorf("%s", v)
		}
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if filesSeen == 0 {
		t.Fatalf("во всех %d каталогах миграций не прочитано ни одного .sql — гейт не утверждал ничего",
			len(services))
	}
	t.Logf("перепись: каталогов миграций %d, файлов миграций %d, надгробий %d, записей в них %d",
		len(services), filesSeen, ledgers, entries)
}
