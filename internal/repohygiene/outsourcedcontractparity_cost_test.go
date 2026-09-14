// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourcedcontractparity_cost_test.go — ЦЕНА КЛАССА, измеренная поведением,
// а не выведенная из чтения.
//
// Гейт-сосед судит ПУТЬ пакета контракта. Почему расхождение пути стоит
// красного, здесь доказывается тем, что видит клиент: различие ровно ОДНОЙ
// строки — полного имени метода — превращает рабочий поток в `Unimplemented`,
// который край читает как `501 this owner does not serve the platform
// subscription verb`.
//
// Проба ОДНО-ФАКТНАЯ: сервер один и тот же, соединение одно и то же, глагол
// РЕАЛИЗОВАН. Меняется только строка имени. Положительный контроль обязателен —
// без него отрицание зеленело бы на сервере, который не отвечает ничего.
package repohygiene

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
)

// contractParityCostServer — владелец журнала, глагол у которого РЕАЛИЗОВАН.
//
// Голая заглушка не годится: она отвечает `Unimplemented` по построению, и
// положительный контроль стал бы вакуумным. Эта ошибка была допущена при первом
// прогоне и поймана именно контролем.
type contractParityCostServer struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
}

func (contractParityCostServer) Subscribe(
	*subscriptionv1.SubscriptionRequest,
	grpc.ServerStreamingServer[subscriptionv1.SubscriptionMessage],
) error {
	return nil
}

// TestContractNameMismatchCostsUnimplemented — цена расхождения имени.
func TestContractNameMismatchCostsUnimplemented(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	subscriptionv1.RegisterInternalSubscriptionServiceServer(srv, contractParityCostServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Действующее имя берётся У СТАБА. Выписанное разошлось бы с двоичным молча.
	current := "/" + subscriptionv1.InternalSubscriptionService_ServiceDesc.ServiceName + "/Subscribe"
	// Прежнее имя выводится из действующего заменой корня контракта: так проба
	// не несёт второго объявления действующего имени.
	previous := strings.Replace(current, "/corelib.subscription.", "/kacho.cloud.subscription.", 1)
	if previous == current {
		t.Fatalf("прежнее имя не выведено из действующего %q — проба не различает имена", current)
	}

	call := func(fullMethod string) codes.Code {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stream, err := conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, fullMethod)
		if err != nil {
			return status.Code(err)
		}
		if err := stream.SendMsg(&subscriptionv1.SubscriptionRequest{}); err != nil {
			return status.Code(err)
		}
		_ = stream.CloseSend()
		var out subscriptionv1.SubscriptionMessage
		return status.Code(stream.RecvMsg(&out))
	}

	if got := call(previous); got != codes.Unimplemented {
		t.Fatalf("имя ПРЕЖНЕГО контракта %s дало %s, ожидался Unimplemented — "+
			"проба не воспроизводит класс", previous, got)
	}
	t.Logf("имя прежнего контракта %s → Unimplemented (край читает это как 501)", previous)

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: на действующем имени того же сервера глагол
	// служится. Без него отрицание выше зеленело бы на сервере, отвечающем
	// Unimplemented всему подряд.
	if got := call(current); got == codes.Unimplemented {
		t.Fatalf("КОНТРОЛЬ ПРОВАЛЕН: действующее имя %s тоже дало Unimplemented — "+
			"сервер не служит глагол ни под каким именем", current)
	}
	t.Logf("КОНТРОЛЬ: имя действующего контракта %s служится", current)
}
