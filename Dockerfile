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
    for bin in api:./cmd migrate:./cmd/migration; do \
      GOOS=$TARGETOS GOARCH=$TARGETARCH \
        go build -trimpath -ldflags="-s -w" -o "/out/${bin%%:*}" "${bin#*:}" || exit 1; \
    done

# runtime
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate

# The migrator resolves these paths relative to /app (see cmd/migration/mongo.yaml):
#   docker run --entrypoint /app/migrate <image> -c ./cmd/migration/mongo.yaml
COPY cmd/migration/mongo.yaml /app/cmd/migration/mongo.yaml
COPY cmd/migration/seeds /app/cmd/migration/seeds
COPY internal/database/mongodb/migrations/schema /app/internal/database/mongodb/migrations/schema

# Configuration comes from the environment (see .env.example). JWT keys must be
# mounted into the container, e.g. at /run/secrets, and pointed to with
# JWT_PRIVATE_KEY_PATH / JWT_PUBLIC_KEY_PATH.
ENV PORT=8000
EXPOSE 8000

USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
