package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: package-mac ARCHIVE SIGNED_BUNDLE")
		os.Exit(2)
	}
	if err := pack(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pack(archive, bundle string) error {
	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for i, path := range []string{"MacOS/IKUNBridge", "Info.plist", "Resources/Bridge.icns", "Resources/Lucide-LICENSE.txt", "_CodeSignature/CodeResources"} {
		header := &zip.FileHeader{Name: "IKUN Bridge.app/Contents/" + path, Method: zip.Deflate}
		mode := os.FileMode(0o644)
		if i == 0 {
			mode = 0o755
		}
		header.SetMode(mode)
		entry, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		src, err := os.Open(filepath.Join(bundle, "Contents", filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, src)
		src.Close()
		if err != nil {
			return err
		}
	}
	return w.Close()
}
