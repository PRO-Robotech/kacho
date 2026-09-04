// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestQuotaRefusalToneIsDerivedFromTheSentinelNotAPrefixList — у каждого
// владельца учёта снимаемый префикс отказа по пределу ВЫВОДИТСЯ из sentinel'а.
//
// Предмет, признак, границы обхода и почему держится именно структурная причина
// совпадения текстов — в шапке ct2_misc_quota_refusal_tone.go. Способность
// гейта упасть и смолчать доказана инъекцией в обе стороны:
// ct2_misc_quota_refusal_tone_injection_test.go.
func TestQuotaRefusalToneIsDerivedFromTheSentinelNotAPrefixList(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — ИНДЕКС git, а не обход диска: под services/ на машине, где
	// поднимали стенд, лежат игнорируемые каталоги, и вердикт, собранный обходом
	// файловой системы, стал бы свойством рабочего каталога, а не коммита.
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	// Перечень владельцев ВЫВОДИТСЯ из единственного источника — того же, из
	// которого каждому владельцу рендерится файл отказа. Выписать его здесь
	// значило бы завести второе место, которое разойдётся с первым молча ровно
	// в тот день, когда учёт заведёт седьмой владелец.
	var owners []string
	for _, o := range quota.RefusalOwners() {
		owners = append(owners, o.Service)
	}
	sort.Strings(owners)

	c, err := collectQuotaRefusalTone(tree, owners)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	// ── ПЕРЕПИСЬ. Печатается ВСЕГДА и несёт ДВЕ величины на каждого владельца:
	// «осмотрено» и «соответствует». Одно число скрывает ровно тот случай, ради
	// которого гейт заведён.
	var perOwner []string
	for _, o := range c.Owners {
		perOwner = append(perOwner, o+"="+ct2ToneOwnerState(c.Facts[o]))
	}
	t.Logf("перепись: владельцев %d · прод-файлов осмотрено %d (разобрано %d) · "+
		"мапперов наружу %d · стрипперов разрешено %d\n"+
		"         ось 1 префикс из sentinel'а: %d · ось 2 пустой остаток ограждён: %d · "+
		"ось 3 словарь общий: %d\n         по владельцам: %s",
		len(c.Owners), c.Files, c.Parsed, c.Outward, c.Resolved,
		c.Conforming, c.Guarding, c.Vocabulary,
		strings.Join(perOwner, ", "))

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт:
	// «нарушений нет» и «нечего было читать» печатаются одинаково.
	if len(c.Owners) == 0 {
		t.Fatal("владельцев учёта не выведено ни одного — предмета у гейта нет")
	}
	if c.Files == 0 || c.Parsed == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: файлов %d, разобрано %d",
			c.Files, c.Parsed)
	}
	if c.Outward == 0 {
		t.Fatal("маппера отказа учёта наружу не найдено НИ У ОДНОГО владельца — " +
			"распознаватель перестал видеть предмет (см. п.7 §«Гейт на класс»)")
	}

	for _, f := range quotaRefusalToneFindings(c) {
		t.Errorf("тон отказа по пределу: %s", f)
	}

	// Число соответствующих обязано сойтись с числом владельцев: расхождение
	// названо находками выше, а это утверждение ловит случай, когда находок
	// нет, а соответствие всё равно неполно.
	for _, ax := range []struct {
		name string
		got  int
	}{
		{"выводят префикс из sentinel'а", c.Conforming},
		{"ограждают пустой остаток", c.Guarding},
		{"несут общий словарь sentinel'ов", c.Vocabulary},
	} {
		if ax.got != len(c.Owners) {
			t.Errorf("владельцев %s, %s %s — расхождение",
				strconv.Itoa(len(c.Owners)), ax.name, strconv.Itoa(ax.got))
		}
	}
}

// ct2ToneOwnerState — имя состояния ОДНОГО владельца для переписи.
//
// Вынесено из тела гейта не ради краткости, а чтобы диагностику можно было
// ПРОВЕРИТЬ: «находка называет симптом вместо причины» — дефект того же рода,
// что молчащий гейт (`testing.md` §«Гейт на класс», п.8), и в диффе он не
// виден. Инъекция утверждает это имя по каждому состоянию:
// TestCt2ToneInjection_BlindSpotsAreFindingsNotSilence.
//
// Порядок ветвей повторяет порядок находок в quotaRefusalToneFindings и этим
// несущий: маппер наружу спрашивается ПЕРВЫМ, потому что стриппер ищется от
// его имени (проход 2) и без маппера пуст тоже. Спроси о стриппере раньше — и
// владелец без маппера получит имя ВТОРОГО симптома, тогда как находка о нём
// говорит «маппера наружу не найдено»: два имени одного состояния.
func ct2ToneOwnerState(f *ct2ToneOwnerFacts) string {
	var state string
	switch {
	case f.outwardFile == "":
		state = "нет маппера"
	case f.literalPrefixFile != "":
		state = "перечень префиксов-строк"
	case f.stripperFile == "":
		state = "стриппер не разрешён"
	case f.derivesPrefix && !f.guardsEmptyRemainder:
		state = "выводит, пустой остаток не ограждён"
	case f.derivesPrefix:
		state = "выводит из sentinel'а"
	default:
		state = "префикс ниоткуда"
	}
	if !ct2ToneVocabularyAgrees(f) {
		state += " · словарь разошёлся"
	}
	return state
}
