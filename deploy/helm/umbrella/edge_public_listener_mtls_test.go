// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_public_listener_mtls_test.go — о публичном слушателе края каждая боевая
// цепочка и гейт посадки говорят одно (kacho#2839).
//
// ПРЕДМЕТ. Гейт посадки (deploy/scripts/assert-production-posture.sh) судит
// самоотчёт живого процесса и принимает от края одно значение признака
// public_mtls. Цепочка профилей производит то значение, которое край получит на
// самом деле. Когда они расходятся, это становится известно только на поднятом
// стенде, красным гейтом, и чинить его тянет гейт, а не цепочку. Проба сводит обе
// стороны до подъёма: для каждой боевой цепочки из deploy/stacks.txt значение,
// которое доложит процесс, обязано быть тем, что гейт принимает.
//
// ПОЧЕМУ РЕНДЕР, А НЕ ОБЪЯВЛЕНИЕ. Признак процесса выводится из нескольких ручек
// окружения, а шаблон чарта эмитит их под разными условиями: ручку слушателя
// только при `tls.enabled`, ручку признака только внутри блока `mtls.enable`.
// Объявленное в профиле значение признака при выключенном слушателе даёт процесс,
// который докладывает false. Поэтому судится то, что получает процесс: окружение
// контейнера края в рендере его чарта.
//
// ПОЧЕМУ ЧАРТ КРАЯ САМ ПО СЕБЕ. Зависимости зонта в дереве не лежат, и рендер
// зонта целиком здесь не поднимается без `helm dep build`. У чарта края
// зависимостей нет. helm складывает значения подчарта из его умолчаний и ключа
// подчарта в каждом слое цепочки; ровно эти ключи подаются ему слоями `-f`, в
// порядке цепочки, первым идёт values.yaml зонта. Каталог чарта и ключ подчарта
// читаются из Chart.yaml зонта.
//
// ОТКУДА ФОРМУЛА ПРИЗНАКА. Из производителя. Выражение поля PublicMTLS в
// gateway/cmd/api-gateway/bootposture.go и тела методов конфигурации, которые
// оно зовёт, разбираются и вычисляются над окружением рендера. Имена ручек и их
// умолчания берутся из тегов полей конфигурации. Форма, которой вычислитель не
// знает, роняет пробу с координатой, а не проходит молча.
//
// ОТКУДА ТРЕБОВАНИЕ ГЕЙТА. Программа вердикта вынимается из гейта и исполняется
// jq на строке самоотчёта, которую пишет LogBootPosture, тот же писатель, что у
// процесса. Требование выводится из исхода программы, а не выписывается здесь.
//
// ЧТО НЕ СУДИТСЯ. Прочие измерения самоотчёта и отказы старта края по иным ручкам
// судят свои стражи и пробы. Живой процесс и его лог судит сам гейт на поднятом
// стенде. Слой учётных данных площадки в цепочки не входит (шапка stacks.txt).
package umbrella_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/observability"
	"gopkg.in/yaml.v3"
)

const (
	// edgeListenerChartName — имя зависимости края в Chart.yaml зонта.
	edgeListenerChartName = "api-gateway"
	// edgeListenerGateScript — гейт посадки, чья программа вердикта исполняется.
	edgeListenerGateScript = "../../scripts/assert-production-posture.sh"
	// edgeListenerPostureSrc — где край собирает свой самоотчёт.
	edgeListenerPostureSrc = "../../../gateway/cmd/api-gateway/bootposture.go"
	// edgeListenerConfigDir — пакет конфигурации края: поля, теги, методы.
	edgeListenerConfigDir = "../../../gateway/internal/config"
	// edgeListenerSignField — поле самоотчёта, о котором спор.
	edgeListenerSignField = "PublicMTLS"
	// edgeListenerSignKey — ключ того же поля в строке самоотчёта.
	edgeListenerSignKey = "public_mtls"
	// edgeListenerToolDeadline — срок одного вызова helm или jq.
	edgeListenerToolDeadline = 2 * time.Minute
)

// edgeListenerRequireTools — helm и jq. Отсутствие при CI роняет пробу, а не
// пропускает её: проба, молча ставшая инертной на задании, гейтящем слияние,
// гейтом не является. Вне CI отсутствие даёт пропуск с именем инструмента.
func edgeListenerRequireTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"helm", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s не в PATH при CI — проба рендера обязана исполняться, а не пропускаться", bin)
			}
			t.Skipf("%s не в PATH — проба рендера пропущена", bin)
		}
	}
}

// ── ПРОИЗВОДИТЕЛЬ: как процесс выводит признак из своего окружения ──────────

// edgeListenerKnob — поле конфигурации края, как его читает загрузчик.
type edgeListenerKnob struct {
	env  string
	def  string
	kind string // "string" | "bool"
}

// edgeListenerProducer — формула признака и всё, что нужно для её вычисления.
type edgeListenerProducer struct {
	service string                   // Service самоотчёта, литерал производителя
	formula ast.Expr                 // значение поля PublicMTLS в самоотчёте
	methods map[string]*ast.FuncDecl // методы Config по имени
	knobs   map[string]edgeListenerKnob
	fset    *token.FileSet
}

// errEdgeListenerShape — форма производителя, которой вычислитель не знает.
// Это отказ пробы: молчать о форме значит вычислять не то, что исполняет процесс.
var errEdgeListenerShape = errors.New("форма производителя не распознана")

// edgeListenerReadProducer разбирает самоотчёт и конфигурацию края.
func edgeListenerReadProducer(t *testing.T) edgeListenerProducer {
	t.Helper()
	p := edgeListenerProducer{
		methods: map[string]*ast.FuncDecl{},
		knobs:   map[string]edgeListenerKnob{},
		fset:    token.NewFileSet(),
	}
	posture, err := parser.ParseFile(p.fset, edgeListenerPostureSrc, nil, 0)
	if err != nil {
		t.Fatalf("самоотчёт края не разобран (%s): %v", edgeListenerPostureSrc, err)
	}
	var formulas int
	ast.Inspect(posture, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return true
		}
		switch key.Name {
		case edgeListenerSignField:
			p.formula = kv.Value
			formulas++
		case "Service":
			if lit, isLit := kv.Value.(*ast.BasicLit); isLit && lit.Kind == token.STRING {
				p.service, _ = strconv.Unquote(lit.Value)
			}
		}
		return true
	})
	if formulas != 1 || p.service == "" {
		t.Fatalf("в %s ожидалось ровно одно поле %s и литерал Service; найдено полей %d, Service=%q — "+
			"производитель сменил форму, чинить надо чтение, а не молчать",
			edgeListenerPostureSrc, edgeListenerSignField, formulas, p.service)
	}

	pkgs, err := parser.ParseDir(p.fset, edgeListenerConfigDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("пакет конфигурации края не разобран (%s): %v", edgeListenerConfigDir, err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				edgeListenerCollectDecl(p, decl)
			}
		}
	}
	if len(p.knobs) == 0 || len(p.methods) == 0 {
		t.Fatalf("в %s не найдено ни полей Config с тегом envconfig (%d), ни его методов (%d)",
			edgeListenerConfigDir, len(p.knobs), len(p.methods))
	}
	return p
}

func edgeListenerCollectDecl(p edgeListenerProducer, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil || len(d.Recv.List) != 1 {
			return
		}
		recv := d.Recv.List[0].Type
		if star, ok := recv.(*ast.StarExpr); ok {
			recv = star.X
		}
		if id, ok := recv.(*ast.Ident); ok && id.Name == "Config" {
			p.methods[d.Name.Name] = d
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Config" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, f := range st.Fields.List {
				if f.Tag == nil || len(f.Names) != 1 {
					continue
				}
				raw, err := strconv.Unquote(f.Tag.Value)
				if err != nil {
					continue
				}
				tag := reflect.StructTag(raw)
				env := tag.Get("envconfig")
				kind, ok := f.Type.(*ast.Ident)
				if env == "" || !ok {
					continue
				}
				p.knobs[f.Names[0].Name] = edgeListenerKnob{env: env, def: tag.Get("default"), kind: kind.Name}
			}
		}
	}
}

// edgeListenerReading — что доложит процесс, поднятый на окружении рендера.
type edgeListenerReading struct {
	reported bool
	refusal  string   // непусто: процесс не стартует на этом окружении
	read     []string // ручки, которые формула прочла, с их значениями
	knobs    []string // имена тех же ручек
}

// edgeListenerEval вычисляет формулу признака над окружением контейнера.
func (p edgeListenerProducer) edgeListenerEval(env map[string]string) (edgeListenerReading, error) {
	var r edgeListenerReading
	v, err := p.evalExpr(p.formula, env, &r, 0)
	if err != nil {
		var refusal edgeListenerRefusal
		if errors.As(err, &refusal) {
			r.refusal = refusal.Error()
			return r, nil
		}
		return r, err
	}
	b, ok := v.(bool)
	if !ok {
		return r, fmt.Errorf("%w: %s даёт %T, а не bool", errEdgeListenerShape, p.pos(p.formula), v)
	}
	r.reported = b
	return r, nil
}

// edgeListenerRefusal — величина ручки, на которой загрузчик откажет в старте.
type edgeListenerRefusal struct{ msg string }

func (e edgeListenerRefusal) Error() string { return e.msg }

func (p edgeListenerProducer) pos(n ast.Node) string { return p.fset.Position(n.Pos()).String() }

// evalExpr знает ровно те формы, из которых сегодня сложен признак: конъюнкцию и
// дизъюнкцию, отрицание, сравнение поля со строковым литералом, чтение поля и
// вызов метода Config без аргументов, чьё тело — один return.
func (p edgeListenerProducer) evalExpr(e ast.Expr, env map[string]string, r *edgeListenerReading, depth int) (any, error) {
	if depth > 16 {
		return nil, fmt.Errorf("%w: %s — вложенность вызовов глубже 16", errEdgeListenerShape, p.pos(e))
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return p.evalExpr(x.X, env, r, depth)
	case *ast.UnaryExpr:
		if x.Op != token.NOT {
			break
		}
		v, err := p.evalBool(x.X, env, r, depth)
		return !v, err
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND, token.LOR:
			left, err := p.evalBool(x.X, env, r, depth)
			if err != nil {
				return nil, err
			}
			right, err := p.evalBool(x.Y, env, r, depth)
			if err != nil {
				return nil, err
			}
			if x.Op == token.LAND {
				return left && right, nil
			}
			return left || right, nil
		case token.EQL, token.NEQ:
			left, err := p.evalExpr(x.X, env, r, depth)
			if err != nil {
				return nil, err
			}
			right, err := p.evalExpr(x.Y, env, r, depth)
			if err != nil {
				return nil, err
			}
			ls, lok := left.(string)
			rs, rok := right.(string)
			if !lok || !rok {
				break
			}
			return (ls == rs) == (x.Op == token.EQL), nil
		}
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return strconv.Unquote(x.Value)
		}
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || len(x.Args) != 0 {
			break
		}
		m, ok := p.methods[sel.Sel.Name]
		if !ok || m.Body == nil || len(m.Body.List) != 1 {
			break
		}
		ret, ok := m.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			break
		}
		return p.evalExpr(ret.Results[0], env, r, depth+1)
	case *ast.SelectorExpr:
		if _, isIdent := x.X.(*ast.Ident); !isIdent {
			break
		}
		knob, ok := p.knobs[x.Sel.Name]
		if !ok {
			break
		}
		raw, set := env[knob.env]
		if set {
			r.read = append(r.read, fmt.Sprintf("%s=%q", knob.env, raw))
		} else {
			raw = knob.def
			r.read = append(r.read, fmt.Sprintf("%s не эмитирована, умолчание %q", knob.env, raw))
		}
		r.knobs = append(r.knobs, knob.env)
		switch knob.kind {
		case "string":
			return raw, nil
		case "bool":
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, edgeListenerRefusal{fmt.Sprintf("%s=%q не разбирается как bool", knob.env, raw)}
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("%w: %s (%T)", errEdgeListenerShape, p.pos(e), e)
}

func (p edgeListenerProducer) evalBool(e ast.Expr, env map[string]string, r *edgeListenerReading, depth int) (bool, error) {
	v, err := p.evalExpr(e, env, r, depth)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s даёт %T, а не bool", errEdgeListenerShape, p.pos(e), v)
	}
	return b, nil
}

// ── РЕНДЕР: окружение, которое получит контейнер края на цепочке ────────────

// edgeListenerSubchart — каталог чарта края и ключ его значений в зонте.
func edgeListenerSubchart(t *testing.T) (dir, key string) {
	t.Helper()
	var chart struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Alias      string `yaml:"alias"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	raw, err := os.ReadFile("Chart.yaml")
	if err != nil {
		t.Fatalf("Chart.yaml зонта не читается: %v", err)
	}
	if err := yaml.Unmarshal(raw, &chart); err != nil {
		t.Fatalf("Chart.yaml зонта не разобран: %v", err)
	}
	for _, dep := range chart.Dependencies {
		if dep.Name != edgeListenerChartName {
			continue
		}
		local, ok := strings.CutPrefix(dep.Repository, "file://")
		if !ok {
			t.Fatalf("зависимость %s в Chart.yaml зонта не локальная (%q) — рендерить нечего",
				edgeListenerChartName, dep.Repository)
		}
		key = dep.Name
		if dep.Alias != "" {
			key = dep.Alias
		}
		return filepath.Clean(local), key
	}
	t.Fatalf("в Chart.yaml зонта нет зависимости %s", edgeListenerChartName)
	return "", ""
}

// edgeListenerRender рендерит чарт края слоями цепочки и возвращает окружение
// его контейнера. extra — слои поверх цепочки, уже в форме значений края:
// так проба вносит расхождение настоящей ручкой чарта.
func edgeListenerRender(t *testing.T, chain []string, extra ...map[string]any) edgeListenerEnv {
	t.Helper()
	dir, key := edgeListenerSubchart(t)
	tmp := t.TempDir()
	args := []string{"template", "kacho-umbrella", dir, "-n", "kacho"}
	layers := append([]string{"values.yaml"}, chain...)
	for i, profile := range layers {
		tree := umbrellaValues(t, profile)
		layer := map[string]any{}
		if sub, ok := tree[key].(map[string]any); ok {
			for k, v := range sub {
				layer[k] = v
			}
		}
		if g, ok := tree["global"]; ok {
			layer["global"] = g
		}
		args = append(args, "-f", edgeListenerWriteLayer(t, tmp, fmt.Sprintf("%02d-%s", i, filepath.Base(profile)), layer))
	}
	for i, layer := range extra {
		args = append(args, "-f", edgeListenerWriteLayer(t, tmp, fmt.Sprintf("x%02d.yaml", i), layer))
	}
	ctx, cancel := context.WithTimeout(t.Context(), edgeListenerToolDeadline)
	defer cancel()
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	if err != nil {
		t.Fatalf("рендер чарта края на цепочке %v не прошёл (срок %s): %v\n%s", chain, edgeListenerToolDeadline, err, out)
	}
	return edgeListenerContainerEnv(t, string(out), chain)
}

func edgeListenerWriteLayer(t *testing.T, dir, name string, layer map[string]any) string {
	t.Helper()
	raw, err := yaml.Marshal(layer)
	if err != nil {
		t.Fatalf("слой %s не сериализовался: %v", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("слой %s не записан: %v", name, err)
	}
	return path
}

// edgeListenerEnv — окружение контейнера края в рендере. fromRef — имена,
// чьё значение приезжает через valueFrom: рендером оно не судимо.
type edgeListenerEnv struct {
	values  map[string]string
	fromRef map[string]bool
}

// edgeListenerContainerEnv — окружение единственного контейнера края в рендере.
// Позднее объявление той же переменной выигрывает, как у kubelet.
func edgeListenerContainerEnv(t *testing.T, manifest string, chain []string) edgeListenerEnv {
	t.Helper()
	type envVar struct {
		Name      string `yaml:"name"`
		Value     string `yaml:"value"`
		ValueFrom any    `yaml:"valueFrom"`
	}
	type doc struct {
		Kind string `yaml:"kind"`
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Name string   `yaml:"name"`
						Env  []envVar `yaml:"env"`
					} `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	dec := yaml.NewDecoder(strings.NewReader(manifest))
	var envs []edgeListenerEnv
	for {
		var d doc
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("рендер края на цепочке %v не разобран: %v", chain, err)
		}
		if d.Kind != "Deployment" {
			continue
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			env := edgeListenerEnv{values: map[string]string{}, fromRef: map[string]bool{}}
			for _, e := range c.Env {
				delete(env.values, e.Name)
				delete(env.fromRef, e.Name)
				if e.ValueFrom != nil {
					env.fromRef[e.Name] = true
					continue
				}
				env.values[e.Name] = e.Value
			}
			envs = append(envs, env)
		}
	}
	if len(envs) != 1 {
		t.Fatalf("рендер края на цепочке %v несёт контейнеров Deployment %d, ожидался один — "+
			"проба не знает, чьё окружение судить", chain, len(envs))
	}
	return envs[0]
}

// ── ГЕЙТ: какое значение признака он принимает ──────────────────────────────

var (
	edgeListenerVerdictHead = regexp.MustCompile(`(?s)^.*?jq -r --argjson need_fwd "\$need_fwd" '`)
	edgeListenerNarrowing   = regexp.MustCompile(`(?m)^FORWARDER_NARROWING_REQUIRED="\$\{FORWARDER_NARROWING_REQUIRED:-([^}]*)\}"`)
	edgeListenerSignJudged  = regexp.MustCompile(`shown\("` + edgeListenerSignKey + `"\)`)
)

// edgeListenerGate — программа вердикта гейта и величина need_fwd, с которой
// гейт зовёт её для края.
type edgeListenerGate struct {
	program string
	needFwd bool
}

func edgeListenerReadGate(t *testing.T, service string) edgeListenerGate {
	t.Helper()
	raw, err := os.ReadFile(edgeListenerGateScript)
	if err != nil {
		t.Fatalf("гейт посадки не прочитан (%s): %v", edgeListenerGateScript, err)
	}
	src := string(raw)
	lines := strings.Split(src, "\n")
	start, end := -1, -1
	for i, l := range lines {
		if start < 0 && strings.Contains(l, `verdict="$(printf`) {
			start = i
		}
		if start >= 0 && strings.Contains(l, `join(", ")`) {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		t.Fatalf("в %s не найдены границы программы вердикта (начало %d, конец %d) — гейт сменил форму",
			edgeListenerGateScript, start, end)
	}
	block := strings.Join(lines[start:end+1], "\n")
	if !edgeListenerVerdictHead.MatchString(block) {
		t.Fatalf("вызов jq в %s записан иначе, чем ожидает извлечение:\n%s", edgeListenerGateScript, block)
	}
	block = strings.TrimSuffix(strings.TrimSpace(edgeListenerVerdictHead.ReplaceAllString(block, "")), `')"`)
	if !edgeListenerSignJudged.MatchString(block) {
		t.Fatalf("программа вердикта %s не судит %s вовсе — сверять цепочки не с чем:\n%s",
			edgeListenerGateScript, edgeListenerSignKey, block)
	}
	m := edgeListenerNarrowing.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("в %s не найден перечень FORWARDER_NARROWING_REQUIRED — не знаю, с каким need_fwd "+
			"гейт зовёт вердикт для %s", edgeListenerGateScript, service)
	}
	g := edgeListenerGate{program: block}
	for _, s := range strings.Fields(m[1]) {
		if s == service {
			g.needFwd = true
		}
	}
	return g
}

// line — самоотчёт края, годный по всем измерениям, кроме спорного: его величина
// задаётся. Пишет его писатель процесса, поэтому имена ключей — его.
func (g edgeListenerGate) line(t *testing.T, service string, sign bool) string {
	t.Helper()
	var buf bytes.Buffer
	observability.LogBootPosture(observability.NewSlogger(&buf), observability.BootPosture{
		Service:            service,
		AuthMode:           "production",
		DBSSLMode:          observability.DBSSLModeNotApplicable,
		PublicMTLS:         sign,
		InternalMTLS:       observability.InternalMTLSNotApplicable,
		AuthZCheck:         true,
		TrustedForwarders:  g.needFwd,
		IdentityProvider:   "own",
		OwnRESTPublicTLS:   observability.OwnRESTFrontNotRaised,
		OwnRESTInternalTLS: observability.OwnRESTFrontNotRaised,
	})
	return buf.String()
}

func (g edgeListenerGate) verdict(t *testing.T, line string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), edgeListenerToolDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "jq", "-r", "--argjson", "need_fwd", strconv.FormatBool(g.needFwd), g.program) // #nosec G204 -- программа из дерева
	cmd.Stdin = strings.NewReader(line)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jq не отработал программу вердикта на %s (срок %s): %v\n%s", line, edgeListenerToolDeadline, err, out)
	}
	return strings.TrimSpace(string(out))
}

// required — единственное значение признака, которое гейт принимает. Годная по
// остальным измерениям строка обязана пройти целиком: иначе отказ по спорному
// измерению неотличим от отказа по чужому.
func (g edgeListenerGate) required(t *testing.T, service string) bool {
	t.Helper()
	var accepted []bool
	for _, sign := range []bool{true, false} {
		v := g.verdict(t, g.line(t, service, sign))
		if !strings.Contains(v, edgeListenerSignKey+"=") {
			if v != "" {
				t.Fatalf("строка, годная по %s=%v, забракована по чужому измерению: %q — "+
					"контроль пробы разошёлся с гейтом", edgeListenerSignKey, sign, v)
			}
			accepted = append(accepted, sign)
		}
	}
	if len(accepted) != 1 {
		t.Fatalf("гейт принимает значений %s: %v — ожидалось ровно одно; сверять цепочки не с чем",
			edgeListenerSignKey, accepted)
	}
	return accepted[0]
}

// ── СУД ─────────────────────────────────────────────────────────────────────

// judgeEdgeListener — находки по одной цепочке; пусто, когда рендер даёт то,
// что гейт принимает.
func judgeEdgeListener(chain string, r edgeListenerReading, required bool) []string {
	if r.refusal != "" {
		return []string{fmt.Sprintf("цепочка %s: край не стартует на окружении рендера (%s) — "+
			"гейт посадки не получит самоотчёта", chain, r.refusal)}
	}
	if r.reported != required {
		return []string{fmt.Sprintf("цепочка %s: рендер даёт краю %s=%v (%s), гейт посадки (%s) "+
			"принимает только %v", chain, edgeListenerSignKey, r.reported, strings.Join(r.read, ", "),
			edgeListenerGateScript, required)}
	}
	return nil
}

// edgeListenerChains — боевые цепочки таблицы в устойчивом порядке и имена
// пропущенных. Пустой набор боевых — отказ: ноль находок значил бы ноль осмотра.
func edgeListenerChains(t *testing.T) (names []string, stacks map[string][]string, skipped []string) {
	t.Helper()
	stacks = deployableStacks(t)
	for _, name := range sortedStackNames(stacks) {
		if stackIsProductionClass(t, stacks[name]) {
			names = append(names, name)
		} else {
			skipped = append(skipped, name)
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s не объявляет ни одной боевой цепочки — проба не вправе заключить, что их нет", stacksTable)
	}
	// Цепочка поставки `prod` несёт слой оператора из каталога образцов —
	// тем же правилом, что обёртки рендера цепочек (`deploy/stacks_render_wrapper_test.go`,
	// `deploy/tests/helm/lib/render-chain.sh`): число доверенных прыжков края
	// поставка не несёт (приёмка NTF-2 Р8, Д51), его задаёт оператор, и рендер
	// без него отказывает. Слой дописывается ПОСЛЕ классификации: класс цепочки
	// судят её собственные профили, а не образец.
	if prod, ok := stacks[edgeOperatorSampleChain]; ok {
		layered := make(map[string][]string, len(stacks))
		for n, c := range stacks {
			layered[n] = c
		}
		layered[edgeOperatorSampleChain] = append(append([]string(nil), prod...), edgeOperatorSample)
		stacks = layered
	}
	return names, stacks, skipped
}

// edgeOperatorSampleChain и edgeOperatorSample — цепочка, к которой гейт
// рендера дописывает слой оператора, и сам слой относительно каталога зонтика.
const (
	edgeOperatorSampleChain = "prod"
	edgeOperatorSample      = "../../testdata/mail-node/operator.yaml"
)

func edgeListenerRead(t *testing.T, p edgeListenerProducer, env edgeListenerEnv) edgeListenerReading {
	t.Helper()
	r, err := p.edgeListenerEval(env.values)
	if err != nil {
		t.Fatalf("формула признака не вычислена: %v", err)
	}
	for _, name := range r.knobs {
		if env.fromRef[name] {
			t.Fatalf("ручка %s приезжает через valueFrom — рендером признак не судим", name)
		}
	}
	return r
}

// TestEdgePublicListener_EveryProductionChainRendersWhatTheLandingGateRequires —
// предмет: на каждой боевой цепочке значение, которое доложит край, принимает
// гейт посадки.
func TestEdgePublicListener_EveryProductionChainRendersWhatTheLandingGateRequires(t *testing.T) {
	edgeListenerRequireTools(t)
	p := edgeListenerReadProducer(t)
	gate := edgeListenerReadGate(t, p.service)
	required := gate.required(t, p.service)
	names, stacks, skipped := edgeListenerChains(t)

	var findings []string
	for _, name := range names {
		r := edgeListenerRead(t, p, edgeListenerRender(t, stacks[name]))
		findings = append(findings, judgeEdgeListener(name, r, required)...)
	}
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("осмотрено: цепочек %d, боевых отрендерено %d, пропущено dev-класса %v; "+
		"гейт принимает %s=%v; находок %d", len(stacks), len(names), skipped,
		edgeListenerSignKey, required, len(findings))
}

// TestEdgePublicListener_Injection_RenderWithoutTheSignIsNamed — расхождение со
// стороны рендера: ручка признака выключена настоящей ручкой чарта. Находка
// называет цепочку. Близнец отличается одним фактом, ручка включена, и молчит.
func TestEdgePublicListener_Injection_RenderWithoutTheSignIsNamed(t *testing.T) {
	edgeListenerRequireTools(t)
	p := edgeListenerReadProducer(t)
	gate := edgeListenerReadGate(t, p.service)
	required := gate.required(t, p.service)
	names, stacks, _ := edgeListenerChains(t)

	for _, name := range names {
		off := edgeListenerRead(t, p, edgeListenerRender(t, stacks[name],
			map[string]any{"mtls": map[string]any{"hybridExternal": !required}}))
		edgeListenerRequireNamed(t, judgeEdgeListener(name, off, required), name)

		twin := edgeListenerRead(t, p, edgeListenerRender(t, stacks[name],
			map[string]any{"mtls": map[string]any{"hybridExternal": required}}))
		if f := judgeEdgeListener(name, twin, required); len(f) != 0 {
			t.Errorf("близнец цепочки %s (ручка признака включена) не молчит: %v", name, f)
		}
	}
}

// TestEdgePublicListener_Injection_DeclaredSignWithoutTheListenerIsNamed — проба
// судит производимое, а не объявленное: признак объявлен, а слушатель выключен.
// Процесс доложит false, и это находка. Близнец с включённым слушателем молчит.
func TestEdgePublicListener_Injection_DeclaredSignWithoutTheListenerIsNamed(t *testing.T) {
	edgeListenerRequireTools(t)
	p := edgeListenerReadProducer(t)
	gate := edgeListenerReadGate(t, p.service)
	required := gate.required(t, p.service)
	if !required {
		t.Fatalf("гейт принимает %s=false — объявление без слушателя расхождением не будет, "+
			"и эта инъекция утверждала бы о пустоте", edgeListenerSignKey)
	}
	names, stacks, _ := edgeListenerChains(t)

	for _, name := range names {
		declared := map[string]any{"mtls": map[string]any{"hybridExternal": true}}
		off := edgeListenerRead(t, p, edgeListenerRender(t, stacks[name], declared,
			map[string]any{"tls": map[string]any{"enabled": false}}))
		edgeListenerRequireNamed(t, judgeEdgeListener(name, off, required), name)

		twin := edgeListenerRead(t, p, edgeListenerRender(t, stacks[name], declared,
			map[string]any{"tls": map[string]any{"enabled": true}}))
		if f := judgeEdgeListener(name, twin, required); len(f) != 0 {
			t.Errorf("близнец цепочки %s (слушатель включён) не молчит: %v", name, f)
		}
	}
}

// TestEdgePublicListener_Injection_GateDemandingTheOppositeIsNamed — расхождение
// со стороны гейта: в вынутой программе требование к признаку обращено одним
// фактом. Требование выводится из исхода программы, поэтому обязано обратиться, и
// каждая цепочка, согласная с настоящим гейтом, становится находкой со своим
// именем. Близнец с настоящим гейтом — первая проба этого файла.
func TestEdgePublicListener_Injection_GateDemandingTheOppositeIsNamed(t *testing.T) {
	edgeListenerRequireTools(t)
	p := edgeListenerReadProducer(t)
	gate := edgeListenerReadGate(t, p.service)
	required := gate.required(t, p.service)

	demand := regexp.MustCompile(`\.` + edgeListenerSignKey + `\s*==\s*` + strconv.FormatBool(required))
	if n := len(demand.FindAllStringIndex(gate.program, -1)); n != 1 {
		t.Fatalf("требование гейта к %s записано не одним сравнением с %v (найдено %d) — "+
			"обратить его одним фактом нельзя", edgeListenerSignKey, required, n)
	}
	flipped := edgeListenerGate{
		program: demand.ReplaceAllString(gate.program, "."+edgeListenerSignKey+" == "+strconv.FormatBool(!required)),
		needFwd: gate.needFwd,
	}
	if got := flipped.required(t, p.service); got != !required {
		t.Fatalf("обращённый гейт принимает %s=%v — требование не выводится из программы", edgeListenerSignKey, got)
	}
	names, stacks, _ := edgeListenerChains(t)
	agreeing := 0
	for _, name := range names {
		r := edgeListenerRead(t, p, edgeListenerRender(t, stacks[name]))
		if len(judgeEdgeListener(name, r, required)) != 0 {
			continue
		}
		agreeing++
		edgeListenerRequireNamed(t, judgeEdgeListener(name, r, !required), name)
	}
	if agreeing == 0 {
		t.Fatalf("ни одна боевая цепочка не согласна с настоящим гейтом — инъекции со стороны гейта " +
			"нечего обращать")
	}
	t.Logf("цепочек, согласных с настоящим гейтом и названных обращённым: %d из %d", agreeing, len(names))
}

// TestEdgePublicListener_ProducerShapeUnknownIsARefusal — вычислитель не молчит
// о форме, которой не знает: вызов метода с аргументом его ронять обязан.
func TestEdgePublicListener_ProducerShapeUnknownIsARefusal(t *testing.T) {
	p := edgeListenerReadProducer(t)
	expr, err := parser.ParseExpr(`cfg.TLSEnabled(1)`)
	if err != nil {
		t.Fatalf("выражение инъекции не разобрано: %v", err)
	}
	p.formula = expr
	if _, err := p.edgeListenerEval(map[string]string{}); !errors.Is(err, errEdgeListenerShape) {
		t.Fatalf("незнакомая форма не дала отказа формы: %v", err)
	}
	twin, err := parser.ParseExpr(`cfg.TLSEnabled()`)
	if err != nil {
		t.Fatalf("выражение близнеца не разобрано: %v", err)
	}
	p.formula = twin
	if _, err := p.edgeListenerEval(map[string]string{}); err != nil {
		t.Fatalf("знакомая форма дала отказ: %v", err)
	}
}

func edgeListenerRequireNamed(t *testing.T, findings []string, chain string) {
	t.Helper()
	if len(findings) != 1 {
		t.Errorf("цепочка %s: ожидалась одна находка, получено %d: %v", chain, len(findings), findings)
		return
	}
	if !strings.Contains(findings[0], "цепочка "+chain+":") {
		t.Errorf("находка не называет цепочку %s: %s", chain, findings[0])
	}
}
