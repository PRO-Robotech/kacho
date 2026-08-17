// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleoneofbranchprobe_test.go — у каждого реестра консоли, создающего
// ресурс с ветвлением контракта, есть проба покрытия ветвей.
//
// # Предмет
//
// Ветвь `oneof`, объявленная контрактом и не имеющая выражения в форме, не
// работает НИ ПРИ КАКОМ вводе: возможность задокументирована, покрыта типами и
// недостижима. Проверять это на уровне имён и тела умеют пробы модулей —
// они держат в руках настоящий объект реестра. Чего проба модуля не умеет —
// увидеть СОСЕДНИЙ модуль.
//
// # Почему гейт про НАЛИЧИЕ пробы, а не про сами ветви
//
// Реестр ресурсов у консоли не один: `shared` обслуживает vpc/iam/system, а
// `compute`, `nlb`, `registry`, `storage` несут свои. Форма, которую видит
// пользователь, берётся из реестра ТОГО модуля, чей маршрут открыт. Именно
// поэтому четыре ветви проверки живости считались закрытыми: их завели в
// `shared`, а `/nlb/*` рисует модуль `nlb`, где их не было, — и ни одна проба
// этого не видела, потому что каждая смотрела в свой реестр.
//
// Сами ветви сверяются пробами модулей (`oneof-form-coverage*.test.ts`): там
// это делается по настоящему объекту, а не по тексту, и потому точно. Гейт
// требует, чтобы такая проба СУЩЕСТВОВАЛА у каждого реестра, который создаёт
// ресурс с ветвлением, и называла его спек. Без него молчание модуля неотличимо
// от его исправности.
//
// # Объём осмотренного
//
// Гейт печатает перепись на каждом прогоне: файлов контракта прочитано,
// мутирующих маршрутов найдено, реестров прочитано, создаваемых спеков с
// ветвлением, проб распознано. «Ноль находок» обязано быть отличимо от «ноль
// прочитанного», а пустой корпус — отказ, а не молчаливый успех.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consoleoneofbranchprobe_injection_test.go`.

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// probeCall — признак пробы покрытия: она читает КОНТРАКТ, а не пересказывает
// его. Проба, выписавшая ветви руками, устаревает молча.
const probeCall = "oneofBranches("

func TestConsoleCreatableSpecWithBranchingHasCoverageProbe(t *testing.T) {
	root := repoRoot(t)
	protoDir := filepath.Join(root, "proto", "kacho")
	uiDir := filepath.Join(root, "ui-future")

	// ── контракт: маршруты мутаций и достижимые из них ветвления ─────────────
	messages := map[string][]protoField{}
	creates := []protoCreate{}
	protoFiles := 0
	if err := rootedWalk(protoDir,
		func(rel string) bool { return strings.HasSuffix(rel, ".proto") },
		func(_ string, body []byte) error {
			protoFiles++
			pf := parseProtoFile(string(body))
			for k, v := range pf.Messages {
				messages[k] = append(messages[k], v...)
			}
			creates = append(creates, pf.Creates...)
			return nil
		}); err != nil {
		t.Fatalf("обход контрактов: %v", err)
	}
	if protoFiles == 0 {
		t.Fatalf("прочитано 0 файлов контракта в %s — гейту нечего рассматривать, "+
			"и его молчание означало бы «ноль прочитанного», а не «ноль находок»", protoDir)
	}

	// Маршрут → ветвления, достижимые из тела его запроса.
	branchingsByPath := map[string][]string{}
	for _, c := range creates {
		if !strings.HasPrefix(c.RPC, "Create") {
			continue
		}
		if _, ok := messages[c.Request]; !ok {
			continue
		}
		b := branchingsReachable(messages, c.Request)
		if len(b) > 0 {
			branchingsByPath[c.Path] = b
		}
	}
	if len(branchingsByPath) == 0 {
		t.Fatalf("в контракте не нашлось ни одного маршрута создания с группой `oneof` — "+
			"разбор перестал видеть предмет (файлов прочитано %d)", protoFiles)
	}

	// ── консоль: реестры и создаваемые ими спеки ─────────────────────────────
	var specs []consoleSpec
	registries := 0
	if err := rootedWalk(uiDir,
		func(rel string) bool { return strings.HasSuffix(rel, "/src/lib/resource-registry.tsx") },
		func(abs string, body []byte) error {
			registries++
			rel, err := filepath.Rel(uiDir, abs)
			if err != nil {
				return err
			}
			specs = append(specs, parseConsoleRegistry(moduleOfRegistry(rel), rel, string(body))...)
			return nil
		}); err != nil {
		t.Fatalf("обход реестров консоли: %v", err)
	}
	if registries == 0 {
		t.Fatalf("прочитано 0 реестров ресурсов в %s — предмета у гейта нет", uiDir)
	}

	// ── пробы покрытия по модулям ────────────────────────────────────────────
	probesByModule := map[string][]string{} // модуль → тела проб
	probeFiles := 0
	if err := rootedWalk(uiDir,
		func(rel string) bool {
			return strings.Contains(rel, "oneof-form-coverage") && strings.HasSuffix(rel, ".test.ts")
		},
		func(abs string, body []byte) error {
			if !strings.Contains(string(body), probeCall) {
				return nil
			}
			probeFiles++
			rel, err := filepath.Rel(uiDir, abs)
			if err != nil {
				return err
			}
			m := moduleOfRegistry(rel)
			probesByModule[m] = append(probesByModule[m], string(body))
			return nil
		}); err != nil {
		t.Fatalf("обход проб покрытия: %v", err)
	}
	if probeFiles == 0 {
		t.Fatalf("не найдено ни одной пробы покрытия ветвей (`%s` в файле `*oneof-form-coverage*.test.ts`) — "+
			"либо их снесли, либо признак пробы разошёлся с деревом", probeCall)
	}

	// ── сверка ───────────────────────────────────────────────────────────────
	watched, findings := 0, []string{}
	for _, s := range specs {
		if !s.Creatable {
			continue
		}
		if _, ok := branchingsByPath[s.APIPath]; !ok {
			continue
		}
		watched++
		if !coveredBySomeProbe(probesByModule[s.Module], s.ID) {
			findings = append(findings, s.Module+":"+s.ID+" ("+s.File+", маршрут "+s.APIPath+")")
		}
	}

	sort.Strings(findings)
	t.Logf("перепись: контрактов %d, маршрутов создания с ветвлением %d, реестров %d, "+
		"создаваемых спеков с ветвлением %d, проб покрытия %d, находок %d",
		protoFiles, len(branchingsByPath), registries, watched, probeFiles, len(findings))

	if watched == 0 {
		t.Fatalf("ни один создаваемый спек консоли не сопоставлен маршруту с ветвлением — "+
			"сопоставление по `apiPath` перестало работать, и гейт молчал бы на любом дереве "+
			"(реестров %d, маршрутов %d)", registries, len(branchingsByPath))
	}
	if len(findings) > 0 {
		t.Errorf("реестр создаёт ресурс, чей контракт несёт группу `oneof`, но пробы покрытия ветвей "+
			"у этого модуля нет — ветвь, невыразимая ЕГО формой, останется незамеченной, пока сосед "+
			"зеленеет:\n  %s", strings.Join(findings, "\n  "))
	}
}

// coveredBySomeProbe — проба модуля называет спек и читает контракт.
func coveredBySomeProbe(bodies []string, specID string) bool {
	for _, b := range bodies {
		if strings.Contains(b, `"`+specID+`"`) {
			return true
		}
	}
	return false
}

// repoRootExists — предпосылка гейта: дерево консоли и контракта на месте.
func TestConsoleOneofProbeGatePremise(t *testing.T) {
	root := repoRoot(t)
	for _, p := range []string{
		filepath.Join(root, "proto", "kacho"),
		filepath.Join(root, "ui-future"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("предпосылка гейта не выполняется: %s не читается (%v). "+
				"Пока её нет, вердикт гейта ничего не означает", p, err)
		}
	}
}
