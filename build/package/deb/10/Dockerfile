FROM debian:10

ENV DEBIAN_FRONTEND noninteractive

RUN set -x \
    && apt-get update && apt-get install -y --no-install-recommends \
        ruby \
        ruby-dev \
        gcc \
        make \
        ca-certificates \
        libffi-dev \
        ruby-ffi \
        libc6-dev \
    && gem install fpm \
    && mkdir /src/

WORKDIR /src/

CMD /usr/local/bin/fpm
