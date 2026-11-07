# syntax=docker/dockerfile:1

# build
FROM --platform=$BUILDPLATFORM golang:1.27 AS build

# The exact Go version comes from the toolchain directive in go.mod (the
# official image defaults to GOTOOLCHAIN=local, which would ignore it).
ENV GOTOOLCHAIN=auto CGO_ENABLED=0

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    for bin in api:./cmd/api migrate:./cmd/migrate; do \
      GOOS=$TARGETOS GOARCH=$TARGETARCH \
        go build -trimpath -ldflags="-s -w" -o "/out/${bin%%:*}" "${bin#*:}" || exit 1; \
    done

# runtime
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate

# PostgreSQL migrations are embedded in both binaries. The MongoDB migrator
# reads these files, resolved relative to /app (see cmd/migrate/mongo.yaml).
# Run migrations as a deployment step:   docker run --entrypoint /app/migrate <image>
# or at startup with MIGRATE_ON_START=true.
COPY cmd/migrate/mongo.yaml /app/cmd/migrate/mongo.yaml
COPY cmd/migrate/seeds /app/cmd/migrate/seeds
COPY internal/platform/database/mongodb/migrations/schema /app/internal/platform/database/mongodb/migrations/schema

# Configuration comes from the environment (see .env.example). JWT keys must be
# mounted into the container, e.g. at /run/secrets, and pointed to with
# JWT_PRIVATE_KEY_PATH / JWT_PUBLIC_KEY_PATH.
ENV PORT=8000
EXPOSE 8000

USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
