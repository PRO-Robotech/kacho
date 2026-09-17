// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// SupplyDependencies exposes process and transport boundaries to independent
// holders. Runtime callers cannot select a command through manifest or env.
type SupplyDependencies struct {
	Command func(context.Context, SupplyCommand) SupplyCommandResult
	HTTP    func(context.Context, *http.Request) (*http.Response, error)
	Now     func() time.Time
	Sleep   func(context.Context, time.Duration) error
}

type SupplyCommand struct {
	Program string
	Args    []string
	Dir     string
	Env     []string
	Stdin   []byte
}

type SupplyCommandResult struct {
	Stdout, Stderr []byte
	ExitCode       int
	Err            error
}

type supplyContext struct {
	GOOS, GOARCH string
	CGO          bool
	Tags         []string
}

type supplyConsumer struct {
	Kind, Repository, Root, Revision string
	Candidate                        bool
	ModuleRoots, ProgramPaths        []string
	Contexts                         []supplyContext
}

type supplyManifest struct {
	Repository, ModulePath, Version, CandidateRoot string
	Consumers                                      []supplyConsumer
	NetworkSeconds, ChecksSeconds                  int
}

type supplyFailure struct {
	Outcome, Reason string
}

func supplyRed(reason string) *supplyFailure { return &supplyFailure{"RED", reason} }
func supplyUnavailable(reason string) *supplyFailure {
	return &supplyFailure{"NOT_EXECUTED", reason}
}

var supplySHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var supplyRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var supplyTag = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// Token decoding rejects duplicate keys as well as unknown/missing fields at
// each typed boundary. json.Unmarshal alone would accept duplicate authority.
func supplyJSON(raw []byte) (map[string]any, bool) {
	if !utf8.Valid(raw) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func() (any, bool)
	read = func() (any, bool) {
		token, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		switch token {
		case json.Delim('{'):
			object := map[string]any{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok {
					return nil, false
				}
				if _, exists := object[name]; exists {
					return nil, false
				}
				value, ok := read()
				if !ok {
					return nil, false
				}
				object[name] = value
			}
			end, err := decoder.Token()
			return object, err == nil && end == json.Delim('}')
		case json.Delim('['):
			list := []any{}
			for decoder.More() {
				value, ok := read()
				if !ok {
					return nil, false
				}
				list = append(list, value)
			}
			end, err := decoder.Token()
			return list, err == nil && end == json.Delim(']')
		default:
			_, delimiter := token.(json.Delim)
			return token, !delimiter
		}
	}
	value, ok := read()
	if !ok {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	object, ok := value.(map[string]any)
	return object, ok
}

func supplyObject(value any, keys ...string) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok || len(object) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return nil, false
		}
	}
	return object, true
}

func supplyString(value any) string {
	text, _ := value.(string)
	return text
}

func supplyInteger(value any, minimum, maximum int) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := number.Int64()
	return int(n), err == nil && n >= int64(minimum) && n <= int64(maximum)
}

func supplyRelative(value string, root bool) bool {
	if value == "." {
		return root
	}
	return value != "" && path.Clean(value) == value && !path.IsAbs(value) &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\\x00\r\n") &&
		!strings.HasPrefix(value, "-") && value != ".git" && !strings.HasPrefix(value, ".git/")
}

func supplyStrings(value any, minimum int, valid func(string) bool) ([]string, bool) {
	list, ok := value.([]any)
	if !ok || len(list) < minimum {
		return nil, false
	}
	result := make([]string, len(list))
	for i, item := range list {
		text, ok := item.(string)
		if !ok || !valid(text) || (i > 0 && result[i-1] >= text) {
			return nil, false
		}
		result[i] = text
	}
	return result, true
}

func supplyAbsolute(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

type supplyInvocation struct {
	Mode, Manifest, Revision string
	FinalMain                bool
}

func supplyParseInvocation(args []string) (supplyInvocation, bool) {
	var result supplyInvocation
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if _, duplicate := values[name]; duplicate {
			return result, false
		}
		if name == "--final-main" {
			values[name] = ""
			result.FinalMain = true
			continue
		}
		if (name != "--mode" && name != "--manifest" && name != "--revision") || i+1 >= len(args) || args[i+1] == "" {
			return result, false
		}
		i++
		values[name] = args[i]
	}
	result.Mode, result.Manifest, result.Revision = values["--mode"], values["--manifest"], values["--revision"]
	if result.Manifest == "" {
		return result, false
	}
	switch result.Mode {
	case "consumers", "candidate":
		return result, len(values) == 3 && !result.FinalMain && supplySHA.MatchString(result.Revision)
	case "pins":
		_, hasRevision := values["--revision"]
		return result, !hasRevision && (len(values) == 2 || (result.FinalMain && len(values) == 3))
	default:
		return result, false
	}
}

func supplyParseManifest(raw []byte) (supplyManifest, *supplyFailure) {
	var result supplyManifest
	parsed, ok := supplyJSON(raw)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	doc, ok := supplyObject(parsed, "schema_version", "repository", "module_path", "version", "candidate_root", "consumers", "budgets")
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	if _, ok := supplyInteger(doc["schema_version"], 1, 1); !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	result.Repository, result.ModulePath = supplyString(doc["repository"]), supplyString(doc["module_path"])
	result.Version, result.CandidateRoot = supplyString(doc["version"]), supplyString(doc["candidate_root"])
	if !supplyRepository.MatchString(result.Repository) || !supplyAbsolute(result.CandidateRoot) {
		return result, supplyRed("INPUT_INVALID")
	}
	if semver.Canonical(result.Version) != result.Version || result.Version == "" || module.Check(result.ModulePath, result.Version) != nil {
		return result, supplyRed("VERSION_INVALID")
	}
	moduleBase, _, ok := module.SplitPathVersion(result.ModulePath)
	if !ok || moduleBase != "github.com/"+result.Repository {
		return result, supplyRed("REPOSITORY_MISMATCH")
	}
	budgets, ok := supplyObject(doc["budgets"], "network_seconds", "checks_seconds")
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	result.NetworkSeconds, ok = supplyInteger(budgets["network_seconds"], 1, 300)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	result.ChecksSeconds, ok = supplyInteger(budgets["checks_seconds"], 1, 7200)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	consumers, ok := doc["consumers"].([]any)
	if !ok {
		return result, supplyRed("CONSUMER_DECLARATION_INVALID")
	}
	seen := map[string]bool{}
	for _, value := range consumers {
		consumer, ok := supplyParseConsumer(value)
		if !ok {
			return result, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		identity, _ := json.Marshal(value)
		if seen[string(identity)] {
			return result, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		seen[string(identity)] = true
		result.Consumers = append(result.Consumers, consumer)
	}
	return result, nil
}

func supplyParseConsumer(value any) (supplyConsumer, bool) {
	var result supplyConsumer
	doc, ok := value.(map[string]any)
	if !ok {
		return result, false
	}
	result.Kind = supplyString(doc["type"])
	switch result.Kind {
	case "repository":
		if _, ok := supplyObject(doc, "type", "repository", "root", "revision", "module_roots", "contexts"); !ok {
			return result, false
		}
		result.Repository, result.Root, result.Revision = supplyString(doc["repository"]), supplyString(doc["root"]), supplyString(doc["revision"])
		result.ModuleRoots, ok = supplyStrings(doc["module_roots"], 1, func(s string) bool { return supplyRelative(s, true) })
		if !ok {
			return result, false
		}
	case "external-program":
		if _, ok := supplyObject(doc, "type", "source", "program_paths", "contexts"); !ok {
			return result, false
		}
		source, ok := doc["source"].(map[string]any)
		if !ok {
			return result, false
		}
		if supplyString(source["kind"]) == "candidate" {
			if _, ok := supplyObject(source, "kind"); !ok {
				return result, false
			}
			result.Candidate = true
		} else if supplyString(source["kind"]) == "repository" {
			if _, ok := supplyObject(source, "kind", "repository", "root", "revision"); !ok {
				return result, false
			}
			result.Repository, result.Root, result.Revision = supplyString(source["repository"]), supplyString(source["root"]), supplyString(source["revision"])
		} else {
			return result, false
		}
		result.ProgramPaths, ok = supplyStrings(doc["program_paths"], 1, func(s string) bool {
			return supplyRelative(s, false) && strings.HasSuffix(s, ".go")
		})
		if !ok {
			return result, false
		}
	default:
		return result, false
	}
	if !result.Candidate && (!supplyRepository.MatchString(result.Repository) || !supplyAbsolute(result.Root) || !supplySHA.MatchString(result.Revision)) {
		return result, false
	}
	contexts, ok := doc["contexts"].([]any)
	if !ok || len(contexts) == 0 {
		return result, false
	}
	seen := map[string]bool{}
	for _, value := range contexts {
		item, ok := supplyObject(value, "goos", "goarch", "cgo_enabled", "tags")
		if !ok {
			return result, false
		}
		c := supplyContext{GOOS: supplyString(item["goos"]), GOARCH: supplyString(item["goarch"])}
		c.CGO, ok = item["cgo_enabled"].(bool)
		if !ok || !supplyTag.MatchString(c.GOOS) || !supplyTag.MatchString(c.GOARCH) {
			return result, false
		}
		c.Tags, ok = supplyStrings(item["tags"], 0, supplyTag.MatchString)
		if !ok {
			return result, false
		}
		key, _ := json.Marshal(value)
		if seen[string(key)] {
			return result, false
		}
		seen[string(key)] = true
		result.Contexts = append(result.Contexts, c)
	}
	return result, true
}

func supplyEnvironment(extra ...string) []string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && !strings.HasPrefix(key, "GIT_") {
			values[key] = value
		}
	}
	for _, entry := range append([]string{"GOWORK=off", "GOFLAGS=", "GOENV=off", "GOTOOLCHAIN=local", "GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}, extra...) {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(values))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func supplyOSCommand(ctx context.Context, request SupplyCommand) SupplyCommandResult {
	cmd := exec.CommandContext(ctx, request.Program, request.Args...)
	cmd.Dir, cmd.Env, cmd.Stdin = request.Dir, request.Env, bytes.NewReader(request.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	rc := 0
	if err != nil {
		rc = -1
		if exit, ok := err.(*exec.ExitError); ok {
			rc = exit.ExitCode()
		}
	}
	return SupplyCommandResult{stdout.Bytes(), stderr.Bytes(), rc, err}
}
