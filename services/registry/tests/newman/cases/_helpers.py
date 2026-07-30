# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/cases/_helpers.py — reserved module for future per-domain helpers.

gen.py skips any file in tests/newman/cases/ whose name begins with '_', so this
file is NOT compiled into a collection. It exists as a stable home for helpers
that grow too large to live inline in gen.py or are too NLB-specific to share
with kacho-vpc.

Currently all reusable blocks live in scripts/gen.py:
  - Step / Case dataclasses
  - assert_status / assert_grpc_code / assert_field_violation
  - save_from_response / assert_operation_envelope
  - poll_operation_until_done
  - http_method_not_allowed_block

`conf_alreadyexists_block` здесь БОЛЬШЕ НЕТ, и это не пропуск. Он был скопирован из
kacho-nlb, ни одним кейсом registry не вызывался ни разу, и принимал `oneOf([200, 409])`
там, где у registry полоса отказа на дубликате ОДНА и она синхронная (вставка идёт в
самом вызове). То есть в дереве лежал генератор кейсов, который никто не звал и который
на первом же вызове дал бы утверждение, неспособное упасть. Дубликаты у registry
покрывают собственные кейсы: `REG-CR-CONF-ALREADY-EXISTS` (реестр) и `REPO-CR-NEG-DUP`
(репозиторий).

If you find yourself copy-pasting the same multi-step block across cases/*.py
files, lift it here, then explicitly inject it into the module namespace inside
gen.load_cases_module().
"""
