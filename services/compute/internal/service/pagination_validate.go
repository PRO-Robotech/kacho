// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"bytes"
	"encoding/base64"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/validate"
)

// ValidateListPagination validates the List pagination inputs (page_size + page_token)
// as a SYNC, request-shape guard the handler can run BEFORE the listauthz empty-grant
// short-circuit.
//
// Why here (not only in the repo): every compute List handler returns an empty page
// early when the caller's per-object grant resolves to zero ids — without ever calling
// the repo. The repo IS where page_size (validate.PageSize) + page_token (decode) are
// validated, so a malformed page_token / out-of-range page_size on an empty-grant List
// used to fall through to `200 {[]}` instead of `400 InvalidArgument`, diverging from
// the api-convention ("garbage token → InvalidArgument", "page_size max 1000") and from
// the sibling vpc service. Validating here makes the 400 deterministic regardless of
// grant state, and the repo keeps its own validation as the authoritative backstop.
//
// Token shape mirrors repo.decodePageToken: base64 RawURLEncoding of "<unixnano>:<id>".
// We only assert well-formedness (decodable + contains the ':' separator) — the repo
// re-parses the fields. Empty token = first page (valid).
func ValidateListPagination(p Pagination) error {
	if _, err := validate.PageSize("page_size", p.PageSize); err != nil {
		return err
	}
	if p.PageToken != "" {
		b, err := base64.RawURLEncoding.DecodeString(p.PageToken)
		if err != nil || !bytes.Contains(b, []byte(":")) {
			return status.Error(codes.InvalidArgument, "page_token is invalid")
		}
	}
	return nil
}
