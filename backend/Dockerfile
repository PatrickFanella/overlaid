FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN go build -o /out/api ./cmd/app

FROM alpine:3.22
WORKDIR /app
RUN addgroup -S overlaid && adduser -S -G overlaid overlaid && mkdir -p /data && chown -R overlaid:overlaid /app /data
COPY --from=build /out/api /app/api
USER overlaid
EXPOSE 8080
CMD ["/app/api"]
