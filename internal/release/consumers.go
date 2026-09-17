// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	goversion "go/version"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

type supplyReport struct {
	document map[string]any
	checks   []map[string]any
	census   map[string]any
}

func supplyNewReport() *supplyReport {
	census := map[string]any{}
	for _, key := range []string{"input_files", "preserved_files", "previous_packages", "candidate_packages", "consumers", "consumer_imports", "modules", "internal_requirements", "pseudo_versions", "resolved_pins", "required_checks", "payload_files"} {
		census[key] = nil
	}
	return &supplyReport{document: map[string]any{
		"schema_version": 1, "phase": "plan", "outcome": "GREEN", "reason": "OK", "exit_code": 0,
		"dry_run": true, "stage": "NONE", "repository": nil, "version": nil, "plan_sha256": nil,
		"input_sha256": nil, "candidate_sha": nil, "tag_target_sha": nil,
		"snapshots": []any{}, "effects": []any{}, "census": census,
	}, checks: []map[string]any{}, census: census}
}

func (r *supplyReport) check(predicate string, failure *supplyFailure, subjects []string, examined any) {
	if subjects == nil {
		subjects = []string{}
	}
	outcome, reason := "GREEN", "OK"
	if failure != nil {
		outcome, reason = failure.Outcome, failure.Reason
		current := r.document["outcome"]
		if current == "GREEN" || (current == "NOT_EXECUTED" && outcome == "RED") {
			r.document["outcome"], r.document["reason"] = outcome, reason
		}
	}
	r.checks = append(r.checks, map[string]any{"predicate": predicate, "outcome": outcome,
		"reason": reason, "subjects": subjects, "examined": examined})
}

func (r *supplyReport) finish(stdout io.Writer) int {
	rc := map[string]int{"GREEN": 0, "RED": 1, "USAGE_ERROR": 2, "NOT_EXECUTED": 3}[r.document["outcome"].(string)]
	r.document["exit_code"], r.document["checks"] = rc, r.checks
	if err := json.NewEncoder(stdout).Encode(r.document); err != nil {
		return 3
	}
	return rc
}

// RunSupplyPreflight implements the read-only declared-consumers boundary.
// Candidate, pins, publisher phases and legacy P9 wiring have separate source
// transitions; an invocation of those modes cannot acquire a consumers permit.
func RunSupplyPreflight(ctx context.Context, args []string, deps SupplyDependencies, stdout, stderr io.Writer) int {
	report := supplyNewReport()
	arguments := map[string]string{}
	valid := len(args)%2 == 0
	for i := 0; valid && i < len(args); i += 2 {
		flag := args[i]
		if flag != "--mode" && flag != "--manifest" && flag != "--revision" {
			valid = false
			break
		}
		if _, duplicate := arguments[flag]; duplicate || args[i+1] == "" {
			valid = false
			break
		}
		arguments[flag] = args[i+1]
	}
	if !valid || len(arguments) != 3 || arguments["--mode"] != "consumers" || !supplySHA.MatchString(arguments["--revision"]) {
		report.check("invocation", &supplyFailure{"USAGE_ERROR", "INVALID_INVOCATION"}, nil, nil)
		return report.finish(stdout)
	}
	report.document["phase"] = "consumers"
	report.check("invocation", nil, []string{"consumers"}, 1)
	raw, err := os.ReadFile(arguments["--manifest"])
	if err != nil {
		report.check("input", supplyUnavailable("SOURCE_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	manifest, failure := supplyParseManifest(raw)
	if failure != nil {
		report.check("input", failure, nil, nil)
		return report.finish(stdout)
	}
	report.document["repository"], report.document["version"] = manifest.Repository, manifest.Version
	report.document["input_sha256"] = supplyDigest(raw)
	stage, cancel := context.WithTimeout(ctx, 3600*time.Second)
	defer cancel()
	work, err := os.MkdirTemp("", "release-consumers-")
	if err != nil {
		report.check("input", supplyUnavailable("HARNESS_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	defer supplyRemoveOwned(work)
	if deps.Command == nil {
		deps.Command = supplyOSCommand
	}
	engine := supplyEngine{ctx: stage, deps: deps, manifest: manifest, work: work}
	revision := arguments["--revision"]
	failure = engine.identify(manifest.CandidateRoot, manifest.Repository, revision)
	report.check("identity", failure, []string{manifest.Repository, revision}, 1)
	if failure != nil {
		return report.finish(stdout)
	}
	archive, failure := engine.archive(revision)
	if failure != nil {
		report.check("input", failure, []string{manifest.Repository, revision}, nil)
		return report.finish(stdout)
	}
	report.document["candidate_sha"] = revision
	report.census["input_files"] = len(archive.Files)
	report.check("input", nil, []string{manifest.Repository, revision, archive.Digest}, len(archive.Files))
	report.census["consumers"] = len(manifest.Consumers)
	if len(manifest.Consumers) == 0 {
		report.census["consumer_imports"] = 0
		report.check("consumer-census", supplyRed("CONSUMER_CENSUS_EMPTY"), nil, 0)
		return report.finish(stdout)
	}
	if failure = engine.dependencies(); failure != nil {
		report.check("consumer-census", failure, nil, nil)
		return report.finish(stdout)
	}
	supported, failure := engine.platforms()
	if failure != nil {
		report.check("consumer-census", failure, nil, nil)
		return report.finish(stdout)
	}
	prepared := []supplyPrepared{}
	var censusFailure *supplyFailure
	totalImports := 0
	subjects := []string{}
	for i, consumer := range manifest.Consumers {
		p, failed := engine.prepareConsumer(consumer, revision, i, archive.GoMod, supported)
		if failed != nil {
			if censusFailure == nil || (censusFailure.Outcome == "NOT_EXECUTED" && failed.Outcome == "RED") {
				censusFailure = failed
			}
			continue
		}
		prepared = append(prepared, p)
		totalImports += len(p.Imports)
		subjects = append(subjects, p.Repository+"@"+p.Revision)
	}
	if censusFailure == nil {
		report.census["consumer_imports"] = totalImports
		for _, p := range prepared {
			if len(p.Imports) == 0 {
				censusFailure = supplyRed("CONSUMER_CENSUS_EMPTY")
			}
		}
	}
	report.check("consumer-census", censusFailure, subjects, report.census["consumer_imports"])
	if censusFailure != nil {
		return report.finish(stdout)
	}
	missing := []string{}
	for _, p := range prepared {
		for _, imported := range p.Imports {
			if !archive.Packages[imported] {
				missing = append(missing, p.Repository+"@"+p.Revision+":"+imported)
			}
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		report.check("consumer-archive", supplyRed("CONSUMER_IMPORT_MISSING"), missing, totalImports)
		return report.finish(stdout)
	}
	var buildFailure *supplyFailure
	for _, p := range prepared {
		for _, c := range p.Contexts {
			failed := engine.buildConsumer(p, c)
			if failed != nil && (buildFailure == nil || (buildFailure.Outcome == "NOT_EXECUTED" && failed.Outcome == "RED")) {
				buildFailure = failed
			}
		}
	}
	report.check("consumer-archive", buildFailure, append(subjects, archive.Digest), totalImports)
	return report.finish(stdout)
}

func (e *supplyEngine) platforms() (map[string]bool, *supplyFailure) {
	result, failure := e.command(SupplyCommand{Program: e.goBinary, Args: []string{"tool", "dist", "list"}, Dir: e.work,
		Env: supplyEnvironment("GOPROXY=" + e.proxy)}, 600*time.Second)
	if failure != nil {
		return nil, failure
	}
	if result.ExitCode != 0 {
		return nil, supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	platforms := map[string]bool{}
	for _, name := range strings.Fields(string(result.Stdout)) {
		platforms[name] = true
	}
	if len(platforms) == 0 {
		return nil, supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	return platforms, nil
}

type supplyPreparedContext struct {
	Context supplyContext
	Modules []supplyBuildModule
}

type supplyBuildModule struct {
	Root, ModulePath string
	Packages         []string
	Imports          []string
}

type supplyPrepared struct {
	Repository, Revision string
	Imports              []string
	Contexts             []supplyPreparedContext
}

type supplyGoSource struct {
	Path, Root string
	Imports    []string
}

func (e *supplyEngine) prepareConsumer(consumer supplyConsumer, candidate string, index int, target *modfile.File, supported map[string]bool) (supplyPrepared, *supplyFailure) {
	var prepared supplyPrepared
	if consumer.Candidate {
		consumer.Root, consumer.Repository, consumer.Revision = e.manifest.CandidateRoot, e.manifest.Repository, candidate
	}
	prepared.Repository, prepared.Revision = consumer.Repository, consumer.Revision
	if failure := e.identify(consumer.Root, consumer.Repository, consumer.Revision); failure != nil {
		return prepared, failure
	}
	files, failure := e.tracked(consumer.Root, consumer.Revision)
	if failure != nil {
		if failure.Reason == "INPUT_INVALID" {
			failure = supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		return prepared, failure
	}
	fileMap := map[string]supplyTrackedFile{}
	moduleFiles := map[string]*modfile.File{}
	for _, file := range files {
		fileMap[file.Path] = file
		if path.Base(file.Path) == "go.mod" {
			parsed, err := modfile.Parse(file.Path, file.Data, nil)
			if err != nil || parsed.Module == nil {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
			moduleFiles[path.Dir(file.Path)] = parsed
		}
	}
	root := filepath.Join(e.work, fmt.Sprintf("consumer-%d", index))
	selected := []supplyTrackedFile{}
	roots := append([]string(nil), consumer.ModuleRoots...)
	programBase := "."
	if consumer.Kind == "external-program" {
		programBase = path.Dir(consumer.ProgramPaths[0])
		for _, name := range consumer.ProgramPaths {
			for programBase != "." && name != programBase && !strings.HasPrefix(name, programBase+"/") {
				programBase = path.Dir(programBase)
			}
			file, exists := fileMap[name]
			if !exists {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
			selected = append(selected, file)
		}
		roots = []string{"."}
		probe := new(modfile.File)
		if err := probe.AddModuleStmt("consumer.invalid/release-probe"); err != nil {
			return prepared, supplyUnavailable("HARNESS_UNAVAILABLE")
		}
		moduleFiles = map[string]*modfile.File{".": probe}
	} else {
		for _, name := range roots {
			if moduleFiles[name] == nil {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
		}
		selected = files
	}
	rootSet := map[string]bool{}
	for _, name := range roots {
		rootSet[name] = true
	}
	sources := []supplyGoSource{}
	allImports := map[string]bool{}
	materialized := []supplyTrackedFile{}
	for _, file := range selected {
		name := file.Path
		owner := "."
		if consumer.Kind == "external-program" {
			if programBase != "." {
				name = strings.TrimPrefix(name, programBase+"/")
			}
		} else {
			for candidate := range moduleFiles {
				if candidate != "." && strings.HasPrefix(name, candidate+"/") && (owner == "." || len(candidate) > len(owner)) {
					owner = candidate
				}
			}
			if !rootSet[owner] {
				continue
			}
		}
		file.Path = name
		materialized = append(materialized, file)
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, file.Data, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		source := supplyGoSource{Path: name, Root: owner}
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
			if value == e.manifest.ModulePath || strings.HasPrefix(value, e.manifest.ModulePath+"/") {
				source.Imports = append(source.Imports, value)
				allImports[value] = true
			}
		}
		sources = append(sources, source)
	}
	if failure := supplyMaterialize(root, materialized); failure != nil {
		return prepared, failure
	}
	prepared.Imports = supplySortedSet(allImports)
	covered := map[string]bool{}
	for _, context := range consumer.Contexts {
		if !supported[context.GOOS+"/"+context.GOARCH] {
			return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		buildContext := build.Default
		buildContext.GOOS, buildContext.GOARCH, buildContext.CgoEnabled = context.GOOS, context.GOARCH, context.CGO
		buildContext.BuildTags = append([]string(nil), context.Tags...)
		packages, imports := map[string]map[string]bool{}, map[string]map[string]bool{}
		for _, source := range sources {
			directory := filepath.Join(root, filepath.FromSlash(path.Dir(source.Path)))
			matches, err := buildContext.MatchFile(directory, path.Base(source.Path))
			if err != nil {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
			if !matches {
				continue
			}
			if packages[source.Root] == nil {
				packages[source.Root], imports[source.Root] = map[string]bool{}, map[string]bool{}
			}
			relative, err := filepath.Rel(filepath.Join(root, filepath.FromSlash(source.Root)), directory)
			if err != nil {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
			packagePath := "."
			if relative != "." {
				packagePath = "./" + filepath.ToSlash(relative)
			}
			packages[source.Root][packagePath] = true
			for _, imported := range source.Imports {
				imports[source.Root][imported], covered[source.Path+":"+imported] = true, true
			}
		}
		preparedContext := supplyPreparedContext{Context: context}
		for _, moduleRoot := range roots {
			if len(packages[moduleRoot]) == 0 {
				continue
			}
			preparedContext.Modules = append(preparedContext.Modules, supplyBuildModule{
				Root: filepath.Join(root, filepath.FromSlash(moduleRoot)), ModulePath: moduleFiles[moduleRoot].Module.Mod.Path,
				Packages: supplySortedSet(packages[moduleRoot]), Imports: supplySortedSet(imports[moduleRoot]),
			})
		}
		prepared.Contexts = append(prepared.Contexts, preparedContext)
	}
	for _, source := range sources {
		for _, imported := range source.Imports {
			if !covered[source.Path+":"+imported] {
				return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
			}
		}
	}
	for _, moduleRoot := range roots {
		file := moduleFiles[moduleRoot]
		// A local replacement would change the subject, even in an owned copy.
		if len(file.Replace) != 0 {
			return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		language := "1.21"
		if file.Go != nil {
			language = file.Go.Version
		}
		if target.Go != nil && goversion.Compare("go"+target.Go.Version, "go"+language) > 0 {
			language = target.Go.Version
		}
		if err := file.AddGoStmt(language); err != nil {
			return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		if err := file.AddRequire(e.manifest.ModulePath, e.version); err != nil {
			return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		data, err := file.Format()
		if err != nil {
			return prepared, supplyRed("CONSUMER_DECLARATION_INVALID")
		}
		name := filepath.Join(root, filepath.FromSlash(moduleRoot), "go.mod")
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return prepared, supplyUnavailable("HARNESS_UNAVAILABLE")
		}
		if err := os.WriteFile(name, data, 0600); err != nil {
			return prepared, supplyUnavailable("HARNESS_UNAVAILABLE")
		}
	}
	return prepared, nil
}

type supplyListedPackage struct {
	ImportPath, Dir string
	Standard        bool
	Incomplete      bool
	Module          *struct {
		Path, Version string
		Replace       *json.RawMessage
	}
	Error      *json.RawMessage
	DepsErrors []json.RawMessage
}

func (e *supplyEngine) buildConsumer(consumer supplyPrepared, plan supplyPreparedContext) *supplyFailure {
	ctx, cancel := context.WithTimeout(e.ctx, 600*time.Second)
	defer cancel()
	local := *e
	local.ctx = ctx
	cache, err := os.MkdirTemp(e.work, "modcache-")
	if err != nil {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	cgo := "0"
	if plan.Context.CGO {
		cgo = "1"
	}
	env := supplyEnvironment("GOPROXY="+e.proxy, "GOMODCACHE="+cache, "GOSUMDB=off", "GONOSUMDB=", "GOPRIVATE=", "GONOPROXY=",
		"GOOS="+plan.Context.GOOS, "GOARCH="+plan.Context.GOARCH, "CGO_ENABLED="+cgo)
	for _, module := range plan.Modules {
		args := []string{"list", "-e", "-deps", "-test", "-json", "-mod=mod"}
		if len(plan.Context.Tags) != 0 {
			args = append(args, "-tags="+strings.Join(plan.Context.Tags, ","))
		}
		args = append(args, module.Packages...)
		listed, failure := local.command(SupplyCommand{Program: e.goBinary, Args: args, Dir: module.Root, Env: env}, 600*time.Second)
		if failure != nil {
			return failure
		}
		decoder := json.NewDecoder(bytes.NewReader(listed.Stdout))
		seen, examined := map[string]bool{}, 0
		var declarationFailure, dependencyFailure bool
		for {
			var pkg supplyListedPackage
			err := decoder.Decode(&pkg)
			if err == io.EOF {
				break
			}
			if err != nil || pkg.ImportPath == "" {
				return supplyUnavailable("SOURCE_UNAVAILABLE")
			}
			examined++
			target := pkg.ImportPath == e.manifest.ModulePath || strings.HasPrefix(pkg.ImportPath, e.manifest.ModulePath+"/")
			if pkg.Error != nil {
				if !target && pkg.ImportPath != module.ModulePath && !strings.HasPrefix(pkg.ImportPath, module.ModulePath+"/") && !pkg.Standard {
					dependencyFailure = true
				} else {
					declarationFailure = true
				}
			}
			if pkg.Incomplete || len(pkg.DepsErrors) != 0 {
				declarationFailure = true
			}
			if target {
				if pkg.Module == nil || pkg.Module.Path != e.manifest.ModulePath || pkg.Module.Version != e.version || pkg.Module.Replace != nil {
					return supplyRed("CONSUMER_IMPORT_MISSING")
				}
				relative, err := filepath.Rel(cache, pkg.Dir)
				if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					return supplyRed("CONSUMER_IMPORT_MISSING")
				}
				seen[pkg.ImportPath] = true
			}
		}
		if dependencyFailure || examined == 0 || listed.ExitCode != 0 {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if declarationFailure {
			return supplyRed("CONSUMER_BUILD_FAILED")
		}
		for _, imported := range module.Imports {
			if !seen[imported] {
				return supplyRed("CONSUMER_IMPORT_MISSING")
			}
		}
		for i, name := range module.Packages {
			for _, operation := range []string{"build", "test"} {
				args := []string{operation, "-mod=mod"}
				if operation == "test" {
					args = append(args, "-c")
				}
				if len(plan.Context.Tags) != 0 {
					args = append(args, "-tags="+strings.Join(plan.Context.Tags, ","))
				}
				args = append(args, "-o", filepath.Join(cache, fmt.Sprintf("build-%s-%d", operation, i)), name)
				built, failure := local.command(SupplyCommand{Program: e.goBinary, Args: args, Dir: module.Root, Env: env}, 600*time.Second)
				if failure != nil {
					return failure
				}
				if built.ExitCode != 0 {
					return supplyRed("CONSUMER_BUILD_FAILED")
				}
			}
		}
	}
	return nil
}
