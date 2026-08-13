FROM golang:1.26-alpine AS build

WORKDIR /src
COPY nextendo-nex ./nextendo-nex
COPY server ./server
WORKDIR /src/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mk8d-server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 nextendo \
    && adduser -S -D -H -u 10001 -G nextendo nextendo \
    && mkdir -p /data \
    && chown nextendo:nextendo /data
COPY --from=build /out/mk8d-server /usr/local/bin/mk8d-server
USER nextendo
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/mk8d-server"]
