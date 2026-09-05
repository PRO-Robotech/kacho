// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт принятия носителя СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// Обе пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditHostAdoption`), на
// синтетическом дереве: проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthRootOwnAssembly — корень, который собирает сервер САМ. Это и есть дефект.
const synthRootOwnAssembly = `package main

import (
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

func serve() {
	var unary []grpc.UnaryServerInterceptor
	_ = grpcsrv.NewServer(grpc.ChainUnaryInterceptor(unary...))
}
`

// synthRootOnHost — тот же корень, переведённый на носитель: приносит ДАННЫЕ и
// регистрирует обработчики. Сервера в нём нет, цепочки тоже.
const synthRootOnHost = `package main

import (
	"context"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"
)

func serve(ctx context.Context, d servicecontract.Descriptor) error {
	return servicehost.Serve(ctx, d,
		func(reg grpc.ServiceRegistrar) {},
		func(reg grpc.ServiceRegistrar) {},
	)
}
`

// synthRootDecoy — ЛОВУШКА ТЕМЫ, и она неудобна для гейта нарочно.
//
// Имя конструктора и оба имени интерсепторных типов стоят здесь в комментарии и
// в строковом литерале — то есть ровно там, где текстовый гейт нашёл бы их и
// покраснел на исправном коде. Исполняемая часть при этом чиста: сервер не
// собирается, значений интерсепторного типа нет.
const synthRootDecoy = `package main

// Историческая справка: до перевода на носитель этот корень звал
// grpcsrv.NewServer и держал свои grpc.UnaryServerInterceptor и
// grpc.StreamServerInterceptor. Теперь всё это в pkg/servicehost.
func doc() string {
	return "grpcsrv.NewServer + grpc.UnaryServerInterceptor + grpc.StreamServerInterceptor"
}
`

// synthAdoptionTree строит минимальное дерево с ОДНИМ композиционным корнем.
//
// Файлы кладутся в индекс git: гейт берёт состав у `pkg/treecorpus`, то
// есть у git, а не у диска. Фикстура, которая этого не делает, оставляет гейт с
// пустым составом — и он молчит не потому, что нарушения нет, а потому, что
// смотреть было не на что. Направление «гейт обязан покраснеть» такую пустоту от
// исправного дерева не отличило бы.
func synthAdoptionTree(t *testing.T, svc string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module github.com/PRO-Robotech/kacho\n\ngo 1.25\n")
	for rel, body := range files {
		write("services/"+svc+"/cmd/"+svc+"/"+rel, body)
	}
	// Край обязан существовать в синтетическом дереве, потому что обход гейта
	// требует ОБА поддерева и отказывает на отсутствующем: «состав взять неоткуда»
	// — это отказ, а не пустой успех. Файл заведомо чистый: предмет проб инъекции
	// — распознавание сборки у СЕРВИСА, и посторонняя находка на крае смазала бы
	// счёт.
	write("gateway/cmd/api-gateway/main.go", "package main\n\nfunc main() {}\n")
	synthTrack(t, root)
	return root
}

// TestAdoptionGateRedOnOwnAssembly — направление (а): корень собирает сервер сам
// → гейт находит это И НАЗЫВАЕТ КООРДИНАТУ. Без координаты находка не действие.
func TestAdoptionGateRedOnOwnAssembly(t *testing.T) {
	root := synthAdoptionTree(t, "demo", map[string]string{"serve.go": synthRootOwnAssembly})
	res := auditHostAdoption(t, root)
	t.Log(res.summary)

	if len(res.findings) == 0 {
		t.Fatalf("корень собирает сервер сам, а гейт молчит — он не способен упасть.\n%s", res.summary)
	}
	var joined []string
	for _, f := range res.findings {
		joined = append(joined, f.service+" "+f.where+" "+f.what)
	}
	all := strings.Join(joined, "\n")
	if !strings.Contains(all, "services/demo/cmd/demo/serve.go") {
		t.Fatalf("находка не называет файл — по ней нечего чинить:\n%s", all)
	}
	// Обе половины предмета обязаны быть названы порознь: конструктор сервера и
	// значение интерсепторного типа — разные способы завести вторую поверхность.
	if !strings.Contains(all, "конструктора") {
		t.Fatalf("гейт не назвал вызов конструктора сервера:\n%s", all)
	}
	if !strings.Contains(all, "UnaryServerInterceptor") {
		t.Fatalf("гейт не назвал значение интерсепторного типа:\n%s", all)
	}
	t.Logf("направление (а): гейт покраснел и назвал координату:\n%s", all)
}

// TestAdoptionGateSilentOnConvertedRoot — направление (б): переведённый корень
// той же формы гейта не задевает.
func TestAdoptionGateSilentOnConvertedRoot(t *testing.T) {
	root := synthAdoptionTree(t, "demo", map[string]string{"serve.go": synthRootOnHost})
	res := auditHostAdoption(t, root)
	t.Log(res.summary)

	if len(res.findings) != 0 {
		t.Fatalf("переведённый корень объявлен находкой — гейт ловит форму, а не существо: %+v",
			res.findings)
	}
	if res.roots == 0 {
		t.Fatalf("гейт не засчитал ни одного корня — молчание означает «не нашёл», "+
			"а не «всё чисто».\n%s", res.summary)
	}
}

// TestAdoptionGateIgnoresProseDecoyEvenNextToADefect — прямая проверка ловушки:
// приманка лежит рядом со СНЯТЫМ переводом. Гейт обязан покраснеть на настоящем
// дефекте и НЕ засчитать приманку.
//
// Эта проба и есть доказательство, что распознавание идёт по исполняемой части:
// без неё «гейт читает AST, а не текст» осталось бы утверждением о намерении.
func TestAdoptionGateIgnoresProseDecoyEvenNextToADefect(t *testing.T) {
	// Только приманка — гейт обязан МОЛЧАТЬ.
	quiet := auditHostAdoption(t, synthAdoptionTree(t, "demo",
		map[string]string{"doc.go": synthRootDecoy}))
	if len(quiet.findings) != 0 {
		t.Fatalf("имя конструктора в комментарии и в строковом литерале засчитано за сборку — "+
			"гейт читает текст, а не исполняемую часть: %+v", quiet.findings)
	}

	// Приманка РЯДОМ с настоящим дефектом — гейт обязан покраснеть, и ровно на
	// дефекте.
	loud := auditHostAdoption(t, synthAdoptionTree(t, "demo", map[string]string{
		"doc.go":   synthRootDecoy,
		"serve.go": synthRootOwnAssembly,
	}))
	if len(loud.findings) == 0 {
		t.Fatalf("приманка заслонила настоящий дефект.\n%s", loud.summary)
	}
	for _, f := range loud.findings {
		if strings.Contains(f.where, "doc.go") {
			t.Fatalf("находка указывает на приманку вместо дефекта: %s — %s", f.where, f.what)
		}
	}
	t.Logf("ловушка не обманула гейт в обе стороны: молча на приманке, красно на дефекте (%d находок)",
		len(loud.findings))
}

// TestAdoptionExceptionsExpireWhenTheirSubjectIsGone — самоистечение перечня
// ожидающих перевода, доказанное ИНЪЕКЦИЕЙ, а не прочтением.
//
// Сервис, попавший в перечень и при этом уже переведённый, обязан дать находку:
// иначе запись переживает свой предмет и укрывает следующую сборку, которую в
// этом корне заведут.
func TestAdoptionExceptionsExpireWhenTheirSubjectIsGone(t *testing.T) {
	// Берём ЛЮБОЕ имя из действующего перечня и строим дерево, где этот сервис
	// уже переведён. Имя берётся из самого перечня, а не выписывается: выписанное
	// пережило бы правку перечня и проба стала бы вакуумной.
	var excused string
	for svc := range hostAdoptionExceptions {
		excused = svc
		break
	}
	if excused == "" {
		t.Skip("перечень ожидающих перевода пуст — истекать нечему")
	}
	root := synthAdoptionTree(t, excused, map[string]string{"serve.go": synthRootOnHost})
	res := auditHostAdoption(t, root)
	if len(res.findings) != 0 {
		t.Fatalf("переведённый корень дал находки: %+v", res.findings)
	}
	// Гейт по дереву объявляет такую запись просроченной; здесь фиксируем сам
	// вход: ноль находок у сервиса, который числится ожидающим.
	t.Logf("вход самоистечения построен: %q числится ожидающим перевода, находок у него 0 — "+
		"гейт по дереву обязан на этом покраснеть", excused)
}
