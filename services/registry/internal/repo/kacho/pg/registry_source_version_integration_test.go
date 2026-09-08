// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registry_source_version_integration_test.go — outbox source_version обязан быть
// commit-order-monotonic per-object маркером В ШКАЛЕ ВРЕМЕНИ (clock_timestamp(),
// миграция 0011), пригодным к декодированию drainer'ом и сравнимым с версией, которую
// синхронный registrar штампует после commit'а.
//
// Почему не BIGSERIAL (как было с 0002 до 0011): вторым путём доставки той же
// регистрации идёт синхронный registrar в процессе сервиса — у него нет outbox-id, а
// kaname хранит маркер как timestamptz и принимает его как protobuf.Timestamp. Пока
// шкалы расходились, маркер не доставлялся ВООБЩЕ ни одним путём, и gate редоставки в
// iam — намеренно требующий положительного доказательства редоставки — открывался в
// сторону работы: registry платил за обе доставки.
//
// Почему не to_jsonb(now()) (как было до 0002): now() == transaction_timestamp()
// фиксируется на BEGIN, поэтому под конкуренцией маркер мог оказаться в ОБРАТНОМ
// commit-порядку. clock_timestamp() читает часы на INSERT'е, который у воркера,
// захватившего row-lock вторым, исполняется после commit'а первого — свойство
// монотонности из 0002 сохранено, заменена только шкала (см.
// TestRepo_SourceVersion_MonotonicUnderConcurrentUpdates).
package pg_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// decodeLastIntent читает payload последней outbox-строки ресурса и прогоняет его
// через ТОТ ЖЕ декодер, что и register-drainer, — так тест проверяет весь путь
// «триггер → JSONB → domain.RegisterIntent», а не только SQL-сторону.
func decodeLastIntent(t *testing.T, pool *pgxpool.Pool, resourceID string) domain.RegisterIntent {
	t.Helper()
	var payload []byte
	err := pool.QueryRow(context.Background(),
		`SELECT payload
		   FROM kacho_registry.registry_outbox
		  WHERE resource_id=$1
		  ORDER BY id DESC LIMIT 1`, resourceID).Scan(&payload)
	require.NoError(t, err)
	intent, err := domain.UnmarshalRegisterIntent(payload)
	require.NoError(t, err, "payload must decode with the drainer's decoder, not poison the row")
	return intent
}

// TestRepo_SourceVersion_IsDecodableTimestamp — маркер, застампленный триггером,
// доезжает до drainer'а как НЕПУСТОЕ время. Это и есть предусловие экономии: без
// доставленной версии gate редоставки в kaname доказательства не имеет и
// открывается в сторону работы.
func TestRepo_SourceVersion_IsDecodableTimestamp(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Minute)
	r := newReg("prj-P", "team-images", map[string]string{"env": "prod"})
	_, _, err := repo.Insert(ctx, r, domain.RegisterIntentForCreate(r, "user", "usr-alice"))
	require.NoError(t, err)
	after := time.Now().UTC().Add(time.Minute)

	intent := decodeLastIntent(t, pool, r.ID)
	require.False(t, intent.SourceVersion.IsZero(), "outbox row must carry a decodable source_version")
	assert.True(t, intent.SourceVersion.After(before) && intent.SourceVersion.Before(after),
		"source_version must be wall-clock now, got %s", intent.SourceVersion.Time)
}

// TestRepo_SourceVersion_MonotonicAcrossUpdates — последовательные мутации одного
// реестра дают СТРОГО растущий маркер: последнее состояние несёт больший, поэтому
// last-source-state-wins в зеркале iam выбирает именно его, а повторная регистрация
// («выдать → отозвать → выдать») не может схлопнуться с предыдущей.
func TestRepo_SourceVersion_MonotonicAcrossUpdates(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	r := newReg("prj-P", "team-images", map[string]string{"env": "prod"})
	_, _, err := repo.Insert(ctx, r, domain.RegisterIntentForCreate(r, "user", "usr-alice"))
	require.NoError(t, err)
	v0 := decodeLastIntent(t, pool, r.ID)

	upd := func(env string) domain.RegisterIntent {
		_, uerr := repo.Update(ctx, registry.UpdateSpec{RegistryID: r.ID, ApplyLabels: true, Labels: map[string]string{"env": env}},
			func(rr *domain.Registry) domain.RegisterIntent { return domain.RegisterIntentForUpdate(rr) })
		require.NoError(t, uerr)
		return decodeLastIntent(t, pool, r.ID)
	}
	v1 := upd("staging")
	v2 := upd("canary")
	require.True(t, v1.SourceVersion.After(v0.SourceVersion.Time), "update-1 strictly increases the marker")
	require.True(t, v2.SourceVersion.After(v1.SourceVersion.Time), "update-2 strictly increases the marker")
}

// TestRepo_SourceVersion_UnregisterTombstoneNotOlderThanRegister — снятие регистрации
// несёт маркер НЕ СТАРШЕ последней регистрации того же объекта. kaname сносит строку
// зеркала под `source_version <= $tombstone`, поэтому более старый tombstone оставил бы
// зеркало (и tuple'ы, которые level-triggered реконсайлер с него ре-материализует)
// пережившими удаление реестра. Снятие регистрации обязано удалять строку целиком.
func TestRepo_SourceVersion_UnregisterTombstoneNotOlderThanRegister(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	r := newReg("prj-P", "team-images", nil)
	_, _, err := repo.Insert(ctx, r, domain.RegisterIntentForCreate(r, "user", "usr-alice"))
	require.NoError(t, err)
	reg := decodeLastIntent(t, pool, r.ID)

	_, err = repo.MarkDeleting(ctx, r.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, r.ID, domain.UnregisterIntentForDelete(r.ID, r.ProjectID)))

	tomb := decodeLastIntent(t, pool, r.ID)
	require.False(t, tomb.SourceVersion.IsZero(), "unregister row must carry a tombstone version")
	assert.False(t, tomb.SourceVersion.Before(reg.SourceVersion.Time),
		"tombstone %s must not precede the register it revokes (%s)", tomb.SourceVersion.Time, reg.SourceVersion.Time)
}

// TestRepo_SourceVersion_MonotonicUnderConcurrentUpdates — свойство, ради которого
// миграция 0002 ушла от now(): под конкуренцией маркер обязан следовать COMMIT-порядку.
// Два Update-воркера одного реестра сериализуются row-lock'ом на registries; тот, кто
// закоммитился позже, обязан нести СТРОГО больший маркер. С now()
// (== transaction_timestamp, фиксируется на BEGIN) это ломалось; clock_timestamp()
// читается на INSERT'е, то есть уже под захваченным row-lock'ом.
func TestRepo_SourceVersion_MonotonicUnderConcurrentUpdates(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	r := newReg("prj-P", "team-images", map[string]string{"env": "prod"})
	_, _, err := repo.Insert(ctx, r, domain.RegisterIntentForCreate(r, "user", "usr-alice"))
	require.NoError(t, err)

	// Обе транзакции стартуют «одновременно»; порядок их commit'а определяет row-lock.
	var wg sync.WaitGroup
	for _, env := range []string{"staging", "canary"} {
		wg.Add(1)
		go func(env string) {
			defer wg.Done()
			_, uerr := repo.Update(ctx, registry.UpdateSpec{RegistryID: r.ID, ApplyLabels: true, Labels: map[string]string{"env": env}},
				func(rr *domain.Registry) domain.RegisterIntent { return domain.RegisterIntentForUpdate(rr) })
			assert.NoError(t, uerr)
		}(env)
	}
	wg.Wait()

	// Маркеры outbox-строк в порядке их id (== порядок INSERT'а == порядок commit'а
	// под row-lock) обязаны строго расти.
	rows, err := pool.Query(ctx,
		`SELECT payload FROM kacho_registry.registry_outbox WHERE resource_id=$1 ORDER BY id`, r.ID)
	require.NoError(t, err)
	defer rows.Close()

	var prev time.Time
	n := 0
	for rows.Next() {
		var payload []byte
		require.NoError(t, rows.Scan(&payload))
		intent, derr := domain.UnmarshalRegisterIntent(payload)
		require.NoError(t, derr)
		require.False(t, intent.SourceVersion.IsZero())
		if n > 0 {
			require.True(t, intent.SourceVersion.After(prev),
				"row %d marker %s must strictly follow the previous %s (commit order)", n, intent.SourceVersion.Time, prev)
		}
		prev = intent.SourceVersion.Time
		n++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 3, n, "create + two updates")
}
