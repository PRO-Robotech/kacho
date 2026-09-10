// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleselfgating_test.go — ГЕЙТ: объявление «модуль гейтит отношение своим
// кодом» сходится с прод-кодом каждого модуля платформы (задача продукта #2375).
//
// # Зачем гейт заведён именно здесь
//
// Читатель объявления — служба доступа: у неё модель, и только она знает, какое
// отношение материализация ПРОИЗВОДИТ. Проверить объявление она не может — чужой
// код в её поставку не входит. Обратно: у платформы код есть, а модели нет.
//
// После разреза это два продукта без общего дерева, поэтому шов проведён так,
// чтобы НИ ОДНА координата сверки его не пересекала: объявление живёт в
// фундаменте (обе стороны тянут его зависимостью), обход и вердикт — здесь, где
// лежит прод-код. Служба доступа сюда не входит и объявлением не описывается: её
// собственный код едет вместе с ней, и она ВЫВОДИТ свой источник сама.
//
// # Что делает гейт неспособным молчать
//
// Сверка требует РАВЕНСТВА, а не включения: объявленное-и-не-читаемое — находка
// ровно так же, как читаемое-и-не-объявленное. Перепись печатается ВСЕГДА и по
// осям, поэтому «ноль находок» отличимо от «ноль прочитанного»; пустой обход и
// пустое объявление — отказ, а не согласие.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/moduleselfgating"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// selfGatingRelationLiteral — литерал отношения модели прав.
var selfGatingRelationLiteral = regexp.MustCompile(`^v_\w+$`)

// selfGatingSkipModules — модули, о которых объявление молчит ПО РЕШЕНИЮ, а не
// по недосмотру, и потому не сверяется.
//
// Перечень ИСТЕКАЕТ САМ: имя, которого нет ни в объявлении, ни в дереве, —
// находка ниже.
//
// ЗДЕСЬ СТОЯЛА ЕДИНСТВЕННАЯ ЗАПИСЬ — служба доступа (`iam`), выводившая своих
// читателей обходом собственного модуля вместо объявления в фундаменте. День
// разреза настал: служба вынесена отдельным продуктом, каталога `services/iam` в
// дереве платформы нет, скрывать стало нечего — и запись снята тем же изменением,
// которым уехала служба, ровно как здесь и было предписано.
//
// Красной её сделал сам гейт («пропущен модуль iam, а каталога services/iam в
// дереве нет»), а не чьё-то внимание: перечень истёк, как задумано.
//
// Перечень ПУСТ, и это его цель, а не поломка. Предмет гейта — сверка объявления
// фундамента с прод-кодом каждого модуля — от перечня не зависит: он держится
// обходом дерева (замер после разреза: 5 модулей, 12 пар, 724 прод-файла) и
// падает на пустом обходе тремя отдельными премисами выше.
var selfGatingSkipModules = map[string]string{}

// TestModuleSelfGatingMatchesTheProdCodeOfEachModule — сам гейт.
func TestModuleSelfGatingMatchesTheProdCodeOfEachModule(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	observed, files := selfGatingObservedLiterals(t, root)
	declared := selfGatingDeclared()

	found, c := auditModuleSelfGating(declared, observed)
	c.FilesParsed = files

	t.Logf("перепись: %s", c)

	if c.FilesParsed == 0 {
		t.Fatal("прод-файлов Go не разобрано ни одного — обход пуст, и вердикт беспредметен: " +
			"«ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if c.ModulesDeclared == 0 {
		t.Fatal("объявление фундамента пусто — сверять нечего, а читатель объявления " +
			"остался бы без единого источника третьей полосы")
	}
	if c.ModulesWalked == 0 {
		t.Fatal("ни у одного модуля не найдено литералов отношений — разбор смотрит не туда")
	}

	for _, f := range found {
		t.Errorf("%s", f)
	}
	if len(found) > 0 {
		t.Fatalf("объявление разошлось с прод-кодом — %d находка(и). Снятие: привести "+
			"pkg/moduleselfgating к тому, что модуль читает НА САМОМ ДЕЛЕ — объявление есть "+
			"единственный источник третьей полосы читателей у службы доступа, и его "+
			"расхождение с деревом она увидеть не может by construction", len(found))
	}

	// Перечень пропущенных истекает сам: имя, которого нет ни в объявлении, ни в
	// дереве, — запись без предмета.
	for m, why := range selfGatingSkipModules {
		if _, inDecl := declared[m]; inDecl {
			t.Errorf("модуль %s объявлен и одновременно пропущен (%s): два решения об одном "+
				"предмете, и действующим окажется прочитанное последним", m, why)
		}
		if !selfGatingModuleExists(t, root, m) {
			t.Errorf("пропущен модуль %s (%s), а каталога services/%s в дереве нет — "+
				"запись пропуска пережила свой предмет", m, why, m)
		}
	}
}

// selfGatingDeclared — объявление фундамента в форме предиката.
func selfGatingDeclared() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range moduleselfgating.Modules() {
		rels, ok := moduleselfgating.Relations(m)
		if !ok {
			continue
		}
		set := make(map[string]bool, len(rels))
		for _, r := range rels {
			set[r] = true
		}
		out[m] = set
	}
	return out
}

// selfGatingModuleExists — есть ли у модуля каталог в дереве платформы.
func selfGatingModuleExists(t *testing.T, root, module string) bool {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services", module), ".go")
	if err != nil {
		return false
	}
	return len(files) > 0
}

// selfGatingObservedLiterals — литералы отношений в НЕ-тестовом Go каждого
// модуля платформы, собранные РАЗБОРОМ.
//
// Разбор, а не поиск по тексту: имя отношения стоит и в комментариях (в том
// числе в комментариях, объясняющих ЭТУ ЖЕ сверку), и предикат по подстроке
// зеленел бы на собственном объяснении. Теги полей исключены явно — они не
// читаются как решение о доступе.
//
// Состав берётся у ИНДЕКСА отслеживаемых файлов, а не обходом диска: под
// `services/` на всякой машине, где поднимали стенд, лежат распаковки чартов и
// отчёты прогонов, и найденный в них литерал сошёл бы за читателя.
func selfGatingObservedLiterals(t *testing.T, root string) (map[string]map[string]bool, int) {
	t.Helper()
	servicesDir := filepath.Join(root, "services")
	files, err := treecorpus.UnderWithSuffix(servicesDir, ".go")
	if err != nil {
		t.Fatalf("индекс отслеживаемых файлов под services/: %v — проверка НЕ ИСПОЛНЯЛАСЬ", err)
	}

	out := map[string]map[string]bool{}
	parsed := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(servicesDir, path)
		if rerr != nil {
			t.Fatalf("координата %s от %s: %v", path, servicesDir, rerr)
		}
		module, _, ok := strings.Cut(filepath.ToSlash(rel), "/")
		if !ok {
			continue
		}
		if _, skip := selfGatingSkipModules[module]; skip {
			continue
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("разбор %s: %v — прод-файл, который не читается, есть слепая зона "+
				"предиката, а не «литералов в нём нет»", path, perr)
		}
		parsed++

		tags := map[*ast.BasicLit]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if fld, ok := n.(*ast.Field); ok && fld.Tag != nil {
				tags[fld.Tag] = true
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING || tags[bl] {
				return true
			}
			v, uerr := strconv.Unquote(bl.Value)
			if uerr != nil || !selfGatingRelationLiteral.MatchString(v) {
				return true
			}
			if out[module] == nil {
				out[module] = map[string]bool{}
			}
			out[module][v] = true
			return true
		})
	}
	return out, parsed
}
