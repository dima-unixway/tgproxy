FROM golang:1.25-alpine AS tdlib-builder

ARG TDLIB_COMMIT=22d49d5b87a4d5fc60a194dab02dd1d71529687f

# Stage 1: Build static tdlib libraries using Alpine (musl-based for static linking)

RUN apk add --no-cache git cmake gperf gcc g++ musl-dev make zlib-dev zlib-static \
    openssl-dev openssl-libs-static linux-headers bash pkgconf-dev coreutils

WORKDIR /tdlib

RUN git clone --recursive https://github.com/tdlib/td.git .

RUN git fetch origin ${TDLIB_COMMIT} && \
    git checkout ${TDLIB_COMMIT} && \
    git submodule update --init --recursive

RUN mkdir build && \
    cd build && \
    cmake build \
          -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
          -DCMAKE_BUILD_TYPE=Release \
          -DBUILD_SHARED_LIBS=OFF \
          -DTD_INSTALL_STATIC_LIBRARIES=ON \
          -DTD_INSTALL_SHARED_LIBRARIES=OFF \
          -DOPENSSL_USE_STATIC_LIBS=ON \
          -DZLIB_USE_STATIC_LIBS=ON \
          -DTD_ENABLE_LTO=ON \
          -DCMAKE_CXX_FLAGS="-static-libstdc++ -O3" \
          -DCMAKE_C_FLAGS="-O3" \
          -DCMAKE_EXE_LINKER_FLAGS="-static" \
          .. && \
    make -j$(nproc) tdjson_static

# Stage 2: Build static Go binary

RUN mkdir -p /usr/local/lib/pkgconfig /usr/local/include/td || true
RUN find /tdlib/build -name '*.a' -exec cp {} /usr/local/lib/ \;
RUN cp -r /tdlib/build/pkgconfig/* /usr/local/lib/pkgconfig/
RUN cp -r /tdlib/build/td/* /usr/local/include/td/
RUN cp -r /tdlib/td/* /usr/local/include/td/

# Set pkg-config path for tdjson (assumes your Go code uses `#cgo pkg-config: tdjson`)
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig
ENV CGO_CFLAGS="-I/usr/local/include"
ENV CGO_LDFLAGS="-L/usr/local/lib -static"

WORKDIR /app

# Copy Go source files from current directory
COPY . .

# Build fully static binary (adjust binary name/path if needed, e.g., ./cmd/tgproxy)
# Assumes your project builds with 'go build' (uses go.mod/go.sum if present)
# Flags ensure static linking with musl libc and tdlib static libs
RUN CGO_ENABLED=1 \
    go build \
    -a \
    -trimpath \
    -tags netgo,osusergo \
    -ldflags '-s -w -extldflags "-static"' \
    -o tgproxy .

# Stage 3: Runtime (scratch for minimal static binary)
FROM scratch

# Copy the static binary
COPY --from=tdlib-builder /app/tgproxy /tgproxy

# Set entrypoint
ENTRYPOINT ["/tgproxy"]
