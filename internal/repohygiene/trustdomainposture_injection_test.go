// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainposture_injection_test.go — доказательство того, что
// TestAcceptedTrustDomainMatchesIssued СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТЕ ЖЕ функции разбора, что и гейт: доказательство,
// проверяющее вторую реализацию, доказывает вторую реализацию.
//
// Прогонов ТРИ, и третий обязателен: без него молчание распознавателя на
// законной записи неотличимо от молчания мёртвого разбора.
//
//	control    — обе стороны берут домен из объявления: находок ноль, перепись НЕ ноль;
//	injection  — внесён второй адрес домена, каждой формой: находка с координатой;
//	legitimate — проза, называющая ручку и домен: молчание.
package repohygiene

import (
	"strings"
	"testing"
)

// postureChartWellFormed — чарт, где домен объявлен ОДИН раз и обе стороны
// берут его оттуда.
const postureChartWellFormed = `{{- $trust := include "kaname.trustDomain" . }}
uris:
  - {{ printf "spiffe://%s/ns/%s/sa/%s" $trust $ns $sa | quote }}
config:
  authn:
    trust-domain: {{ include "kaname.trustDomain" . | quote }}
`

// postureChartSecondAddress — тот же чарт, но домен взят СВОЕЙ рукой: умолчание
// объявлено на месте употребления, вне именованного шаблона.
const postureChartSecondAddress = `{{- $sp := .Values.mtls.spiffe | default dict }}
{{- $trust := $sp.trustDomain | default "kacho.cloud" }}
uris:
  - {{ printf "spiffe://%s/ns/%s/sa/%s" $trust $ns $sa | quote }}
config:
  authn:
    trust-domain: {{ $trust | quote }}
`

// postureHelperDeclaration — объявление домена именованным шаблоном: законное
// место умолчания.
const postureHelperDeclaration = `{{- define "kaname.trustDomain" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $sp.trustDomain | default "kacho.cloud" -}}
{{- end -}}
`

// postureHelperDisagreeing — второе объявление, расходящееся умолчанием.
// Отличие от близнеца выше — ОДИН факт: величина умолчания.
const postureHelperDisagreeing = `{{- define "kacho-geo.trustDomain" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $sp.trustDomain | default "kaname.local" -}}
{{- end -}}
`

// postureProseNamesTheKnob — ПРОЗА: комментарий профиля называет и ручку, и
// домен. Ни то, ни другое действующим объявлением не является, и распознаватель,
// судящий по подстроке, покраснел бы здесь — на собственном объяснении.
const postureProseNamesTheKnob = `# Домен доверия задаётся ручкой KANAME_AUTHN__TRUST_DOMAIN
# (ключ файла настроек authn.trust-domain). Умолчание — kacho.cloud.
# Ниже spiffe:// не чеканится: это комментарий.
image: kaname:latest
`

func postureKnobTokens() []string {
	return append(
		TrustDomainKnobTokens("authn.trust-domain (env KANAME_AUTHN__TRUST_DOMAIN)"),
		TrustDomainKnobTokens("KACHO_GEO_AUTHZ_TRUST_DOMAIN")...)
}

// TestTrustDomainPostureScannerAcceptsOneDeclaration — прогон-контроль: на
// верной записи находок нет, а перепись НЕ ноль.
func TestTrustDomainPostureScannerAcceptsOneDeclaration(t *testing.T) {
	decls, uses, census := ScanTrustDomainPosture(
		"deploy/helm/umbrella/charts/kaname/templates/certificate.yaml", "kaname",
		[]byte(postureChartWellFormed), postureKnobTokens())

	if census.Actions == 0 {
		t.Fatalf("осмотрено ноль действий шаблона — разбирается не то дерево")
	}
	if len(decls) != 0 {
		t.Fatalf("на месте употребления найдено объявление (%+v) — тогда гейт запрещал бы "+
			"брать домен из именованного шаблона", decls)
	}
	var issuing, accepting int
	for _, u := range uses {
		if !u.FromHelper {
			t.Errorf("употребление объявлено взятым НЕ из объявления чарта, хотя include в файле "+
				"есть: %+v", u)
		}
		switch u.Side {
		case "issuing":
			issuing++
		case "accepting":
			accepting++
		}
	}
	if issuing == 0 || accepting == 0 {
		t.Fatalf("сторон опознано: чеканка %d, приём %d — молчание гейта сказано ни о чём",
			issuing, accepting)
	}
}

// TestTrustDomainPostureScannerFindsASecondAddress — сторона (а): второй адрес
// домена становится находкой, и находка несёт координату.
func TestTrustDomainPostureScannerFindsASecondAddress(t *testing.T) {
	decls, uses, _ := ScanTrustDomainPosture(
		"deploy/helm/umbrella/charts/kaname/templates/certificate.yaml", "kaname",
		[]byte(postureChartSecondAddress), postureKnobTokens())

	if len(decls) != 1 {
		t.Fatalf("объявлений найдено %d, ожидалось 1: %+v", len(decls), decls)
	}
	d := decls[0]
	if d.Helper != "" {
		t.Errorf("объявление вне именованного шаблона опознано как принадлежащее ему: %+v", d)
	}
	if d.Line == 0 || d.File == "" {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", d)
	}
	if d.Default != "kacho.cloud" {
		t.Errorf("умолчание прочитано как %q: %+v", d.Default, d)
	}
	if len(uses) == 0 {
		t.Fatalf("употреблений не опознано вовсе — гейт молчал бы и о стороне, взявшей домен мимо объявления")
	}
	for _, u := range uses {
		if u.FromHelper {
			t.Errorf("употребление в файле БЕЗ include объявлено взятым из объявления: %+v", u)
		}
	}
}

// TestTrustDomainPostureScannerNamesBothSidesOfADisagreement — расхождение
// умолчаний называет ОБЕ стороны и обе величины.
func TestTrustDomainPostureScannerNamesBothSidesOfADisagreement(t *testing.T) {
	a, _, _ := ScanTrustDomainPosture("deploy/helm/umbrella/charts/kaname/templates/_helpers.tpl",
		"kaname", []byte(postureHelperDeclaration), nil)
	b, _, _ := ScanTrustDomainPosture("deploy/helm/umbrella/charts/kacho-geo/templates/_helpers.tpl",
		"kacho-geo", []byte(postureHelperDisagreeing), nil)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("объявлений найдено %d и %d, ожидалось по одному: %+v %+v", len(a), len(b), a, b)
	}
	if a[0].Helper == "" || b[0].Helper == "" {
		t.Fatalf("объявление внутри именованного шаблона не связано с ним: %+v %+v", a[0], b[0])
	}

	// Согласные объявления молчат — иначе отрицание ниже зеленело бы на любом дереве.
	if got := TrustDomainDeclDisagreement([]TrustDomainDeclSite{a[0], a[0]}); got != "" {
		t.Fatalf("согласные объявления объявлены расходящимися: %s", got)
	}

	got := TrustDomainDeclDisagreement([]TrustDomainDeclSite{a[0], b[0]})
	if got == "" {
		t.Fatalf("расхождение умолчаний не найдено — гейт молчал бы там, где сертификат " +
			"чеканится под одним доменом, а признаётся другой")
	}
	for _, want := range []string{a[0].File, b[0].File, "kacho.cloud", "kaname.local"} {
		if !strings.Contains(got, want) {
			t.Errorf("текст расхождения не называет %q — по такому отказу нельзя понять, "+
				"какие две стороны разошлись: %s", want, got)
		}
	}
}

// TestTrustDomainPostureScannerIsSilentOnProse — сторона (б): проза, называющая
// и ручку, и домен, молчание не теряет.
func TestTrustDomainPostureScannerIsSilentOnProse(t *testing.T) {
	decls, uses, census := ScanTrustDomainPosture(
		"deploy/helm/umbrella/values.prod.yaml", "umbrella",
		[]byte(postureProseNamesTheKnob), postureKnobTokens())

	if census.Files == 0 {
		t.Fatalf("файл не прочитан — молчание сказано ни о чём")
	}
	if len(decls) != 0 {
		t.Fatalf("комментарий объявлен объявлением домена: %+v", decls)
	}
	if len(uses) != 0 {
		t.Fatalf("комментарий объявлен действующей стороной (%+v) — распознаватель судит "+
			"подстроку, а не позицию, и покраснел бы на собственном объяснении", uses)
	}
}

// TestTrustDomainKnobTokensKnowsBothFormsOfTheName — имя ручки живёт в тексте,
// написанном ОПЕРАТОРУ, и форм у него две. Распознаватель, знающий одну, к
// службе со второй формой слеп — и слеп молча.
func TestTrustDomainKnobTokensKnowsBothFormsOfTheName(t *testing.T) {
	got := TrustDomainKnobTokens("authn.trust-domain (env KANAME_AUTHN__TRUST_DOMAIN)")
	want := map[string]bool{"authn.trust-domain": false, "KANAME_AUTHN__TRUST_DOMAIN": false}
	for _, g := range got {
		if _, ok := want[g]; !ok {
			t.Errorf("из текста ручки выбрано лишнее слово %q — оно попадёт в поиск и даст "+
				"находку там, где её никто не писал", g)
			continue
		}
		want[g] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("форма имени %q не выбрана — служба, называющая ручку так, остаётся "+
				"вне наблюдения, и это не красное и не зелёное, а молчание", k)
		}
	}

	// Ручка без формы файла настроек даёт ровно одно имя.
	if got := TrustDomainKnobTokens("KACHO_GEO_AUTHZ_TRUST_DOMAIN"); len(got) != 1 || got[0] != "KACHO_GEO_AUTHZ_TRUST_DOMAIN" {
		t.Errorf("из простого имени ручки выбрано %v", got)
	}
}
