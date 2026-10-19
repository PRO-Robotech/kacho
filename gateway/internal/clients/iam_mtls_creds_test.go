// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iam_mtls_creds_test.go — швы удостоверения ребра край→служба доступа:
// соединение решения о праве принимает внедряемое удостоверение транспорта, и
// корень подаёт ему mTLS ребра под KACHO_API_GATEWAY_MTLS_IAM_ENABLE.
//
// Рукопожатие здесь не утверждается — его держат bufconn-пробы корня против
// настоящего TLS-сервера. Здесь утверждается, что шов есть: своё удостоверение
// транспорта принято, и клиент собирается без сетевого обмена (grpc.NewClient
// ленив).
package clients_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
)

// Клиент субъекта своего соединения не заводит (kacho#3284): удостоверение
// ребра несёт соединение `iamInternal` карты бэкендов корня
// (`buildBackendDialCreds`, mtls_config_test.go). Здесь — шов: соединения нет —
// отказ сборки, а не клиент, отвечающий отказом на каждом входе.
func TestIAMSubjectClient_RefusesAnAbsentConnection(t *testing.T) {
	logger := slog.Default()

	_, err := clients.NewIAMSubjectClient(nil, logger)
	require.Error(t, err)

	empty, err := grpc.NewClient("", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = empty.Close() })
	_, err = clients.NewIAMSubjectClient(empty, logger)
	require.Error(t, err, "соединение без адреса принято")

	conn, err := grpc.NewClient("iam.kacho.svc:9091", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	c, err := clients.NewIAMSubjectClient(conn, logger)
	require.NoError(t, err)
	require.NotNil(t, c)
}

// IAMAuthorizeClientConfig несёт внедряемое удостоверение транспорта: nil —
// незащищённое умолчание, не nil — применяется (mTLS в боевой посадке).
func TestSECE_IAMAuthorizeClient_AcceptsTransportCreds(t *testing.T) {
	logger := slog.Default()

	opt := grpc.WithTransportCredentials(insecure.NewCredentials())
	c, err := clients.NewIAMAuthorizeClient(clients.IAMAuthorizeClientConfig{
		Addr:           "iam.kacho.svc:9090",
		Timeout:        200 * time.Millisecond,
		Logger:         logger,
		TransportCreds: opt,
	})
	require.NoError(t, err)
	require.NotNil(t, c)
	t.Cleanup(func() { _ = c.Close() })

	// nil TransportCreds → insecure default (backward-compat).
	c2, err := clients.NewIAMAuthorizeClient(clients.IAMAuthorizeClientConfig{
		Addr:    "iam.kacho.svc:9090",
		Timeout: 200 * time.Millisecond,
		Logger:  logger,
	})
	require.NoError(t, err)
	require.NotNil(t, c2)
	t.Cleanup(func() { _ = c2.Close() })
}
