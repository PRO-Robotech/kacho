// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// crossmodulegopath_test.go — КОМАНДА, НАЗВАННАЯ В ДЕРЕВЕ, ОБЯЗАНА БЫТЬ
// ИСПОЛНИМОЙ: пакет Go, лежащий в СВОЁМ модуле, не адресуется путём от корня
// монорепо.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба, вынесенная в собственный модуль, перестаёт принадлежать корневому: её
// пакетов там нет by construction, и `go test ./services/<svc>/...`, запущенный
// из корня, отвечает
//
//	main module (github.com/PRO-Robotech/kacho) does not contain package …
//
// Законная форма — `-C <модуль>` и путь ОТНОСИТЕЛЬНО его корня.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ — ЦЕНА ИЗМЕРЕНА
//
// На вынесении службы класс дал ТРИ живых носителя, и громким был ОДИН:
// самопроба формы удостоверения роняла джобу конвейера. Два других молчали —
// цель `cla-check` конвейером не зовётся вовсе, а перепись источников
// классифицировала свой отказ как «условие не создано», то есть выглядела
// отсутствующим инструментом, а не сломанным путём. Плюс шестнадцать предикатов
// в комментариях манифестов, которые читатель набирает руками и получает отказ.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ СУДИТ ПО ФОРМЕ ПУТИ, А НЕ ПО СТРОКЕ ВЫЗОВА
//
// Первая мысль — искать строку с `go test ./services/iam/`. Она пропускает ровно
// тот носитель, который и уронил конвейер: там путь лежал в ПЕРЕМЕННОЙ
// (`MINT_PKG = "./services/iam/…"`), а вызов собирался списком аргументов. Форма
// записи не одна, поэтому судится не она, а сам путь: `./<модуль>/<остаток>`,
// где `<остаток>` — каталог с файлами `.go`. Что ещё может означать такой путь в
// сценарии, кроме пакета Go, — вопрос без второго ответа.
//
// Комментарий из-под гейта НЕ выведен намеренно: предикат, названный в
// комментарии и не исполняющийся, — тот же дефект, только дороже (его набирают
// руками и получают отказ, не понимая, чей он).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕЧЕНЬ МОДУЛЕЙ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// Модули берутся из состава дерева (`go.mod` ниже корня). Выписанный перечень
// разошёлся бы с деревом на первой же вынесенной службе — молча, потому что
// гейт продолжал бы быть зелёным.
package repohygiene

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// crossModuleGoPath — одно попадание: путь в чужой модуль, названный от корня.
type crossModuleGoPath struct {
	File   string
	Line   int
	Module string
	Text   string
}

// goPathCarrierSuffixes — носители, в которых команда исполняется либо
// называется читателю. Документация (`.md`) сюда НЕ входит: там живут
// свидетельства о прошлых прогонах, и переписывать их значило бы делать ложными
// утверждения, которые были верны.
var goPathCarrierSuffixes = []string{".py", ".sh", ".yml", ".yaml", ".mk"}

// isGoPathCarrier — носитель ли это команды.
func isGoPathCarrier(rel string) bool {
	if path.Base(rel) == "Makefile" {
		return true
	}
	for _, s := range goPathCarrierSuffixes {
		if strings.HasSuffix(rel, s) {
			return true
		}
	}
	return false
}

// findCrossModuleGoPaths — ТЕЛО гейта. Вынесено, чтобы инъекция звала то же, что
// исполняется на дереве.
//
// `isGoPkgDir` спрашивает у СОСТАВА, а не у диска: каталог, которого в
// репозитории нет, пакетом не является, сколько бы файлов в нём ни лежало на
// чьей-то машине.
func findCrossModuleGoPaths(
	carriers map[string]string,
	modules []string,
	isGoPkgDir func(dir string) bool,
) []crossModuleGoPath {
	var out []crossModuleGoPath
	for _, mod := range modules {
		// Граница слева обязательна: без неё `../services/iam/x` совпал бы
		// своим хвостом, а это путь ОТНОСИТЕЛЬНО другого каталога и к предмету
		// отношения не имеет.
		re := regexp.MustCompile(`(?:^|[\s"'` + "`" + `=(,])\./` + regexp.QuoteMeta(mod) + `/([A-Za-z0-9_./-]+)`)
		for file, src := range carriers {
			for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
				rel := strings.TrimRight(src[m[2]:m[3]], "/.")
				if rel == "" || !isGoPkgDir(mod+"/"+rel) {
					continue
				}
				out = append(out, crossModuleGoPath{
					File:   file,
					Line:   1 + strings.Count(src[:m[0]], "\n"),
					Module: mod,
					Text:   "./" + mod + "/" + rel,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// TestNamedGoPathsResolveInTheModuleThatOwnsThem — гейт на дереве.
func TestNamedGoPathsResolveInTheModuleThatOwnsThem(t *testing.T) {
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	var modules []string
	goDirs := map[string]bool{}
	for _, rel := range tree.Tree.SortedFiles() {
		if path.Base(rel) == "go.mod" {
			if dir := path.Dir(rel); dir != "." {
				modules = append(modules, dir)
			}
		}
		if strings.HasSuffix(rel, ".go") {
			goDirs[path.Dir(rel)] = true
		}
	}
	sort.Strings(modules)

	carriers := map[string]string{}
	for _, rel := range tree.Tree.SortedFiles() {
		if !isGoPathCarrier(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		carriers[rel] = string(src)
	}

	// Предпосылка гейта: без носителей «ноль находок» неотличимо от «ноль
	// прочитанного».
	//
	// Отсутствие второго модуля пропуском НЕ объявляется. Пропуск в этом пакете
	// прогонщик конвейера сверяет с ведомостью и краснеет на незаявленном, а
	// «модулей ниже корня ноль» — законное состояние дерева, а не отказ: находок
	// в нём и так ноль, и перепись ниже называет это числом. Пропуск здесь
	// добавил бы ведомости запись, которой нечего покрывать.
	if len(carriers) == 0 {
		t.Fatal("обход пуст: носителей команд не найдено ни одного — вердикт беспредметен")
	}
	if len(goDirs) == 0 {
		t.Fatal("обход пуст: каталогов с файлами .go не найдено — предикат пакета слеп")
	}

	found := findCrossModuleGoPaths(carriers, modules, func(dir string) bool { return goDirs[dir] })

	t.Logf("осмотрено: состав %d файлов -> носителей команд %d; модулей ниже корня %d (%s); "+
		"каталогов с .go %d; находок %d",
		tree.Tree.Count(), len(carriers), len(modules), strings.Join(modules, ", "),
		len(goDirs), len(found))

	for _, f := range found {
		t.Errorf("%s:%d — путь %q адресует пакет чужого модуля (%s) от корня монорепо.\n"+
			"  Корневой модуль его НЕ СОДЕРЖИТ, и go отвечает «main module … does not contain package …».\n"+
			"  ЧТО ДЕЛАТЬ: `go <глагол> -C %s ./%s` — путь относительно корня модуля-владельца.",
			f.File, f.Line, f.Text, f.Module, f.Module, strings.TrimPrefix(f.Text, "./"+f.Module+"/"))
	}
}
