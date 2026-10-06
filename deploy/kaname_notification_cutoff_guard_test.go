// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_notification_cutoff_guard_test.go — ПОЛОСА ОТСЕЧКИ ВЫДАЧИ УВЕДОМЛЕНИЙ
// ДОЕЗЖАЕТ ДО СЛУЖБЫ ДОСТУПА НА КАЖДОМ СТЕКЕ (kacho#2918).
//
// Пин службы `745640d6c296` (kaname#484, приёмка NTF-1 Р5, NTF1-F20) собирает
// службу выдачи уведомлений БЕЗУСЛОВНО, и её строитель без ключа
// `notifications.cutoff-guard` отказывает в старте. Блок «Обязательные
// величины» INSTALL.md пина этого ключа не называет (страж старта его не
// судит — судит строитель), поэтому TestPinnedRequiredSettingsReachTheServiceOnEveryStack
// его не видит: без этой пробы подъём пина дал бы стенд, где под службы
// доступа не поднимается ни при каком профиле.
//
// Что утверждается: на каждом стеке `deploy/stacks.txt` рендер подчарта службы
// пишет `notifications.cutoff-guard` непустой строкой; незаданная, нулевая и
// не-длительность — отказ рендера, называющий ручку. Годность величины по
// границе [1s..10m] судит строитель службы, не эта проба.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const cutoffGuardKnob = "config.notifications.cutoffGuard"

// TestKanameCutoffGuardReachesTheServiceOnEveryStack — доставка по всем стекам.
func TestKanameCutoffGuardReachesTheServiceOnEveryStack(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	base, err := filepath.Abs(umbrellaDir)
	if err != nil {
		t.Fatal(err)
	}
	judged := 0
	for _, name := range names {
		body, err := yaml.Marshal(stackIdentityValues(t, stacks[name]))
		if err != nil {
			t.Fatalf("стек %s: значения подчарта не сериализуются: %v", name, err)
		}
		file := filepath.Join(t.TempDir(), name+".yaml")
		if err := os.WriteFile(file, body, 0o600); err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(base, file)
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := renderIdentitySubchart(t, []string{rel})
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы не удался: %v\n%s", name, err, rendered)
		}
		judged++
		v, ok := lookup(kanameServiceConfig(t, rendered), "notifications", "cutoff-guard")
		s, _ := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			t.Errorf("стек %s: `notifications.cutoff-guard` рендер не доставляет (получено %#v) — "+
				"строитель службы выдачи откажет в старте (NTF1-F20)", name, v)
		}
	}
	if judged == 0 {
		t.Fatal("ни одного стека не осмотрено — вердикта нет")
	}
	t.Logf("перепись: стеков осмотрено %d", judged)
}

// TestKanameCutoffGuardRefusalNamesTheKnob — незаданная, нулевая и
// не-длительность отказывают рендером с именем ручки; законный близнец
// (`45s`) рендерится дословно.
func TestKanameCutoffGuardRefusalNamesTheKnob(t *testing.T) {
	for _, bad := range []string{"null", "0s", "abc"} {
		out, err := renderIdentitySubchart(t, nil, cutoffGuardKnob+"="+bad)
		if err == nil {
			t.Errorf("%s=%s: рендер обязан отказать, а прошёл", cutoffGuardKnob, bad)
			continue
		}
		if !strings.Contains(out, cutoffGuardKnob) {
			t.Errorf("%s=%s: отказ не называет ручку:\n%s", cutoffGuardKnob, bad, out)
		}
	}
	out, err := renderIdentitySubchart(t, nil, cutoffGuardKnob+"=45s")
	if err != nil {
		t.Fatalf("законный близнец 45s обязан рендериться: %v\n%s", err, out)
	}
	v, _ := lookup(kanameServiceConfig(t, out), "notifications", "cutoff-guard")
	if v != "45s" {
		t.Errorf("законный близнец: `notifications.cutoff-guard` = %#v, ожидалось \"45s\"", v)
	}
}
