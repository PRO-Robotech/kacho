// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registry_token_lane_sides_agree_test.go — у докерной полосы выдачи ДВЕ
// стороны, и объявления обеих обязаны сойтись (задача #1184).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Реестр называет докер-клиенту имя своей службы (`registry.serviceAud`) в
// вызове на аутентификацию; клиент возвращает это же имя в `?service=`; iam
// чеканит удостоверение адресату, которого объявил сам
// (`kacho-iam.config.apiServer.registryToken.service`). Пока iam адресата не
// сверял, эти два объявления могли расходиться и расхождение было НЕВИДИМО:
// клиент echo-ит то, что услышал от реестра, реестр это и ожидает, а iam
// чеканил что попросят.
//
// Теперь iam чеканит только объявленному адресату — и разошедшиеся объявления
// означают отказ во входе докера. Свойство, обязательное для одной стороны,
// утверждается СРАВНЕНИЕМ сторон, а не по каждой отдельно: обе по отдельности
// валидны, неверна их РАЗНИЦА, и решал её не оператор, а умолчания двух разных
// чартов.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ТРЕБУЕТСЯ ОБЪЯВЛЕННОСТЬ, А НЕ РАЗБОР УМОЛЧАНИЙ
//
// Умолчания живут в двух местах: чарт реестра несёт своё, а у iam чарт отдаёт
// пустое и величину подставляет процесс — в дереве, закрытом для этого пакета
// (`services/iam/internal/...`). Выписав сюда копию, мы завели бы третье место
// об одном предмете, и оно разошлось бы с процессом молча.
//
// Поэтому предикат другой и он от констант свободен: профиль, поднимающий
// полосу, объявляет ОБЕ стороны сам. Тогда сверять есть что, и сверка не
// зависит от того, чьё умолчание сегодня какое.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА
//
//   - профиль, не поднимающий реестр, ничего объявлять не обязан;
//   - проверяется РАВЕНСТВО двух объявлений, а не их содержание: какое имя у
//     стенда — решение профиля;
//   - перечень профилей берётся КАТАЛОГОМ.
package deploy_test

import (
	"sort"
	"strings"
	"testing"
)

// laneSideFinding — одна находка с координатой, по которой её чинят.
type laneSideFinding struct {
	profile string
	what    string
}

func (f laneSideFinding) String() string { return f.profile + ": " + f.what }

// scanRegistryLaneSides — ядро проверки. Возвращает находки И число профилей, у
// которых полоса поднята: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
func scanRegistryLaneSides(profiles map[string]map[string]any) (findings []laneSideFinding, serving, inherited int) {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		reg, _ := dig(profiles[name], "registry").(map[string]any)
		if reg == nil {
			continue
		}
		if on, present := reg["enabled"].(bool); present && !on {
			continue
		}
		iamOn, _ := dig(profiles[name], "kacho-iam").(map[string]any)
		if iamOn == nil {
			continue
		}
		serving++

		regSide, _ := reg["serviceAud"].(string)
		regSide = strings.TrimSpace(regSide)
		iamSide, _ := dig(profiles[name], "kacho-iam", "config", "apiServer", "registryToken", "service").(string)
		iamSide = strings.TrimSpace(iamSide)

		switch {
		case regSide == "" && iamSide == "":
			// Ни одна сторона не объявлена: имя службы приезжает из умолчаний
			// ДВУХ разных чартов, и одно из них живёт в дереве, закрытом для
			// этого пакета. Судить их ЗДЕСЬ нечем — выписанная сюда копия была
			// бы третьим местом об одном предмете и разошлась бы молча.
			//
			// Считается и печатается ОТДЕЛЬНОЙ величиной: пропуск, невидимый в
			// переписи, неотличим от проверенного. Согласование самих умолчаний
			// — свой предмет со своим владельцем (тенант-фейсинг DNS реестра).
			inherited++
		case regSide == "":
			findings = append(findings, laneSideFinding{name,
				"объявлена только сторона iam (" + iamSide + "); registry.serviceAud унаследован от умолчания чарта"})
		case iamSide == "":
			findings = append(findings, laneSideFinding{name,
				"объявлена только сторона реестра (" + regSide + "); " +
					"kacho-iam.config.apiServer.registryToken.service унаследован от умолчания процесса"})
		case regSide != iamSide:
			findings = append(findings, laneSideFinding{name,
				"стороны докерной полосы разошлись: реестр называет " + regSide +
					", iam чеканит " + iamSide + " — вход докера был бы отвергнут"})
		}
	}
	return findings, serving, inherited
}

// TestRegistryTokenLaneSidesAgree — сама проверка.
func TestRegistryTokenLaneSidesAgree(t *testing.T) {
	files := profileFiles(t)
	profiles := make(map[string]map[string]any, len(files))
	for _, f := range files {
		profiles[f] = readYAML(t, f)
	}

	findings, serving, inherited := scanRegistryLaneSides(profiles)
	t.Logf("перепись: профилей осмотрено %d · поднимают полосу %d · судимо здесь %d · "+
		"обе стороны унаследованы от умолчаний двух чартов %d (свой предмет, см. заголовок файла) · находок %d",
		len(files), serving, serving-inherited, inherited, len(findings))

	if serving == 0 {
		t.Fatal("ни один профиль не поднимает докерную полосу — проверке нечего сравнивать")
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// TestRegistryLaneSidesScannerSeesEachOmissionAndIsSilentOnAgreement —
// инъекция по КАЖДОЙ оси плюс законный близнец.
//
// Оси разведены намеренно: одна проба, снимающая всё разом, зеленела бы на
// починке любой одной стороны.
func TestRegistryLaneSidesScannerSeesEachOmissionAndIsSilentOnAgreement(t *testing.T) {
	profile := func(regSide, iamSide string) map[string]any {
		reg := map[string]any{"enabled": true}
		if regSide != "" {
			reg["serviceAud"] = regSide
		}
		rt := map[string]any{}
		if iamSide != "" {
			rt["service"] = iamSide
		}
		return map[string]any{
			"registry": reg,
			"kacho-iam": map[string]any{"config": map[string]any{
				"apiServer": map[string]any{"registryToken": rt},
			}},
		}
	}

	// (а) законный близнец — молчание.
	got, serving, inherited := scanRegistryLaneSides(map[string]map[string]any{
		"agree.yaml": profile("registry.kacho.local", "registry.kacho.local"),
	})
	if len(got) != 0 || serving != 1 || inherited != 0 {
		t.Fatalf("на сошедшихся сторонах сканер обязан молчать, получено %v (поднимающих %d)", got, serving)
	}

	// (б) четыре оси, каждая отдельно.
	for name, p := range map[string]map[string]any{
		"reg-only.yaml": profile("registry.kacho.local", ""),
		"iam-only.yaml": profile("", "registry.kacho.local"),
		"drift.yaml":    profile("registry.in-cloud.io", "registry.kacho.local"),
	} {
		got, serving, inherited = scanRegistryLaneSides(map[string]map[string]any{name: p})
		if len(got) != 1 || serving != 1 || inherited != 0 {
			t.Fatalf("%s: ось обязана быть находкой, получено %v (поднимающих %d, унаследованных %d)",
				name, got, serving, inherited)
		}
		if !strings.Contains(got[0].String(), name) {
			t.Fatalf("%s: находка не называет координату: %s", name, got[0])
		}
	}

	// (в) обе стороны унаследованы — НЕ находка, но и не «судимо»: величина
	// переписи своя, иначе пропуск неотличим от проверенного.
	got, serving, inherited = scanRegistryLaneSides(map[string]map[string]any{
		"inherited.yaml": profile("", ""),
	})
	if len(got) != 0 || serving != 1 || inherited != 1 {
		t.Fatalf("унаследованные стороны обязаны считаться отдельно, получено %v (поднимающих %d, унаследованных %d)",
			got, serving, inherited)
	}

	// (г) предмета нет — не находка: профиль, не поднимающий реестр.
	got, serving, inherited = scanRegistryLaneSides(map[string]map[string]any{
		"off.yaml": {"registry": map[string]any{"enabled": false}},
	})
	if len(got) != 0 || serving != 0 || inherited != 0 {
		t.Fatalf("выключенный реестр — не предмет, получено %v (поднимающих %d, унаследованных %d)",
			got, serving, inherited)
	}
}
