// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

type supplyPolicy struct {
	Main     string
	Required map[string]int
}
type supplyPR struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	Body      string `json:"body"`
	Merged    bool   `json:"merged"`
	MergeSHA  string `json:"merge_commit_sha"`
	Mergeable *bool  `json:"mergeable"`
	Head      struct {
		Ref, SHA string
		Repo     struct {
			FullName string `json:"full_name"`
		}
	} `json:"head"`
	Base struct {
		Ref, SHA string
		Repo     struct {
			FullName string `json:"full_name"`
		}
	} `json:"base"`
}

func supplyRuntimeHTTP(ctx context.Context, r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || (r.URL.Host != "api.github.com" && r.URL.Host != "proxy.golang.org") || r.URL.User != nil {
		return nil, fmt.Errorf("unsupported release endpoint")
	}
	copy := r.Clone(ctx)
	copy.Header = r.Header.Clone()
	if copy.URL.Host == "api.github.com" {
		token, err := supplyGitHubToken(ctx)
		if err != nil {
			return nil, err
		}
		copy.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client.Do(copy)
}

// Existing user credentials stay inside the runtime HTTP adapter. Credential
// stdout/stderr never enter the command evidence seam or public diagnostics.
func supplyGitHubToken(ctx context.Context) (string, error) {
	unavailable := func() (string, error) { return "", fmt.Errorf("GitHub authorization unavailable") }
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com")
		for _, entry := range supplyPublisherEnvironment() {
			key, _, _ := strings.Cut(entry, "=")
			if key != "GH_PROMPT_DISABLED" {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "GH_PROMPT_DISABLED=1")
		var private bytes.Buffer
		cmd.Stdout, cmd.Stderr = &private, io.Discard
		cmd.WaitDelay = time.Second
		if cmd.Run() != nil {
			return unavailable()
		}
		// gh emits one trailing LF; arbitrary whitespace is not token framing.
		token = strings.TrimSuffix(private.String(), "\n")
	}
	if token == "" {
		return unavailable()
	}
	for _, b := range []byte(token) {
		if b < 33 || b > 126 {
			return unavailable()
		}
	}
	return token, nil
}

// A read owns one deadline over all attempts. A mutation uses one attempt;
// the caller must establish its outcome using a separate bounded readback.
func (p *supplyPublisher) http(method, address string, value any) (int, []byte, *supplyFailure) {
	raw := []byte{}
	if value != nil {
		raw = supplyCanonical(value)
	}
	ctx, cancel := context.WithTimeout(p.e.ctx, time.Duration(p.c.NetworkSeconds)*time.Second)
	defer cancel()
	deadline := p.e.deps.Now().Add(time.Duration(p.c.NetworkSeconds) * time.Second)
	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(raw))
		if err != nil {
			return 0, nil, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		response, callErr := p.e.deps.HTTP(ctx, request)
		status := 0
		var body []byte
		var readErr error
		if response != nil {
			status = response.StatusCode
			if response.Body != nil {
				body, readErr = io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr == nil {
					readErr = closeErr
				}
			} else {
				readErr = io.ErrUnexpectedEOF
			}
		}
		if ctx.Err() != nil || !p.e.deps.Now().Before(deadline) {
			return status, body, supplyUnavailable("BUDGET_EXHAUSTED")
		}
		if callErr == nil && readErr == nil && status > 0 && (status < 500 && status != 429) {
			return status, body, nil
		}
		if attempt+1 < attempts {
			delay := time.Duration(attempt+1) * time.Second
			if remain := deadline.Sub(p.e.deps.Now()); delay > remain {
				delay = remain
			}
			if delay <= 0 || p.e.deps.Sleep(ctx, delay) != nil {
				return status, body, supplyUnavailable("BUDGET_EXHAUSTED")
			}
		}
	}
	return 0, nil, supplyUnavailable("SOURCE_UNAVAILABLE")
}
func (p *supplyPublisher) api(path string) string {
	return "https://api.github.com/repos/" + p.c.Repository + path
}
func (p *supplyPublisher) readPolicy() (supplyPolicy, *supplyFailure) {
	result := supplyPolicy{Required: map[string]int{}}
	status, raw, f := p.http("GET", p.api(""), nil)
	if f != nil {
		return result, f
	}
	var repo struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if status != 200 || json.Unmarshal(raw, &repo) != nil {
		return result, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if repo.FullName != p.c.Repository {
		return result, supplyRed("REPOSITORY_MISMATCH")
	}
	if repo.DefaultBranch != "main" {
		return result, supplyRed("PR_REQUIRED")
	}
	status, raw, f = p.http("GET", p.api("/branches/main"), nil)
	if f != nil {
		return result, f
	}
	var branch struct {
		Name      string
		Protected bool
		Commit    struct{ SHA string }
	}
	if status != 200 || json.Unmarshal(raw, &branch) != nil || !supplySHA.MatchString(branch.Commit.SHA) {
		return result, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if branch.Name != "main" || !branch.Protected {
		return result, supplyRed("PR_REQUIRED")
	}
	result.Main = branch.Commit.SHA
	status, raw, f = p.http("GET", p.api("/branches/main/protection"), nil)
	if f != nil {
		return result, f
	}
	var protection struct {
		RequiredStatusChecks *struct {
			Contexts []string
			Checks   []struct {
				Context string
				AppID   int `json:"app_id"`
			}
		} `json:"required_status_checks"`
		Reviews json.RawMessage `json:"required_pull_request_reviews"`
	}
	if status != 200 || json.Unmarshal(raw, &protection) != nil || protection.RequiredStatusChecks == nil || len(protection.Reviews) == 0 || string(protection.Reviews) == "null" {
		return result, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	for _, name := range protection.RequiredStatusChecks.Contexts {
		if name == "" {
			return result, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		result.Required[name] = 0
	}
	for _, check := range protection.RequiredStatusChecks.Checks {
		if check.Context == "" || check.AppID < -1 {
			return result, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if old, ok := result.Required[check.Context]; ok && old > 0 && old != check.AppID {
			return result, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		result.Required[check.Context] = check.AppID
	}
	if len(result.Required) == 0 {
		return result, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	p.report.census["required_checks"] = len(result.Required)
	p.snapshot("base", result.Main, false)
	return result, nil
}
func (p *supplyPublisher) snapshot(point, main string, ancestor bool) {
	p.report.document["snapshots"] = append(p.report.document["snapshots"].([]any), map[string]any{"point": point, "main_sha": main, "observed_at": p.e.deps.Now().UTC().Format(time.RFC3339Nano), "target_is_ancestor": ancestor})
}
func (p *supplyPublisher) branch() string { return "release/module-" + p.plan }
func (p *supplyPublisher) marker() string { return "CI-RS-1 plan-sha256:" + p.plan }
func (p *supplyPublisher) validatePR(pr *supplyPR) *supplyFailure {
	if pr.Number <= 0 || pr.Head.Ref != p.branch() || pr.Base.Ref != "main" || pr.Head.Repo.FullName != p.c.Repository || pr.Base.Repo.FullName != p.c.Repository || !supplyHasPlanMarker(pr.Body, p.marker()) {
		return supplyRed("EFFECT_IDENTITY_CONFLICT")
	}
	if !supplySHA.MatchString(pr.Head.SHA) || pr.Head.SHA != p.revision {
		return supplyRed("PR_HEAD_CHANGED")
	}
	if pr.Merged && !supplySHA.MatchString(pr.MergeSHA) {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if !pr.Merged && pr.State != "open" {
		return supplyRed("EFFECT_IDENTITY_CONFLICT")
	}
	return nil
}
func (p *supplyPublisher) findPR() (*supplyPR, *supplyFailure) {
	found := []supplyPR{}
	for page := 1; page <= 10; page++ {
		q := url.Values{"state": {"all"}, "head": {strings.Split(p.c.Repository, "/")[0] + ":" + p.branch()}, "base": {"main"}, "per_page": {"100"}, "page": {fmt.Sprint(page)}}
		status, raw, f := p.http("GET", p.api("/pulls?"+q.Encode()), nil)
		if f != nil {
			return nil, f
		}
		var list []supplyPR
		if status != 200 || json.Unmarshal(raw, &list) != nil {
			return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		for _, pr := range list {
			if pr.Head.Ref == p.branch() {
				found = append(found, pr)
			}
		}
		if len(list) < 100 {
			if len(found) > 1 {
				return nil, supplyRed("EFFECT_IDENTITY_CONFLICT")
			}
			if len(found) == 0 {
				return nil, nil
			}
			pr := found[0]
			return &pr, p.validatePR(&pr)
		}
	}
	return nil, supplyUnavailable("BUDGET_EXHAUSTED")
}
func (p *supplyPublisher) readPR(number int) (*supplyPR, *supplyFailure) {
	status, raw, f := p.http("GET", p.api(fmt.Sprintf("/pulls/%d", number)), nil)
	if f != nil {
		return nil, f
	}
	var pr supplyPR
	if status != 200 || json.Unmarshal(raw, &pr) != nil {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return &pr, p.validatePR(&pr)
}

func (p *supplyPublisher) checks(sha string) *supplyFailure {
	ctx, cancel := context.WithTimeout(p.e.ctx, time.Duration(p.c.ChecksSeconds)*time.Second)
	defer cancel()
	local := *p
	engine := *p.e
	engine.ctx = ctx
	local.e = &engine
	deadline := p.e.deps.Now().Add(time.Duration(p.c.ChecksSeconds) * time.Second)
	for {
		if ctx.Err() != nil || !p.e.deps.Now().Before(deadline) {
			return supplyUnavailable("BUDGET_EXHAUSTED")
		}

		state := map[string]string{}
		for page := 1; page <= 10; page++ {
			status, raw, f := local.http("GET", p.api(fmt.Sprintf("/commits/%s/check-runs?per_page=100&page=%d", sha, page)), nil)
			if f != nil {
				return f
			}
			var result struct {
				TotalCount int `json:"total_count"`
				Runs       []struct {
					ID                 int
					Name               string
					HeadSHA            string `json:"head_sha"`
					Status, Conclusion string
					App                struct{ ID int }
				} `json:"check_runs"`
			}
			if status != 200 || json.Unmarshal(raw, &result) != nil {
				return supplyUnavailable("SOURCE_UNAVAILABLE")
			}
			for _, run := range result.Runs {
				owner, required := p.policy.Required[run.Name]
				if !required {
					continue
				}
				if run.HeadSHA != sha {
					return supplyRed("REQUIRED_CHECKS_FAILED")
				}
				if owner > 0 && run.App.ID != owner {
					continue
				}
				verdict := "pending"
				if run.Status == "completed" {
					verdict = "failed"
					if run.Conclusion == "success" {
						verdict = "success"
					}
				}
				if old := state[run.Name]; old == "failed" || verdict == "failed" {
					state[run.Name] = "failed"
				} else if old == "pending" || verdict == "pending" {
					state[run.Name] = "pending"
				} else {
					state[run.Name] = verdict
				}
			}
			if page*100 >= result.TotalCount {
				break
			}
			if len(result.Runs) == 0 || page == 10 {
				return supplyUnavailable("SOURCE_UNAVAILABLE")
			}
		}
		status, raw, f := local.http("GET", p.api("/commits/"+sha+"/status?per_page=100"), nil)
		if f != nil {
			return f
		}
		var combined struct {
			SHA        string
			TotalCount int `json:"total_count"`
			Statuses   []struct{ Context, State string }
		}
		if status != 200 || json.Unmarshal(raw, &combined) != nil || combined.SHA != sha || combined.TotalCount > len(combined.Statuses) {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		for _, s := range combined.Statuses {
			owner, required := p.policy.Required[s.Context]
			if !required || owner > 0 {
				continue
			}
			if _, exists := state[s.Context]; exists {
				continue
			}
			switch s.State {
			case "success":
				state[s.Context] = "success"
			case "pending":
				state[s.Context] = "pending"
			default:
				state[s.Context] = "failed"
			}
		}
		pending := false
		for name := range p.policy.Required {
			if state[name] == "failed" {
				return supplyRed("REQUIRED_CHECKS_FAILED")
			}
			if state[name] != "success" {
				pending = true
			}
		}
		if !pending {
			subjects := []string{sha}
			for _, name := range supplySortedKeys(p.policy.Required) {
				subjects = append(subjects, name)
			}
			p.pass("required-checks", subjects, len(p.policy.Required))
			return nil
		}
		remaining := deadline.Sub(p.e.deps.Now())
		if remaining <= 0 {
			return supplyUnavailable("BUDGET_EXHAUSTED")
		}
		delay := 5 * time.Second
		if delay > remaining {
			delay = remaining
		}
		if p.e.deps.Sleep(ctx, delay) != nil {
			return supplyUnavailable("BUDGET_EXHAUSTED")
		}
	}
}
func supplySortedKeys(m map[string]int) []string {
	set := map[string]bool{}
	for k := range m {
		set[k] = true
	}
	return supplySortedSet(set)
}

func supplyHasPlanMarker(body, marker string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}
