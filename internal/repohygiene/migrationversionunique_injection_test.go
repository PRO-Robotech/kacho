// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationversionunique_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) верни дефект — разбор краснеет и НАЗЫВАЕТ координату (каталог, номер, оба
// имени);
// (б) поставь рядом ЗАКОННУЮ конструкцию той же формы — разбор молчит.
//
// Без (б) гейт ловил бы форму, а не существо: «два файла, начинающихся с цифр» —
// нормальное состояние любого каталога миграций, и первое же ложное
// срабатывание его отключило бы.
//
// # Почему инъекция идёт в НАСТОЯЩИЙ состав дерева, а не в синтетический
//
// Предмет задачи #567 — не разбор имени, а ОХВАТ: прежний обход брал каталоги
// выпиской пути и потому не видел пяти файлов из 268. Синтетическое дерево про
// охват не утверждает ничего — оно содержит ровно то, что положила проба.
// Поэтому здесь берётся настоящий состав из индекса git, и дубль подкладывается
// именно в каталог, которого ПРЕЖНЯЯ выписка не видела: доказательство обязано
// бить в закрытую слепую зону, а не «куда-нибудь».
package repohygiene

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// previousGlobShapeRe — форма пути, которую видела ПРЕЖНЯЯ выписка
// (`services/*/internal/migrations`). Здесь она нужна ровно затем, чтобы
// выбрать каталог ВНЕ неё, и больше нигде в надзоре не участвует.
var previousGlobShapeRe = regexp.MustCompile(`^services/[^/]+/internal/migrations$`)

// realCorpus — состав настоящего дерева. Отказ, а не пропуск: без него проба
// ничего не доказывает.
func realCorpus(t *testing.T) []string {
	t.Helper()
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}
	return tree.SortedFiles()
}

// dirPreviouslyInvisible — каталог миграций, которого не видела прежняя выписка
// И который уже несёт номер, подкладываемый инъекцией.
//
// ВТОРОЕ УСЛОВИЕ — НЕ ПРИДИРКА, И ОНО ИЗМЕРЕНО. Обе инъекции ниже подкладывают
// файл с номером `1` (в формах `0001_…` и `1_…`) и требуют СТОЛКНОВЕНИЯ. Такое
// требование осмысленно ровно тогда, когда номер `1` в целевом каталоге уже
// занят; в каталоге, чья нумерация начинается с номера задачи, столкновению
// взяться неоткуда — и проба, ничего не доказав, покраснела бы на исправном
// разборе.
//
// Предпосылка была НЕЯВНОЙ и держалась тем, что все каталоги миграций дерева
// происходили из порядковой эры. Первый каталог новой формы
// (`gateway/internal/idempotencypg/migrations`, #694, единственный файл
// `694001_…`) сортируется раньше остальных и стал целью — инъекция получила ноль
// находок на полностью исправном дереве. Условие сделано явным, а не обойдено
// перестановкой: следующий такой каталог иначе воспроизвёл бы то же самое.
//
// Если каталога, отвечающего ОБОИМ условиям, не осталось, слепая зона исчезла
// сама, и проба целится в любой каталог, который несёт номер `1`: утверждение о
// РАЗБОРЕ от этого не слабеет, а превращать исчезновение слепой зоны в красное
// было бы падением на достижении собственной цели.
func dirPreviouslyInvisible(t *testing.T, census migrationUniqueCensus, files []string) string {
	t.Helper()
	carriesVersionOne := map[string]bool{}
	for _, rel := range files {
		dir, base := path.Split(rel)
		dir = strings.TrimSuffix(dir, "/")
		if !strings.HasSuffix(base, ".sql") {
			continue
		}
		num := base[:strings.IndexByte(base+"_", '_')]
		if n, err := strconv.Atoi(num); err == nil && n == 1 {
			carriesVersionOne[dir] = true
		}
	}
	for _, d := range census.ByDir {
		if !previousGlobShapeRe.MatchString(d.Dir) && carriesVersionOne[d.Dir] {
			return d.Dir
		}
	}
	for _, d := range census.ByDir {
		if carriesVersionOne[d.Dir] {
			t.Logf("каталогов вне прежней выписки, несущих номер 1, в дереве нет — "+
				"инъекция целится в %s", d.Dir)
			return d.Dir
		}
	}
	if len(census.ByDir) == 0 {
		t.Fatal("в дереве нет ни одного каталога миграций — инъектировать некуда")
	}
	t.Fatalf("ни один каталог миграций не несёт номер 1, а обе инъекции ниже "+
		"подкладывают именно его и требуют столкновения: доказывать нечем.\n"+
		"Каталоги: %s", census)
	return ""
}

func TestMigrationVersionUnique_ProvenByInjection(t *testing.T) {
	files := realCorpus(t)
	baseCensus, baseFindings := findMigrationVersionCollisions(files)

	// КОНТРОЛЬ. Без него краснота ниже неотличима от красноты самого дерева.
	if len(baseFindings) != 0 {
		t.Fatalf("настоящее дерево уже несёт столкновение — инъекция ничего не докажет:\n%s",
			collisionsText(baseFindings))
	}
	t.Logf("контроль: настоящее дерево разобрано молча — %s", baseCensus)

	target := dirPreviouslyInvisible(t, baseCensus, files)

	t.Run("дубль в ранее невидимом каталоге — краснеет и называет координату", func(t *testing.T) {
		injected := append(append([]string{}, files...), target+"/0001_injected_duplicate.sql")
		census, found := findMigrationVersionCollisions(injected)
		if census.Files != baseCensus.Files+1 {
			t.Fatalf("перепись обязана расти на подложенный файл: было %d, стало %d",
				baseCensus.Files, census.Files)
		}
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d:\n%s", len(found), collisionsText(found))
		}
		msg := found[0].String()
		for _, want := range []string{target, "0001_injected_duplicate.sql"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("находка не называет координату (%q нет в тексте):\n%s", want, msg)
			}
		}
		if found[0].Version != 1 {
			t.Fatalf("находка называет не тот номер: %d", found[0].Version)
		}
	})

	t.Run("ведущие нули не создают двух ключей из одного", func(t *testing.T) {
		// `1_…` против существующего `0001_…` — для инструмента это ОДИН ключ.
		injected := append(append([]string{}, files...), target+"/1_injected_same_key.sql")
		_, found := findMigrationVersionCollisions(injected)
		if len(found) != 1 {
			t.Fatalf("`0001` и `1` — один ключ; ожидалась находка, получено %d:\n%s",
				len(found), collisionsText(found))
		}
	})

	t.Run("законная пара в том же каталоге — разбор молчит", func(t *testing.T) {
		// Свободный номер выведенной формы + файл-сосед с УЖЕ занятым номером,
		// но не миграция (описание рядом). Обе конструкции законны и обязаны
		// пройти, иначе отрицание выше зеленело бы на чём угодно.
		injected := append(append([]string{}, files...),
			target+"/567001_injected_legit.sql",
			target+"/0001_injected_notes.md",
		)
		census, found := findMigrationVersionCollisions(injected)
		if len(found) != 0 {
			t.Fatalf("ложное срабатывание на законной паре:\n%s", collisionsText(found))
		}
		if census.Files != baseCensus.Files+1 {
			t.Fatalf("не-.sql сосед не должен попадать в перепись миграций: было %d, стало %d",
				baseCensus.Files, census.Files)
		}
	})

	t.Run("один номер в РАЗНЫХ каталогах — не находка (ключ живёт в каталоге)", func(t *testing.T) {
		// Утверждение выводится из дерева, а не объявляется: номер 1 занят в
		// каждом каталоге миграций, и находок при этом ноль.
		var withOne int
		for _, d := range baseCensus.ByDir {
			for _, f := range files {
				if strings.HasPrefix(f, d.Dir+"/") &&
					migrationVersionFileRe.MatchString(f[len(d.Dir)+1:]) &&
					strings.HasPrefix(f[len(d.Dir)+1:], "0001_") {
					withOne++
					break
				}
			}
		}
		if withOne < 2 {
			t.Fatalf("предпосылка не выполняется: номер 1 занят лишь в %d каталоге(-ах) — "+
				"утверждение «одинаковый номер в разных каталогах законен» проверять не на чем", withOne)
		}
		t.Logf("номер 0001 занят в %d каталогах, находок ноль — ключ действительно per-каталог", withOne)
	})
}

func collisionsText(cs []migrationVersionCollision) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, "\n")
}
