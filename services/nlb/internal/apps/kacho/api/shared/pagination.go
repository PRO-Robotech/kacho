// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"bytes"
	"encoding/base64"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxPageSize = 1000

// ValidatePagination validates List pagination inputs (page_size + page_token) as a
// SYNC guard every nlb List use-case runs BEFORE it reads the page.
//
// Same systemic bug as compute (fixed there via service.ValidateListPagination): a List
// that short-circuits on the caller's grant before decoding the page_token answers
// `200 {[]}` for a malformed page_token / out-of-range page_size instead of
// `400 InvalidArgument`, diverging from the api-convention ("garbage token →
// InvalidArgument", page_size max 1000). Validating in the use-case makes the 400
// deterministic regardless of grant state; the repo keeps its decodePageToken/
// pageSizeOrDefault as the authoritative backstop.
//
// Token shape mirrors repo pg.decodePageToken: base64 RawURLEncoding of
// "<RFC3339Nano>\x00<id>". We assert only well-formedness (decodable + contains the
// \x00 separator); the repo re-parses the fields. Empty token = first page (valid).
func ValidatePagination(pageToken string, pageSize int64) error {
	if pageSize < 0 || pageSize > maxPageSize {
		return status.Errorf(codes.InvalidArgument, "page_size must be in range [1, %d]", maxPageSize)
	}
	if pageToken != "" {
		b, err := base64.RawURLEncoding.DecodeString(pageToken)
		if err != nil || !bytes.Contains(b, []byte{0}) {
			return status.Error(codes.InvalidArgument, "page_token is invalid")
		}
	}
	return nil
}
