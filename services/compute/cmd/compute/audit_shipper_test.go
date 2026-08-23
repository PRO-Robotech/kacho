// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
)

// TestAuditShipperRefusesToStartOnASilencedStream — брат пробы службы прав, и
// заведён он ИМЕННО потому, что здесь предмета сегодня нет.
//
// Логгер этой службы строится без ручки уровня, поэтому заглушить журнал сейчас
// нечем. Свойство от этого не становится проверенным: ручка уровня появляется у
// службы одной строкой, и появится она у того, кто про журнал аудита не думает.
// Проба утверждает СТРАЖА, а не сегодняшнюю настройку, — тогда день, когда
// ручку заведут, не станет днём, когда журнал перестанет доезжать молча.
//
// Пара обязательна: без нормального старта на Info проба зеленела бы на страже,
// отвергающем любой уровень.
func TestAuditShipperRefusesToStartOnASilencedStream(t *testing.T) {
	for _, tc := range []struct {
		name   string
		level  slog.Level
		refuse bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := observability.NewSloggerLevel(io.Discard, tc.level)

			sink, err := auditSink(logger)
			switch {
			case tc.refuse && err == nil:
				t.Fatalf("уровень %s заглушает журнал аудита — служба обязана отказать в старте, "+
					"а не подняться с очередью, растущей бессрочно", tc.level)
			case tc.refuse:
				if !strings.Contains(err.Error(), "журнал аудита не доедет") {
					t.Fatalf("отказ обязан называть предмет и ручку, а не быть общим: %v", err)
				}
				if sink != nil {
					t.Fatal("при отказе приёмник не отдаётся")
				}
			case err != nil:
				t.Fatalf("уровень %s поток принимает — служба обязана подняться: %v", tc.level, err)
			case sink == nil:
				t.Fatal("на исправном уровне приёмник обязан быть построен")
			}

			// Порядок: предпосылка журнала проверяется ДО зависимостей вывоза.
			// Пул здесь заведомо отсутствует, и на заглушённом уровне отказ обязан
			// называть ЖУРНАЛ, а не первую попавшуюся недостающую зависимость —
			// иначе оператор чинил бы не то.
			_, werr := buildAuditShipper(nil, metrics.NewMemRecorder(), logger)
			if werr == nil {
				t.Fatal("без пула вывоз построиться не может — проверка порядка беспредметна")
			}
			named := strings.Contains(werr.Error(), "журнал аудита не доедет")
			if named != tc.refuse {
				t.Fatalf("уровень %s: отказ проводки %q — предпосылка журнала обязана "+
					"проверяться первой и только она", tc.level, werr)
			}
		})
	}
}
