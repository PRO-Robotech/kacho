// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basic_credential_lane_bounded_test.go — ПОТОЛОК КЭША ВЕРДИКТОВ БАЗОВОЙ ПОЛОСЫ.
//
// Задача #1218. Кэш полосы хранился в голой карте: вытеснения не было НИ ПО
// РАЗМЕРУ, НИ ПО ИСТЕЧЕНИЮ. Замер на ревизии e4da590cf: тысяча удостоверений,
// часы сдвинуты на сто окон вперёд, ещё тысяча ДРУГИХ — в карте две тысячи
// записей. То есть рост ограничивался не окном, как предполагала задача, а
// только временем жизни процесса.
//
// Пробы ниже утверждают ПЯТЬ разных свойств, и ни одно не выводится из другого:
//
//  1. потолок есть — поток различных удостоверений его не переполняет;
//  2. мёртвые записи не держат кэш занятым;
//  3. вытеснение НЕ ОТКРЫВАЕТ ПОЛОСУ — промах ведёт к проверке, а не к пропуску;
//  4. заполнение видно снаружи И доходит до оператора (два разных читателя);
//  5. вытеснение безопасно при гонке.
//
// Способность каждой упасть доказана инъекцией в примитив вытеснения, а не
// прочтением: снятие освобождения по истечению краснит (2), снятие вытеснения
// по потолку — (1), (2), (3), (5), снятие защёлки и понижение уровня записи —
// обе половины (4). Первая редакция пробы (2) была ВАКУУМНОЙ и это показала
// именно инъекция: повторная запись под тем же ключом кладёт его на то же
// место, поэтому счёт занятых равнялся единице при любом поведении.

package middleware_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/credsecret"
	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// mintDistinct чеканит годное удостоверение со СВОИМ идентификатором и
// регистрирует его у авторитета.
func mintDistinct(t *testing.T, auth *fakeAuthority) (credID, secret string) {
	t.Helper()
	credID = ids.NewID("bcr")
	s, _, err := credsecret.Mint(credID)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if auth.byCredential == nil {
		auth.byCredential = map[string]string{}
	}
	auth.byCredential[credID] = s
	return credID, s
}

// #1218-1 — у кэша ЕСТЬ потолок: поток различных удостоверений его не
// переполняет.
func TestBasicLaneCacheIsBoundedBySize(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })

	capacity := lane.CacheStats().Capacity
	if capacity <= 0 {
		t.Fatalf("потолок не объявлен (Capacity=%d) — проба беспредметна", capacity)
	}

	// Вдвое больше потолка РАЗЛИЧНЫХ удостоверений, все в пределах окна.
	for i := 0; i < 2*capacity; i++ {
		_, s := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), s); err != nil {
			t.Fatalf("годное удостоверение отвергнуто: %v", err)
		}
	}

	got := lane.CacheStats().Entries
	if got > capacity {
		t.Errorf("записей в кэше %d при потолке %d — вытеснения по размеру нет", got, capacity)
	}
	if keys := len(lane.CacheKeysForTest()); keys > capacity {
		t.Errorf("ключей в карте %d при потолке %d — карта растёт помимо потолка", keys, capacity)
	}
}

// #1218-2 — МЁРТВЫЕ записи не могут удержать кэш занятым.
//
// Утверждается ИМЕННО ЭТО, а не «мёртвая запись исчезает сразу»: общий примитив
// намеренно не держит фонового сборщика (см. godoc lrucache.Len), и заводить его
// ради одной полосы значило бы форкнуть политику вытеснения — ровно то, против
// чего примитив и заведён. Память при этом ограничена потолком независимо от
// живости записей, поэтому неограниченного роста нет by construction (#1218-1).
//
// Освобождение здесь имеет две наблюдаемые стороны, и обе проверяются:
//
//	А. чтение просроченного ключа его ОСВОБОЖДАЕТ;
//	Б. под давлением потолка мёртвые записи вытесняются, поэтому кэш, полный
//	   мёртвых, принимает живые и НЕ перерастает потолок.
func TestBasicLaneExpiredEntriesDoNotHoldTheCache(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })
	capacity := lane.CacheStats().Capacity

	// ── Сторона А ────────────────────────────────────────────────────────────
	// Освобождение чтением наблюдаемо ТОЛЬКО когда за промахом НЕ следует
	// запись: повторное предъявление годного кладёт ключ обратно на то же место,
	// и счёт занятых равен единице при любом поведении — такая проба была бы
	// вакуумной (проверено инъекцией: снятие освобождения в примитиве её не
	// краснит). Поэтому удостоверение ОТЗЫВАЕТСЯ: тогда положительного вердикта
	// нет, записи нет, и просроченный ключ обязан уйти сам.
	victimID, s := mintDistinct(t, auth)
	if _, err := lane.Verify(context.Background(), s); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if n := len(lane.CacheKeysForTest()); n != 1 {
		t.Fatalf("занятых ключей %d, ожидался 1 — проба беспредметна", n)
	}
	clock = clock.Add(middleware.BasicCredentialVerdictWindow * 2)
	if n := lane.CacheStats().Entries; n != 0 {
		t.Errorf("после истечения окна живых записей %d, ожидался 0", n)
	}
	delete(auth.byCredential, victimID)
	if _, err := lane.Verify(context.Background(), s); err == nil {
		t.Fatal("отозванное удостоверение прошло после истечения окна")
	}
	if n := len(lane.CacheKeysForTest()); n != 0 {
		t.Errorf("занятых ключей %d, ожидался 0 — просроченная запись не освобождена чтением", n)
	}

	// ── Сторона Б ────────────────────────────────────────────────────────────
	// Заполняем потолок ЦЕЛИКОМ и умерщвляем всё.
	for i := 0; i < capacity; i++ {
		_, sec := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), sec); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	clock = clock.Add(middleware.BasicCredentialVerdictWindow * 100)
	if n := lane.CacheStats().Entries; n != 0 {
		t.Fatalf("живых записей %d при полностью мёртвом кэше — проба беспредметна", n)
	}

	// Волна ЖИВЫХ: кэш, полный мёртвых, обязан их принять и не перерасти.
	const fresh = 500
	for i := 0; i < fresh; i++ {
		_, sec := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), sec); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	if n := lane.CacheStats().Entries; n != fresh {
		t.Errorf("живых записей %d, ожидалось %d — мёртвые не пускают живых", n, fresh)
	}
	if n := len(lane.CacheKeysForTest()); n > capacity {
		t.Errorf("занятых ключей %d при потолке %d — мёртвые записи растут помимо потолка", n, capacity)
	}
}

// #1218-3 — ГЛАВНОЕ: вытеснение НЕ ОТКРЫВАЕТ ПОЛОСУ. Промах ведёт к ПРОВЕРКЕ,
// а не к пропуску, и отозванное удостоверение после вытеснения отвергается.
//
// Проба несёт ОБЕ стороны: вытесненное годное по-прежнему проходит (иначе
// «отвергает всё» зеленело бы), вытесненное ОТОЗВАННОЕ — отвергается.
func TestBasicLaneEvictionDoesNotOpenTheLane(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })

	capacity := lane.CacheStats().Capacity

	// Жертва — первое удостоверение: после потока оно наименее свежее и потому
	// вытесняется первым.
	victimID, victimSecret := mintDistinct(t, auth)
	if _, err := lane.Verify(context.Background(), victimSecret); err != nil {
		t.Fatalf("жертва не прошла: %v", err)
	}
	callsAfterFirst := auth.callCount()

	// Положительный контроль: пока запись жива, повтор идёт ИЗ КЭША.
	if _, err := lane.Verify(context.Background(), victimSecret); err != nil {
		t.Fatalf("повтор не прошёл: %v", err)
	}
	if auth.callCount() != callsAfterFirst {
		t.Fatal("живая запись не обслужена кэшем — проба беспредметна")
	}

	// Вытесняем: поток различных удостоверений на весь потолок.
	for i := 0; i < capacity; i++ {
		_, s := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), s); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}

	// Сторона А: вытесненное ГОДНОЕ проходит — и оплачено НОВЫМ вызовом к
	// авторитету, то есть промах привёл к ПРОВЕРКЕ.
	before := auth.callCount()
	if _, err := lane.Verify(context.Background(), victimSecret); err != nil {
		t.Fatalf("вытесненное годное удостоверение отвергнуто: %v", err)
	}
	if auth.callCount() == before {
		t.Error("после вытеснения вердикт взят ниоткуда: авторитета не спросили")
	}

	// Сторона Б: удостоверение ОТОЗВАНО у авторитета. Оно вытеснено, значит
	// вердикт обязан быть спрошен заново — и обязан быть отказом.
	delete(auth.byCredential, victimID)
	for i := 0; i < capacity; i++ {
		_, s := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), s); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	if _, err := lane.Verify(context.Background(), victimSecret); err == nil {
		t.Error("ОТОЗВАННОЕ удостоверение прошло: вердикт взят из кэша, а не спрошен заново")
	}
}

// #1218-4 — заполнение НАБЛЮДАЕМО: исчерпание отличимо от исправной работы.
func TestBasicLaneFillIsObservable(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })

	st := lane.CacheStats()
	if st.Capacity <= 0 {
		t.Fatalf("потолок не объявлен: %d", st.Capacity)
	}
	if st.Entries != 0 {
		t.Errorf("на пустом кэше записей %d, ожидался 0", st.Entries)
	}
	if st.AtCapacity {
		t.Error("пустой кэш объявлен исчерпанным")
	}

	for i := 0; i < st.Capacity; i++ {
		_, s := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), s); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}

	full := lane.CacheStats()
	if full.Entries != full.Capacity {
		t.Errorf("после заполнения записей %d при потолке %d", full.Entries, full.Capacity)
	}
	if !full.AtCapacity {
		t.Error("потолок достигнут, а снаружи это не видно — исчерпание неотличимо от исправной работы")
	}
}

// #1218-4б — исчерпание доходит ДО ОПЕРАТОРА, и ровно один раз.
//
// Защёлка сама по себе читается только тем, кто её спросит. Отдельная проба
// потому, что запись в журнал — единственный сегодня НЕ-ТЕСТОВЫЙ читатель
// заполнения: без неё величина считалась бы в никуда.
func TestBasicLaneCapacityIsAnnouncedOnceToTheOperator(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	var buf bytes.Buffer
	lane := newLane(t, auth, func() time.Time { return clock }).
		WithLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	capacity := lane.CacheStats().Capacity

	// Заполняем ПОЧТИ до потолка: пока он не достигнут, оператора не беспокоят.
	for i := 0; i < capacity-1; i++ {
		_, sec := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), sec); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	if n := strings.Count(buf.String(), "reached capacity"); n != 0 {
		t.Errorf("до достижения потолка записей в журнале %d, ожидался 0", n)
	}

	// Пересекаем потолок и идём заметно дальше.
	for i := 0; i < 1000; i++ {
		_, sec := mintDistinct(t, auth)
		if _, err := lane.Verify(context.Background(), sec); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	got := buf.String()
	if n := strings.Count(got, "reached capacity"); n != 1 {
		t.Errorf("записей об исчерпании %d, ожидалась ровно 1 — либо оператор не узнал, либо журнал залит", n)
	}
	// Запись обязана нести ВЕЛИЧИНЫ, иначе она не отличает исчерпание от шума.
	for _, want := range []string{`"capacity":10000`, `"entries":`} {
		if !strings.Contains(got, want) {
			t.Errorf("в записи журнала нет %s: %s", want, got)
		}
	}
}

// #1218-5 — карту читают и пишут ПАРАЛЛЕЛЬНЫЕ запросы: вытеснение обязано быть
// безопасным при гонке. Детерминированно, без пауз по времени; гонять под -race.
func TestBasicLaneCacheUnderConcurrency(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Unix(1700000000, 0)
	lane := newLane(t, auth, func() time.Time { return clock })

	capacity := lane.CacheStats().Capacity

	// Удостоверений заведомо больше потолка → вытеснение идёт ПОД гонкой.
	total := capacity + capacity/2
	type cred struct{ id, secret string }
	creds := make([]cred, 0, total)
	for i := 0; i < total; i++ {
		id, s := mintDistinct(t, auth)
		creds = append(creds, cred{id, s})
	}

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers*4)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			// Два прохода со СМЕЩЕНИЕМ: ключ, тронутый одним работником в первом
			// проходе, во втором достаётся другому — чтение и запись одной записи
			// идут врозь. Список обходится полосами, а не целиком каждым: под
			// -race полный обход каждым работником стоит двадцати секунд и ничего
			// сверх этого не утверждает.
			for pass := 0; pass < 2; pass++ {
				for i := w; i < len(creds); i += workers {
					c := creds[(i+pass*(workers/2))%len(creds)]
					v, err := lane.Verify(context.Background(), c.secret)
					if err != nil {
						errs <- err
						return
					}
					if v.CredentialID != c.id {
						errs <- errWrongCredential
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("параллельный прогон дал отказ: %v", err)
	}

	if got := lane.CacheStats().Entries; got > capacity {
		t.Errorf("после гонки записей %d при потолке %d", got, capacity)
	}
	if keys := len(lane.CacheKeysForTest()); keys > capacity {
		t.Errorf("после гонки ключей %d при потолке %d", keys, capacity)
	}
}

// errWrongCredential — вердикт выдан не о том удостоверении. Отдельная ошибка,
// а не общий отказ: под гонкой перепутанный вердикт и отказ означают разное.
var errWrongCredential = errors.New("вердикт выдан не о том удостоверении")
