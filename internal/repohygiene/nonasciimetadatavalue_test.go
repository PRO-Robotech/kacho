// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHumanTextGoesInBinaryMetadataKey — значение метаданных, которое может
// содержать текст, введённый ЧЕЛОВЕКОМ, кладётся ключом с суффиксом `-bin`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Значение обычного метаданного ключа gRPC ограничено печатаемой латиницей
// (0x20–0x7E). Продукт русскоязычный, поэтому всякое значение, происходящее от
// ввода человека — отображаемое имя, название, описание, — рано или поздно
// приходит кириллицей. Библиотека отвергает при этом ВЕСЬ вызов кодом
// «внутренняя ошибка», не дойдя до обработчика.
//
// Цена класса измерена: отображаемое имя, положенное обычным ключом, роняло
// КАЖДЫЙ запрос арендатора, чьё имя записано по-русски. Вход при этом проходил,
// печенье ставилось, консоль открывалась — и оставалась пустой. Со стороны
// неотличимо от «продукт не работает», а по логу — от дефекта в совсем другом
// месте: падает не там, где имя записали.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ЗАПРЕЩЕНО
//
// Постановка в метаданные значения под ключом, чьё ИМЯ говорит о человеческом
// тексте (`display`, `name`, `title`, `label`, `description`, `comment`), без
// суффикса `-bin`. Идентификаторы, типы, отметки времени и коды под запрет не
// подпадают: они латиница by construction, и кодировать их значило бы платить
// за то, что оплаты не требует.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА, ОБЪЯВЛЕННАЯ САМОЙ ПРОВЕРКОЙ
//
// Судятся вызовы постановки в метаданные (`Set`, `Append`, `Pairs`) в
// не-тестовом дереве. Перепись печатает, сколько таких вызовов осмотрено и
// сколько из них несут человеческий текст, — поэтому «ноль находок» отличимо
// от «ноль прочитанного».
func TestHumanTextGoesInBinaryMetadataKey(t *testing.T) {
	census, findings, err := auditMetadataHumanText(t, "../..")
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Log(census)
	for _, f := range findings {
		t.Error(f)
	}
}

// humanTextMarkers — признаки того, что значение происходит от ввода человека.
// Список намеренно узкий: широкий поймал бы идентификаторы («name» внутри
// «namespace») и превратился бы в помеху, а помеху снимают.
var humanTextMarkers = []string{"display", "-name", "title", "label", "description", "comment"}

// idLikeMarkers — то, что несёт человеческое слово в имени, но человеческим
// текстом НЕ является: имя ресурса в нашем контракте — DNS label (латиница
// by construction, RFC 1123), имя пространства — тоже.
var idLikeMarkers = []string{"namespace", "hostname", "servername", "-name-bin", "username"}

func auditMetadataHumanText(t *testing.T, root string) (string, []string, error) {
	var scanned, human int
	var findings []string

	// Ключи собираются ПОФАЙЛОВО: законность прежней формы решается наличием
	// её двоичного близнеца в том же объявлении, а не сама по себе.
	var dirsSeen int
	for _, d := range []string{"pkg", "gateway", "services"} {
		abs := filepath.Join(root, d)
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			// Каталога нет — это граница обхода, а не находка. Перепись назовёт
			// число осмотренных каталогов, поэтому «пропустили два из трёх»
			// останется видимым, а не растворится в нуле находок.
			continue
		}
		dirsSeen++
		for _, path := range trackedGoFiles(t, abs) {
			perFile := map[string]bool{}
			fset := token.NewFileSet()
			node, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue
			}
			ast.Inspect(node, func(n ast.Node) bool {
				vs, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					key := strings.Trim(lit.Value, `"`+"`")
					// Ключ метаданных gRPC — строка в НИЖНЕМ регистре: метаданные
					// приводятся к нижнему регистру по построению, а HTTP-заголовок
					// в этом дереве объявляется в канонической форме («X-Kacho-…»).
					// Различение по РЕГИСТРУ точно и не требует разбора типов:
					// проверка по имени метода их не различает вовсе — `Set` есть
					// и у метаданных, и у заголовков.
					if key != strings.ToLower(key) || !strings.HasPrefix(key, "x-kacho-") {
						continue
					}
					scanned++
					perFile[key] = true
				}
				return true
			})

			// Второй проход по собранному: у ключа с человеческим текстом
			// обязана СУЩЕСТВОВАТЬ двоичная форма. Сам прежний ключ при этом
			// законен — он остаётся читаемым запасным путём на окно выкатки,
			// когда край и сервисы катятся не одновременно.
			//
			// Послабление ИСТЕКАЕТ САМО: снимут прежний ключ — останется только
			// двоичный, и проверка продолжит молчать; заведут новый ключ без
			// пары — покраснеет.
			for key := range perFile {
				if hasAny(key, idLikeMarkers) || !hasAny(key, humanTextMarkers) {
					continue
				}
				human++
				if strings.HasSuffix(key, "-bin") || perFile[key+"-bin"] {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				findings = append(findings, rel+": ключ метаданных "+key+
					" несёт текст, введённый человеком, а двоичной формы у него нет — "+
					"первое же не-латинское значение уронит ВЕСЬ вызов, и упадёт он "+
					"не здесь, а на любом последующем запросе вызывающего")
			}
		}
	}

	census := fmt.Sprintf("осмотрено: каталогов %d из 3, объявленных ключей метаданных %d, "+
		"из них несущих человеческий текст %d", dirsSeen, scanned, human)
	if scanned == 0 {
		return census, nil, errors.New("объявленных ключей метаданных не найдено ни одного — " +
			"либо обход слеп, либо форма объявления сменилась; «ноль находок» здесь " +
			"неотличимо от «ноль прочитанного», поэтому это отказ")
	}
	return census, findings, nil
}

func hasAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
