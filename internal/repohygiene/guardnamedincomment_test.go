// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// guardnamedincomment_test.go — гейт на класс: комментарий, называющий защиту,
// обязан иметь эту защиту в прод-коде своего пакета.
//
// Предмет. Комментарий, противоречащий коду, — не косметика. В обычном случае
// следующий читатель «чинит» код под неверный комментарий. Здесь хуже: комментарий
// уже ОТВЕТИЛ на вопрос «закрыт ли этот сегмент словарём», поэтому читатель не идёт
// проверять и пробел остаётся незамеченным ровно потому, что о нём написано.
// Реальный случай: файл в пакете ролей утверждал, что сегмент глагола закрывается
// словарём домена, тогда как во всём пакете это имя встречалось РОВНО ОДИН раз — в
// самом этом комментарии, а проверка правил сверяла лишь мощность, одиночную
// подстановку и грамматику.
//
// Как гейт устроен. Разбор синтаксического дерева, а не текста: иначе гейт нашёл бы
// имя в комментарии, который эту же защиту объясняет, и считал бы её присутствующей.
// Комментарии и выражения читаются РАЗДЕЛЬНО — упоминание засчитывается только из
// комментария, наличие — только из выражения.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// guardedIdentifier — защита, которую нельзя называть действующей, не имея её.
//
// Реестр узкий НАМЕРЕННО: гейт закрывает один класс на поимённом наборе защит, а не
// делает читателя из каждого идентификатора дерева. Область объявлена ниже
// (guardScanRoots) и утверждается переписью в логе.
type guardedIdentifier struct {
	name       string // имя как оно пишется в выражении (без квалификатора пакета)
	why        string // почему именно эта защита в реестре
	retireWhen string // условие снятия, привязанное к ВНЕШНЕМУ факту
}

var guardedIdentifiers = []guardedIdentifier{
	{
		name:       "IsVerbOfType",
		why:        "проверка принадлежности глагола НАБОРУ ТИПА: комментарий, называющий её действующей, отвечает читателю на вопрос о наличии проверки вместо самой проверки",
		retireWhen: "идентификатор удалён из дерева",
	},
	{
		name:       "NormalizeVerb",
		why:        "единственная точка приведения имени глагола: упоминание её как действующей закрывает вопрос, приводится ли имя на этом пути",
		retireWhen: "идентификатор удалён из дерева",
	},
}

// guardScanRoots — ОБЛАСТЬ гейта. Расширение области — отдельная работа, а не
// строчка сюда.
var guardScanRoots = []string{"services", "internal", "gateway"}

func TestCommentsNamingAGuardHaveItInScope(t *testing.T) {
	root := repoRoot(t)
	if len(guardedIdentifiers) == 0 {
		t.Fatalf("реестр защит пуст — гейту нечего проверять")
	}

	// пакет → есть ли идентификатор в ВЫРАЖЕНИИ непрод… то есть прод-кода пакета
	usedIn := map[string]map[string]bool{}
	// пакет → упоминания в комментариях: файл:строка
	mentionedIn := map[string]map[string][]string{}

	files := 0
	for _, sub := range guardScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if n := d.Name(); n == "vendor" || n == "node_modules" || n == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".pb.go") {
				return nil
			}
			files++
			pkgDir := filepath.Dir(path)
			isTest := strings.HasSuffix(path, "_test.go")

			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				t.Fatalf("%s: разбор не удался (%v) — гейт не вправе трактовать неразобранный "+
					"файл как «упоминаний нет»", path, perr)
			}

			// (а) наличие — ТОЛЬКО из выражения и ТОЛЬКО из прод-кода
			if !isTest {
				ast.Inspect(f, func(n ast.Node) bool {
					id, ok := n.(*ast.Ident)
					if !ok {
						return true
					}
					for _, g := range guardedIdentifiers {
						if id.Name == g.name {
							if usedIn[pkgDir] == nil {
								usedIn[pkgDir] = map[string]bool{}
							}
							usedIn[pkgDir][g.name] = true
						}
					}
					return true
				})
			}

			// (б) упоминание — ТОЛЬКО из комментария
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					for _, g := range guardedIdentifiers {
						if !strings.Contains(c.Text, g.name) {
							continue
						}
						if mentionedIn[pkgDir] == nil {
							mentionedIn[pkgDir] = map[string][]string{}
						}
						rel, _ := filepath.Rel(root, path)
						pos := fset.Position(c.Pos())
						mentionedIn[pkgDir][g.name] = append(mentionedIn[pkgDir][g.name],
							rel+":"+itoa(pos.Line))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", base, err)
		}
	}

	// перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного»
	if files == 0 {
		t.Fatalf("не прочитано ни одного файла в %v — предпосылка гейта сломана, "+
			"молчание ничего не доказывает (корень=%s)", guardScanRoots, root)
	}
	mentions := 0
	for _, byName := range mentionedIn {
		for _, locs := range byName {
			mentions += len(locs)
		}
	}
	t.Logf("перепись: файлов осмотрено: %d; защит в реестре: %d; упоминаний в комментариях: %d",
		files, len(guardedIdentifiers), mentions)
	if mentions == 0 {
		t.Fatalf("ни одного упоминания защит реестра не найдено во всём дереве — "+
			"либо реестр устарел (снимите записи), либо обход не дошёл до кода; "+
			"молчание при нуле упоминаний ничего не утверждает")
	}

	var pkgs []string
	for pkg := range mentionedIn {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		for _, g := range guardedIdentifiers {
			locs := mentionedIn[pkg][g.name]
			if len(locs) == 0 || usedIn[pkg][g.name] {
				continue
			}
			sort.Strings(locs)
			rel, _ := filepath.Rel(root, pkg)
			t.Errorf("%s: комментарий называет %s действующей защитой (%s), но в прод-коде "+
				"этого пакета такого идентификатора НЕТ.\n"+
				"Это хуже обычного расхождения комментария с кодом: комментарий уже ОТВЕТИЛ на "+
				"вопрос «закрыт ли этот сегмент», поэтому читатель не идёт проверять и пробел "+
				"остаётся незамеченным именно потому, что о нём написано.\n"+
				"Исходы: сказать правду о том, чем сегмент закрыт на самом деле / реально "+
				"вызвать защиту / снять упоминание. Зачем запись в реестре: %s",
				rel, g.name, strings.Join(locs, ", "), g.why)
		}
	}
}

// TestGuardedIdentifiersStillHaveSubject — самоистечение реестра: запись про
// идентификатор, которого в дереве больше нет, исключать нечего.
func TestGuardedIdentifiersStillHaveSubject(t *testing.T) {
	root := repoRoot(t)
	alive := map[string]bool{}
	files := 0
	for _, sub := range guardScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, ".pb.go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files++
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					alive[id.Name] = true
				}
				return true
			})
			return nil
		})
	}
	if files == 0 {
		t.Fatalf("не прочитано ни одного файла — самоистечению нечего проверять")
	}
	for _, g := range guardedIdentifiers {
		if !alive[g.name] {
			t.Errorf("запись реестра %q больше нечего исключать — идентификатора нет в прод-коде "+
				"дерева. Это находка: снимите запись (условие снятия: %s). Оставленная запись "+
				"описывает вчерашнее дерево", g.name, g.retireWhen)
		}
	}
	t.Logf("перепись: прод-файлов осмотрено: %d; записей реестра с живым предметом: %d из %d",
		files, len(guardedIdentifiers), len(guardedIdentifiers))
}
