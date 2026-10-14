// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// edge_front_link_identity_render_test.go — ЗВЕНО ФРОНТА ПРЕДЪЯВЛЯЕТ КРАЮ ЛИСТ
// ЯКОРЯ ЗВЕНЬЕВ, И АДРЕС КЛИЕНТА ДОХОДИТ ДО ЗВЕНА (kacho#3028, круг 5).
//
// Четвёртый круг доказал звено в КОДЕ края (имя в проверенном листе) и не
// доказал его в РЕНДЕРЕ: ни одно звено ни в одной цепочке лист не предъявляло
// (консоль ходила к краю открытым текстом, контроллер входа — без клиентского
// листа), а лист с любым именем выпускал кластерный выпускающий. Этот гейт
// судит рендер каждой цепочки deploy/stacks.txt, у которой край объявляет
// звенья поимённо, по переписи каналов «кто может прислать адрес»:
//
//  1. ЯКОРЬ (кто выпускает лист). Ручка якоря звеньев края указывает в том
//     секрета, который заводит Certificate удостоверяющего центра (isCA) от
//     НАМЕСПЕЙСНОГО самоподписанного выпускающего, и это НЕ секрет якоря
//     установки. Каждое имя звена, которое край принимает, несёт хотя бы один
//     лист выпускающего этого центра, и НИ ОДИН лист с этим именем не
//     выпускает кто-то другой (кластерный выпускающий выдал бы его любому
//     пространству имён).
//  2. ПРЕДЪЯВЛЕНИЕ (звено несёт лист к краю). Каждый под, выбранный службой
//     звена, монтирует лист якоря звеньев и ставит его на каждый путь к краю:
//     - раздача консоли — каждая полоса к краю идёт `https://` на порт `tls`
//     края (на `cmux` рукопожатия нет — звено там не опознаётся) с
//     `proxy_ssl_certificate`/`_key` из тома листа;
//     - контроллер входа — `grpc_ssl_certificate`/`_key` и
//     `proxy_ssl_certificate`/`_key` уровня `http` (основной вход края —
//     GRPCS) из тома листа; путь — литералом либо переменной `map`.
//  3. АДРЕС ДОХОДИТ ДО ЗВЕНА (исход маршрутизации). Где хост консоли ведёт
//     контроллер входа, КАЖДАЯ полоса раздачи к краю на этом хосте достаётся
//     краю (порт `tls`, бэкенд HTTPS), а не раздаче: за раздачей пир — под
//     контроллера, и все клиенты были бы для края одним адресом. Маршрут
//     выбирается как у контроллера: точное совпадение, затем самый длинный
//     префикс по сегментам среди объектов входа хоста.
//
// ЗНАМЕНАТЕЛЬ печатается: цепочки, цепочки со звеньями, имена, поды звеньев,
// полосы и маршруты. Ноль цепочек со звеньями, имён или подов — гейт не
// исполнился.
package deploy_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/proxycircle"
)

const (
	edgeSANsKnob     = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_SANS"
	edgeLinkCAKnob   = "KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CA_FILE"
	edgeInstCAKnob   = "KACHO_API_GATEWAY_MTLS_CA_FILE"
	edgeDeployName   = "api-gateway"
	edgeTLSPortName  = "tls"
	backendProtoAnno = "nginx.ingress.kubernetes.io/backend-protocol"
)

// linkIdentityCensus — объём осмотренного одной цепочкой.
type linkIdentityCensus struct {
	Linked, SANs, LinkPods, Lanes, Routes int
}

var (
	nginxSSLCert  = regexp.MustCompile(`(?m)^\s*(proxy|grpc)_ssl_certificate\s+(\S+?)\s*;`)
	nginxSSLKey   = regexp.MustCompile(`(?m)^\s*(proxy|grpc)_ssl_certificate_key\s+(\S+?)\s*;`)
	nginxMapVar   = regexp.MustCompile(`map\s+"[^"]*"\s+\$(\w+)\s*\{\s*default\s+(\S+?)\s*;\s*\}`)
	nginxSetVar   = regexp.MustCompile(`(?m)^\s*set\s+\$(\w+)\s+"\$\{(\w+)\}"\s*;`)
	nginxPassVar  = regexp.MustCompile(`(?m)^\s*proxy_pass\s+(\w+)://\$(\w+)\s*;`)
	configMapArgs = regexp.MustCompile(`^--configmap=(?:[^/]+/)?(\S+)$`)
)

// certIndex — Certificate рендера по имени секрета и по имени (SAN).
type renderedCert struct {
	name, secret, issuerKind, issuerName string
	isCA                                 bool
	sans                                 []string
}

func renderedCerts(docs []map[string]any) []renderedCert {
	var out []renderedCert
	for _, d := range docs {
		if docKind(d) != "Certificate" {
			continue
		}
		spec := submap(d, "spec")
		c := renderedCert{name: docName(d), secret: str(spec, "secretName")}
		ir := submap(spec, "issuerRef")
		c.issuerKind, c.issuerName = str(ir, "kind"), str(ir, "name")
		c.isCA, _ = spec["isCA"].(bool)
		for _, k := range []string{"dnsNames", "uris"} {
			for _, v := range slice(spec, k) {
				c.sans = append(c.sans, fmt.Sprint(v))
			}
		}
		out = append(out, c)
	}
	return out
}

// envOf — окружение (только value) контейнеров пода.
func podEnv(spec map[string]any) map[string]string {
	env := map[string]string{}
	cs, _ := spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, e := range slice(cm, "env") {
			em, _ := e.(map[string]any)
			env[str(em, "name")] = str(em, "value")
		}
	}
	return env
}

// secretMounts — точка монтирования → имя секрета тома, по всем контейнерам.
func secretMounts(spec map[string]any) map[string]string {
	vol := map[string]string{}
	for _, v := range slice(spec, "volumes") {
		vm, _ := v.(map[string]any)
		if s := submap(vm, "secret"); s != nil {
			vol[str(vm, "name")] = str(s, "secretName")
		}
	}
	out := map[string]string{}
	cs, _ := spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, m := range slice(cm, "volumeMounts") {
			mm, _ := m.(map[string]any)
			if sec, ok := vol[str(mm, "name")]; ok {
				out[strings.TrimSuffix(str(mm, "mountPath"), "/")] = sec
			}
		}
	}
	return out
}

// secretOfPath — секрет тома, в который указывает путь файла.
func secretOfPath(mounts map[string]string, path string) (string, bool) {
	for mp, sec := range mounts {
		if strings.HasPrefix(path, mp+"/") {
			return sec, true
		}
	}
	return "", false
}

// podConfigText — весь текст карт настроек, которые под получает (тома,
// envFrom, valueFrom и карта контроллера из `--configmap=`).
func podConfigText(spec map[string]any, maps map[string]map[string]any) string {
	var b strings.Builder
	add := func(v any) {
		npWalkStrings(v, func(s string) { b.WriteString(s); b.WriteByte('\n') })
	}
	for _, c := range configSources(spec, maps) {
		add(c)
	}
	cs, _ := spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, a := range slice(cm, "args") {
			if m := configMapArgs.FindStringSubmatch(fmt.Sprint(a)); m != nil {
				if d, ok := maps[m[1]]; ok {
					add(d["data"])
				}
			}
		}
	}
	return b.String()
}

// controllerConfigYAML — ключи карты настройки контроллера (`--configmap=`)
// строками `ключ: значение`.
func controllerConfigYAML(spec map[string]any, maps map[string]map[string]any) string {
	var b strings.Builder
	cs, _ := spec["containers"].([]any)
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		for _, a := range slice(cm, "args") {
			if m := configMapArgs.FindStringSubmatch(fmt.Sprint(a)); m != nil {
				if d, ok := maps[m[1]]; ok {
					data, _ := d["data"].(map[string]any)
					for k, v := range data {
						fmt.Fprintf(&b, "%s: %v\n", k, v)
					}
				}
			}
		}
	}
	return b.String()
}

// resolveNginxPath — путь директивы: литерал либо переменная `map "" $v {default …}`.
func resolveNginxPath(v, text string) string {
	if name, ok := strings.CutPrefix(v, "$"); ok {
		for _, m := range nginxMapVar.FindAllStringSubmatch(text, -1) {
			if m[1] == name {
				return m[2]
			}
		}
		return ""
	}
	return v
}

// laneSamplePath — путь, попадающий в полосу `location <head>`.
func laneSamplePath(head string) ([]string, error) {
	f := strings.Fields(head)
	switch {
	case len(f) == 2 && f[0] == "=":
		return []string{f[1]}, nil
	case len(f) == 2 && (f[0] == "~" || f[0] == "~*"):
		re, err := regexp.Compile(f[1])
		if err != nil {
			return nil, err
		}
		body := strings.TrimSuffix(strings.TrimPrefix(f[1], "^"), "$")
		var alts []string
		if m := regexp.MustCompile(`^/\(([a-z0-9|]+)\)(/.*)$`).FindStringSubmatch(body); m != nil {
			for _, a := range strings.Split(m[1], "|") {
				alts = append(alts, "/"+a+m[2])
			}
		} else {
			alts = []string{body}
		}
		var out []string
		for _, a := range alts {
			if strings.HasSuffix(a, "/") {
				a += "probe"
			}
			if !re.MatchString(a) {
				return nil, fmt.Errorf("путь %q не попадает в полосу %q", a, head)
			}
			out = append(out, a)
		}
		return out, nil
	case len(f) == 2 && f[0] == "^~", len(f) == 1:
		return []string{strings.TrimSuffix(f[len(f)-1], "/") + "/probe"}, nil
	}
	return nil, fmt.Errorf("форма заголовка полосы %q гейту неизвестна", head)
}

// ingressRoute — бэкенд, который контроллер класса class выберет для host+path:
// точное совпадение, затем самый длинный префикс по сегментам.
type ingressBackend struct {
	ingress, service, port, proto string
}

func ingressRoute(docs []map[string]any, class, host, path string) (ingressBackend, bool) {
	var best ingressBackend
	bestLen, exact := -1, false
	for _, d := range docs {
		if docKind(d) != "Ingress" {
			continue
		}
		spec := submap(d, "spec")
		if str(spec, "ingressClassName") != class {
			continue
		}
		proto := stringMap(submap(d, "metadata")["annotations"])[backendProtoAnno]
		if proto == "" {
			proto = "HTTP"
		}
		for _, r := range slice(spec, "rules") {
			rm, _ := r.(map[string]any)
			if str(rm, "host") != host {
				continue
			}
			for _, p := range slice(submap(rm, "http"), "paths") {
				pm, _ := p.(map[string]any)
				pp, pt := str(pm, "path"), str(pm, "pathType")
				bs := submap(submap(pm, "backend"), "service")
				port := str(submap(bs, "port"), "name")
				if port == "" {
					port = fmt.Sprint(submap(bs, "port")["number"])
				}
				be := ingressBackend{ingress: docName(d), service: str(bs, "name"), port: port, proto: proto}
				switch {
				case pt == "Exact" && pp == path:
					best, exact = be, true
				case !exact && pt != "Exact":
					prefix := strings.TrimSuffix(pp, "/")
					if prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/") {
						if l := len(prefix); l > bestLen {
							best, bestLen = be, l
						}
					}
				}
			}
		}
	}
	return best, exact || bestLen >= 0
}

// judgeEdgeLinkIdentity — находки по документам одной цепочки. Чистая функция.
func judgeEdgeLinkIdentity(ns string, docs []map[string]any) ([]string, linkIdentityCensus) {
	var out []string
	var c linkIdentityCensus
	say := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	works, services, maps, _, err := npIndexDocs(ns, docs)
	if err != nil {
		return []string{"документы не разбираются: " + err.Error()}, c
	}
	var edge *npWorkload
	for i := range works {
		if works[i].kind == "Deployment" && works[i].name == edgeDeployName {
			edge = &works[i]
		}
	}
	if edge == nil {
		return nil, c
	}
	env := podEnv(edge.spec)
	peers, perr := proxycircle.ParsePeers(env[edgePeersKnob])
	if perr != nil {
		return []string{"звенья поимённо не разбираются: " + perr.Error()}, c
	}
	if len(peers) == 0 {
		return nil, c
	}
	c.Linked++
	sans, serr := proxycircle.ParseLinkSANs(env[edgeSANsKnob])
	if serr != nil || len(sans) == 0 {
		say("звенья поимённо %v объявлены, а имена звеньев в листе (%s=%q) — нет (%v)", peers, edgeSANsKnob, env[edgeSANsKnob], serr)
		return out, c
	}
	c.SANs = len(sans)

	// 1. Якорь.
	mounts := secretMounts(edge.spec)
	anchor, ok := secretOfPath(mounts, env[edgeLinkCAKnob])
	if !ok {
		say("ручка якоря звеньев %s=%q не указывает в том секрета пода края", edgeLinkCAKnob, env[edgeLinkCAKnob])
		return out, c
	}
	if inst, ok := secretOfPath(mounts, env[edgeInstCAKnob]); ok && inst == anchor {
		say("якорь звеньев — тот же секрет %q, что якорь установки: лист с именем звена выдаёт кластерный выпускающий", anchor)
	}
	certs := renderedCerts(docs)
	issuers := map[string]map[string]any{}
	for _, d := range docs {
		if docKind(d) == "Issuer" {
			issuers[docName(d)] = d
		}
	}
	var anchorOK bool
	for _, ct := range certs {
		if ct.secret != anchor {
			continue
		}
		root, isIssuer := issuers[ct.issuerName]
		switch {
		case !ct.isCA:
			say("секрет якоря звеньев %q заводит Certificate %q без isCA", anchor, ct.name)
		case ct.issuerKind != "Issuer" || !isIssuer || submap(root, "spec")["selfSigned"] == nil:
			say("удостоверяющий центр звеньев %q выпускает %s/%s, а не намеспейсный самоподписанный выпускающий",
				ct.name, ct.issuerKind, ct.issuerName)
		default:
			anchorOK = true
		}
	}
	if !anchorOK {
		say("секрет якоря звеньев %q не заводит ни один годный Certificate удостоверяющего центра", anchor)
	}
	linkIssuers := map[string]bool{}
	for name, d := range issuers {
		if str(submap(submap(d, "spec"), "ca"), "secretName") == anchor {
			linkIssuers[name] = true
		}
	}
	linkSecrets := map[string]string{} // секрет листа → имя
	for _, san := range sans {
		issued := 0
		for _, ct := range certs {
			has := false
			for _, s := range ct.sans {
				has = has || s == san
			}
			if !has {
				continue
			}
			if ct.issuerKind != "Issuer" || !linkIssuers[ct.issuerName] {
				say("лист с именем звена %q (Certificate %q) выпускает %s/%s, а не выпускающий якоря звеньев — "+
					"имя звена получил бы тот, кто вправе заводить запросы к нему", san, ct.name, ct.issuerKind, ct.issuerName)
				continue
			}
			issued++
			linkSecrets[ct.secret] = san
		}
		if issued == 0 {
			say("имя звена %q край принимает, а листа якоря звеньев с этим именем рендер не заводит", san)
		}
	}

	// 2. Предъявление.
	edgeSvc := map[string]bool{}
	for name, svc := range services {
		if svc.selector.matches(edge.labels) {
			edgeSvc[name] = true
		}
	}
	edgePortName := func(port int) string {
		for name := range edgeSvc {
			for _, sp := range services[name].ports {
				if sp.port == port {
					_, pn, _ := edge.containerPort(sp.target)
					return pn
				}
			}
		}
		return ""
	}
	headless := map[string]map[string]any{} // все службы рендера по имени
	for _, d := range docs {
		if docKind(d) == "Service" {
			headless[docName(d)] = d
		}
	}
	consoleHosts := map[string]bool{} // службы раздачи консоли (для маршрутов)
	type consoleLane struct{ head string }
	var consoleLanes []consoleLane
	controllerClass := ""
	for _, peer := range peers {
		short, _, _ := strings.Cut(peer, ".")
		svc, ok := headless[short]
		if !ok {
			say("служба звена %q не отрисована", peer)
			continue
		}
		sel := npSelector{match: stringMap(submap(svc, "spec")["selector"])}
		for _, w := range works {
			if !sel.matches(w.labels) || len(sel.match) == 0 {
				continue
			}
			c.LinkPods++
			pm := secretMounts(w.spec)
			linkDir := ""
			for mp, sec := range pm {
				if _, ok := linkSecrets[sec]; ok {
					linkDir = mp
				}
			}
			if linkDir == "" {
				say("звено %s (%s) не монтирует ни одного листа якоря звеньев — краю нечего предъявить", w.id(), peer)
				continue
			}
			text := podConfigText(w.spec, maps)
			inDir := func(p string) bool { return strings.HasPrefix(p, linkDir+"/") }
			isController := false
			cs, _ := w.spec["containers"].([]any)
			for _, ci := range cs {
				cm, _ := ci.(map[string]any)
				for _, a := range slice(cm, "args") {
					if v, ok := strings.CutPrefix(fmt.Sprint(a), "--ingress-class="); ok {
						isController, controllerClass = true, v
					}
				}
			}
			if isController {
				// Контроллер пишет краю адрес СВОЕГО TCP-пира. Ключи, с которыми он
				// берёт адрес из заголовка клиента или из PROXY-протокола (без
				// балансировщика, его пишущего), отдали бы краю заявление клиента
				// о себе — с листом звена, то есть доверенно.
				for _, key := range []string{"use-forwarded-headers", "compute-full-forwarded-for",
					"use-proxy-protocol", "enable-real-ip"} {
					if v := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*"?true"?\s*$`).FindString(
						controllerConfigYAML(w.spec, maps)); v != "" {
						say("контроллер входа %s берёт адрес клиента из его заголовка (%s) — край принял бы подделку "+
							"от звена", w.id(), key)
					}
				}
				for _, kind := range []string{"grpc", "proxy"} {
					var crt, key bool
					for _, m := range nginxSSLCert.FindAllStringSubmatch(text, -1) {
						crt = crt || (m[1] == kind && inDir(resolveNginxPath(m[2], text)))
					}
					for _, m := range nginxSSLKey.FindAllStringSubmatch(text, -1) {
						key = key || (m[1] == kind && inDir(resolveNginxPath(m[2], text)))
					}
					if !crt || !key {
						say("контроллер входа %s не ставит %s_ssl_certificate/_key из тома листа звена %s — "+
							"край видит всех его клиентов одним адресом пода", w.id(), kind, linkDir)
					}
				}
				continue
			}
			// Раздача: каждая полоса к краю.
			podEnvs := podEnv(w.spec)
			locs, lerr := nginxLocations(text)
			if lerr != nil {
				say("%s: карта настройки не разбирается: %v", w.id(), lerr)
				continue
			}
			// Заголовок адреса раздача пишет сама адресом своего TCP-пира
			// (judgeLaneForwardedFor — тот же судья, что у внешнего входа консоли).
			if lf, _ := judgeLaneForwardedFor(text); len(lf) > 0 {
				for _, f := range lf {
					say("%s: %s", w.id(), f)
				}
			}
			lanes := 0
			for _, l := range locs {
				pass := nginxPassVar.FindStringSubmatch(l.Body)
				if pass == nil {
					continue
				}
				target := ""
				for _, sm := range nginxSetVar.FindAllStringSubmatch(l.Body, -1) {
					if sm[1] == pass[2] {
						target = podEnvs[sm[2]]
					}
				}
				var toEdge *npHostPort
				for _, hp := range dialTargets(target, ns) {
					if edgeSvc[hp.host] {
						hp := hp
						toEdge = &hp
					}
				}
				if toEdge == nil {
					continue
				}
				lanes++
				c.Lanes++
				consoleLanes = append(consoleLanes, consoleLane{l.Head})
				if pass[1] != "https" {
					say("%s: полоса `location %s` идёт к краю %s:// — без рукопожатия звено не опознаётся", w.id(), l.Head, pass[1])
				}
				if pn := edgePortName(toEdge.port); pn != edgeTLSPortName {
					say("%s: полоса `location %s` идёт на порт края %d (%q), а не на %q", w.id(), l.Head, toEdge.port, pn, edgeTLSPortName)
				}
				var crt, key bool
				for _, m := range nginxSSLCert.FindAllStringSubmatch(l.Body, -1) {
					crt = crt || (m[1] == "proxy" && inDir(resolveNginxPath(m[2], text)))
				}
				for _, m := range nginxSSLKey.FindAllStringSubmatch(l.Body, -1) {
					key = key || (m[1] == "proxy" && inDir(resolveNginxPath(m[2], text)))
				}
				if !crt || !key {
					say("%s: полоса `location %s` не ставит proxy_ssl_certificate/_key из тома листа звена %s", w.id(), l.Head, linkDir)
				}
			}
			if lanes == 0 {
				say("звено %s (%s) не проксирует к краю ни одной полосы — звеном оно быть не должно", w.id(), peer)
			}
			for name, svc := range services {
				if svc.selector.matches(w.labels) && str(submap(headless[name], "spec"), "clusterIP") != "None" {
					consoleHosts[name] = true
				}
			}
		}
	}

	// 3. Адрес доходит до звена: хост консоли за контроллером.
	if controllerClass != "" && len(consoleLanes) > 0 {
		hosts := map[string]bool{}
		for _, d := range docs {
			if docKind(d) != "Ingress" || str(submap(d, "spec"), "ingressClassName") != controllerClass {
				continue
			}
			for _, r := range slice(submap(d, "spec"), "rules") {
				rm, _ := r.(map[string]any)
				for _, p := range slice(submap(rm, "http"), "paths") {
					pm, _ := p.(map[string]any)
					if consoleHosts[str(submap(submap(pm, "backend"), "service"), "name")] {
						hosts[str(rm, "host")] = true
					}
				}
			}
		}
		hs := make([]string, 0, len(hosts))
		for h := range hosts {
			hs = append(hs, h)
		}
		sort.Strings(hs)
		for _, h := range hs {
			for _, ln := range consoleLanes {
				paths, perr := laneSamplePath(ln.head)
				if perr != nil {
					say("полоса `location %s`: %v", ln.head, perr)
					continue
				}
				for _, p := range paths {
					c.Routes++
					be, ok := ingressRoute(docs, controllerClass, h, p)
					switch {
					case !ok:
						say("хост консоли %s: путь %s полосы к краю не ведёт никуда", h, p)
					case !edgeSvc[be.service] || be.port != edgeTLSPortName || be.proto != "HTTPS":
						say("хост консоли %s: путь %s полосы к краю ведёт на %s:%s (%s, объект %s), а не на порт %q края "+
							"бэкендом HTTPS — за раздачей пир края — под контроллера, все клиенты одним адресом",
							h, p, be.service, be.port, be.proto, be.ingress, edgeTLSPortName)
					}
				}
			}
		}
	}
	return out, c
}

func TestEveryStackFrontLinkPresentsTheLinkAnchorLeafToTheEdge(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	var total linkIdentityCensus
	var findings []string
	for _, name := range names {
		got, c := judgeEdgeLinkIdentity(npNamespace, npChainDocs(t, name))
		t.Logf("%s: со звеньями %d · имён %d · подов звеньев %d · полос к краю %d · маршрутов хоста консоли %d",
			name, c.Linked, c.SANs, c.LinkPods, c.Lanes, c.Routes)
		total.Linked += c.Linked
		total.SANs += c.SANs
		total.LinkPods += c.LinkPods
		total.Lanes += c.Lanes
		total.Routes += c.Routes
		for _, f := range got {
			findings = append(findings, name+": "+f)
		}
	}
	t.Logf("перепись: цепочек %d · со звеньями %d · имён %d · подов звеньев %d · полос %d · маршрутов %d",
		len(names), total.Linked, total.SANs, total.LinkPods, total.Lanes, total.Routes)
	for _, f := range findings {
		t.Error(f)
	}
	if total.Linked == 0 || total.SANs == 0 || total.LinkPods == 0 || total.Lanes == 0 || total.Routes == 0 {
		t.Fatalf("знаменатель пуст (%+v) — гейт не исполнился", total)
	}
}

// ─── инъекции: каждая ломает в рендере настоящей цепочки ровно одно ─────────

// linkInjection — правка документов цепочки в памяти.
type linkInjection struct {
	name, says string
	mutate     func(t *testing.T, docs []map[string]any)
}

func docNamed(t *testing.T, docs []map[string]any, kind, name string) map[string]any {
	t.Helper()
	for _, d := range docs {
		if docKind(d) == kind && docName(d) == name {
			return d
		}
	}
	t.Fatalf("в рендере нет %s/%s — предпосылка инъекции исчезла", kind, name)
	return nil
}

// rewriteStrings — замена подстроки во всех строках документа.
func rewriteStrings(v any, from, to string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = rewriteStrings(e, from, to)
		}
	case []any:
		for i, e := range x {
			x[i] = rewriteStrings(e, from, to)
		}
	case string:
		return strings.ReplaceAll(x, from, to)
	}
	return v
}

func TestFrontLinkIdentityJudgeCatchesEachChannel(t *testing.T) {
	const chain = "own"
	base := npChainDocs(t, chain)
	if got, c := judgeEdgeLinkIdentity(npNamespace, base); len(got) != 0 || c.Linked == 0 || c.Routes == 0 {
		t.Fatalf("близнец %s: находки %v либо знаменатель пуст %+v", chain, got, c)
	}
	for _, inj := range []linkInjection{
		{"лист консоли выпускает кластерный выпускающий", "не выпускающий якоря звеньев",
			func(t *testing.T, docs []map[string]any) {
				ir := submap(submap(docNamed(t, docs, "Certificate", "api-gateway-front-console-link"), "spec"), "issuerRef")
				ir["kind"], ir["name"] = "ClusterIssuer", "kacho-internal-ca"
			}},
		{"центр звеньев от кластерного выпускающего", "не намеспейсный",
			func(t *testing.T, docs []map[string]any) {
				ir := submap(submap(docNamed(t, docs, "Certificate", "api-gateway-front-link-ca"), "spec"), "issuerRef")
				ir["kind"], ir["name"] = "ClusterIssuer", "kacho-selfsigned"
			}},
		{"якорь звеньев = якорь установки", "тот же секрет",
			func(t *testing.T, docs []map[string]any) {
				spec := submap(submap(submap(docNamed(t, docs, "Deployment", "api-gateway"), "spec"), "template"), "spec")
				for _, v := range slice(spec, "volumes") {
					vm := v.(map[string]any)
					if str(vm, "name") == "front-link-ca" {
						submap(vm, "secret")["secretName"] = "api-gateway-client-tls"
					}
				}
			}},
		{"полоса консоли без клиентского листа", "не ставит proxy_ssl_certificate",
			func(t *testing.T, docs []map[string]any) {
				rewriteStrings(docNamed(t, docs, "ConfigMap", "ui-nginx"), "proxy_ssl_certificate ", "# proxy_ssl_certificate ")
			}},
		{"полоса консоли открытым текстом", "без рукопожатия",
			func(t *testing.T, docs []map[string]any) {
				rewriteStrings(docNamed(t, docs, "ConfigMap", "ui-nginx"), "proxy_pass https://$api_gw_upstream", "proxy_pass http://$api_gw_upstream")
			}},
		{"консоль зовёт открытый порт края", "а не на",
			func(t *testing.T, docs []map[string]any) {
				rewriteStrings(docNamed(t, docs, "Deployment", "ui"), "api-gateway.kacho.svc.cluster.local:8443", "api-gateway.kacho.svc.cluster.local:8080")
			}},
		{"контроллер без листа на gRPC", "grpc_ssl_certificate",
			func(t *testing.T, docs []map[string]any) {
				rewriteStrings(docNamed(t, docs, "ConfigMap", "kacho-umbrella-ingress-nginx-controller"), "grpc_ssl_certificate ", "# grpc_ssl_certificate ")
			}},
		{"контроллер не монтирует лист", "не монтирует",
			func(t *testing.T, docs []map[string]any) {
				rewriteStrings(docNamed(t, docs, "Deployment", "kacho-umbrella-ingress-nginx-controller"), "api-gateway-front-ingress-link", "someone-else-tls")
			}},
		{"контроллер берёт адрес из заголовка клиента", "use-forwarded-headers",
			func(t *testing.T, docs []map[string]any) {
				data := submap(docNamed(t, docs, "ConfigMap", "kacho-umbrella-ingress-nginx-controller"), "data")
				data["use-forwarded-headers"] = "true"
			}},
		{"раздача дописывает заголовок клиента", "ожидался $remote_addr",
			func(t *testing.T, docs []map[string]any) {
				rewriteStrings(docNamed(t, docs, "ConfigMap", "ui-nginx"), "proxy_set_header X-Forwarded-For $remote_addr;",
					"proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;")
			}},
		{"полосы края на хосте консоли снова через раздачу", "а не на порт",
			func(t *testing.T, docs []map[string]any) {
				ing := docNamed(t, docs, "Ingress", "console-edge-lanes")
				ing["kind"] = "RemovedByInjection"
			}},
	} {
		t.Run(inj.name, func(t *testing.T) {
			docs := npCopyDocs(base)
			inj.mutate(t, docs)
			got, _ := judgeEdgeLinkIdentity(npNamespace, docs)
			if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), inj.says) {
				t.Fatalf("инъекция не поймана либо находка не называет %q: %v", inj.says, got)
			}
		})
	}
}
