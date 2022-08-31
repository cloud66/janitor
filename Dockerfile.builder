#use the golang base image
FROM golang:1.18
MAINTAINER Vic van Gool

#switch to our app directory
RUN mkdir -p /go/src/github.com/cloud66/janitor
WORKDIR /go/src/github.com/cloud66/janitor

#copy the source files
ADD . /go/src/github.com/cloud66/janitor

ENV CGO_ENABLED=0
ENV GOOS=linux

RUN go get
RUN go build -a -o janitor
