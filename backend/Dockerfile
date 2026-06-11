FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN go build -o /out/api ./cmd/app

FROM alpine:3.22
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
