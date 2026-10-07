// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package publicnotice

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/operations"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
)

type fakeNarrower struct {
	visible map[string]bool
	err     error
	asked   []string
}

func (f *fakeNarrower) Visible(_ context.Context, subject, typ, _, relation string, ids []string) ([]string, error) {
	f.asked = append(f.asked, subject+" "+relation+" "+typ+":"+strings.Join(ids, ","))
	if f.err != nil {
		return nil, f.err
	}
	var out []string
	for _, id := range ids {
		if f.visible[typ+":"+id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func tenant() context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-c"})
}

// TestNarrow_OneQuestionPerTypeOrderKept — по пакетному вопросу на каждый
// различный тип; оставшиеся ссылки — в порядке оператора, повтор проверяется
// как каждая (З13 п.3, п.4).
func TestNarrow_OneQuestionPerTypeOrderKept(t *testing.T) {
	t.Parallel()
	n := &fakeNarrower{visible: map[string]bool{"compute_instance:ins-2": true, "vpc_network:net-1": true}}
	refs := []notice.Ref{
		{Type: "compute_instance", ID: "ins-1"}, {Type: "vpc_network", ID: "net-1"},
		{Type: "compute_instance", ID: "ins-2"}, {Type: "compute_instance", ID: "ins-2"},
	}
	got, err := narrow(tenant(), n, refs)
	if err != nil {
		t.Fatal(err)
	}
	want := []notice.Ref{{Type: "vpc_network", ID: "net-1"}, {Type: "compute_instance", ID: "ins-2"}, {Type: "compute_instance", ID: "ins-2"}}
	if !slices.Equal(got, want) {
		t.Fatalf("сужено %v, ожидалось %v", got, want)
	}
	if len(n.asked) != 2 || n.asked[0] != "user:usr-c v_get compute_instance:ins-1,ins-2,ins-2" ||
		n.asked[1] != "user:usr-c v_get vpc_network:net-1" {
		t.Fatalf("вопросы %v", n.asked)
	}
}

// TestNarrow_PeerFailureRefusesInsteadOfEmptying — отказ сужателя — UNAVAILABLE,
// а не пустой список ссылок; близнец — без ссылок вопросов нет.
func TestNarrow_PeerFailureRefusesInsteadOfEmptying(t *testing.T) {
	t.Parallel()
	n := &fakeNarrower{err: errors.New("unreachable")}
	_, err := narrow(tenant(), n, []notice.Ref{{Type: "compute_instance", ID: "ins-1"}})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("отказ %v, ожидался UNAVAILABLE", err)
	}
	quiet := &fakeNarrower{}
	if got, err := narrow(tenant(), quiet, nil); err != nil || got != nil || len(quiet.asked) != 0 {
		t.Fatalf("без ссылок: %v %v вопросы %v", got, err, quiet.asked)
	}
}

// TestScope_RequiredThenForm — пустая область — `<field>: required`; неверная
// форма — `invalid <type> id`.
func TestScope_RequiredThenForm(t *testing.T) {
	t.Parallel()
	if _, err := ProjectScope(""); status.Convert(err).Message() != "project_id: required" {
		t.Fatalf("пустой проект: %v", err)
	}
	if _, err := AccountScope("bad-1"); status.Convert(err).Message() != "invalid account id 'bad-1'" {
		t.Fatalf("неверная форма: %v", err)
	}
	if s, err := ProjectScope("prj-1"); err != nil || s.Type != notice.ScopeProject {
		t.Fatalf("близнец: %v %v", s, err)
	}
}
