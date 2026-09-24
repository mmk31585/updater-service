FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY entry /entry
COPY migrations /migrations
ENV MIGRATIONS_DIR=/migrations
EXPOSE 8080
ENTRYPOINT ["/entry"]
