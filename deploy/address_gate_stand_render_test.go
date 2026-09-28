// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// address_gate_stand_render_test.go — ПРИЁМНИК ПИСЕМ ТОЛЬКО У СТЕНДА, ПОЧТОВАЯ
// ПОЛОСА СЛУЖБЫ ДОСТУПА ДОХОДИТ ДО НЕГО, И НИ ОДНА РУЧКА РУБЕЖ НЕ СНИМАЕТ
// (приёмка F6b, сценарии F6b-54 и F6b-56; kacho#2901).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Решение владельца 2026-09-27: дальше экранов регистрации и входа человек
// проходит только с подтверждённым адресом почты, а подтверждает адрес код из
// письма. На стенде письмо ставит служба доступа, и уходит оно к приёмнику писем
// стенда — узлу, который поднимает эта же поставка (`templates/mail-receiver.yaml`).
// Отсюда три свойства рендера, и каждое судится здесь исходом, а не объявлением:
//
//   - F6b-54: боевая посадка приёмника не несёт; у стенда наборов он ровно один;
//     в блоке `authn.login` настроек службы ключей с приставкой `verification-`
//     ровно пять, с величинами профиля продукта, — шестого, который снимал бы
//     рубеж, нет ни в одном из двух рендеров;
//   - F6b-56: полоса службы на стенде проверяет сертификат приёмника якорем ЕГО ЖЕ
//     секрета (`ca-bundle-file` — путь, по которому рабочему объекту службы
//     смонтирован ключ `ca.crt` этого секрета, и только он), и несёт адрес входа
//     консоли, чьё происхождение — происхождение консоли профиля;
//   - близнец F6b-56: у боевой посадки тома из секрета приёмника нет, и якорь
//     полосы на такой том не указывает.
//
// Полоса к приёмнику без его якоря проверяла бы сертификат системными корнями:
// лист приёмника выписан внутренним удостоверяющим стенда, рукопожатие не
// состоится, письмо не уйдёт, и человек стенда останется в положении
// подтверждения навсегда — то есть посев наборов стал бы «условием не создано»
// на каждом прогоне без дефекта в продукте.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Что письмо ДОШЛО — вопрос поднятого стенда; держат его посев наборов (F6b-46,
// F6b-53) и прогон, а не рендер. Отказ старта службы без любой из пяти ручек —
// страж службы (EV-90) и проба зонта address_verification_knobs_umbrella_test.go.
package deploy_test

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Имена стеков — из таблицы stacks.txt, а не цепочки: вторая копия цепочки —
// находка stack_table_test.go. Приёмка называет (а) боевой профиль и (б) профиль
// стенда наборов и консоли; в таблице это стеки ниже, и их цепочки проба печатает
// из таблицы, а не выписывает.
const (
	addrGateProdStack  = "prod"
	addrGateStandStack = "dev-prod"
)

// addrGateKnobs — пять ключей подтверждения адреса и величины профиля продукта
// (Р9 приёмки службы; F6b-54).
var addrGateKnobs = map[string]string{
	"verification-code-ttl":        "30m",
	"verification-code-attempts":   "5",
	"verification-resend-interval": "60s",
	"verification-resend-limit":    "5",
	"verification-resend-window":   "24h",
}

// Метка, которой шаблон приёмника метит ВСЕ свои объекты. Признак, а не имя:
// имя выводится из релиза.
const (
	mailReceiverLabelKey   = "kacho.cloud/component"
	mailReceiverLabelValue = "mail-receiver"
)

// stackRender — рендер одного стека, разобранный до того, что судят пробы.
type stackRender struct {
	name   string
	chain  []string
	values map[string]any
	docs   []renderedDoc
}

func renderNamedStack(t *testing.T, name string) stackRender {
	t.Helper()
	chain, ok := deployStacks(t)[name]
	if !ok {
		t.Fatalf("стека %q в таблице stacks.txt нет — предпосылка пробы исчезла, а не "+
			"рендер стал чистым", name)
	}
	out, err := renderStack(t, chain)
	if err != nil {
		t.Fatalf("стек %s (%s): рендер отказал: %v\n%s", name, strings.Join(chain, " + "), err, out)
	}
	merged := readYAML(t, umbrellaDir+"/values.yaml")
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, umbrellaDir+"/"+p))
	}
	return stackRender{name: name, chain: chain, values: merged, docs: decodeRender(t, out)}
}

func docMeta(d renderedDoc) (kind, name string, labels map[string]any) {
	kind, _ = d["kind"].(string)
	md, _ := d["metadata"].(map[string]any)
	name, _ = md["name"].(string)
	labels, _ = md["labels"].(map[string]any)
	return kind, name, labels
}

// receiverObjects — объекты шаблона приёмника по виду.
func (r stackRender) receiverObjects() map[string][]renderedDoc {
	out := map[string][]renderedDoc{}
	for _, d := range r.docs {
		kind, _, labels := docMeta(d)
		if v, _ := labels[mailReceiverLabelKey].(string); v == mailReceiverLabelValue {
			out[kind] = append(out[kind], d)
		}
	}
	return out
}

// serviceConfig — настройки службы доступа, как их получает процесс: ключ
// `config.yaml` карты, которую монтирует её рабочий объект.
func (r stackRender) serviceConfig(t *testing.T) (map[string]any, renderedDoc) {
	t.Helper()
	var cm renderedDoc
	for _, d := range r.docs {
		kind, name, _ := docMeta(d)
		if kind == "ConfigMap" && name == "kaname-config" {
			cm = d
		}
	}
	if cm == nil {
		t.Fatalf("стек %s: карты настроек службы доступа (kaname-config) в рендере нет", r.name)
	}
	data, _ := cm["data"].(map[string]any)
	body, _ := data["config.yaml"].(string)
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil || cfg == nil {
		t.Fatalf("стек %s: config.yaml службы доступа не разбирается: %v", r.name, err)
	}
	var dep renderedDoc
	for _, d := range r.docs {
		kind, _, _ := docMeta(d)
		if kind != "Deployment" {
			continue
		}
		for _, v := range podVolumes(d) {
			if cmv, _ := v["configMap"].(map[string]any); cmv != nil && cmv["name"] == "kaname-config" {
				dep = d
			}
		}
	}
	if dep == nil {
		t.Fatalf("стек %s: рабочего объекта, монтирующего kaname-config, в рендере нет", r.name)
	}
	return cfg, dep
}

func podSpec(d renderedDoc) map[string]any {
	spec, _ := d["spec"].(map[string]any)
	tpl, _ := spec["template"].(map[string]any)
	ps, _ := tpl["spec"].(map[string]any)
	return ps
}

func podVolumes(d renderedDoc) []map[string]any {
	var out []map[string]any
	vs, _ := podSpec(d)["volumes"].([]any)
	for _, v := range vs {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// secretVolumes — тома рабочего объекта из секрета с данным именем.
func secretVolumes(d renderedDoc, secret string) []map[string]any {
	var out []map[string]any
	for _, v := range podVolumes(d) {
		if s, _ := v["secret"].(map[string]any); s != nil && s["secretName"] == secret {
			out = append(out, v)
		}
	}
	return out
}

// mountDirOf — каталог, в который главный (первый) контейнер монтирует том.
func mountDirOf(d renderedDoc, volume string) string {
	cs, _ := podSpec(d)["containers"].([]any)
	if len(cs) == 0 {
		return ""
	}
	c, _ := cs[0].(map[string]any)
	ms, _ := c["volumeMounts"].([]any)
	for _, m := range ms {
		mm, _ := m.(map[string]any)
		if mm["name"] == volume {
			s, _ := mm["mountPath"].(string)
			return s
		}
	}
	return ""
}

func TestF6b54_ProdLandingCarriesNoReceiverAndFiveKnobsLiftNothing(t *testing.T) {
	prod := renderNamedStack(t, addrGateProdStack)
	stand := renderNamedStack(t, addrGateStandStack)
	t.Logf("F6b-54: (а) стек %s — %v; (б) стек %s — %v", prod.name, prod.chain, stand.name, stand.chain)

	// (а) боевая посадка: объектов шаблона приёмника нет ни одного.
	for kind, objs := range prod.receiverObjects() {
		t.Errorf("F6b-54 (а): боевая посадка несёт %d объект(ов) вида %s шаблона приёмника писем — "+
			"приёмник стенда в боевой посадке не поднимается (Р18)", len(objs), kind)
	}
	// (б) стенд наборов: ровно один рабочий объект приёмника и ровно одна служба с тем же именем.
	rx := stand.receiverObjects()
	if len(rx["Deployment"]) != 1 || len(rx["Service"]) != 1 {
		t.Fatalf("F6b-54 (б): у стенда наборов рабочих объектов приёмника %d и служб %d, ждали по одному",
			len(rx["Deployment"]), len(rx["Service"]))
	}
	_, depName, _ := docMeta(rx["Deployment"][0])
	_, svcName, _ := docMeta(rx["Service"][0])
	if depName != "kacho-umbrella-mailpit" || svcName != depName {
		t.Errorf("F6b-54 (б): приёмник назван %q, служба %q — ждали kacho-umbrella-mailpit у обоих", depName, svcName)
	}

	// Оба рендера: в блоке authn.login ключей `verification-` ровно пять, с величинами профиля.
	for _, r := range []stackRender{prod, stand} {
		cfg, _ := r.serviceConfig(t)
		login, _ := lookup(cfg, "authn", "login")
		block, _ := login.(map[string]any)
		if len(block) == 0 {
			t.Fatalf("F6b-54: стек %s — блок authn.login настроек службы пуст: осмотрено ключей 0, "+
				"проба не вправе считать это отсутствием находок", r.name)
		}
		var seen []string
		for k, v := range block {
			if !strings.HasPrefix(k, "verification-") {
				continue
			}
			seen = append(seen, k)
			want, known := addrGateKnobs[k]
			if !known {
				t.Errorf("F6b-54: стек %s — в authn.login ключ %q с приставкой verification-, которого "+
					"нет среди пяти ручек рубежа: шестая ручка рядом с рубежом — находка (Р11, Р18)", r.name, k)
				continue
			}
			if got := yamlScalar(v); got != want {
				t.Errorf("F6b-54: стек %s — %s = %q, величина профиля продукта %q", r.name, k, got, want)
			}
		}
		sort.Strings(seen)
		t.Logf("F6b-54: стек %s — осмотрено ключей authn.login: %d; с приставкой verification-: %d %v",
			r.name, len(block), len(seen), seen)
		if len(seen) != len(addrGateKnobs) {
			t.Errorf("F6b-54: стек %s — ключей verification- %d, ждали ровно %d", r.name, len(seen), len(addrGateKnobs))
		}
	}
}

func yamlScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case nil:
		return ""
	default:
		b, _ := yaml.Marshal(v)
		return strings.TrimSpace(string(b))
	}
}

// laneAnchor — что полоса службы объявляет о якоре и адресе входа.
type laneAnchor struct {
	block    map[string]any
	relay    string
	caBundle string
	loginURL string
}

func inviteMailOf(cfg map[string]any) laneAnchor {
	im, _ := cfg["invite-mail"].(map[string]any)
	a := laneAnchor{block: im}
	a.relay, _ = im["relay"].(string)
	a.caBundle, _ = im["ca-bundle-file"].(string)
	a.loginURL, _ = im["login-url"].(string)
	return a
}

func TestF6b56_StandMailLaneVerifiesTheReceiverByItsOwnAnchor(t *testing.T) {
	stand := renderNamedStack(t, addrGateStandStack)
	prod := renderNamedStack(t, addrGateProdStack)

	// ── (б) стенд наборов и консоли ────────────────────────────────────────
	rx := stand.receiverObjects()
	if len(rx["Certificate"]) != 1 || len(rx["Service"]) != 1 {
		t.Fatalf("F6b-56 (б): у стенда сертификатов приёмника %d и служб %d — ждали по одному",
			len(rx["Certificate"]), len(rx["Service"]))
	}
	certSpec, _ := rx["Certificate"][0]["spec"].(map[string]any)
	receiverSecret, _ := certSpec["secretName"].(string)
	if receiverSecret != "kacho-mailpit-tls" {
		t.Errorf("F6b-56 (б): секрет сертификата приёмника %q, приёмка называет kacho-mailpit-tls", receiverSecret)
	}
	_, receiverSvc, _ := docMeta(rx["Service"][0])
	smtpPort := ""
	svcSpec, _ := rx["Service"][0]["spec"].(map[string]any)
	ports, _ := svcSpec["ports"].([]any)
	for _, p := range ports {
		pm, _ := p.(map[string]any)
		if pm["name"] == "smtp" {
			smtpPort = yamlScalar(pm["port"])
		}
	}

	cfg, dep := stand.serviceConfig(t)
	lane := inviteMailOf(cfg)
	t.Logf("F6b-56 (б): стек %s — осмотрено ключей блока invite-mail: %d", stand.name, len(lane.block))
	if len(lane.block) == 0 {
		t.Fatalf("F6b-56 (б): у стенда блока invite-mail в настройках службы нет — осмотрено 0; "+
			"проба не вправе считать это отсутствием находок")
	}
	if want := receiverSvc + ":" + smtpPort; lane.relay != want {
		t.Errorf("F6b-56 (б): relay = %q, узел приёмника стенда — %q", lane.relay, want)
	}

	vols := secretVolumes(dep, receiverSecret)
	if len(vols) != 1 {
		t.Fatalf("F6b-56 (б): томов рабочего объекта службы из секрета %s — %d, ждали ровно один "+
			"(ca-bundle-file = %q)", receiverSecret, len(vols), lane.caBundle)
	}
	sec, _ := vols[0]["secret"].(map[string]any)
	items, _ := sec["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("F6b-56 (б): том %v проецирует %d ключей секрета приёмника — ждали ровно один ca.crt: "+
			"без перечня том отдал бы службе и закрытый ключ приёмника", vols[0]["name"], len(items))
	}
	item, _ := items[0].(map[string]any)
	if item["key"] != "ca.crt" {
		t.Errorf("F6b-56 (б): том проецирует ключ %v, ждали ca.crt", item["key"])
	}
	volName, _ := vols[0]["name"].(string)
	dir := mountDirOf(dep, volName)
	if dir == "" {
		t.Fatalf("F6b-56 (б): том %s объявлен, но главный контейнер службы его не монтирует", volName)
	}
	itemPath, _ := item["path"].(string)
	if want := strings.TrimSuffix(dir, "/") + "/" + itemPath; lane.caBundle != want {
		t.Errorf("F6b-56 (б): ca-bundle-file = %q, а ключ ca.crt секрета приёмника смонтирован по %q", lane.caBundle, want)
	}

	appBase, _ := lookup(stand.values, "global", "kacho", "identity", "appBaseURL")
	wantOrigin := originOf(yamlScalar(appBase))
	if wantOrigin == "" {
		t.Fatalf("F6b-56 (б): у профиля стенда global.kacho.identity.appBaseURL = %v — происхождения нет", appBase)
	}
	if got := originOf(lane.loginURL); got != wantOrigin {
		t.Errorf("F6b-56 (б): login-url = %q, его происхождение %q, а происхождение консоли профиля %q",
			lane.loginURL, got, wantOrigin)
	}

	// ── (а) боевой профиль — близнец, различие только в профиле ────────────
	if n := len(prod.receiverObjects()); n != 0 {
		t.Errorf("F6b-56 (а): боевая посадка несёт объекты приёмника (%d видов)", n)
	}
	pcfg, pdep := prod.serviceConfig(t)
	secretName, _ := lookup(prod.values, "mailpit", "tlsSecretName")
	name := yamlScalar(secretName)
	if name == "" {
		t.Fatalf("F6b-56 (а): mailpit.tlsSecretName умбреллы пуст — сверять боевой рабочий объект не с чем")
	}
	pv := secretVolumes(pdep, name)
	for _, v := range pv {
		t.Errorf("F6b-56 (а): рабочий объект службы в боевой посадке монтирует том %v из секрета приёмника %s", v["name"], name)
	}
	plane := inviteMailOf(pcfg)
	t.Logf("F6b-56 (а): стек %s — осмотрено ключей блока invite-mail: %d", prod.name, len(plane.block))
	if plane.caBundle != "" {
		for _, v := range podVolumes(pdep) {
			vn, _ := v["name"].(string)
			if d := mountDirOf(pdep, vn); d != "" && strings.HasPrefix(plane.caBundle, strings.TrimSuffix(d, "/")+"/") {
				if s, _ := v["secret"].(map[string]any); s != nil && s["secretName"] == name {
					t.Errorf("F6b-56 (а): ca-bundle-file боевой посадки %q указывает на том секрета приёмника", plane.caBundle)
				}
			}
		}
	}
}

func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// TestF6b56_EveryStackRaisingTheReceiverAnchorsOnlyTheLaneThatNamesIt — то же
// свойство по КАЖДОМУ стеку таблицы, а не по двум названным: стек, поднимающий
// приёмник и ведущий полосу к нему, обязан нести его якорь; стек, ведущий полосу
// к внешнему ретранслятору, якоря приёмника нести не вправе — якорь заменяет
// системные корни процесса, и сертификат ретранслятора тогда не проверился бы.
func TestF6b56_EveryStackRaisingTheReceiverAnchorsOnlyTheLaneThatNamesIt(t *testing.T) {
	names := make([]string, 0)
	for n := range deployStacks(t) {
		names = append(names, n)
	}
	sort.Strings(names)
	var toReceiver, elsewhere, none int
	for _, n := range names {
		r := renderNamedStack(t, n)
		cfg, dep := r.serviceConfig(t)
		lane := inviteMailOf(cfg)
		rx := r.receiverObjects()
		secretName, _ := lookup(r.values, "mailpit", "tlsSecretName")
		receiverSecret := yamlScalar(secretName)
		anchored := len(secretVolumes(dep, receiverSecret)) > 0
		var receiverSvc string
		if len(rx["Service"]) == 1 {
			_, receiverSvc, _ = docMeta(rx["Service"][0])
		}
		host := strings.SplitN(lane.relay, ":", 2)[0]
		switch {
		case lane.relay == "":
			none++
			if anchored || lane.caBundle != "" {
				t.Errorf("стек %s: полосы нет, а якорь объявлен (том %v, ca-bundle-file %q)", n, anchored, lane.caBundle)
			}
		case receiverSvc != "" && host == receiverSvc:
			toReceiver++
			if !anchored || lane.caBundle == "" {
				t.Errorf("стек %s: полоса ведёт к приёмнику %s, а якоря его секрета нет (том %v, ca-bundle-file %q)",
					n, receiverSvc, anchored, lane.caBundle)
			}
		default:
			elsewhere++
			if anchored {
				t.Errorf("стек %s: полоса ведёт к %s, а служба монтирует якорь приёмника стенда — "+
					"сертификат ретранслятора этим якорем не проверится", n, lane.relay)
			}
		}
	}
	t.Logf("стеков осмотрено %d: полоса к приёмнику — %d, к внешнему узлу — %d, без полосы — %d",
		len(names), toReceiver, elsewhere, none)
	if toReceiver == 0 {
		t.Errorf("ни один стек не ведёт полосу к приёмнику — предпосылка пробы исчезла")
	}
}

// TestF6b56_LaneGuardRefusesAnAnchorThatDoesNotMatchTheLane — страж рендера
// (templates/identity-mail-lane-guard.yaml, (7)–(8); шаблон подчарта —
// половина пары) обязан уметь отказать. Каждая инъекция меняет ОДИН факт
// против законного близнеца, и законный близнец — неизменённый стек — молчит.
func TestF6b56_LaneGuardRefusesAnAnchorThatDoesNotMatchTheLane(t *testing.T) {
	stacks := deployStacks(t)
	cases := []struct {
		name, stack string
		sets        []string
		want        string // пусто — рендер обязан пройти
	}{
		{"законный близнец: стенд к приёмнику с его якорем", addrGateStandStack, nil, ""},
		{"законный близнец: внешний ретранслятор без якоря", "a8f60d", nil, ""},
		{"полоса к приёмнику без якоря", addrGateStandStack,
			[]string{"global.kacho.identity.smtp.trustAnchorSecret.name=", "global.kacho.identity.smtp.trustAnchorSecret.key="},
			"якорь нашего отправителя"},
		{"полоса к приёмнику, якорь — чужой секрет", addrGateStandStack,
			[]string{"global.kacho.identity.smtp.trustAnchorSecret.name=kacho-other-tls"},
			"якорь нашего отправителя"},
		{"внешний ретранслятор с якорем приёмника", "a8f60d",
			[]string{"global.kacho.identity.smtp.trustAnchorSecret.name=kacho-mailpit-tls", "global.kacho.identity.smtp.trustAnchorSecret.key=ca.crt"},
			"секрет приёмника стенда"},
		{"якорь наполовину: имя без ключа", addrGateStandStack,
			[]string{"global.kacho.identity.smtp.trustAnchorSecret.key="},
			"объявлен наполовину"},
		{"якорь наполовину: ключ без имени", "a8f60d",
			[]string{"global.kacho.identity.smtp.trustAnchorSecret.key=ca.crt"},
			"объявлен наполовину"},
		{"якорь без полосы", addrGateProdStack,
			[]string{"global.kacho.identity.smtp.trustAnchorSecret.name=kacho-mailpit-tls", "global.kacho.identity.smtp.trustAnchorSecret.key=ca.crt"},
			"узел полосы НЕ задан"},
	}
	for _, c := range cases {
		chain, ok := stacks[c.stack]
		if !ok {
			t.Fatalf("%s: стека %q в таблице нет", c.name, c.stack)
		}
		out, err := renderStack(t, chain, c.sets...)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: законный близнец отвергнут рендером: %v\n%s", c.name, err, lastLines(out, 5))
		case c.want != "" && err == nil:
			t.Errorf("%s: рендер прошёл, а страж обязан был отказать (ждали %q)", c.name, c.want)
		case c.want != "" && !strings.Contains(out, c.want):
			t.Errorf("%s: рендер отказал, но не тем текстом (ждали %q):\n%s", c.name, c.want, lastLines(out, 5))
		default:
			t.Logf("%s: исход как ждали", c.name)
		}
	}
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}
