// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_rest_front_client_auth_vocabulary.go — разбор: словарь режимов проверки
// клиента, объявленный ПРОДУКТОМ, и его зеркало в харнессе сквозных проб
// (#2463).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Харнесс решает, подавать ли клиентский лист на собственный REST-фронт службы,
// ЗЕРКАЛЯ продуктовый предикат `MTLSConfig.InternalRESTRequiresClientCert`:
// транспорт поднят И эффективный режим — взаимный, а незаданная ручка отвечает
// односторонним умолчанием. Зеркало верное — и ВЫПИСАННОЕ.
//
// Продукт сменит величину режима либо умолчание — зеркало разойдётся МОЛЧА:
// харнесс продолжит решать по прежним величинам. На посадке, где лист требуется,
// он его не подаст (рукопожатие не состоится, суита отдаст «ответа нет»), либо
// подаст там, где он ничего не доказывает. Обе стороны по отдельности верны и
// покрыты своими пробами — неверна их РАЗНИЦА.
//
// ─────────────────────────────────────────────────────────────────────────────
// СУДЯТСЯ ОБЪЯВЛЕНИЯ, А НЕ ПОДСТРОКИ
//
// Обе величины стоят по обе стороны и в пояснениях: комментарий харнесса прямо
// называет продуктовые константы, а продуктовый комментарий — режимы словами.
// Гейт по слову краснел бы на СОБСТВЕННОМ ОБЪЯСНЕНИИ проверяемого — ровно тот
// класс, ради которого он заведён. Поэтому:
//
//	ПРОДУКТ   — синтаксическое дерево Go: объявление константы с именем вида
//	            `clientAuth*` и её строковое значение;
//	ХАРНЕСС   — присваивание МОДУЛЬНОГО уровня `ИМЯ = "значение"` с нулевой
//	            колонки. Комментарий, строка документации и величина внутри
//	            функции присваиванием модульного уровня не являются.
//
// Питон разбирается построчной формой, а не деревом, и это названо честно:
// разборщика Python в этом дереве нет, а заводить его ради двух констант дороже
// самой проверки. Строгость формы (нулевая колонка, ровно одно присваивание на
// имя) покрывает то, ради чего дерево понадобилось бы: второе присваивание того
// же имени — НАХОДКА, а не последнее выигравшее.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// clientAuthConstPrefix — приставка имён продуктовых констант словаря.
const clientAuthConstPrefix = "clientAuth"

// clientAuthDefaultResolver — функция продукта, отвечающая эффективным режимом
// на незаданную ручку. Умолчание берётся ИЗ НЕЁ, а не из перечня констант:
// перечень говорит, какие величины бывают, и молчит о том, какая действует при
// пустом входе.
const clientAuthDefaultResolver = "resolveClientAuthMode"

// productClientAuthVocabulary — словарь продукта: имя константы → значение,
// плюс имя константы, которой отвечает разрешатель на пустой вход.
type productClientAuthVocabulary struct {
	Values     map[string]string
	DefaultVia string // имя константы, возвращаемой разрешателем на ""
	FilesRead  int
}

// ReadProductClientAuthVocabulary читает словарь из каталога продукта.
func ReadProductClientAuthVocabulary(dir string) (productClientAuthVocabulary, error) {
	out := productClientAuthVocabulary{Values: map[string]string{}}

	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return out, err
	}
	sort.Strings(names)
	for _, path := range names {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return out, rerr
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			continue
		}
		out.FilesRead++

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if !strings.HasPrefix(name.Name, clientAuthConstPrefix) || i >= len(vs.Values) {
							continue
						}
						if value, ok := goStringLiteral(vs.Values[i]); ok {
							out.Values[name.Name] = value
						}
					}
				}
			case *ast.FuncDecl:
				if d.Name.Name != clientAuthDefaultResolver || d.Body == nil {
					continue
				}
				if name, ok := emptyInputAnswer(d); ok {
					out.DefaultVia = name
				}
			}
		}
	}
	return out, nil
}

// emptyInputAnswer — имя константы, которой разрешатель отвечает на пустой вход.
//
// Читается ВЕТВЬ `if <параметр> == "" { return <имя> }`, а не первый попавшийся
// возврат: разрешатель отвечает переданным значением во всех прочих случаях, и
// «первый возврат» вернул бы параметр.
func emptyInputAnswer(fn *ast.FuncDecl) (string, bool) {
	var found string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		stmt, ok := n.(*ast.IfStmt)
		if !ok || found != "" {
			return true
		}
		bin, ok := stmt.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQL {
			return true
		}
		lit, ok := goStringLiteral(bin.Y)
		if !ok || lit != "" {
			return true
		}
		for _, s := range stmt.Body.List {
			ret, ok := s.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			if id, ok := ret.Results[0].(*ast.Ident); ok {
				found = id.Name
				return false
			}
		}
		return true
	})
	return found, found != ""
}

func goStringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// pyModuleAssign — присваивание МОДУЛЬНОГО уровня строкового литерала.
// Якорено с нулевой колонки: величина внутри функции, в комментарии и в строке
// документации под него не подпадает.
var pyModuleAssign = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)\s*=\s*("[^"\n]*"|'[^'\n]*')\s*(?:#.*)?$`)

// harnessVocabulary — словарь харнесса: имя → значение, и сколько раз имя
// присвоено (второе присваивание — находка, а не «последнее выиграло»).
type harnessVocabulary struct {
	Values map[string]string
	Counts map[string]int
	Lines  int
}

// ReadHarnessVocabulary читает присваивания модульного уровня из скрипта.
func ReadHarnessVocabulary(src []byte) harnessVocabulary {
	out := harnessVocabulary{Values: map[string]string{}, Counts: map[string]int{}}
	out.Lines = strings.Count(string(src), "\n")
	for _, m := range pyModuleAssign.FindAllStringSubmatch(string(src), -1) {
		name, raw := m[1], m[2]
		out.Values[name] = raw[1 : len(raw)-1]
		out.Counts[name]++
	}
	return out
}

// vocabularyMirror — одна ось зеркала: имя харнесса против имени продукта.
type vocabularyMirror struct {
	HarnessName string
	ProductName string
	// FromDefault — величина берётся не у константы напрямую, а у разрешателя:
	// «чем отвечает продукт на незаданную ручку».
	FromDefault bool
}

// clientAuthMirror — оси, по которым харнесс зеркалит продукт.
//
// Перечень СВЯЗЫВАЮЩИЙ, а не описательный: имя, добавленное в харнесс и не
// названное здесь, останется несверенным — поэтому разбор отдельно утверждает,
// что каждое имя харнесса вида `CLIENT_AUTH_*` этим перечнем накрыто.
var clientAuthMirror = []vocabularyMirror{
	{HarnessName: "CLIENT_AUTH_MUTUAL", ProductName: "clientAuthMutual"},
	{HarnessName: "CLIENT_AUTH_DEFAULT", FromDefault: true},
}

// harnessClientAuthPrefix — приставка имён харнесса, обязанных быть накрытыми
// перечнем осей.
const harnessClientAuthPrefix = "CLIENT_AUTH_"

// VocabularyCensus — объём осмотренного.
type VocabularyCensus struct {
	ProductFiles   int
	ProductValues  int
	HarnessLines   int
	HarnessValues  int
	MirroredAxes   int
	AgreeingAxes   int
	DefaultViaName string
}

func (c VocabularyCensus) String() string {
	return fmt.Sprintf(
		"исходников продукта разобрано %d · величин словаря объявлено %d · умолчание отвечает %q · "+
			"строк харнесса прочитано %d · присваиваний модульного уровня %d · осей зеркала %d · сходится %d",
		c.ProductFiles, c.ProductValues, c.DefaultViaName, c.HarnessLines, c.HarnessValues,
		c.MirroredAxes, c.AgreeingAxes)
}

// AuditClientAuthMirror сверяет зеркало с объявлением. `*testing.T` не трогает:
// разбор, роняющий пробу изнутри, инъекции не поддаётся.
func AuditClientAuthMirror(product productClientAuthVocabulary, harness harnessVocabulary,
	axes []vocabularyMirror) ([]string, VocabularyCensus) {

	census := VocabularyCensus{
		ProductFiles:   product.FilesRead,
		ProductValues:  len(product.Values),
		HarnessLines:   harness.Lines,
		HarnessValues:  len(harness.Values),
		MirroredAxes:   len(axes),
		DefaultViaName: product.DefaultVia,
	}
	var findings []string

	// ПУСТОЙ ОБХОД — находка, а не тишина: «зеркало сходится» верно тривиально,
	// когда сверять нечего.
	if census.ProductValues == 0 {
		findings = append(findings, "обход пуст: продукт не объявил ни одной величины словаря — "+
			"либо имена констант перестали начинаться с объявленной приставки, либо каталог не тот; "+
			"и то и другое читается этой строкой одинаково")
	}
	if census.HarnessValues == 0 {
		findings = append(findings, "обход пуст: у харнесса не прочитано ни одного присваивания "+
			"модульного уровня — сверять зеркало не с чем")
	}
	if product.DefaultVia == "" {
		findings = append(findings, "продукт не назвал константы, которой отвечает на незаданную "+
			"ручку: умолчание невыразимо, и зеркало сверять не с чем")
	}
	if len(findings) > 0 {
		return findings, census
	}

	for _, axis := range axes {
		want, wantName := "", axis.ProductName
		if axis.FromDefault {
			wantName = product.DefaultVia
		}
		value, declared := product.Values[wantName]
		if !declared {
			findings = append(findings, fmt.Sprintf(
				"ось %s ссылается на продуктовую константу %q, которой продукт не объявляет — "+
					"перечень осей пережил свой предмет", axis.HarnessName, wantName))
			continue
		}
		want = value

		got, ok := harness.Values[axis.HarnessName]
		if !ok {
			findings = append(findings, fmt.Sprintf(
				"харнесс не объявляет %s, а продукт объявляет %s=%q — решение о клиентском листе "+
					"принимается по величине, которой у харнесса нет", axis.HarnessName, wantName, want))
			continue
		}
		if n := harness.Counts[axis.HarnessName]; n != 1 {
			findings = append(findings, fmt.Sprintf(
				"%s присвоено %d раз(а): величина выразима несколькими способами, и какой из них "+
					"действует, решает порядок строк, а не объявление", axis.HarnessName, n))
			continue
		}
		if got != want {
			findings = append(findings, fmt.Sprintf(
				"зеркало разошлось с продуктом: харнесс %s=%q, продукт %s=%q. Харнесс продолжит "+
					"решать по прежней величине — на посадке, где лист требуется, не подаст его "+
					"(рукопожатие не состоится, суита отдаст «ответа нет»), либо подаст там, где он "+
					"ничего не доказывает", axis.HarnessName, got, wantName, want))
			continue
		}
		census.AgreeingAxes++
	}

	// Имя харнесса, не накрытое ни одной осью, остаётся НЕСВЕРЕННЫМ — и это
	// невидимо: у него нет ни красного, ни зелёного.
	covered := map[string]bool{}
	for _, axis := range axes {
		covered[axis.HarnessName] = true
	}
	var uncovered []string
	for name := range harness.Values {
		if strings.HasPrefix(name, harnessClientAuthPrefix) && !covered[name] {
			uncovered = append(uncovered, name)
		}
	}
	sort.Strings(uncovered)
	for _, name := range uncovered {
		findings = append(findings, fmt.Sprintf(
			"харнесс объявляет %s, и ни одна ось зеркала его не накрывает — величина не сверяется "+
				"с продуктом ни в какую сторону, то есть расхождение по ней не даст ни красного, "+
				"ни зелёного", name))
	}

	sort.Strings(findings)
	return findings, census
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ.
//
// Способность упасть доказана инъекцией —
// own_rest_front_client_auth_vocabulary_injection_test.go.

// clientAuthProductDir — каталог, где продукт объявляет словарь. Координата
// КАТАЛОГА, а не файла: файл — деталь раскладки, и перечислять его значило бы
// завести перечень, стареющий при первом переименовании.
const clientAuthProductDir = "../services/iam/internal/apps/kaname/config"

func TestOwnRestFront_ClientAuthVocabularyMirrorsTheProduct(t *testing.T) {
	product, err := ReadProductClientAuthVocabulary(clientAuthProductDir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: словарь продукта не прочитан: %v", err)
	}
	src, err := os.ReadFile(ownRestFrontScript)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: харнесс не прочитан: %v", err)
	}
	harness := ReadHarnessVocabulary(src)

	findings, census := AuditClientAuthMirror(product, harness, clientAuthMirror)

	t.Logf("объём осмотренного: %s", census)
	t.Logf("словарь продукта: %s", strings.Join(sortedPairs(product.Values), ", "))

	if len(findings) > 0 {
		t.Fatalf("находок %d:\n  • %s", len(findings), strings.Join(findings, "\n  • "))
	}
	if census.AgreeingAxes != census.MirroredAxes {
		t.Fatalf("перепись не сходится: осей зеркала %d, сходится %d — равенство и есть предмет "+
			"этого гейта", census.MirroredAxes, census.AgreeingAxes)
	}
}

// sortedPairs — детерминизм текста переписи.
func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+strconv.Quote(v))
	}
	sort.Strings(out)
	return out
}
