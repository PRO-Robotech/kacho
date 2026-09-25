// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// auth_http_lane_roster_test.go — ШАПКА `AuthInterceptor.HTTP` НАЗЫВАЕТ КАЖДУЮ
// ПОЛОСУ, КОТОРУЮ ЭТОТ ЖЕ КОД ПРОВЯЗЫВАЕТ (задача kacho#2663).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Шапка объявляла ДВУХ носителей личности — сессию провайдера и подписанного
// предъявителя, — тогда как тот же перехватчик провязывает и полосу базового
// удостоверения. Читатель, правящий путь аутентификации, о третьей полосе из
// шапки не узнавал.
//
// Это не косметика, а ловушка `security-hardening.md` п. 5: шапка уже ОТВЕТИЛА
// на вопрос «сколько здесь входов личности», поэтому дальше её не проверяют — и
// полоса, о которой не написано, правится последней либо не правится вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕЧЕНЬ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// Перечень полос берётся у `identityLanesFromTree` — семантической переписи
// соседнего файла (`stepup_lane_census_test.go`): полоса это метод, достижимый
// от `AuthInterceptor.HTTP` по вызовам получателя И устанавливающий личность.
// Ни один признак не смотрит на ИМЯ, поэтому полоса, названная иначе, мимо не
// пройдёт. Свой перечень здесь был бы вторым местом об одном предмете — ровно
// тем, из-за чего шапка и разошлась с кодом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВЕ СТОРОНЫ, И ОБЕ НУЖНЫ
//
//	(1) провязана и НЕ названа — шапка молчит о живом входе личности;
//	(2) названа полосой и НЕ провязана — шапка обещает вход, которого нет.
//
// Одной первой мало: шапка, перечисляющая снятую полосу, читается как
// действующая ровно так же.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАЗБОР СУДИТ УЗЕЛ, А НЕ ТЕКСТ ФАЙЛА
//
// Шапка читается как `Doc` узла объявления. Поиск подстрокой по файлу нашёл бы
// имя полосы в её собственном теле и в соседних комментариях — гейт зеленел бы
// на коде вместо документа, то есть не проверял бы ничего.

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

// httpDocFromTree — шапка `AuthInterceptor.HTTP` как УЗЕЛ дерева разбора, плюс
// объём осмотренного.
func httpDocFromTree(t *testing.T) (doc string, filesRead, methodsSeen int) {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err, "пакет не разобран — вердикта о шапке нет")

	for _, p := range pkgs {
		for range p.Files {
			filesRead++
		}
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
					continue
				}
				if laneRecvTypeName(fd.Recv.List[0].Type) != laneReceiverType {
					continue
				}
				methodsSeen++
				if fd.Name.Name == laneEntryPoint && fd.Doc != nil {
					doc = fd.Doc.Text()
				}
			}
		}
	}
	return doc, filesRead, methodsSeen
}

// laneRosterFindings — предикат ОБЕИХ сторон. Вынесен функцией: инъекция обязана
// звать тот же предикат, иначе она доказывает способность упасть у своей копии.
func laneRosterFindings(doc string, wired []string, methodNames map[string]bool) []string {
	var out []string
	for _, lane := range wired {
		if !strings.Contains(doc, lane) {
			out = append(out, "провязана и НЕ названа шапкой: "+lane)
		}
	}
	// Обратная сторона: шапка называет полосой имя, которого у получателя нет.
	for _, w := range strings.FieldsFunc(doc, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
	}) {
		if !strings.HasPrefix(w, "try") || methodNames[w] {
			continue
		}
		out = append(out, "названа шапкой и НЕ существует: "+w)
	}
	sort.Strings(out)
	return out
}

// TestHTTPHeaderNamesEveryIdentityLaneItWires — сам гейт.
func TestHTTPHeaderNamesEveryIdentityLaneItWires(t *testing.T) {
	c := identityLanesFromTree(t)
	doc, filesRead, methodsSeen := httpDocFromTree(t)

	t.Logf("перепись: файлов прочитано %d · методов %s %d · полос личности выведено %d · "+
		"длина шапки %s.%s %d знаков",
		filesRead, laneReceiverType, methodsSeen, len(c.lanes),
		laneReceiverType, laneEntryPoint, len(doc))
	for _, l := range c.lanes {
		t.Logf("  полоса: %s — названа шапкой: %t", l, strings.Contains(doc, l))
	}

	require.Positive(t, filesRead,
		"прочитано ноль файлов — это «ноль прочитанного», а не «ноль находок»")
	require.NotEmpty(t, doc,
		"у %s.%s нет шапки вовсе: гейт стерёг бы пустую строку и молчал при любой полосе",
		laneReceiverType, laneEntryPoint)
	require.NotEmpty(t, c.lanes,
		"полос личности выведено ноль — предпосылка гейта не выполняется, и его молчание "+
			"сказано ни о чём")

	methodNames := map[string]bool{}
	for _, l := range c.lanes {
		methodNames[l] = true
	}
	// Имена ВСЕХ методов получателя, а не только полос: шапка вправе назвать
	// метод, полосой не являющийся, и обвинять её в этом нечем.
	for _, n := range allReceiverMethodNames(t) {
		methodNames[n] = true
	}

	findings := laneRosterFindings(doc, c.lanes, methodNames)
	require.Empty(t, findings,
		"шапка %s.%s разошлась с кодом — %d находк(и): %v\n\n"+
			"Шапка уже ОТВЕЧАЕТ читателю, сколько здесь входов личности, поэтому дальше её "+
			"не проверяют: полоса, о которой не написано, правится последней либо не "+
			"правится вовсе (security-hardening.md п. 5).\n"+
			"Снятие: назвать полосу в шапке либо снять из шапки имя, которого код не несёт.",
		laneReceiverType, laneEntryPoint, len(findings), findings)
}

// allReceiverMethodNames — все имена методов получателя из дерева.
func allReceiverMethodNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	require.NoError(t, err)
	var out []string
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
					continue
				}
				if laneRecvTypeName(fd.Recv.List[0].Type) == laneReceiverType {
					out = append(out, fd.Name.Name)
				}
			}
		}
	}
	return out
}

// TestHTTPHeaderRosterPredicateCanFail — инъекция по КАЖДОЙ стороне, с законным
// близнецом. Предикат, не краснеющий на дефекте, не удерживает ничего.
func TestHTTPHeaderRosterPredicateCanFail(t *testing.T) {
	// Синтетика, а не живой перечень полос: опирайся инъекция на дерево,
	// доказательство менялось бы вместе с ним — и исчезло бы ровно тогда, когда
	// полосу снимают. Здесь снятие чужого поставщика этот файл уже задело:
	// прежняя синтетика называла полосу его сессии (#2792).
	wired := []string{"tryOwnSession", "tryBasicCredential", "tryHydraJWT"}
	known := map[string]bool{
		"tryOwnSession": true, "tryBasicCredential": true, "tryHydraJWT": true,
		"tryDevSecretJWT": true,
	}

	t.Run("законная шапка — молчание", func(t *testing.T) {
		doc := "Носителей ТРИ: tryOwnSession, tryBasicCredential, tryHydraJWT. " +
			"tryDevSecretJWT носителем сверх них не является."
		require.Empty(t, laneRosterFindings(doc, wired, known),
			"законная шапка объявлена находкой — предикат ловит форму, а не существо")
	})

	t.Run("провязана и не названа — тот самый дефект kacho#2663", func(t *testing.T) {
		doc := "Носителей личности на этом пути ДВА: наша сессия " +
			"(tryOwnSession) и подписанный предъявитель (tryHydraJWT)."
		f := laneRosterFindings(doc, wired, known)
		require.Len(t, f, 1, "умолчание о провязанной полосе находкой не стало: %v", f)
		require.Contains(t, f[0], "tryBasicCredential",
			"находка не называет полосу: %q", f[0])
	})

	t.Run("названа и не существует — обратная сторона", func(t *testing.T) {
		doc := "Носителей ТРИ: tryOwnSession, tryBasicCredential, tryHydraJWT, " +
			"а также снятая tryCookieRewrite."
		f := laneRosterFindings(doc, wired, known)
		require.Len(t, f, 1, "имя снятой полосы находкой не стало: %v", f)
		require.Contains(t, f[0], "tryCookieRewrite",
			"находка не называет имя: %q", f[0])
	})

	t.Run("метод получателя, полосой не являющийся, находкой не становится", func(t *testing.T) {
		doc := "Полосы: tryOwnSession, tryBasicCredential, tryHydraJWT. " +
			"tryDevSecretJWT читает тот же заголовок и носителем сверх них не является."
		require.Empty(t, laneRosterFindings(doc, wired, known),
			"упоминание существующего метода, не входящего в перечень полос, объявлено "+
				"находкой — шапка вправе назвать соседа")
	})
}
