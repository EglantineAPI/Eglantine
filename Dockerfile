FROM golang:1.26-alpine AS build
WORKDIR /opt/build

# Dependencies are copied first so the module download layer is only rebuilt
# when go.mod or go.sum change, not on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal ./internal
RUN go build -o server-executable .

FROM alpine
WORKDIR /opt/app

COPY --from=build /opt/build/server-executable .
COPY config.toml .

EXPOSE 19130/udp

ENTRYPOINT ["/opt/app/server-executable"]
