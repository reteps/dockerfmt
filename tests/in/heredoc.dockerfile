RUN <<EOF
echo "Hello" >>             /hello
echo "World!">>/hello
EOF
RUN ls

RUN <<-EOF
echo "Hello" >>             /hello
echo "World!">>/hello
EOF

COPY <<-EOF /x
x
EOF

COPY <<-EOT /script.sh
  echo "hello ${FOO}"
EOT
COPY <<-EOF /x
x
EOF

