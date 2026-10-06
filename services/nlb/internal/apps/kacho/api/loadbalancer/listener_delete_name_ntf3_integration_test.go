// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

// listener_delete_name_ntf3_integration_test.go — NTF-3 NTF3-59 (nlb-половина
// S1-A3, замысел issue-2918 З2 «Имя на снятии»), слушатель. Вид `nlb_listener`
// объявлен с формой имени (NameFormDNS), и функция фундамента снятие без имени
// под ключом `name` отвергает до оператора: тогда транзакция снятия
// откатывается, и Delete слушателя не доходит до конца НИКОГДА.
//
// Проба лежит в пакете балансировщика, потому что здесь есть стенд на настоящей
// базе и настоящем писателе журнала; у пакета слушателя его нет, а двойник
// журнала функцию фундамента не исполняет и этот отказ не воспроизводит.

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/ids"
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/listener"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func TestIntegration_DeleteListener_DeletedRowCarriesTheName(t *testing.T) {
	t.Parallel()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)

	w, err := repo.Writer(ctxNamedCaller())
	require.NoError(t, err)
	lb := &domain.LoadBalancer{
		ID:        domain.ResourceID(ids.NewID(ids.PrefixLoadBalancer)),
		ProjectID: "prj-x", RegionID: "ru-central1",
		Name: "edge", Type: domain.LBTypeExternal, Status: domain.LBStatusInactive,
		SessionAffinity: domain.SessionAffinity5Tuple,
	}
	_, err = w.LoadBalancers().Insert(context.Background(), lb)
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	const name = "lsn-named"
	lsnID := ids.NewID(ids.PrefixListener)
	require.NoError(t, execJournaledFixture(pool, `
		INSERT INTO kacho_nlb.listeners (id, project_id, load_balancer_id, region_id, name,
			description, labels, protocol, port,
			default_target_group_id, status)
		VALUES ($1, $2, $3, $4, $5, '', '{}', 'TCP', 8080, '', 'ACTIVE')`,
		lsnID, "prj-x", string(lb.ID), "ru-central1", name,
	), "ФИКСТУРА: строка слушателя")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	uc := listener.NewDeleteUseCase(repo, opsRepo, logger)
	op, err := uc.Run(ctxNamedCaller(), &lbv1.DeleteListenerRequest{ListenerId: lsnID})
	require.NoError(t, err)
	deleted := pollOpDone(t, opsRepo, op.ID)
	require.Nilf(t, deleted.Error, "Delete слушателя: %v", deleted.Error)

	var gone bool
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT NOT EXISTS (SELECT 1 FROM kacho_nlb.listeners WHERE id = $1)`, lsnID).Scan(&gone))
	require.True(t, gone, "строка слушателя снята")

	var rows int
	var got string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*), coalesce(max(payload->>'name'), '')
		  FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = 'nlb_listener' AND resource_id = $1 AND action = 'DELETED'`, lsnID).Scan(&rows, &got))
	require.Equal(t, 1, rows, "строк DELETED слушателя")
	require.Equal(t, name, got, "снимок имени в строке DELETED")
}
