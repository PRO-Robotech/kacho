// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// probeownrowscope_test.go — гейт: проба отбирает СВОИ строки положительно,
// а не перечислением чужих.
//
// # Предмет
//
// Запрос пробы вида «всё, кроме вот этих посевных строк» ломается от появления
// нового посева — то есть от работы, к пробе отношения не имеющей. Ломается он
// молча: он не говорит «появился неизвестный посев», он говорит «моих строк
// больше, чем ожидалось», и разбираться идут в код, который ни при чём.
//
// Отрицательный список — родня перечню исключений, который не самоистекает: его
// предмет растёт сам, а сопровождать его никто не обязан.
//
// # Почему гейт, а не правило
//
// Класс укусил ЧЕТЫРЕ раза на одной таблице `kaname.fga_outbox`, и три раза
// его чинили поимённо, оставляя форму жить в соседнем файле:
//
//	fga_outbox/emitter_integration_test.go          → отбор по своим объектам;
//	cluster_admin_grant_integration_test.go         → отбор по relation+user;
//	access_binding_fga_outbox_integration_test.go   → счёт дельты;
//	internal_iam/register_resource_*_test.go        → issue #510, отбор по своим
//	                                                  объектам.
//
// Каждая починка оставила у себя честный комментарий с уроком — и ни одна не
// помешала следующей. Правило, которое держится вниманием, отличается от
// невыполняемого только тем, что его неисполнение незаметно
// (`multi-agent-flow.md` §11).
//
// # Что гейт держит
//
//	ПРЕДМЕТ   строка запроса, которая РЕАЛЬНО уходит в базу из тестового файла
//	          (аргумент Query/QueryRow/Exec…), содержащая `NOT IN (`.
//	          Разбор идёт по AST и склеивает конкатенацию и именованные куски:
//	          в найденном дефекте предикат жил отдельной константой
//	          (`const notSeed = …`), приклеенной к запросу через `+`, — сканер
//	          по целому литералу его не видел.
//	ПЕРЕПИСЬ  сколько файлов разобрано, сколько обращений к базе найдено,
//	          сколько запросов удалось собрать. «Ноль находок» обязано быть
//	          отличимо от «ноль прочитанного», а ноль собранных запросов при
//	          непустом дереве — поломка разбора, а не чистота.
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
// Он читает ОДНУ форму отрицания — `NOT IN (`. Формы `<>` и `!=` намеренно НЕ
// читаются: перепись по дереву на 2026-08-17 дала 11 таких мест в тестовых
// запросах, и все одиннадцать законны — это предикаты о состоянии предмета
// (`WHERE z.status <> 'UP'`), а не способ отгородиться от посева. Гейт, который
// краснел бы на них, был бы снят первым же автором как непонятный.
//
// Он не отличает «исключил посев» от «исключил что-то другое»: структурно это
// одно и то же. Поэтому находка — повод перевести отбор на положительный, а не
// приговор; две принятые формы названы в тексте падения.
//
// # Две принятые формы отбора
//
//	ПО СВОИМ ОБЪЕКТАМ   `WHERE payload->>'object' = ANY($1::text[])`, где массив
//	                    — те же значения, что тест отправил в запросе. Слепой
//	                    зоны нет by construction: фикстура знает свои объекты.
//	ДЕЛЬТОЙ             снимок `count(*)` до и после, сверка `before == after`.
//	                    Годится ровно для утверждения «ничего не записано» и
//	                    immune к посеву by construction.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `probeownrowscope_injection_test.go`:
// историческая форма дефекта краснеет с координатой, в том числе когда предикат
// вынесен в константу; законные близнецы (положительный отбор, дельта, `NOT IN`
// в тексте, который в базу не уходит) молчат, и перепись при этом растёт.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// probeScopeCensus — объём осмотренного. Без него «ноль находок» неотличимо от
// «ноль прочитанного».
type probeScopeCensus struct {
	Files   int // тестовых файлов разобрано
	DBCalls int // обращений к базе найдено
	Queries int // из них запросов удалось собрать в текст
}

type probeScopeFinding struct {
	File  string
	Line  int
	Query string
}

// dbCallMethods — имена методов, чей строковый аргумент уходит в базу.
// Перечень закрытый и намеренно узкий: расширять его нужно вместе с предметом,
// а не «на всякий случай». `Queue` — постановка запроса в батч pgx.
var dbCallMethods = map[string]bool{
	"Query": true, "QueryRow": true, "Exec": true,
	"QueryContext": true, "QueryRowContext": true, "ExecContext": true,
	"Queue": true,
}

// auditProbeOwnRowScope разбирает тестовые исходники и возвращает перепись и
// находки. Тот же вход, что у прогона по дереву, — поэтому инъекция проверяет
// именно эту функцию, а не её копию.
func auditProbeOwnRowScope(sources map[string]string) (probeScopeCensus, []probeScopeFinding) {
	var census probeScopeCensus
	var findings []probeScopeFinding

	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, sources[path], 0)
		if err != nil {
			// Нечитаемый исходник — находка разбора, а не тишина: иначе гейт
			// объявит чистым файл, которого не прочитал.
			findings = append(findings, probeScopeFinding{
				File: path, Line: 0, Query: "не разобран: " + err.Error(),
			})
			continue
		}
		census.Files++

		names := probeScopeStringNames(file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !dbCallMethods[sel.Sel.Name] {
				return true
			}
			census.DBCalls++
			for _, arg := range call.Args {
				text, ok := resolveSQLText(arg, names)
				if !ok || !probeScopeLooksLikeSQL(text) {
					continue
				}
				census.Queries++
				if probeScopeHasNegativeMembership(text) {
					findings = append(findings, probeScopeFinding{
						File:  path,
						Line:  fset.Position(arg.Pos()).Line,
						Query: probeScopeSquash(text),
					})
				}
				break // SQL в вызове один
			}
			return true
		})
	}
	return census, findings
}

// probeScopeStringNames собирает ИМЕНОВАННЫЕ строковые куски — и на уровне файла,
// и внутри тел функций. Именно там жил предикат найденного дефекта.
func probeScopeStringNames(file *ast.File) map[string]string {
	names := map[string]string{}
	// Два прохода: значение может ссылаться на объявленное выше имя.
	for pass := 0; pass < 2; pass++ {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, id := range spec.Names {
				if i >= len(spec.Values) {
					continue
				}
				if v, ok := resolveSQLText(spec.Values[i], names); ok {
					names[id.Name] = v
				}
			}
			return true
		})
	}
	return names
}

// resolveSQLText собирает текст из литерала, имени и конкатенации.
func resolveSQLText(e ast.Expr, names map[string]string) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			// Сырая строка в обратных кавычках может нести перевод строки —
			// Unquote с ней справляется; сюда попадает только настоящий брак.
			return "", false
		}
		return s, true
	case *ast.Ident:
		v, ok := names[x.Name]
		return v, ok
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := resolveSQLText(x.X, names)
		r, rok := resolveSQLText(x.Y, names)
		switch {
		case lok && rok:
			return l + r, true
		case lok:
			return l, true // правый кусок — подстановка; левого хватает для признака
		case rok:
			return r, true
		}
		return "", false
	case *ast.ParenExpr:
		return resolveSQLText(x.X, names)
	}
	return "", false
}

func probeScopeLooksLikeSQL(s string) bool {
	l := strings.ToLower(s)
	for _, verb := range []string{"select ", "insert ", "update ", "delete "} {
		if strings.Contains(l, verb) {
			return true
		}
	}
	// Голый предикат-кусок (`payload->>'object' NOT IN (…)`) склеивается с
	// запросом и сам глаголом не начинается — его читаем по признаку отбора.
	return probeScopeHasNegativeMembership(s)
}

// probeScopeHasNegativeMembership — `NOT IN (` с любым пробельным набором между
// словами. `not` берётся ТОЛЬКО как отдельное слово: без границы слева совпало бы
// и `cannot in (…)`, то есть гейт краснел бы на форме, к предмету отношения не
// имеющей.
func probeScopeHasNegativeMembership(s string) bool {
	l := strings.ToLower(s)
	for i := 0; i+3 <= len(l); i++ {
		if l[i:i+3] != "not" {
			continue
		}
		if i > 0 && probeScopeIsWordByte(l[i-1]) {
			continue
		}
		j := i + 3
		if j >= len(l) || !probeScopeIsSpace(l[j]) {
			continue
		}
		for j < len(l) && probeScopeIsSpace(l[j]) {
			j++
		}
		if j+2 > len(l) || l[j:j+2] != "in" {
			continue
		}
		j += 2
		for j < len(l) && probeScopeIsSpace(l[j]) {
			j++
		}
		if j < len(l) && l[j] == '(' {
			return true
		}
	}
	return false
}

func probeScopeIsSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func probeScopeIsWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func probeScopeSquash(s string) string {
	out := strings.Join(strings.Fields(s), " ")
	if len(out) > 160 {
		out = out[:160] + "…"
	}
	return out
}

// probeScopeSources читает тестовые исходники ДЕРЕВА — состав берётся из
// индекса git, а не обходом диска: обход прочитал бы рабочие копии агентов и
// распаковки под игнорируемыми каталогами, и вердикт гейта перестал бы быть
// свойством коммита (`treewalkindex_test.go`).
func probeScopeSources(t *testing.T, tt *trackedTree) map[string]string {
	t.Helper()
	sources := map[string]string{}
	for rel := range tt.files {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tt.root, rel))
		if err != nil {
			// Файл есть в индексе, а прочитать нельзя — это находка, а не повод
			// молча уменьшить перепись.
			t.Fatalf("%s: числится в дереве, но не читается: %v", rel, err)
		}
		sources[rel] = string(b)
	}
	return sources
}

func TestProbeSelectsItsOwnRowsPositively(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))
	sources := probeScopeSources(t, tt)

	// Предпосылка гейта: соглашение об имени тестового файла могло смениться.
	// Зелёный вердикт на пустом входе был бы получен даром.
	if len(sources) == 0 {
		t.Fatalf("в составе дерева (%d файлов) не нашлось ни одного *_test.go — "+
			"гейт беспредметен", tt.count())
	}

	census, findings := auditProbeOwnRowScope(sources)

	// Предпосылка разбора: если ни одного запроса собрать не удалось, молчание
	// гейта означает поломку сборки текста, а не чистоту дерева.
	if census.Queries == 0 {
		t.Error("в тестовых файлах не собрано ни одного запроса к базе — разбор сломан. " +
			"Всякий отрицательный отбор был бы объявлен отсутствующим")
	}

	for _, f := range findings {
		t.Errorf("%s:%d — проба отбирает свои строки ОТРИЦАТЕЛЬНО: %q\n\n"+
			"Такой список стареет молча: он растёт от работы, к пробе отношения не "+
			"имеющей, и сопровождать его никто не обязан. Красная сторона шумит; тихая "+
			"хуже — исключив лишнее, тот же список даёт «ноль», и утверждение зеленеет, "+
			"не посмотрев ни на одну строку.\n"+
			"Две принятые формы:\n"+
			"  · ПО СВОИМ — `WHERE <col> = ANY($1::text[])` с теми же значениями, что "+
			"тест отправил в запросе (слепой зоны нет by construction);\n"+
			"  · ДЕЛЬТОЙ — снимок count(*) до и после, сверка before == after (годится "+
			"для утверждения «ничего не записано»).\n"+
			"Прецеденты и разбор — issue #510.",
			f.File, f.Line, f.Query)
	}

	t.Logf("перепись: тестовых файлов разобрано %d, обращений к базе %d, "+
		"запросов собрано %d, находок %d",
		census.Files, census.DBCalls, census.Queries, len(findings))
}
