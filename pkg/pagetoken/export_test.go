// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pagetoken

import "encoding/base64"

// encodeRawForTest даёт пробе построить ПРОИЗВОЛЬНОЕ тело токена — в том числе
// негодное и в прежних формах. Кодек такого входа не производит by construction,
// поэтому без этого производителя отрицательные пробы не на чем было бы прогнать.
func encodeRawForTest(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}
