// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция на файле цепочки пробы: имена объектов там в КАВЫЧКАХ и со схемой
// (`CREATE TABLE "kacho_notifyprobe"."…"`, `CONSTRAINT "…" CHECK`). Разбор,
// знающий только голые имена, такую запись не видит вовсе — ни объекта, ни
// оператора, и «ноль находок» на ней неотличим от «ноль прочитанного».

// probeFeedMigration — путь живого файла цепочки пробы от корня.
const probeFeedMigration = "services/notify/internal/probemigrations/20261002090001_notification_feed_v1.sql"

func readProbeFeedMigration(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(retiredEngineRepoRoot(t), filepath.FromSlash(probeFeedMigration)))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл цепочки пробы %s не прочитан: %v", probeFeedMigration, err)
	}
	return string(body)
}

// Близнец: живой файл как есть — объектов со следом движка 0, а операторов
// распознано не меньше, чем в нём CREATE (кавычки разбор читает).
func TestQuotedInjection_ControlProbeChainIsReadAndSilent(t *testing.T) {
	t.Parallel()
	src := readProbeFeedMigration(t)
	keys, census := scanOrFail(t, map[string]string{probeFeedMigration: src})
	creates := strings.Count(strings.ToUpper(stripSQLProse(upSection(src))), "CREATE ")
	if creates == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s нет CREATE — предпосылка пробы снята", probeFeedMigration)
	}
	if census.Statements < creates {
		t.Fatalf("распознано операторов %d при %d CREATE в %s — кавычки разбор не читает",
			census.Statements, creates, probeFeedMigration)
	}
	if len(keys) != 0 {
		t.Fatalf("на живом файле пробы найдено %v — близнец обязан молчать", keys)
	}
}

// Инъекция: таблица и ограничение в кавычках взяли имя движка → находка с
// именем объекта и НАСТОЯЩИМ путём файла цепочки.
func TestQuotedInjection_RedOnAQuotedObjectInTheProbeChain(t *testing.T) {
	t.Parallel()
	src := readProbeFeedMigration(t)
	inj := strings.Replace(src, `"notifyprobe_notification_window"`, `"notifyprobe_fga_window"`, 1)
	inj = strings.Replace(inj, "CONSTRAINT closed_carries_no_secret CHECK", `CONSTRAINT "fga_closed_ck" CHECK`, 1)
	if inj == src || strings.Count(inj, "fga") < 2 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: инъекция не нашла своего входа в %s", probeFeedMigration)
	}
	objects, _, err := FindRetiredEngineDatabaseObjects(map[string]string{probeFeedMigration: inj})
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	want := map[string]bool{"TABLE notifyprobe_fga_window": false, "CONSTRAINT fga_closed_ck": false}
	for _, o := range objects {
		k := o.Kind + " " + o.Name
		if _, ok := want[k]; ok {
			want[k] = true
		}
		line := retiredEngineFindingLine(o)
		if !strings.Contains(line, probeFeedMigration) {
			t.Errorf("строка находки не называет настоящий файл цепочки %s: %q", probeFeedMigration, line)
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("объект в кавычках %s не найден — гейт слеп к записи с кавычками (найдено %d)", k, len(objects))
		}
	}
}
