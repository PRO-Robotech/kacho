// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// poolparamrefusal_test.go — страж пулового параметра в строке подключения
// обязан иметь ОДИН предикат на дерево, и жить этот предикат обязан там, где он
// не выдаёт строку наружу.
//
// ПРЕДМЕТ. Строка подключения собирается через `url.UserPassword(...)`, то есть
// НЕСЁТ ПАРОЛЬ БАЗЫ. Отказ старта уезжает в журнал и оператору. Текст отказа
// обязан называть РУЧКУ — имя найденного пулового ключа, — а не значение, в
// котором ручка встретилась. Тот же довод уже записан в godoc
// `db.SSLModeFromDSN`: «никогда не возвращает ничего, кроме режима — DSN несёт
// пароль, а результат уходит в лог».
//
// ПОЧЕМУ ГЕЙТ СУДИТ ЕДИНСТВЕННОСТЬ ПРЕДИКАТА, А НЕ ТЕКСТ ОТКАЗА. «Не напечатать
// пароль» — свойство ЗНАЧЕНИЯ, и на месте вызова оно неразрешимо: нужен поток
// данных от сборки строки до аргумента форматирования. Разрешимо другое, и оно
// это свойство ВЛЕЧЁТ: пуловый ключ ищет ровно одна функция дерева
// (`db.PoolParamFromDSN`), она возвращает ТОЛЬКО имя ключа, и её собственная
// проба утверждает это напрямую (`pkg/db/poolparam_test.go`). Тогда у стража на
// руках имя ключа, и напечатать строку ему нечего.
//
// ВТОРОЕ, ЧТО ЗАКРЫЛОСЬ ПО ДОРОГЕ. Подстрочная проверка совпадала и на ПАРОЛЕ:
// секрет, содержащий эту последовательность, отказывал сервису в старте. Разбор
// ключей такого исхода не имеет by construction — контроль стоит пробой
// `TestPoolParamFromDSNIgnoresThePassword`.
//
// ГРАНИЦА, НАЗВАННАЯ ВСЛУХ. Гейт не запрещает напечатать строку подключения
// каким-то иным способом; он закрывает ровно ту дверь, через которую класс
// вошёл, — пять рукописных копий подстрочной проверки, каждая со своим
// `fmt.Errorf(..., dsn)`. Появится другой способ — ему нужен свой предикат.

// poolParamTreeSources — непроверочное дерево Go, спрошенное У ИНДЕКСА.
//
// Обход диска не знает правил игнорирования и судит чужой рабочий каталог —
// произведённые файлы, чужие копии, остатки прогонов.
func poolParamTreeSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		sources[rel] = string(src)
	}
	return sources
}

// TestPoolParamPredicateHasASingleHome — предикат пулового ключа один на дерево.
func TestPoolParamPredicateHasASingleHome(t *testing.T) {
	findings, census := FindPoolParamSubstringChecks(poolParamTreeSources(t))
	if census.Files == 0 {
		t.Fatal("разобрано ноль файлов Go — гейт беспредметен, «ноль находок» " +
			"здесь неотличимо от «ноль прочитанного»")
	}
	for _, f := range findings {
		t.Errorf("%s:%d: пуловый ключ ищется подстрокой на месте вызова.\n"+
			"Предикат обязан быть один — db.PoolParamFromDSN: он отдаёт ИМЯ ключа, "+
			"поэтому отказу старта нечем напечатать строку подключения, а она несёт пароль базы.",
			f.File, f.Line)
	}
	t.Logf("перепись: разобрано файлов Go %d, не разобрано %d, пропущено как дом предиката %d, "+
		"копий подстрочного предиката %d", census.Files, census.Unparsed, census.Skipped, len(findings))
}
