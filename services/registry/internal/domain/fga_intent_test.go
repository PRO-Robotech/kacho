// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// fga_intent_test.go — repo register-intent carries the owning-project so the iam
// reconciler can materialize per-object v_* on registry_repository.
//
// Баг: register-on-first-push эмитил intent БЕЗ ParentProjectID → resource_mirror
// строка репо пустая → IsContainedIn(project)=false → v_* НЕ материализуется → образы
// недоступны даже владельцу. Фикс: RegisterIntentForRepoPush несёт ParentProjectID
// (project реестра-владельца), как это уже делает RegisterIntentForCreate реестра.

import "testing"

// TestRegisterIntentForRepoPush_CarriesParentProjectID — intent на первый push несёт
// ParentProjectID = project реестра (containment scope для iam-mirror). parent-tuple
// остаётся ПЕРВЫМ (структурная привязка repo→registry раньше owner-tuple).
func TestRegisterIntentForRepoPush_CarriesParentProjectID(t *testing.T) {
	intent := RegisterIntentForRepoPush("regX000000000000000", "app", "prj-P", "service_account:sva-ci")

	if intent.ParentProjectID != "prj-P" {
		t.Fatalf("ParentProjectID = %q; want %q (repo mirror needs owning-project for containment)",
			intent.ParentProjectID, "prj-P")
	}
	if len(intent.Tuples) < 2 {
		t.Fatalf("want parent+owner tuples, got %d", len(intent.Tuples))
	}
	// parent-tuple ПЕРВЫМ.
	if intent.Tuples[0].Relation != FGARelationParent {
		t.Errorf("Tuples[0].Relation = %q; want parent (structural link first)", intent.Tuples[0].Relation)
	}
	if intent.Tuples[1].Relation != FGARelationOwner {
		t.Errorf("Tuples[1].Relation = %q; want owner", intent.Tuples[1].Relation)
	}
	// У registry_repository НЕТ project-relation — project-tuple на репо НЕ добавляем
	// (материализацию обеспечивает mirror.parent_project_id, не FGA project-tuple).
	for _, tp := range intent.Tuples {
		if tp.Relation == FGARelationProject {
			t.Errorf("repo intent must NOT carry a project-tuple (repo type has no project-relation); got %+v", tp)
		}
	}
}

// TestRegisterIntentForRepoPush_NoSubject_NoOwnerTuple — system/unauthenticated push
// (пустой subject) → owner-tuple пропущен, но parent-tuple + ParentProjectID остаются
// (репо всё равно линкуется к реестру и получает containment scope).
func TestRegisterIntentForRepoPush_NoSubject_NoOwnerTuple(t *testing.T) {
	intent := RegisterIntentForRepoPush("regX000000000000000", "app", "prj-P", "")

	if intent.ParentProjectID != "prj-P" {
		t.Fatalf("ParentProjectID = %q; want prj-P even without a subject", intent.ParentProjectID)
	}
	if len(intent.Tuples) != 1 || intent.Tuples[0].Relation != FGARelationParent {
		t.Fatalf("want exactly the parent-tuple, got %+v", intent.Tuples)
	}
}

// TestRepoPublicGrantNamesTheAnonymousClass — кортеж публикации репозитория
// (регистрация И снятие) называет субъектом класс `system_class:anonymous`, а не
// подстановку `user:*` (kacho#3287, приёмка kaname
// anonymous-and-authenticated-are-subject-classes.md, SC-12).
//
// Написание закреплено литералом, а не константой пакета: проба ловит правку
// самой константы. Снятие обязано назвать то же слово, что запись, — иначе оно
// не встретит записанного и публикация переживёт смену видимости.
//
// Законный близнец — кортеж владения первого push: его субъект — конкретный
// принципал, и эта правка его не касается.
func TestRepoPublicGrantNamesTheAnonymousClass(t *testing.T) {
	const (
		wantSubject = "system_class:anonymous"
		wantObject  = "registry_repository:regX000000000000000/team/app"
	)
	cases := map[string]RegisterIntent{
		"регистрация": RegisterIntentForRepoPublicGrant("regX000000000000000", "team/app"),
		"снятие":      UnregisterIntentForRepoPublicGrant("regX000000000000000", "team/app"),
	}
	for name, intent := range cases {
		if len(intent.Tuples) != 1 {
			t.Fatalf("%s: кортежей %d, ожидался ровно один: %+v", name, len(intent.Tuples), intent.Tuples)
		}
		got := intent.Tuples[0]
		if got.SubjectID != wantSubject || got.Relation != "v_get" || got.Object != wantObject {
			t.Errorf("%s: кортеж публикации {%q %q %q}; ожидался {%q %q %q}",
				name, got.SubjectID, got.Relation, got.Object, wantSubject, "v_get", wantObject)
		}
	}

	push := RegisterIntentForRepoPush("regX000000000000000", "team/app", "prj-P", "service_account:sva-ci")
	if push.Tuples[1].SubjectID != "service_account:sva-ci" {
		t.Errorf("близнец: кортеж владения назвал %q вместо принципала", push.Tuples[1].SubjectID)
	}
}
