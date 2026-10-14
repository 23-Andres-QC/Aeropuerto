FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./... && go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /camara-web .
FROM alpine:3.22
RUN adduser -D -u 10001 app
USER app
# Aquí se guardan el certificado autofirmado y los códigos de unión (volúmenes camara-cert y camara-datos en compose).
RUN mkdir -p /home/app/certs /home/app/datos
COPY --from=build /camara-web /camara-web
EXPOSE 8080 8443
ENTRYPOINT ["/camara-web"]
