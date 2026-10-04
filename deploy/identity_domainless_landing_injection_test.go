// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_domainless_landing_injection_test.go — гейт выразимости посадки без
// доменного имени СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Вход — настоящая цепочка стенда a8f60d из таблицы стендов, отрендеренная
// подчартом службы, с ОДНИМ подменённым фактом: внешний origin консоли стоит на
// IP-литерале (адрес из документационного диапазона RFC 5737). Посадки на
// IP-литерале в дереве больше нет: a8f60d получил имя `console.in-cloud.io`
// (kacho#3024), и прежний вход «дерево как есть» перестал быть IP-посадкой.
// Подмена — ровно узел `global.kacho.identity`, тот же, что объявлял её в
// профиле; настройки службы (печенье host-only, пустой перечень происхождений)
// приходят из дерева без изменений. Рядом ставится ЗАКОННЫЙ БЛИЗНЕЦ той же
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

// ipLandingOrigin — внешний origin посадки без доменного имени для инъекции:
// IP-литерал документационного диапазона (RFC 5737), а не адрес чьей-то площадки.
const ipLandingOrigin = "http://192.0.2.10"

// renderedOwnConfig — тело настроек службы по цепочке стека плюс установки поверх.
// identity — подмена узла `global.kacho.identity` поверх цепочки (nil — без
// подмены); хост внешнего origin выводится из узла ПОСЛЕ подмены, тем же
// выводом, что у шаблона.
func renderedOwnConfig(t *testing.T, stack string, identity map[string]any, sets ...string) (string, map[string]any) {
	t.Helper()
	chain := chainOf(t, stack)
	values := stackIdentityValues(t, chain)
	id := identityOfStack(t, chain)
	if identity != nil {
		over := map[string]any{"global": map[string]any{"kacho": map[string]any{"identity": identity}}}
		values = mergeValues(values, over)
		id = mergeValues(mergeValues(map[string]any{}, id), identity)
	}
	out, err := renderStackSubchart(t, stack, values, sets...)
	if err != nil {
		t.Fatalf("стек %s: рендер подчарта службы отказал: %v\n%s", stack, err, out)
	}
	return externalOriginHost(t, id), kanameServiceConfig(t, out)
}

// ipLanding — подмена, переводящая цепочку на посадку без доменного имени:
// origin на IP-литерале, имя доверяющей стороны снято (у IP-литерала его нет).
func ipLanding() map[string]any {
	return map[string]any{"appBaseURL": ipLandingOrigin, "domainless": true, "webauthnRpId": ""}
}

func TestIdentityDomainlessGate_ProvenByInjection(t *testing.T) {
	host, twin := renderedOwnConfig(t, "a8f60d", ipLanding())
	isIP, f := judgeOurDomainlessLanding("a8f60d", host, twin)
	if !isIP {
		t.Fatalf("вход инъекции сменил форму: хост внешнего origin после подмены — %q, не IP-литерал", host)
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
		_, cfg := renderedOwnConfig(t, "a8f60d", ipLanding(), c.sets...)
		_, f := judgeOurDomainlessLanding("a8f60d", host, cfg)
		if len(f) != 1 || !strings.Contains(f[0], c.want) {
			t.Errorf("%s: ожидалась одна находка с %q, получено %v", c.name, c.want, f)
		}
	}
}

// TestIdentityDomainlessGate_TheSameChainWithItsNameIsNotJudged — вторая
// половина пары на ТОЙ ЖЕ цепочке: дерево как есть (origin с доменным именем)
// гейтом не судится, а подмена одного узла личности переводит её под суд. Это и
// есть утверждение о РАЗЛИЧИИ посадок, которое прежде держала перепись дерева,
// пока в нём была IP-посадка.
func TestIdentityDomainlessGate_TheSameChainWithItsNameIsNotJudged(t *testing.T) {
	host, cfg := renderedOwnConfig(t, "a8f60d", nil, "config.authn.login.cookieDomain=api.kacho.cloud")
	isIP, f := judgeOurDomainlessLanding("a8f60d", host, cfg)
	if isIP || len(f) != 0 {
		t.Fatalf("a8f60d как в дереве (%s) — посадка с доменным именем и здесь не судится; "+
			"получено isIP=%v, находки %v", host, isIP, f)
	}
	ipHost, ipCfg := renderedOwnConfig(t, "a8f60d", ipLanding(), "config.authn.login.cookieDomain=api.kacho.cloud")
	if isIP, f := judgeOurDomainlessLanding("a8f60d", ipHost, ipCfg); !isIP || len(f) != 1 {
		t.Fatalf("та же цепочка на IP-литерале обязана судиться и дать одну находку: isIP=%v, находки %v", isIP, f)
	}
}

func TestIdentityDomainlessGate_DomainLandingIsNotJudgedHere(t *testing.T) {
	host, cfg := renderedOwnConfig(t, "prod", nil, "config.authn.login.cookieDomain=api.kacho.cloud")
	isIP, f := judgeOurDomainlessLanding("prod", host, cfg)
	if isIP || len(f) != 0 {
		t.Fatalf("посадка с доменным именем (%s) здесь не судится — печенье на ней судит "+
			"own_session_cookie_domain_test.go; получено isIP=%v, находки %v", host, isIP, f)
	}
}
