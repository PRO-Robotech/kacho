// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Команда quota-refusal-migration рендерит миграцию отказа учёта каждому
// владельцу из единого шаблона.
//
// Запуск из корня дерева:
//
//	go run ./tools/quota-refusal-migration
//
// Идемпотентна: повторный запуск на неизменённом шаблоне не меняет ни одного
// файла и говорит об этом. Печатает, что именно записано, — «ноль записанных»
// обязано быть отличимо от «не запускали».
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/PRO-Robotech/kacho/pkg/quota"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "quota-refusal-migration:", err)
		os.Exit(1)
	}
}

func run() error {
	owners := quota.RefusalOwners()
	if len(owners) == 0 {
		// Ноль владельцев — отказ, а не успех: генератор, которому нечего
		// генерировать, отчитался бы нулём и был бы неотличим от исправного.
		return fmt.Errorf("перечень владельцев пуст — генерировать нечего")
	}

	written, unchanged, consolidated := 0, 0, 0
	for _, o := range owners {
		// Владельцу со сведённой цепью файл НЕ рендерится, и это не пропуск.
		// Отказ стоит в его первичной миграции; рендер завёл бы отдельный файл с
		// версией НИЖЕ уже применённой первичной — то есть версию, которую
		// мигратор не применит вовсе, а гейт номеров назовёт находкой. Тело
		// функции у такого владельца сверяется наравне с остальными, просто по
		// другому референту (`quota.RefusalFunctionBodies`).
		if !o.RendersOwnFile() {
			consolidated++
			continue
		}
		body, err := quota.RenderRefusalMigration(o)
		if err != nil {
			return err
		}
		path := filepath.Join("services", o.Service, "internal", "migrations", o.Migration)

		// #nosec G304 -- путь собран из перечня владельцев в этом же модуле
		// (`quota.RefusalOwners`), а не из ввода: снаружи сюда не приходит ничего.
		if prev, rerr := os.ReadFile(path); rerr == nil && string(prev) == body {
			unchanged++
			continue
		}
		// 0600, а не 0644: право чтения для всех тут ничего не даёт — git хранит
		// из режима только бит исполнения, и файл всё равно приедет к каждому по
		// его umask. Послабления к правам, за которое нечем заплатить, быть не
		// должно (gosec G306); исключений на этот счёт в дереве нет ни одного.
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return fmt.Errorf("запись %s: %w", path, err)
		}
		fmt.Println("записано:", path)
		written++
	}

	// Перепись называет и тех, кому не рендерится: «ноль записанных» обязано быть
	// отличимо от «ноль рассмотренных», а «пропущен» — от «нечего было писать».
	fmt.Printf("владельцев %d; записано %d; без изменений %d; со сведённой цепью (файл не рендерится) %d\n",
		len(owners), written, unchanged, consolidated)
	return nil
}
