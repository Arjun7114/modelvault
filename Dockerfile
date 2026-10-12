# ---- build stage ----
FROM golang:1.27 AS build
WORKDIR /src

# Cache deps first (layer stays cached unless go.mod/go.sum change).
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled -> a fully static binary, so the final image needs no libc.
# -ldflags "-s -w" strips debug info to shrink the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/modelvault ./cmd/modelvault

# ---- runtime stage ----
# distroless static: no shell, no package manager, runs as nonroot. It includes
# CA certificates, which the S3/Azure HTTPS calls need.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/modelvault /modelvault
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/modelvault"]
CMD ["serve", "--addr", ":8080"]