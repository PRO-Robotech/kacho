// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/authz/catalogderive"
)

// Ось `scope_extractor.bound_to_server` (форма ScopeBound, kacho#2915) обязана
// сверяться аннотацией против строки каталога так же, как всякая другая ось:
// строка без признака при аннотации с признаком — это край, читающий пустое
// `from_request_field` как подстановку `*`. Пара ниже — инъекция настоящей
// формы `InternalNotificationFeedService/Claim` и её законный близнец.

func boundClaimAnnotations() catalogderive.Annotations {
	return catalogderive.Annotations{
		Permission:         "platform.notification_feed.claim",
		RequiredRelation:   "reader",
		ScopeObjectType:    "notification_feed",
		ScopeBoundToServer: true,
	}
}

func boundClaimRow(bound bool) catalogderive.Entry {
	var row catalogderive.Entry
	row.FQN = "corelib.notify.InternalNotificationFeedService/Claim"
	row.Permission = "platform.notification_feed.claim"
	row.RequiredRelation = "reader"
	row.ScopeExtractor.ObjectType = "notification_feed"
	row.ScopeExtractor.BoundToServer = bound
	return row
}

func TestCatalogParityBoundInjection_RowWithoutTheFlagIsAFinding(t *testing.T) {
	const fqn = "/corelib.notify.InternalNotificationFeedService/Claim"
	got := diffAnnotationAgainstRow(fqn, boundClaimAnnotations(), boundClaimRow(false))
	if len(got) != 1 || !strings.Contains(got[0], "scope_extractor.bound_to_server") {
		t.Fatalf("строка без bound_to_server при аннотации с ним обязана быть ОДНОЙ находкой по этой оси, получено %q", got)
	}
}

func TestCatalogParityBoundInjection_RowCarryingTheFlagIsSilent(t *testing.T) {
	const fqn = "/corelib.notify.InternalNotificationFeedService/Claim"
	if got := diffAnnotationAgainstRow(fqn, boundClaimAnnotations(), boundClaimRow(true)); len(got) != 0 {
		t.Fatalf("законный близнец (признак в строке есть) дал находки: %q", got)
	}
}

func TestCatalogParityBoundInjection_FlagWithoutAnnotationIsAFinding(t *testing.T) {
	const fqn = "/corelib.notify.InternalNotificationFeedService/Claim"
	a := boundClaimAnnotations()
	a.ScopeBoundToServer = false
	got := diffAnnotationAgainstRow(fqn, a, boundClaimRow(true))
	if len(got) != 1 || !strings.Contains(got[0], "scope_extractor.bound_to_server") {
		t.Fatalf("признак в строке без аннотации обязан быть находкой, получено %q", got)
	}
}
