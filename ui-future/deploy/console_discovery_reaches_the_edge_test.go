// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_discovery_reaches_the_edge_test.go — ОБЪЯВЛЕНИЕ ЦЕРЕМОНИИ OAUTH НА
// ВХОДЕ КОНСОЛИ ДОСТАЁТСЯ КРАЮ, И РОВНО ОНО ОДНО (kacho#3060).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Край объявляет координату `/.well-known/oauth-authorization-server`
// (RFC 8414). Вход API-шлюза её несёт; раздача консоли — нет: адрес доставался
// общему `location /`, а тот отдаёт оболочку консоли с кодом `200` и типом
// `text/html`. Клиент церемонии, спросивший объявление у происхождения консоли,
// получал страницу вместо документа — со стороны это «поставщик не настроен».
//
// Утверждается, КАКОЙ блок раздача выберет для адреса (порядок разрешения —
// console_serving_template_test.go), а не наличие строки.
//
// ─────────────────────────────────────────────────────────────────────────────
// РОВНО ОДИН АДРЕС (запрет #6)
//
// Блок — ТОЧНОЕ совпадение. Приставка `/.well-known/` открыла бы краю всё
// пространство имён разом, включая то, чего в нём ещё нет. Поэтому соседи —
// `/.well-known/openid-configuration`, форма с хвостом по RFC 8414 §3.1,
// склейка без разделителя — обязаны краю НЕ доставаться: их обслуживает
// оболочка. Отрицание судится вместе с положительным контролем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Что край на этот адрес отвечает документом — предмет проб самой ручки. Что
// документ отвечает снаружи на стенде — проба после выкатки (kacho#3067). На
// цепочках, где хост консоли ведёт контроллер входа, исход маршрутизации судит
// deploy/edge_front_link_identity_render_test.go.
package deploy_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// discoveryPath — координата объявления, как её видит клиент церемонии.
const discoveryPath = "/.well-known/oauth-authorization-server"

// discoveryNeighbours — адреса, которые краю доставаться НЕ обязаны.
var discoveryNeighbours = []string{
	"/.well-known/openid-configuration",
	discoveryPath + "/kacho",
	discoveryPath + "x",
	"/.well-known/",
}

// clientAddressWrittenRe — раздача пишет адрес своего TCP-пира, а не дописывает
// заявленный клиентом (kacho#3028).
var clientAddressWrittenRe = regexp.MustCompile(`(?m)^\s*proxy_set_header\s+X-Forwarded-For\s+\$remote_addr\s*;`)

// edgeLinkTLSRe — полоса к краю предъявляет лист звена (kacho#3028).
var edgeLinkTLSRe = regexp.MustCompile(`include\s+"ui\.edgeLinkTLS"`)

// discoveryCensus — объём осмотренного.
type discoveryCensus struct {
	Locs, EdgeLocs, Neighbours int
}

func (c discoveryCensus) String() string {
	return fmt.Sprintf("блоков %d · ведут на край %d · соседей осмотрено %d", c.Locs, c.EdgeLocs, c.Neighbours)
}

// judgeDiscoveryRouting — НАХОДКИ по блокам серверного блока консоли. Чистая
// функция: инъекция подаёт ей синтетический вход.
func judgeDiscoveryRouting(locs []nginxLoc) ([]string, discoveryCensus) {
	var findings []string
	c := discoveryCensus{Locs: len(locs)}
	for _, l := range locs {
		if edgeUpstreamRe.MatchString(l.body) {
			c.EdgeLocs++
		}
	}
	if c.EdgeLocs == 0 {
		return []string{"ни один блок не ведёт на край — судить исход не с чем"}, c
	}

	idx, why := selectLocation(discoveryPath, locs)
	switch {
	case idx < 0:
		findings = append(findings, fmt.Sprintf("адрес %q не выбирает ни одного блока", discoveryPath))
	case !edgeUpstreamRe.MatchString(locs[idx].body):
		findings = append(findings, fmt.Sprintf("адрес %q достаётся блоку %s (%s), который НЕ ведёт на край: "+
			"клиент церемонии получает оболочку консоли вместо объявления", discoveryPath, locs[idx].name(), why))
	default:
		l := locs[idx]
		if l.mod != "=" || l.spec != discoveryPath {
			findings = append(findings, fmt.Sprintf("адрес %q ведёт на край блок %s — не точное совпадение: "+
				"блок открывает краю больше одного адреса (запрет #6)", discoveryPath, l.name()))
		}
		if !clientAddressWrittenRe.MatchString(l.body) {
			findings = append(findings, fmt.Sprintf("блок %s не пишет краю адрес своего пира "+
				"(`X-Forwarded-For $remote_addr`) — заявление клиента ушло бы дальше раздачи", l.name()))
		}
		if !edgeLinkTLSRe.MatchString(l.body) {
			findings = append(findings, fmt.Sprintf("блок %s не предъявляет краю лист звена (`ui.edgeLinkTLS`)", l.name()))
		}
	}

	for _, n := range discoveryNeighbours {
		c.Neighbours++
		j, nwhy := selectLocation(n, locs)
		if j >= 0 && edgeUpstreamRe.MatchString(locs[j].body) {
			findings = append(findings, fmt.Sprintf("соседний адрес %q достаётся краю блоком %s (%s) — "+
				"наружу открыто больше одного адреса", n, locs[j].name(), nwhy))
		}
	}
	return findings, c
}

// TestConsoleDiscoveryReachesTheEdgeAndOnlyIt — суд по объявлению раздачи из дерева.
func TestConsoleDiscoveryReachesTheEdgeAndOnlyIt(t *testing.T) {
	srv, path := consoleServer(t)
	findings, c := judgeDiscoveryRouting(srv.locs)
	t.Logf("осмотрено: %s · серверный блок консоли строка %d · %s", path, srv.line, c)
	for _, f := range findings {
		t.Error(f)
	}
}

// discoveryFixture — серверный блок с полосой края и заданным блоком объявления.
func discoveryFixture(block string) string {
	return strings.Join([]string{
		"server {",
		"    location ~ ^/iam/v1/ {",
		"        set $api_gw_upstream \"${KACHO_UI_API_GATEWAY_UPSTREAM}\";",
		"        {{- include \"ui.edgeLinkTLS\" . | nindent 12 }}",
		"        proxy_set_header X-Forwarded-For $remote_addr;",
		"    }",
		block,
		"    location / {",
		"        try_files $uri $uri/ /index.html;",
		"    }",
		"}",
	}, "\n")
}

const discoveryEdgeBody = `        set $api_gw_upstream "${KACHO_UI_API_GATEWAY_UPSTREAM}";
        {{- include "ui.edgeLinkTLS" . | nindent 12 }}
        proxy_set_header X-Forwarded-For $remote_addr;
    }`

// TestConsoleDiscoveryJudgement_CanFailAndStaysSilent — суд падает на каждом
// воспроизведённом дефекте и молчит на законном близнеце той же формы.
func TestConsoleDiscoveryJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name, block, want string
	}{
		{name: "законный близнец: точное совпадение к краю", block: "    location = " + discoveryPath + " {\n" + discoveryEdgeBody},
		{name: "блока нет — адрес достаётся оболочке", block: "", want: "НЕ ведёт на край"},
		{name: "приставка вместо точного совпадения", block: "    location ^~ /.well-known/ {\n" + discoveryEdgeBody,
			want: "не точное совпадение"},
		{name: "приставка открывает соседей", block: "    location ^~ /.well-known/ {\n" + discoveryEdgeBody,
			want: "/.well-known/openid-configuration"},
		{name: "дописывает заявление клиента", block: "    location = " + discoveryPath + " {\n" +
			strings.Replace(discoveryEdgeBody, "$remote_addr", "$proxy_add_x_forwarded_for", 1), want: "адрес своего пира"},
		{name: "без листа звена", block: "    location = " + discoveryPath + " {\n" +
			strings.Replace(discoveryEdgeBody, `include "ui.edgeLinkTLS"`, `include "ui.other"`, 1), want: "лист звена"},
	}
	for _, c := range cases {
		servers := parseServingTemplate(t, discoveryFixture(c.block))
		if len(servers) != 1 {
			t.Fatalf("%s: разобрано серверных блоков %d, ждали 1", c.name, len(servers))
		}
		findings, census := judgeDiscoveryRouting(servers[0].locs)
		got := strings.Join(findings, "\n")
		switch {
		case c.want == "" && len(findings) != 0:
			t.Errorf("%s: законный близнец назван находкой (%s):\n%s", c.name, census, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s: суд не назвал %q (%s):\n%s", c.name, c.want, census, got)
		}
		if census.Neighbours != len(discoveryNeighbours) {
			t.Errorf("%s: соседей осмотрено %d из %d", c.name, census.Neighbours, len(discoveryNeighbours))
		}
	}
}
