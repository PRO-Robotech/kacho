// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_audience_validation.go — startup-validation АДРЕСАТА принимаемых
// токенов (задача #2567).
//
// Подпись доказывает, КТО чеканил удостоверение. Она не говорит, КОМУ оно
// адресовано, — это говорит только `aud`, и только он отличает токен, выпущенный
// для ЭТОЙ установки, от токена, выпущенного для соседней тем же издателем.
//
// Пока адресат выводился построением («https://» + домен API, у которого есть
// умолчание), величина не бывала пустой НИ НА ОДНОЙ посадке: ручки у неё не
// было, стража не было, а сужение применялось лишь при непустом ожидаемом
// значении. То есть пустое означало «не сужаем» — состояние, которое
// `security.md` §«Пустой список — это „не сужаем", а НЕ „запрещаем"» запрещает
// прямо, и заметить его было нечем: проверка провязана, исполняется на каждом
// запросе и не отвергла по этой оси ни разу.
//
// Поэтому незаявленный адресат — не нейтральное умолчание, а выключенный
// контроль, и этот страж отказывает в старте боевому классу окружения в таком
// состоянии. Полосность взята у соседних стражей (validateProductionAuthzConfig
// / validateProductionRevocationConfig): только явные dev-метки терпят
// ненастроенное, а пустая либо незнакомая метка — боевой класс, поэтому
// развёртывание, забывшее KACHO_APP_ENV, по-прежнему падает закрыто.
package main

import (
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// validateProductionTokenAudience refuses to start a production-class gateway
// whose accepted-token audience is not declared.
//
// Судится ЗНАЧЕНИЕ, а не длина строки: вырожденный вход (пробелы, табуляция)
// непуст по любому взгляду на профиль и пуст по существу — тот же разрыв, что
// однажды дала одинокая запятая в круге доверенных отправителей, где длина была
// у стража, а записей у транспорта не было ни одной.
func validateProductionTokenAudience(env, declared string) error {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return nil
	}
	if strings.TrimSpace(declared) != "" {
		return nil
	}
	return fmt.Errorf(
		"token audience invalid in %q env: %s is empty — the audience is the only claim "+
			"that says WHICH INSTALLATION a token was issued for, so with it undeclared the "+
			"edge narrows by nothing and accepts a token minted for a different installation "+
			"of this product; declare it as the installation's own API origin "+
			"(scheme and host, e.g. https://api.example.com) (refuse to start)",
		env, config.AudienceKnob,
	)
}
