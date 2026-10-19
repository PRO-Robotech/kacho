// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// registry_forwarder_client_address_render_test.go — САЙДКАР РЕЕСТРА — ДОВЕРЕННЫЙ
// ПЕРЕСЫЛЬЩИК, И АДРЕС КЛИЕНТА ДО НЕГО ДОХОДИТ (kacho#3330; kaname#930).
//
// Служба доступа берёт адрес клиента из X-Forwarded-For только у пира с
// проверенным клиентским листом объявленного пересыльщика; адрес — ключ темпа
// анонимной выдачи токена реестра на источник. Сайдкар годен пересыльщиком,
// только если сходятся три части, и каждую судит рендер каждой цепочки
// deploy/stacks.txt тем же вызовом helm, которым стенд поднимается:
//
//  1. хоп `location = /iam/token` предъявляет клиентскую пару
//     (proxy_ssl_certificate, proxy_ssl_certificate_key), а контейнер сайдкара
//     монтирует по этому каталогу том с секретом;
//  2. заголовок ПЕРЕЗАПИСЫВАЕТСЯ адресом пира — ровно `$remote_addr`, а не
//     дописывается к присланному клиентом;
//  3. внешняя служба (LoadBalancer, NodePort), выбирающая под сайдкара, несёт
//     externalTrafficPolicy Local — при Cluster адрес пира есть адрес узла
//     (тот же замок, что у звена фронта края, judgeFrontExternalPolicy).
//
// ЗНАМЕНАТЕЛЬ — цепочки с сайдкаром и сверенные внешние службы; ноль любого —
// судить было нечего. Цепочка fe3455 — стенд предмета: сайдкар включён там.
package deploy_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

const (
	registrySidecarContainer = "dataplane-tls"
	registrySidecarMap       = "registry-dataplane-nginx"
	registrySubjectStack     = "fe3455"
)

// registryForwarder — что рендер одной цепочки говорит о сайдкаре реестра.
type registryForwarder struct {
	Sidecars, ExternalJudged int
}

// nginxDirectives — значения директивы в теле, без комментариев.
func nginxDirectives(body, name string) []string {
	var out []string
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) + `\s+([^;]+);`)
	for _, line := range strings.Split(body, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

// tokenHopBody — тело `location = /iam/token { … }`; пусто — блока нет.
func tokenHopBody(conf string) string {
	i := strings.Index(conf, "location = /iam/token {")
	if i < 0 {
		return ""
	}
	j := strings.Index(conf[i:], "}")
	if j < 0 {
		return conf[i:]
	}
	return conf[i : i+j]
}

// judgeRegistryForwarder судит рендер одной цепочки.
func judgeRegistryForwarder(stack string, docs []map[string]any) (findings []string, a registryForwarder) {
	say := func(f string, args ...any) { findings = append(findings, stack+": "+fmt.Sprintf(f, args...)) }
	var conf string
	for _, d := range docs {
		if docKind(d) == "ConfigMap" && docName(d) == registrySidecarMap {
			data, _ := d["data"].(map[string]any)
			conf, _ = data["default.conf"].(string)
		}
	}
	for _, d := range docs {
		tpl, ok := podTemplateOf(d)
		if !ok {
			continue
		}
		spec, _ := tpl["spec"].(map[string]any)
		var side map[string]any
		for _, c := range slice(spec, "containers") {
			if cm, _ := c.(map[string]any); str(cm, "name") == registrySidecarContainer {
				side = cm
			}
		}
		if side == nil {
			continue
		}
		a.Sidecars++
		hop := tokenHopBody(conf)
		if hop == "" {
			say("сайдкар %s поднят, а в карте %s нет location = /iam/token", docName(d), registrySidecarMap)
			continue
		}
		crt, key := nginxDirectives(hop, "proxy_ssl_certificate"), nginxDirectives(hop, "proxy_ssl_certificate_key")
		if len(crt) != 1 || len(key) != 1 {
			say("хоп /iam/token не предъявляет клиентскую пару (cert %v, key %v) — служба доступа не узнает "+
				"пересыльщика, и все клиенты за сайдкаром делят один темп", crt, key)
		} else {
			dir := strings.TrimSuffix(crt[0], "/tls.crt")
			mounted := false
			for _, m := range slice(side, "volumeMounts") {
				mm, _ := m.(map[string]any)
				if str(mm, "mountPath") == dir {
					mounted = true
				}
			}
			if !mounted {
				say("контейнер %s не монтирует каталог клиентской пары %s", registrySidecarContainer, dir)
			}
		}
		var xff []string
		for _, v := range nginxDirectives(hop, "proxy_set_header") {
			if f := strings.Fields(v); len(f) == 2 && strings.EqualFold(f[0], "X-Forwarded-For") {
				xff = append(xff, f[1])
			}
		}
		if len(xff) != 1 || xff[0] != "$remote_addr" {
			say("хоп /iam/token ставит X-Forwarded-For %v, ожидалось ровно [$remote_addr] — клиент выбирал бы "+
				"ключ темпа сам", xff)
		}
		tm, _ := tpl["metadata"].(map[string]any)
		labels := stringMap(tm["labels"])
		for _, s := range docs {
			if docKind(s) != "Service" {
				continue
			}
			ss, _ := s["spec"].(map[string]any)
			typ := str(ss, "type")
			if typ != "LoadBalancer" && typ != "NodePort" {
				continue
			}
			sel := stringMap(ss["selector"])
			if len(sel) == 0 || !(npSelector{match: sel}).matches(labels) {
				continue
			}
			a.ExternalJudged++
			if p := str(ss, "externalTrafficPolicy"); p != "Local" {
				say("внешняя служба %s (%s) перед сайдкаром %s: externalTrafficPolicy %q, ожидался Local — адрес "+
					"клиента подменяется адресом узла, и все клиенты — один источник", docName(s), typ, docName(d), orDash(p))
			}
		}
	}
	return findings, a
}

func TestEveryStackForwardsTheRegistryClientAddressAsATrustedForwarder(t *testing.T) {
	stacks := deployStacks(t)
	var sidecars, externals int
	subject := false
	for _, n := range sortedStackNames(stacks) {
		findings, a := judgeRegistryForwarder(n, npChainDocs(t, n))
		sidecars += a.Sidecars
		externals += a.ExternalJudged
		if n == registrySubjectStack && a.Sidecars > 0 {
			subject = true
		}
		t.Logf("цепочка %-11s: сайдкаров %d · внешних служб сверено %d · находок %d", n, a.Sidecars, a.ExternalJudged, len(findings))
		for _, f := range findings {
			t.Error(f)
		}
	}
	t.Logf("перепись: цепочек %d · сайдкаров %d · внешних служб сверено %d", len(stacks), sidecars, externals)
	if sidecars == 0 || externals == 0 {
		t.Fatalf("сайдкаров %d, внешних служб перед ними %d — судить было нечего", sidecars, externals)
	}
	if !subject {
		t.Errorf("цепочка %s не поднимает сайдкар реестра — предпосылка пробы исчезла", registrySubjectStack)
	}
}

// TestInjection_RegistryForwarderDefectsAreFound — каждая из трёх частей,
// снятая на рендере стенда предмета, — находка; нетронутый рендер молчит.
func TestInjection_RegistryForwarderDefectsAreFound(t *testing.T) {
	docs := npChainDocs(t, registrySubjectStack)
	if f, a := judgeRegistryForwarder(registrySubjectStack, docs); len(f) != 0 || a.Sidecars == 0 {
		t.Fatalf("законный близнец: находок %d, сайдкаров %d: %v", len(f), a.Sidecars, f)
	}
	for _, tc := range []struct {
		name, from, to, says string
		lb                   bool
	}{
		{"XFF дописывается", "X-Forwarded-For $remote_addr;", "X-Forwarded-For $proxy_add_x_forwarded_for;", "ожидалось ровно [$remote_addr]", false},
		{"пара не предъявляется", "proxy_ssl_certificate ", "# proxy_ssl_certificate ", "не предъявляет клиентскую пару", false},
		{"политика Cluster", "Local", "Cluster", "externalTrafficPolicy \"Cluster\"", true},
	} {
		mutated := make([]map[string]any, 0, len(docs))
		for _, orig := range docs {
			d := deepCopyDoc(orig).(map[string]any)
			switch {
			case !tc.lb && docKind(d) == "ConfigMap" && docName(d) == registrySidecarMap:
				d = rewriteStrings(d, tc.from, tc.to).(map[string]any)
			case tc.lb && docKind(d) == "Service":
				if spec, _ := d["spec"].(map[string]any); str(spec, "externalTrafficPolicy") == tc.from &&
					strings.HasSuffix(docName(d), "-lb") && strings.Contains(docName(d), "registry") {
					d = rewriteStrings(d, tc.from, tc.to).(map[string]any)
				}
			}
			mutated = append(mutated, d)
		}
		f, _ := judgeRegistryForwarder(registrySubjectStack, mutated)
		if !strings.Contains(strings.Join(f, "\n"), tc.says) {
			t.Errorf("%s: находки нет (%v)", tc.name, f)
		}
	}
}

// deepCopyDoc — копия документа рендера: rewriteStrings правит на месте, а
// случаи инъекции обязаны начинать с нетронутого рендера.
func deepCopyDoc(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = deepCopyDoc(e)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = deepCopyDoc(e)
		}
		return l
	}
	return v
}
