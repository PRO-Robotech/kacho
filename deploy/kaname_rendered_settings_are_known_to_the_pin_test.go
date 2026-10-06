// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_rendered_settings_are_known_to_the_pin_test.go — КАЖДЫЙ КЛЮЧ, КОТОРЫЙ
// ЧАРТ ПИШЕТ В ФАЙЛ НАСТРОЕК СЛУЖБЫ ДОСТУПА, ЗНАЕТ ПИНЕННАЯ СЛУЖБА (kacho#2915).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба судит файл настроек СТРОГО: ключ, которого не знает её декодер, —
// отказ старта с полным путём ключа («служба этого ключа не читает — снимите
// его из файла настроек»). Тот же файл читает и мигратор, init-контейнер того
// же пода, — и отказывает первым, до всякого слушателя.
//
// Цена измерена: пин службы, снявший секцию `jobs.catalog-snapshot` (её поле
// не вело петлю ни дня и было снято с объявления), встретил чарт, который эту
// секцию рендерил безусловно. Init-контейнер `migrate` ушёл в CrashLoopBackOff
// на каждом стенде, все четыре шарда сквозного прогона исполнили ноль
// коллекций, а текст отказа в артефакт не попал — разбирать было нечего.
//
// ─────────────────────────────────────────────────────────────────────────────
// АВТОРИТЕТ КЛЮЧЕЙ — ПИНЕННЫЙ МОДУЛЬ, А НЕ ПЕРЕЧЕНЬ
//
// Множество законных путей выводится из исходников пиненного модуля (`go.mod`
// даёт версию, `go env GOMODCACHE` — каталог) разбором структуры `Config` по её
// разметке `mapstructure`, тем же правилом, каким её сопоставляет декодер
// службы: имя — из тега до запятой, поле без тега — имя поля в нижнем
// регистре, `squash` встраивает поля без сегмента, `-` и неэкспортируемые поля
// декодер не видит, вложенная структура пакета даёт сегмент пути. Выписанный
// здесь перечень разошёлся бы с пином молча — на той строке, которую забыли
// поправить при подъёме пина, то есть ровно на этом классе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ — НАЗВАНО, ЧТОБЫ НЕ ЧИТАЛИ ШИРЕ
//
//   - Поле, чей тип объявлен ВНЕ пакета настроек (кроме `time.Duration`),
//     отображение и интерфейс — ОТКРЫТЫ для этой проверки: подключи под ними не
//     судятся. Их число печатается переписью, поэтому «не судилось» отличимо от
//     «судилось и сошлось».
//   - Элементы списков не судятся: список для декодера — одно значение.
//   - ГОДНОСТЬ величин не судится — её судит страж старта службы; доставку
//     обязательных величин держит TestPinnedRequiredSettingsReachTheServiceOnEveryStack.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"gopkg.in/yaml.v3"
)

// settingNode — узел дерева ключей декодера: структура (есть поля), лист или
// открытая форма.
type settingNode struct {
	fields map[string]*settingNode // структура пакета настроек
	open   bool                    // подключи не судятся
}

// settingSchema — разобранное дерево ключей и перепись разбора.
type settingSchema struct {
	root    *settingNode
	structs int // структур пакета, вошедших в дерево
}

// parseSettingSchema строит дерево ключей декодера из исходников пакета
// настроек: srcs — имя файла → текст. Корень — тип rootType.
func parseSettingSchema(srcs map[string]string, rootType string) (settingSchema, error) {
	fset := token.NewFileSet()
	types := map[string]ast.Expr{}
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return settingSchema{}, fmt.Errorf("исходник пакета настроек не разобран (%s): %w", name, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				types[ts.Name.Name] = ts.Type
			}
		}
	}
	rootExpr, ok := types[rootType]
	if !ok {
		return settingSchema{}, fmt.Errorf("тип %s в пакете настроек не найден — разбирать нечего", rootType)
	}
	b := schemaBuilder{types: types, seen: map[string]bool{}}
	root := b.node(rootExpr, 0)
	if root.fields == nil || len(root.fields) == 0 {
		return settingSchema{}, fmt.Errorf("у типа %s не выведено ни одного ключа — форма объявления сменилась", rootType)
	}
	return settingSchema{root: root, structs: len(b.seen)}, nil
}

type schemaBuilder struct {
	types map[string]ast.Expr
	seen  map[string]bool
}

// node — узел для выражения типа поля.
func (b *schemaBuilder) node(expr ast.Expr, depth int) *settingNode {
	if depth > 32 {
		return &settingNode{open: true}
	}
	switch e := expr.(type) {
	case *ast.StarExpr:
		return b.node(e.X, depth+1)
	case *ast.ParenExpr:
		return b.node(e.X, depth+1)
	case *ast.StructType:
		n := &settingNode{fields: map[string]*settingNode{}}
		b.addFields(n, e, depth)
		return n
	case *ast.Ident:
		under, local := b.types[e.Name]
		if !local {
			return &settingNode{} // встроенный тип — лист
		}
		if _, isStruct := under.(*ast.StructType); isStruct {
			b.seen[e.Name] = true
		}
		return b.node(under, depth+1)
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok && pkg.Name == "time" && e.Sel.Name == "Duration" {
			return &settingNode{}
		}
		return &settingNode{open: true} // тип чужого пакета — не судится
	case *ast.MapType, *ast.InterfaceType:
		return &settingNode{open: true}
	case *ast.ArrayType:
		return &settingNode{} // список — одно значение для декодера
	default:
		return &settingNode{open: true}
	}
}

func (b *schemaBuilder) addFields(n *settingNode, st *ast.StructType, depth int) {
	for _, f := range st.Fields.List {
		tag := ""
		if f.Tag != nil {
			if raw, err := strconv.Unquote(f.Tag.Value); err == nil {
				tag = reflect.StructTag(raw).Get("mapstructure")
			}
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		squash := false
		for _, o := range strings.Split(opts, ",") {
			if strings.TrimSpace(o) == "squash" {
				squash = true
			}
		}
		child := b.node(f.Type, depth+1)
		if len(f.Names) == 0 { // встроенное поле
			typeName := embeddedName(f.Type)
			if !isExported(typeName) {
				continue
			}
			if name == "" && squash && child.fields != nil {
				for k, v := range child.fields {
					n.fields[k] = v
				}
				continue
			}
			if name == "" {
				name = strings.ToLower(typeName)
			}
			n.fields[name] = child
			continue
		}
		for _, id := range f.Names {
			if !id.IsExported() {
				continue
			}
			key := name
			if key == "" && squash && child.fields != nil {
				for k, v := range child.fields {
					n.fields[k] = v
				}
				continue
			}
			if key == "" {
				key = strings.ToLower(id.Name)
			}
			n.fields[key] = child
		}
	}
}

func embeddedName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return embeddedName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

func isExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

// settingsCensus — перепись суда одного файла настроек.
type settingsCensus struct {
	judged int // ключей, сверенных с деревом
	open   int // ключей под открытой формой — не судились
}

// judgeRenderedSettings — пути файла настроек, которых декодер не знает.
func judgeRenderedSettings(schema *settingNode, cfg map[string]any) ([]string, settingsCensus) {
	var unknown []string
	var c settingsCensus
	var walk func(node *settingNode, tree map[string]any, prefix string)
	walk = func(node *settingNode, tree map[string]any, prefix string) {
		for k, v := range tree {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			child, ok := node.fields[k]
			if !ok {
				unknown = append(unknown, path)
				continue
			}
			c.judged++
			if child.open {
				c.open++
				continue
			}
			if sub, isMap := v.(map[string]any); isMap && child.fields != nil {
				walk(child, sub, path)
			}
		}
	}
	walk(schema, cfg, "")
	sort.Strings(unknown)
	return unknown, c
}

// pinnedSettingSchema — дерево ключей декодера ПИНЕННОЙ службы.
func pinnedSettingSchema(t *testing.T) settingSchema {
	t.Helper()
	dir := filepath.Join(kanameModuleDir(t, ".."), "internal", "apps", "kaname", "config")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("пакет настроек пиненной службы не прочитан (%s): %v", dir, err)
	}
	srcs := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, n)) // #nosec G304 -- путь собран из пина go.mod, не из ввода
		if err != nil {
			t.Fatalf("исходник пакета настроек не прочитан (%s): %v", n, err)
		}
		srcs[n] = string(body)
	}
	if len(srcs) == 0 {
		t.Fatalf("в пакете настроек пиненной службы ни одного исходника (%s) — вердикт был бы беспредметен", dir)
	}
	schema, err := parseSettingSchema(srcs, "Config")
	if err != nil {
		t.Fatalf("дерево ключей пиненной службы не выведено (%s): %v", dir, err)
	}
	return schema
}

// TestRenderedServiceSettingsAreKnownToThePinOnEveryStack — суд по всем стекам.
func TestRenderedServiceSettingsAreKnownToThePinOnEveryStack(t *testing.T) {
	schema := pinnedSettingSchema(t)
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var census []string
	for _, name := range names {
		values := stackIdentityValues(t, stacks[name])
		body, err := yaml.Marshal(values)
		if err != nil {
			t.Fatalf("стек %s: значения подчарта не сериализуются: %v", name, err)
		}
		file := filepath.Join(t.TempDir(), name+".yaml")
		if err := os.WriteFile(file, body, 0o600); err != nil {
			t.Fatal(err)
		}
		base, err := filepath.Abs(umbrellaDir)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(base, file)
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := renderIdentitySubchart(t, []string{rel})
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы не удался: %v\n%s", name, err, rendered)
		}
		cfg := kanameServiceConfig(t, rendered)
		unknown, c := judgeRenderedSettings(schema.root, cfg)
		for _, k := range unknown {
			t.Errorf("стек %s: чарт пишет ключ `%s`, которого пиненная служба не знает — "+
				"строгий загрузчик откажет в старте и мигратору, и службе "+
				"(«служба этого ключа не читает — снимите его из файла настроек»)", name, k)
		}
		if c.judged == 0 {
			t.Errorf("стек %s: ни одного ключа не сверено — файл настроек не найден в рендере", name)
		}
		census = append(census, fmt.Sprintf("%s: сверено %d, из них под открытой формой %d, неизвестных %d",
			name, c.judged, c.open, len(unknown)))
	}
	if len(census) == 0 {
		t.Fatal("ни одного стека не осмотрено — вердикта нет")
	}
	t.Logf("перепись: пин %s · структур пакета в дереве %d · ключей верхнего уровня %d · стеков %d\n  %s",
		productModulePins(t, "..")[kanameModulePart], schema.structs, len(schema.root.fields),
		len(census), strings.Join(census, "\n  "))
}

// TestRenderedSettingsJudgeSeesAnUnknownKeyAndIsSilentOnAKnownOne — самопроверка
// разбора и суда на синтетике: тот же файл, в котором меняется один факт.
func TestRenderedSettingsJudgeSeesAnUnknownKeyAndIsSilentOnAKnownOne(t *testing.T) {
	src := `package config

import (
	"time"

	"example.invalid/grpcsrv"
)

type Config struct {
	Jobs     JobsConfig            ` + "`mapstructure:\"jobs\"`" + `
	Logger   *LoggerConfig         ` + "`mapstructure:\"logger\"`" + `
	Admit    grpcsrv.AdmissionKnobs ` + "`mapstructure:\"admission\"`" + `
	Labels   map[string]string     ` + "`mapstructure:\"labels\"`" + `
	Hidden   string                ` + "`mapstructure:\"-\"`" + `
	Plain    string
	Shared                         ` + "`mapstructure:\",squash\"`" + `
	internal string
}

type Shared struct {
	Region string ` + "`mapstructure:\"region\"`" + `
}

type JobsConfig struct {
	Reclaim ReclaimConfig ` + "`mapstructure:\"expired-credential-reclaim\"`" + `
}

type ReclaimConfig struct {
	Enabled  bool          ` + "`mapstructure:\"enabled\"`" + `
	Interval time.Duration ` + "`mapstructure:\"interval\"`" + `
}

type LoggerConfig struct {
	Level Level ` + "`mapstructure:\"level\"`" + `
}

type Level string
`
	schema, err := parseSettingSchema(map[string]string{"config.go": src}, "Config")
	if err != nil {
		t.Fatalf("законный пакет обязан разбираться: %v", err)
	}

	known := map[string]any{
		"jobs":      map[string]any{"expired-credential-reclaim": map[string]any{"enabled": true, "interval": "1h"}},
		"logger":    map[string]any{"level": "info"},
		"admission": map[string]any{"anything": map[string]any{"below": 1}},
		"labels":    map[string]any{"free": "form"},
		"plain":     "x",
		"region":    "r1",
	}
	if unknown, c := judgeRenderedSettings(schema.root, known); len(unknown) != 0 || c.open != 2 {
		t.Errorf("законный файл: неизвестных %v (ждали 0), под открытой формой %d (ждали 2)", unknown, c.open)
	}

	// Близнец: тот же файл плюс снятая секция — ровно одна находка с полным путём.
	retired := map[string]any{}
	for k, v := range known {
		retired[k] = v
	}
	retired["jobs"] = map[string]any{
		"expired-credential-reclaim": map[string]any{"enabled": true},
		"catalog-snapshot":           map[string]any{"refresh-interval": "1m"},
	}
	if unknown, _ := judgeRenderedSettings(schema.root, retired); len(unknown) != 1 || unknown[0] != "jobs.catalog-snapshot" {
		t.Errorf("снятая секция обязана быть ровно одной находкой `jobs.catalog-snapshot`, получено %v", unknown)
	}

	// Ключи, которых декодер не видит: `-`, неэкспортируемое поле, имя встроенной
	// при squash структуры — находки.
	for _, k := range []string{"hidden", "internal", "shared"} {
		if unknown, _ := judgeRenderedSettings(schema.root, map[string]any{k: "x"}); len(unknown) != 1 {
			t.Errorf("ключ %q декодер не видит — обязан быть находкой, получено %v", k, unknown)
		}
	}

	// Отказы разбора называются.
	if _, err := parseSettingSchema(map[string]string{"a.go": "package config\ntype Other struct{}\n"}, "Config"); err == nil ||
		!strings.Contains(err.Error(), "не найден") {
		t.Errorf("пакет без корня обязан отказывать, получено %v", err)
	}
	if _, err := parseSettingSchema(map[string]string{"a.go": "package config\ntype Config struct{}\n"}, "Config"); err == nil ||
		!strings.Contains(err.Error(), "ни одного ключа") {
		t.Errorf("пустой корень обязан отказывать, получено %v", err)
	}
}
