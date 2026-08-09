// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import "fmt"

// Form names one of the shapes under comparison.
type Form string

const (
	FormA   Form = "A-flat"          // tuple per object × verb × subject           N·M·S
	FormB   Form = "B-group"         // grant to group#member                       N·M + S
	FormC   Form = "C-role-relation" // one role relation per object, verbs derive  N·S
	FormD   Form = "D-container"     // objects point at a container                N + S
	FormBCD Form = "BCD-combined"    // container + group                           N + S + 1
)

// AllForms in report order: today's shape first, then each fold, then the
// composition. The composition is measured BECAUSE the folds compose — their gains
// need not add, and their costs (graph depth on the read path) do accumulate.
var AllForms = []Form{FormA, FormB, FormC, FormD, FormBCD}

// Fixture ids. Fixed strings, not random: the comparison is between shapes, and a
// difference in ids would be a difference in the data (requirement 3).
const (
	clusterObj = "cluster:cluster_kacho_root"
	accountObj = "account:acc-bench"
	projectObj = "project:prj-bench"
	groupObj   = "group:grp-bench"
	labelObj   = "label_set:lbl-bench"
)

// Scenario is the ONE dataset every shape is measured against.
type Scenario struct {
	N        int      // objects inside the selected set
	Spare    int      // objects OUTSIDE it, kept for the relabel operations
	Verbs    []string // M — the verbs the role grants
	Subjects []string // S — full principal strings, e.g. "user:u000"
	Role     string   // "viewer" | "editor" — selects the C/D grant relation
}

// DefaultVerbs is the verb set 22 of the model's 24 verb-bearing types declare.
func DefaultVerbs() []string { return []string{"v_get", "v_list", "v_update", "v_delete"} }

// AllVerbs is every verb the bench type declares — used by the equivalence probe,
// which must also ask about verbs the role does NOT grant.
func AllVerbs() []string { return []string{"v_get", "v_list", "v_update", "v_delete"} }

// NewScenario builds the dataset. Subjects are `user:` principals because that is
// what a tenant binding names; `service_account:` resolves through the same direct
// userset and would not change the shape of the graph.
func NewScenario(n, spare, subjects int, role string, verbs []string) Scenario {
	subs := make([]string, subjects)
	for i := range subs {
		subs[i] = fmt.Sprintf("user:u%04d", i)
	}
	return Scenario{N: n, Spare: spare, Verbs: verbs, Subjects: subs, Role: role}
}

// Object returns the id of the i-th object. Objects [0,N) are inside the selected
// set; [N, N+Spare) are outside it.
func (sc Scenario) Object(i int) string { return fmt.Sprintf("%s:obj-%06d", BenchType, i) }

// Objects returns the in-set objects.
func (sc Scenario) Objects() []string {
	out := make([]string, sc.N)
	for i := range out {
		out[i] = sc.Object(i)
	}
	return out
}

// Structural returns the parent-pointer tuples. They are IDENTICAL in every shape
// and are therefore excluded from the grant accounting: attributing them to a shape
// would flatter whichever shape has fewest grant tuples.
func (sc Scenario) Structural() []Tuple {
	out := make([]Tuple, 0, sc.N+sc.Spare+2)
	out = append(out,
		Tuple{User: clusterObj, Relation: "cluster", Object: accountObj},
		Tuple{User: accountObj, Relation: "account", Object: projectObj},
	)
	for i := 0; i < sc.N+sc.Spare; i++ {
		out = append(out, Tuple{User: projectObj, Relation: "project", Object: sc.Object(i)})
	}
	return out
}

func (sc Scenario) grantRelation() string { return "grant_" + sc.Role }

// Model returns the DSL each shape needs, derived from the canonical text.
//
// A and B share the canonical model unchanged — that is the point of B: it is a
// discipline of granting, not a model change. C and D each carry one transform.
// BCD reuses D's model because `group#member` is already an accepted subject on the
// container's grant relations.
func ModelFor(f Form, canon string) (dsl string, note string, err error) {
	switch f {
	case FormA, FormB:
		return canon, "canonical, unchanged", nil
	case FormC:
		r, e := ModelC(canon)
		if e != nil {
			return "", "", e
		}
		return r.DSL, fmt.Sprintf("canonical + grant_viewer/grant_editor on %s, %d verbs rewritten", BenchType, len(r.VerbsTouched)), nil
	case FormD, FormBCD:
		r, e := ModelD(canon)
		if e != nil {
			return "", "", e
		}
		return r.DSL, fmt.Sprintf("canonical + type label_set + labels pointer on %s, %d verbs rewritten", BenchType, len(r.VerbsTouched)), nil
	default:
		return "", "", fmt.Errorf("unknown form %q", f)
	}
}

// Grant returns the tuples that materialize the binding over the in-set objects.
func Grant(f Form, sc Scenario) []Tuple {
	rel := sc.grantRelation()
	switch f {
	case FormA:
		out := make([]Tuple, 0, sc.N*len(sc.Verbs)*len(sc.Subjects))
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, v := range sc.Verbs {
				for _, s := range sc.Subjects {
					out = append(out, Tuple{User: s, Relation: v, Object: obj})
				}
			}
		}
		return out
	case FormB:
		out := make([]Tuple, 0, sc.N*len(sc.Verbs)+len(sc.Subjects))
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: "member", Object: groupObj})
		}
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, v := range sc.Verbs {
				out = append(out, Tuple{User: groupObj + "#member", Relation: v, Object: obj})
			}
		}
		return out
	case FormC:
		out := make([]Tuple, 0, sc.N*len(sc.Subjects))
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, s := range sc.Subjects {
				out = append(out, Tuple{User: s, Relation: rel, Object: obj})
			}
		}
		return out
	case FormD:
		out := make([]Tuple, 0, sc.N+len(sc.Subjects))
		for i := 0; i < sc.N; i++ {
			out = append(out, Tuple{User: labelObj, Relation: "labels", Object: sc.Object(i)})
		}
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: rel, Object: labelObj})
		}
		return out
	case FormBCD:
		out := make([]Tuple, 0, sc.N+len(sc.Subjects)+1)
		for i := 0; i < sc.N; i++ {
			out = append(out, Tuple{User: labelObj, Relation: "labels", Object: sc.Object(i)})
		}
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: "member", Object: groupObj})
		}
		out = append(out, Tuple{User: groupObj + "#member", Relation: rel, Object: labelObj})
		return out
	}
	return nil
}

// RelabelOne returns the tuples written when ONE object ENTERS the selected set —
// the "a label changed on one resource" operation.
//
// This is the axis on which the shapes differ most and the one a flat index pays
// worst: A rewrites the whole verb × subject cross-product for that object, D
// writes one pointer.
func RelabelOne(f Form, sc Scenario, obj string) []Tuple {
	rel := sc.grantRelation()
	switch f {
	case FormA:
		out := make([]Tuple, 0, len(sc.Verbs)*len(sc.Subjects))
		for _, v := range sc.Verbs {
			for _, s := range sc.Subjects {
				out = append(out, Tuple{User: s, Relation: v, Object: obj})
			}
		}
		return out
	case FormB:
		out := make([]Tuple, 0, len(sc.Verbs))
		for _, v := range sc.Verbs {
			out = append(out, Tuple{User: groupObj + "#member", Relation: v, Object: obj})
		}
		return out
	case FormC:
		out := make([]Tuple, 0, len(sc.Subjects))
		for _, s := range sc.Subjects {
			out = append(out, Tuple{User: s, Relation: rel, Object: obj})
		}
		return out
	case FormD, FormBCD:
		return []Tuple{{User: labelObj, Relation: "labels", Object: obj}}
	}
	return nil
}

// RelabelMany is RelabelOne applied to K objects — the "mass re-tagging" operation.
func RelabelMany(f Form, sc Scenario, objs []string) []Tuple {
	var out []Tuple
	for _, o := range objs {
		out = append(out, RelabelOne(f, sc, o)...)
	}
	return out
}

// RevokeSubject returns the tuples DELETED when ONE subject loses the binding.
//
// Withdrawal is measured separately from grant on purpose: an additive-only
// materialization path is green on every "was it granted" assertion and wrong on
// exactly the operation revocation exists for (see .claude/rules/testing.md
// §"Параллельный newman", create-vs-update discriminator).
func RevokeSubject(f Form, sc Scenario, subject string) []Tuple {
	rel := sc.grantRelation()
	switch f {
	case FormA:
		out := make([]Tuple, 0, sc.N*len(sc.Verbs))
		for i := 0; i < sc.N; i++ {
			obj := sc.Object(i)
			for _, v := range sc.Verbs {
				out = append(out, Tuple{User: subject, Relation: v, Object: obj})
			}
		}
		return out
	case FormB, FormBCD:
		return []Tuple{{User: subject, Relation: "member", Object: groupObj}}
	case FormC:
		out := make([]Tuple, 0, sc.N)
		for i := 0; i < sc.N; i++ {
			out = append(out, Tuple{User: subject, Relation: rel, Object: sc.Object(i)})
		}
		return out
	case FormD:
		return []Tuple{{User: subject, Relation: rel, Object: labelObj}}
	}
	return nil
}

// ExpectedGrantTuples is the closed-form count each shape predicts, so the measured
// count can be checked against the arithmetic rather than trusted.
func ExpectedGrantTuples(f Form, sc Scenario) int {
	n, m, s := sc.N, len(sc.Verbs), len(sc.Subjects)
	switch f {
	case FormA:
		return n * m * s
	case FormB:
		return n*m + s
	case FormC:
		return n * s
	case FormD:
		return n + s
	case FormBCD:
		return n + s + 1
	}
	return -1
}
