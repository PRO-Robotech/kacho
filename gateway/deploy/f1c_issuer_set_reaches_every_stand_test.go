// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1c_issuer_set_reaches_every_stand_test.go — КАЖДЫЙ стенд объявляет перечень
// принимаемых издателей, с которым его край поднимается.
//
// # Чем это отличается от соседа
//
// Сосед (f1b_token_acceptance_declared_test.go) судит ФАЙЛ: поднимется ли
// процесс с тем, что профиль объявил сам. Профиль, приёма не объявляющий, он не
// судит — сам по себе такой профиль даёт отказ старта, а слоем цепочки получает
// перечень от слоя ниже. Получает ли — вопрос этого файла.
//
// # Чем опасен необъявленный перечень
//
// Край, которому перечень не объявлен, НЕ ПОДНИМАЕТСЯ: `TokenAcceptance`
// отказывает, называя ручку. Отказ громкий и виден оператору при выкате — но
// виден ТОЛЬКО на поднятом стенде. Здесь тот же вердикт выносится до выката, по
// дереву: стенд, чья цепочка перечня не объявляет, не выкатился бы вовсе.
//
// # Почему единиц ДВЕ, и почему одной не хватает
//
// Профили НАКЛАДЫВАЮТСЯ (deploy/stacks.txt). Спрашивать только ФАЙЛ значило бы
// требовать от накладки повторного объявления того, что слой под ней уже
// объявил, — а повтор в ПОСЛЕДНЕМ слое цепочки не безобиден: при расхождении с
// нижним слоем выигрывает он, и стенд молча поедет на устаревшей копии.
// Спрашивать только СТЕНД значило бы не заметить профиль, который ни одна
// цепочка не называет и который поэтому применяется как есть.
//
// Поэтому судится и то, и другое:
//
//	(1) КАЖДЫЙ СТЕНД таблицы, сложенный так, как его складывает helm (умолчания
//	    чарта плюс цепочка `-f`), даёт записи приёма, с которыми край поднимается;
//	(2) КАЖДЫЙ ПРОФИЛЬ, называющий край, либо объявляет перечень сам, либо
//	    каждая называющая его цепочка объявляет перечень слоем НИЖЕ него —
//	    и это ПРОВЕРЯЕТСЯ по таблице, а не принимается на слово.
//
// # Законный близнец
//
// Профиль, края НЕ называющий (сегодня — values.fe3455-ory-posture.yaml:
// накладка посадки поставщика личности), отличается от судимого ровно ОДНИМ
// фактом и обязан молчать. Что он в дереве есть — утверждается, а не
// предполагается: близнец, исчезнувший из дерева, превратил бы «молчит на
// близнеце» в утверждение ни о чём.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// f1cChartDefaults — файл умолчаний чарта. helm читает его ВСЕГДА и без `-f`,
// поэтому он является первым слоем КАЖДОГО стенда и слоем цепочки при этом не
// значится. Складывать стенд без него значило бы судить не то, что установлено.
const f1cChartDefaults = "values.yaml"

// f1cIssuerSetOf возвращает объявленный профилем перечень издателей.
//
// Читается ТОЧНЫМ именем ключа: переименование ключа с суффиксом обязано
// читаться как «ключа нет», а не удовлетворять проверку.
func f1cIssuerSetOf(gw map[string]any) (string, bool) {
	ta, ok := gw["tokenAcceptance"].(map[string]any)
	if !ok {
		return "", false
	}
	s, _ := ta["issuers"].(string)
	return s, true
}

// f1cDeclares отвечает, объявил ли профиль НЕПУСТОЙ перечень издателей.
//
// Считаются ЭЛЕМЕНТЫ, а не длина строки: значение «,» непусто как строка и
// пусто как перечень, и именно на таком входе предикат по длине молчит.
// Элементы считает ТОТ ЖЕ читатель, которого исполняет процесс.
func f1cDeclares(gw map[string]any) bool {
	raw, present := f1cIssuerSetOf(gw)
	if !present {
		return false
	}
	cfg := config.Config{TokenIssuers: raw}
	elems, err := cfg.AcceptedTokenIssuers()
	return err == nil && len(elems) > 0
}

// f1cEdgeBlock — блок края профиля, или nil, если профиль края не называет.
func f1cEdgeBlock(t *testing.T, profile string) map[string]any {
	t.Helper()
	gw, _ := umbrellaValues(t, profile)[edgeChartKey].(map[string]any)
	return gw
}

// f1cProfileNames — все профили зонта, ВЫВЕДЕННЫЕ из дерева. Рукописный список
// разошёлся бы с деревом молча, и новый профиль остался бы непроверенным.
func f1cProfileNames(t *testing.T) []string {
	t.Helper()
	dir := umbrellaDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог профилей зонта не прочитан (%v) — посылка этой проверки "+
			"исчезла, а это НЕ то же самое, что «находок ноль»", err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "values") || !strings.HasSuffix(n, ".yaml") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// f1cExclusion — осознанное исключение из правила «профиль, называющий край,
// объявляет перечень или наследует его от слоя ниже».
//
// Форма та же, что у исключений таблицы стеков (deploy/stack_table_test.go), и
// по той же причине: исключение несёт ПРИЧИНУ и ПРЕДИКАТ, по которому истекает
// само. Исключение, пережившее свой предмет, разрешает то, чего нет, и его
// наследует следующая слепая зона.
type f1cExclusion struct {
	why string
	// subject читает ДЕРЕВО и отвечает «предмет исключения ещё есть?».
	subject func(t *testing.T) (bool, string)
}

var f1cRecordedExclusions = map[string]f1cExclusion{
	"values.yaml": {
		why: "умолчания чарта: helm читает этот файл ВСЕГДА и без `-f`, поэтому слоем " +
			"цепочки он не является и назвать его цепочкой нечем. Объявлять перечень " +
			"ЗДЕСЬ прямо запрещено: ненулевое умолчание у ручки, по которой судит страж " +
			"старта, делает стража мёртвым — состояние «не объявлено» перестало бы " +
			"наступать на любом стенде, и отказ в пуске не сработал бы НИ РАЗУ. " +
			"Перечень объявляет цепочка, и предикат ниже это ПРОВЕРЯЕТ",
		subject: func(t *testing.T) (bool, string) {
			t.Helper()
			root := umbrellaDir
			if _, err := os.Stat(filepath.Join(root, "Chart.yaml")); err != nil {
				return false, fmt.Sprintf("рядом нет Chart.yaml (%v) — это больше не корень чарта", err)
			}
			// Предмет исключения жив ровно пока КАЖДАЯ цепочка объявляет
			// перечень сама. Перестала хоть одна — исключение стало разрешением
			// ехать на умолчании, и его надо снять, а не унаследовать.
			var mute []string
			for name, chain := range deployableStacks(t) {
				declared := false
				for _, layer := range chain {
					if gw := f1cEdgeBlock(t, layer); gw != nil && f1cDeclares(gw) {
						declared = true
					}
				}
				if !declared {
					mute = append(mute, name)
				}
			}
			sort.Strings(mute)
			if len(mute) > 0 {
				return false, fmt.Sprintf("цепочки, не объявляющие перечень своими слоями: %v — "+
					"умолчания чарта стали тем, на чём стенд едет", mute)
			}
			return true, "рядом лежит Chart.yaml, и каждая цепочка объявляет перечень своими слоями"
		},
	},
	"values.digests.example.yaml": {
		why: "образец закрепления образов по digest, а не слой развёртывания: ни одна " +
			"цепочка его не называет и назвать не может — значения намеренно не являются " +
			"digest'ами, и применить файл как есть нельзя. Блок края в нём объявляет " +
			"ОДНУ ручку (тег образа) и о приёме токена не решает ничего",
		subject: func(t *testing.T) (bool, string) {
			t.Helper()
			path := filepath.Join(umbrellaDir, "values.digests.example.yaml")
			raw, err := os.ReadFile(path) // #nosec G304 -- путь выписан константой, не вводом
			if err != nil {
				return false, fmt.Sprintf("файл не читается (%v) — исключать нечего", err)
			}
			n := strings.Count(string(raw), "REPLACE_WITH_REAL_DIGEST")
			if n == 0 {
				return false, "заглушек REPLACE_WITH_REAL_DIGEST не осталось — файл стал " +
					"применимым как есть, то есть слоем развёртывания, и объявлять приём обязан"
			}
			return true, fmt.Sprintf("заглушек REPLACE_WITH_REAL_DIGEST — %d, файл неприменим как есть", n)
		},
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// (1) СТЕНД: то, что helm ставит на самом деле.

// TestF1c_EveryStandDeclaresTheIssuerSetItsEdgeBootsWith — каждый стенд таблицы
// объявляет перечень издателей, с которым его край поднимается.
//
// Вердикт выносит `config.Config.TokenAcceptance` — тот же предикат, который
// исполняет процесс при старте: необъявленный перечень там — отказ старта.
func TestF1c_EveryStandDeclaresTheIssuerSetItsEdgeBootsWith(t *testing.T) {
	stacks := deployableStacks(t)
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		chain := append([]string{f1cChartDefaults}, stacks[name]...)
		merged := map[string]any{}
		for _, profile := range chain {
			merged = mergeInto(merged, umbrellaValues(t, profile))
		}
		gw, ok := merged[edgeChartKey].(map[string]any)
		if !ok {
			// Стенд без края — законный исход, а не находка: судить в нём
			// нечего, и молчать надо явно, а не по совпадению.
			t.Logf("стенд %s края не разворачивает — предмета нет", name)
			continue
		}
		cfg, _ := f1bGatewayConfig(gw)
		bindings, err := cfg.TokenAcceptance()
		if err != nil {
			t.Errorf("стенд %s (цепочка %v) объявляет приём, с которым край НЕ ПОДНИМЕТСЯ: %v",
				name, chain, err)
			continue
		}
		t.Logf("стенд %-12s цепочка %v → записей приёма %d: %v",
			name, chain, len(bindings), f1cRecords(bindings))
	}
}

// f1cRecords — записи приёма в читаемом виде: издатель и адрес его набора.
func f1cRecords(bindings []config.TokenIssuerBinding) []string {
	out := make([]string, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, fmt.Sprintf("%s ← %s", b.Issuer, b.KeySetURL))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// (2) ПРОФИЛЬ: объявляет сам или наследует от слоя НИЖЕ.

// TestF1c_EveryProfileNamingTheEdgeDeclaresTheIssuerSetOrInheritsItFromBelow —
// профиль, называющий край, обязан объявить перечень издателей или доказуемо
// получить его от слоя, лежащего под ним в КАЖДОЙ называющей его цепочке.
func TestF1c_EveryProfileNamingTheEdgeDeclaresTheIssuerSetOrInheritsItFromBelow(t *testing.T) {
	stacks := deployableStacks(t)
	profiles := f1cProfileNames(t)
	if len(profiles) == 0 {
		t.Fatalf("профилей зонта прочитано НОЛЬ — «ноль находок» на таком объёме означает " +
			"«ноль прочитанного», и молчание этой проверки сказано ни о чём")
	}

	namesEdge, declaresOwn, inherits, excluded, silentTwins := 0, 0, 0, 0, 0
	for _, profile := range profiles {
		gw := f1cEdgeBlock(t, profile)
		if gw == nil {
			// ЗАКОННЫЙ БЛИЗНЕЦ: отличается от судимого ровно одним фактом —
			// края не называет, — и обязан молчать.
			silentTwins++
			t.Logf("%-32s края не называет — законный близнец, предмета нет", profile)
			continue
		}
		namesEdge++
		if f1cDeclares(gw) {
			declaresOwn++
			t.Logf("%-32s объявляет перечень САМ", profile)
			continue
		}

		// Не объявил сам. Тогда КАЖДАЯ цепочка, его называющая, обязана
		// объявлять перечень слоем НИЖЕ него — и это читается из таблицы, а не
		// принимается на слово.
		naming := f1cChainsNaming(stacks, profile)
		if len(naming) > 0 {
			ok := true
			for _, stack := range naming {
				below, found := f1cDeclaringLayerBelow(t, stacks[stack], profile)
				if !found {
					ok = false
					t.Errorf("профиль %s называет край, перечень издателей не объявляет, и в "+
						"цепочке %q (%v) НИ ОДИН слой под ним его не объявляет.\n\n"+
						"Тогда край этого стенда не поднимется: перечень издателей не "+
						"объявлен, и страж старта отказывает, называя ручку.",
						profile, stack, stacks[stack])
					continue
				}
				t.Logf("%-32s наследует в цепочке %q от слоя %s", profile, stack, below)
			}
			if ok {
				inherits++
			}
			continue
		}

		// Ни одна цепочка его не называет: применяется как есть или не
		// применяется вовсе. Второе — записанное исключение с живым предметом.
		if ex, recorded := f1cRecordedExclusions[profile]; recorded {
			alive, why := ex.subject(t)
			if !alive {
				t.Errorf("исключение для %s потеряло основание: %s. Основание было: %s.\n"+
					"Либо профиль объявляет перечень, либо исключение переписывается под "+
					"новое основание — исключение, пережившее свой предмет, разрешает то, "+
					"чего нет", profile, why, ex.why)
				continue
			}
			excluded++
			t.Logf("%-32s записанное исключение: предмет есть (%s)", profile, why)
			continue
		}
		t.Errorf("профиль %s называет край, перечень издателей не объявляет и НИ ОДНОЙ "+
			"цепочкой не назван — значит, применяется как есть, и край на нём не поднимется: "+
			"перечень издателей не объявлен.\n\n"+
			"Либо объяви перечень в самом профиле, либо внеси его в deploy/stacks.txt слоем "+
			"поверх объявляющего, либо запиши исключение с причиной и предикатом, по "+
			"которому оно истекает само (f1cRecordedExclusions).", profile)
	}

	if silentTwins == 0 {
		t.Errorf("в дереве не осталось НИ ОДНОГО профиля, края не называющего — законного " +
			"близнеца у этой проверки больше нет, и «молчит на близнеце» стало утверждением " +
			"ни о чём. Заведи близнеца или перепиши проверку под новое устройство дерева")
	}
	t.Logf("перепись: профилей %d · называют край %d (объявляют сами %d · наследуют %d · "+
		"записанных исключений %d) · законных близнецов %d",
		len(profiles), namesEdge, declaresOwn, inherits, excluded, silentTwins)
}

// f1cChainsNaming — имена цепочек, называющих этот профиль, в устойчивом порядке.
func f1cChainsNaming(stacks map[string][]string, profile string) []string {
	var out []string
	for name, chain := range stacks {
		for _, p := range chain {
			if p == profile {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// f1cDeclaringLayerBelow — ПОСЛЕДНИЙ слой цепочки, лежащий строго раньше
// профиля и объявляющий непустой перечень. Именно он и выигрывает: helm
// накладывает слева направо, и объявление, найденное раньше, ничего не говорит
// о том, что получит процесс, если ниже есть второе.
//
// Умолчания чарта донором НЕ считаются намеренно: объявлять перечень в них
// запрещено (см. записанное исключение для values.yaml), и засчитывать их здесь
// значило бы сделать эту ветвь тождественно истинной.
func f1cDeclaringLayerBelow(t *testing.T, chain []string, profile string) (string, bool) {
	t.Helper()
	last, found := "", false
	for _, layer := range chain {
		if layer == profile {
			break
		}
		if gw := f1cEdgeBlock(t, layer); gw != nil && f1cDeclares(gw) {
			last, found = layer, true
		}
	}
	return last, found
}

// ─────────────────────────────────────────────────────────────────────────────
// (3) САМОПРОВЕРКА: предикат СПОСОБЕН упасть, и падает он ровно на своём факте.

// TestF1c_TheIssuerSetPredicateCanFail — половины отличаются ровно ОДНИМ фактом.
//
// Вход берётся НАСТОЯЩИЙ — блок края живого профиля дерева, — а не сочинённый:
// сочинённый вход доказывает свойство сочинителя. Дефектная половина получается
// СНЯТИЕМ объявления из этого самого блока.
func TestF1c_TheIssuerSetPredicateCanFail(t *testing.T) {
	// Донор — профиль, объявляющий перечень СЕГОДНЯ, выведенный из дерева.
	var donor string
	for _, p := range f1cProfileNames(t) {
		if gw := f1cEdgeBlock(t, p); gw != nil && f1cDeclares(gw) {
			donor = p
			break
		}
	}
	if donor == "" {
		t.Fatalf("в дереве нет НИ ОДНОГО профиля, объявляющего перечень издателей — " +
			"брать настоящий вход неоткуда, и самопроверка сказана ни о чём")
	}
	live := f1cEdgeBlock(t, donor)
	if !f1cDeclares(live) {
		t.Fatalf("донор %s перестал объявлять перечень", donor)
	}

	cases := []struct {
		name   string
		gw     map[string]any
		expect bool
	}{
		{"как в дереве", live, true},
		{"объявление снято", f1cWithout(live, "tokenAcceptance"), false},
		{"перечень пуст", f1cWithIssuers(live, ""), false},
		{"перечень из одних разделителей", f1cWithIssuers(live, " , , "), false},
	}
	defective := 0
	for _, c := range cases {
		if !c.expect {
			defective++
		}
		if got := f1cDeclares(c.gw); got != c.expect {
			t.Errorf("%s (донор %s): предикат сказал %v, ожидалось %v", c.name, donor, got, c.expect)
		}
	}

	t.Logf("самопроверка: донор %s, осмотрено половин %d (дефектных %d)", donor, len(cases), defective)
}

// f1cWithout — копия блока края БЕЗ одного ключа.
func f1cWithout(gw map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, v := range gw {
		if k == key {
			continue
		}
		out[k] = v
	}
	return out
}

// f1cWithIssuers — копия блока края с подменённым перечнем издателей.
func f1cWithIssuers(gw map[string]any, issuers string) map[string]any {
	out := map[string]any{}
	for k, v := range gw {
		out[k] = v
	}
	ta := map[string]any{}
	if cur, ok := gw["tokenAcceptance"].(map[string]any); ok {
		for k, v := range cur {
			ta[k] = v
		}
	}
	ta["issuers"] = issuers
	out["tokenAcceptance"] = ta
	return out
}
