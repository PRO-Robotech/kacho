// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
)

// cursor — opaque page_token курсорной пагинации List по (created_at, id) ASC
// (api-conventions.md). Кодируется как base64("<unixnano>|<id>").
type cursor struct {
	createdAt time.Time
	id        string
}

// encodePageToken сериализует курсор последней строки страницы в opaque base64.
func encodePageToken(c cursor) string {
	raw := fmt.Sprintf("%d|%s", c.createdAt.UTC().UnixNano(), c.id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodePageToken разбирает opaque page_token. Битый токен → ErrInvalidArg
// (api-conventions.md garbage-token → InvalidArgument), без утечки внутренней формы.
func decodePageToken(token string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: invalid page_token", ports.ErrInvalidArg)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return cursor{}, fmt.Errorf("%w: invalid page_token", ports.ErrInvalidArg)
	}
	var nanos int64
	if _, err := fmt.Sscanf(parts[0], "%d", &nanos); err != nil {
		return cursor{}, fmt.Errorf("%w: invalid page_token", ports.ErrInvalidArg)
	}
	return cursor{createdAt: time.Unix(0, nanos).UTC(), id: parts[1]}, nil
}
