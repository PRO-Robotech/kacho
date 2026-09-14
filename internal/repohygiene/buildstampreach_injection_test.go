// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Инъекция держателя «подстановка доходит до объявившего» — В ОБЕ СТОРОНЫ И ПО
// КАЖДОЙ ОСИ.
//
// Осей у разбора четыре, и каждая — отдельный способ остаться зелёным при
// сломанном свойстве:
//
//  1. ПОДСТАНОВКА ЕСТЬ ИЛИ НЕТ. Сборка без `-ldflags` — не штампует; та же
//     сборка с подстановкой обоих символов — штампует. Дельта миров ОДИН факт.
//  2. ОБА СИМВОЛА. Подстановка одного из двух — не штамп: компоновщик молчит о
//     промахе, и половина витрины отвечала бы умолчанием.
//  3. ИСПОЛНЯЕМАЯ ЧАСТЬ. `-ldflags`, названный КОММЕНТАРИЕМ файла сборки, — не
//     подстановка. Без этой оси держатель нашёл бы собственное объяснение.
//  4. ФОРМА ПУТИ ПАКЕТА. Сборка из корня дерева (`./services/x/cmd/x`) и из
//     своего модуля (`./cmd/x`) — обе законны, и обе обязаны опознаваться;
//     `go list -deps` тем же путём сборкой НЕ является.
//  5. ВИДИМОСТЬ АРГУМЕНТА В СВОЕЙ СТУПЕНИ. `-X`, берущий значение из аргумента,
//     объявленного в ДРУГОЙ ступени, даёт пустую подстановку — штамп, молча
//     ставший «не проставлено». Ось заведена задачей #2527: до неё держатель на
//     таком файле сборки печатал «ставит» и МОЛЧАЛ, то есть зеленел ровно на
//     сломанном свойстве. Законный близнец — тот же файл с `ARG` в ступени
//     сборки.
//
// Плюс отдельно — что перепись без предмета не выдаётся за «нарушений нет»:
// пустой корпус даёт ОШИБКУ, а не тихий зелёный.
package repohygiene

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// srcDeclaresStamp — композиционный корень, объявляющий штамп.
const srcDeclaresStamp = `package main

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)
`

// dockerNoLdflags — сборка БЕЗ подстановки.
const dockerNoLdflags = `FROM golang AS builder
RUN go build -o /alpha ./services/alpha/cmd/alpha
`

// dockerWithLdflags — ЗАКОННЫЙ БЛИЗНЕЦ предыдущего: та же сборка, отличается
// ОДНИМ фактом — подстановкой обоих символов.
const dockerWithLdflags = `FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION -X main.buildCommit=$OCI_IMAGE_REVISION" -o /alpha ./services/alpha/cmd/alpha
`

func auditOne(t *testing.T, goPath, goBody, dockerPath, dockerBody string) StampedBinary {
	t.Helper()
	files := map[string][]byte{goPath: []byte(goBody)}
	if dockerPath != "" {
		files[dockerPath] = []byte(dockerBody)
	}
	binaries, census, err := AuditBuildStampReach(files)
	require.NoError(t, err)
	require.Equalf(t, 1, census.Declaring, "перепись обязана видеть РОВНО одного объявившего")
	require.Len(t, binaries, 1)
	return binaries[0]
}

func TestBuildStampInjection_BuildWithoutLdflagsDoesNotStamp(t *testing.T) {
	t.Parallel()
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", dockerNoLdflags)
	require.False(t, b.Stamped, "сборка без -ldflags штампа не ставит")
	require.NotEmptyf(t, b.BuiltBy, "сборка обязана быть НАЙДЕНА — иначе находка назвала бы "+
		"«образа нет» там, где он есть, и послала бы читателя не туда")
}

func TestBuildStampInjection_BuildWithLdflagsStamps(t *testing.T) {
	t.Parallel()
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", dockerWithLdflags)
	require.True(t, b.Stamped, "законный близнец обязан МОЛЧАТЬ: подстановка обоих символов есть")
}

func TestBuildStampInjection_HalfTheSymbolsIsNotAStamp(t *testing.T) {
	t.Parallel()
	half := `FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION" -o /alpha ./services/alpha/cmd/alpha
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", half)
	require.Falsef(t, b.Stamped, "подстановка ОДНОГО символа из двух штампом не является: "+
		"компоновщик о промахе второго не сообщает")
}

func TestBuildStampInjection_LdflagsInACommentIsNotAStamp(t *testing.T) {
	t.Parallel()
	prose := `FROM golang AS builder
# версия инжектится через -ldflags "-X main.buildVersion=… -X main.buildCommit=…"
RUN go build -o /alpha ./services/alpha/cmd/alpha
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", prose)
	require.Falsef(t, b.Stamped, "держатель, читающий сырой текст, признал бы штампом "+
		"собственное объяснение и остался бы зелёным при снятой подстановке")
}

func TestBuildStampInjection_ContinuationLinesAreStitched(t *testing.T) {
	t.Parallel()
	split := `FROM golang AS builder
RUN go build \
    -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION \
      -X main.buildCommit=$OCI_IMAGE_REVISION" \
    -o /alpha ./services/alpha/cmd/alpha
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", split)
	require.Truef(t, b.Stamped, "команда разнесена по строкам: построчный разбор увидел бы "+
		"половину и объявил бы штамп отсутствующим")
}

func TestBuildStampInjection_PackagePathRelativeToItsOwnModule(t *testing.T) {
	t.Parallel()
	ownModule := `FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=$V -X main.buildCommit=$R" -o /alpha ./cmd/alpha
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", ownModule)
	require.Truef(t, b.Stamped, "сборка из СВОЕГО модуля — законная форма: требовать полный "+
		"путь значило бы объявить её ненаблюдаемой")
}

func TestBuildStampInjection_GoListIsNotABuild(t *testing.T) {
	t.Parallel()
	listOnly := `FROM golang AS builder
RUN go list -deps ./services/alpha/cmd/alpha >/dev/null
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", listOnly)
	require.Emptyf(t, b.BuiltBy, "перечисление зависимостей сборкой не является: приняв его "+
		"за сборку, держатель объявил бы образ существующим и промолчал бы о его отсутствии")
	require.False(t, b.Stamped)
}

func TestBuildStampInjection_NoImageAtAllIsItsOwnFinding(t *testing.T) {
	t.Parallel()
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp, "", "")
	require.Empty(t, b.BuiltBy, "образа нет — и это отдельная находка, а не «не подставляет»")
	require.False(t, b.Stamped)
}

func TestBuildStampInjection_NonMainPackageIsNotADeclarer(t *testing.T) {
	t.Parallel()
	_, census, err := AuditBuildStampReach(map[string][]byte{
		"internal/x/x.go": []byte("package x\n\nvar buildVersion = \"dev\"\n"),
	})
	require.NoError(t, err)
	require.Zerof(t, census.Declaring, "штамп несёт КОМПОЗИЦИОННЫЙ КОРЕНЬ: переменная "+
		"библиотечного пакета компоновщику по имени main.* недоступна")
}

func TestBuildStampInjection_SecondarySymbolAloneIsNotADeclarer(t *testing.T) {
	t.Parallel()
	_, census, err := AuditBuildStampReach(map[string][]byte{
		"cmd/alpha/main.go": []byte("package main\n\nvar buildCommit = \"unknown\"\n"),
	})
	require.NoError(t, err)
	require.Zerof(t, census.Declaring, "один второй символ штампа не составляет — иначе "+
		"держатель требовал бы подстановки от всякого, кто назвал ревизию")
}

func TestBuildStampInjection_TestFileIsNotADeclarer(t *testing.T) {
	t.Parallel()
	_, census, err := AuditBuildStampReach(map[string][]byte{
		"cmd/alpha/main_test.go": []byte(srcDeclaresStamp),
	})
	require.NoError(t, err)
	require.Zerof(t, census.Declaring, "фикстура пробы, объявившая символы, выдала бы "+
		"несуществующий двоичный файл за объявивший штамп")
}

func TestBuildStampInjection_EmptyCorpusIsAnError(t *testing.T) {
	t.Parallel()
	_, _, err := AuditBuildStampReach(map[string][]byte{})
	require.Errorf(t, err, "пустой корпус обязан быть ОШИБКОЙ, а не тихим зелёным: "+
		"молчание держателя на нулевом обходе ничего не утверждает")
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ 5 — ВИДИМОСТЬ АРГУМЕНТА В СТУПЕНИ СБОРКИ (#2527).
//
// Дельта миров ниже — ОДИН факт: строка `ARG` в ступени сборки. Всё остальное —
// подстановка, символы, путь пакета, имя ступени — совпадает дословно.

// dockerArgInWrongStage — аргументы объявлены в КОНЕЧНОЙ ступени (там они нужны
// клейму), а сборка идёт в первой. Ровно та форма, что лежала в дереве до #2527.
const dockerArgInWrongStage = `FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION -X main.buildCommit=$OCI_IMAGE_REVISION" -o /alpha ./services/alpha/cmd/alpha

FROM alpine
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$OCI_IMAGE_REVISION"
`

// dockerArgInBuilderStage — ЗАКОННЫЙ БЛИЗНЕЦ: те же две ступени, то же клеймо,
// та же строка сборки; отличие ОДНО — аргументы объявлены и в ступени сборки.
const dockerArgInBuilderStage = `FROM golang AS builder
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
RUN go build -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION -X main.buildCommit=$OCI_IMAGE_REVISION" -o /alpha ./services/alpha/cmd/alpha

FROM alpine
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$OCI_IMAGE_REVISION"
`

func TestBuildStampInjection_ArgFromAnotherStageIsAnEmptySubstitution(t *testing.T) {
	t.Parallel()
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", dockerArgInWrongStage)
	require.Truef(t, b.Stamped, "подстановка НАЗВАНА — и именно поэтому одной оси «есть "+
		"ли -ldflags» мало: файл выглядит проставленным")
	require.ElementsMatchf(t, []string{"OCI_IMAGE_REVISION", "OCI_IMAGE_VERSION"},
		b.ArgsUnseen, "оба аргумента объявлены в ЧУЖОЙ ступени: в ступени сборки они "+
			"не видны, и компоновщик впишет пустую строку")
}

func TestBuildStampInjection_ArgDeclaredInBuilderStageIsSilent(t *testing.T) {
	t.Parallel()
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", dockerArgInBuilderStage)
	require.True(t, b.Stamped)
	require.Emptyf(t, b.ArgsUnseen, "законный близнец обязан МОЛЧАТЬ: дельта с предыдущим "+
		"миром — одна строка `ARG` в ступени сборки, и она здесь есть")
}

func TestBuildStampInjection_ArgDeclaredAfterTheBuildIsStillUnseen(t *testing.T) {
	t.Parallel()
	late := `FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=$V -X main.buildCommit=$R" -o /alpha ./services/alpha/cmd/alpha
ARG V=""
ARG R=""
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", late)
	require.ElementsMatchf(t, []string{"R", "V"}, b.ArgsUnseen, "аргумент действует с места "+
		"объявления и НИЖЕ: объявленный после сборки, для неё он не существует, и "+
		"разбор, судящий по файлу целиком, прощал бы промах порядка")
}

func TestBuildStampInjection_GlobalArgBeforeFirstStageIsNotVisibleInside(t *testing.T) {
	t.Parallel()
	global := `ARG OCI_IMAGE_VERSION=""
ARG OCI_IMAGE_REVISION=""
FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION -X main.buildCommit=$OCI_IMAGE_REVISION" -o /alpha ./services/alpha/cmd/alpha
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", global)
	require.ElementsMatchf(t, []string{"OCI_IMAGE_REVISION", "OCI_IMAGE_VERSION"},
		b.ArgsUnseen, "аргумент до первого `FROM` виден строкам `FROM`, а ВНУТРИ ступени "+
			"требует повторного объявления: считать его видимым значило бы прощать "+
			"ровно тот промах, ради которого ось заведена")
}

func TestBuildStampInjection_LiteralValueReferencesNoArgAndIsNotJudgedHere(t *testing.T) {
	t.Parallel()
	literal := `FROM golang AS builder
RUN go build -ldflags "-X main.buildVersion=v1.2.3 -X main.buildCommit=deadbeef" -o /alpha ./services/alpha/cmd/alpha
`
	b := auditOne(t, "services/alpha/cmd/alpha/main.go", srcDeclaresStamp,
		"services/alpha/Dockerfile", literal)
	require.True(t, b.Stamped, "литерал ДОЕДЕТ до двоичного файла — подстановка состоялась")
	require.Emptyf(t, b.ArgsUnseen, "ссылки на аргумент нет, значит и невидимого аргумента "+
		"нет: держатель не вправе краснеть на том, чего не судит. Что литерал — это "+
		"ВТОРАЯ величина об одном предмете, названо в шапке отдельно и здесь не судится")
}
