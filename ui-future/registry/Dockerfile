FROM node:22-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/registry-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

# Этот remote ставит зависимости сам (свой package-lock), но собирает и
# исходники shared/ — они лежат рядом, а не внутри его дерева. Установка в
# КОРЕНЬ стадии даёт обоим деревьям один node_modules: иначе импорты
# shared/src (react, @radix-ui, clsx …) резолвились бы вверх от /app/shared
# и не находили ничего, а react нашёлся бы дважды.
COPY registry/package.json registry/package-lock.json ./
RUN npm ci

COPY shared/ shared/
COPY registry/ registry/
WORKDIR /app/registry
RUN npm run build

FROM nginx:1.27-alpine

COPY registry/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/registry/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
