// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Tuple is one relation tuple. Field names match the OpenFGA wire form.
//
// `Condition` is emitted only when set, so every tuple the five measured shapes
// write goes on the wire byte-for-byte as before: the field exists for the ONE
// probe that has to show what a conditioned tuple does, and its absence must not
// move a single number of the published comparison. Deletes carry no condition —
// the engine's delete key has no such field — and a nil pointer emits nothing.
type Tuple struct {
	User      string          `json:"user"`
	Relation  string          `json:"relation"`
	Object    string          `json:"object"`
	Condition *TupleCondition `json:"condition,omitempty"`
}

// TupleCondition — условие на кортеже: имя из каталога модели и контекст,
// который движок подставляет в CEL.
type TupleCondition struct {
	Name    string         `json:"name"`
	Context map[string]any `json:"context,omitempty"`
}

// Store is one OpenFGA store with one authorization model written into it — the
// isolation boundary a shape gets. Nothing crosses between two stores.
type Store struct {
	stack   *Stack
	ID      string
	ModelID string

	// WritesPerRequest — how many tuples one /write carries. The engine enforces
	// its own ceiling (default 100) and REFUSES an over-cap request rather than
	// trimming it, so this is measured-or-configured, never guessed upward.
	WritesPerRequest int

	hc *http.Client
}

func (s *Stack) NewStore(ctx context.Context, name string, modelJSON []byte) (*Store, error) {
	st := &Store{
		stack:            s,
		WritesPerRequest: 100,
		hc:               &http.Client{Timeout: 120 * time.Second},
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := st.call(ctx, "/stores", map[string]string{"name": name}, &created); err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("create store: empty id")
	}
	st.ID = created.ID

	var body map[string]any
	if err := json.Unmarshal(modelJSON, &body); err != nil {
		return nil, fmt.Errorf("model json: %w", err)
	}
	var wrote struct {
		ID string `json:"authorization_model_id"`
	}
	if err := st.call(ctx, "/stores/"+st.ID+"/authorization-models", body, &wrote); err != nil {
		return nil, fmt.Errorf("write model: %w", err)
	}
	if wrote.ID == "" {
		return nil, fmt.Errorf("write model: empty model id")
	}
	st.ModelID = wrote.ID
	return st, nil
}

// Delete removes the store. Failure is reported, not swallowed: an abandoned store
// keeps its rows in the `tuple` table, and the volume measurement reads that table.
func (st *Store) Delete(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, st.stack.HTTPBase+"/stores/"+st.ID, nil)
	if err != nil {
		return err
	}
	resp, err := st.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("delete store: http %d", resp.StatusCode)
	}
	return nil
}

// call POSTs and decodes. An API-level error (the engine answers 4xx with a
// {code,message} body) is returned as an error carrying the engine's own words —
// a refusal must be distinguishable from a slow success, never folded into it.
func (st *Store) call(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, st.stack.HTTPBase+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := st.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		return &APIError{Status: resp.StatusCode, Code: apiErr.Code, Message: apiErr.Message}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// APIError — the engine refused. Kept as its own type so a caller can tell
// "the store said no" from "the network said nothing", which are different
// outcomes and must not be averaged together.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openfga http %d %s: %s", e.Status, e.Code, e.Message)
}

// ── writes ────────────────────────────────────────────────────────────────────

// WriteTuples writes every tuple, chunked at WritesPerRequest, and returns how
// many round trips it took. The count is reported alongside the duration because a
// write figure without it cannot say whether a shape moved the WORK or only the
// number of requests carrying it.
func (st *Store) WriteTuples(ctx context.Context, tuples []Tuple) (requests int, err error) {
	return st.mutate(ctx, tuples, true)
}

// DeleteTuples is the withdrawal counterpart. Same chunking, same accounting.
func (st *Store) DeleteTuples(ctx context.Context, tuples []Tuple) (requests int, err error) {
	return st.mutate(ctx, tuples, false)
}

func (st *Store) mutate(ctx context.Context, tuples []Tuple, write bool) (int, error) {
	n := st.WritesPerRequest
	if n <= 0 {
		n = 100
	}
	reqs := 0
	for i := 0; i < len(tuples); i += n {
		end := min(i+n, len(tuples))
		chunk := tuples[i:end]
		payload := map[string]any{"authorization_model_id": st.ModelID}
		if write {
			payload["writes"] = map[string]any{"tuple_keys": chunk}
		} else {
			payload["deletes"] = map[string]any{"tuple_keys": chunk}
		}
		reqs++
		if err := st.call(ctx, "/stores/"+st.ID+"/write", payload, nil); err != nil {
			return reqs, err
		}
	}
	return reqs, nil
}

// ── reads ─────────────────────────────────────────────────────────────────────

// Check asks ONE question.
func (st *Store) Check(ctx context.Context, user, relation, object string) (bool, error) {
	return st.CheckWithContext(ctx, user, relation, object, nil)
}

// CheckWithContext asks the same question WITH a request context — the half of a
// conditioned tuple that lives outside the store.
//
// It exists to make one thing demonstrable rather than merely asserted: a
// conditioned tuple answers DIFFERENTLY to two requests that differ only in
// context. A shape whose fact is a row has nowhere to put that half, so the
// difference is a property of the model, not a caveat about it.
func (st *Store) CheckWithContext(ctx context.Context, user, relation, object string,
	reqCtx map[string]any) (bool, error) {
	var out struct {
		Allowed bool `json:"allowed"`
	}
	body := map[string]any{
		"authorization_model_id": st.ModelID,
		"tuple_key":              map[string]string{"user": user, "relation": relation, "object": object},
	}
	if len(reqCtx) > 0 {
		body["context"] = reqCtx
	}
	err := st.call(ctx, "/stores/"+st.ID+"/check", body, &out)
	return out.Allowed, err
}

// BatchCheck asks ONE relation about MANY objects in one request and returns one
// verdict per object IN THE GIVEN ORDER.
//
// The engine keys its answer map by a correlation id, not by position, so the
// answers are re-aligned here. A short or misaligned answer is an ERROR, never a
// page of denials: a caller cannot tell silent denials from a truncated reply, and
// that confusion is the permanent-invisibility defect authzfilter exists to
// prevent (services/iam/internal/authzfilter/visibility.go, BatchObjectChecker).
func (st *Store) BatchCheck(ctx context.Context, user, relation string, objects []string) ([]bool, error) {
	items := make([]map[string]any, 0, len(objects))
	for i, o := range objects {
		items = append(items, map[string]any{
			"tuple_key":      map[string]string{"user": user, "relation": relation, "object": o},
			"correlation_id": fmt.Sprintf("c%d", i),
		})
	}
	var out struct {
		Result map[string]struct {
			Allowed bool `json:"allowed"`
			Error   any  `json:"error"`
		} `json:"result"`
	}
	if err := st.call(ctx, "/stores/"+st.ID+"/batch-check", map[string]any{
		"authorization_model_id": st.ModelID,
		"checks":                 items,
	}, &out); err != nil {
		return nil, err
	}
	res := make([]bool, len(objects))
	for i := range objects {
		v, ok := out.Result[fmt.Sprintf("c%d", i)]
		if !ok {
			return nil, fmt.Errorf("batch-check answer missing correlation c%d of %d — refusing a short answer", i, len(objects))
		}
		if v.Error != nil {
			return nil, fmt.Errorf("batch-check c%d carried an error: %v", i, v.Error)
		}
		res[i] = v.Allowed
	}
	return res, nil
}

// CheckPage resolves a whole page the way production does: partitions of at most
// `partition` objects, at most `parallel` of them in flight.
//
// The two bounds mirror authzfilter.MaxBatchChecksPerRequest (50) and
// BatchParallelism (8). They are parameters rather than constants so the harness
// can show what the shape costs at other settings without pretending the tree's
// numbers changed.
func (st *Store) CheckPage(ctx context.Context, user, relation string, objects []string,
	partition, parallel int) ([]bool, int, error) {
	if partition <= 0 {
		partition = 50
	}
	if parallel <= 0 {
		parallel = 8
	}
	type part struct {
		lo, hi int
	}
	var parts []part
	for i := 0; i < len(objects); i += partition {
		parts = append(parts, part{i, min(i+partition, len(objects))})
	}
	out := make([]bool, len(objects))
	errs := make([]error, len(parts))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for pi, p := range parts {
		wg.Add(1)
		go func(pi int, p part) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := st.BatchCheck(ctx, user, relation, objects[p.lo:p.hi])
			if err != nil {
				errs[pi] = err
				return
			}
			copy(out[p.lo:p.hi], res)
		}(pi, p)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, len(parts), e
		}
	}
	return out, len(parts), nil
}

// probeBatchCap measures the engine's BatchCheck ceiling instead of assuming it.
//
// Doubling ALONE is not a measurement of the cap, it is a measurement of the
// largest accepted power of two — and the first version of this function returned
// 32 against an engine whose ceiling is 50, then handed 32 to the page partition as
// if it were the engine's number. Doubling therefore only BRACKETS the answer; the
// exact value comes from a binary search between the last accepted size and the
// first refused one.
//
// The distinction matters beyond tidiness: the tree carries two claims about this
// ceiling (authzfilter says 50, read off openfga/openfga:v1.14.0; the harness pins
// v1.8.4), so the number this reports is a fact about the engine under test and is
// printed as such rather than compared to a constant.
func (st *Store) probeBatchCap(ctx context.Context, probeObjectType string, from, to int) (int, error) {
	mk := func(n int) []string {
		o := make([]string, n)
		for i := range o {
			o[i] = fmt.Sprintf("%s:probe-%d", probeObjectType, i)
		}
		return o
	}
	accepts := func(n int) (bool, error) {
		if _, err := st.BatchCheck(ctx, "user:probe", "v_get", mk(n)); err != nil {
			var apiErr *APIError
			if asAPIError(err, &apiErr) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	lo, hi := 0, 0 // lo: largest known-accepted, hi: smallest known-refused (0 = none yet)
	for n := from; n <= to; n *= 2 {
		ok, err := accepts(n)
		if err != nil {
			return lo, err
		}
		if !ok {
			hi = n
			break
		}
		lo = n
	}
	if hi == 0 {
		// Never refused inside the search window. Report the largest tried and say so
		// by returning it — the caller prints the number, and a cap at or above `to`
		// is indistinguishable from no cap for every use this harness makes of it.
		return lo, nil
	}
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		ok, err := accepts(mid)
		if err != nil {
			return lo, err
		}
		if ok {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo, nil
}

func asAPIError(err error, target **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*target = e
		return true
	}
	return false
}
