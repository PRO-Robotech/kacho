// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// console_ingress_origin_is_served_over_tls_render_test.go — КОНСОЛЬ, КОТОРУЮ
// РАЗДАЁТ КОНТРОЛЛЕР ВХОДА НА УЗЛЕ, ОТВЕЧАЕТ ПО TLS НА ОБЪЯВЛЕННОМ ПРОИСХОЖДЕНИИ
// (kacho#3025).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Сверка происхождения (console_origin_is_secure_test.go) судит ОБЪЯВЛЕНИЕ:
// `global.kacho.identity.appBaseURL` по https. Объявление без раздачи — ложь о
// стенде: браузер придёт на https и не найдёт ни слушателя, ни сертификата.
// Здесь судится, что объявленное происхождение РАЗДАЁТСЯ: для каждой цепочки,
// у которой консоль публикует Ingress, а контроллер входа стоит на порту узла
// (hostPort — так устроен стенд kind), рендер обязан нести:
//
//   (1) происхождение консоли по https — иначе Secure-печенье формы браузер не
//       хранит и регистрация получает `403 FORM_TOKEN_REJECTED`;
//   (2) у КАЖДОГО Ingress хоста консоли — запись tls с этим хостом;
//   (3) секрет этой записи производит Certificate того же рендера, и среди его
//       dnsNames есть хост консоли: секрет, которого никто не заводит, —
//       контроллер отдаёт свой поддельный лист;
//   (4) порт происхождения — тот, на который кластер kind отображает порт TLS
//       контроллера на узле (deploy/kind/kind-config.yaml): иначе объявлен адрес,
//       на котором никто не слушает.
//
// Цепочки, у которых вход консоли — собственная раздача (`uif.publicFront`),
// судит console_public_front_render_test.go; здесь они считаются и не судятся.
//
// Способность упасть и смолчать — TestConsoleIngressTLSJudgement_CanFailAndStaysSilent.
package deploy_test

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kindConfigPath — объявление отображения портов узла стенда kind.
const kindConfigPath = "kind/kind-config.yaml"

// ingressTLSRender — то, что судится у одной цепочки.
type ingressTLSRender struct {
	Stack  string
	Origin string
	Docs   []renderedDoc
}

type ingressTLSCensus struct {
	Chains, Judged, ByOwnFront, WithoutConsole int
}

func (c ingressTLSCensus) String() string {
	return fmt.Sprintf("цепочек %d · судится (вход контроллером на узле) %d · своя раздача консоли %d · без консоли %d",
		c.Chains, c.Judged, c.ByOwnFront, c.WithoutConsole)
}

// kindHostPortFor — на какой порт хоста кластер kind отображает порт узла.
func kindHostPortFor(t *testing.T, path string, containerPort int) (int, bool) {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- объявление кластера этого дерева либо копия инъекции
	if err != nil {
		t.Fatalf("объявление кластера kind %s не читается (%v) — судить порт нечем", path, err)
	}
	var cfg struct {
		Nodes []struct {
			ExtraPortMappings []struct {
				ContainerPort int `yaml:"containerPort"`
				HostPort      int `yaml:"hostPort"`
			} `yaml:"extraPortMappings"`
		} `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("объявление кластера kind %s не разобрано: %v", path, err)
	}
	for _, n := range cfg.Nodes {
		for _, m := range n.ExtraPortMappings {
			if m.ContainerPort == containerPort {
				return m.HostPort, true
			}
		}
	}
	return 0, false
}

func docsOfKind(docs []renderedDoc, kind string) []renderedDoc {
	var out []renderedDoc
	for _, d := range docs {
		if str(d, "kind") == kind {
			out = append(out, d)
		}
	}
	return out
}

// controllerTLSHostPort — порт узла, на котором контроллер входа принимает TLS
// (hostPort контейнера с портом `https`), если контроллер стоит на порту узла.
func controllerTLSHostPort(docs []renderedDoc) (nodePort int, onNode bool) {
	for _, d := range docsOfKind(docs, "Deployment") {
		if !strings.Contains(str(submap(d, "metadata"), "name"), "ingress-nginx-controller") {
			continue
		}
		spec := submap(submap(submap(d, "spec"), "template"), "spec")
		cs, _ := spec["containers"].([]any)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			ps, _ := cm["ports"].([]any)
			for _, p := range ps {
				pm, _ := p.(map[string]any)
				if str(pm, "name") != "https" {
					continue
				}
				if hp, ok := pm["hostPort"].(int); ok {
					return hp, true
				}
			}
		}
	}
	return 0, false
}

// judgeConsoleIngressTLS — находки по цепочкам. kindCfg — путь объявления kind.
func judgeConsoleIngressTLS(t *testing.T, renders []ingressTLSRender, kindCfg string) ([]string, ingressTLSCensus) {
	t.Helper()
	var findings []string
	census := ingressTLSCensus{Chains: len(renders)}
	for _, r := range renders {
		u, err := url.Parse(r.Origin)
		if err != nil || u.Hostname() == "" {
			findings = append(findings, fmt.Sprintf("цепочка %s: происхождение консоли %q не разбирается", r.Stack, r.Origin))
			continue
		}
		host := u.Hostname()

		var consoleIngresses []renderedDoc
		for _, ing := range docsOfKind(r.Docs, "Ingress") {
			rules, _ := submap(ing, "spec")["rules"].([]any)
			for _, rule := range rules {
				if rm, _ := rule.(map[string]any); str(rm, "host") == host {
					consoleIngresses = append(consoleIngresses, ing)
					break
				}
			}
		}
		nodePort, onNode := controllerTLSHostPort(r.Docs)
		switch {
		case len(consoleIngresses) == 0:
			// Хост происхождения не публикует ни один Ingress: вход консоли —
			// собственная раздача (её судит соседняя проба) либо консоли нет.
			if len(docsOfKind(r.Docs, "Ingress")) > 0 || hasPublicFront(r.Docs) {
				census.ByOwnFront++
			} else {
				census.WithoutConsole++
			}
			continue
		case !onNode:
			census.ByOwnFront++
			continue
		}
		census.Judged++

		if u.Scheme != "https" {
			findings = append(findings, fmt.Sprintf("цепочка %s: консоль раздаёт контроллер входа, а происхождение "+
				"%q — не https. Secure-печенье формы с него браузер не хранит (kacho#3025)", r.Stack, r.Origin))
			continue
		}

		certs := map[string][]string{} // секрет → dnsNames
		for _, c := range docsOfKind(r.Docs, "Certificate") {
			spec := submap(c, "spec")
			var names []string
			dn, _ := spec["dnsNames"].([]any)
			for _, n := range dn {
				if s, ok := n.(string); ok {
					names = append(names, s)
				}
			}
			certs[str(spec, "secretName")] = names
		}
		for _, ing := range consoleIngresses {
			name := str(submap(ing, "metadata"), "name")
			var secret string
			tls, _ := submap(ing, "spec")["tls"].([]any)
			for _, e := range tls {
				em, _ := e.(map[string]any)
				hs, _ := em["hosts"].([]any)
				for _, h := range hs {
					if h == host {
						secret = str(em, "secretName")
					}
				}
			}
			if secret == "" {
				findings = append(findings, fmt.Sprintf("цепочка %s: Ingress %s раздаёт хост консоли %s без записи tls — "+
					"на объявленном https контроллер отдаст свой поддельный лист либо откажет", r.Stack, name, host))
				continue
			}
			names, ok := certs[secret]
			if !ok {
				findings = append(findings, fmt.Sprintf("цепочка %s: секрет %s записи tls Ingress %s не производит ни один "+
					"Certificate рендера — листа хоста консоли никто не заводит", r.Stack, secret, name))
				continue
			}
			covered := false
			for _, n := range names {
				if n == host {
					covered = true
				}
			}
			if !covered {
				findings = append(findings, fmt.Sprintf("цепочка %s: Certificate секрета %s не называет хост консоли %s "+
					"(dnsNames %v)", r.Stack, secret, host, names))
			}
		}

		port := 443
		if p := u.Port(); p != "" {
			port, _ = strconv.Atoi(p)
		}
		hostPort, mapped := kindHostPortFor(t, kindCfg, nodePort)
		switch {
		case !mapped:
			findings = append(findings, fmt.Sprintf("цепочка %s: контроллер входа принимает TLS на порту узла %d, а "+
				"%s этот порт на хост не отображает — происхождение %q недостижимо", r.Stack, nodePort, kindCfg, r.Origin))
		case hostPort != port:
			findings = append(findings, fmt.Sprintf("цепочка %s: происхождение %q называет порт %d, а %s отображает порт "+
				"TLS узла %d на %s — объявлен адрес, на котором никто не слушает",
				r.Stack, r.Origin, port, kindCfg, nodePort, net.JoinHostPort("хост", strconv.Itoa(hostPort))))
		}
	}
	return findings, census
}

func hasPublicFront(docs []renderedDoc) bool {
	for _, s := range docsOfKind(docs, "Service") {
		if strings.HasSuffix(str(submap(s, "metadata"), "name"), "-public") {
			return true
		}
	}
	return false
}

// readConsoleIngressRenders — рендер каждой цепочки и её происхождение консоли.
func readConsoleIngressRenders(t *testing.T, sets map[string][]string) []ingressTLSRender {
	t.Helper()
	facts := readConsoleOriginFacts(t, nil)
	stacks := deployStacks(t)
	var out []ingressTLSRender
	for _, f := range facts {
		rendered, err := renderStack(t, stacks[f.Stack], sets[f.Stack]...)
		if err != nil {
			t.Fatalf("цепочка %s не рендерится: %v\n%s", f.Stack, err, rendered)
		}
		out = append(out, ingressTLSRender{Stack: f.Stack, Origin: f.Origin, Docs: decodeRender(t, rendered)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stack < out[j].Stack })
	return out
}

func TestConsoleServedByTheNodeIngressAnswersTLSOnTheDeclaredOrigin(t *testing.T) {
	renders := readConsoleIngressRenders(t, nil)
	findings, census := judgeConsoleIngressTLS(t, renders, kindConfigPath)
	t.Logf("перепись: %s", census)
	if census.Judged == 0 {
		t.Fatal("ни одна цепочка не раздаёт консоль контроллером входа на узле — предмет пробы исчез, " +
			"а не стал чистым")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

func TestConsoleIngressTLSJudgement_CanFailAndStaysSilent(t *testing.T) {
	stacks := deployStacks(t)
	if _, ok := stacks["dev"]; !ok {
		t.Fatal("цепочки dev в таблице нет — инъекции не на чем стоять")
	}
	facts := readConsoleOriginFacts(t, nil)
	var origin string
	for _, f := range facts {
		if f.Stack == "dev" {
			origin = f.Origin
		}
	}
	render := func(sets ...string) ingressTLSRender {
		out, err := renderStack(t, stacks["dev"], sets...)
		if err != nil {
			t.Fatalf("рендер dev с %v: %v\n%s", sets, err, out)
		}
		return ingressTLSRender{Stack: "dev", Origin: origin, Docs: decodeRender(t, out)}
	}

	// Законный близнец — дерево как есть.
	if f, c := judgeConsoleIngressTLS(t, []ingressTLSRender{render()}, kindConfigPath); len(f) != 0 || c.Judged != 1 {
		t.Fatalf("близнец: дерево без правки не молчит либо не судится (%s): %v", c, f)
	}

	// Kind отображает порт TLS узла на другой порт хоста.
	dir := t.TempDir()
	raw, err := os.ReadFile(kindConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(origin)
	shifted := strings.Replace(string(raw), "hostPort: "+u.Port(), "hostPort: 1", 1)
	if shifted == string(raw) {
		t.Fatalf("инъекция порта не внесена: в %s нет «hostPort: %s»", kindConfigPath, u.Port())
	}
	moved := filepath.Join(dir, "kind-config.yaml")
	if err := os.WriteFile(moved, []byte(shifted), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		r    ingressTLSRender
		kind string
		want string
	}{
		{"происхождение по http", func() ingressTLSRender { r := render(); r.Origin = "http://" + u.Host; return r }(), kindConfigPath, "не https"},
		// Запись снимается вместе с листом: лист без записи отвергает сам рендер
		// (templates/certificate-ingress.yaml), и до суда такой вход не дошёл бы.
		{"запись tls снята", render("uif.ingress.tls=null", "uif.ingress.certificate.create=false"), kindConfigPath, "без записи tls"},
		{"сертификат не заводится", render("uif.ingress.certificate.create=false"), kindConfigPath, "не производит ни один"},
		{"порт не отображён туда", render(), moved, "никто не слушает"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, _ := judgeConsoleIngressTLS(t, []ingressTLSRender{c.r}, c.kind)
			if !strings.Contains(strings.Join(f, "\n"), c.want) {
				t.Fatalf("инъекция «%s» не найдена (ждали «%s»): %v", c.name, c.want, f)
			}
		})
	}
}
