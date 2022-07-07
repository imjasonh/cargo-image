package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// TODO: multiplatform based on platforms provided by base image and --platforms flag
// TODO: rewrite it all in rust
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Build the binary to a temp file.

	if err := run(ctx, "rustup", "target", "add", "x86_64-unknown-linux-musl"); err != nil {
		log.Fatal("rustup target add:", err)
	}
	tmpdir, err := ioutil.TempDir("", "cargo-image-*")
	if err != nil {
		log.Fatal("creating temp file:", err)
	}
	// TODO: --out-dir to tmpdir; out-dir is only available on rust nightly
	// nightly doesn't seem to work on aarch64/darwin
	// https://github.com/rust-lang/cargo/issues/6790
	// TODO: specify input src
	// TODO: specify build flags
	if err := run(ctx, "cargo", "build", "--target", "x86_64-unknown-linux-musl"); err != nil {
		log.Fatal("cargo build:", err)
	}

	// Construct a tarball with the binary and produce a layer.

	buf := bytes.NewBuffer(nil)
	tw := tar.NewWriter(buf)
	defer tw.Close()

	if err := filepath.Walk(tmpdir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %q: %w", path, err)
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		// write the header to the tarball archive
		if err := tw.WriteHeader(&tar.Header{
			Name:     "rust-bin", // TODO: better name
			Size:     stat.Size(),
			Typeflag: tar.TypeReg,
			// Use a fixed Mode, so that this isn't sensitive to the directory and umask
			// under which it was created. Additionally, windows can only set 0222,
			// 0444, or 0666, none of which are executable.
			Mode: 0555,
		}); err != nil {
			return fmt.Errorf("writing header: %w", err)
		}
		// copy the file data to the tarball
		if _, err := io.Copy(tw, file); err != nil {
			return fmt.Errorf("writing tar entry: %w", err)
		}
		return nil
	}); err != nil {
		log.Fatal("walk:", err)
	}
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return ioutil.NopCloser(buf), nil
	}, tarball.WithCompressedCaching)
	if err != nil {
		log.Fatal("tarball layer:", err)
	}

	// Pull the base image, append the new layer, and push.

	// TODO: specify base image
	base, err := remote.Image(name.MustParseReference("ghcr.io/distroless/static:latest"),
		remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		log.Fatal("pulling base:", err)
	}

	img, err := mutate.AppendLayers(base, l)
	if err != nil {
		log.Fatal("append:", err)
	}

	// TODO: specify output ref
	ref := name.MustParseReference("gcr.io/jason-chainguard/cargo-built")
	if err := remote.Write(ref, img,
		remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		log.Fatal("pushing:", err)
	}

	d, err := img.Digest()
	if err != nil {
		log.Fatalf("digest:", err)
	}
	fmt.Println(ref.Context().Digest(d.String())) // Print reference by digest.
}

func run(ctx context.Context, s ...string) error {
	log.Println("Running:", strings.Join(s, " "))
	var c *exec.Cmd
	switch len(s) {
	case 0:
		return errors.New("no command or args")
	case 1:
		c = exec.CommandContext(ctx, s[0])
	default:
		c = exec.CommandContext(ctx, s[0], s[1:]...)
	}
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = os.Environ()
	return c.Run()
}
