// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package placementgroup

import (
	"context"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// Проверки единственной формы имени для PlacementGroup: строка ресурса не может
// нести пустое имя, и снять имя правкой нельзя.
//
// Пробы стоят на уровне use-case, а не на уровне общей функции проверки: она уже
// проверена своими пробами в pkg/validate, и повторять её ответ здесь означало бы
// закрепить ОТВЕТ вместо МЕСТА. Предмет этого файла — что группа размещения её
// действительно зовёт, зовёт в точке, где идентификатор уже есть, и записывает
// результат.

// createUnnamedGroup — создание без имени, доведённое до конца операции.
// Возвращает то, что увидел бы вызывающий в ответе.
func createUnnamedGroup(t *testing.T, svc *Service, ops *memOps, req CreateReq) *computev1.PlacementGroup {
	t.Helper()
	req.Name = ""
	op, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("создание без имени обязано пройти: пустое имя означает «назови сам», получено %v", err)
	}
	return awaitGroup(t, ops, op.ID)
}

// awaitGroup доводит операцию до конца и отдаёт разобранный ответ, а её отказ
// называет вслух: молчаливый провал сделал бы «ресурс не создан» неотличимым от
// «создан не тот».
func awaitGroup(t *testing.T, ops *memOps, opID string) *computev1.PlacementGroup {
	t.Helper()
	ops.awaitFinished(t, opID)
	ops.mu.Lock()
	defer ops.mu.Unlock()
	if st, bad := ops.errs[opID]; bad {
		t.Fatalf("операция %s отказала: %s", opID, st.GetMessage())
	}
	resp, ok := ops.done[opID]
	if !ok || resp == nil {
		t.Fatalf("операция %s завершилась без ответа", opID)
	}
	var g computev1.PlacementGroup
	if err := resp.UnmarshalTo(&g); err != nil {
		t.Fatalf("ответ операции %s не разбирается как группа: %v", opID, err)
	}
	return &g
}

// refusalNamesField — назвал ли отказ поле ПО ИМЕНИ.
//
// Утверждать надо именно это, а не только код: общая проверка входа кладёт имя
// поля в google.rpc.BadRequest.field_violations, а верхнее сообщение у неё
// дословно «invalid argument». Проба, смотрящая на сообщение, зеленела бы на
// любом отказе валидации — в том числе на отказе про совсем другое поле.
func refusalNamesField(t *testing.T, err error, field string) bool {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("ошибка обязана быть gRPC-статусом, получено %v", err)
	}
	for _, d := range st.Details() {
		br, isBR := d.(*errdetails.BadRequest)
		if !isBR {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			if v.GetField() == field {
				return true
			}
		}
	}
	return false
}

// TestCreate_EmptyName_WritesIdDerivedDefault — создание без имени записывает
// имя, производное от идентификатора, а не пустую строку.
//
// Утверждается ЗАПИСАННОЕ, а не факт вызова общей функции: подстановка, сделанная
// до генерации идентификатора, дала бы пустое умолчание и всё равно выглядела бы
// как «общая функция вызвана».
func TestCreate_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	repo := &fakeGroupRepo{}
	svc, ops := svcWith(repo, fakeGeo{})
	g := createUnnamedGroup(t, svc, ops, zonalReq())

	if g.Name == "" {
		t.Fatal("строка ресурса не может нести пустое имя")
	}
	if g.Name != g.Id {
		t.Errorf("умолчание — сам идентификатор, получено имя %q при идентификаторе %q", g.Name, g.Id)
	}
}

// TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames — два безымянных создания
// в ОДНОМ проекте проходят ОБА и получают РАЗНЫЕ имена.
//
// Умолчание, производное от чего угодно, кроме идентификатора (константа, имя
// вида ресурса, счётчик), столкнулось бы на уникальности (project, name) — и
// второе создание отвергалось бы у арендатора, не сделавшего ничего неверного.
// Хранилище-дублёр это ограничение несёт, поэтому утверждение не вакуумно.
func TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	repo := &fakeGroupRepo{}
	svc, ops := svcWith(repo, fakeGeo{})

	first := createUnnamedGroup(t, svc, ops, zonalReq())
	second := createUnnamedGroup(t, svc, ops, zonalReq())

	if first.Name == second.Name {
		t.Errorf("два безымянных создания в одном проекте обязаны получить разные имена, оба = %q", first.Name)
	}
	if len(repo.all) != 2 {
		t.Errorf("до хранилища обязаны доехать обе группы, доехало %d", len(repo.all))
	}
}

// TestCreate_NameStillValidated — форма имени по-прежнему проверяется.
//
// Положительный контроль обязателен рядом с отрицанием: без него проба зеленела
// бы и на реализации, отвергающей ЛЮБОЕ имя, то есть на противоположном дефекте.
func TestCreate_NameStillValidated(t *testing.T) {
	t.Run("законное имя проходит и не подменяется умолчанием", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, ops := svcWith(repo, fakeGeo{})
		req := zonalReq()
		req.Name = "plg-legal-1"
		op, err := svc.Create(context.Background(), req)
		if err != nil {
			t.Fatalf("законное имя обязано проходить, получено %v", err)
		}
		if g := awaitGroup(t, ops, op.ID); g.Name != "plg-legal-1" {
			t.Errorf("названное имя не должно подменяться, получено %q", g.Name)
		}
	})

	t.Run("заглавные и подчёркивание отвергаются", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, _ := svcWith(repo, fakeGeo{})
		req := zonalReq()
		req.Name = "Bad_Name"
		_, err := svc.Create(context.Background(), req)
		if err == nil {
			t.Fatal("заглавные и подчёркивание формой не приняты")
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("код = %v, ожидался InvalidArgument", status.Code(err))
		}
		if !refusalNamesField(t, err, "name") {
			t.Error("отказ обязан называть поле")
		}
		if repo.inserted != nil {
			t.Error("отвергнутая группа не должна доезжать до хранилища")
		}
	})
}

// TestUpdate_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, значение пусто:
// отказ с именем поля.
//
// Отказ синхронный и приходит из проверки входа, а не из базы: без него пустое
// имя доехало бы до столбца, на который миграция 715001 поставила ограничение
// формы, и вызывающий получил бы внутреннюю ошибку вместо контрактного отказа.
func TestUpdate_MaskNamesName_EmptyRejected(t *testing.T) {
	repo := &fakeGroupRepo{}
	svc, _ := svcWith(repo, fakeGeo{})

	_, err := svc.Update(context.Background(), UpdateReq{
		ID: someGroup, UpdateMask: []string{"name"}, Name: "",
	})
	if err == nil {
		t.Fatal("снять имя правкой нельзя — имени без значения не бывает")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("код = %v, ожидался InvalidArgument", status.Code(err))
	}
	if !refusalNamesField(t, err, "name") {
		t.Error("отказ обязан называть поле: код InvalidArgument возвращает вся проверка входа, " +
			"и по одному коду вызывающий не узнает, что именно прислал неверно")
	}
	if repo.updated {
		t.Error("отвергнутая правка не должна доезжать до хранилища")
	}

	// Положительный контроль ТОЙ ЖЕ маской: без него отрицание выше зеленело бы
	// на реализации, отвергающей всякую правку имени.
	okRepo := &fakeGroupRepo{}
	okSvc, okOps := svcWith(okRepo, fakeGeo{})
	op, err := okSvc.Update(context.Background(), UpdateReq{
		ID: someGroup, UpdateMask: []string{"name"}, Name: "plg-2",
	})
	if err != nil {
		t.Fatalf("та же маска с законным именем обязана проходить, получено %v", err)
	}
	okOps.awaitFinished(t, op.ID)
	if okRepo.updateArg.Name == nil || *okRepo.updateArg.Name != "plg-2" {
		t.Errorf("законное имя обязано доехать до записи, получено %+v", okRepo.updateArg)
	}
}

// TestUpdate_EmptyMask_EmptyNameKeepsCurrent — полная правка, НЕ назвавшая имя,
// имя не стирает.
//
// Предмет — дыра, которую проверка входа закрыть не может: при пустой маске
// пустое имя законно (в proto3 «не прислано» и «пусто» неразличимы), поэтому
// вопрос «записывать ли» решается уже на применении. Записав пустоту, полная
// правка описания молча оставила бы группу без имени.
func TestUpdate_EmptyMask_EmptyNameKeepsCurrent(t *testing.T) {
	repo := &fakeGroupRepo{}
	svc, ops := svcWith(repo, fakeGeo{})

	op, err := svc.Update(context.Background(), UpdateReq{
		ID: someGroup, Description: "полная правка описания",
	})
	if err != nil {
		t.Fatalf("полная правка обязана проходить, получено %v", err)
	}
	ops.awaitFinished(t, op.ID)

	if repo.updateArg.Name != nil {
		t.Errorf("полная правка без имени не должна писать имя вовсе, получено %q", *repo.updateArg.Name)
	}
	// Положительный контроль в той же правке: названное ею обязано примениться —
	// иначе утверждение выше зеленело бы на правке, не пишущей ничего.
	if repo.updateArg.Description == nil || *repo.updateArg.Description != "полная правка описания" {
		t.Errorf("описание обязано примениться той же правкой, получено %+v", repo.updateArg)
	}
}

// TestUpdate_EmptyMask_NewNameApplied — вторая половина предыдущего: та же полная
// правка с НЕПУСТЫМ именем имя меняет. Без неё проба выше зеленела бы и на
// применении, которое имя не трогает вовсе.
func TestUpdate_EmptyMask_NewNameApplied(t *testing.T) {
	repo := &fakeGroupRepo{}
	svc, ops := svcWith(repo, fakeGeo{})

	op, err := svc.Update(context.Background(), UpdateReq{ID: someGroup, Name: "plg-renamed"})
	if err != nil {
		t.Fatalf("полная правка с именем обязана проходить, получено %v", err)
	}
	ops.awaitFinished(t, op.ID)

	if repo.updateArg.Name == nil || *repo.updateArg.Name != "plg-renamed" {
		t.Errorf("непустое имя при полной правке обязано примениться, получено %+v", repo.updateArg)
	}
}
