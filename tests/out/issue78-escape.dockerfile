# escape=`
FROM scratch
# With a backtick escape, a backslash is an ordinary character.
ENV MSG="line one `
line two" `
    DIR="C:\dir\" `
    OTHER=`"x `
    Y=`"
ENV QUOTE="say `"hi `
   there`"" `
    NEXT=1
LABEL path="C:\x\ `
 two" `
    b=c
ARG X="a\" `
    Y=1
