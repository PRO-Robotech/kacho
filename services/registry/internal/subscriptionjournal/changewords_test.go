// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// Производитель слов журнала у реестра — МИГРАЦИЯ, а не код Go: строку пишет
// триггер базы, и словарь родов события закрыт ограничением той же таблицы.
// Поэтому перепись читает SQL, а не синтаксическое дерево Go.
//
// Файл миграции ищется ПО ПРЕДМЕТУ (упоминание имени таблицы), а не по имени:
// имя несёт метку времени заведения, и выписанное сюда оно устарело бы при
// первой же следующей миграции журнала.
var (
	// checkWordsRe — перечень слов, разрешённых ограничением базы.
	checkWordsRe = regexp.MustCompile(`(?s)registry_resource_journal_event_type_check\s*\n?\s*CHECK\s*\(\s*event_type\s*=\s*ANY\s*\(\s*ARRAY\[(.*?)\]\s*\)\s*\)`)
	// sqlWordRe — строковый литерал SQL.
	sqlWordRe = regexp.MustCompile(`'([A-Za-z_.]+)'`)
	// emitWordRe — слово, которое ПИШЕТ триггер (присваивание рода события).
	emitWordRe = regexp.MustCompile(`v_event\s*:=\s*'([A-Z]+)'`)
	// emitKindRe — слово вида, которым триггер заполняет колонку `resource_kind`.
	emitKindRe = regexp.MustCompile(`VALUES\s*\(\s*'([A-Za-z_]+)'`)
)

// journalMigration возвращает текст миграции, заводящей ресурсный журнал.
//
// Отказ, а не пустая строка: разбор, судящий пустоту, отвечает «расхождений нет»
// даром.
func journalMigration(t *testing.T) string {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("состав миграций не прочитался: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("миграций ноль — встроенная файловая система пуста, и перепись беспредметна")
	}
	var found []string
	var body string
	for _, name := range names {
		b, rerr := fs.ReadFile(migrations.FS, name)
		if rerr != nil {
			t.Fatalf("миграция %s не прочиталась: %v", name, rerr)
		}
		if strings.Contains(string(b), "CREATE TABLE kacho_registry.registry_resource_journal") {
			found = append(found, name)
			body = string(b)
		}
	}
	if len(found) != 1 {
		t.Fatalf("миграций, заводящих ресурсный журнал, найдено %d %v среди %d осмотренных, ожидалась ровно одна: "+
			"ноль означает, что разбор сломан либо таблица переименована; больше одной — что журнал заводится дважды",
			len(found), found, len(names))
	}
	t.Logf("осмотрено миграций %d; журнал заводит %s", len(names), found[0])
	return body
}

// TestChangeDictionaryIsDerivedFromTheMigration — словарь родов изменения
// сверяется с ПРОИЗВОДИТЕЛЕМ, а не со вторым рукописным перечнем.
//
// # Почему перепись, а не список
//
// Производителей у слова два, и они обязаны сходиться: ограничение базы решает,
// какое слово вообще может лечь в таблицу, триггер решает, какое ложится
// сегодня. Проба, выписывающая слова третий раз, закрепила бы ОТВЕТ словаря, а
// не его согласие с деревом: слово, заменённое в триггере на необъявленное,
// такой пробой не ловится ничем — строка просто перестаёт доставляться, тихо.
//
// # Что именно утверждается — ТРИ стороны
//
//	каждое слово ОГРАНИЧЕНИЯ названо словарём  — иначе строка, законно лежащая в
//	                                             таблице, недоставляема;
//	каждое слово словаря разрешено ОГРАНИЧЕНИЕМ — иначе запись пережила свой
//	                                             предмет: такой строки не бывает;
//	каждое слово ТРИГГЕРА разрешено ограничением — иначе вставка отказывает, и
//	                                             мутация ресурса падает целиком.
//
// Пустой обход — отказ: ноль найденных слов означает, что разбор сломан, и
// «расхождений нет» получено даром.
func TestChangeDictionaryIsDerivedFromTheMigration(t *testing.T) {
	body := journalMigration(t)

	m := checkWordsRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("ограничение словаря родов события не найдено в миграции: разбор сломан, " +
			"и согласие объявления с базой не установлено ни в одну сторону")
	}
	allowed := map[string]bool{}
	for _, w := range sqlWordRe.FindAllStringSubmatch(m[1], -1) {
		allowed[w[1]] = true
	}
	if len(allowed) == 0 {
		t.Fatalf("ограничение найдено, а слов в нём ноль (%q): разбор перечня сломан", m[1])
	}

	emitted := map[string]bool{}
	for _, w := range emitWordRe.FindAllStringSubmatch(body, -1) {
		emitted[w[1]] = true
	}
	if len(emitted) == 0 {
		t.Fatal("триггер не пишет ни одного рода события: разбор сломан либо эмиссии нет вовсе")
	}

	declared := Journal(probeEndpointBase, false).Mapping.Changes

	for word := range allowed {
		if declared[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("ограничение базы разрешает род %q, а словарь его НЕ называет: строка с ним "+
				"недоставляема, и потеря эта тихая — ни отказа, ни пропуска в нумерации", word)
		}
	}
	for word := range declared {
		if !allowed[word] {
			t.Errorf("словарь называет род %q, которого ограничение базы НЕ разрешает: "+
				"такой строки в журнале не бывает, и запись пережила свой предмет", word)
		}
	}
	for word := range emitted {
		if !allowed[word] {
			t.Errorf("триггер пишет род %q, которого ограничение базы НЕ разрешает: "+
				"вставка отказала бы, и мутация реестра упала бы целиком", word)
		}
	}

	words := make([]string, 0, len(allowed))
	for w := range allowed {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("родов разрешено ограничением %d %v; пишет триггер %d; объявлено словарём %d",
		len(allowed), words, len(emitted), len(declared))
}

// TestJournalWordIsDerivedFromTheTrigger — ключ словаря видов сверяется с тем,
// что ПРОИЗВОДИТЕЛИ РЕАЛЬНО кладут в колонку `resource_kind`.
//
// Производителей два: триггер базы (строка реестра) и писатель признака
// существования репозитория на Go (`emitRepositoryJournal` в
// `internal/repo/kacho/pg`, слово — литералом в вызове `outbox.EmitAnchored`).
// Слово выписано в двух местах — у производителя и константой здесь, — и
// расхождение между ними ТИХОЕ: строка с неназванным словом перестаёт
// доставляться без отказа и без пропуска в нумерации.
func TestJournalWordIsDerivedFromTheTrigger(t *testing.T) {
	body := journalMigration(t)

	produced := map[string]int{}
	for _, w := range emitKindRe.FindAllStringSubmatch(body, -1) {
		produced[w[1]]++
	}
	if len(produced) == 0 {
		t.Fatal("в миграции не найдено ни одной вставки со словом вида: разбор сломан, " +
			"и «расхождений нет» получено даром")
	}
	goWords, goFiles := goJournalWords(t)
	if len(goWords) == 0 {
		t.Fatalf("в %d файлах писателя на Go не найдено ни одного вызова %s: разбор сломан либо "+
			"писатель переехал", goFiles, goEmitFunc)
	}
	for w, n := range goWords {
		produced[w] += n
	}

	declared := Journal(probeEndpointBase, false).Mapping.Kinds
	for word := range produced {
		if _, ok := declared[word]; !ok {
			t.Errorf("производитель пишет вид %q, а словарь его НЕ называет: строка недоставляема, "+
				"и вопрос о её видимости задать нечем", word)
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет вид %q, которого не пишет НИ ОДИН производитель: "+
				"запись пережила свой предмет и читается как способность журнала", word)
		}
	}
	t.Logf("видов пишут производители %d %v (из них на Go %v, файлов осмотрено %d); объявлено словарём %d",
		len(produced), produced, goWords, goFiles, len(declared))
}

// goEmitterDir — каталог писателя журнала на Go; goEmitFunc — вызов, которым он
// пишет строку (`outbox.EmitAnchored(ctx, tx, table, kind, …)`), goKindArg —
// позиция слова вида в его аргументах.
const (
	goEmitterDir = "../repo/kacho/pg"
	goEmitFunc   = "EmitAnchored"
	goKindArg    = 3
)

// goJournalWords — слова вида, которые пишут вызовы [goEmitFunc] в не-тестовых
// файлах [goEmitterDir]; слово не литералом — отказ пробы.
func goJournalWords(t *testing.T) (map[string]int, int) {
	t.Helper()
	entries, err := os.ReadDir(goEmitterDir)
	if err != nil {
		t.Fatalf("каталог писателя %s не прочитан: %v", goEmitterDir, err)
	}
	words := map[string]int{}
	files := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(goEmitterDir, name), nil, 0)
		if perr != nil {
			t.Fatalf("файл %s не разобрался: %v", name, perr)
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != goEmitFunc {
				return true
			}
			if len(call.Args) <= goKindArg {
				t.Errorf("%s: вызов %s с %d аргументами", fset.Position(call.Pos()), goEmitFunc, len(call.Args))
				return true
			}
			lit, ok := call.Args[goKindArg].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: вид задан не строковым литералом — перепись его не увидит", fset.Position(call.Pos()))
				return true
			}
			w, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				t.Errorf("%s: литерал вида не разобрался: %v", fset.Position(call.Pos()), uerr)
				return true
			}
			words[w]++
			return true
		})
	}
	if files == 0 {
		t.Fatalf("в %s не осмотрено ни одного файла", goEmitterDir)
	}
	return words, files
}
