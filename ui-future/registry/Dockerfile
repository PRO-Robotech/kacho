FROM node:22-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/registry-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

COPY registry/package*.json ./
RUN npm ci

COPY registry/ ./
RUN npm run build

FROM nginx:1.27-alpine

COPY registry/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
