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
//     тем, как имя ограничения строит миграция 715001 в каждой из пяти схем.
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

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
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

// migration715001 — файл, которым форма имени пришла в схемы сервисов.
const migration715001 = "715001_resource_name_single_form.sql"

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
// Каждая из пяти схем, прошедших 715001, обязана строить имя ограничения тем же
// суффиксом, который читает отображение ошибки. Гейт падает и когда суффикс
// разошёлся, и когда файлов миграции не нашлось вовсе: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
func TestNameFormConstraintSuffixMatchesMigrations(t *testing.T) {
	root := repoRoot(t)

	tracked, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева не читается: %v", err)
	}

	var seen []string
	for _, abs := range tracked {
		if filepath.Base(abs) != migration715001 {
			continue
		}
		src, rerr := os.ReadFile(abs)
		if rerr != nil {
			t.Fatalf("read %s: %v", abs, rerr)
		}
		rel, e := filepath.Rel(root, abs)
		if e != nil {
			rel = abs
		}
		if !strings.Contains(string(src), nameFormConstraintSuffix) {
			t.Errorf("%s: миграция не строит имя ограничения суффиксом %q — "+
				"отображение ошибки перестанет узнавать полосу формы имени",
				rel, nameFormConstraintSuffix)
		}
		seen = append(seen, rel)
	}

	t.Logf("осмотрено миграций %s: %d — %s", migration715001, len(seen), strings.Join(seen, ", "))
	const want = 5 // vpc, compute, storage, nlb, geo
	if len(seen) != want {
		t.Fatalf("миграций формы имени найдено %d, ожидалось %d: перепись беспредметна "+
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
