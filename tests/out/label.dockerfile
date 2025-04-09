# QLC Docker Container
# https://www.qlcplus.org

# https://github.com/phusion/baseimage-docker/blob/master/Changelog.md
FROM phusion/baseimage:0.11

LABEL maintainer="REDACTED"

ARG BUILD_DATE
ARG VCS_REF
ARG BUILD_VERSION

# Labels.
LABEL org.label-schema.schema-version="1.0"
LABEL org.label-schema.build-date=$BUILD_DATE
LABEL org.label-schema.name="djarbz/qlcplus"
LABEL org.label-schema.description="QLC+ Docker Image with GUI"
LABEL org.label-schema.url="https://www.qlcplus.org"
LABEL org.label-schema.vcs-url="https://github.com/djarbz/qlcplus"
LABEL org.label-schema.vcs-ref=$VCS_REF
LABEL org.label-schema.vendor="DJArbz"
LABEL org.label-schema.version=$BUILD_VERSION
LABEL org.label-schema.docker.cmd="docker run -it --rm --name QLCplus --device /dev/snd -p 9999:80 --volume='/tmp/.X11-unix:/tmp/.X11-unix:rw' --env=DISPLAY=unix${DISPLAY} djarbz/qlcplus"
LABEL org.label-schema.docker.cmd.devel="docker run -it --rm --name QLCplus djarbz/qlcplus:4.11.2 xvfb-run qlcplus"

VOLUME /QLC

WORKDIR /QLC

ENV QLC_DEPENDS="\
    libasound2 \
    libfftw3-double3 \
    libftdi1 \
    libqt4-network \
    libqt4-script \
    libqtcore4 \
    libqtgui4 \
    libusb-0.1-4"

# XVFB is used to fake an X server for testing and headless mode.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ${QLC_DEPENDS} \
        xvfb \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# https://github.com/mcallegari/qlcplus/releases/tag/QLC+_4.11.2
ARG QLC_VERSION=4.11.2

ADD https://www.qlcplus.org/downloads/${QLC_VERSION}/qlcplus_${QLC_VERSION}_amd64.deb /opt/qlcplus.deb

RUN dpkg -i /opt/qlcplus.deb

# https://www.qlcplus.org/docs/html_en_EN/commandlineparameters.html
CMD ["/usr/bin/qlcplus", "--operate", "--web", "--open", "/QLC/default_workspace.qxw"]
