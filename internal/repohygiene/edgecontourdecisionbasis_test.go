// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edgecontourdecisionbasis_test.go — основание, на котором стоит решение
// оставить КРАЙ вне контура носителя.
//
// # Зачем отдельная проба
//
// Запись в реестре гейта усыновления говорит: край не переводится, потому что он
// прокси, а носителю прокси-форма невыразима. Это утверждение О НОСИТЕЛЕ, и оно
// может стать ложным без единой правки края: достаточно, чтобы у дескриптора
// появилось поле обработчика неизвестной службы или мультиплексора — и запись
// останется стоять, объявляя невозможным то, что стало возможным.
//
// Поэтому основание пинится ЗДЕСЬ и истекает от ВНЕШНЕГО факта: появления такого
// поля. Тем же порядком, каким основание решения по владельцу модели держится
// пробой на его пять рубежей.
//
// # Что проба НЕ утверждает
//
// Она не говорит, что край нельзя перевести никогда, и не оправдывает объём его
// собственной сборки. Она отвечает на один вопрос: выразима ли форма края
// дескриптором СЕГОДНЯ. Станет выразима — проба покраснеет и потребует пересмотра
// записи, а не молча переживёт свой предмет.
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
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// proxyShapedFields — имена полей дескриптора, появление которых означало бы,
// что прокси-форма стала выразимой объявлением.
//
// Ключ — подстрока имени поля в нижнем регистре; значение — что именно она
// сделала бы возможным. Совпадение по подстроке намеренно: поле, названное
// `UnknownServiceHandler` или `UnknownHandler`, — один и тот же предмет.
var proxyShapedFields = map[string]string{
	"unknownservice": "обработчик неизвестной службы: сервис смог бы принести СВОЮ маршрутизацию " +
		"чужих методов, и «набор служб объявлен» перестало бы быть свойством контура",
	"unknownhandler": "то же под другим именем",
	"cmux":           "мультиплексор: два протокола на одном слушателе — форма края, не сервиса",
	"multiplex":      "то же под общим именем",
}

// auditProxyShapedFields — обход дескриптора: поля, которыми прокси-форма
// выражалась бы объявлением.
//
// Принимает КОРЕНЬ и возвращает ошибку вместо того чтобы ронять прогон изнутри:
// иначе пробу нельзя прогнать на синтетическом дескрипторе, то есть нельзя
// доказать инъекцией — ни что она краснеет на появившемся поле, ни что она
// молчит на законном поле той же формы.
func auditProxyShapedFields(root string) (found []string, files, fields int, err error) {
	dir := filepath.Join(root, "pkg", "servicecontract")
	tracked, terr := treecorpus.Under(dir)
	if terr != nil {
		return nil, 0, 0, fmt.Errorf("состав пакета дескриптора не читается: %w", terr)
	}

	fset := token.NewFileSet()
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, 0, 0, fmt.Errorf("разбор %s: %w", abs, perr)
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, fld := range st.Fields.List {
				for _, name := range fld.Names {
					fields++
					low := strings.ToLower(name.Name)
					for needle, what := range proxyShapedFields {
						if strings.Contains(low, needle) {
							pos := fset.Position(name.Pos())
							rel, _ := filepath.Rel(root, abs)
							found = append(found, fmt.Sprintf("%s:%d %s — %s",
								filepath.ToSlash(rel), pos.Line, name.Name, what))
						}
					}
				}
			}
			return true
		})
	}
	sort.Strings(found)
	return found, files, fields, nil
}

// auditCarrierUnknownServiceUse — обращения носителя к обработчику неизвестной
// службы.
func auditCarrierUnknownServiceUse(root string) (uses []string, err error) {
	path := filepath.Join(root, "pkg", "servicehost", "serve.go")
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if perr != nil {
		return nil, fmt.Errorf("разбор носителя: %w", perr)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if strings.Contains(strings.ToLower(sel.Sel.Name), "unknownservice") {
			pos := fset.Position(sel.Pos())
			uses = append(uses, fmt.Sprintf("serve.go:%d %s", pos.Line, sel.Sel.Name))
		}
		return true
	})
	sort.Strings(uses)
	return uses, nil
}

// TestCarrierStillCannotExpressAProxyEdge — у дескриптора нет полей, которыми
// прокси-форма выражалась бы объявлением.
func TestCarrierStillCannotExpressAProxyEdge(t *testing.T) {
	ex, declared := hostAdoptionExceptions[edgeService]
	if !declared || ex.kind != adoptionDecided {
		t.Skip("край больше не объявлен решением владельца — основание держать не за что; " +
			"предмет этой пробы задаёт запись в servicehostadoption_test.go")
	}
	if strings.TrimSpace(ex.basis) == "" {
		t.Fatal("запись-решение обязана нести основание: без него нечего пинить, и самоистечение " +
			"по внешнему факту становится объявлением без предмета")
	}

	root := repoRoot(t)
	found, files, fields, err := auditProxyShapedFields(root)
	if err != nil {
		t.Fatalf("%v", err)
	}

	t.Logf("перепись: файлов дескриптора прочитано %d, полей структур осмотрено %d, "+
		"словарь прокси-форм %d записей, совпадений %d",
		files, fields, len(proxyShapedFields), len(found))

	if files == 0 || fields == 0 {
		t.Fatal("пакет дескриптора не прочитан — «совпадений нет» здесь означало бы «ничего не " +
			"читал», а не невыразимость прокси-формы")
	}

	for _, f := range found {
		t.Errorf("основание записи о крае истекло: у дескриптора появилось поле, которым "+
			"прокси-форма выражается объявлением — %s.\n"+
			"Значит «носителю край невыразим» больше не факт. Пересмотри запись %q в "+
			"hostAdoptionExceptions: либо переводи край, либо назови НОВОЕ основание.",
			f, edgeService)
	}
}

// TestCarrierRegistersOnlyDeclaredServices — вторая половина того же основания:
// службы попадают в слушатель ТОЛЬКО через переданные регистраторы.
//
// Без неё первая проба закрывала бы лишь одну дверь: поля нет, но если бы носитель
// сам дотягивался до маршрутизации чужих методов, форма края была бы выразима
// мимо дескриптора.
func TestCarrierRegistersOnlyDeclaredServices(t *testing.T) {
	uses, err := auditCarrierUnknownServiceUse(repoRoot(t))
	if err != nil {
		t.Fatalf("%v", err)
	}

	t.Logf("перепись: осмотрен pkg/servicehost/serve.go, обращений к обработчику неизвестной "+
		"службы %d", len(uses))

	for _, u := range uses {
		t.Errorf("носитель обращается к обработчику неизвестной службы (%s) — значит он умеет "+
			"маршрутизировать чужие методы, и основание записи о крае («форма невыразима») "+
			"перестало быть верным", u)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ функцию, что и пробы по дереву
//
// Основание держится утверждением «такого поля НЕТ», а отрицание без парного
// положительного неотличимо от сломанного распознавания: проба, которая не
// узнаёт поле, когда оно есть, молчит ровно так же, как проба на дереве, где
// его нет.

// synthContractTree — синтетический дескриптор и носитель, видимые индексу.
func synthContractTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

// Сторона дефекта: у дескриптора появилось поле прокси-формы.
func TestProxyShapeProbeRedOnAFieldThatExpressesIt(t *testing.T) {
	root := synthContractTree(t, map[string]string{
		"pkg/servicecontract/spec.go": `package servicecontract

type Spec struct {
	Registrars            []func()
	UnknownServiceHandler func()
}
`,
	})
	found, files, fields, err := auditProxyShapedFields(root)
	if err != nil {
		t.Fatalf("обход синтетического дескриптора: %v", err)
	}
	if files == 0 || fields == 0 {
		t.Fatalf("синтетический дескриптор не прочитан (файлов %d, полей %d)", files, fields)
	}
	if len(found) != 1 || !strings.Contains(found[0], "UnknownServiceHandler") {
		t.Fatalf("поле прокси-формы не поймано: %v", found)
	}
}

// Законный близнец той же формы: поле есть, структура та же, имя не из словаря.
//
// Без этой половины проба ловила бы «у дескриптора есть поля», а не «появилось
// поле прокси-формы», и основание решения краснело бы на каждой правке пакета.
func TestProxyShapeProbeSilentOnALawfulField(t *testing.T) {
	root := synthContractTree(t, map[string]string{
		"pkg/servicecontract/spec.go": `package servicecontract

type Spec struct {
	Registrars  []func()
	CacheWindow int
}
`,
	})
	found, files, fields, err := auditProxyShapedFields(root)
	if err != nil {
		t.Fatalf("обход синтетического дескриптора: %v", err)
	}
	if files == 0 || fields == 0 {
		t.Fatalf("синтетический дескриптор не прочитан (файлов %d, полей %d)", files, fields)
	}
	if len(found) != 0 {
		t.Fatalf("законное поле объявлено прокси-формой: %v — проба ловит форму, а не существо", found)
	}
}

// Сторона дефекта второй пробы: носитель сам дотянулся до маршрутизации чужих
// методов.
func TestCarrierUnknownServiceProbeRedOnAReach(t *testing.T) {
	root := synthContractTree(t, map[string]string{
		"pkg/servicehost/serve.go": `package servicehost

func Serve(h any) any { return grpc.UnknownServiceHandler(h) }
`,
	})
	uses, err := auditCarrierUnknownServiceUse(root)
	if err != nil {
		t.Fatalf("разбор синтетического носителя: %v", err)
	}
	if len(uses) != 1 || !strings.Contains(uses[0], "UnknownServiceHandler") {
		t.Fatalf("обращение к обработчику неизвестной службы не поймано: %v", uses)
	}
}

// Законный близнец: носитель регистрирует ОБЪЯВЛЕННЫЕ службы и до чужих методов
// не дотягивается.
func TestCarrierUnknownServiceProbeSilentOnDeclaredRegistration(t *testing.T) {
	root := synthContractTree(t, map[string]string{
		"pkg/servicehost/serve.go": `package servicehost

func Serve(rs []func(any)) any {
	s := grpc.NewServer()
	for _, r := range rs {
		r(s)
	}
	return s
}
`,
	})
	uses, err := auditCarrierUnknownServiceUse(root)
	if err != nil {
		t.Fatalf("разбор синтетического носителя: %v", err)
	}
	if len(uses) != 0 {
		t.Fatalf("объявленная регистрация засчитана маршрутизацией чужих методов: %v", uses)
	}
}
