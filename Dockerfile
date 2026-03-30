#
# Stage 1 – build the Go binary
#
FROM golang:1.22-alpine3.20 AS builder

WORKDIR /src
COPY server /src

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /GoGtmGaProxy .

#
# Stage 2 – minimal runtime image
#
FROM alpine:3.20

ARG BUILD_DATE
ARG BUILD_VERSION
ARG VCS_REF

LABEL author="Dennis Paul"
LABEL maintainer="dennis@blaumedia.com"

LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build_date=$BUILD_DATE
LABEL org.label-schema.name="Go-GTM-GA-Proxy"
LABEL org.label-schema.vcs-url="https://github.com/blaumedia/go-gtm-ga-proxy"
LABEL org.label-schema.version=$BUILD_VERSION
LABEL org.label-schema.vcs-ref=$VCS_REF

ENV APP_VERSION=${BUILD_VERSION}

EXPOSE 8080

RUN apk --no-cache add ca-certificates uglify-js curl && \
    addgroup -S docker -g 433 && \
    adduser -u 431 -S -g docker -h /app -s /sbin/nologin docker

USER docker
WORKDIR /app

COPY --from=builder /GoGtmGaProxy ./GoGtmGaProxy

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f "http://localhost:8080/${JS_SUBDIRECTORY}/${GA_FILENAME}" || exit 1

CMD ["./GoGtmGaProxy"]
