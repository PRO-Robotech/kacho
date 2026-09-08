// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Инъекция в ОБЕ стороны для TestEveryRegisterResourceProducerCarriesParentChain.
//
// Гейт, доказанный только красным, ловит форму, а не существо: первый же ложный
// срабат на законной конструкции той же формы приведёт к тому, что его отключат.
// Поэтому здесь красная половина одна, а зелёных — четыре, и каждая снята с
// реальной формы из дерева, а не выдумана.
//
// Вход красной половины — не догадка «как бы это выглядело», а форма, стоявшая в
// services/{vpc,compute,nlb,storage} до этой правки: запрос регистрации собран,
// область объявлена, цепь предков не названа.

// injectedChainlessRegister — производитель регистрации без цепи предков.
const injectedChainlessRegister = `package clients

import (
	"context"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

func apply(ctx context.Context, cli Client, p Payload) error {
	_, err := cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
		SubjectId:       p.SubjectID,
		Relation:        p.Relation,
		Object:          p.Object,
		Labels:          p.Labels,
		ParentProjectId: p.ParentProjectID,
	})
	return err
}
`

// lawfulChainNamed — ТА ЖЕ форма, но цепь названа и вычислена из области,
// объявленной этой же доставкой. Гейт обязан молчать.
const lawfulChainNamed = `package clients

import (
	"context"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
)

func apply(ctx context.Context, cli Client, p Payload) error {
	_, err := cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
		SubjectId:       p.SubjectID,
		Relation:        p.Relation,
		Object:          p.Object,
		Labels:          p.Labels,
		ParentProjectId: p.ParentProjectID,
		ParentChain:     ownerregister.ParentChain(nil, p.ParentProjectID, ""),
	})
	return err
}
`

// lawfulRootObject — объект, у которого предка НЕТ по построению: поле названо,
// значение вычислено, и вычисление даёт пустую цепь. Гейт обязан молчать —
// иначе он запрещает честно сказать «предков нет».
//
// Это и есть граница гейта, названная явно: он требует УТВЕРЖДЕНИЯ владельца о
// предках, а не непустоты цепи. Непустоту здесь потребовать нельзя: корневой
// объект (сам аккаунт, сам кластер) предка не имеет, и требование цепи от него
// было бы требованием того, чего нет в природе.
const lawfulRootObject = `package clients

import (
	"context"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

func apply(ctx context.Context, cli Client, p Payload) error {
	_, err := cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
		SubjectId:   p.SubjectID,
		Relation:    p.Relation,
		Object:      p.Object,
		ParentChain: chainOfRootLevelObject(p),
	})
	return err
}
`

// lawfulUnregisterHasNoChain — снятие регистрации адресуется ОБЪЕКТОМ, и цепь
// предков ему не нужна: контракт приёмной стороны её у снятия не принимает.
// Гейт по имени типа обязан отличать две формы, иначе потребует поля, которого
// у сообщения нет.
const lawfulUnregisterHasNoChain = `package clients

import (
	"context"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

func apply(ctx context.Context, cli Client, p Payload) error {
	_, err := cli.UnregisterResource(ctx, &iamv1.UnregisterResourceRequest{
		SubjectId: p.SubjectID,
		Relation:  p.Relation,
		Object:    p.Object,
	})
	return err
}
`

// lawfulZeroValueReturn — нулевое значение рядом с ошибкой. Полей не называет
// ни одного, поэтому производителем не является и цепи не должен.
const lawfulZeroValueReturn = `package repo

import (
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
)

func emit(ok bool) (ownerregister.Registration, error) {
	if !ok {
		return ownerregister.Registration{}, errors.New("nothing to emit")
	}
	return ownerregister.Registration{
		TraceID:     "res-1",
		ParentChain: ownerregister.ParentChain(nil, "prj-1", ""),
	}, nil
}
`

// forwardOnly — измерение (B) в чистом виде: поле НАЗВАНО везде, где надо, но
// нигде не заполняется — только пробрасывается дальше. Такой сервис проходит (A)
// и не несёт ни одного предка.
const forwardOnly = `package clients

import (
	"context"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

func apply(ctx context.Context, cli Client, p Payload) error {
	_, err := cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
		Object:      p.Object,
		ParentChain: p.ParentChain,
	})
	return err
}
`

// readerNotProducer — второй вход измерения (B), и он тоньше первого: значение
// приходит ВЫЗОВОМ, но вызовом без аргументов, то есть читателем чужого поля.
// Так читает пришедшую цепь принимающая сторона, и засчитывать ей производство
// значило бы считать сборкой любое чтение.
const readerNotProducer = `package internal_iam

import "context"

func handle(ctx context.Context, in Request) error {
	return store(ctx, Row{ParentChain: in.GetParentChain()})
}
`

// parse — разбор синтетического исходника; общая часть всех проб ниже.
func parseInjected(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "injected.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор синтетического исходника: %v", err)
	}
	return fset, file
}

// chainlessLiterals — какие координаты напечатал бы гейт на этом исходнике.
func chainlessLiterals(t *testing.T, src string) []string {
	t.Helper()
	fset, in := parseInjected(t, src)
	var out []string
	for _, lit := range parentChainBearingLiterals(in, ownerRegisterLocalName(in)) {
		if !lit.namesChain {
			out = append(out, lit.kind+"@"+fset.Position(lit.pos).String())
		}
	}
	return out
}

// TestParentChainGateRedOnChainlessRegister — верни дефект, и гейт краснеет,
// НАЗЫВАЯ координату. Без имени места находка бесполезна: гейт, печатающий
// «где-то в дереве что-то не так», снимают следующим коммитом.
func TestParentChainGateRedOnChainlessRegister(t *testing.T) {
	hits := chainlessLiterals(t, injectedChainlessRegister)
	if len(hits) == 0 {
		t.Fatal("гейт НЕ увидел запрос регистрации без цепи предков — он не поймал бы " +
			"ни один из четырёх реальных случаев, ради которых написан")
	}
	if hits[0][:len(litRequest)] != litRequest {
		t.Fatalf("гейт назвал не ту форму: %q", hits[0])
	}
}

// TestParentChainGateSilentOnLawfulForms — четыре законные формы гейт не
// задевает.
//
// Эта половина важнее красной: гейт, краснеющий на законной конструкции,
// отключат при первом же ложном срабатывании, и тогда он не поймает и настоящий
// дефект.
func TestParentChainGateSilentOnLawfulForms(t *testing.T) {
	for name, src := range map[string]string{
		"цепь названа и вычислена":         lawfulChainNamed,
		"объект без предка по построению":  lawfulRootObject,
		"снятие регистрации":               lawfulUnregisterHasNoChain,
		"нулевое значение рядом с ошибкой": lawfulZeroValueReturn,
	} {
		if hits := chainlessLiterals(t, src); len(hits) != 0 {
			t.Errorf("гейт краснеет на ЗАКОННОЙ форме (%s): %v", name, hits)
		}
	}
}

// TestParentChainProductionRecognisesOnlyRealAssembly — измерение (B) отличает
// сборку цепи от её передачи.
func TestParentChainProductionRecognisesOnlyRealAssembly(t *testing.T) {
	assembly := map[string]string{
		"вычисление из области": lawfulChainNamed,
		"явный литерал":         lawfulZeroValueReturn,
	}
	for name, src := range assembly {
		_, in := parseInjected(t, src)
		if got := chainProductionSites(in); got == 0 {
			t.Errorf("сборка цепи (%s) не распознана — измерение (B) объявило бы "+
				"честный сервис нарушителем", name)
		}
	}

	passthrough := map[string]string{
		"проброс поля":         forwardOnly,
		"чтение без сборки":    readerNotProducer,
		"регистрация без цепи": injectedChainlessRegister,
	}
	for name, src := range passthrough {
		_, in := parseInjected(t, src)
		if got := chainProductionSites(in); got != 0 {
			t.Errorf("передача (%s) засчитана за сборку (%d) — измерение (B) стало бы "+
				"вакуумным: сервис, не заполняющий цепь нигде, прошёл бы его", name, got)
		}
	}
}

// TestParentChainInjectionInputsAreDistinguishable — контроль самой пары
// инъекции: входы обязаны РАЗЛИЧАТЬСЯ по измеряемому признаку и содержать
// предмет гейта.
//
// Без этого утверждения пара доказывает лишь то, что распознавание что-то
// делает: окажись оба входа, например, лишены литерала регистрации, обе половины
// прошли бы вакуумно (реальный класс — «гейт читает свой недетерминизм как
// свойство»).
func TestParentChainInjectionInputsAreDistinguishable(t *testing.T) {
	bearing := map[string]string{
		"дефект":       injectedChainlessRegister,
		"цепь названа": lawfulChainNamed,
		"объект без предка по построению": lawfulRootObject,
		"снятие регистрации":              lawfulUnregisterHasNoChain,
		"проброс поля":                    forwardOnly,
	}
	for name, src := range bearing {
		_, in := parseInjected(t, src)
		if name == "снятие регистрации" {
			// У снятия предмета быть и НЕ должно — это и проверяем: форма
			// распознаётся отдельно от регистрации, а не «заодно».
			if got := len(parentChainBearingLiterals(in, ownerRegisterLocalName(in))); got != 0 {
				t.Errorf("форма снятия принята за регистрацию (%d литералов) — гейт "+
					"потребовал бы поля, которого у этого сообщения нет", got)
			}
			continue
		}
		if got := len(parentChainBearingLiterals(in, ownerRegisterLocalName(in))); got == 0 {
			t.Errorf("%s: не содержит литерала, обязанного нести цепь — вход инъекции "+
				"не является предметом гейта, и обе половины пары прошли бы вакуумно", name)
		}
	}
}
