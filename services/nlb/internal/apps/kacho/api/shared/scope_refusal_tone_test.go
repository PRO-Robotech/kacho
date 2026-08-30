// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Отказ на правку ОБЛАСТИ ВЛАДЕНИЯ — один тон на все ресурсы, и каждый называет
// следующий шаг.
//
// # Предмет
//
// `project_id` неизменяем у каждого ресурса nlb, и отвергают его правку ТРИ
// разные полосы — балансировщик, группа целей, слушатель. Тон сообщений — часть
// контракта (`api-conventions.md` §Error-format: «Тексты — часть контракта;
// меняются только осознанно»), поэтому три ответа на ОДИН запрет обязаны
// совпадать по форме. До #1671 они расходились, и различие никем не решалось:
// балансировщик называл глагол переноса и терял канонический хвост
// «after <Resource>.Create», а группа целей и слушатель несли канонический текст
// и следующего шага не называли вовсе.
//
// Цена расхождения измерима в шагах клиента: получив отказ балансировщика, он
// знает, что делать; получив отказ группы целей — идёт читать документацию, хотя
// `TargetGroupService.Move` существует и ему доступен.
//
// # Почему СРАВНЕНИЕ ПОЛОС, а не проба у каждой
//
// Проба у каждой полосы закрепляет ОТВЕТ этой полосы и о согласии полос не
// утверждает ничего: обе редакции были защитимы по отдельности, неверна была их
// РАЗНИЦА (`architecture.md` §«Параллельные полосы одного механизма обязаны
// сверяться МЕЖДУ СОБОЙ»). Поэтому здесь спрашивается не «каким должен быть
// текст» — это и есть спорный вопрос, — а «решал ли кто-нибудь, что они
// различаются».
//
// # Почему разбор, а не поиск по образцу
//
// Тексты отказов дословно стоят в объяснениях — в шапке `listener/doc.go`, в
// комментариях самих полос и в этом файле тоже. Поиск по подстроке краснел бы на
// собственном объяснении. Здесь судится УЗЕЛ: строковый литерал исходника
// (`*ast.BasicLit`), а комментарий узлом-литералом не является by construction.
//
// # Чего проверка НЕ утверждает, и это названо, а не умолчано
//
// Она читает текст, ЗАПИСАННЫЙ литералом. Сообщение, собранное во время
// исполнения из частей (имя поля подставляется в формат), ей не видно — такой
// текст для неё не существует, и молчания это не оправдывает
// (`testing.md` §«Гейт на класс», п. 7). Ровно так до #1671 была невидима полоса
// слушателя: она производила отказ форматом `"%s is immutable after
// Listener.Create"` по НАБОРУ путей, поэтому перепись по литералу находила две
// полосы из трёх. Полосы сведены к одной форме записи (таблица путь→текст)
// именно затем, чтобы распознаватель видел их все; нижняя граница переписи ниже
// делает молчаливое возвращение к формату видимым.
const scopeField = "project_id"

// Канонический тон неизменяемости (`api-conventions.md` §Error-format) —
// «<field> is immutable after <Resource>.Create». Ему следует вся платформа:
// vpc Network/CidrGroup/RouteTable, storage Snapshot, registry Registry.
var scopeCanonicalPrefix = regexp.MustCompile(`^project_id is immutable after [A-Za-z]+\.Create`)

// Следующий шаг — глагол переноса, названный поимённо. Связка `; use ` взята у
// балансировщика: она уже была в дереве, и изобретать вторую не за чем.
var scopeNextStep = regexp.MustCompile(`; use [A-Za-z]+Service\.Move`)

// scopeRefusal — один найденный отказ: координата и дословный текст.
type scopeRefusal struct {
	pos  string
	text string
}

// isScopeRefusal — отбор. Отдельной функцией, чтобы законный близнец (отказ про
// СОСЕДНЕЕ неизменяемое поле) проверялся тем же кодом, каким идёт отбор в
// дереве, а не похожим на него.
func isScopeRefusal(s string) bool {
	return strings.HasPrefix(s, scopeField+" is immutable")
}

// scopeToneDefects — предикат в чистом виде: принимает найденное, возвращает
// находки. Вынесен из обхода намеренно — так его способность краснеть и молчать
// доказывается подачей входа, а не сборкой синтетического дерева.
func scopeToneDefects(found []scopeRefusal) []string {
	var out []string
	for _, r := range found {
		switch {
		case !scopeCanonicalPrefix.MatchString(r.text):
			out = append(out, r.pos+": отказ не несёт канонического тона "+
				`"<field> is immutable after <Resource>.Create" — `+strconv.Quote(r.text))
		case !scopeNextStep.MatchString(r.text):
			out = append(out, r.pos+": отказ не называет следующего шага "+
				`("; use <Service>.Move") — `+strconv.Quote(r.text))
		}
	}
	sort.Strings(out)
	return out
}

// foldStringConcat — склеивает `"a" + "b" + …` в одну строку. Возвращает
// ok=false, если хотя бы одно слагаемое не строковый литерал: текст, часть
// которого вычисляется, целиком не известен, и судить его как известный нельзя.
func foldStringConcat(e ast.Expr) (string, bool) {
	switch n := e.(type) {
	case *ast.BasicLit:
		if n.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(n.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if n.Op != token.ADD {
			return "", false
		}
		l, okL := foldStringConcat(n.X)
		r, okR := foldStringConcat(n.Y)
		if !okL || !okR {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return foldStringConcat(n.X)
	}
	return "", false
}

// collectScopeRefusals — обход прод-дерева use-cases nlb. Возвращает найденное и
// объём осмотренного: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
func collectScopeRefusals(t *testing.T) (found []scopeRefusal, filesRead int) {
	t.Helper()

	root := filepath.Join("..")
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("%s не разобран: %v — гейт судит по узлам, и неосмотренный файл его "+
				"молчания не оправдывает", path, parseErr)
		}
		filesRead++
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BinaryExpr:
				// Склейка литералов — законная и обычная здесь форма записи
				// длинного текста. Читается ЦЕЛИКОМ и не раскладывается на части:
				// иначе распознаватель увидел бы первое слагаемое и объявил бы
				// находкой обрыв, которого у клиента нет. Ровно так гейт и повёл
				// себя на первом же прогоне после сведения полос — доказательство
				// того, что форму надо уметь прочитать, а не обойти переносом
				// текста в одну строку.
				whole, ok := foldStringConcat(node)
				if !ok {
					return true
				}
				if isScopeRefusal(whole) {
					found = append(found, scopeRefusal{
						pos: fset.Position(node.Pos()).String(), text: whole})
				}
				return false // части уже прочитаны — не считать их второй раз
			case *ast.BasicLit:
				if node.Kind != token.STRING {
					return true
				}
				s, unqErr := strconv.Unquote(node.Value)
				if unqErr != nil || !isScopeRefusal(s) {
					return true
				}
				found = append(found, scopeRefusal{
					pos: fset.Position(node.Pos()).String(), text: s})
				return true
			}
			return true
		})
		return nil
	})
	require.NoError(t, err, "обход %s не завершён", root)
	return found, filesRead
}

// TestProjectScopeRefusalsCarryOneToneAndNameTheNextStep — несущее утверждение
// гейта. Печатает ОБЕ величины переписи («полос N · несут свойство M»): одна
// скрыла бы ровно тот случай, ради которого гейт заведён — полосу, ушедшую из-под
// наблюдения.
func TestProjectScopeRefusalsCarryOneToneAndNameTheNextStep(t *testing.T) {
	t.Parallel()

	found, filesRead := collectScopeRefusals(t)
	defects := scopeToneDefects(found)

	t.Logf("перепись: файлов Go прочитано %d · полос отказа по %s найдено %d · несут один тон %d",
		filesRead, scopeField, len(found), len(found)-len(defects))

	require.Positive(t, filesRead,
		"обход пуст — вердикт беспредметен: гейт не прочитал ни одного файла")

	// Нижняя граница переписи. Полос известно три — балансировщик, группа целей,
	// слушатель. Она стоит здесь не ради числа, а ради ВИДИМОСТИ: распознаватель
	// читает литерал, поэтому полоса, вернувшаяся к сборке текста из частей, для
	// него просто исчезнет, и гейт замолчал бы, ничего не найдя. Снятие полосы —
	// законный исход, но он обязан быть РЕШЕНИЕМ: тогда правится и эта граница,
	// тем же изменением.
	require.GreaterOrEqual(t, len(found), 3,
		"полос отказа по %s найдено %d, а известно три (балансировщик, группа целей, "+
			"слушатель): либо полоса снята — тогда правь эту границу тем же изменением, "+
			"либо её текст перестал быть литералом и ушёл из-под наблюдения", scopeField, len(found))

	require.Empty(t, defects, "отказы об одном запрете расходятся тоном:\n%s",
		strings.Join(defects, "\n"))
}

// TestScopeToneGateCanFailAndCanStaySilent — доказательство способности гейта
// упасть и смолчать, инъекцией в обе стороны. Без него «ноль находок» неотличим
// от предиката, который не находит ничего никогда.
func TestScopeToneGateCanFailAndCanStaySilent(t *testing.T) {
	t.Parallel()

	t.Run("законный отказ молчит", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, scopeToneDefects([]scopeRefusal{{
			pos:  "synthetic:1",
			text: "project_id is immutable after TargetGroup.Create; use TargetGroupService.Move",
		}}))
	})

	t.Run("канонический тон без следующего шага — находка", func(t *testing.T) {
		t.Parallel()
		got := scopeToneDefects([]scopeRefusal{{
			pos:  "synthetic:2",
			text: "project_id is immutable after TargetGroup.Create",
		}})
		require.Len(t, got, 1)
		require.Contains(t, got[0], "synthetic:2", "находка обязана называть координату")
		require.Contains(t, got[0], "не называет следующего шага")
	})

	t.Run("следующий шаг без канонического тона — находка", func(t *testing.T) {
		t.Parallel()
		got := scopeToneDefects([]scopeRefusal{{
			pos:  "synthetic:3",
			text: "project_id is immutable; use NetworkLoadBalancerService.Move",
		}})
		require.Len(t, got, 1)
		require.Contains(t, got[0], "synthetic:3")
		require.Contains(t, got[0], "не несёт канонического тона")
	})

	// Склейка литералов читается ЦЕЛИКОМ. Без этой пробы распознаватель молча
	// вернулся бы к чтению первого слагаемого и объявлял бы находкой обрыв,
	// которого у клиента нет, — то есть краснел бы на верном продукте.
	t.Run("склейка литералов читается целиком", func(t *testing.T) {
		t.Parallel()
		fset := token.NewFileSet()
		src := "package p\n\nvar _ = \"project_id is immutable after Listener.Create; \" +\n" +
			"\t\"use NetworkLoadBalancerService.Move on the parent load balancer\"\n" +
			"\nvar _ = \"project_id is immutable \" + who\n"
		f, err := parser.ParseFile(fset, "synthetic.go", src, parser.SkipObjectResolution)
		require.NoError(t, err)

		var folded []string
		var partial int
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			if whole, ok := foldStringConcat(be); ok {
				folded = append(folded, whole)
			} else {
				partial++
			}
			return false
		})

		require.Equal(t, []string{
			"project_id is immutable after Listener.Create; " +
				"use NetworkLoadBalancerService.Move on the parent load balancer",
		}, folded)
		require.Empty(t, scopeToneDefects([]scopeRefusal{{pos: "synthetic:1", text: folded[0]}}),
			"склеенный законный отказ обязан молчать")
		require.Equal(t, 1, partial,
			"склейка с вычисляемой частью целиком не известна и известной не объявляется")
	})

	// Законный близнец на СТОРОНЕ ОТБОРА: отказ про соседнее неизменяемое поле
	// следующего шага не имеет — глагола переноса региона не существует, — и под
	// наблюдение попадать не должен. Без этой пробы гейт, начавший судить все
	// отказы неизменяемости подряд, краснел бы на верном продукте, и его сняли бы
	// первым.
	t.Run("отказ про соседнее поле под наблюдение не попадает", func(t *testing.T) {
		t.Parallel()
		require.False(t, isScopeRefusal("region_id is immutable after TargetGroup.Create"))
		require.False(t, isScopeRefusal("type is immutable after NetworkLoadBalancer.Create"))
		require.True(t, isScopeRefusal("project_id is immutable after Listener.Create"))
	})
}
