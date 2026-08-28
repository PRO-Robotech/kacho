// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operationnotfoundproducer_test.go — гейт: у отказа «нет такой операции» ровно
// ОДИН производитель в прод-дереве (задача продукта #1370).
//
// # Предмет
//
// Этот 404 приходит клиенту на адрес `/operations/{id}` из двух мест — от
// владельца операции и от края, — и потому обязан быть побайтово одинаковым:
// различие отличает «нет доступа» от «не существует», то есть отменяет само
// сокрытие (`security.md` §Hardening-инварианты, п. 6). Держится это НЕ сверкой
// двух записей, а тем, что запись одна: `operations.NotFoundStatus`.
//
// Гейт стережёт именно ЭТО — чтобы второй записи не завелось снова. Прежде их
// было две, и разошлись они регистром одной буквы: различие, невидимое ни в
// обзоре изменения, ни в проверке, утверждающей код ответа.
//
// # Почему разбор, а не текстовый поиск
//
// Форма стоит в комментариях, объясняющих эту же защиту (в том числе в шапке
// этого файла и в godoc обоих вызывающих). Поиск по подстроке принял бы
// объяснение за производство и покраснел бы на самом себе
// (`testing.md` §«Гейт на класс», п. 4). Судятся только строковые литералы.
//
// # Границы, названные вслух
//
//   - Судится ПРОД-дерево. Дублёр backend'а в пробе воспроизводит текст
//     владельца намеренно — это предмет самой пробы, а не производство на
//     проводе.
//   - Сентинел `operations.ErrNotFound` («operation not found») под
//     распознаватель НЕ подпадает и подпадать не должен: он не несёт id
//     by construction, наружу не уезжает и служит внутрипроцессным значением
//     ошибки. Дискриминатор — подстановка: текст ДЛЯ ВЫЗЫВАЮЩЕГО обязан нести
//     тот id, который вызывающий прислал.
//
// # Предпосылка гейта проверяется им самим
//
// «Ровно один» падает в обе стороны: два и больше — вторая запись завелась;
// ноль — либо производителя не стало, либо распознаватель перестал узнавать
// его форму. Второе опаснее первого, потому что выглядит как чистое дерево.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// operationNotFoundForm — законные формы записи текста «нет такой операции».
//
// Перечислены ВСЕ, в которых предмет записывается в этом дереве, а не одна
// наблюдённая: распознаватель, знающий часть форм, не даёт ни красного, ни
// зелёного — он молчит, и записанное в незнакомой форме оказывается вне
// наблюдения (`testing.md` §«Гейт на класс», п. 7). Регистр не различается
// намеренно: расхождение регистром и было предметом #1370, поэтому
// распознаватель обязан видеть обе буквы, а «ровно один» — не давать им
// сосуществовать.
var operationNotFoundForm = regexp.MustCompile(`(?i)\boperation\b.*%[sqv].*\bnot found\b`)

// operationNotFoundProducerPath — единственное место, где этот текст записан.
var operationNotFoundProducerPath = filepath.Join("pkg", "operations", "notfound.go")

// operationNotFoundProducers — координаты строковых литералов формы, найденные в
// перечисленных файлах, и число ПРОЧИТАННЫХ файлов.
//
// Состав файлов приходит параметром, а не читается изнутри: инъекция обязана
// прогонять ту же перепись, что и гейт, иначе доказывала бы способность падать
// у другого кода.
func operationNotFoundProducers(root string, paths []string) (found []string, filesRead int) {
	for _, p := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			continue
		}
		filesRead++
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		ast.Inspect(f, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				return true
			}
			v, uerr := strconv.Unquote(bl.Value)
			if uerr != nil {
				return true
			}
			if operationNotFoundForm.MatchString(v) {
				found = append(found, fmt.Sprintf("%s:%d: %q", rel, fset.Position(bl.Pos()).Line, v))
			}
			return true
		})
	}
	return found, filesRead
}

// TestOperationNotFoundHasOneProducer — записей этого текста в прод-дереве ровно
// одна, и она там, где объявлена.
func TestOperationNotFoundHasOneProducer(t *testing.T) {
	root := repoRoot(t)
	files := trackedGoFiles(t, root)
	if len(files) == 0 {
		t.Fatal("осмотрено 0 файлов — перепись беспредметна, «ноль находок» неотличим от «ноль прочитанного»")
	}

	found, read := operationNotFoundProducers(root, files)
	t.Logf("осмотрено не-тестовых файлов Go: %d (прочитано разбором %d) · производителей найдено: %d",
		len(files), read, len(found))

	switch {
	case len(found) == 0:
		t.Fatalf("производителей отказа «нет такой операции» найдено 0.\n"+
			"Либо общий производитель снят, либо распознаватель перестал узнавать его форму —\n"+
			"второе выглядит как чистое дерево и потому опаснее. Ожидался ровно один, в %s.",
			operationNotFoundProducerPath)
	case len(found) > 1:
		t.Fatalf("текст отказа «нет такой операции» записан в %d местах:\n%s\n\n"+
			"Он уезжает клиенту с ДВУХ полос одного адреса — от владельца операции и от края, —\n"+
			"поэтому обязан быть побайтово одинаковым (`security.md` §Hardening #6).\n"+
			"Держится это единственностью записи, а не сверкой двух: зови %s.",
			len(found), strings.Join(found, "\n"), "operations.NotFoundStatus")
	}

	if !strings.HasPrefix(found[0], operationNotFoundProducerPath+":") {
		t.Fatalf("единственный производитель лежит не там, где объявлено:\n  найден:  %s\n  объявлен: %s\n"+
			"Переехал — поправь объявление в этом гейте тем же изменением.",
			found[0], operationNotFoundProducerPath)
	}
}
