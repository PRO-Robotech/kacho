// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_domainless_landing_injection_test.go — гейт выразимости посадки без
// доменного имени СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Гейт, доказанный только зелёным деревом, доказан не был: он остаётся зелёным
// и когда перестаёт читать. Здесь дефект ВОЗВРАЩАЕТСЯ настоящим входом — тем же
// чартом, той же цепочкой профилей, — и рядом ставится ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы, на котором гейт обязан молчать.
//
// Зовутся ТЕ ЖЕ функции вердикта, что исполняет гейт (`sessionCookieDomain`,
// `webauthnEnabled`, `landingDeclarationVerdict`), а не их копии: копия
// разошлась бы с оригиналом молча и доказывала бы саму себя.
//
// Осей четыре, у каждой обе стороны:
//
//	(а) ключ печенья:   дефект возвращён ⇒ ключ есть   · законный близнец ⇒ ключ есть по праву
//	(б) ключи доступа:  дефект возвращён ⇒ полоса включена · без имени ⇒ выключена
//	(в) отказ рендера:  два правила об одном предмете ⇒ ОТКАЗ · очищено ⇒ рендер проходит
//	(г) согласие с фактом: IP без объявления и объявление без IP ⇒ находка · оба ровных ⇒ молчание
package deploy_test

import (
	"strings"
	"testing"
)

// devChain / a8f60dChain — настоящие цепочки профилей, читаемые из таблицы
// стендов: инъекция обязана идти по тому же входу, что и гейт.
func chainOf(t *testing.T, stack string) []string {
	t.Helper()
	stacks := deployStacks(t)
	chain, ok := stacks[stack]
	if !ok {
		t.Fatalf("стека %q в таблице нет — инъекция потеряла вход, а не предмет", stack)
	}
	return chain
}

// TestIdentityDomainlessGate_ProvenByInjection — оси (а) и (б).
func TestIdentityDomainlessGate_ProvenByInjection(t *testing.T) {
	var found, silent int

	// ── (а)+(б) ДЕФЕКТ ВОЗВРАЩЁН: у посадки на голом IP снято объявление ────
	// Ровно то состояние, из которого выведена задача: профиль называет
	// IP-origin, а шаблон печатает `domain` безусловно.
	t.Run("дефект возвращён: IP-посадка без объявления", func(t *testing.T) {
		out, err := renderIdentitySubchart(t, chainOf(t, "a8f60d"),
			"global.kacho.identity.domainless=false")
		if err != nil {
			t.Fatalf("рендер отказал: %v\n%s", err, out)
		}
		cfg, _ := identityConfigOf(t, out)

		value, present := sessionCookieDomain(cfg)
		if !present {
			t.Fatal("ключ session.cookie.domain НЕ появился при снятом объявлении — " +
				"дискриминатор гейта не различает состояния, ради которых заведён")
		}
		if strings.TrimSpace(value) == "" {
			t.Fatalf("ключ появился пустым (%q) — дефект воспроизведён не тот", value)
		}
		waOn, rpID, _ := webauthnEnabled(cfg)
		if !waOn || rpID == "" {
			t.Fatalf("полоса ключей доступа не вернулась (включена=%v, rp.id=%q) — "+
				"вторая ось дефекта не воспроизведена", waOn, rpID)
		}
		if v := landingDeclarationVerdict("46.173.29.131", false); v != landingIPNotDeclared {
			t.Fatalf("адъюдикатор согласия сказал %q, ждали %q", v, landingIPNotDeclared)
		}
		found++
		t.Logf("дефект воспроизведён: domain=%q · webauthn включена, rp.id=%q", value, rpID)
	})

	// ── (а)+(б) ЗАКОННЫЙ БЛИЗНЕЦ: посадка С доменным именем ────────────────
	// Та же форма входа, тот же рендер — гейт обязан молчать, иначе он ловит
	// форму, а не существо, и первый же ложный срабат его отключит.
	t.Run("законный близнец: посадка с доменным именем", func(t *testing.T) {
		out, err := renderIdentitySubchart(t, chainOf(t, "dev"))
		if err != nil {
			t.Fatalf("рендер отказал: %v\n%s", err, out)
		}
		cfg, _ := identityConfigOf(t, out)

		value, present := sessionCookieDomain(cfg)
		if !present || strings.TrimSpace(value) == "" {
			t.Fatalf("на посадке с именем ключ session.cookie.domain обязан быть "+
				"непустым (есть=%v, величина=%q)", present, value)
		}
		waOn, rpID, _ := webauthnEnabled(cfg)
		if !waOn || rpID == "" {
			t.Fatalf("на посадке с именем полоса ключей доступа обязана остаться "+
				"(включена=%v, rp.id=%q)", waOn, rpID)
		}
		if v := landingDeclarationVerdict(externalOriginHost(t, identityOfStack(t, chainOf(t, "dev"))), false); v != landingAgrees {
			t.Fatalf("адъюдикатор согласия сказал %q на исправной посадке, ждали %q", v, landingAgrees)
		}
		silent++
		t.Logf("законный близнец молчит: domain=%q · webauthn включена, rp.id=%q", value, rpID)
	})

	// ── (б) обратная сторона: объявленная посадка полосу СНИМАЕТ ────────────
	t.Run("объявленная посадка без имени: полоса снята, ключа нет", func(t *testing.T) {
		out, err := renderIdentitySubchart(t, chainOf(t, "a8f60d"))
		if err != nil {
			t.Fatalf("рендер отказал: %v\n%s", err, out)
		}
		cfg, body := identityConfigOf(t, out)
		if value, present := sessionCookieDomain(cfg); present {
			t.Fatalf("ключ session.cookie.domain напечатан (%q) на посадке без имени", value)
		}
		if waOn, rpID, declared := webauthnEnabled(cfg); waOn || rpID != "" {
			t.Fatalf("полоса ключей доступа не снята (включена=%v, rp.id=%q, объявлена=%v)", waOn, rpID, declared)
		}
		for _, dead := range []string{"kacho.local", "console.kacho.local", "app.api.kacho.cloud"} {
			if strings.Contains(body, dead) {
				t.Errorf("в настройках посадки остался чужой адрес %q — профиль "+
					"наследует посадку, о которой молчит", dead)
			}
		}
		silent++
		t.Logf("посадка без имени: ключа нет, полоса снята, чужих адресов нет (байт тела %d)", len(body))
	})

	t.Logf("перепись инъекции (оси а/б): находок %d · законных близнецов %d", found, silent)
	if found == 0 || silent == 0 {
		t.Fatal("инъекция односторонняя — доказательства нет: гейт, у которого не " +
			"проверена одна из сторон, отличает не то, что заявляет")
	}
}

// TestIdentityDomainlessGate_RefusesTwoRulesAboutOneSubject — ось (в).
//
// Объявив посадку без доменного имени, профиль обязан ОЧИСТИТЬ
// `cookieDomain`/`webauthnRpId`, если их объявил слой под ним. Принять и молча
// выбросить — запрещённый исход: оператор видит объявленную величину и уверен,
// что она применена.
func TestIdentityDomainlessGate_RefusesTwoRulesAboutOneSubject(t *testing.T) {
	dev := chainOf(t, "dev") // объявляет cookieDomain и webauthnRpId = kacho.local

	// ДЕФЕКТ: посадка объявлена без имени поверх слоя, который имя объявил.
	out, err := renderIdentitySubchart(t, dev, "global.kacho.identity.domainless=true")
	if err == nil {
		t.Fatal("рендер ПРОШЁЛ при двух правилах об одном предмете — объявленная " +
			"величина была бы принята и молча выброшена")
	}
	for _, must := range []string{"cookieDomain", "webauthnRpId", "domainless"} {
		if !strings.Contains(out, must) {
			t.Errorf("текст отказа не называет %q — оператору нечего править:\n%s", must, out)
		}
	}
	t.Logf("отказ получен и называет обе ручки (байт сообщения %d)", len(out))

	// ЗАКОННЫЙ БЛИЗНЕЦ: тот же вход с ОЧИЩЕННЫМИ ручками обязан пройти —
	// иначе отказ ловит форму, а не существо, и выразить посадку по-прежнему
	// нечем.
	out, err = renderIdentitySubchart(t, dev,
		"global.kacho.identity.domainless=true",
		"global.kacho.identity.cookieDomain=",
		"global.kacho.identity.webauthnRpId=")
	if err != nil {
		t.Fatalf("рендер ОТКАЗАЛ на очищенных ручках — ложное срабатывание, посадка "+
			"по-прежнему невыразима: %v\n%s", err, out)
	}
	cfg, _ := identityConfigOf(t, out)
	if value, present := sessionCookieDomain(cfg); present {
		t.Fatalf("ключ session.cookie.domain напечатан (%q) при очищенных ручках", value)
	}
	t.Log("законный близнец рендерится: ключа нет, ложного отказа нет")
}

// TestIdentityLandingVerdict_ProvenByInjection — ось (г) на синтетическом входе.
//
// Разбор согласия объявления с фактом — чистая функция; её обе стороны дешевле
// и полнее доказывать перечнем, чем рендерами. Зовётся ТА ЖЕ функция.
func TestIdentityLandingVerdict_ProvenByInjection(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		declared bool
		want     string
	}{
		{"IPv4-посадка без объявления — находка", "46.173.29.131", false, landingIPNotDeclared},
		{"IPv6-посадка без объявления — тоже находка", "2001:db8::1", false, landingIPNotDeclared},
		{"объявление на доменном имени — находка с другой стороны", "app.api.kacho.cloud", true, landingDeclaredNoIP},
		{"IP-посадка объявлена — молчит", "46.173.29.131", true, landingAgrees},
		{"доменное имя не объявлено — молчит", "console.kacho.local", false, landingAgrees},
		{"имя, ПОХОЖЕЕ на адрес, доменом и остаётся — молчит", "10.20.30.example", false, landingAgrees},
	}
	var found, silent int
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := landingDeclarationVerdict(c.host, c.declared)
			if got != c.want {
				t.Fatalf("вердикт %q, ожидался %q (хост %q, объявлено %v)", got, c.want, c.host, c.declared)
			}
			if got == landingAgrees {
				silent++
			} else {
				found++
			}
		})
	}
	t.Logf("перепись инъекции (ось г): случаев %d · находок %d · законных близнецов %d",
		len(cases), found, silent)
	if found == 0 || silent == 0 {
		t.Fatal("инъекция односторонняя — адъюдикатор не доказан")
	}
}
