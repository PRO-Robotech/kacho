// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package peercheck

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeProjectCheckClient — minimal ProjectClient stub for the Project
// contract test (exists / absent / peer-down).
type fakeProjectCheckClient struct {
	exists bool
	err    error
}

func (f fakeProjectCheckClient) Exists(context.Context, string) (bool, error) {
	return f.exists, f.err
}

// TestProject_UsesProjectVocabulary locks the error TEXT of the compute →
// iam project existence-check to the Kachō vocabulary.
//
// The resource is `Project` (proto/kaname/cloud/iam/v1/project.proto); `Folder`
// names nothing in the Kachō API. The caller sends `projectId` and must not get
// back an error naming a `Folder`: Kachō describes its API in its own terms, and
// the contract tone is "<Resource> %s not found" (api-conventions.md).
//
// Error CODE is deliberately NOT changed here (NotFound stays NotFound); moving
// this peer-validate lane to FAILED_PRECONDITION is a separate breaking decision.
func TestProject_UsesProjectVocabulary(t *testing.T) {
	const projectID = "prj1234567890abcdefg"

	t.Run("absent project → NotFound with Project vocabulary", func(t *testing.T) {
		err := Project(context.Background(), fakeProjectCheckClient{exists: false}, projectID)
		if err == nil {
			t.Fatal("expected an error for an absent project")
		}
		st, _ := status.FromError(err)
		if st.Code() != codes.NotFound {
			t.Errorf("code = %v, want NotFound (code change is out of scope here)", st.Code())
		}
		want := "Project " + projectID + " not found"
		if st.Message() != want {
			t.Errorf("message = %q, want %q", st.Message(), want)
		}
		if containsFold(st.Message(), "folder") {
			t.Errorf("message %q uses the `Folder` vocabulary — no such resource in the Kachō API", st.Message())
		}
	})

	t.Run("peer down → Unavailable with project vocabulary", func(t *testing.T) {
		err := Project(context.Background(),
			fakeProjectCheckClient{err: errors.New("dial tcp: connection refused")}, projectID)
		if err == nil {
			t.Fatal("expected an error when the peer is unreachable")
		}
		st, _ := status.FromError(err)
		if st.Code() != codes.Unavailable {
			t.Errorf("code = %v, want Unavailable (fail-closed for mutations)", st.Code())
		}
		want := "project check: upstream project service unavailable"
		if st.Message() != want {
			t.Errorf("message = %q, want %q", st.Message(), want)
		}
		if containsFold(st.Message(), "folder") {
			t.Errorf("message %q uses the `Folder` vocabulary — no such resource in the Kachō API", st.Message())
		}
		// The peer error text must not leak through (no transport detail on the wire).
		if containsFold(st.Message(), "connection refused") {
			t.Errorf("peer transport detail leaked into the message %q", st.Message())
		}
	})

	t.Run("existing project → no error", func(t *testing.T) {
		if err := Project(context.Background(),
			fakeProjectCheckClient{exists: true}, projectID); err != nil {
			t.Fatalf("existing project must pass the check, got %v", err)
		}
	})
}

// containsFold — case-insensitive substring check (keeps the assertions readable
// without pulling strings.ToLower into every call site).
func containsFold(haystack, needle string) bool {
	hl, nl := []rune(haystack), []rune(needle)
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	if len(nl) == 0 {
		return true
	}
	for i := 0; i+len(nl) <= len(hl); i++ {
		ok := true
		for j := range nl {
			if lower(hl[i+j]) != lower(nl[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
