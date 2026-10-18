// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// session_cookie_domain_render_test.go — kacho#3174: на каждом стенде таблицы
// домен печенья сессии, с которым его выдаёт служба доступа, и домен, с которым
// его гасит край, равны.
//
// Сравниваются РАЗОБРАННЫЕ значения тем же кодом, что у процессов: у края —
// `config.ParseSessionCookieDomain` от переменной окружения его контейнера, у
// службы — её словарь (`none` — только на хост). Значение службы — полным путём:
// ConfigMap `kaname-config` → `config.yaml` → `authn.login.cookie-domain`;
// переменная `KANAME_AUTHN__LOGIN__COOKIE_DOMAIN` контейнера `kaname` её
// перекрывает. Стенды и отрисовка — общими помощниками пакета
// (ka1_issuer_agreement_render_test.go).
package deploy_test

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const kanameCookieDomainEnv = "KANAME_AUTHN__LOGIN__COOKIE_DOMAIN"

// serviceCookieDomain — домен печенья службы стенда в форме `Set-Cookie`
// (пусто — только на хост). raisesService=false — отрисовка службы не поднимает.
func serviceCookieDomain(docs []map[string]any) (domain string, raisesService bool, err error) {
	var cfgYAML, env string
	haveCM, haveEnv := false, false
	for _, d := range docs {
		meta := ka1Sub(d, "metadata")
		switch {
		case ka1Str(d, "kind") == "ConfigMap" && ka1Str(meta, "name") == "kaname-config":
			cfgYAML, haveCM = ka1Str(ka1Sub(d, "data"), "config.yaml"), true
		case ka1Str(d, "kind") == "Deployment" && ka1Str(meta, "name") == "kaname":
			raisesService = true
			pod := ka1Sub(ka1Sub(ka1Sub(d, "spec"), "template"), "spec")
			for _, c := range ka1Slice(pod, "containers") {
				cm, _ := c.(map[string]any)
				if ka1Str(cm, "name") != "kaname" {
					continue
				}
				for _, e := range ka1Slice(cm, "env") {
					em, _ := e.(map[string]any)
					if ka1Str(em, "name") == kanameCookieDomainEnv {
						env, haveEnv = ka1Str(em, "value"), true
					}
				}
			}
		}
	}
	if !raisesService {
		return "", false, nil
	}
	raw := env
	if !haveEnv {
		if !haveCM {
			return "", true, fmt.Errorf("ConfigMap kaname-config с ключом config.yaml не отрисована")
		}
		var tree map[string]any
		if err := yaml.Unmarshal([]byte(cfgYAML), &tree); err != nil {
			return "", true, fmt.Errorf("config.yaml карты kaname-config не разбирается: %v", err)
		}
		v, ok := ka1Sub(ka1Sub(tree, "authn"), "login")["cookie-domain"].(string)
		if !ok {
			return "", true, fmt.Errorf("пути authn.login.cookie-domain нет ни в config.yaml карты kaname-config, ни переменной %s", kanameCookieDomainEnv)
		}
		raw = v
	}
	raw = strings.TrimSpace(raw)
	if raw == config.SessionCookieDomainNone {
		return "", true, nil
	}
	return raw, true, nil
}

// cookieDomainDisagreement — расхождение домена гашения края с доменом выдачи
// службы на отрисованном стенде. compared=false — служба на стенде не поднята.
func cookieDomainDisagreement(docs []map[string]any) (finding string, compared bool, err error) {
	svc, raises, err := serviceCookieDomain(docs)
	if err != nil || !raises {
		return "", false, err
	}
	env, ok := edgeContainerEnv(docs)
	if !ok {
		return "", false, fmt.Errorf("служба поднята, а Deployment края не отрисован")
	}
	raw, declared := env[config.KnobSessionCookieDomain]
	edge, perr := config.ParseSessionCookieDomain(raw, declared)
	if perr != nil {
		return "", false, fmt.Errorf("край не стартует: %v", perr)
	}
	if edge != svc {
		return fmt.Sprintf("служба выдаёт печенье с доменом %q, край гасит с %q — браузер гашение "+
			"не сопоставит, отказ по отсечке останется стоящим", svc, edge), true, nil
	}
	return "", true, nil
}

func TestEdgeEndsTheCarrierUnderTheServiceCookieDomainOnEveryStand(t *testing.T) {
	stands := ka1Stacks(t)
	compared, skipped := 0, 0
	for name, chain := range stands {
		r := ka1Render(t, chain)
		if r.err != nil {
			t.Errorf("стенд %s: отрисовка отказала: %v\n%s", name, r.err, ka1LastLines(r.out, 6))
			continue
		}
		finding, ok, err := cookieDomainDisagreement(r.docs)
		switch {
		case err != nil:
			t.Errorf("стенд %s: %v", name, err)
		case !ok:
			skipped++
		case finding != "":
			compared++
			t.Errorf("стенд %s: %s", name, finding)
		default:
			compared++
		}
	}
	if compared == 0 {
		t.Fatalf("сравнено стендов 0 из %d — проба не исполнилась", len(stands))
	}
	t.Logf("перепись: стендов %d · сравнено %d · без службы %d", len(stands), compared, skipped)
}

// syntheticStand — отрисовка из двух документов: карта службы с доменом
// cmDomain (и переменной envDomain, если задана) и Deployment края с ручкой.
func syntheticStand(cmDomain, envDomain, edgeDomain string) []map[string]any {
	kanameEnv := []any{}
	if envDomain != "" {
		kanameEnv = append(kanameEnv, map[string]any{"name": kanameCookieDomainEnv, "value": envDomain})
	}
	deploy := func(name string, env []any) map[string]any {
		return map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": name},
			"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{"name": name, "env": env}}}}}}
	}
	return []map[string]any{
		{"kind": "ConfigMap", "metadata": map[string]any{"name": "kaname-config"},
			"data": map[string]any{"config.yaml": "authn:\n  login:\n    cookie-domain: " + cmDomain + "\n"}},
		deploy("kaname", kanameEnv),
		deploy("api-gateway", []any{map[string]any{"name": config.KnobSessionCookieDomain, "value": edgeDomain}}),
	}
}

// Инъекция в обе стороны на синтетике: расхождение находится, согласие молчит,
// переменная службы перекрывает карту.
func TestCookieDomainAgreementJudge_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name                 string
		cm, env, edge        string
		wantFinding, wantCmp bool
	}{
		{"служба с доменом, край без", "kacho.example", "", "none", true, true},
		{"оба только на хост", "none", "", "none", false, true},
		{"оба с доменом", "kacho.example", "", "kacho.example", false, true},
		{"переменная службы перекрывает карту", "none", "kacho.example", "none", true, true},
	}
	for _, tc := range cases {
		finding, cmp, err := cookieDomainDisagreement(syntheticStand(tc.cm, tc.env, tc.edge))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cmp != tc.wantCmp || (finding != "") != tc.wantFinding {
			t.Errorf("%s: сравнено=%v находка=%q, ожидалось сравнено=%v находка=%v", tc.name, cmp, finding, tc.wantCmp, tc.wantFinding)
		}
	}
}
