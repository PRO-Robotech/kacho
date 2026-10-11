// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	iampb "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
)

// accessServicePackage — пакет контракта службы доступа. Берётся у
// дескриптора контракта, а не выписан строкой: это тот же пакет `iampb`, чьи
// службы мост регистрирует на адресах службы доступа (`iam`, `iamInternal`).
var accessServicePackage = iampb.File_kaname_cloud_iam_v1_access_key_proto.Package()

// isAccessServiceMethod — адресат вызова моста (полное имя метода gRPC,
// `/<пакет>.<служба>/<метод>`) — служба доступа.
//
// По нему мост решает, класть ли номер записи сессии (kacho#3136): номер
// читает только служба доступа, и читать его она вправе на любом своём методе —
// поэтому решение по пакету службы, а не по перечню методов. Имя, которое не
// разбирается или не названо в реестре контрактов, адресатом службы доступа не
// признаётся: по умолчанию номер не уходит.
func isAccessServiceMethod(method string) bool {
	name, ok := strings.CutPrefix(method, "/")
	if !ok {
		return false
	}
	i := strings.LastIndexByte(name, '/')
	if i <= 0 || i == len(name)-1 {
		return false
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name[:i]))
	if err != nil {
		return false
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok || sd.Methods().ByName(protoreflect.Name(name[i+1:])) == nil {
		return false
	}
	return sd.ParentFile().Package() == accessServicePackage
}
