// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_verified_address_required_on_both_lanes_test.go — MAIL-52 приёмки
// ID-MAIL-1.
//
// # Предмет и почему гейт заведён ЗАРАНЕЕ
//
// Задача #1234 («после выхода войти нельзя») закрывается тем, что подтверждение
// адреса ДОСТАВЛЯЕТСЯ, а требование подтверждённого адреса на входе ОСТАЁТСЯ.
// У этого требования сегодня ноль утверждающих — и это опасно ровно в момент
// первой неудачи с доставкой: самый дешёвый способ вернуть вход состоит в том,
// чтобы снять требование с полосы. Продукт после этого впускает человека с
// неподтверждённым адресом, все пробы зелены, и вердикт остаётся верным про
// НЕВЕРНЫЙ продукт.
//
// Поэтому гейт ложится ДО правок доставки, а не после: пока требования нечем
// удержать, вся фаза теряет предмет.
//
// # Требование ПРЕВЕНТИВНОЕ, и это сказано заранее
//
// Сегодняшняя раскладка гейт НЕ роняет: требование стоит на обеих полосах. Его
// штатное состояние — молчание, и это не «нечего проверять»: первая же полоса,
// у которой требование снимут, станет находкой с её именем. Способность упасть
// доказана инъекцией в соседнем файле, а не предполагается.
//
// # Зеркало названо соседом, а не переписано
//
// Обратная сторона — «требование стоит, а поток подтверждения выключен» — судит
// MAIL-16 (identity_verification_mirrors_the_requirement_test.go) на ЭФФЕКТИВНОМ
// наборе стенда. Здесь она не воспроизводится: два места об одном предмете
// разошлись бы молча, и это ровно тот класс, который обе проверки ловят.
//
// Здесь стоял MAIL-18. Это было утверждение о СОСЕДЕ, сделанное до того, как
// сосед появился: MAIL-18 судит ФОРМУ («у потока есть второе объявление») и о
// значениях набора не спрашивает — поток, выключенный в ЕДИНСТВЕННОМ
// объявлении, для него молчание. То есть зеркало было объявлено покрытым и
// покрыто не было.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// verifiedAddressHook — имя хука службы личности, которым требование
// подтверждённого адреса и выражается. Второго способа его выразить в этой
// конфигурации нет; появится — имя добавляется сюда вместе с ним.
const verifiedAddressHook = "require_verified_address"

// loginLanesWithRequirement разбирает объявление полос входа НАШЕЙ конфигурации
// личности: имя полосы → несёт ли она требование подтверждённого адреса.
//
// Разбор идёт по отступам, а не YAML-парсером: тело шаблона несёт выражения
// `{{ … }}` и валидным YAML не является by construction.
func loginLanesWithRequirement(t *testing.T, body string) map[string]bool {
	t.Helper()
	lines := strings.Split(body, "\n")

	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

	// `login:` внутри блока потоков, затем его `after:`.
	loginAt, loginIndent := -1, 0
	for i, l := range lines {
		if strings.TrimSpace(l) == "login:" {
			loginAt, loginIndent = i, indentOf(l)
			break
		}
	}
	if loginAt < 0 {
		t.Fatalf("в объявлении конфигурации личности нет блока `login:` — форма " +
			"сменилась, и полосы входа вывести неоткуда. «Ноль находок» здесь " +
			"неотличимо от «ноль прочитанного», поэтому это отказ, а не тишина")
	}

	afterAt, afterIndent := -1, 0
	for i := loginAt + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if indentOf(l) <= loginIndent {
			break // блок `login:` кончился
		}
		if strings.TrimSpace(l) == "after:" {
			afterAt, afterIndent = i, indentOf(l)
			break
		}
	}
	if afterAt < 0 {
		t.Fatalf("у блока `login:` нет `after:` — полос входа с хуками не объявлено " +
			"вовсе, и требование подтверждённого адреса не стоит НИ НА ОДНОЙ. Это " +
			"отказ, а не тишина: именно так выглядит снятое требование")
	}

	lanes := map[string]bool{}
	var cur string
	var curIndent int
	for i := afterAt + 1; i < len(lines); i++ {
		l := lines[i]
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		ind := indentOf(l)
		if ind <= afterIndent {
			break // блок `after:` кончился
		}
		// Имя полосы — ключ на первом уровне под `after:`.
		if cur == "" || ind == curIndent {
			if name, _, ok := splitYAMLPair(trimmed); ok && !strings.HasPrefix(trimmed, "-") {
				cur, curIndent = name, ind
				if _, seen := lanes[cur]; !seen {
					lanes[cur] = false
				}
				continue
			}
		}
		if cur != "" && strings.Contains(trimmed, verifiedAddressHook) {
			lanes[cur] = true
		}
	}
	return lanes
}

// verifiedAddressFindings — находки MAIL-52 по НАЗВАННОМУ телу объявления.
func verifiedAddressFindings(t *testing.T, body string) []string {
	t.Helper()
	lanes := loginLanesWithRequirement(t, body)

	names := make([]string, 0, len(lanes))
	for n := range lanes {
		names = append(names, n)
	}
	sort.Strings(names)

	with := 0
	var findings []string
	for _, n := range names {
		if lanes[n] {
			with++
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"полоса входа %q не несёт требования подтверждённого адреса (%s).\n"+
				"  Продукт впустит человека с НЕподтверждённым адресом, и все пробы "+
				"останутся зелёными: вердикт будет верен про неверный продукт.\n"+
				"  Если требование снимается осознанно — снимайте его на ВСЕХ полосах "+
				"и вместе с потоком подтверждения, а не на одной: полосы, разошедшиеся "+
				"как побочный эффект чужой правки, — тот класс, который этот гейт ловит",
			n, verifiedAddressHook))
	}

	t.Logf("перепись: полос входа прочитано %d (%v) · из них с требованием "+
		"подтверждённого адреса %d", len(names), names, with)
	if len(names) == 0 {
		t.Fatalf("полос входа не прочитано ни одной — предикат перестал их узнавать; " +
			"это отказ, а не тишина")
	}
	return findings
}

// TestIdentity_VerifiedAddressRequiredOnEveryLoginLane — MAIL-52 на рабочем дереве.
func TestIdentity_VerifiedAddressRequiredOnEveryLoginLane(t *testing.T) {
	body := readFileForTest(t, identityConfigTemplate)
	for _, f := range verifiedAddressFindings(t, body) {
		t.Error(f)
	}
}
