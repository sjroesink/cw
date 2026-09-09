# The site as one binary and one data directory, so it runs the same anywhere.
#
# Mermaid, the highlighter and the two typefaces are pulled in during the build
# rather than at runtime. That way a container with no outbound network still
# serves a page that draws its diagrams, and a cold start is not the slow one.

FROM golang:1.27-alpine AS build
WORKDIR /src

# The module has no dependencies, so there is nothing to download and nothing to
# cache: the source is the whole input.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cw .

FROM golang:1.27-alpine AS assets
COPY --from=build /out/cw /usr/local/bin/cw
# XDG_CACHE_HOME is where the vendored assets land, and it is copied into the
# runtime image as it is.
ENV XDG_CACHE_HOME=/vendor
RUN apk add --no-cache ca-certificates && cw cache warm

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cw /cw
COPY --from=assets --chown=nonroot:nonroot /vendor /vendor

ENV XDG_CACHE_HOME=/vendor \
    CW_DATA=/data \
    CW_ADDR=:8080
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/cw"]
CMD ["host"]
