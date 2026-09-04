// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationnameproducer_test.go — имя файла миграции в этом дереве не
// производит ни один инструмент.
//
// # Предмет (задача #566)
//
// Мигратор трёх сервисов нёс глагол `create`, и тот звал `goose.Create` без
// `SetSequential`, то есть выдавал имя с 14-значной меткой времени
// (`20260817042704_имя.sql`). На день заведения гейта это была форма, которой
// дерево не принимало: гейт пространства номеров отвергал номер больше номера
// всякой возможной задачи, и файлов такой формы в дереве было ноль — глаголом ни
// разу не пользовались.
//
// ОБА ЭТИХ ФАКТА ИСТЕКЛИ, и запрет держится уже не ими (перемерено 2026-08-23).
// Уточнением #921 метка времени объявлена ЗАКОННОЙ формой номера, а от файла,
// ДОБАВЛЕННОГО относительно ствола, `TestNewMigrationOutranksEveryAppliedOne` её
// прямо ТРЕБУЕТ; файл такой формы в дереве есть — его приносит линия сведения
// строк личности (#472). Оставить прежнюю формулировку значило бы держать запрет
// на основании, которого нет, и читаться она будет как действующее «дерево эту
// форму не принимает».
//
// Что от запрета ОСТАЁТСЯ: имя миграции в этом дереве пишет АВТОР, потому что
// выбор между двумя законными формами — решение (номер по задаче связывает
// миграцию с задачей; метка времени — только с моментом заведения), а инструмент
// сделал бы этот выбор молча и всегда одинаково. Предмет у гейта, стало быть,
// у́же прежнего, и предикат его снятия назван: если дерево когда-нибудь оставит
// ровно одну законную форму, запрещать выбор станет нечего.
//
// # Почему запрет именно на производителя, а не на форму имени
//
// Форму имени уже судит `TestMigrationVersionIsDerivedFromItsIssue` — по файлам,
// которые в дерево попали. Этот гейт закрывает вход с другой стороны: пока в
// дереве нет вызова, порождающего имя, расхождение между «что предлагает
// инструмент» и «что принимает дерево» не может завестись снова.
//
// Форму номера этот гейт не объявляет и не повторяет — она названа в ОДНОМ
// месте, docs/architecture/migration-version-namespace.md. Здесь важно лишь то,
// что имя пишет АВТОР, а не инструмент.
//
// # Почему AST, а не текст
//
// Слова `goose.Create` в этом дереве встречаются в прозе — в этой шапке, в
// разборе решения, в тексте отказа соседнего гейта. Текстовый предикат краснел
// бы на собственном объяснении, и первое же ложное срабатывание его отключило
// бы. Разбор синтаксиса различает вызов, комментарий и строковый литерал by
// construction.
//
// Доказательство способности упасть — в migrationnameproducer_injection_test.go.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// goosePkgPath — путь пакета, чей `Create` порождает имя файла миграции.
const goosePkgPath = "github.com/pressly/goose/v3"

// nameProducerFinding — вызов, порождающий имя файла миграции.
type nameProducerFinding struct {
	File string
	Line int
	Call string
}

func (f nameProducerFinding) String() string {
	return fmt.Sprintf(
		"%s:%d: вызов %s порождает ИМЯ файла миграции.\n"+
			"    goose без SetSequential выдаёт 14-значную метку времени. Форм номера в этом "+
			"дереве ДВЕ, и обе законны (#921), поэтому выбор между ними — решение автора, а не "+
			"инструмента: номер по задаче связывает миграцию с задачей, метка времени — только "+
			"с моментом заведения.\n"+
			"    Имя новой миграции пишется рукой. Разбор — "+
			"docs/architecture/migration-version-namespace.md.",
		f.File, f.Line, f.Call)
}

// nameProducerCensus — объём осмотренного: «ноль находок» обязано быть отличимо
// от «ноль прочитанного», а у гейта, чья предпосылка — присутствие чужого
// пакета в дереве, есть и третий исход: пакета больше нет, и запрещать нечего.
type nameProducerCensus struct {
	FilesParsed   int
	FilesWithGoos int
	CallsChecked  int
}

func (c nameProducerCensus) String() string {
	return fmt.Sprintf("файлов Go разобрано %d · из них импортируют goose %d · "+
		"вызовов вида <goose>.Create осмотрено %d",
		c.FilesParsed, c.FilesWithGoos, c.CallsChecked)
}

// scanNameProducers разбирает ОДИН файл: имя, под которым в нём импортирован
// goose, и вызовы `<это имя>.Create`.
//
// src == nil означает «прочитай файл с диска» (контракт go/parser). Инъекция
// подаёт исходник байтами — разбор при этом ОДИН, поэтому фикстура не может
// оказаться снисходительнее того, что судит настоящее дерево.
func scanNameProducers(fset *token.FileSet, filename string, src any) (found []nameProducerFinding, importsGoose bool, checked int, err error) {
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, false, 0, err
	}

	// Имя, под которым импортирован goose. Псевдоним учитывается: запрет,
	// который обходится переименованием импорта, ничего не запрещает.
	alias := ""
	for _, imp := range f.Imports {
		p, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil || p != goosePkgPath {
			continue
		}
		importsGoose = true
		alias = "goose"
		if imp.Name != nil {
			alias = imp.Name.Name
		}
	}
	if !importsGoose || alias == "_" || alias == "." {
		// Слепой импорт (`_`) вызовов не порождает; точечный импорт в этом
		// дереве не встречается и запрещён линтером — на нём гейт молчит
		// осознанно, а не по недосмотру.
		return nil, importsGoose, 0, nil
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != alias {
			return true
		}
		checked++
		if sel.Sel.Name != "Create" {
			return true
		}
		found = append(found, nameProducerFinding{
			File: filename,
			Line: fset.Position(call.Lparen).Line,
			Call: alias + "." + sel.Sel.Name,
		})
		return true
	})
	return found, importsGoose, checked, nil
}

func TestNoToolProducesAMigrationFileName(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}

	fset := token.NewFileSet()
	var census nameProducerCensus
	var findings []nameProducerFinding

	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		got, imports, checked, perr := scanNameProducers(fset, filepath.Join(root, filepath.FromSlash(rel)), nil)
		if perr != nil {
			t.Fatalf("разбор %s: %v — гейт обязан отказать, а не пропустить нечитаемый файл", rel, perr)
		}
		census.FilesParsed++
		if imports {
			census.FilesWithGoos++
		}
		census.CallsChecked += checked
		for _, f := range got {
			f.File = rel
			findings = append(findings, f)
		}
	}

	t.Logf("перепись: %s, находок %d", census, len(findings))

	// ПРЕДПОСЫЛКИ. Обе — факты о дереве, которые могут измениться и сделать
	// «ноль находок» пустым звуком.
	if census.FilesParsed == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: разобрано ноль файлов Go — обход сломан, " +
			"и «ноль находок» ниже не значит ничего")
	}
	if census.FilesWithGoos == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: пакет %s в дереве больше не импортирует никто — "+
			"запрещать нечего, и гейт стал формой без содержания. Сними его вместе "+
			"с предметом либо переформулируй под новый инструмент миграций.", goosePkgPath)
	}

	for _, f := range findings {
		t.Error(f.String())
	}
}
