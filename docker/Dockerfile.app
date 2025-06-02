FROM golang:1.24

COPY server/ ./server
RUN go mod download
RUN go build -o /server

CMD ["/server"]
