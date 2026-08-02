// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredrpcsurface.go — надгробие снятой RPC-поверхности.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ МЕХАНИЗМ, А НЕ `reserved`. Норма снятия с контракта требует
// удалять объявление «с резервированием номера И имени». Для ПОЛЯ сообщения это
// выразимо и обязательно. Для СЕРВИСА и его метода — не выразимо: грамматика
// protobuf принимает `reserved` только внутри `message` и `enum`, внутри
// `service` компилятор отвечает `Expected "rpc"`, а у метода нет номера, который
// можно было бы зарезервировать. Проверяется одной командой:
//
//	printf 'syntax="proto3";package t;service S{reserved "Foo";}' > a.proto
//	protoc --proto_path=. --descriptor_set_out=/dev/null a.proto
//
// Значит намерение нормы («имя не должно вернуться молча и с чужим смыслом»)
// нужно выразить тем, что в этом дереве выразимо — переписью снятых имён и
// гейтом, который краснеет на их повторное появление.
//
// ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ ГЕЙТА ДОСТИЖИМОСТИ (catalogreachability.go). Тот
// спрашивает «у объявленного метода есть ли листенер» и ловит объявление БЕЗ
// реализации. Он по построению молчит на снятом имени, вернувшемся ВМЕСТЕ с
// реализацией: такой сервис смонтирован, его строки каталога резолвятся, и
// вердикт зелёный. Между тем именно этот случай — возвращение имени с уже
// готовой полосой авторизации, выбранной когда-то под другой замысел, — и есть
// то, ради чего резервируют имена. Два гейта отвечают на разные вопросы и не
// подменяют друг друга.
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RetiredRPC — одна снятая с контракта поверхность.
type RetiredRPC struct {
	// FQN — полное имя метода в форме `<пакет>.<Сервис>/<Метод>`.
	FQN string
	// Reason — почему снято. Часть надгробия: следующий, кто захочет занять имя,
	// обязан прочитать, чем оно было и почему исчезло.
	Reason string
}

// RetiredRPCSurfaceOptions — вход анализатора.
type RetiredRPCSurfaceOptions struct {
	// Root — корень репозитория.
	Root string
	// APIRoot — путь (относительно Root) к сгенерированным стабам.
	APIRoot string
	// ProtoRoot — путь (относительно Root) к дереву исходного контракта.
	ProtoRoot string
	// CatalogPaths — пути (относительно Root) к вшитым копиям каталога прав.
	// Читаются ВСЕ: копия, которую забыли перегенерировать, — ровно тот случай,
	// ради которого гейт и написан.
	CatalogPaths []string
	// Retired — перепись снятого.
	Retired []RetiredRPC
}

// RetiredRPCSurfaceCensus — то, что анализатор прочитал. Ноль находок обязано
// быть отличимо от нуля прочитанного.
type RetiredRPCSurfaceCensus struct {
	RetiredEntries  int
	StubFiles       int
	DeclaredSvcs    int
	DeclaredMethods int
	ProtoFiles      int
	ProtoSvcs       int
	CatalogFiles    int
	CatalogRows     int
}

// RetiredRPCSurfaceFinding — одна находка.
type RetiredRPCSurfaceFinding struct {
	Kind   string // "redeclared-stub" | "redeclared-proto" | "catalog-row"
	FQN    string
	Where  string
	Reason string
}

func (f RetiredRPCSurfaceFinding) String() string {
	return f.Kind + " " + f.FQN + " (" + f.Where + "): " + f.Reason
}

var (
	protoPackageRe = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z0-9_.]+)\s*;`)
	protoServiceRe = regexp.MustCompile(`(?m)^\s*service\s+([A-Za-z0-9_]+)\s*\{`)
	protoRPCRe     = regexp.MustCompile(`(?m)^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`)
)

// AuditRetiredRPCSurface проверяет, что ни одно снятое имя не вернулось — ни в
// исходный контракт, ни в сгенерированные стабы, ни в каталог прав.
func AuditRetiredRPCSurface(opts RetiredRPCSurfaceOptions, out io.Writer) ([]RetiredRPCSurfaceFinding, RetiredRPCSurfaceCensus, error) {
	var c RetiredRPCSurfaceCensus
	c.RetiredEntries = len(opts.Retired)

	// Премиса самого надгробия: перепись пуста ⇒ гейт инертен, и его зелёный
	// вердикт не значит ничего. Пустое надгробие — ошибка, а не «ноль находок».
	if c.RetiredEntries == 0 {
		return nil, c, fmt.Errorf("перепись снятого пуста — гейту нечего охранять, и любой вердикт ниже беспредметен")
	}
	retired := map[string]string{}
	for _, r := range opts.Retired {
		if _, _, ok := splitFQN(r.FQN); !ok {
			return nil, c, fmt.Errorf("запись переписи %q не имеет формы `<сервис>/<метод>`", r.FQN)
		}
		retired[r.FQN] = r.Reason
	}

	var findings []RetiredRPCSurfaceFinding

	// ── 1. Сгенерированные стабы: та же таблица, по которой идёт диспатч ──────
	var rc CatalogReachabilityCensus
	declared, err := declaredMethods(filepath.Join(opts.Root, opts.APIRoot), &rc)
	if err != nil {
		return nil, c, err
	}
	c.StubFiles, c.DeclaredSvcs, c.DeclaredMethods = rc.StubFiles, rc.DeclaredSvcs, rc.DeclaredMethods
	if c.StubFiles == 0 || c.DeclaredMethods == 0 {
		return nil, c, fmt.Errorf("из стабов %q прочитано файлов %d, методов %d — «снятое не вернулось» получено даром",
			opts.APIRoot, c.StubFiles, c.DeclaredMethods)
	}
	for fqn, reason := range retired {
		svc, method, _ := splitFQN(fqn)
		if methods, ok := declared[svc]; ok {
			if _, ok := methods[method]; ok {
				findings = append(findings, RetiredRPCSurfaceFinding{
					Kind: "redeclared-stub", FQN: fqn, Where: opts.APIRoot,
					Reason: "имя снято с контракта, но объявлено в стабах снова. " +
						"Снято было потому, что: " + reason,
				})
			}
		}
	}

	// ── 2. Исходный контракт: имя ловится до регенерации стабов ───────────────
	protoDir := filepath.Join(opts.Root, opts.ProtoRoot)
	err = filepath.WalkDir(protoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		c.ProtoFiles++
		src := string(b)
		pkg := ""
		if m := protoPackageRe.FindStringSubmatch(src); m != nil {
			pkg = m[1]
		}
		if pkg == "" {
			return nil
		}
		// Разбор посервисно: файл может нести несколько сервисов, и метод одного
		// нельзя приписывать другому.
		locs := protoServiceRe.FindAllStringSubmatchIndex(src, -1)
		for i, loc := range locs {
			svcName := src[loc[2]:loc[3]]
			c.ProtoSvcs++
			end := len(src)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			body := src[loc[1]:end]
			for _, m := range protoRPCRe.FindAllStringSubmatch(body, -1) {
				fqn := pkg + "." + svcName + "/" + m[1]
				if reason, ok := retired[fqn]; ok {
					rel, _ := filepath.Rel(opts.Root, path)
					findings = append(findings, RetiredRPCSurfaceFinding{
						Kind: "redeclared-proto", FQN: fqn, Where: rel,
						Reason: "имя снято с контракта, но объявлено в контракте снова. " +
							"Снято было потому, что: " + reason,
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, c, err
	}
	if c.ProtoFiles == 0 || c.ProtoSvcs == 0 {
		return nil, c, fmt.Errorf("в дереве контракта %q прочитано файлов %d, сервисов %d — "+
			"«снятое не вернулось» получено даром", opts.ProtoRoot, c.ProtoFiles, c.ProtoSvcs)
	}

	// ── 3. Каталог прав: ВСЕ вшитые копии ─────────────────────────────────────
	if len(opts.CatalogPaths) == 0 {
		return nil, c, fmt.Errorf("не передано ни одной копии каталога — третье плечо гейта беспредметно")
	}
	for _, cp := range opts.CatalogPaths {
		rows, err := readCatalogRows(filepath.Join(opts.Root, cp))
		if err != nil {
			return nil, c, err
		}
		if len(rows) == 0 {
			return nil, c, fmt.Errorf("копия каталога %q пуста — её плечо беспредметно", cp)
		}
		c.CatalogFiles++
		c.CatalogRows += len(rows)
		for _, row := range rows {
			if reason, ok := retired[row.FQN]; ok {
				findings = append(findings, RetiredRPCSurfaceFinding{
					Kind: "catalog-row", FQN: row.FQN, Where: cp,
					Reason: "каталог объявляет полосу авторизации для снятого имени. " +
						"Снято было потому, что: " + reason,
				})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		if findings[i].FQN != findings[j].FQN {
			return findings[i].FQN < findings[j].FQN
		}
		return findings[i].Where < findings[j].Where
	})

	if out != nil {
		_, _ = fmt.Fprintf(out, "перепись: снятых имён %d; стабов %d файлов (%d сервисов, %d методов); "+
			"контракта %d файлов (%d сервисов); каталога %d копий (%d строк суммарно); находок %d\n",
			c.RetiredEntries, c.StubFiles, c.DeclaredSvcs, c.DeclaredMethods,
			c.ProtoFiles, c.ProtoSvcs, c.CatalogFiles, c.CatalogRows, len(findings))
	}
	return findings, c, nil
}
