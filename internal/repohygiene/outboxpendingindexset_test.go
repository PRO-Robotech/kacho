// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outboxpendingindexset_test.go — гейт на КЛАСС: набор частичных индексов по
// неотправленным строкам очереди.
//
// Предмет. Выборка очереди с ключом партиции устроена так, что её эффективность
// целиком держится на ранней остановке по LIMIT поверх УПОРЯДОЧЕННОГО прохода;
// доставить нужный порядок может только индекс этого порядка, любой другой путь
// доступа форсирует сортировку. Очередь при этом почти всё время пуста, поэтому
// последний сбор статистики почти всегда пришёлся на пустой бэклог, и во
// всплеск планировщик входит с оценкой в одну строку — а на ней сортировка
// бесплатна. Тогда ЛЮБОЙ более узкий частичный индекс по тем же строкам
// выглядит дешевле, план перестаёт останавливаться рано, и анти-соединение по
// партиции прогоняется по разу на каждую неотправленную строку: выборка читает
// всю очередь, которую разгребает.
//
// Почему гейт, а не пять правок. Класс был найден, обоснован числами (на живом
// стенде: 6 990 мс против 29 мс на один клейм при 8 600 неотправленных строк) и
// закрыт РОВНО В ОДНОЙ очереди из шести; пять его братьев пережили эту правку на
// сутки и были найдены следующим раундом. Пять сервисных проб при этом молчали
// by construction: каждая утверждала НАЛИЧИЕ двух нужных индексов и никогда —
// отсутствие третьего.
//
// Разбор идёт по тексту миграций, а не по живой базе, намеренно: гейт обязан
// работать без Docker и падать на РЕВЬЮ миграции, а не после её применения.
// Сервисные интеграционные пробы (по одной на очередь) проверяют то же
// утверждение на реальной схеме — это две разные линии обороны, и обе нужны.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// partitionDrainedOutboxes — очереди, выборка которых идёт «только голова
// партиции» (drainer.Config.PartitionColumn задан). Для них набор частичных
// индексов по неотправленным строкам обязан быть РОВНО двумя: по ключу партиции
// и по порядку выборки.
//
// Ключ — имя таблицы без схемы (в миграциях одна и та же таблица встречается и с
// квалификатором, и без него).
var partitionDrainedOutboxes = map[string]string{
	"fga_register_outbox":         "resource_id", // vpc / nlb / storage (одноимённые таблицы в разных схемах)
	"compute_fga_register_outbox": "resource_id",
	"registry_outbox":             "resource_id",
	"fga_outbox":                  "tuple_key", // iam
}

// nonPartitionDrainedOutboxes — очереди, выборка которых НЕ использует ключ
// партиции. Их правильный набор индексов определяется их собственным запросом,
// а не правилом выше, поэтому здесь они только УЧИТЫВАЮТСЯ — чтобы новая очередь
// не оказалась вне обоих ярусов молча.
//
// Запись обязана называть, чем эта очередь выбирается, а не «так исторически».
var nonPartitionDrainedOutboxes = map[string]string{
	"resource_reconcile_outbox": "" +
		"собственная выборка, не общий дренаж: `WHERE sent_at IS NULL ORDER BY id ASC LIMIT n " +
		"FOR UPDATE SKIP LOCKED` (reconcile_outbox/outbox.go). Единственный частичный индекс (id) " +
		"— ровно тот порядок, который эта выборка и просит, то есть законная форма той же записи, " +
		"а не приманка.",
	"provider_compensation_outbox": "" +
		"общий дренаж БЕЗ ключа партиции (provider_compensation_wiring.go): выборка `ORDER BY " +
		"attempt_count, id LIMIT n`, анти-соединения по партиции нет. Ключ партиции не задан " +
		"НАМЕРЕННО: событие ровно одного вида — «снять клиента у провайдера», два таких события " +
		"одного client_id коммутируют и идемпотентны (провайдер отвечает 404 на повторное снятие, " +
		"клиент трактует 404 как исполнено), поэтому финальное состояние сходится при любом " +
		"порядке — условие (а) из drainer.Config.PartitionColumn. Единственный частичный индекс " +
		"(attempt_count, id) — ровно тот порядок, который эта выборка и просит.",
	"subject_change_outbox": "" +
		"общий дренаж БЕЗ ключа партиции (subject_change_wiring.go): выборка `ORDER BY " +
		"attempt_count, id LIMIT n`, анти-соединения по партиции нет, поэтому механизм " +
		"квадратичного роста к ней не относится и измерение, на котором построено правило выше, " +
		"на неё не переносится. Её набор — предмет отдельного разбора со своим измерением; " +
		"учтена здесь, чтобы не выпасть из поля зрения.",
}

// TestOutboxPendingIndexSetIsExactlyTwo — по каждой очереди с ключом партиции
// набор частичных индексов по `sent_at IS NULL` равен ровно двум канонoческим.
//
// Что делать, если гейт сработал, — три исхода, четвёртого нет:
//
//  1. индекс лишний -> дропнуть новой миграцией (существующую не править);
//  2. индекс нужен по существу -> сначала ИЗМЕРИТЬ план выборки на бэклоге и
//     приложить числа, потом менять правило, а не список;
//  3. это новая очередь -> отнести её к одному из двух ярусов выше С
//     ОБОСНОВАНИЕМ, чем она выбирается.
//
// Проверено инъекцией в обе стороны: возврат приманки в любую из миграций красит
// гейт и печатает координату таблицы и определение индекса; законный НЕ-частичный
// индекс той же формы (без предиката по sent_at) он пропускает молча, как и
// одиночный (id) у очереди с собственной выборкой по id.
func TestOutboxPendingIndexSetIsExactlyTwo(t *testing.T) {
	root := repoRoot(t)
	inv, files := pendingIndexInventory(t, root)

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if files == 0 {
		t.Fatalf("гейт не прочитал ни одной миграции — предпосылка обхода сломана, "+
			"молчание ничего не доказывает (корень %s)", root)
	}
	if len(inv) == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОГО частичного индекса по неотправленным строкам в %d "+
			"миграциях — распознавание сломано", files)
	}
	queues := make([]string, 0, len(inv))
	for key := range inv {
		queues = append(queues, key)
	}
	sort.Strings(queues)
	t.Logf("прочитано миграций: %d; ФИЗИЧЕСКИХ очередей с частичными индексами по неотправленным "+
		"строкам: %d (ключ — сервис:таблица; одно имя таблицы бывает у нескольких сервисов)\n  %s",
		files, len(queues), strings.Join(queues, "\n  "))

	covered := map[string]bool{}
	for _, key := range queues {
		tbl := queueTable(key)
		partCol, partitioned := partitionDrainedOutboxes[tbl]
		if !partitioned {
			if _, known := nonPartitionDrainedOutboxes[tbl]; !known {
				t.Errorf("очередь %s не отнесена ни к одному ярусу: неизвестно, чем она "+
					"выбирается, а значит нечем проверить её набор индексов. Добавь её в "+
					"partitionDrainedOutboxes (если выборка идёт по ключу партиции) либо в "+
					"nonPartitionDrainedOutboxes с указанием её собственного запроса.", key)
			}
			continue
		}
		covered[tbl] = true

		want := []string{partCol + ", id", "attempt_count, id"}
		sort.Strings(want)
		got := inv[key]
		var extra []string
		seen := map[string]bool{}
		for name, cols := range got {
			if cols == want[0] || cols == want[1] {
				seen[cols] = true
				continue
			}
			extra = append(extra, name+" ("+cols+")")
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			t.Errorf("очередь %s несёт лишний частичный индекс по неотправленным строкам: %s\n"+
				"Порядок, отличный от (%s, id) и (attempt_count, id), планировщик берёт при "+
				"статистике пустой очереди — выборка теряет раннюю остановку по LIMIT и читает "+
				"всю очередь, которую разгребает. Дропни новой миграцией либо приложи измерение "+
				"плана на бэклоге.", key, strings.Join(extra, ", "), partCol)
		}
		for _, cols := range want {
			if !seen[cols] {
				t.Errorf("очередь %s НЕ несёт частичного индекса (%s) по неотправленным строкам — "+
					"выборка останется без нужного пути доступа. Индекс называется в миграциях ЭТОГО "+
					"сервиса: одноимённый индекс соседа на его собственной схеме здесь не считается.",
					key, cols)
			}
		}
	}

	// Область обхода утверждается числом: если роспись очередей с ключом
	// партиции вдруг перестала находиться в дереве, гейт молчал бы «чисто».
	// Считаются РАЗНЫЕ имена росписи, а не физические очереди: одно имя росписи
	// покрывает столько физических таблиц, сколько сервисов её завели.
	if len(covered) < len(partitionDrainedOutboxes) {
		t.Errorf("в дереве нашлось %d имён очередей с ключом партиции из %d объявленных — запись "+
			"пережила свой предмет либо распознавание сломано", len(covered), len(partitionDrainedOutboxes))
	}
}

// TestNonPartitionOutboxExemptionsHaveSubject — запись второго яруса живёт, пока
// у неё есть предмет: очередь, исчезнувшая из дерева, унаследует следующую
// слепую зону.
func TestNonPartitionOutboxExemptionsHaveSubject(t *testing.T) {
	root := repoRoot(t)
	inv, _ := pendingIndexInventory(t, root)

	present := map[string]bool{}
	for key := range inv {
		present[queueTable(key)] = true
	}
	for tbl, why := range nonPartitionDrainedOutboxes {
		if strings.TrimSpace(why) == "" {
			t.Errorf("запись %s без обоснования: обязана называть, чем эта очередь выбирается", tbl)
		}
		if !present[tbl] {
			t.Errorf("запись %s больше не имеет предмета: такой очереди с частичными индексами "+
				"по неотправленным строкам в дереве нет. Удали запись.", tbl)
		}
	}
}

var (
	createPartialIdxRe = regexp.MustCompile(
		`(?is)CREATE\s+INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+NOT\s+EXISTS)?\s+([\w."]+)\s+ON\s+([\w."]+)\s*(?:USING\s+\w+\s*)?\(([^)]*)\)\s*(?:WHERE\s*\(?([^;]*?)\)?)?\s*;`)
	dropIdxRe = regexp.MustCompile(`(?i)DROP\s+INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+EXISTS)?\s+([\w."]+)\s*;`)
)

// pendingIndexInventory проигрывает Up-секции всех миграций в порядке версий и
// возвращает итоговый набор ЧАСТИЧНЫХ индексов по `sent_at IS NULL`:
// **`<сервис>:<таблица без схемы>`** → имя индекса → список колонок.
//
// # Почему ключ несёт СЕРВИС, а не только имя таблицы
//
// `fga_register_outbox` — это ТРИ физические очереди: в схемах vpc, nlb и
// storage. Серии миграций у них независимые, имена индексов совпадают дословно.
// Ключ без сервиса складывал их в одну корзину, и вердикт становился функцией
// того, В КАКОМ СЕРВИСЕ лежит дефект: снятие головного индекса партиции в
// сервисе, который обход проходит раньше по алфавиту, затиралось созданием
// одноимённого индекса в сервисе, который обход проходит позже. Измерено
// инъекцией на настоящем дереве в обе стороны — тот же дефект у storage не виден,
// у vpc виден. Симметрично: снятие у более позднего сервиса вычёркивало запись
// более раннего, и находка приписывалась не тому. Проба —
// outboxpendingindexperservice_test.go.
//
// Дропы применяются В ПРЕДЕЛАХ СЕРВИСА: `DROP INDEX` называет индекс, а не
// таблицу, поэтому внутри своей схемы он снимает запись с любой таблицы, но
// схемы соседа не касается.
//
// Down-секции намеренно игнорируются: они описывают откат, а не состояние схемы.
func pendingIndexInventory(t *testing.T, root string) (map[string]map[string]string, int) {
	t.Helper()

	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("чтение %s: %v", servicesDir, err)
	}

	inv := map[string]map[string]string{}
	files := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		dir := filepath.Join(servicesDir, svc, "internal", "migrations")
		sqls, globErr := filepath.Glob(filepath.Join(dir, "*.sql"))
		if globErr != nil {
			t.Fatalf("обход %s: %v", dir, globErr)
		}
		sort.Strings(sqls) // имена миграций начинаются с версии — лексикографический порядок = порядок применения
		svcKeys := map[string]bool{}
		for _, path := range sqls {
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("чтение %s: %v", path, readErr)
			}
			files++
			up := string(body)
			if i := strings.Index(up, "-- +goose Down"); i >= 0 {
				up = up[:i]
			}
			for _, m := range createPartialIdxRe.FindAllStringSubmatch(up, -1) {
				where := strings.Join(strings.Fields(m[4]), " ")
				if !strings.Contains(strings.ToLower(where), "sent_at") {
					continue
				}
				key := svc + ":" + unqualify(m[2])
				idx := unqualify(m[1])
				cols := strings.Join(strings.Fields(m[3]), " ")
				if inv[key] == nil {
					inv[key] = map[string]string{}
				}
				inv[key][idx] = cols
				svcKeys[key] = true
			}
			for _, m := range dropIdxRe.FindAllStringSubmatch(up, -1) {
				idx := unqualify(m[1])
				for key := range svcKeys {
					delete(inv[key], idx)
				}
			}
		}
	}
	// Очереди, у которых после всех дропов не осталось ни одного такого индекса,
	// из инвентаря убираем — иначе они выглядели бы как очередь без индексов.
	for key, set := range inv {
		if len(set) == 0 {
			delete(inv, key)
		}
	}
	return inv, files
}

// unqualify отбрасывает схему и кавычки: одна и та же таблица встречается в
// миграциях и с квалификатором, и без него.
func unqualify(ident string) string {
	ident = strings.ReplaceAll(ident, `"`, "")
	if i := strings.LastIndex(ident, "."); i >= 0 {
		ident = ident[i+1:]
	}
	return ident
}
