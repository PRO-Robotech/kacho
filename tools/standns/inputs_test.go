// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// inputs_test.go — правило различения «вход сборки образа / не вход» доказывается
// парами: законный путь оснастки — «не вход», его близнец в замыкании образа —
// «вход»; и на НАСТОЯЩИХ Dockerfile дерева, а не только на придуманных.
package standns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goDockerfile = `FROM golang AS builder
WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    go list -deps ./svc/cmd/svc >/dev/null
RUN CGO_ENABLED=0 GOOS=${TARGETOS} go build -o /svc ./svc/cmd/svc \
 && CGO_ENABLED=0 go build -o /mig ./svc/cmd/migrator
FROM alpine
RUN apk add --no-cache ca-certificates
COPY --from=builder /svc /usr/local/bin/svc
`

const uiDockerfile = `FROM node AS build
COPY package.json package-lock.json ./
COPY shared/ shared/
COPY app/ app/
RUN npm ci && npm run build
FROM nginx
COPY app/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/app/dist /usr/share/nginx/html
`

var tree = []string{
	"go.mod", "go.sum", ".dockerignore",
	"svc/Dockerfile", "svc/cmd/svc/main.go", "svc/cmd/migrator/main.go",
	"svc/internal/core/core.go", "svc/internal/core/core_test.go", "svc/internal/core/sql/001.sql",
	"svc/deploy/values.yaml", "internal/tooling/tool.go", "internal/gate/census.go",
	"ui/package.json", "ui/package-lock.json", "ui/app/Dockerfile", "ui/app/src/main.ts",
	"ui/app/nginx.conf", "ui/shared/lib.ts", "ui/e2e/probe.spec.ts", "ui/deploy/values.yaml",
}

func stubDeps(ctx string, mains []string) ([]string, error) {
	return []string{"svc/cmd/svc/main.go", "svc/cmd/migrator/main.go", "svc/internal/core/core.go", "svc/internal/core/sql/001.sql"}, nil
}

func closure(t *testing.T) Closure {
	t.Helper()
	c, err := BuildClosure(tree, []Dockerfile{
		ParseDockerfile("svc/Dockerfile", []byte(goDockerfile)),
		ParseDockerfile("ui/app/Dockerfile", []byte(uiDockerfile)),
	}, stubDeps)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseDockerfileFindsGoMainsAndGoOnlyStage(t *testing.T) {
	df := ParseDockerfile("svc/Dockerfile", []byte(goDockerfile))
	if strings.Join(df.GoMains, ",") != "svc/cmd/svc,svc/cmd/migrator" {
		t.Fatalf("пакеты go build: %v", df.GoMains)
	}
	if !df.Stages[0].GoOnly || !df.Stages[0].CopyAll {
		t.Fatalf("стадия сборки должна быть go-only с COPY . .: %+v", df.Stages[0])
	}
	if df.Stages[1].GoOnly {
		t.Fatalf("финальная стадия с apk не go-only")
	}
	// Близнец: та же стадия с одной не-go командой — уже не сужается.
	tw := ParseDockerfile("svc/Dockerfile", []byte(strings.Replace(goDockerfile, "go list -deps ./svc/cmd/svc >/dev/null", "sh scripts/gen.sh", 1)))
	if tw.Stages[0].GoOnly {
		t.Fatalf("стадия с sh — не go-only")
	}
}

func TestToolingOutsideTheBinaryClosureIsNotAnInputButProductCodeIs(t *testing.T) {
	c := closure(t)
	for p, want := range map[string]bool{
		"internal/tooling/tool.go":       false, // оснастка, не в замыкании
		"internal/gate/census.go":        false,
		"svc/deploy/values.yaml":         false, // чарт
		"svc/internal/core/core_test.go": false,
		"ui/e2e/probe.spec.ts":           false, // проба, вне COPY
		"ui/deploy/values.yaml":          false,
		"svc/internal/core/core.go":      true, // продуктовый код в замыкании
		"svc/internal/core/sql/001.sql":  true, // встроенный файл
		"go.mod":                         true,
		"go.sum":                         true,
		"svc/Dockerfile":                 true,
		"ui/app/src/main.ts":             true, // исходник консоли в её COPY
		"ui/shared/lib.ts":               true,
		"ui/package.json":                true,
		"ui/app/nginx.conf":              true,
	} {
		if _, got := c.Why(p); got != want {
			t.Errorf("%s: вход=%v, ждали %v", p, got, want)
		}
	}
}

func TestStageWithANonGoCommandMakesTheWholeContextAnInput(t *testing.T) {
	body := strings.Replace(goDockerfile, "go list -deps ./svc/cmd/svc >/dev/null", "sh scripts/gen.sh", 1)
	c, err := BuildClosure(tree, []Dockerfile{ParseDockerfile("svc/Dockerfile", []byte(body))}, stubDeps)
	if err != nil {
		t.Fatal(err)
	}
	if _, in := c.Why("internal/tooling/tool.go"); !in {
		t.Fatalf("при не-go команде в стадии с COPY . . весь контекст — вход")
	}
}

func TestContextIsTheNearestAncestorHoldingEverySource(t *testing.T) {
	c := closure(t)
	if c.Contexts != 2 {
		t.Fatalf("контекстов %d, ждали 2 (корень и ui)", c.Contexts)
	}
	if _, err := BuildClosure(tree, []Dockerfile{ParseDockerfile("x/Dockerfile", []byte("FROM a\nCOPY nowhere/ x/\n"))}, stubDeps); err == nil {
		t.Fatalf("Dockerfile с несуществующим источником должен отказывать, а не судиться")
	}
	if _, err := BuildClosure(tree, nil, stubDeps); err == nil {
		t.Fatalf("пустой обход — отказ")
	}
}

func TestIgnoreChangeIsInertOnlyWhenItAddsExclusionsOfNothingTracked(t *testing.T) {
	inert := "--- a/.dockerignore\n+++ b/.dockerignore\n@@ -1,0 +2,2 @@\n+# рабочие файлы\n+deploy/.stand-ns/\n"
	if ok, why := IgnoreChangeIsInert("", inert, tree); !ok {
		t.Fatalf("исключение неотслеживаемого каталога должно быть инертным: %s", why)
	}
	for name, d := range map[string]string{
		"исключает отслеживаемое": "+svc/internal/\n",
		"снимает правило":         "-terraform\n",
		"обратное правило":        "+!secret\n",
		"двойная звезда":          "+**/x\n",
	} {
		if ok, _ := IgnoreChangeIsInert("", d, tree); ok {
			t.Errorf("%s: признано инертным", name)
		}
	}
}

func TestClassifyMarksOnlyClosureMembers(t *testing.T) {
	c := closure(t)
	v, err := Classify([]string{".dockerignore", "internal/tooling/tool.go", "svc/internal/core/core.go"}, c, tree,
		func(string) (string, error) { return "+deploy/.stand-ns/\n", nil })
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, x := range v {
		got[x.Path] = x.Input
	}
	if got[".dockerignore"] || got["internal/tooling/tool.go"] || !got["svc/internal/core/core.go"] {
		t.Fatalf("вердикты: %v", got)
	}
}

// На настоящих Dockerfile: сайт документации КОПИРУЕТ deploy/default.conf в образ —
// прежнее правило («deploy/ — не вход») это пропускало.
func TestRealDockerfilesOfTheTree(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(p string) Dockerfile {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatalf("настоящий %s не прочитан: %v", p, err)
		}
		return ParseDockerfile(p, b)
	}
	gw := read("gateway/Dockerfile")
	if len(gw.GoMains) != 1 || gw.GoMains[0] != "gateway/cmd/api-gateway" || !gw.Stages[0].GoOnly {
		t.Fatalf("gateway/Dockerfile разобран не так: %+v", gw)
	}
	docs := read("gateway/docs/Dockerfile")
	files := []string{"gateway/docs/package.json", "gateway/docs/package-lock.json", "gateway/docs/deploy/default.conf", "gateway/docs/src/x.md"}
	c, err := BuildClosure(files, []Dockerfile{docs}, stubDeps)
	if err != nil {
		t.Fatal(err)
	}
	if _, in := c.Why("gateway/docs/deploy/default.conf"); !in {
		t.Fatalf("deploy/default.conf сайта документации копируется в образ — это вход")
	}
}
