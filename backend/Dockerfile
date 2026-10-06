FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown
RUN go build -ldflags "-X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Version=${BUILD_VERSION} -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Commit=${BUILD_COMMIT} -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.BuiltAt=${BUILD_DATE}" -o /out/api ./cmd/app

FROM docker.io/library/alpine:3.22
WORKDIR /app
RUN apk add --no-cache su-exec \
  && addgroup -S overlaid \
  && adduser -S -G overlaid overlaid \
  && mkdir -p /data \
  && chown -R overlaid:overlaid /app /data
COPY --from=build /out/api /app/api
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["/app/api"]
