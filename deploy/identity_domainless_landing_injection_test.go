// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_domainless_landing_injection_test.go — гейт выразимости посадки без
// доменного имени СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Дефект ВОЗВРАЩАЕТСЯ настоящим входом — рендером подчарта службы настоящей
// цепочкой стенда, переведённой фикстурой на IP-литерал
// (`domainlessFixtureStack`, `domainlessFixtureSet`), — и рядом ставится ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы, отличающийся одним фактом. Зовётся ТОТ ЖЕ адъюдикатор, что исполняет
// гейт (`judgeOurDomainlessLanding`), а не его копия.
package deploy_test

import (
	"strings"
	"testing"
)

// chainOf — настоящая цепочка профилей стека, читаемая из таблицы стендов:
// инъекция обязана идти по тому же входу, что и гейт.
func chainOf(t *testing.T, stack string) []string {
	t.Helper()
	stacks := deployStacks(t)
	chain, ok := stacks[stack]
	if !ok {
		t.Fatalf("стека %q в таблице нет — инъекция потеряла вход, а не предмет", stack)
	}
	return chain
}

// renderedOwnConfig — тело настроек службы по цепочке стека плюс установки поверх.
func renderedOwnConfig(t *testing.T, stack string, sets ...string) (string, map[string]any) {
	t.Helper()
	chain := chainOf(t, stack)
	out, err := renderStackSubchart(t, stack, stackIdentityValues(t, chain), sets...)
	if err != nil {
		t.Fatalf("стек %s: рендер подчарта службы отказал: %v\n%s", stack, err, out)
	}
	id := identityOfStack(t, chain)
	for _, s := range sets {
		if v, ok := strings.CutPrefix(s, "global.kacho.identity.appBaseURL="); ok {
			id["appBaseURL"] = v
		}
	}
	return externalOriginHost(t, id), kanameServiceConfig(t, out)
}

func TestIdentityDomainlessGate_ProvenByInjection(t *testing.T) {
	st := domainlessFixtureStack
	host, twin := renderedOwnConfig(t, st, domainlessFixtureSet)
	isIP, f := judgeOurDomainlessLanding(st, host, twin)
	if !isIP {
		t.Fatalf("вход инъекции сменил форму: хост внешнего origin фикстуры %s — %q, не IP-литерал", st, host)
	}
	if len(f) != 0 {
		t.Fatalf("законный близнец (дерево как есть) обязан молчать: %v", f)
	}
	for _, c := range []struct {
		name, want string
		sets       []string
	}{
		{"печенье с доменом", "cookie-domain", []string{"config.authn.login.cookieDomain=api.kacho.cloud"}},
		{"печенье на IP", "cookie-domain", []string{"config.authn.login.cookieDomain=" + host}},
		{"происхождение ключей на IP", "IP-литерале", []string{"config.authn.accessKeys.origins[0]=http://" + host}},
	} {
		_, cfg := renderedOwnConfig(t, st, append([]string{domainlessFixtureSet}, c.sets...)...)
		_, f := judgeOurDomainlessLanding(st, host, cfg)
		if len(f) != 1 || !strings.Contains(f[0], c.want) {
			t.Errorf("%s: ожидалась одна находка с %q, получено %v", c.name, c.want, f)
		}
	}
}

func TestIdentityDomainlessGate_DomainLandingIsNotJudgedHere(t *testing.T) {
	host, cfg := renderedOwnConfig(t, "prod", "config.authn.login.cookieDomain=api.kacho.cloud")
	isIP, f := judgeOurDomainlessLanding("prod", host, cfg)
	if isIP || len(f) != 0 {
		t.Fatalf("посадка с доменным именем (%s) здесь не судится — печенье на ней судит "+
			"own_session_cookie_domain_test.go; получено isIP=%v, находки %v", host, isIP, f)
	}
}
