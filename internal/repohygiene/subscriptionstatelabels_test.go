// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionstatelabels_test.go — гейт «обещание отбирать по меткам на
// клиенте ИСПОЛНИМО у каждого вида».
//
// # Предмет
//
// Серверного отбора по меткам у подписки нет, и это решение, а не пропуск:
// сервер видит ТОЛЬКО текущее состояние строки журнала, поэтому выход ресурса из
// выборки — правку метки — он выразить не может, а подавив событие, оставит
// подписчика с ресурсом, который выборке больше не отвечает, молча и навсегда.
// Отбор по меткам делает КЛИЕНТ, и контракт с клиентской страницей это обещают.
//
// Обещание исполнимо ровно до тех пор, пока состояние несёт метки. Вид, чьё
// состояние их не несёт, делает его неисполнимым — и делает молча: подписка
// откроется, события пойдут, отбирать будет не из чего.
//
// Разбор, числа и цена обеих сторон — docs/architecture/subscription-label-filter.md.
//
// # Почему источника ДВА
//
// Виды считаются по объявлениям владельцев, типы состояния — по клиентской
// странице. Сверка их ЧИСЕЛ и есть проверка полноты обхода: один источник
// молчал бы одинаково и на чистом дереве, и на разборе, потерявшем половину
// предмета.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// subscriptionPage — клиентская страница подписки: единственное место, где
	// типы состояния названы полными именами контракта.
	subscriptionPage = "gateway/docs/content/api/subscription.mdx"
	// journalDeclDir — где живут объявления журналов владельцев. Перечень
	// владельцев ВЫВОДИТСЯ обходом этого шаблона, а не выписывается.
	journalDeclSuffix = "/internal/subscriptionjournal/journal.go"
	// protoDomainRoot — дерево контрактов.
	protoDomainRoot = "proto/kacho/cloud"
)

// TestSubscriptionStateCarriesLabelsForEveryKind — сам гейт.
func TestSubscriptionStateCarriesLabelsForEveryKind(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// ── Источник 1: владельцы журнала и их виды.
	var owners []JournalKinds
	var ownerRels []string
	for rel := range tt.files {
		if strings.HasPrefix(rel, "services/") && strings.HasSuffix(rel, journalDeclSuffix) {
			ownerRels = append(ownerRels, rel)
		}
	}
	sort.Strings(ownerRels)
	kindsTotal := 0
	for _, rel := range ownerRels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		k, err := ScanJournalKinds(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		owners = append(owners, k)
		kindsTotal += k.Count
	}

	// ── Источник 2: типы состояния на клиентской странице.
	pageSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(subscriptionPage)))
	if err != nil {
		t.Fatalf("чтение %s: %v — страница есть ЕДИНСТВЕННОЕ место, где типы состояния "+
			"названы полными именами; без неё гейт беспредметен", subscriptionPage, err)
	}
	types, mentions := ScanStateTypesOnPage(string(pageSrc))

	t.Logf("перепись: владельцев журнала %d, видов в их словарях %d; "+
		"страница %s — байт %d, упоминаний типов %d, различных типов %d",
		len(owners), kindsTotal, subscriptionPage, len(pageSrc), mentions, len(types))
	for _, o := range owners {
		t.Logf("  владелец %s — видов %d", o.File, o.Count)
	}

	// ── Предпосылки. Каждая отвечает на свой вопрос.
	if len(owners) == 0 {
		t.Fatalf("владельцев журнала НОЛЬ — либо подписку сняли целиком, либо обход "+
			"перестал их находить по %q. Гейт беспредметен: по молчанию эти два случая "+
			"неразличимы", journalDeclSuffix)
	}
	if kindsTotal == 0 {
		t.Fatal("видов в словарях владельцев НОЛЬ — владелец с пустым словарём не " +
			"поднимается вовсе, значит разбор объявления сломан")
	}
	if len(types) == 0 {
		t.Fatalf("на странице %s не названо НИ ОДНОГО типа состояния — либо страница "+
			"переписана, либо разбор перестал их видеть", subscriptionPage)
	}

	// ── Полнота обхода: числа двух источников обязаны сойтись.
	if len(types) != kindsTotal {
		t.Errorf("видов у владельцев %d, а типов состояния на странице %d — сойтись "+
			"обязаны.\nРасхождение означает одно из двух, и оба требуют работы: у вида "+
			"появилось состояние, о котором клиентская страница молчит (клиент не знает, "+
			"что может отбирать по меткам сам), либо страница называет тип, которого "+
			"больше не производит ни один владелец (мёртвая координата в обещании).\n"+
			"Типы страницы: %s", kindsTotal, len(types), joinTypes(types))
	}

	// ── Требование: у каждого типа состояния есть метки.
	var findings []string
	withLabels := 0
	for _, ty := range types {
		dir := filepath.Join(root, filepath.FromSlash(protoDomainRoot), ty.Domain, "v1")
		entries, err := os.ReadDir(dir)
		if err != nil {
			findings = append(findings, fmt.Sprintf(
				"%s — каталога контракта %s/%s/v1 нет: страница называет домен, которого "+
					"в дереве не существует", ty.FullName(), protoDomainRoot, ty.Domain))
			continue
		}
		found := false
		labels := false
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".proto") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			l, ok := MessageCarriesLabels(string(src), ty.Message)
			if ok {
				found, labels = true, l
				break
			}
		}
		switch {
		case !found:
			findings = append(findings, fmt.Sprintf(
				"%s — сообщения с таким именем в контрактах домена НЕТ. Клиентская "+
					"страница обещает состояние типа, которого не существует", ty.FullName()))
		case !labels:
			findings = append(findings, fmt.Sprintf(
				"%s — состояние этого вида НЕ несёт меток.\n     Отбор по меткам объявлен "+
					"клиентским: клиент отбирает там, где событие принесло состояние. Без "+
					"поля меток обещание неисполнимо ни при каком входе — подписка "+
					"откроется, события пойдут, отбирать будет не из чего.\n     Исходов "+
					"три: завести поле меток у ресурса · снять вид из словаря владельца · "+
					"назвать исключение на клиентской странице и в "+
					"docs/architecture/subscription-label-filter.md", ty.FullName()))
		default:
			withLabels++
		}
	}

	t.Logf("типов состояния с метками %d из %d", withLabels, len(types))

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("видов, у которых обещанный клиентский отбор по меткам НЕИСПОЛНИМ — %d:\n  %s\n\n"+
			"Серверного отбора по меткам у подписки нет намеренно: у сервера нет "+
			"предыдущего состояния строки, поэтому выход ресурса из выборки он выразить "+
			"не может. Решение и цена обеих сторон — "+
			"docs/architecture/subscription-label-filter.md",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// joinTypes — перечень полных имён одной строкой: находка обязана показывать то,
// что нашла.
func joinTypes(types []StateType) string {
	var names []string
	for _, t := range types {
		names = append(names, t.FullName())
	}
	return strings.Join(names, ", ")
}
