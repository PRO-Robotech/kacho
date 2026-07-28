// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/shared/prototime"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// operationProto — псевдоним типа operation.Operation в форме, которую возвращают
// gRPC-стабы RegistryService мутаций (совпадает с corelib operation proto).
type operationProto = operationpb.Operation

// Проекция domain.Registry → registryv1.Registry (с output-only endpoint) —
// единый источник в use-case (UseCase.ProtoRegistry), т.к. endpoint зависит от
// конфигурируемой base. Handler зовёт h.uc.ProtoRegistry, отдельного конвертера
// Registry в transport-слое нет.
//
// Проекция domain.Repository → registryv1.Repository — тоже ЕДИНЫЙ источник
// (UseCase.ProtoRepository), и её зовут ОБА пути: поштучное чтение и список. Второй
// конвертер в transport-слое существовал и не переносил lifecycle, из-за чего один и тот
// же репозиторий выглядел по-разному в зависимости от того, как его спросили. Два
// конвертера одного типа расходятся молча — поэтому конвертер один.

// toProtoTag конвертирует domain.Tag → registryv1.Tag.
func toProtoTag(t *domain.Tag) *registryv1.Tag {
	if t == nil {
		return nil
	}
	return &registryv1.Tag{
		RegistryId:    t.RegistryID,
		Repository:    t.Repository,
		Tag:           t.Tag,
		Digest:        t.Digest,
		SizeBytes:     t.SizeBytes,
		MediaType:     t.MediaType,
		CreatedAt:     prototime.Truncate(t.CreatedAt),
		Architecture:  t.Architecture,
		LastPulledAt:  prototime.Truncate(t.LastPulledAt),
		PushedBy:      t.PushedBy,
		DownloadCount: t.DownloadCount,
	}
}

// toProtoStats конвертирует domain.RegistryStats → registryv1.RegistryStats.
// proto-поле last_gc_at НЕ заполняется: у zot нет ad-hoc GC-триггера с меткой времени
// (TriggerGC — лишь handshake), источника GC-времени нет → поле остаётся unset (честный
// zero), а не всегда-нулевая колонка, вводящая оператора в заблуждение.
func toProtoStats(s *domain.RegistryStats) *registryv1.RegistryStats {
	if s == nil {
		return nil
	}
	return &registryv1.RegistryStats{
		RegistryId:      s.RegistryID,
		RepositoryCount: s.RepositoryCount,
		TagCount:        s.TagCount,
		TotalSizeBytes:  s.TotalSizeBytes,
		BlobCount:       s.BlobCount,
	}
}

// operationToProto конвертирует corelib operations.Operation в proto-форму
// (OperationService.Get/мутации возвращают её клиенту). oneof result —
// error|response (заполнен только при done).
func operationToProto(op *operations.Operation) *operationpb.Operation {
	if op == nil {
		return nil
	}
	p := &operationpb.Operation{
		Id:                   op.ID,
		Description:          op.Description,
		CreatedAt:            prototime.Truncate(op.CreatedAt),
		CreatedBy:            op.CreatedBy,
		ModifiedAt:           prototime.Truncate(op.ModifiedAt),
		Done:                 op.Done,
		Metadata:             op.Metadata,
		PrincipalType:        op.Principal.Type,
		PrincipalId:          op.Principal.ID,
		PrincipalDisplayName: op.Principal.DisplayName,
	}
	if op.Error != nil {
		p.Result = &operationpb.Operation_Error{Error: op.Error}
	} else if op.Response != nil {
		p.Result = &operationpb.Operation_Response{Response: op.Response}
	}
	return p
}
