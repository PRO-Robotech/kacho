// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationformdecl.go — форму номера миграции объявляет ОДНО место, остальные
// на него ссылаются.
//
// # Предмет: документ рядом с кодом сильнее гейта
//
// Форма номера была названа в одиннадцати местах дерева, и они разошлись на три
// несовместимые редакции: порядковую (`0NNN_`), выведенную из номера задачи
// (`<задача><порядковый:3>`) и метку времени (`YYYYMMDDHHMMSS`). Гейт требовал
// последней; README каталога миграций, справка мигратора и страница установки
// предписывали первые две.
//
// Расхождение это не косметическое, и цена у него измерена: документ читают ДО
// работы, а гейт краснеет ПОСЛЕ. Исполнитель успевает назвать файл, написать
// миграцию и собрать коммит — и только потом узнаёт, что правило другое.
// Переименовать применённую миграцию нельзя (запрет #5), поэтому починка стоит
// пересборки коммитов. За одну волну в эту ловушку попали три независимых
// исполнителя из семи, коснувшихся миграций; один сдал работу с красным гейтом,
// не назвав красноту в отчёте.
//
// # Механизм: роль места решает, что ему позволено
//
// Мест, где форма может быть названа, три вида, и требование к каждому своё:
//
//  1. КАНОНИЧЕСКИЙ ДОКУМЕНТ ([canonicalMigrationFormDoc]) — единственное место,
//     где форма ОБЪЯВЛЕНА. Ему позволено назвать и отвергнутые формы: разбор
//     решения обязан говорить, что именно отвергнуто и чем закрыт довод
//     отвергнутого. Обязан назвать действующую форму.
//
//  2. ГЕЙТЫ (`internal/repohygiene/`) — обязаны называть то, что требуют:
//     сообщение отказа, не называющее канона, посылает читателя искать его
//     самостоятельно, а комментарий у гейта, не называющий предмета запрета,
//     снимут как непонятный. Им позволено назвать отвергнутые формы в разборе —
//     но только вместе с действующей, в том же файле.
//
//  3. ВСЕ ОСТАЛЬНЫЕ — README каталога миграций, справка мигратора, страницы
//     документации. Своей редакции не имеют: они вправе ПОВТОРИТЬ действующую
//     форму (читателю удобно найти ответ там, где он смотрит) и не вправе
//     назвать отвергнутую. Повтор безопасен ровно потому, что расхождение с
//     каноном ловится здесь механически.
//
// # Канон читается из ГЕЙТА, а не выписывается здесь
//
// Действующая форма берётся из того гейта, который её ТРЕБУЕТ
// ([enforcingMigrationFormGate]), — из ширины его регулярного выражения и из
// названного им токена. Выписать канон здесь значило бы завести двенадцатое
// место об одном предмете: ровно тот дефект, который эта проверка ловит.
// Поменяется требование гейта — находками станут документы, а не эта проверка.
//
// # Граница, названная честно
//
// Проверка судит ШАБЛОНЫ формы (запись с местозаполнителями), а не конкретные
// имена файлов. `715001_resource_name_single_form.sql` в прозе — ссылка на
// применённую миграцию, и таких ссылок в дереве десятки; требовать от них
// канонической формы значило бы требовать переименования применённого. Поэтому
// «ноль находок» здесь означает «ни один шаблон не разошёлся с каноном», а не
// «ни один документ не сбивает с толку»: документ, прескриптивный по интонации и
// без шаблона, эта проверка не увидит. Такие места читает человек — их немного,
// и корпус печатается переписью.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// canonicalMigrationFormDoc — единственное место, где форма ОБЪЯВЛЕНА.
	// Путь назван, а не выведен: «документ, на который ссылаются остальные» —
	// определение через тех, кого проверяем, и оно поехало бы вместе с ними.
	// Переезд документа ловит проверка предпосылки, а не молчание.
	canonicalMigrationFormDoc = "docs/architecture/migration-version-namespace.md"

	// enforcingMigrationFormGate — гейт, ТРЕБУЮЩИЙ форму от новых файлов.
	// Отсюда читается канон.
	enforcingMigrationFormGate = "internal/repohygiene/migrationmonotonic_test.go"

	// gateSourceDir — каталог гейтов: им позволено называть отвергнутые формы
	// в разборе, если рядом названа действующая.
	gateSourceDir = "internal/repohygiene/"
)

// legacyMigrationFormTokens — записи ОТВЕРГНУТЫХ форм.
//
// Каждая — запись с местозаполнителями, то есть утверждение «называй так», а не
// ссылка на существующий файл. Появится четвёртая отвергнутая форма — она вносится
// сюда вместе с решением, которым отвергнута.
var legacyMigrationFormTokens = []string{
	"<задача><порядковый",
	"<номер><порядковый",
	"0NNN",
}

// migrationFormCensus — объём осмотренного. Отдельное утверждение: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type migrationFormCensus struct {
	FilesRead     int
	FilesWithForm int
	Canonical     int
	Legacy        int
}

// migrationFormDoc — один прочитанный документ корпуса.
type migrationFormDoc struct {
	Rel  string
	Body string
}

// canonicalMigrationForm — действующая форма, прочитанная из требующего её гейта.
type canonicalMigrationForm struct {
	// Token — запись формы, названная гейтом (например `YYYYMMDDHHMMSS`).
	Token string
	// Digits — ширина номера, которую гейт принимает по своему регулярному
	// выражению.
	Digits int
}

// datePlaceholderRe — запись формы из местозаполнителей даты и времени.
var datePlaceholderRe = regexp.MustCompile(`\b[YMDHS]{4,}\b`)

// readCanonicalMigrationForm вытаскивает канон из гейта, который его требует.
//
// Ширина берётся РАЗБОРОМ: у гейта две регулярки — принимающая и отвергающая, —
// и текстовый поиск взял бы любую. Разбор берёт ту, что связана с именем
// принимающей переменной.
func readCanonicalMigrationForm(src string) (canonicalMigrationForm, error) {
	var out canonicalMigrationForm

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, enforcingMigrationFormGate, src, parser.ParseComments)
	if err != nil {
		return out, fmt.Errorf("разбор гейта не удался: %w", err)
	}

	widthRe := regexp.MustCompile(`\\d\{(\d+)\}`)
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok || id.Name != "timestamped" {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if m := widthRe.FindStringSubmatch(lit.Value); m != nil {
			out.Digits, _ = strconv.Atoi(m[1])
		}
		return false
	})

	// Токен — самая длинная запись из местозаполнителей даты, названная гейтом.
	// Их в файле несколько (шапка и текст отказа), и они обязаны совпадать —
	// иначе гейт называет читателю не то, что требует.
	seen := map[string]bool{}
	for _, m := range datePlaceholderRe.FindAllString(src, -1) {
		seen[m] = true
		if len(m) > len(out.Token) {
			out.Token = m
		}
	}
	if len(seen) > 1 {
		var names []string
		for s := range seen {
			names = append(names, s)
		}
		sort.Strings(names)
		return out, fmt.Errorf(
			"гейт называет РАЗНЫЕ записи формы (%s) — читателю сказано одно, требуется другое",
			strings.Join(names, ", "))
	}
	return out, nil
}

// migrationFormFindings — находки разбора. Каждая называет координату.
func migrationFormFindings(docs []migrationFormDoc, form canonicalMigrationForm) ([]string, migrationFormCensus) {
	var (
		out    []string
		census migrationFormCensus
	)

	for _, d := range docs {
		census.FilesRead++

		var (
			legacyHits []string
			hasCanon   bool
			hasAny     bool
		)
		for i, line := range strings.Split(d.Body, "\n") {
			if strings.Contains(line, form.Token) {
				hasCanon, hasAny = true, true
				census.Canonical++
			}
			for _, tok := range legacyMigrationFormTokens {
				if !strings.Contains(line, tok) {
					continue
				}
				hasAny = true
				census.Legacy++
				legacyHits = append(legacyHits,
					fmt.Sprintf("%s:%d — %s", d.Rel, i+1, tok))
			}
		}
		if hasAny {
			census.FilesWithForm++
		}
		if len(legacyHits) == 0 {
			continue
		}

		switch {
		case d.Rel == canonicalMigrationFormDoc:
			// Каноническому документу отвергнутые формы позволены: он обязан
			// сказать, что именно отвергнуто. Но действующую он назвать обязан.
			if !hasCanon {
				out = append(out, fmt.Sprintf(
					"%s: документ ОБЪЯВЛЯЕТ форму и не называет действующую (%s).\n"+
						"    Это единственное место, где форма объявлена; пока действующей "+
						"формы в нём нет, её нет нигде, а остальные места ссылаются в пустоту.",
					d.Rel, form.Token))
			}
		case strings.HasPrefix(d.Rel, gateSourceDir):
			// Гейту разбор позволен — вместе с действующей формой в том же файле.
			if !hasCanon {
				out = append(out, fmt.Sprintf(
					"%s: гейт называет отвергнутую форму и НЕ называет действующую (%s):\n      %s\n"+
						"    Сообщение отказа и комментарий у гейта обязаны называть то, что "+
						"требуется, иначе читатель уйдёт исполнять отвергнутое.",
					d.Rel, form.Token, strings.Join(legacyHits, "\n      ")))
			}
		default:
			out = append(out, fmt.Sprintf(
				"%s: место называет ОТВЕРГНУТУЮ форму номера миграции:\n      %s\n"+
					"    Действующая форма — %s (%d цифр); объявлена она в %s, и своей редакции "+
					"здесь быть не должно.\n"+
					"    Документ рядом с кодом читают ДО работы, а гейт краснеет ПОСЛЕ: пока "+
					"эти строки стоят, они уводят исполнителя в форму, которую дерево отвергает, "+
					"и переименовать миграцию задним числом уже нельзя.\n"+
					"    Повторить действующую форму здесь МОЖНО; назвать отвергнутую — нет.",
				d.Rel, strings.Join(legacyHits, "\n      "),
				form.Token, form.Digits, canonicalMigrationFormDoc))
		}
	}

	sort.Strings(out)
	return out, census
}
