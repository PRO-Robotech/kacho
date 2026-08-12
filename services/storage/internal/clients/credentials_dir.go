// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirCredentials разрешает ссылку на учётный материал в файл смонтированного каталога.
//
// # Почему ссылка, а не значение
//
// Секрет, положенный в колонку, переживает ротацию и уезжает в резервные копии.
// Ресурс несёт ССЫЛКУ, процесс — способ её разрешить, и материал не проходит ни
// через API, ни через БД.
//
// # Почему имя файла НЕ склеивается с путём напрямую
//
// Ссылка приходит из строки БД, то есть её значение мог выбрать администратор.
// Склейка каталога с произвольной строкой даёт выход за его пределы одним «..» —
// и процесс прочитает файл, который ему не адресован. Поэтому из ссылки берётся
// только ПОСЛЕДНИЙ сегмент, а результат сверяется с каталогом ещё раз.
type DirCredentials struct {
	dir string
}

// NewDirCredentials собирает резолвер поверх смонтированного каталога.
func NewDirCredentials(dir string) *DirCredentials { return &DirCredentials{dir: dir} }

// Resolve читает материал по ссылке.
func (d *DirCredentials) Resolve(_ context.Context, ref string) ([]byte, error) {
	if d.dir == "" {
		return nil, fmt.Errorf("credentials directory is not configured")
	}
	name := ref
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" || name == "." || name == ".." {
		return nil, fmt.Errorf("credentials reference %q names no file", ref)
	}
	path := filepath.Join(d.dir, name)
	// Вторая сверка: результат склейки обязан остаться внутри каталога. Первая
	// (взятие последнего сегмента) уже это обеспечивает, но проверка стоит здесь
	// потому, что она защищает от БУДУЩЕЙ правки первой, а не от нынешней.
	if !strings.HasPrefix(path, filepath.Clean(d.dir)+string(filepath.Separator)) {
		return nil, fmt.Errorf("credentials reference %q escapes the credentials directory", ref)
	}
	material, err := os.ReadFile(path) //nolint:gosec // путь сведён к файлу внутри смонтированного каталога выше
	if err != nil {
		return nil, fmt.Errorf("credentials reference %q is not resolvable: %w", ref, err)
	}
	if len(material) == 0 {
		return nil, fmt.Errorf("credentials reference %q resolves to an empty file", ref)
	}
	return material, nil
}
