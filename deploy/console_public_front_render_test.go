// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// console_public_front_render_test.go — ВНЕШНИЙ ВХОД КОНСОЛИ, КОТОРЫЙ ВЫРАЖАЕТ
// ЧАРТ, ЗАВЕРШАЕТ TLS И НЕ ОБСЛУЖИВАЕТ ОТКРЫТЫЙ HTTP (kacho#3024). Судится
// РЕНДЕР каждой цепочки deploy/stacks.txt.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Стенд, отдававший консоль балансировщиком площадки по одному порту 80/http,
// терял Secure-печенье формы: законная регистрация получала
// `403 FORM_TOKEN_REJECTED`. Вход теперь выражает чарт консоли (`uif.publicFront`),
// и о его рендере утверждается ровно то, что нужно браузеру:
//
//  1. Service типа LoadBalancer к раздаче консоли публикует РОВНО два порта:
//     443 → именованный порт `https` и 80 → именованный порт `redirect`; ни один
//     порт не ведёт на внутренний `http` раздачи;
//  2. у пода раздачи оба именованных порта есть, а лист TLS смонтирован томом
//     секрета (не subPath — иначе продление не доходит до файлов);
//  3. серверный блок раздачи слушает порт `https` с `ssl`, а слушатель
//     `redirect` — отдельный блок, в котором нет ничего, кроме `308` на
//     ОБЪЯВЛЕННОЕ происхождение консоли (не на заголовок `Host`), то есть по
//     http ни одна форма не обслуживается;
//  4. происхождение консоли — `https`, и его хост назван в сертификате, который
//     выписывается в смонтированный секрет.
//
// ЗНАМЕНАТЕЛЬ. Судятся две вещи, и обе обязательны:
//
//   - каждая цепочка deploy/stacks.txt, которая рендерит вход, — как есть;
//   - ФИКСТУРНАЯ цепочка: та цепочка, ради стенда которой вход заведён
//     (`publicFrontFixtureStack`), со входом, включённым ручками чарта поверх её
//     профилей (`publicFrontFixtureSets`). Она есть всегда, поэтому входов в
//     переписи не меньше одного, и «на всех цепочках со входом всё верно» не
//     бывает истинным и пустым.
//
// Почему фикстура, а не профиль стенда. В профиле a8f60d вход сегодня
// ВЫКЛЮЧЕН: выпуск сертификата на IP-литерал отвергнут решением владельца, вход
// стенда переезжает на доменное имя (kacho#3024). Открытый http этой цепочки
// при этом не прощён молча — его называет запись-исключение сверки
// происхождения (console_origin_is_secure_test.go) с той же задачей. Фикстура
// держит хост в зарезервированной зоне `.example` (RFC 2606): адреса стенда в
// сверку не копируются.
//
// Способность упасть — console_public_front_render_injection_test.go.
package deploy_test

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// publicFrontRender — что рендер одной цепочки говорит о внешнем входе консоли.
type publicFrontRender struct {
	Stack  string
	Origin string           // происхождение консоли, объявленное цепочкой
	Docs   []map[string]any // документы рендера
}

type publicFrontCensus struct {
	Stacks, Fronts int
}

func (c publicFrontCensus) String() string {
	return fmt.Sprintf("цепочек осмотрено %d · с внешним входом консоли %d", c.Stacks, c.Fronts)
}

const consoleHostComponent = "host"

func docKind(d map[string]any) string { k, _ := d["kind"].(string); return k }

func docName(d map[string]any) string {
	v, _ := lookup(d, "metadata", "name")
	s, _ := v.(string)
	return s
}

// selects — выбирает ли селектор Service под с метками labels.
func selects(selector, labels map[string]any) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

var (
	nginxServerBlock = regexp.MustCompile(`(?s)server \{(.*?)\n    \}`)
	listenDirective  = regexp.MustCompile(`(?m)^\s*listen\s+(\d+)(\s+ssl)?\s*;`)
	redirectReturn   = regexp.MustCompile(`(?m)^\s*return 308 (\S+)\$request_uri;\s*$`)
	nginxDirective   = regexp.MustCompile(`(?m)^\s*(proxy_pass|try_files|root|alias|fastcgi_pass|grpc_pass)\b`)
)

// judgePublicFronts — НАХОДКИ по рендерам. Чистая функция.
func judgePublicFronts(renders []publicFrontRender) ([]string, publicFrontCensus) {
	var findings []string
	var census publicFrontCensus
	for _, r := range renders {
		census.Stacks++
		for _, svc := range r.Docs {
			if docKind(svc) != "Service" {
				continue
			}
			if t, _ := lookup(svc, "spec", "type"); t != "LoadBalancer" {
				continue
			}
			selV, _ := lookup(svc, "spec", "selector")
			sel, _ := selV.(map[string]any)
			if sel["app.kubernetes.io/component"] != consoleHostComponent {
				continue
			}
			census.Fronts++
			findings = append(findings, judgeOneFront(r, svc, sel)...)
		}
	}
	if census.Fronts == 0 {
		findings = append(findings, "ни одна цепочка не рендерит внешнего входа консоли (Service типа LoadBalancer к раздаче) — "+
			"стенд, отдающий консоль наружу, остался без TLS-входа, выраженного чартом (kacho#3024)")
	}
	return findings, census
}

func judgeOneFront(r publicFrontRender, svc, sel map[string]any) []string {
	var out []string
	say := func(format string, a ...any) {
		out = append(out, fmt.Sprintf("цепочка %s, Service %s: ", r.Stack, docName(svc))+fmt.Sprintf(format, a...))
	}
	// 1. порты Service
	ports := map[string]any{}
	portsV, _ := lookup(svc, "spec", "ports")
	pl, _ := portsV.([]any)
	for _, p := range pl {
		pm, _ := p.(map[string]any)
		ports[fmt.Sprint(pm["port"])] = pm["targetPort"]
		if pm["targetPort"] == "http" {
			say("порт %v ведёт на внутренний `http` раздачи — открытый http опубликован наружу", pm["port"])
		}
	}
	if len(pl) != 2 || ports["443"] != "https" || ports["80"] != "redirect" {
		say("порты %v, ожидались ровно 443→https и 80→redirect", ports)
	}
	// 2. под раздачи
	var pod map[string]any
	for _, d := range r.Docs {
		if docKind(d) != "Deployment" {
			continue
		}
		lv, _ := lookup(d, "spec", "template", "metadata", "labels")
		if lm, _ := lv.(map[string]any); selects(sel, lm) {
			pod = d
		}
	}
	if pod == nil {
		say("ни один Deployment рендера не выбирается селектором %v", sel)
		return out
	}
	named := map[string]int{}
	tlsMount := false
	cs, _ := lookup(pod, "spec", "template", "spec", "containers")
	csl, _ := cs.([]any)
	for _, c := range csl {
		cm, _ := c.(map[string]any)
		ps, _ := cm["ports"].([]any)
		for _, p := range ps {
			pm, _ := p.(map[string]any)
			if n, ok := pm["name"].(string); ok {
				named[n] = toInt(pm["containerPort"])
			}
		}
		vms, _ := cm["volumeMounts"].([]any)
		for _, vm := range vms {
			m, _ := vm.(map[string]any)
			if m["name"] == "console-tls" {
				tlsMount = true
				if m["subPath"] != nil {
					say("лист TLS смонтирован через subPath — продлённый сертификат до файлов не дойдёт")
				}
			}
		}
	}
	if named["https"] == 0 || named["redirect"] == 0 {
		say("у пода раздачи нет именованных портов https/redirect (%v)", named)
		return out
	}
	secret := ""
	vs, _ := lookup(pod, "spec", "template", "spec", "volumes")
	vsl, _ := vs.([]any)
	for _, v := range vsl {
		vm, _ := v.(map[string]any)
		if vm["name"] == "console-tls" {
			sn, _ := lookup(vm, "secret", "secretName")
			secret, _ = sn.(string)
		}
	}
	if !tlsMount || secret == "" {
		say("лист TLS не смонтирован из секрета (том console-tls)")
	}
	// 3. настройка раздачи
	conf := ""
	for _, d := range r.Docs {
		if docKind(d) == "ConfigMap" && strings.HasSuffix(docName(d), "-nginx") {
			if t, ok := lookup(d, "data", "default.conf.template"); ok {
				if s, _ := t.(string); strings.Contains(s, fmt.Sprintf("listen %d", named["redirect"])) {
					conf = s
				}
			}
		}
	}
	if conf == "" {
		say("ни одна карта настройки раздачи не слушает порт redirect %d", named["redirect"])
		return out
	}
	u, err := url.Parse(r.Origin)
	if err != nil || u.Scheme != "https" {
		say("происхождение консоли %q не по https — Secure-печенье формы с него браузер не хранит", r.Origin)
		return out
	}
	tlsServed, redirectOnly := false, false
	for _, m := range nginxServerBlock.FindAllStringSubmatch(conf, -1) {
		block := m[1]
		for _, l := range listenDirective.FindAllStringSubmatch(block, -1) {
			port := toInt(l[1])
			switch {
			case port == named["https"] && l[2] != "":
				tlsServed = true
			case port == named["https"]:
				say("порт https %d слушается без ssl", port)
			case port == named["redirect"]:
				ret := redirectReturn.FindStringSubmatch(block)
				switch {
				case ret == nil:
					say("слушатель redirect %d не отвечает `308 <происхождение>$request_uri`", port)
				case ret[1] != r.Origin:
					say("слушатель redirect уводит на %q, а происхождение консоли — %q", ret[1], r.Origin)
				case nginxDirective.MatchString(block):
					say("слушатель redirect %d что-то обслуживает (%s) — по http не должно обслуживаться ничего",
						port, nginxDirective.FindString(block))
				default:
					redirectOnly = true
				}
			}
		}
	}
	if !tlsServed {
		say("ни один серверный блок не слушает порт https %d с ssl", named["https"])
	}
	if !redirectOnly {
		say("слушателя redirect %d, отвечающего одной переадресацией, нет", named["redirect"])
	}
	// 4. сертификат
	host := u.Hostname()
	covered := false
	for _, d := range r.Docs {
		if docKind(d) != "Certificate" {
			continue
		}
		if sn, _ := lookup(d, "spec", "secretName"); sn != secret {
			continue
		}
		for _, key := range []string{"dnsNames", "ipAddresses"} {
			v, _ := lookup(d, "spec", key)
			l, _ := v.([]any)
			for _, n := range l {
				if n == host {
					covered = true
				}
			}
		}
	}
	if !covered {
		say("хост происхождения %q не назван ни в одном сертификате секрета %q", host, secret)
	}
	return out
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		var i int
		_, _ = fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}

// publicFrontFixtureStack — цепочка, ради стенда которой вход заведён; её
// профили — основа фикстуры.
const publicFrontFixtureStack = "a8f60d"

// publicFrontFixtureOrigin — происхождение фикстуры: зарезервированная зона
// `.example` (RFC 2606), не адрес стенда.
const publicFrontFixtureOrigin = "https://console.stand.example"

// publicFrontFixtureSets — вход, включённый ручками чарта: ровно те, что
// объявляет профиль, переводя стенд на вход по доменному имени.
var publicFrontFixtureSets = []string{
	"global.kacho.identity.appBaseURL=" + publicFrontFixtureOrigin,
	"uif.publicFront.enabled=true",
	"uif.publicFront.tls.secretName=console-public-tls",
	"uif.publicFront.tls.certificate.create=true",
	"uif.publicFront.tls.certificate.issuerRef.name=console-public",
	"uif.publicFront.tls.certificate.dnsNames[0]=console.stand.example",
}

// readPublicFrontRenders — рендер каждой цепочки с её объявленным
// происхождением и, последней, фикстурная цепочка со входом.
func readPublicFrontRenders(t *testing.T) []publicFrontRender {
	t.Helper()
	stacks := deployStacks(t)
	origins := map[string]string{}
	for _, f := range readConsoleOriginFacts(t, nil) {
		origins[f.Stack] = f.Origin
	}
	var out []publicFrontRender
	for _, n := range sortedStackNames(stacks) {
		out = append(out, publicFrontRender{
			Stack: n, Origin: origins[n], Docs: decodeRender(t, renderChainCached(t, stacks[n])),
		})
	}
	chain, ok := stacks[publicFrontFixtureStack]
	if !ok {
		t.Fatalf("цепочки %q нет в таблице стеков — фикстуре входа не на чем стоять, "+
			"переписи входов не с чем сверяться", publicFrontFixtureStack)
	}
	out = append(out, publicFrontRender{
		Stack:  publicFrontFixtureStack + "+вход-фикстура",
		Origin: publicFrontFixtureOrigin,
		Docs:   decodeRender(t, renderChainCached(t, chain, publicFrontFixtureSets...)),
	})
	return out
}

func TestConsolePublicFrontTerminatesTLSAndServesNoPlainHTTP(t *testing.T) {
	renders := readPublicFrontRenders(t)
	findings, census := judgePublicFronts(renders)
	t.Logf("перепись: %s", census)
	for _, f := range findings {
		t.Error(f)
	}
}
