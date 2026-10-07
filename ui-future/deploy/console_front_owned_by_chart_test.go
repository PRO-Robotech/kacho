// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_front_owned_by_chart_test.go — ФРОНТ КОНСОЛИ СТЕНДА ПРИНАДЛЕЖИТ
// РЕЛИЗУ: КАЖДЫЙ ОБЪЕКТ ПУТИ «КЛИЕНТ → БАЛАНСИРОВЩИК → РАЗДАЧА» ЕСТЬ В РЕНДЕРЕ
// (kacho#3024).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРИЗНАК
//
// На управляемом стенде вход консоли под TLS держали объекты, заведённые рукой
// мимо чарта: отдельный прокси, его настройка, Service балансировщика,
// поставщик сертификата, Service решателя ACME. Релиз о них не знал: снятие
// релиза их не снимало, а прокси держал адрес снятого Service раздачи до
// ручного перезапуска. Объект, которого нет в рендере, ни переустановка, ни
// откат не приводят к дереву.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Для каждой цепочки deploy/stacks.txt, которая включает вход консоли
// (`uif.publicFront`), рендер чарта консоли обязан нести весь путь:
//
//   - Service типа LoadBalancer: 443 → TLS раздачи, 80 → переадресация, и
//     никакого порта к внутреннему http раздачи;
//   - Certificate в секрет, который монтирует под раздачи, с поставщиком,
//     который РАЗРЕШАЕТСЯ В РЕНДЕРЕ (Issuer того же релиза), с хостом
//     происхождения консоли среди имён и с временным листом на первый выпуск —
//     иначе под раздачи не стартует без секрета, а решатель за ним недостижим;
//   - Issuer ACME с единственным решателем http01, чьи поды помечены;
//   - Service решателя типа ClusterIP, выбирающий ровно эти поды;
//   - путь решателя на порту ПЕРЕАДРЕСАЦИИ ведёт на Service решателя, а на
//     TLS-порту консоли — нет (там его обслуживает оболочка).
//
// Цепочка стенда, ради которого вход заведён (`frontStandStack`), обязана вход
// включать: иначе перепись пуста, и «на всех цепочках со входом всё верно»
// истинно ни о чём.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Что живые объекты стенда приняты релизом во владение и ручные сняты — шаг
// выкатки (kacho#3067). Что сертификат выпускается — рантайм поставщика.
// Слой площадки (настоящее происхождение) в рендер не входит: судится профиль
// дерева с его заглушкой.
package deploy_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// frontStandStack — цепочка стенда, ради которого вход заведён.
const frontStandStack = "a8f60d"

// acmeSolverPodLabel — метка, которой cert-manager помечает каждый под решателя
// http01; Service решателя обязан выбирать по ней И по метке своего Issuer.
const acmeSolverPodLabel = "acme.cert-manager.io/http01-solver"

// acmeSolverPort — порт пода решателя http01 (задан cert-manager).
const acmeSolverPort = 8089

// acmeChallengeProbe — адрес вызова решателя, как его спрашивает ACME.
const acmeChallengeProbe = "/.well-known/acme-challenge/probe-token"

const tempCertAnnotation = "cert-manager.io/issue-temporary-certificate"

// stackChains — цепочки профилей зонта из deploy/stacks.txt.
func stackChains(t *testing.T) map[string][]string {
	t.Helper()
	path := filepath.Join(repoRootFromTest(t), "deploy", "stacks.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("таблица стеков %s не читается: %v", path, err)
	}
	defer f.Close()
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, files, ok := strings.Cut(line, ":")
		if !ok || name == "" || files == "" {
			t.Fatalf("%s: строка %q не в форме `имя:файл[,файл]`", path, line)
		}
		out[name] = strings.Split(files, ",")
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s: ни одной цепочки — судить нечего", path)
	}
	return out
}

func readValuesFile(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(body, &m); err != nil {
		t.Fatalf("%s не разбирается: %v", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func deepMerge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = deepMerge(cur, sub)
				continue
			}
			dst[k] = deepMerge(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

// consoleValuesOfChain — значения, которые чарт консоли получает в цепочке:
// узел `uif` зонта поверх умолчаний зонта плюс `global`, профили слева направо.
func consoleValuesOfChain(t *testing.T, chain []string) map[string]any {
	t.Helper()
	dir := filepath.Join(repoRootFromTest(t), "deploy", "helm", "umbrella")
	merged := readValuesFile(t, filepath.Join(dir, "values.yaml"))
	for _, f := range chain {
		merged = deepMerge(merged, readValuesFile(t, filepath.Join(dir, f)))
	}
	uif, _ := merged["uif"].(map[string]any)
	vals := deepMerge(map[string]any{}, uif)
	if g, ok := merged["global"].(map[string]any); ok {
		vals["global"] = deepMerge(map[string]any{}, g)
	}
	return vals
}

// renderConsoleChart — `helm template` чарта консоли с этими значениями.
// helm вне PATH при CI — провал, а не пропуск: гейт, молча ставший инертным на
// джобе, гейтящей слияние, гейтом не является.
func renderConsoleChart(t *testing.T, vals map[string]any, sets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	body, err := yaml.Marshal(vals)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"template", "kacho-umbrella", filepath.Join(repoRootFromTest(t), "ui-future", "deploy"),
		"-n", "kacho", "-f", file}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	return string(out), err
}

func decodeDocs(t *testing.T, rendered string) []map[string]any {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewBufferString(rendered))
	var docs []map[string]any
	for {
		var d map[string]any
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("рендер не разбирается как YAML: %v", err)
		}
		if d != nil {
			docs = append(docs, d)
		}
	}
	return docs
}

func dig(m any, path ...string) any {
	cur := m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// frontCensus — объём осмотренного одной цепочки.
type frontCensus struct {
	Objects                                                 int
	LoadBalancers, Certificates, Issuers, SolverSvcs, Hosts int
}

func (c frontCensus) String() string {
	return fmt.Sprintf("объектов %d · LoadBalancer %d · Certificate %d · Issuer %d · Service решателя %d · раздач %d",
		c.Objects, c.LoadBalancers, c.Certificates, c.Issuers, c.SolverSvcs, c.Hosts)
}

// judgeFrontOwnership — НАХОДКИ о пути входа в рендере одной цепочки. Чистая
// функция: инъекция подаёт ей испорченный рендер.
func judgeFrontOwnership(t *testing.T, docs []map[string]any, originHost string) ([]string, frontCensus) {
	t.Helper()
	var findings []string
	c := frontCensus{Objects: len(docs)}
	say := func(f string, a ...any) { findings = append(findings, fmt.Sprintf(f, a...)) }

	issuers := map[string]map[string]any{}
	var lbs, certs, solverSvcs, hosts, cms []map[string]any
	for _, d := range docs {
		kind := str(d["kind"])
		switch {
		case kind == "Service" && str(dig(d, "spec", "type")) == "LoadBalancer":
			lbs = append(lbs, d)
		case kind == "Service" && dig(d, "spec", "selector", acmeSolverPodLabel) != nil:
			solverSvcs = append(solverSvcs, d)
		case kind == "Certificate":
			certs = append(certs, d)
		case kind == "Issuer" || kind == "ClusterIssuer":
			issuers[kind+"/"+str(dig(d, "metadata", "name"))] = d
		case kind == "Deployment" && hasVolume(d, "console-tls"):
			hosts = append(hosts, d)
		case kind == "ConfigMap" && dig(d, "data", "default.conf.template") != nil:
			cms = append(cms, d)
		}
	}
	c.LoadBalancers, c.Certificates, c.Issuers, c.SolverSvcs, c.Hosts = len(lbs), len(certs), len(issuers), len(solverSvcs), len(hosts)

	// Раздача и её секрет.
	if len(hosts) != 1 {
		say("раздач, монтирующих лист входа `console-tls`, %d, ждали 1", len(hosts))
		return findings, c
	}
	secret := ""
	for _, v := range list(dig(hosts[0], "spec", "template", "spec", "volumes")) {
		if str(dig(v, "name")) == "console-tls" {
			secret = str(dig(v, "secret", "secretName"))
		}
	}

	// Балансировщик.
	if len(lbs) != 1 {
		say("Service типа LoadBalancer в рендере %d, ждали 1 — вход вне релиза не снимается и не переустанавливается", len(lbs))
	} else {
		ports := map[string]string{}
		for _, p := range list(dig(lbs[0], "spec", "ports")) {
			ports[str(dig(p, "port"))] = str(dig(p, "targetPort"))
		}
		if ports["443"] != "https" || ports["80"] != "redirect" || len(ports) != 2 {
			say("Service %s публикует порты %v — ждали ровно 443→https и 80→redirect", str(dig(lbs[0], "metadata", "name")), ports)
		}
	}

	// Сертификат и поставщик.
	var cert map[string]any
	for _, d := range certs {
		if str(dig(d, "spec", "secretName")) == secret {
			cert = d
		}
	}
	var solverLabels map[string]any
	if cert == nil {
		say("Certificate в секрет %q, который монтирует раздача, в рендере нет", secret)
	} else {
		ref := str(dig(cert, "spec", "issuerRef", "kind")) + "/" + str(dig(cert, "spec", "issuerRef", "name"))
		iss, ok := issuers[ref]
		if !ok {
			say("issuerRef сертификата %s не разрешается в рендере — поставщик вне релиза", ref)
		} else {
			if !strings.HasPrefix(str(dig(iss, "spec", "acme", "server")), "https://") {
				say("поставщик %s не объявляет каталог ACME по https", ref)
			}
			solvers := list(dig(iss, "spec", "acme", "solvers"))
			if len(solvers) != 1 || dig(solvers[0], "http01") == nil {
				say("поставщик %s несёт решателей %d, ждали ровно один http01", ref, len(solvers))
			} else {
				solverLabels, _ = dig(solvers[0], "http01", "ingress", "podTemplate", "metadata", "labels").(map[string]any)
				if len(solverLabels) == 0 {
					say("решатель поставщика %s не помечает свои поды — Service решателя выбрать их не может", ref)
				}
			}
		}
		if str(dig(cert, "metadata", "annotations", tempCertAnnotation)) != "true" {
			say("Certificate %q без временного листа (%s): на чистой установке секрета нет, раздача не стартует, "+
				"и решатель за ней недостижим", secret, tempCertAnnotation)
		}
		names := append(list(dig(cert, "spec", "dnsNames")), list(dig(cert, "spec", "ipAddresses"))...)
		covered := false
		for _, n := range names {
			covered = covered || str(n) == originHost
		}
		if !covered {
			say("хост происхождения консоли %q не назван в сертификате %q (%v)", originHost, secret, names)
		}
	}

	// Service решателя.
	solverName := ""
	switch {
	case len(solverSvcs) != 1:
		say("Service решателя ACME в рендере %d, ждали 1", len(solverSvcs))
	default:
		s := solverSvcs[0]
		solverName = str(dig(s, "metadata", "name"))
		if tp := str(dig(s, "spec", "type")); tp != "" && tp != "ClusterIP" {
			say("Service решателя %s типа %s — решатель наружу не публикуется", solverName, tp)
		}
		if str(dig(s, "spec", "selector", acmeSolverPodLabel)) != "true" {
			say("Service решателя %s не выбирает по метке %s", solverName, acmeSolverPodLabel)
		}
		for k, v := range solverLabels {
			if str(dig(s, "spec", "selector", k)) != str(v) {
				say("Service решателя %s не выбирает по метке поставщика %s=%v — выберет поды чужого решателя", solverName, k, v)
			}
		}
		okPort := false
		for _, p := range list(dig(s, "spec", "ports")) {
			okPort = okPort || str(dig(p, "targetPort")) == fmt.Sprint(acmeSolverPort)
		}
		if !okPort {
			say("Service решателя %s не ведёт на порт пода решателя %d", solverName, acmeSolverPort)
		}
	}

	// Путь решателя в раздаче: настройка, которую монтирует под входа.
	var hostCM map[string]any
	for _, v := range list(dig(hosts[0], "spec", "template", "spec", "volumes")) {
		ref := str(dig(v, "configMap", "name"))
		for _, cm := range cms {
			if ref != "" && str(dig(cm, "metadata", "name")) == ref {
				hostCM = cm
			}
		}
	}
	if hostCM == nil {
		say("настройки раздачи, которую монтирует под входа, в рендере нет (настроек раздач %d)", len(cms))
		return findings, c
	}
	servers := parseServingTemplate(t, str(dig(hostCM, "data", "default.conf.template")))
	var redirect, console *nginxServer
	for i := range servers {
		for _, l := range servers[i].locs {
			if edgeUpstreamRe.MatchString(l.body) {
				console = &servers[i]
			}
			if l.spec == "/" && strings.Contains(l.body, "return 308") {
				redirect = &servers[i]
			}
		}
	}
	// Адрес решателя раздача получает ручкой пода (форма соседа —
	// console_serving_neighbours_test.go): ручка обязана называть Service
	// решателя этого релиза.
	solverAddr := ""
	for _, ctr := range list(dig(hosts[0], "spec", "template", "spec", "containers")) {
		for _, e := range list(dig(ctr, "env")) {
			if str(dig(e, "name")) == "KACHO_UI_"+acmeSolverUpstream+"_UPSTREAM" {
				solverAddr = str(dig(e, "value"))
			}
		}
	}
	if !strings.HasPrefix(solverAddr, solverName+".") || !strings.HasSuffix(solverAddr, fmt.Sprintf(":%d", acmeSolverPort)) {
		say("ручка адреса решателя в поде раздачи %q не называет Service решателя %q на порту %d", solverAddr, solverName, acmeSolverPort)
	}
	reachesSolver := func(srv *nginxServer) (bool, string) {
		idx, _ := selectLocation(acmeChallengeProbe, srv.locs)
		if idx < 0 {
			return false, "не выбран ни один блок"
		}
		l := srv.locs[idx]
		return strings.Contains(l.body, "${KACHO_UI_"+acmeSolverUpstream+"_UPSTREAM}"), l.name()
	}
	switch {
	case redirect == nil:
		say("сервер переадресации (порт 80) в настройке раздачи не найден")
	default:
		if ok, where := reachesSolver(redirect); !ok {
			say("вызов решателя на порту переадресации достаётся блоку %s, а не Service решателя %q — "+
				"выпуск и продление невозможны", where, solverName)
		}
	}
	if console != nil {
		if ok, where := reachesSolver(console); ok {
			say("путь решателя на порту консоли ведёт на решатель (%s) — он обслуживается только на порту переадресации", where)
		}
	} else {
		say("серверный блок консоли в настройке раздачи не найден")
	}
	return findings, c
}

func hasVolume(d map[string]any, name string) bool {
	for _, v := range list(dig(d, "spec", "template", "spec", "volumes")) {
		if str(dig(v, "name")) == name {
			return true
		}
	}
	return false
}

// originHostOf — хост происхождения консоли цепочки.
func originHostOf(vals map[string]any) string {
	o := str(dig(vals, "global", "kacho", "identity", "appBaseURL"))
	o = strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
	o, _, _ = strings.Cut(o, "/")
	return strings.TrimSuffix(o, ":443")
}

// TestConsoleFrontIsOwnedByTheReleaseOnEveryChainThatRaisesIt — суд по дереву.
func TestConsoleFrontIsOwnedByTheReleaseOnEveryChainThatRaisesIt(t *testing.T) {
	chains := stackChains(t)
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	judged := 0
	for _, n := range names {
		vals := consoleValuesOfChain(t, chains[n])
		if on, _ := dig(vals, "enabled").(bool); !on {
			continue
		}
		if on, _ := dig(vals, "publicFront", "enabled").(bool); !on {
			if n == frontStandStack {
				t.Errorf("цепочка %s не включает вход консоли (`uif.publicFront.enabled`) — "+
					"пользовательский край стенда без TLS, и Secure-печенье формы браузер не хранит", n)
			}
			continue
		}
		out, err := renderConsoleChart(t, vals)
		if err != nil {
			t.Errorf("цепочка %s: рендер чарта консоли отказал: %v\n%s", n, err, out)
			continue
		}
		judged++
		findings, c := judgeFrontOwnership(t, decodeDocs(t, out), originHostOf(vals))
		t.Logf("цепочка %s: %s", n, c)
		for _, f := range findings {
			t.Errorf("цепочка %s: %s", n, f)
		}
	}
	t.Logf("перепись: цепочек %d · со входом консоли судимо %d", len(names), judged)
	if judged == 0 {
		t.Errorf("ни одна цепочка не включает вход консоли — перепись пуста")
	}
}

// TestConsoleFrontOwnershipJudgement_CanFailAndStaysSilent — суд падает на
// каждом воспроизведённом дефекте рендера стенда и молчит на нём нетронутом.
func TestConsoleFrontOwnershipJudgement_CanFailAndStaysSilent(t *testing.T) {
	vals := consoleValuesOfChain(t, stackChains(t)[frontStandStack])
	if on, _ := dig(vals, "publicFront", "enabled").(bool); !on {
		t.Fatalf("цепочка %s не включает вход — инъекции не на чем стоять", frontStandStack)
	}
	out, err := renderConsoleChart(t, vals)
	if err != nil {
		t.Fatalf("рендер стенда отказал: %v\n%s", err, out)
	}
	host := originHostOf(vals)

	spoil := func(kind string, f func(d map[string]any) bool) []map[string]any {
		docs := decodeDocs(t, out)
		var kept []map[string]any
		for _, d := range docs {
			if str(d["kind"]) == kind && f(d) {
				continue
			}
			kept = append(kept, d)
		}
		return kept
	}
	mutate := func(kind string, f func(d map[string]any)) []map[string]any {
		docs := decodeDocs(t, out)
		for _, d := range docs {
			if str(d["kind"]) == kind {
				f(d)
			}
		}
		return docs
	}

	cases := []struct {
		name string
		docs []map[string]any
		want string
	}{
		{name: "законный близнец: рендер стенда как есть", docs: decodeDocs(t, out)},
		{name: "балансировщик вне релиза", docs: spoil("Service", func(d map[string]any) bool {
			return str(dig(d, "spec", "type")) == "LoadBalancer"
		}), want: "LoadBalancer в рендере 0"},
		{name: "поставщик вне релиза", docs: spoil("Issuer", func(map[string]any) bool { return true }),
			want: "не разрешается в рендере"},
		{name: "сертификат ссылается на внешний ClusterIssuer", docs: mutate("Certificate", func(d map[string]any) {
			d["spec"].(map[string]any)["issuerRef"] = map[string]any{"kind": "ClusterIssuer", "name": "outside", "group": "cert-manager.io"}
		}), want: "ClusterIssuer/outside не разрешается"},
		{name: "решатель вне релиза", docs: spoil("Service", func(d map[string]any) bool {
			return dig(d, "spec", "selector", acmeSolverPodLabel) != nil
		}), want: "Service решателя ACME в рендере 0"},
		{name: "без временного листа", docs: mutate("Certificate", func(d map[string]any) {
			delete(d["metadata"].(map[string]any), "annotations")
		}), want: "без временного листа"},
		{name: "переадресация не пропускает решателя", docs: mutate("ConfigMap", func(d map[string]any) {
			data, _ := d["data"].(map[string]any)
			if s, ok := data["default.conf.template"].(string); ok {
				data["default.conf.template"] = strings.Replace(s, "/.well-known/acme-challenge/", "/.well-known/acme-elsewhere/", -1)
			}
		}), want: "вызов решателя на порту переадресации"},
		{name: "ручка решателя называет чужой Service", docs: mutate("Deployment", func(d map[string]any) {
			for _, ctr := range list(dig(d, "spec", "template", "spec", "containers")) {
				for _, e := range list(dig(ctr, "env")) {
					if str(dig(e, "name")) == "KACHO_UI_"+acmeSolverUpstream+"_UPSTREAM" {
						e.(map[string]any)["value"] = "elsewhere.kacho.svc.cluster.local:8089"
					}
				}
			}
		}), want: "ручка адреса решателя"},
		{name: "внутренний порт наружу", docs: mutate("Service", func(d map[string]any) {
			if str(dig(d, "spec", "type")) == "LoadBalancer" {
				spec := d["spec"].(map[string]any)
				spec["ports"] = append(list(spec["ports"]), map[string]any{"port": 8080, "targetPort": "http"})
			}
		}), want: "ждали ровно 443→https и 80→redirect"},
	}
	for _, c := range cases {
		findings, census := judgeFrontOwnership(t, c.docs, host)
		got := strings.Join(findings, "\n")
		switch {
		case c.want == "" && len(findings) != 0:
			t.Errorf("%s: законный близнец назван находкой (%s):\n%s", c.name, census, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s: суд не назвал %q (%s):\n%s", c.name, c.want, census, got)
		}
	}
}
