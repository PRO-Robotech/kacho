// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_anonymous_registry_lane_test.go — анонимный поток докерной полосы в
// копии чарта службы доступа рендерится только целиком (kacho#3330; kaname#928,
// kaname#930).
//
// Оси, каждое отрицание — в паре с положительным близнецом:
//
//	(а) поток включён со своими условиями — в карте настроек все три ручки:
//	    принципал, темп на источник, доверенный пересыльщик — ровно те, что
//	    объявлены;
//	(б) поток выключен (умолчание) — ни одной;
//	(в) поток включён без TLS слушателя выдачи, без режима optional-mutual,
//	    без темпа или без пересыльщика — отказ рендера, называющий недостающее.
//	    Недостающее — это ОПУЩЕННЫЙ ключ, а не нуль в нём: профиль, который
//	    ручку не называет, проходит путь умолчаний копии, и только он показывает,
//	    что умолчание не подставило за профиль «разумное»;
//	(г) у копии нет умолчаний для ручек под отказом рендера: темп — 0,
//	    пересыльщик — пусто (декларативно, по объявлениям значений);
//	(д) на КАЖДОЙ цепочке, где поток включён, пересыльщик есть ровно URI SAN
//	    клиентского листа, который выписывает чарт реестра той же цепочки.
package umbrella_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// anonLaneForwarder — URI SAN клиентского листа реестра при умолчаниях его чарта;
// у копии службы доступа умолчания нет, и профиль называет его сам.
const anonLaneForwarder = "spiffe://kacho.cloud/ns/kacho/sa/kacho-registry"

// Ключи двух ручек под отказом рендера — по одному написанию на пробы.
const (
	anonPaceKey      = "config.apiServer.registryToken.anonymousPerSourcePerSec"
	anonForwarderKey = "config.apiServer.registryToken.anonymousForwarderSan"
)

// anonLaneOn — поток со всеми условиями. Темп 7 — число, которого нет ни в
// одном умолчании: карта с 7 доказывает, что темп пришёл из объявления.
var anonLaneOn = []string{
	"config.apiServer.registryToken.anonymousClientId=registry-anonymous",
	anonPaceKey + "=7",
	anonForwarderKey + "=" + anonLaneForwarder,
	"mtls.enable=true",
	"mtls.registryToken=true",
	"mtls.registryTokenClientAuthMode=optional-mutual",
}

// registryTokenKeys — ключи секции `registry-token:` карты настроек.
func registryTokenKeys(t *testing.T, rendered string) map[string]string {
	t.Helper()
	i := strings.Index(rendered, "\n      registry-token:\n")
	out := map[string]string{}
	if i < 0 {
		return out
	}
	kv := regexp.MustCompile(`^        ([a-z-]+):\s*"?([^"\n]*)"?$`)
	for _, line := range strings.Split(rendered[i+len("\n      registry-token:\n"):], "\n") {
		m := kv.FindStringSubmatch(line)
		if m == nil {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			break
		}
		out[m[1]] = m[2]
	}
	return out
}

func TestKanameAnonymousRegistryLane_OnRendersAllThreeKnobs(t *testing.T) {
	out, err := renderIAM(t, anonLaneOn...)
	if err != nil {
		t.Fatalf("законный включённый поток отвергнут рендером: %v\n%s", err, out)
	}
	got := registryTokenKeys(t, out)
	want := map[string]string{
		"anonymous-client-id":          "registry-anonymous",
		"anonymous-per-source-per-sec": "7",
		"anonymous-forwarder-san":      anonLaneForwarder,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ручка %s = %q, ожидалось %q (секция registry-token: %v)", k, got[k], v, got)
		}
	}
}

func TestKanameAnonymousRegistryLane_OffRendersNone(t *testing.T) {
	out, err := renderIAM(t)
	if err != nil {
		t.Fatalf("умолчания копии не рендерятся: %v\n%s", err, out)
	}
	if strings.Contains(out, "anonymous-") {
		t.Fatalf("поток выключен, а ручка потока отрендерена:\n%s", out)
	}
}

func TestKanameAnonymousRegistryLane_RefusesWithoutItsConditions(t *testing.T) {
	// without — anonLaneOn без ключа drop (ключ ОПУЩЕН, а не обнулён) и с extra.
	without := func(drop string, extra ...string) []string {
		var s []string
		for _, v := range anonLaneOn {
			if !strings.HasPrefix(v, drop+"=") {
				s = append(s, v)
			}
		}
		return append(s, extra...)
	}
	for _, tc := range []struct {
		name string
		sets []string
		says string
	}{
		{"слушатель без TLS", without("mtls.registryToken"), "kaname.mtls.registryToken=true"},
		{"mtls выключен", without("mtls.enable"), "kaname.mtls.enable=true"},
		{"слушатель не спрашивает сертификат", without("mtls.registryTokenClientAuthMode"),
			"registryTokenClientAuthMode=optional-mutual"},
		{"слушатель требует сертификат у всех", without("mtls.registryTokenClientAuthMode",
			"mtls.registryTokenClientAuthMode=mutual"), "registryTokenClientAuthMode=optional-mutual"},
		{"темп не назван", without(anonPaceKey), "anonymousPerSourcePerSec"},
		{"темп назван нулём", without(anonPaceKey, anonPaceKey+"=0"), "anonymousPerSourcePerSec"},
		{"пересыльщик не назван", without(anonForwarderKey), "anonymousForwarderSan"},
		{"пересыльщик назван пустым", without(anonForwarderKey, anonForwarderKey+"="), "anonymousForwarderSan"},
	} {
		out, err := renderIAM(t, tc.sets...)
		if err == nil {
			t.Errorf("%s: рендер принял поток без его условия", tc.name)
			continue
		}
		if !strings.Contains(out, tc.says) {
			t.Errorf("%s: отказ не называет %q:\n%s", tc.name, tc.says, out)
		}
	}
}

// anonLaneGuardedKnobs — ручки копии, которые судит отказ рендера
// `kaname.requireAnonymousRegistryLane` по «не задано»: ненулевое умолчание
// любой из них подставило бы значение за профиль и убило бы отказ.
var anonLaneGuardedKnobs = [][]string{
	{"config", "apiServer", "registryToken", "anonymousPerSourcePerSec"},
	{"config", "apiServer", "registryToken", "anonymousForwarderSan"},
}

// judgeGuardedKnobDefaults — находки по значениям копии: ручка под отказом
// рендера несёт непустое умолчание. Отсутствующий путь — тоже находка: ручку,
// которой нет в значениях, проба не вправе считать «без умолчания».
func judgeGuardedKnobDefaults(values map[string]any) []string {
	var findings []string
	for _, path := range anonLaneGuardedKnobs {
		v, ok := lookup(values, path...)
		name := strings.Join(path, ".")
		switch {
		case !ok:
			findings = append(findings, fmt.Sprintf("%s в значениях копии не объявлена — предпосылка пробы исчезла", name))
		case v == nil, v == "", v == 0, v == 0.0:
		default:
			findings = append(findings, fmt.Sprintf("%s = %v — умолчание у ручки под отказом рендера: "+
				"профиль, не назвавший её, получит это число без отказа", name, v))
		}
	}
	return findings
}

// TestKanameAnonymousRegistryLane_GuardedKnobsHaveNoDefault — (г), по объявлению:
// исполняется без helm, поэтому не пропускается там, где рендер-проба пропущена.
func TestKanameAnonymousRegistryLane_GuardedKnobsHaveNoDefault(t *testing.T) {
	values := loadTree(t, filepath.Join(iamChartDir, "values.yaml"))
	for _, f := range judgeGuardedKnobDefaults(values) {
		t.Error(f)
	}
	t.Logf("осмотрено ручек под отказом рендера: %d", len(anonLaneGuardedKnobs))
}

// TestKanameAnonymousRegistryLane_GuardedKnobJudgeCanFail — близнецы судьи (г):
// ненулевой темп, непустой пересыльщик и снятый ключ — находки; 0 и пусто —
// молчание.
func TestKanameAnonymousRegistryLane_GuardedKnobJudgeCanFail(t *testing.T) {
	tree := func(pace any, san any) map[string]any {
		rt := map[string]any{}
		if pace != nil {
			rt["anonymousPerSourcePerSec"] = pace
		}
		if san != nil {
			rt["anonymousForwarderSan"] = san
		}
		return map[string]any{"config": map[string]any{"apiServer": map[string]any{"registryToken": rt}}}
	}
	if f := judgeGuardedKnobDefaults(tree(0, "")); len(f) != 0 {
		t.Errorf("законные «не задано» названы находкой: %v", f)
	}
	for name, tr := range map[string]map[string]any{
		"темп 10":           tree(10, ""),
		"пересыльщик задан": tree(0, anonLaneForwarder),
		"темп снят":         tree(nil, ""),
	} {
		if f := judgeGuardedKnobDefaults(tr); len(f) != 1 {
			t.Errorf("%s: ожидалась одна находка, получено %v", name, f)
		}
	}
}

// ── (д) пересыльщик и лист реестра — на каждой цепочке с потоком ───────────

// forwarderChain — сложенные значения зонтика одной цепочки: умолчания чарта
// копии под `kaname`, умолчания чарта реестра под `registry`, values.yaml
// зонтика и слои цепочки по порядку — так, как их складывает helm.
type forwarderChain struct {
	name   string
	merged map[string]any
}

func anonStr(tree map[string]any, path ...string) string {
	v, _ := lookup(tree, path...)
	s, _ := v.(string)
	return s
}

// judgeForwarderSan — находки цепочки; judged=false — поток на ней выключен.
// Лист реестра выводится тем же правилом, что certificate.yaml его чарта:
// домен — `registry.trustDomain` (умолчание kacho.cloud), пространство —
// `mtls.spiffe.namespace` (пусто — пространство релиза), учётка — `saName`
// (умолчание kacho-registry). Пространство релиза пересыльщику-литералу не
// сверить: такой лист — находка, а не догадка.
func judgeForwarderSan(c forwarderChain) (judged bool, findings []string) {
	if anonStr(c.merged, "kaname", "config", "apiServer", "registryToken", "anonymousClientId") == "" {
		return false, nil
	}
	san := anonStr(c.merged, "kaname", "config", "apiServer", "registryToken", "anonymousForwarderSan")
	or := func(s, d string) string {
		if s == "" {
			return d
		}
		return s
	}
	trust := or(anonStr(c.merged, "registry", "mtls", "spiffe", "trustDomain"), "kacho.cloud")
	ns := anonStr(c.merged, "registry", "mtls", "spiffe", "namespace")
	sa := or(anonStr(c.merged, "registry", "mtls", "spiffe", "saName"), "kacho-registry")
	kanTrust := or(anonStr(c.merged, "kaname", "mtls", "spiffe", "trustDomain"), "kacho.cloud")
	if ns == "" {
		return true, []string{fmt.Sprintf("цепочка %s: поток включён, а пространство листа реестра "+
			"(registry.mtls.spiffe.namespace) не закреплено — лист берёт пространство релиза, "+
			"пересыльщик %q с ним не сверяется", c.name, san)}
	}
	if leaf := "spiffe://" + trust + "/ns/" + ns + "/sa/" + sa; san != leaf {
		findings = append(findings, fmt.Sprintf("цепочка %s: пересыльщик %q ≠ URI SAN клиентского "+
			"листа реестра %q — служба его не узнает, все клиенты за сайдкаром делят один темп",
			c.name, san, leaf))
	}
	if !strings.HasPrefix(san, "spiffe://"+kanTrust+"/") {
		findings = append(findings, fmt.Sprintf("цепочка %s: пересыльщик %q вне домена доверия "+
			"службы доступа %q — служба его отвергнет", c.name, san, kanTrust))
	}
	return true, findings
}

// standProbeOverlay — накладка стенда сквозной пробы, записанная её скриптом.
func standProbeOverlay(t *testing.T) map[string]any {
	t.Helper()
	p := filepath.Join(t.TempDir(), "overlay.yaml")
	probe := filepath.Join("..", "..", "scripts", "stand-registry-forwarder-probe.sh")
	if out, err := exec.Command("bash", probe, "overlay", p).CombinedOutput(); err != nil { // #nosec G204 -- фиксированный скрипт дерева
		t.Fatalf("накладка стенда не записана: %v\n%s", err, out)
	}
	return loadTree(t, p)
}

// forwarderChains — каждая цепочка таблицы стеков и она же под накладкой стенда
// пробы (`STAND_VALUES` кладёт накладку поверх любой цепочки).
func forwarderChains(t *testing.T) []forwarderChain {
	t.Helper()
	base := mergeInto(nil, map[string]any{
		"kaname":   loadTree(t, filepath.Join(iamChartDir, "values.yaml")),
		"registry": loadTree(t, filepath.Join("..", "..", "..", "services", "registry", "deploy", "values.yaml")),
	})
	base = mergeInto(base, umbrellaValues(t, "values.yaml"))
	overlay := standProbeOverlay(t)
	stacks := deployableStacks(t)
	var out []forwarderChain
	for _, name := range sortedStackNames(stacks) {
		merged := mergeInto(nil, base)
		for _, p := range stacks[name] {
			merged = mergeInto(merged, umbrellaValues(t, p))
		}
		out = append(out,
			forwarderChain{name, merged},
			forwarderChain{name + "+накладка пробы", mergeInto(mergeInto(nil, merged), overlay)})
	}
	return out
}

// TestKanameAnonymousForwarderIsTheRegistryClientLeafOnEveryChain — (д).
// Пересыльщик называется в профиле литералом, лист выводится чартом реестра;
// сравнение у службы точное, поэтому согласие судится на каждой цепочке, где
// поток включён. Ноль таких цепочек — отказ: накладка стенда включает поток
// всегда, и её исчезновение — пропажа предпосылки, а не чистое дерево.
func TestKanameAnonymousForwarderIsTheRegistryClientLeafOnEveryChain(t *testing.T) {
	chains := forwarderChains(t)
	var judged, off int
	for _, c := range chains {
		ok, findings := judgeForwarderSan(c)
		if !ok {
			off++
			continue
		}
		judged++
		for _, f := range findings {
			t.Error(f)
		}
	}
	t.Logf("цепочек %d: с потоком %d, без потока %d", len(chains), judged, off)
	if judged == 0 {
		t.Fatalf("ни одной цепочки с потоком — проба не вправе заключить, что пересыльщик сходится")
	}
}

// TestKanameAnonymousForwarderJudgeCanFail — близнецы судьи (д) на настоящей
// цепочке под накладкой: каждая инъекция меняет ровно один факт.
func TestKanameAnonymousForwarderJudgeCanFail(t *testing.T) {
	var agreeing forwarderChain
	for _, c := range forwarderChains(t) {
		if strings.HasSuffix(c.name, "+накладка пробы") {
			agreeing = c
			break
		}
	}
	if agreeing.merged == nil {
		t.Fatal("цепочки под накладкой нет — близнецам не на чем стоять")
	}
	inject := func(val string, path ...string) forwarderChain {
		m := mergeInto(nil, agreeing.merged)
		setPath(m, val, path...)
		return forwarderChain{agreeing.name + " (инъекция " + strings.Join(path, ".") + ")", m}
	}
	if ok, f := judgeForwarderSan(agreeing); !ok || len(f) != 0 {
		t.Fatalf("согласная цепочка %s: судима=%v, находки %v", agreeing.name, ok, f)
	}
	for _, c := range []forwarderChain{
		inject("kacho-registry-2", "registry", "mtls", "spiffe", "saName"),
		inject("kacho-other", "registry", "mtls", "spiffe", "namespace"),
		inject("", "registry", "mtls", "spiffe", "namespace"),
		inject("other.cloud", "kaname", "mtls", "spiffe", "trustDomain"),
	} {
		if ok, f := judgeForwarderSan(c); !ok || len(f) != 1 || !strings.Contains(f[0], c.name) {
			t.Errorf("%s: ожидалась одна находка с именем цепочки, судима=%v, находки %v", c.name, ok, f)
		}
	}
	off := inject("", "kaname", "config", "apiServer", "registryToken", "anonymousClientId")
	setPath(off.merged, "spiffe://kacho.cloud/ns/other/sa/x", "kaname", "config", "apiServer", "registryToken", "anonymousForwarderSan")
	if ok, f := judgeForwarderSan(off); ok || len(f) != 0 {
		t.Errorf("поток выключен — судить нечего, а судима=%v, находки %v", ok, f)
	}
}
