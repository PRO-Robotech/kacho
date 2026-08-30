// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outboxpendingindexperservice_test.go — самопроверка гейта набора частичных
// индексов: его инвентарь обязан различать ФИЗИЧЕСКИЕ очереди, а не их имена.
//
// # Предмет
//
// `fga_register_outbox` — это ТРИ таблицы: в схемах vpc, nlb и storage. У каждой
// своя серия миграций, свой номер версии и свой автор (в дереве они и заведены
// тремя разными правками, под номерами 0018/0020/0022, 0024/0026/0029 и
// 0008/0009/0012 соответственно). Имена индексов у всех трёх совпадают дословно.
//
// Инвентарь гейта складывал их в ОДНУ корзину по имени без схемы. Следствие —
// вердикт становился функцией того, В КАКОМ СЕРВИСЕ лежит дефект, а не самого
// дефекта: снятие головного индекса партиции в сервисе, который обход проходит
// РАНЬШЕ, затиралось созданием одноимённого индекса в сервисе, который обход
// проходит ПОЗЖЕ. Обход идёт по `os.ReadDir`, то есть по алфавиту, поэтому
// «раньше» и «позже» — свойство имени каталога сервиса, а не решения инженера.
//
// Измерено инъекцией на настоящем дереве (обе стороны, один и тот же дефект):
// снятие `(resource_id, id) WHERE sent_at IS NULL` у storage — гейт ЗЕЛЁНЫЙ; то
// же снятие у vpc — гейт красный. Индекс, о котором речь, — тот самый, под
// который построено коррелированное анти-соединение поголовной выборки; без него
// выборка перестаёт останавливаться рано и читает всю очередь, которую
// разгребает, а дальше включается положительная обратная связь «глубже очередь →
// медленнее клейм». То есть невидимой оказывалась не косметика, а то, из-за чего
// отзыв прав перестаёт доезжать под нагрузкой.
//
// Второе следствие того же слияния — симметричное: снятие индекса в сервисе,
// который обход проходит позже, удаляло запись СОСЕДА, и находка приписывалась
// не тому сервису.
//
// # Второй предмет того же класса: проигрывание расходится с тем, что сделал бы Postgres
//
// Инвентарь не «читает миграции», он ПРОИГРЫВАЕТ их — и обязан прийти к тому же
// состоянию схемы, к какому пришёл бы Postgres. Разойтись он может тремя
// способами, и все три здесь закрыты пробами:
//
//   - ПОРЯДОК ВНУТРИ ФАЙЛА. Проигрывание шло двумя раздельными проходами (сперва
//     все создания файла, потом все удаления), поэтому текстовый порядок
//     игнорировался и законная перестройка индекса «удалить и создать заново в
//     одной миграции» читалась как «удалён». Форма законная: в дереве она
//     встречается 4 раза (iam 0055/0056, nlb 0012/0021), и то, что живого ложного
//     срабатывания сегодня нет, — свойство этих четырёх (их индексы не частичные
//     по `sent_at`), а не свойство разбора. Первая же перестройка частичного
//     индекса очереди покрасила бы гейт на ПРАВИЛЬНОЙ схеме, а ложно краснеющий
//     гейт снимают.
//
//   - `CREATE UNIQUE INDEX` не читался вовсе. Для планировщика частичный
//     УНИКАЛЬНЫЙ индекс по неотправленным строкам — такая же приманка, как
//     обычный: он тоже даёт более узкий порядок по тем же строкам. Сегодня таких
//     в дереве ноль, поэтому слепота была тихой.
//
//   - `DROP INDEX` внутри `DO`-блока применяется БЕЗУСЛОВНО, хотя в самом блоке
//     он под условием. Это объявленная слепая зона, а не починенная: в дереве
//     таких три (nlb 0012 ×2, 0021 ×1), все под шаблоном «снять, если индекс
//     невалиден, и тут же создать заново», поэтому после починки порядка
//     проигрывание сходится с Postgres и на них. Опасен остаточный случай —
//     условное удаление БЕЗ последующего создания: там проигрывание объявит
//     индекс снятым, а он стоит. Обратная эвристика («не применять удаления из
//     `DO`-блоков») завела бы свою слепую зону — безусловное удаление внутри
//     такого блока, — поэтому выбрано назвать зону, а не подменить её другой.
//
// # Что здесь проверяется
//
// Инвентарь строится над СИНТЕТИЧЕСКИМ деревом миграций (два сервиса, одна и та
// же таблица, одни и те же имена индексов), поэтому проба детерминирована, не
// требует ни Docker, ни настоящего дерева, и не поплывёт от следующей миграции.
// Настоящее дерево остаётся предметом самого гейта (outboxpendingindexset_test.go).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// queueTable — имя таблицы из ключа инвентаря. Ключ несёт сервис
// (`<сервис>:<таблица>`), потому что одно имя таблицы — это несколько ФИЗИЧЕСКИХ
// очередей; на ключе без сервиса они сливались. Ключ без разделителя (форма, на
// которой гейт стоял до этой правки) отдаётся как есть, чтобы проба говорила о
// СВОЙСТВЕ, а не о формате, и краснела на слиянии, а не на отсутствии функции.
func queueTable(key string) string {
	if i := strings.Index(key, ":"); i >= 0 {
		return key[i+1:]
	}
	return key
}

// writeSyntheticMigration кладёт одну миграцию в синтетическое дерево
// `<root>/services/<svc>/internal/migrations/<name>`.
func writeSyntheticMigration(t *testing.T, root, svc, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "services", svc, "internal", "migrations")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("создаю %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("пишу %s: %v", name, err)
	}
}

// syntheticPendingIndexTree собирает дерево из двух сервисов с ОДНОИМЁННОЙ
// очередью и ОДНОИМЁННЫМИ индексами. Имена каталогов подобраны так, чтобы
// `alpha` обход проходил РАНЬШЕ `zeta` — именно этот порядок и прятал дефект.
// dropInAlpha=true снимает у alpha головной индекс партиции.
func syntheticPendingIndexTree(t *testing.T, dropInAlpha bool) string {
	t.Helper()
	root := t.TempDir()
	for _, svc := range []string{"alpha", "zeta"} {
		writeSyntheticMigration(t, root, svc, "0001_create.sql", `
-- +goose Up
CREATE INDEX fga_register_outbox_partition_head_idx
    ON kacho_`+svc+`.fga_register_outbox (resource_id, id) WHERE sent_at IS NULL;
CREATE INDEX fga_register_outbox_claim_order_idx
    ON kacho_`+svc+`.fga_register_outbox (attempt_count, id) WHERE sent_at IS NULL;
-- +goose Down
DROP INDEX IF EXISTS fga_register_outbox_partition_head_idx;
`)
	}
	if dropInAlpha {
		writeSyntheticMigration(t, root, "alpha", "0002_drop_head.sql", `
-- +goose Up
DROP INDEX IF EXISTS kacho_alpha.fga_register_outbox_partition_head_idx;
-- +goose Down
SELECT 1;
`)
	}
	return root
}

// pendingIndexKeysFor возвращает записи инвентаря, относящиеся к данной таблице,
// — независимо от того, чем инвентарь ключуется. Нужен, чтобы проба говорила о
// СВОЙСТВЕ («сколько физических очередей видно»), а не о формате ключа.
func pendingIndexKeysFor(inv map[string]map[string]string, table string) []string {
	var out []string
	for key := range inv {
		if queueTable(key) == table {
			out = append(out, key)
		}
	}
	return out
}

// Test_PendingIndexInventory_KeepsSameNamedQueuesOfServicesApart — две
// одноимённые очереди РАЗНЫХ сервисов видны инвентарю как две записи.
//
// Положительный контроль (обе несут полный набор) стоит рядом с отрицанием
// намеренно: без него «две записи» зеленело бы и на инвентаре, который просто
// перестал что-либо находить.
func Test_PendingIndexInventory_KeepsSameNamedQueuesOfServicesApart(t *testing.T) {
	root := syntheticPendingIndexTree(t, false)
	inv, files := pendingIndexInventory(t, root, syntheticMigrationSQL)

	if files != 2 {
		t.Fatalf("проба прочитала %d миграций вместо двух — синтетическое дерево собрано не так, "+
			"молчание ничего не доказывает", files)
	}
	keys := pendingIndexKeysFor(inv, "fga_register_outbox")
	if len(keys) != 2 {
		t.Fatalf("инвентарь показывает %d записей об очереди fga_register_outbox (%v), а физических "+
			"таблиц с этим именем в дереве ДВЕ (kacho_alpha, kacho_zeta). Слив одноимённых очередей "+
			"в одну корзину делает набор индексов ОДНОГО сервиса неотличимым от набора ДРУГОГО: "+
			"создание индекса в сервисе, который обход проходит позже, восстанавливает запись, "+
			"снятую сервисом, который обход проходит раньше.", len(keys), keys)
	}
	for _, key := range keys {
		got := inv[key]
		if len(got) != 2 {
			t.Errorf("%s: ожидались оба канонических индекса, инвентарь показывает %v", key, got)
		}
	}
}

// Test_PendingIndexInventory_SeesDropInEarlierWalkedService — тот же дефект, что
// был измерен инъекцией на настоящем дереве: головной индекс партиции снят в
// сервисе, который обход проходит РАНЬШЕ. Инвентарь обязан показать его
// отсутствие ИМЕННО у этого сервиса и не тронуть соседа.
func Test_PendingIndexInventory_SeesDropInEarlierWalkedService(t *testing.T) {
	root := syntheticPendingIndexTree(t, true)
	inv, files := pendingIndexInventory(t, root, syntheticMigrationSQL)

	if files != 3 {
		t.Fatalf("проба прочитала %d миграций вместо трёх — синтетическое дерево собрано не так", files)
	}

	var alphaKey, zetaKey string
	for _, key := range pendingIndexKeysFor(inv, "fga_register_outbox") {
		switch {
		case key == "alpha:fga_register_outbox":
			alphaKey = key
		case key == "zeta:fga_register_outbox":
			zetaKey = key
		}
	}
	if alphaKey == "" || zetaKey == "" {
		t.Fatalf("инвентарь не показал обе физические очереди порознь: alpha=%q zeta=%q (все записи: %v). "+
			"Ключ инвентаря обязан нести сервис — иначе снятие индекса в одном сервисе неотличимо "+
			"от его наличия в другом.", alphaKey, zetaKey, pendingIndexKeysFor(inv, "fga_register_outbox"))
	}

	if _, has := inv[alphaKey]["fga_register_outbox_partition_head_idx"]; has {
		t.Errorf("%s: головной индекс партиции снят миграцией этого сервиса, но инвентарь его ВИДИТ — "+
			"запись восстановлена одноимённым индексом соседнего сервиса. Ровно на этом гейт и "+
			"молчал: выборка сервиса осталась без пути доступа под своё анти-соединение, а проверка "+
			"набора индексов оставалась зелёной.", alphaKey)
	}
	if _, has := inv[zetaKey]["fga_register_outbox_partition_head_idx"]; !has {
		t.Errorf("%s: сосед свой головной индекс НЕ снимал, но инвентарь его потерял — снятие в одном "+
			"сервисе вычёркивает запись другого, и находка приписывается не тому сервису", zetaKey)
	}
}

// syntheticStatementOrderTree — одно и то же ПАРНОЕ содержимое в двух текстовых
// порядках, по одной миграции на порядок:
//
//   - 0002 — ПЕРЕСТРОЙКА: сперва `DROP`, следом `CREATE` того же имени с другими
//     колонками. Postgres придёт к «индекс есть, колонки новые»;
//   - 0003 — СНЯТИЕ: сперва `CREATE`, следом `DROP` того же имени. Postgres
//     придёт к «индекса нет».
//
// Пара нужна целиком: по одной половине не отличить «порядок учтён» от «удаления
// всегда побеждают» (тогда зеленела бы вторая половина и краснела первая) и от
// «создания всегда побеждают» (наоборот). Проверяемое свойство — именно то, что
// исход РАЗНЫЙ у одинакового набора операторов в разном порядке.
func syntheticStatementOrderTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSyntheticMigration(t, root, "alpha", "0001_create.sql", `
-- +goose Up
CREATE INDEX q_outbox_partition_head_idx
    ON kacho_alpha.q_outbox (resource_id, id) WHERE sent_at IS NULL;
CREATE INDEX q_outbox_claim_order_idx
    ON kacho_alpha.q_outbox (attempt_count, id) WHERE sent_at IS NULL;
-- +goose Down
SELECT 1;
`)
	writeSyntheticMigration(t, root, "alpha", "0002_rebuild_head.sql", `
-- +goose Up
DROP INDEX IF EXISTS kacho_alpha.q_outbox_partition_head_idx;
CREATE INDEX q_outbox_partition_head_idx
    ON kacho_alpha.q_outbox (resource_id, id, created_at) WHERE sent_at IS NULL;
-- +goose Down
SELECT 1;
`)
	writeSyntheticMigration(t, root, "alpha", "0003_retire_decoy.sql", `
-- +goose Up
CREATE INDEX q_outbox_decoy_idx
    ON kacho_alpha.q_outbox (created_at) WHERE sent_at IS NULL;
DROP INDEX IF EXISTS kacho_alpha.q_outbox_decoy_idx;
-- +goose Down
SELECT 1;
`)
	return root
}

// Test_PendingIndexInventory_ReplaysStatementsInTextOrder — проигрывание идёт в
// ТЕКСТОВОМ порядке операторов, а не «сперва все создания файла, потом все
// удаления».
//
// Что ловится: законная перестройка индекса в одной миграции («удалить и создать
// заново») читалась как «удалён» — гейт краснел бы на ПРАВИЛЬНОЙ схеме, а ложно
// краснеющий гейт снимают как непонятный. Форма в дереве уже используется
// (iam 0055/0056, nlb 0012/0021).
func Test_PendingIndexInventory_ReplaysStatementsInTextOrder(t *testing.T) {
	root := syntheticStatementOrderTree(t)
	inv, files := pendingIndexInventory(t, root, syntheticMigrationSQL)

	if files != 3 {
		t.Fatalf("проба прочитала %d миграций вместо трёх — синтетическое дерево собрано не так, "+
			"молчание ничего не доказывает", files)
	}
	keys := pendingIndexKeysFor(inv, "q_outbox")
	if len(keys) != 1 {
		t.Fatalf("инвентарь показывает %d записей об очереди q_outbox (%v) вместо одной — "+
			"распознавание сломано, дальнейшие утверждения бессмысленны", len(keys), keys)
	}
	got := inv[keys[0]]

	cols, has := got["q_outbox_partition_head_idx"]
	if !has {
		t.Errorf("перестройка индекса в одной миграции (DROP, следом CREATE того же имени) прочитана "+
			"как СНЯТИЕ: инвентарь показывает %v. Проигрывание идёт двумя раздельными проходами — "+
			"сперва все создания файла, потом все удаления, — поэтому текстовый порядок теряется. "+
			"Postgres пришёл бы к «индекс есть»; гейт покраснеет на правильной схеме, а ложно "+
			"краснеющий гейт снимают.", got)
	} else if cols != "resource_id, id, created_at" {
		t.Errorf("перестроенный индекс несёт колонки %q, а последний CREATE файла объявляет "+
			"(resource_id, id, created_at) — проигрывание взяло не последнее состояние", cols)
	}

	if cols, has := got["q_outbox_decoy_idx"]; has {
		t.Errorf("снятие индекса в одной миграции (CREATE, следом DROP того же имени) прочитано как "+
			"НАЛИЧИЕ: инвентарь показывает %q. Это обратная половина той же пары — если она "+
			"краснеет вместе с первой, порядок не учитывается вовсе; если вместо первой, то "+
			"«создания всегда побеждают».", cols)
	}
}

// Test_PendingIndexInventory_SeesPartialUniqueIndex — частичный УНИКАЛЬНЫЙ индекс
// по неотправленным строкам виден инвентарю.
//
// Для планировщика он такая же приманка, как обычный: даёт более узкий порядок по
// тем же строкам, поэтому выборка теряет раннюю остановку по LIMIT. Инвентарь
// читал только `CREATE INDEX`, то есть целый вид приманки был ему невидим —
// молча, потому что сегодня таких в дереве ноль.
//
// Отрицание идёт в паре с положительным контролем: НЕ-частичный уникальный индекс
// (без предиката по `sent_at`) инвентарь обязан пропустить. Без этой половины
// проба зеленела бы на инвентаре, который начал считать любой индекс подряд.
func Test_PendingIndexInventory_SeesPartialUniqueIndex(t *testing.T) {
	root := t.TempDir()
	writeSyntheticMigration(t, root, "alpha", "0001_create.sql", `
-- +goose Up
CREATE UNIQUE INDEX q_outbox_pending_uniq
    ON kacho_alpha.q_outbox (resource_id) WHERE sent_at IS NULL;
CREATE UNIQUE INDEX q_outbox_all_rows_uniq
    ON kacho_alpha.q_outbox (resource_id, id);
-- +goose Down
SELECT 1;
`)
	inv, files := pendingIndexInventory(t, root, syntheticMigrationSQL)
	if files != 1 {
		t.Fatalf("проба прочитала %d миграций вместо одной — синтетическое дерево собрано не так", files)
	}
	keys := pendingIndexKeysFor(inv, "q_outbox")
	if len(keys) != 1 {
		t.Fatalf("частичный УНИКАЛЬНЫЙ индекс по неотправленным строкам инвентарю невиден: записей "+
			"об очереди q_outbox %d (%v). Для планировщика он такая же приманка, как обычный "+
			"частичный индекс, — более узкий порядок по тем же строкам, из-за которого выборка "+
			"теряет раннюю остановку по LIMIT. Целый вид приманки проходил мимо гейта молча.",
			len(keys), keys)
	}
	got := inv[keys[0]]
	if _, has := got["q_outbox_pending_uniq"]; !has {
		t.Errorf("частичный уникальный индекс по неотправленным строкам не попал в инвентарь: %v", got)
	}
	if _, has := got["q_outbox_all_rows_uniq"]; has {
		t.Errorf("НЕ-частичный уникальный индекс попал в инвентарь: %v. Предмет гейта — индексы по "+
			"неотправленным строкам; индекс по всей таблице планировщику этой выборки не приманка, "+
			"и считать его лишним значило бы краснеть на законной схеме", got)
	}
}
