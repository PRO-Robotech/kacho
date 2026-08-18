// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «проба не пишет в дерево, из которого запущена»
// СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны и настоящими формами из дерева:
//
//	живой корень → запись файла            → краснеет, называет координату;
//	живой корень → изменяющая git-команда  → краснеет;
//	живой корень → помощник, который пишет → краснеет (шаг наружу);
//	живой корень → ЧТЕНИЕ                  → молчит (иначе запрет на гейты);
//	живой корень + запись во временный     → молчит;
//	копия дерева (`Rel` → `Join`)          → молчит;
//	запрещённая форма в комментарии        → молчит (читается код, не текст).
//
// Четвёртая и шестая строки — не украшение: обе формы В ДЕРЕВЕ ЕСТЬ, и на
// шестой первая редакция предиката дала ложную находку (считала `filepath.Rel`
// сохраняющим происхождение). Гейт, краснеющий на копии дерева, снимут первым.
package repohygiene

import "testing"

// Производитель живого корня. Форма снята с дерева: восхождение от рабочего
// каталога процесса до `go.mod`. Имя намеренно НЕ `repoRoot` — предикат выводит
// производителя из тела, а не из имени, и это здесь проверяется заодно.
const synthLiveRootProducer = `package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func treeTop(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("корень не найден")
		}
		dir = parent
	}
}
`

// ДЕФЕКТ 1 — запись файла по пути от живого корня.
const synthWritesLiveFile = `package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInjectsIntoTheLiveTree(t *testing.T) {
	root := treeTop(t)
	abs := filepath.Join(root, "ui-future/injected.test.tsx")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(abs) })
}
`

// ДЕФЕКТ 2 — изменяющая git-команда против живого репозитория, в том числе в
// форме «аргументы собраны строкой выше».
const synthMutatesLiveIndex = `package probe

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

func TestStagesIntoTheLiveIndex(t *testing.T) {
	root := treeTop(t)
	rels := []string{"ui-future/injected.test.tsx"}
	addArgs := append([]string{"-C", root, "add", "-f", "--"}, rels...)
	if out, err := gitenv.Command("", addArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}
`

// ДЕФЕКТ 3 — живой корень уезжает в помощника, и пишет уже он. Без шага наружу
// у запрета была бы дыра шириной в одну функцию.
const synthWritesViaHelper = `package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, root, rel, body string) {
	full := filepath.Join(root, rel)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestInjectsThroughAHelper(t *testing.T) {
	mustWrite(t, treeTop(t), "ui-future/injected.test.tsx", "x")
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — живой корень ЧИТАЕТСЯ, пишется временный каталог. Это
// форма подавляющего большинства гейтов дерева: запрет здесь был бы запретом на
// проверки.
const synthReadsLiveWritesTemp = `package probe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

func TestReadsLiveTreeWritesItsOwn(t *testing.T) {
	root := treeTop(t)
	if _, err := gitenv.Command(root, "ls-files", "-z").Output(); err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "copy.mod"), body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — копия дерева во временный каталог. Настоящая форма из
// дерева: относительный отрезок берётся от живого корня, а кладётся под
// временный. Первая редакция предиката краснела ровно здесь.
const synthCopiesTreeToTemp = `package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func copyOut(t *testing.T, src, dst string, files []string) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, path := range files {
		rel, err := filepath.Rel(src, path)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestCopiesTreeIntoItsOwnDir(t *testing.T) {
	copyOut(t, treeTop(t), t.TempDir(), []string{"go.mod"})
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — запрещённая форма в КОММЕНТАРИИ. Гейт, краснеющий на
// собственном объяснении, снимут первым.
const synthForbiddenFormInProse = `package probe

import "testing"

// Так делать нельзя:
//
//	root := treeTop(t)
//	os.WriteFile(filepath.Join(root, "x"), nil, 0o644)
//	gitenv.Command(root, "add", "--", "x")
//
// Поэтому проба работает со своим каталогом.
func TestExplainsTheBanWithoutCommittingIt(t *testing.T) {
	_ = t.TempDir()
}
`

func TestProbeWriteGateSeparatesLiveWritesFromReads(t *testing.T) {
	const (
		producerRel = "internal/synth/producer_test.go"
		defWrite    = "internal/synth/writes_live_test.go"
		defGit      = "internal/synth/mutates_index_test.go"
		defHelper   = "internal/synth/via_helper_test.go"
		twinRead    = "internal/synth/reads_live_test.go"
		twinCopy    = "internal/synth/copies_tree_test.go"
		twinProse   = "internal/synth/prose_test.go"
	)

	findings, census := auditProbeWritesToLiveTree(map[string]string{
		producerRel: synthLiveRootProducer,
		defWrite:    synthWritesLiveFile,
		defGit:      synthMutatesLiveIndex,
		defHelper:   synthWritesViaHelper,
		twinRead:    synthReadsLiveWritesTemp,
		twinCopy:    synthCopiesTreeToTemp,
		twinProse:   synthForbiddenFormInProse,
	})

	got := map[string][]liveWriteFinding{}
	for _, f := range findings {
		got[f.File] = append(got[f.File], f)
	}

	t.Run("запись по живому корню краснеет и называет координату", func(t *testing.T) {
		fs, ok := got[defWrite]
		if !ok {
			t.Fatal("запись в живое дерево гейтом НЕ поймана — это ровно тот образец, " +
				"ради которого гейт заведён")
		}
		if fs[0].What != "os.WriteFile" || fs[0].Line == 0 {
			t.Errorf("вердикт без координаты не приводит к правке: %+v", fs[0])
		}
	})

	t.Run("изменяющая git-команда против живого репозитория краснеет", func(t *testing.T) {
		fs, ok := got[defGit]
		if !ok {
			t.Fatal("`git add` по живому корню НЕ пойман — фантомная запись в индексе " +
				"осталась бы невидимой ровно для тех гейтов, что берут состав у индекса")
		}
		if fs[0].What != "git add" {
			t.Errorf("подкоманда названа неверно: %q", fs[0].What)
		}
	})

	t.Run("запись через помощника краснеет", func(t *testing.T) {
		if _, ok := got[defHelper]; !ok {
			t.Fatal("живой корень, уехавший в помощника, НЕ пойман — у запрета осталась " +
				"дыра шириной в одну функцию, а это самая частая форма")
		}
	})

	t.Run("чтение живого дерева молчит", func(t *testing.T) {
		if fs, ok := got[twinRead]; ok {
			t.Errorf("чтение живого дерева объявлено находкой (%+v) — запрет на чтение "+
				"был бы запретом на сами гейты: они только этим и заняты", fs)
		}
	})

	t.Run("копия дерева во временный каталог молчит", func(t *testing.T) {
		if fs, ok := got[twinCopy]; ok {
			t.Errorf("копия дерева объявлена находкой (%+v) — предикат считает "+
				"`filepath.Rel` сохраняющим происхождение, хотя его результат "+
				"относителен и корня не содержит", fs)
		}
	})

	t.Run("запрещённая форма в комментарии молчит", func(t *testing.T) {
		if fs, ok := got[twinProse]; ok {
			t.Errorf("гейт покраснел на собственном объяснении запрета (%+v) — "+
				"такой гейт снимут первым", fs)
		}
	})

	t.Run("перепись сама различает виды", func(t *testing.T) {
		// Производитель ОДИН и выведен из тела, а не из имени: имя здесь
		// намеренно не `repoRoot`. Если счётчик схлопнется, вердикты выше
		// станут свойством сломанного разбора, а не предиката.
		if census.Producers != 1 {
			t.Errorf("производителей живого корня насчитано %d, ожидался 1 — "+
				"вывод производителя из тела сломан", census.Producers)
		}
		if census.Files != 7 {
			t.Errorf("разобрано файлов %d, ожидалось 7 — часть корпуса не прочитана, "+
				"и молчание по ней ничего не значит", census.Files)
		}
		if census.Writes == 0 {
			t.Error("мест записи насчитано 0 — распознавание записи сломано, " +
				"и «ноль находок» неотличимо от «ноль прочитанного»")
		}
		if census.Tainted != len(findings) {
			t.Errorf("помеченных %d при находках %d — счётчик и вердикт разошлись",
				census.Tainted, len(findings))
		}
	})
}

// TestProbeWriteGateNeedsAProducerToSayAnything — предпосылка предиката.
//
// Происхождение ведётся ОТ производителя живого корня. Убери производителя — и
// тот же самый дефект перестаёт находиться. Проба закрепляет это как СВОЙСТВО,
// а не как случайность: иначе «ноль находок» на дереве без производителей
// читалось бы как чистота, и гейт молчал бы именно там, где сломан.
func TestProbeWriteGateNeedsAProducerToSayAnything(t *testing.T) {
	withProducer, census := auditProbeWritesToLiveTree(map[string]string{
		"internal/synth/producer_test.go":    synthLiveRootProducer,
		"internal/synth/writes_live_test.go": synthWritesLiveFile,
	})
	if len(withProducer) == 0 {
		t.Fatalf("с производителем дефект не найден — предикат мёртв (перепись: %+v)", census)
	}

	withoutProducer, census2 := auditProbeWritesToLiveTree(map[string]string{
		"internal/synth/writes_live_test.go": synthWritesLiveFile,
	})
	if census2.Producers != 0 {
		t.Fatalf("производитель насчитан без своего файла: %+v", census2)
	}
	if len(withoutProducer) != 0 {
		t.Fatalf("без производителя найдено %d — предикат ведёт происхождение не от того, "+
			"от чего заявлено", len(withoutProducer))
	}
	// Отсюда и требование к гейту по дереву: на нуле производителей он ПАДАЕТ,
	// а не молчит (`TestProbesDoNotWriteIntoTheTreeTheyRunFrom`, проверка
	// предпосылки) — иначе исчезновение источника происхождения выглядело бы
	// как чистое дерево.
}
