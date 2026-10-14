// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// kacho#2741, п. 1 DoD на пути ПЕРЕПРОСА открытых потоков.
//
// Адаптер уже различает «спрошен не тот слушатель» и «глагола нет у сборки»
// (unimplemented_diagnosis_test.go), но перепрос читал обе корзины одной
// проверкой признака «вопрос не предложен» — и дежурный получал одну подсказку
// («докатится раскатом») на два состояния с противоположным действием.
//
// Входы — настоящие отказы библиотеки на проводе через настоящий адаптер, а не
// подставные ошибки: иначе проба зеленела бы на перепросе, который различает
// ошибку, не производимую ни одним адаптером.
//
// Исход полосы — поток жив в обоих диагнозах — утверждается тоже: на пути
// запроса оба диагноза дают один ответ арендатору, и перепрос, закрывающий
// поток по одному из них, разошёлся бы с путём запроса (полосы одного
// механизма). Меняется ТОЛЬКО подсказка дежурному.

// oneStream — реестр из одного открытого потока с заданным удостоверением.
type oneStream struct {
	mu     sync.Mutex
	cred   principalmeta.Credential
	closed int
}

func (o *oneStream) OpenStreams() []subscriptionstream.OpenStream {
	return []subscriptionstream.OpenStream{{
		Subject:    "user:usr-00000000000002741",
		Credential: o.cred,
		Close: func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.closed++
		},
	}}
}

func (o *oneStream) CloseAll() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed++
	return 1
}

func (o *oneStream) closedCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closed
}

// sweepOnce — один перепрос потока с удостоверением cred через адаптер к
// слушателю, собранному register. Возвращает журнал и число закрытий.
func sweepOnce(t *testing.T, register func(*grpc.Server), cred principalmeta.Credential) (string, int) {
	t.Helper()
	ad := adapterTo(t, register)
	reg := &oneStream{cred: cred}
	var buf bytes.Buffer
	sw, err := streamrevocation.New(streamrevocation.Config{
		Streams:    reg,
		Authority:  ad,
		Interval:   time.Minute,
		StaleAfter: time.Hour,
		Logger:     slog.New(slog.NewTextHandler(&buf, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	sw.Sweep(context.Background())
	return buf.String(), reg.closedCount()
}

func TestSweeperTellsMisaddressedFromRolloutWindow(t *testing.T) {
	type lane struct {
		name    string
		cred    principalmeta.Credential
		wrong   func(*grpc.Server) // слушатель не служит вопросу вовсе
		skew    func(*grpc.Server) // служба есть, глагола у сборки нет
		misaddr string             // величина «не тот слушатель» этой полосы
		rollout string             // величина «окно раската» этой полосы
	}
	lanes := []lane{
		{
			name:  "liveness",
			cred:  principalmeta.Credential{BasicCredentialID: "bas-00000000000002741"},
			wrong: func(*grpc.Server) {},
			skew: func(s *grpc.Server) {
				iamv1.RegisterInternalIAMServiceServer(s, bareServices{})
			},
			misaddr: "streams_liveness_misaddressed=1",
			rollout: "streams_liveness_unsupported=1",
		},
		{
			name: "cutoff",
			cred: principalmeta.Credential{
				UserID:    "usr-00000000000002741",
				Presented: principalmeta.PresentedSession("s-2741"),
			},
			wrong: func(s *grpc.Server) {
				iamv1.RegisterInternalHumanSessionServiceServer(s, humanOnly{})
			},
			skew: func(s *grpc.Server) {
				iamv1.RegisterInternalHumanSessionServiceServer(s, humanOnly{})
				iamv1.RegisterInternalSessionRevocationsServiceServer(s, bareServices{})
			},
			misaddr: "streams_cutoff_misaddressed=1",
			rollout: "streams_cutoff_unsupported=1",
		},
	}
	const fixAddress = "исправьте адрес"
	const imageSkew = "image skew"
	for _, l := range lanes {
		t.Run(l.name, func(t *testing.T) {
			wlog, wclosed := sweepOnce(t, l.wrong, l.cred)
			skewLog, sclosed := sweepOnce(t, l.skew, l.cred)

			// Законный близнец: окно раската называется раскатом, как и прежде.
			if !strings.Contains(skewLog, l.rollout) || !strings.Contains(skewLog, imageSkew) {
				t.Errorf("окно раската не названо раскатом:\n%s", skewLog)
			}
			if strings.Contains(skewLog, l.misaddr+" ") || strings.Contains(skewLog, fixAddress) {
				t.Errorf("окно раската подсказывает дежурному исправить адрес:\n%s", skewLog)
			}

			// Предмет: не тот слушатель называется настройкой, а не раскатом.
			if !strings.Contains(wlog, l.misaddr) || !strings.Contains(wlog, fixAddress) {
				t.Errorf("«спрошен не тот слушатель» не назван настройкой адреса:\n%s", wlog)
			}
			if strings.Contains(wlog, imageSkew) || strings.Contains(wlog, l.rollout) {
				t.Errorf("«спрошен не тот слушатель» назван окном раската — подсказка ведёт к раскату, который его не лечит:\n%s", wlog)
			}

			// Исход полосы от диагноза не зависит: поток жив в обоих.
			if wclosed != 0 || sclosed != 0 {
				t.Errorf("исход полосы зависит от диагноза: закрытий не тот слушатель %d · раскат %d", wclosed, sclosed)
			}
		})
	}
}
