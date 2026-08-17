// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// leaserefusalassuccess_injection_test.go — доказательство того, что гейт
// `TestReleaseLaneNeverReadsARefusalAsProofOfRelease` СПОСОБЕН упасть.
//
// Проверка без такого доказательства неотличима от проверки, которая всегда
// зелена: обе печатают «находок 0». Инъекция ведётся В ОБЕ СТОРОНЫ — дефект
// краснеет с координатой, а законный близнец той же формы молчит. Без второй
// половины гейт ловил бы ФОРМУ, и первый же ложный срабат его отключил бы.
//
// Инъекция идёт по СИНТЕТИЧЕСКОМУ дереву во временном каталоге: трогать рабочую
// копию ради пробы нельзя — состояние, которого проба не заводила, ей не
// принадлежит.
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// synthPkg раскладывает один Go-файл как пакет и отдаёт ПЕРЕЧЕНЬ путей.
//
// Перечень, а не каталог: боевой гейт берёт состав у индекса git, а синтетика
// лежит вне рабочего дерева — там индекса нет. Разбор отделён от получения
// состава именно ради этого, и инъекция проверяет ТОТ ЖЕ разбор, что исполняется
// на дереве, а не свою копию.
func synthPkg(t *testing.T, name, src string) []string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("подготовка синтетики: %v", err)
	}
	path := filepath.Join(dir, "lane.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("подготовка синтетики: %v", err)
	}
	return []string{path}
}

// ---------------------------------------------------------------------------
// Свойство (б): отказ, возвращённый успехом.
// ---------------------------------------------------------------------------

// Форма 1 — `if`. Именно так были записаны ДВА из трёх исходных мест, поэтому
// предикат, узнающий только `case`, промахнулся бы мимо обоих.
const injIfForm = `package lane

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func release(rerr error) error {
	if st, ok := status.FromError(rerr); ok && st.Code() == codes.NotFound {
		return nil
	}
	return rerr
}
`

// Форма 2 — `case`. Так было записано третье место.
const injCaseForm = `package lane

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func release(rerr error) error {
	st, _ := status.FromError(rerr)
	switch st.Code() {
	case codes.NotFound:
		return nil
	default:
		return rerr
	}
}
`

// Форма 3 — полоса общего носителя. Тот же исход другим словарём: гейт обязан
// узнавать существо, а не конкретное имя кода.
const injLaneForm = `package lane

import "github.com/PRO-Robotech/kacho/pkg/peer"

func release(rerr error) error {
	switch peer.Classify(rerr) {
	case peer.OutcomeMissing:
		return nil
	}
	return rerr
}
`

func TestGateReds_WhenARefusalIsReturnedAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"ветвление через if", injIfForm},
		{"ветвление через case", injCaseForm},
		{"полоса общего носителя", injLaneForm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := synthPkg(t, "swallow", tc.src)
			hits, census := scanRefusalsReturnedAsSuccess(t, files)
			if census.files == 0 {
				t.Fatalf("инъекция не прочитана — доказывать было бы нечего")
			}
			if len(hits) == 0 {
				t.Fatalf("гейт НЕ УВИДЕЛ возвращённый дефект (%s): он не способен упасть, "+
					"а значит его зелёное ничего не значит", tc.name)
			}
			if census.notFounds == 0 {
				t.Fatalf("перепись не засчитала ветвление — «ноль находок» стало бы " +
					"неотличимо от «ноль рассмотренного»")
			}
			t.Logf("краснеет с координатой: %v (перепись: %s)", hits, census)
		})
	}
}

// ---------------------------------------------------------------------------
// Законные близнецы: та же форма, ДРУГОЙ исход. На них гейт обязан молчать.
// ---------------------------------------------------------------------------

// Близнец 1 — та же ветка возвращает ОШИБКУ. Ровно так записаны выжившие места
// в дереве, и снимать их нельзя.
const twinRefusalStaysRefusal = `package lane

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func release(rerr error) error {
	if st, ok := status.FromError(rerr); ok && st.Code() == codes.NotFound {
		return fmt.Errorf("lease is not releasable: %w", rerr)
	}
	return rerr
}
`

// Близнец 2 — успех возвращается там, где о «не найдено» речи не идёт вовсе.
const twinUnrelatedSuccess = `package lane

func release(ok bool) error {
	if ok {
		return nil
	}
	return nil
}
`

// Близнец 3 — «не найдено» упоминается, но успехом не оборачивается: значение
// кладётся в поле ответа, что и есть починенная форма.
const twinOutcomeIsNamed = `package lane

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type outcome string

func release(rerr error) (outcome, error) {
	if st, ok := status.FromError(rerr); ok && st.Code() == codes.NotFound {
		return "", status.Error(codes.FailedPrecondition, "owner does not serve the release verb")
	}
	return "RELEASED", rerr
}
`

func TestGateStaysSilent_OnLegitimateTwins(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"отказ остаётся отказом", twinRefusalStaysRefusal},
		{"успех не про «не найдено»", twinUnrelatedSuccess},
		{"исход назван полем", twinOutcomeIsNamed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := synthPkg(t, "twin", tc.src)
			hits, census := scanRefusalsReturnedAsSuccess(t, files)
			if census.files == 0 {
				t.Fatalf("близнец не прочитан — молчание ничего не доказывает")
			}
			if len(hits) != 0 {
				t.Fatalf("гейт покраснел на ЗАКОННОЙ конструкции (%s): %v. "+
					"Он ловит форму, а не существо, и первый же ложный срабат его отключит",
					tc.name, hits)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Свойство (а): вызов публичного удаления адреса.
// ---------------------------------------------------------------------------

const injPublicDelete = `package lane

import "context"

type stub interface {
	Delete(ctx context.Context, req any) (any, error)
}

type client struct{ addrs stub }

func (c *client) free(ctx context.Context) error {
	_, err := c.addrs.Delete(ctx, nil)
	return err
}
`

// Законный близнец к (а): ЧТЕНИЕ у того же клиента остаётся законным — оно
// ничего необратимого не делает и на нём гейт обязан молчать.
const twinPublicGet = `package lane

import "context"

type stub interface {
	Get(ctx context.Context, req any) (any, error)
}

type client struct{ addrs stub }

func (c *client) read(ctx context.Context) error {
	_, err := c.addrs.Get(ctx, nil)
	return err
}
`

func TestGateReds_WhenPublicAddressDeleteIsCalled(t *testing.T) {
	files := synthPkg(t, "publicdelete", injPublicDelete)
	hits, census := scanPublicAddressDelete(t, files)
	if census.files == 0 {
		t.Fatalf("инъекция не прочитана — доказывать было бы нечего")
	}
	if len(hits) == 0 {
		t.Fatalf("гейт НЕ УВИДЕЛ вызов публичного удаления адреса: его зелёное ничего не значит")
	}
	t.Logf("краснеет с координатой: %v", hits)
}

func TestGateStaysSilent_OnPublicRead(t *testing.T) {
	files := synthPkg(t, "publicget", twinPublicGet)
	hits, census := scanPublicAddressDelete(t, files)
	if census.files == 0 {
		t.Fatalf("близнец не прочитан — молчание ничего не доказывает")
	}
	if len(hits) != 0 {
		t.Fatalf("гейт покраснел на ЧТЕНИИ: %v. Читать у владельца законно — "+
			"необратимого шага на этом не строится", hits)
	}
}
