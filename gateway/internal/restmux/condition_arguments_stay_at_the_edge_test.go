// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// condition_arguments_stay_at_the_edge_test.go — доводы условия модели прав за
// мост НЕ едут (#1252).
//
// ПОЧЕМУ ЭТО ОТДЕЛЬНОЕ УТВЕРЖДЕНИЕ, А НЕ СЛЕДСТВИЕ ИМЕНИ. Мост снимает свой
// префикс сам, поэтому «мостовой формы у ключа нет» его не удерживает: голая
// форма пересекает мост наравне с префиксованной. Замер до правки: обе формы
// `X-Kacho-Token-Amr` отбор ПРОПУСКАЛ. Решение «остаётся на краю» поэтому
// объявлено явно, и проверяется оно здесь.
//
// ПОЧЕМУ С ПОЛОЖИТЕЛЬНЫМ КОНТРОЛЕМ. Утверждение «мост это не пропускает» верно и
// для отбора, который не пропускает ничего, — а такой отбор потерял бы личность
// на каждом запросе.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

func TestBridge_DoesNotCarryTheConditionArguments(t *testing.T) {
	edgeOnly := []struct{ bare, bridged string }{
		{principalmeta.HeaderTokenAMR, "Grpc-Metadata-" + principalmeta.HeaderTokenAMR},
		{principalmeta.HeaderTokenMfaAt, "Grpc-Metadata-" + principalmeta.HeaderTokenMfaAt},
	}
	for _, k := range edgeOnly {
		for _, key := range []string{k.bare, k.bridged} {
			if name, ok := principalHeaderMatcher(key); ok {
				t.Errorf("мост пропустил %s как %q — довод условия уехал к соседу, "+
					"который его не читает: мёртвая поверхность решения о правах", key, name)
			}
		}
	}

	// Положительный контроль: соседние ключи того же семейства мост обязан
	// пропускать — у них за краем есть потребитель, и отбор, отбросивший их,
	// молча потерял бы срок, область и уровень удостоверения.
	for _, key := range []string{"X-Kacho-Token-Jti", "Grpc-Metadata-X-Kacho-Token-Scope"} {
		if name, ok := principalHeaderMatcher(key); !ok || name == "" {
			t.Errorf("мост отбросил %s — у этого ключа других производителей нет", key)
		}
	}
	t.Logf("перепись: осмотрено ключей края %d (по 2 формы), контрольных ключей 2", len(edgeOnly))
}
