// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_anonymous_registry_lane_test.go — анонимный поток докерной полосы в
// копии чарта службы доступа рендерится только целиком (kacho#3330; kaname#928,
// kaname#930).
//
// Оси, каждое отрицание — в паре с положительным близнецом:
//
//	(а) поток включён со своими условиями — в карте настроек все три ручки:
//	    принципал, темп на источник, доверенный пересыльщик;
//	(б) поток выключен (умолчание) — ни одной, хотя темп в значениях
//	    объявлен (пересыльщика в значениях копии нет);
//	(в) поток включён без TLS слушателя выдачи, без режима optional-mutual,
//	    без темпа или без пересыльщика — отказ рендера, называющий недостающее;
//	(г) пересыльщик, который называет накладка стенда пробы, есть ровно URI SAN
//	    клиентского листа, который выписывает чарт реестра.
package umbrella_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// anonLaneForwarder — URI SAN клиентского листа реестра при умолчаниях его чарта;
// у копии службы доступа умолчания нет, и профиль называет его сам.
const anonLaneForwarder = "spiffe://kacho.cloud/ns/kacho/sa/kacho-registry"

var anonLaneOn = []string{
	"config.apiServer.registryToken.anonymousClientId=registry-anonymous",
	"config.apiServer.registryToken.anonymousForwarderSan=" + anonLaneForwarder,
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
		"anonymous-per-source-per-sec": "10",
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
		{"темп не объявлен", without("", "config.apiServer.registryToken.anonymousPerSourcePerSec=0"),
			"anonymousPerSourcePerSec"},
		{"пересыльщик не объявлен", without("", "config.apiServer.registryToken.anonymousForwarderSan="),
			"anonymousForwarderSan"},
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

// TestKanameAnonymousForwarderIsTheRegistryClientLeaf — (г): пересыльщик,
// которого называет накладка стенда сквозной пробы, совпадает с URI SAN,
// который выписывает чарт реестра (`spiffe://<домен>/ns/<пространство>/sa/<учётка>`
// из его `mtls.spiffe`), в домене доверия копии. Сравнение у службы точное:
// расхождение в одну метку — пересыльщик не узнаётся, и все клиенты делят один
// темп. Умолчания у копии нет, поэтому судится накладка, а не значения чарта.
func TestKanameAnonymousForwarderIsTheRegistryClientLeaf(t *testing.T) {
	read := func(p string) map[string]any {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("значения %s не читаются: %v", p, err)
		}
		var m map[string]any
		if err := yaml.Unmarshal(raw, &m); err != nil {
			t.Fatalf("значения %s не разбираются: %v", p, err)
		}
		return m
	}
	get := func(m map[string]any, path ...string) string {
		var cur any = m
		for _, k := range path {
			mm, _ := cur.(map[string]any)
			cur = mm[k]
		}
		s, _ := cur.(string)
		return s
	}
	overlayPath := filepath.Join(t.TempDir(), "overlay.yaml")
	probe := filepath.Join("..", "..", "scripts", "stand-registry-forwarder-probe.sh")
	if out, err := exec.Command("bash", probe, "overlay", overlayPath).CombinedOutput(); err != nil {
		t.Fatalf("накладка стенда не записана: %v\n%s", err, out)
	}
	overlay := read(overlayPath)
	kan := read(filepath.Join(iamChartDir, "values.yaml"))
	reg := read(filepath.Join("..", "..", "..", "services", "registry", "deploy", "values.yaml"))
	san := get(overlay, "kaname", "config", "apiServer", "registryToken", "anonymousForwarderSan")
	trust := get(kan, "mtls", "spiffe", "trustDomain")
	leaf := "spiffe://" + get(reg, "mtls", "spiffe", "trustDomain") + "/ns/" +
		get(reg, "mtls", "spiffe", "namespace") + "/sa/" + get(reg, "mtls", "spiffe", "saName")
	if san == "" || trust == "" || leaf == "spiffe:///ns//sa/" {
		t.Fatalf("предпосылка исчезла: пересыльщик %q, домен копии %q, лист реестра %q", san, trust, leaf)
	}
	if san != leaf {
		t.Errorf("пересыльщик накладки %q ≠ URI SAN клиентского листа реестра %q", san, leaf)
	}
	if !strings.HasPrefix(san, "spiffe://"+trust+"/") {
		t.Errorf("пересыльщик %q вне домена доверия копии %q — служба его отвергнет", san, trust)
	}
}
