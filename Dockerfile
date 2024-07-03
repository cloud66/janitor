FROM golang AS build
ARG SHORT_SHA
WORKDIR /github.com/cloud66/janitor
COPY . .
RUN go mod vendor
RUN CGO_ENABLED=0 go build -o /github.com/cloud66/janitor/janitor -ldflags="-X 'github.com/cloud66/janitor/utils.Commit=${SHORT_SHA}'"

FROM alpine
LABEL maintainer="Cloud 66 Engineering <hello@cloud66.com>"
COPY --from=build /github.com/cloud66/janitor/janitor /bin/janitor