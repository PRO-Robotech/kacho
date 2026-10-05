// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_client_address_circle_render_test.go — ЗАГОЛОВКУ АДРЕСА КЛИЕНТА КРАЙ
// ДОВЕРЯЕТ ТОЛЬКО ОТ ЗВЕНА ФРОНТА (kacho#3028).
//
// Край принимает `X-Forwarded-For` только от TCP-пира, который лежит в сети
// круга (KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS) и назван поимённо (п. 5).
// Сеть сама по себе выделяет «под кластера», а не «раздачу консоли». Звено
// фронта выделяют два замка — имя службы звена (п. 5) и политика сети на поде
// края: до порта, принимающего пересылку, доходят
// только поды, названные МЕТКАМИ (раздача консоли, контроллер входа, явно
// объявленные поды администрирования). Без политики любой под кластера,
// дошедший до края напрямую, одной строкой заголовка подменял бы `client_ip` и
// ключ ограничения частоты входа у службы доступа.
//
// Судится рендер каждой цепочки deploy/stacks.txt тем же вызовом helm, которым
// стенд поднимается (npChainDocs):
//
//  1. ручка у Deployment края есть, и её значение — законный круг
//     (pkg/proxycircle: каждая запись — сеть внутри частных диапазонов);
//  2. круг пуст — «никому»: заголовок не принимается ни от кого, политика не
//     требуется;
//  3. круг непуст — под края выбран политикой сети на вход, и КАЖДОЕ правило,
//     открывающее порт контейнера края, впускает ТОЛЬКО звенья фронта.
//     Политика читается разбором гейта политик сети: policyTypes опущен либо
//     пуст — правила входа действуют; порт — имя, число либо диапазон
//     `port`..`endPort`, по протоколу. Звенья фронта выводятся из рендера, а
//     не выписываются (edgeFrontLinks): тот, кто звонит краю по адресу из
//     своих настроек, и контроллер входа, ведущего на край. Находки: правило
//     без отправителей, селектор всех подов пространства, селектор
//     пространства, блок адресов, селектор, не выбирающий ни одного пода
//     рендера (метку навесит кто угодно), и селектор, выбирающий хоть один под,
//     не являющийся звеном. Порт, которого контейнер края не объявляет (сбор
//     величин), не судится: адреса клиента он не принимает;
//  4. цепочка a8f60d — стенд предмета: за раздачей консоли без круга все
//     клиенты снова стали бы одним источником, поэтому её круг непуст, и
//     звено фронта из её рендера выводится;
//  5. УЗКИЙ КРУГ (круг 3): сеть подов общая, и сеть круга — «под кластера», а
//     не «звено». Звено край узнаёт поимённо (KACHO_API_GATEWAY_AUTHZ_TRUSTED_
//     PROXY_PEERS — имена безголовых служб, чьи поды и только они звенья).
//     Круг непуст — имена непусты, и наоборот (pkg/proxycircle.ParsePeers —
//     тот же разбор, что у края); каждое имя — безголовая служба рендера с
//     селектором, выбирающим хоть один под и только звенья фронта; каждое
//     звено выбрано хоть одной из них (judgeEdgePeers);
//  6. адрес клиента ДОХОДИТ до звена: внешняя служба (LoadBalancer, NodePort)
//     перед звеном фронта несёт externalTrafficPolicy Local — при Cluster
//     адрес источника подменяется адресом узла (judgeFrontExternalPolicy).
//
// Что раздача консоли до порта края ДОХОДИТ, держит гейт политик сети
// (network_policy_admission_render_test.go, половина «достижимость»): она
// звонит краю по адресу из своих настроек.
//
// ЗНАМЕНАТЕЛЬ — цепочки с Deployment края, цепочки с непустым кругом,
// сверенные селекторы отправителей и службы звеньев: ноль любого — судить было
// нечего; ноль вторых, третьих или четвёртых — замок «только фронт» не
// осмотрен ни разу.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/proxycircle"
)

const (
	edgeCircleKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS"
	// edgePeersKnob — звенья фронта поимённо: безголовые службы, чьи поды край
	// признаёт звеньями (kacho#3028, круг 3).
	edgePeersKnob = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_PEERS"
	// circleSubjectStack — стенд, на котором дефект наблюдался: консоль за
	// внешним входом, адрес клиента к краю несёт раздача.
	circleSubjectStack = "a8f60d"
)

// edgeAdmission — что рендер одной цепочки говорит о доверии краю.
type edgeAdmission struct {
	Found, Declared bool
	Circle          string
	Trusting        bool     // круг непуст
	Policies        int      // политик на вход, выбирающих под края
	RulesJudged     int      // правил, открывающих порт пересылки
	Links           []string // звенья фронта, выведенные из рендера
	SendersJudged   int      // селекторов отправителей, сверенных со звеньями
	Peers           string   // значение ручки звеньев поимённо
	PeersJudged     int      // служб звеньев, сверенных со звеньями фронта
	ExternalJudged  int      // внешних служб перед звеньями, сверенных по политике трафика
}

// edgeCircleOf — значение ручки круга у контейнеров края.
func edgeCircleOf(edge npWorkload) (string, bool) { return edgeEnvOf(edge, edgeCircleKnob) }

// edgeEnvOf — значение ручки у контейнеров края.
func edgeEnvOf(edge npWorkload, knob string) (string, bool) {
	cs, _ := edge.spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, e := range slice(cm, "env") {
			em, _ := e.(map[string]any)
			if str(em, "name") == knob {
				return str(em, "value"), true
			}
		}
	}
	return "", false
}

// judgeEdgePeers — УЗКИЙ КРУГ (kacho#3028, круг 3): сеть круга выделяет «под
// кластера», звено фронта край узнаёт поимённо — по адресам подов, которые
// выбирают названные безголовые службы. Находки:
//
//   - имени нет среди служб рендера (край разрешал бы имя, которого установка
//     не заводит: звеньев нет, либо их заведёт кто угодно этим именем);
//   - служба не безголовая (имя разрешалось бы в адрес службы, а не подов);
//   - селектор службы пуст, не выбирает ни одного пода рендера либо выбирает
//     под, не являющийся звеном фронта;
//   - звено фронта не выбрано ни одной названной службой (его заголовок край
//     не примет: все его клиенты — один источник).
func judgeEdgePeers(peers []string, works []npWorkload, links map[string]bool, docs []map[string]any,
	a *edgeAdmission, say func(string, ...any)) {
	svcs := map[string]map[string]any{}
	for _, d := range docs {
		if docKind(d) == "Service" {
			svcs[docName(d)] = d
		}
	}
	covered := map[string]bool{}
	for _, name := range peers {
		short, _, _ := strings.Cut(name, ".")
		d, ok := svcs[short]
		if !ok {
			say("звено %q (%s) не названо ни одной службой рендера — край разрешал бы имя, которого установка "+
				"не заводит", name, edgePeersKnob)
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		if str(spec, "clusterIP") != "None" {
			say("служба звена %s не безголовая (clusterIP %q) — имя разрешается в адрес службы, а не подов фронта",
				short, str(spec, "clusterIP"))
			continue
		}
		raw, _ := spec["selector"].(map[string]any)
		sel, err := parseSelector(map[string]any{"matchLabels": raw})
		if err != nil || len(sel.match) == 0 {
			say("служба звена %s без селектора — её адреса заводит кто угодно, а не поды фронта", short)
			continue
		}
		a.PeersJudged++
		var chosen []string
		for _, w := range works {
			if sel.matches(w.labels) {
				chosen = append(chosen, w.id())
			}
		}
		if len(chosen) == 0 {
			say("служба звена %s: селектор %s не выбирает ни одного пода рендера — эти метки навесит любой под "+
				"пространства, и край признает его звеном", short, sel)
		}
		for _, id := range chosen {
			if !links[id] {
				say("служба звена %s: селектор %s выбирает %s — не звено фронта: его заголовок адреса край примет",
					short, sel, id)
				continue
			}
			covered[id] = true
		}
	}
	for l := range links {
		if !covered[l] {
			say("звено фронта %s не выбрано ни одной службой из %s — его заголовок адреса край не примет, и все "+
				"его клиенты — один источник", l, edgePeersKnob)
		}
	}
}

// judgeFrontExternalPolicy — адрес клиента обязан ДОЙТИ до звена (kacho#3028):
// внешняя служба (LoadBalancer, NodePort), выбирающая под звена фронта, с
// политикой `Cluster` (умолчание) подменяет адрес источника адресом узла, и
// звено пишет в заголовок адрес узла — все клиенты снова один источник.
func judgeFrontExternalPolicy(works []npWorkload, links map[string]bool, docs []map[string]any,
	a *edgeAdmission, say func(string, ...any)) {
	for _, d := range docs {
		if docKind(d) != "Service" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		typ := str(spec, "type")
		if typ != "LoadBalancer" && typ != "NodePort" {
			continue
		}
		raw, _ := spec["selector"].(map[string]any)
		sel, err := parseSelector(map[string]any{"matchLabels": raw})
		if err != nil || len(sel.match) == 0 {
			continue
		}
		for _, w := range works {
			if !links[w.id()] || !sel.matches(w.labels) {
				continue
			}
			a.ExternalJudged++
			if p := str(spec, "externalTrafficPolicy"); p != "Local" {
				say("внешняя служба %s (%s) перед звеном фронта %s: externalTrafficPolicy %q, ожидался Local — "+
					"адрес клиента подменяется адресом узла, и все клиенты — один источник", docName(d), typ, w.id(),
					orDash(p))
			}
			break
		}
	}
}

// edgePort — порт контейнера края: номер и имя.
type edgePort struct {
	num  int
	name string
}

func (p edgePort) String() string { return fmt.Sprint(p.num) }

// edgePortsOf — порты, объявленные контейнерами края. Каждый из них принимает
// запрос, а с ним заголовок адреса; порт, которого контейнер не объявляет
// (сбор величин), не судится.
func edgePortsOf(edge npWorkload) []edgePort {
	var out []edgePort
	cs, _ := edge.spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, p := range slice(cm, "ports") {
			pm, _ := p.(map[string]any)
			out = append(out, edgePort{num: toInt(pm["containerPort"]), name: str(pm, "name")})
		}
	}
	return out
}

// edgeFrontLinks — звенья фронта, выведенные из рендера, а не выписанные:
//
//   - рабочий объект, в настройках которого назван адрес службы края (раздача
//     консоли: `uif.host.upstreams.apiGateway`);
//   - контроллер входа того класса, у которого есть вход (Ingress), ведущий на
//     службу края. Контроллер узнаётся по аргументу `--ingress-class=<класс>`.
//
// Иных звеньев нет: заголовок адреса пишет только то, что стоит между
// клиентом и краем.
func edgeFrontLinks(ns string, edge npWorkload, works []npWorkload, services map[string]npService,
	maps map[string]map[string]any, docs []map[string]any) map[string]bool {
	edgeSvc := map[string]bool{}
	for name, svc := range services {
		if svc.selector.matches(edge.labels) {
			edgeSvc[name] = true
		}
	}
	links := map[string]bool{}
	for _, w := range works {
		if w.id() == edge.id() {
			continue
		}
		for _, hp := range npDialsOf(w, ns, maps) {
			if !edgeSvc[hp.host] {
				continue
			}
			for _, sp := range services[hp.host].ports {
				if sp.port == hp.port {
					links[w.id()] = true
				}
			}
		}
	}
	classes := map[string]bool{}
	for _, d := range docs {
		if docKind(d) != "Ingress" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		class := str(spec, "ingressClassName")
		if class == "" {
			class = stringMap(submap(d, "metadata")["annotations"])["kubernetes.io/ingress.class"]
		}
		backends := []any{submap(spec, "defaultBackend")}
		for _, r := range slice(spec, "rules") {
			for _, p := range slice(submap(r.(map[string]any), "http"), "paths") {
				backends = append(backends, submap(p.(map[string]any), "backend"))
			}
		}
		for _, b := range backends {
			bm, _ := b.(map[string]any)
			if edgeSvc[str(submap(bm, "service"), "name")] && class != "" {
				classes[class] = true
			}
		}
	}
	for _, w := range works {
		cs, _ := w.spec["containers"].([]any)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			for _, a := range slice(cm, "args") {
				if v, ok := strings.CutPrefix(fmt.Sprint(a), "--ingress-class="); ok && classes[v] {
					links[w.id()] = true
				}
			}
		}
	}
	return links
}

// judgeEdgeAdmission — находки по документам одной цепочки. Чистая функция.
//
// Политика читается тем же разбором, что у гейта политик сети (parsePolicy):
// policyTypes опущен либо пуст — правила входа действуют (умолчание API);
// порт задаётся именем, числом либо диапазоном `port`..`endPort`, по
// протоколу. Отправитель судится по тому, КОГО он выбирает среди подов
// рендера: каждый выбранный обязан быть звеном фронта (edgeFrontLinks), и
// выбран обязан быть хоть один — селектор, не выбирающий никого из рендера,
// впускает любой под пространства, навесивший эти метки.
func judgeEdgeAdmission(stack string, docs []map[string]any) ([]string, edgeAdmission) {
	var out []string
	var a edgeAdmission
	say := func(f string, args ...any) {
		out = append(out, fmt.Sprintf("цепочка %s: ", stack)+fmt.Sprintf(f, args...))
	}
	works, services, maps, policies, err := npIndexDocs(npNamespace, docs)
	var edge npWorkload
	for _, w := range works {
		if w.kind == "Deployment" && w.name == edgeDeploymentName {
			edge, a.Found = w, true
		}
	}
	if !a.Found {
		return nil, a
	}
	if err != nil {
		say("политики сети не читаются: %v — судить доверие краю не по чему", err)
		return out, a
	}
	a.Circle, a.Declared = edgeCircleOf(edge)
	if !a.Declared {
		say("у края нет %s", edgeCircleKnob)
		return out, a
	}
	circle, err := proxycircle.Parse(a.Circle)
	if err != nil {
		say("круг края не законен: %v", err)
		return out, a
	}
	a.Trusting = len(circle) > 0
	a.Peers, _ = edgeEnvOf(edge, edgePeersKnob)
	peers, err := proxycircle.ParsePeers(a.Peers)
	if err != nil {
		say("звенья края поимённо не законны (%s): %v", edgePeersKnob, err)
		return out, a
	}
	if !a.Trusting {
		if len(peers) != 0 {
			say("круг пуст, а звенья поимённо объявлены (%s=%q) — край откажет в старте: доверию нечем исполниться",
				edgePeersKnob, a.Peers)
		}
		return out, a
	}
	links := edgeFrontLinks(npNamespace, edge, works, services, maps, docs)
	for l := range links {
		a.Links = append(a.Links, l)
	}
	sort.Strings(a.Links)
	if len(peers) == 0 {
		say("круг %s непуст, а звеньев поимённо нет (%s) — заголовку адреса доверяла бы вся сеть подов", a.Circle,
			edgePeersKnob)
	} else {
		judgeEdgePeers(peers, works, links, docs, &a, say)
	}
	judgeFrontExternalPolicy(works, links, docs, &a, say)
	ports := edgePortsOf(edge)
	raw := map[string]map[string]any{}
	for _, d := range docs {
		if docKind(d) == "NetworkPolicy" {
			raw[docName(d)] = d
		}
	}
	for _, p := range policies {
		if !p.ingress || !p.target.matches(edge.labels) {
			continue
		}
		a.Policies++
		rawRules := slice(submap(raw[p.name], "spec"), "ingress")
		for _, r := range p.inRules {
			var opened []string
			for _, ep := range ports {
				if r.admitsPort(ep.num, ep.name) {
					opened = append(opened, ep.String())
				}
			}
			if len(opened) == 0 {
				continue
			}
			sort.Strings(opened)
			if len(r.ports) == 0 {
				opened = []string{"все"}
			}
			a.RulesJudged++
			where := fmt.Sprintf("политика %s, правило %d (порты %s)", p.name, r.index, strings.Join(opened, ","))
			if r.anyPeer {
				say("%s открыто всем: правило без отправителей — заголовок адреса от любого пира в круге %s "+
					"принимается краем", where, a.Circle)
				continue
			}
			if r.index >= len(rawRules) {
				say("%s: правило в документе политики не найдено — отправители не прочитаны", where)
				continue
			}
			rm, _ := rawRules[r.index].(map[string]any)
			for _, peer := range slice(rm, "from") {
				pm, _ := peer.(map[string]any)
				switch {
				case pm["ipBlock"] != nil:
					say("%s впускает блок адресов — звено называется метками пода, а не сетью", where)
					continue
				case pm["namespaceSelector"] != nil:
					say("%s впускает по селектору пространства — отправитель вне пространства рендером не назван", where)
					continue
				}
				sel, err := parseSelector(pm["podSelector"])
				if err != nil || len(sel.match)+len(sel.exprs) == 0 {
					say("%s впускает все поды пространства — заголовок адреса принимается от любого из них", where)
					continue
				}
				a.SendersJudged++
				var chosen []string
				for _, w := range works {
					if sel.matches(w.labels) {
						chosen = append(chosen, w.id())
					}
				}
				if len(chosen) == 0 {
					say("%s: селектор отправителя %s не выбирает ни одного пода рендера — эти метки навесит любой "+
						"под пространства, и заголовок адреса от него край примет", where, sel)
				}
				for _, id := range chosen {
					if !links[id] {
						say("%s: селектор %s впускает %s — не звено фронта: к краю он не звонит, а заголовок "+
							"адреса от него край примет", where, sel, id)
					}
				}
			}
		}
	}
	if a.Policies == 0 {
		say("круг %s непуст, а под края не выбран ни одной политикой сети на вход — любой под кластера, "+
			"дошедший до края, подменяет адрес клиента одной строкой заголовка", a.Circle)
	}
	return out, a
}

func TestEveryStackTrustsTheClientAddressOnlyFromTheFront(t *testing.T) {
	stacks := deployStacks(t)
	var edges, trusting, rules, senders, peerSvcs int
	subjectTrusting, subjectLinks := false, 0
	for _, n := range sortedStackNames(stacks) {
		findings, a := judgeEdgeAdmission(n, npChainDocs(t, n))
		if !a.Found {
			continue
		}
		edges++
		if a.Trusting {
			trusting++
			rules += a.RulesJudged
			senders += a.SendersJudged
			peerSvcs += a.PeersJudged
		}
		if n == circleSubjectStack {
			subjectTrusting = a.Trusting
			subjectLinks = len(a.Links)
		}
		t.Logf("цепочка %-11s: %s=%q · %s=%q · политик на край %d · правил с портом пересылки %d · "+
			"отправителей сверено %d · служб звеньев сверено %d · внешних служб сверено %d · звенья фронта %v · находок %d",
			n, edgeCircleKnob, a.Circle, edgePeersKnob, a.Peers, a.Policies, a.RulesJudged, a.SendersJudged,
			a.PeersJudged, a.ExternalJudged, a.Links, len(findings))
		for _, f := range findings {
			t.Error(f)
		}
	}
	t.Logf("перепись: цепочек %d · с краем %d · с непустым кругом %d · правил осмотрено %d · отправителей сверено %d · "+
		"служб звеньев сверено %d", len(stacks), edges, trusting, rules, senders, peerSvcs)
	if edges == 0 {
		t.Fatal("ни одна цепочка не рендерит края — судить нечего")
	}
	if _, ok := stacks[circleSubjectStack]; !ok {
		t.Fatalf("цепочки %s в таблице нет — предпосылка пробы исчезла", circleSubjectStack)
	}
	if !subjectTrusting {
		t.Errorf("цепочка %s: круг края пуст — за раздачей консоли все клиенты один источник для ограничения "+
			"частоты входа (kacho#3028)", circleSubjectStack)
	}
	if subjectTrusting && subjectLinks == 0 {
		t.Errorf("цепочка %s: круг непуст, а ни одного звена фронта из рендера не выведено — раздача консоли "+
			"адреса края в настройках не несёт, и «только фронт» сверять не с чем", circleSubjectStack)
	}
	if trusting == 0 {
		t.Fatal("ни у одной цепочки круг не непуст — замок «только фронт» не осмотрен ни разу")
	}
	if senders == 0 {
		t.Fatal("круг непуст, а ни один селектор отправителя не сверен со звеньями фронта — «только фронт» не осмотрен")
	}
	if peerSvcs == 0 {
		t.Fatal("круг непуст, а ни одна служба звеньев поимённо не сверена — узкий круг не осмотрен ни разу")
	}
}
