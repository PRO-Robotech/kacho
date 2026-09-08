// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaauthoritydeclared.go — разбор композиционных корней на предмет того,
// ЧЕМ у потребителя включается фоновый тянущий величины.
//
// # Предмет
//
// Ребро потребитель → домен величин ОДНО, а полос у него ДВЕ: разрешение
// величины на пути запроса и фоновая дельта, которой снимок догоняет авторитет
// (`polyrepo.md` §runtime-edges). Полосы означают разное при недоступности
// соседа — первая fail-closed, вторая ограниченное отставание, — но включаются
// они ОДНИМ объявлением: два объявления об одном ребре разошлись бы молча, и
// разошлись бы именно там, где расхождение значит «спрашиваем у одного,
// догоняем у другого».
//
// До приёмки KAN-QUOTA-1 обе полосы включались НАЛИЧИЕМ СОЕДИНЕНИЯ соседа по
// авторизации. Пока авторитет величин и авторитет авторизации — одна служба,
// это верно; уход модуля из службы доступа это условие снимает, и тогда
// безусловный подъём тянущего отказывает ПРИ СБОРКЕ, а у всех пяти
// потребителей этот отказ ФАТАЛЕН — пять чужих служб не поднимаются вовсе.
//
// # Что здесь считается находкой
//
// Вызов `quota.StartLimitSync`, стоящий ПОД УСЛОВИЕМ. Решение «заводить ли
// тянущего» принимает сама эта функция, читая объявление; всякое объемлющее
// `if`/`switch`/`for`/`select` означает, что потребитель решил ещё раз и по
// СВОЕМУ признаку — обычно по наличию соединения, то есть ровно так, как было.
//
// Свойство выбрано именно «вызов безусловен», а не «объемлющее условие не
// упоминает соединение»: перечислять имена переменных соединения значило бы
// ключеваться на имени, а не на источнике значения, и первый же потребитель с
// другим именем ушёл бы из наблюдения молча.
//
// # Перечень потребителей ВЫВОДИТСЯ, а не выписывается
//
// Потребитель — служба, несущая таблицу курсора дельты (`quota_sync_cursor`) в
// своих миграциях: без неё тянущему не с чего продолжать, а с ней он обязан
// быть заведён. Выписанный список из пяти имён разошёлся бы с деревом при
// шестом потребителе — и разошёлся бы в сторону молчания.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
// Вызов, спрятанный за собственной обёрткой потребителя (`startQuotaSync()`,
// объявленной в том же `cmd`), разбор находит только если обёртка зовёт
// `StartLimitSync` сама и делает это безусловно: тогда находкой станет место
// внутри обёртки. Обёртка, принимающая решение за неё, — форма, которой в
// дереве нет, и заводить её значит вернуть предмет запрета.
//
// Единственная сборка тянущего (`newLimitSyncer`) не экспортируется, поэтому
// обойти `StartLimitSync` нельзя ВОВСЕ — это свойство держит компилятор, а не
// этот гейт.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// quotaSyncStarter — имя глагола, включающего обе полосы ребра.
const quotaSyncStarter = "StartLimitSync"

// quotaCursorTable — таблица курсора дельты; её наличие в миграциях службы и
// делает службу потребителем ребра.
const quotaCursorTable = "quota_sync_cursor"

// quotaStartSite — одно место подъёма: где стоит и под условием ли.
type quotaStartSite struct {
	File        string
	Line        int
	Conditional bool
	// Under — имя объемлющей конструкции, чтобы находка называла предмет, а не
	// симптом: читатель обязан увидеть, ЧТО именно решило за объявление.
	Under string
}

// quotaConsumers отдаёт службы, несущие таблицу курсора дельты, — то есть тех,
// у кого тянущий обязан быть заведён.
func quotaConsumers(root string) ([]string, error) {
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, fmt.Errorf("каталог служб: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		migrations := filepath.Join(servicesDir, e.Name(), "internal", "migrations")
		carries, cerr := dirMentions(migrations, quotaCursorTable)
		if cerr != nil {
			return nil, cerr
		}
		if carries {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// dirMentions — есть ли в каталоге файл, называющий fragment. Отсутствие
// каталога не отказ: служба без миграций потребителем не является.
func dirMentions(dir, fragment string) (bool, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s: %w", dir, err)
	}
	files, err := treecorpus.UnderWithSuffix(dir, ".sql")
	if err != nil {
		return false, fmt.Errorf("состав каталога миграций %s: %w", dir, err)
	}
	for _, f := range files {
		// Форма подавления — та, которую читает сам сканер, и стоит она в
		// строке вызова: чужая форма в этом дереве инертна по построению, а
		// объяснение её в комментарии считается подавлением наравне с
		// настоящим — поэтому здесь она не цитируется вовсе.
		body, rerr := os.ReadFile(f) // #nosec G304 -- путь из индекса git этого дерева
		if rerr != nil {
			return false, fmt.Errorf("%s: %w", f, rerr)
		}
		if strings.Contains(string(body), fragment) {
			return true, nil
		}
	}
	return false, nil
}

// quotaStartSitesOf разбирает композиционный корень службы и отдаёт все места
// подъёма тянущего.
func quotaStartSitesOf(root, service string) ([]quotaStartSite, error) {
	cmdDir := filepath.Join(root, "services", service, "cmd")
	if _, err := os.Stat(cmdDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", cmdDir, err)
	}

	// Состав берётся у ИНДЕКСА git, а не с диска. Правила игнорирования
	// действуют на любой глубине, и под каталогом службы на всякой машине, где
	// поднимали стенд или собирали фронтенд, лежит неверсионируемое —
	// распаковки чартов, сборочные каталоги, отчёты прогонов. Обход по диску
	// делает вердикт свойством машины, а не дерева.
	files, err := treecorpus.UnderWithSuffix(cmdDir, ".go")
	if err != nil {
		return nil, fmt.Errorf("состав композиционного корня %s: %w", cmdDir, err)
	}

	var sites []quotaStartSite
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, fmt.Errorf("разбор %s: %w", path, perr)
		}
		sites = append(sites, quotaStartSitesIn(fset, file, path)...)
	}
	return sites, nil
}

// quotaStartSitesIn — разбор одного файла. Судит УЗЕЛ вызова, а не подстроку:
// имя глагола стоит и в комментариях, объясняющих сам запрет, и гейт, судящий
// текст, краснел бы на собственном объяснении.
func quotaStartSitesIn(fset *token.FileSet, file *ast.File, path string) []quotaStartSite {
	var (
		sites []quotaStartSite
		stack []ast.Node
	)
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		stack = append(stack, n)

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != quotaSyncStarter {
			return true
		}
		under := enclosingBranch(stack)
		sites = append(sites, quotaStartSite{
			File:        path,
			Line:        fset.Position(call.Pos()).Line,
			Conditional: under != "",
			Under:       under,
		})
		return true
	})
	return sites
}

// enclosingBranch отдаёт имя ближайшей объемлющей ветвящейся конструкции либо
// пустую строку, если вызов безусловен.
func enclosingBranch(stack []ast.Node) string {
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.IfStmt:
			return "if"
		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			return "switch"
		case *ast.ForStmt, *ast.RangeStmt:
			return "for"
		case *ast.SelectStmt:
			return "select"
		case *ast.FuncDecl:
			// Дошли до объявления функции, не встретив ветвления, — вызов
			// безусловен в пределах своей функции.
			return ""
		}
	}
	return ""
}
