// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_serving_neighbours_injection_test.go — суд соседей раздачи краснеет
// на соседе без роли и молчит на законном близнеце (задача #2874).
//
// Вход — НАСТОЯЩИЕ объявления из дерева: шаблон раздачи и под, который её
// поднимает. Рядом с внесённым блоком в живом файле стоят полосы модулей, края
// и церемоний, условия шаблона и комментарии, и суд обязан найти свой предмет
// среди них. Каждый вариант меняет РОВНО ОДИН факт против законного близнеца.
//
// Имени снятой службы во входах нет намеренно: суд читает РОЛЬ соседа, а не его
// имя, поэтому прежняя полоса к службе выдачи токена записана своей формой с
// подставным сегментом `idp` — ветка суда та же, что у настоящего имени.
package deploy_test

import (
	"strings"
	"testing"
)

// neighbourVariant — вход с одним изменённым фактом и ожидаемый исход.
type neighbourVariant struct {
	name     string
	block    string          // блок раздачи, вносимый в серверный блок консоли
	env      []string        // ручки адресов, вносимые в под раздачи
	dropEnv  string          // ручка, снимаемая из пода раздачи
	modules  map[string]bool // модули сверх выведенных из дерева
	findings int
	mentions []string
}

// servingAnchor — место вставки: первый блок проверки живости, он стоит в
// серверном блоке консоли.
const servingAnchor = "        location = /healthz {"

// deploymentAnchor — место вставки ручки: перед первой ручкой адреса соседа.
const deploymentAnchor = "            - name: KACHO_UI_"

func TestConsoleServingNeighboursGate_ProvenByInjection(t *testing.T) {
	root := repoRootFromTest(t)
	serving := readTreeFile(t, root, servingTemplateRel)
	deployment := readTreeFile(t, root, deploymentHostRel)
	modules := consoleModules(t, root)

	if strings.Count(serving, servingAnchor) < 1 {
		t.Fatalf("в %s нет места вставки %q — форма шаблона изменилась, и инъекция стала бы вакуумной",
			servingTemplateRel, servingAnchor)
	}
	if strings.Count(deployment, deploymentAnchor) < 1 {
		t.Fatalf("в %s нет места вставки %q — форма пода изменилась, и инъекция стала бы вакуумной",
			deploymentHostRel, deploymentAnchor)
	}

	envLine := func(name string) string {
		return "            - name: KACHO_UI_" + name + "_UPSTREAM\n              value: \"\"\n"
	}
	lines := func(xs ...string) string { return strings.Join(xs, "\n") + "\n\n" }

	variants := []neighbourVariant{
		{name: "настоящее дерево — законный вход", findings: 0},
		{
			name: "полоса прежней формы к службе выдачи токена (сегмент подставной) — сосед без роли",
			block: lines(
				"        location ^~ /.ory/idp/public/ {",
				`            set $idp_public "${KACHO_UI_IDP_PUBLIC_UPSTREAM}";`,
				"            proxy_pass http://$idp_public;",
				"        }"),
			env:      []string{"IDP_PUBLIC"},
			findings: 1, mentions: []string{"IDP_PUBLIC", "нет роли"},
		},
		{
			name: "близнец: та же полоса закомментирована — директивы нет",
			block: lines(
				"        location ^~ /.ory/idp/public/ {",
				`            # set $idp_public "${KACHO_UI_IDP_PUBLIC_UPSTREAM}";`,
				"            # proxy_pass http://$idp_public;",
				"            return 404;",
				"        }"),
			findings: 0,
		},
		{
			name: "обращение по буквальному адресу мимо подстановки",
			block: lines(
				"        location ^~ /.ory/idp/public/ {",
				"            proxy_pass http://idp-public.kacho.svc.cluster.local:4444;",
				"        }"),
			findings: 1, mentions: []string{"мимо подстановки"},
		},
		{
			name: "сосед за другой директивой передачи — тот же сосед",
			block: lines(
				"        location ^~ /idp.v1.Issuer/ {",
				"            grpc_pass grpc://idp-public.kacho.svc.cluster.local:9090;",
				"        }"),
			findings: 1, mentions: []string{"grpc_pass", "мимо подстановки"},
		},
		{
			name: "переменная связана с литералом, а не с адресом соседа",
			block: lines(
				"        location ^~ /.ory/idp/public/ {",
				`            set $idp_public "idp-public.kacho.svc.cluster.local:4444";`,
				"            proxy_pass http://$idp_public;",
				"        }"),
			findings: 1, mentions: []string{"не связана"},
		},
		{
			name: "подстановка адреса соседа вне `set`",
			block: lines(
				"        location ^~ /probe/ {",
				`            proxy_set_header X-Upstream "${KACHO_UI_API_GATEWAY_UPSTREAM}";`,
				"            return 204;",
				"        }"),
			findings: 1, mentions: []string{"вне `set"},
		},
		{
			name: "блок формы модуля для каталога, которого в консоли нет",
			block: lines(
				"        location ^~ /ghost-remote/ {",
				`            set $ghost_upstream "${KACHO_UI_GHOST_UPSTREAM}";`,
				"            proxy_pass http://$ghost_upstream;",
				"        }"),
			env:      []string{"GHOST"},
			findings: 1, mentions: []string{"GHOST", "нет роли"},
		},
		{
			name: "близнец: тот же блок, каталог модуля в консоли есть",
			block: lines(
				"        location ^~ /ghost-remote/ {",
				`            set $ghost_upstream "${KACHO_UI_GHOST_UPSTREAM}";`,
				"            proxy_pass http://$ghost_upstream;",
				"        }"),
			env:      []string{"GHOST"},
			modules:  map[string]bool{"ghost": true},
			findings: 0,
		},
		{
			name: "блок модуля отдаёт запросы адресу ЧУЖОГО модуля",
			block: lines(
				"        location ^~ /ghost-remote/ {",
				`            set $ghost_upstream "${KACHO_UI_VPC_UPSTREAM}";`,
				"            proxy_pass http://$ghost_upstream;",
				"        }"),
			modules:  map[string]bool{"ghost": true},
			findings: 1, mentions: []string{"VPC", "нет роли"},
		},
		{
			name: "близнец: полоса церемоний к подставному экрану — роль выводится из формы",
			block: lines(
				"        location ~ ^/(signin)(/|$) {",
				`            set $screen "${KACHO_UI_SIGNIN_SCREEN_UPSTREAM}";`,
				"            proxy_pass http://$screen;",
				"        }"),
			env:      []string{"SIGNIN_SCREEN"},
			findings: 0,
		},
		{
			name: "тот же экран входа отдан блоком вне полосы церемоний",
			block: lines(
				"        location ~ ^/(signin)(/|$) {",
				`            set $screen "${KACHO_UI_SIGNIN_SCREEN_UPSTREAM}";`,
				"            proxy_pass http://$screen;",
				"        }",
				"",
				"        location ^~ /elsewhere/ {",
				`            set $screen "${KACHO_UI_SIGNIN_SCREEN_UPSTREAM}";`,
				"            proxy_pass http://$screen;",
				"        }"),
			env:      []string{"SIGNIN_SCREEN"},
			findings: 1, mentions: []string{"/elsewhere/", "нет роли"},
		},
		{
			name: "запасной путь статики к соседу, которого не отдаёт ни одна полоса",
			block: lines(
				"        location ^~ /ghost-assets/ {",
				"            try_files $uri @ghost_fallback;",
				"        }",
				"",
				"        location @ghost_fallback {",
				`            set $ghost_screen "${KACHO_UI_GHOST_SCREEN_UPSTREAM}";`,
				"            proxy_pass http://$ghost_screen;",
				"        }"),
			env:      []string{"GHOST_SCREEN"},
			findings: 1, mentions: []string{"запасной путь", "GHOST_SCREEN"},
		},
		{
			name: "близнец: тот же запасной путь к экрану, который отдаёт полоса",
			block: lines(
				"        location ~ ^/(signin)(/|$) {",
				`            set $screen "${KACHO_UI_GHOST_SCREEN_UPSTREAM}";`,
				"            proxy_pass http://$screen;",
				"        }",
				"",
				"        location ^~ /ghost-assets/ {",
				"            try_files $uri @ghost_fallback;",
				"        }",
				"",
				"        location @ghost_fallback {",
				`            set $ghost_screen "${KACHO_UI_GHOST_SCREEN_UPSTREAM}";`,
				"            proxy_pass http://$ghost_screen;",
				"        }"),
			env:      []string{"GHOST_SCREEN"},
			findings: 0,
		},
		{
			name:     "ручка адреса соседа в поде без читателя в раздаче",
			env:      []string{"IDP_PUBLIC"},
			findings: 1, mentions: []string{"KACHO_UI_IDP_PUBLIC_UPSTREAM", "без читателя"},
		},
		{
			name:     "раздача читает адрес края, а под ручки не объявляет",
			dropEnv:  "API_GATEWAY",
			findings: 1, mentions: []string{"KACHO_UI_API_GATEWAY_UPSTREAM", "не объявляет"},
		},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			srv := serving
			if v.block != "" {
				srv = strings.Replace(serving, servingAnchor, v.block+servingAnchor, 1)
			}
			dep := deployment
			for _, e := range v.env {
				dep = strings.Replace(dep, deploymentAnchor, envLine(e)+deploymentAnchor, 1)
			}
			if v.dropEnv != "" {
				line := "            - name: KACHO_UI_" + v.dropEnv + "_UPSTREAM\n"
				i := strings.Index(dep, line)
				if i < 0 {
					t.Fatalf("в %s нет ручки %q — снимать нечего, вариант стал бы вакуумным", deploymentHostRel, line)
				}
				rest := dep[i+len(line):]
				j := strings.Index(rest, "\n")
				if j < 0 || !strings.Contains(rest[:j], "value:") {
					t.Fatalf("за ручкой %q не стоит строка значения — форма пода изменилась", line)
				}
				dep = dep[:i] + rest[j+1:]
			}
			mods := map[string]bool{}
			for m := range modules {
				mods[m] = true
			}
			for m := range v.modules {
				mods[m] = true
			}

			c := judgeConsoleNeighbours(t, srv, dep, mods)
			if len(c.findings) != v.findings {
				t.Fatalf("находок %d, ожидалось %d:\n  %s", len(c.findings), v.findings, strings.Join(c.findings, "\n  "))
			}
			joined := strings.Join(c.findings, "\n")
			for _, want := range v.mentions {
				if !strings.Contains(joined, want) {
					t.Errorf("находка не называет %q:\n  %s", want, joined)
				}
			}
		})
	}
	t.Logf("осмотрено: вариантов %d на настоящих %s и %s", len(variants), servingTemplateRel, deploymentHostRel)
}
