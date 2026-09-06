// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iamcontourdecisionbasis_test.go — проба, держащая ОСНОВАНИЕ решения владельца
// оставить iam вне контура носителя.
//
// # Зачем отдельная проба, если исключение уже самоистекает
//
// Самоистечение соседнего гейта (`servicehostadoption_test.go`) читает ПРЕДМЕТ
// записи: есть ли у сервиса своя сборка сервера. Для записи-решения этого мало,
// и мало по построению, а не по недосмотру.
//
// У решения два срока годности, и они независимы:
//
//   - предмет — своя сборка сервера у iam. Исчезнет ⇒ iam перевели, и соседний
//     гейт это скажет;
//   - ОСНОВАНИЕ — «носитель ставит одно звено решения о доступе, а владелец
//     модели держит пять». Оно может стать ложным, ПОКА предмет жив: сборка на
//     месте, находки на месте, запись выглядит действующей — а обоснование под
//     ней уже неверно, и никто об этом не узнает.
//
// Второй случай — не гипотетический: ровно так исключения переживают свой довод.
// Поэтому основание пинится здесь, отдельным предикатом по дереву.
//
// # Что именно утверждается
//
// Композиционный корень владельца модели по-прежнему несёт ВСЕ рубежи, ради
// которых решение принято. Не «сколько-то», не «хотя бы один»: НАБОР целиком,
// с обеих сторон — пропажа и прибавление одинаково означают, что основание
// пора перечитать.
//
// Проба НЕ утверждает, что рубежи правильные: их правильность держат
// собственные пробы `services/iam/internal/authzguard/*_test.go`. Здесь только
// то, ради чего запись существует, — что их по-прежнему пять и что носитель
// по-прежнему не умеет их выразить.
//
// # Почему разбор AST, а не текст
//
// Имена всех шести проводок стоят рядом в объяснительных комментариях самой
// цепочки (`serve.go` описывает порядок звеньев прозой перед тем, как их
// собрать). Текстовый предикат считал бы эти упоминания и остался бы зелёным на
// корне, где рубеж описан, но снят, — то есть ровно на том состоянии, которое
// проба обязана поймать.
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

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// authzguardPkgPath — дом рубежей владельца модели.
const authzguardPkgPath = "github.com/PRO-Robotech/kaname/internal/authzguard"

// modelOwnerRoot — композиционный корень владельца модели, относительно
// `services/`.
const modelOwnerRoot = "iam/cmd/kaname"

// modelOwnerRubicons — проводки, которыми рубежи владельца модели попадают в
// цепочку.
//
// Проводок СЕМЬ, рубежей ПЯТЬ: у политики вызывающего своя реализация на каждом
// слушателе, у анти-анонима — своя на каждый вид вызова (unary и stream). Все
// они обязаны быть на месте: «на внутреннем есть, на публичном нет» — это не
// «рубеж ослаб наполовину», а дыра целиком на той поверхности, где его нет.
//
// Число «семь» здесь не выписано из головы: первая редакция объявила шесть, и
// проба на настоящем дереве нашла седьмую проводку сама (см. ниже про инъекцию).
//
// # Почему «сконструирован» — НЕ то же, что «на месте» (найдено инъекцией)
//
// Первая редакция этой пробы считала ВЫЗОВ конструктора и на том успокаивалась.
// Инъекция показала, чего такая проба не видит: рубеж, который по-прежнему
// конструируется, но ВЫНУТ ИЗ ЦЕПОЧКИ, оставлял её зелёной. Это тот самый класс,
// который гейты этого дерева ловят в чужом коде, — форма без содержания, и
// написать его в собственной пробе оказалось так же легко.
//
// Поэтому предикат ниже требует не конструирования, а ПОПАДАНИЯ В ЦЕПОЧКУ:
// значение рубежа обязано стоять элементом собираемого среза интерсепторов.
// Конструктор, чей результат никуда не доезжает, красит пробу.
// # Проводка и рубеж — разные единицы, и считать их надо порознь
//
// Перечень ниже отображает ПРОВОДКУ в РУБЕЖ, потому что отношение не взаимно
// однозначное: у анти-анонима две проводки (unary и stream), у политики
// вызывающего — по одной на слушатель. Перепись печатает обе величины, а число
// рубежей сверяется с тем, которое называет основание решения: иначе «пять» в
// записи и состав в коде разъехались бы молча — ровно то расхождение, ради
// которого проба и заведена.
var modelOwnerRubicons = map[string]string{
	"AntiAnonymousUnary":    rubiconAntiAnonymous,
	"AntiAnonymousStream":   rubiconAntiAnonymous,
	"NewCallerPolicy":       rubiconCallerPolicy,
	"NewPublicCallerPolicy": rubiconCallerPolicy,
	"NewSystemViewerFloor":  rubiconSystemViewerFloor,
	"NewACRFloor":           rubiconStepUpFloor,
	"DenyDetailUnary":       rubiconDenyReason,
	"NewOwnDoor":            rubiconOwnDoor,
}

// Рубежи владельца модели — те самые, на которых стоит решение оставить iam вне
// контура носителя.
const (
	rubiconAntiAnonymous     = "анти-аноним: неаутентифицированный вызывающий не доходит до мутирующего RPC"
	rubiconCallerPolicy      = "пер-RPC политика вызывающего (своя на КАЖДОМ слушателе)"
	rubiconSystemViewerFloor = "этаж system_viewer на читающих RPC"
	rubiconStepUpFloor       = "этаж step-up (required_acr_min)"
	rubiconDenyReason        = "машиночитаемая причина отказа на полосе, где край не проверяет"
	// Шестой рубеж — СОБСТВЕННАЯ ДВЕРЬ: пообъектный вопрос о доступе на
	// публичном слушателе. Появился последним и не от роста, а от закрытия дыры:
	// до него пообъектного вопроса на своих слушателях iam не задавал ВОВСЕ,
	// полагаясь на край. Вынесенный в чужое облако iam края не имеет.
	rubiconOwnDoor = "собственная дверь: пообъектный вопрос о доступе на публичном слушателе"
)

// modelOwnerRubiconCount — сколько рубежей называет ОСНОВАНИЕ решения.
//
// Величина стоит здесь, а не выводится из карты выше: выведенная, она
// подтверждала бы сама себя и молчала бы ровно тогда, когда состав рубежей
// поехал. Разойдётся с картой — красное, и чинится это перечитыванием решения,
// а не правкой числа.
const modelOwnerRubiconCount = 6

// TestModelOwnerStillCarriesTheRubiconsTheDecisionRestsOn — сама проба.
//
// Что делать, если сработала, — три исхода, и ни один не «поправить перечень
// выше, чтобы позеленело»:
//
//  1. рубеж СНЯТ → это ослабление контура владельца модели, разбирается как
//     находка безопасности, а не как расхождение пробы с деревом;
//  2. рубеж ПЕРЕЕХАЛ (иначе назван, собран в другом месте) → поправить проводку
//     здесь И перечитать основание решения: оно ссылается на состав рубежей;
//  3. рубежей стало МЕНЬШЕ, потому что их научился выражать носитель → основание
//     решения ИСТЕКЛО по внешнему факту. Решение переоткрывается ВЛАДЕЛЬЦЕМ; ни
//     эта проба, ни запись исключения не вправе закрыть вопрос сами.
//
// # Чем доказана (инъекция в обе стороны, на настоящем дереве)
//
//   - рубеж ВЫНУТ из обеих цепочек, конструктор оставлен на месте → красный,
//     называет проводку (`NewACRFloor`, `NewSystemViewerFloor` — проверено на
//     двух разных). Это и есть та инъекция, которую первая редакция пробы НЕ
//     ловила: она считала вызов конструктора и зеленела на рубеже, который
//     построен и никуда не подключён;
//   - законный соседний вызов того же пакета, не являющийся рубежом
//     (`authzguard.ReadFloorRPCs()`), добавленный в корень → молчит. Без этого
//     контроля проба ловила бы форму («тронули пакет»), а не существо;
//   - проводка, которой перечень не знает → красный. Проверено не синтетикой:
//     ветка ожила на настоящем дереве и сразу нашла `AntiAnonymousStream`,
//     стоявший в цепочке и отсутствовавший в перечне. Так и выяснилось, что
//     проводок семь, а рубежей за ними пять;
//   - ноль прочитанных файлов → падение с этим текстом, а не «рубежей не
//     найдено».
func TestModelOwnerStillCarriesTheRubiconsTheDecisionRestsOn(t *testing.T) {
	ex, declared := hostAdoptionExceptions["iam"]
	if !declared || ex.kind != adoptionDecided {
		t.Skip("iam больше не объявлен решением владельца — основание держать не за что; " +
			"предмет этой пробы задаёт запись в servicehostadoption_test.go")
	}
	if strings.TrimSpace(ex.basis) == "" {
		t.Fatal("запись-решение обязана нести основание: без него нечего пинить, и самоистечение " +
			"по внешнему факту становится объявлением без предмета")
	}

	root := repoRoot(t)
	wired, files := scanModelOwnerRubicons(t, root)

	held := map[string]struct{}{}
	for name := range wired {
		if rubicon, ok := modelOwnerRubicons[name]; ok {
			held[rubicon] = struct{}{}
		}
	}
	t.Logf("перепись: не-тестовых файлов в корне владельца модели %d, проводок в цепочках найдено %d "+
		"(объявлено %d), рубежей за ними %d (основание называет %d)",
		files, len(wired), len(modelOwnerRubicons), len(held), modelOwnerRubiconCount)

	if files == 0 {
		t.Fatalf("не прочитано ни одного файла в services/%s — «рубежи не найдены» здесь означало бы "+
			"«ничего не читал», а не снятый контур", modelOwnerRoot)
	}

	var missing, extra []string
	for name, what := range modelOwnerRubicons {
		if _, ok := wired[name]; !ok {
			missing = append(missing, fmt.Sprintf("%s (%s)", name, what))
		}
	}
	for name, where := range wired {
		if _, ok := modelOwnerRubicons[name]; !ok {
			extra = append(extra, fmt.Sprintf("%s — %s", name, where))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	// Число рубежей — часть основания, а не украшение переписи: решение стоит на
	// «у владельца их пять».
	if len(held) != modelOwnerRubiconCount && len(missing) == 0 && len(extra) == 0 {
		t.Errorf("рубежей за проводками %d, а основание решения называет %d — состав сошёлся, "+
			"а счёт нет. Разошлись перечень проводок и число в основании; чинится перечитыванием "+
			"решения, а не правкой числа", len(held), modelOwnerRubiconCount)
	}

	if len(missing) > 0 {
		t.Errorf("ОСНОВАНИЕ решения оставить iam вне контура изменилось: рубежей в корне владельца "+
			"модели меньше объявленного (%d не найдено):\n  %s\n\nЕсли рубеж снят — это ослабление "+
			"контура, а не расхождение пробы. Если его научился выражать носитель — основание "+
			"истекло, решение переоткрывает ВЛАДЕЛЕЦ. Записанное основание: %s",
			len(missing), strings.Join(missing, "\n  "), ex.basis)
	}
	if len(extra) > 0 {
		t.Errorf("в корне владельца модели появился рубеж, которого основание решения не называет "+
			"(%d):\n  %s\n\nОснование перечисляет состав рубежей — прибавление делает его неполным "+
			"ровно так же, как пропажа делает его ложным. Допишите рубеж в перечень пробы И в "+
			"основание записи", len(extra), strings.Join(extra, "\n  "))
	}
}

// scanModelOwnerRubicons обходит композиционный корень владельца модели и
// собирает проводки рубежей вместе с координатой.
//
// Состав берётся у git (`treecorpus`), а не с диска: вердикт обязан быть
// свойством коммита. Локальное имя пакета — из ОБЪЯВЛЕНИЯ ИМПОРТА каждого файла:
// корень вправе импортировать `authzguard` под своим именем, и поиск по
// «authzguard.» был бы на этом слеп.
func scanModelOwnerRubicons(t *testing.T, root string) (map[string]string, int) {
	t.Helper()

	servicesDir := filepath.Join(root, "services")
	tracked, err := treecorpus.Under(filepath.Join(servicesDir, modelOwnerRoot))
	if err != nil {
		t.Fatalf("состав дерева под services/%s не читается: %v", modelOwnerRoot, err)
	}

	wired := map[string]string{}
	files := 0
	fset := token.NewFileSet()
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		files++

		file, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор %s: %v", abs, perr)
		}

		local := ""
		for _, imp := range file.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil || path != authzguardPkgPath {
				continue
			}
			local = filepath.Base(path)
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
		if local == "" {
			continue
		}

		where := relTo(root, abs)

		// Шаг 1. Переменные, в которые попал ЛЮБОЙ конструктор пакета рубежей —
		// не только объявленный в перечне.
		//
		// «Любой», а не «объявленный», ради второй половины утверждения: рубеж,
		// заведённый СВЕРХ основания, обязан краснеть так же, как пропавший.
		// Собирая только объявленные, проба знала бы ровно то, что уже написано
		// в её собственном перечне, и ветка про лишнее была бы недостижимой —
		// contract, который код никогда не произведёт.
		varCtor := map[string]string{}
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				if i >= len(assign.Lhs) {
					break
				}
				lhs, ok := assign.Lhs[i].(*ast.Ident)
				if !ok {
					continue
				}
				if name := authzguardCtorIn(rhs, local); name != "" {
					varCtor[lhs.Name] = name
				}
			}
			return true
		})

		// Шаг 2. Элементы собираемых срезов интерсепторов — и ТОЛЬКО они.
		// Значение, не доехавшее до среза, цепочкой не является.
		mark := func(name string, pos token.Pos) {
			if _, seen := wired[name]; seen {
				return
			}
			wired[name] = fmt.Sprintf("%s:%d", where, fset.Position(pos).Line)
		}
		visitElem := func(e ast.Expr) {
			call, ok := e.(*ast.CallExpr)
			if !ok {
				return
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok {
				return
			}
			switch {
			// Прямая функция-интерсептор пакета: `authzguard.AntiAnonymousUnary(…)`.
			case recv.Name == local:
				mark(sel.Sel.Name, sel.Pos())
			// Значение рубежа, снятое с конструктора: `internalACRFloor.Unary()`.
			case sel.Sel.Name == "Unary" || sel.Sel.Name == "Stream":
				if name, ok := varCtor[recv.Name]; ok {
					mark(name, sel.Pos())
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				// `[]grpc.UnaryServerInterceptor{ … }`
				for _, e := range node.Elts {
					visitElem(e)
				}
			case *ast.CallExpr:
				// `append(chain, …)` — элементы идут со второго аргумента.
				if id, ok := node.Fun.(*ast.Ident); ok && id.Name == "append" {
					for _, e := range node.Args[1:] {
						visitElem(e)
					}
				}
			}
			return true
		})
	}
	return wired, files
}

// authzguardCtorIn ищет в выражении вызов конструктора пакета рубежей и
// возвращает его имя.
//
// Ищется по всему поддереву: `authzguard.NewCallerPolicy(…).WithFoo(…)` — это
// вызов доводчика НАД вызовом конструктора, и вершина выражения принадлежит
// доводчику, а не тому, что проба обязана распознать.
func authzguardCtorIn(e ast.Expr, local string) string {
	found := ""
	ast.Inspect(e, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != local {
			return true
		}
		if strings.HasPrefix(sel.Sel.Name, "New") {
			found = sel.Sel.Name
			return false
		}
		return true
	})
	return found
}
