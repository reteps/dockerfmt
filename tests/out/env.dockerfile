# https://github.com/un-ts/prettier/issues/398
ENV a=1 \
    b=2 \
    # comment
    c=3 \
    d=4 \
    # comment
    e=5

ENV MY_VAR=my-value
ENV MY_VAR=my-value2 \
    c=4
ENV MY_VAR=my-value3
