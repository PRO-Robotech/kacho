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
	inv, files := pendingIndexInventory(t, root)

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
	inv, files := pendingIndexInventory(t, root)

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
