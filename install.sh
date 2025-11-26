#!/usr/bin/env bash
set -euo pipefail

print_info() { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
print_success() { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
print_error() { printf "\033[1;31m==>\033[0m %s\n" "$1"; }

install_bv() {
    local bin_name="bv"
    local install_path="/usr/local/bin/$bin_name"

    print_info "Checking requirements..."
    if ! command -v go >/dev/null 2>&1; then
        print_error "Go is not installed. Please install Go (golang.org) to build from source."
        exit 1
    fi

    print_info "Building bv..."
    # Build to a temporary file first
    local tmp_bin
    tmp_bin=$(mktemp)
    
    if go build -o "$tmp_bin" cmd/bv/main.go; then
        print_info "Installing to $install_path..."
        # Check if we need sudo
        if [ -w "$(dirname "$install_path")" ]; then
            mv "$tmp_bin" "$install_path"
        else
            sudo mv "$tmp_bin" "$install_path"
        fi
        chmod +x "$install_path"
        print_success "Successfully installed $bin_name!"
        print_info "Run '$bin_name' in any initialized beads project to view issues."
    else
        rm -f "$tmp_bin"
        print_error "Build failed."
        exit 1
    fi
}

install_bv
