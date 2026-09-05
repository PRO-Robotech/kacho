// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_refusal_reason_coverage_injection_test.go — доказательство, что гейт
// покрытия СПОСОБЕН упасть, СПОСОБЕН смолчать и знает ВСЕ ТРИ законные формы
// записи токена.
//
// Вход подаётся СТРОКОЙ: доказательство, трогающее дерево, испортило бы чужую
// рабочую копию (`multi-agent-flow.md` §13), а доказательство на копии разбора
// говорило бы о копии, а не о том, что исполняется.
//
// ПО ПРОБЕ НА ФОРМУ (testing.md §«Гейт на класс», п. 7). Форма, распознавателю
// неизвестная, не даёт ни красного, ни зелёного — она МОЛЧИТ, и всё записанное
// в ней оказывается вне наблюдения. Именно так и было: предикат задачи знал одну
// форму (константа с именем `reason*`) и в одном сервисе — он назвал ЧЕТЫРЕ
// токена там, где дерево несёт ВОСЕМНАДЦАТЬ в семи.
//
// У каждой формы стоит ЗАКОННЫЙ БЛИЗНЕЦ: без него гейт ловил бы форму записи, а
// не предмет, и первый же ложный срабат его отключил бы.

package deploy_test

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// ФОРМА A — литерал прямо в составном литерале ErrorInfo.
const srcFormDirect = `package middleware

func denyStatus() *status.Status {
	info := &errdetails.ErrorInfo{
		Reason: "AUTHZ_DENIED",
		Domain: "kacho.cloud.iam.v1",
	}
	return st
}`

// ФОРМА B — закрытый словарь полос.
const srcFormTyped = `package errors

var (
	ReasonPeerUnavailable = Reason{token: "PEER_UNAVAILABLE", code: codes.Unavailable}
)`

// ФОРМА C — константа в файле, который СТРОИТ ErrorInfo.
const srcFormConst = `package shared

const reasonQuotaRateExceeded = "QUOTA_RATE_EXCEEDED"

func refuse() error {
	return st.WithDetails(&errdetails.ErrorInfo{Reason: reasonQuotaRateExceeded})
}`

// ЗАКОННЫЙ БЛИЗНЕЦ формы C: константа названа похоже, но файл ErrorInfo НЕ
// строит — это не признак отказа клиенту, и считать его находкой значило бы
// требовать вердикта консоли от внутренней причины реконсиляции.
const srcFormConstTwin = `package domain

const ReasonBackendUnavailable StatusReason = "BACKEND_UNAVAILABLE"

func decide() StatusReason { return ReasonBackendUnavailable }`

func TestScannerKnowsEveryLegalFormOfWritingAToken(t *testing.T) {
	for _, c := range []struct {
		name string
		src  string
		want string
	}{
		{"форма A — литерал в ErrorInfo", srcFormDirect, "AUTHZ_DENIED"},
		{"форма B — закрытый словарь полос", srcFormTyped, "PEER_UNAVAILABLE"},
		{"форма C — константа рядом с ErrorInfo", srcFormConst, "QUOTA_RATE_EXCEEDED"},
	} {
		got := scanGoSource(c.src)
		if !containsWhere(got, c.want) {
			t.Errorf("%s: токен %q НЕ РАСПОЗНАН (получено %v) — всё записанное в этой форме "+
				"оказалось бы вне наблюдения: ни красного, ни зелёного, молчание", c.name, c.want, got)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ обязан молчать.
	if got := scanGoSource(srcFormConstTwin); len(got) != 0 {
		t.Errorf("константа состояния в файле БЕЗ ErrorInfo зачтена признаком отказа (%v) — "+
			"гейт ловит форму имени, а не предмет, и первый же ложный срабат его отключит", got)
	}
}

func TestCoverageJudgeFallsAndStaysSilentInBothDirections(t *testing.T) {
	produced := map[string][]string{
		"AUTHZ_DENIED":   {"gateway/internal/middleware/permission_denied_response.go"},
		"QUOTA_EXCEEDED": {"services/iam/internal/apps/kaname/shared/quota.go"},
	}

	// ПРОГОН 1 — КОНТРОЛЬ: множества равны, гейт молчит с обеих сторон.
	missing, orphan := judgeCoverage(produced, map[string]bool{
		"AUTHZ_DENIED": true, "QUOTA_EXCEEDED": true,
	})
	if len(missing) != 0 || len(orphan) != 0 {
		t.Fatalf("контроль: на равных множествах гейт обязан молчать, получено missing=%v orphan=%v — "+
			"проверка, красная на верном дереве, будет отключена первой", missing, orphan)
	}

	// ПРОГОН 2 — ПРОИЗВЕДЕНО, НЕ РАЗОБРАНО: токен доезжает до арендатора
	// необъяснённым. Это ровно то состояние, в котором дерево было до #1736.
	missing, orphan = judgeCoverage(produced, map[string]bool{"QUOTA_EXCEEDED": true})
	if len(missing) != 1 || missing[0] != "AUTHZ_DENIED" {
		t.Errorf("непокрытый токен НЕ НАЗВАН: missing=%v — находка, не называющая координату, "+
			"посылает читателя искать не там", missing)
	}
	if len(orphan) != 0 {
		t.Errorf("прогон 2 уронил ВТОРУЮ сторону (orphan=%v) — инъекция обязана ронять только "+
			"проверяемое, иначе красное приходит от соседа", orphan)
	}

	// ПРОГОН 3 — САМОИСТЕЧЕНИЕ: вердикт есть, производителя нет. Без этого
	// прогона молчание второй стороны в прогоне 2 неотличимо от молчания мёртвой.
	missing, orphan = judgeCoverage(produced, map[string]bool{
		"AUTHZ_DENIED": true, "QUOTA_EXCEEDED": true, "QUOTA_RETIRED_LANE": true,
	})
	if len(orphan) != 1 || orphan[0] != "QUOTA_RETIRED_LANE" {
		t.Errorf("вердикт, которому нечего разбирать, НЕ НАЙДЕН: orphan=%v — послабление "+
			"пережило бы свой предмет и выглядело работающим", orphan)
	}
	if len(missing) != 0 {
		t.Errorf("прогон 3 уронил ПЕРВУЮ сторону (missing=%v) — инъекция роняет не своё", missing)
	}
}

func TestVerdictDictionaryParserSeesEntriesAndRefusesAMissingDeclaration(t *testing.T) {
	const dict = `type RefusalVerdict = { kind: "passthrough" };

const REFUSALS: Record<string, RefusalVerdict> = {
  AUTHZ_DENIED: { kind: "explain", text: FORBIDDEN_EXPLANATION },
  QUOTA_EXCEEDED: { kind: "quota", lane: "exceeded" },
};

const QUOTA_TITLES: Record<QuotaLane, string> = {
  NOT_AN_ENTRY: "за пределами блока",
};`

	got, ok := parseVerdictDict(dict)
	if !ok {
		t.Fatal("объявление словаря не распознано на законном тексте")
	}
	if !got["AUTHZ_DENIED"] || !got["QUOTA_EXCEEDED"] {
		t.Errorf("записи словаря не прочитаны: %v", got)
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ: ключ СОСЕДНЕГО объявления не есть вердикт. Счётчик,
	// читающий файл целиком вместо блока, объявил бы лишнюю запись и был бы
	// красен самоистечением на верном дереве.
	if got["NOT_AN_ENTRY"] {
		t.Error("ключ соседнего объявления зачтён вердиктом — разбор берёт файл, а не блок")
	}

	// Словаря нет вовсе — перепись беспредметна, и это ОТКАЗ, а не ноль находок.
	if _, ok := parseVerdictDict(strings.ReplaceAll(dict, "const REFUSALS", "const RENAMED")); ok {
		t.Error("отсутствие объявления прочитано как пустой словарь — тогда снятие словаря " +
			"давало бы зелёный гейт при нуле разобранных полос")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// КЛАССИФИКАТОР ПОТРЕБИТЕЛЯ — исключение задано ПУТЁМ, и оно обязано истекать.
//
// Проба заведена вместе с обобщением перечня: прежде исключений было два и оба с
// одним потребителем, поэтому «полоса потока» и «не консоль» были одним и тем же
// множеством. Теперь это РАЗНЫЕ множества, и различие обязано быть проверено, а
// не подразумеваться — иначе первый же путь, добавленный без основания, стал бы
// молчаливым послаблением шириной в каталог.
//
// Утверждается ОБЕ стороны: путь запросной полосы обязан классифицироваться
// консольным (пустой потребитель), путь вне консоли — назвать СВОЕГО. Без первой
// половины проба зеленела бы на классификаторе, исключающем всё подряд; без
// второй — на классификаторе, не исключающем ничего.
func TestOffConsoleClassifierNamesAConsumerAndOnlyWhereItShould(t *testing.T) {
	consoleLane := []string{
		"services/iam/internal/apps/kaname/api/internal_iam/handler.go",
		"pkg/errors/reason.go",
		"gateway/internal/middleware/permission_denied_response.go",
		// Законный близнец: имя каталога начинается ТАК ЖЕ, но каталог другой.
		// Исключение по префиксу без разделителя приняло бы его под себя.
		"pkg/subscriptionpolicy/refusal.go",
	}
	for _, rel := range consoleLane {
		if got := offConsoleConsumer(rel); got != "" {
			t.Errorf("%s отнесён вне консоли (потребитель %q) — токены запросной полосы "+
				"перестали бы требовать вердикта, и это послабление шириной в каталог", rel, got)
		}
	}

	offConsole := map[string]string{
		"pkg/subscription/server.go":                     "хаб подписки браузера",
		"gateway/internal/subscriptionstream/handler.go": "хаб подписки браузера",
		"pkg/subjectchange/positionlost.go":              "читатель отзыва края",
	}
	for rel, want := range offConsole {
		got := offConsoleConsumer(rel)
		if got == "" {
			t.Errorf("%s не исключён — от него потребовали бы вердикта консоли, "+
				"которого его отказ не достигает ни при каком входе", rel)
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s исключён с потребителем %q, ожидалось упоминание %q — "+
				"основание исключения обязано быть названо, иначе запись не истечёт", rel, got, want)
		}
	}
}
