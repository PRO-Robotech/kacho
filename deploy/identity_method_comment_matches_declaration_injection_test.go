// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_method_comment_matches_declaration_injection_test.go — ДОКАЗАТЕЛЬСТВО,
// что соседний гейт умеет и краснеть, и молчать.
//
// По каждой оси — обе стороны: возвращённый дефект обязан краснить с ИМЕНЕМ
// виновной полосы, законный близнец той же формы обязан молчать. Вход берётся
// НАСТОЯЩИЙ — тело шаблона из дерева, — а синтетика служит только там, где
// нужного состояния в дереве нет by construction.
//
// Отдельная ось — та, ради которой перечень отмечен: проза, называющая имена
// полос рядом со словами о состоянии, читаться НЕ должна. Без этой оси гейт
// краснел бы на собственном объяснении, и первый же ложный срабат его отключил
// бы.
package deploy_test

import (
	"strings"
	"testing"
)

// realClaimLines — отмеченный перечень, каким он стоит в дереве. Инъекции
// подменяют именно его.
const realClaimLines = "    #   ВЫКЛЮЧЕНЫ: link, oidc\n    #   ВКЛЮЧЕНЫ: code, profile"

func identityBodyForInjection(t *testing.T) string {
	t.Helper()
	body := readFileForTest(t, identityConfigTemplate)
	if !strings.Contains(body, realClaimLines) {
		t.Fatalf("отмеченный перечень в %s не найден в ожидаемой форме — все инъекции "+
			"ниже перестали бы что-либо доказывать", identityConfigTemplate)
	}
	return body
}

func TestIdentityMethodCommentInjection_ScanSeesTheRealDeclaration(t *testing.T) {
	// Контроль предпосылки: без него все утверждения ниже вакуумны.
	states, claims, comments := scanIdentityMethodsBlock(identityBodyForInjection(t))
	if len(states) < 4 {
		t.Fatalf("разбор увидел %d полос в %s — этого мало, чтобы утверждать что-либо",
			len(states), identityConfigTemplate)
	}
	if _, ok := states["code"]; !ok {
		t.Fatalf("разбор не увидел полосы восстановления доступа в %s — предпосылка исчезла",
			identityConfigTemplate)
	}
	if len(claims) == 0 {
		t.Fatalf("отмеченных строк перечня не разобрано ни одной (%s)", identityConfigTemplate)
	}
	if comments == 0 {
		t.Fatalf("комментариев блока не прочитано ни одного (%s)", identityConfigTemplate)
	}
	t.Logf("перепись: полос %d · отмеченных строк %d · комментариев %d",
		len(states), len(claims), comments)
}

func TestIdentityMethodCommentInjection_TheOriginalLieIsFound(t *testing.T) {
	body := identityBodyForInjection(t)

	// ДЕФЕКТ, ВОЗВРАЩЁННЫЙ В НАСТОЯЩИЙ ВХОД: перечень ровно того состава, что
	// стоял до починки (#1256) — четыре имени, все объявлены выключенными.
	broken := strings.Replace(body, realClaimLines,
		"    #   ВЫКЛЮЧЕНЫ: profile, link, oidc, code", 1)
	states, claims, _ := scanIdentityMethodsBlock(broken)
	findings := judgeIdentityStateClaims(states, claims)
	if len(findings) == 0 {
		t.Fatal("исходная ложь перечня не найдена — гейт не отличает перечень от объявления")
	}
	joined := strings.Join(findings, " | ")
	for _, name := range []string{"code", "profile"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("находка не называет полосу %q: %s", name, joined)
		}
	}
	if strings.Contains(joined, "link") || strings.Contains(joined, "oidc") {
		t.Fatalf("находка обвиняет верно названные полосы — гейт ловит форму, а не существо: %s",
			joined)
	}
	t.Logf("инъекция исходной лжи: находок %d", len(findings))

	// ЗАКОННЫЙ БЛИЗНЕЦ: тот же вход без инъекции обязан молчать.
	statesOK, claimsOK, _ := scanIdentityMethodsBlock(body)
	if f := judgeIdentityStateClaims(statesOK, claimsOK); len(f) != 0 {
		t.Fatalf("дерево объявлено расходящимся, хотя перечень верен: %v", f)
	}
}

func TestIdentityMethodCommentInjection_SilentlyDisabledLaneIsFound(t *testing.T) {
	body := identityBodyForInjection(t)

	// ДЕФЕКТ: полоса выключена, перечень не тронут — молчаливое выключение.
	broken := strings.Replace(body,
		"    profile:\n      enabled: true", "    profile:\n      enabled: false", 1)
	if broken == body {
		t.Fatal("инъекция не изменила вход — форма объявления сменилась, и это утверждение " +
			"перестало что-либо доказывать")
	}
	states, claims, _ := scanIdentityMethodsBlock(broken)
	joined := strings.Join(judgeIdentityStateClaims(states, claims), " | ")
	if !strings.Contains(joined, "profile") {
		t.Fatalf("молчаливое выключение полосы не найдено: %q", joined)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ той же формы: та же правка, но перечень приведён в
	// соответствие — гейт обязан молчать.
	fixed := strings.Replace(broken, realClaimLines,
		"    #   ВЫКЛЮЧЕНЫ: link, oidc, profile\n    #   ВКЛЮЧЕНЫ: code", 1)
	statesF, claimsF, _ := scanIdentityMethodsBlock(fixed)
	if f := judgeIdentityStateClaims(statesF, claimsF); len(f) != 0 {
		t.Fatalf("выключение, названное в перечне, объявлено находкой: %v", f)
	}
}

func TestIdentityMethodCommentInjection_ConditionalLaneCannotBeClaimed(t *testing.T) {
	body := identityBodyForInjection(t)

	// ДЕФЕКТ: полоса, объявленная по-разному в разных ветках шаблона
	// (`webauthn` — посадка без доменного имени), названа безусловно.
	broken := strings.Replace(body, realClaimLines,
		"    #   ВЫКЛЮЧЕНЫ: link, oidc\n    #   ВКЛЮЧЕНЫ: code, profile, webauthn", 1)
	states, claims, _ := scanIdentityMethodsBlock(broken)
	if !states["webauthn"].conditional() {
		t.Fatal("`webauthn` перестал быть условным в дереве — ось инъекции потеряла предмет; " +
			"перемерьте ветку `$domainless`")
	}
	joined := strings.Join(judgeIdentityStateClaims(states, claims), " | ")
	if !strings.Contains(joined, "webauthn") {
		t.Fatalf("безусловное утверждение об условной полосе не найдено: %q", joined)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: безусловная полоса того же состояния — молчание.
	if f := judgeIdentityStateClaims(
		map[string]identityMethodState{"code": {SawEnabled: true}},
		[]identityStateClaim{{Line: 1, Enabled: true, Names: []string{"code"}}},
	); len(f) != 0 {
		t.Fatalf("безусловно включённая полоса объявлена находкой: %v", f)
	}
}

func TestIdentityMethodCommentInjection_PhantomNameIsFound(t *testing.T) {
	// ДЕФЕКТ: перечень пережил полосу, которой в объявлении нет.
	findings := judgeIdentityStateClaims(
		map[string]identityMethodState{"code": {SawEnabled: true}},
		[]identityStateClaim{{Line: 7, Enabled: false, Names: []string{"saml"}}},
	)
	if len(findings) == 0 || !strings.Contains(strings.Join(findings, " "), "saml") {
		t.Fatalf("имя-призрак в перечне не найдено: %v", findings)
	}
}

func TestIdentityMethodCommentInjection_ProseIsNotReadAsAClaim(t *testing.T) {
	// ОСЬ, РАДИ КОТОРОЙ ПЕРЕЧЕНЬ ОТМЕЧЕН: проза, называющая имена полос рядом
	// со словами о состоянии, обязана оставаться невидимой для гейта. Иначе он
	// краснел бы на комментарии, который объясняет сам дефект.
	body := identityBodyForInjection(t)
	prose := "    # Здесь проза: profile, link, oidc, code были выключены и включены.\n" +
		"    #   Это ОБЪЯСНЕНИЕ, а не перечень.\n"
	withProse := strings.Replace(body, "    link:\n      enabled: false", prose+"    link:\n      enabled: false", 1)
	if withProse == body {
		t.Fatal("проза не вставлена — ось не доказана")
	}
	states, claims, _ := scanIdentityMethodsBlock(withProse)
	if f := judgeIdentityStateClaims(states, claims); len(f) != 0 {
		t.Fatalf("гейт прочитал прозу как утверждение и покраснел на собственном объяснении: %v", f)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ той же формы: та же строка С ОТМЕТКОЙ обязана читаться.
	marked := strings.Replace(body, "    link:\n      enabled: false",
		"    #   ВЫКЛЮЧЕНЫ: code\n    link:\n      enabled: false", 1)
	statesM, claimsM, _ := scanIdentityMethodsBlock(marked)
	if f := judgeIdentityStateClaims(statesM, claimsM); len(f) == 0 {
		t.Fatal("отмеченная строка с ложным составом не прочитана — детектор отметки мёртв, " +
			"и молчание на прозе выше ничего не значило бы")
	}
	t.Logf("перепись оси: отмеченных строк без прозы %d · с прозой %d", len(claimsM), len(claims))
}

func TestIdentityMethodCommentInjection_VanishedLedgerIsNotSilence(t *testing.T) {
	// ДЕФЕКТ: перечень снят целиком. Гейт обязан не молчать — иначе он
	// становится вакуумным, оставаясь зелёным.
	body := identityBodyForInjection(t)
	stripped := strings.Replace(body, realClaimLines, "    #   (перечень снят)", 1)
	_, claims, _ := scanIdentityMethodsBlock(stripped)
	if len(claims) != 0 {
		t.Fatalf("после снятия перечня разобрано %d отмеченных строк — детектор отметки "+
			"находит то, чего нет", len(claims))
	}
	// Само падение по пустому перечню живёт в теле гейта (t.Fatalf); здесь
	// доказано условие, по которому оно наступает.
}
