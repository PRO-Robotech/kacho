// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// checkProject — единый cross-service existence-check владельца-Project через
// ProjectClient (kacho-iam ProjectService.Get). Раньше был byte-for-byte
// продублирован в instance/image/disk (метод `checkFolder`) + inline в snapshot;
// сведён в один helper (rule 11), чтобы маппинг (peer-недоступен → Unavailable,
// не-найдено → NotFound) не расходился между ресурсами.
//
// Словарь и тон текстов — Kachō (`Project`, api-conventions.md
// "<Resource> %s not found"). Ресурс называется Project
// (proto/kacho/cloud/iam/v1/project.proto); `Folder` в API Kachō не существует,
// а клиент присылает `projectId` — ошибка про `Folder` не соответствовала бы
// ничему на публичной поверхности.
//
// Код НЕ меняем: NotFound остаётся NotFound. Перевод этой peer-validate-линии на
// FAILED_PRECONDITION (api-conventions.md by-lane code-split) — отдельное
// ломающее решение, не здесь.
func checkProject(ctx context.Context, pc ProjectClient, projectID string) error {
	exists, err := pc.Exists(ctx, projectID)
	if err != nil {
		return status.Error(codes.Unavailable, "project check: upstream project service unavailable")
	}
	if !exists {
		return status.Errorf(codes.NotFound, "Project %s not found", projectID)
	}
	return nil
}
