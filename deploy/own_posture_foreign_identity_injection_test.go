// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_foreign_identity_injection_test.go — ДОКАЗАТЕЛЬСТВО, ЧТО ГЕЙТ
// СПОСОБЕН УПАСТЬ, и упасть на воспроизведённом дефекте, а не на любом входе.
//
// Оси гоняют ТУ ЖЕ функцию, что и проверка по дереву (`judgeStandIdentity`), а
// не её копию: доказательство копии доказывает свойство копии.
//
// У каждого дефекта здесь есть ЗАКОННЫЙ БЛИЗНЕЦ — вход, отличающийся ровно
// одним фактом и находкой НЕ являющийся. Без близнеца «красное» не отличимо от
// «красное на всём».
package deploy_test

import (
	"strings"
	"testing"
)

func TestOwnPostureForeignIdentityGate_FindsTheInheritedFlag(t *testing.T) {
	own := standPosture{IAM: "own", Edge: "own"}

	// ДЕФЕКТ: посадка своя, чужая служба унаследована включённой снизу.
	got := judgeStandIdentity("own", own, []string{"kratos", "pg-kratos"})
	if got == nil {
		t.Fatal("гейт молчит о стенде посадки `own` с включённой чужой службой личности — " +
			"ровно тот вход, ради которого он заведён")
	}
	if got.Reason != ownRaisesForeign {
		t.Errorf("находка отнесена к %q, ожидалось %q", got.Reason, ownRaisesForeign)
	}
	// Текст обязан называть КООРДИНАТУ: находка без имени включённого компонента
	// не говорит читающему, что именно выключать.
	for _, want := range []string{"own", "kratos", "pg-kratos"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("текст находки не называет %q: %s", want, got.Text)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же посадка, чужого не включено ничего.
	if f := judgeStandIdentity("own", own, nil); f != nil {
		t.Errorf("гейт краснеет на законном близнеце (посадка `own` без чужой службы): %s", f.Text)
	}
}

func TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll(t *testing.T) {
	external := standPosture{IAM: "external", Edge: "external"}

	// ДЕФЕКТ ВТОРОЙ СТОРОНЫ: флаги сняли, посадку перевести забыли — стенду
	// проверять человека нечем.
	got := judgeStandIdentity("prod", external, nil)
	if got == nil {
		t.Fatal("гейт молчит о стенде посадки `external` без единого компонента чужой " +
			"службы личности — эту сторону предиката он и заводился держать")
	}
	if got.Reason != externalRaisesNothing {
		t.Errorf("находка отнесена к %q, ожидалось %q", got.Reason, externalRaisesNothing)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же посадка, чужой поставщик поднят — так и решено.
	if f := judgeStandIdentity("prod", external, []string{"kratos"}); f != nil {
		t.Errorf("гейт краснеет на законном близнеце (посадка `external` с поставщиком): %s", f.Text)
	}
}

// Стенд, половины которого разошлись, здесь НЕ судится: это предмет соседа
// (helm/umbrella/identity_posture_profiles_test.go). Ось держит границу — без
// неё два гейта вынесли бы два вердикта об одном предмете и разъехались.
func TestOwnPostureForeignIdentityGate_LeavesDisagreeingHalvesToItsNeighbour(t *testing.T) {
	mixed := standPosture{IAM: "own", Edge: "external"}
	if f := judgeStandIdentity("mixed", mixed, nil); f != nil {
		t.Errorf("гейт высказался о стенде с разошедшимися половинами: %s", f.Text)
	}
	// Половина на `own` с включённым чужим — находка и при расхождении: наша
	// полоса уже объявлена, вторая дверь рядом с ней решена не была.
	if f := judgeStandIdentity("mixed", mixed, []string{"hydra"}); f == nil {
		t.Error("гейт молчит о половине на `own` рядом с включённой чужой службой")
	}
}

// Состав чужого выводится из дерева, а не выписан в гейте: перепись обязана
// находить обе службы поставщика И их базы. Ось падает при переименовании,
// вместо того чтобы молча сузить обход.
func TestOwnPostureForeignIdentityGate_DerivesTheForeignComponentsFromTheTree(t *testing.T) {
	got := foreignIdentityComponents(t)
	names := make([]string, 0, len(got))
	for _, c := range got {
		names = append(names, c.Name)
	}
	t.Logf("перепись состава: компонентов %d (%s)", len(got), strings.Join(names, ", "))

	var services, databases int
	for _, c := range got {
		if strings.HasPrefix(c.Name, "pg-") {
			databases++
			continue
		}
		services++
	}
	if services < 2 {
		t.Errorf("служб поставщика выведено %d (%s) — признак репозитория %q перестал их "+
			"узнавать; гейт судил бы сузившийся обход, не сказав об этом",
			services, strings.Join(names, ", "), foreignIdentityRepoMark)
	}
	if databases < 2 {
		t.Errorf("баз поставщика выведено %d (%s) — база включается СВОИМ флагом и без него "+
			"остаётся стоять при выключенной службе", databases, strings.Join(names, ", "))
	}
	for _, c := range got {
		if len(c.Flag) < 2 || c.Flag[len(c.Flag)-1] != "enabled" {
			t.Errorf("у компонента %s путь флага не оканчивается ключом включения: %v", c.Name, c.Flag)
		}
	}
}
