// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// admission_classification_test.go — гейт на КЛАСС: мутация, купившая
// читательский бюджет.
//
// # Предмет
//
// Ограничитель допуска относит вызов к чтениям по префиксу имени (`Get`/`List` —
// конвенция продукта), а всё остальное считает мутацией. У полярности есть ровно
// одна ОПАСНАЯ сторона: метод, который на самом деле мутирует, но назван по-
// читательски, получит читательский бюджет — впятеро более щедрый и втрое более
// дорогой за запрос (три строки в базе против одной страницы чтения).
//
// Обратная сторона (настоящее чтение, названное не по конвенции, получает бюджет
// мутации) дыры не открывает: она сужает, а не расширяет. Поэтому здесь
// утверждается только опасное направление, и утверждается МЕХАНИЧЕСКИ.
//
// # Дискриминатор — контракт, а не список имён
//
// «Мутация» в этом продукте имеет машинно-проверяемый признак: мутации
// асинхронны и возвращают `Operation` (правило #9). Поэтому гейт не выписывает
// перечень методов — он обходит дескрипторы, СЛИНКОВАННЫЕ В ЭТОТ БИНАРЬ, и
// требует: метод, возвращающий `Operation`, не может быть отнесён к чтениям.
// Выписанный перечень отстал бы от следующего RPC — то есть ровно от того, ради
// которого гейт и нужен, — и вдобавок краснел бы у соседа, добавляющего свой
// ресурс.
//
// # Предпосылка проверяется
//
// Ноль осмотренных методов — НАХОДКА, а не «всё чисто»: значит обход смотрит не
// туда, и молчание гейта ничего не доказывает. Перепись печатается всегда.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// vpcProtoPackage — пакет контрактов домена. Обход ограничен им: чужие
// дескрипторы, попавшие в реестр транзитивно, к бюджету vpc отношения не имеют.
const vpcProtoPackage = "kacho.cloud.vpc.v1"

// operationMessage — конверт асинхронной мутации.
//
// Имя выписано, и это осознанно: оно проверено обходом дерева, а не взято по
// памяти — в первой редакции здесь стояло `kacho.cloud.operation.v1.Operation`,
// и дискриминатор молча не находил НИ ОДНОЙ мутации, то есть гейт был зелёным на
// всём. Отсюда же требование к переписи ниже: она обязана назвать число методов,
// ВОЗВРАЩАЮЩИХ этот конверт, иначе опечатка в имени снова станет невидимой.
const operationMessage = "kacho.cloud.operation.Operation"

// TestNoMutationBuysTheReadBudget — ядро гейта.
func TestNoMutationBuysTheReadBudget(t *testing.T) {
	var methods, operationReturning, readNamed int
	var violations []string

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != vpcProtoPackage {
			return true
		}
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				m := svc.Methods().Get(j)
				methods++
				full := "/" + string(svc.FullName()) + "/" + string(m.Name())
				class := grpcsrv.ClassifyByKachoConvention(full)
				if class == grpcsrv.ClassRead {
					readNamed++
				}
				if string(m.Output().FullName()) != operationMessage {
					continue
				}
				operationReturning++
				if class == grpcsrv.ClassRead {
					violations = append(violations, full)
				}
			}
		}
		return true
	})

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("перепись: методов %d, возвращают Operation %d, отнесены к чтениям %d",
		methods, operationReturning, readNamed)
	require.NotZero(t, methods,
		"обход не нашёл НИ ОДНОГО метода домена %s: гейт смотрит не туда, и его молчание "+
			"ничего не доказывает", vpcProtoPackage)
	require.NotZero(t, operationReturning,
		"ни один метод не возвращает %s: дискриминатор мутации не нашёл своего предмета "+
			"(имя конверта разошлось с деревом), поэтому гейт зелёный на всём", operationMessage)
	require.NotZero(t, readNamed,
		"ни один метод не отнесён к чтениям: классификатор отвечает одинаково на всё, "+
			"и опасное направление проверять не на чем")

	require.Empty(t, violations,
		"метод(ы) возвращают %s (то есть мутируют по правилу #9), но названы по-читательски "+
			"и потому купят ЧИТАТЕЛЬСКИЙ бюджет — впятеро более щедрый при втрое большей "+
			"стоимости запроса: %s. Переименуйте по конвенции (мутация — не Get*/List*) либо "+
			"объясните классификатору эту форму явно",
		operationMessage, strings.Join(violations, ", "))
}
