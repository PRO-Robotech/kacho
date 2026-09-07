// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// presented_credential_declaration_test.go — семейство LAND приёмки KAN-AUTHN-1
// (задача продукта #2191).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Собственный публичный REST-фронт службы поднимается на ЛЮБОЙ посадке — его
// поднимает объявленный адрес, а не выбор посадки. Всякий, кто до него
// дотянулся, приходит обычным клиентом: модульного сертификата у арендатора нет
// и быть не может, а нашего края перед этим фронтом нет by construction. Значит
// предъявленное удостоверение — единственное, чем он может назваться, и ручка
// его приёма обязана быть ОБЪЯВЛЕНА чартом, а включена — решением профиля.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СВЕРКА С СОСЕДНЕЙ РУЧКОЙ, А НЕ С ВЫПИСАННЫМ ОБРАЗЦОМ
//
// Форма объявления — свойство ЧАРТА, а не платформы; она менялась и будет
// меняться. Предикат, сверяющий с ЖИВОЙ соседкой того же блока (своя чеканка),
// переживает смену формы; предикат, сверяющий с выписанным образцом, тихо
// устареет.
//
// Перепись печатает ОБЕ величины — вхождений искомой ручки и вхождений
// соседней, — чтобы «ноль» читалось как находка, а не как «предикат не нашёл
// ничего вообще».
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// presentedCredentialKnobs — величины читателя, которые профиль обязан объявить
// сам, включив его.
//
// Перечень выписан здесь намеренно, и это единственное выписанное место: он есть
// КОНТРАКТ фазы, а не свойство дерева. Ручка, добавленная в чарт и забытая
// здесь, обязана быть замечена человеком — вывод её из чарта сделал бы проверку
// тождественно истинной.
var presentedCredentialKnobs = []string{"audience", "revocationCacheTtl"}

// countIn — вхождений имени в файле. Величина, а не признак: перепись обязана
// печатать число, иначе «ноль» неотличим от «не читали».
func countIn(t *testing.T, path, needle string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("не прочитан %s: %v", path, err)
	}
	return strings.Count(string(b), needle)
}

// TestKAN_LAND_01_ReaderKnobIsDeclaredTheSameWayItsNeighbourIs — ручка объявлена
// ОБОИМИ способами, которыми объявлена соседняя ручка того же блока.
func TestKAN_LAND_01_ReaderKnobIsDeclaredTheSameWayItsNeighbourIs(t *testing.T) {
	chart := filepath.Join(umbrellaDir, "charts", "kaname")
	values := filepath.Join(chart, "values.yaml")
	configmap := filepath.Join(chart, "templates", "configmap.yaml")

	// Соседка того же блока — своя чеканка: её объявление живо, и форма берётся
	// у неё, а не у константы.
	neighbourValues := countIn(t, values, "tokenSigning")
	neighbourTemplate := countIn(t, configmap, "token-signing")
	readerValues := countIn(t, values, "presentedCredential")
	readerTemplate := countIn(t, configmap, "presented-credential")

	t.Logf("перепись объявлений: значения — соседка %d, искомая %d; шаблон настроек — соседка %d, искомая %d",
		neighbourValues, readerValues, neighbourTemplate, readerTemplate)

	if neighbourValues == 0 || neighbourTemplate == 0 {
		t.Fatal("перепись беспредметна: соседняя ручка того же блока не найдена ни одним " +
			"вхождением — сверять форму не с чем, и «ноль» у искомой ничего не значил бы")
	}
	if readerValues == 0 {
		t.Errorf("ручка приёма предъявленного удостоверения не объявлена в значениях чарта "+
			"(%s): фронт поднимается на любой посадке, а назвать предъявителя нечем", values)
	}
	if readerTemplate == 0 {
		t.Errorf("ручка приёма предъявленного удостоверения не подставляется в шаблон "+
			"настроек (%s): объявление, не доезжающее до процесса, — половина пары", configmap)
	}

	// Умолчание НЕ включает читателя молча: включение — выбор профиля.
	chartValues := readYAML(t, values)
	pc, _ := dig(chartValues, "config", "authn", "presentedCredential").(map[string]any)
	if pc == nil {
		t.Fatalf("в значениях чарта нет узла config.authn.presentedCredential (%s)", values)
	}
	if on, _ := pc["enabled"].(bool); on {
		t.Error("умолчание чарта ВКЛЮЧАЕТ читателя: тогда решение о посадке принимает сборка, " +
			"а не оператор, и близнец профиля становится неисполнимым by construction")
	}
	for _, knob := range presentedCredentialKnobs {
		if _, present := pc[knob]; !present {
			t.Errorf("величина %s не объявлена в значениях чарта: профиль, о ней умолчавший, "+
				"выглядел бы настроенным", knob)
		}
	}
}

// TestKAN_LAND_02_EveryProductionProfileRaisingTheFrontDeclaresTheReader —
// профиль, поднимающий службу в БОЕВОЙ посадке, объявляет читателя и его
// величины; профиль промежуточного шага установки — не обязан.
//
// # Пара, на которой держится утверждение
//
// Различие ЖИВОЕ и решает его профиль, а не сборка: боевые профили включают
// читателя, базовый профиль подъёма — нет. Без второй половины «профиль
// включает» зеленело бы на чарте, включающем читателя ВСЕГДА, — то есть на том,
// что KAN-LAND-01 как раз и запрещает.
func TestKAN_LAND_02_EveryProductionProfileRaisingTheFrontDeclaresTheReader(t *testing.T) {
	files := profileFiles(t)

	var production, withReader, devPosture int
	for _, f := range files {
		p := readYAML(t, f)
		mode, _ := dig(p, "kaname", "authMode").(string)
		// Признак боевой посадки — ДИЗЪЮНКЦИЯ, и вторая половина обязательна:
		// профиль-накладка вправе не объявлять посадку вовсе (умолчание чарта
		// уже боевое), но объявляет СВОЮ ЧЕКАНКУ — а её не бывает в дев. Отбор
		// по одному объявленному режиму пропустил бы такой профиль молча.
		ts, _ := dig(p, "kaname", "config", "authn", "tokenSigning").(map[string]any)
		ownMinting := false
		if ts != nil {
			ownMinting, _ = ts["enabled"].(bool)
		}
		if mode == "" && !ownMinting {
			continue
		}
		pc, _ := dig(p, "kaname", "config", "authn", "presentedCredential").(map[string]any)
		on := false
		if pc != nil {
			on, _ = pc["enabled"].(bool)
		}
		if !strings.HasPrefix(mode, "production") && !ownMinting {
			devPosture++
			if on {
				t.Errorf("%s: профиль дев-посадки включает читателя — своей чеканки на нём нет, "+
					"и служба не поднимется", filepath.Base(f))
			}
			continue
		}
		production++
		if !on {
			t.Errorf("%s: боевая посадка поднимает собственный публичный REST-фронт и НЕ включает "+
				"читателя предъявленного — годное удостоверение получит тот же отказ, что и "+
				"негодное, и это выглядит настроенным", filepath.Base(f))
			continue
		}
		withReader++
		for _, knob := range presentedCredentialKnobs {
			v, present := pc[knob]
			switch {
			case !present:
				t.Errorf("%s: читатель включён, величина %s не объявлена", filepath.Base(f), knob)
			case isDegenerate(v):
				t.Errorf("%s: величина %s объявлена вырожденной (%v)", filepath.Base(f), knob, v)
			}
		}
	}

	t.Logf("перепись профилей: осмотрено %d, боевая посадка %d (из них с читателем %d), "+
		"дев-посадка %d", len(files), production, withReader, devPosture)

	if production == 0 {
		t.Fatal("перепись беспредметна: ни один профиль не объявляет боевой посадки службы — " +
			"проверять нечего, и «ноль находок» означало бы «ноль прочитанного»")
	}
	if devPosture == 0 {
		t.Fatal("перепись беспредметна: вторая половина пары пуста — профиля промежуточного " +
			"шага установки нет, и «включение решает профиль» ничем не доказывается")
	}
}
