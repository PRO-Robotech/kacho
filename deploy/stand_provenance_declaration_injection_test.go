// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

// stand_provenance_declaration_injection_test.go — доказательство того, что гейт
// провенанса СПОСОБЕН упасть, и что он молчит на законной конструкции той же формы.
//
// Проба кормит синтетикой те же функции, что судят дерево. Читать гейт глазами
// нельзя: прочтение доказывает, что он написан, а не что он работает. Каждая ось
// вносится ОТДЕЛЬНО — одна инъекция, снимающая всё разом, показала бы только, что
// пустой вход роняет что-нибудь.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const injRevisionPath = "/etc/kacho/image-revision"

// legitDockerfile — законный образ. Положительный контроль: без него отрицания
// ниже зеленели бы и на проверке, отвергающей вообще всё.
const legitDockerfile = `FROM alpine:3.24
COPY --from=builder /kacho-vpc /usr/local/bin/kacho-vpc
ARG KACHO_IMAGE_REVISION=""
ARG KACHO_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$KACHO_IMAGE_REVISION" \
      org.opencontainers.image.version="$KACHO_IMAGE_VERSION"
RUN mkdir -p /etc/kacho && printf '%s\n' "$KACHO_IMAGE_REVISION" > /etc/kacho/image-revision
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-vpc"]
`

func TestProvenanceGateFailsOnAnImageThatDoesNotCarryTheRevision(t *testing.T) {
	if f := checkDockerfile(legitDockerfile, injRevisionPath); len(f) != 0 {
		t.Fatalf("положительный контроль: законный образ объявлен нарушителем — %v", f)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "снято объявление аргумента",
			body: strings.Replace(legitDockerfile, `ARG KACHO_IMAGE_REVISION=""`+"\n", "", 1),
			want: "не объявлен ARG",
		},
		{
			// Самый коварный подлог: провенанс ЕСТЬ на вид, но величина вписана
			// рукой — значит она не функция дерева и лжёт с первой пересборки.
			name: "клеймо не выводится из аргумента, а вписано",
			body: strings.Replace(legitDockerfile,
				`org.opencontainers.image.revision="$KACHO_IMAGE_REVISION"`,
				`org.opencontainers.image.revision="c11f1d52b93471f7321683c516403def8ae632c8"`, 1),
			want: "клеймо org.opencontainers.image.revision не выводится",
		},
		{
			name: "файл пишется НЕ по тому пути, что читает читатель",
			body: strings.Replace(legitDockerfile,
				"> /etc/kacho/image-revision", "> /etc/kacho/revision", 1),
			want: "величина не записывается в " + injRevisionPath,
		},
		{
			name: "файл пишется не из аргумента",
			body: strings.Replace(legitDockerfile,
				`printf '%s\n' "$KACHO_IMAGE_REVISION" > /etc/kacho/image-revision`,
				`printf '%s\n' "dev" > /etc/kacho/image-revision`, 1),
			want: "величина не записывается",
		},
		{
			name: "подъём до root не закрыт",
			body: strings.Replace(legitDockerfile, "USER 65532", "USER root", 1),
			want: "последний USER — root",
		},
		{
			name: "образ вовсе не объявляет пользователя",
			body: strings.Replace(legitDockerfile, "USER 65532\n", "", 1),
			want: "не объявляет USER",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := checkDockerfile(c.body, injRevisionPath)
			if len(findings) == 0 {
				t.Fatalf("дефект внесён, а гейт молчит")
			}
			joined := strings.Join(findings, " | ")
			if !strings.Contains(joined, c.want) {
				t.Fatalf("гейт покраснел не на том: %s (ждали «%s»)", joined, c.want)
			}
		})
	}
}

// Законный близнец семейства консоли: пользователь поднимается до root РАДИ
// записи величины и тут же закрывается. Без этой пробы правило про USER
// отвергало бы ровно ту конструкцию, которую само же и вводит.
func TestProvenanceGateStaysSilentOnTheConsoleFamilyShape(t *testing.T) {
	body := `FROM nginxinc/nginx-unprivileged:1.31-alpine
COPY --from=build /app/vpc/dist /usr/share/nginx/html
ARG KACHO_IMAGE_REVISION=""
ARG KACHO_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$KACHO_IMAGE_REVISION" \
      org.opencontainers.image.version="$KACHO_IMAGE_VERSION"
USER root
RUN mkdir -p /etc/kacho && printf '%s\n' "$KACHO_IMAGE_REVISION" > /etc/kacho/image-revision
USER 101
CMD ["nginx", "-g", "daemon off;"]
`
	if f := checkDockerfile(body, injRevisionPath); len(f) != 0 {
		t.Fatalf("законная форма объявлена нарушителем — %v", f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА СБОРКИ
// ─────────────────────────────────────────────────────────────────────────────

func TestProvenanceGateFailsOnABuildThatDoesNotPassTheRevision(t *testing.T) {
	recipe := "docker:\n\tcd .. && docker build -f services/vpc/Dockerfile -t kacho-vpc:dev .\n"
	invs := makeBuildInvocations("синтетика/Makefile", recipe)
	if len(invs) != 1 {
		t.Fatalf("вызовов распознано %d, ждали 1 — предмет пробы не построен", len(invs))
	}
	if invocationCarriesRevision(invs[0].text, nil) {
		t.Fatal("вызов без величины признан несущим её")
	}

	// Законный близнец: та же строка с величиной — молчание.
	ok := "docker:\n\tcd .. && docker build $(IMAGE_BUILD_ARGS) -f services/vpc/Dockerfile -t kacho-vpc:dev .\n"
	invs = makeBuildInvocations("синтетика/Makefile", ok)
	if len(invs) != 1 || !invocationCarriesRevision(invs[0].text, []string{"$(IMAGE_BUILD_ARGS)"}) {
		t.Fatal("законный вызов объявлен нарушителем")
	}
}

// Гейт не краснеет на СОБСТВЕННОМ объяснении: `docker build` в комментарии
// вызовом не является. Без этой оси проверка по подстроке выглядела бы рабочей и
// падала бы на первом же разборе, написанном рядом.
func TestProvenanceGateDoesNotReadItsOwnCommentaryAsABuild(t *testing.T) {
	body := "# сюда дописывается величина к каждому `docker build -f Dockerfile`\nfoo:\n\techo нет сборки\n"
	if invs := makeBuildInvocations("синтетика/Makefile", body); len(invs) != 0 {
		t.Fatalf("комментарий прочитан как вызов сборки: %+v", invs)
	}
}

// Включение обходится в ОБЕ стороны: файл с величиной оправдывает, файл без неё —
// нет, а включение в пустоту само является находкой.
func TestProvenanceGateFollowsIncludesInBothDirections(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("запись %s: %v", name, err)
		}
		return p
	}

	write("carries.mk", "IMAGE_BUILD_ARGS := --build-arg KACHO_IMAGE_REVISION=\"$(GIT_COMMIT)\"\n")
	write("empty.mk", "SOMETHING := else\n")

	good := write("Makefile.good", "include carries.mk\ndocker:\n\tdocker build $(IMAGE_BUILD_ARGS) -f Dockerfile -t x .\n")
	carriers, findings := revisionCarryingVars(good, 0)
	if len(carriers) != 1 || carriers[0] != "$(IMAGE_BUILD_ARGS)" || len(findings) != 0 {
		t.Fatalf("носитель из включённого файла не найден: носители %v, находки %v", carriers, findings)
	}

	bad := write("Makefile.bad", "include empty.mk\ndocker:\n\tdocker build $(IMAGE_BUILD_ARGS) -f Dockerfile -t x .\n")
	if carriers, _ := revisionCarryingVars(bad, 0); len(carriers) != 0 {
		t.Fatalf("включение файла БЕЗ величины сочтено носителем: %v", carriers)
	}

	missing := write("Makefile.missing", "include nowhere.mk\ndocker:\n\tdocker build -f Dockerfile -t x .\n")
	if _, findings := revisionCarryingVars(missing, 0); len(findings) == 0 {
		t.Fatal("включение несуществующего файла прошло молча")
	}
}

func TestProvenanceGateJudgesWorkflowStepsByTheParsedDeclaration(t *testing.T) {
	// Шаг действия публикации БЕЗ величины — находка.
	noArgs := `
jobs:
  build:
    steps:
      - uses: actions/checkout@v7
      - uses: docker/build-push-action@abc
        with:
          file: ui-future/vpc/Dockerfile
          tags: ns/kacho-ui-future-vpc:t
`
	invs := workflowBuildInvocations(t, "синтетика.yml", noArgs)
	if len(invs) != 1 {
		t.Fatalf("вызовов распознано %d, ждали 1 (шаг checkout вызовом не является)", len(invs))
	}
	if invocationCarriesRevision(invs[0].text, nil) {
		t.Fatal("шаг без build-args признан несущим величину")
	}

	// Законный близнец — тот же шаг с величиной.
	withArgs := strings.Replace(noArgs,
		"          file: ui-future/vpc/Dockerfile",
		"          file: ui-future/vpc/Dockerfile\n          build-args: |\n            KACHO_IMAGE_REVISION=${{ github.sha }}", 1)
	invs = workflowBuildInvocations(t, "синтетика.yml", withArgs)
	if len(invs) != 1 || !invocationCarriesRevision(invs[0].text, nil) {
		t.Fatalf("законный шаг объявлен нарушителем: %+v", invs)
	}

	// Шаг-скрипт с `docker buildx build` судится так же.
	runStep := `
jobs:
  build:
    steps:
      - run: |
          docker buildx build --provenance=false -f ./services/vpc/Dockerfile -t x .
`
	invs = workflowBuildInvocations(t, "синтетика.yml", runStep)
	if len(invs) != 1 || invocationCarriesRevision(invs[0].text, nil) {
		t.Fatalf("вызов в шаге-скрипте не распознан либо ложно оправдан: %+v", invs)
	}
}
