// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup_test

// delete_name_ntf3_integration_test.go — NTF-3 NTF3-59 (nlb-половина S1-A3,
// замысел issue-2918 З2 «Имя на снятии»): строка журнала `DELETED` группы целей
// несёт снимок имени под ключом `name` из `RETURNING` удаляющего оператора.
// Вид `nlb_target_group` объявлен с формой имени (NameFormDNS), и функция
// фундамента снятие без имени отвергает до оператора.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

func TestIntegration_DeleteTargetGroup_DeletedRowCarriesTheName(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := mkHandler(t, repo, opsRepo)
	ctx := ctxNamedCaller()

	const name = "tg-named"
	op, err := h.Create(ctx, &lbv1.CreateTargetGroupRequest{
		ProjectId: "prj-integ-create", RegionId: "ru-central1", Name: name, Port: 8080,
		HealthCheck: &lbv1.HealthCheck{
			Interval: durationpb.New(2 * time.Second), Timeout: durationpb.New(1 * time.Second),
			UnhealthyThreshold: 2, HealthyThreshold: 2,
			Options: &lbv1.HealthCheck_Http{Http: &lbv1.HealthCheck_HttpOptions{Port: 8080, Path: "/healthz"}},
		},
		DeregistrationDelay: durationpb.New(300 * time.Second),
	})
	require.NoError(t, err)
	created := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, created.Error, "ФИКСТУРА: создание группы целей: %v", created.Error)

	var id string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT id FROM kacho_nlb.target_groups WHERE project_id = 'prj-integ-create' AND name = $1`, name).Scan(&id))

	op, err = h.Delete(ctx, &lbv1.DeleteTargetGroupRequest{TargetGroupId: id})
	require.NoError(t, err)
	deleted := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, deleted.Error, "Delete группы целей: %v", deleted.Error)

	var rows int
	var got string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*), coalesce(max(payload->>'name'), '')
		  FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = 'nlb_target_group' AND resource_id = $1 AND action = 'DELETED'`, id).Scan(&rows, &got))
	require.Equal(t, 1, rows, "строк DELETED группы целей")
	require.Equal(t, name, got, "снимок имени в строке DELETED")
}
