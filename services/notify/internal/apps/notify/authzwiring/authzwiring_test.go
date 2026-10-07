// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzwiring

import (
	"context"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/listnarrow"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

type allow struct{}

func (allow) BatchCheck(_ context.Context, checks []listnarrow.Check) ([]bool, error) {
	out := make([]bool, len(checks))
	for i := range out {
		out[i] = true
	}
	return out, nil
}

// TestNewListNarrower_WindowIsTheKnobAndZeroIsRefused — нулевое окно — ошибка
// сборки, а не умолчание фундамента; близнец — положительное окно собирает
// сужатель, который кладёт положительный вердикт в окно и знает отношение
// каждого типа перечня.
func TestNewListNarrower_WindowIsTheKnobAndZeroIsRefused(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	if _, err := NewListNarrower(allow{}, config.ListFilter{CheckTimeout: time.Second}, clock); err == nil {
		t.Fatal("нулевое окно принято")
	}
	n, err := NewListNarrower(allow{}, config.ListFilter{CacheTTL: 2 * time.Second, CheckTimeout: time.Second}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Visible(context.Background(), "user:usr-c", "compute_instance", ActionGet, RelationGet, []string{"ins-1"}); err != nil {
		t.Fatal(err)
	}
	if got := n.CacheSize(); got != 1 {
		t.Fatalf("окно держит %d записей, ожидалась 1", got)
	}
	if rels, ok := n.Relations("storage_image"); !ok || len(rels) != 1 || rels[0] != RelationGet {
		t.Fatalf("отношение типа storage_image: %v %v", rels, ok)
	}
}
