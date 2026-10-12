// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// TestAccessServiceMethodPredicate — предикат адресата судит по пакету
// контракта службы доступа, а не по выписанному перечню: каждая служба этого
// пакета в реестре — адресат, служба любого другого пакета — нет. Перепись
// печатается: «ноль находок» отличим от «ноль осмотренных».
func TestAccessServiceMethodPredicate(t *testing.T) {
	access, other := 0, 0
	for _, svc := range registeredServices() {
		method := "/" + svc.fullName + "/" + svc.firstMethod
		want := svc.pkg == string(accessServicePackage)
		if got := isAccessServiceMethod(method); got != want {
			t.Errorf("%s: адресат «служба доступа» = %v, want %v", method, got, want)
		}
		if want {
			access++
		} else {
			other++
		}
	}
	t.Logf("перепись: служб пакета %s %d, прочих %d", accessServicePackage, access, other)
	if access == 0 || other == 0 {
		t.Fatalf("перепись вырождена (служб доступа %d, прочих %d) — предикату не на чем различать", access, other)
	}
	// Якоря вне предиката: имена, а не вывод из того же пакета.
	if !isAccessServiceMethod("/kaname.cloud.iam.v1.AccessKeyService/Revoke") {
		t.Error("снятие ключа доступа не признано вызовом службы доступа — номер записи до неё не доедет")
	}
	if isAccessServiceMethod("/kacho.cloud.vpc.v1.NetworkService/Delete") {
		t.Error("служба сети признана службой доступа")
	}
	if isAccessServiceMethod("/corelib.operation.OperationService/Get") {
		t.Error("служба операций признана службой доступа — номер уехал бы владельцу операции")
	}
	for _, m := range []string{"", "/", "kaname.cloud.iam.v1.AccessKeyService/Revoke",
		"/kaname.cloud.iam.v1.NoSuchService/Get", "/kaname.cloud.iam.v1.AccessKeyService"} {
		if isAccessServiceMethod(m) {
			t.Errorf("непредставимый адресат %q признан службой доступа — по умолчанию номер не уходит", m)
		}
	}
}

type registeredService struct {
	fullName, pkg, firstMethod string
}

// registeredServices — все службы с методами в реестре контрактов процесса.
func registeredServices() []registeredService {
	var out []registeredService
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := 0; i < fd.Services().Len(); i++ {
			sd := fd.Services().Get(i)
			if sd.Methods().Len() == 0 {
				continue
			}
			out = append(out, registeredService{
				fullName: string(sd.FullName()), pkg: string(fd.Package()),
				firstMethod: string(sd.Methods().Get(0).Name()),
			})
		}
		return true
	})
	return out
}
