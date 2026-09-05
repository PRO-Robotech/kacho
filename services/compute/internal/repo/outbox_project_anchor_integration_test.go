// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// TestOutboxRowsCarryTheProjectAnchorOnEveryPath — КАЖДЫЙ путь записи журнала
// проставляет якорь проекта, и путь СНЯТИЯ проверяется отдельно.
//
// Это проба ПРОИЗВОДСТВЕННОГО пути, а не объявления журнала: подписка читает
// колонку, а заполняет её репозиторий, и между ними нет ничего, что поймало бы
// расхождение. Соседняя проба поднимает поток над готовыми строками — она
// доказывает, что колонку ЧИТАЮТ верно, и молчала бы, если бы её никто не ПИСАЛ.
//
// Снятие вынесено в отдельное утверждение, потому что оно единственное, где
// якоря нет в нагрузке: нагрузка удаления несёт один идентификатор. Пропусти его
// репозиторий — событие удаления уходило бы с пустым якорем, подписка с осью
// проекта молча его не пропускала бы, и потребитель, снявший опрос, держал бы
// удалённую машину вечно. Отказ этот тихий, поэтому он утверждается, а не
// подразумевается.
func TestOutboxRowsCarryTheProjectAnchorOnEveryPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	inID := ids.NewID(ids.PrefixInstance)
	const projectID = "proj-ccccccccccccccccc"
	in := &domain.Instance{
		ID: inID, ProjectID: projectID, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: "vm-anchor", ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning,
		FQDN: inID + ".auto.internal", InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
	}
	_, _, err = instRepo.Insert(ctx, in)
	require.NoError(t, err)
	require.NoError(t, instRepo.Delete(ctx, inID))

	// Перепись по РОДАМ, а не общим числом: общее число сошлось бы и тогда, когда
	// якорь есть у создания и потерян у снятия — то есть ровно в том случае, ради
	// которого проба написана.
	rows, err := pool.Query(ctx,
		`SELECT event_type, project_id FROM compute_outbox WHERE resource_id = $1 ORDER BY sequence_no`, inID)
	require.NoError(t, err)
	defer rows.Close()

	anchors := map[string]string{}
	for rows.Next() {
		var kind, anchor string
		require.NoError(t, rows.Scan(&kind, &anchor))
		anchors[kind] = anchor
	}
	require.NoError(t, rows.Err())

	require.Contains(t, anchors, "CREATED", "путь создания не записал строки журнала вовсе")
	require.Contains(t, anchors, "DELETED", "путь снятия не записал строки журнала вовсе")

	assert.Equal(t, projectID, anchors["CREATED"], "у события создания якорь проекта пуст")
	assert.Equal(t, projectID, anchors["DELETED"],
		"у события СНЯТИЯ якорь проекта пуст. Нагрузка снятия несёт один идентификатор, "+
			"поэтому якорь брать больше неоткуда: подписка с осью проекта такое событие "+
			"не пропустит, и потребитель, снявший опрос, будет держать удалённую машину вечно")
}
