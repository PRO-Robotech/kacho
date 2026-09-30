// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// admin_hop_transport_test.go — no production-class stack may carry the hop to
// the identity provider's ADMIN API in the clear, and none may carry it over TLS
// with nothing to verify the peer against.
//
// WHAT RIDES THIS HOP. The administrative bearer of every call its consumers make.
// The admin API authenticates nobody: reaching it IS the authorization.
//
// IAM NO LONGER TAKES THIS HOP (kacho#2818). The access service pinned here keeps
// no administrative road to the provider (kaname#362): the knob left the chart
// with its reader, and so did the probe that judged iam's side of the hop by its
// landing.
//
// THE EDGE NO LONGER TAKES THIS HOP (#2734). It used to: introspection asked
// about a bearer by SENDING it on every cache miss, and the logout ended the
// provider-side session there. Both calls are retired together with their knobs,
// and so are the cases that judged the edge's side of the hop.
//
// WHERE THIS LIVES (#2734). Both ends of the hop are the umbrella's: iam's side
// is declared under the umbrella's `kaname` key, the listener's TLS switch at the
// umbrella root. The edge takes no part in either, so the probe lives with the
// umbrella chart rather than in gateway/deploy, where it used to sit. It moved
// unchanged: same stacks, same findings.
//
// WHY THIS READS DECLARATIONS. Same reason as its neighbour token_shape_test.go
// and as gateway/deploy/revocation_endpoint_test.go: the contract is what the
// profiles DECLARE, it needs no chart dependencies, and it therefore can never
// skip. The umbrella's
// dependencies are not vendored, so a render-based check here would be skipped on
// every machine that has not run `helm dep build` — which is exactly when it
// would be needed.
//
// WHY THE EXEMPTION IS DERIVED, NOT LISTED. Whether a stack is production-class
// is read from the stack's OWN declaration (the gateway's environment label and
// iam's authn mode), not from a hard-coded list of names here. A hard-coded list
// goes stale silently the moment a stand changes posture, and the stale entry
// then exempts the very stack that just started needing the check.
package umbrella_test

import (
	"sort"
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
//
// iam is no longer a consumer (kacho#2818): the access service pinned here keeps no
// administrative road to the provider (kaname#362), and its chart knob left with
// the reader.
var adminHopConsumers = map[string][]string{
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
	stacks := deployableStacks(t)
	labels := make([]string, 0, len(adminHopConsumers))
	for label := range adminHopConsumers {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, name := range sortedStackNames(stacks) {
		stack := stacks[name]
		t.Run(name, func(t *testing.T) {
			on, _ := resolveStackBoolAt(t, stack, "mtls", "hydraAdminTls", "enabled")
			if !on {
				t.Skipf("%s does not serve the admin listener over TLS — consumers may address "+
					"it over http, and the transport gates above cover whether it may stay that way", name)
			}
			checked := 0
			for _, label := range labels {
				got, ok := resolveStackAt(t, stack, adminHopConsumers[label]...)
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
	stacks := deployableStacks(t)
	for _, name := range sortedStackNames(stacks) {
		stack := stacks[name]
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
