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
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$OCI_IMAGE_REVISION" \
      org.opencontainers.image.version="$OCI_IMAGE_VERSION"
RUN mkdir -p /etc/kacho && printf '%s\n' "$OCI_IMAGE_REVISION" > /etc/kacho/image-revision
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-vpc"]
`

func TestProvenanceGateFailsOnAnImageThatDoesNotCarryTheRevision(t *testing.T) {
	if f, arg := checkDockerfile(legitDockerfile, injRevisionPath); len(f) != 0 || arg != canonicalProvenanceArg {
		t.Fatalf("положительный контроль: законный образ объявлен нарушителем — находки %v, имя довода %q", f, arg)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "снято объявление аргумента",
			body: strings.Replace(legitDockerfile, `ARG OCI_IMAGE_REVISION=""`+"\n", "", 1),
			want: "не объявлен ARG",
		},
		{
			// Самый коварный подлог: провенанс ЕСТЬ на вид, но величина вписана
			// рукой — значит она не функция дерева и лжёт с первой пересборки.
			name: "клеймо не выводится из аргумента, а вписано",
			body: strings.Replace(legitDockerfile,
				`org.opencontainers.image.revision="$OCI_IMAGE_REVISION"`,
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
				`printf '%s\n' "$OCI_IMAGE_REVISION" > /etc/kacho/image-revision`,
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
			findings, _ := checkDockerfile(c.body, injRevisionPath)
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
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$OCI_IMAGE_REVISION" \
      org.opencontainers.image.version="$OCI_IMAGE_VERSION"
USER root
RUN mkdir -p /etc/kacho && printf '%s\n' "$OCI_IMAGE_REVISION" > /etc/kacho/image-revision
USER 101
CMD ["nginx", "-g", "daemon off;"]
`
	if f, _ := checkDockerfile(body, injRevisionPath); len(f) != 0 {
		t.Fatalf("законная форма объявлена нарушителем — %v", f)
	}
}

// ЗАКОННЫЙ БЛИЗНЕЦ ПЕРЕХОДА: образ, объявляющий ПРЕЖНЕЕ имя, нарушителем не
// является — приём прежнего имени и есть предмет перехода. Без этой оси
// переименование краснело бы на файле службы доступа, который правится в её
// репозитории, а не здесь.
func TestProvenanceGateAcceptsTheLegacyArgNameWhileItIsStillDeclared(t *testing.T) {
	body := strings.ReplaceAll(legitDockerfile, canonicalProvenanceArg, legacyProvenanceArg)
	findings, arg := checkDockerfile(body, injRevisionPath)
	if len(findings) != 0 {
		t.Fatalf("образ под прежним именем объявлен нарушителем — %v", findings)
	}
	if arg != legacyProvenanceArg {
		t.Fatalf("имя довода прочитано как %q, ждали %q", arg, legacyProvenanceArg)
	}
}

// Имя определяется ПЕРВЫМ, и остальное сверяется с НИМ. Файл, объявивший одно имя
// и выводящий клеймо из другого, прошёл бы проверку, которая принимает любое
// известное имя в каждой строке ПО ОТДЕЛЬНОСТИ, — то есть ровно ту проверку, в
// которую вырождается гейт, когда имён становится два.
func TestProvenanceGateJudgesTheLabelAgainstTheNameTheFileDeclares(t *testing.T) {
	body := strings.Replace(legitDockerfile,
		`org.opencontainers.image.revision="$`+canonicalProvenanceArg+`"`,
		`org.opencontainers.image.revision="$`+legacyProvenanceArg+`"`, 1)
	findings, _ := checkDockerfile(body, injRevisionPath)
	joined := strings.Join(findings, " | ")
	if !strings.Contains(joined, "клеймо org.opencontainers.image.revision не выводится") {
		t.Fatalf("клеймо из ЧУЖОГО известного имени принято за своё: %v", findings)
	}
}

// Два имени одной величины в одном файле — два места об одном предмете; клеймо
// возьмёт одно, файл ревизии другое, и разойдутся они молча. Заодно эта ось
// закрывает «решить переход, объявив в каждом Dockerfile оба имени».
func TestProvenanceGateRejectsAFileDeclaringBothNames(t *testing.T) {
	body := strings.Replace(legitDockerfile,
		"ARG "+canonicalProvenanceArg+`=""`,
		"ARG "+canonicalProvenanceArg+`=""`+"\nARG "+legacyProvenanceArg+`=""`, 1)
	findings, _ := checkDockerfile(body, injRevisionPath)
	if !strings.Contains(strings.Join(findings, " | "), "объявлены ДВА имени") {
		t.Fatalf("файл с двумя именами одной величины прошёл молча: %v", findings)
	}
}

// САМОИСТЕЧЕНИЕ приёма прежнего имени — в обе стороны. Пока прежнее имя кто-то
// объявляет, приём молчит; как только объявивших ноль, приём становится находкой.
// Проба кормит перепись, а не дерево: доказательство, требующее дерева БЕЗ службы
// доступа, не воспроизводимо.
func TestLegacyArgAcceptanceExpiresWhenNobodyDeclaresItAnymore(t *testing.T) {
	if f := legacyAcceptanceFinding(map[string]int{canonicalProvenanceArg: 16, legacyProvenanceArg: 1}); f != "" {
		t.Fatalf("приём с живым предметом объявлен находкой: %s", f)
	}
	f := legacyAcceptanceFinding(map[string]int{canonicalProvenanceArg: 17})
	if f == "" {
		t.Fatal("приёму прежнего имени прощать нечего, а гейт молчит — послабление не истекает")
	}
	if !strings.Contains(f, legacyProvenanceArg) || !strings.Contains(f, "provenance.mk") {
		t.Fatalf("находка не называет ни имени, ни места снятия: %s", f)
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
	if len(namesPassedBy(invs[0].text, nil)) != 0 {
		t.Fatal("вызов без величины признан несущим её")
	}

	// Законный близнец: та же строка с величиной — молчание.
	ok := "docker:\n\tcd .. && docker build $(IMAGE_BUILD_ARGS) -f services/vpc/Dockerfile -t kacho-vpc:dev .\n"
	invs = makeBuildInvocations("синтетика/Makefile", ok)
	carriers := map[string]map[string]bool{"$(IMAGE_BUILD_ARGS)": {canonicalProvenanceArg: true}}
	if len(invs) != 1 || !namesPassedBy(invs[0].text, carriers)[canonicalProvenanceArg] {
		t.Fatal("законный вызов объявлен нарушителем")
	}
}

// НЕСУЩАЯ ОСЬ ЭТОЙ ПРАВКИ: производитель передаёт довод под одним именем, а его
// цель объявляет другое. docker непотреблённый довод выбрасывает МОЛЧА, `ARG`
// остаётся при пустом умолчании, образ уезжает без ревизии — и сборка зелена.
// Пока имя было одно, эта дыра была закрыта случайно (непереименованная половина
// не находила своего имени); с двумя именами её закрывает только сверка.
//
// Проба заводит СВОЁ дерево: сверка читает настоящий файл цели, поэтому подать ей
// синтетику текстом нельзя.
func TestProvenanceGateFailsWhenTheNamePassedDoesNotMatchTheTargetsArg(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("запись %s: %v", name, err)
		}
		return p
	}

	// Цель объявляет ПРЕЖНЕЕ имя — как файл службы доступа сегодня.
	write("Dockerfile", "FROM alpine\nARG "+legacyProvenanceArg+`=""`+"\n")
	site := write("Makefile", "docker:\n\tdocker build --build-arg "+canonicalProvenanceArg+"=x -f Dockerfile -t t .\n")

	invs := makeBuildInvocations(site, "docker:\n\tdocker build --build-arg "+canonicalProvenanceArg+"=x -f Dockerfile -t t .\n")
	if len(invs) != 1 {
		t.Fatalf("вызовов распознано %d, ждали 1 — предмет пробы не построен", len(invs))
	}
	want, resolved := dockerfileArgOf(site, invs[0].text)
	if !resolved {
		t.Fatal("цель не разрешилась — сверять нечего, и предмет пробы не построен")
	}
	if want != legacyProvenanceArg {
		t.Fatalf("имя цели прочитано как %q, ждали %q", want, legacyProvenanceArg)
	}
	if namesPassedBy(invs[0].text, nil)[want] {
		t.Fatal("расхождение имён не обнаружено: вызов признан передающим то, чего не передаёт")
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ — переходная пара: тот же вызов передаёт ОБА имени.
	both := "docker:\n\tdocker build --build-arg " + canonicalProvenanceArg + "=x --build-arg " +
		legacyProvenanceArg + "=x -f Dockerfile -t t .\n"
	invs = makeBuildInvocations(site, both)
	if len(invs) != 1 || !namesPassedBy(invs[0].text, nil)[legacyProvenanceArg] {
		t.Fatalf("переходная пара объявлена нарушителем: %+v", invs)
	}

	// ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ — цель, разрешаемая из переменной оболочки: сверка
	// не применима by construction, и это ГРАНИЦА, а не находка.
	shellPath := "docker:\n\tdocker build $(IMAGE_BUILD_ARGS) -f $$d/Dockerfile -t t .\n"
	if _, resolved := dockerfileArgOf(site, joinContinuations(shellPath)[1]); resolved {
		t.Fatal("путь из переменной оболочки объявлен разрешённым — сверка утверждала бы о чужом файле")
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

	write("carries.mk", "IMAGE_BUILD_ARGS := --build-arg "+canonicalProvenanceArg+"=\"$(GIT_COMMIT)\" --build-arg "+legacyProvenanceArg+"=\"$(GIT_COMMIT)\"\n")
	write("empty.mk", "SOMETHING := else\n")

	good := write("Makefile.good", "include carries.mk\ndocker:\n\tdocker build $(IMAGE_BUILD_ARGS) -f Dockerfile -t x .\n")
	carriers, findings := revisionCarryingVars(good, 0)
	if len(carriers) != 1 || len(findings) != 0 {
		t.Fatalf("носитель из включённого файла не найден: носители %v, находки %v", carriers, findings)
	}
	// Носитель несёт ОБА имени переходной пары — иначе сверка соответствия
	// объявила бы нарушителем цель, объявляющую прежнее имя.
	names := carriers["$(IMAGE_BUILD_ARGS)"]
	if !names[canonicalProvenanceArg] || !names[legacyProvenanceArg] {
		t.Fatalf("носитель прочитан не полностью: %v", names)
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
	if len(namesPassedBy(invs[0].text, nil)) != 0 {
		t.Fatal("шаг без build-args признан несущим величину")
	}

	// Законный близнец — тот же шаг с величиной.
	withArgs := strings.Replace(noArgs,
		"          file: ui-future/vpc/Dockerfile",
		"          file: ui-future/vpc/Dockerfile\n          build-args: |\n            OCI_IMAGE_REVISION=${{ github.sha }}", 1)
	invs = workflowBuildInvocations(t, "синтетика.yml", withArgs)
	if len(invs) != 1 || !namesPassedBy(invs[0].text, nil)[canonicalProvenanceArg] {
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
	if len(invs) != 1 || len(namesPassedBy(invs[0].text, nil)) != 0 {
		t.Fatalf("вызов в шаге-скрипте не распознан либо ложно оправдан: %+v", invs)
	}
}
