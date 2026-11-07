FROM scratch
# Continuation lines inside quoted values keep their leading whitespace.
ENV MSG="line one \
line two"
ENV SINGLE='single one \
        single two'
ENV TRAILING="ends with spaces   \
  next"
ENV AFTER_ESCAPE="spaces after the backslash are dropped \
  next" \
    TAB=1
ENV ESCAPED="say \"hi \
   there\" \
   friend" \
    OTHER=1
ENV MIXED=plain \
    QUOTED="a \
b" \
    AFTER=c \
    # a comment line is dropped by BuildKit, even inside quotes
    LAST='x \
    # dropped too
  y'
ENV JOINED=foo\
bar \
    NEXT=1
ENV NOTQUOTED=it\'s \
    NEXT="x"
LABEL description="first line \
second line" \
    version="1.0"
LABEL single='one \
   two'
ARG GREETING="hello \
world"
ARG OTHER='it\' \
    ESC="a \"b\" \
  c"
