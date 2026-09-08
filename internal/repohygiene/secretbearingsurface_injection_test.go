// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// secretbearingsurface_injection_test.go — ДОКАЗАТЕЛЬСТВО, ЧТО ОСЬ 2 BAT-1-73
// СПОСОБНА УПАСТЬ И СПОСОБНА СМОЛЧАТЬ.
//
// # ВХОД БЕРЁТСЯ ИЗ НАСТОЯЩИХ ДЕСКРИПТОРОВ
//
// Инъекция, собравшая себе поля из литералов, доказала бы свойство СВОЕЙ копии
// предмета: образцы, пометка и имена разошлись бы с деревом молча. Поэтому вход
// здесь — те же `collectSecretSurfaceFields()`, что исполняет ось, а дефект
// вносится В ЭТОТ ЖЕ набор: снимается пометка у настоящего носителя, добавляется
// запись ведомости на настоящее помеченное поле, снимается предмет у настоящей
// записи.
//
// # ОСИ РАЗВЕДЕНЫ — одна проба «на всё» зеленела бы на четырёх сломанных из семи
//
//	СНЯТАЯ ПОМЕТКА        носитель без пометки и без записи — НАХОДКА с координатой и образцом
//	ЗАКОННЫЙ БЛИЗНЕЦ      нетронутое дерево — МОЛЧАНИЕ
//	ИМЯ НЕ СОВПАЛО        поле, чьё имя ни одному образцу не отвечает, — МОЛЧАНИЕ
//	ЗАКОННАЯ ЗАПИСЬ       ведомость на НЕпомеченное не-секретное поле — МОЛЧАНИЕ
//	ПРОТИВОРЕЧИЕ          помечено И названо в ведомости — НАХОДКА
//	САМОИСТЕЧЕНИЕ         запись, потерявшая предмет, — НАХОДКА (обе формы)
//	СТРУКТУРНЫЙ ВЫЧЕТ     курсор молчит; сними вычет — сотни находок

// realSecretSurfaceFields — вход оси, взятый из настоящих дескрипторов.
func realSecretSurfaceFields(t *testing.T) []secretSurfaceField {
	t.Helper()
	fields, census := collectSecretSurfaceFields()
	if census.matched == 0 || census.marked == 0 {
		t.Fatalf("вход инъекции пуст: совпавших %d, помеченных %d — доказывать нечего",
			census.matched, census.marked)
	}
	return fields
}

// withField — копия набора, в которой одно поле подменено. Дерево не трогается.
func withField(fields []secretSurfaceField, id string, patch func(*secretSurfaceField)) []secretSurfaceField {
	out := make([]secretSurfaceField, len(fields))
	copy(out, fields)
	for i := range out {
		if out[i].id() == id {
			patch(&out[i])
		}
	}
	return out
}

func hasField(fields []secretSurfaceField, id string) bool {
	for _, f := range fields {
		if f.id() == id {
			return true
		}
	}
	return false
}

func mentioning(findings []string, id string) []string {
	var out []string
	for _, f := range findings {
		if strings.HasPrefix(f, id+" ") || strings.HasPrefix(f, id+" —") {
			out = append(out, f)
		}
	}
	return out
}

// realBearer — настоящий носитель дерева, на котором проверяется ось.
const realBearer = "kaname.cloud.iam.v1.IssueSAKeyResponse.secret"

// realExclusion — настоящая законная запись ведомости.
const realExclusion = "kaname.cloud.iam.v1.IssueSAKeyResponse.public_key_pem"

// TestBAT1_73_Axis2_InjectionUnmarkedBearerIsAFinding — ОСЬ «снятая пометка».
func TestBAT1_73_Axis2_InjectionUnmarkedBearerIsAFinding(t *testing.T) {
	fields := realSecretSurfaceFields(t)
	if !hasField(fields, realBearer) {
		t.Fatalf("носителя %s в дереве нет — фикстура ПОТЕРЯЛА ПРЕДМЕТ", realBearer)
	}
	for _, f := range fields {
		if f.id() == realBearer && (!f.marked || f.pattern == "") {
			t.Fatalf("%s: помечен=%v, образец=%q — фикстура доказывает не то",
				realBearer, f.marked, f.pattern)
		}
	}

	broken := withField(fields, realBearer, func(f *secretSurfaceField) { f.marked = false })
	findings, stale, _ := secretSurfaceVerdict(broken, secretSurfaceExclusions)

	hit := mentioning(findings, realBearer)
	if len(hit) != 1 {
		t.Fatalf("находок по %s = %d, ожидалась 1.\nвсе находки: %v", realBearer, len(hit), findings)
	}
	if !strings.Contains(hit[0], "*secret*") {
		t.Errorf("находка не называет СОВПАВШИЙ ОБРАЗЕЦ: %q", hit[0])
	}
	if len(stale) != 0 {
		t.Errorf("снятие пометки просрочило записи ведомости: %v", stale)
	}
}

// TestBAT1_73_Axis2_InjectionUntouchedTreeIsSilent — ОСЬ «законный близнец».
func TestBAT1_73_Axis2_InjectionUntouchedTreeIsSilent(t *testing.T) {
	fields := realSecretSurfaceFields(t)
	findings, stale, excluded := secretSurfaceVerdict(fields, secretSurfaceExclusions)
	if len(findings) != 0 {
		t.Fatalf("ось краснеет на НЕТРОНУТОМ дереве: %v", findings)
	}
	if len(stale) != 0 {
		t.Fatalf("записи ведомости просрочены на нетронутом дереве: %v", stale)
	}
	if excluded != len(secretSurfaceExclusions) {
		t.Fatalf("записей ведомости %d, предмет нашёлся у %d — остальные прощают пустоту",
			len(secretSurfaceExclusions), excluded)
	}
}

// TestBAT1_73_Axis2_InjectionNameThatMatchesNothingIsSilent — ОСЬ «имя не совпало».
//
// Без неё ось краснела бы на всём подряд и была бы снята первым же ложным
// срабатом. Близнец берётся из НАСТОЯЩЕГО дерева и стоит рядом с находкой — в
// том же сообщении.
func TestBAT1_73_Axis2_InjectionNameThatMatchesNothingIsSilent(t *testing.T) {
	const neighbour = "kaname.cloud.iam.v1.IssueSAKeyResponse.algorithm"
	fields := realSecretSurfaceFields(t)
	if !hasField(fields, neighbour) {
		t.Fatalf("соседнего поля %s в дереве нет — фикстура потеряла предмет", neighbour)
	}
	for _, f := range fields {
		if f.id() == neighbour {
			if f.pattern != "" {
				t.Fatalf("%s совпал с образцом %q — близнец перестал быть законным",
					neighbour, f.pattern)
			}
			if f.marked {
				t.Fatalf("%s помечен — близнец доказывает не то", neighbour)
			}
		}
	}
	findings, _, _ := secretSurfaceVerdict(fields, secretSurfaceExclusions)
	if hit := mentioning(findings, neighbour); len(hit) != 0 {
		t.Fatalf("поле, не совпавшее ни с одним образцом, объявлено находкой: %v", hit)
	}
}

// TestBAT1_73_Axis2_InjectionMarkedAndListedIsAContradiction — ОСЬ «противоречие».
//
// Ограничение «запись законна только для НЕ-носителя» держится механически:
// иначе ведомость проглатывает поле, вводимое самой фазой, ось 1 отрабатывает на
// пустом множестве, и §4.3.2 остаётся держаться прозой.
func TestBAT1_73_Axis2_InjectionMarkedAndListedIsAContradiction(t *testing.T) {
	fields := realSecretSurfaceFields(t)
	ledger := append([]secretSurfaceExclusion{}, secretSurfaceExclusions...)
	ledger = append(ledger, secretSurfaceExclusion{
		message: "kaname.cloud.iam.v1.IssueSAKeyResponse", field: "secret",
		why: "синтетика инъекции: запись на ПОМЕЧЕННОЕ поле",
	})

	findings, stale, _ := secretSurfaceVerdict(fields, ledger)
	hit := mentioning(findings, realBearer)
	if len(hit) != 1 {
		t.Fatalf("противоречие не стало находкой: %v", findings)
	}
	if !strings.Contains(hit[0], "ОДНОВРЕМЕННО") {
		t.Errorf("находка не называет предмет противоречия: %q", hit[0])
	}
	if len(stale) != 0 {
		t.Errorf("противоречивая запись объявлена ещё и просроченной: %v", stale)
	}
}

// TestBAT1_73_Axis2_InjectionLedgerExpiresOnItsOwn — ОСЬ «самоистечение», обе формы.
func TestBAT1_73_Axis2_InjectionLedgerExpiresOnItsOwn(t *testing.T) {
	fields := realSecretSurfaceFields(t)

	// ЗДЕСЬ ИНЪЕКЦИЯ НАШЛА ДЕФЕКТ В МОЁМ ЖЕ СУЖДЕНИИ, И ЭТО ЗАПИСАНО НАРОЧНО.
	//
	// Первая редакция этой пробы требовала, чтобы помеченное поле, названное в
	// ведомости, дало И находку-противоречие, И строку «запись просрочена».
	// Суждение давало только первое, и проба покраснела.
	//
	// Прав оказался КОД, а не проба: два отчёта об одном предмете заставляют
	// чинить дважды и подсказывают ровно один из двух исходов — «снимите
	// запись», — тогда как гейт различить их не может. Ошибочной могла быть
	// пометка (поле секретом не является), и тогда правильный исход обратный.
	// Поэтому исход один, а внутри него названы ОБА пути.
	t.Run("поле помечено — противоречие, называющее ОБА исхода", func(t *testing.T) {
		patched := withField(fields, realExclusion, func(f *secretSurfaceField) { f.marked = true })
		findings, stale, _ := secretSurfaceVerdict(patched, secretSurfaceExclusions)
		hit := mentioning(findings, realExclusion)
		if len(hit) != 1 {
			t.Fatalf("противоречие не названо: %v", findings)
		}
		for _, must := range []string{"снимите ЗАПИСЬ", "снимите ПОМЕТКУ"} {
			if !strings.Contains(hit[0], must) {
				t.Errorf("находка не называет исход %q: %s", must, hit[0])
			}
		}
		if len(stale) != 0 {
			t.Errorf("предмет назван ДВАЖДЫ — противоречием и просрочкой: %v", stale)
		}
	})

	t.Run("поле снято — запись ПОТЕРЯЛА предмет", func(t *testing.T) {
		var without []secretSurfaceField
		for _, f := range fields {
			if f.id() == realExclusion {
				continue
			}
			without = append(without, f)
		}
		_, stale, _ := secretSurfaceVerdict(without, secretSurfaceExclusions)
		if len(stale) != 1 || !strings.HasPrefix(stale[0], realExclusion) {
			t.Fatalf("просроченная запись не названа при снятом поле: %v", stale)
		}
	})

	t.Run("поле переименовано мимо образца — запись ПОТЕРЯЛА предмет", func(t *testing.T) {
		patched := withField(fields, realExclusion, func(f *secretSurfaceField) { f.pattern = "" })
		_, stale, _ := secretSurfaceVerdict(patched, secretSurfaceExclusions)
		if len(stale) != 1 || !strings.HasPrefix(stale[0], realExclusion) {
			t.Fatalf("просроченная запись не названа при несовпавшем имени: %v", stale)
		}
	})
}

// TestBAT1_73_Axis2_InjectionStructuralCursorSubtractionIsLoadBearing — ОСЬ
// «структурный вычет».
//
// Вычет курсора постраничного чтения — не косметика: без него ось краснеет
// сотнями находок, ведомость пришлось бы вести в сотни строк, и её перестали бы
// читать. Здесь это ЗАМЕРЕНО, а не объявлено.
func TestBAT1_73_Axis2_InjectionStructuralCursorSubtractionIsLoadBearing(t *testing.T) {
	fields := realSecretSurfaceFields(t)

	silent, _, _ := secretSurfaceVerdict(fields, secretSurfaceExclusions)
	if len(silent) != 0 {
		t.Fatalf("ось краснеет на нетронутом дереве: %v", silent)
	}

	var noSubtraction []secretSurfaceField
	for _, f := range fields {
		f.cursor = false
		noSubtraction = append(noSubtraction, f)
	}
	loud, _, _ := secretSurfaceVerdict(noSubtraction, secretSurfaceExclusions)
	if len(loud) < 100 {
		t.Fatalf("без структурного вычета находок %d — вычет ничего не снимает, "+
			"и объяснение в шапке оси неверно", len(loud))
	}
	t.Logf("замер: со структурным вычетом находок %d, без него %d — вычет снимает %d",
		len(silent), len(loud), len(loud)-len(silent))
}

// TestBAT1_73_Axis2_InjectionBlindInputIsNotHealth — ОСЬ «ноль — это слепота».
func TestBAT1_73_Axis2_InjectionBlindInputIsNotHealth(t *testing.T) {
	findings, stale, excluded := secretSurfaceVerdict(nil, nil)
	if len(findings) != 0 || len(stale) != 0 || excluded != 0 {
		t.Fatalf("пустой вход дал находки %v / просроченные %v / прощённых %d",
			findings, stale, excluded)
	}
	// А на пустом входе С ВЕДОМОСТЬЮ каждая запись обязана оказаться просроченной:
	// «прощать нечего» и «читать нечего» — разные вещи, и ось обязана их различать.
	_, stale, _ = secretSurfaceVerdict(nil, secretSurfaceExclusions)
	if len(stale) != len(secretSurfaceExclusions) {
		t.Fatalf("на пустом входе просрочено %d записей из %d — ведомость прощает пустоту",
			len(stale), len(secretSurfaceExclusions))
	}
}
