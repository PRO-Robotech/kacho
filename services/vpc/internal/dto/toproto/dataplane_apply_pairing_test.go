// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// dataplane_apply_pairing_test.go — ПАРА «публичное поле ↔ его заполнитель»,
// проверяемая в обе стороны.
//
// # Предмет
//
// Шов датаплейна принимает от исполнителя подтверждение применения; читатель,
// сводящий эту таблицу к тому, что видит арендатор, живёт в
// `services/vpc/internal/repo/dataplane/public_state.go`
// (`Store.PublicApplyStates`). Публичного ПОЛЯ, в которое состояние приезжает, в
// контракте сегодня нет — вторая половина изменения принадлежит другому автору
// (`proto/kacho/cloud/vpc/v1/*.proto`).
//
// Обе половины по отдельности — дефекты, и оба класса в этом продукте уже
// случались:
//
//   - поле есть, заполнителя нет → мёртвое поле: арендатор всегда видит «не
//     применено», потому что никто ничего не кладёт;
//   - заполнитель есть, поля нет → чтение никуда не едет: код исполняется, база
//     читается, и ни один вызывающий этого не видит.
//
// # Почему гейт, а не строка в отчёте
//
// «Не забыть провязать» — обещание, за которым никто не отвечает. Гейт ИСТЕКАЕТ
// САМ: в день, когда появится любая одна половина, он краснеет и называет
// адрес второй. Пока нет ни одной — остаётся зелёным и ПЕЧАТАЕТ состояние,
// чтобы «зелёный гейт» не читался как «арендатор видит отказ».
//
// # Почему разбор синтаксиса, а не поиск подстроки
//
// Поиск по тексту находит имя в КОММЕНТАРИИ — в том числе в комментарии,
// объясняющем сам гейт, — и объявляет заполнителя провязанным, пока он не
// провязан. Здесь ищется вызов как узел дерева разбора; упоминания в прозе и
// строковых литералах не считаются.
//
// # Что здесь НЕ утверждается
//
// Ничего о том, как поле должно называться и какого оно типа: предмет —
// существование пары, а не выбор имени. Предикат поля намеренно широк.

// publicResourceMessages — публичные сообщения ресурсов vpc.
//
// Перечень выписан, а не выведен: вывести его из дескрипторов пакета можно было
// бы только по признаку «сообщение верхнего уровня», под который попадают и
// запросы, и ответы, — то есть предикат оказался бы шире предмета. Полнота
// самого обхода проверяется положительным контролем ниже.
var publicResourceMessages = []proto.Message{
	(*vpcv1.Network)(nil),
	(*vpcv1.Subnet)(nil),
	(*vpcv1.Address)(nil),
	(*vpcv1.RouteTable)(nil),
	(*vpcv1.SecurityGroup)(nil),
	(*vpcv1.Gateway)(nil),
	(*vpcv1.NetworkInterface)(nil),
}

// fillerSymbol — имя читателя, сводящего подтверждение к публичному виду.
const fillerSymbol = "PublicApplyStates"

// serviceRoot — корень дерева сервиса относительно этого пакета.
const serviceRoot = "../../.."

// looksLikeApplyState — признак поля, несущего состояние применения намерения.
//
// Широк намеренно: узкий предикат («поле называется ровно так») промолчал бы о
// поле, названном иначе, — то есть ровно в том случае, ради которого гейт
// написан.
func looksLikeApplyState(fd protoreflect.FieldDescriptor) bool {
	if strings.Contains(strings.ToLower(string(fd.Name())), "apply") {
		return true
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return strings.Contains(strings.ToLower(string(fd.Message().Name())), "apply")
	case protoreflect.EnumKind:
		return strings.Contains(strings.ToLower(string(fd.Enum().Name())), "apply")
	default:
		return false
	}
}

// TestDataplaneApplyFieldAndItsFillerAppearTogether — пара в обе стороны.
func TestDataplaneApplyFieldAndItsFillerAppearTogether(t *testing.T) {
	fieldSites, scannedFields := scanPublicMessages(t)
	callSites, scannedFiles, scannedCalls := scanFillerCallers(t)

	t.Logf("осмотрено %d поле(й) в %d публичном(ых) сообщении(ях); "+
		"разобрано %d файл(ов) прод-кода под %s, в них %d вызов(ов)",
		scannedFields, len(publicResourceMessages), scannedFiles, serviceRoot, scannedCalls)

	switch {
	case len(fieldSites) > 0 && len(callSites) == 0:
		t.Fatalf("публичное поле состояния применения появилось, а заполнителя нет — мёртвое поле.\n"+
			"  поле(я): %s\n"+
			"  позовите %s (services/vpc/internal/repo/dataplane/public_state.go) на пути чтения\n"+
			"  и переложите PublicApplyState{Applied, Reason} в это поле переводом БЕЗ корзины «прочее»",
			strings.Join(fieldSites, ", "), fillerSymbol)

	case len(fieldSites) == 0 && len(callSites) > 0:
		t.Fatalf("заполнитель %s провязан, а публичного поля нет — чтение никуда не едет.\n"+
			"  вызывающие: %s\n"+
			"  либо добавьте поле в публичные сообщения vpc, либо снимите вызовы",
			fillerSymbol, strings.Join(callSites, ", "))

	case len(fieldSites) == 0 && len(callSites) == 0:
		t.Logf("ОТКРЫТЫЙ ДОЛГ: публичного поля состояния применения в контракте vpc нет, "+
			"поэтому %s ни у кого не вызывается. Отказ исполнителя виден в базе и НЕ виден "+
			"арендатору. Гейт покраснеет сам, как только появится любая одна половина.", fillerSymbol)

	default:
		t.Logf("пара сошлась: поле(я) %s, вызывающие %s",
			strings.Join(fieldSites, ", "), strings.Join(callSites, ", "))
	}
}

// scanPublicMessages ищет поля состояния применения и возвращает объём
// осмотренного.
func scanPublicMessages(t *testing.T) (sites []string, scannedFields int) {
	t.Helper()

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ обхода: тот же обход обязан находить поле, которое
	// в каждом из этих сообщений заведомо есть. Без него «поля не найдено» было
	// бы неотличимо от «дескрипторы не прочитаны».
	seenID := 0

	for _, m := range publicResourceMessages {
		md := m.ProtoReflect().Descriptor()
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			scannedFields++
			if fd.Name() == "id" {
				seenID++
			}
			if looksLikeApplyState(fd) {
				sites = append(sites, string(md.FullName())+"."+string(fd.Name()))
			}
		}
	}
	if seenID != len(publicResourceMessages) {
		t.Fatalf("обход дескрипторов нашёл поле id в %d сообщении(ях) из %d — "+
			"проверка не читает то, о чём отчитывается", seenID, len(publicResourceMessages))
	}
	sort.Strings(sites)
	return sites, scannedFields
}

// scanFillerCallers ищет ВЫЗОВЫ заполнителя в не-тестовом дереве сервиса.
//
// Исключений по файлам нет и не нужно: объявление метода, объявление метода
// интерфейса и утверждение о выполнении порта вызовами не являются, поэтому
// заполнитель не считает провязанным сам себя by construction. Список
// прощённых не заводится — каждая запись в нём была бы местом, куда пропажа
// вызова въезжает незамеченной.
func scanFillerCallers(t *testing.T) (sites []string, scannedFiles, scannedCalls int) {
	t.Helper()

	fset := token.NewFileSet()
	err := filepath.WalkDir(serviceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "testdata", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			// Нечитаемый файл — слепое пятно, а не пропуск: промолчав о нём, гейт
			// отчитался бы об осмотре дерева, части которого он не видел.
			t.Fatalf("файл %s не разбирается, гейт слеп на нём: %v", path, perr)
		}
		scannedFiles++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			scannedCalls++
			var name string
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			if name == fillerSymbol {
				sites = append(sites, fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева сервиса: %v", err)
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ обхода: дерево прочитано и вызовы в нём видны.
	// «Ноль вызывающих» обязано быть отличимо от «ноль разобранного» и от
	// «разобрано, но узлы вызова не смотрелись».
	if scannedFiles == 0 {
		t.Fatal("обход не разобрал ни одного файла прод-кода — проверка не доказывает ничего")
	}
	if scannedCalls == 0 {
		t.Fatal("в разобранном дереве не нашлось ни одного вызова — обход смотрит не туда")
	}
	sort.Strings(sites)
	return sites, scannedFiles, scannedCalls
}
