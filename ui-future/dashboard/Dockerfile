FROM node:26-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/dashboard-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

# Этот remote ставит зависимости сам (свой package-lock), но собирает и исходники
# shared/ — они лежат рядом, а не внутри его дерева.
#
# ПОЧЕМУ ЭТО ЗДЕСЬ. Модуль начал импортировать из `@shared` (граница отказа
# раздела, канон имён), и без этой строки образ собирается на дереве, где
# `shared/` нет вовсе: `tsc` не находит модуль, сборка падает. Локальная сборка
# при этом зелёная — каталог рядом, — и расхождение видно ТОЛЬКО в образе.
#
# Установка в КОРЕНЬ стадии даёт обоим деревьям один node_modules: иначе импорты
# shared/src резолвились бы вверх от /app/shared и не находили ничего, а react
# нашёлся бы дважды. Так же устроены образы compute, storage, nlb и registry.
COPY dashboard/package*.json ./
RUN npm ci

COPY shared/ shared/
COPY dashboard/ dashboard/
WORKDIR /app/dashboard
RUN npm run build

# nginx-unprivileged: работает под non-root uid 101, слушает 8080, pid и временные файлы
# уводит в /tmp — поэтому уживается с runAsNonRoot + readOnlyRootFilesystem
# (см. deploy/templates/deployment-*.yaml). Конфиг в кластере приезжает шаблоном из
# ConfigMap и рендерится в /etc/nginx/conf.d, куда деплоймент подкладывает emptyDir;
# конфиг ниже — запечённый, для одиночного `docker run`.
FROM nginxinc/nginx-unprivileged:1.31-alpine

COPY dashboard/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dashboard/dist /usr/share/nginx/html

# ПРОВЕНАНС ОБРАЗА — из какого дерева он собран. Клеймо и файл берут величину из
# ОДНОГО `ARG`, поэтому разойтись не могут. Файл нужен потому, что у РАБОТАЮЩЕГО
# контейнера клеймо не прочесть ничем, кроме демона хоста или реестра, — а у
# управляемого кластера нет ни того, ни другого, тогда как `kubectl exec … cat`
# есть всегда. Пустое умолчание намеренно: сборка руками ревизии знать не обязана,
# а цель провенанса скажет словами, что величина не проставлена.
# Разбор целиком — deploy/scripts/stand-provenance.sh; что величину проставляет
# КАЖДАЯ сборка дерева — deploy/stand_provenance_declaration_test.go.
ARG KACHO_IMAGE_REVISION=""
ARG KACHO_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$KACHO_IMAGE_REVISION" \
      org.opencontainers.image.version="$KACHO_IMAGE_VERSION"
USER root
RUN mkdir -p /etc/kacho && printf '%s\n' "$KACHO_IMAGE_REVISION" > /etc/kacho/image-revision

# Явный non-root USER (trivy DS-0002: контейнер не должен работать под root).
USER 101

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
