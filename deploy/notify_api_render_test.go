// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_api_render_test.go — развёртывание notify-api (NTF-5 Р2; NTF-4 Р16,
// Р20, Р22) выразимо и сходится с краем в каждой цепочке, где notify поднимается.
//
// Страж старта notify-api отказывает на незаданной ручке («умолчания у неё
// нет»): ручка, которую загрузчик объявил, а чарт не вывел, — под в отказе
// старта на каждой установке, при зелёном рендере. Держатель судит рендер:
// ручка «выведена», когда переменная видна контейнеру `notify-api` — ключом
// карты, которую он берёт `envFrom`, либо записью `env`.
//
// Перепись ручек — разбором загрузчика notify-api (go/parser, теги `envconfig`
// полей Config), а не поиском по тексту. Исключение одно и называется
// правилом, а не именем: ручка с умолчанием загрузчика (тег `default`) —
// окно сужателя, величину которого сверяет с записью политики гейт окон
// отзыва (NTF-5 Р17); вывод её чартом был бы вторым местом одного числа.
//
// Утверждения по каждой цепочке stacks.txt:
//
//	(а) notify-api рендерится ровно там, где рендерится notify-sender: условие
//	    одно (`notify.renders`), второго включателя нет;
//	(б) каждая ручка загрузчика без умолчания видна контейнеру notify-api, её
//	    значение не пусто; ручки с умолчанием чарт не выводит;
//	(в) край знает адрес notify ⇔ notify-api в рендере, и адрес — узел и порт
//	    отрендеренной службы notify-api; ребро mTLS к notify включено;
//	(г) шаблон пода несёт checksum/config своей карты.
//
// Обход, осмотревший ноль цепочек с notify-api либо ноль ручек, —
// «НЕ ВЫПОЛНИЛОСЬ». Способность упасть — инъекции настоящим входом дерева
// (TestNotifyAPIRenderInjections) и отказы рендера на ноге без зонтика
// (TestNotifyAPIRenderRefusesUnsetKnobs).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	notifyAPILoaderDir = "../services/notify/cmd/notify-api/internal/config"
	notifyAPIContainer = "notify-api"
	edgeNotifyAddrEnv  = "KACHO_API_GATEWAY_NOTIFY_INTERNAL_GRPC"
	edgeNotifyMTLSEnv  = "KACHO_API_GATEWAY_MTLS_NOTIFY_ENABLE"
)

// notifyAPILoaderKnobs — ручки загрузчика notify-api: обязательные (без тега
// `default`) и с умолчанием загрузчика.
func notifyAPILoaderKnobs(t *testing.T) (required, defaulted []string) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, notifyAPILoaderDir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: загрузчик notify-api (%s) не разобран: %v", notifyAPILoaderDir, err)
	}
	files := 0
	for _, p := range pkgs {
		for _, f := range p.Files {
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok || ts.Name.Name != "Config" {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, fld := range st.Fields.List {
					if fld.Tag == nil {
						continue
					}
					raw, uerr := strconv.Unquote(fld.Tag.Value)
					if uerr != nil {
						continue
					}
					tag := reflect.StructTag(raw)
					env := tag.Get("envconfig")
					if env == "" {
						continue
					}
					if _, def := tag.Lookup("default"); def {
						defaulted = append(defaulted, env)
					} else {
						required = append(required, env)
					}
				}
				return false
			})
		}
	}
	if files == 0 || len(required) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файлов загрузчика notify-api %d, обязательных ручек %d — перепись слепа", files, len(required))
	}
	sort.Strings(required)
	sort.Strings(defaulted)
	return required, defaulted
}

// notifyAPIView — вид рендера одной цепочки: под notify-api, его служба,
// notify-sender и адрес notify у края.
type notifyAPIView struct {
	apiDeployments int
	senders        int
	vars           map[string]string // переменные контейнера notify-api
	checksum       string
	serviceAddr    string // <имя>.<ns>.svc:<порт> службы notify-api
	edgeAddr       string // KACHO_API_GATEWAY_NOTIFY_INTERNAL_GRPC
	edgeMTLS       string // KACHO_API_GATEWAY_MTLS_NOTIFY_ENABLE
	edgeSeen       bool
}

func notifyAPIViewOf(t *testing.T, rendered, ns string) notifyAPIView {
	t.Helper()
	v := notifyAPIView{vars: map[string]string{}}
	all := parseRendered(t, rendered)
	var mine []renderedObj
	for _, o := range all {
		if strings.Contains(o.source, "/charts/notify/") || strings.HasPrefix(o.source, "notify/") {
			mine = append(mine, o)
		}
	}
	maps := map[string]map[string]any{}
	for _, cm := range objsOfKind(mine, "ConfigMap") {
		if d, ok := ndig(cm.doc, "data").(map[string]any); ok {
			maps[cm.name] = d
		}
	}
	for _, d := range objsOfKind(mine, "Deployment") {
		for _, c := range nlist(nPodSpec(d)["containers"]) {
			cm, _ := c.(map[string]any)
			switch nstr(cm["name"]) {
			case "notify":
				v.senders++
			case notifyAPIContainer:
				v.apiDeployments++
				v.checksum = nstr(ndig(d.doc, "spec", "template", "metadata", "annotations", "checksum/config"))
				for _, ef := range nlist(cm["envFrom"]) {
					for k, val := range maps[nstr(ndig(ef, "configMapRef", "name"))] {
						v.vars[k] = nstr(val)
					}
				}
				for _, e := range nlist(cm["env"]) {
					m, _ := e.(map[string]any)
					if m["valueFrom"] != nil {
						v.vars[nstr(m["name"])] = "<ref>"
					} else {
						v.vars[nstr(m["name"])] = nstr(m["value"])
					}
				}
			}
		}
	}
	for _, s := range objsOfKind(mine, "Service") {
		if nstr(ndig(s.doc, "metadata", "labels", "app.kubernetes.io/component")) != "notify-api" {
			continue
		}
		for _, p := range nlist(ndig(s.doc, "spec", "ports")) {
			if nstr(ndig(p, "name")) == "grpc-internal" {
				port, _ := ndig(p, "port").(int)
				v.serviceAddr = s.name + "." + ns + ".svc:" + strconv.Itoa(port)
			}
		}
	}
	for _, d := range objsOfKind(all, "Deployment") {
		if !strings.Contains(d.source, "/charts/api-gateway/") {
			continue
		}
		for _, c := range nlist(nPodSpec(d)["containers"]) {
			cm, _ := c.(map[string]any)
			for _, e := range nlist(cm["env"]) {
				m, _ := e.(map[string]any)
				switch nstr(m["name"]) {
				case edgeNotifyAddrEnv:
					v.edgeAddr = nstr(m["value"])
				case edgeNotifyMTLSEnv:
					v.edgeMTLS = nstr(m["value"])
				}
			}
			v.edgeSeen = true
		}
	}
	return v
}

// judgeNotifyAPI — РЕШЕНИЕ держателя по виду цепочки: находки «предмет — почему».
func judgeNotifyAPI(required, defaulted []string, v notifyAPIView) []string {
	var out []string
	if v.apiDeployments != v.senders {
		out = append(out, "(а) notify-api рендерится "+strconv.Itoa(v.apiDeployments)+" раз, notify-sender — "+
			strconv.Itoa(v.senders)+": условие у развёртываний одно (`notify.renders`)")
	}
	if v.apiDeployments > 0 {
		for _, env := range required {
			val, ok := v.vars[env]
			switch {
			case !ok:
				out = append(out, env+": (б) рендер ручку не выводит — страж старта notify-api откажет «ручка не задана»")
			case strings.TrimSpace(val) == "":
				out = append(out, env+": (б) значение пусто — страж старта notify-api отвергнет его с именем ручки")
			}
		}
		for _, env := range defaulted {
			if _, ok := v.vars[env]; ok {
				out = append(out, env+": (б) ручка с умолчанием загрузчика выведена чартом — второе место величины, которую сверяет гейт окон отзыва (NTF-5 Р17)")
			}
		}
		if v.checksum == "" {
			out = append(out, "(г) шаблон пода notify-api без checksum/config — смена карты не перекатит под")
		}
	}
	if !v.edgeSeen {
		return out
	}
	switch {
	case v.apiDeployments > 0 && v.edgeAddr == "":
		out = append(out, edgeNotifyAddrEnv+": (в) notify-api в рендере, а край адреса notify не знает — NoticeService наружу не выйдет")
	case v.apiDeployments == 0 && v.edgeAddr != "":
		out = append(out, edgeNotifyAddrEnv+": (в) край зовёт notify ("+v.edgeAddr+"), а notify-api в рендере нет")
	case v.apiDeployments > 0 && v.edgeAddr != v.serviceAddr:
		out = append(out, edgeNotifyAddrEnv+": (в) адрес у края "+v.edgeAddr+" ≠ служба notify-api "+v.serviceAddr)
	}
	if v.edgeAddr != "" && v.edgeMTLS != "true" {
		out = append(out, edgeNotifyMTLSEnv+": (в) адрес notify у края без ребра mTLS — слушатель notify-api принимает только mTLS")
	}
	return out
}

// notifyAPIChainViews — вид каждой цепочки stacks.txt (рендер — цепочками гейта).
func notifyAPIChainViews(t *testing.T, c umbrellaCopy) map[string]notifyAPIView {
	t.Helper()
	stacks := deployStacksForRender(t, renderGateOperatorSample)
	out := map[string]notifyAPIView{}
	for n, chain := range stacks {
		rendered, err := renderStandProfile(t, c, chain)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер цепочки %s отказал: %v\n%s", n, err, lastLines(rendered, 5))
		}
		out[n] = notifyAPIViewOf(t, rendered, "kacho")
	}
	return out
}

// TestNotifyAPIRendersWithEveryLoaderKnobAndMeetsTheEdge — держатель на дереве как оно есть.
func TestNotifyAPIRendersWithEveryLoaderKnobAndMeetsTheEdge(t *testing.T) {
	required, defaulted := notifyAPILoaderKnobs(t)
	views := notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}))
	var names []string
	rendered := 0
	for n, v := range views {
		names = append(names, n)
		if v.apiDeployments > 0 {
			rendered++
		}
	}
	sort.Strings(names)
	t.Logf("ручек загрузчика notify-api: обязательных %d, с умолчанием %d (%s); цепочек %d, notify-api рендерится в %d",
		len(required), len(defaulted), strings.Join(defaulted, ", "), len(views), rendered)
	if rendered == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: ни в одной цепочке notify-api не рендерится — судить нечего")
	}
	for _, n := range names {
		v := views[n]
		f := judgeNotifyAPI(required, defaulted, v)
		t.Logf("  %s: notify-api %d, notify-sender %d, переменных %d, служба %q, адрес у края %q, mTLS %q, находок %d",
			n, v.apiDeployments, v.senders, len(v.vars), v.serviceAddr, v.edgeAddr, v.edgeMTLS, len(f))
		for _, x := range f {
			t.Errorf("КРАСНЫЙ: %s: %s", n, x)
		}
	}
}

// TestNotifyAPIRenderInjections — держатель краснеет на настоящем входе дерева,
// и находка называет ровно внесённый дефект.
func TestNotifyAPIRenderInjections(t *testing.T) {
	required, defaulted := notifyAPILoaderKnobs(t)
	expect := func(name, prefix string, views map[string]notifyAPIView, chains ...string) {
		t.Helper()
		for _, chain := range chains {
			v, ok := views[chain]
			if !ok {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочки %s в таблице нет", chain)
			}
			hit := false
			for _, f := range judgeNotifyAPI(required, defaulted, v) {
				if strings.HasPrefix(f, prefix) {
					hit = true
					t.Logf("инъекция «%s» → %s: %s", name, chain, f)
				}
			}
			if !hit {
				t.Errorf("инъекция «%s»: держатель промолчал о %s в цепочке %s", name, prefix, chain)
			}
		}
	}

	// Близнец — дерево как есть: находок нет.
	for chain, v := range notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{})) {
		if f := judgeNotifyAPI(required, defaulted, v); len(f) != 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец %s красный — инъекции недействительны: %v", chain, f)
		}
	}

	// Снятая строка карты — ручка звена прав и ручка извещений.
	for _, env := range []string{"KACHO_NOTIFY_AUTHZ_CACHE_TTL", "KACHO_NOTIFY_NOTICE_REMINDER_LEAD"} {
		views := notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
			"templates/api-configmap.yaml": dropLineWith("  " + env + ":"),
		}}))
		expect("снята строка "+env, env+":", views, "dev", "dev-prod")
	}

	// Ручка с умолчанием загрузчика выведена картой.
	views := notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
		"templates/api-configmap.yaml": replaceOnce(
			`  KACHO_NOTIFY_NOTICE_REMINDER_LEAD:`,
			`  KACHO_NOTIFY_LIST_FILTER_CACHE_TTL: "5s"
  KACHO_NOTIFY_NOTICE_REMINDER_LEAD:`),
	}}))
	expect("окно сужателя выведено картой", "KACHO_NOTIFY_LIST_FILTER_CACHE_TTL:", views, "dev")

	// Край без адреса notify при рендерящемся notify-api.
	views = notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.dev.yaml": replaceOnce(`    notifyInternal: kacho-notify-api.kacho.svc:9091`, `    notifyInternal: ""`),
	}}))
	expect("адрес notify у края снят", edgeNotifyAddrEnv+":", views, "dev", "dev-prod")

	// Край зовёт notify там, где notify-api нет (снято снятие адреса в prorobotech).
	views = notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.prorobotech.yaml": replaceOnce(`    notifyInternal: ""`, `    notifyInternal: kacho-notify-api.kacho.svc:9091`),
	}}))
	expect("адрес notify без notify-api", edgeNotifyAddrEnv+":", views, "prorobotech")

	// Адрес у края расходится со службой notify-api.
	views = notifyAPIChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.dev.yaml": replaceOnce(`    notifyInternal: kacho-notify-api.kacho.svc:9091`, `    notifyInternal: kacho-notify-api.kacho.svc:9095`),
	}}))
	expect("адрес у края не тот", edgeNotifyAddrEnv+":", views, "dev")
}

// TestNotifyAPIRenderRefusesUnsetKnobs — на ноге без зонтика каждая ручка
// notify-api без значения — отказ рендера с её именем (без умолчаний в
// шаблоне); пустой круг пересылающих — тоже. Близнец — нога как есть
// рендерит notify-api с величиной окна из values.
func TestNotifyAPIRenderRefusesUnsetKnobs(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	objs := mustRenderNotify(t, chart, standaloneLeg())
	got := ""
	for _, cm := range objsOfKind(objs, "ConfigMap") {
		if strings.HasSuffix(cm.name, "-api-config") {
			got = nstr(ndig(cm.doc, "data", "KACHO_NOTIFY_AUTHZ_CACHE_TTL"))
		}
	}
	if got != "5s" {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец — окно звена прав в карте notify-api %q, ожидалось 5s из values ноги", got)
	}
	for _, c := range []struct{ set, want string }{
		{"authz.cacheTTL=", "notify.authz.cacheTTL"},
		{"authz.checkTimeout=", "notify.authz.checkTimeout"},
		{"authz.denyBudgetPerSec=", "notify.authz.denyBudgetPerSec"},
		{"authz.iamGRPCAddr=", "notify.authz.iamGRPCAddr"},
		{"handlingBudget=", "notify.handlingBudget"},
		{"notice.reminderLead=", "notify.notice.reminderLead"},
		{"authz.trustedForwarderSANs=null", "notify.authz.trustedForwarderSANs"},
		{"api.networkPolicy.edgePodLabels=null", "api.networkPolicy.edgePodLabels"},
	} {
		out, err := renderNotify(t, chart, standaloneLeg(), c.set)
		switch {
		case err == nil:
			t.Errorf("КРАСНЫЙ: --set %s — рендер прошёл, отказа нет", c.set)
		case !strings.Contains(out, c.want):
			t.Errorf("КРАСНЫЙ: --set %s — отказ без имени ручки %s:\n%s", c.set, c.want, lastLines(out, 3))
		default:
			t.Logf("--set %s → отказ рендера: %s", c.set, lineWith(out, c.want))
		}
	}
}
