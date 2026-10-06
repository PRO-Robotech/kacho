// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

var supplySHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type supplySourceFile struct{ Path, Mode, SHA256 string }
type supplyPayloadFile struct {
	supplySourceFile
	Component string
}
type supplyCandidate struct {
	supplyManifest
	Base, Baseline, InputDigest                    string
	Input, Owned                                   []supplySourceFile
	Preserve, Replace, Remove, Components, Addenda []string
	Payload                                        []supplyPayloadFile
	Proofs                                         []any
	Producer                                       map[string]any
	Raw                                            map[string]any
}

// JSON object keys are sorted by encoding/json. Optional HTML and JavaScript
// escapes are not part of the agreed compact UTF-8 identity. Literal escaped
// backslashes are left intact, so a path containing six ASCII characters
// "\\u2028" cannot alias the actual Unicode line separator.
func supplyCanonical(value any) []byte {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return nil
	}
	raw := bytes.TrimSuffix(b.Bytes(), []byte{'\n'})
	var out bytes.Buffer
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			if bytes.HasPrefix(raw[i:], []byte(`\u2028`)) {
				out.WriteRune('\u2028')
				i += 5
				continue
			}
			if bytes.HasPrefix(raw[i:], []byte(`\u2029`)) {
				out.WriteRune('\u2029')
				i += 5
				continue
			}
			out.WriteByte(raw[i])
			i++
			out.WriteByte(raw[i])
			continue
		}
		out.WriteByte(raw[i])
	}
	return out.Bytes()
}

func supplyFileEntries(value any, owned bool) ([]supplySourceFile, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	entries := make([]supplySourceFile, 0, len(list))
	for _, raw := range list {
		keys := []string{"path", "mode", "sha256"}
		if owned {
			keys = append(keys, "reason")
		}
		item, ok := supplyObject(raw, keys...)
		if !ok {
			return nil, false
		}
		f := supplySourceFile{supplyString(item["path"]), supplyString(item["mode"]), supplyString(item["sha256"])}
		if !supplyRelative(f.Path, false) || (f.Mode != "100644" && f.Mode != "100755") || !supplySHA256.MatchString(f.SHA256) ||
			(owned && strings.TrimSpace(supplyString(item["reason"])) == "") || (len(entries) > 0 && entries[len(entries)-1].Path >= f.Path) {
			return nil, false
		}
		entries = append(entries, f)
	}
	return entries, true
}

func supplyParseCandidate(raw []byte) (supplyCandidate, *supplyFailure) {
	var c supplyCandidate
	doc, ok := supplyJSON(raw)
	if !ok {
		return c, supplyRed("INPUT_INVALID")
	}
	keys := []string{"schema_version", "repository", "module_path", "version", "base_sha", "baseline_version", "input_tree_digest", "input_files", "receiving_owned", "preserve_paths", "replace_paths", "remove_paths", "candidate_root", "consumers", "product_trees", "internal_modules", "components", "ci_dt_addenda", "payload", "proofs", "producer_execution", "budgets"}
	if _, ok := supplyObject(doc, keys...); !ok {
		return c, supplyRed("INPUT_INVALID")
	}
	common := map[string]any{}
	for _, key := range []string{"schema_version", "repository", "module_path", "version", "candidate_root", "consumers", "budgets"} {
		common[key] = doc[key]
	}
	var failure *supplyFailure
	c.supplyManifest, failure = supplyParseManifest(supplyCanonical(common))
	if failure != nil {
		return c, failure
	}
	pinFields := map[string]any{}
	for _, key := range []string{"schema_version", "product_trees", "internal_modules", "budgets"} {
		pinFields[key] = doc[key]
	}
	if _, failure = supplyParsePins(supplyCanonical(pinFields)); failure != nil {
		return c, failure
	}
	c.Base, c.Baseline, c.InputDigest = supplyString(doc["base_sha"]), supplyString(doc["baseline_version"]), supplyString(doc["input_tree_digest"])
	if !supplySHA.MatchString(c.Base) || !supplySHA256.MatchString(c.InputDigest) {
		return c, supplyRed("INPUT_INVALID")
	}
	if c.Baseline == "" || semver.Canonical(c.Baseline) != c.Baseline || module.Check(c.ModulePath, c.Baseline) != nil || semver.Major(c.Baseline) != semver.Major(c.Version) {
		return c, supplyRed("VERSION_INVALID")
	}
	c.Input, ok = supplyFileEntries(doc["input_files"], false)
	if !ok || len(c.Input) == 0 {
		return c, supplyRed("INPUT_INVALID")
	}
	c.Owned, ok = supplyFileEntries(doc["receiving_owned"], true)
	if !ok {
		return c, supplyRed("INPUT_INVALID")
	}
	paths := func(s string) bool { return supplyRelative(s, false) }
	c.Preserve, ok = supplyStrings(doc["preserve_paths"], 0, paths)
	if !ok {
		return c, supplyRed("INPUT_INVALID")
	}
	c.Replace, ok = supplyStrings(doc["replace_paths"], 0, paths)
	if !ok {
		return c, supplyRed("INPUT_INVALID")
	}
	c.Remove, ok = supplyStrings(doc["remove_paths"], 0, paths)
	if !ok {
		return c, supplyRed("INPUT_INVALID")
	}
	c.Components, ok = supplyStrings(doc["components"], 0, func(s string) bool { return s == "CI-NP-1" || s == "CI-DT-1" })
	if !ok {
		return c, supplyRed("PAYLOAD_MISMATCH")
	}
	c.Addenda, ok = supplyStrings(doc["ci_dt_addenda"], 0, func(s string) bool { return s == "2590" })
	if !ok {
		return c, supplyRed("PAYLOAD_MISMATCH")
	}
	if len(c.Addenda) > 0 && !supplyContains(c.Components, "CI-DT-1") {
		return c, supplyRed("PAYLOAD_MISMATCH")
	}
	payload, ok := doc["payload"].([]any)
	if !ok {
		return c, supplyRed("PAYLOAD_MISMATCH")
	}
	for _, raw := range payload {
		item, ok := supplyObject(raw, "path", "mode", "sha256", "component")
		if !ok {
			return c, supplyRed("PAYLOAD_MISMATCH")
		}
		files, ok := supplyFileEntries([]any{map[string]any{"path": item["path"], "mode": item["mode"], "sha256": item["sha256"]}}, false)
		component := supplyString(item["component"])
		if !ok || !supplyContains(c.Components, component) || (len(c.Payload) > 0 && c.Payload[len(c.Payload)-1].Path >= files[0].Path) {
			return c, supplyRed("PAYLOAD_MISMATCH")
		}
		c.Payload = append(c.Payload, supplyPayloadFile{files[0], component})
	}
	c.Proofs, ok = doc["proofs"].([]any)
	if !ok {
		return c, supplyRed("PAYLOAD_MISMATCH")
	}
	c.Producer, ok = doc["producer_execution"].(map[string]any)
	if !ok {
		return c, supplyRed("INVALID_CONFIRMATION")
	}
	c.Raw = doc
	return c, nil
}

func supplyContains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func supplyFileMatches(file supplySourceFile, tracked supplyTrackedFile) bool {
	return file.Path == tracked.Path && file.Mode == tracked.Mode && file.SHA256 == supplyDigest(tracked.Data)
}

func supplyTrackedMap(files []supplyTrackedFile) map[string]supplyTrackedFile {
	result := map[string]supplyTrackedFile{}
	for _, file := range files {
		result[file.Path] = file
	}
	return result
}
