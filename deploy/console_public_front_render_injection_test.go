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
			raw = renderChainCached(t, stacks[r.Stack])
			break
		}
	}
	if front == nil {
		t.Fatal("в дереве нет цепочки с внешним входом консоли — инъекциям некуда попасть")
	}
	host := strings.TrimPrefix(front.Origin, "https://")
	cases := []struct {
		name, from, to string
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
				text = strings.Replace(text, c.from, c.to, 1)
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
