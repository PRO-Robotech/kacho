// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// TestOperationToProto_TruncatesTimestampsToSecond — created_at/modified_at
// операции усекаются до секунд на wire (единый apiconv-формат: микросекунды с БД
// не текут наружу, как и в protoconv.ts для Region/Zone).
func TestOperationToProto_TruncatesTimestampsToSecond(t *testing.T) {
	created := time.Date(2026, 7, 9, 12, 0, 0, 123_456_000, time.UTC)
	modified := time.Date(2026, 7, 9, 12, 5, 0, 987_654_000, time.UTC)

	got := operationToProto(&operations.Operation{
		ID:         "opr-test",
		CreatedAt:  created,
		ModifiedAt: modified,
	})

	if !got.GetCreatedAt().AsTime().Equal(created.Truncate(time.Second)) {
		t.Errorf("created_at not truncated to second: got %v, want %v",
			got.GetCreatedAt().AsTime(), created.Truncate(time.Second))
	}
	if !got.GetModifiedAt().AsTime().Equal(modified.Truncate(time.Second)) {
		t.Errorf("modified_at not truncated to second: got %v, want %v",
			got.GetModifiedAt().AsTime(), modified.Truncate(time.Second))
	}
}
