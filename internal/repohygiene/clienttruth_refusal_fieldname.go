// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_refusal_fieldname.go — анализатор «имя поля, названное отказом,
// существует в контракте того ресурса, чей use-case его вынес».
//
// # Предмет
//
// Имя поля в отказе — часть контракта: по нему вызывающий понимает, ЧТО ПРАВИТЬ
// в своём теле. Оно едет и в машинную часть ответа —
// `BadRequest.FieldViolations[].field`, — по которой ветвится автомат. Имя,
// которого в контракте нет, отправляет обоих искать ключ, которого они не
// посылали.
//
// Замер на день заведения (kacho#1623): отказы подсети называли
// `v4_cidr_blocks` — имя ДОМЕННОГО поля (`domain.Subnet.V4CidrBlocks`), которого
// нет ни в одном сообщении контракта подсети: `Create` принимает
// `ipv4_cidr_primary`, `:addCidrBlocks` — `ipv4_cidr_blocks`. Клиентская
// документация была вынуждена объяснять несовпадение отдельным абзацем — то есть
// цену признали и переложили на читателя.
//
// # Что судит анализатор
//
// ПРОИЗВОДИТЕЛЬ ИМЕНИ ОДИН И ОН ОДНОЗНАЧЕН: первый аргумент
// `serviceerr.InvalidArg`. По контракту этой функции он и есть значение
// `FieldViolation.Field`, поэтому «поле или не поле» здесь не гадается.
//
// РАЗБОР — AST, А НЕ ОБРАЗЕЦ ПО ТЕКСТУ. Имя функции встречается в комментариях,
// объясняющих её же (в том числе в шапке этого файла), и проверка по подстроке
// краснела бы на собственном объяснении.
//
// СЛОВАРЬ РЕСУРСА ВЫВОДИТСЯ ИЗ ДЕСКРИПТОРОВ, а не из текста контрактов: имена
// полей, их json-форма и ИМЕНА ONEOF берутся у `protoregistry`. Второй рукописной
// таблицы не заводится — она разошлась бы с контрактом молча.
//
// # ONEOF — ЗАКОННОЕ ИМЯ, и это стоило двух ложных находок
//
// Первая редакция распознавателя знала только поля и объявила находками два
// верных отказа: `InvalidArg("gateway", …)` у шлюза и `InvalidArg("disk", …)` у
// машины. Оба называют ИМЯ ВЕТВИ ONEOF (`oneof gateway`, `oneof disk`) — то есть
// конструкцию контракта, про которую вызывающему и говорят («ветвь не выбрана»).
// Распознаватель, не знающий одной из законных форм записи предмета, не даёт ни
// красного, ни зелёного — он обвиняет наугад.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо ЧИСЛОМ, а не умолчанием
//
//  1. НЕ-ЛИТЕРАЛЬНЫЙ первый аргумент (переменная, вызов, конкатенация) не
//     судится: значение известно только в рантайме. Число печатается отдельно —
//     «находок ноль» обязано быть отличимо от «судить было нечего».
//
//  2. ВЛОЖЕННЫЕ СЕГМЕНТЫ пути (`boot_source.id` — часть после точки) не судятся:
//     чтобы спуститься, надо знать, полем какого сообщения является голова, а
//     одно и то же имя встречается в нескольких сообщениях ресурса. Судится
//     ГОЛОВА пути — именно она называет ключ верхнего уровня в теле.
//
//  3. ПРОЧИЕ ПРОИЗВОДИТЕЛИ ОТКАЗА (`status.Error(codes.InvalidArgument, …)` с
//     именем поля внутри прозы) не судятся вовсе: имя там не отделено от текста.
//     Это НЕ значит, что там чисто, — разнобой записи имён в прозе отказов
//     остаётся открытым предметом kacho#1623.
//
//  4. ПОЛНОТА не судится: молчание об имени поля нарушением не является.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных пакетов, ноль сообщений в словаре либо ноль судимых имён —
// «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/servicelayout"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Регистрация дескрипторов доменов: источник словаря имён полей и oneof.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

// RefusalFieldNameOptions — вход анализатора.
type RefusalFieldNameOptions struct {
	// Root — корень дерева.
	Root string
	// ServicesRel — каталог сервисов относительно Root.
	ServicesRel string
	// UseCaseRelOf — путь от каталога службы до каталога use-case-пакетов.
	//
	// ФУНКЦИЯ, а не строка: сегмент каталога выведен из имени ПРОДУКТА службы,
	// и одна строка на всех молча обходила бы не тот каталог у той службы,
	// которая назвалась иначе. Промах здесь не краснеет — он даёт «ноль
	// находок» там, где не прочитано ничего.
	UseCaseRelOf func(service string) string
	// ProtoPackageOf — пакет контрактов по имени каталога сервиса. Имя каталога и
	// имя домена совпадают не всегда (`nlb` → `loadbalancer`), поэтому таблица
	// объявляется вызывающим, а не угадывается из строки.
	ProtoPackageOf map[string]string
}

// DefaultRefusalFieldNameOptions — раскладка этого дерева.
func DefaultRefusalFieldNameOptions(root string) RefusalFieldNameOptions {
	return RefusalFieldNameOptions{
		Root:        root,
		ServicesRel: "services",
		UseCaseRelOf: func(service string) string {
			return path.Join("internal", "apps", servicelayout.UseCaseSegment(service), "api")
		},
		ProtoPackageOf: map[string]string{
			"compute":  "kacho.cloud.compute.v1",
			"geo":      "kacho.cloud.geo.v1",
			"iam":      "kaname.cloud.iam.v1",
			"nlb":      "kacho.cloud.loadbalancer.v1",
			"registry": "kacho.cloud.registry.v1",
			"storage":  "kacho.cloud.storage.v1",
			"vpc":      "kacho.cloud.vpc.v1",
		},
	}
}

// RefusalFieldNameCensus — объём осмотренного.
type RefusalFieldNameCensus struct {
	// Packages — use-case-пакетов ресурса обойдено.
	Packages int
	// PackagesWithVocabulary — из них те, чьему имени нашлось хотя бы одно
	// сообщение контракта. Остальные пропущены: судить не по чему.
	PackagesWithVocabulary int
	// Files — не-тестовых файлов Go разобрано.
	Files int
	// Calls — вызовов производителя имени найдено.
	Calls int
	// Judged — из них с ЛИТЕРАЛЬНЫМ именем: столько и рассужено.
	Judged int
	// NotLiteral — имя приходит значением: не судится (см. шапку, п.1).
	NotLiteral int
	// VocabularyNames — уникальных имён (поля + oneof, обе записи) в словарях.
	VocabularyNames int
}

// RefusalFieldNameFinding — одно имя, которого в контракте ресурса нет.
type RefusalFieldNameFinding struct {
	File     string
	Line     int
	Resource string
	Name     string
	Head     string
}

func (f RefusalFieldNameFinding) String() string {
	return fmt.Sprintf("%s:%d: отказ ресурса %s называет поле %q — имени %q нет "+
		"ни в одном сообщении его контракта: вызывающий будет искать в своём теле ключ, "+
		"которого туда не клал", f.File, f.Line, f.Resource, f.Name, f.Head)
}

// vocabularyFor собирает имена полей и oneof всех сообщений пакета контрактов,
// чьё имя содержит имя ресурса.
//
// Сопоставление — по строчной подстроке: каталог use-case называется `cidrgroup`,
// а сообщения — `AddCidrGroupCidrBlocksRequest`, и подчёркиваний в каталоге нет.
// Приводя обе стороны к нижнему регистру, сопоставление не зависит от того, как
// разделены слова.
func vocabularyFor(protoPkg, resource string) map[string]struct{} {
	out := map[string]struct{}{}
	prefix := protoreflect.FullName(protoPkg)
	needle := strings.ToLower(resource)
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != prefix {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			if !strings.Contains(strings.ToLower(string(md.Name())), needle) {
				continue
			}
			fields := md.Fields()
			for j := 0; j < fields.Len(); j++ {
				out[string(fields.Get(j).Name())] = struct{}{}
				out[fields.Get(j).JSONName()] = struct{}{}
			}
			oneofs := md.Oneofs()
			for j := 0; j < oneofs.Len(); j++ {
				out[string(oneofs.Get(j).Name())] = struct{}{}
			}
		}
		return true
	})
	return out
}

// headOf — голова пути имени: `boot_source.id` → `boot_source`,
// `v4_cidr_blocks[0]` → `v4_cidr_blocks`.
func headOf(name string) string {
	head := name
	if i := strings.IndexByte(head, '.'); i >= 0 {
		head = head[:i]
	}
	if i := strings.IndexByte(head, '['); i >= 0 {
		head = head[:i]
	}
	return head
}

// AuditRefusalFieldNames — вердикт по дереву.
func AuditRefusalFieldNames(opts RefusalFieldNameOptions, log io.Writer) ([]RefusalFieldNameFinding, RefusalFieldNameCensus, error) {
	var (
		findings []RefusalFieldNameFinding
		census   RefusalFieldNameCensus
	)
	servicesAbs := filepath.Join(opts.Root, filepath.FromSlash(opts.ServicesRel))
	svcEntries, err := os.ReadDir(servicesAbs)
	if err != nil {
		return nil, census, fmt.Errorf("каталог сервисов не читается: %w", err)
	}
	vocabSeen := map[string]struct{}{}

	for _, svc := range svcEntries {
		if !svc.IsDir() {
			continue
		}
		protoPkg, ok := opts.ProtoPackageOf[svc.Name()]
		if !ok {
			continue
		}
		apiAbs := filepath.Join(servicesAbs, svc.Name(), filepath.FromSlash(opts.UseCaseRelOf(svc.Name())))
		resEntries, rerr := os.ReadDir(apiAbs)
		if rerr != nil {
			continue // у сервиса нет каталога use-case'ов — не находка
		}
		for _, res := range resEntries {
			if !res.IsDir() {
				continue
			}
			census.Packages++
			vocab := vocabularyFor(protoPkg, res.Name())
			if len(vocab) == 0 {
				continue
			}
			census.PackagesWithVocabulary++
			for name := range vocab {
				vocabSeen[protoPkg+"."+name] = struct{}{}
			}
			pkgAbs := filepath.Join(apiAbs, res.Name())
			files, ferr := os.ReadDir(pkgAbs)
			if ferr != nil {
				return nil, census, fmt.Errorf("пакет %s не читается: %w", pkgAbs, ferr)
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
					continue
				}
				path := filepath.Join(pkgAbs, f.Name())
				fset := token.NewFileSet()
				parsed, perr := parser.ParseFile(fset, path, nil, 0)
				if perr != nil {
					return nil, census, fmt.Errorf("файл %s не разобран: %w", path, perr)
				}
				census.Files++
				rel, _ := filepath.Rel(opts.Root, path)
				ast.Inspect(parsed, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || len(call.Args) == 0 {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "InvalidArg" {
						return true
					}
					pkgIdent, ok := sel.X.(*ast.Ident)
					if !ok || pkgIdent.Name != "serviceerr" {
						return true
					}
					census.Calls++
					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						census.NotLiteral++
						return true
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						census.NotLiteral++
						return true
					}
					census.Judged++
					head := headOf(value)
					if _, known := vocab[head]; known {
						return true
					}
					findings = append(findings, RefusalFieldNameFinding{
						File: filepath.ToSlash(rel), Line: fset.Position(lit.Pos()).Line,
						Resource: res.Name(), Name: value, Head: head,
					})
					return true
				})
			}
		}
	}
	census.VocabularyNames = len(vocabSeen)

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: пакетов ресурса %d · со словарём %d · файлов %d · "+
			"вызовов производителя %d · рассужено имён %d · не судимо (имя не литерал) %d · "+
			"имён в словарях %d · находок %d\n",
			census.Packages, census.PackagesWithVocabulary, census.Files, census.Calls,
			census.Judged, census.NotLiteral, census.VocabularyNames, len(findings))
	}
	return findings, census, nil
}
