// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"encoding/json"
	"fmt"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// marshalJSONB сериализует v в JSONB-байты. Возвращает обёрнутую ports.ErrInternal
// при ошибке. Парная форма к unmarshalJSONB. Зеркалит kacho-vpc/internal/repo/jsonb.go.
func marshalJSONB(v any, field string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal JSONB %s: %v", ports.ErrInternal, field, err)
	}
	return b, nil
}

// unmarshalJSONB десериализует JSONB-байты в target. Возвращает обёрнутую
// ports.ErrInternal при ошибке. nil/empty raw — no-op.
func unmarshalJSONB(raw []byte, target any, field string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: corrupted JSONB %s: %v", ports.ErrInternal, field, err)
	}
	return nil
}
