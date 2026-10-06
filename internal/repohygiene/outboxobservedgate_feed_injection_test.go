// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// outboxobservedgate_feed_injection_test.go — NTF1-B20: четвёртый вход гейта
// наблюдаемости очередей доказан инъекцией в настоящий корень notify-probe.
//
// Обе стороны гоняют ТЕ ЖЕ функции, что и гейт по дереву
// ([scanOutboxWiringSrc] и [unobservedFeeds]); подменяется только содержимое
// настоящего файла проводки ленты, ровно одним фактом против близнеца.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probeFeedWiring — файл проводки ленты корня notify-probe.
const probeFeedWiring = "services/notify/cmd/notify-probe/feed_wiring.go"

func TestNTF1B20FeedInjection(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := filepath.Join(root, probeFeedWiring)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	consts := serviceConstStrings(t, filepath.Join(root, "services", "notify"))
	scan := func(body string) outboxInventory {
		inv := newOutboxInventory()
		scanOutboxWiringSrc(t, path, []byte(body), "notify", root, consts, &inv)
		return inv
	}
	const sweeperLit = "feed.SweeperConfig{"
	const serverLit = "feed.ServerConfig{"
	for _, lit := range []string{sweeperLit, serverLit} {
		if n := strings.Count(string(src), lit); n != 1 {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %s в %s встречается %d раз, ожидался 1 — инъекция "+
				"не меняет ровно один факт", lit, probeFeedWiring, n)
		}
	}

	// Близнец: настоящий корень — лента движется и наблюдается, гейт молчит,
	// перепись называет таблицу в обоих множествах.
	twin := scan(string(src))
	if len(twin.feedMoved) != 1 || len(twin.feedObserved) != 1 {
		t.Fatalf("близнец: движимых %v, наблюдаемых %v — ожидалась одна лента в обоих",
			sortedKeys(twin.feedMoved), sortedKeys(twin.feedObserved))
	}
	table := sortedKeys(twin.feedMoved)[0]
	if sortedKeys(twin.feedObserved)[0] != table {
		t.Fatalf("близнец: движется %s, наблюдается %v", table, sortedKeys(twin.feedObserved))
	}
	if f := unobservedFeeds(twin); len(f) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", f)
	}
	t.Logf("близнец: лента %s движется (%v) и наблюдается (%v), находок 0",
		table, twin.feedMoved[table], twin.feedObserved[table])

	// Порча: сборщика состояния нет (литерал уборщика ленты снят) — находка
	// называет таблицу и координату сервера ленты.
	noCollector := strings.Replace(string(src), sweeperLit, "feedSweeperConfigInjected{", 1)
	got := strings.Join(unobservedFeeds(scan(noCollector)), "\n")
	if !strings.Contains(got, table) || !strings.Contains(got, "notify:"+probeFeedWiring) ||
		!strings.Contains(got, "сборщика состояния нет") {
		t.Errorf("корень с лентой и без сборщика состояния не назван находкой с таблицей и координатой: %q", got)
	}
	t.Logf("порча «без сборщика» → %s", got)

	// Близнец: сервера ленты нет (доставка выключена) — двигать нечего, и
	// уборщик без сервера находкой не является.
	noServer := scan(strings.Replace(string(src), serverLit, "feedServerConfigInjected{", 1))
	if f := unobservedFeeds(noServer); len(f) != 0 || len(noServer.feedMoved) != 0 {
		t.Errorf("корень без сервера ленты: движимых %v, находки %v — ожидалось ни того ни другого",
			sortedKeys(noServer.feedMoved), f)
	}

	// Порча: префикс таблиц не резолвится разбором — гейт обязан это назвать.
	unresolved := scan(strings.Replace(string(src), "Service: journal.Service,\n\t\tEnabled: cfg.Notifications,\n\t\tDB:",
		"Service: cfg.Service(),\n\t\tEnabled: cfg.Notifications,\n\t\tDB:", 1))
	if len(unresolved.feedUnresolved) != 1 {
		t.Errorf("нерезолвящийся префикс сервера ленты не назван: %v", unresolved.feedUnresolved)
	}
}
