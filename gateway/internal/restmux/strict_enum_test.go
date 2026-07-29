// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"errors"
	"io"
	"strings"
	"testing"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// strict_enum_test.go — край обязан ОТВЕРГАТЬ значение перечисления, которого в
// контракте нет, и обязан по-прежнему принимать всё остальное, что принимал.
//
// Предмет. Разбор тела шёл с отбрасыванием неизвестного, а protojson трактует
// «неизвестное» шире, чем ключ тела: под отбрасывание попадало и НЕИЗВЕСТНОЕ
// ИМЯ ЗНАЧЕНИЯ перечисления (encoding/protojson/decode.go: unmarshalEnum →
// `if discardUnknown { return Value{}, true }`). Поле оставалось нулевым, и
// сервис не мог отличить мусор от отсутствия: балансировщик создавался с
// умолчанием, а вызывающему отвечали успехом за настройку, которой сервер не
// делал.
//
// Границы правки — ровно одна: имя значения перечисления. Всё, на чём стоит
// конвенция маски обновления (ключ тела, которого нет в message запроса,
// отбрасывается молча), остаётся как было — и это здесь проверяется наравне с
// самим отказом, потому что перепутать эти два «неизвестных» и есть способ
// сломать конвенцию под видом строгости.

func TestStrictEnumRejectsUnknownEnumValueName(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req lbv1.CreateNetworkLoadBalancerRequest
	err := m.Unmarshal([]byte(`{"projectId":"prj-1","sessionAffinity":"DOES_NOT_EXIST"}`), &req)
	if err == nil {
		t.Fatalf("неизвестное значение перечисления принято молча: sessionAffinity=%v — "+
			"вызывающий получит успех за настройку, которой сервер не делал", req.GetSessionAffinity())
	}
	if !strings.Contains(err.Error(), "sessionAffinity") {
		t.Errorf("в отказе нет имени поля, по которому его чинить: %v", err)
	}
	if !strings.Contains(err.Error(), "DOES_NOT_EXIST") {
		t.Errorf("в отказе нет отвергнутого значения: %v", err)
	}
}

func TestStrictEnumRejectsViaDecoder(t *testing.T) {
	// Сгенерированные хендлеры края разбирают тело именно через NewDecoder
	// (`marshaler.NewDecoder(req.Body).Decode(&protoReq)`), а не через Unmarshal.
	// Проверка только Unmarshal оставила бы боевой путь непокрытым.
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req lbv1.CreateNetworkLoadBalancerRequest
	err := m.NewDecoder(strings.NewReader(`{"sessionAffinity":"NOPE"}`)).Decode(&req)
	if err == nil {
		t.Fatal("decoder принял неизвестное значение перечисления")
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("отказ пришёл как io.EOF — сгенерированный хендлер трактует его как пустое тело: %v", err)
	}
}

func TestStrictEnumAcceptsWhatItAcceptedBefore(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	cases := []struct {
		name string
		body string
		// check — что именно обязано доехать до сервиса.
		check func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest)
	}{
		{
			// КОНТРОЛЬ приёмки: законный запрос, где необязательное поле не
			// задано вовсе. Строгость не смеет его тронуть — иначе она чинит
			// один класс и ломает штатный путь.
			name: "необязательное перечисление отсутствует",
			body: `{"projectId":"prj-1","name":"lb"}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if got := req.GetSessionAffinity(); got != lbv1.NetworkLoadBalancer_SESSION_AFFINITY_UNSPECIFIED {
					t.Errorf("отсутствующее поле не осталось нулевым: %v", got)
				}
			},
		},
		{
			name: "необязательные строка/карта пусты",
			body: `{"projectId":"prj-1","name":"","description":"","labels":{}}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if req.GetProjectId() != "prj-1" {
					t.Errorf("projectId потерян: %q", req.GetProjectId())
				}
			},
		},
		{
			// КОНТРОЛЬ приёмки: пустая строка в поле перечисления — «не
			// выбрано», а не «значение, которого нет». Формы кладут туда именно
			// это; отвергать её значило бы починить один класс и сломать
			// штатный путь оператора.
			name: "перечисление пустой строкой = не задано",
			body: `{"projectId":"prj-1","sessionAffinity":""}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if got := req.GetSessionAffinity(); got != lbv1.NetworkLoadBalancer_SESSION_AFFINITY_UNSPECIFIED {
					t.Errorf("пустая строка не прочиталась как «не задано»: %v", got)
				}
			},
		},
		{
			name: "известное значение перечисления",
			body: `{"projectId":"prj-1","sessionAffinity":"CLIENT_IP_ONLY"}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if got := req.GetSessionAffinity(); got != lbv1.NetworkLoadBalancer_CLIENT_IP_ONLY {
					t.Errorf("значение не доехало: %v", got)
				}
			},
		},
		{
			name: "нулевое значение перечисления по имени",
			body: `{"projectId":"prj-1","sessionAffinity":"SESSION_AFFINITY_UNSPECIFIED"}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if got := req.GetSessionAffinity(); got != lbv1.NetworkLoadBalancer_SESSION_AFFINITY_UNSPECIFIED {
					t.Errorf("нулевое имя не принято: %v", got)
				}
			},
		},
		{
			// Числовая форма — часть JSON-отображения protobuf (перечисления
			// открыты, номер вне словаря законен на wire). Сужать её здесь
			// значило бы менять контракт, а не чинить дефект.
			name: "перечисление числом",
			body: `{"projectId":"prj-1","sessionAffinity":1}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if got := req.GetSessionAffinity(); got != lbv1.NetworkLoadBalancer_SessionAffinity(1) {
					t.Errorf("числовая форма не принята: %v", got)
				}
			},
		},
		{
			// Конвенция маски обновления стоит именно на этом: ключ, которому в
			// message запроса нет поля, отбрасывается молча.
			name: "неизвестный КЛЮЧ тела по-прежнему отбрасывается",
			body: `{"projectId":"prj-1","id":"nlb-x","createdAt":"2026-07-29T00:00:00Z","bogus":{"deep":1}}`,
			check: func(t *testing.T, req *lbv1.CreateNetworkLoadBalancerRequest) {
				if req.GetProjectId() != "prj-1" {
					t.Errorf("projectId потерян: %q", req.GetProjectId())
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req lbv1.CreateNetworkLoadBalancerRequest
			if err := m.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("законное тело отвергнуто: %v", err)
			}
			tc.check(t, &req)
		})
	}
}

func TestStrictEnumWalksIntoRepeatedNestedMessages(t *testing.T) {
	// Перечисление редко лежит на верхнем уровне запроса: чаще оно приходит
	// внутри элемента списка вложенных сообщений. Проверка, умеющая только
	// верхний уровень, зелена и бесполезна ровно там, где сложнее всего
	// заметить проглоченное значение.
	//
	// Живой предмет: CreateSecurityGroupRequest.rule_specs[] →
	// SecurityGroupRuleSpec.direction (SecurityGroupRule.Direction).
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req vpcv1.CreateSecurityGroupRequest
	bad := `{"projectId":"prj-1","networkId":"net-1","ruleSpecs":[{"direction":"SIDEWAYS","cidrBlocks":{}}]}`
	err := m.Unmarshal([]byte(bad), &req)
	if err == nil {
		t.Fatalf("значение вне словаря внутри элемента списка принято молча: direction=%v",
			req.GetRuleSpecs()[0].GetDirection())
	}
	if !strings.Contains(err.Error(), "ruleSpecs[0].direction") {
		t.Errorf("в отказе нет пути до поля внутри списка: %v", err)
	}

	var ok vpcv1.CreateSecurityGroupRequest
	good := `{"projectId":"prj-1","networkId":"net-1","ruleSpecs":[{"direction":"INGRESS","cidrBlocks":{}}]}`
	if err := m.Unmarshal([]byte(good), &ok); err != nil {
		t.Fatalf("законное значение внутри списка отвергнуто: %v", err)
	}
	if ok.GetRuleSpecs()[0].GetDirection() != vpcv1.SecurityGroupRule_INGRESS {
		t.Errorf("значение внутри списка не доехало: %v", ok.GetRuleSpecs()[0].GetDirection())
	}
}

func TestStrictEnumWalksIntoMapValues(t *testing.T) {
	// Значение карты — единственная ветвь обхода, у которой в сегодняшнем
	// дереве .proto нет живого носителя перечисления. Ветвь всё равно нужна:
	// без неё день, когда такое поле появится, начнётся с молчаливого приёма —
	// того самого класса, который здесь и чинится. Поэтому она проверяется на
	// собранном на месте дескрипторе, а не оставляется «на веру».
	md := mapOfEnumDescriptor(t)
	msg := dynamicpb.NewMessage(md)
	m := newStrictEnumMarshaler(newPublicJSONPb())

	if err := m.Unmarshal([]byte(`{"colors":{"a":"MAUVE"}}`), msg); err == nil {
		t.Fatal("значение вне словаря в значении карты принято молча")
	} else if !strings.Contains(err.Error(), "colors[a]") {
		t.Errorf("в отказе нет координаты значения карты: %v", err)
	}

	fresh := dynamicpb.NewMessage(md)
	if err := m.Unmarshal([]byte(`{"colors":{"a":"RED"}}`), fresh); err != nil {
		t.Fatalf("законное значение карты отвергнуто: %v", err)
	}
}

// mapOfEnumDescriptor собирает сообщение `map<string, Color> colors = 1`.
func mapOfEnumDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("kacho/internal/strict_enum_probe.proto"),
		Package: proto.String("kacho.internal.strictenumprobe"),
		Syntax:  proto.String("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Color"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("COLOR_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("RED"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Probe"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     proto.String("colors"),
				JsonName: proto.String("colors"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".kacho.internal.strictenumprobe.Probe.ColorsEntry"),
			}},
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:    proto.String("ColorsEntry"),
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name: proto.String("key"), JsonName: proto.String("key"), Number: proto.Int32(1),
						Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name: proto.String("value"), JsonName: proto.String("value"), Number: proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".kacho.internal.strictenumprobe.Color"),
					},
				},
			}},
		}},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	if err != nil {
		t.Fatalf("дескриптор пробы не собрался: %v", err)
	}
	return fd.Messages().Get(0)
}

func TestStrictEnumLeavesArbitraryJSONAlone(t *testing.T) {
	// Well-known-типы (`google.protobuf.Struct`/`Value`/`Any`) принимают
	// произвольные ключи и значения by design. Спуск в них дал бы отказ на
	// корректном теле — это тот самый способ «сломать штатный путь строгостью».
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req lbv1.CreateNetworkLoadBalancerRequest
	body := `{"projectId":"prj-1","labels":{"k":"DOES_NOT_EXIST"}}`
	if err := m.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("строковое значение карты принято за имя перечисления: %v", err)
	}
	if req.GetLabels()["k"] != "DOES_NOT_EXIST" {
		t.Errorf("значение метки потеряно: %v", req.GetLabels())
	}
}

func TestStrictEnumEmptyBodyStillReadsAsEOF(t *testing.T) {
	// Сгенерированный хендлер отличает «тела нет» от «тело плохое» ровно по
	// io.EOF (`err != nil && !errors.Is(err, io.EOF)`). Обёртка, потерявшая
	// io.EOF, превратила бы каждый запрос без тела в 400.
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req lbv1.CreateNetworkLoadBalancerRequest
	err := m.NewDecoder(strings.NewReader("")).Decode(&req)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("пустое тело обязано читаться как io.EOF, получено: %v", err)
	}
}
