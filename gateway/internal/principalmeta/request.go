// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta

import (
	"net/http"

	"google.golang.org/grpc/metadata"
)

// MetadataFromRequest собирает исходящие gRPC-метаданные личности из
// заголовков, выставленных полосой аутентификации края.
//
// # Почему это ОДИН строитель на всех вызывающих
//
// Личность на backend отправляют двое: мост REST→gRPC (`restmux`) и всякая
// ручка края, которая дозванивается сама (проекция потока подписки). Второй
// строитель разошёлся бы с первым молча — и разошёлся бы именно там, где
// расхождение не видно: на обычном запросе оба кладут одно и то же, а
// различаются на кириллическом имени, на отсутствующем заголовке и на мостовой
// форме.
//
// # Что кладётся и почему именно это
//
//   - `x-kacho-principal-{type,id}` — личность конечного вызывающего, которую
//     trust-aware извлечение backend'а заносит в свой контекст;
//   - `x-kacho-principal-display-name-bin` — отображаемое имя ДВОИЧНЫМ ключом:
//     значение обычного ключа gRPC ограничено печатаемой латиницей, а продукт
//     русскоязычный, и кириллическое имя роняет ВЕСЬ вызов, не дойдя до
//     обработчика;
//   - `x-kacho-token-acr` — подтверждённый уровень: без него пол уровня
//     обрывался бы на внутреннем передозвоне.
//
// Полоса аутентификации ставит заголовки в двух формах — голой и мостовой
// (`Grpc-Metadata-`); читаются обе. Отсутствующий заголовок КЛЮЧА НЕ ДОБАВЛЯЕТ:
// пустое значение backend прочитал бы как названную пустую личность.
func MetadataFromRequest(r *http.Request) metadata.MD {
	md := metadata.MD{}
	get := func(canonical, fallback string) string {
		if v := r.Header.Get(canonical); v != "" {
			return v
		}
		return r.Header.Get(fallback)
	}
	principalType := get(HeaderGRPCMetaPrincipalType, HeaderPrincipalType)
	principalID := get(HeaderGRPCMetaPrincipalID, HeaderPrincipalID)
	displayName := get(HeaderGRPCMetaPrincipalDisplay, HeaderPrincipalDisplay)
	acr := get(HeaderGRPCMetaTokenACR, HeaderTokenACR)
	if principalType != "" {
		md.Append(MetaPrincipalType, principalType)
	}
	if principalID != "" {
		md.Append(MetaPrincipalID, principalID)
	}
	if displayName != "" {
		md.Append(MetaPrincipalDisplayBin, displayName)
	}
	if acr != "" {
		md.Append(MetaTokenACR, acr)
	}
	return md
}
