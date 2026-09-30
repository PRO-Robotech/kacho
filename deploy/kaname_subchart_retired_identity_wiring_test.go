// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_subchart_retired_identity_wiring_test.go — ПОДЧАРТ СЛУЖБЫ ДОСТУПА НЕ
// ПЕРЕДАЁТ ПРОЦЕССУ НИ ПРОВЯЗКИ ПРЕЖНЕГО ПОСТАВЩИКА ЛИЧНОСТИ, НИ ТОГО, ЧТО ПИН
// ОТВЕРГАЕТ ИЛИ ПЕРЕСТАЛ ЧИТАТЬ (kacho#2818).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба доступа с kaname#363 держит одну посадку личности и отвергает ключ
// посадки при любом значении — ключом файла настроек и переменной окружения
// (перечень `retiredSettings` пиненного модуля,
// `internal/apps/kaname/config/retired_settings.go`). Слушателя обратных
// вызовов прежнего поставщика у неё больше нет. Подчарт, передающий процессу
// снятый ключ, роняет старт на каждом стенде; подчарт, передающий непрочитанную
// полосу хуков, показывает оператору значение, которое ничего не меняет.
//
// Судятся три утверждения, каждое на рендере подчарта значениями КАЖДОГО стека
// deploy/stacks.txt (секции `kaname` и `global` зонта, слитые так, как их сливает
// helm):
//
//  1. ни один объект подчарта не несёт имени поставщика — видом, именем, меткой,
//     ключом данных, именем контейнера, образом, томом, портом, переменной
//     окружения, ключом файла настроек службы. Кроме стеков судятся умолчания
//     подчарта и умолчания с поднятыми прежними выключателями его карт
//     (`kratos.config.enabled`, `kratos.identitySchema.enabled`): карта, которую
//     профиль поднимает одной ручкой, — ещё провязка, даже если ни один стек её
//     сегодня не поднимает;
//  2. ни одна настройка перечня снятых у ПИНЕННОГО модуля не доезжает до
//     процесса — ни ключом файла настроек (судится присутствие ключа, а не
//     значение: пустое и `null` — тоже объявление), ни переменной окружения;
//  3. полоса хуков (переменные `KANAME_HOOK_TOKEN`, `KANAME_HOOKS_SERVER_*`, порт
//     `http-hooks` пода и внутреннего Service) не передаётся, когда пин её не
//     читает. Пока читает — утверждение не судит и говорит это переписью.
//
// И отказ самого чарта: профиль, всё ещё объявляющий
// `config.authn.identityProvider`, получает отказ рендера с именем ключа, а не
// молча принятое значение, — та же дисциплина, что у стража снятых ключей в
// службе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКИ — ОТКАЗОМ, А НЕ «НАХОДОК НОЛЬ»
//
// Перечень снятых у пина не найден, не разобран или пуст; рендер без пода
// службы или без её карты настроек; ни одного осмотренного рендера. Каждый из
// этих исходов значил бы «смотреть было не на что», а не «провязки нет».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она не судит подчарты поставщика и профили зонта, которые их настраивают, —
// их снятие отдельный предмет (kacho#1276). И не судит комментарии: имя в
// прозе — предмет убывающего потолка привязок
// (internal/repohygiene/retiredidentityvendorceiling.go).
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// retiredVendorNames — имя снимаемого поставщика так, как его несут объекты
// рендера: две службы и пространство их образов.
var retiredVendorNames = []string{"kratos", "hydra", "oryd"}

// retiredVendorToggles — прежние выключатели карт поставщика в значениях
// подчарта. Рендер с поднятыми выключателями — положительный близнец
// утверждения 1: на дереве, где карты ещё есть, он их производит.
var retiredVendorToggles = []string{"kratos.config.enabled=true", "kratos.identitySchema.enabled=true"}

// kanameRetiredLandingKnob — снятая ручка посадки в значениях подчарта.
const kanameRetiredLandingKnob = "config.authn.identityProvider"

// kanameLanding — посадка службы доступа на ЛЮБОМ стеке. С kaname#363 она одна
// — своя полоса, — и ключа посадки у подчарта нет: пробы, прежде читавшие
// `kaname.config.authn.identityProvider`, судят эту половину как `own`
// безусловно (kacho#2818). Что профиль ключ посадки больше объявить не может,
// держит TestKanameSubchartRefusesTheRetiredLandingKnob.
const kanameLanding = "own"

// hooksLaneEnvPrefixes и hooksLanePortName — полоса хуков прежнего поставщика
// в поде службы: общий секрет обратных вызовов, транспорт её слушателя и порт.
var hooksLaneEnvPrefixes = []string{"KANAME_HOOK_TOKEN", "KANAME_HOOKS_SERVER_"}

const hooksLanePortName = "http-hooks"

// refusedSetting — строка перечня снятых настроек пиненного модуля.
type refusedSetting struct {
	Key string // путь ключа в файле настроек
	Env string // переменная окружения
}

// refusedSettingsOfSource — перечень `retiredSettings` из исходника модуля.
//
// Разбор синтаксиса, а не поиск по образцу: судится объявление переменной и
// поля `Key`/`Env` каждой строки. Отказ — объявления нет, оно не литерал,
// строка без ключа или переменной, перечень пуст.
func refusedSettingsOfSource(src []byte) ([]refusedSetting, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "retired_settings.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("исходник не разобран: %w", err)
	}
	var out []refusedSetting
	declared := false
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "retiredSettings" {
					continue
				}
				declared = true
				if i >= len(vs.Values) {
					return nil, fmt.Errorf("retiredSettings объявлен без значения")
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					return nil, fmt.Errorf("retiredSettings объявлен не литералом перечня — форма сменилась")
				}
				for _, el := range lit.Elts {
					row, err := refusedSettingRow(el)
					if err != nil {
						return nil, err
					}
					out = append(out, row)
				}
			}
		}
	}
	if !declared {
		return nil, fmt.Errorf("объявления retiredSettings в исходнике нет")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("перечень retiredSettings пуст")
	}
	return out, nil
}

// refusedSettingRow — одна строка перечня: поля `Key` и `Env` строковыми литералами.
func refusedSettingRow(el ast.Expr) (refusedSetting, error) {
	row, ok := el.(*ast.CompositeLit)
	if !ok {
		return refusedSetting{}, fmt.Errorf("строка перечня — не литерал структуры")
	}
	var r refusedSetting
	for _, fe := range row.Elts {
		kv, ok := fe.(*ast.KeyValueExpr)
		if !ok {
			return refusedSetting{}, fmt.Errorf("строка перечня без имён полей — разбор не угадывает порядок")
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil || (key.Name != "Key" && key.Name != "Env") {
			continue
		}
		bl, isLit := kv.Value.(*ast.BasicLit)
		if !isLit || bl.Kind != token.STRING {
			return refusedSetting{}, fmt.Errorf("поле %s строки перечня — не строковый литерал", key.Name)
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil {
			return refusedSetting{}, fmt.Errorf("поле %s строки перечня не раскавычивается: %w", key.Name, err)
		}
		if key.Name == "Key" {
			r.Key = v
		} else {
			r.Env = v
		}
	}
	if r.Key == "" || r.Env == "" {
		return refusedSetting{}, fmt.Errorf("строка перечня без ключа или переменной: %+v", r)
	}
	return r, nil
}

// pinnedRefusedSettings — перечень снятых настроек у пиненного модуля службы.
func pinnedRefusedSettings(t *testing.T, moduleDir string) []refusedSetting {
	t.Helper()
	path := filepath.Join(moduleDir, "internal", "apps", "kaname", "config", "retired_settings.go")
	src, err := os.ReadFile(path) // #nosec G304 -- путь собран из пина go.mod, не из ввода
	if err != nil {
		t.Fatalf("перечень снятых настроек пиненного модуля не прочитан (%s): %v.\n"+
			"Пин, у которого перечня нет, не несёт kaname#363 — посылка «служба отвергает "+
			"снятый ключ» им не подтверждена, и судить доставку снятого было бы не по чему", path, err)
	}
	rows, err := refusedSettingsOfSource(src)
	if err != nil {
		t.Fatalf("перечень снятых настроек пиненного модуля (%s): %v — почини разбор, а не ослабляй проверку", path, err)
	}
	return rows
}

// moduleReadsHooksLane — есть ли у модуля читатель полосы хуков: строковый
// литерал с именем её переменной в не-тестовом исходнике Go. Упоминание в
// комментарии читателем не является, литерал в пробе — тоже.
func moduleReadsHooksLane(moduleDir string) (bool, int, error) {
	files := 0
	reads := false
	err := filepath.WalkDir(moduleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "vendor" || n == "testdata" || (strings.HasPrefix(n, ".") && path != moduleDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		src, rerr := os.ReadFile(path) // #nosec G304 -- обход каталога пиненного модуля
		if rerr != nil {
			return rerr
		}
		if !containsAny(string(src), hooksLaneEnvPrefixes) {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if perr != nil {
			return fmt.Errorf("%s: %w", path, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING && containsAny(bl.Value, hooksLaneEnvPrefixes) {
				reads = true
			}
			return !reads
		})
		return nil
	})
	if err == nil && files == 0 {
		err = fmt.Errorf("в %s ни одного исходника Go — обход пуст", moduleDir)
	}
	return reads, files, err
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// vendorNameIn — какое имя поставщика несёт строка (без учёта регистра), либо пусто.
func vendorNameIn(s string) string {
	low := strings.ToLower(s)
	for _, n := range retiredVendorNames {
		if strings.Contains(low, n) {
			return n
		}
	}
	return ""
}

// renderedDocs — документы рендера, разобранные как YAML; пустые пропущены.
func renderedDocs(t *testing.T, rendered string) []map[string]any {
	t.Helper()
	var docs []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var d map[string]any
		if err := dec.Decode(&d); err != nil {
			break
		}
		if d != nil {
			docs = append(docs, d)
		}
	}
	return docs
}

// podOf — рабочий объект службы (Deployment с именем `kaname`) либо nil.
func podOf(docs []map[string]any) map[string]any {
	for _, d := range docs {
		if kind, _ := d["kind"].(string); kind != "Deployment" {
			continue
		}
		if name, _ := lookup(d, "metadata", "name"); name == "kaname" {
			return d
		}
	}
	return nil
}

// podContainers — все контейнеры пода, включая инициализирующие.
func kanamePodContainers(pod map[string]any) []map[string]any {
	var out []map[string]any
	for _, sec := range []string{"initContainers", "containers"} {
		raw, _ := lookup(pod, "spec", "template", "spec", sec)
		list, _ := raw.([]any)
		for _, c := range list {
			if cm, ok := c.(map[string]any); ok {
				out = append(out, cm)
			}
		}
	}
	return out
}

// listOfMaps — элементы списка по ключу, которые являются отображениями.
func listOfMaps(m map[string]any, key string) []map[string]any {
	raw, _ := m[key].([]any)
	var out []map[string]any
	for _, e := range raw {
		if em, ok := e.(map[string]any); ok {
			out = append(out, em)
		}
	}
	return out
}

// configKeyPathsOf — пути ВСЕХ ключей тела настроек (и промежуточных узлов).
func configKeyPathsOf(node any, prefix string, out *[]string) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	for k, v := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		*out = append(*out, p)
		configKeyPathsOf(v, p, out)
	}
}

// judgeVendorWiring — утверждение 1 над документами одного рендера.
func judgeVendorWiring(where string, docs []map[string]any) []string {
	var out []string
	note := func(d map[string]any, what, val string) {
		if n := vendorNameIn(val); n != "" {
			kind, _ := d["kind"].(string)
			name, _ := lookup(d, "metadata", "name")
			out = append(out, fmt.Sprintf("%s: %s/%v — %s «%s» несёт имя поставщика «%s»", where, kind, name, what, val, n))
		}
	}
	for _, d := range docs {
		kind, _ := d["kind"].(string)
		note(d, "вид", kind)
		if name, ok := lookup(d, "metadata", "name"); ok {
			note(d, "имя", fmt.Sprint(name))
		}
		if labels, ok := lookup(d, "metadata", "labels"); ok {
			if lm, ok := labels.(map[string]any); ok {
				for k, v := range lm {
					note(d, "метка", k+"="+fmt.Sprint(v))
				}
			}
		}
		for _, sec := range []string{"data", "binaryData"} {
			if dm, ok := d[sec].(map[string]any); ok {
				for k := range dm {
					note(d, "ключ данных", k)
				}
			}
		}
		if kind == "ConfigMap" {
			if raw, ok := lookup(d, "data", "config.yaml"); ok {
				var cfg map[string]any
				if s, ok := raw.(string); ok && yaml.Unmarshal([]byte(s), &cfg) == nil {
					var paths []string
					configKeyPathsOf(cfg, "", &paths)
					for _, p := range paths {
						note(d, "ключ настроек службы", p)
					}
				}
			}
		}
		if kind == "Service" {
			if spec, ok := d["spec"].(map[string]any); ok {
				for _, p := range listOfMaps(spec, "ports") {
					note(d, "порт", fmt.Sprint(p["name"]))
				}
			}
		}
		if kind != "Deployment" {
			continue
		}
		if raw, ok := lookup(d, "spec", "template", "spec", "volumes"); ok {
			vols, _ := raw.([]any)
			for _, v := range vols {
				vm, _ := v.(map[string]any)
				note(d, "том", fmt.Sprint(vm["name"]))
				for _, src := range []string{"configMap", "secret"} {
					if sm, ok := vm[src].(map[string]any); ok {
						note(d, "источник тома", fmt.Sprint(sm["name"], sm["secretName"]))
					}
				}
			}
		}
		for _, c := range kanamePodContainers(d) {
			note(d, "контейнер", fmt.Sprint(c["name"]))
			note(d, "образ", fmt.Sprint(c["image"]))
			for _, e := range listOfMaps(c, "env") {
				note(d, "переменная", fmt.Sprint(e["name"]))
			}
			for _, p := range listOfMaps(c, "ports") {
				note(d, "порт", fmt.Sprint(p["name"]))
			}
		}
	}
	sort.Strings(out)
	return out
}

// declaredIn — объявлен ли ключ (путь через точку) в теле настроек: судится
// присутствие ключа в родительской карте, а не значение.
func declaredIn(cfg map[string]any, key string) bool {
	segs := strings.Split(key, ".")
	cur := cfg
	for i, s := range segs {
		v, ok := cur[s]
		if !ok {
			return false
		}
		if i == len(segs)-1 {
			return true
		}
		next, ok := v.(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	return false
}

// judgeRefusedDelivery — утверждение 2 над телом настроек и переменными пода.
func judgeRefusedDelivery(stack string, refused []refusedSetting, cfg map[string]any, env map[string]bool) []string {
	var out []string
	for _, r := range refused {
		if declaredIn(cfg, r.Key) {
			out = append(out, fmt.Sprintf("стек %s: карта kaname-config объявляет `%s` — пиненная служба "+
				"отвергает этот ключ при любом значении и не стартует", stack, r.Key))
		}
		if env[r.Env] {
			out = append(out, fmt.Sprintf("стек %s: под службы получает переменную %s — пиненная служба "+
				"отвергает её при любом значении и не стартует", stack, r.Env))
		}
	}
	return out
}

// hooksLaneOf — что из полосы хуков несёт рендер: переменные пода и порты
// (пода и Service) с именем полосы.
func hooksLaneOf(docs []map[string]any) []string {
	var out []string
	for _, d := range docs {
		kind, _ := d["kind"].(string)
		switch kind {
		case "Deployment":
			for _, c := range kanamePodContainers(d) {
				for _, e := range listOfMaps(c, "env") {
					name := fmt.Sprint(e["name"])
					for _, p := range hooksLaneEnvPrefixes {
						if strings.HasPrefix(name, p) {
							out = append(out, "переменная "+name)
						}
					}
				}
				for _, p := range listOfMaps(c, "ports") {
					if p["name"] == hooksLanePortName {
						out = append(out, "порт пода "+hooksLanePortName)
					}
				}
			}
		case "Service":
			spec, _ := d["spec"].(map[string]any)
			for _, p := range listOfMaps(spec, "ports") {
				if p["name"] == hooksLanePortName {
					name, _ := lookup(d, "metadata", "name")
					out = append(out, fmt.Sprintf("порт %s Service %v", hooksLanePortName, name))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// judgeHooksLane — утверждение 3: пин полосу не читает ⇒ рендер её не несёт.
func judgeHooksLane(stack string, pinReads bool, lane []string) []string {
	if pinReads {
		return nil
	}
	var out []string
	for _, l := range lane {
		out = append(out, fmt.Sprintf("стек %s: %s — полосу хуков пиненная служба не читает, "+
			"значение до поведения не доходит", stack, l))
	}
	return out
}

// kanameSubchartRender — один рендер под судом: где и какие документы.
type kanameSubchartRender struct {
	where string
	docs  []map[string]any
}

// kanameSubchartRenders — рендеры подчарта по каждому стеку плюс умолчания и
// умолчания с поднятыми выключателями карт поставщика.
func kanameSubchartRenders(t *testing.T) (stacks []kanameSubchartRender, extra []kanameSubchartRender) {
	t.Helper()
	all := deployStacks(t)
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		out, err := renderStackSubchart(t, name, stackIdentityValues(t, all[name]))
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы не удался: %v\n%s", name, err, out)
		}
		stacks = append(stacks, kanameSubchartRender{"стек " + name, renderedDocs(t, out)})
	}
	for _, c := range []struct {
		where string
		sets  []string
	}{
		{"умолчания подчарта", nil},
		{"умолчания подчарта с поднятыми выключателями карт поставщика", retiredVendorToggles},
	} {
		out, err := renderIdentitySubchart(t, nil, c.sets...)
		if err != nil {
			t.Fatalf("%s: рендер подчарта не удался: %v\n%s", c.where, err, out)
		}
		extra = append(extra, kanameSubchartRender{c.where, renderedDocs(t, out)})
	}
	for _, r := range append(append([]kanameSubchartRender{}, stacks...), extra...) {
		if podOf(r.docs) == nil {
			t.Fatalf("%s: в рендере нет пода службы (Deployment kaname) — смотреть было не на что, "+
				"«провязки нет» здесь неотличимо от «рендера нет»", r.where)
		}
	}
	return stacks, extra
}

// TestKanameSubchartRendersNoVendorWiring — утверждение 1.
func TestKanameSubchartRendersNoVendorWiring(t *testing.T) {
	stacks, extra := kanameSubchartRenders(t)
	objects := 0
	var census []string
	for _, r := range append(stacks, extra...) {
		findings := judgeVendorWiring(r.where, r.docs)
		for _, f := range findings {
			t.Error(f)
		}
		objects += len(r.docs)
		census = append(census, fmt.Sprintf("%s: объектов %d, находок %d", r.where, len(r.docs), len(findings)))
	}
	t.Logf("перепись: рендеров %d (стеков %d) · объектов %d\n  %s",
		len(stacks)+len(extra), len(stacks), objects, strings.Join(census, "\n  "))
}

// TestKanameSubchartDeliversNoSettingThePinRefuses — утверждение 2.
func TestKanameSubchartDeliversNoSettingThePinRefuses(t *testing.T) {
	refused := pinnedRefusedSettings(t, kanameModuleDir(t, ".."))
	stacks, extra := kanameSubchartRenders(t)
	var census []string
	for _, r := range append(stacks, extra...) {
		cfg, env := renderedDelivery(t, renderedDocsText(t, r.docs))
		findings := judgeRefusedDelivery(r.where, refused, cfg, env)
		for _, f := range findings {
			t.Error(f)
		}
		census = append(census, fmt.Sprintf("%s: находок %d", r.where, len(findings)))
	}
	keys := make([]string, 0, len(refused))
	for _, r := range refused {
		keys = append(keys, r.Key+" / "+r.Env)
	}
	t.Logf("перепись: пин %s · снятых настроек %d (%s) · рендеров %d\n  %s",
		productModulePins(t, "..")[kanameModulePart], len(refused), strings.Join(keys, ", "),
		len(census), strings.Join(census, "\n  "))
}

// TestKanameSubchartWiresNoHooksLaneThePinDoesNotRead — утверждение 3.
func TestKanameSubchartWiresNoHooksLaneThePinDoesNotRead(t *testing.T) {
	moduleDir := kanameModuleDir(t, "..")
	reads, files, err := moduleReadsHooksLane(moduleDir)
	if err != nil {
		t.Fatalf("читатель полосы хуков у пиненного модуля не установлен: %v", err)
	}
	stacks, extra := kanameSubchartRenders(t)
	carried := 0
	for _, r := range append(stacks, extra...) {
		lane := hooksLaneOf(r.docs)
		carried += len(lane)
		for _, f := range judgeHooksLane(r.where, reads, lane) {
			t.Error(f)
		}
	}
	verdict := "пин полосу НЕ читает — судится"
	if reads {
		verdict = "пин полосу читает — утверждение не судит"
	}
	t.Logf("перепись: пин %s · исходников модуля осмотрено %d · %s · рендеров %d · мест полосы в рендерах %d",
		productModulePins(t, "..")[kanameModulePart], files, verdict, len(stacks)+len(extra), carried)
}

// TestKanameSubchartRefusesTheRetiredLandingKnob — отказ чарта на снятой ручке.
func TestKanameSubchartRefusesTheRetiredLandingKnob(t *testing.T) {
	if out, err := renderIdentitySubchart(t, nil); err != nil {
		t.Fatalf("близнец: подчарт на своих умолчаниях обязан рендериться: %v\n%s", err, out)
	}
	for _, v := range []string{"own", "external", `""`} {
		out, err := renderIdentitySubchart(t, nil, kanameRetiredLandingKnob+"="+v)
		if err == nil {
			t.Errorf("профиль объявил `%s=%s`, и подчарт отрендерился: снятая ручка принята и "+
				"проигнорирована, оператор читает её как действующее решение", kanameRetiredLandingKnob, v)
			continue
		}
		if !strings.Contains(out, kanameRetiredLandingKnob) {
			t.Errorf("отказ рендера на `%s=%s` не называет ручку — оператор не узнает, что убрать:\n%s",
				kanameRetiredLandingKnob, v, out)
		}
	}
}

// renderedDocsText — документы обратно в текст рендера (для общих читателей).
func renderedDocsText(t *testing.T, docs []map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, d := range docs {
		body, err := yaml.Marshal(d)
		if err != nil {
			t.Fatalf("документ рендера не сериализуется: %v", err)
		}
		b.WriteString("---\n")
		b.Write(body)
	}
	return b.String()
}
