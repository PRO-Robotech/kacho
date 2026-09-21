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

// reasons выбирает причины находок — сравнивается класс, а не текст.
func reasons(fs []identityPostureFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Reason)
	}
	return out
}

func TestOwnPostureForeignIdentityGate_FindsTheInheritedFlag(t *testing.T) {
	own := standPosture{IAM: "own", Edge: "own"}

	// ДЕФЕКТ: посадка своя, чужая служба унаследована включённой снизу, и в
	// ведомости остатка о ней не решал никто.
	got := judgeStandIdentity("own", own, []string{"kratos", "pg-kratos"}, nil)
	if len(got) != 1 || got[0].Reason != ownRaisesForeign {
		t.Fatalf("гейт не назвал стенд посадки `own` с нерешённой чужой службой: %v", reasons(got))
	}
	// Текст обязан называть КООРДИНАТУ: находка без имени включённого компонента
	// не говорит читающему, что именно выключать.
	for _, want := range []string{"own", "kratos", "pg-kratos"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("текст находки не называет %q: %s", want, got[0].Text)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ ПЕРВЫЙ: та же посадка, чужого не включено ничего.
	if f := judgeStandIdentity("own", own, nil, nil); len(f) != 0 {
		t.Errorf("гейт краснеет на законном близнеце (посадка `own` без чужой службы): %v", reasons(f))
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ ВТОРОЙ: то же включённое, но РЕШЁННОЕ ведомостью.
	decided := []identityRemainder{
		{Component: "kratos", Reason: "экран входа"},
		{Component: "pg-kratos", Reason: "его база"},
	}
	if f := judgeStandIdentity("own", own, []string{"kratos", "pg-kratos"}, decided); len(f) != 0 {
		t.Errorf("гейт краснеет на решённом остатке: %v", reasons(f))
	}
	// И ГРАНИЦА РЕШЁННОГО: решён один компонент, включено два — второй находка.
	f := judgeStandIdentity("own", own, []string{"kratos", "pg-kratos"}, decided[:1])
	if len(f) != 1 || f[0].Reason != ownRaisesForeign {
		t.Fatalf("ведомость на один компонент укрыла второй: %v", reasons(f))
	}
	if !strings.Contains(f[0].Text, "pg-kratos") {
		t.Errorf("находка не называет нерешённый компонент: %s", f[0].Text)
	}
}

// Запись ведомости, чей компонент на стенде уже выключен, — НАХОДКА. Без этой
// оси послабление пережило бы свой предмет и укрывало бы вернувшийся дефект.
func TestOwnPostureForeignIdentityGate_RefusesARemainderThatOutlivedItsSubject(t *testing.T) {
	own := standPosture{IAM: "own", Edge: "own"}

	got := judgeStandIdentity("own", own, nil, []identityRemainder{{Component: "kratos", Reason: "экран входа"}})
	if len(got) != 1 || got[0].Reason != remainderIsStale {
		t.Fatalf("ведомость, пережившая свой предмет, принята молча: %v", reasons(got))
	}
	if !strings.Contains(got[0].Text, "kratos") {
		t.Errorf("находка не называет запись: %s", got[0].Text)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: тот же компонент включён — запись при деле.
	if f := judgeStandIdentity("own", own, []string{"kratos"},
		[]identityRemainder{{Component: "kratos", Reason: "экран входа"}}); len(f) != 0 {
		t.Errorf("гейт краснеет на действующей записи ведомости: %v", reasons(f))
	}

	// Ведомость на стенде, который на `own` не стоит, — тоже находка: остаток
	// объявлен там, где посадку никто не переводил.
	external := standPosture{IAM: "external", Edge: "external"}
	if f := judgeStandIdentity("prod", external, []string{"kratos"},
		[]identityRemainder{{Component: "kratos", Reason: "экран входа"}}); len(f) != 1 ||
		f[0].Reason != remainderIsStale {
		t.Errorf("ведомость остатка принята на стенде посадки `external`: %v", reasons(f))
	}
}

func TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll(t *testing.T) {
	external := standPosture{IAM: "external", Edge: "external"}

	// ДЕФЕКТ ВТОРОЙ СТОРОНЫ: флаги сняли, посадку перевести забыли — стенду
	// проверять человека нечем.
	got := judgeStandIdentity("prod", external, nil, nil)
	if len(got) != 1 || got[0].Reason != externalRaisesNothing {
		t.Fatalf("гейт молчит о стенде посадки `external` без единого компонента чужой "+
			"службы личности — эту сторону предиката он и заводился держать: %v", reasons(got))
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же посадка, чужой поставщик поднят — так и решено.
	if f := judgeStandIdentity("prod", external, []string{"kratos"}, nil); len(f) != 0 {
		t.Errorf("гейт краснеет на законном близнеце (посадка `external` с поставщиком): %v", reasons(f))
	}
}

// Стенд, половины которого разошлись, по второй стороне НЕ судится: это предмет
// соседа (helm/umbrella/identity_posture_profiles_test.go). Ось держит границу —
// без неё два гейта вынесли бы два вердикта об одном предмете и разъехались.
func TestOwnPostureForeignIdentityGate_LeavesDisagreeingHalvesToItsNeighbour(t *testing.T) {
	mixed := standPosture{IAM: "own", Edge: "external"}
	if f := judgeStandIdentity("mixed", mixed, nil, nil); len(f) != 0 {
		t.Errorf("гейт высказался о стенде с разошедшимися половинами: %v", reasons(f))
	}
	// Половина на `own` с включённым чужим — находка и при расхождении: наша
	// полоса уже объявлена, вторая дверь рядом с ней решена не была.
	if f := judgeStandIdentity("mixed", mixed, []string{"hydra"}, nil); len(f) != 1 ||
		f[0].Reason != ownRaisesForeign {
		t.Errorf("гейт молчит о половине на `own` рядом с включённой чужой службой: %v", reasons(f))
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
	// Каждая запись ведомости остатка обязана называть компонент, КОТОРЫЙ ЕСТЬ
	// в выведенном составе: запись о том, чего состав не знает, не сработает
	// никогда и будет выглядеть исполненной.
	known := map[string]bool{}
	for _, c := range got {
		known[c.Name] = true
	}
	for stack, rs := range foreignIdentityRemainders {
		for _, r := range rs {
			if !known[r.Component] {
				t.Errorf("ведомость остатка стенда %q называет компонент %q, которого нет в "+
					"выведенном составе чужого (%s)", stack, r.Component, strings.Join(names, ", "))
			}
			if strings.TrimSpace(r.Reason) == "" {
				t.Errorf("запись ведомости %q/%q без причины — послабление без объяснения "+
					"переживает своего автора", stack, r.Component)
			}
		}
	}
}
