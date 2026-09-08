// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// producerless_input_test.go — a component wired at the edge must be
// CONFIGURABLE by some deployment profile.
//
// THE CLASS. A handler is constructed in the composition root from configuration
// fields, registered on the mux, and looks wired to every reader. But if EVERY
// field it takes from configuration has an empty default and is rendered by NO
// deployment profile, then no stand can ever give it a value: it refuses on
// every request, for its whole life, and the refusal is indistinguishable from
// "not configured yet". Nothing fails, nothing is logged, no gate goes red —
// the component simply never works, and its presence keeps asserting that the
// capability exists.
//
// That is not a hypothetical. The edge carried an interactive-login handler in
// exactly this state: five configuration keys, all with empty defaults, none
// rendered by any profile in either chart, and a refusal that pointed the
// operator at a provisioning Job that had already been deleted. It was retired
// rather than implemented, and this gate is what keeps the shape from coming
// back — in this component or any other.
//
// WHAT IS AND IS NOT A FINDING. A component with a MIX is fine and must stay
// silent: plenty of legitimate inputs are optional, derived from a sibling
// value, or meaningful by their absence, and a component that also takes a
// produced input is configurable. The finding is the ALL case — every
// configuration input unproducible — because that is the one that means "no
// profile can turn this on".
//
// UNIT AND SCOPE. The unit is a CONSTRUCTION SITE in the composition root
// (`pkg.NewThing(...)`), named by its callee. The producing side is the union
// over BOTH charts' templates — the question is "can ANY profile render this
// key", so a key rendered by one chart and not the other has a producer.
// The gate prints what it examined, and it refuses to pass on an empty scan:
// zero constructors or zero rendered keys means it is inspecting nothing, which
// must be a distinct outcome from "nothing found".
package deploy_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// repoRootFromDeploy walks up from gateway/deploy to the repository root.
func repoRootFromDeploy(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// configField is one `envconfig`-tagged field of the gateway Config.
type configField struct {
	env        string
	hasDefault bool
}

var envconfigTagRe = regexp.MustCompile(
	"(\\w+)\\s+\\S+\\s+`envconfig:\"([A-Z0-9_]+)\"(\\s+default:\"([^\"]*)\")?")

// parseConfigFields maps Go field name → its env key and whether a non-empty
// default exists. A non-empty default IS a producer: the binary supplies it.
func parseConfigFields(t *testing.T, path string) map[string]configField {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err, "config source must be readable — the gate cannot judge what it cannot parse")
	out := map[string]configField{}
	for _, m := range envconfigTagRe.FindAllStringSubmatch(string(src), -1) {
		out[m[1]] = configField{env: m[2], hasDefault: m[4] != ""}
	}
	return out
}

// envNameRe — имя переменной окружения, объявленное шаблоном.
//
// Приставкой имени платформы НЕ сужается, и это решение. Вопрос гейта —
// «существует ли производитель у ЭТОГО ключа», и на такой вопрос сторона
// производителей обязана быть ПОЛНОЙ: лишний собранный ключ может только
// снять обвинение, а пропущенный — предъявить ложное.
//
// Сужение по `KACHO_` было именно таким пропуском и сработало молча: часть
// продукта получила собственное имя (Kaname, #2076), её ключи стали
// `KANAME_*`, и гейт объявил производителя несуществующим при ДВУХ живых
// объявлениях в шаблонах. Приставка — не признак производителя.
var envNameRe = regexp.MustCompile(`name:\s*"?([A-Z][A-Z0-9_]{2,})"?`)

// renderedEnvKeys is the union of env keys ANY chart template can emit. Union is
// the right operator: the question is whether a producer exists at all, not
// whether a particular profile happens to render it.
func renderedEnvKeys(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, dir := range []string{
		filepath.Join(root, "gateway", "deploy", "templates"),
		filepath.Join(root, "deploy", "helm"),
	} {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // a missing chart dir is caught by the premise check below
			}
			if ext := filepath.Ext(p); ext != ".yaml" && ext != ".yml" && ext != ".tpl" {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil //nolint:nilerr // unreadable file cannot produce a key; premise check guards emptiness
			}
			for _, m := range envNameRe.FindAllStringSubmatch(string(b), -1) {
				out[m[1]] = true
			}
			return nil
		})
	}
	return out
}

// constructionSite is one `pkg.NewThing(...)` call in the composition root and
// the Config fields it reads.
type constructionSite struct {
	callee string
	line   int
	fields []string
}

// scanCompositionRoot returns every `*.New*(...)` call that reads at least one
// `cfg.<Field>`, via AST — comments and string literals cannot contribute.
func scanCompositionRoot(t *testing.T, path string) []constructionSite {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err, "composition root must parse — the gate cannot judge what it cannot parse")

	var sites []constructionSite
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "New") {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		seen := map[string]bool{}
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				s, ok := m.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := s.X.(*ast.Ident); ok && id.Name == "cfg" {
					seen[s.Sel.Name] = true
				}
				return true
			})
		}
		if len(seen) == 0 {
			return true
		}
		fields := make([]string, 0, len(seen))
		for k := range seen {
			fields = append(fields, k)
		}
		sort.Strings(fields)
		sites = append(sites, constructionSite{
			callee: pkg.Name + "." + sel.Sel.Name,
			line:   fset.Position(call.Pos()).Line,
			fields: fields,
		})
		return true
	})
	return sites
}

// findProducerless returns the sites whose EVERY configuration input is
// unproducible, plus the census of what was examined.
func findProducerless(sites []constructionSite, cfgFields map[string]configField, rendered map[string]bool) (findings []string, considered int) {
	for _, s := range sites {
		var known, orphan []string
		for _, f := range s.fields {
			cf, ok := cfgFields[f]
			if !ok {
				continue // not an envconfig-bound field: nothing to produce
			}
			known = append(known, f)
			if !cf.hasDefault && !rendered[cf.env] {
				orphan = append(orphan, f+" ("+cf.env+")")
			}
		}
		if len(known) == 0 {
			continue
		}
		considered++
		if len(orphan) == len(known) {
			findings = append(findings,
				s.callee+" at main.go:"+itoa(s.line)+
					" — every configuration input is producible by no profile: "+strings.Join(orphan, ", "))
		}
	}
	return findings, considered
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestCompositionRoot_NoComponentIsConfigurableByNoProfile is the gate.
func TestCompositionRoot_NoComponentIsConfigurableByNoProfile(t *testing.T) {
	root := repoRootFromDeploy(t)
	mainGo := filepath.Join(root, "gateway", "cmd", "api-gateway", "main.go")
	cfgGo := filepath.Join(root, "gateway", "internal", "config", "config.go")

	cfgFields := parseConfigFields(t, cfgGo)
	rendered := renderedEnvKeys(t, root)
	sites := scanCompositionRoot(t, mainGo)

	// PREMISE. Each of these three is what makes the verdict mean anything. If
	// any is empty the gate is inspecting nothing, and "no findings" would be a
	// statement about the gate rather than about the tree.
	require.NotEmpty(t, cfgFields,
		"premise failed: no envconfig-tagged fields parsed from config.go — the tag shape changed and this gate now judges nothing")
	require.NotEmpty(t, rendered,
		"premise failed: no KACHO_* env keys found in any chart template — the rendering shape changed and every input would look producerless")
	require.NotEmpty(t, sites,
		"premise failed: no `pkg.New*(cfg.…)` construction sites found in the composition root — the wiring shape changed and this gate now judges nothing")

	findings, considered := findProducerless(sites, cfgFields, rendered)

	// SCOPE EXAMINED — printed always, so "zero findings" is distinguishable
	// from "zero read" (testing.md §Гейт на класс, п.3).
	t.Logf("осмотрено: %d config-полей с envconfig, %d env-ключей рендерится хоть одним шаблоном, "+
		"%d мест сборки в композиционном корне, из них %d читают конфигурацию",
		len(cfgFields), len(rendered), len(sites), considered)
	require.Positive(t, considered,
		"premise failed: no construction site reads an envconfig-bound field — nothing was actually judged")

	require.Empty(t, findings,
		"компонент собран у края, но его не может настроить НИ ОДИН профиль — он будет отказывать всегда, "+
			"и отказ неотличим от «ещё не настроено». Исходов три: дать ключу производителя в профиле, "+
			"вывести значение из уже производимого, либо снять компонент с контракта.\n%s",
		strings.Join(findings, "\n"))
}

// ── инъекция: гейт обязан краснеть на дефекте и молчать на законном близнеце ──

// TestProducerlessGate_CatchesAProducerlessComponent injects the shape the edge
// actually carried: a component every one of whose inputs no profile renders.
func TestProducerlessGate_CatchesAProducerlessComponent(t *testing.T) {
	cfgFields := map[string]configField{
		"GhostIssuer":   {env: "KACHO_API_GATEWAY_GHOST_ISSUER", hasDefault: false},
		"GhostClientID": {env: "KACHO_API_GATEWAY_GHOST_CLIENT_ID", hasDefault: false},
	}
	rendered := map[string]bool{"KACHO_API_GATEWAY_LISTEN_ADDR": true}
	sites := []constructionSite{{
		callee: "middleware.NewGhostHandler", line: 42,
		fields: []string{"GhostClientID", "GhostIssuer"},
	}}

	findings, considered := findProducerless(sites, cfgFields, rendered)
	require.Equal(t, 1, considered)
	require.Len(t, findings, 1, "a component no profile can configure must be a finding")
	require.Contains(t, findings[0], "middleware.NewGhostHandler",
		"the finding must name the component — a gate that cannot say WHERE gets ignored")
	require.Contains(t, findings[0], "KACHO_API_GATEWAY_GHOST_ISSUER",
		"the finding must name the unproducible key, otherwise it cannot be acted on")
}

// TestProducerlessGate_SilentOnALegitimateTwin is the other half. Without it the
// negative above would go green on a gate that flags everything.
func TestProducerlessGate_SilentOnALegitimateTwin(t *testing.T) {
	cfgFields := map[string]configField{
		"ListenAddr":    {env: "KACHO_API_GATEWAY_LISTEN_ADDR", hasDefault: false},
		"OptionalTweak": {env: "KACHO_API_GATEWAY_OPTIONAL_TWEAK", hasDefault: false},
		"WithDefault":   {env: "KACHO_API_GATEWAY_WITH_DEFAULT", hasDefault: true},
	}
	rendered := map[string]bool{"KACHO_API_GATEWAY_LISTEN_ADDR": true}

	// (a) a MIX — one produced input, one not. Configurable ⇒ silent.
	mixed, considered := findProducerless([]constructionSite{{
		callee: "middleware.NewMixedHandler", line: 7,
		fields: []string{"ListenAddr", "OptionalTweak"},
	}}, cfgFields, rendered)
	require.Equal(t, 1, considered)
	require.Empty(t, mixed, "a component that also takes a produced input is configurable — flagging it would make the gate noise")

	// (b) unrendered but with a non-empty default — the binary is the producer.
	defaulted, considered2 := findProducerless([]constructionSite{{
		callee: "middleware.NewDefaultedHandler", line: 9,
		fields: []string{"WithDefault"},
	}}, cfgFields, rendered)
	require.Equal(t, 1, considered2)
	require.Empty(t, defaulted, "a non-empty default IS a producer — the component works with no profile saying anything")
}
