// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// console_public_front_render_injection_test.go — сверка внешнего входа консоли
// СПОСОБНА упасть и способна смолчать (kacho#3024). Вход — НАСТОЯЩИЙ рендер
// цепочки дерева, несущей вход; каждая инъекция меняет в нём ровно один факт, а
// законный близнец — тот же рендер без правки — молчит.
package deploy_test

import (
	"strings"
	"testing"
)

func TestConsolePublicFrontJudgement_CanFailAndStaysSilent(t *testing.T) {
	var front *publicFrontRender
	var raw string
	stacks := deployStacks(t)
	for _, r := range readPublicFrontRenders(t) {
		if _, c := judgePublicFronts([]publicFrontRender{r}); c.Fronts == 1 {
			r := r
			front = &r
			raw = renderOfFront(t, stacks, r.Stack)
			break
		}
	}
	if front == nil {
		t.Fatal("в дереве нет цепочки с внешним входом консоли — инъекциям некуда попасть")
	}
	host := strings.TrimPrefix(front.Origin, "https://")
	if host == "" || host == front.Origin {
		t.Fatalf("происхождение входа %q не по https — инъекциям не от чего отталкиваться", front.Origin)
	}
	cases := []struct {
		name, from, to string
		all            bool // заменить каждое вхождение, а не первое
		origin         string
		mustSay        string
	}{
		{name: "законный близнец — молчит"},
		{
			name: "порт 80 ведёт на внутренний http раздачи",
			from: "      targetPort: redirect", to: "      targetPort: http",
			mustSay: "открытый http опубликован наружу",
		},
		{
			name: "TLS-порт без ssl",
			from: "listen 8443 ssl;", to: "listen 8443;",
			mustSay: "без ssl",
		},
		{
			name: "переадресация отражает заголовок Host",
			from: "return 308 " + front.Origin + "$request_uri;", to: "return 308 https://$host$request_uri;",
			mustSay: "уводит на",
		},
		{
			name:    "слушатель redirect обслуживает край",
			from:    "return 308 " + front.Origin + "$request_uri;",
			to:      "proxy_pass http://upstream;\n            return 308 " + front.Origin + "$request_uri;",
			mustSay: "что-то обслуживает",
		},
		{
			name: "сертификат не называет хост происхождения",
			from: "    - " + host + "\n", to: "    - 192.0.2.250\n",
			mustSay: "не назван ни в одном сертификате",
		},
		{
			name: "Service подменяет адрес клиента адресом узла",
			from: "externalTrafficPolicy: Local", to: "externalTrafficPolicy: Cluster",
			mustSay: "ожидался Local",
		},
		{
			name: "раздача дописывает заголовок клиента вместо своего",
			from: "proxy_set_header X-Forwarded-For $remote_addr;", to: "proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
			mustSay: "ожидался $remote_addr",
		},
		{
			// Полоса ресурсов к краю (а не первая найденная): строку убирают в ОДНОЙ
			// полосе, остальные целы, — судья обязан назвать именно её.
			name:    "полоса ресурсов к краю без своей строки X-Forwarded-For",
			from:    "proxy_pass http://$api_gw_upstream;\n            proxy_http_version 1.1;\n            proxy_set_header Host $host;\n            proxy_set_header X-Forwarded-For $remote_addr;\n",
			to:      "proxy_pass http://$api_gw_upstream;\n            proxy_http_version 1.1;\n            proxy_set_header Host $host;\n",
			mustSay: "location ~ ^/(vpc|compute|storage|geo|nlb|registry|operations)/` проксирует",
		},
		{
			name:    "полоса модуля консоли без своей строки X-Forwarded-For",
			from:    "proxy_pass http://$vpc_upstream;\n            proxy_http_version 1.1;\n            proxy_set_header Host $host;\n            proxy_set_header X-Forwarded-For $remote_addr;\n",
			to:      "proxy_pass http://$vpc_upstream;\n            proxy_http_version 1.1;\n            proxy_set_header Host $host;\n",
			mustSay: "проксирует (http://$vpc_upstream) без своей строки",
		},
		{
			// X-Real-IP — второй заголовок адреса, который раздача несёт дальше
			// (kacho#3028, C1): дописанный или пропущенный, он отдаёт заявление
			// клиента о себе звену за раздачей так же, как X-Forwarded-For.
			name:    "полоса к краю без своей строки X-Real-IP",
			from:    "proxy_set_header X-Forwarded-For $remote_addr;\n            proxy_set_header X-Real-IP $remote_addr;\n            proxy_set_header X-Request-ID $http_x_request_id;\n\n",
			to:      "proxy_set_header X-Forwarded-For $remote_addr;\n            proxy_set_header X-Request-ID $http_x_request_id;\n\n",
			mustSay: "без своей строки X-Real-IP",
		},
		{
			name: "полоса передаёт X-Real-IP клиента",
			from: "proxy_set_header X-Real-IP $remote_addr;", to: "proxy_set_header X-Real-IP $http_x_real_ip;",
			mustSay: "X-Real-IP = $http_x_real_ip, ожидался $remote_addr",
		},
		{
			// Нулевой обход: ни одной проксирующей полосы — судье п. 6 нечего
			// судить, и «каждая полоса пишет адрес сама» не бывает истинным пусто.
			name: "нулевой обход — ни одной проксирующей полосы",
			from: "proxy_pass ", to: "#proxy_pass ", all: true,
			mustSay: "нет ни одной проксирующей полосы",
		},
		{
			name: "происхождение консоли по http", origin: "http://" + host,
			mustSay: "не по https",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := raw
			if c.from != "" {
				if strings.Count(text, c.from) == 0 {
					t.Fatalf("инъекция не нашла места %q в рендере — она бы ничего не проверила", c.from)
				}
				n := 1
				if c.all {
					n = -1
				}
				text = strings.Replace(text, c.from, c.to, n)
			}
			r := publicFrontRender{Stack: front.Stack, Origin: front.Origin, Docs: decodeRender(t, text)}
			if c.origin != "" {
				r.Origin = c.origin
			}
			findings, census := judgePublicFronts([]publicFrontRender{r})
			if census.Fronts != 1 {
				t.Fatalf("входов %d, подан один", census.Fronts)
			}
			if c.mustSay == "" {
				if len(findings) != 0 {
					t.Fatalf("законный близнец не молчит: %v", findings)
				}
				return
			}
			if !strings.Contains(strings.Join(findings, "\n"), c.mustSay) {
				t.Errorf("ни одна находка не называет %q: %v", c.mustSay, findings)
			}
		})
	}
}

// renderOfFront — сырой рендер цепочки входа: дерева — как есть, фикстурной —
// с ручками фикстуры поверх её профилей.
func renderOfFront(t *testing.T, stacks map[string][]string, stack string) string {
	t.Helper()
	if chain, ok := stacks[stack]; ok {
		return renderChainCached(t, chain)
	}
	if base, ok := strings.CutSuffix(stack, "+вход-фикстура"); ok {
		if chain, ok := stacks[base]; ok {
			return renderChainCached(t, chain, publicFrontFixtureSets...)
		}
	}
	t.Fatalf("цепочка входа %q не опознана ни в дереве, ни как фикстура", stack)
	return ""
}
