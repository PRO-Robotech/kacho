// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenverifier_test.go — стражи старта приёмной стороны: F1-20 (адресат и
// издатель обязательны), F1-21/F1-41 (перечень считается ЭЛЕМЕНТАМИ, а не
// длиной строки), F1-43 (смена издателя видна при старте, а не в рантайме),
// F1-46 (издатель без записи источника и вырожденная запись → отказ в старте).
//
// У каждого отрицания здесь стоит положительный контроль: набор утверждений,
// целиком состоящий из ожиданий отказа, зеленеет на страже, отвергающем всё, —
// то есть не отличает исправную защиту от неподнимаемого сервиса.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/stretchr/testify/require"
)

const (
	probePlatformIssuer = "https://iam.kacho.local"
	probeLegacyIssuer   = "https://hydra.api.kacho.cloud"
	probePlatformKeySet = "https://kaname-internal.kacho.svc:9097/.well-known/kacho/jwks.json"
	probeLegacyKeySet   = "https://kaname-internal.kacho.svc:9097/.well-known/jwks.json"
	probeRevocationURL  = "https://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
)

// probeClientCreds выкладывает файлами якорь и пару клиентского сертификата
// ребра к авторитету отзыва.
//
// Настройка ребра называет ФАЙЛЫ, поэтому проба обязана их создать: страж,
// проверенный на несуществующих путях, утверждал бы только о своей же ошибке
// чтения, а не о полноте объявления.
func probeClientCreds(t *testing.T) grpcclient.TLSClient {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kacho-internal-ca (probe)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)
	caFile := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "kacho-registry"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"kacho-registry"},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	return grpcclient.TLSClient{
		Enable:     true,
		CAFiles:    []string{caFile},
		CertFile:   certFile,
		KeyFile:    keyFile,
		ServerName: "kaname-internal.kacho.svc",
	}
}

// prodConfig — полная, законная посадка приёмной стороны в производственном
// режиме. Пробы портят её ПО ОДНОЙ оси: иначе неясно, что именно уронило старт.
func prodConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		AuthMode:     "production",
		ServiceAud:   "registry.kacho.local",
		TokenIssuers: strings.Join([]string{probePlatformIssuer, probeLegacyIssuer}, ","),
		TokenIssuerKeySets: strings.Join([]string{
			probePlatformIssuer + "=" + probePlatformKeySet,
			probeLegacyIssuer + "=" + probeLegacyKeySet,
		}, ","),
		PlatformTokenIssuer: probePlatformIssuer,
		TokenRevocationURL:  probeRevocationURL,
		TokenRevocationMTLS: probeClientCreds(t),
	}
}

// TestF1_41_IssuerSetIsCountedByElementsNotByLength — F1-41.
//
// Значение, которое ВЫГЛЯДИТ непустым, но элементов не содержит, обязано
// ронять старт. Страж считает элементы: у «,» длина 1 и элементов ноль, и
// именно на этом входе страж по длине молчит, а приём принимает любого.
func TestF1_41_IssuerSetIsCountedByElementsNotByLength(t *testing.T) {
	degenerate := map[string]string{
		"пусто":                      "",
		"один разделитель":           ",",
		"разделители подряд":         ",,,",
		"пробелы":                    "   ",
		"пробелы вокруг разделителя": "  ,  ,  ",
		"перевод строки":             "\n\t",
	}
	for name, raw := range degenerate {
		t.Run(name, func(t *testing.T) {
			cfg := prodConfig(t)
			cfg.TokenIssuers = raw
			_, err := buildTokenVerifier(cfg)
			require.Errorf(t, err, "значение %q содержит ноль издателей и обязано ронять старт", raw)
			require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_ISSUERS",
				"сообщение обязано называть незаполненную настройку")
		})
	}

	t.Run("положительный контроль: один издатель", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = probePlatformIssuer
		cfg.TokenIssuerKeySets = probePlatformIssuer + "=" + probePlatformKeySet
		_, err := buildTokenVerifier(cfg)
		require.NoError(t, err)
	})

	t.Run("положительный контроль: два издателя", func(t *testing.T) {
		_, err := buildTokenVerifier(prodConfig(t))
		require.NoError(t, err, "обе мощности перечня обязаны подниматься")
	})

	t.Run("положительный контроль: пробелы вокруг законных элементов", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = "  " + probePlatformIssuer + " ,  " + probeLegacyIssuer + "  "
		_, err := buildTokenVerifier(cfg)
		require.NoError(t, err, "пробелы вокруг элементов не делают перечень вырожденным")
	})
}

// TestF1_20_AudienceAndIssuerAreRequiredInProduction — F1-20.
//
// Незаданный адресат означает «принимаем любого адресата», незаданный издатель
// — «принимаем любого издателя». Оба — отказ в старте; сообщение называет
// настройку; с обеими заполненными процесс поднимается.
func TestF1_20_AudienceAndIssuerAreRequiredInProduction(t *testing.T) {
	t.Run("адресат не задан", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.ServiceAud = ""
		_, err := buildTokenVerifier(cfg)
		require.Error(t, err, "незаданный ожидаемый адресат означает «любой адресат»")
	})

	t.Run("издатель не задан", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = ""
		_, err := buildTokenVerifier(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_ISSUERS")
	})

	t.Run("положительный контроль: обе настройки заполнены", func(t *testing.T) {
		_, err := buildTokenVerifier(prodConfig(t))
		require.NoError(t, err)
	})
}

// TestF1_46_IssuerWithoutAKeySetRecordRefusesStart — F1-46, половина про страж
// старта.
//
// Проверка НЕ ТОЖДЕСТВЕННО ИСТИННА ровно потому, что адрес объявляется, а не
// выводится: будь он выведен из издателя, состояние «записи нет» не наступало бы
// ни при каком издателе, и страж остался бы в тексте, не имея возможности упасть.
func TestF1_46_IssuerWithoutAKeySetRecordRefusesStart(t *testing.T) {
	t.Run("принимаемый издатель без записи источника", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuerKeySets = probePlatformIssuer + "=" + probePlatformKeySet // запись прежнего снята
		_, err := buildTokenVerifier(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), probeLegacyIssuer, "сообщение обязано называть издателя без записи")
		require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS")
	})

	t.Run("запись источника без принимающего её издателя", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = probePlatformIssuer
		_, err := buildTokenVerifier(cfg)
		require.Error(t, err, "запись, которую никто не читает, переживает свой предмет молча")
	})

	t.Run("положительный контроль: полная привязка поднимается", func(t *testing.T) {
		_, err := buildTokenVerifier(prodConfig(t))
		require.NoError(t, err)
	})
}

// TestF1_46_DegenerateKeySetRecordRefusesStart — F1-46, третий экземпляр класса
// «пустое значение означает „не сужаем“»: издатель в перечне есть, а адрес пуст
// либо состоит из одних разделителей.
func TestF1_46_DegenerateKeySetRecordRefusesStart(t *testing.T) {
	degenerate := map[string]string{
		"адрес пуст":            probePlatformIssuer + "=",
		"адрес из пробелов":     probePlatformIssuer + "=   ",
		"один разделитель пути": probePlatformIssuer + "=/",
		"два разделителя пути":  probePlatformIssuer + "=//",
		"три разделителя пути":  probePlatformIssuer + "=///",
		"относительный путь":    probePlatformIssuer + "=/.well-known/jwks.json",
		"нет знака равенства":   probePlatformIssuer,
		"издателя нет":          "=" + probePlatformKeySet,
	}
	for name, raw := range degenerate {
		t.Run(name, func(t *testing.T) {
			cfg := prodConfig(t)
			cfg.TokenIssuers = probePlatformIssuer
			cfg.TokenIssuerKeySets = raw
			_, err := buildTokenVerifier(cfg)
			require.Errorf(t, err, "запись %q объявляет источник, которого нет", raw)
		})
	}

	t.Run("положительный контроль: адрес со строкой запроса разбирается", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = probePlatformIssuer
		cfg.PlatformTokenIssuer = probePlatformIssuer
		// Знак равенства встречается и ВНУТРИ адреса: разделяет только первый.
		cfg.TokenIssuerKeySets = probePlatformIssuer + "=" + probePlatformKeySet + "?set=platform"
		_, err := buildTokenVerifier(cfg)
		require.NoError(t, err)
	})
}

// TestF1_43_IssuerChangeIsVisibleAtStartNotAtRuntime — F1-43.
//
// Чеканка контура переведена на наш подписант, а пин издателя на приёмной
// стороне не пересмотрен: старт отвергается с сообщением, называющим настройку.
// После пересмотра — поднимается. Место, пройденное не полностью, дало бы отказ
// проверки при первом же запросе вместо отказа при старте.
func TestF1_43_IssuerChangeIsVisibleAtStartNotAtRuntime(t *testing.T) {
	t.Run("пин не пересмотрен: наш издатель не принимается", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = probeLegacyIssuer
		cfg.TokenIssuerKeySets = probeLegacyIssuer + "=" + probeLegacyKeySet
		cfg.PlatformTokenIssuer = probePlatformIssuer // чеканим под ним, а принимать не собираемся

		_, err := buildTokenVerifier(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER",
			"сообщение обязано называть настройку, которую забыли пересмотреть")
	})

	t.Run("положительный контроль: после пересмотра поднимается", func(t *testing.T) {
		_, err := buildTokenVerifier(prodConfig(t))
		require.NoError(t, err)
	})
}

// TestF1_25_RevocationAuthorityIsRequiredWhenOurIssuerIsAccepted — страж старта
// половины F1-25: наш издатель принимается ⇒ у отзыва обязан быть читатель.
//
// Без этого стража контроль существует, провязан, исполняется на каждом запросе
// — и не отказывает ни разу за всё время жизни, потому что его просто нет.
func TestF1_25_RevocationAuthorityIsRequiredWhenOurIssuerIsAccepted(t *testing.T) {
	t.Run("адрес авторитета не задан", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenRevocationURL = ""
		_, err := buildTokenVerifier(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_REVOCATION_URL")
	})

	t.Run("адрес авторитета не абсолютный", func(t *testing.T) {
		for _, bad := range []string{"/internal/tokens/introspect", "kaname-internal:9097", "   "} {
			cfg := prodConfig(t)
			cfg.TokenRevocationURL = bad
			_, err := buildTokenVerifier(cfg)
			require.Errorf(t, err, "адрес %q не является объявленным абсолютным адресом", bad)
		}
	})

	t.Run("адрес авторитета по открытому HTTP", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenRevocationURL = "http://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
		_, err := buildTokenVerifier(cfg)
		require.Error(t, err, "ответ авторитета решает вопрос доступа и не транзитит открытым текстом")
	})

	t.Run("наш издатель не принимается — авторитет не требуется", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = probeLegacyIssuer
		cfg.TokenIssuerKeySets = probeLegacyIssuer + "=" + probeLegacyKeySet
		cfg.PlatformTokenIssuer = ""
		cfg.TokenRevocationURL = ""
		_, err := buildTokenVerifier(cfg)
		require.NoError(t, err,
			"полоса прежнего издателя вне области под-фазы: её поведение не меняется")
	})

	t.Run("положительный контроль: авторитет объявлен — поднимается", func(t *testing.T) {
		_, err := buildTokenVerifier(prodConfig(t))
		require.NoError(t, err)
	})
}

// TestKeySetURLMustBeSecureInProduction — набор проверочных ключей есть
// единственный якорь доверия проверки подписи; по открытому HTTP его документ
// подменяется на пути. В dev открытый HTTP допустим — симметрично
// незашифрованному соединению к базе.
//
// Проба идёт через сборку целиком, а не зовёт предикат по имени: предмет здесь
// — МЕСТО, где объявление отвергается (старт), а не ответ отдельной функции.
// Вызов по имени остался бы зелёным, даже если бы сборка перестала его звать.
func TestKeySetURLMustBeSecureInProduction(t *testing.T) {
	cases := []struct {
		name      string
		authMode  string
		keySetURL string
		wantErr   bool
	}{
		{"dev-http-ok", "dev", "http://kaname-internal.kacho.svc:9097/keys", false},
		{"dev-https-ok", "dev", "https://kaname-internal.kacho.svc:9097/keys", false},
		{"prod-http-rejected", "production", "http://kaname-internal.kacho.svc:9097/keys", true},
		{"prod-https-ok", "production", "https://kaname-internal.kacho.svc:9097/keys", false},
		{"prod-strict-http-rejected", "production-strict", "http://kaname-internal.kacho.svc:9097/keys", true},
		{"prod-strict-https-ok", "production-strict", "https://kaname-internal.kacho.svc:9097/keys", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := prodConfig(t)
			cfg.AuthMode = tc.authMode
			cfg.TokenIssuers = probePlatformIssuer
			cfg.TokenIssuerKeySets = probePlatformIssuer + "=" + tc.keySetURL
			_, err := buildTokenVerifier(cfg)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS",
					"операторская диагностика обязана называть актуальное имя настройки")
				require.Contains(t, err.Error(), probePlatformIssuer,
					"сообщение обязано называть издателя, чья запись негодна")
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestF1_41_IssuerSetIsCountedByElementsInEveryMode — перечень издателей обязан
// содержать элементы В ЛЮБОМ режиме, и это ОДНО правило, а не два.
//
// Здесь стояли два предиката об одном поле: страж старта освобождал режим
// разработки, читатель того же перечня — нет. Освобождение было недостижимым
// (отказ приходил из читателя), но объявляло посадку, которой у кода нет:
// «в разработке принимаем любого издателя». Правило, которое нельзя исполнить,
// хуже отсутствующего; пустой перечень принимающих — прямо запрещённая посадка.
//
// Проба закрепляет РАЗРЕШЕНИЕ спора, а не вводит новое поведение: она зелена и
// до сведения предикатов, и после. Красной её сделает возвращённое послабление.
func TestF1_41_IssuerSetIsCountedByElementsInEveryMode(t *testing.T) {
	for _, mode := range []string{"dev", "production", "production-strict"} {
		t.Run(mode+"/вырожденный перечень отвергается", func(t *testing.T) {
			cfg := prodConfig(t)
			cfg.AuthMode = mode
			cfg.TokenIssuers = ","
			_, err := buildTokenVerifier(cfg)
			require.Errorf(t, err, "режим %q не освобождает от требования непустого перечня: "+
				"пустой перечень означает «принимаем любого издателя»", mode)
			require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_ISSUERS")
			require.Contains(t, err.Error(), "1 characters and 0 elements",
				"диагностика обязана называть ОБЕ величины — длину и число элементов: "+
					"именно их расхождение и есть предмет")
		})

		t.Run(mode+"/положительный контроль: законный перечень поднимается", func(t *testing.T) {
			cfg := prodConfig(t)
			cfg.AuthMode = mode
			_, err := buildTokenVerifier(cfg)
			require.NoErrorf(t, err, "режим %q обязан поднимать законное объявление — "+
				"иначе отрицание выше зеленело бы на страже, отвергающем всё", mode)
		})
	}
}

// TestF1_25_RevocationEdgeCredentialsRefuseTheStartWhenUnusable — учётные данные
// ребра к авторитету отзыва.
//
// Ребро СВОЁ, а не общее с загрузкой набора ключей: набор несёт только публичный
// материал и потому открыт задокументированным исключением, а маршруту отзыва
// присылают ПРЕДЪЯВЛЕННЫЙ ТОКЕН — молчаливое распространение исключения было бы
// допущением «внутренний периметр доверенный». Якорь, объявленный и непригодный,
// роняет СТАРТ: откат на системные корни всегда «работает», поэтому ошибка в
// якоре стала бы ненаблюдаемой.
func TestF1_25_RevocationEdgeCredentialsRefuseTheStartWhenUnusable(t *testing.T) {
	cases := map[string]func(c *config.Config){
		"учётные данные не объявлены": func(c *config.Config) {
			c.TokenRevocationMTLS = grpcclient.TLSClient{}
		},
		"объявлены, но выключены": func(c *config.Config) {
			c.TokenRevocationMTLS.Enable = false
		},
		"якорь не объявлен": func(c *config.Config) {
			c.TokenRevocationMTLS.CAFiles = nil
		},
		"якоря нет на диске": func(c *config.Config) {
			c.TokenRevocationMTLS.CAFiles = []string{filepath.Join(t.TempDir(), "absent.crt")}
		},
		"имя сервера не объявлено": func(c *config.Config) {
			c.TokenRevocationMTLS.ServerName = ""
		},
		"пара сертификата неполна": func(c *config.Config) {
			c.TokenRevocationMTLS.KeyFile = ""
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := prodConfig(t)
			breakIt(&cfg)
			_, err := buildTokenVerifier(cfg)
			require.Error(t, err, "непригодные учётные данные ребра обязаны ронять старт, а не откатываться на системные корни")
			require.Contains(t, err.Error(), "KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_*",
				"операторская диагностика обязана называть настройку, которую надо чинить")
		})
	}

	t.Run("положительный контроль: полные учётные данные поднимаются", func(t *testing.T) {
		_, err := buildTokenVerifier(prodConfig(t))
		require.NoError(t, err)
	})

	t.Run("наш издатель не принимается — ребро не строится вовсе", func(t *testing.T) {
		cfg := prodConfig(t)
		cfg.TokenIssuers = probeLegacyIssuer
		cfg.TokenIssuerKeySets = probeLegacyIssuer + "=" + probeLegacyKeySet
		cfg.PlatformTokenIssuer = ""
		cfg.TokenRevocationURL = ""
		cfg.TokenRevocationMTLS = grpcclient.TLSClient{}
		_, err := buildTokenVerifier(cfg)
		require.NoError(t, err, "полоса прежнего издателя вне области под-фазы и учётных данных этого ребра не требует")
	})
}
