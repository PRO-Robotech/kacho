// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_delivery_flows_declared_once_test.go — MAIL-18 приёмки ID-MAIL-1.
//
// # Предмет
//
// Потоки, ДОСТАВЛЯЕМЫЕ ПИСЬМОМ — подтверждение адреса и восстановление доступа, —
// объявляются ОДИН раз, в нашей конфигурации личности. Профиль, доводящий её до
// процесса и при этом высказывающийся о тех же потоках сам, заводит второе место
// об одном предмете: процесс получает два источника настроек и сливает их по
// порядку, то есть какая из двух величин победит, решает ПОРЯДОК, которого никто
// не выбирал.
//
// # Почему это ровно класс #1234, а не его экземпляр
//
// «После выхода войти нельзя» складывается из двух половин, каждая из которых
// защитима: требование подтверждённого адреса разумно, и выключенный поток
// подтверждения разумен там, где доставки нет. Неверна их СУММА — и увидеть её
// нельзя, читая по файлу: каждый файл внутренне согласован. Расходятся не файлы,
// а их эффективный набор.
//
// Наблюдалось на этом дереве: наша конфигурация объявляет поток подтверждения
// ВКЛЮЧЁННЫМ, а два профиля объявляли его выключенным в конфигурации поставщика.
// Обе величины доезжали до процесса, и какая применится — не решал никто.
//
// # Чем этот гейт отличается от соседей — граница названа, чтобы не завелось дублирование
//
// Соседний `identity_second_factor_reachable_test.go` судит ТОТ ЖЕ класс на
// методах второго фактора; здесь — на потоках доставки и почтовой полосе.
// `identity_mail_lane_single_declaration_test.go` (MAIL-54) судит ЕДИНСТВЕННОСТЬ
// объявления почтовой полосы во всём дереве; здесь — ПРОФИЛЬ, заводящий второе
// мнение о ней. Ни один не задаёт вопроса «высказывается ли этот СТЕНД о потоке
// доставки в обход единственного объявления» — его задаёт только этот.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
package deploy_test

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// deliveryFlowKey — имена, высказывание о которых в профиле есть второе мнение о
// потоке, ДОСТАВЛЯЕМОМ ПИСЬМОМ.
//
// Перечень узкий намеренно. `registration`, `login`, `settings` и `error`
// письмом не доставляются: они здесь не судятся, потому что гейт, краснеющий на
// них, был бы красным на исправном дереве — а такой снимают первым, и вместе с
// ним уходит требование. Появится поток, доставляемый письмом, — имя
// добавляется сюда вместе с ним.
var deliveryFlowKey = regexp.MustCompile(`(?m)^\s+(verification|recovery)\s*:`)

// mailLaneKeyInProfile — второе мнение о самой почтовой полосе, высказанное
// профилем. Держится тем же гейтом: полоса — такой же предмет единственного
// объявления, что и потоки, которые её используют.
var mailLaneKeyInProfile = regexp.MustCompile(`(?m)^\s+(courier)\s*:`)

// shadowedDeliveryFlows — потоки доставки и почтовая полоса, о которых профиль
// высказался САМ, в обход единственного объявления.
func shadowedDeliveryFlows(text string) []string {
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{deliveryFlowKey, mailLaneKeyInProfile} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			seen[m[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// deliveryFlowFindings — находки MAIL-18 по НАЗВАННОМУ корню.
//
// Корень параметризован затем, чтобы доказательство способности гейта упасть шло
// по копии в t.TempDir(), а не по рабочему дереву.
func deliveryFlowFindings(t *testing.T, root string) []string {
	t.Helper()
	stacks := deployStacks(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []string
	raising, mounting, clean := 0, 0, 0
	for _, name := range names {
		chain := stacks[name]
		texts := make([]string, 0, len(chain))
		for _, prof := range chain {
			texts = append(texts, readFileForTest(t, filepath.Join(root, prof)))
		}
		if !identityChainRaisesIdentity(texts) {
			continue
		}
		raising++
		if !identityChainMountsOurConfig(texts) {
			// Не наш предмет: стенд, не доводящий нашу конфигурацию до
			// процесса, судит соседний гейт, и его находка там уже названа.
			// Утверждать о нём ещё и здесь значило бы завести второе место об
			// одном предмете — ровно то, что этот гейт и запрещает.
			continue
		}
		mounting++

		shadowed := map[string][]string{}
		for i, prof := range chain {
			if sh := shadowedDeliveryFlows(texts[i]); len(sh) > 0 {
				shadowed[prof] = sh
			}
		}
		if len(shadowed) == 0 {
			clean++
			continue
		}
		profs := make([]string, 0, len(shadowed))
		for p := range shadowed {
			profs = append(profs, p)
		}
		sort.Strings(profs)
		var parts []string
		for _, p := range profs {
			parts = append(parts, p+" → "+strings.Join(shadowed[p], ", "))
		}
		findings = append(findings, "стенд "+name+" доводит нашу конфигурацию личности до "+
			"процесса и ПРИ ЭТОМ его профили сами высказываются о потоках доставки:\n    "+
			strings.Join(parts, "\n    ")+
			"\n  Процесс получает два источника настроек и сливает их по порядку — какая "+
			"из двух величин победит, решает порядок, которого никто не выбирал. Поток, "+
			"доставляемый письмом, объявляется ОДИН раз, в "+identityConfigTemplate+".\n"+
			"  Это ровно #1234: требование подтверждённого адреса приходит из одного "+
			"источника, а выключенный поток подтверждения — из другого, и ни одна "+
			"пофайловая проверка этого не видит.")
	}

	if raising == 0 {
		t.Fatalf("ни один стенд не поднимает службу личности — проверка беспредметна, "+
			"и её зелёный ничего не значит (корень %s)", root)
	}
	t.Logf("перепись стендов: объявлено %d · поднимают службу личности %d · доводят "+
		"нашу конфигурацию до процесса %d · не заводят второго мнения о потоках "+
		"доставки %d", len(stacks), raising, mounting, clean)
	return findings
}

// TestIdentity_DeliveryFlowsAreDeclaredOnce — MAIL-18 на рабочем дереве.
func TestIdentity_DeliveryFlowsAreDeclaredOnce(t *testing.T) {
	for _, f := range deliveryFlowFindings(t, umbrellaDir) {
		t.Error(f)
	}
}
