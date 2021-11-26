# build stage
FROM golang:1.17 as go

ENV GO111MODULE=on

WORKDIR /cmanager

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

# final stage
FROM scratch
COPY --from=go /cmanager/cmanager /bin/cmanager

ENTRYPOINT ["/bin/cmanager"]
