// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// outboxobservedgate_injection_test.go — ИНЪЕКЦИЯ для проверки «колонки
// доставки, которые пишут и не читают».
//
// Обе стороны гоняют ТЕ ЖЕ функции, что и гейт по дереву:
// [migrationUpSection] → [deliveryColumnDeclsIn] на стороне схемы,
// [goDeliveryAdvancersIn] на стороне кода и [classifyDeliveryColumns] на стороне
// вердикта. Своя копия разбора доказывала бы свойство копии.
//
// # Почему законных близнецов ПЯТЬ, а не один
//
// Молчание достигается пятью РАЗНЫМИ путями, и каждый надо предъявить отдельно —
// иначе гейт неотличим от проверки «в дереве есть хоть одна очередь» и краснел
// бы на всех:
//
//	лента изменений без колонок доставки  — вопрос вообще не задаётся;
//	собственный оператор движения         — [deliveryAdvanced];
//	дренаж, поднятый проводкой            — [deliveryAdvanced];
//	сканер плюс запись «наблюдается»      — [deliveryVisiblySilent];
//	запись «двигать некому»               — [deliveryDeclaredSilent].
//
// # И три контроля, каждый на СВОЮ слепую зону
//
//	комментарий не является оператором  — оператор, стоящий только в комментарии
//	                                      Go, движителем НЕ считается;
//	откат не является накатом           — CREATE TABLE в Down-секции таблицы не
//	                                      заводит;
//	тип пишут в любом регистре          — колонка `sent_at TIMESTAMPTZ`
//	                                      распознаётся так же, как строчная.
//
// Последний контроль стоит здесь не для симметрии: первая редакция разбора
// принимала только строчный тип и не увидела ДВЕ очереди из одиннадцати — то
// есть молчала бы на них при снятом движителе.

import (
	"sort"
	"strings"
	"testing"
)

// injQueueMigration — миграция, заводящая очередь с колонками доставки.
//
// Текст сырой (с маркерами goose и комментариями) намеренно: инъекция обязана
// пройти тот же путь, что и дерево, включая вырезание комментариев.
const injQueueMigration = `-- Пример, который РАЗБОР ОБЯЗАН ПРОПУСТИТЬ:
-- CREATE TABLE ghost_outbox (
--   sent_at timestamptz
-- );

-- +goose Up

CREATE TABLE lonely_outbox (
    id         text PRIMARY KEY,
    payload    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ,
    attempts   integer NOT NULL DEFAULT 0
);

-- +goose Down

CREATE TABLE down_only_outbox (
    id      text PRIMARY KEY,
    sent_at timestamptz
);
DROP TABLE lonely_outbox;
`

// injFeedMigration — лента изменений: очередь по названию, но доставки не
// обещает. Законный близнец, на котором гейт обязан молчать.
const injFeedMigration = `-- +goose Up

CREATE TABLE feed_outbox (
    sequence_no bigserial PRIMARY KEY,
    kind        text NOT NULL,
    payload     jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
`

// injDroppedMigration — таблица, заведённая и снятая. Запись о ней не имеет
// права пережить свой предмет.
const injDroppedMigration = `-- +goose Up

DROP TABLE lonely_outbox;
`

// injAdvancerGo — прод-код, ДВИГАЮЩИЙ строку очереди.
const injAdvancerGo = `package repo

const markSent = ` + "`" + `UPDATE lonely_outbox SET sent_at = now() WHERE id = $1` + "`" + `
`

// injCommentOnlyGo — тот же оператор, но ТОЛЬКО в комментарии.
//
// Контроль на слепую зону «гейт читает текст, а не исполняемую часть»: приняв
// его за движитель, гейт молчал бы на очереди, у которой оператор сняли, а
// объяснение оставили.
const injCommentOnlyGo = `package repo

// Когда-то здесь было: UPDATE lonely_outbox SET sent_at = now() WHERE id = $1.
// Оператор снят вместе с дренажом.
const nothing = 1
`

// injDecls — разбор синтетических миграций ТЕМ ЖЕ входом, которым разбирается
// дерево ([deliveryColumnDeclsInRaw]).
//
// Подготовку текста (отрезание отката, вырезание комментариев) инъекция НЕ
// повторяет своими руками намеренно: повтори её здесь — и снять подготовку в
// обходе дерева можно было бы, не покраснев.
func injDecls(t *testing.T, svc string, raw ...string) []deliveryColumnDecl {
	t.Helper()
	files := make([]rawMigration, 0, len(raw))
	for i, body := range raw {
		files = append(files, rawMigration{
			path: svc + "/migrations/" + string(rune('a'+i)) + ".sql",
			body: body,
		})
	}
	return deliveryColumnDeclsInRaw(svc, files)
}

func injKeys(decls []deliveryColumnDecl) []string {
	out := make([]string, 0, len(decls))
	for _, d := range decls {
		out = append(out, d.key())
	}
	sort.Strings(out)
	return out
}

// TestDeliveryColumnScanFindsAQueueThatPromisesDelivery — сторона «краснеет».
func TestDeliveryColumnScanFindsAQueueThatPromisesDelivery(t *testing.T) {
	decls := injDecls(t, "svc", injQueueMigration)

	if got := injKeys(decls); len(got) != 1 || got[0] != "svc/lonely_outbox" {
		t.Fatalf("разбор обязан найти РОВНО одну очередь с колонками доставки, нашёл %v", got)
	}
	d := decls[0]
	if strings.Join(d.marks, ",") != "sent_at" {
		t.Errorf("колонки доставки прочитаны как %v, ожидалось [sent_at] — тип TIMESTAMPTZ "+
			"пишут заглавными, и разбор обязан это принимать", d.marks)
	}
	if !strings.HasSuffix(d.file, "a.sql") {
		t.Errorf("находка обязана нести координату своей миграции, несёт %q", d.file)
	}

	// Вердикт без движителя, без проводки и без записи — находка.
	if got := classifyDeliveryColumns(false, false, false, false, false); got != deliveryUnaccounted {
		t.Errorf("очередь без движителя и без записи обязана быть находкой, вердикт %v", got)
	}
}

// TestDeliveryColumnScanIgnoresCommentsAndDownSections — два контроля разбора
// схемы: пример в комментарии и таблица, заводимая ОТКАТОМ.
//
// Оба «не найдены» — но по разным причинам, и обе нужны: комментарий обязан
// вырезаться, а Down-секция обязана отрезаться ДО вырезания (маркер отката сам
// является комментарием).
func TestDeliveryColumnScanIgnoresCommentsAndDownSections(t *testing.T) {
	decls := injDecls(t, "svc", injQueueMigration)
	keys := injKeys(decls)
	for _, forbidden := range []string{"svc/ghost_outbox", "svc/down_only_outbox"} {
		for _, got := range keys {
			if got == forbidden {
				t.Errorf("разбор засчитал %s: %s. Гейт обязан читать ИСПОЛНЯЕМУЮ часть наката, "+
					"а не текст файла", forbidden,
					map[string]string{
						"svc/ghost_outbox":     "таблица показана примером в комментарии",
						"svc/down_only_outbox": "таблица заводится только откатом",
					}[forbidden])
			}
		}
	}
}

// TestDeliveryColumnScanSilentOnAChangeFeed — законный близнец: таблица с тем же
// суффиксом имени, но без обещания доставки.
//
// Без этой стороны предикат был бы «имя оканчивается на _outbox», и четыре ленты
// изменений дерева стали бы находками.
func TestDeliveryColumnScanSilentOnAChangeFeed(t *testing.T) {
	if got := injKeys(injDecls(t, "svc", injFeedMigration)); len(got) != 0 {
		t.Errorf("лента изменений доставки не обещает — колонок sent_at/next_attempt_at у неё "+
			"нет, и спрашивать с неё нечего; разбор нашёл %v", got)
	}
}

// TestDeliveryColumnScanDropsWhatTheTreeDropped — запись не переживает свой
// предмет.
func TestDeliveryColumnScanDropsWhatTheTreeDropped(t *testing.T) {
	if got := injKeys(injDecls(t, "svc", injQueueMigration, injDroppedMigration)); len(got) != 0 {
		t.Errorf("снятая таблица обязана уйти из переписи, иначе запись о ней переживёт свой "+
			"предмет; разбор нашёл %v", got)
	}
}

// TestDeliveryAdvancerReadsStatementsNotComments — оператор движения читается из
// строкового литерала, а не из текста файла.
func TestDeliveryAdvancerReadsStatementsNotComments(t *testing.T) {
	live := goDeliveryAdvancersIn(map[string]string{"repo/mark.go": injAdvancerGo})
	if len(live["lonely_outbox"]) != 1 {
		t.Errorf("настоящий оператор движения обязан быть найден: %v", live)
	}
	if got := classifyDeliveryColumns(true, false, false, false, false); got != deliveryAdvanced {
		t.Errorf("очередь с собственным оператором обязана считаться движимой, вердикт %v", got)
	}

	dead := goDeliveryAdvancersIn(map[string]string{"repo/dead.go": injCommentOnlyGo})
	if len(dead) != 0 {
		t.Errorf("оператор, оставшийся ТОЛЬКО в комментарии, движителем не является — иначе "+
			"гейт молчал бы на очереди, у которой оператор сняли, а объяснение оставили: %v", dead)
	}
}

// TestDeliveryClassificationCoversAllFourOutcomes — законные близнецы вердикта:
// дренаж, сканер с записью и запись «двигать некому» дают РАЗНЫЕ исходы, и ни
// один из них не находка.
//
// Исходы разведены намеренно: свернуть их в один булев «всё в порядке» значило бы
// записать «доставки нет» там, где она есть, и наоборот.
func TestDeliveryClassificationCoversAllFourOutcomes(t *testing.T) {
	cases := []struct {
		name                                         string
		advanced, drained, observed, noted, declared bool
		want                                         deliveryAccountability
	}{
		{"дренаж поднят", false, true, true, false, false, deliveryAdvanced},
		{"сканер и запись", false, false, true, true, false, deliveryVisiblySilent},
		{"названо записью", false, false, false, false, true, deliveryDeclaredSilent},
		{"никто и ничего", false, false, false, false, false, deliveryUnaccounted},
		// Сканер БЕЗ записи — не оправдание: величина публикуется, а почему её
		// некому двигать, не сказано нигде.
		{"сканер без записи", false, false, true, false, false, deliveryUnaccounted},
	}
	for _, c := range cases {
		got := classifyDeliveryColumns(c.advanced, c.drained, c.observed, c.noted, c.declared)
		if got != c.want {
			t.Errorf("%s: вердикт %v, ожидался %v", c.name, got, c.want)
		}
	}
}

// TestDeliveryColumnScanPremiseEmptyWhenNothingIsDeclared — отрицательный
// контроль ПРЕДПОСЫЛКИ: на дереве без единой очереди перепись обязана быть
// нулевой, а не тихо успешной.
//
// Гейт по дереву на такой переписи падает (`t.Fatalf` про сломанное
// распознавание) — и это и есть требование «ноль находок отличимо от ноль
// прочитанного».
func TestDeliveryColumnScanPremiseEmptyWhenNothingIsDeclared(t *testing.T) {
	if got := injKeys(injDecls(t, "svc", "-- +goose Up\n\nSELECT 1;\n")); len(got) != 0 {
		t.Errorf("на дереве без очередей перепись обязана быть пустой, нашлось %v", got)
	}
}
