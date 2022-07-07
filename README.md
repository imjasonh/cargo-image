# cargo-image

**This is an experimental work in progress**

The goal is to provide a Cargo plugin that will build a minimal OCI image from Rust source, containing a static binary on a distroless base image.

Like [`ko`](https://github.com/google/ko) but for Rust, invoked as `cargo image`.

This prototype is written in Go to be able to take advantage of [go-containerregistry](https://github.com/google/go-containerregistry) for registry operations, but we should rewrite it in Rust and build/use a Rust OCI crate.

