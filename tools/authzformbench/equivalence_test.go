// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Question is one thing asked of every shape.
type Question struct {
	Name    string
	Subject string
	Verb    string
	Object  string
	Want    bool
}

// questionSet builds the questions every shape must answer identically.
//
// The NEGATIVES are the half that decides. A shape that only ever adds tuples is
// green on every positive and wrong on exactly the operation that matters — the
// same asymmetry .claude/rules/testing.md records for the additive fast-path
// (green on "was it emitted", wrong on "was it withdrawn"). So: an outsider, an
// object outside the set, a verb the role does not grant, and — after mutation —
// a revoked subject and an object taken back out.
func questionSet(sc Scenario, phase string) []Question {
	var qs []Question
	inSet := sc.Object(0)
	inSet2 := sc.Object(sc.N - 1)
	outSet := sc.Object(sc.N) // spare — never granted
	granted := map[string]bool{}
	for _, v := range sc.Verbs {
		granted[v] = true
	}

	member := sc.Subjects[0]
	other := sc.Subjects[len(sc.Subjects)-1]
	stranger := "user:stranger-not-in-any-binding"

	for _, v := range AllVerbs() {
		want := granted[v]
		qs = append(qs,
			Question{fmt.Sprintf("%s/member/%s/in-set-first", phase, v), member, v, inSet, want},
			Question{fmt.Sprintf("%s/member/%s/in-set-last", phase, v), member, v, inSet2, want},
			Question{fmt.Sprintf("%s/other/%s/in-set-first", phase, v), other, v, inSet, want},
			// object outside the selected set — never allowed, whatever the verb
			Question{fmt.Sprintf("%s/member/%s/out-of-set", phase, v), member, v, outSet, false},
			// a principal in no binding at all — never allowed
			Question{fmt.Sprintf("%s/stranger/%s/in-set", phase, v), stranger, v, inSet, false},
		)
	}
	return qs
}

// askAll asks every question of one store and returns the verdict vector.
//
// Спрашивается ГРАНИЦА, а не движок: до неё вопросник был неспособен обратиться к
// шестой форме вовсе — то есть форма E не могла быть даже проверена на то,
// отвечает ли она правильно, не то что измерена.
func askAll(ctx context.Context, t *testing.T, st RightsStore, qs []Question) []bool {
	t.Helper()
	out := make([]bool, len(qs))
	for i, q := range qs {
		got, _, err := st.Check(ctx, q.Subject, q.Verb, q.Object)
		require.NoErrorf(t, err, "check %s", q.Name)
		out[i] = got
	}
	return out
}

// TestFormsAnswerIdentically is requirement 8 — behavioural equivalence is the
// PRECONDITION of the comparison, not a footnote to it.
//
// It runs the full lifecycle, not a snapshot: grant → ask; take one object out of
// the set → ask; revoke one subject → ask. A shape must match form A's verdict
// vector at EVERY stage. A shape that is faster and answers differently is not
// faster, it is a different product.
func TestFormsAnswerIdentically(t *testing.T) {
	if testing.Short() {
		t.Skip("real-OpenFGA proof; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	// Two roles, deliberately: `editor` grants all four verbs, `viewer` only two.
	// The partial role is where a role-relation shape (C/D) can silently over-grant,
	// because it folds M and must therefore declare the subset it means.
	for _, role := range []string{"editor", "viewer"} {
		t.Run("role="+role, func(t *testing.T) {
			verbs := DefaultVerbs()
			if role == "viewer" {
				verbs = []string{"v_get", "v_list"}
			}
			sc := NewScenario(6, 3, 3, role, verbs)

			cfg := DefaultConfig()
			cfg.Ns = []int{sc.N}
			cfg.Role = role
			cfg.Verbs = verbs
			r, err := NewRunner(stack, cfg, canon)
			require.NoError(t, err)

			type vec struct {
				after   []bool
				removed []bool
				revoked []bool
			}
			got := map[Form]vec{}

			// Each lifecycle state gets its OWN store rather than being applied to the
			// previous one. The two withdrawals overlap — "object 0 leaves the set" and
			// "subject 0 is revoked" both name the tuple (subject0, verb, object0) in the
			// flat shape — and OpenFGA answers a delete of an absent tuple with a hard
			// 400, not a no-op. Chaining them would therefore measure the harness's
			// ordering rather than the shapes. (The overlap is a real property worth
			// knowing: any withdrawal path that can run twice, or run beside another
			// withdrawal, has to be idempotent by construction — the store will not make
			// it so.)
			seed := func(f Form, name string) RightsStore {
				st, err := r.NewSeededStore(ctx, f, sc, true, name)
				require.NoErrorf(t, err, "seed %s", f)
				return st
			}
			for _, f := range AllForms {
				stG := seed(f, fmt.Sprintf("eq-%s-%s-granted", role, f))
				v := vec{after: askAll(ctx, t, stG, questionSet(sc, "granted"))}
				require.NoError(t, stG.Teardown(ctx))

				// An object LEAVES the selected set. Every shape must stop allowing it.
				stU := seed(f, fmt.Sprintf("eq-%s-%s-unlabelled", role, f))
				_, err := stU.Remove(ctx, RelabelOne(f, sc, sc.Object(0)))
				require.NoErrorf(t, err, "unlabel %s", f)
				v.removed = askAll(ctx, t, stU, questionSet(sc, "unlabelled"))
				require.NoError(t, stU.Teardown(ctx))

				// A subject is REVOKED. Every shape must stop allowing him.
				stR := seed(f, fmt.Sprintf("eq-%s-%s-revoked", role, f))
				_, err = stR.Remove(ctx, RevokeSubject(f, sc, sc.Subjects[0]))
				require.NoErrorf(t, err, "revoke %s", f)
				v.revoked = askAll(ctx, t, stR, questionSet(sc, "revoked"))
				require.NoError(t, stR.Teardown(ctx))

				got[f] = v
			}

			base := got[FormA]
			qs := questionSet(sc, "granted")

			// Form A itself must match the fixture's INTENT, or every other shape would
			// be compared against a wrong reference and all five could agree on nonsense.
			for i, q := range qs {
				require.Equalf(t, q.Want, base.after[i],
					"form A disagrees with the fixture on %q — the reference itself is wrong", q.Name)
			}
			// After the object leaves the set, the two in-set-first questions must flip
			// to deny; if none flips, the withdrawal did nothing and the whole negative
			// half of this proof is vacuous.
			flipped := 0
			for i := range qs {
				if base.after[i] && !base.removed[i] {
					flipped++
				}
			}
			require.Positivef(t, flipped,
				"removing an object from the set changed NO verdict in form A — "+
					"the negative half of this proof would be vacuous")

			for _, f := range AllForms[1:] {
				for i, q := range qs {
					require.Equalf(t, base.after[i], got[f].after[i],
						"%s answers differently after grant on %q", f, q.Name)
					require.Equalf(t, base.removed[i], got[f].removed[i],
						"%s answers differently after the object left the set on %q", f, q.Name)
					require.Equalf(t, base.revoked[i], got[f].revoked[i],
						"%s answers differently after revoke on %q", f, q.Name)
				}
			}
		})
	}
}

// TestFormCCannotExpressAnArbitraryVerbSubset records the price of folding M,
// as a fact rather than as a caveat in prose.
//
// C and D grant a ROLE, not a verb list. The roles they can express are the ones
// their model DECLARES: grant_viewer (get/list) and grant_editor (all four). A role
// that grants, say, {v_get, v_delete} has no relation to be written to — expressing
// it is a MODEL change and a new model id, i.e. a cluster-wide operation, not a
// binding. The proof asks for exactly that subset and requires that neither
// declared relation delivers it.
func TestFormCCannotExpressAnArbitraryVerbSubset(t *testing.T) {
	if testing.Short() {
		t.Skip("real-OpenFGA proof; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	want := []string{"v_get", "v_delete"} // the subset nobody declared
	sc := NewScenario(3, 1, 2, "editor", want)
	cfg := DefaultConfig()
	cfg.Ns = []int{sc.N}
	cfg.Verbs = want
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)

	// Form A expresses it exactly: it grants verb by verb.
	stA, err := r.NewSeededStore(ctx, FormA, sc, true, "subset-A")
	require.NoError(t, err)
	defer func() { _ = stA.Teardown(ctx) }()
	for _, v := range []string{"v_get", "v_delete"} {
		ok, _, err := stA.Check(ctx, sc.Subjects[0], v, sc.Object(0))
		require.NoError(t, err)
		require.Truef(t, ok, "form A must grant %s", v)
	}
	for _, v := range []string{"v_list", "v_update"} {
		ok, _, err := stA.Check(ctx, sc.Subjects[0], v, sc.Object(0))
		require.NoError(t, err)
		require.Falsef(t, ok, "form A must NOT grant %s", v)
	}

	// Форма E выражает тот же произвольный набор ТОЧНО — и это парный
	// положительный к отрицанию ниже: без него «форма C не может» было бы
	// неотличимо от «этого не может никто, и вопрос бессмысленный».
	//
	// Цена свёртки M у формы C — модельная (набор глаголов объявляется
	// отношением, и произвольное подмножество требует нового отношения и нового
	// идентификатора модели, то есть операции уровня кластера). У формы E набор
	// глаголов роли — строки таблицы, и произвольное подмножество выражается
	// выдачей, а не изменением модели.
	stE, err := r.NewSeededStore(ctx, FormE, sc, true, "subset-E")
	require.NoError(t, err)
	defer func() { _ = stE.Teardown(ctx) }()
	var gotE []bool
	for _, v := range AllVerbs() {
		ok, _, cerr := stE.Check(ctx, sc.Subjects[0], v, sc.Object(0))
		require.NoError(t, cerr)
		gotE = append(gotE, ok)
	}
	require.Equal(t, []bool{true, false, false, true}, gotE,
		"форма E обязана выражать произвольное подмножество глаголов {v_get,v_delete} точно — "+
			"иначе отрицание про форму C ниже утверждает не про свёртку M, а про то, что "+
			"подмножество не выразимо вообще")

	// Form C cannot: whichever declared role it writes, the verdict vector differs
	// from the one asked for.
	for _, role := range []string{"viewer", "editor"} {
		scC := sc
		scC.Role = role
		stC, err := r.NewSeededStore(ctx, FormC, scC, true, "subset-C-"+role)
		require.NoError(t, err)
		var got []bool
		for _, v := range AllVerbs() {
			ok, _, cerr := stC.Check(ctx, scC.Subjects[0], v, scC.Object(0))
			require.NoError(t, cerr)
			got = append(got, ok)
		}
		require.NotEqualf(t, []bool{true, false, false, true}, got,
			"form C with role %q happened to express {v_get,v_delete} — "+
				"if a declared role really covers an arbitrary subset this note is wrong "+
				"and the finding must be rewritten, not deleted", role)
		require.NoError(t, stC.Teardown(ctx))
	}
}
