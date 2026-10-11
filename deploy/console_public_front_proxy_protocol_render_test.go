// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// АДРЕС КЛИЕНТА ОТ БАЛАНСИРОВЩИКА ПЛОЩАДКИ — заголовком PROXY (kacho#3115).
//
// Рендер-гейт п. 7 (console_public_front_render_test.go) судит цепочки дерева
// как они есть. Профиль площадки держит приём выключенным с причиной, поэтому
// включённая форма в дереве не рендерится ни одной цепочкой — и судья п. 7 на
// ней не исполнялся бы вовсе. Здесь она рендерится ручкой поверх той же
// цепочки, и судья исполняется на ней: законный близнец молчит, каждая
// инъекция называется. Отдельно — отказы рендера помощника
// ui.publicFrontProxyProtocol: каждый с законным близнецом.
package deploy_test

import (
	"strings"
	"testing"
)

const proxyProtocolStack = "a8f60d"

func TestConsolePublicFrontProxyProtocol_EnabledFormIsJudged(t *testing.T) {
	requireUmbrellaPackagedFromTree(t)
	stacks := deployStacks(t)
	chain, ok := stacks[proxyProtocolStack]
	if !ok {
		t.Fatalf("цепочки %q нет в таблице стеков", proxyProtocolStack)
	}
	var origin string
	for _, r := range readPublicFrontRenders(t) {
		if r.Stack == proxyProtocolStack {
			origin = r.Origin
		}
	}
	if origin == "" {
		t.Fatalf("у цепочки %q не прочитано происхождение консоли — судье не с чем сверять", proxyProtocolStack)
	}
	on := renderChainCached(t, chain, proxyProtocolOn)
	off := renderChainCached(t, chain)
	cases := []struct {
		name, base, from, to, mustSay string
	}{
		{name: "законный близнец: приём включён — молчит", base: on},
		{name: "законный близнец: приём выключен с причиной — молчит", base: off},
		{name: "порт redirect не принимает заголовок", base: on,
			from: "proxy_protocol;\n        server_name _;", to: ";\n        server_name _;",
			mustSay: "не принимает заголовок PROXY"},
		{name: "круг доверия шире одного адреса", base: on,
			from: "set_real_ip_from 192.0.2.1/32;", to: "set_real_ip_from 192.0.2.0/24;",
			mustSay: "шире одного адреса"},
		{name: "заголовок не берётся из PROXY", base: on,
			from: "real_ip_header proxy_protocol;", to: "real_ip_header X-Forwarded-For;",
			mustSay: "real_ip_header proxy_protocol"},
		{name: "Service не просит балансировщик слать заголовок", base: on,
			from: "lb.beget.com/proxy-protocol: v2", to: "lb.beget.com/proxy-protocol: none",
			mustSay: "не просит балансировщик"},
		{name: "выключено, а порт ждёт заголовок", base: off,
			from: "listen 8443 ssl;", to: "listen 8443 ssl proxy_protocol;",
			mustSay: "объявляет приём выключенным"},
		{name: "выключено без причины", base: off,
			from: proxyProtocolDisabledBecause + ":", to: "kacho.cloud/other:",
			mustSay: "причины выключения нет"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := c.base
			if c.from != "" {
				if strings.Count(text, c.from) != 1 {
					t.Fatalf("место инъекции %q в рендере найдено %d раз, ждали одно", c.from, strings.Count(text, c.from))
				}
				text = strings.Replace(text, c.from, c.to, 1)
			}
			findings, census := judgePublicFronts([]publicFrontRender{{Stack: proxyProtocolStack, Origin: origin, Docs: decodeRender(t, text)}})
			if census.Fronts != 1 {
				t.Fatalf("входов %d, подан один", census.Fronts)
			}
			if c.mustSay == "" {
				if len(findings) != 0 {
					t.Fatalf("законный близнец не молчит:\n%s", strings.Join(findings, "\n"))
				}
				return
			}
			if !strings.Contains(strings.Join(findings, "\n"), c.mustSay) {
				t.Errorf("ни одна находка не называет %q:\n%s", c.mustSay, strings.Join(findings, "\n"))
			}
		})
	}
}

func TestConsolePublicFrontProxyProtocol_RenderRefusals(t *testing.T) {
	stacks := deployStacks(t)
	chain, ok := stacks[proxyProtocolStack]
	if !ok {
		t.Fatalf("цепочки %q нет в таблице стеков", proxyProtocolStack)
	}
	const pp = "uif.publicFront.clientAddress.proxyProtocol"
	const askKey = `uif.publicFront.service.annotations.lb\.beget\.com/proxy-protocol`
	cases := []struct {
		name string
		sets []string
		want string // пусто — рендер обязан пройти
	}{
		{name: "законный близнец: приём включён", sets: []string{proxyProtocolOn}},
		{name: "просьба переобъявлена в service.annotations другим значением",
			sets: []string{proxyProtocolOn, askKey + "=none"}, want: "объявлена дважды"},
		{name: "законный близнец: то же значение в service.annotations",
			sets: []string{proxyProtocolOn, askKey + "=v2"}},
		{name: "круг доверия — подсеть", sets: []string{proxyProtocolOn, pp + ".trustedFrom={10.0.0.0/24}"},
			want: "шире одного адреса"},
		{name: "законный близнец: круг доверия — один адрес", sets: []string{proxyProtocolOn, pp + ".trustedFrom={10.0.0.7/32}"}},
		{name: "круг доверия пуст", sets: []string{proxyProtocolOn, pp + ".trustedFrom=null"}, want: "trustedFrom пуст"},
		{name: "просьба к балансировщику пуста", sets: []string{proxyProtocolOn, pp + ".serviceAnnotations=null"},
			want: "serviceAnnotations пуст"},
		{name: "выключено без причины", sets: []string{pp + ".disabledBecause="}, want: "без причины"},
	}
	for _, c := range cases {
		out, err := renderWithSiteLayer(t, chain, "", c.sets...)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: законный близнец отвергнут рендером: %v\n%s", c.name, err, lastLines(out, 5))
		case c.want != "" && err == nil:
			t.Errorf("%s: рендер прошёл, а помощник обязан был отказать (%q)", c.name, c.want)
		case c.want != "" && !strings.Contains(out, c.want):
			t.Errorf("%s: рендер отказал, но текст не называет %q:\n%s", c.name, c.want, lastLines(out, 5))
		default:
			t.Logf("%s: исход как ждали", c.name)
		}
	}
}
