// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_foreign_identity_injection_test.go — ДОКАЗАТЕЛЬСТВО, ЧТО ГЕЙТ
// СПОСОБЕН УПАСТЬ, и упасть на воспроизведённом дефекте, а не на любом входе.
//
// Оси гоняют ТЕ ЖЕ функции, что и проверка по дереву (`judgeStandIdentity`,
// `judgeForeignIdentityInTree`, `foreignIdentityComponentsIn`), а не их копии:
// доказательство копии доказывает свойство копии.
//
// У каждого дефекта здесь есть ЗАКОННЫЙ БЛИЗНЕЦ — вход, отличающийся ровно
// одним фактом и находкой НЕ являющийся. Без близнеца «красное» не отличимо от
// «красное на всём».
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reasons выбирает причины находок — сравнивается класс, а не текст.
func reasons(fs []identityPostureFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Reason)
	}
	return out
}

func TestOwnPostureForeignIdentityGate_FindsTheInheritedFlag(t *testing.T) {
	own := identityLanding{IAM: landingOwn, Edge: landingOwn}

	// ДЕФЕКТ: посадка своя, чужая служба унаследована включённой снизу.
	got := judgeStandIdentity("own", own, []string{"kratos", "pg-kratos"})
	if len(got) != 1 || got[0].Reason != ownRaisesForeign {
		t.Fatalf("гейт не назвал стенд посадки `own` с включённой чужой службой: %v", reasons(got))
	}
	// Текст обязан называть КООРДИНАТУ: находка без имени включённого компонента
	// не говорит читающему, что именно выключать.
	for _, want := range []string{"own", "kratos", "pg-kratos"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("текст находки не называет %q: %s", want, got[0].Text)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же посадка, чужого не включено ничего.
	if f := judgeStandIdentity("own", own, nil); len(f) != 0 {
		t.Errorf("гейт краснеет на законном близнеце (посадка `own` без чужой службы): %v", reasons(f))
	}
}

func TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll(t *testing.T) {
	external := identityLanding{IAM: landingExternal, Edge: landingExternal}

	// ДЕФЕКТ СТОРОНЫ СТЕНДА: поставщика в дереве нет, посадку перевести забыли —
	// стенду проверять человека нечем.
	got := judgeStandIdentity("prod", external, nil)
	if len(got) != 1 || got[0].Reason != externalRaisesNothing {
		t.Fatalf("гейт молчит о стенде посадки `external` без единого компонента чужой "+
			"службы личности — эту сторону предиката он и заводился держать: %v", reasons(got))
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же посадка, поставщик поднят — стенду есть чем
	// проверять человека (сторона дерева судит сам компонент отдельно).
	if f := judgeStandIdentity("prod", external, []string{"kratos"}); len(f) != 0 {
		t.Errorf("гейт краснеет на законном близнеце (посадка `external` с поставщиком): %v", reasons(f))
	}
}

// Стенд, половины которого разошлись, по стороне стенда НЕ судится: это предмет
// соседа (helm/umbrella/identity_posture_profiles_test.go). Ось держит границу —
// без неё два гейта вынесли бы два вердикта об одном предмете и разъехались.
func TestOwnPostureForeignIdentityGate_LeavesDisagreeingHalvesToItsNeighbour(t *testing.T) {
	mixed := identityLanding{IAM: landingOwn, Edge: landingExternal}
	if f := judgeStandIdentity("mixed", mixed, nil); len(f) != 0 {
		t.Errorf("гейт высказался о стенде с разошедшимися половинами: %v", reasons(f))
	}
	// Половина на `own` с включённым чужим — находка и при расхождении: наша
	// полоса уже объявлена, вторая дверь рядом с ней решена не была.
	if f := judgeStandIdentity("mixed", mixed, []string{"hydra"}); len(f) != 1 ||
		f[0].Reason != ownRaisesForeign {
		t.Errorf("гейт молчит о половине на `own` рядом с включённой чужой службой: %v", reasons(f))
	}
}

// writeInjectedFile — файл копии зонта; каталоги создаются по пути.
func writeInjectedFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// foreignInjectionUmbrella — копия зонта в форме, которую читает обход: одна
// наша зависимость, одна зависимость с репозиторием `repo` и её база алиасом, и
// подчарт в charts/ без объявления с именем `screen`.
func foreignInjectionUmbrella(t *testing.T, repo, screen string) string {
	t.Helper()
	dir := t.TempDir()
	writeInjectedFile(t, filepath.Join(dir, "Chart.yaml"), `apiVersion: v2
name: kacho-umbrella
version: 0.1.0
dependencies:
  - name: kaname
    version: 0.1.0
    repository: file://./charts/kaname
  - name: kratos
    version: 0.62.1
    repository: `+repo+`
    condition: kratos.enabled
  - name: postgresql
    alias: pg-kratos
    version: 13.4.4
    repository: https://charts.bitnami.com/bitnami
    condition: pg-kratos.enabled
`)
	writeInjectedFile(t, filepath.Join(dir, "charts", "kaname", "Chart.yaml"),
		"apiVersion: v2\nname: kaname\nversion: 0.1.0\n")
	writeInjectedFile(t, filepath.Join(dir, "charts", "screen", "Chart.yaml"),
		"apiVersion: v2\nname: "+screen+"\nversion: 0.1.0\n")
	writeInjectedFile(t, filepath.Join(dir, "charts", "screen", "values.yaml"),
		"screenUI:\n  enabled: false\n")
	return dir
}

// Вернувшийся в зонт компонент чужой службы — находка стороны дерева: объявление
// с репозиторием поставщика, его база и экран входа в charts/ без объявления.
// Законный близнец — та же копия, где репозиторий и имя подчарта наши: ноль.
func TestOwnPostureForeignIdentityGate_FindsAComponentReturnedToTheTree(t *testing.T) {
	returned := foreignInjectionUmbrella(t, "https://k8s.ory.sh/helm/charts", "kratos-selfservice-ui")
	got, walked := foreignIdentityComponentsIn(t, returned)
	names := make([]string, 0, len(got))
	for _, c := range got {
		names = append(names, c.Name)
	}
	t.Logf("вернувшийся стек: зависимостей %d · подчартов без объявления %d · компонентов %d (%s)",
		walked.Dependencies, walked.Subcharts, len(got), strings.Join(names, ", "))
	want := map[string]bool{"kratos": false, "pg-kratos": false, "kratos-selfservice-ui": false}
	for _, c := range got {
		if _, ok := want[c.Name]; !ok {
			t.Errorf("обход назвал компонентом чужой службы %s — его нет в инъекции", c)
		}
		want[c.Name] = true
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("обход не нашёл вернувшийся компонент %q (найдено: %s)", n, strings.Join(names, ", "))
		}
	}
	if f := judgeForeignIdentityInTree(got); len(f) != len(want) {
		t.Errorf("сторона дерева назвала %d находок на %d вернувшихся компонентах: %v",
			len(f), len(want), reasons(f))
	}
	for _, c := range got {
		if c.Undeclared && strings.Join(c.Flag, ".") != "kratos-selfservice-ui.screenUI.enabled" {
			t.Errorf("путь флага подчарта без объявления выведен неверно: %v", c.Flag)
		}
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ — каждый отличается ОДНИМ фактом, и каждый снимает ровно
	// свою часть находок.
	for _, c := range []struct {
		name, repo, screen string
		want               []string
	}{
		{"репозиторий зависимости не поставщика", "https://charts.bitnami.com/bitnami",
			"kratos-selfservice-ui", []string{"kratos-selfservice-ui"}},
		{"имя подчарта без объявления не поставщика", "https://k8s.ory.sh/helm/charts",
			"kacho-screen", []string{"kratos", "pg-kratos"}},
	} {
		got, _ := foreignIdentityComponentsIn(t, foreignInjectionUmbrella(t, c.repo, c.screen))
		var have []string
		for _, comp := range got {
			have = append(have, comp.Name)
		}
		if strings.Join(have, ",") != strings.Join(c.want, ",") {
			t.Errorf("близнец «%s»: обход назвал %v, ожидалось %v", c.name, have, c.want)
		}
	}
}
