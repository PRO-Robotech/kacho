// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/check"
)

// InternalLoadBalancerAnnounceService (:9091) — read viewer-gated (v_get),
// inbound write data-plane→nlb least-priv (announce_writer). Per-RPC Check
// энфорсится на обоих листенерах (security.md «authN+authZ на каждом RPC»):
// ни один из RPC НЕ Public/exempt — internal-периметр не доверенный.
const (
	announceGetFM    = "/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/GetAnnounceState"
	announceReportFM = "/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/ReportAnnounceState"
)

func TestAnnounce_GetIsViewerGatedNotExempt(t *testing.T) {
	e, ok := check.PermissionMap()[announceGetFM]
	require.True(t, ok, "GetAnnounceState must be mapped (internal RPC fail-closed)")
	require.False(t, e.Public, "GetAnnounceState must NOT be exempt (per-RPC Check)")
	require.False(t, e.ScopeFiltered)
	// Отношение — кластерный операторский ярус, а НЕ пообъектный глагол.
	// Разница не в строгости вопроса, а в том, кто на него отвечает «да»:
	// `v_get` на балансировщике держит владелец-тенант, `system_viewer` на
	// кластерном синглтоне — только тот, кому его выдали прямым назначением.
	// Announce-state — инфра-данные (security.md), поэтому тенант читать её не
	// должен даже на своём балансировщике.
	require.Equal(t, "system_viewer", e.Relation,
		"read announce-state → кластерный операторский ярус, не тенантский глагол")
	require.NotNil(t, e.Extract)

	objType, objID, err := e.Extract(&lbv1.GetLoadBalancerAnnounceStateRequest{NetworkLoadBalancerId: "nlb-xyz"})
	require.NoError(t, err)
	require.Equal(t, "cluster", objType)
	require.Equal(t, "cluster_root", objID)
}

func TestAnnounce_ReportIsWriterGatedNotExempt(t *testing.T) {
	e, ok := check.PermissionMap()[announceReportFM]
	require.True(t, ok, "ReportAnnounceState must be mapped (internal RPC fail-closed)")
	require.False(t, e.Public, "ReportAnnounceState must NOT be exempt (per-RPC Check)")
	require.False(t, e.ScopeFiltered)
	require.Equal(t, "announce_writer", e.Relation,
		"inbound write → least-priv data-plane writer-relation (not viewer/editor tier)")
	require.NotNil(t, e.Extract)

	const id = "nlb-xyz"
	objType, objID, err := e.Extract(&lbv1.ReportLoadBalancerAnnounceStateRequest{NetworkLoadBalancerId: id})
	require.NoError(t, err)
	require.Equal(t, "nlb_network_load_balancer", objType)
	require.Equal(t, id, objID)
}

// TestAnnounce_PermissionsAreNamed — у обоих announce-RPC есть имя права, и это
// имя — то же, что в каталоге края.
//
// Здесь стояло обратное утверждение: «announce-permissions намеренно НЕ входят в
// каталог, поле пустое». Оно описывало рукописную карту, а не каталог: каталог
// края нёс оба имени всё это время, и пустое поле означало лишь, что домен о них
// умалчивал у себя. Умолчание — не «не входят»: право называлось одним артефактом
// и не называлось другим, и утверждение про «ровно 26 имён» было числом одной из
// двух сторон.
func TestAnnounce_PermissionsAreNamed(t *testing.T) {
	get := check.PermissionMap()[announceGetFM]
	rep := check.PermissionMap()[announceReportFM]
	require.Equal(t, "loadbalancer.networkLoadBalancers.getAnnounceState", get.Permission)
	require.Equal(t, "loadbalancer.networkLoadBalancers.reportAnnounceState", rep.Permission)
}
