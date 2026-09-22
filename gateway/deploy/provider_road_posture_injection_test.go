// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// provider_road_posture_injection_test.go — пробы provider_road_posture_test.go
// и двух переходов admin_hop_transport_test.go УМЕЮТ УПАСТЬ на каждой посадке,
// и молчат на её законном близнеце.
//
// Зачем синтетика. Стек на посадке `own` в дереве сегодня один, и он полон:
// ветка `own` исполняется на нём, ничего не находя, и «зелено» о ней означало
// бы «условие не создано». Поэтому каждый исход доказан на входе, где он
// ЕСТЬ, — и на близнеце, отличающемся ровно одним фактом.
package deploy_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"
)

const (
	syntheticIntrospection = "https://provider-admin.kacho.test:4445/admin/oauth2/introspect"
	syntheticAdmin         = "https://provider-admin.kacho.test:4445"
)

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

func TestProviderRoadPosture_SilentChainInheritsTheChartDefault(t *testing.T) {
	chart := map[string]any{"authn": map[string]any{"identityProvider": "external"}}
	r := readPosture(edgePostureHalf, map[string]any{"api-gateway": map[string]any{}}, chart)
	if r.Err != nil || r.Provider != identityposture.External || !r.Inherited {
		t.Fatalf("молчащая цепочка обязана получить умолчание чарта external, получено %+v", r)
	}
}

func TestProviderRoadPosture_DeclaredPostureOutranksTheChartDefault(t *testing.T) {
	chart := map[string]any{"authn": map[string]any{"identityProvider": "external"}}
	merged := map[string]any{"api-gateway": map[string]any{"authn": map[string]any{"identityProvider": "own"}}}
	r := readPosture(edgePostureHalf, merged, chart)
	if r.Err != nil || r.Provider != identityposture.Own || r.Inherited {
		t.Fatalf("объявленная посадка own обязана перекрыть умолчание чарта, получено %+v", r)
	}
}

// Ключ, заданный пустым, ГАСИТ умолчание (шаблон с `with` ничего не
// выставляет): посадка не объявлена, и это находка, а не наследование.
func TestProviderRoadPosture_BlankedPostureIsNotInheritedAndIsAFinding(t *testing.T) {
	chart := map[string]any{"config": map[string]any{"authn": map[string]any{"identityProvider": "external"}}}
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
	merged := map[string]any{"api-gateway": map[string]any{"authn": map[string]any{"identityProvider": "Own"}}}
	r := readPosture(edgePostureHalf, merged, map[string]any{})
	if r.Err == nil {
		t.Fatalf("значение %q прочитано как %s — разборщик процесса его отвергает", "Own", r.Provider)
	}
	requireNamed(t, roadPresenceFinding("synthetic", introspectionRoad, r, ""),
		"synthetic", "api-gateway.hydra.introspectionUrl", "не разбирается")
}

// ─── наличие адреса: TestStacks_DeclareIntrospectionEndpoint / …AdminEndpoint ─

func TestProviderRoadPresence_Injection_OwnNamingTheProviderIsFound(t *testing.T) {
	for _, c := range []struct {
		knob providerRoadKnob
		addr string
	}{
		{introspectionRoad, syntheticIntrospection},
		{adminRoad, syntheticAdmin},
		{iamAdminRoad, syntheticAdmin},
	} {
		requireNamed(t, roadPresenceFinding("synthetic", c.knob, onPosture(identityposture.Own), c.addr),
			"synthetic", c.knob.label, "own", c.addr)
	}
}

func TestProviderRoadPresence_Twin_OwnWithoutTheProviderIsSilent(t *testing.T) {
	for _, k := range []providerRoadKnob{introspectionRoad, adminRoad, iamAdminRoad} {
		if f := roadPresenceFinding("synthetic", k, onPosture(identityposture.Own), ""); f != "" {
			t.Errorf("посадка own без адреса %s — законное состояние, а проба нашла:\n%s", k.label, f)
		}
	}
}

func TestProviderRoadPresence_Injection_ExternalWithoutTheProviderIsFound(t *testing.T) {
	for _, k := range []providerRoadKnob{introspectionRoad, adminRoad, iamAdminRoad} {
		requireNamed(t, roadPresenceFinding("synthetic", k, onPosture(identityposture.External), ""),
			"synthetic", k.label, "is not declared", k.missing)
	}
}

func TestProviderRoadPresence_Twin_ExternalNamingTheProviderIsSilent(t *testing.T) {
	for _, c := range []struct {
		knob providerRoadKnob
		addr string
	}{
		{introspectionRoad, syntheticIntrospection},
		{adminRoad, syntheticAdmin},
		{iamAdminRoad, syntheticAdmin},
	} {
		if f := roadPresenceFinding("synthetic", c.knob, onPosture(identityposture.External), c.addr); f != "" {
			t.Errorf("посадка external с адресом %s — законное состояние, а проба нашла:\n%s", c.knob.label, f)
		}
	}
}

// ─── переход края: TestStacks_GatewayAdminHopIsNotInTheClear ────────────────

func TestGatewayAdminHop_Twin_OwnWithoutAHopOrAnchorIsSilent(t *testing.T) {
	if got := judgeGatewayAdminHop(gatewayHopFacts{Stack: "synthetic", Posture: onPosture(identityposture.Own)}); len(got) != 0 {
		t.Errorf("посадка own без перехода и без якоря — законное состояние, а проба нашла:\n%s",
			strings.Join(got, "\n"))
	}
}

func TestGatewayAdminHop_Injection_OwnAnchorWithoutAHopIsFound(t *testing.T) {
	got := judgeGatewayAdminHop(gatewayHopFacts{
		Stack: "synthetic", Posture: onPosture(identityposture.Own),
		AdminCASecret: "provider-admin-tls",
	})
	if len(got) != 1 {
		t.Fatalf("ожидалась ОДНА находка о якоре, получено %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	requireNamed(t, got[0], "synthetic", "adminCa.secretName", "provider-admin-tls", "own")
}

// Посадка снимает требование НАЛИЧИЯ, а не транспорта: объявленный под `own`
// адрес в открытую — находка транспорта, как у стража края.
func TestGatewayAdminHop_Injection_OwnDeclaredPlaintextHopIsJudgedForTransport(t *testing.T) {
	got := judgeGatewayAdminHop(gatewayHopFacts{
		Stack: "synthetic", Posture: onPosture(identityposture.Own),
		Admin: "http://provider-admin.kacho.test:4445", AdminCASecret: "provider-admin-tls",
	})
	if len(got) != 1 {
		t.Fatalf("ожидалась ОДНА находка транспорта, получено %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	requireNamed(t, got[0], "synthetic", "api-gateway.hydra.adminUrl", "http://provider-admin.kacho.test:4445")
}

func TestGatewayAdminHop_Injection_ExternalWithoutAHopIsFound(t *testing.T) {
	got := judgeGatewayAdminHop(gatewayHopFacts{Stack: "synthetic", Posture: onPosture(identityposture.External)})
	joined := strings.Join(got, "\n")
	if len(got) != 3 {
		t.Fatalf("ожидались три находки (два адреса и якорь), получено %d:\n%s", len(got), joined)
	}
	for _, want := range []string{"introspectionUrl is not declared", "adminUrl is not declared", "adminCa.secretName is not declared"} {
		if !strings.Contains(joined, want) {
			t.Errorf("находки не называют %q:\n%s", want, joined)
		}
	}
}

func TestGatewayAdminHop_Twin_ExternalCompleteHopIsSilent(t *testing.T) {
	got := judgeGatewayAdminHop(gatewayHopFacts{
		Stack: "synthetic", Posture: onPosture(identityposture.External),
		Introspection: syntheticIntrospection, Admin: syntheticAdmin, AdminCASecret: "provider-admin-tls",
	})
	if len(got) != 0 {
		t.Errorf("полный переход по https с якорем на external — законное состояние, а проба нашла:\n%s",
			strings.Join(got, "\n"))
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

func TestIAMProviderHop_Injection_ExternalWithoutARoadIsFound(t *testing.T) {
	got := judgeIAMProviderHop(iamHopFacts{Stack: "synthetic", Posture: onPosture(identityposture.External)})
	if len(got) != 1 {
		t.Fatalf("ожидалась ОДНА находка, получено %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	requireNamed(t, got[0], "synthetic", "kaname.platform.iam.hydraAdminUrl is not declared")
}

func TestIAMProviderHop_Twin_ExternalProductionRoadOverTLSIsSilent(t *testing.T) {
	got := judgeIAMProviderHop(iamHopFacts{
		Stack: "synthetic", Posture: onPosture(identityposture.External), Production: true,
		AdminURL: syntheticAdmin, CAFile: "/etc/kaname/tls/server/ca.crt",
	})
	if len(got) != 0 {
		t.Errorf("дорога по https с якорем на боевом external — законное состояние, а проба нашла:\n%s",
			strings.Join(got, "\n"))
	}
}

func TestIAMProviderHop_Injection_ExternalProductionPlaintextRoadIsFound(t *testing.T) {
	got := judgeIAMProviderHop(iamHopFacts{
		Stack: "synthetic", Posture: onPosture(identityposture.External), Production: true,
		AdminURL: "http://provider-admin.kacho.test:4445", CAFile: "/etc/kaname/tls/server/ca.crt",
	})
	if len(got) != 1 {
		t.Fatalf("ожидалась ОДНА находка транспорта, получено %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	requireNamed(t, got[0], "synthetic", "hydraAdminUrl", "http://provider-admin.kacho.test:4445")
}
