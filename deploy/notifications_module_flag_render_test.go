//go:build helmcharts

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notifications_module_flag_render_test.go — полоса RED S1-A4 issue-2918 (NTF-3,
// Н3-Ф2), часть в чарте: ручка KACHO_<MODULE>_NOTIFICATIONS_ENABLED каждого из
// пяти модулей kacho выводится из одного объявления
// `global.kacho.notifications.enabled` и переопределения модуля
// `global.kacho.notifications.modules.<модуль>.enabled` (замысел З11; помощник
// `enabledFor` — полоса D2).
//
// Форма переопределения — та, что объявил механизм флага NTF-1 (Р9, CX1-106):
// под `global`, потому что тот же вывод читает чарт notify (перечень
// источников), а ключ под подчартом модуля ему не виден. Приёмка NTF-3
// (редакция 7) пишет переопределение `storage.notifications.enabled` — этот
// путь механизм NTF-1 отвергает как второй путь к значению (CX1-111 (а)), и
// проба держит ОБА факта: переопределение под `global` выключает модуль,
// ключ под подчартом — отказ рендера с полным путём.
//
// Сценарий приёмки NTF-3 (отпечаток ac1f9fc9…) NTF3-66 — в части флагов модулей:
//   - Given: global true, `global.kacho.notifications.modules.storage.enabled:
//     false` → у storage KACHO_STORAGE_NOTIFICATIONS_ENABLED=false, у compute,
//     nlb, registry, vpc — true;
//   - близнец: без переопределения — true у всех пяти;
//   - близнец: global false без переопределения — false у всех пяти;
//   - values без `global.kacho.notifications.enabled` — рендер отказывает с
//     сообщением, называющим ключ (умолчания нет);
//   - собственный ключ флага под подчартом модуля — отказ рендера с полным путём.
// Перечень источников notify, записи sourceLimits и допуск notify-sender в
// политике vpc (остаток NTF3-66) — предмет полосы чарта notify, не этой пробы.
//
// Значение ручки берётся так, как его получит процесс: `env[].value`,
// `env[].valueFrom.configMapKeyRef` либо `envFrom[].configMapRef` карты рендера.

import (
	"fmt"
	"sort"
	"testing"
)

const ntf366GlobalKey = "global.kacho.notifications.enabled"

// ntf366ModulesKey — узел переопределений модулей.
const ntf366ModulesKey = "global.kacho.notifications.modules"

// ntf366Modules — модуль → (рабочий объект рендера, ручка флага, собственный
// ключ флага под подчартом модуля).
var ntf366Modules = []struct{ module, workload, knob, ownKey string }{
	{"compute", "compute", "KACHO_COMPUTE_NOTIFICATIONS_ENABLED", "compute.notifications.enabled"},
	{"nlb", "kacho-nlb", "KACHO_NLB_NOTIFICATIONS_ENABLED", "kacho-nlb.notifications.enabled"},
	{"registry", "registry", "KACHO_REGISTRY_NOTIFICATIONS_ENABLED", "registry.notifications.enabled"},
	{"storage", "kacho-storage", "KACHO_STORAGE_NOTIFICATIONS_ENABLED", "storage.notifications.enabled"},
	{"vpc", "vpc", "KACHO_VPC_NOTIFICATIONS_ENABLED", "vpc.notifications.enabled"},
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
	stacks := deployStacksForRender(t, renderGateOperatorSample)
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
	stacks := deployStacksForRender(t, renderGateOperatorSample)
	for _, name := range ntf366Stacks(t) {
		chain := stacks[name]
		t.Run(name, func(t *testing.T) {
			for _, c := range []struct {
				label string
				sets  []string
				want  map[string]string
			}{
				{"Given: global true, storage false", []string{ntf366GlobalKey + "=true", ntf366ModulesKey + ".storage.enabled=false"},
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
// Близнец (фикстура) — та же цепочка той же копии с объявлением рендерится.
//
// Ключ снимается в КОПИИ values.yaml зонтика, а не набором `--set …=null`:
// под `global` helm v4.2.4 нулевое значение слоя не удаляет умолчание зонтика
// (замер полосы D2, deploy/notifications_flag_test.go TestNTF1N01_…), и рендер
// с `=null` проходит с `enabled: true` — такое отрицание проверяло бы
// фикстуру, а не продукт.
func TestNTF366_RenderRefusesWithoutTheGlobalDeclaration(t *testing.T) {
	requireHelmNTF(t)
	declared := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	unset := notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"values.yaml": replaceOnce("    notifications:\n      enabled: true\n", "    notifications:\n"),
	}})
	refused := 0
	names := ntf366Stacks(t)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if out, err := declared.render(declared.chainFiles(t, name)); err != nil {
				t.Fatalf("ФИКСТУРА (близнец NTF3-66): стек %s с объявленным %s не рендерится: %v\n%s", name, ntf366GlobalKey, err, lastLines(out, 6))
			}
			out, err := unset.render(unset.chainFiles(t, name))
			if rerr := ntfRefusalNames(out, err, ntf366GlobalKey); rerr != nil {
				t.Fatalf("NTF3-66: стек %s без %s: %v — у флага модулей есть умолчание", name, ntf366GlobalKey, rerr)
			}
			refused++
		})
	}
	t.Logf("перепись NTF3-66 (без ключа): стеков %d; отказ с именем ключа у %d", len(names), refused)
}

// TestNTF366_ModuleOwnFlagKeyIsRefused — собственный ключ флага под подчартом
// модуля (`storage.notifications.enabled` и т. п.) — второй путь к значению:
// отказ рендера с полным путём (CX1-111 (а)). Близнец — тот же стек без
// ключа рендерится (TestNTF366_ModuleFlagsAreDerived…).
func TestNTF366_ModuleOwnFlagKeyIsRefused(t *testing.T) {
	stacks := deployStacksForRender(t, renderGateOperatorSample)
	chain, ok := stacks["dev"]
	if !ok {
		t.Fatalf("ФИКСТУРА: стека dev в таблице стеков нет")
	}
	if out, err := renderStack(t, chain, ntf366GlobalKey+"=true"); err != nil {
		t.Fatalf("ФИКСТУРА (близнец): стек dev не рендерится: %v\n%s", err, out)
	}
	for _, m := range ntf366Modules {
		out, err := renderStack(t, chain, ntf366GlobalKey+"=true", m.ownKey+"=false")
		if rerr := ntfRefusalNames(out, err, m.ownKey); rerr != nil {
			t.Errorf("NTF3-66: собственный ключ флага %s модуля %s: %v — второй путь к значению принят", m.ownKey, m.module, rerr)
		}
	}
}
