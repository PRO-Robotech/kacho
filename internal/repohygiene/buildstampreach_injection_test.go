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
RUN go build -ldflags "-X main.buildVersion=$KACHO_IMAGE_VERSION -X main.buildCommit=$KACHO_IMAGE_REVISION" -o /alpha ./services/alpha/cmd/alpha
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
RUN go build -ldflags "-X main.buildVersion=$KACHO_IMAGE_VERSION" -o /alpha ./services/alpha/cmd/alpha
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
    -ldflags "-X main.buildVersion=$KACHO_IMAGE_VERSION \
      -X main.buildCommit=$KACHO_IMAGE_REVISION" \
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
