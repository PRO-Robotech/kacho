// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

// Владелец, который СПИСЫВАЕТ, обязан уметь ОТКАЗАТЬ вслух.
//
// ПРЕДМЕТ. Отказ учёта производит один шаблон на всех владельцев
// (`pkg/quota/refusal.sql.tmpl`) и поднимает его своими SQLSTATE'ами `KQ001`
// (место кончилось) и `KQ002` (предел не назван). Мост SQLSTATE→sentinel у
// каждого владельца свой — иначе и быть не может, у каждого своя база и свой
// пакет ошибок. Владелец, чей мост этих кодов не знает, отправляет отказ
// арендатора в ветвь «неопознанный SQLSTATE», и наружу уходит `INTERNAL
// "internal error"`: вызывающий видит поломку платформы там, где платформа
// сработала как задумано, и не узнаёт ни носителя, ни предела, ни вида.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ПАМЯТЬ. В день заведения списывали шесть владельцев, отказ
// отображали пять. Шестой — `iam` — обзавёлся списанием последним, и пропуск не
// виден ни сборке, ни обзору изменения: код собирается, обе половины по
// отдельности защитимы, а неисполнимость появляется только на стыке и только
// при вызове.
//
// ПОЧЕМУ ТРЕБУЕТСЯ ПАРА (код + признак), А НЕ ОДИН КОД. Клиент различает полосы
// МАШИННО — по `reason`-токену `google.rpc.ErrorInfo` (`api-conventions.md`
// §By-lane code-split). Код без признака не отличает «поднять предел» от
// «завести предел»: оба ответа про квоту, а действия администратора у них
// разные.
//
// ЧТО ГЕЙТ НЕ ПРОВЕРЯЕТ И НЕ ПРИТВОРЯЕТСЯ, ЧТО ПРОВЕРЯЕТ: он не утверждает, что
// отказ ДОЕЗЖАЕТ до края на каждом пути. Это свойство вызова, и его держат
// пробы владельца. Гейт держит более слабое, но проверяемое по дереву: у
// владельца ЕСТЬ производитель обоих исходов, и он приклеивает признак.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// quotaRefusalFacts — что найдено в прод-коде одного владельца.
type quotaRefusalFacts struct {
	// sqlstate — файл, где опознаётся SQLSTATE отказа (оба кода).
	sqlstate string
	// exceeded — файл, где признак «место кончилось» стоит рядом с
	// `codes.ResourceExhausted`.
	exceeded string
	// notProvisioned — файл, где признак «предел не назван» стоит рядом с
	// `codes.FailedPrecondition`.
	notProvisioned string
}

// scanQuotaRefusalMapping разбирает Go-файлы и отвечает, что нашёл у каждого
// владельца. Разбор идёт по AST, а не поиском подстроки, и это несущее решение:
// имена `KQ001` и `QUOTA_EXCEEDED` стоят в комментариях этого дерева десятками —
// в том числе в шапке ЭТОГО файла, — и предикат по тексту зеленел бы на прозе о
// самом себе.
//
// serviceOf отвечает, какому владельцу принадлежит файл; пустая строка означает
// «файл не принадлежит ни одному владельцу учёта» и он пропускается.
func scanQuotaRefusalMapping(t testing.TB, files []string, serviceOf func(string) string) (map[string]*quotaRefusalFacts, int) {
	t.Helper()

	facts := map[string]*quotaRefusalFacts{}
	parsed := 0

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		svc := serviceOf(path)
		if svc == "" {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0) // без комментариев — намеренно
		if err != nil {
			// Файл, который не разбирается, — не «ноль находок»: о нём надо
			// сказать, иначе перепись объявит осмотренным то, чего не читала.
			t.Fatalf("разбор %s: %v", path, err)
		}
		parsed++

		lits := map[string]bool{}
		sels := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					if s, uerr := strconv.Unquote(v.Value); uerr == nil {
						lits[s] = true
					}
				}
			case *ast.SelectorExpr:
				if ident, ok := v.X.(*ast.Ident); ok {
					sels[ident.Name+"."+v.Sel.Name] = true
				}
			}
			return true
		})

		f := facts[svc]
		if f == nil {
			f = &quotaRefusalFacts{}
			facts[svc] = f
		}
		rel := relFromServices(path)
		if lits["KQ001"] && lits["KQ002"] && f.sqlstate == "" {
			f.sqlstate = rel
		}
		if lits["QUOTA_EXCEEDED"] && sels["codes.ResourceExhausted"] && f.exceeded == "" {
			f.exceeded = rel
		}
		if lits["QUOTA_NOT_PROVISIONED"] && sels["codes.FailedPrecondition"] && f.notProvisioned == "" {
			f.notProvisioned = rel
		}
	}
	return facts, parsed
}

// relFromServices укорачивает путь до `services/…`, чтобы координата в тексте
// падения была той же, какой её наберёт читатель.
func relFromServices(path string) string {
	if i := strings.Index(filepath.ToSlash(path), "/services/"); i >= 0 {
		return filepath.ToSlash(path)[i+1:]
	}
	return filepath.ToSlash(path)
}

// quotaRefusalFindings — суждение о собранных фактах, отдельно от их сбора.
// Разделение нужно инъекции: она подаёт сюда факты синтетического дерева и
// проверяет, что судья краснеет по существу и называет владельца.
func quotaRefusalFindings(services []string, facts map[string]*quotaRefusalFacts) []string {
	var findings []string
	for _, svc := range services {
		f := facts[svc]
		if f == nil {
			f = &quotaRefusalFacts{}
		}
		if f.sqlstate == "" {
			findings = append(findings, svc+
				" — списывает квоту, но его мост не знает SQLSTATE отказа (KQ001/KQ002): "+
				"отказ арендатора уйдёт наружу неопознанным, то есть INTERNAL")
		}
		if f.exceeded == "" {
			findings = append(findings, svc+
				" — нет производителя исхода «место кончилось»: признака QUOTA_EXCEEDED "+
				"рядом с codes.ResourceExhausted в прод-коде нет")
		}
		if f.notProvisioned == "" {
			findings = append(findings, svc+
				" — нет производителя исхода «предел не назван»: признака "+
				"QUOTA_NOT_PROVISIONED рядом с codes.FailedPrecondition в прод-коде нет")
		}
	}
	sort.Strings(findings)
	return findings
}

func TestEveryQuotaChargingOwnerMapsTheRefusal(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	// Перечень владельцев берётся у ЕДИНСТВЕННОГО источника отказа — того же,
	// из которого рендерится их миграция. Выписать его здесь значило бы завести
	// второе место об одном предмете: владелец, добавленный там, молча не
	// проверялся бы тут.
	owners := quota.RefusalOwners()
	require.NotEmpty(t, owners,
		"перечень владельцев учёта пуст — гейту нечего рассматривать, и его молчание "+
			"было бы неотличимо от согласия")

	ownerSet := map[string]bool{}
	for _, o := range owners {
		ownerSet[o.Service] = true
	}

	goFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	require.NoError(t, err, "перечень исходников берётся у индекса дерева, а не обходом диска")

	serviceOf := func(path string) string {
		p := filepath.ToSlash(path)
		i := strings.Index(p, "/services/")
		if i < 0 {
			return ""
		}
		seg := strings.Split(p[i+len("/services/"):], "/")
		if len(seg) == 0 || !ownerSet[seg[0]] {
			return ""
		}
		return seg[0]
	}

	facts, parsed := scanQuotaRefusalMapping(t, goFiles, serviceOf)

	// Объём осмотренного — отдельное утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("осмотрено: владельцев учёта %d, файлов Go разобрано %d", len(owners), parsed)
	require.NotZero(t, parsed,
		"гейт не разобрал НИ ОДНОГО файла: предикат перестал находить предмет, "+
			"и «ноль находок» здесь означало бы «ноль прочитанного»")

	services := make([]string, 0, len(owners))
	for _, o := range owners {
		services = append(services, o.Service)
	}
	findings := quotaRefusalFindings(services, facts)

	require.Emptyf(t, findings,
		"владелец учёта обязан производить отказ учёта наружу — кодом И признаком полосы.\n%s",
		strings.Join(findings, "\n"))
}
