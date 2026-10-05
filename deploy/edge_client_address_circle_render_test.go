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
//     открывающее порт пересылки (именованный порт контейнера края либо все
//     порты разом), называет отправителей метками пода: правило без
//     отправителей, селектор всех подов пространства, селектор пространства и
//     блок адресов — находки. Порт, которого у контейнера края нет (сбор
//     величин), не судится: адреса клиента он не принимает;
//  4. цепочка a8f60d — стенд предмета: за раздачей консоли без круга все
//     клиенты снова стали бы одним источником, поэтому её круг непуст.
//
// Что раздача консоли до порта края ДОХОДИТ, держит гейт политик сети
// (network_policy_admission_render_test.go, половина «достижимость»): она
// звонит краю по адресу из своих настроек.
//
// ЗНАМЕНАТЕЛЬ — цепочки с Deployment края и, отдельно, цепочки с непустым
// кругом: ноль первых — судить было нечего, ноль вторых — замок «только
// фронт» не осмотрен ни разу.
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
	Trusting        bool // круг непуст
	Policies        int  // политик на вход, выбирающих под края
	RulesJudged     int  // правил, открывающих порт пересылки
}

func edgeContainer(d map[string]any) (labels map[string]string, env map[string]string, ports map[string]bool) {
	tpl, _ := podTemplateOf(d)
	tm, _ := tpl["metadata"].(map[string]any)
	labels = stringMap(tm["labels"])
	env, ports = map[string]string{}, map[string]bool{}
	spec, _ := tpl["spec"].(map[string]any)
	for _, c := range slice(spec, "containers") {
		cm, _ := c.(map[string]any)
		for _, e := range slice(cm, "env") {
			em, _ := e.(map[string]any)
			if n := str(em, "name"); n != "" {
				env[n] = str(em, "value")
			}
		}
		for _, p := range slice(cm, "ports") {
			pm, _ := p.(map[string]any)
			if n := str(pm, "name"); n != "" {
				ports[n] = true
			}
			ports[fmt.Sprint(toInt(pm["containerPort"]))] = true
		}
	}
	return labels, env, ports
}

// judgeEdgeAdmission — находки по документам одной цепочки. Чистая функция.
func judgeEdgeAdmission(stack string, docs []map[string]any) ([]string, edgeAdmission) {
	var out []string
	var a edgeAdmission
	say := func(f string, args ...any) {
		out = append(out, fmt.Sprintf("цепочка %s: ", stack)+fmt.Sprintf(f, args...))
	}
	var labels map[string]string
	var ports map[string]bool
	for _, d := range docs {
		if docKind(d) != "Deployment" || docName(d) != edgeDeploymentName {
			continue
		}
		a.Found = true
		var env map[string]string
		labels, env, ports = edgeContainer(d)
		a.Circle, a.Declared = env[edgeCircleKnob]
	}
	if !a.Found {
		return nil, a
	}
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
	for _, d := range docs {
		if docKind(d) != "NetworkPolicy" {
			continue
		}
		spec, _ := d["spec"].(map[string]any)
		sel, err := parseSelector(spec["podSelector"])
		if err != nil || !sel.matches(labels) {
			continue
		}
		ingress := false
		for _, pt := range slice(spec, "policyTypes") {
			ingress = ingress || pt == "Ingress"
		}
		if !ingress {
			continue
		}
		a.Policies++
		for i, r := range slice(spec, "ingress") {
			rm, _ := r.(map[string]any)
			opened := openedForwardingPorts(rm, ports)
			if len(opened) == 0 {
				continue
			}
			a.RulesJudged++
			where := fmt.Sprintf("политика %s, правило %d (порты %s)", docName(d), i, strings.Join(opened, ","))
			from := slice(rm, "from")
			if len(from) == 0 {
				say("%s открыто всем: правило без отправителей — заголовок адреса от любого пира в круге %s "+
					"принимается краем", where, a.Circle)
			}
			for _, p := range from {
				pm, _ := p.(map[string]any)
				switch {
				case pm["ipBlock"] != nil:
					say("%s впускает блок адресов — звено называется метками пода, а не сетью", where)
				case pm["namespaceSelector"] != nil:
					say("%s впускает по селектору пространства — отправитель вне пространства рендером не назван", where)
				default:
					ps, err := parseSelector(pm["podSelector"])
					if err != nil || len(ps.match)+len(ps.exprs) == 0 {
						say("%s впускает все поды пространства — заголовок адреса принимается от любого из них", where)
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

// openedForwardingPorts — порты контейнера края, которые правило открывает.
// Правило без перечня портов открывает все.
func openedForwardingPorts(rule map[string]any, ports map[string]bool) []string {
	rp := slice(rule, "ports")
	if len(rp) == 0 {
		return []string{"все"}
	}
	var out []string
	for _, p := range rp {
		pm, _ := p.(map[string]any)
		key := fmt.Sprint(pm["port"])
		if pm["port"] == nil {
			return []string{"все"}
		}
		if n := toInt(pm["port"]); n != 0 {
			key = fmt.Sprint(n)
		}
		if ports[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryStackTrustsTheClientAddressOnlyFromTheFront(t *testing.T) {
	stacks := deployStacks(t)
	var edges, trusting, rules int
	subjectTrusting := false
	for _, n := range sortedStackNames(stacks) {
		findings, a := judgeEdgeAdmission(n, npChainDocs(t, n))
		if !a.Found {
			continue
		}
		edges++
		if a.Trusting {
			trusting++
			rules += a.RulesJudged
		}
		if n == circleSubjectStack {
			subjectTrusting = a.Trusting
		}
		t.Logf("цепочка %-11s: %s=%q · политик на край %d · правил с портом пересылки %d · находок %d",
			n, edgeCircleKnob, a.Circle, a.Policies, a.RulesJudged, len(findings))
		for _, f := range findings {
			t.Error(f)
		}
	}
	t.Logf("перепись: цепочек %d · с краем %d · с непустым кругом %d · правил осмотрено %d",
		len(stacks), edges, trusting, rules)
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
	if trusting == 0 {
		t.Fatal("ни у одной цепочки круг не непуст — замок «только фронт» не осмотрен ни разу")
	}
}
