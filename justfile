set shell := ["bash", "-euo", "pipefail", "-c"]

build_dir := "build"
version := `scripts/version.py full`
version_numeric := `scripts/version.py numeric`

default:
    @just --list

# Debug build. Includes the fake WhatsApp server.
build dir=build_dir:
    @just _build debug "{{dir}}"

# Optimized release build.
build-release dir=build_dir:
    @just _build release "{{dir}}"

# Build and install an optimized release.
install prefix="/usr/local" destdir="":
    @just _install release "{{prefix}}" "{{destdir}}"

# Build and install a debug build for smoke tests.
install-dev prefix="/usr/local" destdir="":
    @just _install debug "{{prefix}}" "{{destdir}}"

# Build release source/binary artifacts and checksums.
artifacts arch=`uname -m`:
    @just _source-tarball
    @just _binary-tarball "{{arch}}"
    @just _checksums

# Run tests. Target: all, daemon, whattui, frontend, protocol, asan.
test target="all" dir=build_dir:
    @case "{{target}}" in \
        all) just _test-daemon && just _test-whattui && just _test-frontend "{{dir}}" && scripts/conformance ;; \
        daemon) just _test-daemon ;; \
        whattui) just _test-whattui ;; \
        frontend) just _test-frontend "{{dir}}" ;; \
        protocol) scripts/conformance ;; \
        asan) scripts/asan "{{dir}}" ;; \
        *) printf 'unknown target: %s\n' "{{target}}" >&2; exit 1 ;; \
    esac

_test-daemon:
    @cd whatevrd && go test -tags sqlite_fts5 ./...
    @cd whatevrd && go test -tags "sqlite_fts5 whatevr_mock" ./internal/wamock/...
    @scripts/check-mock-gate

# Race is not optional: the protocol client, the view models and the render
# loop are three goroutines over one model.
_test-whattui:
    @just _require-vaxis
    @cd whattui && files="$(gofmt -l . | grep -v '^vaxis/' || true)"; \
        if [ -n "$files" ]; then printf '%s\n' "$files"; exit 1; fi
    @cd whattui && go vet ./...
    @cd whattui && go test ./...
    @cd whattui && go test -race ./...

_test-frontend dir=build_dir:
    @just build "{{dir}}"
    @ctest --test-dir "{{dir}}/debug/whatkevr" --output-on-failure

# Photograph whattui at any size and state, in kitty on a dummy monitor.
# Needs a compositor, so it is not in `just test`.
#
#   just screenshot --list
#   just screenshot --size 100x30 --scenario palette
#   just screenshot --size 72x20 --keys ctrl+p,/,a,n --wait "> /an"
#   just screenshot --env WHATTUI_NO_TEXT_SCALE=1
#   just screenshot --account torture --size 120x40

# Take a picture of whattui.
screenshot *args:
    @scripts/whattui-screenshot {{args}}

validate:
    @desktop-file-validate whatkevr/data/in.codelif.Whatevr.desktop
    @appstreamcli validate --no-net whatkevr/data/in.codelif.Whatevr.metainfo.xml
    @xmllint --noout whatkevr/data/in.codelif.Whatevr.xml

version:
    @printf '%s\n' '{{version}}'

# Update metadata, validate, commit, and tag. Does not push. x.y.z
release version:
    @uv run scripts/release.py "{{version}}"

# Regenerate the bundled chat-wallpaper doodle (seed defaults to the version).
gen-doodle seed=version_numeric:
    @scripts/gen_doodle.py --seed "{{seed}}"

uninstall prefix="/usr/local" destdir="":
    @prefix="{{prefix}}"; \
    destdir="{{destdir}}"; \
    rm -f "$destdir$prefix/bin/whatevrd"; \
    rm -f "$destdir$prefix/bin/whattui"; \
    rm -f "$destdir$prefix/bin/whatkevr"; \
    rm -f "$destdir$prefix/lib/systemd/user/whatevrd.service"; \
    rm -f "$destdir$prefix/lib/systemd/user/whatevrd.socket"; \
    rm -f "$destdir$prefix/share/applications/in.codelif.Whatevr.desktop"; \
    rm -f "$destdir$prefix/share/metainfo/in.codelif.Whatevr.metainfo.xml"; \
    rm -f "$destdir$prefix/share/mime/packages/in.codelif.Whatevr.xml"; \
    rm -f "$destdir$prefix/share/icons/hicolor/scalable/apps/in.codelif.Whatevr.svg"

clean:
    @rm -rf {{build_dir}}

_build profile dir=build_dir:
    @test "{{profile}}" = debug -o "{{profile}}" = release
    @just _build-daemon "{{profile}}" "{{dir}}"
    @just _build-whattui "{{profile}}" "{{dir}}"
    @just _build-frontend "{{profile}}" "{{dir}}"

# whattui builds against the vaxis fork in whattui/vaxis, which is a submodule.
# A release tarball is `git archive`, which carries no submodule, so a build
# from one skips whattui rather than failing.
_require-vaxis:
    @if [ ! -f whattui/vaxis/go.mod ]; then \
        printf 'whattui/vaxis is empty: run git submodule update --init --recursive\n' >&2; \
        exit 1; \
    fi

_build-whattui profile dir=build_dir:
    @if [ ! -f whattui/vaxis/go.mod ]; then \
        printf 'skipping whattui: whattui/vaxis is not checked out\n' >&2; \
        exit 0; \
    fi; \
    profile="{{profile}}"; \
    build_root="{{dir}}"; \
    case "$build_root" in \
        /*) out_dir="$build_root/$profile" ;; \
        *) out_dir="$(pwd)/$build_root/$profile" ;; \
    esac; \
    go_flags=(-buildvcs=false); \
    ldflags=""; \
    if [ "$profile" = release ]; then \
        go_flags=(-trimpath "${go_flags[@]}"); \
        ldflags="-s -w"; \
    fi; \
    mkdir -p "$out_dir"; \
    go -C whattui build "${go_flags[@]}" -ldflags "$ldflags" \
        -o "$out_dir/whattui" ./cmd/whattui

_build-daemon profile dir=build_dir:
    @profile="{{profile}}"; \
    build_root="{{dir}}"; \
    case "$build_root" in \
        /*) out_dir="$build_root/$profile" ;; \
        *) out_dir="$(pwd)/$build_root/$profile" ;; \
    esac; \
    ldflags="-X whatevrd/internal/protocol.Version={{version}}"; \
    tags="sqlite_fts5"; \
    if [ "$profile" = release ]; then \
        go_flags=(-trimpath -buildvcs=false); \
        ldflags="$ldflags -s -w"; \
    else \
        tags="$tags whatevr_mock"; \
        go_flags=(-buildvcs=false); \
    fi; \
    go_flags+=(-tags "$tags"); \
    mkdir -p "$out_dir"; \
    CGO_ENABLED=1 go -C whatevrd build "${go_flags[@]}" -ldflags "$ldflags" \
        -o "$out_dir/whatevrd" ./cmd/whatevrd

_build-frontend profile dir=build_dir:
    @profile="{{profile}}"; \
    if [ "$profile" = release ]; then build_type=Release; else build_type=Debug; fi; \
    scripts/configure-frontend \
        --build "{{dir}}/$profile/whatkevr" \
        --build-type "$build_type" \
        --version {{version_numeric}} \
        --version-full {{version}}; \
    cmake --build "{{dir}}/$profile/whatkevr"

_install profile prefix destdir:
    @just _build "{{profile}}"
    @profile="{{profile}}"; \
    prefix="{{prefix}}"; \
    destdir="{{destdir}}"; \
    build_root="{{build_dir}}/$profile"; \
    bindir="$prefix/bin"; \
    user_unit_dir="$prefix/lib/systemd/user"; \
    install -Dm755 "$build_root/whatevrd" "$destdir$bindir/whatevrd"; \
    if [ -f "$build_root/whattui" ]; then \
        install -Dm755 "$build_root/whattui" "$destdir$bindir/whattui"; \
    fi; \
    DESTDIR="$destdir" cmake --install "$build_root/whatkevr" --prefix "$prefix"; \
    sed "s|@BINDIR@|$bindir|g" packaging/systemd/whatevrd.service.in \
        > "$build_root/whatevrd.service"; \
    install -Dm644 "$build_root/whatevrd.service" \
        "$destdir$user_unit_dir/whatevrd.service"; \
    install -Dm644 packaging/systemd/whatevrd.socket \
        "$destdir$user_unit_dir/whatevrd.socket"

_source-tarball:
    @version="{{version}}"; \
    mkdir -p {{build_dir}}; \
    git archive --format=tar --prefix="whatevr-$version/" HEAD \
        > "{{build_dir}}/whatevr-$version.tar"; \
    printf '%s\n' "$version" > {{build_dir}}/VERSION; \
    tar --transform "s,^,whatevr-$version/," \
        -rf "{{build_dir}}/whatevr-$version.tar" -C {{build_dir}} VERSION; \
    gzip -f "{{build_dir}}/whatevr-$version.tar"; \
    printf 'wrote {{build_dir}}/whatevr-%s.tar.gz\n' "$version"

_binary-tarball arch:
    @version="{{version}}"; \
    name="whatevr-$version-linux-{{arch}}"; \
    root="$(pwd)/{{build_dir}}/release/dist-bin-root"; \
    dist_dir="$(pwd)/{{build_dir}}/$name"; \
    rm -rf "$root" "$dist_dir" "$(pwd)/{{build_dir}}/$name.tar.zst"; \
    just _install release /usr "$root"; \
    strip --strip-unneeded "$root/usr/bin/whatevrd"; \
    if [ -f "$root/usr/bin/whattui" ]; then \
        strip --strip-unneeded "$root/usr/bin/whattui"; \
    fi; \
    strip --strip-unneeded "$root/usr/bin/whatkevr"; \
    install -Dm644 LICENSE "$root/usr/share/licenses/whatevr/LICENSE"; \
    mkdir -p "$dist_dir"; \
    cp -a "$root/usr" "$dist_dir/"; \
    tar -C {{build_dir}} -caf "$(pwd)/{{build_dir}}/$name.tar.zst" "$name"; \
    printf 'wrote {{build_dir}}/%s.tar.zst\n' "$name"

_checksums:
    @cd {{build_dir}} && sha256sum whatevr-*.tar.gz whatevr-*.tar.zst > SHA256SUMS
    @printf 'wrote {{build_dir}}/SHA256SUMS\n'
