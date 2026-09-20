# Build kimg without installing Go on the host.
#
#   docker build --output type=local,dest=. .
#   install -m 755 kimg ~/.local/bin/
#
# The final stage is empty on purpose: `--output type=local` writes just the
# binary next to you, and no image is added to your image store.
#
# Podman works the same way, and finds this file without -f:
#
#   podman build --output type=local,dest=. .

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first, so editing the sources does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

# buildx sets these from --platform, defaulting to the host's own platform.
# CGO is off, so cross building needs no extra toolchain.
ARG TARGETOS TARGETARCH

# The build context carries no .git, so the version cannot be read from the
# repository. Stamp it instead: docker build --build-arg VERSION=v0.1.0 ...
ARG VERSION=devel
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/kimg .

# Run the tests instead of exporting a binary: docker build --target test .
FROM build AS test
RUN go vet ./... && go test ./...

FROM scratch
COPY --from=build /out/kimg /kimg
