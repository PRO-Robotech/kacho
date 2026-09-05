// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// fgaoutboxrowowner_test.go — только ОДНО место вправе рендерить строку
// `kaname.fga_outbox`.
//
// ЧТО ЛОВИТСЯ. Строка этой очереди — не «кортеж», а ЕДИНИЦА АТОМАРНОСТИ: она несёт
// весь набор отношений одного субъекта на одном объекте, и дренаж применяет её одним
// вызовом. Значит форма строки и ключ упорядочивающей партиции — один предмет, и
// решать его вправе один пакет (`repo/kacho/pg/fga_outbox`). Второй производитель,
// пишущий свой INSERT со своей раскладкой payload, разъезжается с ним МОЛЧА: он
// собирается, его тесты зелёные, и наблюдаемое расхождение появляется только у
// арендатора — часть его прав видна, часть нет.
//
// Это не гипотеза: такой производитель в дереве был. Выдача, сделанная привязкой,
// доезжала до хранилища прав по одному отношению за раз, тогда как та же выдача,
// сделанная реконсайлером, доезжала целиком.
//
// ЧТО ИМЕННО ЗАПРЕЩЕНО, И ПОЧЕМУ НЕ «ЛЮБАЯ ВСТАВКА». Расщепить набор способен только
// тот, у кого набор ЕСТЬ, — производитель, превращающий срез кортежей в строки. Два
// оставшихся SQL-производителя (посев администратора кластера и наборный backfill
// иерархических указателей) вставляют payload с КОНСТАНТНЫМ отношением: набора у них
// нет by construction, расщеплять нечего, и запрет им нечего было бы запрещать.
// Поэтому предикат сужен до файлов, которые вставляют в очередь И работают с кортежем
// как с типом. Это ловит реальную форму нарушения (та, что была в дереве) и честно
// не претендует на большее — см. перепись, которую гейт печатает.
//
// ЧЕМ ГЕЙТ НЕ ЯВЛЯЕТСЯ. Он не запрещает писать в очередь — он требует делать это
// через владельца формы там, где есть что расщеплять. И он читает ИСПОЛНЯЕМУЮ часть
// (строковые литералы разбором AST), а не текст файла: этот самый комментарий
// называет таблицу, и предикат по подстроке краснел бы на собственном объяснении.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// fgaOutboxTableLiteral — то, что ищется в строковых литералах прод-кода.
const fgaOutboxTableLiteral = "kaname.fga_outbox"

// fgaOutboxRowOwnerDir — единственный каталог, которому разрешено рендерить строку.
// Каталог, а не файл: у владельца может быть больше одного файла, но не больше одного
// дома.
var fgaOutboxRowOwnerDir = filepath.Join("services", "iam", "internal", "repo", "kacho", "pg", "fga_outbox")

// fgaOutboxInsertMarkers — признаки того, что литерал ВСТАВЛЯЕТ строку, а не читает её.
// Чтение (гейты, диагностика, сканер очереди) законно откуда угодно и здесь ни при чём.
var fgaOutboxInsertMarkers = []string{"insert into"}

// fgaRelationTupleType — имя типа, работа с которым означает «у этого места есть
// НАБОР». Оно и отличает производителя, способного расщепить выдачу, от посева с
// константным отношением.
const fgaRelationTupleType = "RelationTuple"

// referencesRelationTuple — упоминает ли файл тип кортежа отношения в исполняемой
// части (селектор `pkg.RelationTuple` либо голый идентификатор).
func referencesRelationTuple(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == fgaRelationTupleType {
			found = true
			return false
		}
		return true
	})
	return found
}

// fgaOutboxScan — результат обхода: перепись плюс находки. Вынесен отдельно, чтобы
// тот же предикат можно было прогнать на синтетическом дереве (инъекция в обе
// стороны) — иначе способность гейта падать осталась бы утверждением о нём самом.
type fgaOutboxScan struct {
	filesScanned            int
	literalsSeen            int
	insertsFound            int
	ownerInserts            int
	ownerDirFound           bool
	offenders               []string
	constantRelationInserts []string
}

// scanFGAOutboxRenderers reads the tree by its GIT INDEX, never by walking the disk:
// under the repository root lie directories the repository does not contain (agent
// worktrees, run reports, build output), and reading them would make this gate's verdict
// a property of somebody's working directory instead of the commit — red on a file that
// is not in the repository, silent in a fresh checkout where it must speak.
//
// A synthetic tree (the injection twins) is not a repository and has no index, so it is
// read from disk — there the filesystem is the only authority there is.
func scanFGAOutboxRenderers(t *testing.T, tree *trackedTree) fgaOutboxScan {
	t.Helper()
	root := tree.root
	var (
		filesScanned            int
		literalsSeen            int
		insertsFound            int
		offenders               []string
		constantRelationInserts []string
		ownerInserts            int
		ownerDirFound           bool
	)

	rels := make([]string, 0, len(tree.files))
	for rel := range tree.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels) // детерминированный порядок находок и переписи
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		// Сгенерированные стабы форму очереди не рендерят и в предмет не входят.
		if strings.HasPrefix(rel, filepath.Join("pkg", "api")) {
			continue
		}
		path := filepath.Join(root, rel)
		filesScanned++

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("%s: разбор не удался: %v", rel, perr)
		}
		inOwner := strings.HasPrefix(rel, fgaOutboxRowOwnerDir)
		if inOwner {
			ownerDirFound = true
		}
		carriesSet := referencesRelationTuple(f)
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				val = lit.Value // сырой backtick-литерал
			}
			literalsSeen++
			low := strings.ToLower(val)
			if !strings.Contains(low, strings.ToLower(fgaOutboxTableLiteral)) {
				return true
			}
			isInsert := false
			for _, m := range fgaOutboxInsertMarkers {
				if strings.Contains(low, m) {
					isInsert = true
					break
				}
			}
			if !isInsert {
				return true
			}
			insertsFound++
			if inOwner {
				ownerInserts++
				return true
			}
			if !carriesSet {
				// Производитель с константным отношением: набора нет, расщеплять нечего.
				constantRelationInserts = append(constantRelationInserts,
					rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
				return true
			}
			offenders = append(offenders,
				rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
			return true
		})
	}
	return fgaOutboxScan{
		filesScanned: filesScanned, literalsSeen: literalsSeen, insertsFound: insertsFound,
		ownerInserts: ownerInserts, ownerDirFound: ownerDirFound,
		offenders: offenders, constantRelationInserts: constantRelationInserts,
	}
}

func TestFGAOutboxRowsAreRenderedOnlyByTheirOwner(t *testing.T) {
	sc := scanFGAOutboxRenderers(t, newTrackedTree(t, repoRoot(t)))
	filesScanned, literalsSeen := sc.filesScanned, sc.literalsSeen
	insertsFound, ownerInserts := sc.insertsFound, sc.ownerInserts
	ownerDirFound, offenders := sc.ownerDirFound, sc.offenders
	constantRelationInserts := sc.constantRelationInserts

	// Перепись — отдельным утверждением, чтобы «ноль находок» было отличимо от «ноль
	// прочитанного».
	t.Logf("осмотрено прод-файлов: %d; строковых литералов: %d; вставок в очередь: %d "+
		"(у владельца: %d; с константным отношением: %d)",
		filesScanned, literalsSeen, insertsFound, ownerInserts, len(constantRelationInserts))
	for _, c := range constantRelationInserts {
		t.Logf("  константное отношение (набора нет, расщеплять нечего): %s", c)
	}

	// Предпосылка гейта: владелец существует и действительно вставляет. Если каталог
	// переехал или вставка ушла в другой слой, запрет ниже перестаёт что-либо значить —
	// и обязан сказать об этом сам, а не тихо зеленеть.
	if !ownerDirFound {
		t.Fatalf("каталог владельца формы строки не найден: %s — гейту нечего защищать, "+
			"перенесите его вместе с владельцем", fgaOutboxRowOwnerDir)
	}
	if ownerInserts == 0 {
		t.Fatalf("владелец (%s) не содержит ни одной вставки в %s — предпосылка гейта не "+
			"выполняется: либо вставка переехала, либо предикат перестал её видеть",
			fgaOutboxRowOwnerDir, fgaOutboxTableLiteral)
	}

	if len(offenders) > 0 {
		t.Fatalf("строку %s рендерит не её владелец (%s):\n  %s\n\n"+
			"Строка этой очереди — единица атомарности: она несёт весь набор отношений "+
			"субъекта на объекте, и ключ партиции обязан покрывать ровно этот набор. "+
			"Второй производитель разъезжается с владельцем молча — соберётся, пройдёт "+
			"свои тесты, и часть выданных прав будет доезжать до хранилища по одному "+
			"отношению. Пишите через fga_outbox.EmitWriteTx / EmitDeleteTx.",
			fgaOutboxTableLiteral, fgaOutboxRowOwnerDir, strings.Join(offenders, "\n  "))
	}
}
