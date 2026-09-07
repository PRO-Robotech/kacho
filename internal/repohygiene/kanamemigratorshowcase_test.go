// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
	"github.com/PRO-Robotech/kacho/internal/repohygiene"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// kanameShowcaseRepoRoot — корень репозитория, найденный ПОДЪЁМОМ до маркера
// `go.mod`, а не счётом `..`: фиксированное число верно ровно для той раскладки,
// в которой его написали, и указывает наружу репозитория во всякой другой.
func kanameShowcaseRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("рабочий каталог не прочитан: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod не найден выше %s — корень дерева взять неоткуда", dir)
		}
		dir = parent
	}
}

// kanameShowcaseCorpus — корпус витрины, собранный ОБХОДОМ ИНДЕКСА git.
//
// Индекса, а не диска: обход диска читал бы игнорируемое — распаковки чартов,
// рабочие копии, отчёты прогонов, — и «находка» приходила бы из того, чего в
// дереве нет.
//
// Пустой обход под любым корнем — ОТКАЗ, а не тихий пропуск: корень витрины без
// единого файла означает, что поверхность переехала, а гейт этого не заметил.
func kanameShowcaseCorpus(t *testing.T) map[string]string {
	t.Helper()
	root := kanameShowcaseRepoRoot(t)
	files := map[string]string{}
	for _, dir := range repohygiene.KanameMigratorShowcaseRoots {
		abs, err := treecorpus.Under(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("состав витрины под %s НЕ ИЗМЕРЕН (%v) — пустой корпус здесь "+
				"дал бы зелёный прогон с нулём осмотренного", dir, err)
		}
		if len(abs) == 0 {
			t.Fatalf("под %s НЕТ ни одного отслеживаемого файла — поверхность витрины "+
				"переехала, а гейт этого не заметил", dir)
		}
		for _, p := range abs {
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				t.Fatalf("относительный путь для %s: %v", p, relErr)
			}
			body, readErr := os.ReadFile(p)
			if readErr != nil {
				t.Fatalf("файл витрины %s не прочитан: %v", rel, readErr)
			}
			files[filepath.ToSlash(rel)] = string(body)
		}
	}
	return files
}

// TestKanameShowcaseNamesItsOwnMigrator — витрина Kaname не называет чужой
// накатчик.
//
// Что именно утверждается: имя, которое арендатор ЧИТАЕТ в документе установки
// и НАБИРАЕТ по спецификации пода, есть имя накатчика ЭТОГО продукта. Не «в
// дереве нет чужого слова» — внутренние упоминания законны и изъяты поимённо с
// причиной (см. перепись).
func TestKanameShowcaseNamesItsOwnMigrator(t *testing.T) {
	files := kanameShowcaseCorpus(t)
	findings, census := repohygiene.KanameMigratorShowcaseScan(files)

	if census.FilesRead == 0 {
		t.Fatal("прочитано НОЛЬ файлов витрины — «ноль находок» здесь неотличимо " +
			"от «ноль прочитанного»")
	}
	if census.TokensSeen == 0 {
		t.Fatal("на витрине НЕ ВСТРЕЧЕНО ни одного токена формы `…migrator` — " +
			"распознаватель либо ослеп, либо витрина перестала называть накат вовсе; " +
			"и то и другое означает, что зелёный прогон ничего не утверждает")
	}

	reasons := make([]string, 0, len(census.ExemptReasons))
	for r := range census.ExemptReasons {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)

	t.Logf("перепись витрины: корней %d, отслеживаемых файлов %d, изъято %d, прочитано %d",
		len(repohygiene.KanameMigratorShowcaseRoots), census.FilesTracked,
		census.FilesExempt, census.FilesRead)
	for _, r := range reasons {
		t.Logf("  изъято %3d — %s", census.ExemptReasons[r], r)
	}
	t.Logf("перепись имён: токенов формы `…migrator` встречено %d, признано именем "+
		"накатчика продукта %d, из них своих %d",
		census.TokensSeen, census.TokensJudged, census.TokensOwn)

	for _, f := range findings {
		t.Errorf("%s", f)
	}
	if len(findings) > 0 {
		t.Logf("накатчик этого продукта — %q (владелец имени: internal/productnaming)",
			productnaming.MigratorBinary("iam"))
	}
}
