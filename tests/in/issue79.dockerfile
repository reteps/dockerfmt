# https://github.com/reteps/dockerfmt/issues/79
FROM scratch
CMD ["a",]
ENTRYPOINT [not json]
CMD ["ok", "x"]
RUN ["a",]
