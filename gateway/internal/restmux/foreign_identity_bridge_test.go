// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// foreign_identity_bridge_test.go — мост не пропускает имя ФОРМЫ личности под
// чужой приставкой (приёмка KAN-WIRE-1, предмет `ПР-1`, следствие для края).
//
// Сужение моста ключевалось на НАШУ приставку, поэтому чужую библиотека
// бриджила умолчанием. Пока такое имя никто не читал, оно было безобидно; после
// того как слушатель научился отличать «личность объявлена и не приехала» от
// законной безымянности, клиент мог бы прислать его и получить отказ на свой же
// запрос.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBridgeDropsAForeignIdentityKey(t *testing.T) {
	dropped := []string{
		"Grpc-Metadata-X-Kaname-Principal-Id",
		"Grpc-Metadata-X-Kaname-Principal-Type",
		"Grpc-Metadata-X-Kaname-Token-Acr",
		"X-Kaname-Principal-Id",
	}
	for _, k := range dropped {
		name, ok := principalHeaderMatcher(k)
		require.False(t, ok, "%q пересёк мост как %q — имя формы личности клиент называть не вправе", k, name)
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них проба зеленела бы на сопоставителе, который
	// отбрасывает всё: мост был бы «безопасен» и неработоспособен разом.
	passed := map[string]string{
		"Grpc-Metadata-X-Request-Id": "X-Request-Id",
		"Authorization":              "grpcgateway-Authorization",
	}
	for k, want := range passed {
		name, ok := principalHeaderMatcher(k)
		require.True(t, ok, "%q обязан пересекать мост: без него тракт не работал бы", k)
		require.Equal(t, want, name, "%q пересёк мост под именем %q", k, name)
	}
}
