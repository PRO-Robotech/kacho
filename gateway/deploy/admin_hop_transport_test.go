// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// admin_hop_transport_test.go — no production-class stack may carry the hop to
// the identity provider's ADMIN API in the clear, and none may carry it over TLS
// with nothing to verify the peer against.
//
// WHAT RIDES THIS HOP. Since the revocation check moved onto the authN layer,
// introspection runs on every authenticated request that misses the short-TTL
// cache — and it asks about a bearer by SENDING it. So the wire carries a LIVE
// END-USER credential, not merely administrative calls, and a bearer read off the
// wire is usable by whoever read it until it expires. On the same host, iam's
// facade carries the administrative bearer for every OAuth2 client registration,
// trust grant and session teardown. The admin API authenticates nobody: reaching
// it IS the authorization.
//
// WHY THIS READS DECLARATIONS. Same reason as its neighbours token_shape_test.go
// and revocation_endpoint_test.go: the contract is what the profiles DECLARE, it
// needs no chart dependencies, and it therefore can never skip. The umbrella's
// dependencies are not vendored, so a render-based check here would be skipped on
// every machine that has not run `helm dep build` — which is exactly when it
// would be needed.
//
// WHY THE EXEMPTION IS DERIVED, NOT LISTED. Whether a stack is production-class
// is read from the stack's OWN declaration (the gateway's environment label and
// iam's authn mode), not from a hard-coded list of names here. A hard-coded list
// goes stale silently the moment a stand changes posture, and the stale entry
// then exempts the very stack that just started needing the check.
package deploy_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// resolveStackAt merges a stack's profiles in order (the way helm does) and reads
// the string at an ABSOLUTE path in the merged tree.
//
// Its neighbour resolveStack is rooted at the gateway's own sub-tree; this hop is
// declared by two different charts, so the absolute form is needed to read iam's
// side without duplicating the merge.
func resolveStackAt(t *testing.T, stack []string, path ...string) (string, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok && strings.TrimSpace(s) != ""
}

// devClassEnvLabels — the labels the gateway's own boot guard treats as
// dev-class. Mirrored from validateProductionRevocationConfig; every other value,
// INCLUDING an empty one, is production-class.
var devClassEnvLabels = map[string]bool{"dev": true, "local": true, "test": true}

// stackIsProductionClass reports whether a stack deploys the production security
// posture, by reading what the stack declares about itself.
//
// Either process being production-class makes the stack production-class: they
// share the hop, and a stand where one of them refuses to start is not a working
// stand.
func stackIsProductionClass(t *testing.T, stack []string) bool {
	t.Helper()
	if label, ok := resolveStack(t, stack, "appEnv"); ok && !devClassEnvLabels[strings.ToLower(strings.TrimSpace(label))] {
		return true
	}
	// Посадка адресуется каноном `kaname.authMode` в корне значений сервиса;
	// прежний адрес (`config.authn.mode`) читается следом, потому что шаблон чарта
	// его тоже пока принимает.
	//
	// Клауза стоит ВТОРОЙ и сегодня ничего не решает — классификацию несёт `appEnv`
	// выше. Именно поэтому её и надо было чинить: отбор по переехавшему ключу не
	// краснеет, он тихо перестаёт находить предмет, а прикрытый соседней клаузой —
	// не краснеет даже переписью. Стек без `appEnv`, объявивший посадку, ушёл бы в
	// Skip как dev-class.
	mode, ok := resolveStackAt(t, stack, "kaname", "authMode")
	if !ok {
		mode, ok = resolveStackAt(t, stack, "kaname", "config", "authn", "mode")
	}
	return ok && strings.HasPrefix(strings.TrimSpace(mode), "production")
}

// Every production-class stack must address the gateway's two admin-API endpoints
// over TLS. A plaintext address here is refused at start by the gateway's boot
// guard — this gate is what keeps that refusal from being discovered on a stand.
func TestStacks_GatewayAdminHopIsNotInTheClear(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			if !stackIsProductionClass(t, stack) {
				t.Skipf("%s is dev-class by its own declaration — the transport requirement "+
					"rides the same exemption as the gateway's boot guard", name)
			}
			for _, knob := range []string{"introspectionUrl", "adminUrl"} {
				got, ok := resolveStack(t, stack, "hydra", knob)
				if !ok {
					t.Errorf("%s: api-gateway.hydra.%s is not declared", name, knob)
					continue
				}
				if err := requireTLSHop(got); err != nil {
					t.Errorf("%s: api-gateway.hydra.%s %v", name, knob, err)
				}
			}
			// TLS without an anchor is not a partial improvement: the provider's
			// in-cluster certificate is internal-CA issued, the gateway trusts the
			// system roots, and the introspection layer files an unknown authority
			// as a PERMANENT misconfiguration — after which it refuses every request.
			if _, ok := resolveStack(t, stack, "hydra", "adminCa", "secretName"); !ok {
				t.Errorf("%s: api-gateway.hydra.adminCa.secretName is not declared while the hop "+
					"is https — the gateway would verify against the system roots, which an "+
					"internal-CA certificate never chains to, and then refuse every request", name)
			}
		})
	}
}

// And the same for iam, the platform's sole facade to the provider. Its hop must
// additionally be DECLARED rather than derived: the derivation is never empty, so
// a profile that never named the address still read as configured — while
// addressing the public ingress hostname, which does not resolve in-cluster.
func TestStacks_IAMProviderAdminHopIsDeclaredAndNotInTheClear(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveStackAt(t, stack, "kaname", "kacho", "iam", "hydraAdminUrl")
			if !ok {
				t.Fatalf("%s: kaname.kacho.iam.hydraAdminUrl is not declared — iam then "+
					"DERIVES it from the issuer, which names the public ingress host and does "+
					"not resolve inside the cluster; the derivation is never empty, so the "+
					"facade reads as configured while addressing a host nobody chose", name)
			}
			if !stackIsProductionClass(t, stack) {
				return // declared is required everywhere; TLS only where production posture applies
			}
			if err := requireTLSHop(got); err != nil {
				t.Errorf("%s: kaname.kacho.iam.hydraAdminUrl %v", name, err)
			}
			if _, ok := resolveStackAt(t, stack, "kaname", "kacho", "iam", "hydraAdminCaFile"); !ok {
				t.Errorf("%s: kaname.kacho.iam.hydraAdminCaFile is not declared while the hop "+
					"is https — iam would verify against the system roots and every call on the "+
					"hop would fail with an unknown authority", name)
			}
		})
	}
}

// requireTLSHop reports why an address is unfit to carry a credential.
func requireTLSHop(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("is not a valid URL: %v", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("is %q — the hop carries a live bearer on every introspection cache miss "+
			"and the administrative bearer on every facade call, so in the clear anything on "+
			"the path can read and reuse them; address the admin listener over https", raw)
	}
	return nil
}

// adminHopConsumers — every place a profile DECLARES the provider's ADMIN API.
//
// This list is the point of the test below. The defect it guards is not "one
// address was left in the clear" but "the listener moved to TLS and a consumer
// was left behind" — which is silent until that consumer's flow is exercised. The
// Kratos self-service UI was exactly that: it dials the admin API for the consent
// flow, its address lives under a different chart, and it was overlooked while the
// gateway and iam were being moved. Adding a consumer means adding it here.
//
// ─── THE BOUNDARY OF THIS REGISTRY, STATED SO ITS COUNT IS NOT READ AS COMPLETE ──
//
// This registry resolves PATHS IN VALUES FILES. By construction it therefore does
// NOT see a consumer whose address arrives as a dependency chart's DEFAULT, nor
// one baked into a template with no values key at all. Its "4/4 inspected" reads
// like completeness and is not: measured on this tree, consumers numbered SEVEN
// while four were declared here.
//
// Two of the three it could not see had no probes of their own, so on an address
// change they would not have gone red — they would have quietly stopped resolving
// a name, which is worse than failing.
//
// The missing half is deploy/tests/helm/admin-hop-address-census-test.sh: it
// builds the census by walking the tree AND the rendered manifests of every
// deployable stack, reads THIS registry (rather than keeping a copy of it), and
// treats an address present in a render but declared by no entry here as a
// finding. Pairing a declaration-built registry with a mechanical walk is the
// general rule, not a fix for this hop — see that gate's header.
var adminHopConsumers = map[string][]string{
	"api-gateway.hydra.introspectionUrl":  {"api-gateway", "hydra", "introspectionUrl"},
	"api-gateway.hydra.adminUrl":          {"api-gateway", "hydra", "adminUrl"},
	"kaname.kacho.iam.hydraAdminUrl":      {"kaname", "kacho", "iam", "hydraAdminUrl"},
	"kratos-selfservice-ui…hydraAdminUrl": {"kratos-selfservice-ui", "kratosSelfServiceUI", "hydraAdminUrl"},
}

// Once a stack fronts the admin hop with TLS, EVERY consumer that names it must
// address it over https.
//
// A consumer left behind does not degrade — it stops working, and only in the
// flow nobody runs on a smoke test. Note WHY it stops, because the reason
// changed: the provider's admin listener still answers plain http, but only on
// the pod's LOOPBACK, and its own Service is removed. So the old address does not
// fail a handshake — it fails to resolve. That is the intended shape: loud and
// immediate, rather than a timeout against something that looks like an address.
func TestStacks_AdminHopConsumersAgreeWithTheListener(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			on, _ := resolveStackBoolAt(t, stack, "mtls", "hydraAdminTls", "enabled")
			if !on {
				t.Skipf("%s does not serve the admin listener over TLS — consumers may address "+
					"it over http, and the transport gates above cover whether it may stay that way", name)
			}
			checked := 0
			for label, path := range adminHopConsumers {
				got, ok := resolveStackAt(t, stack, path...)
				if !ok {
					continue // a consumer this stack does not deploy or does not name
				}
				checked++
				if !strings.HasPrefix(strings.TrimSpace(got), "https://") {
					t.Errorf("%s: %s is %q while this stack fronts the admin hop with TLS — "+
						"the provider's own admin Service is removed, so this address does not "+
						"even resolve and this consumer's flow fails outright", name, label, got)
				}
			}
			// "Nothing found" must be distinguishable from "nothing wrong": a
			// renamed values key would silently empty this test.
			if checked == 0 {
				t.Errorf("%s: no admin-API consumer declaration was found at any known path — "+
					"the keys were renamed and this gate is now inspecting nothing", name)
			}
			// The boundary is logged with the count, not left to the reader of
			// the number: "4/4" is completeness only over what this registry can
			// see, and what it cannot see is where the defect lived.
			t.Logf("%s: %d/%d consumer DECLARATIONS inspected — this registry resolves values "+
				"paths only; consumers arriving from a dependency's chart default or baked "+
				"into a template are invisible to it and are covered by "+
				"deploy/tests/helm/admin-hop-address-census-test.sh", name, checked, len(adminHopConsumers))
		})
	}
}

// resolveStackBoolAt reads a boolean at an ABSOLUTE path in the merged stack.
// Its neighbour resolveStackBool is rooted at the gateway's own sub-tree; this
// switch lives at the umbrella root.
func resolveStackBoolAt(t *testing.T, stack []string, path ...string) (bool, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return false, false
		}
		if cur, ok = m[key]; !ok {
			return false, false
		}
	}
	b, ok := cur.(bool)
	return b, ok
}

// devClassStackNames — стеки, которым РАЗРЕШЕНО быть dev-класса. Перечень
// закрыт и назван по имени, а не выведен из состава: именно вывод из состава и
// подвёл.
var devClassStackNames = map[string]bool{"dev": true}

// TestStacks_OnlyNamedDevStacksAreDevClass — стек, не объявленный dev по имени,
// обязан читаться боевым.
//
// # Предмет
//
// Проверки транспорта административного перехода условны: они спрашивают «стек
// объявил боевую посадку?» и пропускают dev-класс. Условие правильное — dev-стенд
// вправе не нести требований, — но оно опирается на СОСТАВ стека, а состав
// собирается здесь же, рукой.
//
// Так и вышло: накладка образов `values.prorobotech.yaml` описывает только
// образы и наследует безопасность у слоя под собой, о чём прямо говорит её
// собственная шапка («средняя строка НЕ опциональна»). В наборе она была
// собрана без этого слоя — и стек, которым продукт поднимают на управляемом
// кластере, читался dev-классом и уходил из-под ОБЕИХ проверок транспорта.
// Пропуск при этом выглядел законным и печатал «dev-class by its own
// declaration», хотя накладка не объявляет посадку ВООБЩЕ: «объявил dev» и «не
// объявил ничего» — разные состояния, и первое здесь было выводом набора, а не
// заявлением файла.
//
// # Почему страж по ИМЕНИ, а не по составу
//
// Свойство, которое нужно удержать, — «пропуск полагается только тому, кто
// заявлен стендом разработки». Имя стека заявляет намерение автора набора;
// состав — то, что из намерения получилось. Сверять получившееся с самим собой
// бессмысленно, поэтому страж читает намерение и требует, чтобы состав ему
// соответствовал.
func TestStacks_OnlyNamedDevStacksAreDevClass(t *testing.T) {
	if len(deployableStacks(t)) == 0 {
		t.Fatal("набор стеков пуст — «все боевые» здесь означало бы «ни одного не смотрели»")
	}
	devFound := 0
	for name, stack := range deployableStacks(t) {
		production := stackIsProductionClass(t, stack)
		if devClassStackNames[name] {
			devFound++
			if production {
				t.Errorf("%s назван стендом разработки, но состав объявляет боевую посадку — "+
					"перечень devClassStackNames пережил свой предмет", name)
			}
			continue
		}
		if !production {
			t.Errorf("%s читается dev-классом, хотя стендом разработки не назван: обе проверки "+
				"транспорта административного перехода его ПРОПУСТЯТ, и пропуск будет выглядеть "+
				"законным. Составьте стек так, как предписывает шапка его собственной накладки "+
				"(слой с посадкой не опционален), либо внесите имя в devClassStackNames — "+
				"осознанно и с причиной", name)
		}
	}
	if devFound == 0 {
		t.Error("ни один стек перечня devClassStackNames не встретился в наборе — перечню " +
			"больше нечего разрешать, и он остался бы верным при любом дереве")
	}
	t.Logf("осмотрено: стеков %d, из них разрешённых dev-класса %d", len(deployableStacks(t)), devFound)
}
