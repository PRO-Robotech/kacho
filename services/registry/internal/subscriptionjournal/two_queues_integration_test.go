// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	"github.com/PRO-Robotech/kacho/services/registry/internal/subscriptionjournal"
)

// rightsQueueTable — очередь НАМЕРЕНИЙ О ПРАВАХ. Здесь она названа литералом
// намеренно: предмет пробы — что эти две таблицы РАЗНЫЕ, и вывод обоих имён из
// одного места сделал бы утверждение тождественно истинным.
const rightsQueueTable = "kacho_registry.registry_outbox"

// countRows — сколько строк с таким родом события лежит в названной таблице.
func countRows(t *testing.T, s *stand, table, resourceID, eventType string) int {
	t.Helper()
	var n int
	err := s.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM "+table+" WHERE resource_id = $1 AND event_type = $2",
		resourceID, eventType).Scan(&n)
	if err != nil {
		t.Fatalf("счёт строк %s: %v", table, err)
	}
	return n
}

// TestTheRightsQueueAndTheResourceJournalAreTwoTables — очередь прав и ресурсный
// журнал РАЗДЕЛЕНЫ, и разделены не соглашением.
//
// # Почему это отдельное условие приёмки, а не подробность
//
// Очередь `registry_outbox` похожа на журнал ровно настолько, чтобы её захотелось
// переиспользовать. Смешение стоило бы дорого и тихо: её дренаж читает СВОЮ
// таблицу ПО ГОЛОВЕ ПАРТИЦИИ, поэтому чужая строка, легшая между намерениями,
// блокировала бы выдачу прав до конца окна повторов — то есть арендатор получал
// бы отказы на своих же ресурсах, а причина лежала бы в подписке.
//
// # Что именно утверждается — ТРИ стороны, и первые две недостаточны порознь
//
//	перепись     одна мутация кладёт по ОДНОЙ строке в КАЖДУЮ таблицу, и ни одна
//	             из них не видна другой по роду события;
//	ограничение  слово ресурсного журнала в очередь прав НЕ вставляется;
//	ограничение  слово очереди прав в ресурсный журнал НЕ вставляется.
//
// Перепись говорит о том, что происходит СЕГОДНЯ; ограничения — о том, что
// возможно ВООБЩЕ. Только вместе они дают «по построению», а не «пока никто не
// написал».
func TestTheRightsQueueAndTheResourceJournalAreTwoTables(t *testing.T) {
	s := newStand(t)

	if subscriptionjournal.Table == rightsQueueTable {
		t.Fatalf("ресурсный журнал объявлен ТОЙ ЖЕ таблицей, что очередь прав (%s): "+
			"дренаж прав читает её по голове партиции, и чужая строка заклинила бы выдачу прав",
			rightsQueueTable)
	}

	reg := s.create(t, probeProject, "two-queues", nil)

	// Перепись: одна мутация — по одной строке в каждую таблицу, каждая своего рода.
	if got := countRows(t, s, rightsQueueTable, reg.ID, domain.FGAEventRegister); got != 1 {
		t.Errorf("в очереди прав строк рода %q: %d, ожидалась 1", domain.FGAEventRegister, got)
	}
	if got := countRows(t, s, subscriptionjournal.Table, reg.ID, "CREATED"); got != 1 {
		t.Errorf("в ресурсном журнале строк рода CREATED: %d, ожидалась 1", got)
	}
	// И ни одна не видна другой: род события одной в другой не встречается.
	if got := countRows(t, s, rightsQueueTable, reg.ID, "CREATED"); got != 0 {
		t.Errorf("в очередь прав легло %d строк ресурсного рода CREATED — дренаж прав "+
			"попытался бы применить их как намерение", got)
	}
	if got := countRows(t, s, subscriptionjournal.Table, reg.ID, domain.FGAEventRegister); got != 0 {
		t.Errorf("в ресурсный журнал легло %d строк рода %q — подписчик получил бы "+
			"набор кортежей прав вместо состояния предмета", got, domain.FGAEventRegister)
	}

	// Ограничения обеих таблиц: слово одной в другую не проходит ВООБЩЕ.
	assertInsertRefused(t, s, subscriptionjournal.Table, domain.FGAEventRegister,
		"слово очереди прав легло в ресурсный журнал")
	assertInsertRefused(t, s, rightsQueueTable, "CREATED",
		"слово ресурсного журнала легло в очередь прав")
}

// assertInsertRefused — вставка строки с чужим родом события отвергается
// ОГРАНИЧЕНИЕМ базы, а не отсутствием желающих её написать.
func assertInsertRefused(t *testing.T, s *stand, table, eventType, why string) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(),
		"INSERT INTO "+table+" (resource_kind, resource_id, event_type, payload) "+
			"VALUES ($1, $2, $3, '{}'::jsonb)",
		"Registry", "reg-two-queues-probe", eventType)
	if err == nil {
		t.Fatalf("%s: вставка прошла. Разделённость держалась бы соглашением, а не построением", why)
	}
	// Утверждается ПРИЧИНА отказа, а не сам факт: отказ по любой другой причине
	// (нет колонки, нет таблицы) зеленил бы пробу, ничего не доказав об
	// ограничении.
	if !strings.Contains(err.Error(), "event_type_check") {
		t.Fatalf("%s: вставка отвергнута, но НЕ ограничением словаря родов: %v", why, err)
	}
}
