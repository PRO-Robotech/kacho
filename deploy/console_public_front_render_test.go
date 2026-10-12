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
//     http ни одна форма не обслуживается. Единственная законная полоса рядом —
//     вызов решателя ACME (kacho#3024): ровно `location ^~
//     /.well-known/acme-challenge/`, только GET, только к адресу решателя
//     (`KACHO_UI_ACME_SOLVER_UPSTREAM`); полоса шире, с другими методами или к
//     другому соседу — находка;
//  4. происхождение консоли — `https`, и его хост назван в сертификате, который
//     выписывается в смонтированный секрет;
//  5. адрес клиента доезжает до раздачи (kacho#3028): Service несёт
//     `externalTrafficPolicy: Local`. При `Cluster` kube-proxy подменяет адрес
//     источника адресом узла, раздача видит вместо клиентов один–четыре адреса
//     узлов, и ограничение частоты «на источник» у службы доступа становится
//     общим на всех: исчерпав его, один клиент отказывает во входе остальным;
//  6. КАЖДАЯ полоса раздачи, проксирующая запрос (`proxy_pass`), сама пишет
//     `X-Forwarded-For` — адресом TCP-пира (`$remote_addr`), а не дописывает
//     его к заголовку клиента (`$proxy_add_x_forwarded_for`). Судится ПОЛОСА, а
//     не найденная строка: полоса без своей строки отдаёт дальше заголовок
//     клиента как есть (nginx пересылает заголовки запроса по умолчанию), и
//     перебор одних найденных строк такую полосу не видит вовсе;
//  7. адрес клиента доезжает через БАЛАНСИРОВЩИК ПЛОЩАДКИ (kacho#3115).
//     `Local` из п. 5 сохраняет адрес только за звеном, которое пакет не
//     проксирует; балансировщик площадки соединяется со своего адреса, и
//     замер #3028 показал за входом одного клиента — адрес балансировщика.
//     Поэтому адрес приходит заголовком PROXY: оба порта входа, на которые
//     ведёт балансировщик (https и redirect), принимают его (`proxy_protocol`
//     у `listen`; порт redirect — через маршрутизатор, ниже), а внутренний
//     порт `http` — нет (пробы готовности приходят
//     от узла без заголовка); раздача верит заголовку ТОЛЬКО от звена
//     балансировки — `real_ip_header proxy_protocol` с непустым
//     `set_real_ip_from`, каждая запись которого — один адрес (/32, /128):
//     порт https доступен хостам кластера, и широкий круг доверия дал бы
//     им заявить чужой источник; Service просит балансировщик слать
//     заголовок аннотацией площадки (`…proxy-protocol` со значением, отличным
//     от `none`) — слушатель, ждущий заголовка, без отправителя принял бы
//     первую строку клиента за адрес. Выключенный приём допустим только с
//     причиной на объекте Service (аннотация
//     kacho.cloud/client-address-proxy-protocol-disabled-because), и тогда
//     заголовка не ждёт ни один порт.
//     Порт redirect заголовка от КАЖДОГО соединения не ждёт: соединение изнутри
//     кластера к внешнему адресу входа (ipMode VIP) замыкается на под, минуя
//     балансировщик, — без заголовка; так приходит и проверка достижимости
//     вызова ACME HTTP-01, которую делает выпускающий сертификат. Поэтому при
//     включённом приёме порт redirect слушает МАРШРУТИЗАТОР (`stream` в
//     nginx.conf той же карты, смонтированной в под): соединение от звена
//     балансировки (тот же круг, что `set_real_ip_from` порта https, каждая
//     запись — один адрес) он ведёт на петлевой слушатель с `proxy_protocol`,
//     остальные — на петлевой слушатель без него; оба петлевых слушателя — в
//     блоке переадресации, и адресу из заголовка там верят только от петли.
//     Маршрутизатор сам заголовка не ждёт и своего не дописывает.
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
// Фикстура рядом с профилем стенда. Профиль a8f60d вход включает сам
// (kacho#3024) — с поставщиком ACME из релиза и именем сертификата из
// происхождения; его рендер судится в общем обходе цепочек. Фикстура держит
// ВТОРОЙ вариант того же входа — с поставщиком площадки (`issuerRef`, ACME
// чарта выключен) и именем, объявленным перечнем, — чтобы перепись не стала
// односторонней по варианту. Хост фикстуры — в зарезервированной зоне
// `.example` (RFC 2606): адреса стенда в сверку не копируются.
//
// Способность упасть — console_public_front_render_injection_test.go.
package deploy_test

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
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
	// Серверный блок карты — от `server {` в начале строки до `}` в начале
	// строки: разобранная карта несёт блоки без отступа, их полосы — с отступом.
	nginxServerBlock = regexp.MustCompile(`(?ms)^server \{(.*?)^\}`)
	loopbackListen   = regexp.MustCompile(`(?m)^\s*listen\s+127\.0\.0\.1:(\d+)((?:\s+proxy_protocol)?)\s*;`)
	streamBlock      = regexp.MustCompile(`(?ms)^stream \{(.*)^\}`)
	streamGeo        = regexp.MustCompile(`(?ms)^\s*geo\s+\$remote_addr\s+\$(\w+)\s*\{(.*?)^\s*\}`)
	streamServer     = regexp.MustCompile(`(?ms)^\s*server\s*\{(.*?)^\s*\}`)
	streamListen     = regexp.MustCompile(`(?m)^\s*listen\s+(\d+)((?:\s+\w+)*)\s*;`)
	streamProxyPass  = regexp.MustCompile(`(?m)^\s*proxy_pass\s+\$(\w+)\s*;`)
	streamAddsHeader = regexp.MustCompile(`(?m)^\s*proxy_protocol\s+on\s*;`)
	geoEntry         = regexp.MustCompile(`(?m)^\s*(\S+)\s+127\.0\.0\.1:(\d+)\s*;`)
	listenDirective  = regexp.MustCompile(`(?m)^\s*listen\s+(\d+)((?:\s+(?:ssl|proxy_protocol))*)\s*;`)
	realIPFrom       = regexp.MustCompile(`(?m)^\s*set_real_ip_from\s+(\S+);`)
	realIPHeader     = regexp.MustCompile(`(?m)^\s*real_ip_header\s+(\S+);`)
	redirectReturn   = regexp.MustCompile(`(?m)^\s*return 308 (\S+)\$request_uri;\s*$`)
	nginxDirective   = regexp.MustCompile(`(?m)^\s*(proxy_pass|try_files|root|alias|fastcgi_pass|grpc_pass)\b`)
	forwardedForSet  = regexp.MustCompile(`(?mi)^\s*proxy_set_header\s+X-Forwarded-For\s+(\S+?)\s*;`)
	realIPSet        = regexp.MustCompile(`(?mi)^\s*proxy_set_header\s+X-Real-IP\s+(\S+?)\s*;`)
	locationHead     = regexp.MustCompile(`(?m)^[ \t]*location\s+([^{]*?)\s*\{`)
	proxyPassLine    = regexp.MustCompile(`(?m)^\s*proxy_pass\s+(\S+?)\s*;`)
)

// nginxLocation — одна полоса раздачи: заголовок `location` (модификатор и
// образец) и её тело без комментариев.
type nginxLocation struct {
	Head, Body, ProxyPass string
}

// stripNginxComment — строка без комментария: `#` в начале строки либо после
// пробела. Без этого фигурная скобка в комментарии сбила бы подсчёт тела.
func stripNginxComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

// nginxLocations — все полосы карты настройки с телами, найденными подсчётом
// фигурных скобок (тело может нести вложенные блоки). Незакрытое тело —
// отказ разбора, а не «полос меньше».
func nginxLocations(conf string) ([]nginxLocation, error) {
	var b strings.Builder
	for _, l := range strings.Split(conf, "\n") {
		b.WriteString(stripNginxComment(l))
		b.WriteByte('\n')
	}
	text := b.String()
	var out []nginxLocation
	for _, m := range locationHead.FindAllStringSubmatchIndex(text, -1) {
		depth, end := 1, -1
		for i := m[1]; i < len(text) && end < 0; i++ {
			switch text[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
		}
		head := strings.TrimSpace(text[m[2]:m[3]])
		if end < 0 {
			return nil, fmt.Errorf("тело полосы `location %s` не закрыто", head)
		}
		loc := nginxLocation{Head: head, Body: text[m[1]:end]}
		if pp := proxyPassLine.FindStringSubmatch(loc.Body); pp != nil {
			loc.ProxyPass = pp[1]
		}
		out = append(out, loc)
	}
	return out, nil
}

// judgeLaneForwardedFor — находки п. 6 по одной карте настройки: каждая
// проксирующая полоса пишет `X-Forwarded-For $remote_addr` и
// `X-Real-IP $remote_addr` сама и ровно так.
// proxied — число проксирующих полос (знаменатель).
func judgeLaneForwardedFor(conf string) (findings []string, proxied int) {
	locs, err := nginxLocations(conf)
	if err != nil {
		return []string{"карта настройки раздачи не разбирается на полосы: " + err.Error()}, 0
	}
	for _, l := range locs {
		if l.ProxyPass == "" {
			continue
		}
		proxied++
		// Оба заголовка адреса, которые раздача несёт дальше, она пишет сама
		// адресом TCP-пира (kacho#3028, C1): дописанный или пропущенный заголовок
		// отдаёт заявление клиента о себе звену за раздачей.
		for _, h := range []struct {
			name string
			re   *regexp.Regexp
		}{{"X-Forwarded-For", forwardedForSet}, {"X-Real-IP", realIPSet}} {
			set := h.re.FindAllStringSubmatch(l.Body, -1)
			if len(set) == 0 {
				findings = append(findings, fmt.Sprintf("полоса `location %s` проксирует (%s) без своей строки "+
					"%s — раздача отдаёт дальше заголовок, присланный клиентом, как есть (kacho#3028)",
					l.Head, l.ProxyPass, h.name))
			}
			for _, m := range set {
				if m[1] != "$remote_addr" {
					findings = append(findings, fmt.Sprintf("полоса `location %s`: %s = %s, ожидался "+
						"$remote_addr — заголовок, присланный клиентом, уходит от раздачи дальше (kacho#3028)",
						l.Head, h.name, m[1]))
				}
			}
		}
	}
	if proxied == 0 {
		findings = append(findings, "в карте настройки раздачи нет ни одной проксирующей полосы — п. 6 судить нечего")
	}
	return findings, proxied
}

// acmeSolverLaneHead — единственная законная полоса слушателя redirect рядом с
// переадресацией: вызов решателя ACME (kacho#3024).
const acmeSolverLaneHead = "^~ /.well-known/acme-challenge/"

var (
	acmeSolverBound  = regexp.MustCompile(`(?m)^\s*set\s+\$acme_solver\s+"\$\{KACHO_UI_ACME_SOLVER_UPSTREAM\}"\s*;`)
	acmeGetOnly      = regexp.MustCompile(`(?m)^\s*limit_except\s+GET\s*\{\s*deny\s+all;\s*\}`)
	acmeSolverLaneRe = regexp.MustCompile(`(?m)^[ \t]*location\s+\^~\s+/\.well-known/acme-challenge/\s*\{`)
)

// cutSolverLane — блок слушателя redirect без полосы решателя ACME и находки о
// самой полосе. Полосы нет — блок как есть, находок нет: поставщик листа тогда
// вне релиза, и порт 80 обязан только переадресовывать.
func cutSolverLane(block string) (string, []string) {
	loc := acmeSolverLaneRe.FindStringIndex(block)
	if loc == nil {
		return block, nil
	}
	depth, end := 1, -1
	for i := loc[1]; i < len(block) && end < 0; i++ {
		switch block[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
	}
	if end < 0 {
		return block, []string{"тело полосы решателя ACME не закрыто"}
	}
	body := block[loc[1]:end]
	var findings []string
	if pp := proxyPassLine.FindStringSubmatch(body); pp == nil || pp[1] != "http://$acme_solver" {
		findings = append(findings, "полоса решателя ACME передаёт запрос не адресу решателя (`proxy_pass http://$acme_solver`)")
	}
	if !acmeSolverBound.MatchString(body) {
		findings = append(findings, "полоса решателя ACME не связывает `$acme_solver` с адресом решателя (KACHO_UI_ACME_SOLVER_UPSTREAM)")
	}
	if !acmeGetOnly.MatchString(body) {
		findings = append(findings, "полоса решателя ACME принимает не только GET — по http не должно приниматься ничего, кроме вызова решателя")
	}
	return block[:loc[0]] + block[end+1:], findings
}

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
	// 5. адрес клиента не подменяется на Service
	if p, _ := lookup(svc, "spec", "externalTrafficPolicy"); p != "Local" {
		say("externalTrafficPolicy %v, ожидался Local — kube-proxy подменяет адрес клиента адресом узла, "+
			"все клиенты за входом становятся одним источником для ограничения частоты (kacho#3028)", p)
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
	conf, mainConf, confMap := "", "", ""
	for _, d := range r.Docs {
		if docKind(d) == "ConfigMap" && strings.HasSuffix(docName(d), "-nginx") {
			t, _ := lookup(d, "data", "default.conf.template")
			m, _ := lookup(d, "data", "nginx.conf")
			tmpl, _ := t.(string)
			main, _ := m.(string)
			if tmpl == "" {
				continue
			}
			rt, _ := parseRedirectRouter(main, named["redirect"])
			if strings.Contains(tmpl, fmt.Sprintf("listen %d", named["redirect"])) || rt != nil {
				conf, mainConf, confMap = tmpl, main, docName(d)
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
	// 6. заголовок пересылки пишет раздача в КАЖДОЙ проксирующей полосе
	laneFindings, _ := judgeLaneForwardedFor(conf)
	for _, f := range laneFindings {
		say("%s", f)
	}
	// 7. выключенный приём заголовка PROXY — только с объявленной причиной на
	// объекте Service; тогда ни один порт заголовка не ждёт.
	ann, _ := lookup(svc, "metadata", "annotations")
	annMap, _ := ann.(map[string]any)
	disabledBecause := strings.TrimSpace(fmt.Sprint(annMap[proxyProtocolDisabledBecause]))
	proxyOff := annMap[proxyProtocolDisabledBecause] != nil && disabledBecause != ""
	tlsServed, redirectOnly := false, false
	router, routerFindings := parseRedirectRouter(mainConf, named["redirect"])
	for _, f := range routerFindings {
		say("%s", f)
	}
	routed := map[int]bool{}
	if router != nil {
		routed[router.Default] = true
		for _, p := range router.Trusted {
			routed[p] = true
		}
	}
	// Переадресация — одна и та же для слушателя порта redirect и для петлевых
	// слушателей маршрутизатора.
	judgeRedirect := func(block string, port int) {
		block, solverFindings := cutSolverLane(block)
		for _, f := range solverFindings {
			say("слушатель redirect %d: %s", port, f)
		}
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
	httpsTrust := []string{}
	loopProxied := map[int]bool{}
	loopBlock := map[int]int{}
	routedBlock := ""
	for bi, m := range nginxServerBlock.FindAllStringSubmatch(conf, -1) {
		block := m[1]
		blockRouted := false
		for _, l := range loopbackListen.FindAllStringSubmatch(block, -1) {
			port := toInt(l[1])
			loopProxied[port] = strings.Contains(l[2], "proxy_protocol")
			loopBlock[port] = bi
			if routed[port] {
				blockRouted = true
			}
		}
		if blockRouted {
			routedBlock = block
			judgeRedirect(block, named["redirect"])
		}
		for _, l := range listenDirective.FindAllStringSubmatch(block, -1) {
			port := toInt(l[1])
			// 7. заголовок PROXY — на портах балансировщика, и только на них.
			proxied := strings.Contains(l[2], "proxy_protocol")
			switch {
			case proxyOff && proxied:
				say("порт %d ждёт заголовок PROXY, а Service объявляет приём выключенным (%s) — "+
					"балансировщик заголовка не шлёт, и слушатель отверг бы каждое соединение", port, disabledBecause)
			case proxyOff:
			case port == named["redirect"] && proxied:
				say("порт redirect %d ждёт заголовок PROXY от КАЖДОГО соединения — соединение изнутри кластера "+
					"к внешнему адресу входа (ipMode VIP) приходит без него, и проверка вызова ACME HTTP-01 "+
					"встала бы; порт redirect слушает маршрутизатор по звену балансировки (kacho#3115)", port)
			case (port == named["https"] || port == named["redirect"]) && !proxied:
				say("порт %d, на который ведёт балансировщик площадки, не принимает заголовок PROXY и причины "+
					"выключения нет (%s) — адрес клиента за балансировщиком теряется, все клиенты становятся "+
					"одним источником (kacho#3115)", port, proxyProtocolDisabledBecause)
			case port == named["http"] && proxied:
				say("внутренний порт http %d ждёт заголовок PROXY — пробы готовности приходят без него", port)
			}
			if port == named["https"] && !proxyOff {
				say7(block, port, &out, r, svc)
				for _, f := range realIPFrom.FindAllStringSubmatch(block, -1) {
					httpsTrust = append(httpsTrust, f[1])
				}
			}
			switch {
			case port == named["https"] && strings.Contains(l[2], "ssl"):
				tlsServed = true
			case port == named["https"]:
				say("порт https %d слушается без ssl", port)
			case port == named["redirect"] && !blockRouted:
				judgeRedirect(block, port)
			}
		}
	}
	if router != nil {
		if proxyOff {
			say("порт redirect %d слушает маршрутизатор заголовка PROXY, а Service объявляет приём выключенным (%s)",
				named["redirect"], disabledBecause)
		} else {
			for _, f := range judgeRedirectRouter(router, httpsTrust, loopProxied, loopBlock, routedBlock) {
				say("%s", f)
			}
			if !podMountsMainConf(pod, confMap) {
				say("порт redirect %d слушает маршрутизатор из nginx.conf карты %s, а nginx.conf в под раздачи "+
					"не смонтирован — nginx маршрутизатора не исполняет, и порт redirect не слушается вовсе",
					named["redirect"], confMap)
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

// publicFrontFixtureSets — вход, включённый ручками чарта, с поставщиком
// площадки вместо поставщика из релиза.
var publicFrontFixtureSets = []string{
	"global.kacho.identity.appBaseURL=" + publicFrontFixtureOrigin,
	"uif.publicFront.enabled=true",
	"uif.publicFront.tls.secretName=console-public-tls",
	"uif.publicFront.tls.certificate.create=true",
	"uif.publicFront.tls.certificate.issuerRef.name=console-public",
	"uif.publicFront.tls.certificate.dnsNames[0]=console.stand.example",
	"uif.publicFront.tls.certificate.namesFromOrigin=false",
	"uif.publicFront.acme.enabled=false",
}

// readPublicFrontRenders — рендер каждой цепочки с её объявленным
// происхождением и, последней, фикстурная цепочка со входом.
func readPublicFrontRenders(t *testing.T) []publicFrontRender {
	t.Helper()
	requireUmbrellaPackagedFromTree(t)
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

// say7 — п. 7: доверие заголовку PROXY только от звена балансировки и
// просьба к балансировщику слать заголовок.
func say7(block string, port int, out *[]string, r publicFrontRender, svc map[string]any) {
	say := func(format string, a ...any) {
		*out = append(*out, fmt.Sprintf("цепочка %s, Service %s: ", r.Stack, docName(svc))+fmt.Sprintf(format, a...))
	}
	hdr := realIPHeader.FindStringSubmatch(block)
	if hdr == nil || hdr[1] != "proxy_protocol" {
		say("серверный блок порта https %d не берёт адрес клиента из заголовка PROXY (`real_ip_header proxy_protocol`)", port)
	}
	from := realIPFrom.FindAllStringSubmatch(block, -1)
	if len(from) == 0 {
		say("у порта https %d нет `set_real_ip_from` — круг доверия заголовку PROXY пуст", port)
	}
	for _, f := range from {
		if !narrowTrustEntry(f[1]) {
			say("`set_real_ip_from %s` шире одного адреса звена балансировки (/32, /128) либо не адрес — "+
				"хост кластера, дошедший до порта https, заявил бы чужой источник", f[1])
		}
	}
	ann, _ := lookup(svc, "metadata", "annotations")
	am, _ := ann.(map[string]any)
	asked := false
	for k, v := range am {
		if strings.HasSuffix(k, "proxy-protocol") && fmt.Sprint(v) != "" && fmt.Sprint(v) != "none" {
			asked = true
		}
	}
	if !asked {
		say("Service не просит балансировщик слать заголовок PROXY (аннотация `…proxy-protocol`) — " +
			"слушатель ждёт заголовка, которого никто не шлёт")
	}
}

// proxyProtocolDisabledBecause — аннотация Service, которой чарт консоли
// объявляет причину выключенного приёма заголовка PROXY (ui-future/deploy,
// service-public.yaml).
const proxyProtocolDisabledBecause = "kacho.cloud/client-address-proxy-protocol-disabled-because"

// narrowTrustEntry — запись круга доверия — ОДИН адрес (/32, /128): порт https
// раздачи доступен хостам кластера, и каждый хост доверенной подсети заявил
// бы чужой источник.
func narrowTrustEntry(e string) bool {
	if ip := net.ParseIP(e); ip != nil {
		return true
	}
	_, n, err := net.ParseCIDR(e)
	if err != nil {
		return false
	}
	ones, bits := n.Mask.Size()
	return ones == bits
}

// redirectRouter — маршрутизатор порта redirect (п. 7): блок `stream` в
// nginx.conf карты раздачи.
type redirectRouter struct {
	Flags      string         // флаги `listen` маршрутизатора
	AddsHeader bool           // `proxy_protocol on` — дописывает свой заголовок
	Default    int            // петлевой порт прочих соединений
	Trusted    map[string]int // запись круга звена балансировки → петлевой порт
}

// parseRedirectRouter — маршрутизатор, слушающий порт redirect, либо nil, если
// его нет. Находки — о маршрутизаторе, который слушает порт, но не разбирается.
func parseRedirectRouter(main string, port int) (*redirectRouter, []string) {
	if main == "" {
		return nil, nil
	}
	var b strings.Builder
	for _, l := range strings.Split(main, "\n") {
		b.WriteString(stripNginxComment(l))
		b.WriteByte('\n')
	}
	st := streamBlock.FindStringSubmatch(b.String())
	if st == nil {
		return nil, nil
	}
	for _, sv := range streamServer.FindAllStringSubmatch(st[1], -1) {
		l := streamListen.FindStringSubmatch(sv[1])
		if l == nil || toInt(l[1]) != port {
			continue
		}
		r := &redirectRouter{Flags: l[2], AddsHeader: streamAddsHeader.MatchString(sv[1]), Trusted: map[string]int{}}
		pp := streamProxyPass.FindStringSubmatch(sv[1])
		if pp == nil {
			return r, []string{fmt.Sprintf("маршрутизатор порта redirect %d ведёт соединения не переменной маршрута (`proxy_pass $<маршрут>`)", port)}
		}
		var geo []string
		for _, g := range streamGeo.FindAllStringSubmatch(st[1], -1) {
			if g[1] == pp[1] {
				geo = g
			}
		}
		if geo == nil {
			return r, []string{fmt.Sprintf("маршрут $%s маршрутизатора порта redirect %d не объявлен блоком `geo $remote_addr`", pp[1], port)}
		}
		for _, e := range geoEntry.FindAllStringSubmatch(geo[2], -1) {
			if e[1] == "default" {
				r.Default = toInt(e[2])
				continue
			}
			r.Trusted[e[1]] = toInt(e[2])
		}
		return r, nil
	}
	return nil, nil
}

// judgeRedirectRouter — находки п. 7 о маршрутизаторе порта redirect при
// включённом приёме: звену балансировки — петлевой слушатель с заголовком,
// прочим — без него; круг тот же, что у порта https; оба слушателя — в блоке
// переадресации, и адресу из заголовка там верят только от петли.
func judgeRedirectRouter(r *redirectRouter, httpsTrust []string, loopProxied map[int]bool, loopBlock map[int]int, block string) []string {
	var out []string
	if strings.Contains(r.Flags, "proxy_protocol") {
		out = append(out, "маршрутизатор порта redirect сам ждёт заголовок PROXY от каждого соединения — соединение "+
			"изнутри кластера (проверка вызова ACME HTTP-01) отвергается")
	}
	if r.AddsHeader {
		out = append(out, "маршрутизатор порта redirect дописывает свой заголовок PROXY (`proxy_protocol on`) — "+
			"заголовок балансировщика уходит дальше вторым, и адресом клиента становится адрес балансировщика")
	}
	if len(r.Trusted) == 0 {
		out = append(out, "у маршрутизатора порта redirect нет записи звена балансировки — заголовок PROXY не "+
			"принимается ни от кого, и соединение балансировщика читается как запрос с мусорной первой строкой")
	}
	var circle []string
	for e, p := range r.Trusted {
		circle = append(circle, e)
		if !narrowTrustEntry(e) {
			out = append(out, fmt.Sprintf("запись маршрутизатора порта redirect %q шире одного адреса звена "+
				"балансировки (/32, /128) либо не адрес — хост кластера заявил бы чужой источник", e))
		}
		switch px, ok := loopProxied[p]; {
		case !ok:
			out = append(out, fmt.Sprintf("соединение звена балансировки маршрутизатор ведёт на петлевой порт %d, "+
				"который не слушается", p))
		case !px:
			out = append(out, fmt.Sprintf("соединение звена балансировки маршрутизатор ведёт на петлевой порт %d, "+
				"который не принимает заголовок PROXY — первая строка соединения читается как запрос", p))
		}
		if p == r.Default {
			out = append(out, fmt.Sprintf("звено балансировки и прочие соединения маршрутизатор ведёт на один "+
				"петлевой порт %d", p))
		}
	}
	sort.Strings(circle)
	want := append([]string(nil), httpsTrust...)
	sort.Strings(want)
	if strings.Join(circle, ",") != strings.Join(want, ",") {
		out = append(out, fmt.Sprintf("круг маршрутизатора порта redirect %v расходится с кругом доверия порта "+
			"https %v — звено балансировки объявляется одним местом", circle, want))
	}
	switch px, ok := loopProxied[r.Default]; {
	case r.Default == 0 || !ok:
		out = append(out, "прочие соединения (изнутри кластера) маршрутизатор порта redirect не ведёт на "+
			"слушаемый петлевой порт — проверка вызова ACME HTTP-01 изнутри кластера встала бы")
	case px:
		out = append(out, fmt.Sprintf("прочие соединения маршрутизатор порта redirect ведёт на петлевой порт %d, "+
			"который ждёт заголовок PROXY — соединение изнутри кластера отвергается", r.Default))
	}
	blocks := map[int]bool{}
	for p := range loopProxied {
		if p == r.Default || slicesContainsInt(r.Trusted, p) {
			blocks[loopBlock[p]] = true
		}
	}
	if len(blocks) > 1 {
		out = append(out, "петлевые слушатели маршрутизатора порта redirect — в разных серверных блоках")
	}
	if block != "" {
		hdr := realIPHeader.FindStringSubmatch(block)
		if hdr == nil || hdr[1] != "proxy_protocol" {
			out = append(out, "блок переадресации за маршрутизатором не берёт адрес клиента из заголовка PROXY "+
				"(`real_ip_header proxy_protocol`)")
		}
		from := realIPFrom.FindAllStringSubmatch(block, -1)
		if len(from) == 0 {
			out = append(out, "у блока переадресации за маршрутизатором нет `set_real_ip_from` петли")
		}
		for _, f := range from {
			if f[1] != "127.0.0.1" && f[1] != "127.0.0.1/32" {
				out = append(out, fmt.Sprintf("блок переадресации за маршрутизатором верит адресу из заголовка "+
					"не только от петли (`set_real_ip_from %s`) — заголовок туда приводит только маршрутизатор", f[1]))
			}
		}
	}
	return out
}

func slicesContainsInt(m map[string]int, v int) bool {
	for _, x := range m {
		if x == v {
			return true
		}
	}
	return false
}

// podMountsMainConf — под раздачи монтирует nginx.conf из карты confMap в
// /etc/nginx/nginx.conf: без этого блок `stream` карты nginx не читает.
func podMountsMainConf(pod map[string]any, confMap string) bool {
	vols := map[string]bool{}
	vs, _ := lookup(pod, "spec", "template", "spec", "volumes")
	vsl, _ := vs.([]any)
	for _, v := range vsl {
		vm, _ := v.(map[string]any)
		name, _ := lookup(vm, "configMap", "name")
		if name != confMap {
			continue
		}
		items, _ := lookup(vm, "configMap", "items")
		il, _ := items.([]any)
		for _, it := range il {
			im, _ := it.(map[string]any)
			if im["key"] == "nginx.conf" {
				vols[fmt.Sprint(vm["name"])+"/"+fmt.Sprint(im["path"])] = true
			}
		}
	}
	cs, _ := lookup(pod, "spec", "template", "spec", "containers")
	csl, _ := cs.([]any)
	for _, c := range csl {
		cm, _ := c.(map[string]any)
		vms, _ := cm["volumeMounts"].([]any)
		for _, vm := range vms {
			m, _ := vm.(map[string]any)
			if m["mountPath"] == "/etc/nginx/nginx.conf" && vols[fmt.Sprint(m["name"])+"/"+fmt.Sprint(m["subPath"])] {
				return true
			}
		}
	}
	return false
}
