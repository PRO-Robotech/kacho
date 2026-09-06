// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package peercheck — проверки существования чужих ресурсов у их владельцев.
//
// Пакет отдельный НЕ ради слоя, а ради единственности: тот же вопрос задаёт
// теперь не только машина, и вторая копия разошлась бы с первой ровно так, как
// разошлись три копии, сведённые здесь в одну.

package peercheck

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// Project — единый cross-service existence-check владельца-Project через
// ProjectClient (kaname ProjectService.Get). Раньше был byte-for-byte
// продублирован в instance/image/disk (метод `checkFolder`) + inline в snapshot;
// сведён в один helper (rule 11), чтобы маппинг (peer-недоступен → Unavailable,
// не-найдено → NotFound) не расходился между ресурсами.
//
// Словарь и тон текстов — Kachō (`Project`, api-conventions.md
// "<Resource> %s not found"). Ресурс называется Project
// (proto/kaname/cloud/iam/v1/project.proto); `Folder` в API Kachō не существует,
// а клиент присылает `projectId` — ошибка про `Folder` не соответствовала бы
// ничему на публичной поверхности.
//
// Код НЕ меняем: NotFound остаётся NotFound. Перевод этой peer-validate-линии на
// FAILED_PRECONDITION (api-conventions.md by-lane code-split) — отдельное
// ломающее решение, не здесь.
func Project(ctx context.Context, pc ports.ProjectClient, projectID string) error {
	exists, err := pc.Exists(ctx, projectID)
	if err != nil {
		return status.Error(codes.Unavailable, "project check: upstream project service unavailable")
	}
	if !exists {
		return status.Errorf(codes.NotFound, "Project %s not found", projectID)
	}
	return nil
}
