// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// ackpath_test.go — путь `Ack` строки — путь пачки (полоса A2 задачи #2915):
// пачка без него — ошибка сборки цикла источника, и строки не начинаются
// вовсе (ни права, ни SMTP): их выдаст следующий `Claim` после конца аренды.

import (
	"context"
	"strings"
	"testing"
	"time"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"

	"github.com/PRO-Robotech/kacho/services/notify/internal/source"
)

func TestDeliver_BatchWithoutAckPathStartsNoRow(t *testing.T) {
	r := newRig(t, rigOpts{build: buildOf(t, buildEntry{nsProbe, "probe-bhello", 1})})
	row := signRow()
	sentAt := time.Now()
	r.feeds[nsProbe].lease(sentAt, row)
	r.w.Deliver(context.Background(), source.Batch{Source: r.sources[nsProbe], Rows: []*notifyv1.ClaimedNotification{row}, SentAt: sentAt})
	r.w.Wait()
	if n := r.peer.Calls(); n != 0 {
		t.Fatalf("строка пачки без пути Ack спросила право (%d вызовов ResolveSend)", n)
	}
	if n := r.sessions(); n != 0 {
		t.Fatalf("строка пачки без пути Ack открыла SMTP-сессию (%d)", n)
	}
	if !strings.Contains(r.log.String(), "пути Ack") {
		t.Fatalf("журнал не называет пачку без пути Ack:\n%s", r.log.String())
	}

	// Близнец: та же строка с путём пачки доходит до исхода.
	twin := signRow()
	r.wantSent(t, twin, r.one(nsProbe, twin))
}
