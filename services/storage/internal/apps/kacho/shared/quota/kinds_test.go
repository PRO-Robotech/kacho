// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
)

// Вид, о котором СПРАШИВАЕТ совещательная полоса, обязан быть видом, который
// СПИСЫВАЕТ триггер.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1.
//
// # Зачем эта проба существует
//
// Полос учёта две, и токен вида живёт в них на РАЗНЫХ языках: Go-константа у
// совещательной, аргумент `TG_ARGV[0]` у авторитетной. Разъехаться они могут
// молча — и разъедутся не там, где это заметно.
//
// Что происходит при расхождении: use-case, спрашивающий про вид, которого
// триггер не знает, получает «потолок не назван» ВСЕГДА (строк такого вида не
// заводит никто), то есть опечатка в одном токене выключает создание целого
// ресурса наглухо и выглядит как «платформа не назвала потолок». Обратное
// расхождение тише: вид, который триггер списывает, а полоса про него не
// спрашивает, теряет РАННИЙ отказ, при том что предел продолжает соблюдаться, —
// и не выдаёт себя ничем.
//
// # Чего проба НЕ ловит, и это сказано честно
//
// Она читает ЛИТЕРАЛЫ: вид, собранный в рантайме, ей не виден. В этом дереве
// виды объявлены константами именно потому, что они координаты, а не значения, —
// первое отступление от этого станет находкой для читателя, а не для пробы.
//
// # Почему проба живёт здесь, а не в общем гейте дерева
//
// Общий гейт того же класса существует и пиннут на координаты соседнего домена.
// Обобщение его на все домены — работа над самим гейтом, и делать её из этой
// линии значило бы править чужой предмет в своём изменении. Предикат снятия этой
// пробы поэтому назван: общий гейт научился читать домен storage — эта уходит.

// storageQuotaMigration — миграция, объявляющая, на каких таблицах и под какими
// видами висят триггеры учёта. Координата выписана: вывести её из дерева можно
// только тем же поиском, который проба и делает.
const storageQuotaMigration = "services/storage/internal/migrations/0023_project_resource_quotas.sql"

// triggerKindRe — вид в объявлении триггера:
// `EXECUTE FUNCTION kacho_storage.kacho_quota_count('storage.volumes')`.
var triggerKindRe = regexp.MustCompile(`kacho_quota_count\('([a-zA-Z0-9.]+)'`)

// repoRoot поднимается от пакета к корню дерева.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 12; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "корень дерева не найден — предпосылка пробы не выполнена")
		dir = parent
	}
	t.Fatal("корень дерева не найден за 12 уровней")
	return ""
}

// TestQuotaKinds_MatchTheTriggers — наборы совпадают, и сверка идёт в ОБЕ
// стороны.
func TestQuotaKinds_MatchTheTriggers(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), storageQuotaMigration))
	require.NoError(t, err,
		"миграция учёта не читается — проверять нечего, и это провал предпосылки, "+
			"а не «ноль находок»")

	matches := triggerKindRe.FindAllStringSubmatch(string(body), -1)
	require.NotEmpty(t, matches,
		"в миграции не нашлось ни одного объявления триггера учёта: либо форма "+
			"объявления изменилась, либо проба судит пустоту")

	fromTriggers := make([]string, 0, len(matches))
	for _, m := range matches {
		fromTriggers = append(fromTriggers, m[1])
	}
	sort.Strings(fromTriggers)

	fromCode := []string{quota.KindImages, quota.KindSnapshots, quota.KindVolumes}
	sort.Strings(fromCode)

	require.Equal(t, fromTriggers, fromCode,
		"набор видов совещательной полосы разошёлся с набором видов триггеров.\n"+
			"Вид, который списывает триггер, но не спрашивает полоса, теряет ранний отказ; "+
			"вид, о котором спрашивает полоса, но не знает триггер, выключает создание "+
			"ресурса наглухо — и оба расхождения тихие.")

	t.Logf("перепись: видов в триггерах — %d, в константах домена — %d",
		len(fromTriggers), len(fromCode))
}
