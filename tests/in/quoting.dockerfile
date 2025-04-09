FROM hackernews
# https://news.ycombinator.com/item?id=43630653
ENTRYPOINT service ssh restart && bash

ENTRYPOINT sh -c 'service ssh restart && bash'