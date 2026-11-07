# https://github.com/reteps/dockerfmt/issues/80
FROM python:3.13-alpine
RUN <<EOF python3
print("hello")
EOF
RUN python3 <<EOF
print("x")
EOF
RUN <<EOF
#!/usr/bin/env python3
print("y")
EOF
RUN <<EOF
echo shell
EOF
RUN <<EOF
#!/bin/bash
echo bash script
EOF
RUN cat <<EOF > /etc/app.conf
key =   value
EOF
