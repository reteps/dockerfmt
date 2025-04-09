RUN <<EOF
echo "Hello" >>/hello
echo "World!" >>/hello
EOF
RUN ls

RUN <<EOF
echo "Hello" >>/hello
echo "World!" >>/hello
EOF

RUN ls
