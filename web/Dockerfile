FROM docker.io/library/node:24-alpine AS build
WORKDIR /src
COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY . ./
ARG VITE_API_BASE_URL
RUN pnpm run build

FROM docker.io/library/nginx:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
