// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readbudget_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что анализатор бюджета
// чтений способен упасть и способен смолчать.
//
// Гейт, который никогда не видел своего предмета, неотличим от гейта, смотрящего
// не туда: на чистом дереве оба молчат. Поэтому предмет подаётся сюда НАСТОЯЩИМ
// входом — синтетическим контрактом, собранным дескрипторами, а не строкой, — и
// рядом с ним стоит ЗАКОННЫЙ БЛИЗНЕЦ той же формы, на котором анализатор обязан
// молчать. Без близнеца проверка ловила бы форму, а не существо, и первый же
// ложный срабат её отключил бы.
package repohygiene

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// sliceRanger — источник дескрипторов из среза. Настоящий `protoregistry.Files`
// сюда не годится: инъекция обязана подать вход, которого в дереве НЕТ.
type sliceRanger []protoreflect.FileDescriptor

func (s sliceRanger) RangeFiles(f func(protoreflect.FileDescriptor) bool) {
	for _, fd := range s {
		if !f(fd) {
			return
		}
	}
}

// classifyByKachoConvention — та же конвенция, что у ограничителя допуска:
// `Get`/`List` — чтение, всё остальное — мутация.
func classifyByKachoConvention(fullMethod string) CallClass {
	name := fullMethod
	if i := strings.LastIndex(fullMethod, "/"); i >= 0 {
		name = fullMethod[i+1:]
	}
	if strings.HasPrefix(name, "Get") || strings.HasPrefix(name, "List") {
		return ClassRead
	}
	return ClassMutation
}

// syntheticContract — контракт с ТРЕМЯ методами, покрывающими обе стороны:
//
//	GetThing    → возвращает конверт мутации, назван по-читательски — НАХОДКА;
//	CreateThing → возвращает конверт мутации, назван по конвенции   — молчание;
//	GetReal     → чтение, возвращает свой ресурс                    — молчание.
func syntheticContract(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	msg := func(n string) *descriptorpb.DescriptorProto {
		return &descriptorpb.DescriptorProto{Name: proto.String(n)}
	}
	method := func(n, in, out string) *descriptorpb.MethodDescriptorProto {
		return &descriptorpb.MethodDescriptorProto{
			Name: proto.String(n), InputType: proto.String(in), OutputType: proto.String(out),
		}
	}
	const pkg = "kacho.cloud.synthetic.v1"
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("kacho/cloud/synthetic/v1/thing.proto"),
		Package: proto.String(pkg),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			msg("Operation"), msg("Req"), msg("Thing"),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("ThingService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("GetThing", "."+pkg+".Req", "."+pkg+".Operation"),
				method("CreateThing", "."+pkg+".Req", "."+pkg+".Operation"),
				method("GetReal", "."+pkg+".Req", "."+pkg+".Thing"),
			},
		}},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	require.NoError(t, err)
	return fd
}

func syntheticOptions(t *testing.T) ReadBudgetOptions {
	t.Helper()
	return ReadBudgetOptions{
		Files:            sliceRanger{syntheticContract(t)},
		Classify:         classifyByKachoConvention,
		OperationMessage: "kacho.cloud.synthetic.v1.Operation",
		DeclaredPackages: []string{"kacho.cloud.synthetic.v1"},
	}
}

// TestReadBudget_SeesAMutationWearingAReaderName — ОПАСНАЯ сторона: анализатор
// обязан покраснеть и назвать координату.
func TestReadBudget_SeesAMutationWearingAReaderName(t *testing.T) {
	findings, census, err := AuditReadBudgetClassification(syntheticOptions(t), io.Discard)
	require.NoError(t, err, "предпосылки синтетического контракта обязаны выполняться")

	require.Equal(t, 3, census.Methods)
	require.Equal(t, 2, census.OperationReturning)
	require.Equal(t, 2, census.ReadNamed, "GetThing и GetReal названы по-читательски")

	require.Len(t, findings, 1, "находка обязана быть ровно одна: GetThing")
	require.Equal(t, KindMutationBuysReadBudget, findings[0].Kind)
	require.Equal(t, "/kacho.cloud.synthetic.v1.ThingService/GetThing", findings[0].Method,
		"находка обязана НАЗЫВАТЬ координату: без неё вердикт нечем исполнить")
}

// TestReadBudget_StaysSilentOnTheLegalTwin — ЗАКОННЫЙ БЛИЗНЕЦ той же формы.
//
// `CreateThing` возвращает тот же конверт и остаётся мутацией; `GetReal` назван
// по-читательски и чтением является. Ни один не находка. Без этой пробы гейт
// ловил бы форму («метод возвращает Operation»), а не существо.
func TestReadBudget_StaysSilentOnTheLegalTwin(t *testing.T) {
	opts := syntheticOptions(t)
	// Оставляем в контракте только законные формы — тот же вход, снятый предмет.
	opts.Files = sliceRanger{syntheticContractWithout(t, "GetThing")}

	findings, census, err := AuditReadBudgetClassification(opts, io.Discard)
	require.NoError(t, err)
	require.Equal(t, 2, census.Methods)
	require.Equal(t, 1, census.OperationReturning, "дискриминатор обязан сохранить предмет — иначе молчание даровое")
	require.Empty(t, findings, "законные формы не являются находкой")
}

func syntheticContractWithout(t *testing.T, drop string) protoreflect.FileDescriptor {
	t.Helper()
	fd := syntheticContract(t)
	fdp := protodesc.ToFileDescriptorProto(fd)
	kept := fdp.Service[0].Method[:0]
	for _, m := range fdp.Service[0].Method {
		if m.GetName() != drop {
			kept = append(kept, m)
		}
	}
	fdp.Service[0].Method = kept
	out, err := protodesc.NewFile(fdp, nil)
	require.NoError(t, err)
	return out
}

// TestReadBudget_RefusesWhenItsPremiseFails — гейт обязан ОТКАЗАТЬ, а не
// смолчать, когда его собственная предпосылка не выполнена. Четыре формы отказа,
// и каждая — та, при которой молчание было бы даровым.
func TestReadBudget_RefusesWhenItsPremiseFails(t *testing.T) {
	t.Run("имя конверта разошлось с деревом", func(t *testing.T) {
		opts := syntheticOptions(t)
		opts.OperationMessage = "kacho.cloud.operation.v1.Operation" // опечатка вида «лишний v1»
		_, _, err := AuditReadBudgetClassification(opts, io.Discard)
		require.ErrorContains(t, err, "дискриминатор мутации")
	})
	t.Run("объявленный деревом пакет не виден реестру", func(t *testing.T) {
		opts := syntheticOptions(t)
		opts.DeclaredPackages = append(opts.DeclaredPackages, "kacho.cloud.absent.v1")
		_, _, err := AuditReadBudgetClassification(opts, io.Discard)
		require.ErrorContains(t, err, "kacho.cloud.absent.v1")
		require.ErrorContains(t, err, "пустого импорта")
	})
	t.Run("обход смотрит не туда", func(t *testing.T) {
		opts := syntheticOptions(t)
		opts.Files = sliceRanger{}
		_, _, err := AuditReadBudgetClassification(opts, io.Discard)
		require.Error(t, err, "ноль осмотренных методов — отказ, а не чистота")
	})
	t.Run("у исключения не записана причина", func(t *testing.T) {
		opts := syntheticOptions(t)
		opts.Exempt = map[string]string{"kacho.cloud.synthetic.v1": "  "}
		_, _, err := AuditReadBudgetClassification(opts, io.Discard)
		require.ErrorContains(t, err, "не записана причина")
	})
}

// TestReadBudget_ExemptionExpiresOnItsOwn — исключение живёт, пока у него есть
// предмет. Записи, которой больше нечего исключать, полагается быть находкой:
// иначе она унаследует следующую слепую зону.
func TestReadBudget_ExemptionExpiresOnItsOwn(t *testing.T) {
	opts := syntheticOptions(t)
	opts.Files = sliceRanger{syntheticContractWithout(t, "GetThing")}
	opts.Exempt = map[string]string{"kacho.cloud.synthetic.v1": "предмета больше нет"}

	findings, _, err := AuditReadBudgetClassification(opts, io.Discard)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, KindStaleExemption, findings[0].Kind)
	require.Equal(t, "kacho.cloud.synthetic.v1", findings[0].Package)

	// Обратная сторона: пока предмет есть — исключение молчит.
	opts.Files = sliceRanger{syntheticContract(t)}
	findings, _, err = AuditReadBudgetClassification(opts, io.Discard)
	require.NoError(t, err)
	require.Empty(t, findings, "у исключения есть предмет — оно не истекло")
}
