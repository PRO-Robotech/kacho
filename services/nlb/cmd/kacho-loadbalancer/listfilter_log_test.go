// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
)

// stubConn — non-nil clients.Conn so buildListFilter takes its enabled branch.
// It is never dialled: the test reads the boot log, not the wire.
type stubConn struct{}

func (stubConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error { return nil }
func (stubConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}
func (stubConn) Close() error { return nil }

// TestBuildListFilter_LogNamesTheKnobThatGuardsTheConnection — the boot log must
// report the mTLS switch that actually protects the connection the list-filter is
// built over.
//
// The filter is built over the INTERNAL iam connection (the one that also carries
// InternalIAMService.Check), which is guarded by mtls.iam-register. Reporting the
// state of mtls.iam-project instead sends the operator to a knob that has no
// effect on this edge: the two are deliberately separate fields, because the two
// listeners have different dial hosts and cannot share one ServerName. An operator
// who trusts the log turns the wrong one and concludes the edge is protected.
//
// The two switches are given OPPOSITE values here, so the log cannot accidentally
// look right.
func TestBuildListFilter_LogNamesTheKnobThatGuardsTheConnection(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := &config.Config{}
	cfg.Authz.ListFilter = config.AuthzListFilterConfig{
		Enabled:         true,
		Timeout:         500 * time.Millisecond,
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 100,
		FailOpen:        false,
	}
	// The knob that guards the connection the filter is built over.
	cfg.MTLS.IAMRegister.Enable = true
	// The knob that guards the OTHER (public) iam edge — deliberately opposite.
	cfg.MTLS.IAMProject.Enable = false

	f := buildListFilter(cfg, stubConn{}, logger)
	require.NotNil(t, f, "list-filter enabled with a live conn must be built")

	var rec map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec))
	require.Equal(t, "list_filter_enabled", rec["msg"])
	require.Equal(t, true, rec["iam_authz_mtls"],
		"the log must report mtls.iam-register (the internal edge the filter uses), not mtls.iam-project")
}
