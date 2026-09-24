FROM alpine:3.22
RUN apk add --no-cache ca-certificates docker-cli
COPY worker /worker
COPY migrations /migrations
ENV MIGRATIONS_DIR=/migrations
ENTRYPOINT ["/worker"]
