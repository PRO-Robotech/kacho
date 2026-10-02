// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// network_policy_admission_test.go — СУДЬЯ «политика сети пропускает тех, кто
// к службе звонит, и называет только тех, кто в рендере есть» (kacho#2941).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Политика сети, выбравшая под, запрещает ему весь вход, кроме названного. Вход
// называется МЕТКАМИ пода-отправителя, а метки ставит чужой чарт. Когда политика
// пишет метку по памяти, а под несёт другую, правило не пропускает НИКОГО — и
// рендер, и установка, и готовность подов остаются зелёными: служба поднята,
// просто до неё никто не достаёт. Так край не доставал до службы балансировщиков
// (политика ждала одну метку края, под края нёс другую), и так же соседи не
// доставали до внутреннего слушателя сетей (политика называла только край, а
// звонят туда ещё два соседа).
//
// Судятся две половины, и одной мало:
//
//   - (А) МЁРТВЫЙ СЕЛЕКТОР. Селектор цели политики и каждый селектор отправителя
//     (и получателя исхода) выбирает хотя бы один под рендера. Ноль — правило,
//     не пропускающее никого, либо политика, не защищающая ничего.
//   - (Б) ДОСТИЖИМОСТЬ. Для каждого адреса службы, который рабочий объект
//     получает в настройках (переменные, аргументы, смонтированная или
//     подставленная карта настроек), под-адресат обязан впускать этого
//     отправителя на этот порт — и, если отправитель сам ограничен по исходу,
//     отправитель обязан выпускать.
//
// (А) без (Б) зелёная на селекторе, выбирающем НЕ ТОТ под (метка края,
// вычисленная в чужом контексте, совпала бы с меткой самой службы); (Б) без (А)
// молчит о правиле, к которому в рендере никто не звонит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦЫ, названные вслух
//
//   - Отправитель вне пространства имён (селектор пространства, блок адресов)
//     рендером не моделируется: такой отправитель СЧИТАЕТСЯ числом и не
//     засчитывается ни как выбирающий, ни как пропускающий.
//   - Адрес узнаётся в форме `хост:порт` (со схемой, с `@` учётной записи либо
//     голый). Хост и порт, разнесённые по двум ключам (`db.host` + `db.port`),
//     не узнаются — входы хранилищ держит половина (А).
//   - Секреты не читаются: адрес в секрете рендером не виден.
//   - Отправитель, звонящий сам себе, не судится и считается числом.
package deploy_test

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// npSelector — селектор меток в той форме, которую судья умеет исполнять.
type npSelector struct {
	match map[string]string
	exprs []npExpr
}

type npExpr struct {
	key, op string
	values  []string
}

func (s npSelector) String() string {
	parts := make([]string, 0, len(s.match)+len(s.exprs))
	for k, v := range s.match {
		parts = append(parts, k+"="+v)
	}
	for _, e := range s.exprs {
		parts = append(parts, fmt.Sprintf("%s %s %v", e.key, e.op, e.values))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "{} (все поды)"
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (s npSelector) matches(labels map[string]string) bool {
	for k, v := range s.match {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	for _, e := range s.exprs {
		v, has := labels[e.key]
		in := false
		for _, x := range e.values {
			if x == v {
				in = true
			}
		}
		switch e.op {
		case "In":
			if !has || !in {
				return false
			}
		case "NotIn":
			if has && in {
				return false
			}
		case "Exists":
			if !has {
				return false
			}
		case "DoesNotExist":
			if has {
				return false
			}
		}
	}
	return true
}

func parseSelector(raw any) (npSelector, error) {
	s := npSelector{match: map[string]string{}}
	m, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return s, fmt.Errorf("селектор не отображение: %T", raw)
	}
	if ml, ok := m["matchLabels"].(map[string]any); ok {
		for k, v := range ml {
			s.match[k] = fmt.Sprint(v)
		}
	}
	exprs, _ := m["matchExpressions"].([]any)
	for _, x := range exprs {
		em, _ := x.(map[string]any)
		e := npExpr{key: fmt.Sprint(em["key"]), op: fmt.Sprint(em["operator"])}
		switch e.op {
		case "In", "NotIn", "Exists", "DoesNotExist":
		default:
			return s, fmt.Errorf("оператор выражения %q судья не исполняет", e.op)
		}
		vals, _ := em["values"].([]any)
		for _, v := range vals {
			e.values = append(e.values, fmt.Sprint(v))
		}
		s.exprs = append(s.exprs, e)
	}
	return s, nil
}

// npWorkload — рабочий объект рендера и всё, что о нём судят.
type npWorkload struct {
	kind, name string
	labels     map[string]string
	spec       map[string]any
}

func (w npWorkload) id() string { return w.kind + "/" + w.name }

// containerPort — номер и имя порта контейнера, на который указывает targetPort.
func (w npWorkload) containerPort(target any) (int, string, bool) {
	for _, key := range []string{"containers", "initContainers"} {
		cs, _ := w.spec[key].([]any)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			ps, _ := cm["ports"].([]any)
			for _, p := range ps {
				pm, _ := p.(map[string]any)
				num, _ := pm["containerPort"].(int)
				name, _ := pm["name"].(string)
				switch t := target.(type) {
				case int:
					if t == num {
						return num, name, true
					}
				case string:
					if t == name {
						return num, name, true
					}
				}
			}
		}
	}
	if t, ok := target.(int); ok {
		return t, "", true
	}
	return 0, "", false
}

type npPort struct {
	protocol string
	port     any // nil — все порты; int; string — имя порта
	endPort  int
}

func (p npPort) admits(num int, name string) bool {
	if p.protocol != "TCP" {
		return false
	}
	switch v := p.port.(type) {
	case nil:
		return true
	case int:
		if p.endPort > 0 {
			return num >= v && num <= p.endPort
		}
		return num == v
	case string:
		return name != "" && name == v
	}
	return false
}

func (p npPort) String() string {
	if p.port == nil {
		return "все"
	}
	if p.endPort > 0 {
		return fmt.Sprintf("%v-%d", p.port, p.endPort)
	}
	return fmt.Sprint(p.port)
}

type npPeer struct {
	sel       npSelector
	unmodeled string // не пусто — отправитель вне модели (пространство, блок адресов)
}

type npRule struct {
	index    int
	ports    []npPort
	anyPeer  bool
	peers    []npPeer
	outbound bool
}

func (r npRule) portsString() string {
	if len(r.ports) == 0 {
		return "все"
	}
	s := make([]string, len(r.ports))
	for i, p := range r.ports {
		s[i] = p.String()
	}
	return strings.Join(s, ",")
}

func (r npRule) admitsPort(num int, name string) bool {
	if len(r.ports) == 0 {
		return true
	}
	for _, p := range r.ports {
		if p.admits(num, name) {
			return true
		}
	}
	return false
}

func (r npRule) admitsPeer(labels map[string]string) bool {
	if r.anyPeer {
		return true
	}
	for _, p := range r.peers {
		if p.unmodeled == "" && p.sel.matches(labels) {
			return true
		}
	}
	return false
}

type npPolicy struct {
	name          string
	target        npSelector
	ingress       bool
	egress        bool
	inRules       []npRule
	outRules      []npRule
	unmodeledPeer int
}

func parseRules(raw any, peerKey string, outbound bool) ([]npRule, int, error) {
	list, _ := raw.([]any)
	var (
		rules     []npRule
		unmodeled int
	)
	for i, x := range list {
		rm, _ := x.(map[string]any)
		r := npRule{index: i, outbound: outbound}
		ports, _ := rm["ports"].([]any)
		for _, p := range ports {
			pm, _ := p.(map[string]any)
			np := npPort{protocol: "TCP", port: pm["port"]}
			if proto, ok := pm["protocol"].(string); ok && proto != "" {
				np.protocol = proto
			}
			if ep, ok := pm["endPort"].(int); ok {
				np.endPort = ep
			}
			switch np.port.(type) {
			case nil, int, string:
			default:
				return nil, 0, fmt.Errorf("порт правила %d вида %T судья не исполняет", i, np.port)
			}
			r.ports = append(r.ports, np)
		}
		peers, _ := rm[peerKey].([]any)
		r.anyPeer = len(peers) == 0
		for _, p := range peers {
			pm, _ := p.(map[string]any)
			_, hasPod := pm["podSelector"]
			_, hasNS := pm["namespaceSelector"]
			_, hasBlock := pm["ipBlock"]
			switch {
			case hasPod && !hasNS && !hasBlock:
				sel, err := parseSelector(pm["podSelector"])
				if err != nil {
					return nil, 0, fmt.Errorf("правило %d: %w", i, err)
				}
				r.peers = append(r.peers, npPeer{sel: sel})
			default:
				unmodeled++
				r.peers = append(r.peers, npPeer{unmodeled: fmt.Sprint(pm)})
			}
		}
		rules = append(rules, r)
	}
	return rules, unmodeled, nil
}

func parsePolicy(d map[string]any) (npPolicy, error) {
	md, _ := d["metadata"].(map[string]any)
	spec, _ := d["spec"].(map[string]any)
	p := npPolicy{name: fmt.Sprint(md["name"])}
	sel, err := parseSelector(spec["podSelector"])
	if err != nil {
		return p, fmt.Errorf("политика %s: цель: %w", p.name, err)
	}
	p.target = sel
	types, hasTypes := spec["policyTypes"].([]any)
	if !hasTypes {
		// Умолчание API: Ingress всегда, Egress — если правила исхода названы.
		p.ingress = true
		_, p.egress = spec["egress"]
	}
	for _, t := range types {
		switch t {
		case "Ingress":
			p.ingress = true
		case "Egress":
			p.egress = true
		}
	}
	var n int
	if p.inRules, n, err = parseRules(spec["ingress"], "from", false); err != nil {
		return p, fmt.Errorf("политика %s: вход: %w", p.name, err)
	}
	p.unmodeledPeer += n
	if p.outRules, n, err = parseRules(spec["egress"], "to", true); err != nil {
		return p, fmt.Errorf("политика %s: исход: %w", p.name, err)
	}
	p.unmodeledPeer += n
	return p, nil
}

type npServicePort struct {
	port   int
	target any
}

type npService struct {
	name     string
	selector npSelector
	ports    []npServicePort
}

// npHostPort — адрес, названный в настройках: первая метка хоста и порт.
type npHostPort struct {
	host string
	port int
}

func isNPHostRune(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

// dialTargets — адреса `хост:порт` в строке, у которых хост — имя службы в
// пространстве ns (голое, `.ns`, `.ns.svc`, `.ns.svc.cluster.local`).
//
// Хост обязан начинаться на границе: перед ним начало строки, схема (`://`,
// `:///`), `@` учётной записи либо разделитель — но не `/` (`реестр/образ:тег`),
// не `_` и не заглавная (ключи переменных). Порт обязан кончаться на границе:
// `образ:2798-87329e29` — тег, а не порт.
func dialTargets(s, ns string) []npHostPort {
	var out []npHostPort
	suffixes := []string{"", "." + ns, "." + ns + ".svc", "." + ns + ".svc.cluster.local"}
	for i := 0; i < len(s); i++ {
		if s[i] != ':' {
			continue
		}
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i+1 || j-i-1 > 5 {
			continue
		}
		if j < len(s) && (isNPHostRune(s[j]) && s[j] != '.' || s[j] == '_') {
			continue
		}
		k := i
		for k > 0 && isNPHostRune(s[k-1]) {
			k--
		}
		host := strings.TrimRight(s[k:i], ".")
		if host == "" || host[0] == '.' || host[0] == '-' {
			continue
		}
		if k > 0 {
			before := s[:k]
			c := s[k-1]
			switch {
			case strings.HasSuffix(before, "://"), strings.HasSuffix(before, ":///"), c == '@':
			case c == '/' || c == '_' || c == ':' || (c >= 'A' && c <= 'Z'):
				continue
			}
		}
		first, rest, _ := strings.Cut(host, ".")
		if rest != "" {
			rest = "." + rest
		}
		known := false
		for _, suf := range suffixes {
			if rest == suf {
				known = true
			}
		}
		if !known {
			continue
		}
		port, err := strconv.Atoi(s[i+1 : j])
		if err != nil || port == 0 || port > 65535 {
			continue
		}
		out = append(out, npHostPort{host: first, port: port})
	}
	return out
}

func npWalkStrings(v any, visit func(string)) {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			npWalkStrings(e, visit)
		}
	case []any:
		for _, e := range x {
			npWalkStrings(e, visit)
		}
	case string:
		visit(x)
	}
}

func podTemplateOf(d map[string]any) (map[string]any, bool) {
	spec, _ := d["spec"].(map[string]any)
	switch d["kind"] {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "ReplicaSet":
		t, ok := spec["template"].(map[string]any)
		return t, ok
	case "CronJob":
		jt, _ := spec["jobTemplate"].(map[string]any)
		js, _ := jt["spec"].(map[string]any)
		t, ok := js["template"].(map[string]any)
		return t, ok
	}
	return nil, false
}

func stringMap(raw any) map[string]string {
	out := map[string]string{}
	m, _ := raw.(map[string]any)
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// configSources — строки карт настроек, которые рабочий объект получает:
// том (прямой либо составной), envFrom, valueFrom по ключу.
func configSources(spec map[string]any, maps map[string]map[string]any) []any {
	var out []any
	whole := func(name any) {
		if cm, ok := maps[fmt.Sprint(name)]; ok {
			out = append(out, cm["data"])
		}
	}
	vols, _ := spec["volumes"].([]any)
	for _, v := range vols {
		vm, _ := v.(map[string]any)
		if c, ok := vm["configMap"].(map[string]any); ok {
			whole(c["name"])
		}
		pr, _ := vm["projected"].(map[string]any)
		srcs, _ := pr["sources"].([]any)
		for _, s := range srcs {
			sm, _ := s.(map[string]any)
			if c, ok := sm["configMap"].(map[string]any); ok {
				whole(c["name"])
			}
		}
	}
	for _, key := range []string{"containers", "initContainers"} {
		cs, _ := spec[key].([]any)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			efs, _ := cm["envFrom"].([]any)
			for _, ef := range efs {
				efm, _ := ef.(map[string]any)
				if r, ok := efm["configMapRef"].(map[string]any); ok {
					whole(r["name"])
				}
			}
			envs, _ := cm["env"].([]any)
			for _, e := range envs {
				em, _ := e.(map[string]any)
				vf, _ := em["valueFrom"].(map[string]any)
				ref, _ := vf["configMapKeyRef"].(map[string]any)
				if ref == nil {
					continue
				}
				if m, ok := maps[fmt.Sprint(ref["name"])]; ok {
					data, _ := m["data"].(map[string]any)
					out = append(out, data[fmt.Sprint(ref["key"])])
				}
			}
		}
	}
	return out
}

// npVerdict — исход судьи по одному рендеру и объём осмотренного.
type npVerdict struct {
	workloads, policies, selectorsJudged, unmodeledPeers int
	dials, isolated, selfDials, unexposedPorts           int
	findings                                             []string
}

// judgeNetworkPolicies — обе половины по документам одного рендера.
//
// Ошибка — отказ, а не находка: документ, которого судья не умеет прочесть,
// сужал бы перепись молча.
func judgeNetworkPolicies(ns string, docs []map[string]any) (npVerdict, error) {
	var (
		v        npVerdict
		works    []npWorkload
		services = map[string]npService{}
		maps     = map[string]map[string]any{}
		policies []npPolicy
	)
	for _, d := range docs {
		md, _ := d["metadata"].(map[string]any)
		if docNS, ok := md["namespace"].(string); ok && docNS != "" && docNS != ns {
			continue
		}
		name := fmt.Sprint(md["name"])
		switch d["kind"] {
		case "ConfigMap":
			maps[name] = d
		case "Service":
			spec, _ := d["spec"].(map[string]any)
			sel := stringMap(spec["selector"])
			if len(sel) == 0 {
				continue
			}
			s := npService{name: name, selector: npSelector{match: sel}}
			ports, _ := spec["ports"].([]any)
			for _, p := range ports {
				pm, _ := p.(map[string]any)
				num, _ := pm["port"].(int)
				target := pm["targetPort"]
				if target == nil {
					target = num
				}
				s.ports = append(s.ports, npServicePort{port: num, target: target})
			}
			services[name] = s
		case "NetworkPolicy":
			p, err := parsePolicy(d)
			if err != nil {
				return v, err
			}
			policies = append(policies, p)
		default:
			t, ok := podTemplateOf(d)
			if !ok {
				continue
			}
			tm, _ := t["metadata"].(map[string]any)
			spec, _ := t["spec"].(map[string]any)
			works = append(works, npWorkload{kind: fmt.Sprint(d["kind"]), name: name,
				labels: stringMap(tm["labels"]), spec: spec})
		}
	}
	if len(works) == 0 {
		return v, fmt.Errorf("в рендере ни одного рабочего объекта — судить нечего, это не «чисто»")
	}
	v.workloads, v.policies = len(works), len(policies)

	selected := func(s npSelector) []string {
		var out []string
		for _, w := range works {
			if s.matches(w.labels) {
				out = append(out, w.id())
			}
		}
		return out
	}

	// (А) Мёртвый селектор.
	for _, p := range policies {
		v.unmodeledPeers += p.unmodeledPeer
		v.selectorsJudged++
		if len(selected(p.target)) == 0 {
			v.findings = append(v.findings, fmt.Sprintf(
				"(А) политика %s: селектор цели %s не выбирает ни одного пода рендера — политика не защищает ничего",
				p.name, p.target))
		}
		for _, rules := range [][]npRule{p.inRules, p.outRules} {
			for _, r := range rules {
				for _, peer := range r.peers {
					if peer.unmodeled != "" {
						continue
					}
					v.selectorsJudged++
					if len(selected(peer.sel)) == 0 {
						dir := "входа (отправитель)"
						if r.outbound {
							dir = "исхода (получатель)"
						}
						v.findings = append(v.findings, fmt.Sprintf(
							"(А) политика %s, правило %s #%d (порты %s): селектор %s не выбирает ни одного пода рендера — правило не пропускает никого",
							p.name, dir, r.index, r.portsString(), peer.sel))
					}
				}
			}
		}
	}

	// (Б) Достижимость.
	for _, w := range works {
		corpus := append([]any{w.spec}, configSources(w.spec, maps)...)
		seen := map[npHostPort]bool{}
		for _, c := range corpus {
			npWalkStrings(c, func(s string) {
				for _, hp := range dialTargets(s, ns) {
					seen[hp] = true
				}
			})
		}
		dials := make([]npHostPort, 0, len(seen))
		for hp := range seen {
			dials = append(dials, hp)
		}
		sort.Slice(dials, func(i, j int) bool {
			if dials[i].host != dials[j].host {
				return dials[i].host < dials[j].host
			}
			return dials[i].port < dials[j].port
		})
		for _, hp := range dials {
			svc, ok := services[hp.host]
			if !ok {
				continue
			}
			var sp *npServicePort
			for i := range svc.ports {
				if svc.ports[i].port == hp.port {
					sp = &svc.ports[i]
				}
			}
			if sp == nil {
				v.unexposedPorts++
				continue
			}
			for _, t := range works {
				if !svc.selector.matches(t.labels) {
					continue
				}
				if t.id() == w.id() {
					v.selfDials++
					continue
				}
				num, pname, ok := t.containerPort(sp.target)
				if !ok {
					v.unexposedPorts++
					continue
				}
				v.dials++
				addr := fmt.Sprintf("%s:%d", hp.host, hp.port)
				var inPol, outPol []string
				inOK, outOK := true, true
				for _, p := range policies {
					if p.ingress && p.target.matches(t.labels) {
						inPol = append(inPol, p.name)
						inOK = false
					}
					if p.egress && p.target.matches(w.labels) {
						outPol = append(outPol, p.name)
						outOK = false
					}
				}
				if len(inPol) > 0 || len(outPol) > 0 {
					v.isolated++
				}
				for _, p := range policies {
					if p.ingress && p.target.matches(t.labels) {
						for _, r := range p.inRules {
							if r.admitsPort(num, pname) && r.admitsPeer(w.labels) {
								inOK = true
							}
						}
					}
					if p.egress && p.target.matches(w.labels) {
						for _, r := range p.outRules {
							if r.admitsPort(num, pname) && r.admitsPeer(t.labels) {
								outOK = true
							}
						}
					}
				}
				if !inOK {
					v.findings = append(v.findings, fmt.Sprintf(
						"(Б) %s звонит %s → под %s, порт %d (%s): вход не пропускает ни одна из политик %v",
						w.id(), addr, t.id(), num, orDashNP(pname), inPol))
				}
				if !outOK {
					v.findings = append(v.findings, fmt.Sprintf(
						"(Б) %s звонит %s → под %s, порт %d (%s): исход не выпускает ни одна из политик %v",
						w.id(), addr, t.id(), num, orDashNP(pname), outPol))
				}
			}
		}
	}
	sort.Strings(v.findings)
	return v, nil
}

func orDashNP(s string) string {
	if s == "" {
		return "без имени"
	}
	return s
}
