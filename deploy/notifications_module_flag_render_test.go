//go:build helmcharts

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notifications_module_flag_render_test.go — полоса RED S1-A4 issue-2918 (NTF-3,
// Н3-Ф2), часть в чарте: ручка KACHO_<MODULE>_NOTIFICATIONS_ENABLED каждого из
// пяти модулей kacho выводится из одного объявления
// `global.kacho.notifications.enabled` и переопределения модуля
// `<модуль>.notifications.enabled` (замысел З11; помощник `enabledFor` — полоса D2).
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-66 — в части флагов модулей:
//   - Given: global true, `storage.notifications.enabled: false` → у storage
//     KACHO_STORAGE_NOTIFICATIONS_ENABLED=false, у compute, nlb, registry, vpc — true;
//   - близнец: без переопределения — true у всех пяти;
//   - близнец: global false без переопределения — false у всех пяти;
//   - values без `global.kacho.notifications.enabled` — рендер отказывает с
//     сообщением, называющим ключ (умолчания нет).
// Перечень источников notify, записи sourceLimits и допуск notify-sender в
// политике vpc (остаток NTF3-66) — предмет полосы чарта notify, не этой пробы.
//
// Значение ручки берётся так, как его получит процесс: `env[].value`,
// `env[].valueFrom.configMapKeyRef` либо `envFrom[].configMapRef` карты рендера.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

const ntf366GlobalKey = "global.kacho.notifications.enabled"

// ntf366Modules — модуль → (рабочий объект рендера, ручка флага).
var ntf366Modules = []struct{ module, workload, knob string }{
	{"compute", "compute", "KACHO_COMPUTE_NOTIFICATIONS_ENABLED"},
	{"nlb", "kacho-nlb", "KACHO_NLB_NOTIFICATIONS_ENABLED"},
	{"registry", "registry", "KACHO_REGISTRY_NOTIFICATIONS_ENABLED"},
	{"storage", "kacho-storage", "KACHO_STORAGE_NOTIFICATIONS_ENABLED"},
	{"vpc", "vpc", "KACHO_VPC_NOTIFICATIONS_ENABLED"},
}

func ntf366Map(v any) map[string]any { m, _ := v.(map[string]any); return m }
func ntf366List(v any) []any         { l, _ := v.([]any); return l }

// ntf366KnobValues — значение ручки флага у рабочего объекта каждого модуля
// ("" — ручки нет); отсутствующий рабочий объект — отказ фикстуры.
func ntf366KnobValues(t *testing.T, stack string, docs []renderedDoc) map[string]string {
	t.Helper()
	configMaps := map[string]map[string]any{}
	workloads := map[string]renderedDoc{}
	for _, d := range docs {
		kind, name, _ := docMeta(d)
		switch kind {
		case "ConfigMap":
			configMaps[name] = ntf366Map(d["data"])
		case "Deployment":
			workloads[name] = d
		}
	}
	out := map[string]string{}
	for _, m := range ntf366Modules {
		w, ok := workloads[m.workload]
		if !ok {
			t.Fatalf("ФИКСТУРА: стек %s: рабочего объекта %s (модуль %s) в рендере нет — сравнивать нечего", stack, m.workload, m.module)
		}
		pod := ntf366Map(ntf366Map(ntf366Map(w["spec"])["template"])["spec"])
		val := ""
		for _, c := range ntf366List(pod["containers"]) {
			cm := ntf366Map(c)
			for _, ef := range ntf366List(cm["envFrom"]) {
				ref := ntf366Map(ntf366Map(ef)["configMapRef"])
				if v, ok := configMaps[fmt.Sprint(ref["name"])][m.knob]; ok {
					val = fmt.Sprint(v)
				}
			}
			for _, e := range ntf366List(cm["env"]) {
				em := ntf366Map(e)
				if em["name"] != m.knob {
					continue
				}
				if v, ok := em["value"]; ok {
					val = fmt.Sprint(v)
				}
				if ref := ntf366Map(ntf366Map(em["valueFrom"])["configMapKeyRef"]); ref != nil {
					val = fmt.Sprint(configMaps[fmt.Sprint(ref["name"])][fmt.Sprint(ref["key"])])
				}
			}
		}
		out[m.module] = val
	}
	return out
}

func ntf366Stacks(t *testing.T) []string {
	t.Helper()
	stacks := deployStacks(t)
	if len(stacks) == 0 {
		t.Fatalf("ФИКСТУРА: таблица стеков пуста — пустой обход вердиктом не является")
	}
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TestNTF366_ModuleFlagsAreDerivedFromOneDeclarationAndAnOverride — NTF3-66
// (флаги модулей) на каждом стеке таблицы stacks.txt.
func TestNTF366_ModuleFlagsAreDerivedFromOneDeclarationAndAnOverride(t *testing.T) {
	stacks := deployStacks(t)
	for _, name := range ntf366Stacks(t) {
		chain := stacks[name]
		t.Run(name, func(t *testing.T) {
			for _, c := range []struct {
				label string
				sets  []string
				want  map[string]string
			}{
				{"Given: global true, storage false", []string{ntf366GlobalKey + "=true", "storage.notifications.enabled=false"},
					map[string]string{"compute": "true", "nlb": "true", "registry": "true", "storage": "false", "vpc": "true"}},
				{"близнец: global true без переопределения", []string{ntf366GlobalKey + "=true"},
					map[string]string{"compute": "true", "nlb": "true", "registry": "true", "storage": "true", "vpc": "true"}},
				{"близнец: global false без переопределения", []string{ntf366GlobalKey + "=false"},
					map[string]string{"compute": "false", "nlb": "false", "registry": "false", "storage": "false", "vpc": "false"}},
			} {
				out, err := renderStack(t, chain, c.sets...)
				if err != nil {
					t.Fatalf("NTF3-66: стек %s, %s: рендер отказал: %v\n%s", name, c.label, err, out)
				}
				got := ntf366KnobValues(t, name, decodeRender(t, out))
				for _, m := range ntf366Modules {
					if got[m.module] != c.want[m.module] {
						shown := got[m.module]
						if shown == "" {
							shown = "<ручки нет>"
						}
						t.Errorf("NTF3-66: стек %s, %s: у %s %s=%s, ожидалось %s",
							name, c.label, m.workload, m.knob, shown, c.want[m.module])
					}
				}
			}
		})
	}
}

// TestNTF366_RenderRefusesWithoutTheGlobalDeclaration — NTF3-66: values без
// `global.kacho.notifications.enabled` — отказ рендера, называющий ключ.
// Близнец (фикстура) — тот же стек с объявлением рендерится.
func TestNTF366_RenderRefusesWithoutTheGlobalDeclaration(t *testing.T) {
	stacks := deployStacks(t)
	for _, name := range ntf366Stacks(t) {
		chain := stacks[name]
		t.Run(name, func(t *testing.T) {
			if out, err := renderStack(t, chain, ntf366GlobalKey+"=true"); err != nil {
				t.Fatalf("ФИКСТУРА (близнец NTF3-66): стек %s с %s=true не рендерится: %v\n%s", name, ntf366GlobalKey, err, out)
			}
			out, err := renderStack(t, chain, ntf366GlobalKey+"=null")
			if err == nil {
				t.Fatalf("NTF3-66: стек %s: рендер без %s прошёл — у флага модулей есть умолчание", name, ntf366GlobalKey)
			}
			if !strings.Contains(out, ntf366GlobalKey) {
				t.Fatalf("NTF3-66: стек %s: рендер без %s отказал, но сообщение ключа не называет:\n%s", name, ntf366GlobalKey, out)
			}
		})
	}
}
