// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// АДРЕС КЛИЕНТА ОТ БАЛАНСИРОВЩИКА ПЛОЩАДКИ — заголовком PROXY (kacho#3115).
//
// Рендер-гейт п. 7 (console_public_front_render_test.go) судит цепочки дерева
// как они есть. Профиль площадки держит приём включённым, поэтому выключенная
// форма в дереве не рендерится ни одной цепочкой со входом LoadBalancer — и
// судья п. 7 на ней не исполнялся бы вовсе. Здесь обе формы рендерятся
// ручками поверх той же цепочки, и судья исполняется на каждой: законный
// близнец молчит, каждая инъекция называется. Отдельно — отказы рендера
// помощника ui.publicFrontProxyProtocol: каждый с законным близнецом.
package deploy_test

import (
	"strings"
	"testing"
)

const proxyProtocolStack = "a8f60d"

// proxyProtocolOff — приём заголовка PROXY выключен с причиной: форма, которой
// цепочка дерева больше не рендерит, но которую чарт обязан держать.
var proxyProtocolOff = []string{
	"uif.publicFront.clientAddress.proxyProtocol.enabled=false",
	"uif.publicFront.clientAddress.proxyProtocol.disabledBecause=проба выключенной формы",
}

// Имя продолжает семейство TestConsolePublicFrontJudgement_CanFailAndStaysSilent:
// это тот же судья п. 7 на той же цепочке, и прогон конвейера, отбирающий
// семейство по имени (.github/workflows/ci.yaml, прогон helmcharts), отбирает и
// эту пробу. Отказы рендера помощника — её подпроба.
func TestConsolePublicFrontJudgement_CanFailAndStaysSilentOnTheProxyProtocolForm(t *testing.T) {
	t.Run("отказы рендера помощника", proxyProtocolRenderRefusals)
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
	off := renderChainCached(t, chain, proxyProtocolOff...)
	const (
		routerListen  = "listen 8081;\n            proxy_pass $kacho_redirect_route;"
		routerTrusted = "192.0.2.1/32 127.0.0.1:8082;"
		loopProxied   = "listen 127.0.0.1:8082 proxy_protocol;"
		loopPlain     = "listen 127.0.0.1:8083;"
	)
	cases := []struct {
		name, base, from, to, mustSay string
		more                          [][2]string // ещё правки того же факта
	}{
		{name: "законный близнец: приём включён — молчит", base: on},
		{name: "законный близнец: приём выключен с причиной — молчит", base: off},
		{name: "порт https не принимает заголовок", base: on,
			from: "listen 8443 ssl proxy_protocol;", to: "listen 8443 ssl;",
			mustSay: "не принимает заголовок PROXY"},
		{name: "порт redirect ждёт заголовок от каждого соединения (без маршрутизатора)", base: on,
			from: routerListen, to: "listen 8079;\n            proxy_pass $kacho_redirect_route;",
			more:    [][2]string{{loopPlain, "listen 8081 proxy_protocol;"}},
			mustSay: "ждёт заголовок PROXY от КАЖДОГО соединения"},
		{name: "маршрутизатор сам ждёт заголовок", base: on,
			from: routerListen, to: "listen 8081 proxy_protocol;\n            proxy_pass $kacho_redirect_route;",
			mustSay: "сам ждёт заголовок PROXY"},
		{name: "маршрутизатор дописывает свой заголовок", base: on,
			from: routerListen, to: routerListen + "\n            proxy_protocol on;",
			mustSay: "дописывает свой заголовок PROXY"},
		{name: "запись маршрутизатора шире одного адреса", base: on,
			from: routerTrusted, to: "192.0.2.0/24 127.0.0.1:8082;",
			mustSay: "шире одного адреса"},
		{name: "круг маршрутизатора расходится с кругом порта https", base: on,
			from: routerTrusted, to: "192.0.2.9/32 127.0.0.1:8082;",
			mustSay: "расходится с кругом доверия порта https"},
		{name: "звено балансировки ведётся на слушатель без заголовка", base: on,
			from: loopProxied, to: "listen 127.0.0.1:8082;",
			mustSay: "не принимает заголовок PROXY — первая строка"},
		{name: "прочие соединения ведутся на слушатель с заголовком", base: on,
			from: loopPlain, to: "listen 127.0.0.1:8083 proxy_protocol;",
			mustSay: "ждёт заголовок PROXY — соединение изнутри кластера отвергается"},
		{name: "блок переадресации верит заголовку не только от петли", base: on,
			from: "set_real_ip_from 127.0.0.1;", to: "set_real_ip_from 0.0.0.0/0;",
			mustSay: "не только от петли"},
		{name: "nginx.conf маршрутизатора не смонтирован", base: on,
			from: "mountPath: /etc/nginx/nginx.conf", to: "mountPath: /etc/nginx/nginx-unused.conf",
			mustSay: "не смонтирован"},
		{name: "выключено, а порт redirect слушает маршрутизатор", base: on,
			from: "lb.beget.com/proxy-protocol: v2", to: proxyProtocolDisabledBecause + ": проба",
			mustSay: "слушает маршрутизатор заголовка PROXY, а Service объявляет приём выключенным"},
		{name: "круг доверия шире одного адреса", base: on,
			from: "set_real_ip_from 192.0.2.1/32;", to: "set_real_ip_from 192.0.2.0/24;",
			mustSay: "шире одного адреса"},
		{name: "заголовок не берётся из PROXY", base: on,
			from: "real_ip_header proxy_protocol;\n        set_real_ip_from 192.0.2.1/32;",
			to:   "real_ip_header X-Forwarded-For;\n        set_real_ip_from 192.0.2.1/32;",
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
			edits := c.more
			if c.from != "" {
				edits = append([][2]string{{c.from, c.to}}, edits...)
			}
			for _, e := range edits {
				if strings.Count(text, e[0]) != 1 {
					t.Fatalf("место инъекции %q в рендере найдено %d раз, ждали одно", e[0], strings.Count(text, e[0]))
				}
				text = strings.Replace(text, e[0], e[1], 1)
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

func proxyProtocolRenderRefusals(t *testing.T) {
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
		{name: "выключено без причины", sets: []string{pp + ".enabled=false"}, want: "без причины"},
		{name: "законный близнец: выключено с причиной", sets: proxyProtocolOff},
		{name: "петлевой порт маршрутизатора занят портом redirect", sets: []string{proxyProtocolOn, "uif.publicFront.redirectPort=8082"},
			want: "петлевым портом маршрутизатора"},
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
