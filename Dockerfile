FROM golang:1.16 as promqtt
WORKDIR /promqtt
COPY go.mod go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-extldflags="-static"' .

FROM alpine
COPY --from=promqtt /promqtt/promqtt /usr/bin/promqtt
ENTRYPOINT ["/usr/bin/promqtt"]
