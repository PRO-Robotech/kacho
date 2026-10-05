// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_client_address_circle_render_test.go — ЗАГОЛОВКУ АДРЕСА КЛИЕНТА КРАЙ
// ДОВЕРЯЕТ ТОЛЬКО ОТ ЗВЕНА ФРОНТА (kacho#3028).
//
// Край принимает `X-Forwarded-For` только от TCP-пира из круга
// (KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS). Сеть — всё, что край о пире
// знает, и сеть подов у каждого кластера своя: круг сам по себе выделяет «под
// кластера», а не «раздачу консоли». Звено фронта выделяется вторым замком —
// политикой сети на поде края: до порта, принимающего пересылку, доходят
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
//     звено фронта из её рендера выводится.
//
// Что раздача консоли до порта края ДОХОДИТ, держит гейт политик сети
// (network_policy_admission_render_test.go, половина «достижимость»): она
// звонит краю по адресу из своих настроек.
//
// ЗНАМЕНАТЕЛЬ — цепочки с Deployment края, цепочки с непустым кругом и
// сверенные селекторы отправителей: ноль любого — судить было нечего; ноль
// вторых или третьих — замок «только фронт» не осмотрен ни разу.
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
}

// edgeCircleOf — значение ручки круга у контейнеров края.
func edgeCircleOf(edge npWorkload) (string, bool) {
	cs, _ := edge.spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, e := range slice(cm, "env") {
			em, _ := e.(map[string]any)
			if str(em, "name") == edgeCircleKnob {
				return str(em, "value"), true
			}
		}
	}
	return "", false
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
	if !a.Trusting {
		return out, a
	}
	links := edgeFrontLinks(npNamespace, edge, works, services, maps, docs)
	for l := range links {
		a.Links = append(a.Links, l)
	}
	sort.Strings(a.Links)
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
	var edges, trusting, rules, senders int
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
		}
		if n == circleSubjectStack {
			subjectTrusting = a.Trusting
			subjectLinks = len(a.Links)
		}
		t.Logf("цепочка %-11s: %s=%q · политик на край %d · правил с портом пересылки %d · "+
			"отправителей сверено %d · звенья фронта %v · находок %d",
			n, edgeCircleKnob, a.Circle, a.Policies, a.RulesJudged, a.SendersJudged, a.Links, len(findings))
		for _, f := range findings {
			t.Error(f)
		}
	}
	t.Logf("перепись: цепочек %d · с краем %d · с непустым кругом %d · правил осмотрено %d · отправителей сверено %d",
		len(stacks), edges, trusting, rules, senders)
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
}
