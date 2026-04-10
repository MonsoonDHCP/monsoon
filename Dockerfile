FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/monsoon ./cmd/monsoon

FROM scratch
COPY --from=build /out/monsoon /monsoon
EXPOSE 67/udp 547/udp 8067 9067 7067
VOLUME ["/var/lib/monsoon"]
ENTRYPOINT ["/monsoon"]
