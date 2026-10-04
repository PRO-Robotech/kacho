// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"testing"
)

// Отказ рукопожатия разводится по причине: сертификат не проверен — недоверие;
// истёк срок сессии или узел оборвал соединение — разрыв; узел не говорит TLS —
// нет TLS. Истёкший срок — не «нет TLS»: узел мог быть исправен и медленен.
func TestHandshakeFailure_SeparatesTheCause(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Failure
	}{
		{"сертификат", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, FailureUntrusted},
		{"срок сессии", fmt.Errorf("handshake: %w", context.DeadlineExceeded), FailureDisconnect},
		{"отмена", context.Canceled, FailureDisconnect},
		{"узел закрыл", io.EOF, FailureDisconnect},
		{"открытый текст", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, FailureNoTLS},
	}
	for _, k := range cases {
		if got := handshakeFailure(k.err); got != k.want {
			t.Errorf("%s: handshakeFailure(%v) = %v, ожидалось %v", k.name, k.err, got, k.want)
		}
	}
}
