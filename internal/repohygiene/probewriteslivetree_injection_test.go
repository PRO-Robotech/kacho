// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «проба не пишет в дерево, из которого запущена»
// СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны и настоящими формами из дерева:
//
//	живой корень → запись файла                  → краснеет, называет координату;
//	живой корень РАБОЧИМ КАТАЛОГОМ git → мутация → краснеет;
//	живой корень В АРГУМЕНТАХ git (`-C`)         → краснеет (ветка другая);
//	живой корень → помощник, который пишет       → краснеет (шаг наружу);
//	живой корень → ЧТЕНИЕ                        → молчит (иначе запрет на гейты);
//	СВОЁ дерево рабочим каталогом git → мутация  → молчит;
//	живой корень + запись во временный           → молчит;
//	копия дерева (`Rel` → `Join`)                → молчит;
//	запрещённая форма в комментарии              → молчит (читается код, не текст).
//
// # Почему форм git ДВЕ, а не одна (исправлено по рецензии #696)
//
// Первая редакция этого файла закрепляла только форму «корень в аргументах»
// (`gitenv.Command("", "-C", root, "add", …)`). Ею была написана ПРЕЖНЯЯ
// редакция проб — та самая, которую задача снимала, — и на ней инъекция
// выглядела полной. Но сам фикс написан ДРУГОЙ формой: `gitenv.Command(root,
// "add", …)`, корень рабочим каталогом. Ветка гейта у неё своя (`isLive(dir)`),
// и она не была закреплена ничем: рецензент отключил её целиком, а инъекция
// осталась зелёной по всем семи случаям — включая тот, что называется
// «изменяющая git-команда против живого репозитория краснеет».
//
// Соотношение форм в корпусе проб на `e22436f1` (предикат — `git grep -c
// 'gitenv\.Command('` против `'gitenv\.Command("'` по `*_test.go`): всего 43
// вызова, «корень рабочим каталогом» — 38, «корень в аргументах» — 5. То есть
// незакреплённой оставалась ПРЕОБЛАДАЮЩАЯ форма, а не редкая.
//
// Пятая и восьмая строки — не украшение: обе формы В ДЕРЕВЕ ЕСТЬ, и на восьмой
// первая редакция предиката дала ложную находку (считала `filepath.Rel`
// сохраняющим происхождение). Гейт, краснеющий на копии дерева, снимут первым.
// Шестая строка — дословная форма исправленного кода задачи: гейт, краснеющий
// на ней, объявил бы собственный фикс дефектом.
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

// ДЕФЕКТ 2 — изменяющая git-команда, живой корень идёт РАБОЧИМ КАТАЛОГОМ.
// Форма преобладающая (38 вызовов из 43) и, что важнее, ею написан сам фикс
// задачи — поэтому именно она обязана держать ветку `isLive(dir)`.
const synthMutatesLiveIndexViaWorkdir = `package probe

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

func TestStagesIntoTheLiveIndexViaWorkdir(t *testing.T) {
	root := treeTop(t)
	if out, err := gitenv.Command(root, "add", "-f", "--", "ui-future/injected.test.tsx").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}
`

// ДЕФЕКТ 3 — та же команда, но корень уехал В АРГУМЕНТЫ (`-C <root>`), а рабочий
// каталог пуст. Ветка гейта здесь ДРУГАЯ — обход аргументов и разбор среза,
// собранного строкой выше, — поэтому случай отдельный, а не копия предыдущего.
// Этой формой была написана прежняя редакция проб, которую задача сняла.
const synthMutatesLiveIndexViaArgs = `package probe

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

func TestStagesIntoTheLiveIndexViaArgs(t *testing.T) {
	root := treeTop(t)
	rels := []string{"ui-future/injected.test.tsx"}
	addArgs := append([]string{"-C", root, "add", "-f", "--"}, rels...)
	if out, err := gitenv.Command("", addArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}
`

// ДЕФЕКТ 4 — живой корень уезжает в помощника, и пишет уже он. Без шага наружу
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

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
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

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — та же изменяющая команда, тем же рабочим каталогом, но
// каталог СВОЙ. Это дословная форма исправленного кода задачи: завести
// репозиторий во временном каталоге, положить туда пробы и добавить их в ЕГО
// индекс.
//
// Пара с ДЕФЕКТОМ 2 замыкает ветку `isLive(dir)` с ОБЕИХ сторон, и ни одна
// сторона не лишняя: отключи ветку — промолчит дефект; защеми её в «всегда
// живой» — покраснеет этот близнец, то есть гейт объявит находкой собственный
// фикс. Близнец не копия дефекта: у него другой корень (временный против
// живого) и другой набор подкоманд.
const synthMutatesOwnSynthTree = `package probe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

func TestStagesIntoItsOwnTree(t *testing.T) {
	root := t.TempDir()
	if out, err := gitenv.Command(root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	rel := "ui-future/probe.test.tsx"
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	addArgs := append([]string{"add", "-f", "--"}, rel)
	if out, err := gitenv.Command(root, addArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — копия дерева во временный каталог. Настоящая форма из
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

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — запрещённая форма в КОММЕНТАРИИ. Гейт, краснеющий на
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
		defGitDir   = "internal/synth/mutates_index_workdir_test.go"
		defGitArgs  = "internal/synth/mutates_index_args_test.go"
		defHelper   = "internal/synth/via_helper_test.go"
		twinRead    = "internal/synth/reads_live_test.go"
		twinOwnTree = "internal/synth/own_tree_test.go"
		twinCopy    = "internal/synth/copies_tree_test.go"
		twinProse   = "internal/synth/prose_test.go"
	)

	findings, census := auditProbeWritesToLiveTree(map[string]string{
		producerRel: synthLiveRootProducer,
		defWrite:    synthWritesLiveFile,
		defGitDir:   synthMutatesLiveIndexViaWorkdir,
		defGitArgs:  synthMutatesLiveIndexViaArgs,
		defHelper:   synthWritesViaHelper,
		twinRead:    synthReadsLiveWritesTemp,
		twinOwnTree: synthMutatesOwnSynthTree,
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

	// Два случая ниже — РАЗНЫЕ ветки гейта, а не одна форма, записанная дважды.
	// Первый ведёт происхождение через рабочий каталог вызова, второй — через
	// аргументы. Слить их в одно утверждение «хоть где-то нашлось» нельзя: тогда
	// отключение любой одной ветки осталось бы зелёным, что и произошло.
	t.Run("живой корень РАБОЧИМ КАТАЛОГОМ изменяющей git-команды краснеет", func(t *testing.T) {
		fs, ok := got[defGitDir]
		if !ok {
			t.Fatal("`git add` с живым корнем рабочим каталогом НЕ пойман. Этой формой " +
				"написан сам фикс #696 и 38 вызовов `gitenv.Command` из 43 в корпусе " +
				"проб: без этого случая отключение ветки `isLive(dir)` инъекция не " +
				"замечает — проверено рецензией, все семь прежних случаев оставались зелёными")
		}
		if fs[0].What != "git add" {
			t.Errorf("подкоманда названа неверно: %q", fs[0].What)
		}
	})

	t.Run("живой корень В АРГУМЕНТАХ изменяющей git-команды краснеет", func(t *testing.T) {
		fs, ok := got[defGitArgs]
		if !ok {
			t.Fatal("`git -C <живой корень> add` НЕ пойман — фантомная запись в индексе " +
				"осталась бы невидимой ровно для тех гейтов, что берут состав у индекса")
		}
		if fs[0].What != "git add" {
			t.Errorf("подкоманда названа неверно: %q", fs[0].What)
		}
	})

	t.Run("изменяющая git-команда против СВОЕГО дерева молчит", func(t *testing.T) {
		if fs, ok := got[twinOwnTree]; ok {
			t.Errorf("`git init`/`git add` во временном каталоге объявлены находкой (%+v). "+
				"Это дословная форма исправленного кода задачи: гейт, краснеющий на ней, "+
				"объявляет дефектом собственный фикс и будет снят первым же прогоном", fs)
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
		if census.Files != 9 {
			t.Errorf("разобрано файлов %d, ожидалось 9 — часть корпуса не прочитана, "+
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

// Производитель живого корня, ВТОРАЯ форма: оттолкнуться от файла самого
// исходника (`runtime.Caller`) и подняться на известное число каталогов.
// Маркера («go.mod») она не ищет и ничего не статит. Форма снята с дерева —
// `services/vpc/tools/newmanverdict/ci_wiring_test.go`.
const synthCallerRootProducer = `package probe

import (
	"path/filepath"
	"runtime"
	"testing"
)

func moduleTop(t *testing.T) string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for range 4 {
		dir = filepath.Dir(dir)
	}
	return dir
}
`

// Дефект, написанный ВТОРОЙ формой производителя. До расширения признака запись
// по такому корню была гейту невидима: происхождение ведётся ОТ производителя, а
// производителем эта функция не считалась.
const synthWritesViaCallerRoot = `package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInjectsRelativeToItsOwnFile(t *testing.T) {
	root := moduleTop(t)
	abs := filepath.Join(root, "ui-future/injected.test.tsx")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
`

// КОНТРОЛЬ В ДРУГУЮ СТОРОНУ — помощник, который ТОЛЬКО собирает пути. Он
// поднимается по каталогам (`filepath.Dir`), но не спрашивает ни рабочий каталог
// процесса, ни собственный файл, — корня живого репозитория он не производит.
// Без этого контроля расширение признака сделало бы производителем любую работу
// с путями, гейт залило бы ложными находками, и снят он был бы первым же.
const synthPathHelperNotAProducer = `package probe

import "path/filepath"

func parentOf(p string) string {
	return filepath.Dir(filepath.Clean(p))
}
`

// TestProbeWriteGateKnowsBothFormsOfLiveRootProducer — определение производителя
// одно на два гейта пакета.
//
// Корень живого репозитория ищут двумя способами, и второй (`runtime.Caller` →
// восхождение) соседний гейт того же пакета уже признаёт, а этот — не признавал.
// Расхождение двух определений одного предмета не роняет ничего само по себе:
// оно просто делает половину форм невидимой, и «ноль находок» перестаёт значить
// «ноль записей». Поэтому свойство закрепляется пробой, а не комментарием.
func TestProbeWriteGateKnowsBothFormsOfLiveRootProducer(t *testing.T) {
	const (
		producerRel = "internal/synth2/caller_producer_test.go"
		helperRel   = "internal/synth2/path_helper_test.go"
		defectRel   = "internal/synth2/writes_live_test.go"
	)

	findings, census := auditProbeWritesToLiveTree(map[string]string{
		producerRel: synthCallerRootProducer,
		helperRel:   synthPathHelperNotAProducer,
		defectRel:   synthWritesViaCallerRoot,
	})

	if census.Files != 3 {
		t.Fatalf("разобрано файлов %d, ожидалось 3 — молчание по непрочитанному "+
			"ничего не значит (перепись: %+v)", census.Files, census)
	}
	// Ровно ОДИН: форма «от своего файла» обязана узнаваться, а помощник, который
	// только собирает пути, — нет. Два числа здесь неразделимы: признак, ставший
	// шире нужного, даёт те же «производители найдены», что и верный.
	if census.Producers != 1 {
		t.Fatalf("производителей насчитано %d, ожидался ровно 1. Больше — признак "+
			"считает производителем любую работу с путями (ложные находки); "+
			"меньше — форма «от своего файла» не узнана и запись по такому корню "+
			"невидима (перепись: %+v)", census.Producers, census)
	}
	if len(findings) != 1 || findings[0].File != defectRel {
		t.Fatalf("запись по корню, найденному от СВОЕГО ФАЙЛА, не поймана: %+v "+
			"(перепись: %+v)", findings, census)
	}
}
