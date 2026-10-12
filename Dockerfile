# MyCalendar in one image.
#
#   docker build -t mycalendar .
#   docker run -d --name mycalendar -p 8080:8080 -v mycalendar-data:/data mycalendar
#
# Three stages: the frontend is compiled by Node, the resulting bundle is
# embedded into the Go binary, and only that binary is shipped -- the runtime
# image is scratch, so it carries no shell, no package manager and no libc.
#
# The binary must therefore be fully static, which is why CGO is off; the
# pure-Go SQLite driver makes that possible, and time/tzdata is embedded so
# named zones resolve without /usr/share/zoneinfo.

# --------------------------------------------------------------- frontend
FROM node:24-alpine AS web

WORKDIR /src

# Dependencies first so a source-only change does not reinstall them.
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci

COPY web ./web/
# Vite writes the bundle to ../internal/webui/dist, which is where the Go
# embed directive expects it. Vite creates the directory.
RUN cd web && npm run build

# --------------------------------------------------------------- backend
FROM golang:1.27-alpine AS api

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
# The generated bundle is not in the build context (see .dockerignore), so it
# comes across from the frontend stage.
COPY --from=web /src/internal/webui/dist ./internal/webui/dist

# -trimpath keeps build paths out of the binary; -s -w drops the symbol table
# and DWARF data, which scratch has no use for.
ARG VERSION=docker
RUN CGO_ENABLED=0 GOOS=linux go build \
        -tags webui_embedded \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/mycalendar ./cmd/mycalendar

# Prove the result really is static before it reaches scratch, where a dynamic
# binary fails at startup with a bare "no such file or directory".
#
# The obvious `file ... | grep "statically linked"` cannot be used: the
# golang:alpine image has no `file`. A dynamic executable always names its ELF
# interpreter, so looking for that string is both sufficient and dependency
# free -- `grep` comes from busybox. Verified to pass for a CGO_ENABLED=0 build
# and to fail for a CGO_ENABLED=1 one.
RUN if grep -qa 'ld-linux\|ld-musl\|/lib/ld-' /out/mycalendar; then \
        echo "ERROR: the binary is not static, scratch cannot run it" >&2; \
        exit 1; \
    fi

# --------------------------------------------------------------- runtime
FROM scratch

COPY --from=api /out/mycalendar /mycalendar

# Sensible defaults for an intranet deployment. A scratch image has no
# /etc/localtime, so without MYCAL_TZ the server timezone would be UTC.
ENV MYCAL_ADDR=":8080" \
    MYCAL_DB="/data/mycalendar.db" \
    MYCAL_TZ="Asia/Shanghai" \
    MYCAL_WEEK_START="monday" \
    MYCAL_LOG_LEVEL="info"

# The store creates /data on first start, so mounting a fresh volume is enough.
# /data is deliberately not declared as VOLUME: that would silently create a new
# anonymous volume per container and lose the calendar on every recreate.
WORKDIR /
EXPOSE 8080

ENTRYPOINT ["/mycalendar"]
