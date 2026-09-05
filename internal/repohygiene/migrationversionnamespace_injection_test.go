// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationversionnamespace_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) верни дефект — разбор краснеет и НАЗЫВАЕТ координату (каталог и номер);
// (б) поставь рядом законную конструкцию той же формы — разбор молчит.
//
// Без (б) гейт ловил бы форму, а не существо: «файл, начинающийся с цифр» —
// нормальное состояние любого каталога миграций, и первое же ложное срабатывание
// его отключило бы.
//
// Отдельно здесь стоит ВОСПРОИЗВЕДЕНИЕ на синтетических ветках. Предикат снятия
// задачи сформулирован как «две ветки от одной базы, каждая со своей миграцией
// одного сервиса, сливаются без столкновения» и требует проверки слиянием, а не
// рассуждением: тихость дефекта — свойство git-мёржа, и утверждать о ней, не
// выполнив мёржа, значило бы повторить ту же ошибку, из-за которой класс дожил
// до сегодня.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// synthFrozen — перепись синтетического дерева. Отдельная от боевой намеренно:
// проба обязана проверять РАЗБОР, а не заучивать содержимое настоящего дерева.
var synthFrozen = map[string]string{
	"services/widget/internal/migrations": "1-3",
}

// writeSynthTree раскладывает временное дерево и отдаёт его состав тем же
// способом, каким проба это делает у себя, — обходом (репозиторием оно не
// является, спрашивать индекс не у чего).
func writeSynthTree(t *testing.T, files ...string) []string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte("-- +goose Up\nSELECT 1;\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	return tree.SortedFiles()
}

func synthFindings(t *testing.T, files ...string) []string {
	t.Helper()
	byDir, census := collectMigrationVersions(writeSynthTree(t, files...))
	if census.Files != len(files) {
		t.Fatalf("перепись синтетического дерева: разобрано %d файлов из %d — "+
			"разбор читает не то, что положили", census.Files, len(files))
	}
	return migrationVersionFindings(byDir, synthFrozen)
}

const (
	synthDir = "services/widget/internal/migrations"
	synthA   = synthDir + "/0001_a.sql"
	synthB   = synthDir + "/0002_b.sql"
	synthC   = synthDir + "/0003_c.sql"
)

func TestMigrationVersionNamespace_ProvenByInjection(t *testing.T) {
	t.Run("законная конструкция той же формы — разбор молчит", func(t *testing.T) {
		// Замороженная эра дословно + миграция новой формы + ВТОРАЯ миграция той
		// же задачи (порядковый 002). Всё это законно и обязано пройти, иначе
		// отрицание ниже зеленело бы на любом дереве.
		got := synthFindings(t, synthA, synthB, synthC,
			synthDir+"/539001_first.sql",
			synthDir+"/539002_second.sql",
			synthDir+"/559001_other_line.sql",
		)
		if len(got) != 0 {
			t.Fatalf("ложное срабатывание на законном дереве:\n%s", strings.Join(got, "\n"))
		}
	})

	t.Run("номер выбран рукой из каталога — краснеет и называет координату", func(t *testing.T) {
		got := synthFindings(t, synthA, synthB, synthC, synthDir+"/0004_next_free.sql")
		if len(got) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d:\n%s", len(got), strings.Join(got, "\n"))
		}
		if !strings.Contains(got[0], synthDir) || !strings.Contains(got[0], "4") {
			t.Fatalf("находка не называет координату: %s", got[0])
		}
	})

	t.Run("две линии выбрали один номер — краснеет он же", func(t *testing.T) {
		// Именно тот случай, ради которого механизм заведён: обе стороны взяли
		// «следующий свободный». Разбор видит прибавку и без того, чтобы
		// обнаружить дубль, — то есть краснеет и на ОДНОЙ стороне, до слияния.
		got := synthFindings(t, synthA, synthB, synthC,
			synthDir+"/0004_line_a.sql", synthDir+"/0004_line_b.sql")
		if len(got) == 0 {
			t.Fatal("столкновение номеров не замечено разбором пространства номеров")
		}
	})

	t.Run("применённая миграция исчезла — краснеет", func(t *testing.T) {
		got := synthFindings(t, synthA, synthC)
		if len(got) != 1 || !strings.Contains(got[0], "ИСЧЕЗЛИ") {
			t.Fatalf("убыль замороженной эры не названа: %v", got)
		}
	})

	t.Run("выведенный номер без порядкового разряда — краснеет", func(t *testing.T) {
		got := synthFindings(t, synthA, synthB, synthC, synthDir+"/539000_no_ordinal.sql")
		if len(got) != 1 || !strings.Contains(got[0], "539000") {
			t.Fatalf("номер с нулевым порядковым разрядом принят: %v", got)
		}
	})

	// ЗАКОННЫЙ БЛИЗНЕЦ: метка времени — действующая форма номера НОВОЙ миграции
	// (уточнение 2026-08-22, задача #921), поэтому проверка пространства номеров
	// про неё молчит. Форму добавленных файлов требует отдельный гейт
	// (`TestNewMigrationOutranksEveryAppliedOne`) — здесь предмет другой:
	// «не столкнутся ли две линии на одном номере».
	//
	// Проба стоит здесь именно как близнец: раньше на этом входе гейт краснел, и
	// два гейта дерева были НЕСОВМЕСТИМЫ — порядковую форму отвергал один, метку
	// времени другой, и добавить миграцию было нельзя ни в какой форме.
	t.Run("метка времени — молчит (законная форма нового номера)", func(t *testing.T) {
		got := synthFindings(t, synthA, synthB, synthC,
			synthDir+"/20260817042704_timestamped.sql")
		if len(got) != 0 {
			t.Fatalf("законная метка времени объявлена находкой: %v", got)
		}
	})

	t.Run("число между формами — краснеет", func(t *testing.T) {
		// Одиннадцать девяток: больше всякого выводимого номера задачи и меньше
		// всякой возможной метки времени. Ни та, ни другая форма — то есть номер,
		// который не выводится ниоткуда. Проба держит границу с ОБЕИХ сторон:
		// без неё послабление под метку времени открыло бы любое большое число.
		got := synthFindings(t, synthA, synthB, synthC,
			synthDir+"/99999999999_neither_form.sql")
		if len(got) != 1 || !strings.Contains(got[0], "99999999999") {
			t.Fatalf("число вне обеих форм принято: %v", got)
		}
	})

	t.Run("каталог не внесён в перепись — краснеет", func(t *testing.T) {
		got := synthFindings(t, synthA, synthB, synthC,
			"services/gadget/internal/migrations/0001_a.sql")
		if len(got) != 1 || !strings.Contains(got[0], "services/gadget") {
			t.Fatalf("каталог вне переписи принят молча: %v", got)
		}
	})

	t.Run("записи переписи нечего замораживать — краснеет (послабление истекает само)", func(t *testing.T) {
		byDir, _ := collectMigrationVersions(writeSynthTree(t, synthA, synthB, synthC))
		frozen := map[string]string{
			synthDir:                                "1-3",
			"services/vanished/internal/migrations": "1-2",
		}
		got := migrationVersionFindings(byDir, frozen)
		if len(got) != 1 || !strings.Contains(got[0], "services/vanished") {
			t.Fatalf("устаревшая запись переписи не названа: %v", got)
		}
	})

	t.Run("соседний файл той же формы миграцией не притворяется", func(t *testing.T) {
		// Не .sql — не миграция. Без этой стороны гейт ловил бы «файл, имя
		// которого начинается с цифр», а это норма любого каталога.
		byDir, census := collectMigrationVersions(writeSynthTree(t,
			synthA, synthB, synthC, synthDir+"/0004_notes.md"))
		if census.Files != 3 {
			t.Fatalf("разобрано %d файлов — соседний .md принят за миграцию", census.Files)
		}
		if got := migrationVersionFindings(byDir, synthFrozen); len(got) != 0 {
			t.Fatalf("ложное срабатывание на соседнем файле: %v", got)
		}
	})
}

// ───────────────── воспроизведение на синтетических ветках ─────────────────

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command(dir, args...)
	// Подпись фикстуры задаётся ЯВНО: у машины, где прогон идёт без глобальной
	// настройки git, `commit` иначе отказывает, и проба падала бы по причине,
	// не имеющей отношения к её предмету. Дописывание к cmd.Env, а не
	// присваивание os.Environ() — иначе снятые gitenv переменные вернулись бы.
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// twoLinesMerge заводит синтетический репозиторий, отводит от одной базы две
// ветки, кладёт в каждую по своей миграции ОДНОГО сервиса и сливает вторую в
// первую. Возвращает состав каталога после слияния.
func twoLinesMerge(t *testing.T, base []string, lineA, lineB string) []string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")

	write := func(rel string) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte("-- +goose Up\nSELECT 1;\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	for _, rel := range base {
		write(rel)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")

	git(t, root, "checkout", "-q", "-b", "line-a")
	write(lineA)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "line a")

	git(t, root, "checkout", "-q", "main")
	git(t, root, "checkout", "-q", "-b", "line-b")
	write(lineB)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "line b")

	git(t, root, "checkout", "-q", "line-a")
	// Слияние обязано пройти ЧИСТО в обоих случаях — в этом и состоит тихость
	// прежнего дефекта. Отказ здесь означал бы, что проба воспроизводит не то.
	git(t, root, "merge", "-q", "--no-edit", "line-b")

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав слитого дерева: %v", err)
	}
	return tree.SortedFiles()
}

func TestTwoLinesFromOneBaseMergeWithoutVersionCollision(t *testing.T) {
	base := []string{synthA, synthB, synthC}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: прежняя процедура. Обе линии смотрят на последний
	// файл базы (0003) и берут 0004. Мёрж чист — и в дереве два файла под одним
	// ключом, на котором мигратор падает паникой ещё до подключения к базе.
	t.Run("прежняя процедура: обе линии берут следующий свободный", func(t *testing.T) {
		files := twoLinesMerge(t, base, synthDir+"/0004_line_a.sql", synthDir+"/0004_line_b.sql")
		byDir, census := collectMigrationVersions(files)
		if census.Files != 5 {
			t.Fatalf("после слияния разобрано %d файлов, ожидалось 5", census.Files)
		}
		var dup int
		for _, v := range byDir[synthDir].Legacy {
			if v == 4 {
				dup++
			}
		}
		if dup != 2 {
			t.Fatalf("воспроизведение не удалось: номер 4 встречается %d раз(а), ожидалось 2 — "+
				"проба проверяет не тот класс", dup)
		}
		if got := migrationVersionFindings(byDir, synthFrozen); len(got) == 0 {
			t.Fatal("столкновение, воспроизведённое слиянием, не названо разбором")
		}
	})

	// НОВАЯ ПРОЦЕДУРА: каждая линия выводит номер из СВОЕЙ задачи. Мёрж так же
	// чист, но столкнуться нечему — величины пришли от распределителя, который
	// не выдаёт одну дважды.
	t.Run("новая процедура: каждая линия выводит номер из своей задачи", func(t *testing.T) {
		files := twoLinesMerge(t, base, synthDir+"/539001_line_a.sql", synthDir+"/559001_line_b.sql")
		byDir, census := collectMigrationVersions(files)
		if census.Files != 5 {
			t.Fatalf("после слияния разобрано %d файлов, ожидалось 5", census.Files)
		}
		seen := map[int64]bool{}
		for _, v := range byDir[synthDir].Issued {
			if seen[v] {
				t.Fatalf("номер %d встречается дважды — механизм не работает", v)
			}
			seen[v] = true
		}
		if len(seen) != 2 {
			t.Fatalf("выведенных номеров %d, ожидалось 2", len(seen))
		}
		if got := migrationVersionFindings(byDir, synthFrozen); len(got) != 0 {
			t.Fatalf("ложное срабатывание на слиянии двух законных линий:\n%s", strings.Join(got, "\n"))
		}
	})
}
