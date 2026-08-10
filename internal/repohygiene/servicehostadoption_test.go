// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// servicehostadoption_test.go — гейт против второй поверхности правки контура:
// композиционный корень сервиса не собирает сервер и не держит значений
// интерсепторного типа.
//
// # Предмет
//
// Пока сервис собирает слушатели сам, порядок звеньев держится тем, что автор
// написал то же, что сосед. Семь корней — семь мест, где это может разойтись, и
// разошлись они уже дважды: звено восстановления паники существовало в пяти
// независимых реализациях, а круг отправителей проверялся четырьмя разными
// предикатами. Носитель (`pkg/servicehost`) убирает предмет разногласия, но
// сам по себе не мешает завести восьмую сборку рядом.
//
// # На что гейт целится и почему НЕ на `grpc.NewServer`
//
// Прямых вызовов `grpc.NewServer` в не-тестовом коде сервисов **ноль** — и были
// ноль на обеих ревизиях. Гейт по ним был бы зелен всегда и ничего не держал:
// ровно тот класс «форма без содержания», который мы ловим в продуктовом коде.
// Настоящая единица — `grpcsrv.NewServer`, конструктор сервера общего
// фундамента: на нём стоят все существующие слушатели.
//
// Вторая половина — ЗНАЧЕНИЯ интерсепторного типа. Без неё сервис вправе
// перестать звать конструктор, но продолжить собирать цепочку и передавать её
// куда-то ещё; гейт бы молчал, а вторая поверхность правки осталась бы.
//
// # Почему разбор AST, а не текст
//
// Имя конструктора встречается в комментариях (перепись по дереву: из 15
// вхождений одно стоит в комментарии), а имена интерсепторных типов — в
// комментариях и строковых литералах тем более. Текстовый гейт покраснел бы на
// собственной документации и позеленел бы на вынесенной в строку сборке.
// Локальное имя пакета берётся из ОБЪЯВЛЕНИЯ ИМПОРТА каждого файла: файл вправе
// импортировать `grpc` под своим именем, и поиск по «grpc.» был бы на этом слеп.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

const (
	grpcPkgPath    = "google.golang.org/grpc"
	grpcsrvPkgPath = "github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	// serverCtor — конструктор слушателя общего фундамента. Единица счёта.
	serverCtor = "NewServer"
)

// interceptorTypeNames — типы, значение которых в композиционном корне и есть
// «своя цепочка». Оба вида обязательны: «у нас тут нет стримов» — неверное
// основание, стрим-обработчик паникует ровно так же, как unary.
var interceptorTypeNames = map[string]struct{}{
	"UnaryServerInterceptor":  {},
	"StreamServerInterceptor": {},
}

// hostAdoptionException — сервис, чей композиционный корень ЕЩЁ не переведён на
// носитель.
//
// # Почему перечень, а не молчание
//
// Перевод семерых — не один коммит: между первым переведённым и последним у
// части сервисов своя сборка ещё жива. Молчаливое «гейт их не видит» запрещено:
// тогда самая нагруженная поверхность остаётся вне контура, будучи объявленной
// внутри него, и отличить «ещё не перевели» от «гейт сломался» нечем.
//
// # Самоистечение
//
// Запись, у которой БОЛЬШЕ НЕТ находок, — сама находка: сервис переведён, а
// исключение осталось и готово укрыть следующую сборку, которую в этом корне
// заведут. Предикат снятия внешний — состояние дерева, — а не выведенный из
// этого же перечня.
type hostAdoptionException struct {
	// why — почему запись существует и что должно случиться, чтобы она ушла.
	why string
}

// hostAdoptionExceptions — сервисы, ожидающие перевода. Ключ — имя каталога под
// `services/`.
var hostAdoptionExceptions = map[string]hostAdoptionException{
	"iam":     {why: "владелец модели прав; переводится своей под-фазой — у него собственный рубеж на внутреннем слушателе, и снятие рубежа есть решение о доступе, а не об оформлении контура"},
	"vpc":     {why: "переводится вслед за первым: 11 файлов с проводкой сужателя, каждый обязан сойтись с каталогом по обеим половинам О3/О4"},
	"compute": {why: "переводится вслед за первым"},
	"nlb":     {why: "переводится вслед за первым"},
}

// adoptionFinding — одна находка с координатой.
type adoptionFinding struct {
	service string
	where   string // файл:строка
	what    string
}

// adoptionResult — исход обхода вместе с ПЕРЕПИСЬЮ осмотренного.
type adoptionResult struct {
	findings []adoptionFinding
	roots    int // сколько композиционных корней осмотрено
	files    int
	summary  string
}

// TestCompositionRootsCarryNoServerConstructionOfTheirOwn — сам гейт.
//
// Что делать, если сработал, — два исхода, третьего нет:
//
//  1. корень ещё не переведён на носитель → перевести его (см. `pkg/servicehost`),
//     а не заводить исключение: перечень ниже закрыт и пополняется только вместе
//     с решением о порядке фаз;
//  2. это не сборка сервера, а совпадение формы → уточнить распознавание, а не
//     добавлять запись.
//
// Проверено инъекцией в обе стороны (`servicehostadoption_injection_test.go`).
func TestCompositionRootsCarryNoServerConstructionOfTheirOwn(t *testing.T) {
	res := auditHostAdoption(t, repoRoot(t))
	t.Log(res.summary)

	if res.roots == 0 {
		t.Fatal("не осмотрено ни одного композиционного корня — «ноль находок» здесь означало бы " +
			"«ноль прочитанного», а не исправное дерево")
	}

	byService := map[string][]adoptionFinding{}
	for _, f := range res.findings {
		byService[f.service] = append(byService[f.service], f)
	}

	// (1) находки у сервиса, которому исключение не выдано.
	var unexpected []string
	for svc, fs := range byService {
		if _, excused := hostAdoptionExceptions[svc]; excused {
			continue
		}
		for _, f := range fs {
			unexpected = append(unexpected, fmt.Sprintf("%s: %s — %s", f.service, f.where, f.what))
		}
	}
	// (2) САМОИСТЕЧЕНИЕ: исключение, которому больше нечего исключать.
	var expired []string
	for svc, ex := range hostAdoptionExceptions {
		if len(byService[svc]) == 0 {
			expired = append(expired, fmt.Sprintf("%s: исключение потеряло предмет — своей сборки "+
				"сервера в корне больше нет. Причина, записанная при заведении: %s. Снимите запись: "+
				"оставленная, она укроет следующую сборку, которую здесь заведут", svc, ex.why))
		}
	}

	sort.Strings(unexpected)
	sort.Strings(expired)
	if len(unexpected) > 0 {
		t.Errorf("композиционные корни собирают сервер сами вне перечня ожидающих перевода (%d):\n  %s",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}
	if len(expired) > 0 {
		t.Errorf("исключения без предмета (%d):\n  %s", len(expired), strings.Join(expired, "\n  "))
	}
}

// auditHostAdoption обходит композиционные корни, ВЫВЕДЕННЫЕ из дерева.
//
// Область не выписана списком: восьмой сервис попадает под гейт в момент
// появления, а не тогда, когда кто-нибудь снова пройдёт по дереву руками.
// Состав берётся у git (`treecorpus`), а не с диска: вердикт обязан быть
// свойством коммита, а не рабочего каталога с чужими распаковками.
func auditHostAdoption(t *testing.T, root string) adoptionResult {
	t.Helper()
	var res adoptionResult

	servicesDir := filepath.Join(root, "services")
	tracked, err := treecorpus.Under(servicesDir)
	if err != nil {
		t.Fatalf("состав дерева под %s не читается: %v", servicesDir, err)
	}

	roots := map[string]struct{}{}
	seenFinding := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, abs := range tracked {
		rel, err := filepath.Rel(servicesDir, abs)
		if err != nil {
			t.Fatalf("относительный путь для %s: %v", abs, err)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// Композиционный корень — `services/<svc>/cmd/**`.
		if len(parts) < 3 || parts[1] != "cmd" {
			continue
		}
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		svc := parts[0]
		roots[svc+"/"+parts[2]] = struct{}{}
		res.files++

		file, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор %s: %v", abs, perr)
		}
		// Дедупликация по координате И существу: `[]grpc.UnaryServerInterceptor`
		// в одной строке даёт несколько узлов одного и того же предмета, и
		// печатать их порознь значит завышать число находок — то есть врать в ту
		// сторону, в которую врать особенно легко.
		for _, f := range scanFileForOwnAssembly(fset, file, svc, relTo(root, abs)) {
			key := f.where + "\x00" + f.what
			if _, dup := seenFinding[key]; dup {
				continue
			}
			seenFinding[key] = struct{}{}
			res.findings = append(res.findings, f)
		}
	}
	res.roots = len(roots)
	res.summary = fmt.Sprintf(
		"перепись: композиционных корней %d, не-тестовых файлов в них %d, находок %d, "+
			"исключений объявлено %d",
		res.roots, res.files, len(res.findings), len(hostAdoptionExceptions))
	return res
}

// scanFileForOwnAssembly читает ИСПОЛНЯЕМУЮ ЧАСТЬ одного файла.
func scanFileForOwnAssembly(fset *token.FileSet, file *ast.File, svc, where string) []adoptionFinding {
	// Локальные имена пакетов — из объявлений импорта этого файла.
	local := map[string]string{} // локальное имя -> путь пакета
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		local[name] = path
	}
	pkgOf := func(x ast.Expr) string {
		id, ok := x.(*ast.Ident)
		if !ok {
			return ""
		}
		return local[id.Name]
	}

	var out []adoptionFinding
	add := func(pos token.Pos, what string) {
		out = append(out, adoptionFinding{
			service: svc,
			where:   fmt.Sprintf("%s:%d", where, fset.Position(pos).Line),
			what:    what,
		})
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != serverCtor {
				return true
			}
			switch pkgOf(sel.X) {
			case grpcsrvPkgPath:
				add(sel.Pos(), "вызов конструктора слушателя общего фундамента: сервер собирается "+
					"в корне сервиса, значит порядок звеньев остаётся его собственным делом")
			case grpcPkgPath:
				add(sel.Pos(), "прямой вызов конструктора сервера upstream: слушатель поднимается "+
					"мимо контура целиком")
			}
		case *ast.SelectorExpr:
			// Значение интерсепторного типа: тип в объявлении, в литерале, в
			// сигнатуре, в приведении. Все они — узлы SelectorExpr в позиции
			// типа, и AST отличает их от комментария и строки по построению.
			if _, isInterceptor := interceptorTypeNames[sel(node)]; !isInterceptor {
				return true
			}
			if pkgOf(node.X) != grpcPkgPath {
				return true
			}
			add(node.Pos(), "значение интерсепторного типа "+node.Sel.Name+" в композиционном корне: "+
				"цепочка собирается на стороне сервиса — это вторая поверхность правки контура")
		}
		return true
	})
	return out
}

func sel(n *ast.SelectorExpr) string { return n.Sel.Name }
