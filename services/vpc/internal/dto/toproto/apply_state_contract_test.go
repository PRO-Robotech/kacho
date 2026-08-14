// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto_test

import (
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// apply_state_contract_test.go — состав и распространение публичного поля
// состояния применения (APPLY-10, APPLY-11, APPLY-15).
//
// # Почему состав проекции заперт ОТДЕЛЬНОЙ пробой по дескриптору
//
// Серверная проекция уже заперта пробой своего пакета
// (`TestPublicProjectionCarriesExactlyTwoFacts`), но она утверждает про
// Go-структуру. Контракт — отдельный артефакт, и расшириться он может независимо:
// поле, добавленное в `ApplyState` и не добавленное в Go-структуру, серверную
// пробу не тронет вовсе. Инфра-чувствительное приезжает в такие проекции по
// одному полю за раз, и каждое по отдельности выглядит безобидной подробностью:
// сначала ревизия «для отладки», потом время отчёта «чтобы понять, свежий ли»,
// потом идентификатор попытки. Ревизия при этом — счётчик мутаций ВСЕГО домена
// (последовательность одна на весь сервис), то есть её выдача арендатору
// сообщает ему темп изменений у всех остальных.
//
// # Почему перечень ресурсов выписан, а не выведен
//
// Вывести «публичные сообщения ресурсов» из пакета можно только по признаку
// «сообщение верхнего уровня», под который попадают и запросы, и ответы. Такой
// предикат шире предмета. Полнота обхода вместо этого проверяется положительным
// контролем: у каждого перечисленного сообщения обязано найтись поле `id`.

// applyStateBearers — публичные сообщения ресурсов, несущих состояние применения.
//
// Ровно те виды объектов, у которых есть строка намерения (семь триггеров
// миграции 0032). Восьмого нет и не может быть: ограничение
// `dataplane_intent_kind_known` отвергает вид вне этого перечня.
var applyStateBearers = []proto.Message{
	(*vpcv1.Network)(nil),
	(*vpcv1.Subnet)(nil),
	(*vpcv1.NetworkInterface)(nil),
	(*vpcv1.SecurityGroup)(nil),
	(*vpcv1.RouteTable)(nil),
	(*vpcv1.Gateway)(nil),
	(*vpcv1.Address)(nil),
}

// applyStateFieldName — имя поля на ресурсе.
//
// Имя обязано содержать `apply`: гейт пары «поле ↔ заполнитель»
// (`dataplane_apply_pairing_test.go`) опознаёт поле по этому вхождению, и поле,
// названное иначе, оставило бы его зелёным навсегда — то есть наблюдение
// пропало бы ровно в тот момент, когда впервые понадобилось.
const applyStateFieldName = protoreflect.Name("apply_state")

// TestApplyStateCarriesExactlyTwoFacts — APPLY-10: у контракта состояния ровно
// два поля, и это `applied` и `reason` в этом порядке.
func TestApplyStateCarriesExactlyTwoFacts(t *testing.T) {
	md := (*vpcv1.ApplyState)(nil).ProtoReflect().Descriptor()
	fields := md.Fields()

	var got []string
	for i := 0; i < fields.Len(); i++ {
		got = append(got, string(fields.Get(i).Name()))
	}
	want := []string{"applied", "reason"}

	if len(got) != len(want) {
		t.Fatalf("%s несёт поля %v, а обязан ровно %v.\n"+
			"Каждое лишнее поле здесь — утверждение о платформе, адресованное арендатору: "+
			"ревизия намерения есть счётчик мутаций всего домена, время отчёта датирует "+
			"активность исполнителя. Публично сообщаются ДВЕ вещи: применено ли намерение "+
			"текущей ревизии и, если нет, класс причины.", md.FullName(), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: поле %d называется %q, ожидалось %q (порядок — часть контракта)",
				md.FullName(), i+1, got[i], want[i])
		}
	}
	t.Logf("осмотрено %d поле(й) %s", fields.Len(), md.FullName())

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: обход читает дескриптор, а не пустоту.
	if fields.ByName("applied") == nil {
		t.Fatal("обход не нашёл поле applied — проверка читает не дескриптор")
	}
}

// TestApplyStateReusesTheSingleFailureDictionary — класс отказа берётся из ТОГО
// ЖЕ перечисления, что принимает отчёт исполнителя.
//
// Второй перечень разошёлся бы с первым молча и именно там, где расхождение не
// видно: класс, добавленный в публичный контракт и забытый в проверке приёма,
// отвергался бы у исполнителя; класс, забытый в публичном, не доезжал бы до
// арендатора.
func TestApplyStateReusesTheSingleFailureDictionary(t *testing.T) {
	pubReason := (*vpcv1.ApplyState)(nil).ProtoReflect().Descriptor().Fields().ByName("reason")
	if pubReason == nil {
		t.Fatal("у ApplyState нет поля reason")
	}
	if pubReason.Kind() != protoreflect.EnumKind {
		t.Fatalf("reason объявлен как %s — класс обязан быть перечислением, "+
			"иначе в него станет возможно положить имя узла или код ядра", pubReason.Kind())
	}
	inbound := (*vpcv1.ReportIntentAppliedRequest)(nil).ProtoReflect().Descriptor().Fields().ByName("reason")
	if inbound == nil {
		t.Fatal("у ReportIntentAppliedRequest нет поля reason — предпосылка гейта ложна")
	}
	if got, want := pubReason.Enum().FullName(), inbound.Enum().FullName(); got != want {
		t.Fatalf("публичный класс отказа — %s, принимаемый от исполнителя — %s: "+
			"словарей стало два, и разойдутся они молча", got, want)
	}
	t.Logf("словарь один: %s, значений %d",
		pubReason.Enum().FullName(), pubReason.Enum().Values().Len())
}

// TestEveryIntentBearingResourceCarriesApplyState — APPLY-11: поле стоит на всех
// семи ресурсах и НЕ стоит там, где намерения нет.
func TestEveryIntentBearingResourceCarriesApplyState(t *testing.T) {
	applyStateName := (*vpcv1.ApplyState)(nil).ProtoReflect().Descriptor().FullName()

	var missing, scanned []string
	seenID := 0
	for _, m := range applyStateBearers {
		md := m.ProtoReflect().Descriptor()
		scanned = append(scanned, string(md.Name()))
		if md.Fields().ByName("id") != nil {
			seenID++
		}
		fd := md.Fields().ByName(applyStateFieldName)
		switch {
		case fd == nil:
			missing = append(missing, string(md.FullName()))
		case fd.Kind() != protoreflect.MessageKind || fd.Message().FullName() != applyStateName:
			t.Errorf("%s.%s имеет тип, отличный от %s — форма поля есть часть контракта: "+
				"скалярное поле не умеет выразить «утверждения нет»",
				md.FullName(), applyStateFieldName, applyStateName)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("состояние применения отсутствует у %d ресурса(ов): %s.\n"+
			"Строка намерения есть у каждого из семи видов (триггеры миграции 0032), "+
			"значит арендатор каждого из них вправе узнать, доехало ли его изменение.",
			len(missing), strings.Join(missing, ", "))
	}
	if seenID != len(applyStateBearers) {
		t.Fatalf("обход нашёл поле id у %d сообщений из %d — проверка не читает то, "+
			"о чём отчитывается", seenID, len(applyStateBearers))
	}
	t.Logf("осмотрено %d сообщение(й): %s", len(scanned), strings.Join(scanned, ", "))

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: у ресурса без строки намерения поля нет и быть не
	// должно — оно было бы мёртвым, и заполнить его нечем.
	if fd := (*vpcv1.CidrGroup)(nil).ProtoReflect().Descriptor().
		Fields().ByName(applyStateFieldName); fd != nil {
		t.Fatalf("CidrGroup несёт %s, а строки намерения у него нет: поле мёртвое by construction",
			applyStateFieldName)
	}
}

// TestApplyStateIsNeverAcceptedOnInput — APPLY-15: состояние применения нельзя
// прислать НИ ОДНИМ запросом.
//
// # Почему дескриптор, а не сквозная проба
//
// Сквозная половина этого сценария неконструируема: запросы создания и
// обновления ПЛОСКИЕ — они не вкладывают сообщение ресурса, — поэтому ключ
// `applyState` в теле есть неизвестное поле, и край отвергает такое тело сам, до
// сервиса. То есть проба утверждала бы отказ разбора JSON, а не отсутствие поля
// в контракте, и осталась бы зелёной, даже если бы поле в запрос вернули.
//
// Здесь утверждение прямое: НИ ОДИН запрос vpc не объявляет поля состояния
// применения. Обход идёт по ВСЕМ сообщениям запросов пакета, а не по семи
// выписанным: перепись по механизму, а не по диффу.
func TestApplyStateIsNeverAcceptedOnInput(t *testing.T) {
	applyStateName := (*vpcv1.ApplyState)(nil).ProtoReflect().Descriptor().FullName()
	pkg := (*vpcv1.Network)(nil).ProtoReflect().Descriptor().ParentFile().Package()

	var offenders []string
	scannedRequests, scannedFields := 0, 0
	seenControl := false

	forEachVPCMessage(t, func(md protoreflect.MessageDescriptor) {
		if !strings.HasSuffix(string(md.Name()), "Request") {
			return
		}
		scannedRequests++
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			scannedFields++
			// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ обхода: приём отчёта — единственный вход, у
			// которого класс отказа стоять ОБЯЗАН. Он же доказывает, что предикат
			// умеет видеть поле искомой формы.
			if md.Name() == "ReportIntentAppliedRequest" && fd.Name() == "reason" {
				seenControl = true
			}
			isApplyState := fd.Kind() == protoreflect.MessageKind &&
				fd.Message().FullName() == applyStateName
			if isApplyState {
				offenders = append(offenders, string(md.FullName())+"."+string(fd.Name()))
			}
		}
	})

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("состояние применения объявлено на входе: %s.\n"+
			"Его выводит платформа сравнением ревизий; принять его от вызывающего значило бы "+
			"дать ему написать утверждение платформы о самой себе",
			strings.Join(offenders, ", "))
	}
	if scannedRequests == 0 || scannedFields == 0 {
		t.Fatalf("обход разобрал %d запрос(ов) и %d поле(й) в пакете %s — "+
			"«ни одного вхождения» неотличимо от «ничего не прочитано»",
			scannedRequests, scannedFields, pkg)
	}
	if !seenControl {
		t.Fatal("обход не нашёл класс отказа на приёме отчёта — предикат не видит полей искомой формы")
	}
	t.Logf("осмотрено %d сообщение(й) запроса пакета %s, в них %d поле(й)",
		scannedRequests, pkg, scannedFields)
}

// forEachVPCMessage обходит ВСЕ сообщения верхнего уровня всех файлов пакета
// контракта vpc, включая вложенные.
//
// Обход по пакету, а не по выписанному перечню: перепись входной поверхности
// имеет смысл ровно тогда, когда она полна, а выписанный перечень стареет молча.
func forEachVPCMessage(t *testing.T, fn func(protoreflect.MessageDescriptor)) {
	t.Helper()
	pkg := (*vpcv1.Network)(nil).ProtoReflect().Descriptor().ParentFile().Package()

	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			if md.IsMapEntry() {
				continue
			}
			fn(md)
			walk(md.Messages())
		}
	}

	files := 0
	protoregistry.GlobalFiles.RangeFilesByPackage(pkg, func(fd protoreflect.FileDescriptor) bool {
		files++
		walk(fd.Messages())
		return true
	})
	if files == 0 {
		t.Fatalf("в реестре не нашлось ни одного файла пакета %s — обход читает не то дерево", pkg)
	}
}
