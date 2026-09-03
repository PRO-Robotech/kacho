// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// proxytupleintent_injection_test.go — доказательство способности второй величины
// переписи упасть И смолчать (задача продукта #1983).
//
// Инъекция снимает НОВОЕ свойство у элемента, чьё СТАРОЕ на месте: писатель
// остаётся писателем, у него отнимается ровно личность, по которой судится
// владение типом. Форма «завести ещё один элемент» здесь была бы негодной —
// новый писатель нарушал бы всё, что требуется от писателей вообще, и красное
// пришло бы от соседа.
//
// Осей ТРИ, а не одна, потому что предикат носителя состоит из двух половин, и
// каждая ловит свою популяцию:
//
//   - служба, которой в словаре написаний НЕТ вовсе;
//   - служба, которая объявлена, но домена типов объекта не несёт (так у
//     каталога размещения) — приставочная ветвь правила на ней отвечает «не
//     знаю», то есть владение не судится;
//   - ЗАКОННЫЙ БЛИЗНЕЦ: служба, у которой написания РАЗЛИЧНЫ (балансировщик —
//     служба `nlb`, модуль `loadbalancer`). Предикат «короткое имя совпадает с
//     модулем каталога» отнял бы у неё три живых типа; здесь она обязана
//     молчать.
package repohygiene

import (
	"strings"
	"testing"
)

// TestProxyIdentityCarriersRedsOnAWriterWithoutAnIdentity — ось 1: службы нет в
// словаре написаний вовсе.
func TestProxyIdentityCarriersRedsOnAWriterWithoutAnIdentity(t *testing.T) {
	// КОНТРОЛЬ: те же писатели без инъекции — слепых ноль.
	base := []string{"compute", "nlb", "registry", "storage", "vpc"}
	carriers, blind := proxyIdentityCarriers(base)
	if len(blind) != 0 {
		t.Fatalf("КОНТРОЛЬ: живые писатели объявлены слепыми: %v", blind)
	}
	if len(carriers) != len(base) {
		t.Fatalf("КОНТРОЛЬ: носителей %d из %d — предикат считает не то, что объявляет",
			len(carriers), len(base))
	}

	hit := append(append([]string(nil), base...), "madeup")
	carriers, blind = proxyIdentityCarriers(hit)
	if len(blind) != 1 || blind[0] != "madeup" {
		t.Fatalf("писатель без объявленной личности НЕ стал находкой: слепых %v\n"+
			"Пока служба не объявлена в словаре написаний, обе ветви правила приёма "+
			"отвечают на неё «не знаю» — то есть владение типом по ней не судится ничем",
			blind)
	}
	if len(carriers) != len(base) {
		t.Fatalf("инъекция сдвинула носителей: %d вместо %d — красное пришло не оттуда",
			len(carriers), len(base))
	}
}

// TestProxyIdentityCarriersRedsOnAWriterWithoutAnObjectDomain — ось 2:
// служба объявлена, а домена типов объекта не несёт.
//
// Без этой оси предикат мог бы сузиться до «объявлена», и каталог размещения,
// начни он эмитировать, читался бы судимым, не будучи им: приставочная ветвь на
// пустом домене отвечает «не знаю», а словарная судит лишь типы, известные
// закрытой таблице.
func TestProxyIdentityCarriersRedsOnAWriterWithoutAnObjectDomain(t *testing.T) {
	carriers, blind := proxyIdentityCarriers([]string{"vpc", "geo"})
	if len(blind) != 1 || blind[0] != "geo" {
		t.Fatalf("объявленная служба БЕЗ домена типов объекта не стала находкой: слепых %v\n"+
			"Объявленность — половина предиката: приставочная ветвь правила судит "+
			"владение доменом типов, и на пустом отвечает «не знаю»", blind)
	}
	if len(carriers) != 1 || carriers[0] != "vpc" {
		t.Fatalf("носители сдвинулись: %v — красное пришло не от предмета оси", carriers)
	}
}

// TestProxyIdentityCarriersStaysSilentOnDivergentSpellings — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// У балансировщика различны два написания из трёх. Гейт, судящий совпадение
// короткого имени с модулем каталога, отверг бы его — и отверг бы законное.
func TestProxyIdentityCarriersStaysSilentOnDivergentSpellings(t *testing.T) {
	carriers, blind := proxyIdentityCarriers([]string{"nlb"})
	if len(blind) != 0 {
		t.Fatalf("служба с РАЗЛИЧНЫМИ написаниями объявлена слепой: %v\n"+
			"Написаний у неё три и совпадают не все — но личность её объявлена, и "+
			"владение по ней судится обеими ветвями", blind)
	}
	if len(carriers) != 1 || carriers[0] != "nlb" {
		t.Fatalf("законный близнец не признан носителем: %v", carriers)
	}
}

// TestProxyIntentCensusFindingNamesTheBlindWriter — находка НАЗЫВАЕТ виновника,
// а не только считает.
//
// Находка, называющая симптом вместо предмета, посылает читателя искать не там;
// на неё тратят прогон, а потом снимают гейт как непонятный.
func TestProxyIntentCensusFindingNamesTheBlindWriter(t *testing.T) {
	_, blind := proxyIdentityCarriers([]string{"vpc", "madeup", "geo"})
	joined := strings.Join(blind, ",")
	for _, want := range []string{"geo", "madeup"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("перечень слепых писателей не называет %q: %v", want, blind)
		}
	}
	if strings.Contains(joined, "vpc") {
		t.Fatalf("носитель попал в перечень слепых: %v", blind)
	}
}
