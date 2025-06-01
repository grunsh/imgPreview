FROM golang:1.19

#WORKDIR /app

COPY server/ ./server
RUN go mod download
RUN go build -o /server

CMD ["/server"]
