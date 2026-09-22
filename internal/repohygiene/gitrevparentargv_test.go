// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// gitrevparentargv_test.go — держатель одного свойства: ВОПРОС О РОДИТЕЛЕ HEAD
// ЗАДАЁТСЯ В ПАКЕТЕ ОДНИМ НАБОРОМ АРГУМЕНТОВ.
//
// # Почему это свойство нельзя проверить прогоном
//
// Фикстура [gateBaseRealExitError] обязана воспроизводить ТО, что приходит на
// боевой путь: `*exec.ExitError` с кодом 1. Но код 1 — свойство очень широкого
// класса вопросов к `git`, а не этого вопроса: `cat-file -e` о несуществующем
// объекте, в другом репозитории, другой подкомандой даёт ровно тот же класс и
// ровно тот же код. Измерено инъекцией 2026-09-22: подмена
// `rev-parse --verify --quiet HEAD^` на `cat-file -e <несуществующий объект>`
// оставила ОБА стража фикстуры (класс отказа и код) молчащими — `ok`, EXIT=0.
//
// Значит тождество команд ПРОГОНОМ не проверяется в принципе: оба конца дают
// неотличимый исход. Проверяется оно только СТРОЕНИЕМ — тем, что обе стороны
// берут аргументы из одного объявления и обойти его нечем.
//
// # Что именно судится
//
//  1. ДОМ ОДИН. Объявление [gitRevParentArgv] в пакете ровно одно.
//  2. ОБА СЛОВА ВОПРОСА СТОЯТ ЛИТЕРАЛАМИ РОВНО В ОДНОМ МЕСТЕ — в теле дома.
//     Судятся СЛОВА, а не способ их записи: любой вызов любой функции и любой
//     составной литерал, несущий рядом `rev-parse` и `HEAD^`, есть вторая
//     запись вопроса. Прежняя редакция снимала признак только с прямых
//     литералов у `gitenv.Command`, и это закрывало форму, а не класс: 20 из 87
//     вызовов ПАКЕТА подают аргументы раскрытием `args...` (в каталоге вызовов
//     90 — три из них в чужом пакете, и ни один из трёх не раскрывает; единица
//     здесь та же, что у переписи ниже), и один из них — обёртка
//     `run` В ТОЙ ЖЕ ФИКСТУРЕ, чью вторую запись эта полоса снимала. Измерено
//     инъекцией 2026-09-22: `run("rev-parse", "--verify", "--quiet", "HEAD^")`,
//     дописанная в gateBaseOriginRepo, прошла молча — `ok`, EXIT=0.
//  3. ВОПРОС ЗАДАН ИЗ ДОМА ОБЕИМИ СТОРОНАМИ. Вызов вида
//     `gitenv.Command(<дерево>, gitRevParentArgv()...)` есть и в боевом файле, и
//     в пробном. Судится именно ВЫЗОВ, а не упоминание имени: фикстура называет
//     `gitRevParentArgv()` ещё и в текстах своих отказов, и считать упоминания
//     значило бы оставить дыру — подмену спрашивающей команды на `cat-file`
//     такая перепись пережила бы молча, а это ровно снятый дефект.
//
// ПРЕДМЕТ ОБЪЯВЛЕН ЗДЕСЬ И СВЕРЯЕТСЯ С ДОМОМ. Прежняя редакция брала слова у
// дома ПОЗИЦИЕЙ (`argv[0]` и последний элемент), чтобы не заводить третьей их
// записи, — и позиция предметом не оказалась: хвостовой `"--"`, дописанный к
// телу дома, есть та же самая команда
// (`git rev-parse --verify --quiet HEAD^ --`), но объект сместился, и
// распознаватель стал судить пару `"rev-parse"+"--"`. Измерено инъекцией
// 2026-09-22: настоящая вторая запись вопроса при этом прошла молча — `PASS`,
// EXIT=0, тогда как БЕЗ дописки она же даёт `FAIL` с координатой.
//
// Выписанные слова второй записью не становятся, потому что не расходятся
// МОЛЧА: [gitRevParentWordsAgreeWithHome] сверяет их с телом дома на каждом
// прогоне, и расхождение есть ОТКАЗ, а не тихая смена предмета. Дом волен
// менять что угодно, кроме этих двух слов; сменил — гейт откажет и потребует
// решить, тот ли это предмет.
//
// ЗАКОННЫЙ БЛИЗНЕЦ — чужой вопрос о том же объекте (`log … HEAD^`): одного
// `HEAD^` мало, нужны ОБА слова рядом, иначе гейт краснел бы на законном.

// gitRevParentHome — имя общего дома аргументов вопроса о родителе.
const gitRevParentHome = "gitRevParentArgv"

// Предмет гейта — ДВА СЛОВА, ОБЪЯВЛЕННЫЕ ЗДЕСЬ.
//
// Позицией их брать нельзя (довод и замер — в шапке файла): позиция есть
// свойство записи, а не предмета, и правка тела дома молча уводила
// распознаватель на другую пару.
//
// Второй записью они не становятся: их сверяет с домом
// [gitRevParentWordsAgreeWithHome], и расхождение — отказ.
const (
	gitRevParentVerb   = "rev-parse"
	gitRevParentObject = "HEAD^"
)

// gitRevParentWordsAgreeWithHome — СВЕРКА объявленного предмета с домом.
//
// Требование к гейту: после ЛЮБОЙ правки тела дома он обязан либо судить ТОТ ЖЕ
// предмет, либо отказать. Сверка вхождением, а не равенством всего набора,
// делит правки ровно надвое: дописка флага, пути или разделителя предмета не
// трогает — гейт продолжает судить те же два слова; смена подкоманды или
// объекта трогает — и тогда отказ, потому что «какой вопрос мы теперь судим»
// решает человек, а не порядок элементов среза.
func gitRevParentWordsAgreeWithHome(t *testing.T) {
	t.Helper()
	argv := gitRevParentArgv()
	for _, w := range []string{gitRevParentVerb, gitRevParentObject} {
		if !slices.Contains(argv, w) {
			t.Fatalf("дом `%s` отдаёт %v, и слова %q среди них НЕТ — распознаватель "+
				"судит один вопрос, а дом задаёт другой, и молчание гейта ничего не "+
				"значит. Исходов два: вернуть слово дому либо объявить здесь новый "+
				"предмет", gitRevParentHome, argv, w)
		}
	}
}

// carriesBothWords — несёт ли этот список выражений ОБА слова вопроса прямыми
// строковыми литералами.
//
// Список — это либо аргументы вызова, либо элементы составного литерала: обе
// формы суть «слова, записанные рядом», и различать их незачем.
func carriesBothWords(exprs []ast.Expr, verb, object string) bool {
	var seenVerb, seenObject bool
	for _, e := range exprs {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			continue
		}
		switch v {
		case verb:
			seenVerb = true
		case object:
			seenObject = true
		}
	}
	return seenVerb && seenObject
}

// packageCallSites — КТО ЗОВЁТ функцию пакета, с координатами, раздельно по
// боевым и пробным файлам.
//
// Раздельно намеренно: свойство «фикстура берёт команду там же, где боевой
// путь» есть утверждение именно о ДВУХ сторонах, и одно суммарное число его не
// выражает — два боевых зовущих при нуле пробных дали бы то же «2».
func packageCallSites(t *testing.T, name string) (prod, test []string) {
	t.Helper()
	_ = repohygienePackageWalk(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != name {
				return true
			}
			coord := fmt.Sprintf("%s:%d", filepath.Base(path), fset.Position(id.Pos()).Line)
			if strings.HasSuffix(path, "_test.go") {
				test = append(test, coord)
			} else {
				prod = append(prod, coord)
			}
			return true
		})
	})
	return prod, test
}

// TestGitRevParentQuestionHasASingleArgvHome — свойство из шапки файла.
func TestGitRevParentQuestionHasASingleArgvHome(t *testing.T) {
	t.Parallel()

	gitRevParentWordsAgreeWithHome(t)
	verb, object := gitRevParentVerb, gitRevParentObject

	var homes, atHome, offHome, prodAskers, probeAskers []string
	commandCalls := 0

	census := repohygienePackageWalk(t, func(path string, fset *token.FileSet, file *ast.File) {
		base := filepath.Base(path)

		// Границы дома: слова вопроса законны ВНУТРИ него и только там.
		// Берутся разбором, а не по имени файла: дом вправе переехать.
		var homeFrom, homeTo token.Pos
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != gitRevParentHome {
				continue
			}
			homes = append(homes, fmt.Sprintf("%s:%d", base, fset.Position(fn.Name.Pos()).Line))
			homeFrom, homeTo = fn.Pos(), fn.End()
		}
		inHome := func(p token.Pos) bool {
			return homeFrom != token.NoPos && p >= homeFrom && p < homeTo
		}

		ast.Inspect(file, func(n ast.Node) bool {
			var exprs []ast.Expr
			var at token.Pos
			switch x := n.(type) {
			case *ast.CallExpr:
				exprs, at = x.Args, x.Lparen
				// Половина вторая: спрашивающие ИЗ ДОМА. Здесь предмет — именно
				// `gitenv.Command`, потому что утверждение о нём — «обе стороны
				// задают вопрос одной и той же командой».
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Command" {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "gitenv" {
						commandCalls++
						coord := fmt.Sprintf("%s:%d", base, fset.Position(x.Lparen).Line)
						for _, a := range x.Args {
							inner, ok := a.(*ast.CallExpr)
							if !ok || x.Ellipsis == token.NoPos {
								continue
							}
							id, ok := inner.Fun.(*ast.Ident)
							if !ok || id.Name != gitRevParentHome {
								continue
							}
							if strings.HasSuffix(base, "_test.go") {
								probeAskers = append(probeAskers, coord)
							} else {
								prodAskers = append(prodAskers, coord)
							}
						}
					}
				}
			case *ast.CompositeLit:
				exprs, at = x.Elts, x.Lbrace
			default:
				return true
			}

			if !carriesBothWords(exprs, verb, object) {
				return true
			}
			coord := fmt.Sprintf("%s:%d", base, fset.Position(at).Line)
			if inHome(at) {
				atHome = append(atHome, coord)
			} else {
				offHome = append(offHome, coord)
			}
			return true
		})
	})

	// Предпосылка распознавателя: он обязан ВИДЕТЬ вызовы `gitenv.Command`.
	// Ноль вызовов означает, что разбор ослеп либо пакет переехал, — и тогда
	// «спрашивающих мимо дома ноль» есть «мы не смотрели», а не находка.
	if commandCalls == 0 {
		t.Fatalf("обход пакета (%s) не нашёл НИ ОДНОГО вызова `gitenv.Command` — "+
			"распознаватель слеп; молчание здесь означало бы «не смотрели»", census)
	}

	t.Logf("перепись: %s · вызовов `gitenv.Command` %d · домов `%s` %d %v · мест со "+
		"словами %q+%q: в доме %d %v, мимо дома %d %v · спрашивающих ИЗ ДОМА: "+
		"боевых %d %v, пробных %d %v",
		census, commandCalls, gitRevParentHome, len(homes), homes, verb, object,
		len(atHome), atHome, len(offHome), offHome,
		len(prodAskers), prodAskers, len(probeAskers), probeAskers)

	switch len(homes) {
	case 1: // единственный дом — то, ради чего всё
	case 0:
		t.Errorf("объявления `%s` в пакете НЕТ: вопрос о родителе HEAD задаётся каждой "+
			"стороной своими руками, и разойтись они могут молча — ровно это измерено "+
			"инъекцией (см. шапку файла)", gitRevParentHome)
	default:
		t.Errorf("объявлений `%s` в пакете %d (%v) — у одного вопроса два дома, и "+
			"сойтись им нечем", gitRevParentHome, len(homes), homes)
	}

	// ВТОРАЯ ПРЕДПОСЫЛКА: слова обязаны найтись В САМОМ ДОМЕ. Ноль означал бы,
	// что распознаватель ищет не то, что дом спрашивает, — и тогда «мимо дома
	// ноль» снова есть «мы не смотрели».
	if len(atHome) != 1 {
		t.Fatalf("слова вопроса %q+%q стоят в теле дома `%s` %d раз(а) %v, ожидалась "+
			"ровно одна запись — распознаватель и дом разошлись, и его молчание "+
			"ничего не значит", verb, object, gitRevParentHome, len(atHome), atHome)
	}

	if len(offHome) > 0 {
		t.Errorf("слова вопроса %q+%q стоят рядом МИМО общего дома `%s` — координаты "+
			"%v; это вторая запись того же вопроса, и разойтись с первой она может "+
			"молча. Судится запись СЛОВ, а не её форма: прямые литералы, элементы "+
			"`[]string{…}` и аргументы любой обёртки суть одно и то же",
			verb, object, gitRevParentHome, offHome)
	}

	if len(prodAskers) == 0 {
		t.Errorf("НИ ОДИН боевой вызов `gitenv.Command` не спрашивает из дома `%s` — "+
			"проба сверялась бы сама с собой, а боевой путь спрашивал бы своё",
			gitRevParentHome)
	}
	if len(probeAskers) == 0 {
		t.Errorf("НИ ОДНА проба не спрашивает `gitenv.Command` из дома `%s` — фикстура "+
			"задаёт СВОЙ вопрос, и её исход ничего не говорит о боевом пути: код 1 "+
			"приходит и от другой подкоманды, и от другого репозитория",
			gitRevParentHome)
	}
}
