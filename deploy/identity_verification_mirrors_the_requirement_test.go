// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_verification_mirrors_the_requirement_test.go — MAIL-16 и MAIL-17
// приёмки ID-MAIL-1.
//
// # Предмет: ЗЕРКАЛО требования, а не второе мнение о нём
//
// Вход требует подтверждённого адреса. Подтверждение доставляется письмом, и
// доставляется оно только если поток подтверждения ВКЛЮЧЁН. Требование без
// включённого потока означает ровно одно: человека впустить нельзя НИКОГДА, и
// ни одна из двух половин по отдельности неверной не выглядит.
//
// Это задача #1234 целиком. Складывается она из СУММЫ источников: требование
// приходит из одного, выключение — из другого, и по файлу каждый источник
// внутренне согласован.
//
// # ЕДИНИЦА — ЭФФЕКТИВНЫЙ НАБОР СТЕНДА, и это измерено, а не выбрано
//
// Пофайловая проверка здесь зелена на обеих сторонах BY CONSTRUCTION, поэтому
// не измеряет ничего. Замер до написания гейта (§11 шаг 2 приёмки — проверяется
// ДО, а не после), два предиката об одном предмете на двух ревизиях дерева:
//
//	                              пофайловый      по эффективному набору
//	до сведения (46770d597)       11 · 0 находок  6 стендов · 5 находок
//	после сведения (a0d3f4135)    11 · 0 находок  6 стендов · 0 находок
//
// То есть пофайловый предикат не отличает состояние ДО фикса от состояния
// ПОСЛЕ: он молчит одинаково. Способность этого гейта различить их доказана
// инъекцией в соседнем файле, и там же — подпроба, показывающая обе величины
// на одном и том же дефекте.
//
// # ГРАНИЦЫ С СОСЕДЯМИ — названы, чтобы двух гейтов об одном предмете не завелось
//
//   - MAIL-52 (identity_verified_address_required_on_both_lanes_test.go) судит
//     ОДНУ половину: стоит ли требование на каждой полосе входа. О потоке
//     подтверждения он не спрашивает вовсе.
//   - MAIL-18 (identity_delivery_flows_declared_once_test.go) судит ФОРМУ: есть
//     ли у потока второе объявление. Он краснеет на втором мнении независимо от
//     его ЗНАЧЕНИЯ — и молчит там, где мнение ОДНО и оно неверное.
//   - Здесь судится СУММА ЗНАЧЕНИЙ. Отсюда два следствия, каждое проверено
//     инъекцией: профиль, повторивший `enabled: true` вслед за единственным
//     объявлением, для этого гейта — молчание (его предмет у MAIL-18); а поток,
//     выключенный В САМОМ единственном объявлении, — находка ЗДЕСЬ и нигде
//     больше, потому что второго места в этом состоянии нет by construction.
//
// Последнее и есть довод в пользу существования гейта после того, как потоки
// сведены к одному месту: сведение убрало ВТОРОЕ мнение, но не сделало ПЕРВОЕ
// неотзываемым. Самый дешёвый способ вернуть вход при первой неудаче с
// доставкой — снять требование (это ловит MAIL-52) либо выключить поток (это
// ловит только этот гейт).
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// identityConfigInUmbrella — путь единственного объявления ОТНОСИТЕЛЬНО корня
// зонтичного чарта. ВЫВЕДЕН из уже существующей координаты, а не выписан второй
// раз: копия разошлась бы с оригиналом молча — ровно тот класс, который этот
// гейт и ловит.
var identityConfigInUmbrella = strings.TrimPrefix(identityConfigTemplate, umbrellaDir+"/")

// verificationInlineFlow — ПОТОЧНАЯ форма объявления: `verification: { … }`.
// Скобки допускают перенос строк — так пишет профиль боевой посадки.
// Ключ обязан быть НЕзакавыченным: `"verification": { "via": "email" }` внутри
// схемы личности — это про способ доставки поля, а не про поток, и считать её
// объявлением потока значило бы краснеть на исправном дереве.
var verificationInlineFlow = regexp.MustCompile(`(?m)^[ \t]*verification[ \t]*:[ \t]*\{([^}]*)\}`)

// verificationBlockFlow — БЛОЧНАЯ форма: ключ на своей строке, величина ниже с
// бо́льшим отступом.
var verificationBlockFlow = regexp.MustCompile(`(?m)^([ \t]*)verification[ \t]*:[ \t]*$`)

// enabledValue — величина `enabled` в любой из двух форм.
var enabledValue = regexp.MustCompile(`\benabled[ \t]*:[ \t]*(true|false)\b`)

// stripYAMLLineComments убирает прозу, оставляя объявления.
//
// Гейт, читающий сырой текст, находит своё имя в комментарии, ОБЪЯСНЯЮЩЕМ этот
// же предмет, и остаётся зелёным при снятом объявлении (`testing.md` §«Гейт на
// класс», п. 4). Здесь это не гипотеза: комментарии, которыми сведение потоков
// заменило снятые блоки, содержат и слово `verification`, и слово `enabled`.
//
// Кавычки учитываются: `#` внутри строки-значения (например во фрагменте
// адреса) комментария не открывает.
func stripYAMLLineComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		inSingle, inDouble := false, false
		for j := 0; j < len(l); j++ {
			switch l[j] {
			case '\'':
				if !inDouble {
					inSingle = !inSingle
				}
			case '"':
				if !inSingle {
					inDouble = !inDouble
				}
			case '#':
				if inSingle || inDouble {
					continue
				}
				if j == 0 || l[j-1] == ' ' || l[j-1] == '\t' {
					lines[i] = l[:j]
					j = len(l)
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// declarationsOnly — тело источника без прозы обоих родов: комментариев шаблона
// (`{{/* … */}}`) и построчных комментариев YAML.
func declarationsOnly(src string) string {
	return stripYAMLLineComments(stripTemplateComments(src))
}

// verificationVoice — что ОДИН источник эффективного набора говорит.
type verificationVoice struct {
	source   string
	requires bool // объявляет требование подтверждённого адреса
	enables  bool // объявляет поток подтверждения включённым
	disables bool // объявляет поток подтверждения выключенным
}

// verificationVoiceOf разбирает один источник.
//
// ФОРМ ЗАПИСИ ДВЕ, и обе законны в этом дереве: блочная (наша конфигурация,
// профиль стенда) и поточная (профиль боевой посадки). Форма, о которой
// распознаватель не знает, — не край и не редкость: всё записанное в ней
// оказывается ВНЕ НАБЛЮДЕНИЯ, то есть ни находкой, ни молчанием
// (`testing.md` §«Гейт на класс», п. 7). Обе доказаны инъекцией порознь.
func verificationVoiceOf(source, raw string) verificationVoice {
	text := declarationsOnly(raw)
	v := verificationVoice{source: source, requires: strings.Contains(text, verifiedAddressHook)}

	note := func(on bool) {
		if on {
			v.enables = true
		} else {
			v.disables = true
		}
	}

	for _, m := range verificationInlineFlow.FindAllStringSubmatch(text, -1) {
		if val := enabledValue.FindStringSubmatch(m[1]); val != nil {
			note(val[1] == "true")
		}
	}

	lines := strings.Split(text, "\n")
	for _, m := range verificationBlockFlow.FindAllStringSubmatchIndex(text, -1) {
		indent := len(text[m[2]:m[3]])
		start := strings.Count(text[:m[0]], "\n") + 1
		for _, l := range lines[start:] {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if len(l)-len(strings.TrimLeft(l, " \t")) <= indent {
				break // блок кончился
			}
			if val := enabledValue.FindStringSubmatch(l); val != nil {
				note(val[1] == "true")
				break
			}
		}
	}
	return v
}

// verificationVoicesOfStack — все источники ОДНОГО стенда: единственное
// объявление, базовые значения зонтичного чарта и цепочка профилей.
//
// Базовые значения входят в набор потому, что helm накладывает их всегда; без
// них «эффективный набор» был бы набором лишь наполовину.
func verificationVoicesOfStack(t *testing.T, root string, chain []string) []verificationVoice {
	t.Helper()
	sources := append([]string{identityConfigInUmbrella, "values.yaml"}, chain...)
	out := make([]verificationVoice, 0, len(sources))
	for _, s := range sources {
		out = append(out, verificationVoiceOf(s, readFileForTest(t, filepath.Join(root, s))))
	}
	return out
}

func verificationSourcesWhere(voices []verificationVoice, pick func(verificationVoice) bool) []string {
	var out []string
	for _, v := range voices {
		if pick(v) {
			out = append(out, v.source)
		}
	}
	return out
}

// verificationMirrorFindings — находки MAIL-16 по НАЗВАННОМУ корню.
//
// Корень параметризован затем, чтобы доказательство способности гейта упасть
// шло по копии в `t.TempDir()`, а не по рабочему дереву.
//
// ПРАВИЛО ПОРЯДКОМ НЕ ПОЛЬЗУЕТСЯ НАМЕРЕННО. Какой из двух файлов победит при
// слиянии — вопрос, который приёмка не решает чтением (§1.6), и решать его
// здесь значило бы утверждать о поведении чужого двоичного файла. Находкой
// объявляется САМО наличие противоречия в наборе: требование есть, а поток либо
// кем-то выключен, либо не включён никем.
func verificationMirrorFindings(t *testing.T, root string) []string {
	t.Helper()
	stacks := deployStacks(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []string
	raising, requiring, coherent := 0, 0, 0
	// Объявления считаются по ИСТОЧНИКАМ, а не по прочтениям: один и тот же файл
	// входит в набор нескольких стендов, и счёт прочтений сказал бы «шесть
	// объявлений» о единственном.
	enablingSources, disablingSources := map[string]bool{}, map[string]bool{}

	for _, name := range names {
		voices := verificationVoicesOfStack(t, root, stacks[name])

		texts := make([]string, 0, len(voices))
		for _, s := range stacks[name] {
			texts = append(texts, readFileForTest(t, filepath.Join(root, s)))
		}
		if !identityChainRaisesIdentity(texts) {
			continue
		}
		raising++

		requires := verificationSourcesWhere(voices, func(v verificationVoice) bool { return v.requires })
		on := verificationSourcesWhere(voices, func(v verificationVoice) bool { return v.enables })
		off := verificationSourcesWhere(voices, func(v verificationVoice) bool { return v.disables })
		for _, src := range on {
			enablingSources[src] = true
		}
		for _, src := range off {
			disablingSources[src] = true
		}

		if len(requires) == 0 {
			continue
		}
		requiring++
		if len(off) == 0 && len(on) > 0 {
			coherent++
			continue
		}

		reason := fmt.Sprintf("поток подтверждения ВЫКЛЮЧЕН источниками %v", off)
		if len(off) == 0 {
			reason = "поток подтверждения не включает НИ ОДИН источник набора — " +
				"применится умолчание поставщика, а оно этот поток не поднимает"
		}
		findings = append(findings, fmt.Sprintf(
			"стенд %s: требование подтверждённого адреса объявлено (%s, источники %v), "+
				"и в том же эффективном наборе %s (включают %v).\n"+
				"  Человека впустить нельзя НИКОГДА: вход требует подтверждения, а "+
				"подтверждать нечем — письмо не отправляется потоком, которого нет.\n"+
				"  Это задача #1234 целиком, и складывается она только из СУММЫ "+
				"источников: по файлу каждый из них внутренне согласован, поэтому ни "+
				"одна пофайловая проверка этого не видит.\n"+
				"  Чинится ПАРОЙ и осознанно: либо поток включается там, где объявлен "+
				"(%s), либо требование снимается со ВСЕХ полос входа вместе с потоком. "+
				"Половина — это и есть нынешнее состояние.",
			name, verifiedAddressHook, requires, reason, on, identityConfigInUmbrella))
	}

	// ── ПРЕДПОСЫЛКИ ГЕЙТА — проверяются, а не подразумеваются ────────────────
	if raising == 0 {
		t.Fatalf("ни один стенд не поднимает службу личности — проверка беспредметна, "+
			"и её зелёный ничего не значит (корень %s)", root)
	}
	if len(enablingSources)+len(disablingSources) == 0 {
		t.Fatalf("объявлений потока подтверждения не прочитано НИ ОДНОГО (корень %s) — "+
			"распознаватель перестал их узнавать либо форма записи сменилась. "+
			"«Ноль находок» здесь неотличимо от «ноль прочитанного», поэтому это "+
			"отказ, а не тишина", root)
	}
	if requiring == 0 {
		t.Fatalf("требование подтверждённого адреса (%s) не объявлено НИ ОДНИМ стендом "+
			"(корень %s) — предмет этого гейта исчез. Зеркалить нечего, и молчание "+
			"здесь означало бы «проверка не нашла», а не «нарушения нет». Требование "+
			"снято осознанно — снимайте этот гейт ТЕМ ЖЕ изменением; снято не "+
			"осознанно — это находка MAIL-52", verifiedAddressHook, root)
	}

	t.Logf("перепись: стендов объявлено %d · поднимают службу личности %d · требуют "+
		"подтверждённого адреса %d · из них поток подтверждения включён и не выключен "+
		"никем %d · источников, объявляющих поток, %d (включают %d · выключают %d)",
		len(stacks), raising, requiring, coherent,
		len(enablingSources)+len(disablingSources), len(enablingSources), len(disablingSources))
	return findings
}

// verificationMirrorPerFileFindings — ТО ЖЕ ПРАВИЛО, применённое к каждому
// источнику ПО ОТДЕЛЬНОСТИ.
//
// В вердикт дерева не входит и входить не должен: это измерительный прибор,
// которым инъекция показывает, что пофайловая единица счёта на этом предмете
// молчит — и молчит одинаково до фикса и после. Держать его рядом с гейтом
// дешевле, чем помнить замер: число, оставленное в комментарии, стареет молча.
func verificationMirrorPerFileFindings(t *testing.T, root string) []string {
	t.Helper()
	stacks := deployStacks(t)

	seen := map[string]bool{identityConfigInUmbrella: true, "values.yaml": true}
	for _, chain := range stacks {
		for _, p := range chain {
			seen[p] = true
		}
	}
	sources := make([]string, 0, len(seen))
	for s := range seen {
		sources = append(sources, s)
	}
	sort.Strings(sources)

	var findings []string
	for _, s := range sources {
		v := verificationVoiceOf(s, readFileForTest(t, filepath.Join(root, s)))
		if v.requires && (v.disables || !v.enables) {
			findings = append(findings, s)
		}
	}
	t.Logf("пофайловая перепись: источников прочитано %d · находок %d",
		len(sources), len(findings))
	return findings
}

// TestIdentity_VerificationFlowMirrorsTheVerifiedAddressRequirement — MAIL-16 на
// рабочем дереве.
//
// MAIL-17 (положительный контроль: согласованный набор молчит) утверждается
// двумя местами сразу — переписью выше, где число согласованных стендов
// печатается ОТДЕЛЬНО от числа прочитанных, и законными близнецами инъекции.
// Одного молчания мало: молчание исправного гейта и молчание сломанного
// выглядят одинаково.
func TestIdentity_VerificationFlowMirrorsTheVerifiedAddressRequirement(t *testing.T) {
	for _, f := range verificationMirrorFindings(t, umbrellaDir) {
		t.Error(f)
	}
}
