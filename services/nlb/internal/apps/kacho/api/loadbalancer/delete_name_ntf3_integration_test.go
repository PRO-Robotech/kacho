// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

// delete_name_ntf3_integration_test.go — NTF-3 NTF3-59 (nlb-половина S1-A3,
// замысел issue-2918 З2 «Имя на снятии»): строка журнала `DELETED`
// балансировщика несёт снимок имени под ключом `name`, взятый из `RETURNING`
// удаляющего оператора. Вид `nlb_load_balancer` объявлен с формой имени
// (NameFormDNS), и функция фундамента снятие без имени отвергает до оператора —
// без снимка Delete не доходит до конца.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

func TestIntegration_DeleteLoadBalancer_DeletedRowCarriesTheName(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)
	ctx := ctxNamedCaller()

	const name = "edge-named"
	op, err := h.Create(ctx, internalAutoReq("prj-acme-test", name))
	require.NoError(t, err)
	created := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, created.Error, "ФИКСТУРА: создание балансировщика: %v", created.Error)

	var id string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT id FROM kacho_nlb.load_balancers WHERE project_id = 'prj-acme-test' AND name = $1`, name).Scan(&id))

	op, err = h.Delete(ctx, &lbv1.DeleteNetworkLoadBalancerRequest{NetworkLoadBalancerId: id})
	require.NoError(t, err)
	deleted := pollOpDone(t, opsRepo, op.GetId())
	require.Nilf(t, deleted.Error, "Delete балансировщика: %v", deleted.Error)

	var rows int
	var got string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*), coalesce(max(payload->>'name'), '')
		  FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = 'nlb_load_balancer' AND resource_id = $1 AND action = 'DELETED'`, id).Scan(&rows, &got))
	require.Equal(t, 1, rows, "строк DELETED балансировщика")
	require.Equal(t, name, got, "снимок имени в строке DELETED")
}
