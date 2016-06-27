#use the golang base image
FROM golang:1.6
MAINTAINER Vic van Gool

#switch to our app directory
RUN mkdir -p /app
WORKDIR /app

#copy the source files
ADD . /app

ENV CGO_ENABLED=0
ENV GOOS=linux

RUN go build -a -o janitor
