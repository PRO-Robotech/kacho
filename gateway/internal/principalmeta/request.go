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
//     обрывался бы на внутреннем передозвоне;
//   - `x-kacho-token-session-id` — номер записи сессии, за которую край
//     проксирует запрос (kaname#677): служба доступа принимает его, только
//     когда значение ОДНО, поэтому две формы заголовка сводятся здесь к одному
//     значению, а мост этот ключ не пропускает.
//
// Полоса аутентификации ставит заголовки в двух формах — голой и мостовой
// (`Grpc-Metadata-`); читаются обе. Отсутствующий заголовок КЛЮЧА НЕ ДОБАВЛЯЕТ:
// пустое значение backend прочитал бы как названную пустую личность.
func MetadataFromRequest(r *http.Request) metadata.MD {
	md := metadata.MD{}
	principalType := headerEither(r, HeaderGRPCMetaPrincipalType, HeaderPrincipalType)
	principalID := headerEither(r, HeaderGRPCMetaPrincipalID, HeaderPrincipalID)
	displayName := headerEither(r, HeaderGRPCMetaPrincipalDisplay, HeaderPrincipalDisplay)
	acr := headerEither(r, HeaderGRPCMetaTokenACR, HeaderTokenACR)
	sessionRecord := headerEither(r, HeaderGRPCMetaTokenSessionID, HeaderTokenSessionID)
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
	if sessionRecord != "" {
		md.Append(MetaTokenSessionID, sessionRecord)
	}
	return md
}

// headerEither читает мостовую форму заголовка, а при её отсутствии — голую.
//
// Один читатель на весь пакет: полоса аутентификации ставит заголовки в обеих
// формах, и второй порядок чтения разошёлся бы с первым молча — на обычном
// запросе оба дают одно и то же, а различаются там, где выставлена лишь одна.
func headerEither(r *http.Request, canonical, fallback string) string {
	if v := r.Header.Get(canonical); v != "" {
		return v
	}
	return r.Header.Get(fallback)
}
