// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operationresolver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/operationresolver"
)

// Разрешитель осиротевших операций отвечает на один вопрос: чем закрыть строку,
// чей исполнитель проверенно мёртв. Проверяется НАБЛЮДАЕМОЕ — исход, — а не то,
// что функция вызвана.

const (
	volID  = "vol00000000000000001"
	snapID = "snp00000000000000001"
	imgID  = "img00000000000000001"
)

type volReader struct {
	v   *domain.Volume
	err error
}

func (r volReader) Get(context.Context, string) (*domain.Volume, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.v, nil
}

type snapReader struct{ s *domain.Snapshot }

func (r snapReader) Get(context.Context, string) (*domain.Snapshot, error) {
	if r.s == nil {
		return nil, fmt.Errorf("%w: Snapshot %s not found", storageerr.ErrNotFound, snapID)
	}
	return r.s, nil
}

type imgReader struct{ i *domain.Image }

func (r imgReader) Get(context.Context, string) (*domain.Image, error) {
	if r.i == nil {
		return nil, fmt.Errorf("%w: Image %s not found", storageerr.ErrNotFound, imgID)
	}
	return r.i, nil
}

func opWith(t *testing.T, md proto.Message) operations.Operation {
	t.Helper()
	any, err := anypb.New(md)
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	return operations.Operation{ID: "opr-1", Metadata: any}
}

func missingVolume() volReader {
	return volReader{err: fmt.Errorf("%w: Volume %s not found", storageerr.ErrNotFound, volID)}
}

// TestResolve_CreateOfACommittedResourceIsDone — создание закоммичено → строка
// закрывается успехом, и в ответе лежит ТЕКУЩИЙ ресурс (клиент, доживший до
// разрешения, получает то же, что вернул бы Get).
func TestResolve_CreateOfACommittedResourceIsDone(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Volume: volReader{v: &domain.Volume{ID: volID, ProjectID: "prj-1", Name: "kept"}},
	})

	res, err := rs.Resolve(context.Background(), opWith(t, &storagev1.CreateVolumeMetadata{VolumeId: volID}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done — the row is committed", res.Outcome)
	}
	if res.Response == nil {
		t.Fatal("Done without a Response: the client polling this operation would learn the outcome " +
			"and not the resource")
	}
	msg, uerr := res.Response.UnmarshalNew()
	if uerr != nil {
		t.Fatalf("unmarshal response: %v", uerr)
	}
	got, ok := msg.(*storagev1.Volume)
	if !ok || got.GetId() != volID {
		t.Fatalf("response = %T/%v, want the current Volume %s", msg, msg, volID)
	}
}

// TestResolve_CreateOfAnUncommittedResourceIsInterrupted — работа не дошла до
// коммита → строка закрывается отказом, а не висит вечно.
func TestResolve_CreateOfAnUncommittedResourceIsInterrupted(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{Volume: missingVolume()})

	res, err := rs.Resolve(context.Background(), opWith(t, &storagev1.CreateVolumeMetadata{VolumeId: volID}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want Interrupted — nothing was committed", res.Outcome)
	}
}

// TestResolve_DeleteIsMirrored — удаление читается наоборот: ресурса нет → успех;
// ресурс жив → работа прервана.
func TestResolve_DeleteIsMirrored(t *testing.T) {
	gone := operationresolver.New(operationresolver.Readers{Volume: missingVolume()})
	res, err := gone.Resolve(context.Background(), opWith(t, &storagev1.DeleteVolumeMetadata{VolumeId: volID}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeDone || res.Response != nil {
		t.Fatalf("outcome=%v response=%v, want Done with an empty response", res.Outcome, res.Response)
	}

	alive := operationresolver.New(operationresolver.Readers{
		Volume: volReader{v: &domain.Volume{ID: volID}},
	})
	res, err = alive.Resolve(context.Background(), opWith(t, &storagev1.DeleteVolumeMetadata{VolumeId: volID}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want Interrupted — the row is still there", res.Outcome)
	}
}

// TestResolve_TransientReadErrorIsNotAVerdict — недоступная БД не есть ответ про
// ресурс: ошибка уходит наверх, строка остаётся до следующего прохода.
func TestResolve_TransientReadErrorIsNotAVerdict(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Volume: volReader{err: errors.New("connection refused")},
	})

	res, err := rs.Resolve(context.Background(), opWith(t, &storagev1.CreateVolumeMetadata{VolumeId: volID}))
	if err == nil {
		t.Fatalf("Resolve returned outcome %v on an unreadable resource, want an error — "+
			"a transient failure must not be turned into a terminal verdict", res.Outcome)
	}
}

// TestResolve_UnreadableResourceIsSkippedNotGuessed — читатель не провязан →
// пропуск, а не выдуманный исход.
func TestResolve_UnreadableResourceIsSkippedNotGuessed(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{}) // ни одного читателя

	res, err := rs.Resolve(context.Background(), opWith(t, &storagev1.CreateVolumeMetadata{VolumeId: volID}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip — the resource was never read", res.Outcome)
	}
}

// TestResolve_EveryMetadataTypeStorageEmitsIsDispatched — полнота: каждый тип
// метаданных, который эмитит ЭТОТ сервис, обязан получать терминал. Тип, попавший
// в ветку default, остался бы «в процессе» навсегда — ровно тот дефект, ради
// которого разрешитель и появился.
func TestResolve_EveryMetadataTypeStorageEmitsIsDispatched(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Volume:   missingVolume(),
		Snapshot: snapReader{},
		Image:    imgReader{},
	})

	// Все девять типов, эмитируемых services/storage (три ресурса × create/update/delete).
	cases := []proto.Message{
		&storagev1.CreateVolumeMetadata{VolumeId: volID},
		&storagev1.UpdateVolumeMetadata{VolumeId: volID},
		&storagev1.DeleteVolumeMetadata{VolumeId: volID},
		&storagev1.CreateSnapshotMetadata{SnapshotId: snapID},
		&storagev1.UpdateSnapshotMetadata{SnapshotId: snapID},
		&storagev1.DeleteSnapshotMetadata{SnapshotId: snapID},
		&storagev1.CreateImageMetadata{ImageId: imgID},
		&storagev1.UpdateImageMetadata{ImageId: imgID},
		&storagev1.DeleteImageMetadata{ImageId: imgID},
	}
	for _, md := range cases {
		res, err := rs.Resolve(context.Background(), opWith(t, md))
		if err != nil {
			t.Fatalf("%T: Resolve: %v", md, err)
		}
		if res.Outcome == operations.OutcomeSkip {
			t.Errorf("%T fell through to Skip: this service emits that metadata, so the row would "+
				"stay in progress forever", md)
		}
	}
}

// TestResolve_ForeignMetadataIsSkipped — чужой тип метаданных не наш предмет:
// пропуск, а не выдуманный терминал по чужому ресурсу.
func TestResolve_ForeignMetadataIsSkipped(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{Volume: missingVolume()})

	res, err := rs.Resolve(context.Background(), opWith(t, &storagev1.Volume{Id: volID}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip for metadata this service never emits", res.Outcome)
	}
}

// TestResolve_NilMetadataIsSkipped — строка без метаданных не разрешается вслепую.
func TestResolve_NilMetadataIsSkipped(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{Volume: missingVolume()})
	res, err := rs.Resolve(context.Background(), operations.Operation{ID: "opr-2"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip", res.Outcome)
	}
}
