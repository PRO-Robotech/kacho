// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// rootedWalk обходит каталог dir и отдаёт visit содержимое файлов, отобранных
// want. Имя, разрешение которого выходит за пределы dir, не читается — обход
// ОТКАЗЫВАЕТ.
//
// # Почему не filepath.Walk + os.ReadFile
//
// Пара «обход отдал путь — колбэк открыл его по имени» разносит во времени
// снятие типа записи и обращение к ней: между этими двумя моментами запись
// может перестать быть тем, чем была. Открытие в пределах корня (`os.Root`)
// снимает это одним механизмом — разрешение имени не выходит за корень, — и
// заодно закрывает вторую половину того же: симлинк, ведущий наружу, больше не
// втягивает в вердикт гейта посторонний файл.
//
// # Почему отказ, а не пропуск
//
// Гейты этого пакета печатают перепись осмотренного, чтобы «ноль находок» было
// отличимо от «ноль прочитанного». Запись, которую обход не может честно
// прочесть, нельзя ни засчитать в перепись, ни молча пропустить: и то и другое
// вернуло бы ровно ту неразличимость. Поэтому — ошибка с координатой.
//
// Отслеживаемых симлинков в репозитории ноль (`git ls-files -s`, режим 120000),
// поэтому отказ не срабатывает на законном содержимом дерева.
func rootedWalk(dir string, want func(rel string) bool, visit func(abs string, body []byte) error) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("обход %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	return fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !want(rel) {
			return nil
		}
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		body, err := root.ReadFile(rel)
		if err != nil {
			return fmt.Errorf("%s: %w — запись не читается В ПРЕДЕЛАХ осматриваемого "+
				"каталога %s. Прочитать её, выйдя наружу, гейт не станет: вердикт стал бы "+
				"свойством постороннего файла, а не этого дерева", abs, err, dir)
		}
		return visit(abs, body)
	})
}
