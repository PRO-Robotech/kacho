// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

// foreign_identity_key_test.go — край снимает имя ФОРМЫ личности и под чужой
// приставкой (приёмка KAN-WIRE-1, предмет `ПР-1`, следствие для края).
//
// Утверждения стоят парами: признак, отвечающий «да» всему, снял бы у клиента
// законные заголовки, а отвечающий «нет» всему — оставил бы вход решения,
// который клиент называет сам.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

func TestForeignIdentityKeyIsClientForgeable(t *testing.T) {
	forgeable := []string{
		// Чужая приставка, обе поверхностные формы.
		"x-kaname-principal-id",
		"X-Kaname-Principal-Id",
		"Grpc-Metadata-X-Kaname-Principal-Id",
		"x-kaname-token-acr",
		// Своя приставка — как и прежде.
		"x-kacho-principal-id",
		"Grpc-Metadata-X-Kacho-Admin",
	}
	for _, k := range forgeable {
		require.True(t, principalmeta.IsClientForgeableKey(k),
			"%q обязан сниматься у клиента: имя формы личности клиент называть не вправе", k)
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них признак мог бы отвечать «да» всему, и край
	// снимал бы у клиента то, чем он пользуется.
	legitimate := []string{
		"authorization", "cookie", "x-request-id", "x-forwarded-for",
		"content-type", "Grpc-Metadata-X-Request-Id",
		// Общий секрет крючка службы личности: имя ПОД нашей приставкой, но вне
		// контракта личности; служится своим слушателем, а не этим краем.
		"X-Kacho-Hook-Token",
	}
	for _, k := range legitimate {
		require.False(t, principalmeta.IsForeignIdentityKey(k),
			"%q формой ключа личности не является — снимать его нечем и незачем", k)
	}
	require.False(t, principalmeta.IsClientForgeableKey("authorization"),
		"удостоверение обязано доезжать: без него не работал бы ни один запрос")
	require.True(t, principalmeta.IsClientForgeableKey("X-Kacho-Hook-Token"),
		"имя под НАШЕЙ приставкой снимается целиком — это прежнее решение и оно не менялось")
}
