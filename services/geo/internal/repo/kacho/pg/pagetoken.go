// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
)

// pageCursor — непрозрачный курсор, кодируемый в page_token (base64 JSON).
// Region/Zone List сортирует и фильтрует по id (WHERE id > $1), поэтому курсор —
// id-only: created_at в токене не использовался (мертвый payload) и убран.
type pageCursor struct {
	ID string `json:"id"`
}

// encodePageToken собирает непрозрачный base64-курсор из id.
func encodePageToken(id string) string {
	b, _ := json.Marshal(pageCursor{ID: id})
	return base64.StdEncoding.EncodeToString(b)
}

// decodePageToken разбирает непрозрачный курсор; возвращает (id, err).
func decodePageToken(token string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", err
	}
	var c pageCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", err
	}
	if c.ID == "" {
		return "", fmt.Errorf("empty cursor id")
	}
	return c.ID, nil
}

// invalidPageTokenErr оборачивает мусорный page_token в доменный sentinel
// geoerrors.ErrInvalidArg (не gRPC-status: выбор transport-кода — concern
// handler/serviceerr, а не repo-адаптера; так же, как все прочие repo-ошибки
// возвращаются sentinel'ом через dberr.Wrap). serviceerr.ToStatus замапит его в
// codes.InvalidArgument — единая таблица маппинга, без утечки grpc в adapter-слой.
func invalidPageTokenErr(err error) error {
	return fmt.Errorf("%w: invalid page_token: %v", geoerrors.ErrInvalidArg, err)
}
