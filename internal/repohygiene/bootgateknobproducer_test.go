// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bootgateknobproducer_test.go — гейт на КЛАСС: у загрузочного гейта, который
// несёт сервис, обязан быть ПРОИЗВОДИТЕЛЬ ручки в его чарте.
//
// # Предмет
//
// `pkg/outbox/bootgate` — fail-closed гейт: пока путь доставки намерения о
// владении не поднят, мутирующий Create отвергается, а под остаётся NotReady.
// Включается он ОДНОЙ ручкой, и на выключенной ручке вся конструкция — no-op по
// построению: Ready() всегда true, GuardMutation() всегда nil.
//
// Значит сервис может нести гейт, покрыть его тестами, провязать в цепочку
// интерсепторов и в пробу готовности — и не исполнить ни разу за всю жизнь, если
// ручку не выставляет ни один профиль. Это класс «гейт, у входа которого нет
// производителя»: ветка написана, верна, покрыта тестами и мертва. Замер по
// дереву на 2026-08-09 (до этой правки): гейт несли три сервиса, производитель
// был у одного.
//
// # Почему гейт, а не три правки
//
// Сервисная проба про свой гейт не умеет заметить, что её сервис не включает его
// на стенде: она зовёт гейт напрямую, со своим значением ручки. Общего места, где
// видно «сколько сервисов несут гейт и у скольких он вооружён», в дереве не было —
// нужно именно оно, иначе следующий сервис заведёт четвёртую мёртвую ветку.
//
// # Что читается
//
// Перепись НЕСУЩИХ гейт — из дерева, а не выписана: разбор AST композиционных
// корней ищет `bootgate.New(...)`. Для каждого найденного сервиса читается ТЕКСТ
// шаблонов его чарта: производителем считается место, где ручка рендерится из
// `.Values.<путь>`, и этот путь обязан быть объявлен в `values.yaml` чарта.
// Литерал производителем НЕ считается — профиль не может его изменить; свободный
// проброс `range .Values.env` тоже, по той же границе, что в
// `services/compute/deploy/peer_mtls_producer_test.go`: first-class ручка, а не
// пассажирская мапа.
//
// Предпосылка проверяется: ноль найденных носителей — находка, а не «всё чисто».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// bootGateKnobPathMarkers — как ручка гейта опознаётся в ссылке чарта на своё
// значение. Опознаём по ПУТИ значения (`.Values.a.b.c`), а не по тексту строки:
// текст ловит слово «require» в сообщении `required` про строку подключения к
// БД, то есть чужую строку, и производитель у сервиса выглядел бы имеющимся.
//
// Два маркера, потому что два законных порядка слов, и оба стоят в дереве:
// `auth.requireIAM` / `config.fga.requireIam` — «ручка требования iam», а
// `iam.require` — «секция iam, ключ require». Смысл один; навязывать одно
// написание значило бы переименовывать ключ настроек ради удобства проверки.
var bootGateKnobPathMarkers = []string{"requireiam", "iamrequire"}

// bootGateNormalisePath приводит путь значения к сравнимому виду: только буквы и
// цифры в нижнем регистре, разделители отброшены.
func bootGateNormalisePath(path string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(path) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// valuesRef — ссылка на значение чарта в строке шаблона: `.Values.a.b.c`.
var valuesRef = regexp.MustCompile(`\.Values\.([A-Za-z0-9_.]+)`)

// bootGateCarriers — сервисы, чей композиционный корень конструирует гейт.
// Возвращает имя сервиса (каталог под services/) → файл корня.
func bootGateCarriers(t *testing.T) map[string]string {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix("../../services", ".go")
	if err != nil {
		t.Fatalf("enumerate service sources: %v", err)
	}
	// treecorpus отдаёт АБСОЛЮТНЫЕ пути, поэтому имя сервиса берётся относительно
	// корня перечисления, а не срезом префикса: срез молча дал бы ноль совпадений,
	// то есть «чистое дерево» на непрочитанном.
	root, err := filepath.Abs("../../services")
	if err != nil {
		t.Fatalf("resolve services root: %v", err)
	}
	carriers := map[string]string{}
	var scanned int
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			t.Fatalf("relativise %s: %v", path, rerr)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 || parts[1] != "cmd" {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "New" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "bootgate" {
				return true
			}
			carriers[parts[0]] = "services/" + filepath.ToSlash(rel)
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("no composition-root sources were read at all — the gate's premise is gone, " +
			"fix the gate rather than assume the tree is clean")
	}
	return carriers
}

// chartTemplates — тексты шаблонов чарта сервиса.
func chartTemplates(t *testing.T, svc string) map[string]string {
	t.Helper()
	dir := filepath.Join("../../services", svc, "deploy", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s: chart templates unreadable (%s): %v", svc, dir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".tpl") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			t.Fatalf("%s: read %s: %v", svc, name, rerr)
		}
		out[name] = string(raw)
	}
	if len(out) == 0 {
		t.Fatalf("%s: chart has no templates — this gate reads a chart that no longer exists there", svc)
	}
	return out
}

// declaredInValues сообщает, объявлен ли путь `.Values.a.b.c` в values.yaml чарта.
func declaredInValues(t *testing.T, svc, path string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../services", svc, "deploy", "values.yaml"))
	if err != nil {
		t.Fatalf("%s: read chart values: %v", svc, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("%s: parse chart values: %v", svc, err)
	}
	var cur any = tree
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur, ok = m[key]
		if !ok {
			return false
		}
	}
	return true
}

// TestEveryBootGateCarrierHasAKnobProducerInItsChart — ядро гейта.
func TestEveryBootGateCarrierHasAKnobProducerInItsChart(t *testing.T) {
	carriers := bootGateCarriers(t)
	if len(carriers) == 0 {
		t.Fatal("no service constructs bootgate.New — either the gate package was retired (then retire " +
			"this check with it) or this census stopped reading composition roots")
	}

	var report []string
	for _, svc := range bootGateSortedKeys(carriers) {
		templates := chartTemplates(t, svc)
		var producers []string
		for _, name := range bootGateSortedKeys(templates) {
			for _, line := range strings.Split(templates[name], "\n") {
				// Ручка, вписанная литералом, производителем не является: профиль
				// не может её изменить, а значит и вооружить гейт на конкретной
				// посадке нечем. Поэтому кандидатами считаются только ссылки на
				// значения чарта.
				ref := valuesRef.FindStringSubmatch(line)
				if ref == nil || !bootGateContainsAny(bootGateNormalisePath(ref[1]), bootGateKnobPathMarkers) {
					continue
				}
				if !declaredInValues(t, svc, ref[1]) {
					t.Errorf("%s: %s renders the boot-gate knob from .Values.%s, but the chart's "+
						"values.yaml declares no such key — the reference is dangling, so the knob "+
						"renders empty and the gate stays a no-op",
						svc, name, ref[1])
					continue
				}
				producers = append(producers, fmt.Sprintf("%s→.Values.%s", name, ref[1]))
			}
		}
		if len(producers) == 0 {
			t.Errorf("%s carries the fail-closed boot gate (%s) but no chart template produces its knob: "+
				"the gate is inert on every profile — Ready() is always true and GuardMutation() always "+
				"nil, so a resource can be created while the owner-tuple delivery path is down, which is "+
				"exactly what the gate exists to prevent",
				svc, carriers[svc])
			continue
		}
		report = append(report, fmt.Sprintf("%s: %s", svc, strings.Join(producers, ", ")))
	}
	t.Logf("examined %d boot-gate carrier(s): %s", len(carriers), strings.Join(report, "; "))
}

func bootGateSortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func bootGateContainsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
