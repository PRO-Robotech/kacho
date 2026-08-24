// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_lane_census_test.go — ПЕРЕПИСЬ ПОЛОС ЛИЧНОСТИ, ВЫВЕДЕННАЯ ИЗ ДЕРЕВА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ВЫВОДИТЬ, А НЕ ВЫПИСЫВАТЬ
//
// До #1215 сравнение полос (stepup_lane_parity_test.go) знало полосы ПЕРЕЧНЕМ:
// две строки в литерале среза. Полоса базового удостоверения (#1142) завелась
// третьей — и перепись осталась зелёной, печатая «полос 2 · спрашивают пол 2»
// при трёх полосах в дереве. Это «ноль находок», неотличимый от «ноль
// прочитанного»: проба не видела целого вида предмета и не могла об этом
// сообщить, потому что перечень принадлежал ей, а не дереву.
//
// Выведенная перепись переворачивает умолчание: четвёртая полоса роняет пробу с
// её именем, пока привод к ней не написан ОСОЗНАННО. Цена названа честно —
// заведение полосы перестаёт быть бесплатным; это и есть предмет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДИКАТ ВЫВОДА — СЕМАНТИЧЕСКИЙ, А НЕ ПО ИМЕНИ
//
// Полоса личности на REST-поверхности края — это метод `*AuthInterceptor`,
// который
//
//	(1) достижим по вызовам получателя от точки входа `AuthInterceptor.HTTP`
//	    — полоса, к которой не диспетчеризуют, полосой не является; и
//	(2) УСТАНАВЛИВАЕТ ЛИЧНОСТЬ — пишет заголовок принципала, сам либо через
//	    `setPrincipalHeaders`.
//
// Ни один из двух признаков не смотрит на ИМЯ метода. Предикат по имени
// (`try*`) мерил бы соглашение об именовании: полоса, названная иначе,
// прошла бы мимо — то есть ровно тот отказ, который перепись и заводится
// закрыть.
//
// ГРАНИЦА НАЗВАНА: перепись судит REST-вход (`AuthInterceptor.HTTP`). Нативная
// gRPC-поверхность (`AuthInterceptor.authorize`) устанавливает личность другим
// механизмом — метаданными, а не заголовками, — и её полосы этой переписью НЕ
// перечисляются. Про неё утверждают отдельные пробы полос.
//
// Разбор идёт по УЗЛАМ дерева разбора, не по тексту: имена полос встречаются в
// комментариях этого же файла, и проверка подстрокой краснела бы на собственном
// объяснении (`testing.md` §«Гейт на класс», п. 4).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// laneCensus — что перепись увидела. Печатается ЦЕЛИКОМ: «ноль находок» обязано
// быть отличимо от «ноль прочитанного», а для этого объём осмотренного —
// отдельное утверждение, а не подразумеваемое.
type laneCensus struct {
	filesRead   int
	methods     int
	reachable   int
	lanes       []string // имена методов-полос, отсортированы
	entryPoint  string
	receiverTyp string
}

const (
	laneReceiverType = "AuthInterceptor"
	laneEntryPoint   = "HTTP"
)

// identityLanesFromTree выводит полосы личности из исходников пакета.
//
// Проба своей ПРЕДПОСЫЛКИ здесь же: пустой обход, отсутствие точки входа и ноль
// полос — это отказ, а не тихий успех. Перепись, которой нечего было читать,
// зеленела бы на снесённом пакете.
func identityLanesFromTree(t *testing.T) laneCensus {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	require.NoError(t, err, "перепись не смогла разобрать пакет — вердикта о полосах нет")

	c := laneCensus{entryPoint: laneEntryPoint, receiverTyp: laneReceiverType}
	methods := map[string]*ast.FuncDecl{}
	for _, p := range pkgs {
		for name, f := range p.Files {
			_ = name
			c.filesRead++
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
					continue
				}
				if laneRecvTypeName(fd.Recv.List[0].Type) == laneReceiverType {
					methods[fd.Name.Name] = fd
				}
			}
		}
	}
	c.methods = len(methods)

	require.Positive(t, c.filesRead,
		"перепись не прочитала НИ ОДНОГО файла — это «ноль прочитанного», а не «ноль полос»")
	require.Positive(t, c.methods,
		"у %s не найдено ни одного метода — предпосылка переписи не выполняется", laneReceiverType)
	require.Contains(t, methods, laneEntryPoint,
		"точки входа %s.%s в дереве нет: перепись судила бы о механизме, которого не существует",
		laneReceiverType, laneEntryPoint)

	// Транзитивное замыкание по вызовам получателя от точки входа: полоса,
	// диспетчеризуемая через промежуточный метод, тоже полоса.
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		fd, ok := methods[name]
		if !ok {
			return
		}
		for _, callee := range laneReceiverCalls(fd) {
			walk(callee)
		}
	}
	walk(laneEntryPoint)

	for name := range seen {
		fd, ok := methods[name]
		if !ok || name == laneEntryPoint {
			continue
		}
		c.reachable++
		if laneEstablishesPrincipal(fd) {
			c.lanes = append(c.lanes, name)
		}
	}
	sort.Strings(c.lanes)

	require.NotEmpty(t, c.lanes,
		"от %s.%s не выведено НИ ОДНОЙ полосы личности (прочитано файлов %d, методов %d, "+
			"достижимо %d) — предикат вывода перестал совпадать с деревом, и всякое "+
			"сравнение полос ниже было бы вакуумным",
		laneReceiverType, laneEntryPoint, c.filesRead, c.methods, c.reachable)
	return c
}

func laneRecvTypeName(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// laneReceiverCalls — вызовы вида `<получатель>.Метод(...)` внутри тела.
func laneReceiverCalls(fd *ast.FuncDecl) []string {
	if len(fd.Recv.List[0].Names) != 1 {
		return nil // получатель без имени вызвать себя не может
	}
	recv := fd.Recv.List[0].Names[0].Name
	var out []string
	ast.Inspect(fd, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// laneEstablishesPrincipal — пишет ли метод личность на запрос.
//
// Производителей ТРИ, и все обязаны учитываться: общий помощник и прямая запись
// заголовка идентификатора в ОБЕИХ поверхностных формах — голой и мостовой
// (`Grpc-Metadata-`). Учесть не все означало бы не увидеть полосу, написанную
// оставшимся способом; в этом дереве написаны первыми двумя, а третья форма
// закрыта вперёд — потому что незакрытая даёт ровно тот тихий пропуск полосы,
// ради которого перепись и заведена.
func laneEstablishesPrincipal(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "setPrincipalHeaders" {
				found = true
			}
		case *ast.SelectorExpr:
			if !laneIsPrincipalIDHeader(v.Sel.Name) {
				return true
			}
			if x, ok := v.X.(*ast.Ident); ok && x.Name == "principalmeta" {
				found = true
			}
		}
		return true
	})
	return found
}

// laneIsPrincipalIDHeader — имена констант, несущих идентификатор принципала.
// Обе поверхностные формы называют ОДИН предмет, и различать их здесь незачем:
// полоса, написавшая любую, личность установила.
func laneIsPrincipalIDHeader(name string) bool {
	return name == "HeaderPrincipalID" || name == "HeaderGRPCMetaPrincipalID"
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ САМОГО ПРЕДИКАТА. Без него «полос выведено N» ничего
// не значит: предикат, находящий что попало, и предикат, находящий верное, дают
// одинаково непустой ответ.
//
// Утверждается ОБЕ стороны: метод, который личность устанавливает, полосой
// признаётся; метод, достижимый от той же точки входа и личность НЕ
// устанавливающий, полосой НЕ признаётся. Односторонняя проба зеленела бы на
// предикате, объявляющем полосой каждый метод.
func TestIdentityLaneCensus_PredicateSeparatesLanesFromTheirNeighbours(t *testing.T) {
	c := identityLanesFromTree(t)
	t.Logf("перепись вывода: файлов прочитано %d · методов %s %d · достижимо от %s %d · полос %d",
		c.filesRead, c.receiverTyp, c.methods, c.entryPoint, c.reachable, len(c.lanes))
	for _, l := range c.lanes {
		t.Logf("  полоса: %s", l)
	}

	require.Contains(t, c.lanes, "tryKratosSession",
		"положительный контроль: полоса сессии устанавливает личность и обязана выводиться")
	require.Contains(t, c.lanes, "tryBasicCredential",
		"положительный контроль: полоса базового удостоверения (#1142) обязана выводиться — "+
			"именно её невидимость и есть предмет #1215")
	require.NotContains(t, c.lanes, "stripForgeableIdentityHeaders",
		"отрицательный контроль: снятие подделываемых заголовков достижимо от той же точки "+
			"входа и личности НЕ устанавливает — предикат, объявляющий полосой всякий "+
			"достижимый метод, не измеряет ничего")
	require.NotContains(t, c.lanes, "enforceStepUpHTTP",
		"отрицательный контроль: сам пол полосой не является")
}
