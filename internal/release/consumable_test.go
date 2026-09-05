// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consumable_test.go — монорепо обязано СОБИРАТЬСЯ у внешнего потребителя.
//
// # Предмет
//
// Решение владельца: вынесенный iam ссылается на фундамент `pkg/` и контракты
// `proto/` как на ВНЕШНЮЮ зависимость — versioned Go-модуль. С этого дня
// «дерево собирается» и «модуль собирается у чужого потребителя» — РАЗНЫЕ
// утверждения, и первое второго не доказывает.
//
// Расходятся они тихо и в обе стороны:
//
//   - файл, нужный сборке, но НЕ отслеживаемый git (сгенерированный стаб,
//     забытый `git add`), в нашем дереве есть, а в опубликованной версии его
//     нет вовсе: прокси пакует отслеживаемое;
//   - `replace` на локальный путь у нас резолвится, у потребителя — нет;
//   - файл, который правила упаковки Go отвергают (два имени, различающиеся
//     только регистром; путь вне допустимых; превышение предела зипа), ломает
//     публикацию ЦЕЛИКОМ, и узнаётся это в момент, когда версия уже уехала.
//
// # Как это проверяется — тем же кодом, что и инструментарий
//
// Ревизия упаковывается `golang.org/x/mod/zip.CreateFromVCS` — той самой
// функцией, которой модуль-прокси формирует зип версии. Пересказ её правил
// своими словами проверял бы наш пересказ, а не предмет.
//
// Дальше зип кладётся в файловый прокси, во временном каталоге заводится
// ПУСТОЙ модуль, он объявляет зависимость от нашего по версии и собирает
// программу, импортирующую наш пакет. Ни одного `replace`: путь тот же, каким
// пойдёт iam.
//
// # Почему проба герметична
//
// Файловый прокси отдаёт единственный модуль — наш. Импортируется
// `pkg/ids`: у него ноль внешних зависимостей (`go list -deps` даёт только
// стандартную библиотеку), поэтому потребителю нечего скачивать, и зелёное
// здесь не может быть куплено чужим кэшем.
//
// # Что проба НЕ утверждает
//
// Она не утверждает, что версия ОПУБЛИКОВАНА — это необратимое внешнее
// действие владельца, и его проверяет `scripts/release/probe-published.sh`.
// Она не собирает ВСЕ наши пакеты у потребителя: у него нет наших зависимостей
// в кэше, и такая проба измеряла бы наличие сети. Предмет здесь — упаковка и
// достижимость, а не полнота дерева.
//
// # Способность упасть
//
// Доказана `consumable_injection_test.go`: тот же код, применённый к
// синтетическому репозиторию с ОДНИМ изменённым фактом, обязан краснеть, а к
// его законному близнецу — молчать.
package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// TestExternalConsumerCanBuildTheModule — предмет полосы выпуска.
//
// Имя цитируется предпосылкой П8 в `scripts/release/publish-version.sh`:
// механизм публикации требует НАБЛЮДЁННОГО прохождения по этому имени, потому
// что `go test -run` на несовпавшем образце выходит успехом и печатает
// «no tests to run» — то есть опечатка в отборе читается как зелёное.
func TestExternalConsumerCanBuildTheModule(t *testing.T) {
	root := moduleRoot(t)

	// Упаковывается ОТСЛЕЖИВАЕМОЕ содержимое ревизии, а не рабочее дерево:
	// именно этим отличается опубликованная версия от того, что видит автор.
	vcs := gitRootFor(t, root)

	modPath := modulePathOf(t, root)
	res := packAndBuild(t, packRequest{
		vcsRoot:    vcs,
		modulePath: modPath,
		importPath: modPath + "/pkg/ids",
		program:    "package main\n\nimport ids \"" + modPath + "/pkg/ids\"\n\nfunc main() { _ = ids.NewID(\"tst\") }\n",
	})

	t.Logf("перепись: модуль %s, ревизия %s, файлов в зипе %d, зип %.1f МиБ, предел прокси %d МиБ",
		modPath, res.revision, res.filesInZip, float64(res.zipBytes)/(1<<20), maxZipMiB)

	if res.filesInZip == 0 {
		t.Fatalf("обход пуст: в зипе ноль файлов — вердикт беспредметен")
	}
	if res.err != nil {
		t.Fatalf("внешний потребитель не собирает модуль %s: %v\n%s", modPath, res.err, res.output)
	}
}

// moduleRoot — поднимаемся до каталога с go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
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
			t.Fatalf("go.mod не найден выше %s", dir)
		}
		dir = parent
	}
}

func modulePathOf(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod не прочитан: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
			return f[1]
		}
	}
	t.Fatalf("в go.mod нет строки module")
	return ""
}

// gitRootFor — каталог, годный для упаковки из системы контроля версий.
//
// `zip.CreateFromVCS` опознаёт репозиторий по КАТАЛОГУ `.git`. В связанной
// рабочей копии (`git worktree`) `.git` — файл, и функция отвечает «не найдено
// системы контроля версий». Агенты работают именно в связанных копиях,
// поэтому ветвление здесь не редкость, а обычный путь.
//
// Клонируется ВСЕГДА, а не только в связанной копии: ветвь, исполняемая лишь
// на машине агента, в конвейере не проверяется ни разу и ломается незаметно.
func gitRootFor(t *testing.T, root string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "vcs")
	cmd := gitenv.Command(filepath.Dir(dst), "clone", "--quiet", "--depth", "1", "--no-tags",
		"--single-branch", "file://"+root, dst)
	cmd.Env = cleanGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		// УСЛОВИЕ НЕ СОЗДАНО, а не находка о модуле. Различие названо в самом
		// тексте: читатель, увидевший красное, обязан сразу понять, что
		// вердикта об упаковке НЕТ ВОВСЕ, и не искать поломку в своей ветке.
		t.Fatalf("УСЛОВИЕ НЕ СОЗДАНО: клон ревизии %s не сделан (%v) — вердикта об упаковке нет вовсе, это не находка о модуле\n%s",
			root, err, out)
	}
	return dst
}

// cleanGitEnv — окружение без унаследованных GIT_*.
//
// Унаследованный GIT_DIR сильнее рабочего каталога: без снятия действия пробы
// уезжают в индекс той копии, из которой она запущена, и портят чужое
// состояние. Класс наблюдался в этом дереве и стоил ложных красных вердиктов
// на целых гейтах.
func cleanGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GIT_COMMON_DIR": true, "GIT_PREFIX": true,
	}
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && drop[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
