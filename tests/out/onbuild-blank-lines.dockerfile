FROM alpine

ONBUILD ARG FOO=bar

RUN echo $FOO

ONBUILD ENV BAR=baz

ONBUILD LABEL foo=bar

RUN echo done
