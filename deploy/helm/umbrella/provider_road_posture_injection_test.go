// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// provider_road_posture_injection_test.go — пробы provider_road_posture_test.go
// и перехода службы доступа в admin_hop_transport_test.go УМЕЮТ УПАСТЬ на
// каждой посадке, и молчат на её законном близнеце.
//
// Дорог КРАЯ к поставщику больше нет (#2734): их случаи сняты вместе с
// ручками, которые они судили, а пробы переехали к чарту зонта вместе с
// судимыми ими пробами.
//
// Посадку `external` фундамент снял (corelib#26): её случаи — «адреса нет,
// а он требуется», «адрес есть, и это законно», транспорт объявленной дороги
// на боевом стенде — сняты вместе с ветвью пробы по ней (#2873). Стек,
// объявивший снятое значение, получает находку «посадка не известна».
//
// Зачем синтетика. Стек на посадке `own` в дереве сегодня один, и он полон:
// ветка `own` исполняется на нём, ничего не находя, и «зелено» о ней означало
// бы «условие не создано». Поэтому каждый исход доказан на входе, где он
// ЕСТЬ, — и на близнеце, отличающемся ровно одним фактом.
package umbrella_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"
)

const syntheticAdmin = "https://provider-admin.kacho.test:4445"

func onPosture(p identityposture.Provider) postureReading {
	return postureReading{Provider: p, Raw: p.String()}
}

// requireNamed — находка обязана назвать ЧТО и ПОЧЕМУ, а не только покраснеть.
func requireNamed(t *testing.T, finding string, parts ...string) {
	t.Helper()
	if finding == "" {
		t.Fatalf("находки нет — проба не умеет упасть на этом входе")
	}
	for _, p := range parts {
		if !strings.Contains(finding, p) {
			t.Errorf("находка не называет %q:\n%s", p, finding)
		}
	}
}

// ─── чтение посадки ─────────────────────────────────────────────────────────
//
// Две пробы ниже читали посадку края. Дорог края к поставщику нет (#2734), и
// половина края снята: судить ею нечего. Чтение посадки — одна функция на обе
// половины, поэтому пробы спрашивают её о половине, у которой дорога есть.

func TestProviderRoadPosture_SilentChainInheritsTheChartDefault(t *testing.T) {
	chart := map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "own"}}}
	r := readPosture(iamPostureHalf, map[string]any{"kaname": map[string]any{}}, chart)
	if r.Err != nil || r.Provider != identityposture.Own || !r.Inherited {
		t.Fatalf("молчащая цепочка обязана получить умолчание чарта own, получено %+v", r)
	}
}

// Умолчание чарта здесь намеренно НЕ разбирается словарём процесса: будь оно
// прочитано, проба покраснела бы на разборе, а не на том, что объявленное
// значение цепочки перекрывает умолчание.
func TestProviderRoadPosture_DeclaredPostureOutranksTheChartDefault(t *testing.T) {
	chart := map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "Own"}}}
	merged := map[string]any{"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "own"}}}}
	r := readPosture(iamPostureHalf, merged, chart)
	if r.Err != nil || r.Provider != identityposture.Own || r.Inherited {
		t.Fatalf("объявленная посадка own обязана перекрыть умолчание чарта, получено %+v", r)
	}
}

// Ключ, заданный пустым, ГАСИТ умолчание (шаблон с `with` ничего не
// выставляет): посадка не объявлена, и это находка, а не наследование.
func TestProviderRoadPosture_BlankedPostureIsNotInheritedAndIsAFinding(t *testing.T) {
	chart := map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "own"}}}
	merged := map[string]any{"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": ""}}}}
	r := readPosture(iamPostureHalf, merged, chart)
	if r.Inherited || r.Provider.IsSet() {
		t.Fatalf("гашёная посадка прочитана как %+v — пустой ключ перекрывает умолчание", r)
	}
	requireNamed(t, roadPresenceFinding("synthetic", iamAdminRoad, r, ""),
		"synthetic", "kaname.platform.iam.hydraAdminUrl", "не объявлена")
}

// Значение вне словаря отвергается ТЕМ ЖЕ разборщиком, что у процесса:
// регистр не сворачивается.
func TestProviderRoadPosture_UnparsablePostureIsAFinding(t *testing.T) {
	merged := map[string]any{"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "Own"}}}}
	r := readPosture(iamPostureHalf, merged, map[string]any{})
	if r.Err == nil {
		t.Fatalf("значение %q прочитано как %s — разборщик процесса его отвергает", "Own", r.Provider)
	}
	requireNamed(t, roadPresenceFinding("synthetic", iamAdminRoad, r, ""),
		"synthetic", "kaname.platform.iam.hydraAdminUrl", "не разбирается")
}

// Снятое фундаментом значение посадки — находка, а не молчание: при любом пине
// фундамента оно либо не разбирается, либо этой пробе не известно, и стек,
// объявивший его, судить по посадке нельзя.
func TestProviderRoadPosture_RetiredPostureIsAFinding(t *testing.T) {
	merged := map[string]any{"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "external"}}}}
	r := readPosture(iamPostureHalf, merged, map[string]any{})
	requireNamed(t, roadPresenceFinding("synthetic", iamAdminRoad, r, syntheticAdmin),
		"synthetic", "kaname.platform.iam.hydraAdminUrl", "посадка")
	if f := roadPresenceFinding("synthetic", iamAdminRoad, r, ""); f == "" {
		t.Fatal("снятое значение посадки без адреса прочитано законным состоянием")
	}
}

// ─── наличие адреса дороги службы доступа ───────────────────────────────────

func TestProviderRoadPresence_Injection_OwnNamingTheProviderIsFound(t *testing.T) {
	for _, c := range []struct {
		knob providerRoadKnob
		addr string
	}{
		{iamAdminRoad, syntheticAdmin},
	} {
		requireNamed(t, roadPresenceFinding("synthetic", c.knob, onPosture(identityposture.Own), c.addr),
			"synthetic", c.knob.label, "own", c.addr)
	}
}

func TestProviderRoadPresence_Twin_OwnWithoutTheProviderIsSilent(t *testing.T) {
	for _, k := range []providerRoadKnob{iamAdminRoad} {
		if f := roadPresenceFinding("synthetic", k, onPosture(identityposture.Own), ""); f != "" {
			t.Errorf("посадка own без адреса %s — законное состояние, а проба нашла:\n%s", k.label, f)
		}
	}
}

// ─── дорога службы доступа: TestStacks_IAMProviderAdminHopIsDeclaredAndNotInTheClear ─

func TestIAMProviderHop_Twin_OwnWithoutARoadIsSilent(t *testing.T) {
	if got := judgeIAMProviderHop(iamHopFacts{Stack: "synthetic", Posture: onPosture(identityposture.Own), Production: true}); len(got) != 0 {
		t.Errorf("посадка own без дороги — законное состояние, а проба нашла:\n%s", strings.Join(got, "\n"))
	}
}

func TestIAMProviderHop_Injection_OwnNamingTheRoadIsFound(t *testing.T) {
	got := judgeIAMProviderHop(iamHopFacts{
		Stack: "synthetic", Posture: onPosture(identityposture.Own), Production: true,
		AdminURL: syntheticAdmin, CAFile: "/etc/kaname/tls/server/ca.crt",
	})
	if len(got) != 1 {
		t.Fatalf("ожидалась ОДНА находка о дороге, получено %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	requireNamed(t, got[0], "synthetic", "kaname.platform.iam.hydraAdminUrl", "own", syntheticAdmin)
}
