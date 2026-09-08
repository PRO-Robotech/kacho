// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Инъекция в ОБЕ стороны для TestOwnerRegistrationCarriesWriterTxVersion.
//
// Гейт, доказанный только красным, ловит форму, а не существо: первый же ложный
// срабат на законной конструкции той же формы приведёт к тому, что его отключат.
// Поэтому здесь два утверждения, и второе не менее важно первого.
//
// Вход обоих — не выдумка про то, «как бы это выглядело», а форма, снятая с
// дерева ДО правки: у четырёх сервисов маркер версии брался с часов момента
// доставки прямо в функции, собирающей запрос регистрации.

// injectedDefect — путь регистрации, вернувший часы момента доставки. Ровно то,
// что стояло в services/{storage,compute,nlb,registry} до 2026-08-10.
const injectedDefect = `package clients

import (
	"context"
	"time"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *SyncRegistrar) Register(ctx context.Context, items []Item) error {
	sv := timestamppb.New(time.Now())
	for _, it := range items {
		if _, err := s.cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
			SubjectId:     it.SubjectID,
			Object:        it.Object,
			SourceVersion: sv,
		}); err != nil {
			return err
		}
	}
	return nil
}
`

// lawfulTwin — ТА ЖЕ форма (тот же тип запроса, тот же цикл, то же поле), но
// версия приходит ПАРАМЕТРОМ из writer-транзакции. Гейт обязан молчать: иначе он
// запрещает не то, ради чего написан.
const lawfulTwin = `package clients

import (
	"context"
	"time"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *SyncRegistrar) Register(ctx context.Context, items []Item, sourceVersion time.Time) error {
	sv := timestamppb.New(sourceVersion)
	for _, it := range items {
		if _, err := s.cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
			SubjectId:     it.SubjectID,
			Object:        it.Object,
			SourceVersion: sv,
		}); err != nil {
			return err
		}
	}
	return nil
}
`

// lawfulClockElsewhere — второй законный близнец, и он тоньше первого: часы
// читаются в ТОМ ЖЕ ФАЙЛЕ, но в ДРУГОЙ функции, которая запроса регистрации не
// собирает. Гейт по файлу (а не по функции) на этом бы покраснел — и был бы
// снят как мешающий, потому что мерить время рядом никто не запрещал.
const lawfulClockElsewhere = `package clients

import (
	"context"
	"log/slog"
	"time"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *SyncRegistrar) Register(ctx context.Context, items []Item, sourceVersion time.Time) error {
	sv := timestamppb.New(sourceVersion)
	for _, it := range items {
		if _, err := s.cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
			SubjectId:     it.SubjectID,
			Object:        it.Object,
			SourceVersion: sv,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *SyncRegistrar) observe(started time.Time) {
	slog.Info("register delivery", "took", time.Since(started), "at", time.Now())
}
`

// clocksInRequestBuilders — прогон распознавания гейта по одному исходнику:
// сколько координат он бы напечатал.
func clocksInRequestBuilders(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "injected.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор синтетического исходника: %v", err)
	}
	var hits []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if countRegisterRequestLiterals(fn) == 0 {
			continue
		}
		for _, c := range deliveryClocksIn(fn) {
			hits = append(hits, c.call+"@"+fset.Position(c.pos).String())
		}
	}
	return hits
}

// TestGateRedOnInjectedDeliveryClock — верни дефект, и гейт краснеет, НАЗЫВАЯ
// координату. Без имени места находка бесполезна: гейт, печатающий «где-то в
// дереве что-то не так», снимают следующим коммитом.
func TestGateRedOnInjectedDeliveryClock(t *testing.T) {
	hits := clocksInRequestBuilders(t, injectedDefect)
	if len(hits) == 0 {
		t.Fatal("гейт НЕ увидел часы момента доставки в функции, собирающей запрос регистрации — " +
			"он не поймал бы ни один из четырёх реальных случаев, ради которых написан")
	}
	if hits[0][:8] != "time.Now" {
		t.Fatalf("гейт назвал не тот вызов: %q", hits[0])
	}
}

// TestGateSilentOnLawfulTwin — та же форма с версией ИЗ ПАРАМЕТРА гейт не
// задевает.
//
// Эта половина важнее красной: гейт, краснеющий на законной конструкции,
// отключат при первом же ложном срабатывании, и тогда он не поймает и настоящий
// дефект.
func TestGateSilentOnLawfulTwin(t *testing.T) {
	if hits := clocksInRequestBuilders(t, lawfulTwin); len(hits) != 0 {
		t.Fatalf("гейт краснеет на ЗАКОННОЙ форме (версия из writer-транзакции параметром): %v", hits)
	}
}

// TestGateSilentOnClockOutsideTheRequestBuilder — часы в СОСЕДНЕЙ функции того
// же файла гейт не задевает.
//
// Разница между «функция» и «файл» здесь не педантизм: измерять время рядом с
// доставкой законно и обычно (логирование длительности), и гейт по файлу
// запрещал бы это без всякого основания.
func TestGateSilentOnClockOutsideTheRequestBuilder(t *testing.T) {
	if hits := clocksInRequestBuilders(t, lawfulClockElsewhere); len(hits) != 0 {
		t.Fatalf("гейт краснеет на часах в функции, которая запроса регистрации НЕ собирает: %v", hits)
	}
}

// TestInjectionInputsAreDistinguishable — контроль самой пары инъекции: два
// входа обязаны РАЗЛИЧАТЬСЯ по измеряемому признаку.
//
// Без этого утверждения пара доказывает лишь то, что распознавание что-то
// делает: если бы оба входа оказались, например, синтаксически невалидны или
// оба лишены литерала запроса, обе пробы прошли бы, а гейт не был бы проверен
// ничем (реальный класс — «гейт читает свой недетерминизм как свойство»).
func TestInjectionInputsAreDistinguishable(t *testing.T) {
	for name, src := range map[string]string{"дефект": injectedDefect, "близнец": lawfulTwin, "часы рядом": lawfulClockElsewhere} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: не разбирается: %v", name, err)
		}
		found := 0
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				found += countRegisterRequestLiterals(fn)
			}
		}
		if found == 0 {
			t.Fatalf("%s: не содержит литерала запроса регистрации — вход инъекции не является "+
				"предметом гейта, и обе половины пары прошли бы вакуумно", name)
		}
	}
}
