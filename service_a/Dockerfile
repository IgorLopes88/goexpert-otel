FROM golang:latest AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN GOOS=linux CGO_ENABLED=0 go build -ldflags="-w -s" -o bin/service_a main.go

FROM scratch
WORKDIR /app
COPY --from=builder /app/bin/service_a .
ENTRYPOINT [ "./service_a" ]
