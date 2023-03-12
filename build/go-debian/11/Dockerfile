FROM debian:11

ENV DEBIAN_FRONTEND noninteractive
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/go/bin

ARG go_version

RUN set -x \
    && apt-get update && apt-get install -y --no-install-recommends \
        wget \
        ca-certificates \
        tar \
        build-essential

RUN set -x \
    && wget "https://golang.org/dl/${go_version}.linux-amd64.tar.gz" -O /root/go.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf /root/go.linux-amd64.tar.gz

RUN set -x \
    && echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/bash.bashrc \
    && echo 'export GOPATH=/src' >> /etc/bash.bashrc \
    && mkdir /src/

WORKDIR /src/

CMD /bin/bash
