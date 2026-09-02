// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// roleverbsolewriter_test.go — IAM-RV-1-12: У ПРОЕКЦИИ РОЛИ ОДИН ПИСАТЕЛЬ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Таблица `kacho_iam.role_verb` — проекция «роль → тип объекта × глагол», то, из
// чего цепь вердикта собирает ответ «разрешено ли действие». Сегодня её пишут ДВЕ
// реализации, и запускаются они по РАЗНЫМ поводам: путь роли
// (`Role.Create`/`Role.Update` → `roleWriter.ReplaceRoleVerbs`) и путь старта
// (`BackfillOwnerBindings` → `replaceRoleVerbsTx`, свой сырой SQL).
//
// Пока писателей двое, всякое изменение семантики проекции обязано доехать до
// ОБОИХ, а промах в одном МОЛЧАЛИВ: обе реализации компилируются, у обеих есть
// пробы, и расходятся они только на входе, который ни одна проба не подаёт.
// Прецедент этого класса дереву уже стоил инцидента — досев селекторов завели, а
// вторую сторону того же правила нет, и разошлись они на годы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЕДИНИЦА — ОПЕРАТОР ЗАПИСИ, А НЕ ИМЯ МЕТОДА GO
//
// Предикат «у `ReplaceRoleVerbs` ровно два вызывающих» ВЫПОЛНЯЕТСЯ СЕГОДНЯ, до
// всякой работы: второй писатель зовётся иначе и метода не трогает ни разу. Такой
// гейт был бы вакуумным — зелёным при любом числе писателей, лишь бы они не звали
// именно это имя. Здесь единица — ОПЕРАТОР ЗАПИСИ В ТАБЛИЦУ, и она ловит обе
// реализации by construction.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА, А НЕ УМОЛЧАНА
//
// Гейт читает СТРОКОВЫЕ ЛИТЕРАЛЫ непроверочного кода Go, разобранного в дерево, и
// приписывает находку объемлющей функции. Он НЕ видит:
//   - SQL миграций (там запись законна — миграция и есть схема);
//   - проб (им положено готовить состояние);
//   - имя таблицы, СОБРАННОЕ ИЗ КУСКОВ — настоящая слепая зона, предикатом по
//     подстроке не ловится ничем.
//
// И отдельно: инъекция подаёт вход сама, поэтому исчезновение предмета из дерева
// её не трогает. Переименует ли `#1030` таблицу, снимет ли — гейт ЗАМОЛЧИТ, оставаясь
// зелёным. Поэтому предпосылка проверяется отдельно: ноль упоминаний таблицы —
// ОТКАЗ, а не «нарушений нет».

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

// roleVerbTable — проекция «роль → тип объекта × глагол».
const roleVerbTable = "kacho_iam.role_verb"

// roleRuleRefTable — проекция ОБЪЯВЛЕННЫХ сегментов правила (kacho#1030).
//
// ВТОРАЯ проекция того же объявления, и свойство единственного писателя обязано
// прийти вместе с ней: решение #1028 закрыто наполовину, если новая таблица его
// не наследует. Предмет у таблиц разный (та несёт резолвящееся, эта — каждый
// объявленный сегмент), а требование к писателю — одно.
const roleRuleRefTable = "kacho_iam.role_rule_ref"

// roleProjectionTables — таблицы, у каждой из которых писатель обязан быть один.
var roleProjectionTables = []string{roleVerbTable, roleRuleRefTable}

// mutationsOf — глаголы ЗАПИСИ. Чтение (`SELECT … FROM`) в перечень не
// входит намеренно: читателей у таблицы несколько, все считают строки, и все
// обязаны остаться законными — они и служат близнецом, на котором гейт молчит.
func mutationsOf(table string) []string {
	return []string{
		"INSERT INTO " + table,
		"UPDATE " + table,
		"DELETE FROM " + table,
	}
}

// roleVerbWriterLayer — слой, которому принадлежит SQL проекции. Писатель вне
// него — находка, даже если он один: проекция есть состояние репозитория, и
// решение «как писать» не принадлежит ни use-case, ни транспорту.
const roleVerbWriterLayer = "/internal/repo/"

// roleVerbWrite — одна найденная запись в таблицу.
type roleVerbWrite struct {
	// Func — объемлющая функция; пустое имя означает пакетный уровень (константа).
	Func string
	// Verb — какой оператор записи найден.
	Verb string
}

// roleVerbWritesIn разбирает исходник Go и возвращает записи в таблицу проекции,
// приписанные объемлющей функции, плюс число строковых литералов, называющих
// таблицу вообще (перепись предпосылки: читатели тоже считаются).
//
// Признак судит УЗЕЛ-ЛИТЕРАЛ разобранного дерева, а не текст файла: имя таблицы
// встречается и в комментариях — в том числе в комментариях, ОБЪЯСНЯЮЩИХ эту
// самую проверку, — и гейт по подстроке краснел бы на собственном объяснении.
func roleVerbWritesIn(filename, src string) ([]roleVerbWrite, int, error) {
	return projectionWritesIn(filename, src, roleVerbTable)
}

// projectionWritesIn — то же для ЛЮБОЙ из таблиц проекции.
func projectionWritesIn(filename, src, table string) ([]roleVerbWrite, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, 0, err
	}

	// enclosing сопоставляет позицию литерала имени объемлющей функции.
	type span struct {
		from, to token.Pos
		name     string
	}
	var spans []span
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			name = typeName(fn.Recv.List[0].Type) + "." + name
		}
		spans = append(spans, span{from: fn.Body.Pos(), to: fn.Body.End(), name: name})
	}

	var (
		writes   []roleVerbWrite
		mentions int
	)
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if !strings.Contains(lit.Value, table) {
			return true
		}
		mentions++
		upper := strings.ToUpper(lit.Value)
		owner := ""
		for _, s := range spans {
			if lit.Pos() >= s.from && lit.End() <= s.to {
				owner = s.name
				break
			}
		}
		for _, verb := range mutationsOf(table) {
			if strings.Contains(upper, strings.ToUpper(verb)) {
				writes = append(writes, roleVerbWrite{Func: owner, Verb: verb})
			}
		}
		return true
	})
	return writes, mentions, nil
}

// TestIAMRV112_RoleVerbProjectionHasASoleWriter — в непроверочном коде Go ровно
// ОДНА функция пишет проекцию роли, и лежит она в слое репозитория.
func TestIAMRV112_RoleVerbProjectionHasASoleWriter(t *testing.T) {
	// Таблиц ДВЕ (kacho#1030, требование Т5 приёмки
	// rule-segments-have-a-referent): у каждой проекции одного и того же
	// объявления писатель обязан быть один. Подпроба на таблицу, а не один
	// проход по обеим: перепись обязана печататься по КАЖДОЙ, иначе «писателей
	// один» на суммарном счёте зеленело бы при двух и нуле.
	for _, table := range roleProjectionTables {
		t.Run(table, func(t *testing.T) { soleWriterOf(t, table) })
	}
}

func soleWriterOf(t *testing.T, table string) {
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}

	var (
		filesRead int
		mentions  int
		// writers — ключ «путь::функция», значение — какие операторы записи там стоят.
		writers    = map[string][]string{}
		writerKeys []string
	)
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		filesRead++
		body := string(b)
		if !strings.Contains(body, table) {
			continue
		}
		writes, m, perr := projectionWritesIn(rel, body, table)
		if perr != nil {
			t.Fatalf("разбор %s: %v — файл индекса не разобран, и его молчание ничего не значит", rel, perr)
		}
		mentions += m
		for _, w := range writes {
			key := rel + "::" + w.Func
			if _, seen := writers[key]; !seen {
				writerKeys = append(writerKeys, key)
			}
			writers[key] = append(writers[key], w.Verb)
		}
	}

	t.Logf("осмотрено непроверочных файлов Go: %d; литералов, называющих %s: %d; "+
		"функций-писателей найдено: %d", filesRead, table, mentions, len(writerKeys))

	if filesRead == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	// Предпосылка: таблица вообще названа в коде. Ноль упоминаний означает либо
	// переименование (правь `roleVerbTable` вместе с ним), либо что её перестали
	// и читать, и писать. И то и другое обязано быть замечено ЗДЕСЬ, а не принято
	// за «нарушений нет»: инъекция подаёт вход сама и исчезновения предмета не видит.
	if mentions == 0 {
		t.Fatalf("имя %q не встречается в непроверочном коде НИ РАЗУ — предмета у гейта нет: "+
			"либо таблица переименована, либо её перестали читать и писать", table)
	}

	if len(writerKeys) != 1 {
		t.Errorf("писателей проекции роли %d, а обязан быть ОДИН: %v\n"+
			"Пока их двое, изменение семантики проекции обязано доехать до каждого, а промах "+
			"МОЛЧАЛИВ: обе реализации компилируются, у обеих есть пробы, и расходятся они "+
			"только на входе, который ни одна проба не подаёт.", len(writerKeys), writerKeys)
	}
	for _, key := range writerKeys {
		rel := key[:strings.Index(key, "::")]
		if !strings.Contains("/"+rel, roleVerbWriterLayer) {
			t.Errorf("%s — писатель проекции роли ВНЕ слоя репозитория (%s).\n"+
				"Проекция есть состояние репозитория: форму строки, отображение отказов и "+
				"транзакционность держит слой repo/. Решение «как писать» не принадлежит ни "+
				"use-case, ни транспорту — они решают лишь КОГДА и ДЛЯ КАКИХ ролей пересчитывать, "+
				"и зовут писателя через порт.", key, roleVerbWriterLayer)
		}
	}
}
