# https://github.com/reteps/dockerfmt/issues/76
FROM scratch
COPY <<a.txt <<b.txt /dst/
first
a.txt
second
b.txt
FROM alpine
RUN <<A <<B cat
echo  a
A
echo  b
B
# Flags on a heredoc COPY are kept
COPY --chmod=755 <<-EOT /build.sh
#!/bin/sh
	set -ex
EOT
COPY --link --chmod=755 <<"EOF" /entry.sh
#!/bin/sh
exec "$@"
EOF
