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
		Domain: "kaname.cloud.iam.v1",
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
	noExternal := map[string]string{}
	missing, orphan := judgeCoverageWithExternal(produced, map[string]bool{
		"AUTHZ_DENIED": true, "QUOTA_EXCEEDED": true,
	}, noExternal)
	if len(missing) != 0 || len(orphan) != 0 {
		t.Fatalf("контроль: на равных множествах гейт обязан молчать, получено missing=%v orphan=%v — "+
			"проверка, красная на верном дереве, будет отключена первой", missing, orphan)
	}

	// ПРОГОН 2 — ПРОИЗВЕДЕНО, НЕ РАЗОБРАНО: токен доезжает до арендатора
	// необъяснённым. Это ровно то состояние, в котором дерево было до #1736.
	missing, orphan = judgeCoverageWithExternal(produced, map[string]bool{"QUOTA_EXCEEDED": true}, noExternal)
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
	missing, orphan = judgeCoverageWithExternal(produced, map[string]bool{
		"AUTHZ_DENIED": true, "QUOTA_EXCEEDED": true, "QUOTA_RETIRED_LANE": true,
	}, noExternal)
	if len(orphan) != 1 || orphan[0] != "QUOTA_RETIRED_LANE" {
		t.Errorf("вердикт, которому нечего разбирать, НЕ НАЙДЕН: orphan=%v — послабление "+
			"пережило бы свой предмет и выглядело работающим", orphan)
	}
	if len(missing) != 0 {
		t.Errorf("прогон 3 уронил ПЕРВУЮ сторону (missing=%v) — инъекция роняет не своё", missing)
	}

	// ПРОГОН 4 — ЗАКОННЫЙ БЛИЗНЕЦ ВЕДОМОСТИ: вердикт есть, производителя в
	// дереве нет, и токен ОБЪЯВЛЕН производимым в другом репозитории. Гейт
	// обязан молчать: иначе он краснел бы на верно исполненном разрезе и толкал
	// снимать вердикт консоли — то есть ломать продукт ради зелёного.
	missing, orphan = judgeCoverageWithExternal(produced, map[string]bool{
		"AUTHZ_DENIED": true, "QUOTA_EXCEEDED": true, "REFERENCE_MISSING": true,
	}, map[string]string{"REFERENCE_MISSING": "PRO-Robotech/kaname"})
	if len(orphan) != 0 || len(missing) != 0 {
		t.Errorf("объявленный внешний производитель дал находки missing=%v orphan=%v — "+
			"гейт краснеет на верной работе", missing, orphan)
	}

	// ПРОГОН 5 — ОБРАТНАЯ ОСЬ ВЕДОМОСТИ: токен объявлен производимым вне дерева,
	// а производитель нашёлся ЗДЕСЬ. Запись пережила свой предмет, и без этой
	// оси она не истекала бы никогда.
	missing, orphan = judgeCoverageWithExternal(produced, map[string]bool{
		"AUTHZ_DENIED": true, "QUOTA_EXCEEDED": true,
	}, map[string]string{"QUOTA_EXCEEDED": "PRO-Robotech/kaname"})
	if len(orphan) != 1 || !strings.Contains(orphan[0], "QUOTA_EXCEEDED") {
		t.Errorf("вернувшийся производитель НЕ НАЗВАН: orphan=%v — послабление осталось бы "+
			"прикрывать живую координату", orphan)
	}
	if len(missing) != 0 {
		t.Errorf("прогон 5 уронил ПЕРВУЮ сторону (missing=%v) — инъекция роняет не своё", missing)
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
//
// СИНТЕТИЧЕСКИЕ ПУТИ ПЕРЕНАЦЕЛЕНЫ, И ЭТО НЕ КОСМЕТИКА. Здесь стояли
// `pkg/subscription/server.go` и `pkg/subjectchange/positionlost.go` модуля
// `github.com/PRO-Robotech/kaname` — обе
// координаты умерли вместе со своими каталогами (фундамент в #2131, служба доступа
// в kacho#2616), и записи перечня, которые они предъявляли, сняты как исключения
// без предмета. Проба, продолжавшая требовать их исключения, требовала бы
// ВОЗВРАТА снятых записей — то есть держала бы освобождение шириной в каталог за
// каталогами, которых нет.
func TestOffConsoleClassifierNamesAConsumerAndOnlyWhereItShould(t *testing.T) {
	consoleLane := []string{
		"services/iam/internal/apps/kaname/api/internal_iam/handler.go",
		"pkg/errors/reason.go",
		"gateway/internal/middleware/permission_denied_response.go",
		// Законный близнец: имя каталога начинается ТАК ЖЕ, но каталог другой.
		// Исключение по префиксу без разделителя приняло бы его под себя.
		"gateway/internal/subscriptionstreampolicy/refusal.go",
	}
	for _, rel := range consoleLane {
		if got := offConsoleConsumer(rel); got != "" {
			t.Errorf("%s отнесён вне консоли (потребитель %q) — токены запросной полосы "+
				"перестали бы требовать вердикта, и это послабление шириной в каталог", rel, got)
		}
	}

	offConsole := map[string]string{
		"gateway/internal/subscriptionstream/handler.go": "хаб подписки браузера",
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

// ─────────────────────────────────────────────────────────────────────────────
// САМОИСТЕЧЕНИЕ ИСКЛЮЧЕНИЯ ПО ПУТИ — предикат обязан краснеть на записи без
// предмета и молчать на записи с предметом.
//
// Заведено вместе с предикатом, потому что ловит класс, который до него был
// НЕВИДИМ: перечень печатался по ПОТРЕБИТЕЛЯМ, и мёртвый путь делил потребителя с
// живым — в переписи стояло имя потребителя, а токены под ним давал сосед.
// Измеренная цена: два пути из трёх не давали ни одного токена, гейт был зелёным,
// и перепись это не показывала.
//
// Вход подаётся СТРОКАМИ, а не деревом: предикат чистый, и доказательство обязано
// уметь предъявить обе стороны на одном и том же перечне.
func TestOffConsolePathWithoutSubjectIsAFinding(t *testing.T) {
	live := offConsolePath{prefix: "gateway/internal/subscriptionstream/", consumer: "хаб подписки браузера"}
	dead := offConsolePath{prefix: "pkg/gone/", consumer: "читатель, которого нет"}
	produced := []producedReason{
		{token: "SUBSCRIPTION_SUBJECT_STREAM_LIMIT", where: "gateway/internal/subscriptionstream/limit.go"},
		{token: "PROJECT_NOT_FOUND", where: "services/vpc/internal/apps/kacho/api/network/create.go"},
	}

	// Положительный контроль: путь, из-под которого токен пришёл, — НЕ находка.
	deadPaths, subjects := pathsWithoutSubject(produced, []offConsolePath{live})
	if len(deadPaths) != 0 {
		t.Errorf("живое исключение объявлено без предмета: %v — предикат краснел бы на "+
			"законной записи, и его сняли бы первым", deadPaths)
	}
	if subjects[live.prefix] != 1 {
		t.Errorf("токенов из-под живого пути насчитано %d, ожидалось 1 — единица счёта "+
			"сбита, и «ноль» станет неотличим от «не посчитано»", subjects[live.prefix])
	}

	// Отрицательная сторона: та же популяция, путь без предмета — находка.
	deadPaths, subjects = pathsWithoutSubject(produced, []offConsolePath{live, dead})
	if len(deadPaths) != 1 || !strings.Contains(deadPaths[0], dead.prefix) {
		t.Errorf("путь без предмета не назван находкой: %v — исключение шириной в каталог "+
			"переживало бы свой предмет молча", deadPaths)
	}
	if subjects[dead.prefix] != 0 {
		t.Errorf("токенов из-под мёртвого пути насчитано %d, ожидался 0", subjects[dead.prefix])
	}
	// Находка обязана НАЗЫВАТЬ объявленного потребителя: без него читатель не
	// поймёт, что именно снимать.
	if !strings.Contains(deadPaths[0], dead.consumer) {
		t.Errorf("находка не называет объявленного потребителя (%q): %q", dead.consumer, deadPaths[0])
	}

	// Пустая популяция НЕ ВСЕРАЗРЕШЕНИЕ: при нуле произведённых токенов КАЖДОЕ
	// объявление — без предмета, и предикат обязан сказать это, а не промолчать.
	deadPaths, _ = pathsWithoutSubject(nil, []offConsolePath{live, dead})
	if len(deadPaths) != 2 {
		t.Errorf("на пустой популяции находок %d из 2 — пустой вход стал всеразрешением", len(deadPaths))
	}
}
