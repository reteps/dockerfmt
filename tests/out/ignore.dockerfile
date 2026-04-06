FROM scratch
# dockerfmt-ignore
RUN   echo   "do not format this"   &&   echo    "keep as-is"
RUN echo "this should be formatted" && echo "yes"
# a normal comment
# dockerfmt-ignore
ENV    FOO    bar
COPY src dst

# Grouping expressions aren't formatted well by shfmt
# dockerfmt-ignore
RUN { echo hello && echo world; }

# Unescaped semicolons aren't supported
# dockerfmt-ignore
RUN echo hello; echo world
