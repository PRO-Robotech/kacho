// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package guestaccesskey

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ── Одна форма имени на дерево ───────────────────────────────────────────────
//
// Имя ключа проверялось ТОЛЬКО длиной (1..63), без набора символов, поэтому
// `Foo_BAR!!` и `моё имя с пробелами` проходили — при том что у соседних
// ресурсов того же сервиса имя судится формой. Одно правило, два исполнения.

// TestCreateRejectsNamesThatAreNotNames — отрицание: то, что именем не является,
// отвергается СИНХРОННО по имени поля и до хранилища не доезжает.
func TestCreateRejectsNamesThatAreNotNames(t *testing.T) {
	for _, name := range []string{"Foo_BAR!!", "my name with spaces", "рабочий", "-lead", "trail-", ""} {
		repo := &fakeKeyRepo{}
		svc, _ := svcWith(repo)
		_, err := svc.Create(context.Background(), CreateReq{
			ProjectID: "prj1", Name: name, PublicKey: validMaterial,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("Create(name=%q) код = %v, ожидался InvalidArgument", name, status.Code(err))
			continue
		}
		if repo.inserted != nil {
			t.Errorf("Create(name=%q): отвергнутое имя доехало до хранилища", name)
		}
	}
}

// TestCreateAcceptsCanonicalNames — положительный контроль к пробе выше: без
// него «отвергнуто» было бы неотличимо от «отвергается всё».
func TestCreateAcceptsCanonicalNames(t *testing.T) {
	for _, name := range []string{"work", "work-key-1", "9", strings.Repeat("k", 63)} {
		repo := &fakeKeyRepo{}
		svc, ops := svcWith(repo)
		op, err := svc.Create(context.Background(), CreateReq{
			ProjectID: "prj1", Name: name, PublicKey: validMaterial,
		})
		if err != nil {
			t.Errorf("Create(name=%q) отвергнут: %v", name, err)
			continue
		}
		ops.awaitFinished(t, op.ID)
		if repo.inserted == nil {
			t.Errorf("Create(name=%q) не доехал до хранилища", name)
		}
	}
}

// TestUpdateRejectsNamesThatAreNotNames — та же форма на правке: асимметрия
// между созданием и правкой — отдельный класс, и заводить его заново незачем.
func TestUpdateRejectsNamesThatAreNotNames(t *testing.T) {
	const id = "gak-abcdefghjkmnpqrst"
	for _, name := range []string{"Foo_BAR!!", "my name with spaces", ""} {
		repo := &fakeKeyRepo{}
		svc, _ := svcWith(repo)
		_, err := svc.Update(context.Background(), UpdateReq{
			ID: id, UpdateMask: []string{"name"}, Name: name,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("Update(name=%q) код = %v, ожидался InvalidArgument", name, status.Code(err))
			continue
		}
		if repo.updated {
			t.Errorf("Update(name=%q): отвергнутое имя доехало до хранилища", name)
		}
	}
}

// TestUpdateAcceptsCanonicalName — положительный контроль к пробе выше.
func TestUpdateAcceptsCanonicalName(t *testing.T) {
	const id = "gak-abcdefghjkmnpqrst"
	repo := &fakeKeyRepo{}
	svc, ops := svcWith(repo)
	op, err := svc.Update(context.Background(), UpdateReq{
		ID: id, UpdateMask: []string{"name"}, Name: "renamed",
	})
	if err != nil {
		t.Fatalf("Update законным именем отвергнут: %v", err)
	}
	ops.awaitFinished(t, op.ID)
	if !repo.updated || repo.updateArg.Name == nil || *repo.updateArg.Name != "renamed" {
		t.Fatalf("до хранилища дошло %v, want %q", repo.updateArg.Name, "renamed")
	}
}

// TestFullPatchWithoutNameIsAccepted — ДЕФЕКТ, видимый вызывающему: полная
// правка (пустая маска) применяет весь изменяемый набор, включая имя, поэтому
// вызывающий, менявший ОДНИ метки, получал 400 по полю, которого не касался.
//
// В proto3 пропущенное и пустое скалярное поле неразличимы, значит пустое имя
// при пустой маске означает «не прислали». Утверждается наблюдаемое: правка
// проходит И имя до хранилища не едет.
func TestFullPatchWithoutNameIsAccepted(t *testing.T) {
	const id = "gak-abcdefghjkmnpqrst"
	repo := &fakeKeyRepo{}
	svc, ops := svcWith(repo)

	op, err := svc.Update(context.Background(), UpdateReq{
		ID: id, Labels: map[string]string{"env": "probe"},
	})
	if err != nil {
		t.Fatalf("полная правка без имени отвергнута: %v", err)
	}
	ops.awaitFinished(t, op.ID)
	if !repo.updated {
		t.Fatal("правка не доехала до хранилища")
	}
	if repo.updateArg.Name != nil {
		t.Fatalf("имя записано (%q), хотя запрос его не присылал", *repo.updateArg.Name)
	}
	if !repo.updateArg.LabelsSet {
		t.Fatal("метки обязаны примениться — их запрос как раз и прислал")
	}
}

// TestFullPatchWithNameStillApplies — положительный контроль к пробе выше: без
// него она зеленела бы на правке, которая имя не пишет НИКОГДА.
func TestFullPatchWithNameStillApplies(t *testing.T) {
	const id = "gak-abcdefghjkmnpqrst"
	repo := &fakeKeyRepo{}
	svc, ops := svcWith(repo)

	op, err := svc.Update(context.Background(), UpdateReq{ID: id, Name: "renamed"})
	if err != nil {
		t.Fatalf("полная правка с именем отвергнута: %v", err)
	}
	ops.awaitFinished(t, op.ID)
	if repo.updateArg.Name == nil || *repo.updateArg.Name != "renamed" {
		t.Fatalf("до хранилища дошло %v, want %q", repo.updateArg.Name, "renamed")
	}
}
