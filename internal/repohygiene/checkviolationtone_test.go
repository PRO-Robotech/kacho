// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// checkviolationtone_test.go — гейт двух свойств отображения нарушения CHECK
// (задача #718).
//
// # Предмет
//
// Ограничение таблицы бывает двух видов, и они противоположны по тому, кого
// обвиняет отказ: форму значения либо проверяет САМ СЕРВИС до вставки (тогда
// срабатывание ограничения означает НАШ дефект), либо только база (тогда отказ
// по вводу уместен). Код ответа был один на оба смысла, а текст пересказывал
// СУБД: арендатор, создавший адрес, читал «Address violates check constraint» —
// формулировку Postgres, не называющую ни поля, ни того, что исправить.
//
// # Что держит этот файл
//
//  1. язык СУБД не производится прод-кодом как текст ДЛЯ ВЫЗЫВАЮЩЕГО;
//  2. предпосылка разбора — согласие константы `nameform.ConstraintSuffix` с
//     тем, как имя ограничения строит миграция формы имени в каждой схеме, её
//     прошедшей.
//
// Второе — проверка СВОЕЙ предпосылки, а не украшение: разбор полос опирается на
// конструкцию имени. Поменяй миграция суффикс — и отображение перестало бы
// узнавать ограничение молча, а «сервис пропустил негодное имя» снова читалось
// бы как «виноват ваш ввод», причём ни один тест сервиса этого бы не заметил.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// dbTonePhrase — фраза, которой о своём отказе говорит Postgres. Ищется именно
// ФРАЗА, а не отдельные слова: «constraint» законно стоит в именах функций и в
// прозе комментариев, и гейт по одному слову сняли бы первым же ложным
// срабатыванием.
const dbTonePhrase = "violates check constraint"

// nameFormConstraintSuffix — то же значение, что `nameform.ConstraintSuffix`.
// Здесь оно ВЫПИСАНО намеренно: гейт сверяет константу пакета с деревом
// миграций, и взяв её из того же пакета, он сверял бы значение сам с собой.
const nameFormConstraintSuffix = "_name_check"

// TestCheckViolationNeverSpeaksTheDBTone — ни один прод-файл не собирает
// сообщение для вызывающего из фразы Postgres.
//
// Гейт читает ТОЛЬКО строковые литералы и только в не-тестовом Go: фраза стоит
// и в комментариях, объясняющих эту же защиту (в том числе в шапке этого файла и
// в godoc самих отображений), и текстовый поиск принял бы объяснение за
// производство — ровно тот класс, который гейт и ловит.
func TestCheckViolationNeverSpeaksTheDBTone(t *testing.T) {
	root := repoRoot(t)
	files := trackedGoFiles(t, root)
	if len(files) == 0 {
		t.Fatal("осмотрено 0 файлов — перепись беспредметна, «ноль находок» неотличим от «ноль прочитанного»")
	}

	var findings []string
	for _, abs := range files {
		src, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("read %s: %v", abs, err)
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			rel = abs
		}
		for _, lit := range stringLiterals(t, rel, src) {
			if strings.Contains(strings.ToLower(lit.value), dbTonePhrase) {
				findings = append(findings, fmt.Sprintf("%s:%d: строковый литерал %q", rel, lit.line, lit.value))
			}
		}
	}

	t.Logf("осмотрено не-тестовых файлов Go: %d", len(files))
	if len(findings) > 0 {
		t.Fatalf("текст для вызывающего пересказывает СУБД (%d):\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}

// TestNameFormConstraintSuffixMatchesMigrations — предпосылка разбора полос.
//
// Каждая схема, принявшая канон формы имени (#715), обязана строить имя
// ограничения тем же суффиксом, который читает отображение ошибки. Гейт падает и
// когда суффикс разошёлся, и когда схем не нашлось вовсе: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
//
// # Признак отбора — СОДЕРЖИМОЕ схемы, а не имя файла
//
// Прежде схемы отбирались по хвосту имени файла `*_resource_name_single_form.sql`.
// Имя файла — прокси: настоящий признак это ограничение формы имени В СХЕМЕ, а
// файл лишь то место, откуда оно однажды туда попало. Прокси пережил свой предмет,
// когда цепь iam была сведена в одну первичную миграцию — ограничение на месте,
// файла нет, — и гейт стал считать пять схем вместо шести. Вывод из содержимого
// живёт в `nameformcanon.go` и общий с гейтом имён фикстур: два места об одном
// предмете разошлись бы молча, и разошлись бы они именно так, как разошлись.
func TestNameFormConstraintSuffixMatchesMigrations(t *testing.T) {
	root := repoRoot(t)

	adoptions, err := nameFormCanonAdoptions(root)
	if err != nil {
		t.Fatalf("состав дерева не читается: %v", err)
	}

	var (
		seen         []string
		findings     []string
		materialised int
	)
	for _, a := range adoptions {
		src, rerr := os.ReadFile(filepath.Join(root, a.File)) // #nosec G304 -- путь из индекса репозитория
		if rerr != nil {
			t.Fatalf("read %s: %v", a.File, rerr)
		}
		findings = append(findings, adjudicateNameFormConstraintNaming(a, string(src), nameFormConstraintSuffix)...)
		materialised += len(nameFormMaterialisedConstraint.FindAllString(string(src), -1))
		seen = append(seen, a.Service+" ("+a.File+")")
	}

	// Перепись печатает ДВЕ величины отдельно: сколько схем осмотрено и сколько
	// среди них материализованных ограничений. Одно число не отличило бы схему,
	// у которой имена строятся в рантайме, от схемы, где их можно прочесть.
	t.Logf("осмотрено схем, принявших канон формы имени: %d — %s; материализованных ограничений с формой: %d",
		len(seen), strings.Join(seen, ", "), materialised)

	for _, f := range findings {
		t.Error(f)
	}

	// iam пришёл к канону последним (#1279): его форма имени была объявлена
	// СВОИМ текстом, поэтому гейт единственности формы её не видел, а разбор
	// полос ничего не знал о шестой схеме. Число обязано двигаться вместе с
	// деревом — оно и есть перепись, а не украшение.
	const want = 6 // vpc, compute, storage, nlb, geo, iam
	if len(seen) != want {
		t.Fatalf("схем под каноном формы имени найдено %d, ожидалось %d: перепись беспредметна "+
			"либо схема прибавилась/убыла, и разбор полос про неё ничего не знает", len(seen), want)
	}
}

// literal — строковый литерал прод-кода и его строка.
type literal struct {
	value string
	line  int
}

// stringLiterals возвращает РАЗОБРАННЫЕ строковые литералы файла.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: гейт обязан отличать
// код от комментария, иначе он покраснеет на объяснении самой защиты и будет
// снят первым же ложным срабатыванием (`testing.md` §«Гейт на класс», п. 4).
func stringLiterals(t *testing.T, name string, src []byte) []literal {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		// Файл, который не разбирается, пропускается молча только в одном
		// случае — его не существует. Здесь он есть, значит это находка.
		t.Fatalf("parse %s: %v", name, err)
	}
	var out []literal
	ast.Inspect(f, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil {
			return true
		}
		out = append(out, literal{value: v, line: fset.Position(bl.Pos()).Line})
		return true
	})
	return out
}
