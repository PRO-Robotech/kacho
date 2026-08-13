// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// fakeStream — серверный стрим, отдающий сообщения в память.
type fakeStream struct {
	grpc.ServerStream
	ctx  context.Context
	sink *collectSink
}

func (s *fakeStream) Context() context.Context                { return s.ctx }
func (s *fakeStream) Send(m *vpcv1.WatchIntentResponse) error { return s.sink.Send(m) }
func (s *fakeStream) SetHeader(metadata.MD) error             { return nil }
func (s *fakeStream) SendHeader(metadata.MD) error            { return nil }
func (s *fakeStream) SetTrailer(metadata.MD)                  {}
func (s *fakeStream) SendMsg(any) error                       { return nil }
func (s *fakeStream) RecvMsg(any) error                       { return nil }

func newHandler(t *testing.T, reader IntentReader, rec ApplyRecorder) (*Handler, *Observer) {
	t.Helper()
	obs := quietObserver()
	watch := NewWatchIntentUseCase(reader, obs)
	watch.poll = time.Millisecond
	return NewHandler(watch, NewReportAppliedUseCase(rec, obs), obs), obs
}

// Отрицательная позиция отвергается синхронно и слота подписки не занимает.
func TestNegativeKnownRevisionIsRefusedSynchronously(t *testing.T) {
	reader := &fakeReader{bounds: Bounds{Head: 10}, rows: manyRows(10)}
	h, obs := newHandler(t, reader, &fakeRecorder{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := h.WatchIntent(&vpcv1.WatchIntentRequest{KnownRevision: -1},
		&fakeStream{ctx: ctx, sink: &collectSink{}})

	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "Illegal argument known_revision", st.Message())
	assert.Zero(t, reader.pagesRead(), "журнал читался по невыразимому запросу")
	assert.Equal(t, int64(0), obs.Totals().StreamsStarted, "слот подписки занят невыразимым запросом")
}

// Положительный контроль: позиция 0 законна — это «состояния у меня нет».
func TestZeroKnownRevisionIsTheFullSnapshotRequest(t *testing.T) {
	rows := manyRows(4)
	reader := &fakeReader{bounds: Bounds{Head: 4}, rows: rows}
	h, obs := newHandler(t, reader, &fakeRecorder{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sink := &collectSink{until: len(rows) + 1, stop: cancel}

	require.NoError(t, h.WatchIntent(&vpcv1.WatchIntentRequest{KnownRevision: 0},
		&fakeStream{ctx: ctx, sink: sink}))
	assert.Equal(t, int64(1), obs.Totals().StreamsStarted)
	assert.Equal(t, int64(len(rows)), obs.Totals().IntentsSent)
}

// Подписок больше предела — отказ, а не очередь ожидающих.
func TestConcurrentStreamLimitRefusesInsteadOfQueueing(t *testing.T) {
	reader := &fakeReader{bounds: Bounds{Head: 1}, rows: manyRows(1)}
	h, _ := newHandler(t, reader, &fakeRecorder{})

	// Занять все слоты подписками, которые не заканчиваются сами.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	held := make(chan struct{}, MaxConcurrentStreams)
	for range MaxConcurrentStreams {
		go func() {
			held <- struct{}{}
			_ = h.WatchIntent(&vpcv1.WatchIntentRequest{},
				&fakeStream{ctx: ctx, sink: &collectSink{}})
		}()
	}
	for range MaxConcurrentStreams {
		<-held
	}
	require.Eventually(t, func() bool { return len(h.slots) == MaxConcurrentStreams },
		3*time.Second, time.Millisecond, "слоты подписок не заняты — предел проверять не на чем")

	err := h.WatchIntent(&vpcv1.WatchIntentRequest{},
		&fakeStream{ctx: ctx, sink: &collectSink{}})
	st, _ := status.FromError(err)
	assert.Equal(t, codes.ResourceExhausted, st.Code())
	assert.Equal(t, "too many concurrent dataplane intent streams", st.Message())
}

// Каждое значение словаря контракта имеет перевод в словарь домена, и ни одно
// значение вне словаря не переводится в законное.
//
// Предмет — корзина «прочее»: перевод по умолчанию записал бы применение,
// о котором исполнитель не сообщал, либо класс отказа, которого он не называл.
func TestContractVocabulariesTranslateWithoutABucket(t *testing.T) {
	outcomes := vpcv1.ApplyOutcome_name
	require.Len(t, outcomeToDomain, len(outcomes)-1,
		"переведены не все исходы контракта (кроме «не названо»): %v", outcomes)
	for v, name := range outcomes {
		in := vpcv1.ApplyOutcome(v)
		got := outcomeFromProto(in)
		if in == vpcv1.ApplyOutcome_APPLY_OUTCOME_UNSPECIFIED {
			assert.Equal(t, ApplyOutcome(""), got, "«не названо» получило законный перевод")
			continue
		}
		assert.NotEmpty(t, got, "исход %s не переводится", name)
	}
	assert.Equal(t, ApplyOutcome(""), outcomeFromProto(vpcv1.ApplyOutcome(9999)),
		"значение вне словаря получило законный перевод")

	reasons := vpcv1.ApplyFailureReason_name
	require.Len(t, reasonToDomain, len(reasons)-1,
		"переведены не все классы отказа (кроме «не названо»): %v", reasons)
	for v, name := range reasons {
		in := vpcv1.ApplyFailureReason(v)
		got := reasonFromProto(in)
		if in == vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED {
			assert.Equal(t, ReasonNone, got, "«не названо» получило законный перевод")
			continue
		}
		assert.NotEmpty(t, got, "класс отказа %s не переводится", name)
		_, known := knownReasons[got]
		assert.True(t, known, "класс отказа %s переведён в значение вне словаря домена", name)
	}
	assert.Equal(t, ReasonNone, reasonFromProto(vpcv1.ApplyFailureReason(9999)),
		"значение вне словаря получило законный перевод")
}

// Подтверждение доезжает до use-case и возвращает записанное.
func TestReportIntentAppliedReturnsWhatWasRecorded(t *testing.T) {
	rec := &fakeRecorder{result: ApplyRecord{Recorded: true, CurrentRevision: 12}}
	h, _ := newHandler(t, &fakeReader{}, rec)

	resp, err := h.ReportIntentApplied(context.Background(), &vpcv1.ReportIntentAppliedRequest{
		ResourceId: "net12345678901234567",
		Revision:   12,
		Outcome:    vpcv1.ApplyOutcome_APPLY_OUTCOME_FAILED,
		Reason:     vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CAPACITY,
	})

	require.NoError(t, err)
	assert.True(t, resp.GetRecorded())
	assert.Equal(t, int64(12), resp.GetCurrentRevision())
	require.Len(t, rec.calls, 1)
	assert.Equal(t, OutcomeFailed, rec.calls[0].Outcome)
	assert.Equal(t, ReasonCapacity, rec.calls[0].Reason)
}
