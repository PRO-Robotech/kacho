// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_domainless_landing_is_expressible_test.go — посадка БЕЗ ДОМЕННОГО
// ИМЕНИ обязана быть ВЫРАЗИМОЙ нашей полосой входа, и объявленная — исполненной.
//
// # Предмет
//
// Консоль управляемого кластера a8f60d стоит на голом IP-литерале: доменного
// имени у площадки нет, внешнего TLS нет. Два стандарта делают такую посадку
// особой, и оба — факт о браузере, а не наш выбор:
//
//   - RFC 6265 §5.1.3 (domain matching) объявляет совпадение с IP-литералом
//     ЛОЖНЫМ всегда, а §5.3 п.6 отбрасывает печенье, чей `Domain` не совпал, —
//     значит печенье сессии с любым `Domain` браузер отбросит ЦЕЛИКОМ, и вход не
//     состоится молча. Печенье обязано быть host-only;
//   - WebAuthn L3 §5.1.3: RP ID обязан быть валидным доменным именем, у
//     IP-литерала его нет by construction, — значит ни одно происхождение на
//     IP-литерале не может нести церемонию ключа доступа.
//
// Прежде эту выразимость судили на настройках внешнего поставщика личности.
// Их подчарт больше не производит (kacho#2818), и носитель обоих свойств теперь
// НАШ: печенье сессии выдаёт полоса входа службы (`authn.login.cookie-domain`),
// привязку ключей доступа — её блок `authn.access-keys`.
//
// # Что утверждается
//
// По каждой цепочке deploy/stacks.txt: хост внешнего origin посадки (консоль —
// `appBaseURL`, а пусто — `<appSubdomain>.<domain>`, тот же вывод, что у
// шаблона) есть IP-литерал ⇒ в настройках службы `authn.login.cookie-domain`
// равен `none` (host-only) и ни одно происхождение `authn.access-keys.origins`
// не стоит на IP-литерале. Посадка с доменным именем здесь не судится: печенье
// на ней судит deploy/own_session_cookie_domain_test.go.
//
// Перепись обязана встретить ОБЕ посадки: односторонняя ничего не утверждает о
// различии. Способность упасть и смолчать — настоящим входом,
// identity_domainless_landing_injection_test.go.
package deploy_test

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const iamSubchartDir = umbrellaDir + "/charts/kaname"

// renderIdentitySubchart рендерит подчарт kaname с перечисленными файлами
// значений и точечными установками.
//
// Отсутствие helm в CI — жёсткий провал, а не пропуск: гейт, молча ставший
// инертным на джобе, гейтящей мёрж, гейтом не является. Та же дисциплина, что у
// helm/umbrella/iam_lane_service_aud_test.go.
func renderIdentitySubchart(t *testing.T, valueFiles []string, sets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	// Одиночный рендер подчарта — только обёрткой: помощник флага почты живёт в
	// чарте notify (renderKanameAlone, CX1-113).
	args := []string{"-n", "kacho"}
	for _, f := range valueFiles {
		args = append(args, "-f", filepath.Join(umbrellaDir, f))
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	return renderKanameAlone(t, "kacho-umbrella", iamSubchartDir, args...)
}

// identityOfStack — действующий узел `global.kacho.identity` стека: профили
// накладываются слева направо, ровно как их получает helm, поверх умолчаний
// чарта.
func identityOfStack(t *testing.T, chain []string) map[string]any {
	t.Helper()
	merged := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	node, ok := lookup(merged, "global", "kacho", "identity")
	if !ok {
		t.Fatalf("у стека %v нет узла global.kacho.identity — сверять нечего, и это отказ", chain)
	}
	id, _ := node.(map[string]any)
	return id
}

// externalOriginHost — хост внешнего origin посадки. Пустой `appBaseURL`
// означает «вывести из домена» — ровно то, что делает шаблон
// (`kaname.identity.consoleOrigin`).
func externalOriginHost(t *testing.T, id map[string]any) string {
	t.Helper()
	raw, _ := id["appBaseURL"].(string)
	if strings.TrimSpace(raw) == "" {
		sub, _ := id["appSubdomain"].(string)
		dom, _ := id["domain"].(string)
		return sub + "." + dom
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("appBaseURL %q не разбирается как адрес: %v", raw, err)
	}
	return u.Hostname()
}

// judgeOurDomainlessLanding — ЕДИНСТВЕННЫЙ адъюдикатор: зовётся и переписью по
// дереву, и инъекцией. Вход — хост внешнего origin и тело настроек службы.
func judgeOurDomainlessLanding(stack, host string, cfg map[string]any) (domainless bool, findings []string) {
	if net.ParseIP(host) == nil {
		return false, nil
	}
	login, _ := configSection(cfg, "authn", "login")
	if got := fmt.Sprint(login["cookie-domain"]); got != ownCookieDomainWanted {
		findings = append(findings, fmt.Sprintf("стек %s: внешний origin — IP-литерал %s, а "+
			"`authn.login.cookie-domain` = %q, не %q. Печенье сессии с `Domain` браузер на "+
			"IP-посадке отбросит целиком (RFC 6265 §5.1.3), и вход не состоится молча",
			stack, host, got, ownCookieDomainWanted))
	}
	keys, _ := configSection(cfg, "authn", "access-keys")
	origins, _ := keys["origins"].([]any)
	for _, o := range origins {
		u, err := url.Parse(fmt.Sprint(o))
		if err != nil || net.ParseIP(u.Hostname()) != nil {
			findings = append(findings, fmt.Sprintf("стек %s: происхождение ключей доступа %q "+
				"стоит на IP-литерале — RP ID обязан быть доменным именем (WebAuthn L3 §5.1.3), "+
				"и церемония с этого происхождения отвергается на каждой попытке", stack, o))
		}
	}
	return true, findings
}

func TestIdentity_LandingWithoutADomainNameIsExpressible(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var domainless, withDomain int
	for _, name := range names {
		host := externalOriginHost(t, identityOfStack(t, stacks[name]))
		out, err := renderStackSubchart(t, name, stackIdentityValues(t, stacks[name]))
		if err != nil {
			t.Fatalf("стек %q: рендер подчарта службы отказал: %v\n%s", name, err, out)
		}
		isIP, findings := judgeOurDomainlessLanding(name, host, kanameServiceConfig(t, out))
		for _, f := range findings {
			t.Error(f)
		}
		if isIP {
			domainless++
		} else {
			withDomain++
		}
	}
	t.Logf("перепись: стеков осмотрено %d · без доменного имени %d · с доменным именем %d",
		len(names), domainless, withDomain)
	if domainless == 0 || withDomain == 0 {
		t.Fatalf("перепись односторонняя (без имени %d, с именем %d) — утверждение о "+
			"РАЗЛИЧИИ посадок не проверено ни на одной паре", domainless, withDomain)
	}
}
