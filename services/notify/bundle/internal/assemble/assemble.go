// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package assemble — шаг сборки шаблонов notify (NTF-1 Р7, NTF1-G17): по
// дереву исходников он выводит ПОЛНЫЙ набор файлов сборки, пишет его
// (`make -C services/notify bundle`) либо сверяет с записанным
// (`make -C services/notify bundle-check`).
//
// Что здесь решено и почему:
//
//   - Шаблоны попадают в notify только сборкой: регистрации в рантайме нет
//     (Р7). Источник шаблона — строка закрытой таблицы [Sources]:
//     пространство ленты (модуль источника) и каталог шаблонов в дереве.
//     Каталог шаблонов службы (`services/<служба>/notifications`) вне таблицы —
//     отказ шага с его путём: шаблон, не попавший в сборку молча, был бы
//     письмом, которое notify откладывает как `template_skew` до истечения.
//   - Каталог источника проверяется единственным валидатором формата
//     (`notify/spec`, NTF1-A09) — тем же, что notify зовёт на старте над
//     встроенной копией. Шаблон без ревизии в сборку не входит: ревизию
//     сборки строка ленты сравнивает с `schema_rev` (клетка 1 З22).
//   - Файлы шаблона копируются побайтово: валидатор на старте notify читает
//     ровно то, что проверил шаг. Имена файлов формата здесь не упоминаются —
//     копируется каждый обычный файл каталога шаблона, а посторонний файл
//     валидатор уже отверг.
//   - Тем же шагом порождаются эталон `.eml` и превью (HTML-часть письма)
//     каждой локали каждого шаблона (З25, NTF1-G17 And) — рендером notify
//     (internal/render) с постоянными значениями атрибутов и установки:
//     правка текста шаблона видна в диффе письмом, которое уйдёт адресату.
//   - Вывод детерминирован: при том же дереве — те же байты; сверка судит
//     каждый файл трёх каталогов вывода в обе стороны (нет, лишний,
//     отличается) и называет пространство и шаблон.
package assemble

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

// Source — строка таблицы источников сборки.
type Source struct {
	// Namespace — пространство ленты: модуль записи перечня источников notify
	// (таблица модулей чарта notify, deploy/helm/notify/templates/_sources.tpl).
	Namespace string
	// Dir — каталог шаблонов источника от корня репозитория.
	Dir string
}

// Sources — закрытая таблица источников сборки. Источник появляется в сборке
// строкой этой таблицы и больше ничем.
func Sources() []Source {
	return []Source{
		{Namespace: "notify-probe", Dir: "services/notify/notifications"},
	}
}

// Каталоги вывода от каталога сборки (services/notify/bundle). Каждый
// принадлежит шагу целиком: файл в нём, не выведенный из дерева, — лишний.
const (
	// CatalogDir — копия каталогов шаблонов, встраиваемая в notify:
	// CatalogDir/<пространство>/<шаблон>/<файл>.
	CatalogDir = "catalog"
	// GoldenDir — эталоны письма: GoldenDir/<пространство>/<шаблон>.<локаль>.eml.
	GoldenDir = "golden"
	// PreviewDir — превью: HTML-часть эталона,
	// PreviewDir/<пространство>/<шаблон>.<локаль>.html.
	PreviewDir = "preview"
)

// outputDirs — каталоги вывода в порядке сверки.
var outputDirs = []string{CatalogDir, GoldenDir, PreviewDir}

// serviceTemplatesGlob — где в дереве лежат каталоги шаблонов служб; каждый
// найденный обязан быть строкой [Sources].
const serviceTemplatesGlob = "services/*/notifications"

// Постоянные значения эталона: установка, адресат, строка ленты, момент.
// Домен example.invalid не разрешается никогда (RFC 2606).
const (
	previewOrigin      = "https://console.example.invalid"
	previewFromName    = "Kacho"
	previewFromAddress = "noreply@example.invalid"
	previewTo          = "recipient@example.invalid"
	previewRowID       = "bundle-preview"
)

// previewDate — момент письма эталона (заголовок Date).
var previewDate = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// File — файл сборки: путь от каталога сборки, содержимое и шаблон-владелец.
type File struct {
	Path  string
	Data  []byte
	Owner string // <пространство>/<шаблон>
}

// Output — полный вывод шага и перепись прочитанного.
type Output struct {
	Files     []File // упорядочены по Path
	Sources   int
	Templates int
}

// Assemble выводит сборку из дерева с корнем root по таблице sources.
// Ошибка — сборки нет: каталог службы вне таблицы, источник без шаблонов,
// находки валидатора, шаблон без ревизии, отказ рендера.
func Assemble(root string, sources []Source) (Output, error) {
	if len(sources) == 0 {
		return Output{}, errors.New("bundle: таблица источников пуста — собирать нечего")
	}
	if err := checkCoverage(root, sources); err != nil {
		return Output{}, err
	}
	r, err := previewRenderer()
	if err != nil {
		return Output{}, err
	}
	var out Output
	seen := map[string]bool{}
	for _, src := range sources {
		if seen[src.Namespace] {
			return Output{}, fmt.Errorf("bundle: пространство %s названо в таблице источников дважды", src.Namespace)
		}
		seen[src.Namespace] = true
		files, n, err := assembleSource(root, src, r)
		if err != nil {
			return Output{}, err
		}
		out.Files = append(out.Files, files...)
		out.Templates += n
		out.Sources++
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out, nil
}

// checkCoverage — каждый каталог шаблонов службы в дереве назван таблицей.
func checkCoverage(root string, sources []Source) error {
	listed := map[string]bool{}
	for _, s := range sources {
		listed[path.Clean(s.Dir)] = true
	}
	found, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(serviceTemplatesGlob)))
	if err != nil {
		return fmt.Errorf("bundle: обход %s: %w", serviceTemplatesGlob, err)
	}
	var unlisted []string
	for _, p := range found {
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("bundle: %w", err)
		}
		if !info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("bundle: %w", err)
		}
		if rel = filepath.ToSlash(rel); !listed[rel] {
			unlisted = append(unlisted, rel)
		}
	}
	if len(unlisted) > 0 {
		sort.Strings(unlisted)
		return fmt.Errorf("bundle: каталоги шаблонов вне таблицы источников сборки (services/notify/bundle/internal/assemble): %s",
			strings.Join(unlisted, ", "))
	}
	return nil
}

// assembleSource — файлы сборки одного источника и число его шаблонов.
func assembleSource(root string, src Source, r *render.Renderer) ([]File, int, error) {
	dir := filepath.Join(root, filepath.FromSlash(src.Dir))
	cat, census, err := spec.Load(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("bundle: источник %s (%s): %w", src.Namespace, src.Dir, err)
	}
	if census.Templates == 0 || len(cat.Templates) == 0 {
		return nil, 0, fmt.Errorf("bundle: источник %s (%s): шаблонов 0 — строка таблицы без предмета", src.Namespace, src.Dir)
	}
	var files []File
	for _, tpl := range cat.Templates {
		owner := src.Namespace + "/" + tpl.Name
		if !tpl.HasRevision {
			return nil, 0, fmt.Errorf("bundle: %s: у шаблона нет ревизии — выполните make notifications", owner)
		}
		copied, err := copyTemplate(dir, src.Namespace, tpl.Name, owner)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, copied...)
		letters, err := renderTemplate(r, src.Namespace, tpl, owner)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, letters...)
	}
	return files, len(cat.Templates), nil
}

// copyTemplate — каждый обычный файл каталога шаблона в CatalogDir.
func copyTemplate(dir, namespace, name, owner string) ([]File, error) {
	tplFS := os.DirFS(filepath.Join(dir, name))
	var files []File
	err := fs.WalkDir(tplFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := fs.ReadFile(tplFS, p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: path.Join(CatalogDir, namespace, name, p), Data: data, Owner: owner})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: %s: копия каталога шаблона: %w", owner, err)
	}
	return files, nil
}

// renderTemplate — эталон и превью каждой локали шаблона.
func renderTemplate(r *render.Renderer, namespace string, tpl spec.Template, owner string) ([]File, error) {
	attrs, err := previewAttrs(tpl)
	if err != nil {
		return nil, fmt.Errorf("bundle: %s: %w", owner, err)
	}
	to, err := address.Normalize(previewTo)
	if err != nil {
		return nil, fmt.Errorf("bundle: адресат эталона: %w", err)
	}
	locales := make([]string, 0, len(tpl.Subject))
	for loc := range tpl.Subject {
		locales = append(locales, loc)
	}
	sort.Strings(locales)
	var files []File
	for _, loc := range locales {
		eml, err := r.Render(render.Letter{
			Namespace: namespace,
			RowID:     previewRowID,
			Template:  tpl,
			Locale:    loc,
			Attrs:     attrs,
			To:        to,
			Date:      previewDate,
		})
		if err != nil {
			return nil, fmt.Errorf("bundle: %s: рендер локали %s: %w", owner, loc, err)
		}
		letter, err := emlprobe.Parse(eml)
		if err != nil {
			return nil, fmt.Errorf("bundle: %s: эталон локали %s не разбирается: %w", owner, loc, err)
		}
		base := tpl.Name + "." + loc
		files = append(files,
			File{Path: path.Join(GoldenDir, namespace, base+".eml"), Data: eml, Owner: owner},
			File{Path: path.Join(PreviewDir, namespace, base+".html"), Data: []byte(letter.HTML), Owner: owner},
		)
	}
	return files, nil
}

// previewRenderer — рендер установки эталона.
func previewRenderer() (*render.Renderer, error) {
	name, err := form.ParseHeaderText(previewFromName)
	if err != nil {
		return nil, fmt.Errorf("bundle: имя отправителя эталона: %w", err)
	}
	addr, err := address.Normalize(previewFromAddress)
	if err != nil {
		return nil, fmt.Errorf("bundle: адрес отправителя эталона: %w", err)
	}
	r, err := render.New(render.Config{Origin: previewOrigin, From: render.Sender{Name: name, Address: addr}})
	if err != nil {
		return nil, fmt.Errorf("bundle: рендер эталона: %w", err)
	}
	return r, nil
}

// previewAttrs — значение КАЖДОГО объявленного атрибута (optional тоже:
// блоки с when в эталоне видны), построенное единственной функцией формы.
func previewAttrs(tpl spec.Template) (map[string]form.Value, error) {
	out := make(map[string]form.Value, len(tpl.Attrs))
	for _, a := range tpl.Attrs {
		raw, err := previewRaw(a)
		if err != nil {
			return nil, err
		}
		v, err := form.Require(a.Kind, raw)
		if err != nil {
			return nil, fmt.Errorf("значение эталона атрибута %s: %w", a.Name, err)
		}
		out[a.Name] = v
	}
	return out, nil
}

// previewRaw — значение эталона по виду атрибута. Имя атрибута формы
// [a-z][a-z0-9_]* — допустимый сегмент пути и знаки токена.
func previewRaw(a spec.Attr) (any, error) {
	switch a.Kind {
	case form.KindText, form.KindSecret:
		return "[" + a.Name + "]", nil
	case form.KindPath:
		return "/" + a.Name, nil
	case form.KindToken:
		return "preview_" + a.Name + "_0123456789abcdef", nil
	case form.KindTimestamp:
		return previewDate, nil
	}
	return nil, fmt.Errorf("атрибут %s: вид %q вне перечня эталона", a.Name, a.Kind)
}

// Write заменяет каталоги вывода в dir выводом out: каждый каталог
// удаляется и пишется заново — файл снятого шаблона уходит вместе с ним.
func Write(dir string, out Output) error {
	for _, d := range outputDirs {
		if err := os.RemoveAll(filepath.Join(dir, d)); err != nil {
			return fmt.Errorf("bundle: %w", err)
		}
	}
	for _, f := range out.Files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return fmt.Errorf("bundle: %w", err)
		}
		if err := os.WriteFile(p, f.Data, 0o600); err != nil {
			return fmt.Errorf("bundle: %w", err)
		}
	}
	return nil
}

// Finding — расхождение записанной сборки с деревом.
type Finding struct {
	Owner  string // <пространство>/<шаблон>
	Path   string // от каталога сборки
	Reason string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s — %s", f.Owner, f.Path, f.Reason)
}

// Check сверяет каталоги вывода в dir с out в обе стороны и возвращает
// находки по пути и число осмотренных записанных файлов.
func Check(dir string, out Output) ([]Finding, int, error) {
	want := make(map[string]File, len(out.Files))
	for _, f := range out.Files {
		want[f.Path] = f
	}
	var findings []Finding
	inspected := 0
	// Обход и чтение — внутри файловой системы каталога сборки: путь из
	// обхода не выходит за её корень.
	buildFS := os.DirFS(dir)
	for _, d := range outputDirs {
		if _, err := fs.Stat(buildFS, d); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := fs.WalkDir(buildFS, d, func(rel string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				return nil
			}
			inspected++
			w, ok := want[rel]
			if !ok {
				findings = append(findings, Finding{Owner: ownerOf(rel), Path: rel, Reason: "в дереве такого шаблона нет (лишний файл сборки)"})
				return nil
			}
			delete(want, rel)
			got, err := fs.ReadFile(buildFS, rel)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, w.Data) {
				findings = append(findings, Finding{Owner: w.Owner, Path: rel, Reason: "отличается от пересборки из дерева"})
			}
			return nil
		})
		if err != nil {
			return nil, inspected, fmt.Errorf("bundle: обход %s: %w", filepath.Join(dir, d), err)
		}
	}
	for _, w := range want {
		findings = append(findings, Finding{Owner: w.Owner, Path: w.Path, Reason: "нет в сборке"})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Path < findings[j].Path })
	return findings, inspected, nil
}

// ownerOf — пространство и шаблон по пути лишнего файла каталога вывода.
func ownerOf(rel string) string {
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) >= 4 && parts[0] == CatalogDir:
		return parts[1] + "/" + parts[2]
	case len(parts) == 3:
		return parts[1] + "/" + strings.SplitN(parts[2], ".", 2)[0]
	}
	return "(вне раскладки)"
}
