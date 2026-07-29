// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	opv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
)

// strict_enum_http_test.go — то же свойство, но НА ПРОВОДЕ: код ответа и код
// ошибки, а не значение поля Go-структуры.
//
// Зачем сверх юнитов рядом. Кейс регрессионной suite'ы утверждает `400` и
// `code: 3`, а не «маршаллер вернул ошибку». Между ними лежит сгенерированный
// хендлер (`status.Errorf(codes.InvalidArgument, "%v", err)`) и обработчик
// ошибок grpc-gateway; проверка, останавливающаяся на маршаллере, оставляет этот
// участок на веру — а ровно там ошибка разбора могла бы уехать другим кодом.
//
// Стенд не нужен: mux собирается с ЛОКАЛЬНЫМ (in-process) хендлером сервиса,
// который на этом пути вызывается через тот же
// `marshaler.NewDecoder(req.Body).Decode(&protoReq)`.

// lbStub — сервис, отвечающий на Create успехом. Он существует, чтобы отличить
// «край отверг тело» от «до сервиса не дошло»: если бы отказ приходил не с
// разбора, этот заглушечный успех был бы виден в ответе.
type lbStub struct {
	lbv1.UnimplementedNetworkLoadBalancerServiceServer
	called int
}

func (s *lbStub) Create(context.Context, *lbv1.CreateNetworkLoadBalancerRequest) (*opv1.Operation, error) {
	s.called++
	return &opv1.Operation{Id: "iop-stub", Done: false}, nil
}

func newStrictEnumHTTP(t *testing.T) (http.Handler, *lbStub) {
	t.Helper()
	stub := &lbStub{}
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, newStrictEnumMarshaler(newPublicJSONPb())),
	)
	if err := lbv1.RegisterNetworkLoadBalancerServiceHandlerServer(context.Background(), mux, stub); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	return mux, stub
}

func postLB(t *testing.T, h http.Handler, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/nlb/v1/networkLoadBalancers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return rec.Code, payload
}

func TestStrictEnumOverHTTPRejectsUnknownValue(t *testing.T) {
	h, stub := newStrictEnumHTTP(t)

	code, payload := postLB(t, h, `{"projectId":"prj-1","regionId":"ru-1","placement":"EXTERNAL_REGIONAL","sessionAffinity":"DOES_NOT_EXIST","name":"lb"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("ожидался 400, получен %d (%v)", code, payload)
	}
	if got, _ := payload["code"].(float64); int(got) != 3 {
		t.Errorf("ожидался grpc-код 3 (INVALID_ARGUMENT), получен %v в %v", payload["code"], payload)
	}
	if msg, _ := payload["message"].(string); !strings.Contains(msg, "sessionAffinity") {
		t.Errorf("в сообщении нет имени поля: %q", msg)
	}
	if stub.called != 0 {
		t.Errorf("запрос дошёл до сервиса %d раз — отказ обязан быть на краю", stub.called)
	}
}

func TestStrictEnumOverHTTPStillAcceptsLegitimateBody(t *testing.T) {
	// КОНТРОЛЬ: законное тело, где необязательное перечисление не задано, а
	// необязательные строки пусты, проходит и доезжает до сервиса.
	h, stub := newStrictEnumHTTP(t)

	code, payload := postLB(t, h, `{"projectId":"prj-1","regionId":"ru-1","placement":"EXTERNAL_REGIONAL","name":"lb","description":"","labels":{}}`)
	if code != http.StatusOK {
		t.Fatalf("законное тело отвергнуто: %d %v", code, payload)
	}
	if stub.called != 1 {
		t.Fatalf("сервис вызван %d раз, ожидался 1", stub.called)
	}
	if id, _ := payload["id"].(string); id != "iop-stub" {
		t.Errorf("ответ сервиса не доехал до клиента: %v", payload)
	}
}
