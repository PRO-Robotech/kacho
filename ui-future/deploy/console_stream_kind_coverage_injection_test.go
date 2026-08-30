// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_stream_kind_coverage_injection_test.go — доказательство, что гейт
// обратного направления СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Словарь берётся ТОТ ЖЕ, которым доказывает себя соседний гейт
// (`probeDictionary`): второй синтетический словарь об одном предмете разошёлся
// бы с первым молча.
//
// Вход подаётся строкой: доказательство, трогающее дерево, испортило бы чужую
// рабочую копию, а доказательство на копии разбора говорило бы о копии.
//
// ПРОГОНОВ ТРИ, а не два (testing.md §«Гейт на класс», п. 2в): контроль ·
// инъекция НОВОГО свойства · инъекция СУЩЕСТВУЮЩЕГО. Без третьего молчание
// соседнего гейта в прогоне 2 неотличимо от молчания мёртвого.

package deploy_test

import (
	"strings"
	"testing"
)

const (
	// srcCoverageControl — карта, называющая ВСЕ шесть видов синтетического
	// словаря, при пустой ведомости. Законное дерево: молчат оба гейта.
	//
	// Полей `kind:` здесь шесть, и ни одно из них НЕ принадлежит ведомости —
	// счётчик, считающий файл целиком вместо блока, объявил бы шесть невидимых
	// записей и был бы красен на верном дереве.
	srcCoverageControl = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  snapshots: { owner: "storage", kind: "storage_snapshot" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};

/**
 * Образец записи — ЗАКОММЕНТИРОВАН, действующей записью не является:
 *   { kind: "storage_snapshot", why: "спеки списка нет" },
 */
export const UNSHOWN_KINDS: readonly UnshownKind[] = [];
`

	// srcCoverageUncovered — снят `snapshots`: вид объявлен журналом, картой не
	// назван, ведомостью не объявлен. Инъекция НОВОГО свойства.
	srcCoverageUncovered = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [];
`

	// srcCoverageUncoveredDeclared — ЗАКОННЫЙ БЛИЗНЕЦ предыдущего: тот же
	// непокрытый вид, но объявленный ведомостью с причиной. Гейт обязан смолчать,
	// иначе он судит покрытие, а решено судить осознанность.
	srcCoverageUncoveredDeclared = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [
  { kind: "storage_snapshot", why: "у снимка нет страницы списка — он живёт вкладкой тома" },
];
`

	// srcCoverageUndeclaredKind — карта называет вид, которого не объявляет ни один
	// журнал. Инъекция СУЩЕСТВУЮЩЕГО свойства: краснеет СОСЕДНИЙ гейт, новый молчит.
	srcCoverageUndeclaredKind = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  gateways: { owner: "vpc", kind: "vpc_gateway" },
  volumes: { owner: "storage", kind: "storage_volume" },
  snapshots: { owner: "storage", kind: "storage_snapshot" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [];
`

	// srcCoverageStaleLedger — запись про вид, который карта ВСЁ ЖЕ называет:
	// исключение потеряло предмет.
	srcCoverageStaleLedger = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  snapshots: { owner: "storage", kind: "storage_snapshot" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [
  { kind: "storage_snapshot", why: "страницы нет" },
];
`

	// srcCoveragePhantomLedger — запись про вид, которого не объявляет никто.
	srcCoveragePhantomLedger = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  snapshots: { owner: "storage", kind: "storage_snapshot" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [
  { kind: "vpc_gateway", why: "страницы нет" },
];
`

	// srcCoverageReasonless — запись без причины: прощает молча.
	srcCoverageReasonless = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [
  { kind: "storage_snapshot", why: "" },
];
`

	// srcCoverageUnknownForm — запись ОБРАТНЫМ порядком полей. Разбор её не знает,
	// и она обязана быть НАЙДЕНА счётчиком, а не выпасть из осмотренного молча.
	srcCoverageUnknownForm = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [
  { why: "страницы нет", kind: "storage_snapshot" },
];
`

	// srcCoverageNoLedger — объявления ведомости нет вовсе.
	srcCoverageNoLedger = `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
};
`
)

// judgeCoverageProbe — одна инъекция через ТЕ ЖЕ функции, которые зовёт гейт.
func judgeCoverageProbe(t *testing.T, src string) consoleKindCoverageVerdict {
	t.Helper()
	ledger, declared, found := unshownKindsOf(src)
	if !found {
		t.Fatalf("ведомость не найдена в синтетике — инъекция говорила бы не о том")
	}
	if declared != len(ledger) {
		t.Fatalf("разбор ведомости отстал от объёма: объявлено %d, разобрано %d", declared, len(ledger))
	}
	return judgeConsoleKindCoverage(probeDictionary(t), consoleStreamSubjectsOf(src), ledger)
}

// TestKindCoverageInjectionRunOne_Control — ПРОГОН 1 из трёх: всё цело, молчат ОБА
// гейта. Без него молчание существующего контроля в прогоне 2 неотличимо от
// молчания мёртвого.
func TestKindCoverageInjectionRunOne_Control(t *testing.T) {
	got := judgeCoverageProbe(t, srcCoverageControl)
	if !got.empty() {
		t.Fatalf("контроль: гейт покрытия нашёл нарушение на законном дереве: %+v", got)
	}
	if got.CoveredByMap != 6 || got.CoveredByLedger != 0 {
		t.Fatalf("контроль: закрыто картой %d, ведомостью %d — ожидалось 6 и 0; "+
			"перепись, не сходящаяся со входом, делает вердикт непроверяемым",
			got.CoveredByMap, got.CoveredByLedger)
	}
	if neighbour := judgeConsoleKinds(consoleStreamSubjectsOf(srcCoverageControl),
		probeDictionary(t)); !neighbour.empty() {
		t.Fatalf("контроль: СОСЕДНИЙ гейт нашёл нарушение на законном дереве: %+v", neighbour)
	}

	// Ведомость ПУСТА, и это не поломка: пустая — цель. Счётчик обязан прочесть
	// НОЛЬ, хотя полей `kind:` в файле шесть — все они принадлежат карте предметов.
	ledger, declared, found := unshownKindsOf(srcCoverageControl)
	if !found || declared != 0 || len(ledger) != 0 {
		t.Fatalf("контроль: пустая ведомость прочитана как найдена=%v, объявлено=%d, "+
			"разобрано=%d — счётчик считает файл вместо блока и был бы красен на "+
			"верном дереве", found, declared, len(ledger))
	}
}

// TestKindCoverageInjectionRunTwo_NewPropertyOnly — ПРОГОН 2: снято НОВОЕ свойство
// (вид объявлен, не назван, не объявлен непоказанным). Краснеет только новый гейт.
func TestKindCoverageInjectionRunTwo_NewPropertyOnly(t *testing.T) {
	got := judgeCoverageProbe(t, srcCoverageUncovered)
	if len(got.Unjudged) != 1 {
		t.Fatalf("инъекция непокрытого вида: находок %d, ожидалась 1 — гейт не способен "+
			"упасть на предмете, ради которого заведён: %+v", len(got.Unjudged), got.Unjudged)
	}
	if !strings.Contains(got.Unjudged[0], "storage_snapshot") ||
		!strings.Contains(got.Unjudged[0], "services/storage/internal/subscriptionjournal") {
		t.Fatalf("находка не называет КООРДИНАТУ (журнал и вид): %q. Находка, называющая "+
			"симптом, посылает читателя искать не там", got.Unjudged[0])
	}
	if got.CoveredByMap != 5 || len(got.Unjudged) != 1 {
		t.Fatalf("перепись не сошлась: закрыто картой %d при 6 объявленных", got.CoveredByMap)
	}

	// СОСЕДНИЙ гейт молчит: инъекция роняет ТОЛЬКО проверяемое.
	if neighbour := judgeConsoleKinds(consoleStreamSubjectsOf(srcCoverageUncovered),
		probeDictionary(t)); !neighbour.empty() {
		t.Fatalf("инъекция нового свойства уронила и СОСЕДНИЙ гейт (%+v) — красное пришло "+
			"бы от соседа, и новый мог бы оказаться вакуумным, не показав этого ничем",
			neighbour)
	}
}

// TestKindCoverageInjectionRunThree_ExistingPropertyOnly — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ свойство (карта называет вид, которого не объявляет никто).
// Краснеет только соседний гейт, новый молчит.
func TestKindCoverageInjectionRunThree_ExistingPropertyOnly(t *testing.T) {
	neighbour := judgeConsoleKinds(consoleStreamSubjectsOf(srcCoverageUndeclaredKind),
		probeDictionary(t))
	if len(neighbour.Undeclared) != 1 {
		t.Fatalf("инъекция существующего свойства: сосед нашёл %d, ожидалась 1 — его "+
			"молчание в прогоне 2 было бы неотличимо от молчания мёртвого: %+v",
			len(neighbour.Undeclared), neighbour)
	}
	if got := judgeCoverageProbe(t, srcCoverageUndeclaredKind); !got.empty() {
		t.Fatalf("инъекция ЧУЖОГО свойства уронила НОВЫЙ гейт: %+v. Он судит виды, "+
			"объявленные владельцем, и о неназванном владельцем виде не высказывается", got)
	}
}

// TestKindCoverageGateIsSilentOnADeclaredUnshownKind — ЗАКОННЫЙ БЛИЗНЕЦ прогона 2:
// тот же непокрытый вид, объявленный ведомостью с причиной. Гейт обязан смолчать.
func TestKindCoverageGateIsSilentOnADeclaredUnshownKind(t *testing.T) {
	got := judgeCoverageProbe(t, srcCoverageUncoveredDeclared)
	if !got.empty() {
		t.Fatalf("гейт краснеет на РЕШЁННОМ молчании: %+v. Тогда он судит покрытие, а "+
			"решено судить осознанность — и он требовал бы невозможного от вида без спеки",
			got)
	}
	if got.CoveredByLedger != 1 || got.CoveredByMap != 5 {
		t.Fatalf("перепись не различает, ЧЕМ закрыт вид: картой %d, ведомостью %d — "+
			"ожидалось 5 и 1", got.CoveredByMap, got.CoveredByLedger)
	}
}

// TestKindCoverageLedgerExpiresOnBothSides — послабление обязано истекать САМО.
func TestKindCoverageLedgerExpiresOnBothSides(t *testing.T) {
	stale := judgeCoverageProbe(t, srcCoverageStaleLedger)
	if len(stale.StaleUnshown) != 1 || len(stale.Unjudged) != 0 {
		t.Fatalf("запись, чей вид карта ВСЁ ЖЕ называет, не признана потерявшей предмет: %+v", stale)
	}
	phantom := judgeCoverageProbe(t, srcCoveragePhantomLedger)
	if len(phantom.PhantomUnshown) != 1 || len(phantom.Unjudged) != 0 {
		t.Fatalf("запись про вид, которого не объявляет ни один журнал, не признана "+
			"беспредметной: %+v", phantom)
	}
}

// TestKindCoverageReasonlessEntryIsAFinding — запись без причины прощает молча, и
// находка обязана быть ОДНА: «причины нет», а не ещё и «вид не судим» — вторая
// послала бы читателя чинить не то.
func TestKindCoverageReasonlessEntryIsAFinding(t *testing.T) {
	got := judgeCoverageProbe(t, srcCoverageReasonless)
	if len(got.ReasonMissing) != 1 {
		t.Fatalf("запись без причины не признана находкой: %+v", got)
	}
	if len(got.Unjudged) != 0 {
		t.Fatalf("запись без причины дала ВТОРУЮ находку («вид не судим»): %+v — читателя "+
			"послали бы чинить карту вместо причины", got.Unjudged)
	}
}

// TestKindCoverageLedgerCounterCatchesAnUnknownForm — запись формой, которой разбор
// не знает, обязана быть НАЙДЕНА, а не выпасть из осмотренного молча.
func TestKindCoverageLedgerCounterCatchesAnUnknownForm(t *testing.T) {
	ledger, declared, found := unshownKindsOf(srcCoverageUnknownForm)
	if !found {
		t.Fatal("ведомость не найдена — проверять нечего")
	}
	if declared != 1 || len(ledger) != 0 {
		t.Fatalf("объявлено %d, разобрано %d — ожидалось 1 и 0. Счётчик, не замечающий "+
			"незнакомой формы, оставил бы её вид непокрытым НЕЗАМЕТНО: «ноль находок» "+
			"означало бы «ноль прочитанного»", declared, len(ledger))
	}
}

// TestKindCoverageIgnoresACommentedEntryInsideTheBlock — гейт читает ИСПОЛНЯЕМОЕ,
// а не текст (testing.md §«Гейт на класс», п. 4).
//
// Закомментированная запись ВНУТРИ блока — не редкость и не край: образец формы
// естественно держать рядом с ведомостью, а объяснение снятой записи — на её
// месте. Разбор по сырому тексту прочёл бы прозу как действующее послабление и
// молча простил бы непокрытый вид.
func TestKindCoverageIgnoresACommentedEntryInsideTheBlock(t *testing.T) {
	src := `export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
};
export const UNSHOWN_KINDS: readonly UnshownKind[] = [
  // { kind: "storage_snapshot", why: "образец формы, а не объявление" },
  /* { kind: "registry_registry", why: "он же блоком" }, */
  { kind: "vpc_subnet", why: "единственная ДЕЙСТВУЮЩАЯ запись" },
];
`
	ledger, declared, found := unshownKindsOf(src)
	if !found || declared != 1 || len(ledger) != 1 {
		t.Fatalf("проза прочитана как объявление: найдена=%v, объявлено=%d, разобрано=%d "+
			"— ожидалось 1 и 1. Гейт, судящий сырой текст, простил бы непокрытый вид за "+
			"комментарий о нём", found, declared, len(ledger))
	}
	if ledger[0].Kind != "vpc_subnet" {
		t.Fatalf("разобрана НЕ действующая запись, а %q", ledger[0].Kind)
	}
}

// TestKindCoverageMissingLedgerDeclarationIsRefused — отсутствие объявления
// ведомости обязано быть ОТКАЗОМ: без неё гейт судил бы по пустому множеству и
// требовал бы покрытия, невыполнимого для вида без спеки.
func TestKindCoverageMissingLedgerDeclarationIsRefused(t *testing.T) {
	if _, _, found := unshownKindsOf(srcCoverageNoLedger); found {
		t.Fatal("ведомость «найдена» там, где её объявления нет — тогда её снятие прошло " +
			"бы молча, и законный исход стал бы невыразим")
	}
}

// TestKindCoverageLedgerAcceptsATemplateReason — причина длиной в несколько строк
// записывается шаблонным литералом: обычный литерал TypeScript переноса не умеет,
// и разбор, знающий одну форму, объявил бы такую запись невидимой.
func TestKindCoverageLedgerAcceptsATemplateReason(t *testing.T) {
	src := "export const STREAM_SUBJECTS = {\n" +
		"  networks: { owner: \"vpc\", kind: \"vpc_network\" },\n" +
		"};\n" +
		"export const UNSHOWN_KINDS: readonly UnshownKind[] = [\n" +
		"  { kind: \"storage_snapshot\", why: `у снимка нет страницы списка —\n" +
		"    он живёт вкладкой карточки тома` },\n" +
		"];\n"
	ledger, declared, found := unshownKindsOf(src)
	if !found || declared != 1 || len(ledger) != 1 {
		t.Fatalf("шаблонная причина не разобрана: найдена=%v, объявлено=%d, разобрано=%d",
			found, declared, len(ledger))
	}
	if ledger[0].Why == "" {
		t.Fatal("причина прочиталась пустой — запись была бы объявлена беспричинной, " +
			"то есть гейт краснел бы на законной форме")
	}
}
