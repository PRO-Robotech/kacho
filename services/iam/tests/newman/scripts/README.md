# tests/newman/scripts — Newman runner

Скрипты newman regression pipeline:

- `gen.py`             — генератор Postman-коллекций из `cases/*.py`
- `run.sh`             — full-run по всем сервисам или одному (`SERVICE=iam-account`)
- `coverage.py`        — RPC → case-id coverage gate (запускается из CI)
- `validate-cases.py`  — pre-gen валидация уникальности case-id (если есть)

Форма базового удостоверения (#1253) — объявлена ОДИН раз в
`../credential-secret-form.json`; читают её двое, и вторая копия образца не
заводится:

- `credsecretmint/`   — программа чеканки настоящим кодом продукта
                        (`ids.NewID` + `credsecret.Mint`) и Go-проба
                        `form_test.go`, сверяющая объявление с тем, что продукт
                        ЧЕКАНИТ. Идёт обычным `make test-unit` (`go test ./...`),
                        отдельного вызова не требует;
- `gen.py`            — helper `credential_secret_pattern(вид)` подставляет вид и
                        вставляет образец в порождаемый скрипт кейса.

Самопроверки (стенд не нужен; одна провязана в конвейер, вторая — нет):

- `selftest_basic_access_token.py` — настоящий newman по настоящей коллекции
  против подставного края; секрет в законном ответе чеканит продукт. Законный
  вход обязан пройти молча, девять инъекций — упасть, назвав себя.
  ПРОВЯЗАНА в `.github/workflows/e2e-newman.yml` (job `plan`, шаг «форма базового
  удостоверения сверена с чеканкой продукта») — она и есть производитель того
  зелёного утверждения, которого требует предикат #1253. Провязку держит гейт
  дерева `internal/repohygiene` `TestBasicCredentialFormProofIsProducedByARun`:
  снятый вызов — находка, а не тишина;
- `selftest_token_facade_forms.py` — в конвейер НЕ провязана; предмет своей
  задачи, здесь не чинится. Перепись по дереву на день провязки (единица счёта —
  отслеживаемый git файл `tests/newman/scripts/selftest_*.py`): самопроверок 15,
  без единого вызывающего 5. Число — ориентир, а не гейт: перемеряйте предикатом
  из шапки `internal/repohygiene/basiccredentialproofrun_test.go`;

Структурно адаптировано от `kacho-vpc/tests/newman/scripts/`.
