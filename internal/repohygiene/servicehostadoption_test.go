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

// hostAdoptionKind — ПОЧЕМУ запись существует. Различие не оформительское: от
// него зависит, что делать, когда запись потеряет предмет.
//
// Пока вид не был объявлен, обе причины писались одним полем `why`, и решение
// владельца читалось как незаконченная работа — то есть как приглашение её
// «доделать». Ровно это и надо было развести.
type hostAdoptionKind uint8

const (
	// adoptionPending — сервис ЖДЁТ перевода. Запись описывает работу, которую
	// намерены закончить; потеряв предмет, она подлежит уборке.
	adoptionPending hostAdoptionKind = iota
	// adoptionDecided — владелец решил, что сервис остаётся ВНЕ контура. Запись
	// описывает РЕШЕНИЕ; потеряв предмет, она означает, что решение обойдено, —
	// это находка для ВЛАДЕЛЬЦА, а не уборка записи.
	adoptionDecided
)

// hostAdoptionException — сервис, чей композиционный корень собирает сервер сам.
//
// # Почему перечень, а не молчание
//
// Перевод семерых — не один коммит: между первым переведённым и последним у
// части сервисов своя сборка ещё жива. Молчаливое «гейт их не видит» запрещено:
// тогда самая нагруженная поверхность остаётся вне контура, будучи объявленной
// внутри него, и отличить «ещё не перевели» от «гейт сломался» нечем.
//
// # Самоистечение — и почему у решения оно НЕСИММЕТРИЧНО
//
// Запись, у которой БОЛЬШЕ НЕТ находок, — сама находка. Предикат внешний —
// состояние дерева, — а не выведенный из этого же перечня.
//
// Но у записи вида [adoptionDecided] предметов ДВА, и «нет находок» ловит только
// первый:
//
//  1. ПРЕДМЕТ записи — своя сборка сервера у сервиса. Исчез ⇒ сервис переведён.
//     Это ловит обход ниже; для решения смысл срабатывания иной — не «работа
//     сделана», а «сделано вопреки решению».
//  2. ОСНОВАНИЕ решения — факт о носителе и о владельце модели, на котором
//     решение стоит. Оно может стать ложным, ПОКА предмет жив: iam продолжает
//     собирать сервер сам, находки на месте, запись выглядит действующей, — а
//     основание уже неверно. Здесь «нет находок» молчит by construction.
//
// Поэтому основание пинится ОТДЕЛЬНОЙ пробой над композиционным корнем владельца
// модели (`iamcontourdecisionbasis_test.go`). Без неё самоистечение было бы
// объявлено и наполовину не исполнено — тот самый класс, который этот гейт ловит
// в чужом коде.
type hostAdoptionException struct {
	// kind — работа это или решение. См. [hostAdoptionKind].
	kind hostAdoptionKind
	// why — почему запись существует и что должно случиться, чтобы она ушла.
	why string
	// basis — ТОЛЬКО у решения: факт, на котором оно стоит. Отдельно от `why`,
	// потому что истекает отдельно (см. предмет 2 выше) и проверяется отдельной
	// пробой. У ожидающих перевода пусто — им нечего обосновывать, они просто
	// ещё не дошли.
	basis string
}

// hostAdoptionExceptions — сервисы, чей корень собирает сервер сам. Ключ — имя
// каталога под `services/`.
var hostAdoptionExceptions = map[string]hostAdoptionException{
	// iam — НЕ ожидающий перевода. Решение владельца 2026-08-10.
	"iam": {
		kind: adoptionDecided,
		why: "владелец решил (2026-08-10): iam остаётся ВНЕ контура носителя, и это решение, " +
			"а не долг. Перевести «как есть» нельзя: пришлось бы СНЯТЬ пять рубежей и завести " +
			"шестой. Прежняя редакция этой записи держала iam среди ожидающих перевода — то есть " +
			"предъявляла решение как незаконченную работу, и следующий заход взялся бы её " +
			"«доделывать»",
		basis: "носитель ставит ОДНО звено решения о доступе (`decisionLink` в pkg/servicehost " +
			"собирает один `authz.Interceptor` по выведенной карте прав), а владелец модели " +
			"держит ШЕСТЬ. Пять картой не выражаются вовсе: анти-аноним · пер-RPC политика " +
			"вызывающего на ОБОИХ слушателях · этаж system_viewer на читающих RPC · этаж step-up " +
			"· приложение машиночитаемой причины отказа. ШЕСТОЙ — собственная дверь, и он " +
			"выражается картой ровно так же, как у носителя: это ТОТ ЖЕ `authz.Interceptor` по " +
			"той же выведенной карте, поднятый вручную полосой AuthzSelf, у которой до сих пор " +
			"не было ни одного производителя. Прибавление рубежа основание НЕ отменяет, а " +
			"усиливает: перевести «как есть» по-прежнему нельзя, потому что пять остальных " +
			"дескриптором не выражаются — полей интерсепторного типа в servicecontract.Spec нет " +
			"НАМЕРЕННО («цепочку невозможно принести, поэтому её невозможно собрать " +
			"по-своему»). Но предикат истечения решения СУЗИЛСЯ: снять остаётся пять, а не " +
			"шесть. Основание держится пробой " +
			"TestModelOwnerStillCarriesTheRubiconsTheDecisionRestsOn",
	},
	// api-gateway — НЕ ожидающий перевода. Край не является сервисом по ФОРМЕ, а
	// не по объёму работы.
	edgeService: {
		kind: adoptionDecided,
		why: "край — ПРОКСИ, а не сервис со своим набором служб: его сервер поднимается с " +
			"обработчиком НЕИЗВЕСТНОЙ службы и маршрутизирует вызов бэкенду по имени метода, " +
			"а второй слушатель мультиплексируется с REST на одном порту. Носитель контура " +
			"поднимает два слушателя, регистрируя в них ОБЪЯВЛЕННЫЙ набор служб (`Registrar`), " +
			"и о маршрутизации чужих методов не знает — перевести край «как есть» значит либо " +
			"внести в дескриптор обработчик неизвестной службы (то есть поле, которым сервис " +
			"сможет принести свою маршрутизацию), либо оставить край без прокси-семантики " +
			"вовсе. До Ф8 это место молчало: гейт кончался на services/*/cmd/**, и самая " +
			"нагруженная поверхность была вне суждения, будучи объявленной внутри контура",
		basis: "у носителя НЕТ ни поля обработчика неизвестной службы, ни поля мультиплексора: " +
			"`servicecontract.Spec` их не несёт, а `servicehost.Serve` регистрирует службы " +
			"только через переданные `Registrar`. Пока это так, форма края носителю невыразима. " +
			"Основание истекает от ВНЕШНЕГО факта — появления такого поля у дескриптора, — и " +
			"держится пробой TestCarrierStillCannotExpressAProxyEdge",
	},
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
	//
	// Срабатывание одно, а требуемое действие — разное, и от вида записи оно
	// зависит целиком: у ожидающего перевода это уборка, у решения — сигнал, что
	// решение владельца обойдено. Печатать им один текст значило бы предлагать
	// снять запись о решении как выполненную работу.
	var expired []string
	for svc, ex := range hostAdoptionExceptions {
		if len(byService[svc]) > 0 {
			continue
		}
		switch ex.kind {
		case adoptionDecided:
			expired = append(expired, fmt.Sprintf("%s: РЕШЕНИЕ ВЛАДЕЛЬЦА ОБОЙДЕНО — сервис оставлен "+
				"вне контура намеренно, а своей сборки сервера в его корне больше нет, то есть его "+
				"перевели. Это находка для ВЛАДЕЛЬЦА, а не уборка записи: снимать её вправе тот, "+
				"кто отменит решение. Решение: %s. Основание: %s", svc, ex.why, ex.basis))
		default:
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

	// Обход захватывает КРАЙ наравне с сервисами.
	//
	// Прежде он кончался на `services/*/cmd/**`, и два живых вызова конструктора
	// сервера у края не были ни находками, ни записями с причиной — то есть
	// поверхность, через которую проходит весь внешний трафик, оставалась вне
	// суждения гейта, будучи объявленной внутри контура. §9 п.2 приёмки XC-7
	// требует явного состояния по КАЖДОМУ месту с момента, как гейт вооружён.
	var tracked []string
	for _, sub := range []string{"services", "gateway"} {
		dir := filepath.Join(root, sub)
		under, err := treecorpus.Under(dir)
		if err != nil {
			t.Fatalf("состав дерева под %s не читается: %v", dir, err)
		}
		tracked = append(tracked, under...)
	}

	roots := map[string]struct{}{}
	seenFinding := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, abs := range tracked {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("относительный путь для %s: %v", abs, err)
		}
		slashed := filepath.ToSlash(rel)
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		svc, rootKey, ok := adoptionRootOf(slashed)
		if !ok {
			continue
		}
		roots[rootKey] = struct{}{}
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
	// Перепись разделяет виды записей: «исключений 5» скрывало бы, что одна из
	// них — не остаток работы, а решение, и остаток на самом деле вчетверо.
	pending, decided := 0, 0
	for _, ex := range hostAdoptionExceptions {
		if ex.kind == adoptionDecided {
			decided++
			continue
		}
		pending++
	}
	res.summary = fmt.Sprintf(
		"перепись: композиционных корней %d, не-тестовых файлов в них %d, находок %d, "+
			"записей %d (ожидают перевода %d, решение владельца оставить вне контура %d)",
		res.roots, res.files, len(res.findings), len(hostAdoptionExceptions), pending, decided)
	return res
}

// adoptionRootOf — принадлежит ли путь композиционному корню, и чьему.
//
// Два вида корня, и они РАЗНЫЕ по форме, поэтому распознаются порознь:
//   - сервис: `services/<svc>/cmd/**` — набор своих RPC, оба слушателя носителя;
//   - край: `gateway/cmd/**` и `gateway/internal/proxy` — прокси с
//     `UnknownServiceHandler` поверх мультиплексора, а не сервис со своим
//     набором служб. Сборка сервера у него живёт не только в `cmd`, поэтому в
//     обход входит и пакет прокси: иначе «край осмотрен» было бы верно ровно для
//     той половины, где сервера нет.
func adoptionRootOf(rel string) (svc, rootKey string, ok bool) {
	parts := strings.Split(rel, "/")
	switch {
	case parts[0] == "services":
		if len(parts) < 4 || parts[2] != "cmd" {
			return "", "", false
		}
		return parts[1], parts[1] + "/" + parts[3], true
	case parts[0] == "gateway":
		if len(parts) >= 3 && parts[1] == "cmd" {
			return edgeService, "gateway/" + parts[2], true
		}
		if len(parts) >= 3 && parts[1] == "internal" && parts[2] == "proxy" {
			return edgeService, "gateway/internal/proxy", true
		}
		return "", "", false
	}
	return "", "", false
}

// edgeService — имя края в реестре записей. Каталогом под `services/` он не
// является, поэтому имя задано здесь, а не выводится из пути.
const edgeService = "api-gateway"

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
