# Tiny build image for the fake-echo binary. Used by the docker-compose
# under e2e to spin up two scripted Echo instances side by side.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN go build -trimpath -ldflags='-s -w' -o /fake-echo .

FROM alpine:3.20
RUN adduser -D -u 10001 fake
COPY --from=build /fake-echo /usr/local/bin/fake-echo
USER fake
ENTRYPOINT ["fake-echo"]
