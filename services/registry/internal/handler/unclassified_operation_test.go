// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// unclassified_operation_test.go — полярность строки, которую фильтр НЕ СУМЕЛ
// классифицировать.
//
// filterOperations решает судьбу строки по её принадлежности к sub-репозиторию, а
// принадлежность добывается рефлексией из метаданных операции. Рефлексия — это шаг,
// который может НЕ ДАТЬ ОТВЕТА: тип метаданных может не резолвиться в этом бинаре
// (версионный перекос), а байты — не разбираться. «Не разобрал» и «разобрал, там нет
// репозитория» — РАЗНЫЕ исходы, и второй нельзя выдавать за первый.
//
// Тесты ниже фиксируют обе полярности сразу, потому что по отдельности каждая
// зеленеет на сломанном фильтре: «строка выпала» неотличимо от «выпадает всё», а
// «строка прошла» неотличимо от «проходит всё».
package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
)

// unresolvableTypeOpH — операция, тип метаданных которой не резолвится в этом
// бинаре (версионный перекос: строку записал бинарь, знающий тип, читает — не
// знающий). Принадлежность к репозиторию установить НЕЛЬЗЯ.
func unresolvableTypeOpH(id, desc string) operations.Operation {
	return operations.Operation{
		ID:          id,
		Description: desc,
		Metadata: &anypb.Any{
			TypeUrl: "type.googleapis.com/kacho.cloud.registry.v1.MetadataTypeThisBinaryDoesNotKnow",
			Value:   []byte{0x0a, 0x03, 'a', 'p', 'p'},
		},
	}
}

// corruptBytesOpH — тип метаданных известен, но байты не разбираются.
// Принадлежность к репозиторию установить НЕЛЬЗЯ.
func corruptBytesOpH(t *testing.T, id, desc string) operations.Operation {
	t.Helper()
	good, err := anypb.New(&registryv1.DeleteTagMetadata{RegistryId: validReg, Repository: "app-b", Tag: "v1"})
	require.NoError(t, err)
	return operations.Operation{
		ID:          id,
		Description: desc,
		Metadata:    &anypb.Any{TypeUrl: good.GetTypeUrl(), Value: []byte{0xff, 0xff, 0xff, 0xff}},
	}
}

// noMetadataOpH — операция вовсе без метаданных. Ни один путь создания операции в
// registry такую не производит (все девять зовут anypb.New), поэтому строка без
// метаданных — не «registry-level», а строка неизвестного происхождения:
// принадлежность установить НЕЛЬЗЯ.
func noMetadataOpH(id, desc string) operations.Operation {
	return operations.Operation{ID: id, Description: desc}
}

// TestListOperations_DropsRowWhoseScopeCannotBeDetermined — строка, чью
// принадлежность фильтр установить не смог, НЕ отдаётся вызывающему.
//
// Парный положительный исход — в том же ответе: классифицированная registry-level
// строка проходит. Без него «выпала» неотличимо от «фильтр выбрасывает всё».
func TestListOperations_DropsRowWhoseScopeCannotBeDetermined(t *testing.T) {
	cases := []struct {
		name string
		op   func(t *testing.T) operations.Operation
	}{
		{"unresolvable_metadata_type", func(*testing.T) operations.Operation {
			return unresolvableTypeOpH("rop-unknown", "Delete tag v1 of "+validReg+"/app-b")
		}},
		{"corrupt_metadata_bytes", func(t *testing.T) operations.Operation {
			return corruptBytesOpH(t, "rop-corrupt", "Delete tag v1 of "+validReg+"/app-b")
		}},
		{"no_metadata_at_all", func(*testing.T) operations.Operation {
			return noMetadataOpH("rop-nometa", "Delete tag v1 of "+validReg+"/app-b")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops := newMemOpsH()
			// Парный положительный: классифицируемая registry-level строка.
			ops.put(registryLevelOpH(t, "rop-create", validReg, "Create registry"))
			ops.put(tc.op(t))
			// Namespace v_list есть (гейт интерсептора), per-repo — нет ни на что.
			az := &recordingAuthorizer{allow: map[string]bool{registryObjectRef(validReg): true}}
			h := newTestHandlerOps(ops, az)

			resp, err := h.ListOperations(carolCtx(), &registryv1.ListRegistryOperationsRequest{RegistryId: validReg})
			require.NoError(t, err)

			ids := make([]string, 0, len(resp.GetOperations()))
			for _, op := range resp.GetOperations() {
				ids = append(ids, op.GetId())
			}
			require.Contains(t, ids, "rop-create",
				"положительный исход: классифицированная registry-level строка обязана пройти — "+
					"иначе «выпала» ниже неотличимо от «выпадает всё»")
			require.NotContains(t, ids, tc.op(t).ID,
				"строка, чью принадлежность установить не удалось, не может быть отдана без вопроса о правах")
			require.Len(t, ids, 1)
			for _, op := range resp.GetOperations() {
				require.NotContains(t, op.GetDescription(), "app-b",
					"имя репозитория не должно уехать вызывающему через описание неклассифицированной строки")
			}
		})
	}
}

// TestListOperations_NamesTheRowsItCouldNotClassify — выпадение обязано быть
// названо: молча исчезающая строка неотличима от строки, которой не было.
func TestListOperations_NamesTheRowsItCouldNotClassify(t *testing.T) {
	var buf bytes.Buffer
	sink := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ops := newMemOpsH()
	ops.put(registryLevelOpH(t, "rop-create", validReg, "Create registry"))
	ops.put(unresolvableTypeOpH("rop-unknown", "Delete tag v1 of "+validReg+"/app-b"))
	az := &recordingAuthorizer{allow: map[string]bool{registryObjectRef(validReg): true}}
	h := newTestHandlerOps(ops, az)
	h.authz.log = sink

	_, err := h.ListOperations(carolCtx(), &registryv1.ListRegistryOperationsRequest{RegistryId: validReg})
	require.NoError(t, err)

	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		if strings.Contains(rec["msg"].(string), "could not be determined") {
			found = rec
		}
	}
	require.NotNil(t, found, "выпадение неклассифицированной строки обязано быть названо в логе")
	require.Equal(t, float64(1), found["dropped"], "число выпавших строк обязано быть в записи")
}

// TestListOperations_SilentWhenEveryRowIsClassified — парный отрицательный к записи
// выше: на странице, где всё классифицировано, запись НЕ появляется. Без этого
// «запись есть» неотличимо от «запись пишется всегда».
func TestListOperations_SilentWhenEveryRowIsClassified(t *testing.T) {
	var buf bytes.Buffer
	sink := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ops := newMemOpsH()
	ops.put(registryLevelOpH(t, "rop-create", validReg, "Create registry"))
	ops.put(deleteTagOpH(t, "rop-deltag", validReg, "app-b", "v1"))
	az := &recordingAuthorizer{allow: map[string]bool{
		registryObjectRef(validReg):            true,
		repositoryObjectRef(validReg, "app-b"): true,
	}}
	h := newTestHandlerOps(ops, az)
	h.authz.log = sink

	resp, err := h.ListOperations(carolCtx(), &registryv1.ListRegistryOperationsRequest{RegistryId: validReg})
	require.NoError(t, err)
	require.Len(t, resp.GetOperations(), 2, "обе классифицированные строки проходят")
	require.NotContains(t, buf.String(), "could not be determined")
}
