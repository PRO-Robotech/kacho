// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// removalErr — отказ снятия без снимка имени: пробам этого пакета, снимающим
// строку ради фикстуры либо отказа, имя снятой строки не нужно (его держат
// пробы журнала vpc, NTF-3 NTF3-59).
func removalErr(_ string, err error) error { return err }
