FROM alpine:3.24
RUN apk add --no-cache bash curl postgresql-client
COPY --chmod=755 deploy/docker/migrate/run-migrations.sh /usr/local/bin/run-migrations.sh
COPY collector/schema /migrations
USER 10001:10001
ENV MIGRATIONS_DIR=/migrations
CMD ["/usr/local/bin/run-migrations.sh"]
