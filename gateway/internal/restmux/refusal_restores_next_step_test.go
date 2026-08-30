// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// refusal_restores_next_step_test.go — отказ края обязан ВОССТАНАВЛИВАТЬ
// СЛЕДУЮЩИЙ ШАГ отправителя, а не только сообщать, что шаг не тот.
//
// Здесь два предмета. Они живут в одном файле, потому что оба судят один и тот
// же обход тела против дескриптора (`walkEnumValueNames`), и разводить их
// значило бы завести два места об одном механизме.
//
// ПРЕДМЕТ 1 (kacho#1622) — ПЕРЕЧЕНЬ ДОПУСТИМЫХ ЗНАЧЕНИЙ. Форму значения
// перечисления в этом дереве нельзя узнать заранее: из 74 перечислений 68
// пишутся без префикса типа (`ZONAL`, `PROJECT`), 6 — с полным префиксом
// (`SUBJECT_TYPE_USER`), и обе формы встречаются В ОДНОМ теле запроса. Машинного
// описания API в дереве нет вовсе, поэтому единственный источник формы —
// отказ; а он называл отвергнутое значение и молчал о допустимых, то есть
// сообщал «не то» и не сообщал «а что тогда». Отправителю оставался перебор.
//
// ПРЕДМЕТ 2 (kacho#1628) — СНЯТОЕ ПОЛЕ ОТВЕРГАЕТСЯ, А НЕ ГЛОТАЕТСЯ. Контракт
// обещает про снятый номер И имя: «запрос со старым blockSize отвергается как
// неизвестное поле — а не принимается молча, оставляя отправителя в уверенности,
// что размер блока задан им». На REST обещание не исполнялось: `DiscardUnknown`
// выбрасывает ключ без следа.
//
// ГРАНИЦА ПРАВКИ 2 — РОВНО ОДНА, И ОНА УЖЕ, ЧЕМ «НЕИЗВЕСТНЫЙ КЛЮЧ». Отвергается
// только имя, которое сообщение объявило `reserved`, — то есть поле, КОГДА-ТО
// СУЩЕСТВОВАВШЕЕ и снятое осознанно. Именно на нём молчание вредно: отправитель
// помнит поле рабочим. Ключ, которому в сообщении не соответствует ничего и
// никогда не соответствовало (опечатка, поле будущей версии), отбрасывается как
// прежде — на этом стоит клауза маски обновления и диагностика immutable-полей.
// Перепутать «неизвестное» со «снятым» и есть способ сломать конвенцию под видом
// строгости, поэтому граница проверяется здесь наравне с самим отказом.

import (
	"strings"
	"testing"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
)

// TestEnumRefusalNamesTheAllowedValues — отказ по значению перечисления обязан
// назвать, что писать вместо отвергнутого.
func TestEnumRefusalNamesTheAllowedValues(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req lbv1.CreateNetworkLoadBalancerRequest
	err := m.Unmarshal([]byte(`{"projectId":"prj-1","sessionAffinity":"DOES_NOT_EXIST"}`), &req)
	if err == nil {
		t.Fatal("неизвестное значение перечисления принято молча")
	}

	// Перечень выводится из ДЕСКРИПТОРА того же поля, а не выписывается здесь:
	// выписанное ожидание разошлось бы с контрактом молча.
	fd := req.ProtoReflect().Descriptor().Fields().ByJSONName("sessionAffinity")
	if fd == nil {
		t.Fatal("предпосылка пробы не выполнена: у контракта нет поля sessionAffinity")
	}
	vals := fd.Enum().Values()
	if vals.Len() == 0 {
		t.Fatal("предпосылка пробы не выполнена: перечисление пусто")
	}
	for i := 0; i < vals.Len(); i++ {
		name := string(vals.Get(i).Name())
		if !strings.Contains(err.Error(), name) {
			t.Errorf("отказ не называет допустимое значение %q — отправителю остаётся перебор: %v", name, err)
		}
	}
}

// TestEnumRefusalStillNamesFieldAndRejectedValue — прежние два утверждения
// отказа не размываются перечнем.
//
// Положительный контроль к пробе выше: без него «отказ стал длиннее» было бы
// неотличимо от «отказ перестал называть то, что называл».
func TestEnumRefusalStillNamesFieldAndRejectedValue(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req lbv1.CreateNetworkLoadBalancerRequest
	err := m.Unmarshal([]byte(`{"sessionAffinity":"DOES_NOT_EXIST"}`), &req)
	if err == nil {
		t.Fatal("неизвестное значение перечисления принято молча")
	}
	if !strings.Contains(err.Error(), "sessionAffinity") {
		t.Errorf("в отказе нет имени поля: %v", err)
	}
	if !strings.Contains(err.Error(), "DOES_NOT_EXIST") {
		t.Errorf("в отказе нет отвергнутого значения: %v", err)
	}
}

// TestRetiredFieldNameIsRejectedNotDropped — снятое поле в camelCase-форме
// (та, которой пишет REST-клиент).
func TestRetiredFieldNameIsRejectedNotDropped(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req storagev1.CreateVolumeRequest
	err := m.Unmarshal([]byte(`{"projectId":"prj-1","sizeBytes":"1073741824","blockSize":4096}`), &req)
	if err == nil {
		t.Fatal("снятое поле принято молча: отправитель уверен, что размер блока задан им, " +
			"а сервер о нём не знает — ровно то, что контракт обещал не допускать")
	}
	if !strings.Contains(err.Error(), "blockSize") {
		t.Errorf("отказ не называет ключ, который надо убрать: %v", err)
	}
}

// TestRetiredFieldNameIsRejectedInProtoNaming — та же форма имени, что у прямого
// вызывающего по gRPC-именам полей; protojson принимает обе, значит и проверка
// обязана видеть обе.
func TestRetiredFieldNameIsRejectedInProtoNaming(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req storagev1.CreateVolumeRequest
	if err := m.Unmarshal([]byte(`{"project_id":"prj-1","block_size":4096}`), &req); err == nil {
		t.Fatal("снятое поле в proto-форме имени принято молча")
	}
}

// TestUnknownKeyThatWasNeverAFieldStaysDropped — ГРАНИЦА.
//
// Законный близнец: ключ, которого в сообщении нет и не было. Он обязан
// молчать — на этом стоят клауза пустой маски и контракт-тон диагностики
// immutable-полей. Без этой пробы «строгость» неотличима от поломки конвенции.
func TestUnknownKeyThatWasNeverAFieldStaysDropped(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req storagev1.CreateVolumeRequest
	if err := m.Unmarshal([]byte(`{"projectId":"prj-1","noSuchFieldEver":1}`), &req); err != nil {
		t.Fatalf("ключ, который полем никогда не был, обязан отбрасываться молча: %v", err)
	}
	if req.GetProjectId() != "prj-1" {
		t.Errorf("тело перестало доезжать до сервиса: projectId=%q", req.GetProjectId())
	}
}

// TestLiveFieldsStillAccepted — второй положительный контроль: обычное тело
// проходит целиком.
func TestLiveFieldsStillAccepted(t *testing.T) {
	m := newStrictEnumMarshaler(newPublicJSONPb())

	var req storagev1.CreateVolumeRequest
	if err := m.Unmarshal([]byte(`{"projectId":"prj-1","name":"v1","sizeBytes":"1073741824"}`), &req); err != nil {
		t.Fatalf("законное тело отвергнуто: %v", err)
	}
	if req.GetName() != "v1" || req.GetSizeBytes() != 1073741824 {
		t.Errorf("тело доехало не целиком: %+v", &req)
	}
}
