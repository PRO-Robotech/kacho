// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type supplyProduct struct {
	Repository, Root, Revision string
	ModuleRoots                []string
}

type supplyPinManifest struct {
	Products                      []supplyProduct
	Owners                        map[string]string
	NetworkSeconds, ChecksSeconds int
}

type supplyPin struct {
	Product, Revision, File, Module, Version, Owner string
	Pseudo                                          bool
	Short, Base                                     string
	Time                                            time.Time
}

func supplyParsePins(raw []byte) (supplyPinManifest, *supplyFailure) {
	result := supplyPinManifest{Owners: map[string]string{}}
	parsed, ok := supplyJSON(raw)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	doc, ok := supplyObject(parsed, "schema_version", "product_trees", "internal_modules", "budgets")
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	if _, ok := supplyInteger(doc["schema_version"], 1, 1); !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	budget, ok := supplyObject(doc["budgets"], "network_seconds", "checks_seconds")
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	result.NetworkSeconds, ok = supplyInteger(budget["network_seconds"], 1, 300)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	result.ChecksSeconds, ok = supplyInteger(budget["checks_seconds"], 1, 7200)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	products, ok := doc["product_trees"].([]any)
	if !ok {
		return result, supplyRed("INPUT_INVALID")
	}
	seen := map[string]bool{}
	for _, value := range products {
		item, ok := supplyObject(value, "repository", "root", "revision", "module_roots")
		if !ok {
			return result, supplyRed("INPUT_INVALID")
		}
		p := supplyProduct{Repository: supplyString(item["repository"]), Root: supplyString(item["root"]), Revision: supplyString(item["revision"])}
		if !supplyRepository.MatchString(p.Repository) || module.CheckPath("github.com/"+p.Repository) != nil ||
			!supplyAbsolute(p.Root) || !supplySHA.MatchString(p.Revision) || seen[p.Repository] {
			return result, supplyRed("INPUT_INVALID")
		}
		p.ModuleRoots, ok = supplyStrings(item["module_roots"], 0, func(s string) bool { return supplyRelative(s, true) })
		if !ok {
			return result, supplyRed("INPUT_INVALID")
		}
		seen[p.Repository] = true
		result.Products = append(result.Products, p)
	}
	owners, ok := doc["internal_modules"].([]any)
	if !ok {
		return result, supplyRed("MODULE_MAPPING_INVALID")
	}
	for _, value := range owners {
		item, ok := supplyObject(value, "module_path", "repository")
		if !ok {
			return result, supplyRed("MODULE_MAPPING_INVALID")
		}
		name, repository := supplyString(item["module_path"]), supplyString(item["repository"])
		if module.CheckPath(name) != nil || !supplyRepository.MatchString(repository) ||
			module.CheckPath("github.com/"+repository) != nil || result.Owners[name] != "" {
			return result, supplyRed("MODULE_MAPPING_INVALID")
		}
		if _, _, ok := supplyPinCoordinates(name, repository); !ok {
			return result, supplyRed("MODULE_MAPPING_INVALID")
		}
		result.Owners[name] = repository
	}
	return result, nil
}

// The map supplies the owner. The module path only checks that explicit
// binding and supplies Go's nested-module tag prefix, never a guessed owner.
func supplyPinCoordinates(name, repository string) (directory, major string, ok bool) {
	base, major, ok := module.SplitPathVersion(name)
	if !ok {
		return "", "", false
	}
	owner := "github.com/" + repository
	if base == owner {
		return ".", major, true
	}
	if strings.HasPrefix(base, owner+"/") {
		directory = strings.TrimPrefix(base, owner+"/")
		return directory, major, supplyRelative(directory, false)
	}
	return "", "", false
}

func supplyPinFailure(current, next *supplyFailure) *supplyFailure {
	if next != nil && (current == nil || (current.Outcome == "NOT_EXECUTED" && next.Outcome == "RED")) {
		return next
	}
	return current
}

func runSupplyPins(ctx context.Context, invocation supplyInvocation, deps SupplyDependencies, report *supplyReport, stdout io.Writer) int {
	raw, err := os.ReadFile(invocation.Manifest)
	if err != nil {
		report.check("input", supplyUnavailable("SOURCE_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	manifest, failure := supplyParsePins(raw)
	if failure != nil {
		report.check("input", failure, nil, nil)
		return report.finish(stdout)
	}
	report.document["input_sha256"] = supplyDigest(raw)
	stage, cancel := context.WithTimeout(ctx, 3600*time.Second)
	defer cancel()
	work, err := os.MkdirTemp("", "release-pins-")
	if err != nil {
		report.check("input", supplyUnavailable("HARNESS_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	defer supplyRemoveOwned(work)
	if deps.Command == nil {
		deps.Command = supplyOSCommand
	}
	engine := supplyEngine{ctx: stage, deps: deps, work: work,
		manifest: supplyManifest{NetworkSeconds: manifest.NetworkSeconds, ChecksSeconds: manifest.ChecksSeconds}}
	identitySubjects := []string{}
	for _, product := range manifest.Products {
		failed := engine.identify(product.Root, product.Repository, product.Revision)
		failure = supplyPinFailure(failure, failed)
		identitySubjects = append(identitySubjects, product.Repository+"@"+product.Revision)
	}
	report.check("identity", failure, identitySubjects, len(manifest.Products))
	if failure != nil {
		return report.finish(stdout)
	}
	pins, modules, files, failure := engine.pinCensus(manifest)
	if failure == nil || failure.Reason == "MODULE_CENSUS_EMPTY" || failure.Reason == "INTERNAL_CENSUS_EMPTY" {
		report.census["modules"], report.census["input_files"] = modules, files
		report.census["internal_requirements"] = len(pins)
		pseudo := 0
		for _, pin := range pins {
			if pin.Pseudo {
				pseudo++
			}
		}
		report.census["pseudo_versions"] = pseudo
	}
	report.check("input", failure, identitySubjects, report.census["modules"])
	if failure != nil {
		return report.finish(stdout)
	}
	owners := map[string]bool{}
	for _, pin := range pins {
		owners[pin.Owner] = true
	}
	remotes, remoteFailures := map[string]supplyPinRemote{}, map[string]*supplyFailure{}
	originSubjects, mainSubjects := []string{}, []string{}
	for index, owner := range supplySortedSet(owners) {
		remote, failed := engine.pinRemote(owner, index)
		remotes[owner], remoteFailures[owner] = remote, failed
		for _, ref := range remote.Refs {
			originSubjects = append(originSubjects, owner+" "+ref)
		}
	}
	var originFailure, mainFailure *supplyFailure
	resolved, mainExamined := 0, 0
	for _, pin := range pins {
		subject := pin.Product + "@" + pin.Revision + ":" + pin.File + " " + pin.Module + "@" + pin.Version + " owner=" + pin.Owner
		remote := remotes[pin.Owner]
		failed := remoteFailures[pin.Owner]
		commit := ""
		if failed == nil {
			commit, failed = engine.resolvePin(pin, remote)
		}
		originFailure = supplyPinFailure(originFailure, failed)
		if failed == nil {
			resolved++
			subject += " commit=" + commit + " GREEN"
		} else {
			subject += " " + failed.Outcome + "/" + failed.Reason
		}
		originSubjects = append(originSubjects, subject)
		if invocation.FinalMain && pin.Pseudo {
			if failed == nil {
				mainExamined++
				failed = engine.pinOnMain(remote, commit)
				mainSubject := pin.Owner + " commit=" + commit + " main=" + remote.Main
				if failed != nil {
					mainSubject += " " + failed.Outcome + "/" + failed.Reason
				}
				mainSubjects = append(mainSubjects, mainSubject)
			} else {
				// An unresolved pin has no ancestry verdict. Preserve the origin
				// failure and record the unanswered dependent question separately.
				failed = supplyUnavailable("SOURCE_UNAVAILABLE")
				mainSubjects = append(mainSubjects, subject)
			}
			mainFailure = supplyPinFailure(mainFailure, failed)
		}
	}
	report.census["resolved_pins"] = resolved
	report.check("pins-origin", originFailure, originSubjects, len(pins))
	if invocation.FinalMain {
		report.check("pins-main", mainFailure, mainSubjects, mainExamined)
	}
	return report.finish(stdout)
}

func (e *supplyEngine) pinCensus(manifest supplyPinManifest) ([]supplyPin, int, int, *supplyFailure) {
	pins := []supplyPin{}
	modules, files := 0, 0
	for _, product := range manifest.Products {
		tracked, failure := e.tracked(product.Root, product.Revision)
		if failure != nil {
			return nil, 0, 0, failure
		}
		roots := []string{}
		for _, file := range tracked {
			if path.Base(file.Path) != "go.mod" {
				continue
			}
			files++
			roots = append(roots, path.Dir(file.Path))
			parsed, err := modfile.Parse(file.Path, file.Data, nil)
			if err != nil || parsed.Module == nil || module.CheckPath(parsed.Module.Mod.Path) != nil {
				return nil, 0, 0, supplyRed("INPUT_INVALID")
			}
			modules++
			for _, requirement := range parsed.Require {
				name, version := requirement.Mod.Path, requirement.Mod.Version
				owner, mapped := manifest.Owners[name]
				if !mapped && !strings.HasPrefix(name, "github.com/PRO-Robotech/") {
					continue
				}
				if !mapped {
					return nil, 0, 0, supplyRed("MODULE_MAPPING_INVALID")
				}
				if module.Check(name, version) != nil || module.CanonicalVersion(version) != version {
					return nil, 0, 0, supplyRed("INPUT_INVALID")
				}
				pin := supplyPin{Product: product.Repository, Revision: product.Revision, File: file.Path,
					Module: name, Version: version, Owner: owner, Pseudo: module.IsPseudoVersion(version)}
				if pin.Pseudo {
					pin.Short, err = module.PseudoVersionRev(version)
					if err != nil || len(pin.Short) != 12 || !supplySHA.MatchString(pin.Short+strings.Repeat("0", 28)) {
						return nil, 0, 0, supplyRed("INPUT_INVALID")
					}
					pin.Time, err = module.PseudoVersionTime(version)
					if err != nil {
						return nil, 0, 0, supplyRed("INPUT_INVALID")
					}
					pin.Base, err = module.PseudoVersionBase(version)
					if err != nil {
						return nil, 0, 0, supplyRed("INPUT_INVALID")
					}
				}
				pins = append(pins, pin)
			}
		}
		sort.Strings(roots)
		if strings.Join(roots, "\x00") != strings.Join(product.ModuleRoots, "\x00") {
			return nil, 0, 0, supplyRed("INPUT_INVALID")
		}
	}
	if modules == 0 {
		return pins, modules, files, supplyRed("MODULE_CENSUS_EMPTY")
	}
	if len(pins) == 0 {
		return pins, modules, files, supplyRed("INTERNAL_CENSUS_EMPTY")
	}
	return pins, modules, files, nil
}

type supplyPinRemote struct {
	Root, Main string
	Refs       []string
}

func (e *supplyEngine) pinRemote(owner string, index int) (supplyPinRemote, *supplyFailure) {
	remote := supplyPinRemote{Root: filepath.Join(e.work, fmt.Sprintf("owner-%d.git", index))}
	if _, failure := e.git(e.work, nil, "init", "--bare", "--quiet", remote.Root); failure != nil {
		return remote, failure
	}
	// Fresh owned storage, no alternates and no shallow options. The only
	// transport coordinate is the explicit exact GitHub owner binding.
	if _, failure := e.git(remote.Root, nil, "fetch", "--force", "--prune", "--no-recurse-submodules", "--no-write-fetch-head",
		"https://github.com/"+owner+".git", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); failure != nil {
		return remote, failure
	}
	shallow, failure := e.git(remote.Root, nil, "rev-parse", "--is-shallow-repository")
	if failure != nil {
		return remote, failure
	}
	if strings.TrimSpace(string(shallow)) != "false" {
		return remote, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	refs, failure := e.git(remote.Root, nil, "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads", "refs/tags")
	if failure != nil {
		return remote, failure
	}
	for _, line := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !supplySHA.MatchString(fields[0]) || (!strings.HasPrefix(fields[1], "refs/heads/") && !strings.HasPrefix(fields[1], "refs/tags/")) {
			return remote, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		remote.Refs = append(remote.Refs, line)
		if fields[1] == "refs/heads/main" {
			remote.Main = fields[0]
		}
	}
	return remote, nil
}

func (e *supplyEngine) pinQuery(root string, args ...string) (SupplyCommandResult, *supplyFailure) {
	args = append([]string{"-c", "core.hooksPath=/dev/null"}, args...)
	return e.command(SupplyCommand{Program: "git", Args: args, Dir: root, Env: supplyEnvironment()},
		time.Duration(e.manifest.NetworkSeconds)*time.Second)
}

func supplyPinTag(pin supplyPin, version string) string {
	directory, _, _ := supplyPinCoordinates(pin.Module, pin.Owner)
	version = strings.TrimSuffix(version, "+incompatible")
	if directory != "." {
		return "refs/tags/" + directory + "/" + version
	}
	return "refs/tags/" + version
}

func (e *supplyEngine) resolvePin(pin supplyPin, remote supplyPinRemote) (string, *supplyFailure) {
	ref := supplyPinTag(pin, pin.Version)
	if pin.Pseudo {
		ref = pin.Short
	}
	resolved, failure := e.pinQuery(remote.Root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if failure != nil {
		return "", failure
	}
	commit := strings.TrimSpace(string(resolved.Stdout))
	if resolved.ExitCode != 0 || !supplySHA.MatchString(commit) || (pin.Pseudo && !strings.HasPrefix(commit, pin.Short)) {
		return "", supplyRed("PIN_UNREACHABLE")
	}
	contains, failure := e.git(remote.Root, nil, "for-each-ref", "--contains="+commit, "--format=%(refname)", "refs/heads", "refs/tags")
	if failure != nil {
		return "", failure
	}
	if len(strings.Fields(string(contains))) == 0 {
		return "", supplyRed("PIN_UNREACHABLE")
	}
	if pin.Pseudo {
		stamp, failed := e.git(remote.Root, nil, "show", "-s", "--format=%ct", commit)
		if failed != nil {
			return "", failed
		}
		seconds, err := strconv.ParseInt(strings.TrimSpace(string(stamp)), 10, 64)
		if err != nil {
			return "", supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if seconds != pin.Time.Unix() {
			return "", supplyRed("PIN_UNREACHABLE")
		}
		if pin.Base != "" {
			base, failed := e.pinQuery(remote.Root, "rev-parse", "--verify", "--end-of-options", supplyPinTag(pin, pin.Base)+"^{commit}")
			if failed != nil {
				return "", failed
			}
			baseSHA := strings.TrimSpace(string(base.Stdout))
			if base.ExitCode != 0 || !supplySHA.MatchString(baseSHA) {
				return "", supplyRed("PIN_UNREACHABLE")
			}
			ancestor, failed := e.pinQuery(remote.Root, "merge-base", "--is-ancestor", baseSHA, commit)
			if failed != nil {
				return "", failed
			}
			if ancestor.ExitCode != 0 {
				return "", supplyRed("PIN_UNREACHABLE")
			}
		}
	}
	return commit, nil
}

func (e *supplyEngine) pinOnMain(remote supplyPinRemote, commit string) *supplyFailure {
	if remote.Main == "" {
		return supplyRed("PIN_NOT_ON_MAIN")
	}
	result, failure := e.pinQuery(remote.Root, "merge-base", "--is-ancestor", commit, remote.Main)
	if failure != nil {
		return failure
	}
	if result.ExitCode == 1 {
		return supplyRed("PIN_NOT_ON_MAIN")
	}
	if result.ExitCode != 0 {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return nil
}
