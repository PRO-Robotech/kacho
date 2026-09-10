// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// TestMigrationNoticesReachTheOperator — уведомления сервера доезжают до того,
// кто читает вывод наката.
//
// Предмет, три половины и почему разбор, а не подстрока — в шапке
// migratornoticedelivery.go.
func TestMigrationNoticesReachTheOperator(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	census, findings, err := auditMigratorNotice(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Log(census.String())

	// Страж собственной предпосылки. Обход, ничего не прочитавший, обязан
	// ронять прогон: иначе «ноль находок» неотличимо от «ноль прочитанного».
	if census.HomeFiles == 0 {
		t.Fatalf("обход пуст: файлов общего тракта наката прочитано 0 — вердикт беспредметен. %s",
			census.String())
	}

	// ПОЛОЖИТЕЛЬНАЯ половина. Без неё отрицания ниже проверяют пустое множество:
	// приёмник переименован либо унесён, искать нечего, и молчание выглядит
	// исправной работой.
	if !census.SharedRelay || !census.SharedCtor || !census.SharedHandler {
		t.Errorf("общий пакет %s не несёт доставки уведомлений целиком "+
			"(тип %s %v, конструктор %s %v, обработчик %s задан %v). "+
			"Почини премису, а не перечень: без неё отрицания ниже вакуумны. Целевая форма — %s",
			migratorSharedTractHome,
			migratorNoticeRelayType, census.SharedRelay,
			migratorNoticeCtor, census.SharedCtor,
			migratorNoticeField, census.SharedHandler,
			migratorTractDecisionDoc)
	}
	if !census.OpenTakesRelay {
		t.Errorf("%s%s не требует приёмник ПАРАМЕТРОМ. Именно параметр делает «забыть» "+
			"невыразимым: умолчание или ручка вернули бы состояние, в котором накат "+
			"выходит нулём, а сказанное сервером не видит никто",
			migratorSharedTractHome, migratorDBOpenFunc)
	}
	// Словарь форм открытия обязан находить хоть что-то: распознаватель, не
	// узнающий НИ ОДНОЙ формы, молчит на всём — и его молчание неотличимо от
	// исправного дерева.
	if census.OpenersSeen == 0 {
		t.Errorf("ни одна из %d форм открытия соединения в общем тракте не найдена — "+
			"словарь разошёлся с деревом, и половина «открыл — провяжи» проверяет "+
			"пустое множество", len(migratorNoticeOpeners))
	}
	if census.RelaysBuilt == 0 {
		t.Errorf("приёмник не собирается НИГДЕ в прод-коде — доставка объявлена и не " +
			"провязана, а половина про назначение проверяет пустое множество")
	}

	// ОТРИЦАТЕЛЬНЫЕ половины.
	for _, text := range sortedNoticeFindingTexts(findings) {
		t.Error(text)
	}
}

// auditMigratorNotice читает корпус и возвращает перепись с находками.
// Вынесен из пробы, чтобы инъекция звала ТО ЖЕ, что и гейт.
func auditMigratorNotice(root string) (migratorNoticeCensus, []migratorTractFinding, error) {
	var (
		census   migratorNoticeCensus
		findings []migratorTractFinding
	)

	// Два прохода: сперва читаем факты, потом судим. Половина «открыл — провяжи»
	// спрашивает про КАТАЛОГ, а не про файл, — иначе она краснела бы на законном
	// расщеплении, где обработчик задаёт сосед по пакету.
	type read struct {
		rel  string
		home bool
		src  migratorNoticeSource
	}
	var files []read
	wired := map[string]bool{}

	for _, dir := range []string{"pkg", "services"} {
		paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, dir), ".go")
		if err != nil {
			return census, nil, err
		}
		for _, path := range paths {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return census, nil, rerr
			}
			rel = filepath.ToSlash(rel)

			// Пробы исключены: проба вправе поднимать свой Postgres и вправе
			// собирать приёмник в буфер — именно этим она его и проверяет.
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			home := migratorNoticeIsHome(rel)
			if !home && !migratorTractIsEntryPoint(rel) {
				continue
			}

			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return census, nil, rerr
			}
			facts, ferr := readMigratorNoticeSource(rel, string(src))
			if ferr != nil {
				return census, nil, ferr
			}

			census.FilesRead++
			if home {
				census.HomeFiles++
			} else {
				census.TractFiles++
			}
			if facts.SetsHandler {
				wired[filepath.ToSlash(filepath.Dir(rel))] = true
			}
			files = append(files, read{rel: rel, home: home, src: facts})
		}
	}

	for _, f := range files {
		if f.home {
			census.SharedRelay = census.SharedRelay || f.src.DeclaresRelay
			census.SharedCtor = census.SharedCtor || f.src.DeclaresCtor
			census.SharedHandler = census.SharedHandler || f.src.SetsHandler
			census.OpenTakesRelay = census.OpenTakesRelay || f.src.OpenTakesRelay
			census.OpenersSeen += len(f.src.Opens)
		}
		census.RelaysBuilt += len(f.src.Sinks)

		found := migratorNoticeJudge(f.rel, f.src, wired[filepath.ToSlash(filepath.Dir(f.rel))])
		for _, fd := range found {
			if strings.Contains(fd.What, "открывает соединение") {
				census.OpenersUnwired++
			} else {
				census.SinksElsewhere++
			}
		}
		findings = append(findings, found...)
	}

	return census, findings, nil
}
