// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package standns

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// Запись и воспроизведение замыкания (kacho#3353): суд входов на машине читает ответы
// pod сборки. Воспроизведение обязано отвечать ровно записанным, а на незаписанном
// вопросе и на двух разных ответах о том же — отказывать, а не отвечать пустым.
func TestReplayedGoDepsAnswerExactlyWhatWasRecorded(t *testing.T) {
	live := func(ctx string, mains []string) ([]string, error) {
		if ctx == "broken" {
			return nil, errors.New("go list упал")
		}
		return []string{ctx + "/" + strings.Join(mains, "+") + ".go"}, nil
	}
	var recs []GoDepsRecord
	rec := RecordGoDeps(live, &recs)
	for _, q := range []struct {
		ctx   string
		mains []string
	}{{"", []string{"gateway/cmd/api-gateway"}}, {"services/vpc", []string{"cmd/vpc", "cmd/migrator"}}} {
		if _, err := rec(q.ctx, q.mains); err != nil {
			t.Fatalf("запись %q: %v", q.ctx, err)
		}
	}
	if _, err := rec("broken", []string{"x"}); err == nil {
		t.Fatal("отказ настоящего замыкания при записи потерян")
	}
	if len(recs) != 2 {
		t.Fatalf("записей %d, ждали 2 (отказ не записывается)", len(recs))
	}
	replay := ReplayGoDeps(recs)
	got, err := replay("services/vpc", []string{"cmd/vpc", "cmd/migrator"})
	if err != nil || !slices.Equal(got, []string{"services/vpc/cmd/vpc+cmd/migrator.go"}) {
		t.Fatalf("воспроизведение: %v, %v", got, err)
	}
	// Близнецы: другой порядок mains, другой контекст, отказ при записи — вопросы, которых
	// в записи нет: отказ, а не пустое замыкание.
	for _, q := range []struct {
		ctx   string
		mains []string
	}{{"services/vpc", []string{"cmd/migrator", "cmd/vpc"}}, {"services/geo", []string{"cmd/vpc", "cmd/migrator"}}, {"broken", []string{"x"}}} {
		if got, err := replay(q.ctx, q.mains); err == nil || !strings.Contains(err.Error(), "в записи сборки нет") {
			t.Fatalf("незаписанный вопрос %q %v: %v, %v — ждали отказ", q.ctx, q.mains, got, err)
		}
	}
	conflicting := append(slices.Clone(recs), GoDepsRecord{Ctx: "", Mains: []string{"gateway/cmd/api-gateway"}, Files: []string{"иное.go"}})
	if _, err := ReplayGoDeps(conflicting)("", []string{"gateway/cmd/api-gateway"}); err == nil || !strings.Contains(err.Error(), "записано дважды") {
		t.Fatalf("два разных ответа о том же замыкании: %v — ждали отказ", err)
	}
	same := append(slices.Clone(recs), recs[0])
	if _, err := ReplayGoDeps(same)("", []string{"gateway/cmd/api-gateway"}); err != nil {
		t.Fatalf("законный близнец — тот же ответ дважды: %v", err)
	}
}
