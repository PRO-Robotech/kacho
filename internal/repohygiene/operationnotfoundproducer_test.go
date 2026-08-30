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
// (`testing.md` §«Гейт на класс», п. 4). Судятся только строковые ВЫРАЖЕНИЯ.
//
// # Судится выражение, а не литерал
//
// Единица суда — строковое выражение целиком, потому что один и тот же текст
// записывается несколькими законными способами, и распознаватель, знающий
// часть, не даёт ни красного, ни зелёного: он МОЛЧИТ, а записанное в незнакомой
// форме оказывается вне наблюдения (`testing.md` §«Гейт на класс», п. 7).
// Выражение приводится к ФОРМЕ: литеральные куски — как есть, всякая
// подстановка — маркером `%v`. Тогда склейка `"operation " + id + " not found"`
// даёт форму `operation %v not found` и судится наравне с готовым литералом
// `"operation %s not found"` — а различить их на глаз в обзоре изменения нельзя.
//
// Формы, которые распознаватель знает (у каждой — своя проба в
// `operationnotfoundproducer_injection_test.go`):
//
//   - готовый литерал с подстановкой: `"operation %s not found"` (и `%q`, `%v`);
//   - склейка: `"operation " + id + " not found"`;
//   - склейка с вложенным `fmt.Sprintf`: `"operation " + fmt.Sprintf("%s", id) + " not found"`;
//   - склейка с константой того же файла: `"operation " + id + notFoundSuffix`;
//   - любая из них в любом регистре — расхождение регистром и было предметом
//     #1370, поэтому распознаватель обязан видеть обе буквы, а «ровно один» —
//     не давать им сосуществовать.
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
//     то место, куда встанет присланный вызывающим id.
//
// # Формы ВНЕ охвата — названы, чтобы их не приняли за проверенные
//
// Каждая требует анализа потока данных, которого у разбора одного файла нет
// by construction. Ни одна из них в дереве сегодня не встречается, но молчание
// распознавателя на них — граница, а не вердикт:
//
//   - константа или переменная из ДРУГОГО файла либо пакета: распознаватель
//     читает файлы по одному и чужих объявлений не видит;
//   - сборка присваиваниями (`s := "operation "; s += id; s += " not found"`) —
//     формы-выражения у такого текста нет вовсе;
//   - `var`-переменная вместо константы: её значение вправе поменяться после
//     объявления, поэтому подстановка литерала дала бы находку о тексте,
//     которого может не быть — ложное красное дороже этой полосы;
//   - сборка вызовами (`strings.Join`, `strings.Builder`, `bytes.Buffer`);
//   - форматная строка, пришедшая переменной (`fmt.Sprintf(tmpl, id)`).
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

// operationNotFoundForm — форма текста «нет такой операции».
//
// Судится не исходный текст выражения, а его ФОРМА (см. шапку файла), поэтому
// одно выражение описывает все законные записи разом: подстановка приходит сюда
// либо своим глаголом (`%s`/`%q`/`%v`), либо маркером
// operationNotFoundSubstitution, которым распознаватель заменяет всякое
// значение времени исполнения.
var operationNotFoundForm = regexp.MustCompile(`(?i)\boperation\b.*%[sqv].*\bnot found\b`)

// operationNotFoundSubstitution — маркер подстановки в форме выражения: место,
// куда во время исполнения встанет значение. Совпадает с глаголом `%v`
// намеренно — форма не должна зависеть от того, склейкой записан текст или
// готовым литералом.
const operationNotFoundSubstitution = "%v"

// operationNotFoundProducerPath — единственное место, где этот текст записан.
var operationNotFoundProducerPath = filepath.Join("pkg", "operations", "notfound.go")

// operationNotFoundCensus — объём ОСМОТРЕННОГО.
//
// Печатается всегда и по осям раздельно: «ноль находок» обязано быть отличимо
// от «ноль прочитанного», а расширение распознавателя — менять осмотренное.
// Расширение, не изменившее ни одной оси, холостое и подлежит снятию, а не
// хранению «на всякий случай» (`testing.md` §«Гейт на класс», п. 7).
type operationNotFoundCensus struct {
	filesRead int // файлов, разобранных без ошибки
	literals  int // строковых литералов, вынесенных на суд
	concats   int // внешних строковых склеек, вынесенных на суд
}

func (c operationNotFoundCensus) String() string {
	return fmt.Sprintf("прочитано разбором файлов %d · строковых выражений на суде %d (литералов %d · склеек %d)",
		c.filesRead, c.literals+c.concats, c.literals, c.concats)
}

// operationNotFoundStringConsts — строковые константы файла.
//
// Только `const` и только ЭТОТ файл: `var` вправе поменять значение после
// объявления, а чужой файл распознавателю не виден (см. §«Формы вне охвата»).
func operationNotFoundStringConsts(f *ast.File) map[string]string {
	consts := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		decl, ok := n.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			return true
		}
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				if v, hasLiteral := operationNotFoundShape(vs.Values[i], nil); hasLiteral {
					consts[name.Name] = v
				}
			}
		}
		return true
	})
	return consts
}

// operationNotFoundIsSprintf — вызов ли это `fmt.Sprintf`.
func operationNotFoundIsSprintf(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Sprintf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

// operationNotFoundShape — форма выражения: литеральные куски как есть, всякая
// подстановка — маркером. Второе значение — несёт ли выражение хоть один
// литерал: выражение без литералов текста не задаёт и на суд не выносится.
func operationNotFoundShape(e ast.Expr, consts map[string]string) (shape string, hasLiteral bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return operationNotFoundSubstitution, false
		}
		v, err := strconv.Unquote(x.Value)
		if err != nil {
			return operationNotFoundSubstitution, false
		}
		return v, true
	case *ast.ParenExpr:
		return operationNotFoundShape(x.X, consts)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return operationNotFoundSubstitution, false
		}
		l, lh := operationNotFoundShape(x.X, consts)
		r, rh := operationNotFoundShape(x.Y, consts)
		return l + r, lh || rh
	case *ast.Ident:
		if v, ok := consts[x.Name]; ok {
			return v, true
		}
		return operationNotFoundSubstitution, false
	case *ast.CallExpr:
		// Внутри склейки формат `fmt.Sprintf` и есть текст, который увидит
		// вызывающий; вне склейки его литерал судится сам по себе.
		if operationNotFoundIsSprintf(x.Fun) && len(x.Args) > 0 {
			return operationNotFoundShape(x.Args[0], consts)
		}
		return operationNotFoundSubstitution, false
	}
	return operationNotFoundSubstitution, false
}

// operationNotFoundIsStringConcat — склейка ли это, задающая текст.
func operationNotFoundIsStringConcat(n ast.Node, consts map[string]string) bool {
	b, ok := n.(*ast.BinaryExpr)
	if !ok || b.Op != token.ADD {
		return false
	}
	_, hasLiteral := operationNotFoundShape(b, consts)
	return hasLiteral
}

// operationNotFoundProducers — координаты строковых ВЫРАЖЕНИЙ формы, найденные
// в перечисленных файлах, и объём осмотренного.
//
// Состав файлов приходит параметром, а не читается изнутри: инъекция обязана
// прогонять ту же перепись, что и гейт, иначе доказывала бы способность падать
// у другого кода.
//
// Внешняя склейка судится целиком и один раз: форма вложенной есть подстрока
// формы внешней, поэтому отдельный суд над вложенной не добавил бы находок, а
// добавил бы вторую координату одному месту. По той же причине литерал,
// накрытый уже сработавшей склейкой, находкой второй раз не становится.
func operationNotFoundProducers(root string, paths []string) (found []string, census operationNotFoundCensus) {
	for _, p := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			continue
		}
		census.filesRead++
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		consts := operationNotFoundStringConsts(f)

		type span struct{ from, to token.Pos }
		var covered []span
		isCovered := func(pos token.Pos) bool {
			for _, s := range covered {
				if pos >= s.from && pos < s.to {
					return true
				}
			}
			return false
		}
		record := func(n ast.Node, shape string) {
			if isCovered(n.Pos()) {
				return
			}
			covered = append(covered, span{n.Pos(), n.End()})
			found = append(found, fmt.Sprintf("%s:%d: %q", rel, fset.Position(n.Pos()).Line, shape))
		}

		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					census.literals++
					if v, err := strconv.Unquote(x.Value); err == nil && operationNotFoundForm.MatchString(v) {
						record(x, v)
					}
				}
			case *ast.BinaryExpr:
				// Только ВНЕШНЯЯ склейка: у вложенной форма — подстрока формы
				// внешней, и суд над ней был бы вторым судом того же места.
				if operationNotFoundIsStringConcat(x, consts) && !operationNotFoundInsideConcat(stack, consts) {
					census.concats++
					if shape, _ := operationNotFoundShape(x, consts); operationNotFoundForm.MatchString(shape) {
						record(x, shape)
					}
				}
			}
			stack = append(stack, n)
			return true
		})
	}
	return found, census
}

// operationNotFoundInsideConcat — стоит ли узел внутри строковой склейки.
// Скобки прозрачны: `("operation " + id) + " not found"` — одна склейка, а не
// две.
func operationNotFoundInsideConcat(stack []ast.Node, consts map[string]string) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		if _, isParen := stack[i].(*ast.ParenExpr); isParen {
			continue
		}
		return operationNotFoundIsStringConcat(stack[i], consts)
	}
	return false
}

// TestOperationNotFoundHasOneProducer — записей этого текста в прод-дереве ровно
// одна, и она там, где объявлена.
func TestOperationNotFoundHasOneProducer(t *testing.T) {
	root := repoRoot(t)
	files := trackedGoFiles(t, root)
	if len(files) == 0 {
		t.Fatal("осмотрено 0 файлов — перепись беспредметна, «ноль находок» неотличим от «ноль прочитанного»")
	}

	found, census := operationNotFoundProducers(root, files)
	t.Logf("осмотрено не-тестовых файлов Go: %d · %s · производителей найдено: %d",
		len(files), census, len(found))

	switch {
	case census.concats == 0:
		t.Fatalf("склеек на суде 0 — распознаватель конкатенаций беспредметен.\n" +
			"Текст, собранный склейкой, побайтово равен собранному подстановкой, но в обзоре\n" +
			"изменения неотличим от обычного кода; если склеек в дереве не стало вовсе,\n" +
			"это надо увидеть, а не принять за чистоту.")
	case len(found) == 0:
		t.Fatalf("производителей отказа «нет такой операции» найдено 0.\n"+
			"Либо общий производитель снят, либо распознаватель перестал узнавать его форму —\n"+
			"второе выглядит как чистое дерево и потому опаснее. Ожидался ровно один, в %s.",
			operationNotFoundProducerPath)
	case len(found) > 1:
		t.Fatalf("текст отказа «нет такой операции» записан в %d местах:\n%s\n\n"+
			"Он уезжает клиенту с ДВУХ полос одного адреса — от владельца операции и от края, —\n"+
			"поэтому обязан быть побайтово одинаковым (`security.md` §Hardening #6).\n"+
			"Держится это единственностью записи, а не сверкой двух: зови %s.\n"+
			"Координата печатает ФОРМУ выражения, а не его исходный текст: склейка\n"+
			"`\"operation \" + id + \" not found\"` показана как %q.",
			len(found), strings.Join(found, "\n"), "operations.NotFoundStatus", "operation %v not found")
	}

	if !strings.HasPrefix(found[0], operationNotFoundProducerPath+":") {
		t.Fatalf("единственный производитель лежит не там, где объявлено:\n  найден:  %s\n  объявлен: %s\n"+
			"Переехал — поправь объявление в этом гейте тем же изменением.",
			found[0], operationNotFoundProducerPath)
	}
}
