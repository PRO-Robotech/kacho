// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// labelverdictaxis_test.go — у КАЖДОГО типа, объявленного выбираемым по меткам,
// есть ось, по которой меточная ветвь вердикта отвечает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЛОВИТ
//
// Метки объекта лежат в двух разных местах, и место — свойство типа: у ресурса
// чужого сервиса это зеркало ресурсов, у собственного объекта iam — его
// собственная таблица (зеркало таких объектов не держит НИКОГДА, и это
// осознанное решение, а не пробел). Ветвь меток, написанная под одно место,
// становится на другом семействе типов ТОЖДЕСТВЕННО ЛОЖНОЙ: соединение пусто,
// условие не выполняется ни при каких данных, а ответ «нет» неотличим от
// честного отказа по правам. Наблюдаемого симптома у такого состояния нет —
// именно поэтому нужен гейт, а не проба.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ПЕРЕЧЕНЬ ПРОБ
//
// Пробы утверждают свойство семи типов, которые есть СЕГОДНЯ. Свойство, которое
// требуется, — про тип, которого ещё нет: следующий собственный тип iam,
// объявленный выбираемым по меткам, обязан покраснеть сам, без того чтобы кто-то
// вспомнил дописать ему пробу и назначить ось.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКИ ГЕЙТА ПРОВЕРЯЕТ ОН САМ
//
// Гейт стоит на трёх фактах о дереве: перечень выбираемых по меткам типов лежит
// там-то, соответствие «точечное имя ↔ имя модели» там-то, реестр осей там-то.
// Любой из трёх может переехать или переименоваться — и тогда запрет молча
// перестанет что-либо запрещать. Поэтому каждая перепись обязана быть НЕПУСТОЙ и
// обязана находить ОБЕ стороны разбиения (и собственные типы iam, и зеркальные):
// предикат, находящий одну сторону, доказывает половину и зеленеет на другой.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Координаты трёх переписей. Выписаны, потому что гейт обязан отказать, если
// любая из них перестала существовать: молчание из-за переехавшего файла
// неотличимо от молчания из-за исправного дерева.
var (
	feedRegistryPath = filepath.Join("services", "iam", "internal", "domain", "feed_registry.go")
	// `objectTypes` ПОРОЖДАЕТСЯ из манифестов модулей и лежит в порождённом
	// файле (#1092): здесь его больше нет, и координата переехала вместе с ним.
	// Гейт читает продукт, а не манифесты, намеренно — предмет у него «согласны
	// ли три переписи ДЕРЕВА», и разбор манифеста завёл бы вторую форму их
	// чтения. Читать текст порождённого файла безопасно: его свежесть сверяется
	// побайтово своим гейтом, поэтому «текст отстал от манифеста» здесь
	// невыразимо.
	fgaTypesPath  = filepath.Join("services", "iam", "internal", "authzmap", "tables_gen.go")
	labelAxisPath = filepath.Join("services", "iam", "internal", "repo", "kacho", "pg",
		"relverdict", "labelaxis.go")
	// verdictFormFiles — четыре вопроса формы E. Каждый строит СВОЙ запрос,
	// поэтому ось обязан подставлять каждый: реестр, на который смотрит один
	// запрос из четырёх, оставляет три с прежним соединением.
	verdictFormFiles = []string{"query.go", "list.go", "subjects.go", "expand.go"}
	verdictFormDir   = filepath.Join("services", "iam", "internal", "repo", "kacho", "pg", "relverdict")
)

// iamDirectPrefix — по этому признаку тип относится к совпадающим по СВОЕЙ
// таблице. Признак не выдуман здесь: это то же правило, которым классифицирует
// домен (`FeedSourceForType`). Второй перечень был бы вторым местом об одном
// предмете.
const iamDirectPrefix = "iam."

// TestEveryLabelSelectableTypeHasAVerdictAxis — гейт.
func TestEveryLabelSelectableTypeHasAVerdictAxis(t *testing.T) {
	root := repoRoot(t)

	selectable := mapKeysOfVar(t, filepath.Join(root, feedRegistryPath), "labelSelectableTypes")
	dottedToFGA := mapPairsOfVar(t, filepath.Join(root, fgaTypesPath), "objectTypes")
	axis := mapPairsOfVar(t, filepath.Join(root, labelAxisPath), "iamDirectLabelTable")

	// Предпосылка 1: переписи непусты. Пустая означает переехавший или
	// переименованный источник, и вердикт «находок нет» был бы о нечитанном.
	if len(selectable) == 0 {
		t.Fatalf("предпосылка гейта нарушена: в %s не найдено ни одного выбираемого по "+
			"меткам типа — перечень переехал или переименован", feedRegistryPath)
	}
	if len(dottedToFGA) == 0 {
		t.Fatalf("предпосылка гейта нарушена: в %s не найдено соответствия имён типов",
			fgaTypesPath)
	}

	// Предпосылка 2: разбиение находит ОБЕ стороны. Предикат, у которого одна
	// сторона пуста, не разбиение, а совпадение — и он зеленел бы на любой оси.
	var iamDirect, mirrorFed []string
	for _, ty := range selectable {
		if strings.HasPrefix(ty, iamDirectPrefix) {
			iamDirect = append(iamDirect, ty)
			continue
		}
		mirrorFed = append(mirrorFed, ty)
	}
	sort.Strings(iamDirect)
	sort.Strings(mirrorFed)
	if len(iamDirect) == 0 || len(mirrorFed) == 0 {
		t.Fatalf("предпосылка гейта нарушена: разбиение нашло одну сторону из двух "+
			"(собственных типов iam %d, зеркальных %d) — такой предикат ничего не различает",
			len(iamDirect), len(mirrorFed))
	}

	forms, formsRead := verdictFormsSubstitutingAxis(t, filepath.Join(root, verdictFormDir))

	t.Logf("осмотрено: выбираемых по меткам типов %d (собственных iam %d, зеркальных %d); "+
		"записей соответствия имён %d; записей реестра осей %d; вопросов формы E прочитано %d",
		len(selectable), len(iamDirect), len(mirrorFed), len(dottedToFGA), len(axis), formsRead)

	var findings []string

	// (1) Каждому собственному типу iam назначена ось, и она — непустая таблица.
	for _, dotted := range iamDirect {
		fga, ok := dottedToFGA[dotted]
		if !ok {
			findings = append(findings, fmt.Sprintf(
				"%s: тип выбираем по меткам, но в %s ему не сопоставлено имя модели — "+
					"вопрос о доступе назовёт его именем, которого реестр осей не знает",
				dotted, fgaTypesPath))
			continue
		}
		table, ok := axis[fga]
		if !ok {
			findings = append(findings, fmt.Sprintf(
				"%s (%s): ось меточной ветви не назначена в %s — на этом типе условие "+
					"меток не выполнится НИ ПРИ КАКИХ данных, и отказ будет неотличим от "+
					"честного «прав нет»", dotted, fga, labelAxisPath))
			continue
		}
		if strings.TrimSpace(table) == "" {
			findings = append(findings, fmt.Sprintf(
				"%s (%s): ось назначена пустой таблицей — записи нечего выбирать", dotted, fga))
		}
	}

	// (2) Зеркальные типы оси собственных таблиц НЕ получают: их метки живут в
	// зеркале, и запись здесь увела бы вопрос в таблицу, которой у чужого
	// ресурса нет вовсе.
	for _, dotted := range mirrorFed {
		fga, ok := dottedToFGA[dotted]
		if !ok {
			continue
		}
		if _, ok := axis[fga]; ok {
			findings = append(findings, fmt.Sprintf(
				"%s (%s): тип питается зеркалом, но в %s ему назначена собственная таблица "+
					"iam — вопрос уйдёт туда, где этого объекта не бывает", dotted, fga, labelAxisPath))
		}
	}

	// (3) Реестр обязан быть ПРОЧИТАН каждым из четырёх вопросов. Реестр, на
	// который никто не смотрит, — объявление, а не ось.
	for _, f := range verdictFormFiles {
		if !forms[f] {
			findings = append(findings, fmt.Sprintf(
				"%s: вопрос формы E не подставляет ось меток — его запрос остался с одним "+
					"местом хранения меток на все типы", filepath.Join(verdictFormDir, f)))
		}
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("ось меточной ветви не назначена или не читается в %d месте(ах):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// verdictFormsSubstitutingAxis отвечает, какие из вопросов формы E подставляют
// ось, и сколько файлов при этом прочитано.
//
// Признак — ВЫЗОВ строителя фрагмента (`labelsJoinPinned` /
// `labelsJoinPerCandidate` / `candidateFrom`), а не упоминание слова: слово
// нашлось бы в комментарии, объясняющем эту же подстановку, и гейт остался бы
// зелёным на запросе, где подстановки нет.
func verdictFormsSubstitutingAxis(t *testing.T, dir string) (map[string]bool, int) {
	t.Helper()
	builders := map[string]bool{
		"labelsJoinPinned":       true,
		"labelsJoinPerCandidate": true,
		"candidateFrom":          true,
	}
	out := make(map[string]bool, len(verdictFormFiles))
	read := 0
	for _, name := range verdictFormFiles {
		abs := filepath.Join(dir, name)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, abs, nil, 0)
		if err != nil {
			t.Fatalf("предпосылка гейта нарушена: %s не разбирается: %v", abs, err)
		}
		read++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if builders[id.Name] {
				out[name] = true
			}
			return true
		})
	}
	return out, read
}

// mapKeysOfVar отдаёт строковые КЛЮЧИ литерала карты, объявленной переменной
// name в файле path.
func mapKeysOfVar(t *testing.T, path, name string) []string {
	t.Helper()
	lit := mapLiteralOfVar(t, path, name)
	out := make([]string, 0, len(lit.Elts))
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k := stringLit(kv.Key); k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// mapPairsOfVar отдаёт пары «строковый ключ → строковое значение» литерала карты.
func mapPairsOfVar(t *testing.T, path, name string) map[string]string {
	t.Helper()
	lit := mapLiteralOfVar(t, path, name)
	out := make(map[string]string, len(lit.Elts))
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, v := stringLit(kv.Key), stringLit(kv.Value)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// mapLiteralOfVar находит литерал карты у объявления переменной. Отсутствие —
// ОТКАЗ, а не пустой результат: молчание гейта из-за переименованной переменной
// неотличимо от молчания из-за исправного дерева.
func mapLiteralOfVar(t *testing.T, path, name string) *ast.CompositeLit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("предпосылка гейта нарушена: %s не разбирается: %v", path, err)
	}
	var lit *ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, id := range vs.Names {
			if id.Name != name || i >= len(vs.Values) {
				continue
			}
			if cl, ok := vs.Values[i].(*ast.CompositeLit); ok {
				lit = cl
			}
		}
		return true
	})
	if lit == nil {
		t.Fatalf("предпосылка гейта нарушена: в %s нет литерала карты %s — переменная "+
			"переименована или переехала, и гейт молчал бы не потому, что дерево исправно",
			path, name)
	}
	return lit
}
