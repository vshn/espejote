FROM docker.io/library/alpine:3.22

RUN \
  apk add --update --no-cache \
    bash \
    curl \
    ca-certificates \
    tzdata

ENTRYPOINT ["espejote"]
COPY espejote /usr/bin/

USER 65536:0
