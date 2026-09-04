// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationversionunique_test.go — ЕДИНСТВЕННЫЙ гейт против двух миграций с
// одним номером в одном каталоге миграций.
//
// # Почему это не косметика
//
// Номер миграции — не имя, а ключ: сервис хранит в своей базе строку «версия N
// применена». Две разные миграции с одним N — это два разных изменения схемы
// под одним ключом, и различить их база уже не может. Инструмент миграций не
// пытается: он ПАДАЕТ ПАНИКОЙ при сборе списка, до подключения к базе. Паника
// случается в init-контейнере, поэтому сервис не поднимается вовсе.
//
// # Почему этого не ловило ничто
//
// Столкновение рождается не в ветке, а в СЛИЯНИИ: две ветки независимо взяли
// следующий свободный номер, каждая у себя была права, конфликта содержимого
// нет (файлы разные), поэтому слияние проходит чисто и оба файла оказываются
// рядом. Ни компилятор, ни обзор диффа этого не видят — виден только каталог
// целиком, а его никто не смотрит целиком.
//
// НАБЛЮДАВШЕЕСЯ СЛЕДСТВИЕ (2026-07-30). На стволе одновременно оказались
// `0024_instances_cursor_index.sql` и `0024_drop_decoy_pending_idx.sql` в
// машинах и такая же пара под номером 0012 в хранении. Оба сервиса ушли в цикл
// падений init-контейнера, и стенд поднять было нельзя. Причём номер 0024 в
// базе уже стоял применённым — за ним стояла ДРУГАЯ миграция, приехавшая
// раньше; то есть новая работа молча претендовала на ключ, который уже занят и
// уже что-то значит.
//
// # Почему каталоги ВЫВОДЯТСЯ из дерева, а не выписываются путём (#567)
//
// До 2026-08-17 предмет держали ДВА гейта, и оба брали каталоги выпиской
// `services/*/internal/migrations`. Мимо проходили `pkg/migrations/common` и
// `services/compute/migrations` — пять файлов из 268, — а перепись при этом
// печатала «файлов миграций 263, находок 0». То есть проверка утверждала
// уникальность, которой не проверяла, и расширение выписки этого не лечит:
// следующий каталог снова окажется невидим молча.
//
// Здесь каталог — СВОЙСТВО дерева: место, где лежит хотя бы один файл вида
// `<номер>_<что>.sql`, взятое из индекса git (`treecorpus`). Новый сервис,
// новый общий каталог, переезд раскладки — попадают под надзор в тот же день,
// без правки этого файла.
//
// Двух гейтов на один предмет тоже больше нет. Пока их было два, они
// печатали две переписи и два разных отказа об одном и том же; сходились они
// сегодня — и именно поэтому расхождение между ними было бы тихим.
//
// # Что делать при отказе
//
// Переномеровать НОВУЮ миграцию; ту, что уже применена на живой базе, не
// трогать — её ключ там записан (запрет «не редактировать применённую
// миграцию»). Номер новой миграции выводится из номера задачи, а не выбирается
// из каталога, — см. docs/architecture/migration-version-namespace.md.
//
// Доказательство способности упасть — в migrationversionunique_injection_test.go.
package repohygiene

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// migrationVersionCollision — находка: один ключ версии занят двумя и более
// файлами одного каталога. Находка без координаты не действие, поэтому она
// несёт каталог, номер и ВСЕ имена, которые на него претендуют.
type migrationVersionCollision struct {
	Dir     string
	Version int64
	Files   []string
}

func (c migrationVersionCollision) String() string {
	return fmt.Sprintf(
		"%s: номер %d занят %d раза(-ами) — %s.\n"+
			"    Для каталога, который применяет мигратор, это отказ на сборе списка "+
			"(паника до подключения к базе): под уходит в перезапуск инициализации, "+
			"сервис не поднимается вовсе.\n"+
			"    Для каталога, который мигратор не применяет (общая база, источник схемы "+
			"для генератора), паники не будет — но ключ всё равно занят дважды, и процедура "+
			"выбора номера там та же.\n"+
			"    Переномеруй НЕПРИМЕНЁННУЮ сторону: применённую миграцию править нельзя, "+
			"её ключ уже записан в базе. Номер ВЫВОДИТСЯ, а не выбирается из каталога: "+
			"действующая форма — метка времени заведения `YYYYMMDDHHMMSS_<что>.sql`, "+
			"объявлена в docs/architecture/migration-version-namespace.md.",
		c.Dir, c.Version, len(c.Files), strings.Join(c.Files, " и "))
}

// migrationDirCount — сколько файлов миграций прочитано в одном каталоге.
type migrationDirCount struct {
	Dir   string
	Files int
}

// migrationUniqueCensus — объём осмотренного. Отдельное утверждение, а не
// примечание в логе: «ноль находок» обязано быть отличимо от «ноль
// прочитанного», и разложение по каталогам делает видимым именно тот дефект,
// ради которого гейт переписан, — каталог, которого обход не увидел.
type migrationUniqueCensus struct {
	Dirs  int
	Files int
	ByDir []migrationDirCount
}

func (c migrationUniqueCensus) String() string {
	parts := make([]string, 0, len(c.ByDir))
	for _, d := range c.ByDir {
		parts = append(parts, fmt.Sprintf("%s=%d", d.Dir, d.Files))
	}
	return fmt.Sprintf("каталогов миграций %d, файлов миграций %d (%s)",
		c.Dirs, c.Files, strings.Join(parts, ", "))
}

// findMigrationVersionCollisions — разбор состава дерева.
//
// Вход — пути ОТНОСИТЕЛЬНО корня, слэш-разделённые: у настоящего дерева их даёт
// индекс git (свойство КОММИТА, а не рабочего каталога), у инъекции — тот же
// список с подложенной строкой. Разбор ОДИН, поэтому фикстура не может
// оказаться снисходительнее того, что судит настоящее дерево.
//
// Имя файла разбирает [migrationVersionFileRe] — та же регулярка, которой
// пользуется разбор пространства номеров. Двух разборов одного имени в дереве
// не заводим: они разъедутся, и разъедутся молча.
func findMigrationVersionCollisions(rel []string) (migrationUniqueCensus, []migrationVersionCollision) {
	byDir := map[string]map[int64][]string{}
	counts := map[string]int{}

	for _, r := range rel {
		base := path.Base(r)
		m := migrationVersionFileRe.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		// Ведущие нули — часть записи, а не значения: `0024` и `24` для
		// инструмента ОДИН ключ, поэтому сравнивается разобранное ЗНАЧЕНИЕ.
		v, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		dir := path.Dir(r)
		if byDir[dir] == nil {
			byDir[dir] = map[int64][]string{}
		}
		byDir[dir][v] = append(byDir[dir][v], base)
		counts[dir]++
	}

	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	census := migrationUniqueCensus{Dirs: len(dirs)}
	var out []migrationVersionCollision
	for _, d := range dirs {
		census.Files += counts[d]
		census.ByDir = append(census.ByDir, migrationDirCount{Dir: d, Files: counts[d]})

		versions := make([]int64, 0, len(byDir[d]))
		for v := range byDir[d] {
			versions = append(versions, v)
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
		for _, v := range versions {
			names := byDir[d][v]
			if len(names) > 1 {
				sort.Strings(names)
				out = append(out, migrationVersionCollision{Dir: d, Version: v, Files: names})
			}
		}
	}
	return census, out
}

// TestMigrationVersionsAreUniquePerDirectory — ключ версии живёт в КАТАЛОГЕ
// миграций (это один набор, применяемый одним инструментом к одной базе).
// Одинаковый номер в разных каталогах — норма и находкой не является.
func TestMigrationVersionsAreUniquePerDirectory(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}
	census, collisions := findMigrationVersionCollisions(tree.SortedFiles())

	t.Logf("перепись: файлов в дереве %d, %s, находок %d",
		tree.Count(), census, len(collisions))

	// ПРЕДПОСЫЛКИ. Каждая — факт о дереве, который может измениться и сделать
	// «ноль находок» бессмысленным; поэтому гейт проверяет их сам.
	if tree.Count() == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: индекс дерева пуст — прочитано ноль файлов, " +
			"и любое «ноль находок» ниже ничего не значит")
	}
	if census.Dirs == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: в дереве не нашлось ни одного каталога с файлами " +
			"вида <номер>_<что>.sql — либо раскладка изменилась, либо соглашение об " +
			"именах; пока это так, гейт не проверяет ничего")
	}
	if census.Files == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: каталогов %d, а файлов миграций ноль — обход сломан",
			census.Dirs)
	}

	for _, c := range collisions {
		t.Error(c.String())
	}
}
