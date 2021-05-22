FROM golang:alpine as builder
ARG LDFLAGS=""

RUN apk --update --no-cache add git build-base gcc

COPY . /build
WORKDIR /build

RUN go build -ldflags "${LDFLAGS}" -o goflow2 cmd/goflow2/main.go

FROM alpine:latest
ARG src_dir
ARG VERSION=""
ARG CREATED=""
ARG DESCRIPTION=""
ARG NAME=""
ARG MAINTAINER=""
ARG URL=""
ARG LICENSE=""
ARG REV=""

LABEL org.opencontainers.image.created="${CREATED}"
LABEL org.opencontainers.image.authors="${MAINTAINER}"
LABEL org.opencontainers.image.url="${URL}"
LABEL org.opencontainers.image.title="${NAME}"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.description="${DESCRIPTION}"
LABEL org.opencontainers.image.licenses="${LICENSE}"
LABEL org.opencontainers.image.revision="${REV}"

RUN apk update --no-cache && \
    adduser -S -D -H -h / flow
USER flow
COPY --from=builder /build/goflow2 /

ENTRYPOINT ["./goflow2"]
