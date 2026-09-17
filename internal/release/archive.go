// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	modzip "golang.org/x/mod/zip"
)

type supplyEngine struct {
	ctx      context.Context
	deps     SupplyDependencies
	manifest supplyManifest
	work     string
	goBinary string
	proxy    string
	version  string
}

func supplyDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (e *supplyEngine) command(request SupplyCommand, budget time.Duration) (SupplyCommandResult, *supplyFailure) {
	ctx, cancel := context.WithTimeout(e.ctx, budget)
	defer cancel()
	result := e.deps.Command(ctx, request)
	if ctx.Err() != nil {
		return result, supplyUnavailable("BUDGET_EXHAUSTED")
	}
	if result.ExitCode < 0 {
		return result, supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	return result, nil
}

func (e *supplyEngine) git(root string, input []byte, args ...string) ([]byte, *supplyFailure) {
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.autocrlf=input", "-c", "core.eol=lf"}, args...)
	result, failure := e.command(SupplyCommand{Program: "git", Args: args, Dir: root,
		Env: supplyEnvironment(), Stdin: input}, time.Duration(e.manifest.NetworkSeconds)*time.Second)
	if failure != nil {
		return nil, failure
	}
	if result.ExitCode != 0 {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return result.Stdout, nil
}

func (e *supplyEngine) identify(root, repository, revision string) *supplyFailure {
	if !supplyAbsolute(root) || !supplySHA.MatchString(revision) {
		return supplyRed("INPUT_INVALID")
	}
	top, failure := e.git(root, nil, "rev-parse", "--show-toplevel")
	if failure != nil {
		return failure
	}
	if strings.TrimSpace(string(top)) != root {
		return supplyRed("REPOSITORY_MISMATCH")
	}
	origin, failure := e.git(root, nil, "remote", "get-url", "origin")
	if failure != nil {
		return failure
	}
	remote := strings.TrimSpace(string(origin))
	if remote != "https://github.com/"+repository+".git" && remote != "https://github.com/"+repository &&
		remote != "git@github.com:"+repository+".git" && remote != "git@github.com:"+repository &&
		remote != "ssh://git@github.com/"+repository+".git" {
		return supplyRed("REPOSITORY_MISMATCH")
	}
	resolved, failure := e.git(root, nil, "rev-parse", "--verify", revision+"^{commit}")
	if failure != nil {
		return failure
	}
	if strings.TrimSpace(string(resolved)) != revision {
		return supplyRed("INPUT_CHANGED")
	}
	return nil
}

type supplyTrackedFile struct {
	Path, Mode, Object string
	Data               []byte
}

// The census reads Git blobs, never working files or export-ignore omissions.
// Batched reads keep the full tree bound to one immutable commit.
func (e *supplyEngine) tracked(root, revision string) ([]supplyTrackedFile, *supplyFailure) {
	listing, failure := e.git(root, nil, "ls-tree", "-r", "-z", "--full-tree", revision)
	if failure != nil {
		return nil, failure
	}
	files := []supplyTrackedFile{}
	var objects strings.Builder
	for _, entry := range bytes.Split(listing, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		meta, name, ok := strings.Cut(string(entry), "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[1] != "blob" ||
			(fields[0] != "100644" && fields[0] != "100755") || !supplyRelative(name, false) || !supplySHA.MatchString(fields[2]) {
			return nil, supplyRed("INPUT_INVALID")
		}
		files = append(files, supplyTrackedFile{Path: name, Mode: fields[0], Object: fields[2]})
		objects.WriteString(fields[2] + "\n")
	}
	if len(files) == 0 {
		return nil, supplyRed("INPUT_INVALID")
	}
	data, failure := e.git(root, []byte(objects.String()), "cat-file", "--batch")
	if failure != nil {
		return nil, failure
	}
	reader := bufio.NewReader(bytes.NewReader(data))
	for i := range files {
		header, err := reader.ReadString('\n')
		fields := strings.Fields(header)
		if err != nil || len(fields) != 3 || fields[0] != files[i].Object || fields[1] != "blob" {
			return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > int64(len(data)) {
			return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		files[i].Data = make([]byte, size)
		if _, err := io.ReadFull(reader, files[i].Data); err != nil {
			return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if last, err := reader.ReadByte(); err != nil || last != '\n' {
			return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
		}
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return files, nil
}

func supplyMaterialize(root string, files []supplyTrackedFile) *supplyFailure {
	for _, file := range files {
		name := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return supplyUnavailable("HARNESS_UNAVAILABLE")
		}
		mode := fs.FileMode(0600)
		if file.Mode == "100755" {
			mode = 0700
		}
		if err := os.WriteFile(name, file.Data, mode); err != nil {
			return supplyUnavailable("HARNESS_UNAVAILABLE")
		}
	}
	return nil
}

type supplyZipFile struct{ *zip.File }

func (f supplyZipFile) Path() string                 { return f.Name }
func (f supplyZipFile) Lstat() (fs.FileInfo, error)  { return f.FileInfo(), nil }
func (f supplyZipFile) Open() (io.ReadCloser, error) { return f.File.Open() }

type supplyContextWriter struct {
	ctx context.Context
	io.Writer
}

func (w supplyContextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.Writer.Write(data)
}

type supplyArchive struct {
	Files    map[string][]byte
	Digest   string
	GoMod    *modfile.File
	Packages map[string]bool
}

// Git's archive membership/attributes and x/mod's actual ZIP policy are both
// applied. Git runs through the bounded process seam, unlike CreateFromVCS's
// internal unbounded exec; there is no working-directory ZIP fallback.
func (e *supplyEngine) archive(revision string) (supplyArchive, *supplyFailure) {
	var result supplyArchive
	ctx, cancel := context.WithTimeout(e.ctx, 600*time.Second)
	defer cancel()
	gitZip, failure := e.git(e.manifest.CandidateRoot, nil, "archive", "--format=zip", revision)
	if failure != nil {
		return result, failure
	}
	reader, err := zip.NewReader(bytes.NewReader(gitZip), int64(len(gitZip)))
	if err != nil {
		return result, supplyRed("INPUT_INVALID")
	}
	files := make([]modzip.File, 0, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		files = append(files, supplyZipFile{file})
	}
	e.version = module.PseudoVersion(semver.Major(e.manifest.Version), "", time.Unix(0, 0).UTC(), revision[:12])
	var buffer bytes.Buffer
	if err := modzip.Create(supplyContextWriter{ctx, &buffer}, module.Version{Path: e.manifest.ModulePath, Version: e.version}, files); err != nil {
		if ctx.Err() != nil {
			return result, supplyUnavailable("BUDGET_EXHAUSTED")
		}
		return result, supplyRed("INPUT_INVALID")
	}
	result.Digest = supplyDigest(buffer.Bytes())
	reader, err = zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		return result, supplyRed("INPUT_INVALID")
	}
	result.Files, result.Packages = map[string][]byte{}, map[string]bool{}
	prefix := e.manifest.ModulePath + "@" + e.version + "/"
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, prefix) {
			return result, supplyRed("INPUT_INVALID")
		}
		name := strings.TrimPrefix(file.Name, prefix)
		stream, err := file.Open()
		if err != nil {
			return result, supplyRed("INPUT_INVALID")
		}
		data, readErr := io.ReadAll(stream)
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return result, supplyRed("INPUT_INVALID")
		}
		result.Files[name] = data
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			packagePath := e.manifest.ModulePath
			if dir := filepath.ToSlash(filepath.Dir(name)); dir != "." {
				packagePath += "/" + dir
			}
			result.Packages[packagePath] = true
		}
	}
	result.GoMod, err = modfile.Parse("go.mod", result.Files["go.mod"], nil)
	if err != nil || result.GoMod.Module == nil {
		return result, supplyRed("INPUT_INVALID")
	}
	if result.GoMod.Module.Mod.Path != e.manifest.ModulePath {
		return result, supplyRed("REPOSITORY_MISMATCH")
	}
	escaped, err := module.EscapePath(e.manifest.ModulePath)
	if err != nil {
		return result, supplyRed("INPUT_INVALID")
	}
	proxy := filepath.Join(e.work, "proxy")
	versionDir := filepath.Join(proxy, filepath.FromSlash(escaped), "@v")
	if err := os.MkdirAll(versionDir, 0700); err != nil {
		return result, supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	info, _ := json.Marshal(map[string]string{"Version": e.version, "Time": "1970-01-01T00:00:00Z"})
	for name, content := range map[string][]byte{e.version + ".zip": buffer.Bytes(), e.version + ".mod": result.Files["go.mod"], e.version + ".info": info, "list": []byte(e.version + "\n")} {
		if err := os.WriteFile(filepath.Join(versionDir, name), content, 0600); err != nil {
			return result, supplyUnavailable("HARNESS_UNAVAILABLE")
		}
	}
	e.proxy = (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String()
	return result, nil
}

// Link only dependency download trees. The target module's entire proxy
// coordinate is absent, so missing target bytes cannot fall back to cache.
func supplyDependencyProxy(source, destination, excluded, relative string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		coordinate := name
		if relative != "" {
			coordinate = relative + "/" + name
		}
		if coordinate == excluded {
			continue
		}
		from, to := filepath.Join(source, name), filepath.Join(destination, name)
		if strings.HasPrefix(excluded, coordinate+"/") {
			if !entry.IsDir() {
				continue
			}
			if err := supplyDependencyProxy(from, to, excluded, coordinate); err != nil {
				return err
			}
		} else if err := os.Symlink(from, to); err != nil {
			return err
		}
	}
	return nil
}

func (e *supplyEngine) dependencies() *supplyFailure {
	e.goBinary = filepath.Join(runtime.GOROOT(), "bin", "go")
	result, failure := e.command(SupplyCommand{Program: e.goBinary, Args: []string{"env", "GOMODCACHE"}, Dir: e.work,
		Env: supplyEnvironment("GOPROXY=" + e.proxy)}, 600*time.Second)
	if failure != nil {
		return failure
	}
	if result.ExitCode != 0 {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	cache := strings.TrimSpace(string(result.Stdout))
	if !supplyAbsolute(cache) {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	source := filepath.Join(cache, "cache", "download")
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return nil // No dependency is assumed available: Go must resolve it below.
	}
	excluded, _ := module.EscapePath(e.manifest.ModulePath)
	destination := filepath.Join(e.work, "dependency-proxy")
	if err := supplyDependencyProxy(source, destination, excluded, ""); err != nil {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	e.proxy += "," + (&url.URL{Scheme: "file", Path: filepath.ToSlash(destination)}).String()
	return nil
}

func supplyRemoveOwned(root string) {
	// Go's private module cache contains read-only directories. Never chmod a
	// dependency-proxy symlink or its shared target during cleanup.
	_ = filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(name, 0700)
		}
		return nil
	})
	_ = os.RemoveAll(root)
}

func supplySortedSet(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
